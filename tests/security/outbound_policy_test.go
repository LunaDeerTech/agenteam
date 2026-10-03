//go:build integration

package security_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

func outboundPolicy(t *testing.T, f *auditFixture, appender ac.Appender) *outbound.PolicyService {
	t.Helper()
	if appender == nil {
		appender = f.service
	}
	s, e := outbound.NewPolicyService(f.store, appender, outbound.Authorizations{Sessions: f.auth, System: f.auth})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Reload(auditContext(t)); e != nil {
		t.Fatal(e)
	}
	return s
}
func policyCommand(t *testing.T, actor identity.Actor, expected foundation.Version) outbound.CommandMeta {
	t.Helper()
	key := foundation.IdempotencyKey(newID[struct{}](t).String())
	command, e := foundation.NewCommandIdentity("outbound-policy", []string{actor.Details().UserID}, "update", key)
	if e != nil {
		t.Fatal(e)
	}
	return outbound.CommandMeta{Identity: command, ExpectedVersion: expected, HTTPTraceID: newID[struct{}](t).String()}
}
func policyRules(t *testing.T, cidr string) outbound.Rules {
	t.Helper()
	if cidr == "" {
		r, _ := outbound.NewRules()
		return r
	}
	ports, _ := outbound.SelectedPorts(443, 8443)
	rule, e := outbound.NewRule(cidr, ports, false)
	if e != nil {
		t.Fatal(e)
	}
	rules, e := outbound.NewRules(rule)
	if e != nil {
		t.Fatal(e)
	}
	return rules
}
func TestOutboundPolicyDurabilityReplayAndAuthorization(t *testing.T) {
	f := newAuditFixture(t)
	s := outboundPolicy(t, f, nil)
	ctx := auditContext(t)
	initial, e := s.GetPolicy(ctx, f.actor)
	if e != nil || initial.Version() != 1 || initial.Rules().Count() != 0 {
		t.Fatal("wrong initial policy", e)
	}
	k1 := policyCommand(t, f.actor, 1)
	r1 := policyRules(t, "10.7.0.0/16")
	first, e := s.UpdatePolicy(ctx, f.actor, k1, r1)
	if e != nil || first.Version != 2 {
		t.Fatal(first, e)
	}
	k2 := policyCommand(t, f.actor, 2)
	empty := policyRules(t, "")
	second, e := s.UpdatePolicy(ctx, f.actor, k2, empty)
	if e != nil || second.Version != 3 {
		t.Fatal(second, e)
	}
	// A current second Session for the same stable Human may replay K1, with a
	// new transport trace, but the mirror and authoritative row remain at K2.
	session := newID[identity.Session](t)
	_, e = f.store.Exec(ctx, `INSERT INTO audit_fixture.sessions VALUES($1,$2,true)`, session.String(), f.actor.Details().UserID)
	if e != nil {
		t.Fatal(e)
	}
	user, _ := foundation.ParseID[identity.User](f.actor.Details().UserID)
	actor, _ := identity.NewHuman(user, session)
	k1.HTTPTraceID = newID[struct{}](t).String()
	replay, e := s.UpdatePolicy(ctx, actor, k1, r1)
	if e != nil || replay != first {
		t.Fatal("unstable replay", e)
	}
	if status := s.Status(); !status.Available || status.Version == nil || *status.Version != 3 {
		t.Fatal("replay published old mirror", status)
	}
	p, e := s.GetPolicy(ctx, actor)
	if e != nil || p.Version() != 3 || p.Rules().Count() != 0 {
		t.Fatal("replay restored old policy")
	}
	_, e = s.UpdatePolicy(ctx, actor, k1, empty)
	requireCode(t, e, foundation.IdempotencyKeyReused)
	_, e = s.UpdatePolicy(ctx, actor, policyCommand(t, actor, 1), r1)
	requireCode(t, e, foundation.VersionConflict)
	var count int
	if e = f.store.QueryRow(ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='outbound.policy.update'`).Scan(&count); e != nil || count != 2 {
		t.Fatal("wrong audit count", count, e)
	}
	_, e = f.store.Exec(ctx, `UPDATE audit_fixture.system_access SET allowed=false`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.UpdatePolicy(ctx, actor, k1, r1)
	requireCode(t, e, foundation.Forbidden)
	_, e = s.GetPolicy(ctx, actor)
	requireCode(t, e, foundation.Forbidden)
	_, e = f.store.Exec(ctx, `UPDATE audit_fixture.system_access SET allowed=true; UPDATE audit_fixture.sessions SET active=false`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.UpdatePolicy(ctx, actor, k1, r1)
	requireCode(t, e, foundation.SessionRevoked)
	unbound, _ := outbound.NewPolicyService(f.store, f.service, outbound.Authorizations{})
	_, e = unbound.GetPolicy(ctx, actor)
	requireCode(t, e, foundation.DependencyUnbound)
	// Restart reload does not rely on any in-memory command receipt.
	restarted := outboundPolicy(t, f, nil)
	if status := restarted.Status(); !status.Available || *status.Version != 3 {
		t.Fatal("restart lost current policy")
	}
}

type rejectedPolicyAudit struct{ next ac.Appender }

func (a rejectedPolicyAudit) AppendInTx(ctx context.Context, tx foundation.Tx, e ac.Entry, key ac.AppendKey) (ac.AppendReceipt, error) {
	_, err := a.next.AppendInTx(ctx, tx, e, key)
	if err != nil {
		return ac.AppendReceipt{}, err
	}
	return ac.AppendReceipt{}, errors.New("audit-failure-canary")
}
func TestOutboundPolicyRollbackAndUnavailableReplay(t *testing.T) {
	f := newAuditFixture(t)
	ctx := auditContext(t)
	failed := outboundPolicy(t, f, rejectedPolicyAudit{f.service})
	r := policyRules(t, "10.9.0.0/16")
	if _, e := failed.UpdatePolicy(ctx, f.actor, policyCommand(t, f.actor, 1), r); e == nil {
		t.Fatal("audit failure committed")
	}
	var version, receipts, audits int
	if e := f.store.QueryRow(ctx, `SELECT version,(SELECT count(*) FROM agenteam_outbound.outbound_policy_receipts),(SELECT count(*) FROM agenteam_audit.audit_records) FROM agenteam_outbound.outbound_policy`).Scan(&version, &receipts, &audits); e != nil || version != 1 || receipts != 0 || audits != 0 {
		t.Fatal(version, receipts, audits, e)
	}
	if status := failed.Status(); !status.Available || *status.Version != 1 {
		t.Fatal("rollback replaced mirror")
	}
	s := outboundPolicy(t, f, nil)
	k1 := policyCommand(t, f.actor, 1)
	first, e := s.UpdatePolicy(ctx, f.actor, k1, r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.UpdatePolicy(ctx, f.actor, policyCommand(t, f.actor, 2), policyRules(t, "")); e != nil {
		t.Fatal(e)
	}
	// The fixture corrupts a stored typed rule, then restores the original v3.
	// A successful old replay is not a cache reload or an availability claim.
	_, e = f.store.Exec(ctx, `UPDATE agenteam_outbound.outbound_policy SET rules='[{"cidr":"127.0.0.0/8","ports":"all"}]'`)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Reload(ctx); e == nil || s.Status().Available {
		t.Fatal("invalid DB policy allowed")
	}
	_, e = f.store.Exec(ctx, `UPDATE agenteam_outbound.outbound_policy SET rules='[]'`)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.UpdatePolicy(ctx, f.actor, k1, r)
	if e != nil || got != first || s.Status().Available {
		t.Fatal("receipt incorrectly recovered mirror", e)
	}
	if e = s.Reload(ctx); e != nil || !s.Status().Available || *s.Status().Version != 3 {
		t.Fatal("explicit reload did not recover current", e)
	}
}
func TestOutboundPolicyRealCommitUnknown(t *testing.T) {
	for _, mode := range []string{"committed_verified", "committed_unavailable", "not_observed"} {
		t.Run(mode, func(t *testing.T) {
			f := newAuditFixture(t)
			proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), mode != "not_observed")
			proxy.armed.Store(false)
			u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
			u.Host = proxy.listener.Addr().String()
			store := openAuditStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			auth := &auditAuthority{store}
			appender, e := audit.New(store, auditKeys(t), audit.Authorizations{Sessions: auth, System: auth})
			if e != nil {
				t.Fatal(e)
			}
			s, e := outbound.NewPolicyService(store, appender, outbound.Authorizations{Sessions: auth, System: auth})
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Reload(auditContext(t)); e != nil {
				t.Fatal(e)
			}
			proxy.armed.Store(true)
			type outcome struct {
				result outbound.UpdateResult
				err    error
			}
			done := make(chan outcome, 1)
			command := policyCommand(t, f.actor, 1)
			ctx := auditContext(t)
			go func() {
				r, e := s.UpdatePolicy(ctx, f.actor, command, policyRules(t, "10.8.0.0/16"))
				done <- outcome{r, e}
			}()
			select {
			case <-proxy.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("real COMMIT barrier missing")
			}
			var stored int64
			if e = f.store.QueryRow(ctx, `SELECT version FROM agenteam_outbound.outbound_policy`).Scan(&stored); e != nil {
				t.Fatal(e)
			}
			if mode == "not_observed" && stored != 1 || mode != "not_observed" && stored != 2 {
				t.Fatal("server commit fact wrong", stored)
			}
			if mode == "committed_unavailable" {
				store.StopAdmission()
			}
			close(proxy.release)
			select {
			case out := <-done:
				if mode == "committed_verified" {
					if out.err != nil || out.result.Version != 2 || !s.Status().Available || *s.Status().Version != 2 {
						t.Fatal("committed lookup not published", out.err)
					}
				} else {
					requireCode(t, out.err, foundation.CommitUnknown)
					var fault *foundation.Fault
					if !errors.As(out.err, &fault) || fault.CommitState != foundation.Unknown || s.Status().Available {
						t.Fatal("unknown falsely resolved", out.err)
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("unknown reconciliation hung")
			}
			restarted := outboundPolicy(t, f, nil)
			if *restarted.Status().Version != foundation.Version(stored) {
				t.Fatal("restart ignored durable policy")
			}
		})
	}
}
