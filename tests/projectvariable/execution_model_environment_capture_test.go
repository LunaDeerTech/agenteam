//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	auditc "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution/prompt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

// This test-only response boundary loses one acknowledgement after the real
// Store has already reported Committed. It never changes the callback, locks,
// SQL, or production authority. Its Unknown envelope is not a claim that the
// PostgreSQL transaction itself returned an unknown physical outcome.
type captureCommitReceiptLoss struct {
	fixtureStore
	mu       sync.Mutex
	match    func(f.TransactionCause) bool
	attempt  f.ID[f.TransactionAttempt]
	fired    bool
	physical f.CommitResult
}

func (s *captureCommitReceiptLoss) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.fixtureStore.WithinTx(ctx, cause, fn)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.fired && result.State() == f.Committed && s.match != nil && s.match(cause) {
		s.fired, s.physical = true, result
		return f.UnknownResult(s.attempt, cause)
	}
	return result
}

func (s *captureCommitReceiptLoss) observed() (bool, f.CommitResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fired, s.physical
}

// Both observations delegate to the real production port and return its
// original result/error. Observation cannot issue permission, seal input or
// turn a failed provider call into success.
type observedExecutionModel struct {
	provider mc.ExecutionModelCaptureProvider
	observe  func(context.Context, f.Tx, mc.ResolvedModel) error
	result   mc.ResolvedModel
	tx       f.Tx
	discover int
	resolve  int
	readErr  error
}

func (o *observedExecutionModel) DiscoverExecutionModel(ctx context.Context, request mc.ExecutionModelCaptureRequest) (mc.ExecutionModelCapturePlan, error) {
	plan, err := o.provider.DiscoverExecutionModel(ctx, request)
	if err == nil {
		o.discover++
	}
	return plan, err
}
func (o *observedExecutionModel) ResolveExecutionModelInTx(ctx context.Context, tx f.Tx, request mc.ExecutionModelCaptureRequest, plan mc.ExecutionModelCapturePlan) (mc.ResolvedModel, error) {
	result, err := o.provider.ResolveExecutionModelInTx(ctx, tx, request, plan)
	if err == nil {
		o.resolve++
		o.tx, o.result = tx, result.Clone()
		if o.observe != nil {
			o.readErr = o.observe(ctx, tx, result.Clone())
		}
	}
	return result, err
}

type observedExecutionEnvironment struct {
	provider vc.ExecutionEnvironment
	observe  func(context.Context, f.Tx, vc.EnvironmentCapture) error
	result   vc.EnvironmentCapture
	tx       f.Tx
	discover int
	resolve  int
	readErr  error
}

func (o *observedExecutionEnvironment) DiscoverExecutionEnvironment(ctx context.Context, request vc.EnvironmentCaptureRequest) (vc.EnvironmentCapturePlan, error) {
	plan, err := o.provider.DiscoverExecutionEnvironment(ctx, request)
	if err == nil {
		o.discover++
	}
	return plan, err
}
func (o *observedExecutionEnvironment) ResolveExecutionEnvironmentInTx(ctx context.Context, tx f.Tx, request vc.EnvironmentCaptureRequest, plan vc.EnvironmentCapturePlan) (vc.EnvironmentCapture, error) {
	result, err := o.provider.ResolveExecutionEnvironmentInTx(ctx, tx, request, plan)
	if err == nil {
		o.resolve++
		o.tx, o.result = tx, result.Clone()
		if o.observe != nil {
			o.readErr = o.observe(ctx, tx, result.Clone())
		}
	}
	return result, err
}

