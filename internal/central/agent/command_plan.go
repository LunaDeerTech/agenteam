package agent

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

type commandInput struct {
	runnerMounts *[]c.RunnerMountBinding
	actor        i.Actor
	meta         f.CommandMeta
	project      i.ProjectID
	target       i.AgentID
	name         c.CommandName
	create       *c.AgentCreate
	update       *c.AgentUpdate
	semantic     f.Digest
	identity     f.CommandIdentity
	raw          json.RawMessage
}

func (in *commandInput) validate() error {
	if err := currentActor(in.actor); err != nil {
		return err
	}
	if in.project.Validate() != nil || in.target.Validate() != nil || c.ValidateAgentCommandMeta(in.name, in.meta) != nil {
		return invalid()
	}
	var request any
	if in.name == c.CreateAgentCommand && in.create != nil && in.update == nil && in.create.Validate() == nil && in.create.Fields().AgentID == in.target {
		request = in.create.Clone()
	} else if in.name == c.UpdateAgentCommand && in.create == nil && in.update != nil && in.update.Validate() == nil {
		request = in.update.Clone()
	} else {
		return invalid()
	}
	var err error
	in.semantic, err = c.AgentCommandDigest(in.actor, in.meta, in.project, in.target, in.name, request)
	if err != nil {
		return err
	}
	in.identity, err = c.AgentCommandIdentity(in.project, in.name, in.meta.IdempotencyKey)
	if err != nil {
		return err
	}
	in.raw, err = canonical(request)
	if in.meta.ExpectedVersion != nil {
		v := *in.meta.ExpectedVersion
		in.meta.ExpectedVersion = &v
	}
	return err
}

func (in commandInput) locks() []f.LockRequest {
	locks := ownerLocks(in.actor, in.project, in.target, in.identity)
	// Capacity and normalized-name uniqueness share this existing Project gate.
	if in.create != nil || in.update != nil && in.update.Fields().Name != nil {
		locks[2] = projectLock(in.project, f.Exclusive)
	}
	return locks
}

func createConfiguration(project i.ProjectID, request c.AgentCreate, at f.Instant) (c.AgentConfig, error) {
	d := request.Fields()
	return c.NewAgentConfig(c.AgentConfigFields{Core: c.AgentCore{ID: d.AgentID, ProjectID: project, Name: d.Name, NormalizedName: strings.ToLower(d.Name), DisplayName: d.DisplayName, TagColor: d.TagColor, Description: d.Description, Instructions: d.Instructions, InjectAgentsMD: d.InjectAgentsMD, ModelRef: d.ModelRef, ReasoningEffort: d.ReasoningEffort, ApprovalPolicy: d.ApprovalPolicy, ApprovalModelRef: d.ApprovalModelRef, Lifecycle: c.AgentActive, Version: 1, CreatedAt: at, UpdatedAt: at}, AllowedToolIDs: d.AllowedToolIDs, AllowedMountIDs: d.AllowedMountIDs, AllowedSecretVariableIDs: d.AllowedSecretVariableIDs})
}

func changedFields(before, after c.AgentConfig) []string {
	a, b := before.Fields(), after.Fields()
	values := []struct {
		name          string
		before, after any
	}{
		{"allowed_mount_ids", a.AllowedMountIDs, b.AllowedMountIDs}, {"allowed_secret_variable_ids", a.AllowedSecretVariableIDs, b.AllowedSecretVariableIDs}, {"allowed_tool_ids", a.AllowedToolIDs, b.AllowedToolIDs},
		{"approval_model_ref", a.Core.ApprovalModelRef, b.Core.ApprovalModelRef}, {"approval_policy", a.Core.ApprovalPolicy, b.Core.ApprovalPolicy},
		{"description", a.Core.Description, b.Core.Description}, {"display_name", a.Core.DisplayName, b.Core.DisplayName}, {"inject_agents_md", a.Core.InjectAgentsMD, b.Core.InjectAgentsMD},
		{"instructions", a.Core.Instructions, b.Core.Instructions}, {"model_ref", a.Core.ModelRef, b.Core.ModelRef}, {"name", a.Core.Name, b.Core.Name}, {"reasoning_effort", a.Core.ReasoningEffort, b.Core.ReasoningEffort}, {"tag_color", a.Core.TagColor, b.Core.TagColor},
	}
	out := []string{}
	for _, v := range values {
		if !sameValue(v.before, v.after) {
			out = append(out, v.name)
		}
	}
	return out
}

