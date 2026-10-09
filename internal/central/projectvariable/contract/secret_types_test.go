package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func testSecretVariable(t *testing.T) SecretVariable {
	t.Helper()
	v := testVariable(t).Fields()
	s, err := NewSecretVariable(SecretVariableFields{v.ID, v.ProjectID, SecretVariableType, v.Name, v.Description, v.Version, v.CreatedAt, v.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func secretValueIs(t *testing.T, use func(func([]byte) error) error, want []byte) {
	t.Helper()
	called := false
	if err := use(func(got []byte) error {
		called = true
		if !bytes.Equal(got, want) {
			t.Error("material differs")
		}
		return nil
	}); err != nil || !called {
		t.Fatal("material unavailable")
	}
}
func TestSecretVariableMaterialOwnershipAndSafeSurfaces(t *testing.T) {
	canary := []byte("fixture-only-secret-material")
	material, err := sc.NewSecretMaterial(canary)
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	request, err := NewSecretVariableCreate(SecretVariableCreateFields{testID[i.ProjectVariable](1), "NAME", "description", material})
	if err != nil {
		t.Fatal(err)
	}
	defer request.Destroy()
	material.Destroy()
	secretValueIs(t, request.UseValue, canary)
	clone, err := request.Clone()
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Destroy()
	var borrowed []byte
	if err = request.UseValue(func(v []byte) error { borrowed = v; return nil }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("borrowed material not cleared")
	}
	for _, v := range []any{request, struct{ Request SecretVariableCreate }{request}, struct{ request SecretVariableCreate }{request}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, v), string(canary)) {
				t.Fatal("fmt disclosure")
			}
		}
		raw, err := json.Marshal(v)
		if err != nil || bytes.Contains(raw, canary) {
			t.Fatal("JSON disclosure")
		}
		var out bytes.Buffer
		slog.New(slog.NewJSONHandler(&out, nil)).Info("fixture", "input", v)
		if bytes.Contains(out.Bytes(), canary) {
			t.Fatal("log disclosure")
		}
	}
	request.Destroy()
	if request.Validate() == nil {
		t.Fatal("destroyed request usable")
	}
	secretValueIs(t, clone.UseValue, canary)
}
func TestSecretVariableStrictInputAndPresence(t *testing.T) {
	var update SecretVariableUpdate
	if err := json.Unmarshal([]byte(`{"name":"X"}`), &update); err != nil {
		t.Fatal(err)
	}
	if update.Fields().ValuePresent || update.UseValue(func([]byte) error { return nil }) == nil {
		t.Fatal("absent value fabricated")
	}
	for _, raw := range [][]byte{
		[]byte(`{}`), []byte(`{"value":""}`), []byte(`{"value":null}`), []byte(`{"Value":"x"}`), []byte(`{"value":"x","\u0076alue":"y"}`), []byte(`{"value":"\ud800"}`), []byte(`{"value":"\udc00"}`), []byte(`{"value":"\u0000"}`), []byte(`{"value":"x","credential_ref":"forbidden"}`), []byte(`{"value":"x"}{}`), []byte("{\"value\":\"\xff\"}"),
	} {
		if update.UnmarshalJSON(raw) == nil || update.Fields().Name == nil || *update.Fields().Name != "X" {
			t.Fatal("invalid or non-atomic decode")
		}
	}
	worst := bytes.Repeat([]byte("<"), MaxSecretValueBytes)
	raw, err := json.Marshal(map[string]string{"value": string(worst)})
	if err != nil || len(raw) >= MaxSecretRequestBytes {
		t.Fatal("escaping limit")
	}
	if err = update.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	defer update.Destroy()
	secretValueIs(t, update.UseValue, worst)
	for _, value := range [][]byte{nil, {}, bytes.Repeat([]byte("x"), MaxSecretValueBytes+1), {0xff}, {0}} {
		if ValidateSecretValue(value) == nil {
			t.Fatal("invalid material accepted")
		}
	}
	var create SecretVariableCreate
	valid := []byte(`{"variable_id":"` + testID[i.ProjectVariable](1).String() + `","name":"X","description":"","value":" \n\t "}`)
	if err = create.UnmarshalJSON(valid); err != nil {
		t.Fatal(err)
	}
	defer create.Destroy()
	secretValueIs(t, create.UseValue, []byte(" \n\t "))
	if create.UnmarshalJSON(bytes.Replace(valid, []byte(`"value":`), []byte(`"secret":`), 1)) == nil {
		t.Fatal("missing value")
	}
	secretValueIs(t, create.UseValue, []byte(" \n\t "))
	name := "ORIGINAL"
	r, err := NewSecretVariableUpdate(SecretVariableUpdateFields{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	name = "CHANGED"
	*r.Fields().Name = "MUTATED"
	if *r.Fields().Name != "ORIGINAL" {
		t.Fatal("metadata pointer alias")
	}
}
func TestSecretVariableMetadataCannotContainMaterial(t *testing.T) {
	v := testSecretVariable(t)
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SecretVariable
	if decoded.UnmarshalJSON(raw) != nil || decoded.Fields() != v.Fields() {
		t.Fatal("metadata roundtrip")
	}
	for _, key := range []string{"value", "value_hash", "credential_ref", "value_length", "masked_value"} {
		bad := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"`+key+`":"canary"}`)...)
		if decoded.UnmarshalJSON(bad) == nil || decoded.Fields() != v.Fields() {
			t.Fatal("unsafe metadata field accepted")
		}
	}
	if decoded.UnmarshalJSON(bytes.Replace(raw, []byte(`"secret"`), []byte(`"variable"`), 1)) == nil {
		t.Fatal("ordinary/secret type confused")
	}
	d := v.Fields()
	d.Name = "agenteam_reserved"
	if _, err = NewSecretVariable(d); err == nil {
		t.Fatal("shared name validation bypass")
	}
}
