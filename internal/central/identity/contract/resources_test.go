package contract_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	secret "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// A downstream domain can use aliases without introducing its own marker.
type consumerToolID = identity.ToolID
type consumerMountID = identity.MountID
type consumerVariableID = identity.ProjectVariableID

// These assignments also fail compilation if an exported ID uses the wrong
// marker or becomes a distinct defined wrapper rather than the canonical alias.
var (
	_ consumerToolID     = foundation.ID[identity.Tool]{}
	_ consumerMountID    = foundation.ID[identity.Mount]{}
	_ consumerVariableID = foundation.ID[identity.ProjectVariable]{}
)

func TestResourceIdentitiesCanonicalAliasesAndTypeSeparation(t *testing.T) {
	aliases := []struct{ canonical, consumer, generic reflect.Type }{
		{reflect.TypeFor[identity.ToolID](), reflect.TypeFor[consumerToolID](), reflect.TypeFor[foundation.ID[identity.Tool]]()},
		{reflect.TypeFor[identity.MountID](), reflect.TypeFor[consumerMountID](), reflect.TypeFor[foundation.ID[identity.Mount]]()},
		{reflect.TypeFor[identity.ProjectVariableID](), reflect.TypeFor[consumerVariableID](), reflect.TypeFor[foundation.ID[identity.ProjectVariable]]()},
	}
	for _, a := range aliases {
		if a.canonical != a.consumer || a.canonical != a.generic || !a.consumer.AssignableTo(a.canonical) || !a.canonical.AssignableTo(a.consumer) {
			t.Fatal("consumer does not share the canonical Foundation identity")
		}
	}
	types := []reflect.Type{
		reflect.TypeFor[identity.ToolID](), reflect.TypeFor[identity.MountID](),
		reflect.TypeFor[identity.ProjectVariableID](), reflect.TypeFor[identity.AgentID](),
		reflect.TypeFor[secret.CredentialID](),
	}
	for i, a := range types {
		for j, b := range types {
			if i != j && (a == b || a.AssignableTo(b)) {
				t.Fatalf("different domain identities are implicitly assignable: %v -> %v", a, b)
			}
		}
	}
	// Identical UUID bytes may be parsed in different domains; membership and
	// authorization do not follow from successful parsing. Explicit Go conversion
	// is not asserted impossible by this test or by the resource identity contract.
	const text = "01900000-0000-7000-8000-00000000000a"
	tool, err := foundation.ParseID[identity.Tool](text)
	if err != nil {
		t.Fatal(err)
	}
	mount, err := foundation.ParseID[identity.Mount](text)
	if err != nil {
		t.Fatal(err)
	}
	variable, err := foundation.ParseID[identity.ProjectVariable](text)
	if err != nil {
		t.Fatal(err)
	}
	var consumeTool consumerToolID = tool
	var consumeMount consumerMountID = mount
	var consumeVariable consumerVariableID = variable
	if consumeTool.String() != text || consumeMount.String() != text || consumeVariable.String() != text {
		t.Fatal("consumer changed the stable identity")
	}
}

func TestResourceIdentitiesInheritedScalarContract(t *testing.T) {
	t.Run("tool", testResourceScalar[identity.Tool])
	t.Run("mount", testResourceScalar[identity.Mount])
	t.Run("project-variable", testResourceScalar[identity.ProjectVariable])
}

func testResourceScalar[K any](t *testing.T) {
	t.Helper()
	const canonical = "01900000-0000-7000-8000-00000000000a"
	const replacement = "01900000-0000-7000-8000-00000000000b"
	v, err := foundation.ParseID[K](canonical)
	if err != nil {
		t.Fatal(err)
	}
	text, err := v.MarshalText()
	if err != nil || string(text) != canonical {
		t.Fatal("Text did not preserve canonical identity")
	}
	raw, err := json.Marshal(v)
	if err != nil || string(raw) != `"`+canonical+`"` {
		t.Fatal("JSON did not preserve canonical identity")
	}
	var round foundation.ID[K]
	if err := json.Unmarshal(raw, &round); err != nil || round != v {
		t.Fatal("JSON round trip changed identity")
	}
	// Keep the existing Foundation scalar's accepted JSON escapes. This alias
	// does not introduce a second parser or an additional raw-body contract.
	if err := json.Unmarshal([]byte(`"\u00301900000-0000-7000-8000-00000000000a"`), &round); err != nil || round != v {
		t.Fatal("Foundation JSON escape semantics changed")
	}
	copy := v
	if err := copy.UnmarshalText([]byte(replacement)); err != nil || copy == v || v.String() != canonical {
		t.Fatal("value copy shares mutable state")
	}
	fresh, err := foundation.NewID[K]()
	if err != nil || fresh.Validate() != nil {
		t.Fatal("Foundation generation unavailable")
	}
	parsed, err := foundation.ParseID[K](fresh.String())
	if err != nil || parsed != fresh {
		t.Fatal("generated ID did not round trip")
	}
	var zero foundation.ID[K]
	if zero.Validate() == nil {
		t.Fatal("zero ID accepted")
	}
	if _, err := zero.MarshalText(); err == nil {
		t.Fatal("zero ID serialized as Text")
	}
	if _, err := json.Marshal(zero); err == nil {
		t.Fatal("zero ID serialized as JSON")
	}

	badText := []string{
		"", "00000000-0000-0000-0000-000000000000",
		"01900000-0000-4000-8000-00000000000a", // Wrong UUID version.
		"01900000-0000-7000-c000-00000000000a", // Wrong UUID variant.
		"01900000-0000-7000-8000-00000000000A", // Noncanonical case.
		canonical[:35], canonical + "0", " " + canonical, canonical + "\x00",
		"builtin:read-file", "runner:run-command", "mcp:" + canonical + ":remote",
		"GITHUB_TOKEN", "/workspace/project/agent/source", "private-input-sentinel",
	}
	for _, input := range badText {
		out, err := foundation.ParseID[K](input)
		if err == nil || out != zero {
			t.Fatal("invalid input parsed or returned partial identity")
		}
		before := v
		if err := v.UnmarshalText([]byte(input)); err == nil || v != before {
			t.Fatal("failed Text decode changed receiver")
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &v); err == nil || v != before {
			t.Fatal("invalid UUID JSON changed receiver")
		}
	}
	badJSON := [][]byte{
		[]byte(`null`), []byte(`true`), []byte(`7`), []byte(`[]`), []byte(`{}`),
		[]byte(`"\ud800"`), []byte(`"\udfff"`), []byte(`"` + canonical + `" false`),
		[]byte(`"` + canonical + `""` + canonical + `"`),
		[]byte(`"unterminated`), append([]byte{'"'}, 0xff, '"'),
	}
	for _, input := range badJSON {
		before := v
		if err := v.UnmarshalJSON(input); err == nil || v != before {
			t.Fatal("failed direct JSON decode changed receiver")
		}
		if err := json.Unmarshal(input, &v); err == nil || v != before {
			t.Fatal("failed standard JSON decode changed receiver")
		}
	}
	_, err = foundation.ParseID[K]("private-input-sentinel")
	if err == nil || strings.Contains(err.Error(), "private-input-sentinel") {
		t.Fatal("parser error exposed input")
	}
}
