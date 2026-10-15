package project

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MeetingDeletionFacts accepts only its original runtime's live cleanup
// witness. The Project owner comes from this gate's current same-Tx read.
type MeetingDeletionFacts interface {
	RequireMeetingDeletionBindingInTx(context.Context, f.Tx, any) error
	RequireMeetingCleanupInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.UserID) error
}

type MeetingDeletionAccess struct {
	projects *Authority
	facts    MeetingDeletionFacts
}

func NewMeetingDeletionAccess(projects *Authority, facts MeetingDeletionFacts) (*MeetingDeletionAccess, error) {
	if projects.state() == nil || nilPort(facts) {
		return nil, fault(f.DependencyUnbound)
	}
	return &MeetingDeletionAccess{projects: projects, facts: facts}, nil
}

// This is not a Launch/Mutate grant. Only an already accepted deletion can
// finish after Project archive; fresh Human commands keep the original gate.
func (a *MeetingDeletionAccess) RequireMeetingDeletionProjectInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID) error {
	if a == nil || a.projects.state() == nil || nilPort(a.facts) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || project.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.MeetingRuntime || d.ProjectID != project.String() {
		return fault(f.Forbidden)
	}
	if _, err := f.ParseID[struct{}](d.CauseRef); err != nil {
		return fault(f.Forbidden)
	}
	store := a.projects.state().store
	x, err := store.InTx(tx)
	if err != nil {
		return preparationProjectError(err)
	}
	if err := store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
		return preparationProjectError(err)
	}
	if err := a.facts.RequireMeetingDeletionBindingInTx(ctx, tx, store); err != nil {
		return preparationProjectError(err)
	}
	p, err := loadProject(ctx, x, project)
	if err != nil {
		return preparationProjectError(err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil {
		return fault(f.NotFound)
	}
	if p.ref.ID != project || p.ref.OwnerUserID.Validate() != nil || !p.initialized {
		return fault(f.ProjectNotActive)
	}
	if err := a.facts.RequireMeetingCleanupInTx(ctx, tx, actor, project, p.ref.OwnerUserID); err != nil {
		return preparationProjectError(err)
	}
	return a.projects.checkAuditLifecycleBarrierInTx(ctx, tx, project)
}
