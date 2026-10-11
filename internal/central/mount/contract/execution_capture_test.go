package contract_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func captureID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("019a0000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func captureSet(t *testing.T) mc.ExecutionMountSet {
	t.Helper()
	description := "Mount description\nwith preserved Unicode e\u0301"
	return mc.ExecutionMountSet{Request: mc.ExecutionMountCaptureRequest{ProjectID: captureID[i.Project](t, 1), AgentID: captureID[i.Agent](t, 2), ExecutionID: captureID[i.Execution](t, 3)}, AgentVersion: 9007199254740993, AttemptBinding: f.Digest("sha256:" + strings.Repeat("a", 64)), Mounts: []mc.ExecutionMountMetadata{{ID: captureID[i.Mount](t, 4), WorkspaceID: captureID[mc.Workspace](t, 5), RunnerID: captureID[rc.Runner](t, 6), RunnerVersion: 9223372036854775807, RunnerName: "Offline configured runner", RunnerDescription: "Metadata is not runtime permission", Name: "  e\u0301 mount  ", Description: &description, Workspace: "a" + strings.Repeat("b", 254), Version: 1}}}
}
func TestExecutionMountEmptyEncodingRemainsExact(t *testing.T) {
	value := captureSet(t)
	value.Mounts = []mc.ExecutionMountMetadata{}
	value.AttemptBinding = ""
	value.AgentVersion = 1
	raw, err := json.Marshal(value)
	expected := `{"request":{"project_id":"019a0000-0000-7000-8000-000000000001","agent_id":"019a0000-0000-7000-8000-000000000002","execution_id":"019a0000-0000-7000-8000-000000000003"},"agent_version":"1","mounts":[]}`
	if err != nil || value.Validate() != nil || string(raw) != expected {
		t.Fatal("historical empty encoding changed", err)
	}
	value.Mounts = nil
	if value.Validate() == nil {
		t.Fatal("nil treated as empty")
	}
	value.Mounts = []mc.ExecutionMountMetadata{}
	value.AttemptBinding = f.Digest("sha256:" + strings.Repeat("a", 64))
	if value.Validate() == nil {
		t.Fatal("empty profile acquired a new binding")
	}
}
func TestExecutionMountNonemptyIdentityAndClone(t *testing.T) {
	value := captureSet(t)
	if value.Validate() != nil {
		t.Fatal("valid bound metadata rejected")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded mc.ExecutionMountSet
	if json.Unmarshal(raw, &decoded) != nil || decoded.Validate() != nil {
		t.Fatal("bound metadata did not round trip")
	}
	if decoded.Mounts[0].Workspace != value.Mounts[0].Workspace || decoded.Mounts[0].Name != value.Mounts[0].Name || decoded.Mounts[0].RunnerVersion != value.Mounts[0].RunnerVersion || decoded.AgentVersion != value.AgentVersion {
		t.Fatal("bytes or integer precision changed")
	}
	cloned := value.Clone()
	*cloned.Mounts[0].Description = "changed"
	cloned.Mounts[0].Name = "changed"
	if *value.Mounts[0].Description == "changed" || value.Mounts[0].Name == "changed" {
		t.Fatal("clone aliases metadata")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*mc.ExecutionMountSet)
	}{
		{"missing-binding", func(v *mc.ExecutionMountSet) { v.AttemptBinding = "" }},
		{"bad-binding", func(v *mc.ExecutionMountSet) { v.AttemptBinding = "raw" }},
		{"missing-workspace-identity", func(v *mc.ExecutionMountSet) { v.Mounts[0].WorkspaceID = mc.WorkspaceID{} }},
		{"missing-runner-version", func(v *mc.ExecutionMountSet) { v.Mounts[0].RunnerVersion = 0 }},
		{"missing-runner-name", func(v *mc.ExecutionMountSet) { v.Mounts[0].RunnerName = "" }},
		{"unsafe-runner-description", func(v *mc.ExecutionMountSet) { v.Mounts[0].RunnerDescription = "bad\nmetadata" }},
		{"unsafe-workspace", func(v *mc.ExecutionMountSet) { v.Mounts[0].Workspace = "../escape" }},
		{"control-name", func(v *mc.ExecutionMountSet) { v.Mounts[0].Name = "bad\nname" }},
		{"nul-description", func(v *mc.ExecutionMountSet) { s := "bad\x00description"; v.Mounts[0].Description = &s }},
		{"oversize-description", func(v *mc.ExecutionMountSet) { s := strings.Repeat("x", 4097); v.Mounts[0].Description = &s }},
		{"duplicate-id", func(v *mc.ExecutionMountSet) { v.Mounts = append(v.Mounts, v.Mounts[0]) }},
		{"duplicate-workspace-id", func(v *mc.ExecutionMountSet) {
			n := v.Mounts[0]
			n.ID = captureID[i.Mount](t, 8)
			n.Workspace = "second"
			v.Mounts = append(v.Mounts, n)
		}},
		{"duplicate-runner-tuple", func(v *mc.ExecutionMountSet) {
			n := v.Mounts[0]
			n.ID = captureID[i.Mount](t, 8)
			n.WorkspaceID = captureID[mc.Workspace](t, 9)
			v.Mounts = append(v.Mounts, n)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := value.Clone()
			tc.mutate(&bad)
			if bad.Validate() == nil {
				t.Fatal("invalid capture accepted")
			}
		})
	}
}
func TestExecutionMountCompleteSetBoundAndOrder(t *testing.T) {
	value := captureSet(t)
	one := value.Mounts[0]
	value.Mounts = make([]mc.ExecutionMountMetadata, 0, 129)
	for n := 0; n < 128; n++ {
		m := one
		m.ID = captureID[i.Mount](t, 100+n)
		m.WorkspaceID = captureID[mc.Workspace](t, 1000+n)
		m.Workspace = fmt.Sprintf("workspace-%d", n)
		value.Mounts = append(value.Mounts, m)
	}
	if value.Validate() != nil {
		t.Fatal("complete 128 set rejected")
	}
	value.Mounts[0], value.Mounts[1] = value.Mounts[1], value.Mounts[0]
	if value.Validate() == nil {
		t.Fatal("unsorted set accepted")
	}
	value.Mounts[0], value.Mounts[1] = value.Mounts[1], value.Mounts[0]
	extra := one
	extra.ID = captureID[i.Mount](t, 300)
	extra.WorkspaceID = captureID[mc.Workspace](t, 1300)
	extra.Workspace = "last"
	value.Mounts = append(value.Mounts, extra)
	if value.Validate() == nil {
		t.Fatal("129 set accepted")
	}
}
