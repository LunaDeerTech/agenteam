//go:build integration

package security_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type modelReadAudit struct {
	next   ac.Appender
	ctx    context.Context
	tx     foundation.Tx
	entry  ac.Entry
	key    ac.AppendKey
	calls  int
	before func(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey, error)
}

func (a *modelReadAudit) AppendInTx(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	if e.Fields().Action == ac.SecretResolve {
		a.ctx, a.tx, a.entry, a.key = ctx, tx, e, k
		a.calls++
		if a.before != nil {
			var err error
			ctx, e, k, err = a.before(ctx, tx, e, k)
			if err != nil {
				return ac.AppendReceipt{}, err
			}
		}
	}
	return a.next.AppendInTx(ctx, tx, e, k)
}

type modelReadStore struct {
	secret.Store
	mu               sync.Mutex
	unions           [][]foundation.LockRequest
	armed, rollback  bool
	returned, actual foundation.CommitResult
	marker           foundation.ID[foundation.TransactionAttempt]
	afterCommit      func()
}

func (s *modelReadStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.mu.Lock()
	s.unions = append(s.unions, append([]foundation.LockRequest(nil), locks...))
	s.mu.Unlock()
	return s.Store.AcquireAll(ctx, tx, locks)
}
func (s *modelReadStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	actual := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		err := fn(ctx, tx)
		if err == nil && s.armed && s.rollback {
			return deny(foundation.DependencyUnavailable)
		}
		return err
	})
	s.actual = actual
	if actual.State() == foundation.Committed && s.afterCommit != nil {
		s.afterCommit()
	}
	if s.armed {
		s.returned = foundation.UnknownResult(s.marker, cause)
		return s.returned
	}
	return actual
}
func newModelReadService(t *testing.T, f *secretProjectAuditFixture, store secret.Store) (*secret.Service, *modelReadAudit) {
	t.Helper()
	checker, err := secret.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	f.ports.checker = checker
	a, err := audit.New(f.store, auditKeys(t), audit.Authorizations{Sessions: f.ports, System: f.ports, Projects: &secretProjectAuditAppendPorts{f.ports}})
	if err != nil {
		t.Fatal(err)
	}
	tap := &modelReadAudit{next: a}
	s, err := secret.New(store, masterKeys(t, 1, 1), tap, secret.Authorizations{Sessions: f.ports, System: f.ports, Projects: f.ports, Usage: f.ports})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.StopMaintenance)
	return s, tap
}
func bindModelOwner(t *testing.T, f *secretFixture, ref sc.CredentialRef, purpose sc.Purpose, kind sc.LeaseOwnerKind) (sc.CredentialLeaseOwner, identity.Actor) {
	t.Helper()
	owner, err := sc.NewCredentialLeaseOwner(kind, newID[struct{}](t).String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Exec(auditContext(t), `INSERT INTO audit_fixture.secret_bindings(owner_id,credential_id,consumer,owner_kind) VALUES($1,$2,$3,$4)`, owner.Details().ID, ref.Details().ID.String(), string(purpose), string(kind)); err != nil {
		t.Fatal(err)
	}
	reg, _ := identity.RegisterService(identity.SecretService)
	actor, err := reg.Actor(owner.Details().ID, ref.Details().Scope)
	if err != nil {
		t.Fatal(err)
	}
	return owner, actor
}
func newModelReadCase(t *testing.T, system bool, kind sc.LeaseOwnerKind) (*secretProjectAuditFixture, sc.UsageRequest, sc.CredentialLease) {
	t.Helper()
	f := newSecretProjectAuditFixture(t)
	write := f.request(t, sc.Create, []byte("model-usage-material"))
	if system {
		write.Scope = identity.SystemScope()
		var err error
		write.Identity, err = foundation.NewCommandIdentity("secret", []string{f.actor.Details().UserID}, "create", foundation.IdempotencyKey(newID[struct{}](t).String()))
		if err != nil {
			t.Fatal(err)
		}
	}
	created, err := f.secret.ExecuteWrite(auditContext(t), write)
	if err != nil {
		t.Fatal(err)
	}
	owner, actor := bindModelOwner(t, f.secretFixture, created.Metadata.CredentialRef, sc.Model, kind)
	lease := f.acquire(t, created.Metadata.CredentialRef, owner, actor)
	return f, f.modelReadRequest(t, actor, lease.LeaseID), lease
}
func resolveCount(t *testing.T, f *secretFixture) int {
	t.Helper()
	var count int
	if err := f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='secret.resolve'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func modelNoMaterial(t *testing.T, m sc.SecretMaterial, err error) {
	t.Helper()
	if err == nil {
		m.Destroy()
		t.Fatal("unexpected success")
	}
	called := false
	if m.Use(func([]byte) error { called = true; return nil }) == nil || called {
		m.Destroy()
		t.Fatal("failed read published material")
	}
}
func assertModelValue(t *testing.T, m sc.SecretMaterial, want string) {
	t.Helper()
	defer m.Destroy()
	if err := m.Use(func(b []byte) error {
		if string(b) != want {
			t.Fatal("wrong authenticated value")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSecretModelUsagePlannedReadAndAudit(t *testing.T) {
	for _, system := range []bool{false, true} {
		for _, kind := range []sc.LeaseOwnerKind{sc.ExecutionOwner, sc.ModelCallOwner} {
			t.Run(fmt.Sprintf("system=%v/%s", system, kind), func(t *testing.T) {
				f, r, _ := newModelReadCase(t, system, kind)
				s, tap := newModelReadService(t, f, f.store)
				checks := f.ports.checks
				m, err := s.ReadCredentialForUsage(auditContext(t), r)
				if err != nil {
					t.Fatal(err)
				}
				assertModelValue(t, m, "model-usage-material")
				if tap.calls != 1 || tap.entry.Fields().Associations.RequestID != r.RequestID || tap.entry.Fields().Actor.Details().ServiceName != identity.SecretService || tap.entry.Fields().Actor.Details().CauseRef != tap.key.Details().CauseRef || tap.key.Details().CauseRef == r.LeaseOwner.Details().ID || tap.key.Details().CauseRef == r.RequestID || r.Actor.Details().CauseRef != r.LeaseOwner.Details().ID || resolveCount(t, f.secretFixture) != 1 {
					t.Fatal("exact use/audit identity lost")
				}
				if !system && f.ports.checks != checks+1 {
					t.Fatal("new read did not reach real Project Secret checker")
				}
				t.Logf("exact Invocation association and independent resolution; Project checker calls=%d", f.ports.checks-checks)
			})
		}
	}
}
func TestSecretModelUsageRequestAndPlanIsolation(t *testing.T) {
	for _, mode := range []string{"zero-id", "other-invocation", "owner", "ref", "snapshot", "input", "process", "fence", "dispatch", "retired", "issuer", "mapping", "human-owner", "apply-read"} {
		t.Run(mode, func(t *testing.T) {
			f, r, _ := newModelReadCase(t, false, sc.ModelCallOwner)
			s, tap := newModelReadService(t, f, f.store)
			switch mode {
			case "zero-id":
				r.RequestID = ""
			case "other-invocation":
				other, actor := bindModelOwner(t, f.secretFixture, r.Ref, sc.Model, sc.ModelCallOwner)
				lease := f.acquire(t, r.Ref, other, actor)
				otherRequest := f.modelReadRequest(t, actor, lease.LeaseID)
				r.RequestID = otherRequest.RequestID
			case "owner":
				r.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, newID[struct{}](t).String())
			case "ref":
				r.Ref, _ = sc.NewCredentialRef(newID[sc.Credential](t), r.Ref.Details().Scope)
			case "snapshot":
				f.exec(t, `UPDATE audit_fixture.secret_invocations SET snapshot_id=$2 WHERE id=$1`, r.RequestID, newID[struct{}](t).String())
			case "input":
				f.exec(t, `UPDATE audit_fixture.secret_invocations SET input_digest='wrong' WHERE id=$1`, r.RequestID)
			case "process":
				f.exec(t, `UPDATE audit_fixture.secret_invocations SET process_id=$2 WHERE id=$1`, r.RequestID, newID[struct{}](t).String())
			case "fence":
				f.exec(t, `UPDATE audit_fixture.secret_invocations SET fence=fence+1 WHERE id=$1`, r.RequestID)
			case "dispatch":
				f.exec(t, `UPDATE audit_fixture.secret_invocations SET dispatch='sent' WHERE id=$1`, r.RequestID)
			case "retired":
				f.exec(t, `UPDATE audit_fixture.secret_invocations SET active=false WHERE id=$1`, r.RequestID)
			case "issuer":
				f.ports.transformPlan = func(_ sc.UsageRequest, p sc.UsageDependencies) (sc.UsageDependencies, error) {
					return sc.NewUsageDependencies(sc.NewPlanIssuer(), p.Binding(), p.Mapping(), p.RequiredLocks())
				}
			case "mapping":
				f.ports.transformPlan = func(_ sc.UsageRequest, p sc.UsageDependencies) (sc.UsageDependencies, error) {
					return sc.NewUsageDependencies(f.ports.planIssuer, p.Binding(), p.Binding(), p.RequiredLocks())
				}
			case "human-owner":
				r.Actor = f.actor
			case "apply-read":
				plan, err := s.DiscoverUsage(auditContext(t), r)
				if err != nil {
					t.Fatal(err)
				}
				result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
					if err := f.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
						return err
					}
					_, err := s.ApplyUsageInTx(ctx, tx, r, plan)
					return err
				})
				requireCode(t, result.Fault(), foundation.InvalidArgument)
				if tap.calls != 0 || resolveCount(t, f.secretFixture) != 0 {
					t.Fatal("Apply read mutated")
				}
				return
			}
			m, err := s.ReadCredentialForUsage(auditContext(t), r)
			modelNoMaterial(t, m, err)
			if mode == "issuer" || mode == "mapping" {
				requireCode(t, err, foundation.ResourceBusy)
			}
			if tap.calls != 0 || resolveCount(t, f.secretFixture) != 0 {
				t.Fatal("rejected request reached Audit")
			}
		})
	}
}
func TestSecretModelUsageLockMappingAndRevocation(t *testing.T) {
	t.Run("real-lock-then-revocation", func(t *testing.T) {
		f, r, _ := newModelReadCase(t, false, sc.ExecutionOwner)
		store := &modelReadStore{Store: f.store}
		s, tap := newModelReadService(t, f, store)
		store.unions = nil
		ctx := auditContext(t)
		held, release := make(chan struct{}), make(chan struct{})
		holder := make(chan foundation.CommitResult, 1)
		gate, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-model-invocation:"+r.RequestID)
		go func() {
			holder <- f.store.WithinTx(ctx, txCause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: gate, Mode: foundation.Exclusive}}); err != nil {
					return err
				}
				close(held)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				x, err := f.store.InTx(tx)
				if err != nil {
					return err
				}
				_, err = x.Exec(ctx, `UPDATE audit_fixture.secret_invocations SET active=false WHERE id=$1`, r.RequestID)
				return err
			})
		}()
		select {
		case <-held:
		case <-ctx.Done():
			t.Fatal("holder did not acquire actual gate")
		}
		type outcome struct {
			m   sc.SecretMaterial
			err error
		}
		done := make(chan outcome, 1)
		go func() { m, err := s.ReadCredentialForUsage(ctx, r); done <- outcome{m, err} }()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			var waiting bool
			if err := f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=$1 AND wait_event_type='Lock' AND cardinality(pg_blocking_pids(pid))>0)`, f.db.Name).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			select {
			case <-ticker.C:
			case got := <-done:
				got.m.Destroy()
				t.Fatalf("read did not wait: %v", got.err)
			case <-ctx.Done():
				t.Fatal("real lock wait missing")
			}
		}
		if resolveCount(t, f.secretFixture) != 0 {
			t.Fatal("Audit before lock admission")
		}
		close(release)
		if result := <-holder; result.State() != foundation.Committed {
			t.Fatal(result.Fault())
		}
		got := <-done
		modelNoMaterial(t, got.m, got.err)
		requireCode(t, got.err, foundation.Forbidden)
		store.mu.Lock()
		unions := store.unions
		store.mu.Unlock()
		if len(unions) != 1 {
			t.Fatalf("read acquired %d unions", len(unions))
		}
		plan, err := f.ports.DiscoverUsage(context.Background(), r)
		_ = plan
		if err == nil {
			t.Fatal("revocation was not canonical")
		}
		found := false
		for _, lock := range unions[0] {
			found = found || lock.Key.Canonical() == gate.Canonical()
		}
		if !found || tap.calls != 0 || resolveCount(t, f.secretFixture) != 0 {
			t.Fatal("full invocation gate or no-material boundary lost")
		}
		t.Log("actual PG blocking relationship observed; one full AcquireAll; post-lock canonical revocation rejected")
	})
	t.Run("missing-provider-lock-poisons", func(t *testing.T) {
		f, r, _ := newModelReadCase(t, false, sc.ModelCallOwner)
		s, tap := newModelReadService(t, f, f.store)
		gate, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-model-invocation:"+r.RequestID)
		f.ports.transformPlan = func(_ sc.UsageRequest, p sc.UsageDependencies) (sc.UsageDependencies, error) {
			var locks []foundation.LockRequest
			for _, l := range p.RequiredLocks() {
				if l.Key.Canonical() != gate.Canonical() {
					locks = append(locks, l)
				}
			}
			return sc.NewUsageDependencies(f.ports.planIssuer, p.Binding(), p.Mapping(), locks)
		}
		m, err := s.ReadCredentialForUsage(auditContext(t), r)
		modelNoMaterial(t, m, err)
		requireCode(t, err, foundation.InternalError)
		if tap.calls != 0 || resolveCount(t, f.secretFixture) != 0 {
			t.Fatal("missing gate bypass")
		}
	})
}
func TestSecretModelUsageLegacyWrappersStayClosed(t *testing.T) {
	for _, system := range []bool{false, true} {
		for _, kind := range []sc.LeaseOwnerKind{sc.ExecutionOwner, sc.ModelCallOwner} {
			t.Run(fmt.Sprintf("system=%v/%s", system, kind), func(t *testing.T) {
				f, r, lease := newModelReadCase(t, system, kind)
				for _, action := range []sc.UsageAction{sc.AcquireLeaseUsage, sc.ReleaseLeaseUsage} {
					request := r
					request.Action = action
					request.RequestID = ""
					plan, err := f.secret.DiscoverUsage(auditContext(t), request)
					if err != nil {
						t.Fatal(err)
					}
					var rawErr error
					result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
						if err := f.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
							return err
						}
						if action == sc.AcquireLeaseUsage {
							_, rawErr = f.secret.AcquireCredentialLeaseInTx(ctx, tx, r.Actor, r.Ref, r.LeaseOwner)
						} else {
							rawErr = f.secret.ReleaseCredentialLeaseInTx(ctx, tx, r.Actor, r.LeaseID)
						}
						return rawErr
					})
					requireCode(t, result.Fault(), foundation.ResourceBusy)
					var e *secret.Error
					if !errors.As(rawErr, &e) || e.Code() != secret.PreparationRequired {
						t.Fatal("legacy bypass not explicitly closed")
					}
				}
				m, err := f.secret.ReadCredentialForRequest(auditContext(t), r.Actor, r.LeaseID)
				modelNoMaterial(t, m, err)
				requireCode(t, err, foundation.ResourceBusy)
				var released bool
				if err = f.store.QueryRow(auditContext(t), `SELECT released FROM agenteam_secret.secret_leases WHERE id=$1`, r.LeaseID.String()).Scan(&released); err != nil || released {
					t.Fatal("legacy changed lease", err)
				}
				_, commit := f.applyModelUsage(t, f.secret, f.modelLeaseRequest(r.Actor, lease, r.LeaseOwner, sc.ReleaseLeaseUsage))
				if commit.State() != foundation.Committed {
					t.Fatal(commit.Fault())
				}
				commit = f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
					_, err := f.secret.AcquireCredentialLeaseInTx(ctx, tx, r.Actor, r.Ref, r.LeaseOwner)
					return err
				})
				requireCode(t, commit.Fault(), foundation.ResourceBusy)
				_, commit = f.applyModelUsage(t, f.secret, f.modelLeaseRequest(r.Actor, lease, r.LeaseOwner, sc.AcquireLeaseUsage))
				requireCode(t, commit.Fault(), foundation.InvalidState)
				if err = f.store.QueryRow(auditContext(t), `SELECT released FROM agenteam_secret.secret_leases WHERE id=$1`, r.LeaseID.String()).Scan(&released); err != nil || !released || resolveCount(t, f.secretFixture) != 0 {
					t.Fatal("released lease resurrected", err)
				}
			})
		}
	}
	t.Run("earlier-authority-errors", func(t *testing.T) {
		f, r, _ := newModelReadCase(t, false, sc.ModelCallOwner)
		for _, mode := range []string{"unbound", "wrong-cause", "human", "grant-purpose", "uninitialized"} {
			t.Run(mode, func(t *testing.T) {
				f.ports.grant = nil
				service := f.secret
				actor := r.Actor
				want := foundation.Forbidden
				switch mode {
				case "unbound":
					var err error
					service, err = secret.New(f.store, masterKeys(t, 1, 1), f.service, secret.Authorizations{})
					if err != nil {
						t.Fatal(err)
					}
					want = foundation.DependencyUnbound
				case "wrong-cause":
					reg, _ := identity.RegisterService(identity.SecretService)
					actor, _ = reg.Actor(newID[struct{}](t).String(), r.Ref.Details().Scope)
				case "human":
					actor = f.actor
				case "grant-purpose":
					f.ports.grant = func(g *sc.UseGrant) { g.Consumer = sc.MCP }
				case "uninitialized":
					var err error
					service, err = secret.New(f.store, masterKeys(t, 1, 1), f.service, secret.Authorizations{Sessions: f.ports, System: f.ports, Projects: f.ports, Usage: f.ports})
					if err != nil {
						t.Fatal(err)
					}
					want = foundation.DependencyUnavailable
				}
				commit := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
					_, err := service.AcquireCredentialLeaseInTx(ctx, tx, actor, r.Ref, r.LeaseOwner)
					return err
				})
				requireCode(t, commit.Fault(), want)
				if resolveCount(t, f.secretFixture) != 0 {
					t.Fatal("early authority rejection produced Audit")
				}
			})
		}
	})
	t.Run("mcp-execution-legacy-still-works", func(t *testing.T) {
		f := newSecretProjectAuditFixture(t)
		write := f.request(t, sc.Create, []byte("mcp-current"))
		write.Purpose = sc.MCP
		created, err := f.secret.ExecuteWrite(auditContext(t), write)
		if err != nil {
			t.Fatal(err)
		}
		owner, actor := bindModelOwner(t, f.secretFixture, created.Metadata.CredentialRef, sc.MCP, sc.ExecutionOwner)
		lease := f.acquire(t, created.Metadata.CredentialRef, owner, actor)
		m, err := f.secret.ReadCredentialForRequest(auditContext(t), actor, lease.LeaseID)
		if err != nil {
			t.Fatal(err)
		}
		assertModelValue(t, m, "mcp-current")
		commit := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
			return f.secret.ReleaseCredentialLeaseInTx(ctx, tx, actor, lease.LeaseID)
		})
		if commit.State() != foundation.Committed {
			t.Fatal(commit.Fault())
		}
	})
}
func TestSecretModelUsageAuditWitnessAndNoMaterial(t *testing.T) {
	for _, mode := range []string{"request-association", "missing-proof", "cross-store", "grant-request-id", "system-human-subject", "audit-error", "aead"} {
		t.Run(mode, func(t *testing.T) {
			f, r, _ := newModelReadCase(t, mode == "system-human-subject", sc.ModelCallOwner)
			s, tap := newModelReadService(t, f, f.store)
			checks := f.ports.checks
			switch mode {
			case "request-association":
				tap.before = func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey, error) {
					fields := e.Fields()
					fields.Associations.RequestID = newID[struct{}](t).String()
					changed, err := ac.NewEntry(fields)
					return ctx, changed, k, err
				}
			case "missing-proof":
				tap.before = func(_ context.Context, _ foundation.Tx, e ac.Entry, k ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey, error) {
					return auditContext(t), e, k, nil
				}
			case "cross-store":
				other := openAuditStore(t, f.db.Config(t, nil))
				checker, err := secret.NewProjectAuditAuthority(other)
				if err != nil {
					t.Fatal(err)
				}
				f.ports.checker = checker
			case "grant-request-id":
				f.ports.grant = func(g *sc.UseGrant) { g.RequestID = r.RequestID }
			case "system-human-subject":
				f.ports.grant = func(g *sc.UseGrant) { g.Subject = f.actor }
			case "audit-error":
				tap.before = func(ctx context.Context, _ foundation.Tx, e ac.Entry, k ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey, error) {
					return ctx, e, k, deny(foundation.DependencyUnavailable)
				}
			case "aead":
				f.exec(t, `UPDATE agenteam_secret.secret_payloads SET ciphertext=set_byte(ciphertext,0,get_byte(ciphertext,0)#1) WHERE payload_id=(SELECT current_payload_id FROM agenteam_secret.secrets WHERE id=$1)`, r.Ref.Details().ID.String())
			}
			m, err := s.ReadCredentialForUsage(auditContext(t), r)
			modelNoMaterial(t, m, err)
			if resolveCount(t, f.secretFixture) != 0 {
				t.Fatal("rejected read left Audit")
			}
			if mode == "request-association" || mode == "missing-proof" || mode == "cross-store" {
				if tap.calls != 1 || f.ports.checks != checks+1 {
					t.Fatal("negative did not reach real Secret checker")
				}
			}
		})
	}
	t.Run("cross-tx-old-witness", func(t *testing.T) {
		f, r, _ := newModelReadCase(t, false, sc.ModelCallOwner)
		s, tap := newModelReadService(t, f, f.store)
		m, err := s.ReadCredentialForUsage(auditContext(t), r)
		if err != nil {
			t.Fatal(err)
		}
		m.Destroy()
		oldCtx, entry, key := tap.ctx, tap.entry, tap.key
		plan, err := s.DiscoverUsage(auditContext(t), r)
		if err != nil {
			t.Fatal(err)
		}
		commit := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
				return err
			}
			return f.ports.checker.CheckProjectAuditInTx(oldCtx, tx, entry, key)
		})
		requireCode(t, commit.Fault(), foundation.Forbidden)
		if resolveCount(t, f.secretFixture) != 1 {
			t.Fatal("reused witness changed Audit")
		}
	})
}
func TestSecretModelUsageOutcomeAndCurrentValue(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("decorated-unknown/rollback=%v", rollback), func(t *testing.T) {
			f, r, _ := newModelReadCase(t, false, sc.ModelCallOwner)
			store := &modelReadStore{Store: f.store, marker: newID[foundation.TransactionAttempt](t), rollback: rollback}
			s, tap := newModelReadService(t, f, store)
			store.armed = true
			m, err := s.ReadCredentialForUsage(auditContext(t), r)
			modelNoMaterial(t, m, err)
			requireCode(t, err, foundation.CommitUnknown)
			var original interface {
				error
				AttemptID() foundation.ID[foundation.TransactionAttempt]
				Cause() foundation.TransactionCause
			}
			if !errors.As(err, &original) || original.AttemptID() != store.returned.AttemptID() || original.Cause().Details().RecoveryRunID != store.returned.Cause().Details().RecoveryRunID || original.Cause().Details().Owner != "secret-resolution" {
				t.Fatal("original returned UnknownResult changed")
			}
			want := 1
			state := foundation.Committed
			if rollback {
				want = 0
				state = foundation.NotCommitted
			}
			if store.actual.State() != state || resolveCount(t, f.secretFixture) != want || tap.calls != 1 {
				t.Fatal("actual PG commit/rollback evidence missing")
			}
			prior := tap.key.Details().CauseRef
			store.armed = false
			m, err = s.ReadCredentialForUsage(auditContext(t), r)
			if err != nil {
				t.Fatal(err)
			}
			assertModelValue(t, m, "model-usage-material")
			if tap.key.Details().CauseRef == prior || tap.entry.Fields().Associations.RequestID != r.RequestID || resolveCount(t, f.secretFixture) != want+1 {
				t.Fatal("new exact read reused resolution or changed Invocation")
			}
			t.Logf("result decoration only: actual Tx=%s, returned marker preserved; no physical backend attempt claim", state)
		})
	}
	t.Run("committed-then-cancelled", func(t *testing.T) {
		f, r, _ := newModelReadCase(t, false, sc.ModelCallOwner)
		store := &modelReadStore{Store: f.store}
		s, tap := newModelReadService(t, f, store)
		ctx, cancel := context.WithCancel(auditContext(t))
		defer cancel()
		store.afterCommit = cancel
		m, err := s.ReadCredentialForUsage(ctx, r)
		modelNoMaterial(t, m, err)
		var fault *foundation.Fault
		if !errors.Is(err, context.Canceled) || !errors.As(err, &fault) || fault.Code != foundation.DependencyUnavailable || fault.CommitState != foundation.Committed || store.actual.State() != foundation.Committed || tap.calls != 1 || resolveCount(t, f.secretFixture) != 1 {
			t.Fatal("committed/cancelled boundary or real Audit fact lost", err)
		}
		t.Log("real PG Committed, original caller cancelled before Store return, zero material; original Audit remains committed")
	})
	t.Run("current-value-and-issued-material", func(t *testing.T) {
		f, r, _ := newModelReadCase(t, false, sc.ExecutionOwner)
		s, _ := newModelReadService(t, f, f.store)
		old, err := s.ReadCredentialForUsage(auditContext(t), r)
		if err != nil {
			t.Fatal(err)
		}
		defer old.Destroy()
		write := f.request(t, sc.Update, []byte("rotated-current"))
		write.Ref = r.Ref
		write.ExpectedVersion = 1
		if _, err = f.secret.ExecuteWrite(auditContext(t), write); err != nil {
			t.Fatal(err)
		}
		current, err := s.ReadCredentialForUsage(auditContext(t), r)
		if err != nil {
			t.Fatal(err)
		}
		assertModelValue(t, current, "rotated-current")
		assertModelValue(t, old, "model-usage-material")
	})
}
