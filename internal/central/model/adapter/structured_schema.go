package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const (
	maxSchemaBytes      = 64 << 10
	maxSchemaNodes      = 1024
	maxStructuredDepth  = 32
	maxSchemaProperties = 256
	maxSchemaEnum       = 128
	maxStructuredNodes  = 65536
	maxStructuredNumber = 128
)

// The native format and the verifier are made from the same private copy,
// before admission. Neither caller mutation nor a second parse after dispatch
// can replace the schema under which this exchange was accepted.
type structuredWireFormat struct {
	Type       string `json:"type"`
	JSONSchema struct {
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
		Strict bool            `json:"strict"`
	} `json:"json_schema"`
}

type schemaNode struct {
	kind       string
	nullable   bool
	properties map[string]*schemaNode
	items      *schemaNode
	hasEnum    bool
	enumNull   bool
	enum       map[string]bool
}

type structuredSchema struct{ root *schemaNode }
type schemaCompiler struct {
	ctx   context.Context
	nodes int
}

func compileStructured(ctx context.Context, format mc.ResponseFormat) (*structuredSchema, *structuredWireFormat, error) {
	if ctx == nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, contextFailure(err)
	}
	if format.Kind != "json_schema" || len(format.Name) == 0 || len(format.Name) > 64 || len(format.Schema) == 0 || len(format.Schema) > maxSchemaBytes {
		return nil, nil, invalid()
	}
	for _, ch := range format.Name {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return nil, nil, invalid()
		}
	}
	raw := bytes.Clone(format.Schema)
	if jsonShape(raw) != nil {
		return nil, nil, invalid()
	}
	c := schemaCompiler{ctx: ctx}
	root, err := c.node(raw, 1)
	if err != nil {
		return nil, nil, err
	}
	if root.kind != "object" || root.nullable {
		return nil, nil, unsupported()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, contextFailure(err)
	}
	wire := &structuredWireFormat{Type: "json_schema"}
	wire.JSONSchema.Name, wire.JSONSchema.Schema, wire.JSONSchema.Strict = format.Name, raw, true
	return &structuredSchema{root: root}, wire, nil
}

func (c *schemaCompiler) node(raw json.RawMessage, depth int) (*schemaNode, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, contextFailure(err)
	}
	c.nodes++
	if c.nodes > maxSchemaNodes || depth > maxStructuredDepth {
		return nil, unsupported()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, invalid()
	}
	for key := range fields {
		switch key {
		case "type", "properties", "required", "additionalProperties", "items", "enum", "title", "description":
		default:
			return nil, unsupported()
		}
	}
	n := &schemaNode{}
	if err := schemaType(fields["type"], n); err != nil {
		return nil, err
	}
	for _, annotation := range []struct {
		key string
		max int
	}{{"title", 128}, {"description", 4096}} {
		if raw, ok := fields[annotation.key]; ok {
			var text string
			if null(raw) || json.Unmarshal(raw, &text) != nil {
				return nil, invalid()
			}
			if len(text) > annotation.max {
				return nil, unsupported()
			}
		}
	}
	if n.kind != "object" {
		for _, key := range []string{"properties", "required", "additionalProperties"} {
			if _, ok := fields[key]; ok {
				return nil, invalid()
			}
		}
	}
	if _, ok := fields["items"]; ok && n.kind != "array" {
		return nil, invalid()
	}
	if n.kind == "object" {
		if err := c.properties(fields, n, depth); err != nil {
			return nil, err
		}
	}
	if n.kind == "array" {
		items, ok := fields["items"]
		if !ok {
			return nil, unsupported()
		}
		var err error
		n.items, err = c.node(items, depth+1)
		if err != nil {
			return nil, err
		}
	}
	if raw, ok := fields["enum"]; ok {
		if err := schemaEnum(raw, n); err != nil {
			return nil, err
		}
	}
	return n, nil
}

func schemaType(raw json.RawMessage, n *schemaNode) error {
	if null(raw) {
		return invalid()
	}
	if json.Unmarshal(raw, &n.kind) != nil {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
			return invalid()
		}
		types := make([]string, len(items))
		seen := make(map[string]bool, len(items))
		for i, item := range items {
			if null(item) || json.Unmarshal(item, &types[i]) != nil || !schemaTypeName(types[i]) || seen[types[i]] {
				return invalid()
			}
			seen[types[i]] = true
		}
		if len(types) != 2 || types[0] != "null" && types[1] != "null" {
			return unsupported()
		}
		n.nullable = true
		n.kind = types[0]
		if n.kind == "null" {
			n.kind = types[1]
		}
	}
	if !schemaTypeName(n.kind) {
		return invalid()
	}
	return nil
}

