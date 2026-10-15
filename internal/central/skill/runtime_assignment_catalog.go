package skill

import (
	"context"
	"encoding/json"

	ag "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type assignmentCatalogSkill struct {
	Metadata   sc.Metadata              `json:"metadata"`
	Assignment *sc.AgentSkillAssignment `json:"assignment,omitempty"`
}
type assignmentCatalogAgent struct {
	ID      i.AgentID `json:"agent_id"`
	Name    string    `json:"name"`
	Version f.Version `json:"expected_version"`
}
type assignmentCatalogPosition struct {
	ID      i.AgentID `json:"after_agent_id"`
	Created f.Instant `json:"after_agent_created_at"`
}
type assignmentCatalog struct {
	Skills    []assignmentCatalogSkill   `json:"skills"`
	Agents    []assignmentCatalogAgent   `json:"agents"`
	NextSkill *sc.SkillID                `json:"next_skill_id,omitempty"`
	NextAgent *assignmentCatalogPosition `json:"next_agent,omitempty"`
}

func (s *RuntimeAssignmentCommands) catalog(ctx context.Context, actor i.Actor, project i.ProjectID, run ag.SkillAssignmentRun, args sc.AssignmentToolArguments) ([]byte, bool, error) {
	authority, ok := s.runtime.(ag.SkillAssignmentCatalogRunAuthority)
	owner, ownerOK := s.commands.deps.Agents.(ag.SkillAssignmentCatalogOwner)
	if !ok || !ownerOK || nilPort(authority) || nilPort(owner) {
		return nil, false, fault(f.DependencyUnbound)
	}
	call, err := s.commands.deps.Service.begin(ctx, false)
	if err != nil {
		return nil, false, err
	}
	defer s.commands.deps.Service.end(call)
	plan, err := s.runtime.DiscoverSkillAssignmentRun(call.ctx, actor, project, run)
	if err != nil {
		return nil, false, err
	}
	r := ag.SkillAssignmentCatalogRequest{Actor: actor, ProjectID: project, Run: run, Plan: plan, AfterID: args.AfterAgentID, AfterCreated: args.AfterAgentCreated, Limit: args.Limit}
	if args.AgentID.Validate() == nil {
		r.Target = &args.AgentID
	}
	if r.Validate() != nil {
		return nil, false, fault(f.Forbidden)
	}
	locks, err := ob.NormalizeLocks(r.RequiredLocks())
	if err != nil {
		return nil, false, err
	}
	cause, err := f.NewJobCause("skill-assignment-catalog", project.String(), run.OperationID)
	if err != nil {
		return nil, false, err
	}
	store := s.commands.deps.Service.state().authority.state().store
	out := assignmentCatalog{Skills: []assignmentCatalogSkill{}, Agents: []assignmentCatalogAgent{}}
	var callbackErr error
	result := store.WithinTx(call.ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		// Agent validates same Store/Tx and the original private Tool source before
		// either domain returns metadata. The target's version remains a CAS input.
		agents, err := owner.ReadSkillAssignmentCatalogInTx(ctx, tx, r)
		if err != nil {
			return captureError(err)
		}
		if len(agents) > r.Limit {
			tail := agents[r.Limit-1]
			out.NextAgent = &assignmentCatalogPosition{tail.ID, tail.CreatedAt}
			agents = agents[:r.Limit]
		}
		for _, a := range agents {
			out.Agents = append(out.Agents, assignmentCatalogAgent{a.ID, a.Name, a.Version})
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		var after any
		if args.AfterSkillID != nil {
			after = args.AfterSkillID.String()
		}
		rows, err := x.Query(ctx, `SELECT id::text FROM agenteam_skill.skills WHERE project_id=$1 AND serving AND ($2::uuid IS NULL OR id>$2::uuid) ORDER BY id LIMIT $3`, project.String(), after, r.Limit+1)
		if err != nil {
			return portError(err)
		}
		ids := []sc.SkillID{}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return portError(err)
			}
			value, e := f.ParseID[pc.Skill](raw)
			if e != nil {
				rows.Close()
				return unavailable(e)
			}
			ids = append(ids, value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return portError(err)
		}
		if len(ids) > r.Limit {
			tail := ids[r.Limit-1]
			out.NextSkill = &tail
			ids = ids[:r.Limit]
		}
		assignments := map[sc.SkillID]sc.AgentSkillAssignment{}
		if r.Target != nil {
			set, e := loadAssignmentSet(ctx, x, project, *r.Target)
			if e != nil {
				return e
			}
			for _, item := range set.Items {
				assignments[item.SkillID] = item
			}
		}
		for _, id := range ids {
			metadata, _, e := loadAssignmentRevision(ctx, x, project, id)
			if e != nil {
				return e
			}
			item := assignmentCatalogSkill{Metadata: metadata}
			if assignment, ok := assignments[id]; ok {
				copy := assignment
				item.Assignment = &copy
			}
			out.Skills = append(out.Skills, item)
		}
		// No read result may survive current Tool/Execution permission revocation.
		return authority.RequireSkillAssignmentCatalogInTx(ctx, tx, r)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return nil, false, callbackErr
	}
	if err = commitError(result); err != nil {
		return nil, false, err
	}
	if err = call.ctx.Err(); err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, false, unavailable(err)
	}
	if len(raw) > 1<<20 {
		return nil, false, fault(f.ResourceBusy)
	}
	return raw, true, nil
}
