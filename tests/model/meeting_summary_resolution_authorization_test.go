//go:build integration

package model_test

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"strings"
	"testing"
	"time"
)

func TestModelMeetingSummaryResolutionAuthorization(t *testing.T) {
	v := newMeetingResolutionFixture(t, true)
	_, m, _ := v.config(t, false, true)
	v.choose(t, m.ID)
	t.Run("canonical-meeting-operation-input-phase-version", func(t *testing.T) {
		for _, mode := range []string{"meeting-project", "meeting-phase", "meeting-version", "operation-meeting", "call", "purpose", "initiator", "input", "operation-phase", "operation-version"} {
			t.Run(mode, func(t *testing.T) {
				r := v.request(t, mc.MeetingSummaryInitial)
				p := v.discover(t, r)
				before := v.strict.validated.Load()
				other := newID[struct{}](t).String()
				switch mode {
				case "meeting-project":
					v.sql(t, `UPDATE meeting_resolution_fixture.meetings SET project_id=$2 WHERE id=$1`, r.Consumer.MeetingID, other)
				case "meeting-phase":
					v.sql(t, `UPDATE meeting_resolution_fixture.meetings SET phase='closed' WHERE id=$1`, r.Consumer.MeetingID)
				case "meeting-version":
					v.sql(t, `UPDATE meeting_resolution_fixture.meetings SET version=version+1 WHERE id=$1`, r.Consumer.MeetingID)
				case "operation-meeting":
					v.sql(t, `INSERT INTO meeting_resolution_fixture.meetings(id,project_id,phase,version)VALUES($1,$2,'active',1)`, other, r.Consumer.ProjectID.String())
					v.sql(t, `UPDATE meeting_resolution_fixture.operations SET meeting_id=$2 WHERE id=$1`, r.Consumer.OperationID, other)
				case "call":
					v.sql(t, `UPDATE meeting_resolution_fixture.operations SET call_id=$2 WHERE id=$1`, r.Consumer.OperationID, other)
				case "purpose":
					v.sql(t, `UPDATE meeting_resolution_fixture.operations SET purpose='meeting_summary_update' WHERE id=$1`, r.Consumer.OperationID)
				case "initiator":
					v.sql(t, `UPDATE meeting_resolution_fixture.operations SET initiator=$2 WHERE id=$1`, r.Consumer.OperationID, other)
				case "input":
					v.sql(t, `UPDATE meeting_resolution_fixture.operations SET input_id=$2 WHERE id=$1`, r.Consumer.OperationID, other)
				case "operation-phase":
					v.sql(t, `UPDATE meeting_resolution_fixture.operations SET phase='cancelled' WHERE id=$1`, r.Consumer.OperationID)
				case "operation-version":
					v.sql(t, `UPDATE meeting_resolution_fixture.operations SET version=version+1 WHERE id=$1`, r.Consumer.OperationID)
				}
				_, result := v.final(t, r, p, true, false)
				if result.State() != f.NotCommitted {
					t.Fatal("stale canonical fact accepted")
				}
				requireCode(t, result.Fault(), f.Forbidden)
				v.atomicFacts(t, r, p, 0)
				if v.strict.validated.Load() != before {
					t.Fatal("changed fact counted validated")
				}
			})
		}
	})
	t.Run("formal-logout-new-session-and-actual-project-gate", func(t *testing.T) {
		browser := v.identity.login(t, v.identity.ownerBrowser.email)
		r := v.request(t, mc.MeetingSummaryUpdate)
		r.Actor = browser.actor
		p := v.discover(t, r)
		if e := v.identity.core.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); e != nil {
			t.Fatal("formal Logout", e)
		}
		_, result := v.final(t, r, p, true, false)
		if result.State() != f.NotCommitted {
			t.Fatal("logged-out Session accepted")
		}
		v.atomicFacts(t, r, p, 0)
		r.Actor = v.identity.login(t, browser.email).actor
		fresh := v.discover(t, r)
		out, result := v.final(t, r, fresh, true, false)
		if result.State() != f.Committed || out.Validate() != nil {
			t.Fatal("current new Session rejected", result.Fault())
		}
		r = v.request(t, mc.MeetingSummaryInitial)
		p = v.discover(t, r)
		v.sql(t, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.project.ID.String())
		defer v.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',archived_at=NULL,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.project.ID.String())
		before := v.tap.calls.Load()
		_, result = v.final(t, r, p, true, false)
		requireCode(t, result.Fault(), f.ProjectNotActive)
		v.atomicFacts(t, r, p, 0)
		if before != v.tap.calls.Load() {
			t.Fatal("closed gate reached Secret")
		}
	})
	t.Run("admin-other-user-and-other-project-cannot-bypass-canonical", func(t *testing.T) {
		for _, actor := range []id.Actor{v.admin, v.identity.otherBrowser.actor} {
			r := v.request(t, mc.MeetingSummaryInitial)
			r.Actor = actor
			v.bindFacts(t, r)
			p, e := v.resolving.DiscoverResolve(testContext(t), r)
			resolutionZeroPlan(t, p, e)
			requireCode(t, e, f.NotFound)
		}
		r := v.request(t, mc.MeetingSummaryInitial)
		r.Consumer.ProjectID = v.createProject(t, v.identity.otherBrowser.actor).ID
		v.bindFacts(t, r)
		p, e := v.resolving.DiscoverResolve(testContext(t), r)
		resolutionZeroPlan(t, p, e)
		requireCode(t, e, f.NotFound)
	})
	t.Run("signed-request-issuer-and-foreign-store", func(t *testing.T) {
		r := v.request(t, mc.MeetingSummaryInitial)
		p := v.discover(t, r)
		for _, mode := range []string{"actor", "call", "meeting", "operation", "selector-version"} {
			changed := r.Clone()
			switch mode {
			case "actor":
				changed.Actor = v.admin
			case "call":
				changed.LeaseOwner = resolutionCallOwner(t)
			case "meeting":
				changed.Consumer.MeetingID = newID[struct{}](t).String()
			case "operation":
				changed.Consumer.OperationID = newID[struct{}](t).String()
			case "selector-version":
				version := f.Version(99)
				changed.Selection.Version = &version
			}
			_, result := v.final(t, changed, p, false, false)
			if result.State() != f.NotCommitted {
				t.Fatal("signed plan request changed", mode)
			}
		}
		forged, e := mc.NewResolutionPlan(mc.NewPlanIssuer(), p.Details())
		if e != nil {
			t.Fatal(e)
		}
		_, result := v.final(t, r, forged, false, false)
		requireCode(t, result.Fault(), f.Forbidden)
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
			t.Fatal("same database foreign Store accepted")
		}
		v.atomicFacts(t, r, p, 0)
		_, inner = v.resolving.ResolveModelInTx(testContext(t), f.Tx{}, r, p)
		if inner == nil {
			t.Fatal("outside transaction accepted")
		}
	})
	t.Run("consumer-lock-and-lower-order-poison", func(t *testing.T) {
		r := v.request(t, mc.MeetingSummaryInitial)
		p := v.discover(t, r)
		for _, target := range []string{"meeting:", "operation:", "meeting-resolution-facts:"} {
			var locks []f.LockRequest
			removed := false
			for _, l := range p.RequiredLocks() {
				if strings.Contains(l.Key.Canonical(), target) {
					removed = true
					continue
				}
				locks = append(locks, l)
			}
			if !removed {
				t.Fatal("expected consumer lock missing", target)
			}
			var inner error
			result := v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				if e := v.observed.AcquireAll(ctx, tx, locks); e != nil {
					return e
				}
				_, inner = v.resolving.ResolveModelInTx(ctx, tx, r, p)
				return nil
			})
			if inner == nil || result.State() != f.NotCommitted {
				t.Fatal("consumer missing lock did not poison", target)
			}
			v.atomicFacts(t, r, p, 0)
		}
		locks := p.RequiredLocks()
		var inner error
		result := v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
			if e := v.observed.AcquireAll(ctx, tx, locks[len(locks)-1:]); e != nil {
				return e
			}
			inner = v.observed.AcquireAll(ctx, tx, locks[:1])
			return nil
		})
		if inner == nil || result.State() != f.NotCommitted {
			t.Fatal("lower-order acquisition did not poison")
		}
		v.atomicFacts(t, r, p, 0)
	})
	t.Run("exact-secret-witness-cannot-cross-transaction-or-lease", func(t *testing.T) {
		r := v.request(t, mc.MeetingSummaryUpdate)
		p := v.discover(t, r)
		_, result := v.final(t, r, p, false, false)
		if result.State() != f.Committed {
			t.Fatal(result.Fault())
		}
		v.tap.mu.Lock()
		savedCtx, savedRequest, savedPlan := v.tap.ctx, v.tap.request, v.tap.plan
		v.tap.mu.Unlock()
		for _, alter := range []bool{false, true} {
			request := savedRequest
			if alter {
				request.LeaseID = newID[sc.Lease](t)
			}
			txCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			deadline, _ := txCtx.Deadline()
			probeCtx, probeCancel := context.WithDeadline(context.WithoutCancel(savedCtx), deadline)
			reached := false
			var inner error
			result = v.observed.WithinTx(txCtx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				if e := v.observed.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
					return e
				}
				if e := v.observed.RequireHeldLocks(ctx, tx, p.RequiredLocks()); e != nil {
					return e
				}
				reached = true
				_, inner = v.resolutionSecrets.ApplyUsageInTx(probeCtx, tx, request, savedPlan)
				return inner
			})
			probeCancel()
			cancel()
			if !reached || inner == nil || result.State() != f.NotCommitted {
				t.Fatal("stale exact witness accepted", inner)
			}
			if alter {
				// A different LeaseID fails the signed usage binding before the witness gate.
				requireCode(t, inner, f.InvalidArgument)
			} else {
				requireCode(t, inner, f.Forbidden)
			}
		}
	})
}
