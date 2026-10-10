package agent

import (
	"context"
	"math"
	"slices"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func commandTime(ctx context.Context, x postgres.SQLExecutor) (f.Instant, error) {
	var at time.Time
	if err := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return f.Instant{}, unavailable(err)
	}
	v, err := f.NewInstant(at)
	if err != nil {
		return f.Instant{}, unavailable(err)
	}
	return v, nil
}

func commandReceipt(r *commandRecord) (c.AgentMutation, error) {
	if r == nil || r.validate() != nil || r.State != "completed" {
		return c.AgentMutation{}, unavailable(nil)
	}
	out, err := c.DecodeAgentMutation(r.Receipt)
	if err != nil {
		return c.AgentMutation{}, unavailable(err)
	}
	d := out.Fields()
	want := []ec.EventID{}
	if len(r.ChangedFields) > 0 {
		id, err := f.ParseID[ec.EventIdentity](r.ID.String())
		if err != nil {
			return c.AgentMutation{}, unavailable(err)
		}
		want = append(want, id)
	}
	if !sameValue(d.Agent, r.After) || d.Changed != (len(r.ChangedFields) > 0) || !slices.Equal(d.EventIDs, want) {
		return c.AgentMutation{}, unavailable(nil)
	}
	return out, nil
}

func checkPreimage(ctx context.Context, x postgres.SQLExecutor, r *commandRecord) error {
	actual, err := loadAgent(ctx, x, r.Project, r.Target)
	if err != nil {
		return err
	}
	if !sameValue(actual, r.Before) {
		return fault(f.VersionConflict)
	}
	if actual == nil {
		// Agent IDs are global. Check only occupancy, never disclose the other
		// Project's canonical configuration.
		var occupied bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_agent.agents WHERE id=$1)`, r.Target.String()).Scan(&occupied); err != nil {
			return unavailable(err)
		}
		if occupied {
			return targetOccupied()
		}
	}
	return nil
}

func insertCommand(ctx context.Context, x postgres.SQLExecutor, r *commandRecord) error {
	if err := r.validate(); err != nil {
		return err
	}
	after, err := canonical(r.After)
	if err != nil {
		return err
	}
	var before any
	if r.Before != nil {
		raw, err := canonical(*r.Before)
		if err != nil {
			return err
		}
		before = raw
	}
	fields, err := canonical(r.ChangedFields)
	if err != nil {
		return err
	}
	var expected any
	if r.Expected != nil {
		expected = int64(*r.Expected)
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_agent.commands(id,project_id,actor_user_id,target_id,command_name,idempotency_key,semantic_digest,input,expected_version,plan_revision,before_config,after_config,add_skills_enabled,install_skill_enabled,changed_fields,state,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'planned',$16)`, r.ID.String(), r.Project.String(), r.User.String(), r.Target.String(), string(r.Name), string(r.Key), string(r.Semantic), []byte(r.Input), expected, int64(r.Revision), before, after, r.AddSkillsEnabled, r.InstallSkillEnabled, fields, r.Created.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return nil
}