func planConfiguration(in commandInput, before *c.AgentConfig, at f.Instant) (c.AgentConfig, []string, error) {
	if in.create != nil {
		if before != nil {
			return c.AgentConfig{}, nil, targetOccupied()
		}
		after, err := createConfigurationWithMounts(in.project, *in.create, at, in.runnerMounts)
		return after, c.EditableFields(), err
	}
	if before == nil {
		return c.AgentConfig{}, nil, fault(f.NotFound)
	}
	b := before.Fields().Core
	if b.Version != *in.meta.ExpectedVersion {
		return c.AgentConfig{}, nil, fault(f.VersionConflict)
	}
	if b.Lifecycle != c.AgentActive {
		return c.AgentConfig{}, nil, fault(f.InvalidState)
	}
	var after c.AgentConfig
	var err error
	if in.runnerMounts != nil {
		after, err = in.update.ProposedWithRunnerMounts(*before, *in.runnerMounts)
	} else {
		after, err = in.update.Proposed(*before)
	}
	if err != nil {
		return c.AgentConfig{}, nil, err
	}
	fields := changedFields(*before, after)
	if len(fields) == 0 {
		return before.Clone(), fields, nil
	}
	if b.Version == math.MaxInt64 {
		return c.AgentConfig{}, nil, counterExhausted()
	}
	d := after.Fields()
	d.Core.Version = b.Version + 1
	if !at.Time().After(b.UpdatedAt.Time()) {
		at, err = f.NewInstant(b.UpdatedAt.Time().Add(time.Microsecond))
		if err != nil {
			return c.AgentConfig{}, nil, counterExhausted()
		}
	}
	d.Core.UpdatedAt = at
	after, err = c.NewAgentConfig(d)
	return after, fields, err
}

func targetOccupied() error {
	out := fault(f.ResourceBusy)
	out.FieldErrors = []f.FieldError{{Path: "/agent_id", Code: "TARGET_OCCUPIED"}}
	return out
}
func counterExhausted() error {
	out := fault(f.InvalidState)
	out.FieldErrors = []f.FieldError{{Path: "/version", Code: "COUNTER_EXHAUSTED"}}
	return out
}

