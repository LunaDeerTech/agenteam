package contract_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	agent "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	secret "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// These aliases represent downstream consumers, not new canonical markers.
type probeToolID = identity.ToolID
type probeMountID = identity.MountID
type probeVariableID = identity.ProjectVariableID

func probeTool(id foundation.ID[identity.Tool]) probeToolID                    { return id }
func probeMount(id probeMountID) foundation.ID[identity.Mount]                 { return id }
func probeVariable(id foundation.ID[identity.ProjectVariable]) probeVariableID { return id }

const probeUUID = "0190c2a7-47ac-734a-83fe-c3b37fdd1011"

func TestIndependentResourceIdentityConsumers(t *testing.T) {
	tool, err := foundation.ParseID[identity.Tool](probeUUID)
	if err != nil {
		t.Fatal(err)
	}
	mount, err := foundation.ParseID[identity.Mount](probeUUID)
	if err != nil {
		t.Fatal(err)
	}
	variable, err := foundation.ParseID[identity.ProjectVariable](probeUUID)
	if err != nil {
		t.Fatal(err)
	}
	if probeTool(tool).String() != probeUUID || probeMount(mount).String() != probeUUID || probeVariable(variable).String() != probeUUID {
		t.Fatal("downstream consumer changed identity")
	}
	aliases := [][3]reflect.Type{
		{reflect.TypeFor[identity.ToolID](), reflect.TypeFor[probeToolID](), reflect.TypeFor[foundation.ID[identity.Tool]]()},
		{reflect.TypeFor[identity.MountID](), reflect.TypeFor[probeMountID](), reflect.TypeFor[foundation.ID[identity.Mount]]()},
		{reflect.TypeFor[identity.ProjectVariableID](), reflect.TypeFor[probeVariableID](), reflect.TypeFor[foundation.ID[identity.ProjectVariable]]()},
	}
	for _, row := range aliases {
		if row[0] != row[1] || row[0] != row[2] {
			t.Fatal("alias no longer denotes canonical Foundation ID")
		}
	}
	// Consume existing public field types without constructing business facts.
	types := []reflect.Type{reflect.TypeOf(tool), reflect.TypeOf(mount), reflect.TypeOf(variable), reflect.TypeOf(agent.AgentCore{}.ID), reflect.TypeOf(secret.RefDetails{}.ID)}
	for i, left := range types {
		for j, right := range types {
			if i != j && (left == right || left.AssignableTo(right)) {
				t.Fatalf("domain IDs implicitly interchangeable: %v and %v", left, right)
			}
		}
	}
}

func TestIndependentResourceIdentityScalars(t *testing.T) {
	t.Run("tool", probeScalar[identity.Tool])
	t.Run("mount", probeScalar[identity.Mount])
	t.Run("project-variable", probeScalar[identity.ProjectVariable])
}

func probeScalar[K any](t *testing.T) {
	t.Parallel()
	fresh, err := foundation.NewID[K]()
	if err != nil || fresh.Validate() != nil {
		t.Fatal("generation failed")
	}
	parsed, err := foundation.ParseID[K](fresh.String())
	if err != nil || parsed != fresh {
		t.Fatal("generated ID failed to round-trip")
	}
	v, err := foundation.ParseID[K](probeUUID)
	if err != nil {
		t.Fatal(err)
	}
	text, err := v.MarshalText()
	if err != nil || string(text) != probeUUID {
		t.Fatal("Text output is not canonical")
	}
	raw, err := json.Marshal(v)
	if err != nil || string(raw) != `"`+probeUUID+`"` {
		t.Fatal("JSON output is not canonical")
	}
	var decoded foundation.ID[K]
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded != v {
		t.Fatal("JSON round-trip changed ID")
	}
	if err := decoded.UnmarshalJSON([]byte(`"\u0030190c2a7-47ac-734a-83fe-c3b37fdd1011"`)); err != nil || decoded != v {
		t.Fatal("existing Foundation JSON escapes changed")
	}
	copyID := v
	buffer := []byte(fresh.String())
	if err := copyID.UnmarshalText(buffer); err != nil || copyID != fresh {
		t.Fatal("valid Text replacement failed")
	}
	buffer[0] = '!'
	if v.String() != probeUUID || copyID != fresh {
		t.Fatal("copied ID or input bytes share mutable state")
	}
	var zero foundation.ID[K]
	if zero.Validate() == nil {
		t.Fatal("zero accepted")
	}
	if _, err := zero.MarshalText(); err == nil {
		t.Fatal("zero serialized as Text")
	}
	if _, err := zero.MarshalJSON(); err == nil {
		t.Fatal("zero serialized as JSON")
	}
}

func TestIndependentResourceIdentityRejection(t *testing.T) {
	t.Run("tool", probeRejection[identity.Tool])
	t.Run("mount", probeRejection[identity.Mount])
	t.Run("project-variable", probeRejection[identity.ProjectVariable])
}

func probeRejection[K any](t *testing.T) {
	t.Parallel()
	initial, err := foundation.ParseID[K](probeUUID)
	if err != nil {
		t.Fatal(err)
	}
	// Representative inheritance boundaries, not a second UUID implementation.
	badText := []string{
		"00000000-0000-0000-0000-000000000000",
		"0190c2a7-47ac-434a-83fe-c3b37fdd1011",
		"0190c2a7-47ac-734a-c3fe-c3b37fdd1011",
		strings.ToUpper(probeUUID), probeUUID[:35], "!" + probeUUID[1:],
		"builtin:sentinel-private-input", "runner:sentinel-private-input",
		"mcp:sentinel-private-input:remote", "SENTINEL_VARIABLE_NAME",
		"/workspace/sentinel-private-input", "sentinel-private-input",
	}
	var zero foundation.ID[K]
	for _, input := range badText {
		got, err := foundation.ParseID[K](input)
		probeOpaqueError(t, err, input)
		if got != zero {
			t.Fatal("failed Parse returned partial value")
		}
		receiver := initial
		err = receiver.UnmarshalText([]byte(input))
		probeOpaqueError(t, err, input)
		if receiver != initial {
			t.Fatal("failed Text decode replaced receiver")
		}
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		err = receiver.UnmarshalJSON(raw)
		probeOpaqueError(t, err, input)
		if receiver != initial {
			t.Fatal("invalid UUID JSON replaced receiver")
		}
	}
	badJSON := [][]byte{
		[]byte(`null`), []byte(`123`), []byte(`true`), []byte(`{}`), []byte(`[]`),
		[]byte(`"\ud800"`), []byte{'"', 0xff, '"'},
		[]byte(`"` + probeUUID + `" "sentinel-private-input"`),
		[]byte(`{"sentinel-private-input":"` + probeUUID + `"}`),
	}
	for _, raw := range badJSON {
		receiver := initial
		probeOpaqueError(t, receiver.UnmarshalJSON(raw), "sentinel-private-input")
		if receiver != initial {
			t.Fatal("failed direct JSON decode replaced receiver")
		}
		if err := json.Unmarshal(raw, &receiver); err == nil || receiver != initial {
			t.Fatal("failed standard JSON decode accepted input or replaced receiver")
		}
	}
}

func probeOpaqueError(t *testing.T, err error, input string) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid input accepted")
	}
	if strings.Contains(err.Error(), input) {
		t.Fatal("error echoed input")
	}
}
