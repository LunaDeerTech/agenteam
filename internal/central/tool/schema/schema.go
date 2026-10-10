// Package schema compiles immutable Tool schemas with the standard Draft
// 2020-12 implementation. It grants no current registration or execution right.
package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/dlclark/regexp2"
	js "github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	Draft            = "https://json-schema.org/draft/2020-12/schema"
	MaxSchemaBytes   = tc.MaxSchemaBytes
	MaxInstanceBytes = 1 << 20
	maxDepth         = 32 // Same strict JSON nesting boundary as Registry definitions.
	regexBudget      = 100 * time.Millisecond
	resourceURL      = "urn:agenteam:tool-schema"
)

// Compiled does not expose the library's mutable Schema graph or schema text.
// A caller can reuse it concurrently. Each call retains its original context
// and owns all synchronous validation; cancellation never detaches a worker.
type Compiled struct{ data func() *compiledState }
type compiledState struct {
	gate   chan struct{}
	schema *js.Schema
	run    *matchState
}
type matchState struct {
	ctx context.Context
	err error
}

// Compile accepts one in-memory compound schema document. $defs, embedded $id,
// anchors and dynamic references remain standard library operations. Missing
// external resources and other dialects are unsupported, never fetched or
// silently stripped. Format/content retain the standard annotation defaults.
func Compile(ctx context.Context, raw []byte) (out Compiled, err error) {
	if err = checkContext(ctx); err != nil {
		return out, err
	}
	defer func() {
		if recover() != nil {
			out = Compiled{}
			err = fault(f.SchemaUnsupported)
		}
	}()
	if len(raw) > MaxSchemaBytes {
		return out, fault(f.PayloadTooLarge)
	}
	doc, err := decode(ctx, raw)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, fault(f.SchemaUnsupported)
	}
	policy := documentPolicy{ctx: ctx, doc: doc, resources: map[string]string{}, checked: map[string]bool{}, anchors: map[string][]string{}}
	if err = policy.walk(doc, "", "", true); err != nil {
		return out, err
	}
	run := &matchState{ctx: ctx}
	compiler := js.NewCompiler()
	compiler.DefaultDraft(js.Draft2020)
	compiler.UseLoader(noResources{})
	compiler.UseRegexpEngine(run.compile)
	if err = compiler.AddResource(resourceURL, doc); err != nil {
		return out, fault(f.SchemaUnsupported)
	}
	compiled, compileErr := compiler.Compile(resourceURL)
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if run.err != nil {
		return out, run.err
	}
	if compileErr != nil {
		return out, fault(f.SchemaUnsupported)
	}
	if err = policy.compiled(compiler, compiled); err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if run.err != nil {
		return out, run.err
	}
	run.ctx = nil
	state := &compiledState{gate: make(chan struct{}, 1), schema: compiled, run: run}
	state.gate <- struct{}{}
	return Compiled{data: func() *compiledState { return state }}, nil
}

