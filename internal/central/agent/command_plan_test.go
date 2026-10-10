package agent

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func commandID[K any](t *testing.T, text string) f.ID[K] {
	t.Helper()
	v, e := f.ParseID[K](text)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func planFixture(t *testing.T) (commandInput, f.Instant) {
	t.Helper()
	actor, err := i.NewHuman(commandID[i.User](t, "01900000-0000-7000-8000-000000000001"), commandID[i.Session](t, "01900000-0000-7000-8000-000000000002"))
	if err != nil {
		t.Fatal(err)
	}
	create, err := c.NewAgentCreate(c.AgentCreateFields{AgentID: commandID[i.Agent](t, "01900000-0000-7000-8000-000000000003"), Name: "Agent-One", ModelRef: commandID[mc.Model](t, "01900000-0000-7000-8000-000000000004"), ApprovalPolicy: c.ApprovalDefault, InjectAgentsMD: true, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	if err != nil {
		t.Fatal(err)
	}
	in := commandInput{actor: actor, meta: f.CommandMeta{RequestID: commandID[f.Request](t, "01900000-0000-7000-8000-000000000005"), IdempotencyKey: "agent-create-test"}, project: commandID[i.Project](t, "01900000-0000-7000-8000-000000000006"), target: create.Fields().AgentID, name: c.CreateAgentCommand, create: &create}
	if err = in.validate(); err != nil {
		t.Fatal(err)
	}
	at, err := f.NewInstant(time.Date(2026, 10, 10, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return in, at
}
func requireCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var p *f.Fault
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestAgentPlanCreateUpdateAndNoop(t *testing.T) {
	in, at := planFixture(t)
	base, fields, err := planConfiguration(in, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	if base.Fields().Core.Version != 1 || base.Fields().Core.NormalizedName != "agent-one" || !slices.Equal(fields, c.EditableFields()) || !*in.create.Fields().AddSkillsEnabled || !*in.create.Fields().InstallSkillEnabled {
		t.Fatal("create defaults/configuration")
	}
	_, _, err = planConfiguration(in, &base, at)
	requireCode(t, err, f.ResourceBusy)
	patch, err := c.DecodeAgentUpdate([]byte(`{"name":"Agent-Two","display_name":null,"allowed_tool_ids":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	version := f.Version(1)
	update := in
	update.name = c.UpdateAgentCommand
	update.create = nil
	update.update = &patch
	update.meta.ExpectedVersion = &version
	if err = update.validate(); err != nil {
		t.Fatal(err)
	}
	got, fields, err := planConfiguration(update, &base, at)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fields().Core.Version != 2 || !got.Fields().Core.UpdatedAt.Time().Equal(at.Time().Add(time.Microsecond)) || !slices.Equal(fields, []string{"name"}) || base.Fields().Core.Name != "Agent-One" {
		t.Fatal("version, DB instant or preimage changed")
	}
	if update.locks()[2].Mode != f.Exclusive {
		t.Fatal("rename omitted Project EX")
	}
	noop, err := c.DecodeAgentUpdate([]byte(`{"name":"Agent-One"}`))
	if err != nil {
		t.Fatal(err)
	}
	update.update = &noop
	out, fields, err := planConfiguration(update, &base, at)
	if err != nil || len(fields) != 0 || !sameValue(out, base) {
		t.Fatal("no-op changed canonical", err)
	}
	version = 2
	update.meta.ExpectedVersion = &version
	_, _, err = planConfiguration(update, &base, at)
	requireCode(t, err, f.VersionConflict)
	_, _, err = planConfiguration(update, nil, at)
	requireCode(t, err, f.NotFound)
	full := base.Fields()
	full.Core.Version = math.MaxInt64
	max, err := c.NewAgentConfig(full)
	if err != nil {
		t.Fatal(err)
	}
	version = math.MaxInt64
	update.meta.ExpectedVersion = &version
	update.update = &patch
	_, _, err = planConfiguration(update, &max, at)
	requireCode(t, err, f.InvalidState)
	update.update = &noop
	if out, _, err = planConfiguration(update, &max, at); err != nil || out.Fields().Core.Version != math.MaxInt64 {
		t.Fatal("no-op consumed counter", err)
	}
}

func TestAgentStoredPlanBindsExpandedRequest(t *testing.T) {
	in, at := planFixture(t)
	after, fields, err := planConfiguration(in, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	d := in.create.Fields()
	r := commandRecord{ID: commandID[c.AgentCommand](t, "01900000-0000-7000-8000-000000000007"), Project: in.project, User: commandID[i.User](t, in.actor.Details().UserID), Target: in.target, Name: in.name, Key: in.meta.IdempotencyKey, Semantic: in.semantic, Input: in.raw, Revision: 1, After: after, AddSkillsEnabled: d.AddSkillsEnabled, InstallSkillEnabled: d.InstallSkillEnabled, ChangedFields: fields, State: "planned", Created: at}
	if err = r.validate(); err != nil {
		t.Fatal(err)
	}
	original, err := recordMapping(&r)
	if err != nil {
		t.Fatal(err)
	}
	resolved := after.Fields()
	resolved.AllowedToolIDs = []i.ToolID{commandID[i.Tool](t, "01900000-0000-7000-8000-000000000008")}
	r.After, err = c.NewAgentConfig(resolved)
	if err != nil {
		t.Fatal(err)
	}
	r.Revision++
	if err = r.validate(); err != nil {
		t.Fatal("real directory default shape", err)
	}
	next, err := recordMapping(&r)
	if err != nil || next == original {
		t.Fatal("resolved default/revision not bound")
	}
	// A plausible, independently valid postimage cannot rewrite another
	// requested field under the original command and private writer plan.
	resolved.Core.Instructions = "unrequested"
	r.After, err = c.NewAgentConfig(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.validate(); err == nil {
		t.Fatal("postimage detached from original input")
	}
	r.After = after
	r.Input = []byte(`{"agent_id":"01900000-0000-7000-8000-000000000003","name":"Agent-One","model_ref":"01900000-0000-7000-8000-000000000004","inject_agents_md":true,"approval_policy":"default","allowed_tool_ids":[],"allowed_mount_ids":[],"allowed_secret_variable_ids":[],"name":"Agent-One"}`)
	if err = r.validate(); err == nil {
		t.Fatal("duplicate persistent request accepted")
	}
	// Explicit false is not omission and cannot acquire a default as a side
	// effect of saving the configuration.
	d.InstallSkillEnabled = new(bool)
	disabled, err := c.NewAgentCreate(d)
	if err != nil {
		t.Fatal(err)
	}
	r.Input, err = canonical(disabled)
	if err != nil {
		t.Fatal(err)
	}
	r.InstallSkillEnabled = d.InstallSkillEnabled
	resolved = after.Fields()
	resolved.AllowedToolIDs = []i.ToolID{commandID[i.Tool](t, "01900000-0000-7000-8000-000000000008")}
	r.After, err = c.NewAgentConfig(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.validate(); err == nil {
		t.Fatal("false default bypass")
	}
}
