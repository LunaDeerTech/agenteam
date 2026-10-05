//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

// Only the unbound Skills initializer and lifecycle participant implementations
// are isolated. Account, Project Owner/gate, Secret, Model, Audit and Outbox all
// consume the same real Store. No test dependency grants Project access.
type projectConfigurationFixture struct {
	*fixture
	projects       *project.Authority
	projectService *project.Service
	skills         *projectConfigurationSkills
	owner          id.Actor
	project        c.ProjectRef
	scope          id.Scope
	typed          model.ModelEvents
}

func newProjectConfigurationFixture(t *testing.T) *projectConfigurationFixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	v := assembleProjectConfiguration(t, db, raw, raw)
	v.admin = v.human(t, "admin")
	v.regular = v.human(t, "user")
	v.owner = v.regular
	v.project = v.createProject(t, v.owner)
	v.scope, _ = id.InProject(v.project.ID)
	return v
}
func assembleProjectConfiguration(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store sharedStore) *projectConfigurationFixture {
	t.Helper()
	ctx := testContext(t)
	ak, ck, sk := testKeys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	facts, err := secret.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.SecretProducer: facts}})
	if err != nil {
		t.Fatal(err)
	}
	delegate, err := project.NewSecretAuthority(projects)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts, Projects: projects})
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: projects, Models: authority})
	if err != nil {
		t.Fatal(err)
	}
	if err = aud.CheckStorage(ctx); err != nil {
		t.Fatal(err)
	}
	usage, err := model.NewSecretUsageRouter(authority, accounts)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.New(store, sk, aud, secret.Authorizations{Sessions: accounts, System: accounts, Projects: delegate, Usage: usage, AccountWrites: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = secrets.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secrets.StopMaintenance)
	catalog := ec.NewCatalog()
	typed, err := model.DefineEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	projectEvents, err := c.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := liveProcess{newID[oc.Process](t)}
	events, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{model.ModelProducer: authority, c.ProjectProducer: projects}, Projects: projects, Sessions: accounts, System: accounts, Audit: aud, Cursors: ck, Processes: process})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := outbox.NewRuntime(events, nil, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runtime.StopClaims()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := runtime.Drain(ctx); err != nil {
			_ = runtime.Force(ctx)
			t.Error(err)
		}
	})
	deps := model.Dependencies{Secret: secrets, Audit: aud, Events: events, ConfigurationEvents: typed, Cursors: ck}
	service, err := model.New(store, authority, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS model_project_fixture; CREATE TABLE IF NOT EXISTS model_project_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	skills := &projectConfigurationSkills{store: store, authority: projects, issuer: c.NewInitializationPlanIssuer()}
	projectService, err := project.New(store, project.Dependencies{Authority: projects, Activity: accounts, Audit: aud, Events: events, ProjectEvents: projectEvents, Initializer: skills, Processes: process, Cursors: ck, LifecycleRegistry: projectConfigurationRegistry(t)}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		projectService.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := projectService.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	return &projectConfigurationFixture{fixture: &fixture{db: db, raw: raw, store: store, accounts: accounts, authority: authority, service: service, secrets: secrets, aud: aud, events: events, runtime: runtime, deps: deps}, projects: projects, projectService: projectService, skills: skills, typed: typed}
}
func (v *projectConfigurationFixture) createProject(t *testing.T, actor id.Actor) c.ProjectRef {
	t.Helper()
	target := newID[id.Project](t)
	result, err := v.projectService.CreateProject(testContext(t), actor, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: f.IdempotencyKey(target.String())}, c.CreateProjectRequest{ProjectID: target, Name: "project-" + target.String()[24:], Description: "owned configuration fixture"})
	if err != nil || result.State != c.CreationReady || result.Project == nil {
		t.Fatal("real Project creation", result.State, err)
	}
	return *result.Project
}
func (v *projectConfigurationFixture) projectMeta(t *testing.T, key string) mc.CommandMeta {
	t.Helper()
	return mc.CommandMeta{Actor: v.owner, Scope: v.scope, Key: f.IdempotencyKey(key)}
}
func projectProviderInput() mc.ProviderInput {
	return mc.ProviderInput{Name: "Project provider", Protocol: mc.OpenAIChat, BaseURL: "https://project-provider.example/api", Enabled: true, Options: json.RawMessage(`{}`)}
}
func projectModelInput() mc.ModelInput {
	return mc.ModelInput{Name: "Project chat", ProviderModelID: "explicit-chat", Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{StructuredOutputModes: []string{"json_schema"}}}
}
func (v *projectConfigurationFixture) projectProvider(t *testing.T, ref *sc.CredentialRef) mc.ProviderView {
	t.Helper()
	input := projectProviderInput()
	input.CredentialRef = ref
	receipt, err := v.service.CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.projectMeta(t, newID[struct{}](t).String()), Input: input})
	if err != nil {
		t.Fatal("Project Provider", err)
	}
	target, _ := f.ParseID[mc.Provider](receipt.ResourceID)
	view, err := v.service.GetProjectProvider(testContext(t), v.owner, v.project.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func (v *projectConfigurationFixture) projectModel(t *testing.T, p mc.ProviderView) mc.ModelView {
	t.Helper()
	receipt, err := v.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.projectMeta(t, newID[struct{}](t).String()), ProviderID: p.ID, Input: projectModelInput()})
	if err != nil {
		t.Fatal("Project Model", err)
	}
	target, _ := f.ParseID[mc.Model](receipt.ResourceID)
	view, err := v.service.GetProjectModel(testContext(t), v.owner, v.project.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func (v *projectConfigurationFixture) projectCredential(t *testing.T, purpose sc.Purpose) sc.Metadata {
	t.Helper()
	material, err := sc.NewSecretMaterial([]byte("project-model-private-material"))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	command, err := f.NewCommandIdentity("secret", []string{v.project.ID.String(), v.owner.Details().UserID}, string(sc.Create), f.IdempotencyKey(newID[struct{}](t).String()))
	if err != nil {
		t.Fatal(err)
	}
	result, err := v.secrets.ExecuteWrite(testContext(t), sc.WriteRequest{Actor: v.owner, Scope: v.scope, Identity: command, Kind: sc.Create, Purpose: purpose, Value: material})
	if err != nil {
		t.Fatal("real Project Secret", err)
	}
	return result.Metadata
}
func (v *projectConfigurationFixture) sql(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := v.raw.Exec(testContext(t), query, args...); err != nil {
		t.Fatal(err)
	}
}

func (v *projectConfigurationFixture) revokeSession(t *testing.T, actor id.Actor) {
	t.Helper()
	ctx := testContext(t)
	tag, err := v.raw.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1 AND revoked_at IS NULL`, actor.Details().SessionID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal("complete Session revocation fact", err)
	}
	var revoked bool
	if err = v.raw.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='administrative' FROM agenteam_account.sessions WHERE id=$1`, actor.Details().SessionID).Scan(&revoked); err != nil || !revoked {
		t.Fatal("Session revocation did not persist", err)
	}
	requireCode(t, v.accounts.RequireCurrentSession(ctx, f.Tx{}, actor), f.SessionRevoked)
}

