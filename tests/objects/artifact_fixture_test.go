//go:build integration

package objects_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	art "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Only this fixture implements the future identity/Project/Execution ports.
// Artifact/Object/Audit/SourceReads and every database/storage operation below
// are the actual production services, with mutable current SQL permissions.
type artifactAuthority struct{ store *postgres.Store }

func (a *artifactAuthority) executor(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx.Valid() {
		return a.store.InTx(tx)
	}
	return a.store, nil
}
func (a *artifactAuthority) Discover(ctx context.Context, s art.AccessSubject) (oc.AccessDependencies, error) {
	return a.dependencies(ctx, a.store, s)
}
func (a *artifactAuthority) DiscoverInTx(ctx context.Context, tx foundation.Tx, s art.AccessSubject) (oc.AccessDependencies, error) {
	e, err := a.executor(tx)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.dependencies(ctx, e, s)
}
func (a *artifactAuthority) dependencies(ctx context.Context, e postgres.SQLExecutor, s art.AccessSubject) (oc.AccessDependencies, error) {
	if s.Validate() != nil {
		return oc.AccessDependencies{}, fault(foundation.InvalidArgument)
	}
	var owner string
	if err := e.QueryRow(ctx, `SELECT owner_id::text FROM object_fixture.projects WHERE id=$1`, s.ProjectID.String()).Scan(&owner); err != nil {
		return oc.AccessDependencies{}, err
	}
	var locks []foundation.LockRequest
	mapping := []string{"project:" + s.ProjectID.String() + ":" + owner}
	add := func(key foundation.LockKey, err error) {
		if err == nil {
			locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
		}
	}
	add(foundation.ProjectLock(s.ProjectID.String()))
	add(foundation.UserLock(owner))
	d := s.Actor.Details()
	if d.UserID != "" {
		add(foundation.UserLock(d.UserID))
	}
	if d.AgentID != "" {
		add(foundation.AgentLock(d.AgentID))
	}
	if s.ExecutionID != "" {
		var agent, project string
		if err := e.QueryRow(ctx, `SELECT agent_id::text,project_id::text FROM object_fixture.executions WHERE id=$1`, s.ExecutionID).Scan(&agent, &project); err != nil {
			return oc.AccessDependencies{}, err
		}
		mapping = append(mapping, "execution:"+s.ExecutionID+":"+agent+":"+project)
		add(foundation.AggregateLock(foundation.ExecutionAggregate, s.ExecutionID))
		add(foundation.AgentLock(agent))
	}
	if s.OperationID != "" {
		var execution string
		if err := e.QueryRow(ctx, `SELECT execution_id::text FROM object_fixture.operations WHERE id=$1`, s.OperationID).Scan(&execution); err != nil {
			return oc.AccessDependencies{}, err
		}
		mapping = append(mapping, "operation:"+s.OperationID+":"+execution)
		add(foundation.AggregateLock(foundation.OperationAggregate, s.OperationID))
	}
	sort.Strings(mapping)
	raw, _ := json.Marshal(mapping)
	return oc.NewAccessDependencies(digest(raw), locks)
}
func (a *artifactAuthority) AuthorizeInTx(ctx context.Context, tx foundation.Tx, s art.AccessSubject, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if s.Validate() != nil || s.Maintenance {
		return identity.AccessGrant{}, fault(foundation.Forbidden)
	}
	e, err := a.executor(tx)
	if err != nil {
		return identity.AccessGrant{}, err
	}
	d := s.Actor.Details()
	if d.Kind != identity.Service {
		if err = (&authority{store: a.store}).current(ctx, e, s.Actor); err != nil {
			return identity.AccessGrant{}, err
		}
	}
	var owner, state, operation string
	var version int64
	err = e.QueryRow(ctx, `SELECT owner_id::text,state,coalesce(operation_id::text,''),version FROM object_fixture.projects WHERE id=$1`, s.ProjectID.String()).Scan(&owner, &state, &operation, &version)
	if err != nil {
		return identity.AccessGrant{}, err
	}
	if d.Kind == identity.Human && d.UserID != owner || d.Kind == identity.AgentRun && d.ProjectID != s.ProjectID.String() {
		return identity.AccessGrant{}, fault(foundation.Forbidden)
	}
	if d.Kind == identity.Service {
		personalConverge := d.ServiceName == identity.ObjectMaintenance && intent == identity.Converge && (state == "active" || state == "archived")
		lifecycle := (d.ServiceName == identity.ObjectMaintenance || d.ServiceName == identity.ProjectLifecycle) && d.CauseRef == operation && state == "deleting"
		if d.ProjectID != s.ProjectID.String() || !personalConverge && !lifecycle {
			return identity.AccessGrant{}, fault(foundation.Forbidden)
		}
	}
	if state != "active" && !(state == "archived" && (intent == identity.Read || intent == identity.Converge)) && !(state == "deleting" && (intent == identity.Converge || intent == identity.Lifecycle)) {
		return identity.AccessGrant{}, fault(foundation.ProjectNotActive)
	}
	if s.ExecutionID != "" {
		var ok bool
		err = e.QueryRow(ctx, `SELECT active AND project_id=$2 AND ($3='' OR agent_id::text=$3) FROM object_fixture.executions WHERE id=$1`, s.ExecutionID, s.ProjectID.String(), d.AgentID).Scan(&ok)
		if err != nil || !ok {
			return identity.AccessGrant{}, fault(foundation.Forbidden)
		}
	}
	if s.OperationID != "" {
		var ok bool
		err = e.QueryRow(ctx, `SELECT active AND execution_id=$2 AND tool_id::text=$3 AND tool_call_id::text=$4 FROM object_fixture.operations WHERE id=$1`, s.OperationID, s.ExecutionID, s.ToolID, s.ToolCallID).Scan(&ok)
		if err != nil || !ok {
			return identity.AccessGrant{}, fault(foundation.Forbidden)
		}
	}
	if s.ToolID != "" && s.OperationID == "" {
		return identity.AccessGrant{}, fault(foundation.Forbidden)
	}
	scope, _ := identity.InProject(s.ProjectID)
	at, _ := foundation.NewInstant(time.Now())
	return identity.NewAccessGrant(s.Actor, scope, intent, at, foundation.Version(version))
}
func (a *artifactAuthority) CheckProjectCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause oc.ProjectCleanupCause) error {
	d := cause.Details()
	s := art.AccessSubject{Actor: actor, ProjectID: d.ProjectID}
	if _, err := a.AuthorizeInTx(ctx, tx, s, identity.Lifecycle); err != nil {
		return err
	}
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	var ok bool
	err = e.QueryRow(ctx, `SELECT state='deleting' AND operation_id=$2 AND version=$3 FROM object_fixture.projects WHERE id=$1`, d.ProjectID.String(), d.OperationID.String(), int64(d.Version)).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return fault(foundation.Forbidden)
	}
	return nil
}
func (a *artifactAuthority) RequireCurrentSession(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	return (&authority{store: a.store}).current(ctx, e, actor)
}
func (a *artifactAuthority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, project identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	s := art.AccessSubject{Actor: actor, ProjectID: project}
	if actor.Details().Kind == identity.AgentRun {
		s.ExecutionID = actor.Details().ExecutionID
	}
	return a.AuthorizeInTx(ctx, tx, s, intent)
}
func (a *artifactAuthority) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry au.Entry, key au.AppendKey) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	f := entry.Fields()
	pid, err := foundation.ParseID[identity.Project](f.Scope.Details().ProjectID)
	if err != nil {
		return err
	}
	var allowed bool
	var state string
	err = e.QueryRow(ctx, `SELECT audit_allowed,state FROM object_fixture.projects WHERE id=$1`, pid.String()).Scan(&allowed, &state)
	if err != nil {
		return err
	}
	if !allowed {
		return fault(foundation.Forbidden)
	}
	if f.Actor.Details().Kind == identity.Service {
		if f.Actor.Details().ServiceName != identity.ObjectService && f.Actor.Details().ServiceName != identity.ObjectMaintenance {
			return fault(foundation.Forbidden)
		}
		if state != "active" && state != "deleting" {
			return fault(foundation.ProjectNotActive)
		}
		return nil
	}
	intent := identity.Mutate
	if f.Action == au.ArtifactRead || f.Action == au.ArtifactList || f.Action == au.ArtifactDownload {
		intent = identity.Read
	}
	_, err = a.AuthorizeInTx(ctx, tx, art.AccessSubject{Actor: f.Actor, ProjectID: pid, ExecutionID: f.Associations.ExecutionID, OperationID: f.Associations.OperationID, ToolID: f.Associations.ToolID, ToolCallID: f.Associations.ToolCallID}, intent)
	if err != nil {
		return err
	}
	if f.Action == au.ArtifactList {
		if f.Resource.Details().ID != pid.String() {
			return fault(foundation.Forbidden)
		}
		return nil
	}
	var exact bool
	err = e.QueryRow(ctx, `SELECT project_id=$2 FROM agenteam_artifact.artifacts WHERE id=$1`, f.Resource.Details().ID, pid.String()).Scan(&exact)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !exact {
		return fault(foundation.Forbidden)
	}
	return err
}
func (a *artifactAuthority) CheckServiceLookup(context.Context, identity.Actor, identity.Scope, au.AppendKey) error {
	return fault(foundation.Forbidden)
}
func (a *artifactAuthority) CheckCleanupInTx(context.Context, foundation.Tx, identity.Actor, au.LifecycleCause, identity.ProjectID) error {
	return fault(foundation.Forbidden)
}

