//go:build integration

package project_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	idc "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Independently selected assertions. The accepted low-level Project/Creation
// seed is reused, but none of the new author's case/check/delegate helpers are.
// This is a strict controlled delegate, not an actual Skill/Object provider.
type independentInitializationFacts func(context.Context, f.Tx, au.Entry, au.AppendKey) error

func (p independentInitializationFacts) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) error {
	return p(ctx, tx, entry, key)
}

func independentInitializationEntry(t *testing.T, request pc.InitializationRequest, failed bool) (au.Entry, au.AppendKey) {
	t.Helper()
	scope, err := idc.InProject(request.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	operation, object := id[struct{}](t).String(), id[struct{}](t).String()
	registration, err := idc.RegisterService(idc.ObjectService)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(operation, scope)
	if err != nil {
		t.Fatal(err)
	}
	action, outcome, ordinal := au.ObjectUploadComplete, au.Success, int64(0)
	fields := au.ObjectMetadataFields{ObjectID: object, InitiatorKind: idc.Service, InitiatorID: request.CreationID.String(), MediaType: "text/plain", ByteSize: 11, Phase: au.PublishedPhase}
	if failed {
		action, outcome, ordinal = au.ObjectUploadFailed, au.Unknown, 1
		fields.Phase, fields.Reason = au.FailedPhase, au.StorageUnavailable
	}
	metadata, err := au.ObjectMetadata(action, fields)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := au.NewResource(au.ObjectResource, object)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := au.NewEntry(au.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: outcome, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := au.NewAppendKey(au.ObjectProducer, operation, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	if operation == request.CreationID.String() {
		t.Fatal("Object operation confused with Creation")
	}
	return entry, key
}

func independentInitializationCode(err error, code f.Code) bool {
	var known *f.Fault
	return errors.As(err, &known) && known.Code == code
}

func TestIndependentProjectInitializationAuditBoundaries(t *testing.T) {
	pg := newInitializationConvergencePG(t)
	for _, kind := range []string{"owner_recheck", "state_recheck", "unknown_delegate"} {
		t.Run(kind, func(t *testing.T) {
			seed := pg.seed(t, pc.CreationInitializing)
			complete, completeKey := independentInitializationEntry(t, seed.request, false)
			failed, failedKey := independentInitializationEntry(t, seed.request, true)
			entry, key := complete, completeKey
			before := pg.snapshot(t, seed)
			rollback := errors.New("independent caller rollback")
			providerCause := errors.New("controlled unresolved provider outcome")
			unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(providerCause)
			unknown.CauseID = id[f.TransactionAttempt](t).String()
			unknown.RetryHint = "lookup"
			type contextMarker struct{}
			marker := new(int)
			var expectedCtx context.Context
			var expectedTx f.Tx
			var physical int64
			calls := 0
			provider := independentInitializationFacts(func(ctx context.Context, tx f.Tx, e au.Entry, k au.AppendKey) error {
				calls++
				want, got := entry.Fields(), e.Fields()
				if ctx != expectedCtx || ctx.Value(contextMarker{}) != marker || tx != expectedTx || !got.Actor.Equal(want.Actor) || !got.Scope.Equal(want.Scope) || got.Action != want.Action || got.Outcome != want.Outcome || got.Resource.Details() != want.Resource.Details() || got.Associations != want.Associations || !bytes.Equal(got.Metadata.JSON(), want.Metadata.JSON()) || k.Details() != key.Details() {
					return errors.New("independent original arguments changed")
				}
				x, err := pg.store.InTx(tx)
				if err != nil {
					return err
				}
				var current int64
				if err = x.QueryRow(ctx, `SELECT txid_current()`).Scan(&current); err != nil {
					return err
				}
				if current != physical {
					return errors.New("delegate used another physical transaction")
				}
				if kind == "unknown_delegate" {
					return unknown
				}
				return nil
			})
			gate, err := project.NewInitializationAuditAuthority(pg.authority, provider)
			if err != nil {
				t.Fatal(err)
			}
			completed := false
			result := pg.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
				if err := pg.store.AcquireAll(ctx, tx, []f.LockRequest{convergenceLock(seed.request.ProjectID, f.Exclusive)}); err != nil {
					return err
				}
				x, err := pg.store.InTx(tx)
				if err != nil {
					return err
				}
				if err = x.QueryRow(ctx, `SELECT txid_current()`).Scan(&physical); err != nil {
					return err
				}
				expectedCtx = context.WithValue(ctx, contextMarker{}, marker)
				expectedTx = tx
				first := gate.CheckAppendInTx(expectedCtx, tx, entry, key)
				if kind == "unknown_delegate" {
					if first != unknown || unknown.CommitState != f.Unknown || unknown.Code != f.CommitUnknown || unknown.CauseID == "" || unknown.RetryHint != "lookup" || !errors.Is(first, providerCause) || calls != 1 {
						return errors.New("Unknown provider result changed or retried")
					}
					completed = true
					return rollback
				}
				if first != nil || calls != 1 {
					return fmt.Errorf("first exact gate rejected: %w", first)
				}
				if kind == "owner_recheck" {
					foreign := id[idc.User](t)
					if _, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2 WHERE id=$1`, seed.request.ProjectID.String(), foreign.String()); err != nil {
						return err
					}
					if err = gate.CheckAppendInTx(expectedCtx, tx, entry, key); !independentInitializationCode(err, f.DependencyUnavailable) || calls != 1 {
						return errors.New("cached grant ignored same-Tx owner contradiction")
					}
					if _, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2 WHERE id=$1`, seed.request.ProjectID.String(), seed.owner.String()); err != nil {
						return err
					}
				} else {
					if _, err = x.Exec(ctx, `UPDATE agenteam_project.creations SET state='failed',safe_reason='operation_failed' WHERE id=$1`, seed.request.CreationID.String()); err != nil {
						return err
					}
					if err = gate.CheckAppendInTx(expectedCtx, tx, entry, key); !independentInitializationCode(err, f.InvalidState) || calls != 1 {
						return errors.New("cached completion grant ignored same-Tx failed state")
					}
					entry, key = failed, failedKey
				}
				if err = gate.CheckAppendInTx(expectedCtx, tx, entry, key); err != nil || calls != 2 {
					return fmt.Errorf("restored or convergence gate rejected: %w", err)
				}
				completed = true
				return rollback
			})
			if !completed {
				t.Fatal("independent probe did not reach all assertions", result.State(), result.Fault())
			}
			if result.State() != f.NotCommitted || pg.snapshot(t, seed) != before {
				t.Fatal("wrapper changed caller rollback ownership or persisted facts")
			}
		})
	}
	t.Run("ended_transaction", func(t *testing.T) {
		seed := pg.seed(t, pc.CreationInitializing)
		entry, key := independentInitializationEntry(t, seed.request, false)
		before := pg.snapshot(t, seed)
		var original f.Tx
		calls := 0
		gate, err := project.NewInitializationAuditAuthority(pg.authority, independentInitializationFacts(func(ctx context.Context, tx f.Tx, got au.Entry, gotKey au.AppendKey) error {
			calls++
			if tx != original || !got.Fields().Actor.Equal(entry.Fields().Actor) || gotKey.Details() != key.Details() {
				return errors.New("ended-Tx control changed original arguments")
			}
			_, err := pg.store.InTx(tx)
			return err
		}))
		if err != nil {
			t.Fatal(err)
		}
		result := pg.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			original = tx
			if err := pg.store.AcquireAll(ctx, tx, []f.LockRequest{convergenceLock(seed.request.ProjectID, f.Exclusive)}); err != nil {
				return err
			}
			return gate.CheckAppendInTx(ctx, tx, entry, key)
		})
		if result.State() != f.Committed || calls != 1 || !original.Valid() {
			t.Fatal("live transaction positive control did not commit", result.State(), result.Fault(), calls)
		}
		if _, err := pg.store.InTx(original); err == nil {
			t.Fatal("original physical transaction still usable after caller commit")
		}
		if err := gate.CheckAppendInTx(ctxFor(t), original, entry, key); !independentInitializationCode(err, f.DependencyUnavailable) || calls != 1 {
			t.Fatal("ended transaction reused its former Project EX authority", err, calls)
		}
		if pg.snapshot(t, seed) != before {
			t.Fatal("read-only live/ended transaction control changed persisted facts")
		}
	})
}
