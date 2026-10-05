//go:build integration

package model_test

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (v *currentResolutionFixture) atomicFacts(t *testing.T, r mc.ResolveRequest, p mc.ResolutionPlan, want int) {
	t.Helper()
	var input, snapshot, binding, lease int
	e := v.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM model_resolution_fixture.inputs WHERE unit=$1),(SELECT count(*) FROM agenteam_model.snapshots WHERE id=$2),(SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE snapshot_id=$2),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE id=$3::uuid)`, resolutionUnit(r), p.Details().SnapshotID.String(), p.Details().LeaseID.String()).Scan(&input, &snapshot, &binding, &lease)
	if e != nil || input != want || snapshot != want || binding != want || lease != want {
		t.Fatalf("input/snapshot/binding/lease=%d/%d/%d/%d want %d: %v", input, snapshot, binding, lease, want, e)
	}
	var phase string
	if e = v.raw.QueryRow(testContext(t), `SELECT phase FROM agenteam_model.resolution_preparations WHERE snapshot_id=$1`, p.Details().SnapshotID.String()).Scan(&phase); e != nil || phase != map[int]string{0: "prepared", 1: "committed"}[want] {
		t.Fatal("preparation phase", phase, e)
	}
}
func TestModelCurrentResolutionAtomicity(t *testing.T) {
	v := newCurrentResolutionFixture(t)
	for _, projectScope := range []bool{false, true} {
		name := "system"
		if projectScope {
			name = "project"
		}
		t.Run(name, func(t *testing.T) {
			_, m, _ := v.config(t, projectScope, true)
			r := v.request(t, m, false)
			p := v.discover(t, r)
			if p.Details().LeaseID == nil {
				t.Fatal("planned ref lacks lease")
			}
			_, result := v.final(t, r, p, true, true)
			if result.State() != f.NotCommitted {
				t.Fatal("outer failure committed", result.State())
			}
			v.atomicFacts(t, r, p, 0)
			out, result := v.final(t, r, p, true, false)
			if result.State() != f.Committed || out.Validate() != nil {
				t.Fatal("atomic completion", result.Fault())
			}
			v.atomicFacts(t, r, p, 1)
		})
	}
	t.Run("secret-failure-rolls-every-effect-back", func(t *testing.T) {
		_, m, _ := v.config(t, false, true)
		r := v.request(t, m, false)
		p := v.discover(t, r)
		v.tap.mu.Lock()
		v.tap.fail = true
		v.tap.mu.Unlock()
		_, result := v.final(t, r, p, true, false)
		v.tap.mu.Lock()
		v.tap.fail = false
		v.tap.mu.Unlock()
		if result.State() != f.NotCommitted {
			t.Fatal("Secret error committed")
		}
		v.atomicFacts(t, r, p, 0)
	})
	t.Run("missing-and-weak-lock-poison-cannot-be-swallowed", func(t *testing.T) {
		_, m, _ := v.config(t, false, true)
		for _, mode := range []string{"missing-command", "weak-lease"} {
			r := v.request(t, m, true)
			p := v.discover(t, r)
			ls := p.RequiredLocks()
			removed := false
			actual := []f.LockRequest{}
			for _, l := range ls {
				if !removed && (mode == "missing-command" && strings.HasPrefix(l.Key.Canonical(), "command:") || mode == "weak-lease" && strings.Contains(l.Key.Canonical(), "secret-lease:")) {
					removed = true
					if mode == "missing-command" {
						continue
					}
					l.Mode = f.Shared
				}
				actual = append(actual, l)
			}
			if !removed {
				t.Fatal("target lock not in actual union")
			}
			var inner error
			result := v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				if e := v.observed.AcquireAll(ctx, tx, actual); e != nil {
					return e
				}
				_, inner = v.resolving.ResolveModelInTx(ctx, tx, r, p)
				return nil
			})
			if inner == nil || result.State() != f.NotCommitted {
				t.Fatal("missing lock did not poison", mode, inner, result.State())
			}
			v.atomicFacts(t, r, p, 0)
			t.Logf("%s actual inner=%v commit=%s", mode, inner, result.State())
		}
	})
	t.Run("no-ref-zero-secret-apply", func(t *testing.T) {
		_, m, _ := v.config(t, false, false)
		r := v.request(t, m, false)
		before := v.tap.calls.Load()
		out := v.resolve(t, r)
		if out.CredentialLease != nil || out.Snapshot.CredentialRef != nil || before != v.tap.calls.Load() {
			t.Fatal("zero-ref called Secret")
		}
	})
	var reads int
	if e := v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='secret.resolve'`).Scan(&reads); e != nil || reads != 0 {
		t.Fatal("resolver performed material read", e)
	}
}
func TestModelCurrentResolutionCanonicalLease(t *testing.T) {
	v := newCurrentResolutionFixture(t)
	_, m, _ := v.config(t, false, true)
	r1 := v.request(t, m, false)
	r2 := r1.Clone()
	r2.Consumer.Purpose = mc.AgentCompaction
	r2.Purpose = mc.AgentCompaction
	v.bind(t, r2)
	p1 := v.discover(t, r1)
	p2 := v.discover(t, r2)
	if *p1.Details().LeaseID == *p2.Details().LeaseID {
		t.Fatal("two independent preparations did not allocate distinct candidates")
	}
	t.Run("concurrent-final-canonical-conflict-then-replan", func(t *testing.T) {
		type answer struct {
			idx    int
			out    mc.ResolvedModel
			result f.CommitResult
		}
		start := make(chan struct{})
		done := make(chan answer, 2)
		for i := range 2 {
			go func(i int) {
				<-start
				r, p := r1, p1
				if i == 1 {
					r, p = r2, p2
				}
				out, result := v.final(t, r, p, false, false)
				done <- answer{i, out, result}
			}(i)
		}
		close(start)
		a, b := <-done, <-done
		if a.result.State() != f.Committed {
			a, b = b, a
		}
		if a.result.State() != f.Committed || b.result.State() != f.NotCommitted || b.result.Fault() == nil || b.result.Fault().Code != f.ResourceBusy {
			t.Fatal("expected one winner/one exact busy", a.result.State(), b.result.Fault())
		}
		loser, old := r1, p1
		if b.idx == 1 {
			loser, old = r2, p2
		}
		var count int
		if e := v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=$1`, r1.LeaseOwner.Details().ID).Scan(&count); e != nil || count != 1 {
			t.Fatal("second candidate lease escaped", count, e)
		}
		p := v.discover(t, loser)
		if p.Details().SnapshotID != old.Details().SnapshotID || *p.Details().LeaseID != a.out.CredentialLease.LeaseID {
			t.Fatal("replan changed snapshot identity or missed canonical")
		}
		out, result := v.final(t, loser, p, false, false)
		if result.State() != f.Committed || out.CredentialLease.LeaseID != a.out.CredentialLease.LeaseID {
			t.Fatal("canonical reuse", result.Fault())
		}
	})
	t.Run("model-call-distinct-and-stable", func(t *testing.T) {
		var leases []sc.LeaseID
		for range 2 {
			r := v.request(t, m, false)
			r.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, newID[mc.Call](t).String())
			v.bind(t, r)
			a := v.resolve(t, r)
			b := v.resolve(t, r)
			if a.Snapshot.ID != b.Snapshot.ID || a.CredentialLease.LeaseID != b.CredentialLease.LeaseID {
				t.Fatal("same call changed")
			}
			leases = append(leases, a.CredentialLease.LeaseID)
		}
		if leases[0] == leases[1] {
			t.Fatal("different calls share lease")
		}
	})
	t.Run("same-owner-ref-other-project-denied", func(t *testing.T) {
		other := v.createProject(t, v.owner)
		r := r1.Clone()
		r.Consumer.ProjectID = other.ID
		r.Actor, _ = id.NewAgentRun(other.ID, *r.Consumer.AgentID, *r.Consumer.ExecutionID)
		v.bind(t, r)
		p, e := v.resolving.DiscoverResolve(testContext(t), r)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.Forbidden)
		v.bind(t, r1)
	})
	t.Run("released-canonical-never-resurrected", func(t *testing.T) {
		p := v.discover(t, r1)
		_, e := v.raw.Exec(testContext(t), `UPDATE agenteam_secret.secret_leases SET released=true WHERE id=$1`, p.Details().LeaseID.String())
		if e != nil {
			t.Fatal(e)
		}
		out, e := v.resolving.ResolveModel(testContext(t), r1)
		resolutionNoResult(t, out, e)
		requireCode(t, e, f.InvalidState)
		var released bool
		if e = v.raw.QueryRow(testContext(t), `SELECT released FROM agenteam_secret.secret_leases WHERE id=$1`, p.Details().LeaseID.String()).Scan(&released); e != nil || !released {
			t.Fatal("fixture released row resurrected", e)
		}
		t.Log("fixture-only released fact; no production Retire implemented")
	})
}
