//go:build integration

package security_test

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func requireSecretVariableStored(t *testing.T, result sc.ProjectVariableWriteObservation, commit f.CommitResult, effect sc.ProjectVariableEffect, version f.Version) sc.ProjectVariableWriteResultFields {
	t.Helper()
	if commit.State() != f.Committed {
		t.Fatalf("storage transaction state %s: %v", commit.State(), commit.Fault())
	}
	fields, err := result.Result()
	if err != nil || fields.Effect != effect || fields.Version != version {
		t.Fatalf("storage effect/version: %v", err)
	}
	return fields
}

// This suite validates real D04/Audit SQL and transactions. The fixture's D10
// authority remains explicitly controlled; it is not the production Owner.
func TestSecretVariableStorageSQLReplayAndEffects(t *testing.T) {
	v := newSecretVariableStorageFixture(t)
	value := "storage-original-secret-canary"
	createIntent := v.intent(t, sc.Create, "storage-create", 0, &value)
	create := v.prepare(t, createIntent, sc.ProjectVariableWriteNotObserved(), false)
	created, commit := v.apply(t, create, nil)
	original := requireSecretVariableStored(t, created, commit, sc.ProjectVariableCreated, 1)
	if got := v.counts(t); got != (secretVariableStorageCounts{1, 1, 2, 1}) {
		t.Fatal("create durable counts", got)
	}
	noneIntent := v.intent(t, sc.Update, "storage-metadata", 7, nil)
	none := v.prepare(t, noneIntent, created, false)
	unchanged, commit := v.apply(t, none, nil)
	requireSecretVariableStored(t, unchanged, commit, sc.ProjectVariableUnchanged, 1)
	if got := v.counts(t); got != (secretVariableStorageCounts{1, 2, 3, 1}) {
		t.Fatal("metadata-only mutated value/audit", got)
	}
	replacement := "storage-replacement-secret-canary"
	updateIntent := v.intent(t, sc.Update, "storage-replace", 8, &replacement)
	update := v.prepare(t, updateIntent, unchanged, false)
	updated, commit := v.apply(t, update, nil)
	requireSecretVariableStored(t, updated, commit, sc.ProjectVariableReplaced, 2)
	if got := v.counts(t); got != (secretVariableStorageCounts{1, 3, 4, 2}) {
		t.Fatal("replace durable counts", got)
	}
	deleteIntent := v.intent(t, sc.Delete, "storage-delete", 9, nil)
	remove := v.prepare(t, deleteIntent, updated, false)
	deleted, commit := v.apply(t, remove, nil)
	last := requireSecretVariableStored(t, deleted, commit, sc.ProjectVariableDeleted, 3)
	if !last.Deleted || !last.Ref.Equal(original.Ref) {
		t.Fatal("delete identity/result")
	}
	want := secretVariableStorageCounts{0, 4, 4, 3}
	if got := v.counts(t); got != want {
		t.Fatal("delete retained history", got)
	}
	// The current value is gone. Each original receipt still replays against its
	// original semantics and ID, including create and a metadata-only update.
	for _, replay := range []struct {
		intent sc.ProjectVariableIntent
		result sc.ProjectVariableWriteObservation
	}{{createIntent, created}, {noneIntent, unchanged}, {updateIntent, updated}, {deleteIntent, deleted}} {
		p := v.prepare(t, replay.intent, replay.result, true)
		got, commit := v.apply(t, p, nil)
		old, _ := replay.result.Result()
		replayed := requireSecretVariableStored(t, got, commit, old.Effect, old.Version)
		if replayed.ReceiptID != old.ReceiptID || !replayed.Ref.Equal(old.Ref) || v.counts(t) != want {
			t.Fatal("historical receipt changed")
		}
	}
	changed := "different-original-value"
	reused := v.prepare(t, v.intent(t, sc.Create, "storage-create", 0, &changed), created, true)
	got, failed := v.apply(t, reused, nil)
	requireCode(t, failed.Fault(), f.IdempotencyKeyReused)
	if got.Validate() == nil || v.counts(t) != want {
		t.Fatal("KeyReused published/mutated")
	}
	// A fresh persisted Session requires a new plan but is not a new original
	// intent. Its original stable writer and receipt remain unchanged.
	user, _ := f.ParseID[i.User](v.actor.Details().UserID)
	session := newID[i.Session](t)
	if _, err := v.store.Exec(auditContext(t), `INSERT INTO audit_fixture.sessions(id,user_id,active) VALUES($1,$2,true)`, session.String(), user.String()); err != nil {
		t.Fatal(err)
	}
	v.actor, _ = i.NewHuman(user, session)
	freshIntent := v.intent(t, sc.Create, "storage-create", 0, &value)
	fresh := v.prepare(t, freshIntent, created, true)
	freshResult, commit := v.apply(t, fresh, nil)
	requireSecretVariableStored(t, freshResult, commit, sc.ProjectVariableCreated, 1)
	if _, err := v.store.Exec(auditContext(t), `UPDATE audit_fixture.sessions SET active=false WHERE id=$1`, session.String()); err != nil {
		t.Fatal(err)
	}
	got, failed = v.apply(t, fresh, nil)
	requireCode(t, failed.Fault(), f.SessionRevoked)
	if got.Validate() == nil || v.counts(t) != want {
		t.Fatal("revoked history bypassed current gate")
	}
	var leaked bool
	if err := v.store.QueryRow(auditContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE project_id=$1 AND (metadata::text LIKE '%storage-original-secret-canary%' OR metadata::text LIKE '%storage-replacement-secret-canary%'))`, v.project.String()).Scan(&leaked); err != nil || leaked {
		t.Fatal("Audit material disclosure", err)
	}
}

func TestSecretVariableStorageSQLAtomicAuditAndOwnerRollback(t *testing.T) {
	for _, mode := range []string{"owner-tail", "audit-without-witness", "audit-wrong-payload-kind", "missing-lock"} {
		t.Run(mode, func(t *testing.T) {
			v := newSecretVariableStorageFixture(t)
			value := "before-rollback"
			initial := v.prepare(t, v.intent(t, sc.Create, "rollback-create", 0, &value), sc.ProjectVariableWriteNotObserved(), false)
			created, commit := v.apply(t, initial, nil)
			beforeResult := requireSecretVariableStored(t, created, commit, sc.ProjectVariableCreated, 1)
			before := v.counts(t)
			changed := "must-rollback-value"
			p := v.prepare(t, v.intent(t, sc.Update, "rollback-update", 7, &changed), created, false)
			var after func(context.Context, f.Tx) error
			if mode == "owner-tail" {
				after = func(context.Context, f.Tx) error { return f.NewFault(f.InvalidState, f.NotStarted) }
			}
			if mode == "audit-without-witness" || mode == "audit-wrong-payload-kind" {
				v.ports.before = func(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (ac.Entry, ac.AppendKey, error) {
					if mode == "audit-without-witness" {
						return entry, key, v.ports.checker.CheckProjectAuditInTx(context.Background(), tx, entry, key)
					}
					x, err := v.store.InTx(tx)
					if err != nil {
						return entry, key, err
					}
					_, err = x.Exec(ctx, `UPDATE agenteam_secret.secret_payloads SET owner_kind=2 WHERE payload_id=(SELECT digest_payload_id FROM agenteam_secret.project_variable_receipts WHERE project_id=$1 AND command_digest=$2)`, v.project.String(), key.Details().CauseRef)
					return entry, key, err
				}
			}
			var got sc.ProjectVariableWriteObservation
			if mode == "missing-lock" {
				commit = v.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx f.Tx) error {
					var err error
					got, err = v.secret.ApplyProjectVariableWriteInTx(ctx, tx, p)
					return err
				})
			} else {
				got, commit = v.apply(t, p, after)
			}
			if commit.State() != f.NotCommitted || got.Validate() == nil {
				t.Fatalf("rejected transaction published %s", commit.State())
			}
			if v.counts(t) != before {
				t.Fatal("D04/Audit partial commit")
			}
			var version int64
			if err := v.store.QueryRow(auditContext(t), `SELECT version FROM agenteam_secret.secrets WHERE id=$1`, beforeResult.Ref.Details().ID.String()).Scan(&version); err != nil || version != 1 {
				t.Fatal("canonical escaped rollback", err)
			}
		})
	}
}
