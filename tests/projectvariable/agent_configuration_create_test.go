//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
)

// The target Project has actual P2 publication, Object and its original guard.
// Default Agent options remain omitted/true. All providers, including the fixed
// install builtin Source, are real; no Agent/Tool/assignment SQL seeds are used.
// This tests configuration, not Execution capture, tool dispatch or default app.
func TestAgentConfigurationCreate(t *testing.T) {
	t.Run("default-create-and-replay", func(t *testing.T) {
		v := newAgentCreateFixture(t)
		request, meta, command := v.request(t)
		target := request.Fields().AgentID
		receipt, err := v.agents.CreateAgent(ctxFor(t), v.p2.base.ownerBrowser.actor, meta, v.p2.project.ID, request)
		if err != nil {
			t.Fatal("real Agent Create", err)
		}
		fields := receipt.Fields()
		config := fields.Agent.Fields()
		if receipt.Validate() != nil || !fields.Changed || len(fields.EventIDs) != 1 || config.Core.ID != target || config.Core.ProjectID != v.p2.project.ID || config.Core.Version != 1 || config.Core.Lifecycle != ac.AgentActive || len(config.AllowedToolIDs) != 1 || config.AllowedToolIDs[0] != v.tool.ToolID || len(config.AllowedMountIDs) != 0 || len(config.AllowedSecretVariableIDs) != 0 {
			t.Fatal("Create did not return its complete default-enabled canonical receipt")
		}
		want := [16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}
		v.checkFacts(t, target, meta.IdempotencyKey, command, want, true)
		current, err := v.agents.GetAgent(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.project.ID, target)
		if err != nil || !bytes.Equal(jsonBytes(t, current), jsonBytes(t, fields.Agent)) {
			t.Fatal("current configuration differs from committed receipt", err)
		}
		replay, err := v.agents.CreateAgent(ctxFor(t), v.p2.base.ownerBrowser.actor, meta, v.p2.project.ID, request)
		if err != nil || !bytes.Equal(jsonBytes(t, replay), jsonBytes(t, receipt)) {
			t.Fatal("same-key replay changed the complete original receipt", err)
		}
		digest, err := ac.AgentCommandDigest(v.p2.base.ownerBrowser.actor, meta, v.p2.project.ID, target, ac.CreateAgentCommand, request)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := v.agents.LookupAgentCommand(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.project.ID, ac.CreateAgentCommand, meta.IdempotencyKey, digest)
		if err != nil || lookup.Status() != ac.AgentLookupCommitted || lookup.Receipt() == nil || !bytes.Equal(jsonBytes(t, *lookup.Receipt()), jsonBytes(t, receipt)) {
			t.Fatal("original committed command lookup", err)
		}
		v.checkFacts(t, target, meta.IdempotencyKey, command, want, true)
		v.emptyTaskOccupancy(t)
	})
	t.Run("final-transaction-rollback", func(t *testing.T) {
		v := newAgentCreateFixture(t)
		request, meta, command := v.request(t)
		target := request.Fields().AgentID
		var activityBefore time.Time
		if err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, v.p2.base.ownerBrowser.actor.Details().SessionID).Scan(&activityBefore); err != nil {
			t.Fatal("original Session activity", err)
		}
		marker := errors.New("agent-create-final-callback-rollback")
		var observed bool
		var physical f.CommitResult
		var returned bool
		store := v.p2.base.tracked
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			d := cause.Details()
			if d.Kind != f.CommandsCause || d.Primary.Canonical() != command.Canonical() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var state string
			if err = x.QueryRow(ctx, `SELECT state FROM agenteam_agent.commands WHERE project_id=$1 AND target_id=$2 AND idempotency_key=$3`, v.p2.project.ID.String(), target.String(), string(meta.IdempotencyKey)).Scan(&state); err != nil {
				return err
			}
			if state != "completed" {
				return nil
			} // Original plan/freeze transactions commit normally.
			facts, err := agentCreateFacts(ctx, x, v.p2.project.ID, target, meta.IdempotencyKey)
			if err != nil {
				return err
			}
			if facts != ([16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}) {
				return errors.New("final transaction did not contain all original domain facts")
			}
			if err = agentCreateRelations(ctx, x, v.p2.project.ID, target, meta.IdempotencyKey, v.modelID, v.tool.ToolID, command); err != nil {
				return err
			}
			observed = true
			return marker // Real Store performs ROLLBACK; no replacement CommitResult.
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			d := cause.Details()
			if observed && d.Kind == f.CommandsCause && d.Primary.Canonical() == command.Canonical() {
				physical = result
				returned = true
			}
		}
		store.mu.Unlock()
		clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		receipt, err := v.agents.CreateAgent(ctxFor(t), v.p2.base.ownerBrowser.actor, meta, v.p2.project.ID, request)
		clear()
		var activityAfter time.Time
		if err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, v.p2.base.ownerBrowser.actor.Details().SessionID).Scan(&activityAfter); err != nil || !activityBefore.Equal(activityAfter) {
			t.Fatal("rolled-back Agent write changed original Session activity", err)
		}
		if !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || receipt.Validate() == nil {
			t.Fatal("late real rollback did not preserve the original physical result and cause", err)
		}
		v.checkFacts(t, target, meta.IdempotencyKey, command, [16]int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, false)
		_, err = v.agents.GetAgent(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.project.ID, target)
		requireCode(t, err, f.NotFound)
		digest, err := ac.AgentCommandDigest(v.p2.base.ownerBrowser.actor, meta, v.p2.project.ID, target, ac.CreateAgentCommand, request)
		if err != nil {
			t.Fatal(err)
		}
		lookup, err := v.agents.LookupAgentCommand(ctxFor(t), v.p2.base.ownerBrowser.actor, v.p2.project.ID, ac.CreateAgentCommand, meta.IdempotencyKey, digest)
		if err != nil || lookup.Status() != ac.AgentLookupInProgress || lookup.Receipt() != nil {
			t.Fatal("rolled-back final transaction fabricated a committed receipt", err)
		}
	})
}

