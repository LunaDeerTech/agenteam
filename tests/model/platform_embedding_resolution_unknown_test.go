//go:build integration

package model_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func (v *platformEmbeddingResolutionFixture) outcome(t *testing.T, r mc.ResolveRequest) (int, string, int, int, int) {
	t.Helper()
	var preparations, snapshots, bindings, leases int
	var phase string
	e := v.raw.QueryRow(testContext(t), `SELECT
 (SELECT count(*) FROM agenteam_model.resolution_preparations WHERE owner_id=$1),
 coalesce((SELECT phase FROM agenteam_model.resolution_preparations WHERE owner_id=$1),''),
 (SELECT count(*) FROM agenteam_model.snapshots s JOIN agenteam_model.resolution_preparations p ON p.snapshot_id=s.id WHERE p.owner_id=$1),
 (SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE owner_id=$1),
 (SELECT count(*) FROM agenteam_secret.secret_leases l JOIN agenteam_model.snapshot_bindings b ON b.lease_id=l.id WHERE b.owner_id=$1)`, r.LeaseOwner.Details().ID).Scan(&preparations, &phase, &snapshots, &bindings, &leases)
	if e != nil {
		t.Fatal(e)
	}
	return preparations, phase, snapshots, bindings, leases
}

func TestModelPlatformEmbeddingResolutionUnknown(t *testing.T) {
	v := newPlatformEmbeddingResolutionFixture(t)
	_, target := v.embeddingConfig(t, true)
	v.choose(t, target.ID)
	for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
		t.Run(string(purpose), func(t *testing.T) {
			for _, stage := range []int{2, 3} {
				for _, rollback := range []bool{false, true} {
					t.Run(fmt.Sprintf("actual-result-decoration-stage-%d-rollback-%v", stage, rollback), func(t *testing.T) {
						r := v.request(t, purpose)
						v.arm(t, stage, rollback, true, nil)
						var e error
						if stage == 2 {
							p, err := v.resolving.DiscoverResolve(testContext(t), r)
							e = err
							resolutionZeroPlan(t, p, e)
						} else {
							out, err := v.resolving.ResolveModel(testContext(t), r)
							e = err
							resolutionNoResult(t, out, e)
						}
						var unknown *model.UnknownCommandError
						if !errors.As(e, &unknown) {
							t.Fatal("original Unknown lost", e)
						}
						v.observed.mu.Lock()
						actual, returned := v.observed.actual, v.observed.returned
						v.observed.target = 0
						v.observed.mu.Unlock()
						if unknown.AttemptID() != returned.AttemptID() || unknown.Cause().Details().Primary.Canonical() != returned.Cause().Details().Primary.Canonical() || returned.State() != f.Unknown {
							t.Fatal("Unknown attempt/cause replaced")
						}
						wantState := f.Committed
						if rollback {
							wantState = f.NotCommitted
						}
						if actual.State() != wantState {
							t.Fatal("actual Store outcome differs", actual.State())
						}
						preparations, phase, snapshots, bindings, leases := v.outcome(t, r)
						wantPrep, wantPhase, wantFinal := 1, "prepared", 0
						if stage == 2 && rollback {
							wantPrep, wantPhase = 0, ""
						}
						if stage == 3 && !rollback {
							wantPhase, wantFinal = "committed", 1
						}
						if preparations != wantPrep || phase != wantPhase || snapshots != wantFinal || bindings != wantFinal || leases != wantFinal {
							t.Fatalf("actual outcome prep=%d/%s snapshot/binding/lease=%d/%d/%d", preparations, phase, snapshots, bindings, leases)
						}
						out := v.resolve(t, r)
						if out.Validate() != nil {
							t.Fatal("same-key resolution failed")
						}
						preparations, phase, snapshots, bindings, leases = v.outcome(t, r)
						if preparations != 1 || phase != "committed" || snapshots != 1 || bindings != 1 || leases != 1 {
							t.Fatal("same-key confirmation duplicated or omitted final facts")
						}
						t.Logf("formal result decoration stage=%d actual=%s returned=%s; original attempt/cause retained; before-confirm final=%d", stage, actual.State(), returned.State(), wantFinal)
					})
				}
			}
			for _, stage := range []int{2, 3} {
				t.Run(fmt.Sprintf("actual-commit-before-delivery-cancel-stage-%d", stage), func(t *testing.T) {
					r := v.request(t, purpose)
					ctx, cancel := context.WithCancel(testContext(t))
					defer cancel()
					v.arm(t, stage, false, false, cancel)
					var e error
					if stage == 2 {
						p, err := v.resolving.DiscoverResolve(ctx, r)
						e = err
						resolutionZeroPlan(t, p, e)
					} else {
						out, err := v.resolving.ResolveModel(ctx, r)
						e = err
						resolutionNoResult(t, out, e)
					}
					if !errors.Is(e, context.Canceled) {
						t.Fatal("delivery cancellation lost", e)
					}
					v.observed.mu.Lock()
					actual := v.observed.actual
					v.observed.target = 0
					v.observed.after = nil
					v.observed.mu.Unlock()
					preparations, phase, snapshots, bindings, leases := v.outcome(t, r)
					want, wantPhase := 0, "prepared"
					if stage == 3 {
						want, wantPhase = 1, "committed"
					}
					if actual.State() != f.Committed || preparations != 1 || phase != wantPhase || snapshots != want || bindings != want || leases != want {
						t.Fatal("delivery cancellation rewrote committed facts", actual.State(), phase)
					}
					v.resolve(t, r)
				})
			}
			t.Run("missing-row-cannot-pass-original-command-writer", func(t *testing.T) {
				r := v.request(t, purpose)
				v.arm(t, 2, true, true, nil)
				p, e := v.resolving.DiscoverResolve(testContext(t), r)
				resolutionZeroPlan(t, p, e)
				v.observed.mu.Lock()
				returned, actual := v.observed.returned, v.observed.actual
				v.observed.target = 0
				v.observed.mu.Unlock()
				if returned.State() != f.Unknown || actual.State() != f.NotCommitted {
					t.Fatal("missing-row Unknown setup")
				}
				preparations, _, snapshots, bindings, leases := v.outcome(t, r)
				if preparations != 0 || snapshots != 0 || bindings != 0 || leases != 0 {
					t.Fatal("rolled-back writer left facts")
				}
				key, e := f.CommandLock(returned.Cause().Details().Primary)
				if e != nil {
					t.Fatal(e)
				}
				holder, release := v.hold(t, f.LockRequest{Key: key, Mode: f.Exclusive})
				var out mc.ResolvedModel
				pending := platformEmbeddingStart(t, func(ctx context.Context) error { var e error; out, e = v.resolving.ResolveModel(ctx, r); return e })
				v.blocked(t, holder, pending)
				pending.cancel()
				e = pending.wait()
				resolutionNoResult(t, out, e)
				preparations, _, snapshots, bindings, leases = v.outcome(t, r)
				if preparations != 0 || snapshots != 0 || bindings != 0 || leases != 0 {
					t.Fatal("unconfirmed writer barrier published state")
				}
				release()
				v.resolve(t, r)
				t.Log("real rollback then decorated Unknown; actual Command EX holder/waiter observed; canceled waiter and holder both actually joined before same-key confirmation; no physical wire ACK-loss claim")
			})
		})
	}
	v.noSecretRead(t)
}
