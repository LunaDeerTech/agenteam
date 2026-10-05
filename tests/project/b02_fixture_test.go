//go:build integration

package project_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objectc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/minio/minio-go/v7"
)

func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func id[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	v, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func meta(t *testing.T, key string, version *foundation.Version) foundation.CommandMeta {
	return foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: foundation.IdempotencyKey(key), ExpectedVersion: version}
}
func requireCode(t *testing.T, e error, want foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(e, &f) || f.Code != want {
		t.Fatalf("error=%v want=%s", e, want)
	}
}
func cause(t *testing.T) foundation.TransactionCause {
	v, e := foundation.NewRecoveryCause("project.fixture", id[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return v
}

type b02Store interface {
	project.Store
	audit.Store
}
type fixtureProcess struct{ id oc.ProcessID }

func (p fixtureProcess) CurrentProcess() oc.ProcessID { return p.id }
func (p fixtureProcess) ConfirmStopped(context.Context, oc.ProcessID) error {
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
}

// This adapter delegates exact death proof to the accepted Object guard. It
// cannot infer death from age, cancellation, a missing row or a test flag.
type projectGuardProcesses struct {
	guard   *object.ProcessGuard
	process objectc.ProcessID
}

func (p projectGuardProcesses) CurrentProcess() oc.ProcessID {
	id, _ := foundation.ParseID[oc.Process](p.process.String())
	return id
}
func (p projectGuardProcesses) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	id, err := foundation.ParseID[objectc.Process](process.String())
	if err != nil {
		return err
	}
	return p.guard.ConfirmStopped(ctx, id)
}

func projectStorage(t *testing.T, bucket string) (object.StorageConfig, string) {
	t.Helper()
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	prefix := "d08-" + remote.Nonce[:12] + "-"
	if bucket == "" {
		suffix, err := pgfixture.RandomHex(8)
		if err != nil {
			t.Fatal(err)
		}
		bucket = prefix + suffix
		client, transport, err := remote.Client()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(transport.CloseIdleConnections)
		if err = client.MakeBucket(ctxFor(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
			t.Fatal(err)
		}
	} else if !strings.HasPrefix(bucket, prefix) {
		t.Fatal("child bucket does not belong to the current owned fixture")
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	storage, err := object.LoadStorageConfig(func(name string) (string, bool) {
		value, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return storage, bucket
}

func (f *b02Fixture) processRuntime(t *testing.T, storage object.StorageConfig, path string, process objectc.ProcessID) (*object.Runtime, *object.Service, *object.ProcessGuard) {
	t.Helper()
	backend, err := object.NewBackend(storage)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(path, process)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		t.Fatal(err)
	}
	service, err := object.New(f.store, backend, spool, f.aud, object.Authorizations{Processes: guard})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, storage)
	if err != nil {
		t.Fatal(err)
	}
	transfers, err := object.NewTransferService(service, nil, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := object.NewRuntime(service, guard, transfers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runtime.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtime.Drain(ctx); err != nil {
			_ = runtime.Force(ctx)
			t.Error(err)
		}
	})
	return runtime, service, guard
}

type b02Fixture struct {
	db        *pgfixture.Database
	raw       *postgres.Store
	store     b02Store
	accounts  *account.Authority
	authority *project.Authority
	service   *project.Service
	skills    *skillFixture
	events    *outbox.Service
	aud       *audit.Service
	keys      cursor.Keyring
	typed     c.ProjectEvents
	process   oc.ProcessAuthority
}

func keys(t *testing.T) (account.Keyring, cursor.Keyring) {
	t.Helper()
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	cursor, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	secret, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), cursor)
	if e != nil {
		t.Fatal(e)
	}
	download, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), cursor, secret)
	if e != nil {
		t.Fatal(e)
	}
	account, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), cursor, secret, download)
	if e != nil {
		t.Fatal(e)
	}
	return account, cursor
}
func openStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	s, e := postgres.Open(ctxFor(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e = s.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	return s
}
func newDatabase(t *testing.T) *pgfixture.Database {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	m, e := postgres.NewMigrator(db.Config(t, nil))
	if e != nil {
		t.Fatal(e)
	}
	if result := m.Migrate(ctxFor(t)); !result.Migrated {
		t.Fatal("migrate", result.Fault)
	}
	// No runtime/bootstrap schema installation and no draft SQL loading. This
	// fixture must fail until the root-authorized continuous migration exists.
	conn := db.Connect(t)
	var exists bool
	if e = conn.QueryRow(ctxFor(t), `SELECT to_regclass('agenteam_project.projects') IS NOT NULL`).Scan(&exists); e != nil || !exists {
		t.Fatal("accepted Project migration required", e)
	}
	return db
}
func newFixture(t *testing.T) *b02Fixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	return assemble(t, db, raw, raw)
}
func assemble(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store b02Store) *b02Fixture {
	return assembleFixture(t, db, raw, store, true)
}
func assembleFixture(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store b02Store, createSkillSchema bool) *b02Fixture {
	t.Helper()
	ak, ck := keys(t)
	accounts, e := account.NewAuthority(store, ak)
	if e != nil {
		t.Fatal(e)
	}
	if e = accounts.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	authority, e := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if e != nil {
		t.Fatal(e)
	}
	aud, e := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: authority})
	if e != nil {
		t.Fatal(e)
	}
	catalog := event.NewCatalog()
	typed, e := c.RegisterProjectEvents(catalog)
	if e != nil {
		t.Fatal(e)
	}
	process := fixtureProcess{id[oc.Process](t)}
	events, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.ProjectProducer: authority}, Projects: authority, Sessions: accounts, System: accounts, Processes: process, Audit: aud, Cursors: ck})
	if e != nil {
		t.Fatal(e)
	}
	f := &b02Fixture{db: db, raw: raw, store: store, accounts: accounts, authority: authority, aud: aud, keys: ck, typed: typed, events: events, process: process}
	if createSkillSchema {
		if _, e = raw.Exec(ctxFor(t), `CREATE SCHEMA project_fixture; CREATE TABLE project_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); e != nil {
			t.Fatal(e)
		}
	}
	f.skills = &skillFixture{store: store, authority: authority, issuer: c.NewInitializationPlanIssuer()}
	f.service = f.newService(t, f.skills, aud, events)
	return f
}
func (f *b02Fixture) newService(t *testing.T, initializer c.ProjectSkillInitializer, aud ac.Appender, events oc.Appender) *project.Service {
	t.Helper()
	s, e := project.New(f.store, project.Dependencies{Authority: f.authority, Activity: f.accounts, Audit: aud, Events: events, ProjectEvents: f.typed, Initializer: initializer, Processes: f.process, Cursors: f.keys}, project.DefaultConfig())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := s.Drain(ctx); e != nil {
			t.Error(e)
		}
	})
	return s
}
func (f *b02Fixture) human(t *testing.T, username, role string) identity.Actor {
	t.Helper()
	user, session := id[identity.User](t), id[identity.Session](t)
	// Test-owned Account facts are consumed through the real Account authority;
	// no fake Session/Owner provider is installed in the production service.
	if _, e := f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,$2,$3,'fixture',$4,'fixture-not-a-login-hash',1,1,1,false,'system')`, user.String(), username+"@example.test", username, role); e != nil {
		t.Fatal(e)
	}
	if _, e := f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) VALUES($1,$2,$3,'a',clock_timestamp()-interval '2 minutes',clock_timestamp()-interval '2 minutes',3600,clock_timestamp()+interval '1 hour')`, session.String(), user.String(), bytes.Repeat([]byte(username), 32)[:32]); e != nil {
		t.Fatal(e)
	}
	actor, e := identity.NewHuman(user, session)
	if e != nil {
		t.Fatal(e)
	}
	return actor
}
func (f *b02Fixture) create(t *testing.T, actor identity.Actor, name string) (c.ProjectRef, foundation.CommandMeta, c.CreateProjectRequest) {
	t.Helper()
	request := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: name, Description: "private body"}
	m := meta(t, id[struct{}](t).String(), nil)
	result, e := f.service.CreateProject(ctxFor(t), actor, m, request)
	if e != nil || result.State != c.CreationReady {
		t.Fatal("create ready", result.State, e)
	}
	return *result.Project, m, request
}

// This fixture persists protected/published Skill facts in a test-only schema.
// It is not a D10 implementation or a default production initializer.
type skillFixture struct {
	store     project.Store
	authority *project.Authority
	issuer    c.InitializationPlanIssuer
	mu        sync.Mutex
	mode      string
	calls     int
	extraLock foundation.LockKey
}

func (s *skillFixture) setMode(mode string) { s.mu.Lock(); s.mode = mode; s.mu.Unlock() }
func (s *skillFixture) InspectProjectSkills(ctx context.Context, actor identity.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
	if actor.Details().ServiceName != identity.ProjectInitialization || actor.Details().CauseRef != r.CreationID.String() || actor.Details().ProjectID != r.ProjectID.String() {
		return c.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	var project, key, skill string
	var revision int64
	e := s.store.QueryRow(ctx, `SELECT project_id::text,init_key,skill_id::text,revision FROM project_fixture.skills WHERE creation_id=$1`, r.CreationID.String()).Scan(&project, &key, &skill, &revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return c.InitializationResult{State: c.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: c.ReasonWorkPending}, nil
	}
	if e != nil {
		return c.InitializationResult{}, e
	}
	if project != r.ProjectID.String() || key != string(r.InitializationKey) {
		return c.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	skillID, e := foundation.ParseID[c.Skill](skill)
	if e != nil {
		return c.InitializationResult{}, e
	}
	rev := foundation.Revision(revision)
	return c.InitializationResult{State: c.InitializationCompleted, CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: &skillID, Revision: &rev}, nil
}
func (s *skillFixture) InitializeProjectSkills(ctx context.Context, actor identity.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
	s.mu.Lock()
	mode := s.mode
	s.calls++
	s.mu.Unlock()
	if mode == "failed" {
		return c.InitializationResult{State: c.InitializationFailed, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: c.ReasonOperationFailed}, nil
	}
	if mode == "pending" {
		return c.InitializationResult{State: c.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: c.ReasonWorkPending}, nil
	}
	projectKey, _ := foundation.ProjectLock(r.ProjectID.String())
	command, _ := foundation.NewRecoveryCause("project.fixture-skill", r.CreationID.String(), "")
	result := s.store.WithinTx(ctx, command, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: projectKey, Mode: foundation.Exclusive}}); e != nil {
			return e
		}
		if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
			return e
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return e
		}
		skill, e := foundation.NewID[c.Skill]()
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO project_fixture.skills(creation_id,project_id,init_key,skill_id,revision,protected,published) VALUES($1,$2,$3,$4,1,true,true) ON CONFLICT(creation_id) DO NOTHING`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), skill.String())
		return e
	})
	if result.State() != foundation.Committed {
		return c.InitializationResult{}, foundation.NewFault(foundation.DependencyUnavailable, result.State())
	}
	if mode == "unknown" {
		return c.InitializationResult{}, foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
	}
	return s.InspectProjectSkills(ctx, actor, r)
}
func (s *skillFixture) DiscoverConfirmation(ctx context.Context, actor identity.Actor, r c.InitializationRequest) (c.InitializationConfirmationPlan, error) {
	result, e := s.InspectProjectSkills(ctx, actor, r)
	if e != nil {
		return c.InitializationConfirmationPlan{}, e
	}
	if result.State != c.InitializationCompleted {
		return c.InitializationConfirmationPlan{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	key, _ := foundation.ProjectLock(r.ProjectID.String())
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}
	s.mu.Lock()
	extra := s.extraLock
	s.mu.Unlock()
	if extra.Validate() == nil {
		locks = append(locks, foundation.LockRequest{Key: extra, Mode: foundation.Shared})
	}
	return s.issuer.Plan(actor, r, c.InitializationReceipt{CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}, locks)
}
func (s *skillFixture) ConfirmInitializedInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, r c.InitializationRequest, plan c.InitializationConfirmationPlan) (c.InitializationReceipt, error) {
	if !s.issuer.Matches(plan, actor, r) {
		return c.InitializationReceipt{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	locks := plan.RequiredLocks()
	s.mu.Lock()
	extra := s.extraLock
	s.mu.Unlock()
	if extra.Validate() == nil {
		locks = append(locks, foundation.LockRequest{Key: extra, Mode: foundation.Shared})
	}
	if e := s.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return c.InitializationReceipt{}, e
	}
	if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
		return c.InitializationReceipt{}, e
	}
	x, e := s.store.InTx(tx)
	if e != nil {
		return c.InitializationReceipt{}, e
	}
	receipt := plan.ProposedReceipt()
	var valid bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project_fixture.skills WHERE creation_id=$1 AND project_id=$2 AND init_key=$3 AND skill_id=$4 AND revision=$5 AND protected AND published)`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), receipt.AddSkillsID.String(), int64(receipt.Revision)).Scan(&valid)
	if e != nil {
		return c.InitializationReceipt{}, e
	}
	if !valid {
		return c.InitializationReceipt{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	return receipt, nil
}

const projectMigrationName = "00013_project_owner.sql"

func projectMigrationFiles(t *testing.T, through string) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name[:5] > through {
			continue
		}
		raw, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	return files
}
func projectMigrate(t *testing.T, db *pgfixture.Database, sources ...postgres.Source) {
	t.Helper()
	m, err := postgres.NewMigrator(db.Config(t, nil), sources...)
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(ctxFor(t)); !result.Migrated {
		t.Fatal("project migration", result.Fault)
	}
}
func projectPrefix(t *testing.T, through string) postgres.Source {
	t.Helper()
	source, err := postgres.NewSource(projectMigrationFiles(t, through), nil)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
func seedPreProjectFacts(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	user, session, record := id[identity.User](t), id[identity.Session](t), id[struct{}](t)
	if _, err := conn.Exec(ctxFor(t), `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,'legacy@example.test','legacy','before project','user','fixture-not-a-login-hash',1,1,1,false,'system')`, user.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctxFor(t), `INSERT INTO agenteam_audit.audit_records(id,scope,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal) VALUES($1,'system','human',$2,$3,'secret.create','success','secret',$1,'{"name":"historical"}','sha256:'||repeat('0',64),'secret','sha256:'||repeat('0',64),0)`, record.String(), user.String(), session.String()); err != nil {
		t.Fatal(err)
	}
}
func priorProjectSnapshot(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	var snapshot string
	if err := conn.QueryRow(ctxFor(t), `SELECT
 (SELECT coalesce(string_agg(row_to_json(u)::text,'' ORDER BY id),'') FROM agenteam_account.users u)||
 (SELECT coalesce(string_agg(row_to_json(a)::text,'' ORDER BY id),'') FROM agenteam_audit.audit_records a)||
 (SELECT string_agg(conname||pg_get_constraintdef(oid),'' ORDER BY conname) FROM pg_constraint WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname IN ('audit_records_account_contract','audit_records_content_contract','audit_records_outbox_contract'))||
 pg_get_indexdef('agenteam_account.account_avatar_changes_fair'::regclass)`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func TestProjectB02MigrationFreshAndPopulatedUpgrade(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		name := "fresh"
		if upgrade {
			name = "populated-00012"
		}
		t.Run(name, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			if upgrade {
				projectMigrate(t, db, projectPrefix(t, "00012"))
			} else {
				projectMigrate(t, db)
			}
			conn := db.Connect(t)
			seedPreProjectFacts(t, conn)
			before := priorProjectSnapshot(t, conn)
			projectMigrate(t, db)
			if after := priorProjectSnapshot(t, conn); after != before {
				t.Fatal("Project migration changed existing Account/Audit facts or old closed constraints")
			}
			var tables int
			if err := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM information_schema.tables WHERE table_schema='agenteam_project' AND table_type='BASE TABLE'`).Scan(&tables); err != nil || tables != 7 {
				t.Fatal("Project table manifest", tables, err)
			}
			var columns []string
			if err := conn.QueryRow(ctxFor(t), `SELECT array_agg(column_name::text ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema='agenteam_project' AND table_name='deletion_receipts'`).Scan(&columns); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(columns, []string{"operation_id", "deleted_project_id", "original_owner_user_id", "command_key_hash", "request_digest", "completed_at", "status"}) {
				t.Fatal("deleted Project retained extra contents", columns)
			}
			var foreignFK int
			if err := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_constraint c JOIN pg_class a ON a.oid=c.conrelid JOIN pg_namespace an ON an.oid=a.relnamespace JOIN pg_class b ON b.oid=c.confrelid JOIN pg_namespace bn ON bn.oid=b.relnamespace WHERE c.contype='f' AND an.nspname='agenteam_project' AND bn.nspname<>'agenteam_project'`).Scan(&foreignFK); err != nil || foreignFK != 0 {
				t.Fatal("cross-domain database ownership", foreignFK, err)
			}
		})
	}
}
func TestProjectB02MigrationFailureRollsBackSchemaAndAudit(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	files := projectMigrationFiles(t, "00012")
	source, err := postgres.NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	projectMigrate(t, db, source)
	conn := db.Connect(t)
	seedPreProjectFacts(t, conn)
	before := priorProjectSnapshot(t, conn)
	query := `SELECT string_agg(conname||pg_get_constraintdef(oid),'' ORDER BY conname) FROM pg_constraint WHERE conrelid='agenteam_audit.audit_records'::regclass`
	var constraints string
	if err = conn.QueryRow(ctxFor(t), query).Scan(&constraints); err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(migrations.SQL, projectMigrationName)
	if err != nil {
		t.Fatal(err)
	}
	files[projectMigrationName] = &fstest.MapFile{Data: append(append([]byte{}, raw...), []byte("\nSELECT project_fixture_migration_failure();\n")...)}
	bad, err := postgres.NewSource(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := postgres.NewMigrator(db.Config(t, nil), bad)
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(ctxFor(t)); result.Migrated || result.Fault == nil {
		t.Fatal("intentional SQL failure succeeded")
	}
	var absent bool
	if err = conn.QueryRow(ctxFor(t), `SELECT to_regnamespace('agenteam_project') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatal("Project DDL escaped failed transaction", err)
	}
	var after string
	if err = conn.QueryRow(ctxFor(t), query).Scan(&after); err != nil || after != constraints || priorProjectSnapshot(t, conn) != before {
		t.Fatal("failed migration changed existing Audit/Account", err)
	}
	var pending, applied int
	if err = conn.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=13 AND state='pending'),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=13 AND is_applied)`).Scan(&pending, &applied); err != nil || pending != 1 || applied != 0 {
		t.Fatal("migration journal escaped rollback", pending, applied, err)
	}
	// D03 keeps the failed checksum. Make this exact test-only source runnable;
	// replacing it with different SQL would itself be history divergence.
	if _, err = conn.Exec(ctxFor(t), `CREATE FUNCTION public.project_fixture_migration_failure() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); err != nil {
		t.Fatal(err)
	}
	projectMigrate(t, db, bad)
	if priorProjectSnapshot(t, conn) != before {
		t.Fatal("same-source recovery changed old facts")
	}
}
func insertDescriptionReservation(t *testing.T, conn *pgx.Conn, description string) error {
	t.Helper()
	project, creation, owner, event := id[identity.Project](t), id[c.Creation](t), id[identity.User](t), id[struct{}](t)
	tx, err := conn.Begin(ctxFor(t))
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctxFor(t), `INSERT INTO agenteam_project.projects(id,owner_user_id,name,normalized_name,description,lifecycle,version,created_at,updated_at,creation_id) VALUES($1,$2,'description','description',$3,'active',1,clock_timestamp(),clock_timestamp(),$4)`, project.String(), owner.String(), description, creation.String()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctxFor(t), `INSERT INTO agenteam_project.creations(id,project_id,owner_user_id,command_key,semantic_digest,request_name,request_description,state,initialization_key,version,created_at,updated_at,event_id) VALUES($1,$2,$3,'description','sha256:'||repeat('0',64),'description',$4,'accepted','description-init',1,clock_timestamp(),clock_timestamp(),$5)`, creation.String(), project.String(), owner.String(), description, event.String()); err != nil {
		return err
	}
	return tx.Commit(ctxFor(t))
}
func TestProjectB02DescriptionSchemaMatchesControlAndByteRules(t *testing.T) {
	db := newDatabase(t)
	conn := db.Connect(t)
	for _, valid := range []string{"", "  plain\tline\nnext  ", strings.Repeat("x", 8192), strings.Repeat("界", 2730)} {
		if err := insertDescriptionReservation(t, conn, valid); err != nil {
			t.Fatal("legal description rejected", err)
		}
	}
	invalid := []string{strings.Repeat("x", 8193), strings.Repeat("界", 2731), "\x7f"}
	for r := byte(0); r < 32; r++ {
		if r != '\n' && r != '\t' {
			invalid = append(invalid, string([]byte{r}))
		}
	}
	for _, bad := range invalid {
		err := insertDescriptionReservation(t, conn, bad)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" && pg.Code != "22021" {
			t.Fatalf("invalid description accepted or failed outside its constraint: %T %v", err, err)
		}
	}
}

