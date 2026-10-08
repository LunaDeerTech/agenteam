//go:build integration

package project_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Private independent representatives. The fixture/seed/snapshot are explicitly
// inherited from frozen author candidate03, not independent implementations.
// No Skills initializer, Human authorization stub or shared resource is added.
func TestIndependentInitializationConvergenceTransactionBoundary(t *testing.T) {
	f := newInitializationConvergencePG(t)
	for _, mode := range []string{"foreign_live_token_rolls_back_caller_write", "shared_lock_poison", "cancel_after_success_rolls_back_caller_write"} {
		t.Run(mode, func(t *testing.T) {
			v := f.seed(t, c.CreationAccepted)
			before := f.snapshot(t, v)
			owner := f.store
			lockMode := foundation.Exclusive
			if mode == "foreign_live_token_rolls_back_caller_write" {
				owner = f.other
			}
			if mode == "shared_lock_poison" {
				lockMode = foundation.Shared
			}
			var gateError error
			entered, markerWritten, firstAccepted := false, false, false
			// ctxFor starts an independent bounded context, never the callback's
			// Tx marker. Foreign-token rejection must reach the actual gate.
			result := owner.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := owner.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.request.ProjectID, lockMode)}); err != nil {
					return err
				}
				if mode == "cancel_after_success_rolls_back_caller_write" {
					if err := f.authority.ValidateInitializationConvergenceInTx(ctx, tx, v.actor, v.request); err != nil {
						return err
					}
					firstAccepted = true
				}
				if mode != "shared_lock_poison" {
					x, err := owner.InTx(tx)
					if err != nil {
						return err
					}
					// Caller-owned, EX-protected marker, not a write by the gate.
					tag, err := x.Exec(ctx, `UPDATE agenteam_project.creations SET version=version+1 WHERE id=$1`, v.request.CreationID.String())
					if err != nil {
						return err
					}
					if tag.RowsAffected() != 1 {
						return foundation.NewFault(foundation.InternalError, foundation.NotStarted)
					}
					markerWritten = true
				}
				callContext := ctx
				if mode == "cancel_after_success_rolls_back_caller_write" {
					child, cancel := context.WithCancel(ctx)
					cancel()
					callContext = child
				}
				entered = true
				gateError = f.authority.ValidateInitializationConvergenceInTx(callContext, tx, v.actor, v.request)
				return nil // Ignore the error: genuine Store poison must roll back.
			})
			if !entered || (mode != "shared_lock_poison" && !markerWritten) {
				t.Fatal("fixture did not reach the intended gate after its local setup", result.State(), result.Fault())
			}
			if mode == "cancel_after_success_rolls_back_caller_write" && !firstAccepted {
				t.Fatal("initial successful check was not reached")
			}
			requireCode(t, gateError, foundation.DependencyUnavailable)
			if result.State() != foundation.NotCommitted {
				t.Fatal("ignored rejection did not poison the original caller transaction", result.State())
			}
			if f.snapshot(t, v) != before {
				t.Fatal("caller marker escaped rollback or the gate changed facts")
			}
			independentConvergenceObservation(t, f, v, "")
		})
	}
}

func TestIndependentInitializationConvergenceCanonicalFacts(t *testing.T) {
	f := newInitializationConvergencePG(t)
	for _, state := range []c.CreationState{c.CreationAccepted, c.CreationInitializing, c.CreationFailed, c.CreationCompleted} {
		t.Run(string(state), func(t *testing.T) {
			v := f.seed(t, state)
			// Each altered row remains SQL-shape valid; change requires an
			// actual Committed result before testing the authority's rejection.
			switch state {
			case c.CreationAccepted:
				f.change(t, v, `UPDATE agenteam_project.creations SET request_description='other valid original description' WHERE id=$1`, v.request.CreationID.String())
			case c.CreationInitializing:
				f.change(t, v, `UPDATE agenteam_project.creations SET owner_user_id=$2 WHERE id=$1`, v.request.CreationID.String(), id[identity.User](t).String())
			case c.CreationFailed:
				f.change(t, v, `UPDATE agenteam_project.projects SET initialized_at=created_at WHERE id=$1`, v.request.ProjectID.String())
			case c.CreationCompleted:
				bad := v.initial
				bad.OwnerUserID = id[identity.User](t)
				raw, err := json.Marshal(bad)
				if err != nil {
					t.Fatal(err)
				}
				f.change(t, v, `UPDATE agenteam_project.creations SET safe_result=$2::jsonb WHERE id=$1`, v.request.CreationID.String(), raw)
			}
			independentConvergenceObservation(t, f, v, foundation.DependencyUnavailable)
			switch state {
			case c.CreationAccepted:
				f.change(t, v, `UPDATE agenteam_project.creations SET request_description=$2 WHERE id=$1`, v.request.CreationID.String(), v.initial.Description)
			case c.CreationInitializing:
				f.change(t, v, `UPDATE agenteam_project.creations SET owner_user_id=$2 WHERE id=$1`, v.request.CreationID.String(), v.owner.String())
			case c.CreationFailed:
				f.change(t, v, `UPDATE agenteam_project.projects SET initialized_at=NULL WHERE id=$1`, v.request.ProjectID.String())
			case c.CreationCompleted:
				raw, err := json.Marshal(v.initial)
				if err != nil {
					t.Fatal(err)
				}
				f.change(t, v, `UPDATE agenteam_project.creations SET safe_result=$2::jsonb WHERE id=$1`, v.request.CreationID.String(), raw)
				f.change(t, v, `UPDATE agenteam_project.projects SET name='IndependentCurrent',normalized_name='independentcurrent',description='later independent description',version=9,updated_at=clock_timestamp() WHERE id=$1`, v.request.ProjectID.String())
			}
			independentConvergenceObservation(t, f, v, "")
			if state == c.CreationCompleted {
				var name string
				var version int64
				if err := f.store.QueryRow(ctxFor(t), `SELECT safe_result->>'name',(safe_result->>'version')::bigint FROM agenteam_project.creations WHERE id=$1`, v.request.CreationID.String()).Scan(&name, &version); err != nil {
					t.Fatal(err)
				}
				if name != v.initial.Name || version != 1 {
					t.Fatal("current metadata overwrote the initial historical snapshot")
				}
			}
		})
	}
}

func independentConvergenceObservation(t *testing.T, f *initializationConvergencePG, v initializationConvergenceCase, want foundation.Code) {
	t.Helper()
	before := f.snapshot(t, v)
	entered := false
	var gateError error
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.request.ProjectID, foundation.Exclusive)}); err != nil {
			return err
		}
		entered = true
		gateError = f.authority.ValidateInitializationConvergenceInTx(ctx, tx, v.actor, v.request)
		return gateError
	})
	if !entered {
		t.Fatal("gate was not reached after committed seed", result.State(), result.Fault())
	}
	if want == "" {
		if gateError != nil {
			t.Fatal(gateError)
		}
		convergenceCommitted(t, result)
	} else {
		requireCode(t, gateError, want)
		if result.State() != foundation.NotCommitted {
			t.Fatal("rejected observation committed", result.State())
		}
	}
	if f.snapshot(t, v) != before {
		t.Fatal("observation changed canonical facts or observed side-effect counts")
	}
}
