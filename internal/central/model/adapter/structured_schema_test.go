package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func schemaObject(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for name := range properties {
		required = append(required, name)
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func schemaJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func compileSchemaTest(t *testing.T, raw string) *structuredSchema {
	t.Helper()
	s, _, err := compileStructured(context.Background(), mc.ResponseFormat{Kind: "json_schema", Name: "memory_result", Schema: json.RawMessage(raw)})
	if err != nil {
		t.Fatal("schema fixture", err)
	}
	return s
}
func singleSchema(t *testing.T, node any) *structuredSchema {
	t.Helper()
	return compileSchemaTest(t, string(schemaJSON(t, schemaObject(map[string]any{"v": node}))))
}

func TestStructuredSchemaSubsetAndFailures(t *testing.T) {
	raw := `{"type":"object","title":"Memory","description":"fixture","properties":{"facts":{"type":"array","items":{"type":"object","properties":{"text":{"type":"string"},"kind":{"type":["null","string"],"enum":["fact",null]},"confidence":{"type":"number"},"ordinal":{"type":"integer"},"active":{"type":"boolean"},"absent":{"type":"null"}},"required":["text","kind","confidence","ordinal","active","absent"],"additionalProperties":false}},"maybe":{"type":["object","null"],"properties":{},"required":[],"additionalProperties":false}},"required":["maybe","facts"],"additionalProperties":false}`
	s := compileSchemaTest(t, raw)
	good := ` {"facts":[{"text":"世界\n\ud83d\ude00","kind":null,"confidence":0.25,"ordinal":9007199254740993,"active":true,"absent":null}],"maybe":{}} `
	if err := s.complete(context.Background(), good, "stop"); err != nil {
		t.Fatal("nested strict response", err)
	}
	for _, tc := range []struct{ name, body string }{
		{"missing", `{"facts":[]}`}, {"extra", `{"facts":[],"maybe":null,"x":0}`},
		{"nested_enum", strings.Replace(good, `"kind":null`, `"kind":"other"`, 1)},
		{"nested_type", strings.Replace(good, `"active":true`, `"active":0`, 1)},
		{"fractional_integer", strings.Replace(good, `9007199254740993`, `1.2`, 1)},
		{"duplicate", `{"facts":[],"maybe":null,"maybe":{}}`},
		{"escaped_duplicate", `{"facts":[],"maybe":null,"\u006daybe":{}}`},
		{"trailing", good + `{}`}, {"root_null", `null`}, {"root_array", `[]`},
		{"surrogate", strings.Replace(good, `世界\n\ud83d\ude00`, `\ud800`, 1)},
		{"bad_utf8", strings.Replace(good, "世界", string([]byte{0xff}), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireModel(t, s.validate(context.Background(), tc.body), "provider_error", "wire_protocol_invalid")
		})
	}
	for _, tc := range []struct {
		name, schema string
		unsupported  bool
	}{
		{"missing_required", `{"type":"object","properties":{},"additionalProperties":false}`, true},
		{"optional_property", `{"type":"object","properties":{"x":{"type":"string"}},"required":[],"additionalProperties":false}`, true},
		{"loose_object", `{"type":"object","properties":{},"required":[],"additionalProperties":true}`, true},
		{"unknown_required", `{"type":"object","properties":{},"required":["x"],"additionalProperties":false}`, false},
		{"duplicate_required", `{"type":"object","properties":{"x":{"type":"string"}},"required":["x","x"],"additionalProperties":false}`, false},
		{"required_null", `{"type":"object","properties":{},"required":null,"additionalProperties":false}`, false},
		{"required_null_item", `{"type":"object","properties":{"":{"type":"string"}},"required":[null],"additionalProperties":false}`, false},
		{"duplicate_keyword", `{"type":"object","type":"object"}`, false},
		{"bad_annotation", `{"type":"object","properties":{},"required":[],"additionalProperties":false,"title":null}`, false},
		{"root_nullable", `{"type":["object","null"],"properties":{},"required":[],"additionalProperties":false}`, true},
		{"root_string", `{"type":"string"}`, true},
		{"boolean_schema", `true`, false},
		{"nonnullable_union", `{"type":["string","number"]}`, true},
		{"malformed_type", `{"type":42}`, false},
		{"malformed_type_item", `{"type":["string",null]}`, false},
		{"duplicate_type_item", `{"type":["string","string"]}`, false},
		{"misplaced_items", `{"type":"object","properties":{},"required":[],"additionalProperties":false,"items":{"type":"string"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := compileStructured(context.Background(), mc.ResponseFormat{Kind: "json_schema", Name: "x", Schema: json.RawMessage(tc.schema)})
			if tc.unsupported {
				requireModel(t, err, "unsupported_feature", "wire_unsupported_feature")
			} else {
				requireFault(t, err, f.InvalidArgument)
			}
		})
	}
	for _, key := range []string{"$ref", "$defs", "definitions", "$schema", "$id", "anyOf", "oneOf", "allOf", "not", "if", "then", "else", "default", "pattern", "format", "minimum", "maximum", "multipleOf", "minLength", "maxLength", "minItems", "maxItems", "uniqueItems", "prefixItems", "patternProperties", "unknown"} {
		t.Run("reject_keyword_"+key, func(t *testing.T) {
			node := map[string]any{"type": "string", key: "synthetic-schema-canary"}
			_, _, err := compileStructured(context.Background(), mc.ResponseFormat{Kind: "json_schema", Name: "x", Schema: schemaJSON(t, schemaObject(map[string]any{"v": node}))})
			requireModel(t, err, "unsupported_feature", "wire_unsupported_feature")
		})
	}
	for _, node := range []any{
		map[string]any{"type": "integer", "enum": []int{1}},
		map[string]any{"type": "array"},
	} {
		_, _, err := compileStructured(context.Background(), mc.ResponseFormat{Kind: "json_schema", Name: "x", Schema: schemaJSON(t, schemaObject(map[string]any{"v": node}))})
		requireModel(t, err, "unsupported_feature", "wire_unsupported_feature")
	}
	for _, node := range []any{
		map[string]any{"type": "string", "enum": []string{}},
		map[string]any{"type": "string", "enum": []any{nil}},
		map[string]any{"type": []string{"string", "null"}, "enum": []any{nil, nil}},
		map[string]any{"type": "string", "enum": []string{"x", "x"}},
		map[string]any{"type": "string", "enum": []any{true}},
		map[string]any{"type": "string", "properties": map[string]any{}},
	} {
		_, _, err := compileStructured(context.Background(), mc.ResponseFormat{Kind: "json_schema", Name: "x", Schema: schemaJSON(t, schemaObject(map[string]any{"v": node}))})
		requireFault(t, err, f.InvalidArgument)
	}
	withoutNull := singleSchema(t, map[string]any{"type": []string{"string", "null"}, "enum": []string{"x"}})
	requireModel(t, withoutNull.validate(context.Background(), `{"v":null}`), "provider_error", "wire_protocol_invalid")
	if err := withoutNull.validate(context.Background(), `{"v":"x"}`); err != nil {
		t.Fatal(err)
	}
}

func TestStructuredSchemaResourceBounds(t *testing.T) {
	compile := func(raw []byte) error {
		_, _, e := compileStructured(context.Background(), mc.ResponseFormat{Kind: "json_schema", Name: "x", Schema: raw})
		return e
	}
	root := schemaJSON(t, schemaObject(map[string]any{}))
	if err := compile(append(append([]byte{}, root...), []byte(strings.Repeat(" ", maxSchemaBytes-len(root)))...)); err != nil {
		t.Fatal(err)
	}
	requireFault(t, compile(append(append([]byte{}, root...), []byte(strings.Repeat(" ", maxSchemaBytes-len(root)+1))...)), f.InvalidArgument)
	properties := func(n int) map[string]any {
		v := map[string]any{}
		for i := 0; i < n; i++ {
			v[fmt.Sprintf("p%d", i)] = map[string]any{"type": "string"}
		}
		return v
	}
	if err := compile(schemaJSON(t, schemaObject(properties(256)))); err != nil {
		t.Fatal(err)
	}
	requireModel(t, compile(schemaJSON(t, schemaObject(properties(257)))), "unsupported_feature", "wire_unsupported_feature")
	for _, last := range []int{251, 252} {
		p := map[string]any{}
		for i, count := range []int{256, 256, 256, last} {
			p[fmt.Sprint(i)] = schemaObject(properties(count))
		}
		err := compile(schemaJSON(t, schemaObject(p)))
		if last == 251 {
			if err != nil {
				t.Fatal("1024 schema nodes", err)
			}
		} else {
			requireModel(t, err, "unsupported_feature", "wire_unsupported_feature")
		}
	}
	for _, tc := range []struct {
		name      string
		good, bad any
	}{
		{"property_name", schemaObject(map[string]any{strings.Repeat("p", 256): map[string]any{"type": "string"}}), schemaObject(map[string]any{strings.Repeat("p", 257): map[string]any{"type": "string"}})},
		{"enum_string", schemaObject(map[string]any{"v": map[string]any{"type": "string", "enum": []string{strings.Repeat("x", 1024)}}}), schemaObject(map[string]any{"v": map[string]any{"type": "string", "enum": []string{strings.Repeat("x", 1025)}}})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := compile(schemaJSON(t, tc.good)); err != nil {
				t.Fatal(err)
			}
			requireModel(t, compile(schemaJSON(t, tc.bad)), "unsupported_feature", "wire_unsupported_feature")
		})
	}
	for _, annotation := range []struct {
		key string
		max int
	}{{"title", 128}, {"description", 4096}} {
		v := schemaObject(map[string]any{})
		v[annotation.key] = strings.Repeat("a", annotation.max)
		if err := compile(schemaJSON(t, v)); err != nil {
			t.Fatal(err)
		}
		v[annotation.key] = strings.Repeat("a", annotation.max+1)
		requireModel(t, compile(schemaJSON(t, v)), "unsupported_feature", "wire_unsupported_feature")
	}
	for _, count := range []int{128, 129} {
		values := make([]string, count)
		for i := range values {
			values[i] = fmt.Sprint(i)
		}
		err := compile(schemaJSON(t, schemaObject(map[string]any{"v": map[string]any{"type": "string", "enum": values}})))
		if count == 128 {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			requireModel(t, err, "unsupported_feature", "wire_unsupported_feature")
		}
	}
	for _, arrays := range []int{29, 30} {
		var node any = map[string]any{"type": "string"}
		for i := 0; i < arrays; i++ {
			node = map[string]any{"type": "array", "items": node}
		}
		err := compile(schemaJSON(t, schemaObject(map[string]any{"v": node})))
		if arrays == 29 {
			if err != nil {
				t.Fatal("schema JSON depth 32", err)
			}
		} else {
			requireFault(t, err, f.InvalidArgument)
		}
	}
	array := singleSchema(t, map[string]any{"type": "array", "items": map[string]any{"type": "integer"}})
	for _, count := range []int{maxStructuredNodes - 2, maxStructuredNodes - 1} {
		body := `{"v":[` + strings.Repeat("0,", count-1) + `0]}`
		err := array.validate(context.Background(), body)
		if count == maxStructuredNodes-2 {
			if err != nil {
				t.Fatal("65536 output nodes", err)
			}
		} else {
			requireModel(t, err, "provider_error", "wire_limit_exceeded")
		}
	}
	text := singleSchema(t, map[string]any{"type": "string"})
	for _, extra := range []int{0, 1} {
		body := `{"v":"` + strings.Repeat("x", maxTextBytes-len(`{"v":""}`)+extra) + `"}`
		err := text.validate(context.Background(), body)
		if extra == 0 {
			if err != nil {
				t.Fatal("exact content bound", err)
			}
		} else {
			requireModel(t, err, "provider_error", "wire_limit_exceeded")
		}
	}
	// Output depth is independently bounded, even for a future internal schema
	// program not reachable through this revision's stricter schema JSON shape.
	for _, depth := range []int{32, 33} {
		n := &schemaNode{kind: "string"}
		body := `"x"`
		for i := 1; i < depth; i++ {
			n = &schemaNode{kind: "array", items: n}
			body = "[" + body + "]"
		}
		err := (&structuredSchema{root: n}).validate(context.Background(), body)
		if depth == 32 {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			requireModel(t, err, "provider_error", "wire_limit_exceeded")
		}
	}
}

func TestStructuredExactNumericValues(t *testing.T) {
	integer := singleSchema(t, map[string]any{"type": "integer"})
	number := singleSchema(t, map[string]any{"type": "number"})
	for _, value := range []string{"0", "-0", "1.0", "1e2", "100e-2", "1.20E+1", "9007199254740993", strings.Repeat("9", 128), "1e" + strings.Repeat("9", 126), "0e-" + strings.Repeat("9", 125)} {
		if err := integer.validate(context.Background(), `{"v":`+value+`}`); err != nil {
			t.Fatal("exact integer", len(value), err)
		}
	}
	for _, value := range []string{"1.2", "1e-2", "1.20e0", "1e-" + strings.Repeat("9", 125)} {
		requireModel(t, integer.validate(context.Background(), `{"v":`+value+`}`), "provider_error", "wire_protocol_invalid")
		if err := number.validate(context.Background(), `{"v":`+value+`}`); err != nil {
			t.Fatal("decimal number", err)
		}
	}
	requireModel(t, integer.validate(context.Background(), `{"v":`+strings.Repeat("9", 129)+`}`), "provider_error", "wire_limit_exceeded")
	for _, value := range []string{"NaN", "Infinity", "01", "+1", "1.", "1e"} {
		requireModel(t, number.validate(context.Background(), `{"v":`+value+`}`), "provider_error", "wire_protocol_invalid")
	}
}

func TestStructuredValidationAllocationBounds(t *testing.T) {
	for _, kind := range []string{"string", "number"} {
		t.Run(kind, func(t *testing.T) {
			s := singleSchema(t, map[string]any{"type": kind})
			body := `{"v":"` + strings.Repeat("x", 4<<20) + `"}`
			if kind == "number" {
				body = `{"v":` + strings.Repeat("9", 4<<20) + `}`
			}
			// Input/schema allocation is excluded. This checks the contract's
			// bounded verifier workspace, not exact allocator implementation counts.
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			err := s.validate(context.Background(), body)
			runtime.ReadMemStats(&after)
			if kind == "string" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireModel(t, err, "provider_error", "wire_limit_exceeded")
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("verifier input_bytes=%d allocated_bytes=%d workspace_bound=%d", len(body), allocated, 1<<20)
			if allocated > 1<<20 {
				t.Fatal("verifier copied/expanded a whole long value")
			}
		})
	}
}

func TestStructuredStringAndJSONLexing(t *testing.T) {
	s := singleSchema(t, map[string]any{"type": "string"})
	for _, value := range []string{"", "normal", "\"\\/\b\f\n\r\t\x00", "世界😀�"} {
		body := schemaJSON(t, map[string]any{"v": value})
		if err := s.validate(context.Background(), string(body)); err != nil {
			t.Fatal("valid string encoding", err)
		}
	}
	for _, body := range []string{`{"v":"\x"}`, `{"v":"\uZZZZ"}`, `{"v":"\ud800\ud800"}`, `{"v":"\udc00"}`, `{"v":"\ud800"}`, "{\"v\":\"raw\nline\"}", `{"v":"x",}`, `{"v" "x"}`, `{"v":"x" "v":"x"}`, `{"v":"x"}null`} {
		requireModel(t, s.validate(context.Background(), body), "provider_error", "wire_protocol_invalid")
	}
	if err := s.validate(context.Background(), " \r\n{\t\"\\u0076\" : \"\\ud83d\\ude00\" }\t"); err != nil {
		t.Fatal("escaped key/surrogate pair", err)
	}
	enum := singleSchema(t, map[string]any{"type": "string", "enum": []string{"\t世界😀"}})
	if err := enum.validate(context.Background(), `{"v":"\t\u4e16界\ud83d\ude00"}`); err != nil {
		t.Fatal("decoded enum", err)
	}
	requireModel(t, enum.validate(context.Background(), `{"v":"`+strings.Repeat("x", 1025)+`"}`), "provider_error", "wire_protocol_invalid")
	array := singleSchema(t, map[string]any{"type": "array", "items": map[string]any{"type": "integer"}})
	for _, body := range []string{`{"v":[1,]}`, `{"v":[,1]}`, `{"v":[1 2]}`, `{"v":[1}`, `{"v":[1}}`, `{"v":[1.0,1e2,100e-2]}`} {
		err := array.validate(context.Background(), body)
		if body == `{"v":[1.0,1e2,100e-2]}` {
			if err != nil {
				t.Fatal(err)
			}
		} else {
			requireModel(t, err, "provider_error", "wire_protocol_invalid")
		}
	}
}