// Source setup is entirely through current Owner services, before Claim and
// Launch. The Secret value is test-owned material and is never returned as
// fixture metadata or obtained from the environment.
func prepareCaptureEnvironment(t *testing.T, v *taskTransitionFixture) (vc.Variable, vc.SecretVariable, *secret.Service) {
	t.Helper()
	ctx, actor, projectID := ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID
	ordinary, err := v.base.service.CreateVariable(ctx, actor, meta(t, "capture-ordinary-create", nil), projectID, createInput(t, "CAPTURE_MODE", "bounded-test"))
	if err != nil || ordinary.Validate() != nil {
		t.Fatal("formal ordinary capture source", err)
	}
	owner := assembleSecretOwnerFixture(t, v.base)
	created, err := owner.owner.CreateSecretVariable(ctx, actor, meta(t, "capture-secret-create", nil), projectID,
		secretCreateInput(t, "CAPTURE_TOKEN", []byte("owned-environment-capture-material")))
	if err != nil || created.Validate() != nil || created.Fields().Variable.Validate() != nil {
		t.Fatal("formal Secret capture source", err)
	}
	variable := created.Fields().Variable
	current, err := v.agent.agents.GetAgent(ctx, actor, projectID, v.agentID)
	if err != nil {
		t.Fatal("current Agent before environment allowlist", err)
	}
	allowed := []i.ProjectVariableID{variable.Fields().ID}
	injectAgentsMD := false // Explicit supported configuration; no fictitious file result.
	change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{AllowedSecretVariableIDs: &allowed, InjectAgentsMD: &injectAgentsMD})
	if err != nil {
		t.Fatal("formal environment allowlist request", err)
	}
	version := current.Fields().Core.Version
	updated, err := v.agent.agents.UpdateAgent(ctx, actor, meta(t, "capture-secret-allowlist", &version), projectID, v.agentID, change)
	if err != nil || updated.Validate() != nil {
		t.Fatal("formal Agent environment allowlist", err)
	}
	fields := updated.Fields().Agent.Fields()
	if fields.Core.Version != version+1 || fields.Core.InjectAgentsMD || len(fields.AllowedSecretVariableIDs) != 1 || fields.AllowedSecretVariableIDs[0] != variable.Fields().ID {
		t.Fatal("Agent did not persist the actual Secret source identity")
	}
	return ordinary.Fields().Variable, variable, owner.secrets
}

// Consumers must be the real Execution-owned implementation. The existing
// Model resolver, Secret router and credential writer retain their own Store
// and transaction checks; no synthetic consumer/input tables are introduced.
func newCaptureModelResolver(t *testing.T, v *taskTransitionFixture, consumers mc.ConsumerAuthority) (*model.Service, *secret.Service, sc.Metadata) {
	t.Helper()
	return newCaptureModelResolverWithPorts(t, v, consumers, captureModelPorts{})
}

// Optional ports only change real service composition. The default remains
// the original preparation-only resolver and Secret usage router.
type captureModelPorts struct {
	projects      *project.Authority
	auditProjects auditc.ProjectAuthority
	usageFactory  func(*model.Authority) (sc.UsageAuthority, error)
	ready         func(*model.Service, *secret.Service, *audit.Service)
}

