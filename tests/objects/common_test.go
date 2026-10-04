//go:build integration

package objects_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
)

func id[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	v, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func contextFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func digest(b []byte) foundation.Digest {
	sum := sha256.Sum256(b)
	return foundation.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
func fault(code foundation.Code) error { return foundation.NewFault(code, foundation.NotStarted) }
func requireCode(t *testing.T, e error, code foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(e, &f) || f.Code != code {
		t.Fatalf("expected %s, got %v", code, e)
	}
}
func cause(t *testing.T) foundation.TransactionCause {
	t.Helper()
	c, e := foundation.NewRecoveryCause("object-test", id[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func command(t *testing.T, key string) foundation.CommandMeta {
	return foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: foundation.IdempotencyKey(key)}
}

type fixture struct {
	db        *pgfixture.Database
	store     *postgres.Store
	service   *object.Service
	backend   *object.Backend
	spool     *object.Spool
	authority *authority
	actor     identity.Actor
	owner     oc.ObjectOwner
	project   identity.ProjectID
	bucket    string
	remote    *objectfixture.Descriptor
	s3        *minio.Client
	transport *http.Transport
	config    object.StorageConfig
}

func newFixture(t *testing.T, prospective bool) *fixture {
	t.Helper()
	ctx := contextFor(t)
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	m, e := postgres.NewMigrator(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if r := m.Migrate(ctx); !r.Migrated {
		t.Fatalf("migration: %v", r.Fault)
	}
	store, e := postgres.Open(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := store.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	_, e = store.Exec(ctx, `CREATE SCHEMA object_fixture;
 CREATE TABLE object_fixture.agents(id uuid PRIMARY KEY,project_id uuid NOT NULL,active boolean NOT NULL DEFAULT true);
 CREATE TABLE object_fixture.executions(id uuid PRIMARY KEY,agent_id uuid NOT NULL,project_id uuid NOT NULL,active boolean NOT NULL DEFAULT true);
 CREATE TABLE object_fixture.sessions(id uuid PRIMARY KEY,user_id uuid NOT NULL,active boolean NOT NULL DEFAULT true);
 CREATE TABLE object_fixture.projects(id uuid PRIMARY KEY,owner_id uuid NOT NULL,state text NOT NULL DEFAULT 'active',operation_id uuid,version bigint NOT NULL DEFAULT 1);
 CREATE TABLE object_fixture.owners(id uuid PRIMARY KEY,kind text NOT NULL,project_id uuid,user_id uuid,existence text NOT NULL,cause uuid,parent_id uuid,version bigint NOT NULL DEFAULT 1);
 CREATE TABLE object_fixture.uses(id uuid PRIMARY KEY,object_id uuid NOT NULL,kind text NOT NULL,lease_id uuid,owner_id uuid NOT NULL,active boolean NOT NULL DEFAULT true,terminal boolean NOT NULL DEFAULT false);
 CREATE TABLE object_fixture.cleanup(operation_id uuid PRIMARY KEY,object_id uuid NOT NULL,owner_id uuid NOT NULL);`)
	if e != nil {
		t.Fatal(e)
	}
	user, session, project := id[identity.User](t), id[identity.Session](t), id[identity.Project](t)
	actor, _ := identity.NewHuman(user, session)
	owner, _ := oc.NewObjectOwner(oc.Artifact, id[struct{}](t).String(), project.String())
	_, e = store.Exec(ctx, `INSERT INTO object_fixture.sessions(id,user_id) VALUES($1,$2)`, session.String(), user.String())
	if e != nil {
		t.Fatal(e)
	}
	_, e = store.Exec(ctx, `INSERT INTO object_fixture.projects(id,owner_id) VALUES($1,$2)`, project.String(), user.String())
	if e != nil {
		t.Fatal(e)
	}
	existence := "existing"
	if prospective {
		existence = "prospective"
	}
	_, e = store.Exec(ctx, `INSERT INTO object_fixture.owners(id,kind,project_id,user_id,existence,cause) VALUES($1,$2,$3,$4,$5,$6)`, owner.Details().ID, string(owner.Details().Kind), project.String(), user.String(), existence, id[struct{}](t).String())
	if e != nil {
		t.Fatal(e)
	}
	remote, e := objectfixture.Load()
	if e != nil {
		t.Fatal(e)
	}
	s3, transport, e := remote.Client()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(transport.CloseIdleConnections)
	suffix, e := pgfixture.RandomHex(8)
	if e != nil {
		t.Fatal(e)
	}
	bucket := "d05-" + remote.Nonce[:12] + "-" + suffix
	if e = s3.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); e != nil {
		t.Fatal("owned bucket creation failed")
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	storageCfg, e := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if e != nil {
		t.Fatal(e)
	}
	backend, e := object.NewBackend(storageCfg)
	if e != nil {
		t.Fatal(e)
	}
	process := id[oc.Process](t)
	spool, e := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), process)
	if e != nil {
		t.Fatal(e)
	}
	auth := &authority{store: store}
	keys, e := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	auditing, e := audit.New(store, keys, audit.Authorizations{Projects: auditAuthority{auth}})
	if e != nil {
		t.Fatal(e)
	}
	service, e := object.New(store, backend, spool, auditing, object.Authorizations{Planner: auth, Resources: auth, Read: auth, Gate: auth, Cleanup: auth, Leases: auth})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := service.Drain(ctx); e != nil {
			_ = service.Force(ctx)
			t.Error(e)
		}
	})
	if e = service.Initialize(ctx); e != nil {
		t.Fatalf("initialize: %v", e)
	}
	return &fixture{db, store, service, backend, spool, auth, actor, owner, project, bucket, remote, s3, transport, storageCfg}
}
func (f *fixture) put(t *testing.T, key, body string) oc.PutResult {
	t.Helper()
	r, e := f.service.PutObject(contextFor(t), f.actor, f.owner, command(t, key), "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
	if e != nil {
		t.Fatalf("put: %v", e)
	}
	return r
}
func (f *fixture) sql(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, e := f.store.Exec(contextFor(t), query, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *fixture) physical(t *testing.T, object oc.ObjectID) string {
	t.Helper()
	var key string
	if e := f.store.QueryRow(contextFor(t), `SELECT candidate_key FROM agenteam_object.objects WHERE id=$1`, object.String()).Scan(&key); e != nil {
		t.Fatal(e)
	}
	return key
}

type authority struct{ store *postgres.Store }

func (a *authority) exec(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx.Valid() {
		return a.store.InTx(tx)
	}
	return a.store, nil
}
func (a *authority) current(ctx context.Context, e postgres.SQLExecutor, actor identity.Actor) error {
	d := actor.Details()
	if d.Kind == identity.AgentRun {
		var active bool
		err := e.QueryRow(ctx, `SELECT a.active AND x.active FROM object_fixture.agents a JOIN object_fixture.executions x ON x.agent_id=a.id WHERE a.id=$1 AND x.id=$2 AND a.project_id=$3 AND x.project_id=$3`, d.AgentID, d.ExecutionID, d.ProjectID).Scan(&active)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !active {
			return fault(foundation.Forbidden)
		}
		return err
	}
	if d.Kind != identity.Human {
		return fault(foundation.Forbidden)
	}
	var ok bool
	err := e.QueryRow(ctx, `SELECT active FROM object_fixture.sessions WHERE id=$1 AND user_id=$2`, d.SessionID, d.UserID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !ok {
		return fault(foundation.SessionRevoked)
	}
	return err
}
func (a *authority) AuthorizeOwner(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) (oc.OwnerAuthorization, error) {
	return a.AuthorizeOwnerInTx(ctx, foundation.Tx{}, actor, owner, intent)
}
func (a *authority) AuthorizeOwnerInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) (oc.OwnerAuthorization, error) {
	e, err := a.exec(tx)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if err = a.current(ctx, e, actor); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var kind, project, user, existence, creation string
	var version int64
	err = e.QueryRow(ctx, `SELECT kind,coalesce(project_id::text,''),user_id::text,existence,coalesce(cause::text,''),version FROM object_fixture.owners WHERE id=$1`, owner.Details().ID).Scan(&kind, &project, &user, &existence, &creation, &version)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (kind != string(owner.Details().Kind) || project != owner.Details().ProjectID || actor.Details().Kind == identity.Human && user != actor.Details().UserID || actor.Details().Kind == identity.AgentRun && project != actor.Details().ProjectID) {
		return oc.OwnerAuthorization{}, fault(foundation.Forbidden)
	}
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if project != "" {
		var actual string
		err = e.QueryRow(ctx, `SELECT owner_id::text FROM object_fixture.projects WHERE id=$1`, project).Scan(&actual)
		if err != nil {
			return oc.OwnerAuthorization{}, err
		}
		if actor.Details().Kind == identity.Human && actual != actor.Details().UserID {
			return oc.OwnerAuthorization{}, fault(foundation.Forbidden)
		}
	}
	if oc.OwnerKind(kind) == oc.ExecutionPayload {
		var active bool
		err = e.QueryRow(ctx, `SELECT x.active FROM object_fixture.owners o JOIN object_fixture.executions x ON x.id=o.parent_id WHERE o.id=$1`, owner.Details().ID).Scan(&active)
		if err != nil || !active {
			return oc.OwnerAuthorization{}, fault(foundation.Forbidden)
		}
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: oc.OwnerExistence(existence), CreationCause: creation, Version: foundation.Version(version)})
}
func (a *authority) CheckInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) error {
	if owner.Details().ProjectID == "" {
		return nil
	}
	e, err := a.exec(tx)
	if err != nil {
		return err
	}
	var state string
	err = e.QueryRow(ctx, `SELECT state FROM object_fixture.projects WHERE id=$1`, owner.Details().ProjectID).Scan(&state)
	if err != nil {
		return err
	}
	if state == "active" || state == "archived" && intent == identity.Read || state == "deleting" && (intent == identity.Converge || intent == identity.Lifecycle) {
		return nil
	}
	return fault(foundation.ProjectNotActive)
}
func (a *authority) AuthorizeObjectReadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, object oc.ObjectID) (oc.OwnerAuthorization, error) {
	grant, err := a.AuthorizeOwnerInTx(ctx, tx, actor, owner, identity.Read)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	e, err := a.exec(tx)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var lease, use, kind string
	err = e.QueryRow(ctx, `SELECT lease_id::text,id::text,kind FROM object_fixture.uses WHERE object_id=$1 AND owner_id=$2 AND active AND lease_id IS NOT NULL`, object.String(), owner.Details().ID).Scan(&lease, &use, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return oc.OwnerAuthorization{}, fault(foundation.Forbidden)
	}
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	leaseID, err := foundation.ParseID[oc.Lease](lease)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	leaseOwner, err := oc.NewLeaseOwner(oc.LeaseOwnerKind(kind), use)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	d := grant.Details()
	d.ReadObjectID = object
	d.ProtectedLease = &oc.ObjectLease{ID: leaseID, ObjectID: object, Owner: leaseOwner}
	return oc.NewOwnerAuthorization(d)
}
func (a *authority) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, object oc.ObjectID, owner oc.LeaseOwner, action oc.LeaseAction) error {
	e, err := a.exec(tx)
	if err != nil {
		return err
	}
	if err = a.current(ctx, e, actor); err != nil {
		return err
	}
	var active, terminal bool
	err = e.QueryRow(ctx, `SELECT active,terminal FROM object_fixture.uses WHERE id=$1 AND object_id=$2 AND kind=$3`, owner.Details().ID, object.String(), string(owner.Details().Kind)).Scan(&active, &terminal)
	if errors.Is(err, pgx.ErrNoRows) {
		return fault(foundation.Forbidden)
	}
	if err != nil {
		return err
	}
	if action == oc.AcquireLease && !active || action == oc.ReleaseLease && !terminal {
		return fault(foundation.Forbidden)
	}
	return nil
}
func (a *authority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	e, err := a.exec(tx)
	if err != nil {
		return err
	}
	var ok bool
	err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM object_fixture.cleanup WHERE operation_id=$1 AND object_id=$2 AND owner_id=$3)`, cause.Details().OperationID.String(), object.String(), cause.Details().Owner.Details().ID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fault(foundation.Forbidden)
	}
	return nil
}
func (a *authority) CheckProjectCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause oc.ProjectCleanupCause) error {
	e, err := a.exec(tx)
	if err != nil {
		return err
	}
	if err = a.current(ctx, e, actor); err != nil {
		return err
	}
	var ok bool
	d := cause.Details()
	err = e.QueryRow(ctx, `SELECT state='deleting' AND owner_id=$2 AND operation_id=$3 AND version=$4 FROM object_fixture.projects WHERE id=$1`, d.ProjectID.String(), actor.Details().UserID, d.OperationID.String(), int64(d.Version)).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fault(foundation.Forbidden)
	}
	return nil
}

type auditAuthority struct{ *authority }

func (a auditAuthority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, project identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	e, err := a.exec(tx)
	if err != nil {
		return identity.AccessGrant{}, err
	}
	if err = a.current(ctx, e, actor); err != nil {
		return identity.AccessGrant{}, err
	}
	var owner string
	err = e.QueryRow(ctx, `SELECT owner_id::text FROM object_fixture.projects WHERE id=$1`, project.String()).Scan(&owner)
	if err != nil {
		return identity.AccessGrant{}, err
	}
	if owner != actor.Details().UserID {
		return identity.AccessGrant{}, fault(foundation.Forbidden)
	}
	scope, _ := identity.InProject(project)
	at, _ := foundation.NewInstant(time.Now())
	return identity.NewAccessGrant(actor, scope, intent, at, 1)
}
func (a auditAuthority) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	e, err := a.exec(tx)
	if err != nil {
		return err
	}
	var state string
	err = e.QueryRow(ctx, `SELECT state FROM object_fixture.projects WHERE id=$1`, entry.Fields().Scope.Details().ProjectID).Scan(&state)
	if err != nil {
		return err
	}
	if state != "active" && state != "deleting" {
		return fault(foundation.ProjectNotActive)
	}
	return nil
}
func (a auditAuthority) CheckServiceLookup(ctx context.Context, actor identity.Actor, scope identity.Scope, key ac.AppendKey) error {
	return fault(foundation.Forbidden)
}
func (a auditAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause ac.LifecycleCause, project identity.ProjectID) error {
	return fault(foundation.Forbidden)
}
