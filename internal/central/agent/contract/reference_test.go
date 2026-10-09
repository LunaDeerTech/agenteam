package contract_test

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	agent "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Compile-time check of the exact port, without providing an authorization fake.
var _ interface {
	RequireCurrentInTx(context.Context, foundation.Tx, identity.Actor, identity.ProjectID, identity.AgentID) (agent.AgentRef, error)
} = (agent.WorkReferences)(nil)

func refFixture(t *testing.T) agent.AgentRef {
	v := coreFixture(t)
	return agent.AgentRef{ProjectID: v.ProjectID, AgentID: v.ID, ConfigVersion: v.Version}
}

func TestAgentRefShapeAndIdentity(t *testing.T) {
	v := refFixture(t)
	v.ConfigVersion = math.MaxInt64
	raw := mustJSON(t, v)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 3 {
		t.Fatal("Ref must have exactly three keys")
	}
	if string(fields["config_version"]) != `"9223372036854775807"` {
		t.Fatal("version precision lost")
	}
	for _, field := range []string{"project_id", "agent_id", "config_version"} {
		if _, ok := fields[field]; !ok {
			t.Fatal("missing Ref key")
		}
	}
	round, err := agent.DecodeAgentRef(raw)
	if err != nil || round != v || round.Clone() != v {
		t.Fatalf("Ref round trip: %v", err)
	}
	if reflect.TypeOf(v.AgentID) == reflect.TypeOf(v.ProjectID) {
		t.Fatal("Agent and Project markers conflated")
	}
	// A second valid project identity is still a valid DTO. Only the future real
	// current-fact implementation can establish membership; this codec cannot.
	v.ProjectID = mustID[identity.Project](t, "01900000-0000-7000-8000-00000000000f")
	if err := v.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*agent.AgentRef){
		func(v *agent.AgentRef) { v.AgentID = identity.AgentID{} },
		func(v *agent.AgentRef) { v.ProjectID = identity.ProjectID{} },
		func(v *agent.AgentRef) { v.ConfigVersion = 0 },
		func(v *agent.AgentRef) { v.ConfigVersion = -1 },
	} {
		bad := refFixture(t)
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid Ref accepted")
		}
		if _, err := json.Marshal(bad); err == nil {
			t.Fatal("invalid Ref serialized")
		}
	}
}

func TestAgentRefStrictCodecAndFailureReceiver(t *testing.T) {
	v := refFixture(t)
	base := string(mustJSON(t, v))
	for _, field := range []string{"project_id", "agent_id", "config_version"} {
		t.Run(field, func(t *testing.T) {
			var m map[string]json.RawMessage
			if err := json.Unmarshal([]byte(base), &m); err != nil {
				t.Fatal(err)
			}
			value := m[field]
			delete(m, field)
			missing := mustJSON(t, m)
			m[field] = json.RawMessage("null")
			null := mustJSON(t, m)
			m[field] = value
			for _, bad := range [][]byte{missing, null,
				[]byte(strings.Replace(base, `"`+field+`":`, `"`+strings.ToUpper(field)+`":`, 1)),
				[]byte(strings.TrimSuffix(base, "}") + `,"` + strings.ToUpper(field) + `":` + string(value) + `}`),
				[]byte(strings.TrimSuffix(base, "}") + `,"` + field + `":` + string(value) + `}`),
			} {
				out := v
				if out.UnmarshalJSON(bad) == nil || out != v {
					t.Fatal("bad Ref accepted or receiver changed")
				}
				decoded, err := agent.DecodeAgentRef(bad)
				if err == nil || decoded != (agent.AgentRef{}) {
					t.Fatal("decode failure did not return zero Ref")
				}
			}
		})
	}
	for _, literal := range []string{`0`, `"0"`, `"01"`, `"1e0"`, `"9223372036854775808"`, `true`, `[]`, `{}`} {
		bad := strings.Replace(base, `"config_version":"1"`, `"config_version":`+literal, 1)
		if _, err := agent.DecodeAgentRef([]byte(bad)); err == nil {
			t.Fatalf("accepted version %s", literal)
		}
	}
	for _, text := range []string{"01900000-0000-4000-8000-00000000000a", "01900000-0000-7000-8000-00000000000A", "00000000-0000-0000-0000-000000000000"} {
		bad := strings.Replace(base, v.AgentID.String(), text, 1)
		if _, err := agent.DecodeAgentRef([]byte(bad)); err == nil {
			t.Fatal("accepted noncanonical Agent ID")
		}
	}
}
