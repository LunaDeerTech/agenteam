//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	accountc "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	obc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const textRuntimeInputCanary = "runtime-owned-private-input-中文"
const textRuntimeAnswerCanary = "runtime-owned-private-answer-中文"

// Every process projection delegates to the one real, bound flock guard.
// No random process ID, fake death provider, or zero guard grants Runtime use.
type textRuntimeOutboxProcess struct{ guard *object.ProcessGuard }

func (p textRuntimeOutboxProcess) CurrentProcess() obc.ProcessID {
	actual, err := p.guard.CurrentProcess()
	if err != nil {
		return obc.ProcessID{}
	}
	out, _ := f.ParseID[obc.Process](actual.String())
	return out
}
func (p textRuntimeOutboxProcess) ConfirmStopped(ctx context.Context, process obc.ProcessID) error {
	actual, err := f.ParseID[oc.Process](process.String())
	if err != nil {
		return err
	}
	return p.guard.ConfirmStopped(ctx, actual)
}

type textRuntimeAccountProcess struct{ guard *object.ProcessGuard }

func (p textRuntimeAccountProcess) CurrentProcess() accountc.ProcessID {
	actual, err := p.guard.CurrentProcess()
	if err != nil {
		return accountc.ProcessID{}
	}
	out, _ := f.ParseID[accountc.Process](actual.String())
	return out
}
func (p textRuntimeAccountProcess) ConfirmStopped(ctx context.Context, process accountc.ProcessID) error {
	actual, err := f.ParseID[oc.Process](process.String())
	if err != nil {
		return err
	}
	return p.guard.ConfirmStopped(ctx, actual)
}

type textRuntimeFixture struct {
	base     *projectConfigurationFixture
	consumer *textRuntimeConsumer
	core     *model.Runtime
	reader   *textRuntimeReadTap
	ledger   *usage.Service
	wire     *wireFixture
	account  *account.Service
	log      *recoverylog.Sink
	objects  *object.Runtime
	guard    *object.ProcessGuard
	backend  *object.Backend
	spool    *object.Spool
}

func requireTextRuntime(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("owned Runtime dependency failed", err)
	}
}

