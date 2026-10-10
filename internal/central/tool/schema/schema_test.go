package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/dlclark/regexp2"
)

func compileTest(t *testing.T, raw string) Compiled {
	t.Helper()
	compiled, err := Compile(context.Background(), []byte(raw))
	if err != nil {
		t.Fatal("standard schema did not compile", err)
	}
	return compiled
}
func requireCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var value *f.Fault
	if !errors.As(err, &value) || value.Code != code {
		t.Fatalf("expected safe %s, got %v", code, err)
	}
}
func TestToolSchemaActualInstallDefinitions(t *testing.T) {
	t.Cleanup(regexp2.StopTimeoutClock)
	definition := builtin.SkillInstallDefinition()
	input, err := Compile(context.Background(), definition.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	output, err := Compile(context.Background(), definition.OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	valid := []byte(`{"source":{"kind":"text_files","files":[{"path":"SKILL.md","utf8_text":"原文。"}]},"mode":"create"}`)
	before := bytes.Clone(valid)
	if err = input.Validate(context.Background(), valid); err != nil || !bytes.Equal(before, valid) {
		t.Fatal("original install input changed or rejected", err)
	}
	for _, raw := range []string{`{"source":{"kind":"remote","files":[]},"mode":"create"}`, `{"source":{"kind":"text_files","files":[]},"mode":"create"}`, `{"source":{"kind":"text_files","files":[{"path":"x","utf8_text":"y","extra":true}]},"mode":"create"}`} {
		requireCode(t, input.Validate(context.Background(), []byte(raw)), f.InvalidArgument)
	}
	if err = output.Validate(context.Background(), []byte(`{"skill_id":"01997e80-0000-7000-8000-000000000001","revision":"1","version":"1"}`)); err != nil {
		t.Fatal(err)
	}
	requireCode(t, output.Validate(context.Background(), []byte(`{"skill_id":"01997e80-0000-7000-8000-000000000001","revision":"1","version":"1","object_id":"not-public"}`)), f.InvalidArgument)
}
func TestToolSchemaDraft202012AndExactNumbers(t *testing.T) {
	cases := []struct{ name, schema, valid, invalid string }{
		{"exact-integer", `{"type":"integer","const":9007199254740993}`, `9007199254740993`, `9007199254740992`},
		{"exact-decimal", `{"type":"number","multipleOf":0.01}`, `0.03`, `0.031`},
		{"unevaluated", `{"allOf":[{"properties":{"name":{"type":"string"}},"required":["name"]}],"unevaluatedProperties":false}`, `{"name":"x"}`, `{"name":"x","extra":0}`},
		{"prefix-items", `{"type":"array","prefixItems":[{"type":"integer"},{"type":"string"}],"items":false}`, `[1,"x"]`, `[1,"x",2]`},
		{"dependent", `{"type":"object","dependentRequired":{"credit":["billing"]}}`, `{"credit":1,"billing":"x"}`, `{"credit":1}`},
		{"dynamic-ref", `{"$defs":{"node":{"$dynamicAnchor":"node","type":"object","properties":{"value":{"type":"integer"},"children":{"type":"array","items":{"$dynamicRef":"#node"}}},"additionalProperties":false}},"$ref":"#/$defs/node"}`, `{"children":[{"value":1}]}`, `{"children":[{"value":"wrong"}]}`},
		{"literal-schema-key", `{"const":{"$schema":"literal-data"}}`, `{"$schema":"literal-data"}`, `{}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			c := compileTest(t, test.schema)
			if err := c.Validate(context.Background(), []byte(test.valid)); err != nil {
				t.Fatal(err)
			}
			requireCode(t, c.Validate(context.Background(), []byte(test.invalid)), f.InvalidArgument)
		})
	}
	annotation := compileTest(t, `{"type":"string","format":"email"}`)
	if err := annotation.Validate(context.Background(), []byte(`"not-an-email"`)); err != nil {
		t.Fatal("2020-12 format annotation changed into assertion", err)
	}
}
func TestToolSchemaStrictJSONAndOfflineResources(t *testing.T) {
	for _, raw := range []string{
		`{"type":"string","type":"integer"}`, `true false`, `{"const":"\ud800"}`,
		`{"$schema":"http://json-schema.org/draft-07/schema"}`,
		`{"$defs":{"old":{"$schema":"https://json-schema.org/draft/2019-09/schema","type":"integer"}},"$ref":"#/$defs/old"}`,
		`{"$vocabulary":{"urn:unsupported-required":true}}`,
		`{"hidden":{"$id":"urn:hidden","$schema":"http://json-schema.org/draft-07/schema","type":"integer"},"$ref":"#/hidden"}`,
		`{"hidden":{"$id":"urn:hidden","$vocabulary":{"urn:unsupported-required":true},"type":"integer"},"$ref":"#/hidden"}`,
		`{"const":{"$id":"urn:literal-target","$schema":"http://json-schema.org/draft-07/schema"},"$ref":"#/const"}`,
		`{"$ref":"http://json-schema.org/draft-07/schema"}`,
		`{"$ref":"https://json-schema.org/draft/2020-12/schema"}`,
		`{"$defs":{"node":{"$dynamicAnchor":"node","$ref":"#/hidden"}},"hidden":{"$id":"urn:hidden","$schema":"http://json-schema.org/draft-07/schema"}}`,
		`{"definitions":{"node":{"$dynamicAnchor":"node","$ref":"#/hidden"}},"hidden":{"$vocabulary":{"urn:unsupported-required":true}}}`,
		`{"$ref":"file:///must-not-open"}`, `{"$ref":"https://must-not-resolve.invalid/schema"}`,
	} {
		compiled, err := Compile(context.Background(), []byte(raw))
		requireCode(t, err, f.SchemaUnsupported)
		if compiled.data != nil {
			t.Fatal("unsupported schema returned a compiled value")
		}
	}
	for _, raw := range []string{
		`{"hidden":{"$id":"urn:hidden","$schema":"https://json-schema.org/draft/2020-12/schema","type":"integer"},"$ref":"#/hidden"}`,
		`{"hidden/a~b":{"type":"integer"},"$ref":"#/hidden~1a~0b"}`,
		`{"default":{"$schema":"http://json-schema.org/draft-07/schema","$vocabulary":{"urn:unsupported-required":true}},"type":"integer"}`,
	} {
		compiled := compileTest(t, raw)
		if err := compiled.Validate(context.Background(), []byte(`2`)); err != nil {
			t.Fatal(err)
		}
		requireCode(t, compiled.Validate(context.Background(), []byte(`"wrong"`)), f.InvalidArgument)
	}
	c := compileTest(t, `true`)
	for _, raw := range [][]byte{[]byte(`{"x":1,"\u0078":2}`), []byte(`1 2`), []byte(`"\udc00"`), {'"', 0xff, '"'}} {
		requireCode(t, c.Validate(context.Background(), raw), f.InvalidArgument)
	}
	for _, raw := range []string{`"\ud83d\ude00"`, `"\ufffd"`, `"\\ud800"`} {
		if err := c.Validate(context.Background(), []byte(raw)); err != nil {
			t.Fatal("valid Unicode changed", err)
		}
	}
	_, err := Compile(context.Background(), bytes.Repeat([]byte{' '}, MaxSchemaBytes+1))
	requireCode(t, err, f.PayloadTooLarge)
	requireCode(t, c.Validate(context.Background(), bytes.Repeat([]byte{' '}, MaxInstanceBytes+1)), f.PayloadTooLarge)
	local := compileTest(t, `{"$defs":{"inner":{"$id":"urn:local:inner","type":"integer"}},"$ref":"urn:local:inner"}`)
	if err := local.Validate(context.Background(), []byte(`2`)); err != nil {
		t.Fatal("embedded resource incorrectly required external loader", err)
	}
}
func TestToolSchemaECMAScriptBudgetAndCancellation(t *testing.T) {
	t.Cleanup(regexp2.StopTimeoutClock)
	c := compileTest(t, `{"type":"string","pattern":"^(?=a)(a)\\1$"}`)
	if err := c.Validate(context.Background(), []byte(`"aa"`)); err != nil {
		t.Fatal("ECMA lookahead/backreference rejected", err)
	}
	requireCode(t, c.Validate(context.Background(), []byte(`"ab"`)), f.InvalidArgument)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compile(ctx, []byte(`true`)); !errors.Is(err, context.Canceled) {
		t.Fatal("compile cancellation lost")
	}
	if err := c.Validate(ctx, []byte(`"aa"`)); !errors.Is(err, context.Canceled) {
		t.Fatal("validation cancellation lost")
	}
	// Backtracking is genuinely executed by the selected engine. Exhaustion is
	// an inability to decide, never a valid/invalid answer or a detached worker.
	slow := compileTest(t, `{"type":"string","pattern":"^(a+)+$"}`)
	instance, _ := json.Marshal(strings.Repeat("a", 128) + "!")
	requireCode(t, slow.Validate(context.Background(), instance), f.DependencyUnavailable)
	deadline, stop := context.WithTimeout(context.Background(), time.Millisecond)
	defer stop()
	if err := slow.Validate(deadline, instance); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("original deadline error lost", err)
	}
	if err := slow.Validate(context.Background(), []byte(`"aaa"`)); err != nil {
		t.Fatal("old regex failure leaked into subsequent call", err)
	}
}
func TestToolSchemaSafeProjectionAndConcurrentReuse(t *testing.T) {
	t.Cleanup(regexp2.StopTimeoutClock)
	const canary = "schema-input-canary-do-not-project"
	c := compileTest(t, `{"type":"string","pattern":"^ok$","description":"`+canary+`"}`)
	err := c.Validate(context.Background(), []byte(`"`+canary+`"`))
	requireCode(t, err, f.InvalidArgument)
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("fixture", "nested", map[string]any{"slice": []any{c, err}, "struct": struct{ Value Compiled }{c}})
	encoded, _ := json.Marshal(struct{ Value Compiled }{c})
	for _, text := range []string{fmt.Sprintf("%+v %#v", c, c), string(encoded), logged.String(), fmt.Sprintf("%+v %#v", err, err)} {
		if strings.Contains(text, canary) || strings.Contains(text, "ValidationError") {
			t.Fatal("schema or instance escaped through default projection")
		}
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("sensitive validation cause retained")
	}
	var workers sync.WaitGroup
	failed := make(chan error, 8)
	for n := 0; n < 8; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := c.Validate(context.Background(), []byte(`"ok"`)); err != nil {
				failed <- err
			}
		}()
	}
	workers.Wait()
	close(failed)
	for err := range failed {
		t.Fatal("shared compiled validation failed", err)
	}
	var zero Compiled
	requireCode(t, zero.Validate(context.Background(), []byte(`null`)), f.DependencyUnbound)
}
