package project

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This fixed constructor consumes Agent retirement before the Skills substep.
// Variables remain unimplemented, so even both substeps completing leaves the
// combined participant pending/domains. It is not a complete Delete registry.
func NewLifecycleCleanupDriverWithAgents(store Store, projects *Authority, processes oc.ProcessAuthority, agents ac.ProjectRetirementConsumer, skills SkillsCleanupDomain) (*LifecycleCleanupDriver, error) {
	if nilPort(agents) {
		return nil, fault(f.DependencyUnbound)
	}
	driver, err := NewLifecycleCleanupDriverForSkills(store, projects, processes, skills)
	if err != nil {
		return nil, err
	}
	driver.state.agents = agents
	return driver, nil
}

type agentCleanupApplied struct {
	Agent           identity.AgentID  `json:"agent_id"`
	Version         f.Version         `json:"version"`
	Command         ac.AgentCommandID `json:"command_id"`
	Digest          f.Digest          `json:"before_digest"`
	BeforeUpdatedAt *f.Instant        `json:"before_updated_at,omitempty"`
}

func agentCleanupAnchor(plan ac.ProjectRetirementPlan) agentCleanupApplied {
	plan = plan.Clone()
	return agentCleanupApplied{Agent: plan.Change.Request.AgentID, Version: plan.Change.Request.ExpectedVersion, Command: plan.CommandID, Digest: plan.BeforeDigest, BeforeUpdatedAt: plan.BeforeUpdatedAt}
}
func sameAgentCleanupAnchor(a, b agentCleanupApplied) bool {
	return a.Agent == b.Agent && a.Version == b.Version && a.Command == b.Command && a.Digest == b.Digest &&
		(a.BeforeUpdatedAt == nil && b.BeforeUpdatedAt == nil || a.BeforeUpdatedAt != nil && b.BeforeUpdatedAt != nil && *a.BeforeUpdatedAt == *b.BeforeUpdatedAt)
}

type agentsCleanupCheckpoint struct {
	Project    string                  `json:"project_id"`
	Operation  string                  `json:"operation_id"`
	Version    f.Version               `json:"project_version"`
	AgentsDone bool                    `json:"agents_done"`
	Applied    *agentCleanupApplied    `json:"agent_applied,omitempty"`
	Skills     skillsCleanupCheckpoint `json:"skills_progress"`
}

