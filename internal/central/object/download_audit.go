package object

import (
	"bytes"
	"context"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type downloadIssuedAuditKey struct{}
type downloadIssuedAudit struct {
	active  atomic.Bool
	service *Service
	tx      f.Tx
	actor   i.Actor
	claims  downloadClaims
	plan    oc.AccessLockPlan
	locked  oc.LockedAccess
}

// Only the original grant INSERT callback opens this scope. Its lifetime is the
// original provider Append return, not request cancellation or a public event.
func (d *Downloads) appendIssuedAudit(ctx context.Context, tx f.Tx, actor i.Actor, c downloadClaims, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	r := d.state()
	w := &downloadIssuedAudit{service: r.objects, tx: tx, actor: actor, claims: c, plan: plan, locked: locked}
	w.active.Store(true)
	defer w.active.Store(false)
	event := oc.DownloadEvent{GrantID: c.grant, Phase: oc.DownloadIssued, Length: c.target.Details().Source.Details().Meta.ByteSize}
	return r.provider.AppendDownloadInTx(context.WithValue(ctx, downloadIssuedAuditKey{}, w), tx, actor, c.target, event, plan, locked)
}

// CheckDownloadIssuedInTx consumes Object's original issuing scope and exact
// persisted grant. It provides facts to the owning business Audit checker; it
// cannot mint a grant, open a transaction, or authorize a download stream.
func CheckDownloadIssuedInTx(ctx context.Context, tx f.Tx, store Store, actor i.Actor, target oc.DownloadTarget, event oc.DownloadEvent) error {
	if ctx == nil || !tx.Valid() || actor.Validate() != nil || target.Validate() != nil || event.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	w, _ := ctx.Value(downloadIssuedAuditKey{}).(*downloadIssuedAudit)
	if w == nil || !w.active.Load() || w.service == nil || w.service.data == nil || w.tx != tx ||
		!sameProjectAuditStore(store, w.service.state().store) || !actor.Equal(w.actor) || actor.Details().Kind != i.Human ||
		event.Phase != oc.DownloadIssued || event.GrantID != w.claims.grant || event.Length != target.Details().Source.Details().Meta.ByteSize {
		return projectAuditDenied()
	}
	expected, _, err := grantWire(w.claims)
	if err != nil {
		return err
	}
	actual := w.claims
	actual.target = target
	raw, _, err := grantWire(actual)
	if err != nil || !bytes.Equal(raw, expected) {
		return projectAuditDenied()
	}
	if err = w.service.ValidateAccessPlanInTx(ctx, tx, w.plan.Details().Request, w.plan, w.locked); err != nil {
		return err
	}
	held := false
	for _, lock := range w.locked.Locks() {
		want := downloadGrantLock(event.GrantID)
		if lock.Key.Canonical() == want.Key.Canonical() && lock.Mode == want.Mode {
			held = true
		}
	}
	if !held {
		return projectAuditDenied()
	}
	x, err := store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	if nilPort(x) {
		return failure(f.DependencyUnbound, nil)
	}
	if err = checkGrant(ctx, x, w.claims, true); err != nil {
		return err
	}
	if !w.active.Load() {
		return projectAuditDenied()
	}
	if err = ctx.Err(); err != nil {
		return unavailable(err)
	}
	return nil
}