func newCaptureModelResolverWithPorts(t *testing.T, v *taskTransitionFixture, consumers mc.ConsumerAuthority, ports captureModelPorts) (*model.Service, *secret.Service, sc.Metadata) {
	t.Helper()
	b, projects := v.base, v.agent.providers.Projects
	if ports.projects != nil {
		projects = ports.projects
	}
	registration, err := i.RegisterService(i.SecretService)
	if err != nil {
		t.Fatal("actual Secret service registration", err)
	}
	authority, err := model.NewAuthority(b.tracked, model.Authorizations{Sessions: b.accounts, System: b.accounts, Projects: projects,
		Resolution: &model.ResolutionAuthorizations{Consumers: consumers, SecretService: registration}})
	if err != nil {
		t.Fatal("Model resolution authority", err)
	}
	var auditProjects auditc.ProjectAuthority = projects
	if ports.auditProjects != nil {
		auditProjects = ports.auditProjects
	}
	aud, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Projects: auditProjects, Models: authority})
	if err != nil {
		t.Fatal("Model capture Audit", err)
	}
	var usage sc.UsageAuthority
	if ports.usageFactory == nil {
		usage, err = model.NewSecretUsageRouter(authority, b.accounts)
	} else {
		usage, err = ports.usageFactory(authority)
	}
	if err != nil {
		t.Fatal("actual Model Secret usage routing", err)
	}
	projectSecrets, err := project.NewSecretAuthority(projects)
	if err != nil {
		t.Fatal("actual Project Secret authority", err)
	}
	keys, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`,
		base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), b.keys)
	if err != nil {
		t.Fatal("owned capture keyring", err)
	}
	secrets, err := secret.New(b.tracked, keys, aud, secret.Authorizations{Sessions: b.accounts, System: b.accounts, Projects: projectSecrets, Usage: usage, AccountWrites: b.accounts})
	if err != nil {
		t.Fatal("actual Model capture Secret service", err)
	}
	if err = secrets.Initialize(ctxFor(t)); err != nil {
		t.Fatal("Model capture Secret initialization", err)
	}
	t.Cleanup(secrets.StopMaintenance)
	catalog := event.NewCatalog()
	types, err := model.DefineEvents(catalog)
	if err != nil {
		t.Fatal("Model capture event catalog", err)
	}
	process := captureProviderProcesses{v.agent.guard}.CurrentProcess()
	if process.Validate() != nil {
		t.Fatal("Model capture requires the original Object guard")
	}
	box, err := outbox.New(b.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{model.ModelProducer: authority},
		Sessions: b.accounts, System: b.accounts, Projects: projects, Audit: aud, Cursors: b.keys, Processes: fixtureProcess{process}})
	if err != nil {
		t.Fatal("Model capture Outbox", err)
	}
	models, err := model.New(b.tracked, authority, model.Dependencies{Secret: secrets, Audit: aud, Events: box, ConfigurationEvents: types, Cursors: b.keys})
	if err != nil {
		t.Fatal("actual current Model resolver", err)
	}
	if err = models.Initialize(ctxFor(t)); err != nil {
		t.Fatal("Model resolver initialization", err)
	}
	scope, err := i.InProject(b.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("secret", []string{b.project.ID.String(), b.ownerBrowser.actor.Details().UserID}, string(sc.Create), "capture-model-credential")
	if err != nil {
		t.Fatal("Model credential command", err)
	}
	material, err := sc.NewSecretMaterial([]byte("owned-model-capture-material"))
	if err != nil {
		t.Fatal("owned Model credential material", err)
	}
	defer material.Destroy()
	written, err := secrets.ExecuteWrite(ctxFor(t), sc.WriteRequest{Actor: b.ownerBrowser.actor, Scope: scope, Identity: command, Kind: sc.Create, Purpose: sc.Model, Value: material})
	if err != nil {
		t.Fatal("formal Model credential creation", err)
	}
	current, err := models.GetProjectModel(ctxFor(t), b.ownerBrowser.actor, b.project.ID, v.agent.modelID)
	if err != nil {
		t.Fatal("actual configured Model", err)
	}
	provider, err := models.GetProjectProvider(ctxFor(t), b.ownerBrowser.actor, b.project.ID, current.ProviderID)
	if err != nil {
		t.Fatal("actual configured Provider", err)
	}
	input := provider.Input.Clone()
	input.CredentialRef = &written.Metadata.CredentialRef
	_, err = models.UpdateProvider(ctxFor(t), mc.UpdateProviderRequest{CommandMeta: mc.CommandMeta{Actor: b.ownerBrowser.actor, Scope: scope, Key: "capture-bind-model-credential"}, ID: provider.ID, ExpectedVersion: provider.Version, Input: input})
	if err != nil {
		t.Fatal("formal Provider credential binding", err)
	}
	if ports.ready != nil {
		ports.ready(models, secrets, aud)
	}
	return models, secrets, written.Metadata
}

type observedExecutionMounts struct {
	provider          mt.ExecutionMountCaptureProvider
	result            mt.ExecutionMountSet
	tx                f.Tx
	discover, capture int
}

func (o *observedExecutionMounts) DiscoverExecutionMounts(ctx context.Context, r mt.ExecutionMountCaptureRequest) (mt.ExecutionMountCapturePlan, error) {
	plan, err := o.provider.DiscoverExecutionMounts(ctx, r)
	if err == nil {
		o.discover++
	}
	return plan, err
}
func (o *observedExecutionMounts) CaptureExecutionMountsInTx(ctx context.Context, tx f.Tx, r mt.ExecutionMountCaptureRequest, p mt.ExecutionMountCapturePlan) (mt.ExecutionMountSet, error) {
	result, err := o.provider.CaptureExecutionMountsInTx(ctx, tx, r, p)
	if err == nil {
		o.capture++
		o.result = result.Clone()
		o.tx = tx
	}
	return result, err
}

type modelEnvironmentFixture struct {
	v                  *taskTransitionFixture
	claims             *scheduler.Coordinator
	launchPolicy       ec.Policy
	created            ec.Summary
	request            ec.LaunchRequest
	attempt            string
	authority          *execution.Authority
	projectAuthority   *project.Authority
	configuration      *agent.ExecutionConfigurationAuthority
	task               *work.TaskTrigger
	ordinary           vc.Variable
	secret             vc.SecretVariable
	credential         sc.Metadata
	skills             *observedSkillCapture
	tools              *observedToolCapture
	models             *observedExecutionModel
	environment        *observedExecutionEnvironment
	environmentSecrets *secret.Service
	mounts             *observedExecutionMounts
}

func newModelEnvironmentFixture(t *testing.T) *modelEnvironmentFixture {
	t.Helper()
	return newModelEnvironmentFixtureWithPorts(t, nil, nil)
}

func newModelEnvironmentFixtureWithPorts(t *testing.T,
	modelFactory func(*taskTransitionFixture, *execution.Authority) captureModelPorts,
	beforeClaim func(*taskTransitionFixture, *model.Service, *ec.Policy),
) *modelEnvironmentFixture {
	t.Helper()
	return newModelEnvironmentFixtureWithSource(t, modelFactory, beforeClaim, false)
}

// Deferred setup leaves the real todo and all providers ready for the
// Scheduler runner. It creates no Claim, Launch or preparation input.
func newModelEnvironmentFixtureWithSource(t *testing.T,
	modelFactory func(*taskTransitionFixture, *execution.Authority) captureModelPorts,
	beforeClaim func(*taskTransitionFixture, *model.Service, *ec.Policy),
	deferLaunch bool,
) *modelEnvironmentFixture {
	t.Helper()
	return newModelEnvironmentFixtureWithProjectSource(t, modelFactory, beforeClaim, deferLaunch, nil)
}

// Project selection happens before Execution construction. An Audit decorator
// can then retain that exact original Project without rebuilding its facts map.
func newModelEnvironmentFixtureWithProjectSource(t *testing.T,
	modelFactory func(*taskTransitionFixture, *execution.Authority) captureModelPorts,
	beforeClaim func(*taskTransitionFixture, *model.Service, *ec.Policy),
	deferLaunch bool,
	selectProject func(*taskTransitionFixture) *project.Authority,
) *modelEnvironmentFixture {
	t.Helper()
	v, claims := newSchedulerClaimFixture(t)
	ordinary, secretVariable, environmentSecrets := prepareCaptureEnvironment(t, v)
	projects := v.base.projectAuthority
	if selectProject != nil {
		projects = selectProject(v)
	}
	access, err := project.NewSchedulerExecutionAccess(projects, v.pending)
	if err != nil {
		t.Fatal("capture Project access", err)
	}
	authority, err := execution.NewAuthority(v.base.tracked, projects, access)
	if err != nil {
		t.Fatal("capture Execution authority", err)
	}
	var ports captureModelPorts
	if modelFactory != nil {
		ports = modelFactory(v, authority)
	}
	models, _, credential := newCaptureModelResolverWithPorts(t, v, authority, ports)
	modelCapture, err := model.NewExecutionCapture(models, authority)
	if err != nil {
		t.Fatal("real Model capture adapter", err)
	}
	environmentAuthority, err := pv.NewEnvironmentAuthority(v.base.authority, authority)
	if err != nil {
		t.Fatal("real environment authority", err)
	}
	leases, err := secret.NewProjectVariableLeases(environmentSecrets, environmentAuthority)
	if err != nil {
		t.Fatal("dedicated environment lease authority", err)
	}
	environment, err := pv.NewExecutionEnvironment(environmentAuthority, leases)
	if err != nil {
		t.Fatal("real environment capture", err)
	}
	mounts, err := mount.NewExecutionCapture(v.base.tracked, authority)
	if err != nil {
		t.Fatal("real Mount head capture", err)
	}
	configuration, err := agent.NewExecutionConfiguration(v.agent.providers.Agents, authority)
	if err != nil {
		t.Fatal("real Agent capture", err)
	}
	task, err := work.NewTaskTrigger(v.base.tracked, v.authority, authority)
	if err != nil {
		t.Fatal("real Task capture", err)
	}
	skills, err := skill.NewExecutionBindings(v.agent.providers.SkillAuthority, authority)
	if err != nil {
		t.Fatal("real Skill capture", err)
	}
	tools, err := registry.NewExecutionCapture(v.agent.providers.Registry, authority)
	if err != nil {
		t.Fatal("real Tool capture", err)
	}
	claim, policy := schedulerClaimRequest(t, v)
	if beforeClaim != nil {
		beforeClaim(v, models, &policy)
	}
	x := &modelEnvironmentFixture{v: v, claims: claims, launchPolicy: policy.Clone(), authority: authority, projectAuthority: projects, configuration: configuration, task: task,
		ordinary: ordinary, secret: secretVariable, credential: credential,
		skills: &observedSkillCapture{provider: skills}, tools: &observedToolCapture{provider: tools},
		models: &observedExecutionModel{provider: modelCapture}, environment: &observedExecutionEnvironment{provider: environment},
		environmentSecrets: environmentSecrets,
		mounts:             &observedExecutionMounts{provider: mounts}}
	if deferLaunch {
		// Async consumers must use these original providers directly, not the
		// single-Execution observation taps tied to the fields assigned below.
		return x
	}
	dispatch, err := claims.ClaimTask(ctxFor(t), claim, policy)
	if err != nil {
		t.Fatal("formal capture source Claim", err)
	}
	request, err := dispatch.LaunchRequest()
	if err != nil {
		t.Fatal("original capture Launch request", err)
	}
	handoff, counter := newBusyLaunchHandoff(t, v)
	launched, err := handoff.LaunchOnce(ctxFor(t), v.base.project.ID, dispatch.Summary().ID)
	if err != nil {
		t.Fatal("formal capture source Launch", err)
	}
	launches, _, created := counter.observed()
	if launches != 1 || created.Status != ec.Created || created.SnapshotID != nil || launched.Summary().ExecutionID == nil || *launched.Summary().ExecutionID != created.ID {
		t.Fatal("capture source was not the actual associated created Execution")
	}
	x.created, x.request = created, request
	x.models.observe = x.observeModel
	x.environment.observe = x.observeEnvironment
	return x
}

func (x *modelEnvironmentFixture) newDriver(t *testing.T, withMounts bool) *execution.PreparationDriver {
	t.Helper()
	deps := execution.PreparationDependencies{Agents: x.configuration, Projects: x.projectAuthority,
		Processes: captureProviderProcesses{x.v.agent.guard}, Task: x.task, Skills: x.skills, Tools: x.tools,
		Models: x.models, Environment: x.environment}
	if withMounts {
		deps.Mounts = x.mounts
	}
	driver, err := execution.NewPreparationDriver(x.v.base.tracked, x.authority, deps)
	if err != nil {
		t.Fatal("complete preparation composition", err)
	}
	t.Cleanup(func() {
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := driver.Drain(ctx); err != nil || !driver.Joined() {
			t.Error("original capture call did not join before provider and guard cleanup", err)
		}
	})
	return driver
}

func (x *modelEnvironmentFixture) observeModel(ctx context.Context, tx f.Tx, r mc.ResolvedModel) error {
	if r.Validate() != nil || r.CredentialLease == nil || r.Snapshot.CredentialRef == nil || !r.CredentialLease.CredentialRef.Equal(x.credential.CredentialRef) ||
		r.Snapshot.Identity.ModelID != x.v.agent.modelID || r.Consumer.Purpose != mc.AgentGeneration || r.Consumer.ExecutionID == nil || *r.Consumer.ExecutionID != x.created.ID {
		return errors.New("Model capture did not return the actual credential-bearing generation selection")
	}
	sql, err := x.v.base.tracked.InTx(tx)
	if err != nil {
		return err
	}
	var count int64
	err = sql.QueryRow(ctx, `SELECT count(*) FROM agenteam_model.snapshot_bindings b
 JOIN agenteam_model.snapshots s ON(s.id,s.project_id)=(b.snapshot_id,b.project_id)
 JOIN agenteam_model.resolution_preparations p ON(p.id,p.snapshot_id,p.project_id)=(b.preparation_id,b.snapshot_id,b.project_id)
 JOIN agenteam_secret.secret_leases l ON(l.id,l.credential_id,l.owner_kind,l.owner_id)=(b.lease_id,b.credential_id,b.owner_kind,b.owner_id)
 WHERE b.owner_kind='execution' AND b.owner_id=$1::text::uuid AND b.project_id=$2::text::uuid
 AND b.snapshot_id=$3::text::uuid AND l.id=$4::text::uuid AND l.credential_id=$5::text::uuid
 AND l.consumer='model' AND NOT l.released AND p.phase='committed' AND p.committed_at IS NOT NULL
 AND l.scope='project' AND l.project_id=b.project_id AND b.credential_scope='project' AND b.credential_project_id=b.project_id`,
		x.created.ID.String(), x.created.ProjectID.String(), r.Snapshot.ID.String(), r.CredentialLease.LeaseID.String(), x.credential.CredentialRef.Details().ID.String()).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("Model snapshot, binding and credential lease were not atomic in the caller transaction")
	}
	return nil
}

func (x *modelEnvironmentFixture) observeEnvironment(ctx context.Context, tx f.Tx, r vc.EnvironmentCapture) error {
	d := r.Fields()
	if r.Validate() != nil || len(d.Variables) != 1 || len(d.Secrets) != 1 || d.Variables[0].Fields() != x.ordinary.Fields() || d.Secrets[0].Variable.Fields() != x.secret.Fields() ||
		d.AgentVersion != 2 || x.models.result.CredentialLease == nil || d.Secrets[0].LeaseID == x.models.result.CredentialLease.LeaseID || d.Secrets[0].CredentialRef.Equal(x.credential.CredentialRef) {
		return errors.New("environment did not capture actual ordinary data and a distinct Secret metadata lease")
	}
	sql, err := x.v.base.tracked.InTx(tx)
	if err != nil {
		return err
	}
	var count int64
	err = sql.QueryRow(ctx, `SELECT count(*) FROM agenteam_projectvariable.execution_environments h
 JOIN agenteam_projectvariable.execution_secret_references r ON r.execution_id=h.execution_id
 JOIN agenteam_secret.project_variable_execution_leases l ON(l.execution_id,l.lease_id,l.variable_id)=(r.execution_id,r.lease_id,r.variable_id)
 WHERE h.execution_id=$1::text AND h.project_id=$2::text AND h.agent_id=$3::text
 AND h.agent_version=$4 AND h.ordinary_count=1 AND h.secret_count=1
 AND l.project_id=h.project_id AND l.agent_id=h.agent_id AND l.agent_version=h.agent_version AND l.attempt_binding=h.attempt_binding
 AND l.variable_id=$5::text::uuid AND l.variable_version=$6 AND l.lease_id=$7::text::uuid AND l.credential_id=$8::text::uuid`,
		x.created.ID.String(), x.created.ProjectID.String(), x.created.AgentID.String(), int64(d.AgentVersion), x.secret.Fields().ID.String(), int64(x.secret.Fields().Version), d.Secrets[0].LeaseID.String(), d.Secrets[0].CredentialRef.Details().ID.String()).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("environment references and dedicated lease were not atomic in the caller transaction")
	}
	return nil
}

// Counts are limited to the original Execution; prepared Model intentions are
// deliberately separate from committed snapshots, leases and complete input.
func captureInputCounts(ctx context.Context, sql postgres.SQLExecutor, execution string) ([11]int64, error) {
	var n [11]int64
	err := sql.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_skill.execution_binding_heads WHERE execution_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_skill.execution_bindings WHERE execution_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_tool.execution_configurations WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_tool.execution_references WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE owner_kind='execution' AND owner_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_model.snapshots s JOIN agenteam_model.resolution_preparations b ON(b.snapshot_id,b.project_id)=(s.id,s.project_id) WHERE b.owner_kind='execution' AND b.owner_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='execution' AND owner_id=$1::text::uuid AND consumer='model' AND NOT released),
 (SELECT count(*) FROM agenteam_projectvariable.execution_environments WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_projectvariable.execution_secret_references WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_secret.project_variable_execution_leases WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.preparation_inputs WHERE execution_id=$1::text)`, execution).
		Scan(&n[0], &n[1], &n[2], &n[3], &n[4], &n[5], &n[6], &n[7], &n[8], &n[9], &n[10])
	return n, err
}

