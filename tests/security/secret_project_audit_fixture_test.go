//go:build integration

package security_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// Only these test ports own audit_fixture.* facts. They isolate the still
// unbound production Project/Usage adapters; the Secret checker, services,
// encryption, rows and transactions below are the actual production code.
type secretProjectAuditPorts struct {
	*secretAuthority
	checker *secret.ProjectAuditAuthority
	before  func(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) (ac.Entry, ac.AppendKey, error)
	grant   func(*sc.UseGrant)
	checks  int
}

// The two formal Cleanup ports have different cause types. Neither is bound
// by this checker fixture; the wrapper keeps their contracts separate.
type secretProjectAuditAppendPorts struct{ *secretProjectAuditPorts }

func (*secretProjectAuditAppendPorts) CheckCleanupInTx(context.Context, foundation.Tx, identity.Actor, ac.LifecycleCause, identity.ProjectID) error {
	return deny(foundation.DependencyUnbound)
}
func (*secretProjectAuditPorts) CheckCleanupInTx(context.Context, foundation.Tx, identity.Actor, sc.LifecycleCause, identity.ProjectID) error {
	return deny(foundation.DependencyUnbound)
}
func (*secretProjectAuditPorts) CheckServiceLookup(context.Context, identity.Actor, identity.Scope, ac.AppendKey) error {
	return deny(foundation.DependencyUnbound)
}

func (p *secretProjectAuditPorts) RequireCurrentSession(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return deny(foundation.Unauthenticated)
	}
	if tx.Valid() {
		key, _ := foundation.UserLock(actor.Details().UserID)
		if err := p.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); err != nil {
			return err
		}
	}
	return p.auditAuthority.RequireCurrentSession(ctx, tx, actor)
}
func (p *secretProjectAuditPorts) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, id identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if id.Validate() != nil || actor.Details().Kind != identity.Human || !tx.Valid() && intent != identity.Read {
		return identity.AccessGrant{}, deny(foundation.Forbidden)
	}
	if err := p.RequireCurrentSession(ctx, tx, actor); err != nil {
		return identity.AccessGrant{}, err
	}
	if tx.Valid() {
		key, _ := foundation.ProjectLock(id.String())
		if err := p.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); err != nil {
			return identity.AccessGrant{}, err
		}
	}
	x, err := p.executor(tx)
	if err != nil {
		return identity.AccessGrant{}, err
	}
	var owner, state string
	err = x.QueryRow(ctx, `SELECT owner_id::text,state FROM audit_fixture.projects WHERE id=$1`, id.String()).Scan(&owner, &state)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && owner != actor.Details().UserID {
		return identity.AccessGrant{}, deny(foundation.NotFound)
	}
	if err != nil {
		return identity.AccessGrant{}, err
	}
	if intent != identity.Read && intent != identity.Mutate || state == "deleting" || state == "deleted" || intent == identity.Mutate && state != "active" {
		return identity.AccessGrant{}, deny(foundation.ProjectNotActive)
	}
	scope, _ := identity.InProject(id)
	return grant(actor, scope, intent)
}
func (p *secretProjectAuditPorts) CheckMutationInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef) error {
	id, err := foundation.ParseID[identity.Project](ref.Details().Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	_, err = p.AuthorizeProject(ctx, tx, actor, id, identity.Mutate)
	return err
}
func (p *secretProjectAuditPorts) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	key, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, ref.Details().ID.String())
	mode := foundation.Exclusive
	if action == sc.ReadLease {
		mode = foundation.Shared
	}
	locks := []foundation.LockRequest{{Key: key, Mode: mode}}
	if ref.Details().Scope.Details().Kind == identity.ProjectScope {
		key, _ = foundation.ProjectLock(ref.Details().Scope.Details().ProjectID)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	if err := p.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return sc.UseGrant{}, err
	}
	g, err := p.secretAuthority.AuthorizeLeaseInTx(ctx, tx, actor, ref, owner, action)
	if err == nil && p.grant != nil {
		p.grant(&g)
	}
	return g, err
}
func (p *secretProjectAuditPorts) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	f := entry.Fields()
	if f.Scope.Details().Kind != identity.ProjectScope || key.Details().Producer != ac.SecretProducer {
		return deny(foundation.Forbidden)
	}
	id, err := foundation.ParseID[identity.Project](f.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	lock, _ := foundation.ProjectLock(id.String())
	if err = p.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Shared}}); err != nil {
		return err
	}
	switch f.Actor.Details().Kind {
	case identity.Human:
		if _, err = p.AuthorizeProject(ctx, tx, f.Actor, id, identity.Mutate); err != nil {
			return err
		}
	case identity.Service:
		if f.Action != ac.SecretResolve || f.Actor.Details().ServiceName != identity.SecretService || f.Actor.Details().ProjectID != id.String() || f.Actor.Details().CauseRef != key.Details().CauseRef {
			return deny(foundation.Forbidden)
		}
		x, e := p.store.InTx(tx)
		if e != nil {
			return e
		}
		var state string
		if e = x.QueryRow(ctx, `SELECT state FROM audit_fixture.projects WHERE id=$1`, id.String()).Scan(&state); e != nil {
			return e
		}
		if state != "active" {
			return deny(foundation.ProjectNotActive)
		}
	default:
		return deny(foundation.DependencyUnbound)
	}
	if p.before != nil {
		entry, key, err = p.before(ctx, tx, entry, key)
		if err != nil {
			return err
		}
	}
	p.checks++
	return p.checker.CheckProjectAuditInTx(ctx, tx, entry, key)
}

