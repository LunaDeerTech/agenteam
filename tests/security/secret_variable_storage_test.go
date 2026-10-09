//go:build integration

package security_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
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
	for _, mode := range []string{"owner-tail", "audit-without-witness", "audit-wrong-payload-kind", "audit-wrong-payload-owner", "missing-lock"} {
		t.Run(mode, func(t *testing.T) {
			v := newSecretVariableStorageFixture(t)
			value := "before-rollback"
			initial := v.prepare(t, v.intent(t, sc.Create, "rollback-create", 0, &value), sc.ProjectVariableWriteNotObserved(), false)
			created, commit := v.apply(t, initial, nil)
			beforeResult := requireSecretVariableStored(t, created, commit, sc.ProjectVariableCreated, 1)
			before := v.counts(t)
			type snapshot struct {
				purpose, payload                  string
				version, master, revision         int64
				cipher, nonce, wrapped, wrapNonce []byte
			}
			readSnapshot := func(ctx context.Context, x postgres.SQLExecutor) (snapshot, error) {
				var s snapshot
				err := x.QueryRow(ctx, `SELECT s.purpose,s.version,p.payload_id::text,p.ciphertext,p.data_nonce,p.wrapped_dek,p.wrap_nonce,p.master_version,p.wrap_revision FROM agenteam_secret.secrets s JOIN agenteam_secret.secret_payloads p ON p.payload_id=s.current_payload_id WHERE s.id=$1`, beforeResult.Ref.Details().ID.String()).Scan(&s.purpose, &s.version, &s.payload, &s.cipher, &s.nonce, &s.wrapped, &s.wrapNonce, &s.master, &s.revision)
				return s, err
			}
			original, err := readSnapshot(auditContext(t), v.store)
			if err != nil {
				t.Fatal(err)
			}
			changed := "must-rollback-value"
			p := v.prepare(t, v.intent(t, sc.Update, "rollback-update", 7, &changed), created, false)
			projection, err := p.Preparation()
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := projection.Fields()
			if err != nil {
				t.Fatal(err)
			}
			ownerCalls, beforeCalls, checksBefore := 0, 0, v.ports.checks
			writesObserved, mutationApplied := false, false
			ownerFailure := errors.New("controlled-owner-tail-failure")
			var missingWitnessFailure error
			observeWrites := func(ctx context.Context, tx f.Tx, auditWritten bool) error {
				x, err := v.store.InTx(tx)
				if err != nil {
					return err
				}
				now, err := readSnapshot(ctx, x)
				if err != nil {
					return err
				}
				var receiptCount, auditCount int
				if err = x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$2 AND action='secret.update')`, prepared.ReceiptID.String(), v.project.String()).Scan(&receiptCount, &auditCount); err != nil {
					return err
				}
				wantAudit := 0
				if auditWritten {
					wantAudit = 1
				}
				if now.version != 2 || now.purpose != "project_variable" || now.payload == original.payload || bytes.Equal(now.cipher, original.cipher) || receiptCount != 1 || auditCount != wantAudit {
					return errors.New("target native writes not reached")
				}
				writesObserved = true
				return nil
			}
			var after func(context.Context, f.Tx) error
			if mode == "owner-tail" {
				after = func(ctx context.Context, tx f.Tx) error {
					ownerCalls++
					if err := observeWrites(ctx, tx, true); err != nil {
						return err
					}
					return f.NewFault(f.InvalidState, f.NotStarted).WithCause(ownerFailure)
				}
			}
			if mode == "audit-without-witness" || mode == "audit-wrong-payload-kind" || mode == "audit-wrong-payload-owner" {
				v.ports.before = func(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (ac.Entry, ac.AppendKey, error) {
					beforeCalls++
					if err := observeWrites(ctx, tx, false); err != nil {
						return entry, key, err
					}
					if mode == "audit-without-witness" {
						missingWitnessFailure = v.ports.checker.CheckProjectAuditInTx(context.Background(), tx, entry, key)
						return entry, key, missingWitnessFailure
					}
					x, err := v.store.InTx(tx)
					if err != nil {
						return entry, key, err
					}
					query := `UPDATE agenteam_secret.secret_payloads SET owner_kind=2 WHERE payload_id=(SELECT digest_payload_id FROM agenteam_secret.project_variable_receipts WHERE project_id=$1 AND command_digest=$2)`
					args := []any{v.project.String(), key.Details().CauseRef}
					wrongOwner := newID[sc.ProjectVariableReceipt](t).String()
					if mode == "audit-wrong-payload-owner" {
						query = `UPDATE agenteam_secret.secret_payloads SET owner_id=$3::uuid WHERE payload_id=(SELECT digest_payload_id FROM agenteam_secret.project_variable_receipts WHERE project_id=$1 AND command_digest=$2)`
						args = append(args, wrongOwner)
					}
					tag, err := x.Exec(ctx, query, args...)
					if err != nil {
						return entry, key, err
					}
					if tag.RowsAffected() != 1 {
						return entry, key, errors.New("tamper did not update original receipt payload")
					}
					var kind int16
					var owner string
					if err = x.QueryRow(ctx, `SELECT p.owner_kind,p.owner_id::text FROM agenteam_secret.secret_payloads p JOIN agenteam_secret.project_variable_receipts r ON r.digest_payload_id=p.payload_id WHERE r.id=$1`, prepared.ReceiptID.String()).Scan(&kind, &owner); err != nil {
						return entry, key, err
					}
					if mode == "audit-wrong-payload-owner" && (kind != 3 || owner != wrongOwner) || mode == "audit-wrong-payload-kind" && (kind != 2 || owner != prepared.ReceiptID.String()) {
						return entry, key, errors.New("wrong stored native tamper")
					}
					mutationApplied = true
					return entry, key, nil
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
			fault := commit.Fault()
			switch mode {
			case "owner-tail":
				requireCode(t, fault, f.InvalidState)
				if ownerCalls != 1 || !writesObserved || !errors.Is(fault, ownerFailure) || v.ports.checks != checksBefore+1 {
					t.Fatal("owner-tail failure target not reached")
				}
			case "missing-lock":
				// WithinTx rejects the original poisoned LockNotHeld before the callback wrapper.
				requireCode(t, fault, f.InternalError)
				var pg *postgres.Error
				if !errors.As(fault, &pg) || pg.Code() != postgres.LockNotHeld || beforeCalls != 0 || ownerCalls != 0 || v.ports.checks != checksBefore {
					t.Fatal("missing-lock fault origin differs")
				}
			default:
				requireCode(t, fault, f.DependencyUnavailable)
				forbidden := false
				for cause := error(fault); cause != nil; cause = errors.Unwrap(cause) {
					if value, ok := cause.(*f.Fault); ok && value.Code == f.Forbidden {
						forbidden = true
					}
				}
				if beforeCalls != 1 || !writesObserved || !forbidden {
					t.Fatal("native Audit denial target not reached")
				}
				if mode == "audit-without-witness" {
					requireCode(t, missingWitnessFailure, f.Forbidden)
					if !errors.Is(fault, missingWitnessFailure) || v.ports.checks != checksBefore {
						t.Fatal("missing witness failure not propagated")
					}
				} else if !mutationApplied || v.ports.checks != checksBefore+1 {
					t.Fatal("native checker did not reject stored wrong tuple")
				}
			}
			if v.counts(t) != before {
				t.Fatal("D04/Audit partial commit")
			}
			restored, err := readSnapshot(auditContext(t), v.store)
			if err != nil || restored.purpose != original.purpose || restored.version != original.version || restored.payload != original.payload || restored.master != original.master || restored.revision != original.revision || !bytes.Equal(restored.cipher, original.cipher) || !bytes.Equal(restored.nonce, original.nonce) || !bytes.Equal(restored.wrapped, original.wrapped) || !bytes.Equal(restored.wrapNonce, original.wrapNonce) {
				t.Fatal("old canonical payload identity/bytes escaped rollback", err)
			}
		})
	}
}