func (x *modelEnvironmentFixture) readInput(ctx context.Context, sql postgres.SQLExecutor) (ec.PreparationInput, error) {
	var raw []byte
	var launch, request, command, binding, digest, attempt, process string
	var schema, fence int64
	var at time.Time
	err := sql.QueryRow(ctx, `SELECT p.input,p.launch_digest,p.request_id,p.command_identity,p.attempt_binding,p.input_digest,p.schema_version,p.attempt_id,p.process_id,p.fence,p.captured_at
 FROM agenteam_execution.preparation_inputs p
 JOIN agenteam_execution.preparation_claims c ON(c.execution_id,c.attempt_id,c.process_id,c.fence)=(p.execution_id,p.attempt_id,p.process_id,p.fence)
 JOIN agenteam_execution.preparation_attempts a ON(a.execution_id,a.attempt_id,a.process_id,a.fence)=(p.execution_id,p.attempt_id,p.process_id,p.fence)
 JOIN agenteam_execution.executions e ON(e.id,e.project_id,e.agent_id)=(p.execution_id,p.project_id,p.agent_id)
 WHERE p.execution_id=$1 AND p.project_id=$2 AND p.agent_id=$3
 AND e.status='preparing' AND e.snapshot_id IS NULL AND e.started_at IS NULL AND e.completed_at IS NULL AND e.cancel_requested_at IS NULL
 AND e.version=$4 AND e.request_digest=p.launch_digest`, x.created.ID.String(), x.created.ProjectID.String(), x.created.AgentID.String(), int64(x.created.Version)+1).
		Scan(&raw, &launch, &request, &command, &binding, &digest, &schema, &attempt, &process, &fence, &at)
	if err != nil {
		return ec.PreparationInput{}, err
	}
	input, err := ec.DecodePreparationInput(raw)
	if err != nil {
		return ec.PreparationInput{}, err
	}
	fields := input.Fields()
	wantDigest, err := x.request.Digest()
	if err != nil {
		return ec.PreparationInput{}, err
	}
	wantCommand, err := x.request.Command()
	if err != nil {
		return ec.PreparationInput{}, err
	}
	if input.Validate() != nil || !bytes.Equal(input.CanonicalBytes(), raw) || string(input.Digest()) != digest || launch != string(wantDigest) || request != x.request.Meta.RequestID.String() || command != wantCommand.Canonical() ||
		binding != string(fields.AttemptBinding) || schema != int64(ec.PreparationInputSchemaVersion) || fence != 1 || attempt == "" || attempt != x.attempt || process != (captureProviderProcesses{x.v.agent.guard}).CurrentProcess().String() || !fields.CapturedAt.Time().Equal(at) ||
		!fields.Request.Equal(ec.PreparationRequest{ExecutionID: x.created.ID, Launch: x.request}) {
		return ec.PreparationInput{}, errors.New("complete input lost its exact original Launch, claim, canonical bytes or digest")
	}
	a := fields.Agent.Fields()
	env := fields.Environment.Fields()
	if a.Core.ID != x.created.AgentID || a.Core.Version != 2 || a.Core.InjectAgentsMD || len(a.AllowedMountIDs) != 0 || len(a.AllowedSecretVariableIDs) != 1 || a.AllowedSecretVariableIDs[0] != x.secret.Fields().ID ||
		fields.Model.Snapshot.ID != x.models.result.Snapshot.ID || fields.Model.CredentialLease == nil || x.models.result.CredentialLease == nil || fields.Model.CredentialLease.LeaseID != x.models.result.CredentialLease.LeaseID ||
		len(fields.Tools) != 1 || fields.Tools[0].ToolID != x.v.agent.tool.ToolID || len(fields.Skills.Bindings) != 1 || fields.Skills.Bindings[0].RevisionID != x.skills.result.Bindings[0].RevisionID ||
		len(env.Variables) != 1 || env.Variables[0].Fields() != x.ordinary.Fields() || len(env.Secrets) != 1 || env.Secrets[0].Variable.Fields() != x.secret.Fields() || env.Secrets[0].LeaseID != x.environment.result.Fields().Secrets[0].LeaseID ||
		fields.Mounts.Validate() != nil || fields.Mounts.Mounts == nil || len(fields.Mounts.Mounts) != 0 || fields.Mounts.AgentVersion != a.Core.Version ||
		fields.PlatformPrompt.Revision() != prompt.Current().Revision() || fields.PlatformPrompt.Content() != prompt.Current().Content() {
		return ec.PreparationInput{}, errors.New("complete input did not freeze the actual supported provider results")
	}
	// Test-owned material is never logged. Neither secret plaintext belongs in
	// complete input; ordinary variable values intentionally do.
	if bytes.Contains(raw, []byte("owned-model-capture-material")) || bytes.Contains(raw, []byte("owned-environment-capture-material")) {
		return ec.PreparationInput{}, errors.New("Secret material entered captured input")
	}
	counts, err := captureInputCounts(ctx, sql, x.created.ID.String())
	if err != nil {
		return ec.PreparationInput{}, err
	}
	if counts != ([11]int64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}) {
		return ec.PreparationInput{}, errors.New("input and provider references were not committed as one complete set")
	}
	return input, nil
}