// Adapter lock timeouts poison the actual transaction. They are distinct from
// a domain ResourceBusy refusal after successfully acquiring its locks.
func requireProjectLockTimeout(t *testing.T, err error) {
	t.Helper()
	var fault *f.Fault
	var database *postgres.Error
	if !errors.As(err, &fault) || fault.Code != f.InternalError || fault.CommitState != f.NotCommitted || !errors.As(err, &database) || database.Code() != postgres.LockFailed || database.SQLState() != "55P03" {
		t.Fatal("expected NotCommitted InternalError + DATABASE_LOCK_FAILED/55P03", err, postgres.CodeOf(err))
	}
	t.Log("actual NotCommitted InternalError -> DATABASE_LOCK_FAILED SQLSTATE 55P03")
}
func (v *projectConfigurationFixture) modelCounts(t *testing.T) [6]int64 {
	t.Helper()
	p, m, c, a, e := v.facts(t)
	var refs int64
	if err := v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_secret.secret_references`).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	return [6]int64{p, m, c, a, e, refs}
}
func (v *projectConfigurationFixture) unchanged(t *testing.T, want [6]int64) {
	t.Helper()
	if got := v.modelCounts(t); got != want {
		t.Fatalf("Model/configuration/reference atomicity: got %v want %v", got, want)
	}
}

// Real R2 acceptance persists the operation, manifest, participants, Audit and
// Outbox. The optional terminal SQL is an explicit Authority input, not a claim
// that an unbound participant runtime has stopped. One timestamp supplies every
// terminal field, including the Ref validation ordering.
func (v *projectConfigurationFixture) gate(t *testing.T, state c.Lifecycle) {
	t.Helper()
	version := v.project.Version
	meta := f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: f.IdempotencyKey(newID[struct{}](t).String()), ExpectedVersion: &version}
	if state == c.Deleting {
		var username string
		if err := v.raw.QueryRow(testContext(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, v.owner.Details().UserID).Scan(&username); err != nil {
			t.Fatal(err)
		}
		result, err := v.projectService.BeginDeleteProject(testContext(t), v.owner, meta, v.project.ID, c.DeleteProjectRequest{NormalizedCurrentPath: username + "/" + v.project.NormalizedName, Permanent: true})
		if err != nil || result.Operation == nil {
			t.Fatal("real delete acceptance", err)
		}
		return
	}
	op, err := v.projectService.BeginArchive(testContext(t), v.owner, meta, v.project.ID)
	if err != nil {
		t.Fatal("real archive acceptance", err)
	}
	if state == c.Archived {
		result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
			key, _ := f.ProjectLock(v.project.ID.String())
			if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
				return err
			}
			x, err := v.raw.InTx(tx)
			if err != nil {
				return err
			}
			var at time.Time
			if err = x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped' WHERE operation_id=$1`, op.ID.String()); err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='completed',completed_project_version=project_version+1,completed_at=$2,updated_at=$2,version=version+1 WHERE id=$1`, op.ID.String(), at); err != nil {
				return err
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=$2,updated_at=$2,version=version+1 WHERE id=$1`, v.project.ID.String(), at)
			return err
		})
		if result.State() != f.Committed {
			t.Fatal(result.Fault())
		}
	}
}