func decodeAgentsCleanupCheckpoint(cp *c.CleanupCheckpoint, cause c.LifecycleCause, p c.ProjectID) (agentsCleanupCheckpoint, error) {
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: p}
	initialSkills, _, err := decodeSkillsCleanupCheckpoint(nil, cause, scope)
	initial := agentsCleanupCheckpoint{Project: p.String(), Operation: cause.OperationID.String(), Version: cause.ProjectVersion, Skills: initialSkills}
	if err != nil {
		return initial, err
	}
	if cp == nil {
		return initial, nil
	}
	if !cp.Matches(c.SkillsParticipant, cause, scope) || cp.Schema() != 3 && cp.Schema() != 4 || len(cp.Bytes()) > 8192 {
		return initial, unavailable(nil)
	}
	var data agentsCleanupCheckpoint
	raw := cp.Bytes()
	if err = json.Unmarshal(raw, &data); err != nil {
		return initial, unavailable(err)
	}
	canonical, err := json.Marshal(data)
	if err != nil || !bytes.Equal(raw, canonical) || data.Project != initial.Project || data.Operation != initial.Operation || data.Version != initial.Version {
		return initial, unavailable(err)
	}
	if a := data.Applied; a != nil && (a.Agent.Validate() != nil || a.Version.Validate() != nil || a.Command.Validate() != nil || a.Digest.Validate() != nil) {
		return initial, unavailable(nil)
	}
	if cp.Schema() == 4 {
		if data.Applied == nil || data.Applied.BeforeUpdatedAt == nil || data.Applied.BeforeUpdatedAt.Validate() != nil {
			return initial, unavailable(nil)
		}
	} else if data.Applied != nil && data.Applied.BeforeUpdatedAt != nil {
		return initial, unavailable(nil)
	}
	child, err := data.skillsCheckpoint(cause, p)
	if err != nil {
		return initial, err
	}
	if _, _, err = decodeSkillsCleanupCheckpoint(child, cause, scope); err != nil {
		return initial, err
	}
	if !data.AgentsDone && (data.Skills.State != c.CleanupPending || data.Skills.Checkpoint != nil) {
		return initial, unavailable(nil)
	}
	return data, nil
}
func (data agentsCleanupCheckpoint) skillsCheckpoint(cause c.LifecycleCause, p c.ProjectID) (*c.CleanupCheckpoint, error) {
	raw, err := json.Marshal(data.Skills)
	if err != nil {
		return nil, unavailable(err)
	}
	cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: p}, 2, raw)
	if err != nil {
		return nil, unavailable(err)
	}
	return &cp, nil
}
func (st *lifecycleCleanupState) skillsProgress(cp *c.CleanupCheckpoint, cause c.LifecycleCause, p c.ProjectID) (skillsCleanupCheckpoint, *c.CleanupCheckpoint, error) {
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: p}
	if st.agents == nil {
		return decodeSkillsCleanupCheckpoint(cp, cause, scope)
	}
	data, err := st.agentsProgress(cp, cause, p)
	if err != nil {
		return skillsCleanupCheckpoint{}, nil, err
	}
	child, err := data.skillsCheckpoint(cause, p)
	if err != nil {
		return skillsCleanupCheckpoint{}, nil, err
	}
	return decodeSkillsCleanupCheckpoint(child, cause, scope)
}
func agentsCleanupReport(round lifecycleCleanupRound, data agentsCleanupCheckpoint) (c.CleanupReport, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}
	schema := f.Version(3)
	if data.Applied != nil && data.Applied.BeforeUpdatedAt != nil {
		schema = 4
	}
	cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, scope, schema, raw)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	if _, err = decodeAgentsCleanupCheckpoint(&cp, round.cause, round.claim.project); err != nil {
		return c.CleanupReport{}, err
	}
	return c.NewCleanupReport(c.SkillsParticipant, round.cause, scope, c.CleanupDetails{State: c.CleanupPending, SafeReason: c.ReasonWorkPending, Checkpoint: &cp})
}
func agentsSkillsCleanupReport(round lifecycleCleanupRound, report c.CleanupReport) (c.CleanupReport, error) {
	data, err := decodeAgentsCleanupCheckpoint(round.checkpoint, round.cause, round.claim.project)
	if err != nil {
		return c.CleanupReport{}, err
	}
	if !data.AgentsDone {
		return c.CleanupReport{}, fault(f.InvalidState)
	}
	data.Skills, _, err = decodeSkillsCleanupCheckpoint(report.Details().Checkpoint, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project})
	if err != nil {
		return c.CleanupReport{}, err
	}
	return agentsCleanupReport(round, data)
}

func (a *LifecycleAuthority) requireAgentCleanupStepInTx(ctx context.Context, tx f.Tx, cause c.LifecycleCause, p c.ProjectID) error {
	if ctx == nil {
		return invalid()
	}
	witness, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if witness == nil || witness.state == nil || witness.state.agents == nil || !witness.round.agentStep || witness.round.variablesStep {
		return fault(f.Forbidden)
	}
	return a.requireCleanupClaimInTx(ctx, tx, cause, p, c.SkillsParticipant)
}
func (a *LifecycleAuthority) requireAgentCleanupAppliedInTx(ctx context.Context, tx f.Tx, plan ac.ProjectRetirementPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	req := plan.Change.Request
	if err := a.requireAgentCleanupStepInTx(ctx, tx, req.Cause, req.ProjectID); err != nil {
		return err
	}
	witness, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	witness.mu.Lock()
	defer witness.mu.Unlock()
	if !witness.open {
		return fault(f.Forbidden)
	}
	x, err := a.lifecycleExecutor(ctx, tx, req.ProjectID)
	if err != nil {
		return err
	}
	_, _, cp, err := cleanupParticipantRow(ctx, x, witness.round)
	if err != nil {
		return err
	}
	data, err := decodeAgentsCleanupCheckpoint(cp, req.Cause, req.ProjectID)
	if err != nil {
		return err
	}
	want := agentCleanupAnchor(plan)
	if data.Applied == nil || !sameAgentCleanupAnchor(*data.Applied, want) {
		return fault(f.Forbidden)
	}
	return portError(ctx.Err())
}

