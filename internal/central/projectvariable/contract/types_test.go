package contract

import (
	"encoding/json"
	"fmt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
	"time"
)

func testID[K any](n int) f.ID[K] {
	id, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		panic(err)
	}
	return id
}
func testVariable(t *testing.T) Variable {
	t.Helper()
	at, _ := f.NewInstant(time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
	v, err := NewVariable(VariableFields{testID[i.ProjectVariable](1), testID[i.Project](2), VariableType, "API_URL", "ordinary description", "private-value-canary", 1, at, at})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestVariableTextRulesAndStrictAtomicCodec(t *testing.T) {
	for _, name := range []string{"A", "_", "a0", "PATH", strings.Repeat("A", 128)} {
		if ValidateName(name) != nil {
			t.Fatalf("valid name rejected")
		}
	}
	for _, name := range []string{"", "1A", "A-B", "A=B", "é", " A", "AGENTEAM", "agenteam_key", "AgEnTeAm_X", strings.Repeat("A", 129)} {
		if ValidateName(name) == nil {
			t.Fatalf("invalid name accepted")
		}
	}
	v := testVariable(t)
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var copy Variable
	if json.Unmarshal(raw, &copy) != nil || copy.Fields() != v.Fields() {
		t.Fatal("roundtrip")
	}
	for _, bad := range [][]byte{[]byte(`null`), append(append([]byte{}, raw...), []byte(`{}`)...), []byte(strings.Replace(string(raw), `"name":`, `"Name":`, 1)), []byte(strings.Replace(string(raw), `"name":`, `"name":"duplicate","name":`, 1)), []byte(strings.Replace(string(raw), `"API_URL"`, `"\ud800"`, 1)), []byte(strings.Replace(string(raw), `"value":"private-value-canary"`, `"value":null`, 1)), []byte(strings.Replace(string(raw), `"value":"private-value-canary"`, `"value":"\u0000"`, 1))} {
		dst := v
		if dst.UnmarshalJSON(bad) == nil || dst.Fields() != v.Fields() {
			t.Fatal("strict/atomic decode")
		}
	}
	maximal := v.Fields()
	maximal.Name = strings.Repeat("A", MaxNameBytes)
	maximal.Description = strings.Repeat("<", MaxDescriptionBytes)
	maximal.Value = strings.Repeat("<", MaxValueBytes)
	big, err := NewVariable(maximal)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(big)
	if err != nil || len(encoded) > MaxReceiptBytes {
		t.Fatal("worst escaping")
	}
	if json.Unmarshal(encoded, &copy) != nil {
		t.Fatal("max roundtrip")
	}
	for _, v := range []string{"\x00", strings.Repeat("x", MaxValueBytes+1), string([]byte{0xff})} {
		if ValidateValue(v) == nil {
			t.Fatal("invalid value")
		}
	}
	if ValidateValue("\n\t\x01") != nil || ValidateDescription("\n\r\t") != nil || ValidateDescription("\x01") == nil {
		t.Fatal("field-specific control rule")
	}
}
func TestVariableOpaqueValuesAndPresenceAreCaptured(t *testing.T) {
	value := "private-update-canary"
	r, err := NewVariableUpdate(VariableUpdateFields{Value: &value})
	if err != nil {
		t.Fatal(err)
	}
	value = "mutated"
	fields := r.Fields()
	*fields.Value = "second"
	if *r.Fields().Value != "private-update-canary" {
		t.Fatal("alias")
	}
	var clear VariableUpdate
	if json.Unmarshal([]byte(`{"value":""}`), &clear) != nil || clear.Fields().Value == nil || *clear.Fields().Value != "" {
		t.Fatal("empty versus omitted")
	}
	for _, raw := range []string{`{}`, `{"value":null}`, `{"value":"x","\u0076alue":"x"}`, `{"Value":"x"}`, `{"name":"X","extra":1}`} {
		if json.Unmarshal([]byte(raw), &clear) == nil {
			t.Fatal("bad update")
		}
	}
	v := testVariable(t)
	for _, x := range []any{v, r, struct{ X Variable }{v}, struct{ x VariableUpdate }{r}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			out := fmt.Sprintf(format, x)
			if strings.Contains(out, "canary") || strings.Contains(out, "API_URL") {
				t.Fatal("enclosing log disclosure")
			}
		}
	}
}