type countedSource struct {
	oc.SourceResolver
	resolves atomic.Int64
	reject   atomic.Bool
}

func (r *countedSource) Resolve(ctx context.Context, actor identity.Actor, ref oc.BusinessFileRef) (oc.ResolvedSource, error) {
	r.resolves.Add(1)
	if r.reject.Load() {
		return oc.ResolvedSource{}, fault(foundation.Forbidden)
	}
	return r.SourceResolver.Resolve(ctx, actor, ref)
}

type artifactFixture struct {
	*fixture
	artifact *artifact.Service
	objects  *object.Service
	sources  *artifact.Sources
	resolver *countedSource
	auth     *artifactAuthority
	auditing *audit.Service
	keys     cursor.Keyring
	proxy    *storageProxy
}

func newArtifactFixture(t *testing.T) *artifactFixture {
	return newArtifactFixtureWithProxyBudget(t, 0)
}

// Only the 101-object batch test opts into a separate bounded proxy lifetime;
// ordinary fixtures and production operation budgets retain their defaults.
func newArtifactFixtureWithProxyBudget(t *testing.T, proxyBudget time.Duration) *artifactFixture {
	t.Helper()
	f := newFixture(t, false)
	f.sql(t, `ALTER TABLE object_fixture.projects ADD audit_allowed boolean NOT NULL DEFAULT true;CREATE TABLE object_fixture.operations(id uuid PRIMARY KEY,execution_id uuid NOT NULL,tool_id uuid NOT NULL,tool_call_id uuid NOT NULL,active boolean NOT NULL DEFAULT true)`)
	proxy := newStorageProxy(t, f)
	if proxyBudget > 0 {
		// No request has been issued to this owned listener yet.
		proxy.cancel()
		proxy.ctx, proxy.cancel = context.WithTimeout(context.Background(), proxyBudget)
	}
	values := map[string]string{"ENDPOINT": proxy.server.URL, "BUCKET": f.bucket, "ACCESS_KEY": f.remote.AccessKey, "SECRET_KEY": f.remote.SecretKey, "TLS_MODE": "disable"}
	cfg, err := object.LoadStorageConfig(func(key string) (string, bool) { v, ok := values[key[len(object.EnvironmentPrefix):]]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	backend, err := object.NewBackend(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "artifact-spool"), id[oc.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	authority := &artifactAuthority{f.store}
	auditing, err := audit.New(f.store, keys, audit.Authorizations{Sessions: authority, Projects: authority})
	if err != nil {
		t.Fatal(err)
	}
	owners, err := artifact.NewOwnerProvider(f.store, authority)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := object.New(f.store, backend, spool, auditing, object.Authorizations{Planner: owners, Resources: owners, Read: owners, Gate: owners, Cleanup: owners})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		objects.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := objects.Drain(ctx); err != nil {
			_ = objects.Force(ctx)
			t.Error(err)
		}
	})
	if err = objects.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	sources, err := artifact.NewSources(owners, objects, auditing)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &countedSource{SourceResolver: sources}
	reads, err := object.NewSourceReads(objects, resolver)
	if err != nil {
		t.Fatal(err)
	}
	service, err := artifact.New(f.store, owners, objects, objects, objects, reads, resolver, auditing, keys)
	if err != nil {
		t.Fatal(err)
	}
	return &artifactFixture{f, service, objects, sources, resolver, authority, auditing, keys, proxy}
}
func (f *artifactFixture) invocation(t *testing.T) art.Invocation {
	t.Helper()
	v, err := art.NewInvocation(art.InvocationDetails{Actor: f.actor, ProjectID: f.project, AttemptID: id[art.CallAttempt](t)})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// bindOn uses a real second Store (usually the owned PostgreSQL TCP fault
// proxy). Domain providers remain the actual Artifact adapters; wrappers can
// observe a formal boundary and arm a protocol fault, never replace commit.
func (f *artifactFixture) bindOn(t *testing.T, store *postgres.Store, auditWrap func(au.Appender) au.Appender, sourceWrap func(oc.SourceReads) oc.SourceReads) *artifactFixture {
	t.Helper()
	authority := &artifactAuthority{store}
	auditing, err := audit.New(store, f.keys, audit.Authorizations{Sessions: authority, Projects: authority})
	if err != nil {
		t.Fatal(err)
	}
	owners, err := artifact.NewOwnerProvider(store, authority)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"ENDPOINT": f.proxy.server.URL, "BUCKET": f.bucket, "ACCESS_KEY": f.remote.AccessKey, "SECRET_KEY": f.remote.SecretKey, "TLS_MODE": "disable"}
	cfg, err := object.LoadStorageConfig(func(key string) (string, bool) { v, ok := values[key[len(object.EnvironmentPrefix):]]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	backend, err := object.NewBackend(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "rebound-spool"), id[oc.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	objects, err := object.New(store, backend, spool, auditing, object.Authorizations{Planner: owners, Resources: owners, Read: owners, Gate: owners, Cleanup: owners})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		objects.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := objects.Drain(ctx); err != nil {
			_ = objects.Force(ctx)
			t.Error(err)
		}
	})
	if err = objects.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	sources, err := artifact.NewSources(owners, objects, auditing)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &countedSource{SourceResolver: sources}
	original, err := object.NewSourceReads(objects, resolver)
	if err != nil {
		t.Fatal(err)
	}
	var reads oc.SourceReads = original
	if sourceWrap != nil {
		reads = sourceWrap(reads)
	}
	var appender au.Appender = auditing
	if auditWrap != nil {
		appender = auditWrap(appender)
	}
	svc, err := artifact.New(store, owners, objects, objects, objects, reads, resolver, appender, f.keys)
	if err != nil {
		t.Fatal(err)
	}
	return &artifactFixture{f.fixture, svc, objects, sources, resolver, authority, auditing, f.keys, f.proxy}
}
