package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	a "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	m "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	r "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func declarationFixture(t *testing.T) a.RunnerMountDeclaration {
	return a.RunnerMountDeclaration{RunnerID: mustID[r.Runner](t, "01900000-0000-7000-8000-000000000021"), Workspace: "workspace"}
}
func TestAgentRunnerMountDeclarationStrictCompatibility(t *testing.T) {
	legacy := createFixture(t)
	original := mustJSON(t, legacy)
	if bytes.Contains(original, []byte("runner_mounts")) {
		t.Fatal("legacy encoding changed")
	}
	d := legacy.Fields()
	declarations := []a.RunnerMountDeclaration{declarationFixture(t)}
	d.AllowedMountIDs = nil
	d.RunnerMounts = &declarations
	created, err := a.NewAgentCreate(d)
	if err != nil {
		t.Fatal(err)
	}
	raw := mustJSON(t, created)
	if bytes.Contains(raw, []byte("allowed_mount_ids")) {
		t.Fatal("two profiles emitted")
	}
	decoded, err := a.DecodeAgentCreate(raw)
	if err != nil || string(mustJSON(t, decoded)) != string(raw) {
		t.Fatal("new roundtrip", err)
	}
	declarations[0].Workspace = "caller-change"
	if created.Fields().RunnerMounts == nil || (*created.Fields().RunnerMounts)[0].Workspace != "workspace" {
		t.Fatal("borrowed caller storage")
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"runner_mounts":[`, `"allowed_mount_ids":[],"runner_mounts":[`, 1),
		strings.Replace(string(raw), `"runner_mounts":[{"runner_id":"01900000-0000-7000-8000-000000000021","workspace":"workspace"}]`, `"runner_mounts":null`, 1),
		strings.Replace(string(raw), `"workspace":"workspace"`, `"workspace":"workspace","root_path":"/host"`, 1),
		strings.Replace(string(raw), `"workspace":"workspace"`, `"workspace":"workspace","workspace":"other"`, 1),
	} {
		if _, err := a.DecodeAgentCreate([]byte(bad)); err == nil {
			t.Fatal("unsafe request accepted", bad)
		}
	}
	for _, bad := range []string{`{"runner_mounts":null}`, `{"runner_mounts":[],"allowed_mount_ids":[]}`, `{"runner_mounts":[null]}`, `{"runner_mounts":[{"runner_id":"01900000-0000-7000-8000-000000000021","workspace":"../escape"}]}`} {
		if _, err := a.DecodeAgentUpdate([]byte(bad)); err == nil {
			t.Fatal("unsafe update accepted")
		}
	}
	clear, err := a.DecodeAgentUpdate([]byte(`{"runner_mounts":[]}`))
	if err != nil || clear.Fields().RunnerMounts == nil || *clear.Fields().RunnerMounts == nil || len(*clear.Fields().RunnerMounts) != 0 {
		t.Fatal("clear lost", err)
	}
	omit, err := a.DecodeAgentUpdate([]byte(`{"description":"same"}`))
	if err != nil || omit.Fields().RunnerMounts != nil {
		t.Fatal("omission lost")
	}
}

func TestAgentRunnerMountBoundariesAndReceiptProof(t *testing.T) {
	d := declarationFixture(t)
	for _, name := range []string{"a", strings.Repeat("a", 255), "COM10", "con-file", "work.data"} {
		d.Workspace = name
		if d.Validate() != nil {
			t.Fatal("valid directory rejected", name)
		}
	}
	for _, name := range []string{"", strings.Repeat("a", 256), ".hidden", "a.", "con.txt", "LPT9", "a/b", "a\\b", "é"} {
		d.Workspace = name
		if d.Validate() == nil {
			t.Fatal("invalid directory accepted", name)
		}
	}
	declarations := make([]a.RunnerMountDeclaration, 128)
	for n := range declarations {
		declarations[n] = declarationFixture(t)
		declarations[n].Workspace = fmt.Sprintf("workspace-%03d", n)
	}
	if a.ValidateRunnerMountDeclarations(declarations) != nil {
		t.Fatal("128 rejected")
	}
	if a.ValidateRunnerMountDeclarations(append(declarations, declarationFixture(t))) == nil || a.ValidateRunnerMountDeclarations([]a.RunnerMountDeclaration{declarations[0], declarations[0]}) == nil {
		t.Fatal("cap or duplicate accepted")
	}
	binding := a.RunnerMountBinding{RunnerID: declarations[0].RunnerID, Workspace: declarations[0].Workspace, MountID: mustID[i.Mount](t, "01900000-0000-7000-8000-000000000022"), WorkspaceID: mustID[m.Workspace](t, "01900000-0000-7000-8000-000000000023")}
	bindings := []a.RunnerMountBinding{binding}
	ids, err := a.RunnerMountIDs(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if a.MatchRunnerMountBindings(declarations[:1], bindings, ids) != nil {
		t.Fatal("exact mapping rejected")
	}
	wrong := append([]a.RunnerMountBinding{}, bindings...)
	wrong[0].Workspace = "different"
	if a.MatchRunnerMountBindings(declarations[:1], wrong, ids) == nil {
		t.Fatal("shape-only mapping accepted")
	}
	config := configFixture(t).Fields()
	config.AllowedMountIDs = ids
	value, err := a.NewAgentConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	mutation, err := a.NewAgentMutation(a.AgentMutationFields{Agent: value, Changed: true, EventIDs: []e.EventID{mustID[e.EventIdentity](t, "01900000-0000-7000-8000-000000000024")}, RunnerMounts: &bindings})
	if err != nil {
		t.Fatal(err)
	}
	raw := mustJSON(t, mutation)
	round, err := a.DecodeAgentMutation(raw)
	if err != nil || round.Fields().RunnerMounts == nil {
		t.Fatal("receipt mapping lost", err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("json")
	}
	fields["runner_mounts"] = json.RawMessage(`null`)
	bad, _ := json.Marshal(fields)
	if _, err = a.DecodeAgentMutation(bad); err == nil {
		t.Fatal("null receipt accepted")
	}
	mutationFields := mutation.Fields()
	empty := []a.RunnerMountBinding{}
	mutationFields.RunnerMounts = &empty
	if _, err = a.NewAgentMutation(mutationFields); err == nil {
		t.Fatal("mapping set not equal allowlist")
	}
	legacy, err := a.NewAgentMutation(a.AgentMutationFields{Agent: value, Changed: false, EventIDs: []e.EventID{}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(mustJSON(t, legacy), []byte("runner_mounts")) {
		t.Fatal("legacy receipt gained field")
	}
}