type agentCreateFixture struct {
	p2        *skillInstallationFixture
	guard     *object.ProcessGuard
	providers assembly.Providers
	agents    *agent.Service
	modelID   mc.ModelID
	tool      tc.SpecRef
}

func newAgentCreateFixture(t *testing.T) *agentCreateFixture {
	t.Helper()
	var installer *toolruntime.InstallAuthority
	var processGuard *object.ProcessGuard
	var process objc.ProcessID
	var installerRetired sync.Once
	retireInstaller := func() {
		installerRetired.Do(func() {
			installer.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := installer.Drain(ctx); err != nil || !installer.Joined() {
				t.Error("actual InstallAuthority did not retire", err)
			}
		})
	}
	p2 := newSkillInstallationFixtureWithAuthority(t, func(store *hookStore, projects *project.Authority, guard *object.ProcessGuard, p objc.ProcessID) (*skill.Authority, error) {
		process = p
		processGuard = guard
		var err error
		installer, err = toolruntime.NewInstallAuthority(store, guard, p)
		if err != nil {
			return nil, err
		}
		// Construction-failure fallback; the successful composition below
		// retires this borrower before Runtime releases its original guard.
		t.Cleanup(retireInstaller)
		return skill.NewAuthorityWithInstallExecution(store, projects, installer)
	})
	t.Cleanup(retireInstaller)
	source, err := toolruntime.NewInstallSource(installer, p2.service)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := assembly.New(p2.base.tracked, assembly.Options{Accounts: p2.base.accounts, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	if err != nil {
		t.Fatal(err)
	}
	v := &agentCreateFixture{p2: p2, guard: processGuard, providers: graph.Providers()}
	// Registration traverses the actual bound guard and exactly the service
	// constructed with this InstallAuthority, in the original Registry EX Tx.
	p2.base.tx(t, []f.LockRequest{registry.RegistryLock(f.Exclusive)}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		if err := p2.service.RequireInstallBindingInTx(ctx, tx, installer, process); err != nil {
			return err
		}
		registration, err := source.DescribeInTx(ctx, tx)
		if err != nil {
			return err
		}
		if err = source.CheckBindingInTx(ctx, tx, registration); err != nil {
			return err
		}
		v.tool, err = v.providers.Registry.ReconcileBuiltinInTx(ctx, tx, builtin.SkillInstallStableKey)
		return err
	})
	outProcess, err := f.ParseID[oc.Process](process.String())
	if err != nil {
		t.Fatal(err)
	}
	v.modelID = v.createModel(t, fixtureProcess{outProcess})
	v.agents, err = graph.NewService(p2.base.keys, fixtureProcess{outProcess})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		v.agents.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.agents.Drain(ctx); err != nil || !v.agents.Joined() {
			t.Error("original Agent calls did not join", err)
		}
	})
	return v
}

