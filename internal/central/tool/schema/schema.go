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
	if err = dialect(ctx, doc); err != nil {
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

// This is a dialect/resource policy check, not a schema evaluator. Only actual
// standard subschema positions are visited: literal data under const/default
// may contain "$schema" without declaring another dialect.
func dialect(ctx context.Context, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := value.(bool); ok {
		return nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return fault(f.SchemaUnsupported)
	}
	if raw, exists := m["$schema"]; exists && raw != Draft {
		return fault(f.SchemaUnsupported)
	}
	if vocabulary, ok := m["$vocabulary"].(map[string]any); ok {
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
	for _, key := range []string{"additionalProperties", "unevaluatedProperties", "propertyNames", "items", "contains", "unevaluatedItems", "not", "if", "then", "else", "contentSchema"} {
		if child, exists := m[key]; exists {
			if err := dialect(ctx, child); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"$defs", "properties", "patternProperties", "dependentSchemas"} {
		if children, ok := m[key].(map[string]any); ok {
			for _, child := range children {
				if err := dialect(ctx, child); err != nil {
					return err
				}
			}
		}
	}
	for _, key := range []string{"prefixItems", "allOf", "anyOf", "oneOf"} {
		if children, ok := m[key].([]any); ok {
			for _, child := range children {
				if err := dialect(ctx, child); err != nil {
					return err
				}
			}
		}
	}
	return nil
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
