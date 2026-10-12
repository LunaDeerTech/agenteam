package project

import (
	"bytes"
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// NewLifecycleCleanupDriverWithVariables consumes three fixed substeps, one
// per Run. The Variables provider must be constructed from the original owned
// pair and Project authority. Completing metadata cleanup retains the Agent
// recovery anchor and leaves the combined participant pending in domains.
func NewLifecycleCleanupDriverWithVariables(store Store, projects *Authority, processes oc.ProcessAuthority, agents ac.ProjectRetirementConsumer, skills SkillsCleanupDomain, variables pvc.ProjectCleanupDomain) (*LifecycleCleanupDriver, error) {
	if nilPort(variables) {
		return nil, fault(f.DependencyUnbound)
	}
	driver, err := NewLifecycleCleanupDriverWithAgents(store, projects, processes, agents, skills)
	if err != nil {
		return nil, err
	}
	driver.state.variables = variables
	return driver, nil
}

// The complete preceding checkpoint is preserved byte-for-byte. It includes
// the last Agent's immutable-command anchor, which this substep cannot retire.
// Variables scans each bounded batch from its own current rows and has no
// provider checkpoint or cursor. An empty checkpoint never means completion.
type variablesCleanupCheckpoint struct {
	Project      string         `json:"project_id"`
	Operation    string         `json:"operation_id"`
	Version      f.Version      `json:"project_version"`
	AgentsSchema f.Version      `json:"agents_schema"`
	AgentsBytes  []byte         `json:"agents_checkpoint"`
	State        c.CleanupState `json:"variables_state"`
}

func decodeVariablesCleanupCheckpoint(cp *c.CleanupCheckpoint, cause c.LifecycleCause, project c.ProjectID) (variablesCleanupCheckpoint, agentsCleanupCheckpoint, error) {
	data := variablesCleanupCheckpoint{Project: project.String(), Operation: cause.OperationID.String(), Version: cause.ProjectVersion, State: c.CleanupPending}
	var agents agentsCleanupCheckpoint
	if cp == nil {
		return data, agents, fault(f.InvalidState)
	}
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	if !cp.Matches(c.SkillsParticipant, cause, scope) {
		return data, agents, unavailable(nil)
	}
	if cp.Schema() == 5 {
		raw := cp.Bytes()
		if len(raw) > 16384 {
			return data, agents, unavailable(nil)
		}
		var saved variablesCleanupCheckpoint
		if err := json.Unmarshal(raw, &saved); err != nil {
			return data, agents, unavailable(err)
		}
		canonical, err := json.Marshal(saved)
		if err != nil || !bytes.Equal(raw, canonical) || saved.Project != data.Project || saved.Operation != data.Operation || saved.Version != data.Version || saved.State != c.CleanupPending && saved.State != c.CleanupCompleted {
			return data, agents, unavailable(err)
		}
		data = saved
	} else {
		data.AgentsSchema, data.AgentsBytes = cp.Schema(), cp.Bytes()
	}
	if data.AgentsSchema != 3 && data.AgentsSchema != 4 || len(data.AgentsBytes) == 0 || len(data.AgentsBytes) > 8192 {
		return data, agents, unavailable(nil)
	}
	previous, err := c.NewCleanupCheckpoint(c.SkillsParticipant, cause, scope, data.AgentsSchema, data.AgentsBytes)
	if err != nil {
		return data, agents, unavailable(err)
	}
	agents, err = decodeAgentsCleanupCheckpoint(&previous, cause, project)
	if err != nil {
		return data, agents, err
	}
	if !agents.AgentsDone || agents.Skills.State != c.CleanupCompleted {
		return data, agents, fault(f.InvalidState)
	}
	return data, agents, nil
}

func (st *lifecycleCleanupState) agentsProgress(cp *c.CleanupCheckpoint, cause c.LifecycleCause, project c.ProjectID) (agentsCleanupCheckpoint, error) {
	if cp != nil && cp.Schema() == 5 && !nilPort(st.variables) {
		_, agents, err := decodeVariablesCleanupCheckpoint(cp, cause, project)
		return agents, err
	}
	return decodeAgentsCleanupCheckpoint(cp, cause, project)
}

// Selection reads only canonical progress from start's original transaction.
// It never chains the next provider into a preceding provider's return path.
func (st *lifecycleCleanupState) selectDomainStep(cp *c.CleanupCheckpoint, cause c.LifecycleCause, project c.ProjectID) (agent, variables, replay bool, err error) {
	skills, _, err := st.skillsProgress(cp, cause, project)
	if err != nil {
		return false, false, false, err
	}
	if st.agents != nil {
		progress, err := st.agentsProgress(cp, cause, project)
		if err != nil {
			return false, false, false, err
		}
		agent = !progress.AgentsDone
	}
	if agent || skills.State != c.CleanupCompleted {
		return agent, false, false, nil
	}
	if nilPort(st.variables) {
		return false, false, true, nil
	}
	progress, _, err := decodeVariablesCleanupCheckpoint(cp, cause, project)
	if err != nil {
		return false, false, false, err
	}
	return false, progress.State != c.CleanupCompleted, progress.State == c.CleanupCompleted, nil
}

func variablesParentCleanupReport(round lifecycleCleanupRound, child c.CleanupReport) (c.CleanupReport, error) {
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}
	if !child.Matches(c.SkillsParticipant, round.cause, scope) {
		return c.CleanupReport{}, unavailable(nil)
	}
	details := child.Details()
	if len(details.RemainingRefs) != 0 || details.Checkpoint != nil || details.State != c.CleanupPending && details.State != c.CleanupCompleted {
		return c.CleanupReport{}, unavailable(nil)
	}
	data, _, err := decodeVariablesCleanupCheckpoint(round.checkpoint, round.cause, round.claim.project)
	if err != nil {
		return c.CleanupReport{}, err
	}
	data.State = details.State
	raw, err := json.Marshal(data)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, scope, 5, raw)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	if _, _, err = decodeVariablesCleanupCheckpoint(&cp, round.cause, round.claim.project); err != nil {
		return c.CleanupReport{}, err
	}
	return c.NewCleanupReport(c.SkillsParticipant, round.cause, scope, c.CleanupDetails{State: c.CleanupPending, SafeReason: c.ReasonWorkPending, Checkpoint: &cp})
}

func (a *LifecycleAuthority) requireVariablesCleanupClaimInTx(ctx context.Context, tx f.Tx, cause c.LifecycleCause, project c.ProjectID) error {
	if ctx == nil {
		return invalid()
	}
	witness, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if witness == nil || witness.state == nil || nilPort(witness.state.variables) || !witness.round.variablesStep || witness.round.agentStep || witness.round.terminalStep {
		return fault(f.Forbidden)
	}
	return a.requireCleanupClaimInTx(ctx, tx, cause, project, c.SkillsParticipant)
}
