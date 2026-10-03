//go:build integration

package security_test

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

func auditContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func newID[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	id, err := foundation.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func auditKeys(t *testing.T) cursor.Keyring {
	t.Helper()
	k, e := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	return k
}

type auditFixture struct {
	db      *pgfixture.Database
	store   *postgres.Store
	service *audit.Service
	auth    *auditAuthority
	actor   identity.Actor
	project identity.ProjectID
	scope   identity.Scope
}

func openAuditStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	s, err := postgres.Open(auditContext(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.ForceClose(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func newAuditFixture(t *testing.T) *auditFixture {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	m, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state := m.Migrate(auditContext(t)); !state.Migrated {
		t.Fatal(state.Fault)
	}
	store := openAuditStore(t, cfg)
	// These are test-only authority facts. Production migrations do not create
	// identity/Project data or bind a permissive authority.
	_, err = store.Exec(auditContext(t), `CREATE SCHEMA audit_fixture;
 CREATE TABLE audit_fixture.sessions(id uuid PRIMARY KEY, user_id uuid NOT NULL,active boolean NOT NULL);
 CREATE TABLE audit_fixture.projects(id uuid PRIMARY KEY,owner_id uuid NOT NULL,state text NOT NULL,stopped boolean NOT NULL DEFAULT false,operation_id uuid,version bigint NOT NULL DEFAULT 1);
 CREATE TABLE audit_fixture.system_access(user_id uuid PRIMARY KEY,allowed boolean NOT NULL);
 CREATE TABLE audit_fixture.business(id integer PRIMARY KEY,value integer NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	user, session, project := newID[identity.User](t), newID[identity.Session](t), newID[identity.Project](t)
	actor, _ := identity.NewHuman(user, session)
	scope, _ := identity.InProject(project)
	if _, err := store.Exec(auditContext(t), `INSERT INTO audit_fixture.sessions VALUES($1,$2,true)`, session.String(), user.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(auditContext(t), `INSERT INTO audit_fixture.projects(id,owner_id,state) VALUES($1,$2,'active')`, project.String(), user.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(auditContext(t), `INSERT INTO audit_fixture.system_access VALUES($1,true)`, user.String()); err != nil {
		t.Fatal(err)
	}
	auth := &auditAuthority{store: store}
	s, err := audit.New(store, auditKeys(t), audit.Authorizations{Sessions: auth, System: auth, Projects: auth})
	if err != nil {
		t.Fatal(err)
	}
	return &auditFixture{db, store, s, auth, actor, project, scope}
}

type auditAuthority struct{ store *postgres.Store }

func (a *auditAuthority) executor(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx.Valid() {
		return a.store.InTx(tx)
	}
	return a.store, nil
}
func deny(code foundation.Code) error { return foundation.NewFault(code, foundation.NotStarted) }
func (a *auditAuthority) RequireCurrentSession(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	d := actor.Details()
	var active bool
	err = e.QueryRow(ctx, `SELECT active FROM audit_fixture.sessions WHERE id=$1 AND user_id=$2`, d.SessionID, d.UserID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !active {
		return deny(foundation.SessionRevoked)
	}
	return err
}
func grant(actor identity.Actor, scope identity.Scope, intent identity.AccessIntent) (identity.AccessGrant, error) {
	at, _ := foundation.NewInstant(time.Now())
	return identity.NewAccessGrant(actor, scope, intent, at, 1)
}
func (a *auditAuthority) AuthorizeSystem(ctx context.Context, tx foundation.Tx, actor identity.Actor, intent identity.AccessIntent) (identity.AccessGrant, error) {
	e, err := a.executor(tx)
	if err != nil {
		return identity.AccessGrant{}, err
	}
	var allowed bool
	err = e.QueryRow(ctx, `SELECT allowed FROM audit_fixture.system_access WHERE user_id=$1`, actor.Details().UserID).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return identity.AccessGrant{}, deny(foundation.Forbidden)
	}
	if err != nil {
		return identity.AccessGrant{}, err
	}
	return grant(actor, identity.SystemScope(), intent)
}
func (a *auditAuthority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, id identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	e, err := a.executor(tx)
	if err != nil {
		return identity.AccessGrant{}, err
	}
	var owner, state string
	err = e.QueryRow(ctx, `SELECT owner_id::text,state FROM audit_fixture.projects WHERE id=$1`, id.String()).Scan(&owner, &state)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (owner != actor.Details().UserID || state == "deleted" || state == "deleting") {
		return identity.AccessGrant{}, deny(foundation.NotFound)
	}
	if err != nil {
		return identity.AccessGrant{}, err
	}
	scope, _ := identity.InProject(id)
	return grant(actor, scope, intent)
}
func (a *auditAuthority) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	var state string
	err = e.QueryRow(ctx, `SELECT state FROM audit_fixture.projects WHERE id=$1`, entry.Fields().Scope.Details().ProjectID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && state != "active" {
		return deny(foundation.ProjectNotActive)
	}
	return err
}
func (a *auditAuthority) CheckServiceLookup(ctx context.Context, actor identity.Actor, scope identity.Scope, key ac.AppendKey) error {
	var state string
	err := a.store.QueryRow(ctx, `SELECT state FROM audit_fixture.projects WHERE id=$1`, scope.Details().ProjectID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (state == "deleting" || state == "deleted") {
		return deny(foundation.NotFound)
	}
	return err
}
func (a *auditAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause ac.LifecycleCause, id identity.ProjectID) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	var allowed bool
	c := cause.Details()
	err = e.QueryRow(ctx, `SELECT state='deleting' AND stopped AND operation_id=$2 AND version=$3 FROM audit_fixture.projects WHERE id=$1`, id.String(), c.OperationID.String(), int64(c.ProjectVersion)).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return deny(foundation.InvalidState)
	}
	return err
}
func (f *auditFixture) entry(t *testing.T, actor identity.Actor, scope identity.Scope) ac.Entry {
	t.Helper()
	r, _ := ac.NewResource(ac.SecretResource, newID[struct{}](t).String())
	m, _ := ac.SecretMutationMetadata(ac.SecretCreate, 1, []ac.ChangedField{ac.ValueChanged})
	e, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: ac.SecretCreate, Outcome: ac.Success, Resource: r, Metadata: m})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func appendKey(t *testing.T, producer ac.Producer) ac.AppendKey {
	t.Helper()
	key, err := ac.NewAppendKey(producer, newID[struct{}](t).String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func txCause(t *testing.T) foundation.TransactionCause {
	t.Helper()
	c, e := foundation.NewRecoveryCause("audit-fixture", newID[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func appendAudit(t *testing.T, f *auditFixture, entry ac.Entry, key ac.AppendKey) (ac.AppendReceipt, foundation.CommitResult) {
	t.Helper()
	var receipt ac.AppendReceipt
	result := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		if entry.Fields().Scope.Details().Kind == identity.ProjectScope {
			lock, _ := foundation.ProjectLock(entry.Fields().Scope.Details().ProjectID)
			if err := f.store.Acquire(ctx, tx, lock, foundation.Shared); err != nil {
				return err
			}
		}
		var err error
		receipt, err = f.service.AppendInTx(ctx, tx, entry, key)
		return err
	})
	return receipt, result
}
func requireCode(t *testing.T, err error, code foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