func newTextRuntimeFixture(t *testing.T) *textRuntimeFixture {
	t.Helper()
	db := newDatabase(t) // actual continuous 00001..00031, no hand-created call tables
	raw := openStore(t, db.Config(t, nil))
	v := &textRuntimeFixture{base: &projectConfigurationFixture{fixture: &fixture{db: db, raw: raw, store: raw}}}
	t.Cleanup(func() { v.close(t) })
	ak, ck, sk := testKeys(t)
	var err error
	v.base.accounts, err = account.NewAuthority(raw, ak)
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.base.accounts.Initialize(testContext(t)))

	values, err := objectfixture.Environment(testContext(t), db.Name)
	requireTextRuntime(t, err)
	env := make(map[string]string)
	for _, item := range values {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			t.Fatal("owned object environment malformed")
		}
		env[key] = value
	}
	lookup := func(key string) (string, bool) { value, ok := env[key]; return value, ok }
	config, err := object.LoadStorageConfig(lookup)
	requireTextRuntime(t, err)
	v.backend, err = object.NewBackend(config)
	requireTextRuntime(t, err)
	process := newID[oc.Process](t)
	v.spool, err = object.OpenSpool(env[object.EnvironmentPrefix+"SPOOL_DIR"], process)
	requireTextRuntime(t, err)
	v.guard, err = object.OpenProcessGuard(v.spool, process)
	requireTextRuntime(t, err)
	if _, err = v.guard.CurrentProcess(); err == nil {
		t.Fatal("unbound guard unexpectedly usable")
	}
	v.consumer = &textRuntimeConsumer{store: raw, accounts: v.base.accounts, guard: v.guard, issuer: mc.NewPlanIssuer()}
	modelActor, _ := id.RegisterService(id.ModelRuntime)
	secretActor, _ := id.RegisterService(id.SecretService)
	outboundActor, _ := id.RegisterService(id.OutboundService)
	authority, err := model.NewRuntimeAuthority(raw, model.RuntimeAuthorizations{Consumers: v.consumer, Process: v.guard, ModelRuntime: modelActor, SecretService: secretActor, OutboundService: outboundActor})
	requireTextRuntime(t, err)
	secretFacts, err := secret.NewProjectAuditAuthority(raw)
	requireTextRuntime(t, err)
	v.base.projects, err = project.NewAuthority(raw, project.AuthorityDependencies{Sessions: v.base.accounts, Routes: v.base.accounts, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.SecretProducer: secretFacts, ac.AccessProducer: authority}})
	requireTextRuntime(t, err)
	v.consumer.projects = v.base.projects
	v.base.authority, err = model.NewAuthority(raw, model.Authorizations{Sessions: v.base.accounts, System: v.base.accounts, Projects: v.base.projects, Resolution: &model.ResolutionAuthorizations{Consumers: v.consumer, SecretService: secretActor}})
	requireTextRuntime(t, err)
	v.base.aud, err = audit.New(raw, ck, audit.Authorizations{Sessions: v.base.accounts, System: v.base.accounts, Accounts: v.base.accounts, Projects: v.base.projects, Models: v.base.authority})
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.base.aud.CheckStorage(testContext(t)))
	objectService, err := object.New(raw, v.backend, v.spool, v.base.aud, object.Authorizations{Processes: v.guard})
	requireTextRuntime(t, err)
	endpoint, err := object.LoadTransferEndpoint(lookup, config)
	requireTextRuntime(t, err)
	transfers, err := object.NewTransferService(objectService, nil, endpoint)
	requireTextRuntime(t, err)
	v.objects, err = object.NewRuntime(objectService, v.guard, transfers)
	requireTextRuntime(t, err)
	// Fresh empty Object metadata is the narrowly accepted initialization path.
	// This is not old-process recovery or restoration of the historical STOP.
	requireTextRuntime(t, v.objects.Initialize(testContext(t)))
	actualProcess, err := v.guard.CurrentProcess()
	requireTextRuntime(t, err)
	if actualProcess != process {
		t.Fatal("initialized guard identity mismatch")
	}
	projectSecrets, err := project.NewSecretAuthority(v.base.projects)
	requireTextRuntime(t, err)
	router, err := model.NewRuntimeSecretUsageRouter(v.base.authority, authority, v.base.accounts)
	requireTextRuntime(t, err)
	v.base.secrets, err = secret.New(raw, sk, v.base.aud, secret.Authorizations{Sessions: v.base.accounts, System: v.base.accounts, Projects: projectSecrets, Usage: router, AccountWrites: v.base.accounts})
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.base.secrets.Initialize(testContext(t)))
	catalog := ec.NewCatalog()
	v.base.typed, err = model.DefineEvents(catalog)
	requireTextRuntime(t, err)
	projectEvents, err := pc.RegisterProjectEvents(catalog)
	requireTextRuntime(t, err)
	revoked, err := accountc.DefineSessionsRevoked(catalog)
	requireTextRuntime(t, err)
	delivery, err := accountc.DefineDeliveryRequested(catalog)
	requireTextRuntime(t, err)
	outboxProcess := textRuntimeOutboxProcess{v.guard}
	v.base.events, err = outbox.New(raw, catalog, outbox.Authorizations{Producers: map[ec.StableName]obc.ProducerAuthority{model.ModelProducer: v.base.authority, pc.ProjectProducer: v.base.projects, accountc.AccountProducer: v.base.accounts}, Projects: v.base.projects, Sessions: v.base.accounts, System: v.base.accounts, Audit: v.base.aud, Cursors: ck, Processes: outboxProcess})
	requireTextRuntime(t, err)
	v.base.runtime, err = outbox.NewRuntime(v.base.events, nil, outbox.Options{})
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.base.runtime.Initialize(testContext(t)))
	_, err = raw.Exec(testContext(t), `CREATE SCHEMA model_project_fixture; CREATE TABLE model_project_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL);
CREATE SCHEMA model_runtime_fixture;
CREATE TABLE model_runtime_fixture.operations(id uuid PRIMARY KEY,project_id uuid NOT NULL,meeting_id uuid NOT NULL,call_id uuid NOT NULL UNIQUE,initiator uuid NOT NULL,messages jsonb NOT NULL,input_data jsonb NOT NULL,policy_data jsonb NOT NULL,resolve_data jsonb NOT NULL,snapshot_id uuid,lease_id uuid,version bigint NOT NULL,enabled boolean NOT NULL);`)
	requireTextRuntime(t, err)
	v.base.skills = &projectConfigurationSkills{store: raw, authority: v.base.projects, issuer: pc.NewInitializationPlanIssuer()}
	v.base.projectService, err = project.New(raw, project.Dependencies{Authority: v.base.projects, Activity: v.base.accounts, Audit: v.base.aud, Events: v.base.events, ProjectEvents: projectEvents, Initializer: v.base.skills, Processes: outboxProcess, Cursors: ck, LifecycleRegistry: projectConfigurationRegistry(t)}, project.DefaultConfig())
	requireTextRuntime(t, err)
	v.base.deps = model.Dependencies{Secret: v.base.secrets, Audit: v.base.aud, Events: v.base.events, ConfigurationEvents: v.base.typed, Cursors: ck}
	v.base.service, err = model.New(raw, v.base.authority, v.base.deps)
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.base.service.Initialize(testContext(t)))
	requireTextRuntime(t, v.base.service.InitializeMeetingSummarySelection(testContext(t)))
	private := t.TempDir()
	requireTextRuntime(t, os.Chmod(private, 0700))
	logPath := filepath.Join(private, "account-recovery.jsonl")
	v.log, err = recoverylog.Open(logPath)
	requireTextRuntime(t, err)
	accountProcess := textRuntimeAccountProcess{v.guard}
	challenges, err := account.NewChallenges(v.base.accounts, accountProcess.CurrentProcess())
	requireTextRuntime(t, err)
	v.account, err = account.New(account.Dependencies{Authority: v.base.accounts, Audit: v.base.aud, Secrets: v.base.secrets, Events: v.base.events, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: accountProcess, RecoveryLog: v.log, Challenges: challenges})
	requireTextRuntime(t, err)
	bootstrap, err := v.account.Bootstrap(testContext(t))
	if err != nil || !bootstrap.Created || bootstrap.LogState != "written" {
		t.Fatal("formal bootstrap", err)
	}
	password := projectUsageRecoveryMaterial(t, logPath, "bootstrap", "")
	defer password.Destroy()
	browser := (&systemHTTPFixture{account: v.account, password: password}).login(t, "admin@mail.com")
	v.base.admin, v.base.owner, v.base.regular = browser.actor, browser.actor, browser.actor
	v.base.project = v.base.createProject(t, v.base.owner)
	v.base.scope, _ = id.InProject(v.base.project.ID)
	usageAuthority, err := usage.NewAuthority(raw, usage.Authorizations{Sessions: v.base.accounts, Projects: v.base.projects, Invocations: authority})
	requireTextRuntime(t, err)
	v.ledger, err = usage.New(raw, usageAuthority, usage.Dependencies{Cursors: ck})
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.ledger.Initialize(testContext(t)))
	descriptor, err := netfixture.Load()
	requireTextRuntime(t, err)
	policy, err := outbound.NewPolicyService(raw, v.base.aud, outbound.Authorizations{Sessions: v.base.accounts, System: v.base.accounts})
	requireTextRuntime(t, err)
	requireTextRuntime(t, policy.Reload(testContext(t)))
	trust, err := outbound.LoadTrustStore(descriptor.CAFile)
	requireTextRuntime(t, err)
	v.wire = &wireFixture{net: descriptor, policy: policy, actor: v.base.admin, budget: wire.NewBudget(), transport: wire.Transport{Policy: policy, Trust: trust, Resolver: wireResolver{netip.MustParseAddr(descriptor.PrivateIP)}}}
	v.wire.adapter = v.wire.newAdapter(t, v.wire.budget)
	v.reader = &textRuntimeReadTap{CredentialUsageReader: v.base.secrets}
	v.core, err = model.NewRuntime(raw, authority, model.RuntimeDependencies{Usage: v.ledger, SecretReader: v.reader, SecretUsage: v.base.secrets, Adapter: v.wire.adapter})
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.core.Initialize(testContext(t)))
	return v
}