func TestProjectB02AuditSchemaAcceptsOnlyTypedProjectFacts(t *testing.T) {
	db := newDatabase(t)
	conn := db.Connect(t)
	user, session, project := id[identity.User](t), id[identity.Session](t), id[identity.Project](t)
	human, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []ac.Action{ac.ProjectCreateAccepted, ac.ProjectCreateCompleted, ac.ProjectUpdate, ac.ProjectArchiveAccepted, ac.ProjectArchiveCompleted, ac.ProjectRestore, ac.ProjectDeleteAccepted, ac.ProjectLifecycleRetry} {
		t.Run(string(action), func(t *testing.T) {
			resourceID, kind := project.String(), ac.ProjectResource
			version := foundation.Version(1)
			fields := ac.ProjectMetadataFields{ProjectID: project.String(), InitiatorID: user.String(), ProjectVersion: 2}
			actor := human
			cause := id[struct{}](t).String()
			switch action {
			case ac.ProjectCreateAccepted, ac.ProjectCreateCompleted:
				resourceID, kind = id[c.Creation](t).String(), ac.ProjectCreationResource
				fields.ProjectVersion, fields.CreationID, fields.CreationVersion = 1, resourceID, &version
				if action == ac.ProjectCreateCompleted {
					reg, _ := identity.RegisterService(identity.ProjectInitialization)
					scope, _ := identity.InProject(project)
					actor, err = reg.Actor(resourceID, scope)
					cause = resourceID
				}
			case ac.ProjectUpdate:
				fields.ChangedFields = []ac.ProjectChangedField{ac.ProjectNameChanged}
			case ac.ProjectRestore:
				fields.From, fields.To, fields.Action = "archived", "active", "restore"
			default:
				resourceID, kind = id[c.Operation](t).String(), ac.ProjectOperationResource
				fields.OperationID, fields.OperationVersion = resourceID, &version
				switch action {
				case ac.ProjectArchiveAccepted:
					fields.From, fields.To, fields.Action = "active", "archiving", "archive"
				case ac.ProjectArchiveCompleted:
					fields.From, fields.To, fields.Action = "archiving", "archived", "archive"
					reg, _ := identity.RegisterService(identity.ProjectLifecycle)
					scope, _ := identity.InProject(project)
					actor, err = reg.Actor(resourceID, scope)
					cause = resourceID
				case ac.ProjectDeleteAccepted:
					fields.From, fields.To, fields.Action = "active", "deleting", "delete"
				case ac.ProjectLifecycleRetry:
					fields.Action = "delete"
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := ac.ProjectMetadata(action, fields)
			if err != nil {
				t.Fatal(err)
			}
			scope, _ := identity.InProject(project)
			resource, err := ac.NewResource(kind, resourceID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata}); err != nil {
				t.Fatal("invalid typed positive fixture", err)
			}
			raw, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			d := actor.Details()
			null := func(v string) any {
				if v == "" {
					return nil
				}
				return v
			}
			record := id[struct{}](t)
			_, err = conn.Exec(ctxFor(t), `INSERT INTO agenteam_audit.audit_records(id,scope,project_id,actor_kind,user_id,session_id,actor_project_id,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal) VALUES($1,'project',$2,$3,$4,$5,$6,$7,$8,$9,'success',$10,$11,$12::jsonb,'sha256:'||repeat('0',64),'project',$13,0)`, record.String(), project.String(), string(d.Kind), null(d.UserID), null(d.SessionID), null(d.ProjectID), null(string(d.ServiceName)), null(d.CauseRef), string(action), string(kind), resourceID, raw, cause)
			if err != nil {
				t.Fatal("typed Project Audit rejected by schema", err)
			}
			for _, assignment := range []string{`metadata=metadata-'project_id'`, `metadata=metadata||'{"body":"private-canary"}'::jsonb`, `metadata=metadata||'{"project_version":1}'::jsonb`, `project_id='` + id[identity.Project](t).String() + `'`, `resource_id='` + id[struct{}](t).String() + `'`, `producer='secret'`, `ordinal=1`, `outcome='denied'`} {
				_, err = conn.Exec(ctxFor(t), `UPDATE agenteam_audit.audit_records SET `+assignment+` WHERE id=$1`, record.String())
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != "23514" {
					t.Fatal("invalid Project Audit bypassed closed schema", assignment, err)
				}
			}
			if d.Kind == identity.Service {
				_, err = conn.Exec(ctxFor(t), `UPDATE agenteam_audit.audit_records SET service_name='secret' WHERE id=$1`, record.String())
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != "23514" {
					t.Fatal("generic service borrowed exact Project cause", err)
				}
			}
		})
	}
}