func completeCommand(ctx context.Context, x postgres.SQLExecutor, r *commandRecord, out c.AgentMutation) error {
	raw, err := canonical(out)
	if err != nil {
		return err
	}
	at, err := commandTime(ctx, x)
	if err != nil {
		return err
	}
	if at.Time().Before(r.Created.Time()) {
		at = r.Created
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_agent.commands SET state='completed',receipt=$4,committed_at=$5 WHERE project_id=$1 AND id=$2 AND plan_revision=$3 AND state='planned'`, r.Project.String(), r.ID.String(), int64(r.Revision), raw, at.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return nil
}

// currentCommand gives completed replay priority over new-write checks. It
// never lets a different writer learn whether the same key has other meaning.
func (s *Service) currentCommand(ctx context.Context, tx f.Tx, in commandInput) (postgres.SQLExecutor, *commandRecord, *c.AgentMutation, error) {
	st := s.state
	grant, err := st.deps.Authority.state.projects.RequireOwnerInTx(ctx, tx, in.actor, in.project, i.Read)
	if err != nil {
		return nil, nil, nil, portError(err)
	}
	if !grant.Matches(in.actor, in.project) {
		return nil, nil, nil, fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return nil, nil, nil, portError(err)
	}
	r, err := loadCommand(ctx, x, in.project, in.name, in.meta.IdempotencyKey)
	if err != nil {
		return nil, nil, nil, err
	}
	if r == nil {
		return x, nil, nil, nil
	}
	if r.User.String() != in.actor.Details().UserID {
		return nil, nil, nil, fault(f.NotFound)
	}
	if r.Semantic != in.semantic {
		return nil, nil, nil, fault(f.IdempotencyKeyReused)
	}
	if r.Target != in.target || !sameValue(r.Input, in.raw) || !sameValue(r.Expected, in.meta.ExpectedVersion) {
		return nil, nil, nil, unavailable(nil)
	}
	if r.State == "completed" {
		out, err := commandReceipt(r)
		return x, r, &out, err
	}
	return x, r, nil, nil
}

func (s *Service) requireMutation(ctx context.Context, tx f.Tx, in commandInput) error {
	grant, err := s.state.deps.Authority.state.projects.RequireOwnerInTx(ctx, tx, in.actor, in.project, i.Mutate)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(in.actor, in.project) {
		return fault(f.Forbidden)
	}
	return nil
}

func (s *Service) prepareCommand(ctx context.Context, in commandInput) (*commandRecord, *c.AgentMutation, f.CommitResult) {
	var planned *commandRecord
	var replay *c.AgentMutation
	cause, _ := f.NewCommandsCause(in.identity)
	result := s.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state.store.AcquireAll(ctx, tx, in.locks()); err != nil {
			return portError(err)
		}
		x, r, out, err := s.currentCommand(ctx, tx, in)
		if err != nil {
			return err
		}
		if out != nil {
			replay = out
			return nil
		}
		if err = s.requireMutation(ctx, tx, in); err != nil {
			return err
		}
		if r != nil {
			if err = checkPreimage(ctx, x, r); err != nil {
				return err
			}
			planned = r
			return nil
		}
		before, err := loadAgent(ctx, x, in.project, in.target)
		if err != nil {
			return err
		}
		at, err := commandTime(ctx, x)
		if err != nil {
			return err
		}
		after, fields, err := planConfiguration(in, before, at)
		if err != nil {
			return err
		}
		id, err := f.NewID[c.AgentCommand]()
		if err != nil {
			return unavailable(err)
		}
		user, err := f.ParseID[i.User](in.actor.Details().UserID)
		if err != nil {
			return invalid()
		}
		r = &commandRecord{ID: id, Project: in.project, User: user, Target: in.target, Name: in.name, Key: in.meta.IdempotencyKey, Semantic: in.semantic, Input: in.raw, Expected: in.meta.ExpectedVersion, Revision: 1, Before: before, After: after, ChangedFields: fields, State: "planned", Created: at}
		if in.create != nil {
			d := in.create.Fields()
			r.AddSkillsEnabled = d.AddSkillsEnabled
			r.InstallSkillEnabled = d.InstallSkillEnabled
		}
		if err = checkPreimage(ctx, x, r); err != nil {
			return err
		}
		if err = insertCommand(ctx, x, r); err != nil {
			return err
		}
		planned = r
		return nil
	})
	return planned, replay, result
}

// Resolve the real default before any owner/reference plan is issued. Updating
// this persisted plan consumes a revision; final Apply never enlarges it.
func (s *Service) freezeTools(ctx context.Context, in commandInput, prepared *commandRecord, ids []i.ToolID) (*commandRecord, *c.AgentMutation, f.CommitResult) {
	var planned *commandRecord
	var replay *c.AgentMutation
	cause, _ := f.NewCommandsCause(in.identity)
	result := s.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state.store.AcquireAll(ctx, tx, in.locks()); err != nil {
			return portError(err)
		}
		x, r, out, err := s.currentCommand(ctx, tx, in)
		if err != nil {
			return err
		}
		if out != nil {
			replay = out
			return nil
		}
		if err = s.requireMutation(ctx, tx, in); err != nil {
			return err
		}
		want, err := recordMapping(prepared)
		if err != nil {
			return err
		}
		got, err := recordMapping(r)
		if err != nil || want != got {
			return fault(f.VersionConflict)
		}
		if err = checkPreimage(ctx, x, r); err != nil {
			return err
		}
		d := r.After.Fields()
		if slices.Equal(d.AllowedToolIDs, ids) {
			planned = r
			return nil
		}
		if in.create == nil {
			return fault(f.InvalidState)
		}
		if r.Revision == math.MaxInt64 {
			return counterExhausted()
		}
		d.AllowedToolIDs = slices.Clone(ids)
		after, err := c.NewAgentConfig(d)
		if err != nil {
			return err
		}
		next := *r
		next.After = after
		next.Revision++
		if err = next.validate(); err != nil {
			return err
		}
		raw, err := canonical(after)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_agent.commands SET after_config=$4,plan_revision=$3 WHERE project_id=$1 AND id=$2 AND state='planned' AND plan_revision=$5`, r.Project.String(), r.ID.String(), int64(next.Revision), raw, int64(r.Revision))
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		planned = &next
		return nil
	})
	return planned, replay, result
}