type secretProjectAuditFixture struct {
	*secretFixture
	ports *secretProjectAuditPorts
}

func newSecretProjectAuditFixture(t *testing.T) *secretProjectAuditFixture {
	t.Helper()
	f := newSecretFixture(t)
	s, p := projectAuditServiceOnStore(t, f.store)
	f.secret = s
	return &secretProjectAuditFixture{f, p}
}

func projectAuditServiceOnStore(t *testing.T, store *postgres.Store) (*secret.Service, *secretProjectAuditPorts) {
	t.Helper()
	checker, err := secret.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	p := &secretProjectAuditPorts{secretAuthority: &secretAuthority{auditAuthority: &auditAuthority{store: store}}, checker: checker}
	a, err := audit.New(store, auditKeys(t), audit.Authorizations{Sessions: p, System: p, Projects: &secretProjectAuditAppendPorts{p}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := secret.New(store, masterKeys(t, 1, 1), a, secret.Authorizations{Sessions: p, System: p, Projects: p, Usage: p})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.StopMaintenance)
	return s, p
}

func projectAuditProxyStore(t *testing.T, f *secretProjectAuditFixture, address net.Addr) *postgres.Store {
	t.Helper()
	u, err := url.Parse(f.db.Fixture.URL(f.db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = address.String()
	return openAuditStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
}

type secretProjectAuditCounts struct{ canonical, receipts, audits int }

func (f *secretProjectAuditFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.store.Exec(auditContext(t), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *secretProjectAuditFixture) counts(t *testing.T) secretProjectAuditCounts {
	t.Helper()
	var c secretProjectAuditCounts
	err := f.store.QueryRow(auditContext(t), `SELECT (SELECT count(*) FROM agenteam_secret.secrets WHERE project_id=$1),(SELECT count(*) FROM agenteam_secret.secret_command_receipts WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='secret')`, f.project.String()).Scan(&c.canonical, &c.receipts, &c.audits)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func projectAuditNoMaterial(t *testing.T, material sc.SecretMaterial, err error) {
	t.Helper()
	defer material.Destroy()
	if err == nil {
		t.Fatal("failure returned success")
	}
	if material.Use(func([]byte) error { return nil }) == nil {
		t.Fatal("unconfirmed material escaped")
	}
}

// Reacquiring the original writer's gates establishes its real terminal
// serialization before observing committed/absence; a SELECT alone does not.
func projectAuditJoinWriter(t *testing.T, store *postgres.Store, locks []foundation.LockRequest) {
	t.Helper()
	r := store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error { return store.AcquireAll(ctx, tx, locks) })
	if r.State() != foundation.Committed {
		t.Fatal("original writer did not terminate", r.Fault())
	}
}
