//go:build integration

package objects_test

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

func auditVerified(t *testing.T, f *objectAuditFixture, key, body string) (oc.PreparedPayload, oc.UploadAttempt) {
	t.Helper()
	ctx := contextFor(t)
	prepared, err := f.service.PreparePayload(ctx, f.actor, f.owner, "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.service.DiscardPrepared(prepared); err != nil {
			t.Error(err)
		}
	})
	cmd := command(t, key)
	plan := ownerPlan(t, f.service, f.actor, f.owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &cmd, Prepared: prepared})
	var attempt oc.UploadAttempt
	r := plannedTx(f.dbstore, f.service, ctx, cause(t), plan, func(ctx context.Context, tx foundation.Tx, p oc.AccessLockPlan, l oc.LockedAccess) error {
		var err error
		attempt, err = f.service.ReserveUploadInTx(ctx, tx, f.actor, f.owner, cmd, prepared, p, l)
		return err
	})
	if r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	attempt, err = f.service.UploadPrepared(ctx, f.actor, f.owner, prepared, attempt)
	if err != nil {
		t.Fatal(err)
	}
	return prepared, attempt
}
func auditPublish(t *testing.T, f *objectAuditFixture, attempt oc.UploadAttempt) foundation.CommitResult {
	t.Helper()
	plan := ownerPlan(t, f.service, f.actor, f.owner, oc.PublishAccess, oc.AccessRequestDetails{Attempt: attempt})
	return plannedTx(f.dbstore, f.service, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, p oc.AccessLockPlan, l oc.LockedAccess) error {
		e, err := f.dbstore.InTx(tx)
		if err != nil {
			return err
		}
		if _, err = e.Exec(ctx, `INSERT INTO object_fixture.audit_business_marker VALUES(1)`); err != nil {
			return err
		}
		_, err = f.service.PublishVerifiedInTx(ctx, tx, f.actor, f.owner, attempt, p, l)
		return err
	})
}
func TestObjectProjectAuditExternalPublicationExactWitnessAndRollback(t *testing.T) {
	base := newFixture(t, true)
	proxy := newStorageProxy(t, base)
	cfg := auditStorageConfig(t, base, proxy.server.URL)
	f := objectAuditOn(t, base, objectAuditOptions{config: &cfg})
	f.sql(t, `CREATE TABLE object_fixture.audit_business_marker(id int PRIMARY KEY)`)
	_, attempt := auditVerified(t, f, "external-audit", "body")
	var captured context.Context
	var originalTx foundation.Tx
	var originalEntry ac.Entry
	var originalKey ac.AppendKey
	f.tap.set(func(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
		if entry.Fields().Action != ac.ObjectUploadComplete {
			return fault(foundation.InvalidState)
		}
		e, err := f.dbstore.InTx(tx)
		if err != nil {
			return err
		}
		var actual bool
		err = e.QueryRow(ctx, `SELECT a.phase='verified' AND a.io_closed AND u.state<>'committed' AND o.state<>'available' FROM agenteam_object.uploads u JOIN agenteam_object.upload_attempts a ON a.id=u.current_attempt_id JOIN agenteam_object.objects o ON o.id=u.object_id WHERE u.id=$1`, attempt.Details().UploadID.String()).Scan(&actual)
		if err != nil {
			return err
		}
		if !actual {
			t.Error("checker was not called before actual publication")
			return fault(foundation.InvalidState)
		}
		if err = f.checker.CheckProjectAuditInTx(ctx, tx, entry, key); err != nil {
			return err
		}
		if err = f.checker.CheckProjectAuditInTx(context.Background(), tx, entry, key); err == nil {
			t.Error("history without witness accepted")
		}
		other, _ := object.NewProjectAuditAuthority(&objectAuditDistinctStore{f.store})
		if err = other.CheckProjectAuditInTx(ctx, tx, entry, key); err == nil {
			t.Error("different Store accepted")
		}
		for i := 0; i < 9; i++ {
			fields := entry.Fields()
			reflect.ValueOf(&fields.Associations).Elem().Field(i).SetString(id[struct{}](t).String())
			changed, er := ac.NewEntry(fields)
			if er != nil {
				return er
			}
			if er = f.checker.CheckProjectAuditInTx(ctx, tx, changed, key); er == nil {
				t.Error("changed association accepted", i)
			}
		}
		changedKey, _ := ac.NewAppendKey(ac.ObjectProducer, key.Details().CauseRef, key.Details().Ordinal+1)
		if err = f.checker.CheckProjectAuditInTx(ctx, tx, entry, changedKey); err == nil {
			t.Error("changed ordinal accepted")
		}
		captured, originalTx, originalEntry, originalKey = ctx, tx, entry, key
		return fault(foundation.Forbidden)
	})
	r := auditPublish(t, f, attempt)
	if r.State() != foundation.NotCommitted {
		t.Fatal("denied Audit committed")
	}
	requireCode(t, r.Fault(), foundation.Forbidden)
	var n int
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM object_fixture.audit_business_marker`).Scan(&n); err != nil || n != 0 {
		t.Fatal("business marker escaped rollback", err, n)
	}
	if objectAuditCount(t, f.fixture, ac.ObjectUploadComplete) != 0 {
		t.Fatal("denied publication Audit persisted")
	}
	if err := f.checker.CheckProjectAuditInTx(captured, originalTx, originalEntry, originalKey); err == nil {
		t.Fatal("expired Tx accepted")
	}
	for _, change := range []string{"owner", "current-attempt", "digest", "phase"} {
		t.Run(change, func(t *testing.T) {
			f.tap.set(func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
				x, err := f.dbstore.InTx(tx)
				if err != nil {
					return err
				}
				switch change {
				case "owner":
					_, err = x.Exec(ctx, `UPDATE agenteam_object.uploads SET owner_id=$2 WHERE id=$1`, attempt.Details().UploadID.String(), id[struct{}](t).String())
				case "current-attempt":
					_, err = x.Exec(ctx, `UPDATE agenteam_object.uploads SET current_attempt_id=NULL WHERE id=$1`, attempt.Details().UploadID.String())
				case "digest":
					_, err = x.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET sha256=decode($2,'hex') WHERE id=$1`, attempt.Details().ID.String(), digest([]byte("evil")).String()[7:])
				case "phase":
					_, err = x.Exec(ctx, `UPDATE agenteam_object.upload_attempts SET phase='unknown' WHERE id=$1`, attempt.Details().ID.String())
				}
				if err != nil {
					return err
				}
				return f.checker.CheckProjectAuditInTx(ctx, tx, e, k)
			})
			r := auditPublish(t, f, attempt)
			if r.State() != foundation.NotCommitted {
				t.Fatal("changed canonical facts accepted", change)
			}
			requireCode(t, r.Fault(), foundation.Forbidden)
		})
	}
	f.tap.set(nil)
	r = auditPublish(t, f, attempt)
	if r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	if proxy.puts.Load() != 1 || objectAuditCount(t, f.fixture, ac.ObjectUploadComplete) != 1 {
		t.Fatal("publication retry repeated bytes/Audit")
	}
	// Current authorization is independent of retained facts and witness.
	f.sql(t, `UPDATE object_fixture.sessions SET active=false`)
	_, err := f.service.LookupPut(contextFor(t), f.actor, f.owner, "external-audit")
	requireCode(t, err, foundation.SessionRevoked)
}