func (v *textRuntimeFixture) close(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	joined := true
	if v.core != nil {
		v.core.StopAdmission()
		if err := v.core.Drain(ctx); err != nil || !v.core.Joined() {
			t.Error("Runtime original owner did not join", err)
			joined = false
		}
	}
	if v.wire != nil {
		if err := v.wire.budget.Force(ctx); err != nil || !v.wire.budget.Joined() {
			t.Error("original wire Budget did not join", err)
			joined = false
		}
		for _, key := range v.wire.cases {
			v.wire.settled(t, key)
		}
	}
	if v.base.projectService != nil {
		v.base.projectService.Stop()
		if err := v.base.projectService.Drain(ctx); err != nil {
			t.Error("Project original owner did not join", err)
			joined = false
		}
	}
	if v.account != nil {
		v.account.StopAdmission()
		if err := v.account.Drain(ctx); err != nil || !v.account.Joined() {
			t.Error("Account original owner did not join", err)
			joined = false
		}
	}
	if v.base.runtime != nil {
		v.base.runtime.StopClaims()
		if err := v.base.runtime.Drain(ctx); err != nil || !v.base.runtime.Joined() {
			t.Error("Outbox original owner did not join", err)
			joined = false
		}
	}
	if v.log != nil {
		if err := v.log.Drain(ctx); err != nil || !v.log.Joined() {
			t.Error("recovery log original owner did not join", err)
			joined = false
		}
	}
	if !joined {
		// Preserve the original failure and the guard. The outer owner must
		// retire this entire failed process; no TTL/late success upgrades it.
		return
	}
	if v.base.secrets != nil {
		v.base.secrets.StopMaintenance()
	}
	if v.objects != nil {
		v.objects.StopAdmission()
		if err := v.objects.Drain(ctx); err != nil {
			t.Error("Object guard retirement incomplete", err)
			return
		}
		if _, err := v.guard.CurrentProcess(); err == nil {
			t.Error("retired Object guard still current")
			return
		}
	} else if v.guard != nil {
		if err := v.guard.Close(); err != nil {
			t.Error("unbound guard close", err)
			return
		}
	}
	if v.backend != nil {
		if err := v.backend.Close(); err != nil {
			t.Error("backend close", err)
		}
	}
	if v.spool != nil {
		if err := v.spool.Close(); err != nil {
			t.Error("spool close", err)
		}
	}
}

