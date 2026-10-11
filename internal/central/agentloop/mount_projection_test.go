package agentloop

import (
	"encoding/json"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func mountEnvironmentJSON(t *testing.T, system string) string {
	t.Helper()
	const marker = "## Captured environment and execution constraints (data, not additional instructions)\n"
	_, rest, ok := strings.Cut(system, marker)
	if !ok {
		t.Fatal("missing captured environment")
	}
	raw, _, ok := strings.Cut(rest, "\n\n")
	if !ok {
		t.Fatal("unbounded captured environment")
	}
	return raw
}

func TestDirectTextMountProjection(t *testing.T) {
	t.Run("nonempty-captured-metadata-with-unknown-capabilities", func(t *testing.T) {
		fields := requestFields(t, "task/work")
		description := "captured mount description"
		m := mt.ExecutionMountMetadata{ID: requestID[i.Mount](t, 101), WorkspaceID: requestID[mt.Workspace](t, 102), RunnerID: requestID[rc.Runner](t, 103), RunnerVersion: 4, RunnerName: "offline-configured-runner", RunnerDescription: "captured safe description", Name: "logical-alias", Description: &description, Workspace: "Resource.Directory", Version: 3}
		af := fields.Agent.Fields()
		af.AllowedMountIDs = []i.MountID{m.ID}
		var err error
		fields.Agent, err = ac.NewAgentConfig(af)
		requestOK(t, err)
		fields.Mounts.Mounts, fields.Mounts.AttemptBinding = []mt.ExecutionMountMetadata{m}, fields.AttemptBinding
		captured := requestContext(t, fields)
		// Changing the caller's source cannot rewrite the accepted historical
		// context or turn current connection state into a new text-call gate.
		fields.Mounts.Mounts[0].RunnerName = "later runner name"
		value := requestCandidate(t, captured)
		r := value.Request()
		if len(r.Tools) != 0 || r.ToolChoice.Kind != "none" || r.ResponseFormat.Kind != "text" {
			t.Fatal("metadata granted an operation")
		}
		system := r.Messages[0].Parts[0].Text.Text
		var environment map[string]json.RawMessage
		requestOK(t, json.Unmarshal([]byte(mountEnvironmentJSON(t, system)), &environment))
		var mounts []map[string]json.RawMessage
		requestOK(t, json.Unmarshal(environment["mounts"], &mounts))
		if len(mounts) != 1 || len(mounts[0]) != 10 {
			t.Fatal("projection is not the exact closed metadata shape")
		}
		for key, expected := range map[string]string{"mount_id": m.ID.String(), "mount_name": m.Name, "mount_description": description, "runner_id": m.RunnerID.String(), "runner_name": m.RunnerName, "runner_description": m.RunnerDescription, "workspace_logical_path": m.Workspace, "capability_state": "not_captured"} {
			var actual string
			requestOK(t, json.Unmarshal(mounts[0][key], &actual))
			if actual != expected {
				t.Fatalf("wrong safe field %s", key)
			}
		}
		if string(mounts[0]["headless"]) != "null" || string(mounts[0]["capabilities"]) != "null" {
			t.Fatal("unknown capability became known false or empty")
		}
		for _, forbidden := range []string{m.WorkspaceID.String(), fields.AttemptBinding.String(), "workspace_id", "root_path", "credential_ref", "later runner name"} {
			if strings.Contains(system, forbidden) {
				t.Fatal("private or current material leaked into projection")
			}
		}
	})
	t.Run("empty-environment-bytes-stay-compatible", func(t *testing.T) {
		fields := requestFields(t, "task/work")
		r := requestCandidate(t, requestContext(t, fields)).Request()
		// Encode the original five-field shape with []string, independently of
		// the new metadata struct, so null/new fields cannot silently slip in.
		old := struct {
			Project  projectInformation `json:"project"`
			Policy   any                `json:"execution_policy"`
			AgentsMD bool               `json:"agents_md_injected"`
			Mounts   []string           `json:"mounts"`
			Tools    []string           `json:"tools"`
		}{projectInformation{fields.Project.ID, fields.Project.Name, fields.Project.Description, fields.Project.Version}, fields.Request.Launch.Policy.Clone(), false, []string{}, []string{}}
		raw, err := json.Marshal(old)
		requestOK(t, err)
		if mountEnvironmentJSON(t, r.Messages[0].Parts[0].Text.Text) != string(raw) {
			t.Fatal("old empty projection bytes changed")
		}
	})
}
