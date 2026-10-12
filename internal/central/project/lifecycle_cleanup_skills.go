package project

import (
	"bytes"
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// SkillsCleanupDomain is the existing Skills service's cleanup-only port.
// It deliberately has no Name: Skills alone cannot complete the combined
// agent-skills-variables participant.
type SkillsCleanupDomain interface {
	Cleanup(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error)
}

// NewLifecycleCleanupDriverForSkills binds only the Skills substep. It retains
// the original Project authority used by the service graph and the original
// cleanup driver's call/claim ownership. Child completion leaves the parent
// pending in domains; this constructor cannot advance Object, Outbox or Audit.
func NewLifecycleCleanupDriverForSkills(store Store, projects *Authority, processes oc.ProcessAuthority, skills SkillsCleanupDomain) (*LifecycleCleanupDriver, error) {
	ps := projects.state()
	if nilPort(store) || ps == nil || !sameStore(store, ps.store) || nilPort(processes) || nilPort(skills) {
		return nil, fault(f.DependencyUnbound)
	}
	authority, ok := ps.lifecycle.(*LifecycleAuthority)
	if !ok || !authority.bound() || !sameStore(store, authority.store) {
		return nil, fault(f.DependencyUnbound)
	}
	process := processes.CurrentProcess()
	if process.Validate() != nil {
		return nil, invalid()
	}
	registration, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		return nil, portError(err)
	}
	return &LifecycleCleanupDriver{state: &lifecycleCleanupState{store: store, authority: authority, processes: processes, process: process, skills: skills, registration: registration, calls: map[c.ProjectID]context.CancelFunc{}, owners: map[c.OperationID]lifecycleCleanupOwner{}, changed: make(chan struct{})}}, nil
}

// The parent schema is distinct from Skills' provider-owned schema 1. Opaque
// provider bytes are preserved exactly; nil is valid for both child states.
type skillsDomainCheckpoint struct {
	Schema f.Version `json:"schema"`
	Bytes  []byte    `json:"bytes"`
}

type skillsCleanupCheckpoint struct {
	Project    string                  `json:"project_id"`
	Operation  string                  `json:"operation_id"`
	Version    f.Version               `json:"project_version"`
	State      c.CleanupState          `json:"skills_state"`
	Checkpoint *skillsDomainCheckpoint `json:"skills_checkpoint,omitempty"`
}

func decodeSkillsCleanupCheckpoint(checkpoint *c.CleanupCheckpoint, cause c.LifecycleCause, scope c.ScopeRef) (skillsCleanupCheckpoint, *c.CleanupCheckpoint, error) {
	initial := skillsCleanupCheckpoint{Project: scope.ProjectID.String(), Operation: cause.OperationID.String(), Version: cause.ProjectVersion, State: c.CleanupPending}
	if cause.Validate() != nil || cause.Action != c.Delete || scope.Validate() != nil || scope.Kind != c.ProjectScope {
		return initial, nil, unavailable(nil)
	}
	if checkpoint == nil {
		return initial, nil, nil
	}
	if !checkpoint.Matches(c.SkillsParticipant, cause, scope) || checkpoint.Schema() != 2 || len(checkpoint.Bytes()) > 4096 {
		return initial, nil, unavailable(nil)
	}
	var data skillsCleanupCheckpoint
	raw := checkpoint.Bytes()
	if err := json.Unmarshal(raw, &data); err != nil {
		return initial, nil, unavailable(err)
	}
	canonical, err := json.Marshal(data)
	if err != nil || !bytes.Equal(raw, canonical) || data.Project != initial.Project || data.Operation != initial.Operation || data.Version != initial.Version || data.State != c.CleanupPending && data.State != c.CleanupCompleted {
		return initial, nil, unavailable(err)
	}
	if data.Checkpoint == nil {
		return data, nil, nil
	}
	if data.Checkpoint.Schema != 1 || len(data.Checkpoint.Bytes) == 0 || len(data.Checkpoint.Bytes) > 1024 {
		return initial, nil, unavailable(nil)
	}
	child, err := c.NewCleanupCheckpoint(c.SkillsParticipant, cause, scope, data.Checkpoint.Schema, data.Checkpoint.Bytes)
	if err != nil {
		return initial, nil, unavailable(err)
	}
	return data, &child, nil
}

func skillsParentCleanupReport(round lifecycleCleanupRound, child c.CleanupReport) (c.CleanupReport, error) {
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}
	if !child.Matches(c.SkillsParticipant, round.cause, scope) {
		return c.CleanupReport{}, unavailable(nil)
	}
	details := child.Details()
	if len(details.RemainingRefs) != 0 || details.State != c.CleanupPending && details.State != c.CleanupCompleted {
		return c.CleanupReport{}, unavailable(nil)
	}
	data := skillsCleanupCheckpoint{Project: scope.ProjectID.String(), Operation: round.cause.OperationID.String(), Version: round.cause.ProjectVersion, State: details.State}
	if cp := details.Checkpoint; cp != nil {
		if !cp.Matches(c.SkillsParticipant, round.cause, scope) || cp.Schema() != 1 || len(cp.Bytes()) == 0 || len(cp.Bytes()) > 1024 {
			return c.CleanupReport{}, unavailable(nil)
		}
		data.Checkpoint = &skillsDomainCheckpoint{Schema: cp.Schema(), Bytes: cp.Bytes()}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, scope, 2, raw)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	// Agent and Variables have not been cleaned by this provider. Even the
	// final child report can publish only pending progress for their parent.
	return c.NewCleanupReport(c.SkillsParticipant, round.cause, scope, c.CleanupDetails{State: c.CleanupPending, SafeReason: c.ReasonWorkPending, Checkpoint: &cp})
}

func (a *LifecycleAuthority) requireSkillsCleanupClaimInTx(ctx context.Context, tx f.Tx, cause c.LifecycleCause, project c.ProjectID) error {
	if ctx != nil {
		witness, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
		if witness != nil && witness.round.agentStep {
			return fault(f.Forbidden)
		}
	}
	return a.requireCleanupClaimInTx(ctx, tx, cause, project, c.SkillsParticipant)
}
