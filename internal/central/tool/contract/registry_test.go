package contract

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestRegistryDefinitionPreservesSchemaAndCanonicalIdentity(t *testing.T) {
	first := Definition{StableKey: "builtin:fixture", Name: "Fixture", Description: "formal definition", InputSchema: []byte(`{"required":["n"],"type":"object","properties":{"n":{"type":"number","minimum":0.5}},"additionalProperties":false}`)}
	second := first
	second.InputSchema = []byte(`{"additionalProperties":false,"properties":{"n":{"minimum":0.5,"type":"number"}},"type":"object","required":["n"]}`)
	a, err := CanonicalDefinition(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalDefinition(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) || !bytes.Contains(a, []byte(`"required":["n"]`)) || !bytes.Contains(a, []byte(`"minimum":0.5`)) || !bytes.Contains(a, []byte(`"additionalProperties":false`)) {
		t.Fatal("schema was weakened or object order changed identity")
	}
	second.Description = "changed definition"
	b, err = CanonicalDefinition(context.Background(), second)
	if err != nil || bytes.Equal(a, b) {
		t.Fatal("definition edit lost")
	}
	second = first
	second.InputSchema = []byte(`false`)
	if _, err = CanonicalDefinition(context.Background(), second); err != nil {
		t.Fatalf("boolean schema: %v", err)
	}
	second.InputSchema = []byte(`{"const":[1e0,1e200000,"\u0000","\ud83d\ude00","\\ud800"]}`)
	b, err = CanonicalDefinition(context.Background(), second)
	if err != nil || !bytes.Contains(b, []byte(`1e0`)) || !bytes.Contains(b, []byte(`1e200000`)) || !bytes.Contains(b, []byte(`\u0000`)) || !bytes.Contains(b, []byte("😀")) || !bytes.Contains(b, []byte(`\\ud800`)) {
		t.Fatal("valid JSON value or paired Unicode changed")
	}
}
func TestRegistryDefinitionRejectsMalformedAndBoundedInput(t *testing.T) {
	base := Definition{StableKey: "builtin:fixture", Name: "Fixture", InputSchema: []byte(`{}`)}
	for _, raw := range []string{`{"type":"object","type":"string"}`, `{"properties":{"x":true,"x":false}}`, `{} {}`, `[]`, `null`, `{"x":NaN}`, `{"const":"\ud800"}`, `{"\udc00":true}`, `{"const":"\ud800\u0041"}`, `{"const":"\ud800\\udc00"}`} {
		v := base
		v.InputSchema = []byte(raw)
		if got, err := CanonicalDefinition(context.Background(), v); err == nil || got != nil {
			t.Fatalf("accepted invalid schema %q", raw)
		}
	}
	v := base
	v.InputSchema = []byte(strings.Repeat(" ", MaxSchemaBytes) + `{}`)
	if got, err := CanonicalDefinition(context.Background(), v); got != nil || !codeIs(err, f.PayloadTooLarge) {
		t.Fatal("schema byte bound not enforced")
	}
	v = base
	v.StableKey = "builtin:../../forged"
	if got, err := CanonicalDefinition(context.Background(), v); got != nil || !codeIs(err, f.InvalidArgument) {
		t.Fatal("invalid stable key accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := CanonicalDefinition(ctx, base); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancel did not return zero")
	}
}
func codeIs(err error, code f.Code) bool {
	var fault *f.Fault
	return errors.As(err, &fault) && fault.Code == code
}
