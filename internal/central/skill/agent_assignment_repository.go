package skill

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

type agentInitializationRecord struct {
	project      id.ProjectID
	agent        id.AgentID
	command      string
	planRevision f.Version
	binding      f.Digest
	enabled      bool
	sequence     f.Version
	skill        sc.SkillID
	revision     f.Revision
	assignment   *sc.AssignmentID
	created      f.Instant
}

func (r agentInitializationRecord) receipt() (sc.AgentInitializationReceipt, error) {
	if r.project.Validate() != nil || r.agent.Validate() != nil || r.command == "" || r.planRevision.Validate() != nil ||
		r.binding.Validate() != nil || r.skill.Validate() != nil || r.created.Validate() != nil {
		return sc.AgentInitializationReceipt{}, unavailable(nil)
	}
	out := sc.AgentInitializationReceipt{ProjectID: r.project, AgentID: r.agent, AssignmentSequence: r.sequence,
		AddSkillsEnabled: r.enabled, ObservedRevision: r.revision}
	if r.assignment != nil {
		out.Assignment = &sc.InitialAssignment{ID: *r.assignment, ProjectID: r.project, AgentID: r.agent,
			SkillID: r.skill, Sequence: r.sequence, CreatedAt: r.created}
	}
	if err := out.Validate(); err != nil {
		return sc.AgentInitializationReceipt{}, unavailable(err)
	}
	return out, nil
}

// Reading a matching receipt never replaces the Agent writer's same-Tx
// unpublished-create check. The caller must perform that check on every call.
func loadAgentInitialization(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, agent id.AgentID) (*agentInitializationRecord, error) {
	var p, a, command, binding, skill, assignment string
	var revision, planRevision, sequence, total, initial int64
	var enabled bool
	var created time.Time
	err := x.QueryRow(ctx, `SELECT h.project_id::text,h.agent_id::text,h.creation_command,h.plan_revision,h.request_binding,h.add_skills_enabled,h.assignment_sequence,h.initial_skill_id::text,h.observed_revision,COALESCE(h.initial_assignment_id::text,''),h.created_at,
 (SELECT count(*) FROM agenteam_skill.agent_assignments a WHERE a.project_id=h.project_id AND a.agent_id=h.agent_id),
 (SELECT count(*) FROM agenteam_skill.agent_assignments a WHERE a.project_id=h.project_id AND a.agent_id=h.agent_id AND a.id=h.initial_assignment_id AND a.skill_id=h.initial_skill_id AND a.enabled AND a.assignment_sequence=1 AND a.removed_at IS NULL AND a.created_at=h.created_at)
 FROM agenteam_skill.agent_assignment_heads h WHERE h.project_id=$1 AND h.agent_id=$2`, project.String(), agent.String()).Scan(
		&p, &a, &command, &planRevision, &binding, &enabled, &sequence, &skill, &revision, &assignment, &created, &total, &initial)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if p != project.String() || a != agent.String() || total < 0 || initial < 0 ||
		enabled && (total != 1 || initial != 1) || !enabled && (total != 0 || initial != 0) {
		return nil, unavailable(nil)
	}
	r := &agentInitializationRecord{project: project, agent: agent, command: command, planRevision: f.Version(planRevision),
		binding: f.Digest(binding), enabled: enabled, sequence: f.Version(sequence), revision: f.Revision(revision)}
	r.skill, err = f.ParseID[pc.Skill](skill)
	if err != nil {
		return nil, unavailable(err)
	}
	r.created, err = f.NewInstant(created)
	if err != nil {
		return nil, unavailable(err)
	}
	if assignment != "" {
		v, err := f.ParseID[sc.Assignment](assignment)
		if err != nil {
			return nil, unavailable(err)
		}
		r.assignment = &v
	}
	if _, err = r.receipt(); err != nil {
		return nil, err
	}
	return r, nil
}

func insertAgentInitialization(ctx context.Context, x postgres.SQLExecutor, r agentInitializationRecord) (sc.AgentInitializationReceipt, error) {
	var key any
	if r.enabled {
		v, err := f.NewID[sc.Assignment]()
		if err != nil {
			return sc.AgentInitializationReceipt{}, unavailable(err)
		}
		r.assignment = &v
		key = v.String()
	}
	r.sequence = 1
	var created time.Time
	err := x.QueryRow(ctx, `INSERT INTO agenteam_skill.agent_assignment_heads(project_id,agent_id,creation_command,plan_revision,request_binding,add_skills_enabled,assignment_sequence,initial_skill_id,observed_revision,initial_assignment_id,created_at)
 VALUES($1,$2,$3,$4,$5,$6,1,$7,$8,$9,clock_timestamp()) RETURNING created_at`, r.project.String(), r.agent.String(), r.command, int64(r.planRevision), string(r.binding), r.enabled, r.skill.String(), int64(r.revision), key).Scan(&created)
	if err != nil {
		return sc.AgentInitializationReceipt{}, unavailable(err)
	}
	r.created, err = f.NewInstant(created)
	if err != nil {
		return sc.AgentInitializationReceipt{}, unavailable(err)
	}
	if r.assignment != nil {
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_skill.agent_assignments(id,project_id,agent_id,skill_id,enabled,assignment_sequence,created_at,removed_at) VALUES($1,$2,$3,$4,true,1,$5,NULL)`, r.assignment.String(), r.project.String(), r.agent.String(), r.skill.String(), created)
		if err != nil {
			return sc.AgentInitializationReceipt{}, unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return sc.AgentInitializationReceipt{}, unavailable(nil)
		}
	}
	return r.receipt()
}

// Even a disabled initial set retains creation facts. There is no Agent
// retirement provider in this slice, so cleanup cannot consume those facts.
func agentAssignmentsEmpty(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID) (bool, error) {
	var empty bool
	err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.agent_assignment_heads WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_skill.agent_assignments WHERE project_id=$1)`, project.String()).Scan(&empty)
	return empty, portError(err)
}
