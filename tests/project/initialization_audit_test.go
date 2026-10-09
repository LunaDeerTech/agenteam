//go:build integration

package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestProjectInitializationAuditFacts(t *testing.T) {
	f := newInitializationConvergencePG(t)
	for _, action := range []audit.Action{audit.ObjectUploadComplete, audit.ObjectUploadFailed, audit.ObjectDelete} {
		for _, state := range []c.CreationState{c.CreationAccepted, c.CreationInitializing, c.CreationFailed, c.CreationCompleted} {
			t.Run(string(action)+"/"+string(state), func(t *testing.T) {
				v := newInitializationAuditPGCase(t, f, state, action)
				want, calls := foundation.Code(""), 1
				if action == audit.ObjectUploadComplete && (state == c.CreationAccepted || state == c.CreationFailed) {
					want, calls = foundation.InvalidState, 0
				}
				v.check(t, f, want, calls)
				v.check(t, f, want, calls)
			})
		}
	}
	for _, tc := range []struct {
		name, sql string
		arg       func(*testing.T, initializationConvergenceCase) any
		want      foundation.Code
	}{
		{"owner", `UPDATE agenteam_project.projects SET owner_user_id=$2 WHERE id=$1`, func(t *testing.T, _ initializationConvergenceCase) any { return id[identity.User](t).String() }, foundation.DependencyUnavailable},
		{"description", `UPDATE agenteam_project.projects SET description=$2 WHERE id=$1`, func(*testing.T, initializationConvergenceCase) any { return "Changed before initialization" }, foundation.DependencyUnavailable},
		{"initialized", `UPDATE agenteam_project.projects SET initialized_at=clock_timestamp() WHERE id=$1 AND version=$2`, func(*testing.T, initializationConvergenceCase) any { return int64(1) }, foundation.DependencyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadFailed)
			f.change(t, v.v, tc.sql, v.v.request.ProjectID.String(), tc.arg(t, v.v))
			v.check(t, f, tc.want, 0)
		})
	}
	t.Run("reverse_creation_before_deferred_constraint", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		before := f.snapshot(t, v.v)
		reached := false
		var gateError error
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.v.request.ProjectID, foundation.Exclusive)}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			// The FK is formally deferred. Reach the gate with contradictory
			// caller-owned facts, then roll back; never disable a constraint.
			if _, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET creation_id=$2 WHERE id=$1`, v.v.request.ProjectID.String(), id[c.Creation](t).String()); err != nil {
				return err
			}
			reached = true
			gateError = v.append(ctx, tx)
			return gateError
		})
		if !reached {
			t.Fatal("did not reach wrapper before deferred FK", result.Fault())
		}
		requireCode(t, gateError, foundation.DependencyUnavailable)
		if result.State() != foundation.NotCommitted || v.facts.calls != 0 || before != f.snapshot(t, v.v) {
			t.Fatal("contradictory caller transaction escaped rollback")
		}
	})
	for _, lifecycle := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		t.Run("lifecycle_"+string(lifecycle), func(t *testing.T) {
			v := newInitializationAuditPGCase(t, f, c.CreationCompleted, audit.ObjectUploadFailed)
			if lifecycle == c.Archived {
				f.change(t, v.v, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=created_at WHERE id=$1`, v.v.request.ProjectID.String())
			} else {
				operation := id[c.Operation](t)
				action := "archive"
				if lifecycle == c.Deleting {
					action = "delete"
				}
				f.change(t, v.v, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,$4,1,'accepted',1,'[]'::jsonb,'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',clock_timestamp(),clock_timestamp())`, operation.String(), v.v.request.ProjectID.String(), v.v.owner.String(), action)
				f.change(t, v.v, `UPDATE agenteam_project.projects SET lifecycle=$2,current_lifecycle_operation_id=$3 WHERE id=$1`, v.v.request.ProjectID.String(), string(lifecycle), operation.String())
			}
			v.check(t, f, foundation.Forbidden, 0)
		})
	}
	for _, kind := range []string{"missing_creation", "foreign_creation", "object_cause"} {
		t.Run(kind, func(t *testing.T) {
			v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
			if kind == "object_cause" {
				var err error
				v.key, err = audit.NewAppendKey(audit.ObjectProducer, id[struct{}](t).String(), 0)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				creation := id[c.Creation](t)
				if kind == "foreign_creation" {
					creation = f.seed(t, c.CreationInitializing).request.CreationID
				}
				fields := v.entry.Fields()
				metadata, err := audit.ObjectMetadata(fields.Action, audit.ObjectMetadataFields{ObjectID: fields.Resource.Details().ID, InitiatorKind: identity.Service, InitiatorID: creation.String(), MediaType: "text/plain", ByteSize: 7, Phase: audit.PublishedPhase})
				if err != nil {
					t.Fatal(err)
				}
				fields.Metadata = metadata
				v.entry, err = audit.NewEntry(fields)
				if err != nil {
					t.Fatal(err)
				}
			}
			v.check(t, f, foundation.Forbidden, 0)
		})
	}
	t.Run("completed_current_version_and_name", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationCompleted, audit.ObjectUploadComplete)
		f.change(t, v.v, `UPDATE agenteam_project.projects SET name='Later',normalized_name='later',description='Later description',version=3 WHERE id=$1`, v.v.request.ProjectID.String())
		v.check(t, f, "", 1)
	})
	t.Run("different_original_initialization_key", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		v.facts.request.InitializationKey = "another-valid-initialization"
		v.check(t, f, foundation.Forbidden, 1)
	})
	t.Run("command_key_is_not_initialization_key", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		var commandKey string
		if err := f.store.QueryRow(ctxFor(t), `SELECT command_key FROM agenteam_project.creations WHERE id=$1`, v.v.request.CreationID.String()).Scan(&commandKey); err != nil {
			t.Fatal(err)
		}
		if commandKey == string(v.v.request.InitializationKey) {
			t.Fatal("fixture failed to distinguish keys")
		}
		v.facts.request.InitializationKey = foundation.IdempotencyKey(commandKey)
		v.check(t, f, foundation.Forbidden, 1)
	})
	t.Run("different_original_creation", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		other := f.seed(t, c.CreationInitializing)
		v.facts.request.CreationID = other.request.CreationID
		v.check(t, f, foundation.Forbidden, 1)
	})
}

func TestProjectInitializationAuditTransactionBoundary(t *testing.T) {
	f := newInitializationConvergencePG(t)
	for _, kind := range []string{"missing", "shared", "other_project", "foreign_store", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
			before := f.snapshot(t, v.v)
			store := f.store
			locks := []foundation.LockRequest{convergenceLock(v.v.request.ProjectID, foundation.Exclusive)}
			switch kind {
			case "missing":
				locks = nil
			case "shared":
				locks[0].Mode = foundation.Shared
			case "other_project":
				locks[0] = convergenceLock(id[identity.Project](t), foundation.Exclusive)
			case "foreign_store":
				store = f.other
			}
			var gateError error
			reached := false
			result := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if len(locks) > 0 {
					if err := store.AcquireAll(ctx, tx, locks); err != nil {
						return err
					}
				}
				if kind == "cancelled" {
					child, cancel := context.WithCancel(ctx)
					cancel()
					ctx = child
				}
				reached = true
				gateError = v.append(ctx, tx)
				return nil // Ignoring a rejected held-lock check cannot unpoison real Tx.
			})
			if !reached {
				t.Fatal("did not reach wrapper", result.State(), result.Fault())
			}
			requireCode(t, gateError, foundation.DependencyUnavailable)
			if result.State() != foundation.NotCommitted || v.facts.calls != 0 || before != f.snapshot(t, v.v) {
				t.Fatal("bad Tx/lock passed, wrote or escaped poison", result.State())
			}
		})
	}
	t.Run("expired", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		var expired foundation.Tx
		convergenceCommitted(t, f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			expired = tx
			return f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.v.request.ProjectID, foundation.Exclusive)})
		}))
		before := f.snapshot(t, v.v)
		requireCode(t, v.append(ctxFor(t), expired), foundation.DependencyUnavailable)
		if v.facts.calls != 0 || before != f.snapshot(t, v.v) {
			t.Fatal("expired Tx used provider or wrote facts")
		}
	})
	t.Run("closed_store", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		closed := convergenceOpenStore(t, f.db)
		accountKeys, _ := keys(t)
		accounts, err := account.NewAuthority(closed, accountKeys)
		if err != nil {
			t.Fatal(err)
		}
		authority, err := project.NewAuthority(closed, project.AuthorityDependencies{Sessions: accounts})
		if err != nil {
			t.Fatal(err)
		}
		gate, err := project.NewInitializationAuditAuthority(authority, v.facts)
		if err != nil {
			t.Fatal(err)
		}
		var expired foundation.Tx
		convergenceCommitted(t, closed.WithinTx(ctxFor(t), cause(t), func(_ context.Context, tx foundation.Tx) error { expired = tx; return nil }))
		if err = closed.Drain(ctxFor(t)); err != nil {
			t.Fatal(err)
		}
		requireCode(t, gate.CheckAppendInTx(ctxFor(t), expired, v.entry, v.key), foundation.DependencyUnavailable)
		if v.facts.calls != 0 {
			t.Fatal("closed Store called provider")
		}
	})
	for _, mode := range []foundation.LockMode{foundation.Shared, foundation.Exclusive} {
		name := "shared"
		if mode == foundation.Exclusive {
			name = "exclusive"
		}
		t.Run("retains_ex_against_"+name, func(t *testing.T) { initializationAuditLockSurvives(t, f, mode) })
	}
}

func TestProjectInitializationAuditDelegation(t *testing.T) {
	f := newInitializationConvergencePG(t)
	for _, kind := range []string{"fault", "plain_error", "cancel", "query_error"} {
		t.Run(kind, func(t *testing.T) {
			v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
			want := foundation.DependencyUnavailable
			switch kind {
			case "fault":
				v.facts.err = foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
				want = foundation.Forbidden
			case "plain_error":
				v.facts.err = errors.New("controlled upstream failure")
			case "cancel":
				v.facts.err = context.Canceled
			case "query_error":
				// Fail a real statement through the provider's original Tx.
				v.facts.check = func(ctx context.Context, tx foundation.Tx) error {
					x, err := f.store.InTx(tx)
					if err != nil {
						return err
					}
					_, err = x.Exec(ctx, `SELECT 1/0`)
					return err
				}
			}
			v.check(t, f, want, 1)
		})
	}
	t.Run("real_object_checker_requires_private_witness", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		realFacts, err := object.NewProjectAuditAuthority(f.store)
		if err != nil {
			t.Fatal(err)
		}
		v.gate, err = project.NewInitializationAuditAuthority(f.authority, realFacts)
		if err != nil {
			t.Fatal(err)
		}
		v.check(t, f, foundation.Forbidden, 0)
	})
	t.Run("caller_rollback_after_success", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		before := f.snapshot(t, v.v)
		rollback := errors.New("caller rejects after successful gate")
		var gateError error
		reached := false
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.v.request.ProjectID, foundation.Exclusive)}); err != nil {
				return err
			}
			reached = true
			gateError = v.append(ctx, tx)
			if gateError != nil {
				return gateError
			}
			return rollback
		})
		if !reached || gateError != nil || v.facts.calls != 1 || result.State() != foundation.NotCommitted || before != f.snapshot(t, v.v) {
			t.Fatal("wrapper changed caller rollback ownership", result.State(), gateError)
		}
	})
	t.Run("ordinary_unbound_methods", func(t *testing.T) {
		v := newInitializationAuditPGCase(t, f, c.CreationInitializing, audit.ObjectUploadComplete)
		requireCode(t, v.gate.CheckServiceLookup(ctxFor(t), v.v.actor, v.entry.Fields().Scope, v.key), foundation.DependencyUnbound)
		requireCode(t, v.gate.CheckCleanupInTx(ctxFor(t), foundation.NewTx(), v.v.actor, audit.LifecycleCause{}, v.v.request.ProjectID), foundation.DependencyUnbound)
		if v.facts.calls != 0 {
			t.Fatal("ordinary methods used initialization facts")
		}
	})
}
