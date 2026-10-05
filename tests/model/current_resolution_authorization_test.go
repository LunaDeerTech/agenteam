//go:build integration

package model_test

import (
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"testing"
	"time"
)

func resolutionFaultCode(e error) f.Code {
	var ff *f.Fault
	if errors.As(e, &ff) {
		return ff.Code
	}
	return ""
}
func TestModelCurrentResolutionAuthorization(t *testing.T) {
	v := newCurrentResolutionFixture(t)
	_, m, _ := v.config(t, false, true)
	t.Run("strict-agent-and-input-change", func(t *testing.T) {
		r := v.request(t, m, false)
		p := v.discover(t, r)
		before := v.consumers.validated.Load()
		_, e := v.raw.Exec(testContext(t), `UPDATE model_resolution_fixture.consumers SET version=version+1,request_data=jsonb_set(request_data,'{Request,consumer,agent_id}',to_jsonb($2::text)) WHERE unit=$1`, resolutionUnit(r), newID[id.Agent](t).String())
		if e != nil {
			t.Fatal(e)
		}
		_, result := v.final(t, r, p, false, false)
		if result.State() != f.NotCommitted || result.Fault() == nil || result.Fault().Code != f.Forbidden {
			t.Fatal("stale actual input authorized", result.Fault())
		}
		if v.consumers.validated.Load() != before {
			t.Fatal("changed input counted validated")
		}
		v.atomicFacts(t, r, p, 0)
	})
	t.Run("owner-execution-and-actor-tamper", func(t *testing.T) {
		r := v.request(t, m, true)
		p := v.discover(t, r)
		for _, mode := range []string{"actor", "execution", "owner"} {
			changed := r.Clone()
			switch mode {
			case "actor":
				changed.Actor = v.admin
			case "execution":
				execution := newID[id.Execution](t)
				changed.Consumer.ExecutionID = &execution
				changed.LeaseOwner = resolutionOwner(t, execution.String())
			case "owner":
				changed.LeaseOwner = resolutionCallOwner(t)
			}
			_, result := v.final(t, changed, p, false, false)
			if result.State() != f.NotCommitted {
				t.Fatal("altered request reused signed plan", mode)
			}
		}
		v.atomicFacts(t, r, p, 0)
	})
	t.Run("permission-before-existing-and-missing-model", func(t *testing.T) {
		r := v.request(t, m, false)
		_, e := v.raw.Exec(testContext(t), `UPDATE model_resolution_fixture.consumers SET enabled=false WHERE unit=$1`, resolutionUnit(r))
		if e != nil {
			t.Fatal(e)
		}
		p, e := v.resolving.DiscoverResolve(testContext(t), r)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.Forbidden)
		missing := newID[mc.Model](t)
		r.ModelRef = &missing
		p, e = v.resolving.DiscoverResolve(testContext(t), r)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.Forbidden)
	})
	t.Run("session-and-project-gate-current", func(t *testing.T) {
		r := v.request(t, m, true)
		uid, _ := f.ParseID[id.User](r.Actor.Details().UserID)
		r.Actor = v.session(t, uid)
		v.bind(t, r)
		p := v.discover(t, r)
		_, e := v.raw.Exec(testContext(t), `DELETE FROM agenteam_account.sessions WHERE id=$1`, r.Actor.Details().SessionID)
		if e != nil {
			t.Fatal(e)
		}
		_, result := v.final(t, r, p, false, false)
		if result.State() != f.NotCommitted {
			t.Fatal("revoked session accepted")
		}
		r = v.request(t, m, false)
		p = v.discover(t, r)
		var archivedAt time.Time
		if e = v.raw.QueryRow(testContext(t), `SELECT clock_timestamp()`).Scan(&archivedAt); e != nil {
			t.Fatal(e)
		}
		_, e = v.raw.Exec(testContext(t), `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=$2,updated_at=$2,version=version+1 WHERE id=$1`, v.project.ID.String(), archivedAt)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := v.raw.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='active',archived_at=NULL,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.project.ID.String()); err != nil {
				t.Error(err)
			}
		})
		beforeApply := v.tap.calls.Load()
		_, result = v.final(t, r, p, false, false)
		if result.State() != f.NotCommitted {
			t.Fatal("archived gate accepted resolution")
		}
		requireCode(t, result.Fault(), f.ProjectNotActive)
		v.atomicFacts(t, r, p, 0)
		if v.tap.calls.Load() != beforeApply {
			t.Fatal("archived gate reached Secret Apply")
		}
		t.Log("valid archived ProjectRef; exact PROJECT_NOT_ACTIVE, prepared retained and zero input/snapshot/binding/lease effects")
	})
	t.Run("wrong-issuer-and-foreign-store", func(t *testing.T) {
		r := v.request(t, m, false)
		p := v.discover(t, r)
		forged, e := mc.NewResolutionPlan(mc.NewPlanIssuer(), p.Details())
		if e != nil {
			t.Fatal(e)
		}
		_, result := v.final(t, r, forged, false, false)
		if result.State() != f.NotCommitted || result.Fault().Code != f.Forbidden {
			t.Fatal("foreign issuer accepted")
		}
		other := openStore(t, v.db.Config(t, nil))
		var inner error
		result = other.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
			if e := other.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
				return e
			}
			_, inner = v.resolving.ResolveModelInTx(ctx, tx, r, p)
			return inner
		})
		if inner == nil || result.State() != f.NotCommitted {
			t.Fatal("same PG foreign Store Tx accepted")
		}
		t.Logf("same PG foreign Store actual rejection=%v", inner)
		v.atomicFacts(t, r, p, 0)
	})
	t.Run("exact-apply-witness-expires-and-never-reads", func(t *testing.T) {
		r := v.request(t, m, false)
		p := v.discover(t, r)
		_, result := v.final(t, r, p, false, false)
		if result.State() != f.Committed {
			t.Fatal(result.Fault())
		}
		v.tap.mu.Lock()
		savedCtx, savedRequest, savedPlan := v.tap.ctx, v.tap.request, v.tap.plan
		v.tap.mu.Unlock()
		facts := func() string {
			var value string
			if e := v.raw.QueryRow(testContext(t), `SELECT jsonb_build_array((SELECT to_jsonb(p) FROM agenteam_model.resolution_preparations p WHERE snapshot_id=$1),(SELECT to_jsonb(s) FROM agenteam_model.snapshots s WHERE id=$1),(SELECT to_jsonb(b) FROM agenteam_model.snapshot_bindings b WHERE snapshot_id=$1),(SELECT to_jsonb(l) FROM agenteam_secret.secret_leases l WHERE id=$2))::text`, p.Details().SnapshotID.String(), p.Details().LeaseID.String()).Scan(&value); e != nil {
				t.Fatal(e)
			}
			return value
		}
		before := facts()
		// Start the new Tx from a fresh bounded root: savedCtx still contains
		// the old D03 transaction marker. Only Apply receives its stale values.
		transactionCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		deadline, _ := transactionCtx.Deadline()
		probeCtx, cancelProbe := context.WithDeadline(context.WithoutCancel(savedCtx), deadline)
		defer cancelProbe()
		var inner error
		held := false
		result = v.observed.WithinTx(transactionCtx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
			if e := v.observed.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
				return e
			}
			if e := v.observed.RequireHeldLocks(ctx, tx, p.RequiredLocks()); e != nil {
				return e
			}
			held = true
			_, inner = v.resolutionSecrets.ApplyUsageInTx(probeCtx, tx, savedRequest, savedPlan)
			return inner
		})
		if !held || result.State() != f.NotCommitted {
			t.Fatal("expired witness target check not reached", inner, result.State())
		}
		requireCode(t, inner, f.Forbidden)
		requireCode(t, result.Fault(), f.Forbidden)
		if before != facts() {
			t.Fatal("expired witness changed original preparation/snapshot/binding/lease")
		}
		t.Log("new exact Tx holds full union; preserved expired witness yields FORBIDDEN and rollback with original facts unchanged")
	})
}

func resolutionOwner(t *testing.T, execution string) sc.CredentialLeaseOwner {
	t.Helper()
	o, e := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, execution)
	if e != nil {
		t.Fatal(e)
	}
	return o
}
func resolutionCallOwner(t *testing.T) sc.CredentialLeaseOwner {
	t.Helper()
	o, e := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, newID[mc.Call](t).String())
	if e != nil {
		t.Fatal(e)
	}
	return o
}
