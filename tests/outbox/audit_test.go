//go:build integration

package outbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type auditAuthority struct{ f *fixture }

func (a auditAuthority) RequireCurrentSession(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	if !actor.Equal(a.f.actor) {
		return foundation.NewFault(foundation.SessionRevoked, foundation.NotStarted)
	}
	x, err := a.f.store.InTx(tx)
	if err != nil {
		return err
	}
	var visible bool
	if err = x.QueryRow(ctx, `SELECT visible FROM outbox_fixture.authority WHERE id=$1`, a.f.project.String()).Scan(&visible); err != nil {
		return err
	}
	if !visible {
		return foundation.NewFault(foundation.SessionRevoked, foundation.NotStarted)
	}
	return nil
}
func (a auditAuthority) AuthorizeSystem(ctx context.Context, tx foundation.Tx, actor identity.Actor, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if err := a.RequireCurrentSession(ctx, tx, actor); err != nil {
		return identity.AccessGrant{}, err
	}
	x, _ := a.f.store.InTx(tx)
	var active bool
	if err := x.QueryRow(ctx, `SELECT active FROM outbox_fixture.authority WHERE id=$1`, a.f.project.String()).Scan(&active); err != nil {
		return identity.AccessGrant{}, err
	}
	if !active {
		return identity.AccessGrant{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	at, _ := foundation.NewInstant(time.Now())
	return identity.NewAccessGrant(actor, identity.SystemScope(), intent, at, 1)
}
func TestOutboxNewAuditProducerUsesCurrentHumanAuthorizationAndSameTx(t *testing.T) {
	f := newFixture(t)
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	auth := auditAuthority{f}
	svc, err := audit.New(f.store, keys, audit.Authorizations{Sessions: auth, System: auth})
	if err != nil {
		t.Fatal(err)
	}
	delivery, eventID := id[struct{}](t), id[struct{}](t)
	metadata, err := ac.OutboxRequeueMetadata(ac.OutboxRequeueFields{DeliveryID: delivery.String(), EventID: eventID.String(), HandlerID: "fixture.handler", FromState: "failed", RedriveCycle: 1, Reason: ac.OperatorRetry})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := ac.NewResource(ac.OutboxDeliveryResource, delivery.String())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: f.actor, Action: ac.OutboxDeliveryRequeue, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(ac.OutboxProducer, id[struct{}](t).String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	user, _ := foundation.UserLock(f.actor.Details().UserID)
	system, _ := foundation.SystemConfigLock("fixture-system-authority")
	appendAudit := func(rollback bool) (ac.AppendReceipt, foundation.CommitResult) {
		var receipt ac.AppendReceipt
		r := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: system, Mode: foundation.Shared}, {Key: user, Mode: foundation.Shared}}); err != nil {
				return err
			}
			x, _ := f.store.InTx(tx)
			if _, err := x.Exec(ctx, `INSERT INTO outbox_fixture.facts(id,value,version) VALUES($1,'requeue-fixture',1) ON CONFLICT DO NOTHING`, delivery.String()); err != nil {
				return err
			}
			var err error
			receipt, err = svc.AppendInTx(ctx, tx, entry, key)
			if err != nil {
				return err
			}
			if rollback {
				return foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
			}
			return nil
		})
		return receipt, r
	}
	_, result := appendAudit(true)
	state(t, result, foundation.NotCommitted)
	if f.count(t, `SELECT count(*) FROM outbox_fixture.facts`) != 0 || f.count(t, `SELECT count(*) FROM agenteam_audit.audit_records`) != 0 {
		t.Fatal("new Audit survived domain rollback")
	}
	receipt, result := appendAudit(false)
	state(t, result, foundation.Committed)
	again, result := appendAudit(false)
	state(t, result, foundation.Committed)
	if receipt != again || f.count(t, `SELECT count(*) FROM agenteam_audit.audit_records`) != 1 {
		t.Fatal("new Audit idempotency changed")
	}
	f.sql(t, `UPDATE outbox_fixture.authority SET visible=false WHERE id=$1`, f.project.String())
	_, result = appendAudit(false)
	state(t, result, foundation.NotCommitted)
	code(t, result.Fault(), foundation.SessionRevoked)
}