type objectAuditDistinctStore struct{ objectAuditStore }

func TestObjectProjectAuditWriterRecoveryAndDeleteFacts(t *testing.T) {
	t.Run("writer-and-real-verification", func(t *testing.T) {
		base := newFixture(t, true)
		proxy := newStorageProxy(t, base)
		cfg := auditStorageConfig(t, base, proxy.server.URL)
		f := objectAuditOn(t, base, objectAuditOptions{config: &cfg})
		proxy.mode.Store(proxyRejectCandidateRead)
		_, err := f.service.PutObject(contextFor(t), f.actor, f.owner, command(t, "failed-writer"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
		if err == nil {
			t.Fatal("verification failure accepted")
		}
		var attempt, key string
		var closed bool
		if err = f.store.QueryRow(contextFor(t), `SELECT id::text,candidate_key,io_closed FROM agenteam_object.upload_attempts`).Scan(&attempt, &key, &closed); err != nil || !closed {
			t.Fatal("writer did not close", err)
		}
		if objectAuditCount(t, f.fixture, ac.ObjectUploadFailed) != 1 {
			t.Fatal("writer failure Audit missing")
		}
		var ordinal int64
		if err = f.store.QueryRow(contextFor(t), `SELECT ordinal FROM agenteam_audit.audit_records WHERE cause_ref=$1`, attempt).Scan(&ordinal); err != nil || ordinal != 0 {
			t.Fatal("writer original cause/ordinal", err, ordinal)
		}
		// The recovery branch must obtain a real backend absence, not JSON or an
		// inserted receipt. The original failed candidate was genuinely written.
		if err = f.s3.RemoveObject(contextFor(t), f.bucket, key, minio.RemoveObjectOptions{}); err != nil {
			t.Fatal(err)
		}
		proxy.mode.Store(proxyPass)
		seen := false
		f.tap.set(func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
			if e.Fields().Action != ac.ObjectUploadFailed {
				return nil
			}
			if k.Details().CauseRef != attempt || k.Details().Ordinal != 1 {
				return fault(foundation.InvalidState)
			}
			x, er := f.dbstore.InTx(tx)
			if er != nil {
				return er
			}
			var ok bool
			er = x.QueryRow(ctx, `SELECT a.phase='abandoned' AND a.cleanup_gate AND EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations c WHERE c.attempt_id=a.id) FROM agenteam_object.upload_attempts a WHERE a.id=$1`, attempt).Scan(&ok)
			if er != nil {
				return er
			}
			if !ok {
				return fault(foundation.InvalidState)
			}
			seen = true
			return nil
		})
		if err = f.service.Recover(contextFor(t)); err != nil {
			t.Fatal(err)
		}
		if !seen || objectAuditCount(t, f.fixture, ac.ObjectUploadFailed) != 2 {
			t.Fatal("recovery failure original ordinal missing")
		}
	})
	t.Run("delete-prestate-and-earliest-cause", func(t *testing.T) {
		f := newObjectAuditFixture(t, true)
		put := f.put(t, "delete-audit", "body")
		seen := 0
		f.tap.set(func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
			if e.Fields().Action != ac.ObjectDelete {
				return nil
			}
			seen++
			x, err := f.dbstore.InTx(tx)
			if err != nil {
				return err
			}
			var valid bool
			if err = x.QueryRow(ctx, `SELECT cleaning AND state<>'deleted' AND NOT EXISTS(SELECT 1 FROM agenteam_object.upload_attempts WHERE object_id=o.id AND phase<>'cleaned') FROM agenteam_object.objects o WHERE id=$1`, put.Meta.ID.String()).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return fault(foundation.InvalidState)
			}
			if err = f.checker.CheckProjectAuditInTx(ctx, tx, e, k); err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `UPDATE agenteam_object.cleanup_operations SET operation_id=$2 WHERE object_id=$1`, put.Meta.ID.String(), id[oc.CleanupOperation](t).String()); err != nil {
				return err
			}
			err = f.checker.CheckProjectAuditInTx(ctx, tx, e, k)
			if err == nil {
				t.Error("replacement cleanup cause accepted")
				return fault(foundation.InvalidState)
			}
			return err
		})
		_, err := f.service.CancelUpload(contextFor(t), f.actor, f.owner, "delete-audit")
		requireCode(t, err, foundation.Forbidden)
		if seen != 1 || objectAuditCount(t, f.fixture, ac.ObjectDelete) != 0 {
			t.Fatal("delete rejection did not roll back")
		}
		var state string
		if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.objects WHERE id=$1`, put.Meta.ID.String()).Scan(&state); err != nil || state == "deleted" {
			t.Fatal("deleted UPDATE escaped rollback", err, state)
		}
		f.tap.set(nil)
		if err = f.service.Recover(contextFor(t)); err != nil {
			t.Fatal(err)
		}
		if objectAuditCount(t, f.fixture, ac.ObjectDelete) != 1 {
			t.Fatal("delete Audit missing")
		}
		if err = f.service.Recover(contextFor(t)); err != nil {
			t.Fatal(err)
		}
		if objectAuditCount(t, f.fixture, ac.ObjectDelete) != 1 {
			t.Fatal("delete replay duplicated Audit")
		}
	})
}

func TestObjectProjectAuditPublicationActualCommitUnknownReplaysWithoutPUT(t *testing.T) {
	base := newFixture(t, true)
	raw, proxy := proxyStore(t, base, true)
	http := newStorageProxy(t, base)
	cfg := auditStorageConfig(t, base, http.server.URL)
	wrapped := &objectAuditCommitStore{Store: raw, proxy: proxy}
	f := objectAuditOn(t, base, objectAuditOptions{pg: raw, store: wrapped, config: &cfg})
	f.sql(t, `CREATE TABLE object_fixture.audit_business_marker(id int PRIMARY KEY)`)
	_, attempt := auditVerified(t, f, "unknown-publication", "body")
	wrapped.arm(ac.ObjectUploadComplete, attempt.Details().ObjectID.String(), false)
	done := make(chan foundation.CommitResult, 1)
	go func() { done <- auditPublish(t, f, attempt) }()
	select {
	case <-proxy.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("publication Audit COMMIT not intercepted")
	}
	wrapped.assertHit(t, base, false)
	var visible bool
	if err := base.store.QueryRow(contextFor(t), `SELECT u.state='committed' AND o.state='available' AND EXISTS(SELECT 1 FROM object_fixture.audit_business_marker) FROM agenteam_object.uploads u JOIN agenteam_object.objects o ON o.id=u.object_id WHERE u.id=$1`, attempt.Details().UploadID.String()).Scan(&visible); err != nil || !visible {
		t.Fatal("actual final publication not committed", err)
	}
	close(proxy.release)
	select {
	case r := <-done:
		if r.State() != foundation.Unknown {
			t.Fatal("lost ACK was not Unknown", r.State())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("publication return did not join")
	}
	wrapped.waitWriter(t, base)
	result, err := f.service.PutObject(contextFor(t), f.actor, f.owner, command(t, "unknown-publication"), "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if err != nil || result.Meta.ID != attempt.Details().ObjectID || http.puts.Load() != 1 || objectAuditCount(t, f.fixture, ac.ObjectUploadComplete) != 1 {
		t.Fatal("publication replay changed original facts or bytes", err)
	}
}