func schemaTypeName(kind string) bool {
	switch kind {
	case "object", "array", "string", "number", "integer", "boolean", "null":
		return true
	default:
		return false
	}
}

func (c *schemaCompiler) properties(fields map[string]json.RawMessage, n *schemaNode, depth int) error {
	for _, key := range []string{"properties", "required", "additionalProperties"} {
		if _, ok := fields[key]; !ok {
			return unsupported()
		}
	}
	var additional bool
	if null(fields["additionalProperties"]) || json.Unmarshal(fields["additionalProperties"], &additional) != nil {
		return invalid()
	}
	if additional {
		return unsupported()
	}
	var properties map[string]json.RawMessage
	var required []json.RawMessage
	if json.Unmarshal(fields["properties"], &properties) != nil || properties == nil || json.Unmarshal(fields["required"], &required) != nil || required == nil {
		return invalid()
	}
	if len(properties) > maxSchemaProperties {
		return unsupported()
	}
	seen := make(map[string]bool, len(required))
	for _, raw := range required {
		var name string
		if null(raw) || json.Unmarshal(raw, &name) != nil {
			return invalid()
		}
		if _, exists := properties[name]; !exists || seen[name] {
			return invalid()
		}
		seen[name] = true
	}
	if len(required) != len(properties) {
		return unsupported()
	}
	n.properties = make(map[string]*schemaNode, len(properties))
	for name, raw := range properties {
		if len(name) > 256 {
			return unsupported()
		}
		child, err := c.node(raw, depth+1)
		if err != nil {
			return err
		}
		n.properties[name] = child
	}
	return nil
}

func schemaEnum(raw json.RawMessage, n *schemaNode) error {
	if n.kind != "string" {
		return unsupported()
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return invalid()
	}
	if len(values) > maxSchemaEnum {
		return unsupported()
	}
	n.hasEnum, n.enum = true, make(map[string]bool, len(values))
	for _, raw := range values {
		if null(raw) {
			if !n.nullable || n.enumNull {
				return invalid()
			}
			n.enumNull = true
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil || n.enum[value] {
			return invalid()
		}
		if len(value) > 1024 {
			return unsupported()
		}
		n.enum[value] = true
	}
	return nil
}

func (s *structuredSchema) complete(ctx context.Context, text string, finish mc.FinishReason) error {
	if err := ctx.Err(); err != nil {
		return contextFailure(err)
	}
	switch finish {
	case "content_filter":
		return failure("content_filter", "")
	case "stop":
		return s.validate(ctx, text)
	default:
		return protocolFailure()
	}
}

// Validation walks the original content directly. Strings without an enum are
// never materialized; decoded keys/enums use a bounded scratch area. Numbers
// stop at their 129th byte, before a decoder can scan or copy the whole value.
func (s *structuredSchema) validate(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return contextFailure(err)
	}
	if len(text) > maxTextBytes {
		return limitFailure()
	}
	v := structuredValidator{ctx: ctx, text: text}
	if err := v.value(s.root, 1); err != nil {
		return err
	}
	if err := v.space(); err != nil {
		return err
	}
	if v.pos != len(text) {
		return protocolFailure()
	}
	if err := ctx.Err(); err != nil {
		return contextFailure(err)
	}
	return nil
}

type structuredValidator struct {
	ctx                   context.Context
	text                  string
	pos, nodes, nextCheck int
}

