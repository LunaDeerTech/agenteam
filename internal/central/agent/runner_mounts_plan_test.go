package agent

import (
	"slices"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	m "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	r "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func TestAgentRunnerMountFrozenPlanAndNoop(t *testing.T) {
	in, at := planFixture(t)
	legacy := in.semantic
	d := in.create.Fields()
	decl := []c.RunnerMountDeclaration{{RunnerID: commandID[r.Runner](t, "01900000-0000-7000-8000-000000000021"), Workspace: "dir"}}
	d.RunnerMounts = &decl
	d.AllowedMountIDs = nil
	request, err := c.NewAgentCreate(d)
	if err != nil {
		t.Fatal(err)
	}
	in.create = &request
	if err = in.validate(); err != nil {
		t.Fatal(err)
	}
	if legacy == in.semantic {
		t.Fatal("declaration absent from semantic")
	}
	bindings := []c.RunnerMountBinding{{RunnerID: decl[0].RunnerID, Workspace: decl[0].Workspace, MountID: commandID[i.Mount](t, "01900000-0000-7000-8000-000000000022"), WorkspaceID: commandID[m.Workspace](t, "01900000-0000-7000-8000-000000000023")}}
	if _, _, err := planConfiguration(in, nil, at); err == nil {
		t.Fatal("unbound declarations accepted")
	}
	in.runnerMounts = &bindings
	after, fields, err := planConfiguration(in, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	record := commandRecord{RunnerMounts: &bindings, ID: commandID[c.AgentCommand](t, "01900000-0000-7000-8000-000000000024"), Project: in.project, User: commandID[i.User](t, in.actor.Details().UserID), Target: in.target, Name: in.name, Key: in.meta.IdempotencyKey, Semantic: in.semantic, Input: in.raw, Revision: 1, After: after, AddSkillsEnabled: d.AddSkillsEnabled, InstallSkillEnabled: d.InstallSkillEnabled, ChangedFields: fields, State: "planned", Created: at}
	if err = record.validate(); err != nil {
		t.Fatal("valid frozen command", err)
	}
	goodMapping, err := recordMapping(&record)
	if err != nil {
		t.Fatal(err)
	}
	wrong := slices.Clone(bindings)
	wrong[0].Workspace = "other"
	record.RunnerMounts = &wrong
	if record.validate() == nil {
		t.Fatal("mapping detached from original intent")
	}
	wrong = slices.Clone(bindings)
	wrong[0].WorkspaceID = commandID[m.Workspace](t, "01900000-0000-7000-8000-000000000025")
	record.RunnerMounts = &wrong
	changedMapping, err := recordMapping(&record)
	if err != nil || changedMapping == goodMapping {
		t.Fatal("workspace allocation not bound to private witness", err)
	}
	version := f.Version(1)
	patch, err := c.NewAgentUpdate(c.AgentUpdateFields{RunnerMounts: &decl})
	if err != nil {
		t.Fatal(err)
	}
	update := in
	update.name = c.UpdateAgentCommand
	update.create = nil
	update.update = &patch
	update.meta.ExpectedVersion = &version
	update.meta.IdempotencyKey = "runner-mount-noop"
	if err = update.validate(); err != nil {
		t.Fatal(err)
	}
	noop, fields, err := planConfiguration(update, &after, at)
	if err != nil || len(fields) != 0 || !sameValue(noop, after) {
		t.Fatal("same mapping caused mutation", err)
	}
	emptyDecl := []c.RunnerMountDeclaration{}
	clear, err := c.NewAgentUpdate(c.AgentUpdateFields{RunnerMounts: &emptyDecl})
	if err != nil {
		t.Fatal(err)
	}
	emptyBindings := []c.RunnerMountBinding{}
	update.update = &clear
	update.runnerMounts = &emptyBindings
	cleared, fields, err := planConfiguration(update, &after, at)
	if err != nil || cleared.Fields().Core.Version != 2 || len(cleared.Fields().AllowedMountIDs) != 0 || !slices.Equal(fields, []string{"allowed_mount_ids"}) {
		t.Fatal("clear not original Agent mutation", err)
	}
	held, err := in.prepareLocks()
	if err != nil {
		t.Fatal(err)
	}
	required, err := r.ConfigurationCandidateLockRequests([]r.RunnerID{decl[0].RunnerID})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range required {
		if !slices.ContainsFunc(held, func(got f.LockRequest) bool {
			return got.Mode == want.Mode && f.CompareLockKeys(got.Key, want.Key) == 0
		}) {
			t.Fatal("first union missing Runner lock")
		}
	}
	for n := 1; n < len(held); n++ {
		if f.CompareLockKeys(held[n-1].Key, held[n].Key) >= 0 {
			t.Fatal("first union unsorted")
		}
	}
}