// This fixture persists protected/published Skill facts in a test-only schema.
// It is not a D10 implementation or a default production initializer.
type projectConfigurationSkills struct {
	store     project.Store
	authority *project.Authority
	issuer    c.InitializationPlanIssuer
	mu        sync.Mutex
	mode      string
	calls     int
	extraLock f.LockKey
}

func (s *projectConfigurationSkills) setMode(mode string) { s.mu.Lock(); s.mode = mode; s.mu.Unlock() }
func (s *projectConfigurationSkills) InspectProjectSkills(ctx context.Context, actor id.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
	if actor.Details().ServiceName != id.ProjectInitialization || actor.Details().CauseRef != r.CreationID.String() || actor.Details().ProjectID != r.ProjectID.String() {
		return c.InitializationResult{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	var project, key, skill string
	var revision int64
	e := s.store.QueryRow(ctx, `SELECT project_id::text,init_key,skill_id::text,revision FROM model_project_fixture.skills WHERE creation_id=$1`, r.CreationID.String()).Scan(&project, &key, &skill, &revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return c.InitializationResult{State: c.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: c.ReasonWorkPending}, nil
	}
	if e != nil {
		return c.InitializationResult{}, e
	}
	if project != r.ProjectID.String() || key != string(r.InitializationKey) {
		return c.InitializationResult{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	skillID, e := f.ParseID[c.Skill](skill)
	if e != nil {
		return c.InitializationResult{}, e
	}
	rev := f.Revision(revision)
	return c.InitializationResult{State: c.InitializationCompleted, CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: &skillID, Revision: &rev}, nil
}
func (s *projectConfigurationSkills) InitializeProjectSkills(ctx context.Context, actor id.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
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
	projectKey, _ := f.ProjectLock(r.ProjectID.String())
	command, _ := f.NewRecoveryCause("project.fixture-skill", r.CreationID.String(), "")
	result := s.store.WithinTx(ctx, command, func(ctx context.Context, tx f.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: projectKey, Mode: f.Exclusive}}); e != nil {
			return e
		}
		if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
			return e
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return e
		}
		skill, e := f.NewID[c.Skill]()
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO model_project_fixture.skills(creation_id,project_id,init_key,skill_id,revision,protected,published) VALUES($1,$2,$3,$4,1,true,true) ON CONFLICT(creation_id) DO NOTHING`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), skill.String())
		return e
	})
	if result.State() != f.Committed {
		return c.InitializationResult{}, f.NewFault(f.DependencyUnavailable, result.State())
	}
	if mode == "unknown" {
		return c.InitializationResult{}, f.NewFault(f.CommitUnknown, f.Unknown)
	}
	return s.InspectProjectSkills(ctx, actor, r)
}
func (s *projectConfigurationSkills) DiscoverConfirmation(ctx context.Context, actor id.Actor, r c.InitializationRequest) (c.InitializationConfirmationPlan, error) {
	result, e := s.InspectProjectSkills(ctx, actor, r)
	if e != nil {
		return c.InitializationConfirmationPlan{}, e
	}
	if result.State != c.InitializationCompleted {
		return c.InitializationConfirmationPlan{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	key, _ := f.ProjectLock(r.ProjectID.String())
	locks := []f.LockRequest{{Key: key, Mode: f.Exclusive}}
	s.mu.Lock()
	extra := s.extraLock
	s.mu.Unlock()
	if extra.Validate() == nil {
		locks = append(locks, f.LockRequest{Key: extra, Mode: f.Shared})
	}
	return s.issuer.Plan(actor, r, c.InitializationReceipt{CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}, locks)
}
func (s *projectConfigurationSkills) ConfirmInitializedInTx(ctx context.Context, tx f.Tx, actor id.Actor, r c.InitializationRequest, plan c.InitializationConfirmationPlan) (c.InitializationReceipt, error) {
	if !s.issuer.Matches(plan, actor, r) {
		return c.InitializationReceipt{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	locks := plan.RequiredLocks()
	s.mu.Lock()
	extra := s.extraLock
	s.mu.Unlock()
	if extra.Validate() == nil {
		locks = append(locks, f.LockRequest{Key: extra, Mode: f.Shared})
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
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_project_fixture.skills WHERE creation_id=$1 AND project_id=$2 AND init_key=$3 AND skill_id=$4 AND revision=$5 AND protected AND published)`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), receipt.AddSkillsID.String(), int64(receipt.Revision)).Scan(&valid)
	if e != nil {
		return c.InitializationReceipt{}, e
	}
	if !valid {
		return c.InitializationReceipt{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	return receipt, nil
}

type projectConfigurationParticipant struct {
	name  c.ParticipantName
	calls atomic.Int64
}

func (p *projectConfigurationParticipant) Name() c.ParticipantName { return p.name }
func (p *projectConfigurationParticipant) RequestStop(context.Context, id.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	p.calls.Add(1)
	return c.StopReport{}, errors.New("R2 must not call RequestStop")
}
func (p *projectConfigurationParticipant) InspectStop(context.Context, id.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	p.calls.Add(1)
	return c.StopReport{}, errors.New("R2 must not call InspectStop")
}
func (p *projectConfigurationParticipant) Cleanup(context.Context, id.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
	p.calls.Add(1)
	return c.CleanupReport{}, errors.New("R2 must not call Cleanup")
}
func projectConfigurationEntries(version f.Version, skills bool) []c.ParticipantRegistration {
	entries := []c.ParticipantRegistration{
		{Name: c.ArtifactObjectParticipant, ContractVersion: version, OwnerModule: "artifact-object", ReferenceKinds: []c.ReferenceKind{"object"}},
		{Name: c.SecretParticipant, ContractVersion: version, OwnerModule: "secret", ReferenceKinds: []c.ReferenceKind{"secret"}},
		{Name: c.OutboxParticipant, ContractVersion: version, OwnerModule: "outbox", CleanupAfter: []c.ParticipantName{c.ArtifactObjectParticipant, c.SecretParticipant}},
		{Name: c.AuditParticipant, ContractVersion: version, OwnerModule: "audit", CleanupAfter: []c.ParticipantName{c.OutboxParticipant}},
	}
	if skills {
		entries = append(entries, c.ParticipantRegistration{Name: c.SkillsParticipant, ContractVersion: version, OwnerModule: "skills"})
		entries[2].CleanupAfter = append(entries[2].CleanupAfter, c.SkillsParticipant)
	}
	return entries
}
func projectConfigurationRegistryWithEntries(t *testing.T, current []c.ParticipantRegistration, retained ...c.ParticipantRegistration) *project.LifecycleRegistry {
	t.Helper()
	manifest, err := c.NewRequiredManifest(current)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []project.LifecycleParticipantBinding{}
	for _, entry := range append(append([]c.ParticipantRegistration{}, current...), retained...) {
		adapter := &projectConfigurationParticipant{name: entry.Name}
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: adapter})
		t.Cleanup(func() {
			if adapter.calls.Load() != 0 {
				t.Errorf("acceptance invoked participant %s", adapter.name)
			}
		})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func projectConfigurationRegistry(t *testing.T) *project.LifecycleRegistry {
	t.Helper()
	return projectConfigurationRegistryWithEntries(t, projectConfigurationEntries(1, false))
}
