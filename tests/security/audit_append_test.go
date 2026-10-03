//go:build integration

package security_test

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"sync"
	"testing"
)

func TestAuditSameTxRollbackReplayAndCurrentAuthorization(t *testing.T) {
	f := newAuditFixture(t)
	entry, key := f.entry(t, f.actor, f.scope), appendKey(t, ac.SecretProducer)
	result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		e, _ := f.store.InTx(tx)
		if _, err := e.Exec(ctx, `INSERT INTO audit_fixture.business VALUES(1,1)`); err != nil {
			return err
		}
		if _, err := f.service.AppendInTx(ctx, tx, entry, key); err != nil {
			return err
		}
		return deny(foundation.InvalidState)
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("rollback not observed")
	}
	var count int
	if err := f.store.QueryRow(auditContext(t), `SELECT (SELECT count(*) FROM audit_fixture.business)+(SELECT count(*) FROM agenteam_audit.audit_records)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("audit or business escaped transaction")
	}
	receipts := make(chan ac.AppendReceipt, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			r, result := appendAudit(t, f, entry, key)
			if result.State() != foundation.Committed {
				t.Errorf("append: %v", result.Fault())
				return
			}
			receipts <- r
		})
	}
	wg.Wait()
	close(receipts)
	var first ac.AppendReceipt
	for r := range receipts {
		if first.AuditID.Validate() != nil {
			first = r
		} else if first != r {
			t.Fatal("replay did not retain first ID/time")
		}
	}
	if first.AuditID.Validate() != nil {
		t.Fatal("no receipt")
	}
	changed := f.entry(t, f.actor, f.scope)
	if _, result := appendAudit(t, f, changed, key); result.State() != foundation.NotCommitted || result.Fault().Code != foundation.IdempotencyKeyReused {
		t.Fatal("semantic conflict accepted")
	}
	// A new current session and trace replay the same stable human event.
	session := newID[identity.Session](t)
	if _, err := f.store.Exec(auditContext(t), `INSERT INTO audit_fixture.sessions VALUES($1,$2,true)`, session.String(), f.actor.Details().UserID); err != nil {
		t.Fatal(err)
	}
	user, _ := foundation.ParseID[identity.User](f.actor.Details().UserID)
	actor, _ := identity.NewHuman(user, session)
	fields := entry.Fields()
	fields.Actor = actor
	fields.Associations.HTTPTraceID = newID[struct{}](t).String()
	replay, _ := ac.NewEntry(fields)
	r, result := appendAudit(t, f, replay, key)
	if result.State() != foundation.Committed || r != first {
		t.Fatal("transport data changed semantic replay")
	}
	got, err := f.service.Get(auditContext(t), actor, f.scope, first.AuditID)
	if err != nil || got.Associations.HTTPTraceID != "" {
		t.Fatal("replay overwrote first diagnostic association")
	}
	// Actual Runner/Tool attempts are business identity, not HTTP trace.
	fields.Associations.RequestID = newID[struct{}](t).String()
	attempt1, _ := ac.NewEntry(fields)
	attemptKey := appendKey(t, ac.SecretProducer)
	r1, state := appendAudit(t, f, attempt1, attemptKey)
	if state.State() != foundation.Committed {
		t.Fatal(state.Fault())
	}
	fields.Associations.RequestID = newID[struct{}](t).String()
	attempt2, _ := ac.NewEntry(fields)
	if _, state = appendAudit(t, f, attempt2, attemptKey); state.State() != foundation.NotCommitted || state.Fault().Code != foundation.IdempotencyKeyReused {
		t.Fatal("different actual attempt deduplicated")
	}
	r2, state := appendAudit(t, f, attempt2, appendKey(t, ac.SecretProducer))
	if state.State() != foundation.Committed || r1.AuditID == r2.AuditID {
		t.Fatal("independent attempt key lost event")
	}
	digest, _ := audit.SemanticDigest(entry)
	lookup, err := f.service.LookupAppend(auditContext(t), actor, f.scope, key, digest)
	if err != nil || lookup.State != ac.Committed || *lookup.Receipt != first {
		t.Fatal("receipt lookup failed")
	}
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.sessions SET active=false WHERE id=$1`, session.String()); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.LookupAppend(auditContext(t), actor, f.scope, key, digest)
	requireCode(t, err, foundation.SessionRevoked)
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET owner_id=$1 WHERE id=$2`, newID[identity.User](t).String(), f.project.String()); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.LookupAppend(auditContext(t), f.actor, f.scope, key, digest)
	requireCode(t, err, foundation.NotFound)
	// System permission above remains granted; it must not bypass Project owner.
	_, err = f.service.Get(auditContext(t), f.actor, f.scope, first.AuditID)
	requireCode(t, err, foundation.NotFound)
}

func TestAuditServiceReceiptScopeAndUnboundDenial(t *testing.T) {
	f := newAuditFixture(t)
	scope := identity.SystemScope()
	key := appendKey(t, ac.SecretProducer)
	registration, _ := identity.RegisterService(identity.SecretService)
	actor, _ := registration.Actor(key.Details().CauseRef, scope)
	entry := f.entry(t, actor, scope)
	receipt, result := appendAudit(t, f, entry, key)
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	digest, _ := audit.SemanticDigest(entry)
	lookup, err := f.service.LookupAppend(auditContext(t), actor, scope, key, digest)
	if err != nil || lookup.State != ac.Committed || lookup.Receipt.AuditID != receipt.AuditID {
		t.Fatal("service own receipt denied")
	}
	wrong, _ := registration.Actor(newID[struct{}](t).String(), scope)
	_, err = f.service.LookupAppend(auditContext(t), wrong, scope, key, digest)
	requireCode(t, err, foundation.Forbidden)
	_, err = f.service.List(auditContext(t), actor, scope, ac.Filter{}, foundation.DefaultPageRequest())
	requireCode(t, err, foundation.Forbidden)
	unbound, _ := audit.New(f.store, auditKeys(t), audit.Authorizations{})
	_, err = unbound.Get(auditContext(t), f.actor, scope, receipt.AuditID)
	requireCode(t, err, foundation.DependencyUnbound)
	absent := appendKey(t, ac.SecretProducer)
	lookup, err = f.service.LookupAppend(auditContext(t), f.actor, scope, absent, digest)
	if err != nil || lookup.State != ac.NotObserved {
		t.Fatal("absent lookup failed")
	}
	f.store.StopAdmission()
	_, err = f.service.LookupAppend(auditContext(t), actor, scope, key, digest)
	requireCode(t, err, foundation.DependencyUnavailable)
}