func (v *structuredValidator) advance(n int) error {
	v.pos += n
	if v.pos >= v.nextCheck {
		v.nextCheck = v.pos + 1024
		if err := v.ctx.Err(); err != nil {
			return contextFailure(err)
		}
	}
	return nil
}
func (v *structuredValidator) peek() byte {
	if v.pos == len(v.text) {
		return 0
	}
	return v.text[v.pos]
}
func (v *structuredValidator) space() error {
	for v.pos < len(v.text) {
		switch v.text[v.pos] {
		case ' ', '\t', '\r', '\n':
			if err := v.advance(1); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}
func (v *structuredValidator) literal(want string) error {
	if !strings.HasPrefix(v.text[v.pos:], want) {
		return protocolFailure()
	}
	return v.advance(len(want))
}
func (v *structuredValidator) value(n *schemaNode, depth int) error {
	if err := v.ctx.Err(); err != nil {
		return contextFailure(err)
	}
	v.nodes++
	if depth > maxStructuredDepth || v.nodes > maxStructuredNodes {
		return limitFailure()
	}
	if err := v.space(); err != nil {
		return err
	}
	if v.pos == len(v.text) {
		return protocolFailure()
	}
	switch ch := v.peek(); {
	case ch == 'n':
		if err := v.literal("null"); err != nil {
			return err
		}
		if (n.nullable || n.kind == "null") && (!n.hasEnum || n.enumNull) {
			return nil
		}
	case ch == 't' || ch == 'f':
		want := "true"
		if ch == 'f' {
			want = "false"
		}
		if err := v.literal(want); err != nil {
			return err
		}
		if n.kind == "boolean" {
			return nil
		}
	case ch == '"':
		if n.kind != "string" {
			return protocolFailure()
		}
		bound := 0
		if n.hasEnum {
			bound = 1024
		}
		text, err := v.quoted(bound)
		if err != nil {
			return err
		}
		if !n.hasEnum || n.enum[text] {
			return nil
		}
	case ch == '-' || ch >= '0' && ch <= '9':
		number, err := v.number()
		if err != nil {
			return err
		}
		if n.kind == "number" || n.kind == "integer" && structuredInteger(number) {
			return nil
		}
	case ch == '{' && n.kind == "object":
		if err := v.advance(1); err != nil {
			return err
		}
		if err := v.space(); err != nil {
			return err
		}
		seen := make(map[string]bool, len(n.properties))
		if v.peek() != '}' {
			for {
				name, err := v.quoted(256)
				if err != nil {
					return err
				}
				if seen[name] || n.properties[name] == nil {
					return protocolFailure()
				}
				seen[name] = true
				if err := v.space(); err != nil {
					return err
				}
				if v.peek() != ':' {
					return protocolFailure()
				}
				if err := v.advance(1); err != nil {
					return err
				}
				if err := v.value(n.properties[name], depth+1); err != nil {
					return err
				}
				if err := v.space(); err != nil {
					return err
				}
				if v.peek() != ',' {
					break
				}
				if err := v.advance(1); err != nil {
					return err
				}
				if err := v.space(); err != nil {
					return err
				}
			}
		}
		if v.peek() != '}' || len(seen) != len(n.properties) {
			return protocolFailure()
		}
		return v.advance(1)
	case ch == '[' && n.kind == "array":
		if err := v.advance(1); err != nil {
			return err
		}
		if err := v.space(); err != nil {
			return err
		}
		if v.peek() != ']' {
			for {
				if err := v.value(n.items, depth+1); err != nil {
					return err
				}
				if err := v.space(); err != nil {
					return err
				}
				if v.peek() != ',' {
					break
				}
				if err := v.advance(1); err != nil {
					return err
				}
			}
		}
		if v.peek() != ']' {
			return protocolFailure()
		}
		return v.advance(1)
	}
	return protocolFailure()
}

// bound==0 validates only; otherwise it decodes at most 256 key / 1024 enum
// bytes. A long string cannot allocate a decoder buffer or decoded whole value.
func (v *structuredValidator) quoted(bound int) (string, error) {
	if v.peek() != '"' {
		return "", protocolFailure()
	}
	if err := v.advance(1); err != nil {
		return "", err
	}
	var scratch [1024]byte
	decoded := scratch[:0]
	for v.pos < len(v.text) {
		ch := v.peek()
		if ch == '"' {
			if err := v.advance(1); err != nil {
				return "", err
			}
			return string(decoded), nil
		}
		var r rune
		if ch == '\\' {
			if err := v.advance(1); err != nil {
				return "", err
			}
			if v.pos == len(v.text) {
				return "", protocolFailure()
			}
			escape := v.peek()
			if err := v.advance(1); err != nil {
				return "", err
			}
			switch escape {
			case '"', '\\', '/':
				r = rune(escape)
			case 'b':
				r = '\b'
			case 'f':
				r = '\f'
			case 'n':
				r = '\n'
			case 'r':
				r = '\r'
			case 't':
				r = '\t'
			case 'u':
				u, err := v.hex4()
				if err != nil {
					return "", err
				}
				if u >= 0xdc00 && u <= 0xdfff {
					return "", protocolFailure()
				}
				if u >= 0xd800 && u <= 0xdbff {
					if !strings.HasPrefix(v.text[v.pos:], `\u`) {
						return "", protocolFailure()
					}
					if err := v.advance(2); err != nil {
						return "", err
					}
					low, err := v.hex4()
					if err != nil {
						return "", err
					}
					if low < 0xdc00 || low > 0xdfff {
						return "", protocolFailure()
					}
					u = 0x10000 + (u-0xd800)*0x400 + low - 0xdc00
				}
				r = u
			default:
				return "", protocolFailure()
			}
		} else {
			if ch < 0x20 {
				return "", protocolFailure()
			}
			var size int
			r, size = utf8.DecodeRuneInString(v.text[v.pos:])
			if r == utf8.RuneError && size == 1 {
				return "", protocolFailure()
			}
			if err := v.advance(size); err != nil {
				return "", err
			}
		}
		if bound > 0 {
			if len(decoded)+utf8.RuneLen(r) > bound {
				return "", protocolFailure()
			}
			decoded = utf8.AppendRune(decoded, r)
		}
	}
	return "", protocolFailure()
}
func (v *structuredValidator) hex4() (rune, error) {
	var result rune
	for i := 0; i < 4; i++ {
		if v.pos == len(v.text) {
			return 0, protocolFailure()
		}
		ch := v.peek()
		var digit byte
		switch {
		case ch >= '0' && ch <= '9':
			digit = ch - '0'
		case ch >= 'a' && ch <= 'f':
			digit = ch - 'a' + 10
		case ch >= 'A' && ch <= 'F':
			digit = ch - 'A' + 10
		default:
			return 0, protocolFailure()
		}
		result = result*16 + rune(digit)
		if err := v.advance(1); err != nil {
			return 0, err
		}
	}
	return result, nil
}
func (v *structuredValidator) number() (string, error) {
	start := v.pos
	step := func() error {
		if err := v.advance(1); err != nil {
			return err
		}
		if v.pos-start > maxStructuredNumber {
			return limitFailure()
		}
		return nil
	}
	digit := func() bool { return v.pos < len(v.text) && v.peek() >= '0' && v.peek() <= '9' }
	if v.peek() == '-' {
		if err := step(); err != nil {
			return "", err
		}
	}
	if !digit() {
		return "", protocolFailure()
	}
	if v.peek() == '0' {
		if err := step(); err != nil {
			return "", err
		}
		if digit() {
			return "", protocolFailure()
		}
	} else {
		for digit() {
			if err := step(); err != nil {
				return "", err
			}
		}
	}
	if v.peek() == '.' {
		if err := step(); err != nil {
			return "", err
		}
		if !digit() {
			return "", protocolFailure()
		}
		for digit() {
			if err := step(); err != nil {
				return "", err
			}
		}
	}
	if v.peek() == 'e' || v.peek() == 'E' {
		if err := step(); err != nil {
			return "", err
		}
		if v.peek() == '+' || v.peek() == '-' {
			if err := step(); err != nil {
				return "", err
			}
		}
		if !digit() {
			return "", protocolFailure()
		}
		for digit() {
			if err := step(); err != nil {
				return "", err
			}
		}
	}
	return v.text[start:v.pos], nil
}

// A <=128-byte JSON number is integral iff its decimal exponent moves every
// nonzero fractional digit left of the point. Saturation at 256 suffices for
// this bounded comparison; even a very long exponent is never expanded.
func structuredInteger(number string) bool {
	if strings.HasPrefix(number, "-") {
		number = number[1:]
	}
	mantissa, exponent := number, ""
	if i := strings.IndexAny(number, "eE"); i >= 0 {
		mantissa, exponent = number[:i], number[i+1:]
	}
	fraction, trailing, nonzero := 0, 0, false
	if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
		fraction = len(mantissa) - dot - 1
	}
	for _, ch := range mantissa {
		if ch == '.' {
			continue
		}
		if ch == '0' {
			trailing++
		} else {
			nonzero, trailing = true, 0
		}
	}
	if !nonzero {
		return true
	}
	negative := strings.HasPrefix(exponent, "-")
	if len(exponent) > 0 && (exponent[0] == '+' || exponent[0] == '-') {
		exponent = exponent[1:]
	}
	exp := 0
	for _, digit := range exponent {
		exp = min(256, exp*10+int(digit-'0'))
	}
	if negative {
		exp = -exp
	}
	return exp >= fraction-trailing
}