// Validate returns only fixed safe faults or the original context cancellation.
// Library diagnostics include instance values and schema locations, so neither
// their message nor their error chain is retained. Input bytes are never edited.
func (c Compiled) Validate(ctx context.Context, raw []byte) (err error) {
	if err = checkContext(ctx); err != nil {
		return err
	}
	if c.data == nil {
		return fault(f.DependencyUnbound)
	}
	if len(raw) > MaxInstanceBytes {
		return fault(f.PayloadTooLarge)
	}
	doc, err := decode(ctx, raw)
	if err != nil {
		return err
	}
	s := c.data()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.gate:
	}
	defer func() {
		s.run.ctx, s.run.err = nil, nil
		s.gate <- struct{}{}
		if recover() != nil {
			err = fault(f.DependencyUnavailable)
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	s.run.ctx = ctx
	validationErr := s.schema.Validate(doc)
	if err = ctx.Err(); err != nil {
		return err
	}
	if s.run.err != nil {
		return s.run.err
	}
	if validationErr != nil {
		return fault(f.InvalidArgument)
	}
	return nil
}

// This loader cannot read host paths or initiate network requests. Built-in
// standard metaschemas are embedded by the library; all other resources must
// resolve within the document already added to the compiler.
type noResources struct{}

func (noResources) Load(string) (any, error) { return nil, fault(f.SchemaUnsupported) }

type boundedRegexp struct {
	expression *regexp2.Regexp
	state      *matchState
}

func (r *boundedRegexp) String() string { return r.expression.String() }
func (r *boundedRegexp) MatchString(value string) bool {
	if r.state.err != nil {
		return false
	}
	ctx := r.state.ctx
	if ctx == nil {
		r.state.err = fault(f.DependencyUnavailable)
		return false
	}
	if err := ctx.Err(); err != nil {
		r.state.err = err
		return false
	}
	budget := regexBudget
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < budget {
			budget = remaining
		}
	}
	if budget <= 0 {
		r.state.err = context.DeadlineExceeded
		return false
	}
	r.expression.MatchTimeout = budget
	matched, err := r.expression.MatchString(value) // Actual synchronous return.
	if cancelled := ctx.Err(); cancelled != nil {
		r.state.err = cancelled
		return false
	}
	if err != nil {
		r.state.err = fault(f.DependencyUnavailable)
		return false
	}
	return matched
}
func (s *matchState) compile(expression string) (js.Regexp, error) {
	if s.ctx == nil {
		return nil, fault(f.DependencyUnavailable)
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	re, err := regexp2.Compile(expression, regexp2.ECMAScript)
	if err != nil {
		return nil, fault(f.SchemaUnsupported)
	}
	re.MatchTimeout = regexBudget
	return &boundedRegexp{re, s}, nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	return ctx.Err()
}
func fault(code f.Code) error { return f.NewFault(code, f.NotStarted) }

// documentPolicy checks dialect/resource policy, not validation semantics.
// Only standard subschema positions and actual compiled reference targets are
// schema objects. Literal const/default data is never recursively classified.
// Locations from the library retain the retrieval document URL and JSONPointer,
// including for embedded $id resources; this also excludes embedded metaschemas
// reached through $ref, which the library can load without calling our loader.
type documentPolicy struct {
	ctx       context.Context
	doc       any
	resources map[string]string // discovered schema pointer -> containing resource pointer
	checked   map[string]bool   // discovery alone does not classify legacy annotations
	anchors   map[string][]string
}

func (p *documentPolicy) walk(value any, pointer, resource string, check bool) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if _, seen := p.resources[pointer]; seen && (!check || p.checked[pointer]) {
		return nil
	}
	if _, ok := value.(bool); ok {
		p.resources[pointer] = resource
		p.checked[pointer] = check
		return nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		if !check {
			return nil
		}
		return fault(f.SchemaUnsupported)
	}
	if raw, exists := m["$schema"]; check && exists && raw != Draft {
		return fault(f.SchemaUnsupported)
	}
	if vocabulary, ok := m["$vocabulary"].(map[string]any); check && ok {
		for name, required := range vocabulary {
			if required != true {
				continue
			}
			switch name {
			case "https://json-schema.org/draft/2020-12/vocab/core", "https://json-schema.org/draft/2020-12/vocab/applicator", "https://json-schema.org/draft/2020-12/vocab/unevaluated", "https://json-schema.org/draft/2020-12/vocab/validation", "https://json-schema.org/draft/2020-12/vocab/meta-data", "https://json-schema.org/draft/2020-12/vocab/format-annotation", "https://json-schema.org/draft/2020-12/vocab/format-assertion", "https://json-schema.org/draft/2020-12/vocab/content":
			default:
				return fault(f.SchemaUnsupported)
			}
		}
	}
	if id, ok := m["$id"].(string); ok && id != "" {
		resource = pointer
	}
	p.resources[pointer] = resource
	p.checked[pointer] = check
	if _, ok := m["$dynamicAnchor"].(string); ok {
		p.anchors[resource] = append(p.anchors[resource], pointer)
	}
	child := func(key string, value any, verify bool) error {
		return p.walk(value, pointer+"/"+pointerToken(key), resource, verify)
	}
	for _, key := range []string{"additionalProperties", "unevaluatedProperties", "propertyNames", "items", "contains", "unevaluatedItems", "not", "if", "then", "else", "contentSchema", "additionalItems"} {
		if value, exists := m[key]; exists {
			// additionalItems is an annotation in 2020-12. Discover possible
			// resources/anchors exactly as the compiler does, but classify its
			// contents only if a compiled reference/anchor actually reaches them.
			verify := check && key != "additionalItems"
			if err := child(key, value, verify); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"$defs", "definitions", "properties", "patternProperties", "dependentSchemas", "dependencies"} {
		if children, ok := m[key].(map[string]any); ok {
			for name, value := range children {
				// The pinned compiler recognizes legacy schema containers too.
				// Property-dependency string arrays are values, not schemas.
				if key == "dependencies" {
					if _, names := value.([]any); names {
						continue
					}
				}
				if err := p.walk(value, pointer+"/"+key+"/"+pointerToken(name), resource, check); err != nil {
					return err
				}
			}
		}
	}
	for _, key := range []string{"prefixItems", "allOf", "anyOf", "oneOf"} {
		if children, ok := m[key].([]any); ok {
			for n, value := range children {
				if err := p.walk(value, pointer+"/"+key+"/"+strconv.Itoa(n), resource, check); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func pointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func (p *documentPolicy) at(location string) (any, string, error) {
	fragment, ok := strings.CutPrefix(location, resourceURL+"#")
	if !ok {
		return nil, "", fault(f.SchemaUnsupported)
	}
	pointer, err := url.PathUnescape(fragment)
	if err != nil || (pointer != "" && !strings.HasPrefix(pointer, "/")) {
		return nil, "", fault(f.SchemaUnsupported)
	}
	value := p.doc
	if pointer != "" {
		for _, token := range strings.Split(pointer[1:], "/") {
			if err := p.ctx.Err(); err != nil {
				return nil, "", err
			}
			token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
			switch parent := value.(type) {
			case map[string]any:
				value, ok = parent[token]
			case []any:
				n, parseErr := strconv.Atoi(token)
				ok = parseErr == nil && n >= 0 && n < len(parent)
				if ok {
					value = parent[n]
				}
			default:
				ok = false
			}
			if !ok {
				return nil, "", fault(f.SchemaUnsupported)
			}
		}
	}
	return value, pointer, nil
}

func (p *documentPolicy) compiled(compiler *js.Compiler, root *js.Schema) error {
	queue := []*js.Schema{root}
	seen := map[*js.Schema]bool{}
	anchors := map[string]bool{}
	// All public compiled schema edges in the pinned library, including legacy
	// edges it can retain. Values/annotations and regex implementation fields
	// are deliberately not inspected. No private reflection or unsafe access.
	var add func(any)
	add = func(value any) {
		switch value := value.(type) {
		case *js.Schema:
			if value != nil && !seen[value] {
				queue = append(queue, value)
			}
		case []*js.Schema:
			for _, child := range value {
				add(child)
			}
		case map[string]*js.Schema:
			for _, child := range value {
				add(child)
			}
		case map[js.Regexp]*js.Schema:
			for _, child := range value {
				add(child)
			}
		case map[string]any:
			for _, child := range value {
				add(child)
			}
		}
	}
	for len(queue) != 0 {
		if err := p.ctx.Err(); err != nil {
			return err
		}
		s := queue[0]
		queue = queue[1:]
		if seen[s] {
			continue
		}
		seen[s] = true
		if s.DraftVersion != 2020 {
			return fault(f.SchemaUnsupported)
		}
		value, pointer, err := p.at(s.Location)
		if err != nil {
			return err
		}
		// A newly promoted reference target inherits the nearest already known
		// resource, just as ensureSubschema does before processing its own $id.
		parent := pointer
		for {
			if _, known := p.resources[parent]; known {
				break
			}
			n := strings.LastIndexByte(parent, '/')
			if n < 0 {
				parent = ""
				break
			}
			parent = parent[:n]
		}
		if err := p.walk(value, pointer, p.resources[parent], true); err != nil {
			return err
		}
		// Resource dynamic anchors are compiled by the library even when their
		// only edge is its private dynamic-anchor table. Reuse public Compile on
		// exactly those original locations; do not force unrelated resources.
		for _, pointer := range p.anchors[p.resources[pointer]] {
			if anchors[pointer] {
				continue
			}
			anchors[pointer] = true
			anchor, err := compiler.Compile(resourceURL + "#" + url.PathEscape(pointer))
			if err != nil {
				if p.ctx.Err() != nil {
					return p.ctx.Err()
				}
				return fault(f.SchemaUnsupported)
			}
			add(anchor)
		}
		for _, edge := range []any{s.Ref, s.RecursiveRef, s.Not, s.AllOf, s.AnyOf, s.OneOf, s.If, s.Then, s.Else,
			s.PropertyNames, s.Properties, s.PatternProperties, s.AdditionalProperties, s.Dependencies,
			s.DependentSchemas, s.UnevaluatedProperties, s.Contains, s.Items, s.AdditionalItems,
			s.PrefixItems, s.Items2020, s.UnevaluatedItems, s.ContentSchema} {
			add(edge)
		}
		if s.DynamicRef != nil {
			add(s.DynamicRef.Ref)
		}
	}
	return p.ctx.Err()
}

func decode(ctx context.Context, raw []byte) (any, error) {
	if !utf8.Valid(raw) || !validScalars(ctx, raw) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fault(f.InvalidArgument)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	value, err := jsonValue(ctx, d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fault(f.InvalidArgument)
	}
	return value, ctx.Err()
}
func jsonValue(ctx context.Context, d *json.Decoder, depth int) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > maxDepth {
		return nil, fault(f.InvalidArgument)
	}
	token, err := d.Token()
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			value := map[string]any{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok {
					return nil, fault(f.InvalidArgument)
				}
				if _, exists := value[name]; exists {
					return nil, fault(f.InvalidArgument)
				}
				child, err := jsonValue(ctx, d, depth+1)
				if err != nil {
					return nil, err
				}
				value[name] = child
			}
			if end, err := d.Token(); err != nil || end != json.Delim('}') {
				return nil, fault(f.InvalidArgument)
			}
			return value, nil
		case '[':
			value := []any{}
			for d.More() {
				child, err := jsonValue(ctx, d, depth+1)
				if err != nil {
					return nil, err
				}
				value = append(value, child)
			}
			if end, err := d.Token(); err != nil || end != json.Delim(']') {
				return nil, fault(f.InvalidArgument)
			}
			return value, nil
		default:
			return nil, fault(f.InvalidArgument)
		}
	}
	return token, nil // json.Number is retained, never converted through float64.
}
func validScalars(ctx context.Context, raw []byte) bool {
	next := 0
	for n := 0; n < len(raw); n++ {
		if n >= next {
			if ctx.Err() != nil {
				return false
			}
			next = n + 4096
		}
		if raw[n] != '\\' {
			continue
		}
		n++
		if n == len(raw) {
			return false
		}
		if raw[n] != 'u' {
			continue
		}
		high, ok := unicodeUnit(raw[n+1:])
		if !ok || high >= 0xdc00 && high <= 0xdfff {
			return false
		}
		n += 4
		if high < 0xd800 || high > 0xdbff {
			continue
		}
		if len(raw)-n < 7 || raw[n+1] != '\\' || raw[n+2] != 'u' {
			return false
		}
		low, ok := unicodeUnit(raw[n+3:])
		if !ok || low < 0xdc00 || low > 0xdfff {
			return false
		}
		n += 6
	}
	return true
}
func unicodeUnit(raw []byte) (uint16, bool) {
	if len(raw) < 4 {
		return 0, false
	}
	var value uint16
	for _, c := range raw[:4] {
		value <<= 4
		switch {
		case c >= '0' && c <= '9':
			value |= uint16(c - '0')
		case c >= 'a' && c <= 'f':
			value |= uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			value |= uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return value, true
}
func (Compiled) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "compiled_tool_schema") }
func (Compiled) LogValue() slog.Value         { return slog.StringValue("compiled_tool_schema") }
func (Compiled) MarshalJSON() ([]byte, error) { return []byte(`"compiled_tool_schema"`), nil }