// Validate stored request shape and its relation to the entire pre/postimage.
// JSONB ordering is immaterial; no stored request is decoded into a loose map.
func validateCommandInput(r *commandRecord) error {
	if r.Name == c.CreateAgentCommand {
		request, err := c.DecodeAgentCreate(r.Input)
		if err != nil || request.Fields().AgentID != r.Target {
			return unavailable(err)
		}
		d := request.Fields()
		if r.AddSkillsEnabled == nil || r.InstallSkillEnabled == nil || *r.AddSkillsEnabled != *d.AddSkillsEnabled || *r.InstallSkillEnabled != *d.InstallSkillEnabled {
			return unavailable(nil)
		}
		base, err := createConfigurationWithMounts(r.Project, request, r.After.Fields().Core.CreatedAt, r.RunnerMounts)
		if err != nil {
			return unavailable(err)
		}
		b := base.Fields()
		actual := r.After.Fields()
		// Only the real Tool directory may add its resolved default. All other
		// canonical fields remain exactly the original expanded create request.
		if !*d.InstallSkillEnabled && !slices.Equal(b.AllowedToolIDs, actual.AllowedToolIDs) {
			return unavailable(nil)
		}
		for _, id := range b.AllowedToolIDs {
			if !slices.Contains(actual.AllowedToolIDs, id) {
				return unavailable(nil)
			}
		}
		if len(actual.AllowedToolIDs)-len(b.AllowedToolIDs) > 1 {
			return unavailable(nil)
		}
		b.AllowedToolIDs = actual.AllowedToolIDs
		want, err := c.NewAgentConfig(b)
		if err != nil || !sameValue(want, r.After) || !slices.Equal(r.ChangedFields, c.EditableFields()) {
			return unavailable(err)
		}
	} else {
		request, err := c.DecodeAgentUpdate(r.Input)
		if err != nil || r.Before == nil {
			return unavailable(err)
		}
		if (request.Fields().RunnerMounts == nil) != (r.RunnerMounts == nil) {
			return unavailable(nil)
		}
		var after c.AgentConfig
		if r.RunnerMounts != nil {
			after, err = request.ProposedWithRunnerMounts(*r.Before, *r.RunnerMounts)
		} else {
			after, err = request.Proposed(*r.Before)
		}
		if err != nil {
			return unavailable(err)
		}
		if !slices.Equal(changedFields(*r.Before, after), r.ChangedFields) {
			return unavailable(nil)
		}
		d := after.Fields()
		d.Core.Version = r.After.Fields().Core.Version
		d.Core.UpdatedAt = r.After.Fields().Core.UpdatedAt
		want, err := c.NewAgentConfig(d)
		if err != nil || !sameValue(want, r.After) {
			return unavailable(err)
		}
	}
	return nil
}

func createConfigurationWithMounts(project i.ProjectID, request c.AgentCreate, at f.Instant, bindings *[]c.RunnerMountBinding) (c.AgentConfig, error) {
	d := request.Fields()
	if (d.RunnerMounts == nil) != (bindings == nil) {
		return c.AgentConfig{}, unavailable(nil)
	}
	if bindings != nil {
		ids, err := c.RunnerMountIDs(*bindings)
		if err != nil || c.MatchRunnerMountBindings(*d.RunnerMounts, *bindings, ids) != nil {
			return c.AgentConfig{}, unavailable(nil)
		}
		d.RunnerMounts, d.AllowedMountIDs = nil, ids
		resolved, err := c.NewAgentCreate(d)
		if err != nil {
			return c.AgentConfig{}, err
		}
		return createConfiguration(project, resolved, at)
	}
	return createConfiguration(project, request, at)
}
func (in commandInput) mountDeclarations() *[]c.RunnerMountDeclaration {
	if in.create != nil {
		return in.create.Fields().RunnerMounts
	}
	if in.update != nil {
		return in.update.Fields().RunnerMounts
	}
	return nil
}
func (in commandInput) mountPlanRequest(before *c.AgentConfig) c.RunnerMountPlanRequest {
	d := in.mountDeclarations()
	var items []c.RunnerMountDeclaration
	if d != nil {
		items = slices.Clone(*d)
	}
	return c.RunnerMountPlanRequest{Actor: in.actor, ProjectID: in.project, AgentID: in.target, Command: in.identity, ExpectedOwnerVersion: in.meta.ExpectedVersion, Before: before, Declarations: items}.Clone()
}

func (in commandInput) prepareLocks() ([]f.LockRequest, error) {
	held := in.locks()
	if declarations := in.mountDeclarations(); declarations != nil {
		ids := make([]rc.RunnerID, 0, len(*declarations))
		for _, d := range *declarations {
			ids = append(ids, d.RunnerID)
		}
		extra, err := rc.ConfigurationCandidateLockRequests(ids)
		if err != nil {
			return nil, err
		}
		held = append(held, extra...)
	}
	if len(held) > 512 {
		return nil, fault(f.ResourceBusy)
	}
	return oc.NormalizeLocks(held)
}
