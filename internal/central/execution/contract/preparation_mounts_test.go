package contract_test

import (
	"bytes"
	"encoding/json"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func mountInputFixture(t *testing.T) ec.PreparationInputFields {
	t.Helper()
	v := inputFixture(t)
	m := mt.ExecutionMountMetadata{ID: inputID[i.Mount](t, 100), WorkspaceID: inputID[mt.Workspace](t, 101), RunnerID: inputID[rc.Runner](t, 102), RunnerVersion: 3, RunnerName: "runner", RunnerDescription: "safe metadata", Name: "source", Workspace: "workspace", Version: 1}
	a := v.Agent.Fields()
	a.AllowedMountIDs = []i.MountID{m.ID}
	var err error
	v.Agent, err = ac.NewAgentConfig(a)
	inputOK(t, err)
	v.Mounts.Mounts = []mt.ExecutionMountMetadata{m}
	v.Mounts.AttemptBinding = v.AttemptBinding
	return v
}

func TestPreparationInputMountMaterialBinding(t *testing.T) {
	t.Run("nonempty-roundtrip-and-owned-copies", func(t *testing.T) {
		v := mountInputFixture(t)
		original, err := ec.NewPreparationInput(v)
		inputOK(t, err)
		decoded, err := ec.DecodePreparationInput(original.CanonicalBytes())
		inputOK(t, err)
		if !bytes.Equal(original.CanonicalBytes(), decoded.CanonicalBytes()) || original.Digest() != decoded.Digest() {
			t.Fatal("nonempty fixed material changed through the original codec")
		}
		v.Mounts.Mounts[0].Workspace = "later"
		out := decoded.Fields()
		out.Mounts.Mounts[0].RunnerName = "later"
		if original.Fields().Mounts.Mounts[0].Workspace != "workspace" || decoded.Fields().Mounts.Mounts[0].RunnerName != "runner" {
			t.Fatal("captured material borrowed caller memory")
		}
	})
	t.Run("old-empty-bytes", func(t *testing.T) {
		v := inputFixture(t)
		raw, err := json.Marshal(v.Mounts)
		inputOK(t, err)
		const old = `{"request":{"project_id":"01900000-0000-7000-8000-000000000001","agent_id":"01900000-0000-7000-8000-000000000002","execution_id":"01900000-0000-7000-8000-000000000003"},"agent_version":"2","mounts":[]}`
		if string(raw) != old {
			t.Fatal("historical empty Mount encoding changed")
		}
		original, err := ec.NewPreparationInput(v)
		inputOK(t, err)
		decoded, err := ec.DecodePreparationInput(original.CanonicalBytes())
		inputOK(t, err)
		if !bytes.Equal(original.CanonicalBytes(), decoded.CanonicalBytes()) {
			t.Fatal("old empty input failed byte-preserving decode")
		}
	})
	for _, test := range []struct {
		name   string
		change func(*ec.PreparationInputFields)
	}{
		{"foreign-attempt", func(v *ec.PreparationInputFields) { v.Mounts.AttemptBinding = ec.TriggerInputDigest([]byte("foreign")) }},
		{"missing-attempt", func(v *ec.PreparationInputFields) { v.Mounts.AttemptBinding = "" }},
		{"missing-mapping", func(v *ec.PreparationInputFields) { v.Mounts.Mounts[0].WorkspaceID = mt.WorkspaceID{} }},
		{"wrong-allowed-id", func(v *ec.PreparationInputFields) { v.Mounts.Mounts[0].ID = inputID[i.Mount](t, 103) }},
		{"missing-allowed-id", func(v *ec.PreparationInputFields) {
			v.Mounts.Mounts = []mt.ExecutionMountMetadata{}
			v.Mounts.AttemptBinding = ""
		}},
		{"missing-runner-version", func(v *ec.PreparationInputFields) { v.Mounts.Mounts[0].RunnerVersion = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := mountInputFixture(t)
			test.change(&v)
			if _, err := ec.NewPreparationInput(v); err == nil {
				t.Fatal("partial or cross-attempt material accepted")
			}
		})
	}
}