func (v *agentCreateFixture) createModel(t *testing.T, process fixtureProcess) mc.ModelID {
	t.Helper()
	b, p := v.p2.base, v.providers
	aud, err := audit.New(b.tracked, b.keys, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Projects: p.Projects, Models: p.Models})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := model.NewSecretUsageRouter(p.Models, b.accounts)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewSecretAuthority(p.Projects)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), b.keys)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.New(b.tracked, keys, aud, secret.Authorizations{Sessions: b.accounts, System: b.accounts, Projects: projects, Usage: usage, AccountWrites: b.accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = secrets.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secrets.StopMaintenance)
	catalog := event.NewCatalog()
	types, err := model.DefineEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(b.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{model.ModelProducer: p.Models}, Sessions: b.accounts, System: b.accounts, Projects: p.Projects, Audit: aud, Cursors: b.keys, Processes: process})
	if err != nil {
		t.Fatal(err)
	}
	models, err := model.New(b.tracked, p.Models, model.Dependencies{Secret: secrets, Audit: aud, Events: box, ConfigurationEvents: types, Cursors: b.keys})
	if err != nil {
		t.Fatal(err)
	}
	if err = models.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(v.p2.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	command := func() mc.CommandMeta {
		return mc.CommandMeta{Actor: b.ownerBrowser.actor, Scope: scope, Key: f.IdempotencyKey(id[struct{}](t).String())}
	}
	provider, err := models.CreateProvider(ctxFor(t), mc.CreateProviderRequest{CommandMeta: command(), Input: mc.ProviderInput{Name: "agent-create-provider", Protocol: mc.OpenAIChat, BaseURL: "https://agent-create.example/v1", Enabled: true, Options: json.RawMessage(`{}`)}})
	if err != nil {
		t.Fatal("formal Provider create", err)
	}
	pid, err := f.ParseID[mc.Provider](provider.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := models.CreateModel(ctxFor(t), mc.CreateModelRequest{CommandMeta: command(), ProviderID: pid, Input: mc.ModelInput{Name: "agent-create-chat", ProviderModelID: "agent-create-chat", Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}})
	if err != nil {
		t.Fatal("formal Model create", err)
	}
	mid, err := f.ParseID[mc.Model](result.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := models.GetProjectModel(ctxFor(t), b.ownerBrowser.actor, v.p2.project.ID, mid)
	if err != nil || current.Version != 1 {
		t.Fatal("formal current Model", err)
	}
	return mid
}

func (v *agentCreateFixture) request(t *testing.T) (ac.AgentCreate, f.CommandMeta, f.CommandIdentity) {
	t.Helper()
	request, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: id[i.Agent](t), Name: "real-agent", Description: "Actual default configuration", Instructions: "Use the configured project capabilities", InjectAgentsMD: true, ModelRef: v.modelID, ApprovalPolicy: ac.ApprovalAuto, ApprovalModelRef: &v.modelID, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}})
	if err != nil {
		t.Fatal(err)
	}
	expanded := request.Fields()
	if expanded.AddSkillsEnabled == nil || !*expanded.AddSkillsEnabled || expanded.InstallSkillEnabled == nil || !*expanded.InstallSkillEnabled {
		t.Fatal("omitted defaults were not true")
	}
	m := meta(t, id[struct{}](t).String(), nil)
	command, err := ac.AgentCommandIdentity(v.p2.project.ID, ac.CreateAgentCommand, m.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	return request, m, command
}

func (v *agentCreateFixture) checkFacts(t *testing.T, target i.AgentID, key f.IdempotencyKey, command f.CommandIdentity, want [16]int64, relations bool) {
	t.Helper()
	projectLock, _ := f.ProjectLock(v.p2.project.ID.String())
	agentLock, _ := f.AgentLock(target.String())
	v.p2.base.tx(t, []f.LockRequest{{Key: projectLock, Mode: f.Shared}, {Key: agentLock, Mode: f.Shared}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		actual, err := agentCreateFacts(ctx, x, v.p2.project.ID, target, key)
		if err != nil {
			return err
		}
		if actual != want {
			return fmt.Errorf("Agent domain fact counts=%v want=%v", actual, want)
		}
		if relations {
			return agentCreateRelations(ctx, x, v.p2.project.ID, target, key, v.modelID, v.tool.ToolID, command)
		}
		return nil
	})
}

func (v *agentCreateFixture) emptyTaskOccupancy(t *testing.T) {
	t.Helper()
	// A copied fixture projection points its existing real Work constructor at
	// this real P2 Project. It replaces no service, authority or persistence fact.
	b := *v.p2.base
	b.project = v.p2.project
	b.projectAuthority = v.providers.Projects
	_, task := newPreparationTaskInput(t, &b)
	reader, err := execution.NewWorkOccupancy(b.tracked)
	if err != nil {
		t.Fatal(err)
	}
	userLock, _ := f.UserLock(b.ownerBrowser.actor.Details().UserID)
	projectLock, _ := f.ProjectLock(v.p2.project.ID.String())
	scheduleLock, _ := f.ProjectScheduleLock(v.p2.project.ID.String())
	b.tx(t, []f.LockRequest{{Key: userLock, Mode: f.Shared}, {Key: projectLock, Mode: f.Shared}, {Key: scheduleLock, Mode: f.Exclusive}}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		if _, err := v.providers.Projects.RequireOwnerInTx(ctx, tx, b.ownerBrowser.actor, v.p2.project.ID, i.Read); err != nil {
			return err
		}
		occupied, err := reader.ReadInTx(ctx, tx, v.p2.project.ID, []string{task.ID.String()})
		if err != nil {
			return err
		}
		if occupied.Active == nil || occupied.HistoryTaskIDs == nil || len(occupied.Active) != 0 || len(occupied.HistoryTaskIDs) != 0 {
			return errors.New("new real Task unexpectedly has Execution occupancy")
		}
		return nil
	})
}
