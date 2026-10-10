package project

import (
	"bytes"
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type SprintLifecycle struct {
	authority *Authority
	facts     c.SprintStartFacts
}

func NewSprintLifecycle(authority *Authority, facts c.SprintStartFacts) (*SprintLifecycle, error) {
	if authority.state() == nil || nilPort(facts) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SprintLifecycle{authority: authority, facts: facts}, nil
}

// ApplySprintStartInTx never starts/commits a transaction or acquires a lock.
// Current Owner and the Work owner's private same-Tx write witness are both
// required. In particular this is not an arbitrary current-Sprint setter.
func (s *SprintLifecycle) ApplySprintStartInTx(ctx context.Context, tx f.Tx, actor i.Actor, change c.SprintStartChange) (c.ProjectRef, error) {
	if s == nil || s.authority.state() == nil || nilPort(s.facts) {
		return c.ProjectRef{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || change.Validate() != nil {
		return c.ProjectRef{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.ProjectRef{}, err
	}
	if err := human(actor); err != nil {
		return c.ProjectRef{}, err
	}
	user, err := f.ParseID[i.User](actor.Details().UserID)
	if err != nil {
		return c.ProjectRef{}, fault(f.Unauthenticated)
	}
	change = change.Clone()
	locks, err := change.RequiredLocks(user)
	if err != nil {
		return c.ProjectRef{}, err
	}
	st := s.authority.state()
	x, err := st.store.InTx(tx)
	if err != nil {
		return c.ProjectRef{}, portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return c.ProjectRef{}, portError(err)
	}
	access, err := s.authority.RequireOwnerInTx(ctx, tx, actor, change.Before.ID, i.Mutate)
	if err != nil {
		return c.ProjectRef{}, err
	}
	actual, _ := json.Marshal(access.Project())
	before, _ := json.Marshal(change.Before)
	if !bytes.Equal(actual, before) {
		return c.ProjectRef{}, fault(f.VersionConflict)
	}
	if err = s.facts.CheckSprintStartAppliedInTx(ctx, tx, actor, change); err != nil {
		return c.ProjectRef{}, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return c.ProjectRef{}, err
	}
	err = affected(x.Exec(ctx, `UPDATE agenteam_project.projects SET current_sprint_id=$2,version=$3,updated_at=$4 WHERE id=$1 AND version=$5 AND current_sprint_id IS NULL AND lifecycle='active' AND initialized_at IS NOT NULL`, change.Before.ID.String(), change.SprintID.String(), int64(change.After.Version), change.StartedAt.Time(), int64(change.Before.Version)))
	if err != nil {
		return c.ProjectRef{}, err
	}
	if err = ctx.Err(); err != nil {
		return c.ProjectRef{}, err
	}
	return change.After.Clone(), nil
}

var _ c.SprintLifecycleAuthority = (*SprintLifecycle)(nil)
