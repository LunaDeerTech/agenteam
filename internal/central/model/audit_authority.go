package model

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func auditEntry(actor id.Actor, p *mutationPlan) (ac.Entry, ac.AppendKey, error) {
	metadata, e := ac.ModelMetadata(ac.Action(p.Kind), p.Metadata)
	if e != nil {
		return ac.Entry{}, ac.AppendKey{}, e
	}
	kind := ac.ModelProviderResource
	if p.BeforeModel != nil || p.AfterModel != nil {
		kind = ac.ModelConfigResource
	}
	if p.Kind == "model.selection.update" {
		kind = ac.ModelSelectionResource
	}
	resource, e := ac.NewResource(kind, p.Resource)
	if e != nil {
		return ac.Entry{}, ac.AppendKey{}, e
	}
	entry, e := ac.NewEntry(ac.EntryFields{Scope: id.SystemScope(), Actor: actor, Action: ac.Action(p.Kind), Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if e != nil {
		return ac.Entry{}, ac.AppendKey{}, e
	}
	cause, e := cursor.Digest([]byte(p.Identity))
	if e != nil {
		return ac.Entry{}, ac.AppendKey{}, e
	}
	key, e := ac.NewAppendKey(ac.ModelProducer, cause.String(), 0)
	return entry, key, e
}
func (a *Authority) validatePreparedFact(ctx context.Context, tx f.Tx, p *preparedCommand) error {
	if e := a.state().store.RequireHeldLocks(ctx, tx, p.locks); e != nil {
		return portError(e)
	}
	if e := a.current(ctx, tx, p.actor, id.Mutate); e != nil {
		return e
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return portError(e)
	}
	r, e := loadCommand(ctx, x, p.identity)
	if e != nil {
		return e
	}
	if r == nil || r.Phase != "prepared" || r.ID != p.plan.CommandID || r.User != p.actor.Details().UserID || r.Semantic != p.plan.Semantic || !sameValue(r.Plan, p.plan) {
		return fault(f.Forbidden)
	}
	if p.plan.BeforeProvider != nil || p.plan.AfterProvider != nil {
		actual, e := loadProvider(ctx, x, p.plan.Resource)
		if e != nil {
			return e
		}
		if !sameValue(actual, p.plan.AfterProvider) {
			return fault(f.Forbidden)
		}
	}
	if p.plan.BeforeModel != nil || p.plan.AfterModel != nil {
		actual, e := loadModel(ctx, x, p.plan.Resource)
		if e != nil {
			return e
		}
		if !sameValue(actual, p.plan.AfterModel) {
			return fault(f.Forbidden)
		}
	}
	if p.plan.AfterSelection != nil {
		actual, e := loadSelection(ctx, x)
		if e != nil {
			return e
		}
		if !sameValue(actual, p.plan.AfterSelection) {
			return fault(f.Forbidden)
		}
		refs, e := loadOwnerReferences(ctx, x, actual.ID)
		if e != nil {
			return e
		}
		if !sameValue(refs, selectionReferences(actual)) {
			return fault(f.Forbidden)
		}
	}
	for _, event := range r.Plan.Events {
		if !validPersistedEvent(event) {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (a *Authority) CheckAppendInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if entry.Validate() != nil || key.Validate() != nil || !ac.ModelAction(entry.Fields().Action) {
		return fault(f.InvalidArgument)
	}
	p, e := a.contextPlan(ctx, entry.Fields().Actor)
	if e != nil {
		return e
	}
	expected, expectedKey, e := auditEntry(p.actor, &p.plan)
	if e != nil {
		return e
	}
	got, want := entry.Fields(), expected.Fields()
	if key.Details() != expectedKey.Details() || got.Action != want.Action || got.Outcome != want.Outcome || !got.Scope.Equal(want.Scope) || got.Resource.Details() != want.Resource.Details() || !sameValue(got.Metadata.JSON(), want.Metadata.JSON()) || got.Associations != (ac.Associations{}) {
		return fault(f.Forbidden)
	}
	return a.validatePreparedFact(ctx, tx, p)
}

var _ ac.ModelAuthority = (*Authority)(nil)