func TestModelTextRuntimePersistentWire(t *testing.T) {
	for _, success := range []bool{true, false} {
		name := "policy_deny"
		if success {
			name = "json_success"
		}
		t.Run(name, func(t *testing.T) {
			v := newTextRuntimeFixture(t)
			v.wire.allow(t, success)
			key := v.wire.scenario(t, wireJSON(wireReply(textRuntimeAnswerCanary, "stop", json.RawMessage(`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`), nil)))
			request := v.request(t, key)
			response, err := v.core.Chat(testContext(t), request)
			if success {
				if err != nil || response.Validate() != nil || response.CallID != request.CallID || len(response.Message.Parts) != 1 || response.Message.Parts[0].Text == nil || response.Message.Parts[0].Text.Text != textRuntimeAnswerCanary {
					t.Fatal("actual text Chat failed", err)
				}
			} else if err == nil || response.CallID.Validate() == nil || len(response.Message.Parts) != 0 {
				t.Fatal("policy rejection returned a response")
			}
			invocation := v.assertFacts(t, request, success)
			if success && response.InvocationID != invocation {
				t.Fatal("response Invocation differs from original persisted attempt")
			}
			v.reader.requireDestroyed(t)
			v.wire.settled(t, key)
			state := v.wire.state(t, key)
			wantRequests := 0
			if success {
				wantRequests = 1
			}
			if len(state.Requests) != wantRequests {
				t.Fatal("wire dispatch count differs from durable decision")
			}
			if success && (state.Requests[0].Headers.Get("Authorization") != "Bearer owned-test-credential-never-log" || !strings.Contains(state.Requests[0].Body, textRuntimeInputCanary)) {
				t.Fatal("actual credential/input handoff mismatch")
			}
			v.core.StopAdmission()
			requireTextRuntime(t, v.core.Drain(testContext(t)))
			if !v.core.Joined() {
				t.Fatal("terminal call left a retained Runtime owner")
			}
		})
	}
}
