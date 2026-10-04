//go:build integration

package account_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type rejectingAudit struct {
	ac.Appender
	action  ac.Action
	enabled atomic.Bool
}

func (a *rejectingAudit) AppendInTx(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	if a.enabled.Load() && e.Fields().Action == a.action {
		return ac.AppendReceipt{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return a.Appender.AppendInTx(ctx, tx, e, k)
}
func TestAccountAuditFailureRollsBackLoginAndDestroysUnconfirmedRead(t *testing.T) {
	for _, action := range []ac.Action{ac.AccountLogin, ac.SecretResolve} {
		t.Run(string(action), func(t *testing.T) {
			_, s, a := database(t)
			var deny *rejectingAudit
			f := assembleAccount(t, s, s, a, liveProcess{id[c.Process](t)}, func(real ac.Appender) ac.Appender {
				deny = &rejectingAudit{Appender: real, action: action}
				return deny
			})
			password := f.bootstrap(t)
			r, _ := loginRequest(t, f, password, "admin@mail.com")
			deny.enabled.Store(true)
			response, e := f.service.Login(ctxFor(t), r)
			wantCode := foundation.Forbidden
			if action == ac.SecretResolve {
				wantCode = foundation.DependencyUnavailable
			}
			if !hasCode(e, wantCode) {
				t.Fatal("audit failure not propagated", e, safeFailure(e))
			}
			if e = response.UseCookie(func([]byte) error { t.Error("failed Audit exposed cookie"); return nil }); e == nil {
				t.Fatal("invalid response usable")
			}
			var sessions, refs int
			if e = s.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.sessions),(SELECT count(*) FROM agenteam_secret.secret_references)`).Scan(&sessions, &refs); e != nil {
				t.Fatal(e)
			}
			want := 0
			if action == ac.SecretResolve {
				want = 1
			}
			if sessions != want || refs != want {
				t.Fatal("business transaction split", sessions, refs)
			}
			deny.enabled.Store(false)
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(5 * time.Millisecond)
			defer tick.Stop()
			for {
				if _, e = f.service.Recover(ctxFor(t)); e != nil {
					t.Fatal(e, safeFailure(e))
				}
				var active int
				if e = s.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE NOT released`).Scan(&active); e != nil {
					t.Fatal(e)
				}
				if active == 0 {
					break
				}
				select {
				case <-tick.C:
				case <-deadline.C:
					t.Fatal("failed read lease not joined")
				}
			}
		})
	}
}
func responseRequest(t *testing.T, f *fixture, id c.AttemptID, action sc.UsageAction) sc.UsageRequest {
	t.Helper()
	var refRaw, leaseRaw string
	if e := f.store.QueryRow(ctxFor(t), `SELECT credential_id::text,lease_id::text FROM agenteam_account.response_plans WHERE id=$1`, id.String()).Scan(&refRaw, &leaseRaw); e != nil {
		t.Fatal(e)
	}
	refID, _ := foundation.ParseID[sc.Credential](refRaw)
	ref, _ := sc.NewCredentialRef(refID, identity.SystemScope())
	lease, _ := foundation.ParseID[sc.Lease](leaseRaw)
	reg, _ := identity.RegisterService(identity.SecretService)
	actor, _ := reg.Actor(id.String(), identity.SystemScope())
	owner, _ := sc.NewCredentialLeaseOwner(sc.AccountResponseOwner, id.String())
	return sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.System, LeaseOwner: owner, LeaseID: lease, Action: action}
}
func TestAccountUsagePlansCannotBeForgedDowngradedOrBypassed(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	login, _ := loginRequest(t, f, password, "admin@mail.com")
	response, e := f.service.Login(ctxFor(t), login)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Close(ctxFor(t))
	r := responseRequest(t, f, response.AttemptID(), sc.AcquireLeaseUsage)
	plan, e := f.secrets.DiscoverUsage(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	provider, e := f.authority.DiscoverUsage(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	_, cursor, master := keys(t)
	aud, e := audit.New(f.store, cursor, audit.Authorizations{Sessions: f.authority, System: f.authority, Accounts: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	other, e := secret.New(f.store, master, aud, secret.Authorizations{Sessions: f.authority, System: f.authority, Usage: f.authority, AccountWrites: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	otherPlan, e := other.DiscoverUsage(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		name    string
		request sc.UsageRequest
		plan    sc.UsageDependencies
		locks   bool
	}{
		{"provider_plan", r, provider, true}, {"other_service", r, otherPlan, true}, {"missing_locks", r, plan, false},
		{"changed_id", func() sc.UsageRequest { v := r; v.LeaseID = id[sc.Lease](t); return v }(), plan, true},
		{"changed_action", func() sc.UsageRequest { v := r; v.Action = sc.ReleaseLeaseUsage; return v }(), plan, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var operationError error
			result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if test.locks {
					if e := f.store.AcquireAll(ctx, tx, plan.RequiredLocks()); e != nil {
						return e
					}
				}
				_, operationError = f.secrets.ApplyUsageInTx(ctx, tx, test.request, test.plan)
				return operationError
			})
			if operationError == nil || result.State() != foundation.NotCommitted {
				t.Fatal("invalid plan committed", result.State(), operationError)
			}
		})
	}
	// An ignored held-lock error must poison the real transaction too.
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, _ = f.secrets.ApplyUsageInTx(ctx, tx, r, plan)
		return nil
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("missing locks did not poison")
	}
	read := r
	read.Action = sc.ReadLeaseUsage
	readPlan, e := f.secrets.DiscoverUsage(ctxFor(t), read)
	if e != nil {
		t.Fatal(e)
	}
	result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := f.store.AcquireAll(ctx, tx, readPlan.RequiredLocks()); e != nil {
			return e
		}
		_, e := f.secrets.ApplyUsageInTx(ctx, tx, read, readPlan)
		return e
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("read exposed through Apply")
	}
	result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, e := f.secrets.AcquireCredentialLeaseInTx(ctx, tx, r.Actor, r.Ref, r.LeaseOwner)
		return e
	})
	if result.State() != foundation.NotCommitted || !hasCode(result.Fault(), foundation.ResourceBusy) {
		t.Fatal("legacy account acquire bypass", result.State(), result.Fault())
	}
	result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		return f.secrets.ReleaseCredentialLeaseInTx(ctx, tx, r.Actor, r.LeaseID)
	})
	if result.State() != foundation.NotCommitted || !hasCode(result.Fault(), foundation.ResourceBusy) {
		t.Fatal("legacy account release bypass", result.State(), result.Fault())
	}
	var ownerID string
	if e = f.store.QueryRow(ctxFor(t), `SELECT command_id::text FROM agenteam_account.response_plans WHERE id=$1`, response.AttemptID().String()).Scan(&ownerID); e != nil {
		t.Fatal(e)
	}
	reg, _ := identity.RegisterService(identity.SecretService)
	refActor, _ := reg.Actor(ownerID, identity.SystemScope())
	for _, retain := range []bool{false, true} {
		result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if retain {
				return f.secrets.RetainReferenceInTx(ctx, tx, refActor, r.Ref, sc.System, ownerID)
			}
			return f.secrets.ReleaseReferenceInTx(ctx, tx, refActor, r.Ref, sc.System, ownerID)
		})
		if result.State() != foundation.NotCommitted || !hasCode(result.Fault(), foundation.ResourceBusy) {
			t.Fatal("legacy account reference bypass", retain, result.State(), result.Fault())
		}
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := f.store.AcquireAll(ctx, tx, plan.RequiredLocks()); e != nil {
			return e
		}
		_, e := f.secrets.ApplyUsageInTx(ctx, tx, r, plan)
		return e
	})
	if result.State() != foundation.NotCommitted {
		t.Fatal("terminal lease revived")
	}
	var active int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE id=$1 AND NOT released`, r.LeaseID.String()).Scan(&active); e != nil || active != 0 {
		t.Fatal("terminal lease changed", active, e)
	}
}

func TestAccountReadWaitsExactCurrentUserGateAndRejectsRevocation(t *testing.T) {
	f := newAccount(t)
	password := f.bootstrap(t)
	login, _ := loginRequest(t, f, password, "admin@mail.com")
	response, e := f.service.Login(ctxFor(t), login)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Close(ctxFor(t))
	r := responseRequest(t, f, response.AttemptID(), sc.ReadLeaseUsage)
	key, _ := foundation.UserLock(response.User().ID.String())
	held, release := make(chan struct{}), make(chan struct{})
	writer := make(chan foundation.CommitResult, 1)
	go func() {
		writer <- f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); e != nil {
				return e
			}
			close(held)
			<-release
			x, e := f.store.InTx(tx)
			if e != nil {
				return e
			}
			_, e = x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='logout' WHERE id=$1`, response.Session().ID.String())
			return e
		})
	}()
	<-held
	read := make(chan error, 1)
	go func() {
		material, e := f.secrets.ReadCredentialForRequest(ctxFor(t), r.Actor, r.LeaseID)
		material.Destroy()
		read <- e
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	observed := false
	for !observed {
		var blocked bool
		hash := uint64(key.AdvisoryKey())
		e = f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND NOT granted AND classid::bigint=$1 AND objid::bigint=$2)`, int64(uint32(hash>>32)), int64(uint32(hash))).Scan(&blocked)
		if e != nil {
			close(release)
			<-writer
			t.Fatal("exact gate wait not observed", e)
		}
		observed = blocked
		if !observed {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				close(release)
				<-writer
				t.Fatal("read did not wait at exact User gate")
			}
		}
	}
	close(release)
	if result := <-writer; result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	if e = <-read; !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("read used stale permission", e, safeFailure(e))
	}
}
