package skill

import (
	"bytes"
	"context"
	"slices"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type retirementGrant struct {
	value   sc.AgentSkillAssignment
	change  string
	removed *time.Time
}
type assignmentRetirementHistory struct {
	head     *agentInitializationRecord
	commands []*assignmentCommand
	changes  []*assignmentCommand
	grants   map[string]retirementGrant
	ingress  []sc.ProjectAgentIngressRetirementEntry
	current  assignmentSet
}

// Only small IDs are collected together. Each original command/revision body
// goes through the existing bounded canonical decoder, never a JSON aggregate.
func retirementHistoryIDs(ctx context.Context, x postgres.SQLExecutor, table, agent string) ([]string, error) {
	var ids []string
	err := x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id),'{}'::text[]) FROM agenteam_skill.`+table+` WHERE agent_id=$1`, agent).Scan(&ids)
	if err != nil {
		return nil, unavailable(err)
	}
	return ids, portError(ctx.Err())
}

func loadAssignmentRetirementHistory(ctx context.Context, x postgres.SQLExecutor, change ac.ProjectRetirementChange) (*assignmentRetirementHistory, error) {
	r := change.Request
	var blocked bool
	err := x.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_skill.execution_binding_heads WHERE agent_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_skill.execution_bindings WHERE agent_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs WHERE agent_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_commands WHERE agent_id=$1 AND (project_id<>$2 OR state<>'completed'))
 OR EXISTS(SELECT 1 FROM agenteam_skill.agent_assignment_heads WHERE agent_id=$1 AND project_id<>$2)
 OR EXISTS(SELECT 1 FROM agenteam_skill.agent_assignments WHERE agent_id=$1 AND project_id<>$2)
 OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_changes WHERE agent_id=$1 AND project_id<>$2)
 OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_current WHERE agent_id=$1 AND project_id<>$2)`, r.AgentID.String(), r.ProjectID.String()).Scan(&blocked)
	if err != nil {
		return nil, unavailable(err)
	}
	if blocked {
		return nil, fault(f.ResourceBusy)
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	head, err := loadAgentInitialization(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return nil, err
	}
	if head == nil || head.sequence != 1 {
		return nil, fault(f.ResourceBusy)
	}
	current, err := loadAssignmentSet(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return nil, err
	}
	h := &assignmentRetirementHistory{head: head, current: current, grants: map[string]retirementGrant{}}
	ids, err := retirementHistoryIDs(ctx, x, "assignment_commands", r.AgentID.String())
	if err != nil {
		return nil, err
	}
	for _, raw := range ids {
		id, err := f.ParseID[sc.AssignmentChange](raw)
		if err != nil {
			return nil, unavailable(err)
		}
		command, err := loadAssignmentByID(ctx, x, r.ProjectID, r.AgentID, id)
		if err != nil {
			return nil, err
		}
		if command.State != "completed" || command.Candidate.Version > r.ExpectedVersion {
			return nil, fault(f.ResourceBusy)
		}
		h.commands = append(h.commands, command)
		if command.Candidate.Changed {
			h.changes = append(h.changes, command)
			if err = verifyRetirementChange(ctx, x, command); err != nil {
				return nil, err
			}
		}
		if command.Candidate.Runtime != nil {
			receipt, err := command.runtimeReceipt()
			if err != nil {
				return nil, err
			}
			if receipt == nil || receipt.Queued || receipt.ExecutionID != nil {
				return nil, fault(f.ResourceBusy)
			}
			entry := sc.ProjectAgentIngressRetirementEntry{Change: *command.Candidate.Runtime, Receipt: receipt.Clone()}
			if entry.Validate() != nil {
				return nil, unavailable(nil)
			}
			h.ingress = append(h.ingress, entry)
		}
	}
	if err = h.verifyChain(); err != nil {
		return nil, err
	}
	changedIDs, err := retirementHistoryIDs(ctx, x, "assignment_changes", r.AgentID.String())
	if err != nil {
		return nil, err
	}
	expectedIDs := make([]string, len(h.changes))
	for n, command := range h.changes {
		expectedIDs[n] = command.ID.String()
	}
	slices.Sort(expectedIDs)
	if !slices.Equal(changedIDs, expectedIDs) {
		return nil, fault(f.ResourceBusy)
	}
	grantIDs, err := retirementHistoryIDs(ctx, x, "agent_assignments", r.AgentID.String())
	if err != nil {
		return nil, err
	}
	if len(grantIDs) != len(h.grants) {
		return nil, fault(f.ResourceBusy)
	}
	for _, id := range grantIDs {
		grant, ok := h.grants[id]
		if !ok {
			return nil, fault(f.ResourceBusy)
		}
		if err = verifyRetirementGrant(ctx, x, grant); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(h.ingress, func(a, b sc.ProjectAgentIngressRetirementEntry) int {
		return bytes.Compare([]byte(a.Change.ID.String()), []byte(b.Change.ID.String()))
	})
	return h, portError(ctx.Err())
}

func verifyRetirementChange(ctx context.Context, x postgres.SQLExecutor, r *assignmentCommand) error {
	c := r.Candidate
	var project, agent, previous, digest string
	var sequence, version int64
	var state, runtime []byte
	var created time.Time
	err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,assignment_sequence,agent_version,previous_digest,state_data,state_digest,runtime_data,created_at FROM agenteam_skill.assignment_changes WHERE id=$1`, r.ID.String()).Scan(&project, &agent, &sequence, &version, &previous, &state, &digest, &runtime, &created)
	if err != nil {
		return unavailable(err)
	}
	expectedDigest, expectedState, err := captureDigest(c.After)
	if err != nil {
		return err
	}
	var expectedRuntime []byte
	if c.Runtime != nil {
		_, expectedRuntime, err = captureDigest(c.Runtime)
		if err != nil {
			return err
		}
	}
	if project != c.After.Project.String() || agent != c.After.Agent.String() || sequence != int64(c.After.Sequence) || version != int64(c.Version) || previous != string(c.BeforeDigest) || digest != string(expectedDigest) || !bytes.Equal(state, expectedState) || !bytes.Equal(runtime, expectedRuntime) || !created.Equal(c.Created.Time()) {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

// Verify the complete immutable chain and all grants, including removed and
// disabled history. A valid latest current row cannot hide extra old facts.
func (h *assignmentRetirementHistory) verifyChain() error {
	state := assignmentSet{Project: h.head.project, Agent: h.head.agent, Sequence: 1, Items: []sc.AgentSkillAssignment{}}
	if h.head.assignment != nil {
		a := sc.AgentSkillAssignment{ID: *h.head.assignment, ProjectID: h.head.project, AgentID: h.head.agent, SkillID: h.head.skill, Enabled: true, Sequence: 1, CreatedAt: h.head.created}
		state.Items = append(state.Items, a)
		h.grants[a.ID.String()] = retirementGrant{value: a}
	}
	digest, _, err := captureDigest(state)
	if err != nil {
		return err
	}
	states := map[f.Version]f.Digest{1: digest}
	slices.SortFunc(h.changes, func(a, b *assignmentCommand) int {
		if a.Candidate.After.Sequence < b.Candidate.After.Sequence {
			return -1
		}
		if a.Candidate.After.Sequence > b.Candidate.After.Sequence {
			return 1
		}
		return 0
	})
	version := f.Version(1)
	for _, r := range h.changes {
		c := r.Candidate
		if c.After.Sequence != state.Sequence+1 || c.BeforeDigest != digest || c.Version <= version {
			return fault(f.ResourceBusy)
		}
		if c.Removed != nil {
			grant, ok := h.grants[c.Removed.ID.String()]
			if !ok || grant.removed != nil || grant.value != *c.Removed {
				return fault(f.ResourceBusy)
			}
			at := c.Created.Time()
			grant.removed = &at
			h.grants[c.Removed.ID.String()] = grant
		}
		if c.Added != nil {
			if _, ok := h.grants[c.Added.ID.String()]; ok {
				return fault(f.ResourceBusy)
			}
			h.grants[c.Added.ID.String()] = retirementGrant{value: *c.Added, change: r.ID.String()}
		}
		state, version = c.After, c.Version
		digest, _, err = captureDigest(state)
		if err != nil {
			return err
		}
		states[state.Sequence] = digest
	}
	actual, _, err := captureDigest(h.current)
	if err != nil || actual != digest {
		return fault(f.ResourceBusy)
	}
	for _, r := range h.commands {
		if !r.Candidate.Changed {
			d, _, err := captureDigest(r.Candidate.After)
			if err != nil || d != states[r.Candidate.After.Sequence] {
				return fault(f.ResourceBusy)
			}
		}
	}
	return nil
}

func verifyRetirementGrant(ctx context.Context, x postgres.SQLExecutor, g retirementGrant) error {
	var project, agent, skill, change string
	var sequence int64
	var enabled bool
	var created time.Time
	var removed *time.Time
	err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,skill_id::text,enabled,assignment_sequence,created_at,removed_at,COALESCE(change_id::text,'') FROM agenteam_skill.agent_assignments WHERE id=$1`, g.value.ID.String()).Scan(&project, &agent, &skill, &enabled, &sequence, &created, &removed, &change)
	if err != nil {
		return unavailable(err)
	}
	a := g.value
	if project != a.ProjectID.String() || agent != a.AgentID.String() || skill != a.SkillID.String() || enabled != a.Enabled || sequence != int64(a.Sequence) || !created.Equal(a.CreatedAt.Time()) || change != g.change || (removed == nil) != (g.removed == nil) || removed != nil && !removed.Equal(*g.removed) {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

func (s *ProjectAgentRetirementReferences) retireAssignmentHistory(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, change ac.ProjectRetirementChange) error {
	h, err := loadAssignmentRetirementHistory(ctx, x, change)
	if err != nil {
		return err
	}
	if err = s.ingress.RetireProjectAgentIngressInTx(ctx, tx, change.Clone(), h.ingress); err != nil {
		return portError(err)
	}
	if err = ctx.Err(); err != nil {
		return portError(err)
	}
	return deleteAssignmentRetirementHistory(ctx, x, change, h)
}

func deleteAssignmentRetirementHistory(ctx context.Context, x postgres.SQLExecutor, change ac.ProjectRetirementChange, h *assignmentRetirementHistory) error {
	currentCount := int64(0)
	if h.current.Sequence > 1 {
		currentCount = 1
	}
	for _, step := range []struct {
		table  string
		count  int64
		suffix string
	}{
		{"assignment_current", currentCount, ""},
		{"agent_assignments", int64(len(h.grants)), ""},
		{"assignment_changes", int64(len(h.changes)), ""},
		{"assignment_commands", int64(len(h.commands)), " AND state='completed'"},
	} {
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_skill.`+step.table+` WHERE project_id=$1 AND agent_id=$2`+step.suffix, change.Request.ProjectID.String(), change.Request.AgentID.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != step.count {
			return fault(f.ResourceBusy)
		}
		if err = ctx.Err(); err != nil {
			return portError(err)
		}
	}
	var assignment any
	if h.head.assignment != nil {
		assignment = h.head.assignment.String()
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_skill.agent_assignment_heads WHERE project_id=$1 AND agent_id=$2 AND creation_command=$3 AND plan_revision=$4 AND request_binding=$5 AND add_skills_enabled=$6 AND assignment_sequence=1 AND initial_assignment_id IS NOT DISTINCT FROM $7::uuid`, change.Request.ProjectID.String(), change.Request.AgentID.String(), h.head.command, int64(h.head.planRevision), string(h.head.binding), h.head.enabled, assignment)
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