// Discovery and restoration read through this exact active Agent substep.
// The mutation and Project checkpoint then share one original Store attempt.
func (st *lifecycleCleanupState) runAgentStep(ctx context.Context, round lifecycleCleanupRound) (settled bool, err error) {
	run, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	witness := &lifecycleCleanupScope{state: st, round: round, open: true}
	run = context.WithValue(run, lifecycleCleanupScopeKey{}, witness)
	defer func() {
		witness.mu.Lock()
		witness.open = false
		witness.mu.Unlock()
		if recover() != nil {
			settled = false
			err = unavailable(nil)
		}
	}()
	scope, e := identity.InProject(round.claim.project)
	if e != nil {
		return st.agentStepError(ctx, round, invalid())
	}
	actor, e := st.registration.Actor(round.claim.operation.String(), scope)
	if e != nil {
		return st.agentStepError(ctx, round, invalid())
	}
	data, e := decodeAgentsCleanupCheckpoint(round.checkpoint, round.cause, round.claim.project)
	if e != nil {
		return st.agentStepError(ctx, round, e)
	}
	if data.AgentsDone {
		return st.agentStepError(ctx, round, fault(f.InvalidState))
	}
	var previous *ac.ProjectRetirementPlan
	if applied := data.Applied; applied != nil {
		var updatedAt *f.Instant
		if applied.BeforeUpdatedAt != nil {
			at := *applied.BeforeUpdatedAt
			updatedAt = &at
		}
		restored, e := st.agents.RestoreProjectRetirement(run, actor, round.cause, round.claim.project, applied.Agent, applied.Version, applied.Command, applied.Digest, updatedAt)
		if e != nil {
			return st.agentStepError(ctx, round, portError(e))
		}
		if e = agentCleanupPlanMatches(restored, actor, round); e != nil {
			return st.agentStepError(ctx, round, e)
		}
		previous = &restored
	}
	plan, e := st.agents.DiscoverProjectRetirement(run, actor, round.cause, round.claim.project)
	if e != nil {
		return st.agentStepError(ctx, round, portError(e))
	}
	if plan != nil {
		if e = agentCleanupPlanMatches(*plan, actor, round); e != nil {
			return st.agentStepError(ctx, round, e)
		}
	}
	locks := []f.LockRequest{projectLock(round.claim.project, f.Exclusive)}
	for _, p := range []*ac.ProjectRetirementPlan{previous, plan} {
		if p != nil {
			more, e := st.agents.ProjectRetirementLocks(p.Clone())
			if e != nil {
				return st.agentStepError(ctx, round, portError(e))
			}
			locks = append(locks, more...)
		}
	}
	locks, e = oc.NormalizeLocks(locks)
	if e != nil || len(locks) > 1024 {
		return st.agentStepError(ctx, round, invalid())
	}
	validated := false
	result := st.store.WithinTx(run, round.claim.cause(), func(txctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(txctx, tx, locks); e != nil {
			return portError(e)
		}
		if previous != nil {
			if e := st.agents.RequireProjectAgentRetiredInTx(txctx, tx, previous.Clone()); e != nil {
				return portError(e)
			}
		}
		if plan == nil {
			if e := st.agents.RequireProjectAgentsEmptyInTx(txctx, tx, actor, round.cause, round.claim.project); e != nil {
				return portError(e)
			}
			data.AgentsDone = true
		} else {
			if e := st.agents.RetireProjectAgentInTx(txctx, tx, plan.Clone()); e != nil {
				return portError(e)
			}
			anchor := agentCleanupAnchor(*plan)
			data.Applied = &anchor
		}
		if e := txctx.Err(); e != nil {
			return portError(e)
		}
		report, e := agentsCleanupReport(round, data)
		if e != nil {
			return e
		}
		if e = st.finishInTx(txctx, tx, round, report, nil); e != nil {
			return e
		}
		validated = true
		return nil
	})
	if e = commitError(result); e != nil {
		// Only a known rollback may terminate the claim separately. Unknown keeps
		// this original owner and its atomic applied checkpoint for exact recovery.
		if result.State() == f.NotCommitted {
			return st.agentStepError(ctx, round, e)
		}
		return false, e
	}
	if !validated {
		return false, unavailable(nil)
	}
	if e = run.Err(); e != nil {
		return false, portError(e)
	}
	return true, nil
}
func (st *lifecycleCleanupState) agentStepError(ctx context.Context, round lifecycleCleanupRound, callErr error) (bool, error) {
	checkpoint, end := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer end()
	finishErr := st.finish(checkpoint, round, c.CleanupReport{}, callErr)
	return finishErr == nil, lifecycleStopRoundError(finishErr, callErr)
}

func agentCleanupPlanMatches(plan ac.ProjectRetirementPlan, actor identity.Actor, round lifecycleCleanupRound) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	r := plan.Change.Request
	if !r.Actor.Equal(actor) || r.ProjectID != round.claim.project || r.Cause != round.cause {
		return fault(f.Forbidden)
	}
	return nil
}