func (x *modelEnvironmentFixture) requireProviderCalls(t *testing.T, withMounts bool) {
	t.Helper()
	if x.models.readErr != nil || x.environment.readErr != nil {
		t.Fatal("original provider facts", errors.Join(x.models.readErr, x.environment.readErr))
	}
	if x.skills.discover != 1 || x.skills.resolve != 1 || x.tools.discover != 1 || x.tools.resolve != 1 || x.models.discover != 1 || x.models.resolve != 1 || x.environment.discover != 1 || x.environment.resolve != 1 ||
		x.skills.tx != x.tools.tx || x.tools.tx != x.models.tx || x.models.tx != x.environment.tx {
		t.Fatal("providers did not return exactly once in the original final transaction")
	}
	if withMounts {
		if x.mounts.discover != 1 || x.mounts.capture != 1 || x.mounts.tx != x.environment.tx || x.mounts.result.Validate() != nil || x.mounts.result.Mounts == nil || len(x.mounts.result.Mounts) != 0 {
			t.Fatal("real Mount head was not captured in the original transaction")
		}
	} else if x.mounts.discover != 0 || x.mounts.capture != 0 {
		t.Fatal("missing Mount dependency was called")
	}
}

func TestExecutionModelEnvironmentCapture(t *testing.T) {
	t.Run("complete-input-unknown-recovery", func(t *testing.T) {
		x := newModelEnvironmentFixture(t)
		driver := x.newDriver(t, true)
		store := x.v.base.tracked
		before := x.v.databaseSnapshot(t)
		var tentative ec.PreparationInput
		ready := false
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if ready || d.Kind != f.JobCause || d.JobType != "execution-preparation" || d.JobID != x.created.ID.String() || x.mounts.capture != 1 {
				return nil
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			x.attempt = d.JobAttemptID
			tentative, err = x.readInput(ctx, sql)
			if err != nil {
				return err
			}
			ready = true
			return nil // Observe all final facts without changing the original callback.
		})
		original := store.fixtureStore
		loss := &captureCommitReceiptLoss{fixtureStore: original, attempt: id[f.TransactionAttempt](t), match: func(cause f.TransactionCause) bool {
			d := cause.Details()
			return ready && d.Kind == f.JobCause && d.JobType == "execution-preparation" && d.JobID == x.created.ID.String()
		}}
		store.fixtureStore = loss
		defer func() { store.setAfter(nil); store.fixtureStore = original }()
		err := driver.Run(ctxFor(t), x.created.ID)
		store.setAfter(nil)
		store.fixtureStore = original
		requireCode(t, err, f.CommitUnknown)
		fired, physical := loss.observed()
		if !ready || !fired || physical.State() != f.Committed {
			t.Fatal("response-loss boundary did not follow the original successful physical COMMIT")
		}
		x.requireProviderCalls(t, true)
		committed, err := x.readInput(ctxFor(t), x.v.base.raw)
		if err != nil || !bytes.Equal(committed.CanonicalBytes(), tentative.CanonicalBytes()) {
			t.Fatal("committed input differed from original same-Tx observation", err)
		}
		if err = driver.ResolveUnknown(ctxFor(t), x.created.ID); err != nil {
			t.Fatal("original capture owner could not observe its committed input", err)
		}
		x.requireProviderCalls(t, true)
		after, err := x.readInput(ctxFor(t), x.v.base.raw)
		if err != nil || after.Digest() != committed.Digest() || !bytes.Equal(after.CanonicalBytes(), committed.CanonicalBytes()) || before != x.v.databaseSnapshot(t) {
			t.Fatal("Unknown observation changed input, provider facts or source business data", err)
		}
		x.requireAttemptReturned(t)
		// Known replay reads the durable input. It cannot rediscover providers,
		// acquire another lease, or create another preparation claim.
		if err = driver.Run(ctxFor(t), x.created.ID); err != nil {
			t.Fatal("complete input replay", err)
		}
		x.requireProviderCalls(t, true)
		x.requireAttemptReturned(t)
	})
	t.Run("missing-provider-rolls-back", func(t *testing.T) {
		x := newModelEnvironmentFixture(t)
		driver := x.newDriver(t, false)
		before := x.v.databaseSnapshot(t)
		store := x.v.base.tracked
		var physical f.CommitResult
		returned := false
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			d := cause.Details()
			if !returned && x.environment.resolve == 1 && d.Kind == f.JobCause && d.JobType == "execution-preparation" && d.JobID == x.created.ID.String() {
				x.attempt = d.JobAttemptID
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		clear := func() { store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		err := driver.Run(ctxFor(t), x.created.ID)
		clear()
		requireCode(t, err, f.DependencyUnbound)
		if !returned || physical.State() != f.NotCommitted {
			t.Fatal("missing provider did not roll back the original capture transaction")
		}
		requireCode(t, physical.Fault(), f.DependencyUnbound)
		x.requireProviderCalls(t, false)
		counts, err := captureInputCounts(ctxFor(t), x.v.base.raw, x.created.ID.String())
		if err != nil || counts != ([11]int64{}) || before != x.v.databaseSnapshot(t) {
			t.Fatal("partial input, provider references or leases escaped physical rollback", err)
		}
		var prepared int64
		err = x.v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_model.resolution_preparations WHERE owner_kind='execution' AND owner_id=$1::text::uuid AND phase='prepared' AND committed_at IS NULL`, x.created.ID.String()).Scan(&prepared)
		if err != nil || prepared != 1 {
			t.Fatal("original Model planning intent was confused with a committed snapshot", err)
		}
		x.requireAttemptReturned(t)
	})
}

func (x *modelEnvironmentFixture) requireAttemptReturned(t *testing.T) {
	t.Helper()
	var phase, status string
	var count, active, fence int64
	err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT a.phase,e.status,c.fence,
 (SELECT count(*) FROM agenteam_execution.preparation_attempts n WHERE n.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.executions n WHERE n.agent_id=e.agent_id AND n.status IN('created','preparing','running','waiting'))
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.preparation_claims c ON c.execution_id=e.id
 JOIN agenteam_execution.preparation_attempts a ON(a.execution_id,a.attempt_id,a.process_id,a.fence)=(c.execution_id,c.attempt_id,c.process_id,c.fence)
 WHERE e.id=$1 AND a.attempt_id=$2 AND e.snapshot_id IS NULL AND e.started_at IS NULL AND e.completed_at IS NULL`, x.created.ID.String(), x.attempt).Scan(&phase, &status, &fence, &count, &active)
	if err != nil || phase != "terminal" || status != string(ec.Preparing) || fence != 1 || count != 1 || active != 1 {
		t.Fatal("returned capture attempt was confused with a sealed Snapshot or Execution termination", err)
	}
}
