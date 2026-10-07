//go:build integration

package model_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelMeetingSummaryResolutionUnknown(t *testing.T) {
	v := newMeetingResolutionFixture(t, true)
	_, m, _ := v.config(t, false, true)
	v.choose(t, m.ID)
	for _, stage := range []int{2, 3} {
		for _, rollback := range []bool{false, true} {
			t.Run(fmt.Sprintf("result-decoration-stage-%d-rollback-%v", stage, rollback), func(t *testing.T) {
				r := v.request(t, mc.MeetingSummaryInitial)
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
					t.Fatal("original Unknown projection lost", e)
				}
				v.observed.mu.Lock()
				actual, returned := v.observed.actual, v.observed.returned
				v.observed.target = 0
				v.observed.mu.Unlock()
				if unknown.AttemptID() != returned.AttemptID() || unknown.Cause().Details().Primary.Canonical() != returned.Cause().Details().Primary.Canonical() || returned.State() != f.Unknown {
					t.Fatal("returned attempt/cause replaced")
				}
				expected := f.Committed
				if rollback {
					expected = f.NotCommitted
				}
				if actual.State() != expected {
					t.Fatal("decoration actual Store mismatch", actual.State())
				}
				var snapshots int
				if e = v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE owner_id=$1`, r.LeaseOwner.Details().ID).Scan(&snapshots); e != nil {
					t.Fatal(e)
				}
				want := 0
				if stage == 3 && !rollback {
					want = 1
				}
				if snapshots != want {
					t.Fatal("actual PG outcome differs", snapshots, want)
				}
				out := v.resolve(t, r)
				if out.Validate() != nil {
					t.Fatal("correct reentry failed")
				}
				t.Logf("formal result decoration only: stage=%d actual=%s returned=%s; canonical bindings before retry=%d", stage, actual.State(), returned.State(), snapshots)
			})
		}
	}
	t.Run("committed-then-caller-cancelled-zero-publication", func(t *testing.T) {
		r := v.request(t, mc.MeetingSummaryInitial)
		ctx, cancel := context.WithCancel(testContext(t))
		defer cancel()
		v.arm(t, 3, false, false, cancel)
		out, e := v.resolving.ResolveModel(ctx, r)
		resolutionNoResult(t, out, e)
		if !errors.Is(e, context.Canceled) {
			t.Fatal("caller cancellation lost", e)
		}
		v.observed.mu.Lock()
		actual := v.observed.actual
		v.observed.target = 0
		v.observed.after = nil
		v.observed.mu.Unlock()
		var count int
		if e = v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE owner_id=$1`, r.LeaseOwner.Details().ID).Scan(&count); e != nil || count != 1 || actual.State() != f.Committed {
			t.Fatal("committed facts rewritten on delivery cancellation", count, actual.State(), e)
		}
	})
	t.Run("unknown-rollback-missing-row-still-waits-original-writer", func(t *testing.T) {
		r := v.request(t, mc.MeetingSummaryInitial)
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
		var count int
		if e = v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.resolution_preparations WHERE owner_id=$1`, r.LeaseOwner.Details().ID).Scan(&count); e != nil || count != 0 {
			t.Fatal("original rolled-back writer left a row", e)
		}
		key, e := f.CommandLock(returned.Cause().Details().Primary)
		if e != nil {
			t.Fatal(e)
		}
		release := v.hold(t, f.LockRequest{Key: key, Mode: f.Exclusive})
		ctx, cancel := context.WithTimeout(testContext(t), 150*time.Millisecond)
		out, e := v.resolving.ResolveModel(ctx, r)
		cancel()
		resolutionNoResult(t, out, e)
		release()
		v.resolve(t, r)
		t.Log("real rollback then decorated Unknown; missing preparation still waits original Command EX; ordinary holder actually joined; no wire ACK fault")
	})
}
