package skill

import (
	"context"
	"reflect"

	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	o "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func (a *AssignmentAuditFacts) assignmentProjectEventOwner(ctx context.Context, actor i.Actor, summary e.Summary) (*AssignmentAuthority, error) {
	if a == nil || nilPort(a.store) || ctx == nil {
		return nil, fault(f.DependencyUnbound)
	}
	project, target, _, err := assignmentEventCoordinates(actor, summary)
	if err != nil || actor.Details().Kind != i.AgentRun || actor.Details().ProjectID != project.String() {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(assignmentRunKey{}).(*assignmentRunWitness)
	if w == nil || !w.matches(w.input) || !w.actor.Equal(actor) || w.project != project || w.input.Request.AgentID != target || w.owner == nil || w.owner.commands == nil || w.owner.commands.deps.Service.state() == nil || w.owner.commands.facts == nil || nilPort(w.plan) {
		return nil, fault(f.Forbidden)
	}
	// Discovery has no Tx yet. Require the exact fixed Store before delegating
	// to the original command owner; validation also checks the actual same Tx.
	actual, expected := reflect.ValueOf(a.store), reflect.ValueOf(w.owner.commands.store())
	if actual.Type() != expected.Type() || !actual.Comparable() || !actual.Equal(expected) {
		return nil, fault(f.Forbidden)
	}
	return w.owner.commands.facts, ctx.Err()
}

func (a *AssignmentAuditFacts) DiscoverAssignmentProjectEvent(ctx context.Context, actor i.Actor, summary e.Summary) (o.Dependencies, error) {
	owner, err := a.assignmentProjectEventOwner(ctx, actor, summary)
	if err != nil {
		return o.Dependencies{}, err
	}
	return owner.DiscoverAppend(ctx, actor, summary)
}

func (a *AssignmentAuditFacts) ValidateAssignmentProjectEventInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary e.Summary, deps o.Dependencies, stage o.Stage) error {
	owner, err := a.assignmentProjectEventOwner(ctx, actor, summary)
	if err != nil {
		return err
	}
	if _, err = a.store.InTx(tx); err != nil {
		return captureError(err)
	}
	return owner.ValidateAppendInTx(ctx, tx, actor, summary, deps, stage)
}

var _ sc.AssignmentProjectEventAuthority = (*AssignmentAuditFacts)(nil)
