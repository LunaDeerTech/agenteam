//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const firstRoundAnswer = "owned-first-round-answer-中文"

// The resolver only maps the nonce-owned fixture host to its real private
// container address. Production D04 classification, policy and TLS still run.
type firstRoundResolver struct{ address netip.Addr }

func (r firstRoundResolver) Lookup(ctx context.Context, _ string) ([]netip.Addr, error) {
	return []netip.Addr{r.address}, ctx.Err()
}

type firstRoundFixture struct {
	capture          *modelEnvironmentFixture
	runtimeAuthority *model.RuntimeAuthority
	projects         *project.Authority
	audit            *audit.Service
	secrets          *secret.Service
	ledger           *usage.Service
	runtime          *model.Runtime
	budget           *wire.Budget
	network          *netfixture.Descriptor
	scenario         string
	closed           sync.Once
}

// All business and invocation authority comes from the real Execution
// instance shared with preparation. There is no test ConsumerAuthority and
// no fixture table supplying running, Round, Call, or credential permission.
func newFirstRoundFixture(t *testing.T, held bool) *firstRoundFixture {
	t.Helper()
	x := &firstRoundFixture{}
	x.capture = newModelEnvironmentFixtureWithPorts(t,
		func(v *taskTransitionFixture, owner *execution.Authority) captureModelPorts {
			modelActor, err := i.RegisterService(i.ModelRuntime)
			firstRoundRequire(t, err)
			secretActor, err := i.RegisterService(i.SecretService)
			firstRoundRequire(t, err)
			outboundActor, err := i.RegisterService(i.OutboundService)
			firstRoundRequire(t, err)
			x.runtimeAuthority, err = model.NewRuntimeAuthority(v.base.tracked, model.RuntimeAuthorizations{
				Consumers: owner, Process: v.agent.guard, ModelRuntime: modelActor,
				SecretService: secretActor, OutboundService: outboundActor,
			})
			firstRoundRequire(t, err)
			secretFacts, err := secret.NewProjectAuditAuthority(v.base.tracked)
			firstRoundRequire(t, err)
			x.projects, err = project.NewAuthority(v.base.tracked, project.AuthorityDependencies{
				Sessions: v.base.accounts, Routes: v.base.accounts,
				AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{
					ac.SecretProducer: secretFacts, ac.AccessProducer: x.runtimeAuthority,
				},
			})
			firstRoundRequire(t, err)
			return captureModelPorts{
				projects: x.projects,
				usageFactory: func(authority *model.Authority) (sc.UsageAuthority, error) {
					return model.NewRuntimeSecretUsageRouter(authority, x.runtimeAuthority, v.base.accounts)
				},
				ready: func(_ *model.Service, secrets *secret.Service, audit *audit.Service) {
					x.secrets, x.audit = secrets, audit
				},
			}
		},
		func(v *taskTransitionFixture, models *model.Service, policy *ec.Policy) {
			// Construction-failure fallback; the successful registration below
			// retires Runtime before the original providers and Object guard.
			t.Cleanup(func() { x.close(t) })
			x.configureWire(t, held)
			current, err := models.GetProjectModel(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, v.agent.modelID)
			firstRoundRequire(t, err)
			provider, err := models.GetProjectProvider(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, current.ProviderID)
			firstRoundRequire(t, err)
			input := provider.Input.Clone()
			input.BaseURL = "https://fixture.test:8443/case/" + x.scenario
			scope, err := i.InProject(v.base.project.ID)
			firstRoundRequire(t, err)
			_, err = models.UpdateProvider(ctxFor(t), mc.UpdateProviderRequest{
				CommandMeta: mc.CommandMeta{Actor: v.base.ownerBrowser.actor, Scope: scope, Key: "first-round-real-wire"},
				ID:          provider.ID, ExpectedVersion: provider.Version, Input: input,
			})
			firstRoundRequire(t, err)
			// The actual capture observes this policy; neither Context nor the
			// resulting Model request has its captured Tool list edited later.
			policy.DeniedToolIDs = []i.ToolID{v.agent.tool.ToolID}
			firstRoundRequire(t, policy.Validate())
		})
	t.Cleanup(func() { x.close(t) })
	v := x.capture.v
	invocations, err := usage.NewAuthority(v.base.tracked, usage.Authorizations{
		Sessions: v.base.accounts, Projects: x.projects, Invocations: x.runtimeAuthority,
	})
	firstRoundRequire(t, err)
	x.ledger, err = usage.New(v.base.tracked, invocations, usage.Dependencies{Cursors: v.base.keys})
	firstRoundRequire(t, err)
	firstRoundRequire(t, x.ledger.Initialize(ctxFor(t)))
	timing, err := mc.NewAgentRetryTiming(mc.AgentRetryTimingFields{
		InitialRequestTimeout: 10 * time.Second, MaxRequestTimeout: 20 * time.Second,
		TimeoutMultiplier: 2, InitialBackoff: 100 * time.Millisecond, MaxBackoff: time.Second,
	})
	firstRoundRequire(t, err)
	adapter, err := wire.NewOpenAIChat(x.transport(t, v), x.budget)
	firstRoundRequire(t, err)
	x.runtime, err = model.NewRuntimeWithAgentRetry(v.base.tracked, x.runtimeAuthority, model.RuntimeDependencies{
		Usage: x.ledger, SecretReader: x.secrets, SecretUsage: x.secrets, Adapter: adapter,
	}, timing)
	firstRoundRequire(t, err)
	firstRoundRequire(t, x.runtime.Initialize(ctxFor(t)))
	return x
}

func (x *firstRoundFixture) configureWire(t *testing.T, held bool) {
	t.Helper()
	var err error
	x.network, err = netfixture.Load()
	firstRoundRequire(t, err)
	response, err := json.Marshal(map[string]any{
		"id": "first-round-owned", "object": "chat.completion", "created": 1, "model": "fixture-native-model",
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": firstRoundAnswer}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
	})
	firstRoundRequire(t, err)
	scenario := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{response}}
	if held {
		beforeBody := 0
		scenario.HoldAfter = &beforeBody
	}
	x.scenario, err = x.network.Create(ctxFor(t), netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &scenario})
	firstRoundRequire(t, err)
	x.budget = wire.NewBudget()
}

func (x *firstRoundFixture) transport(t *testing.T, v *taskTransitionFixture) wire.Transport {
	t.Helper()
	policy, err := outbound.NewPolicyService(v.base.tracked, x.audit, outbound.Authorizations{Sessions: v.base.accounts, System: v.base.accounts})
	firstRoundRequire(t, err)
	firstRoundRequire(t, policy.Reload(ctxFor(t)))
	ports, err := outbound.SelectedPorts(8443)
	firstRoundRequire(t, err)
	rule, err := outbound.NewRule(x.network.PrivateIP+"/32", ports, false)
	firstRoundRequire(t, err)
	rules, err := outbound.NewRules(rule)
	firstRoundRequire(t, err)
	actor := v.base.adminBrowser.actor
	command, err := f.NewCommandIdentity("outbound-policy", []string{actor.Details().UserID}, "update", f.IdempotencyKey(id[struct{}](t).String()))
	firstRoundRequire(t, err)
	status := policy.Status()
	if status.Version == nil {
		t.Fatal("actual outbound policy version missing")
	}
	_, err = policy.UpdatePolicy(ctxFor(t), actor, outbound.CommandMeta{Identity: command, ExpectedVersion: *status.Version, HTTPTraceID: id[struct{}](t).String()}, rules)
	firstRoundRequire(t, err)
	trust, err := outbound.LoadTrustStore(x.network.CAFile)
	firstRoundRequire(t, err)
	address, err := netip.ParseAddr(x.network.PrivateIP)
	firstRoundRequire(t, err)
	return wire.Transport{Policy: policy, Trust: trust, Resolver: firstRoundResolver{address}}
}

func (x *firstRoundFixture) prepare(t *testing.T) ec.ExecutionContext {
	t.Helper()
	driver := x.capture.newDriver(t, true)
	firstRoundRequire(t, driver.Run(ctxFor(t), x.capture.created.ID))
	input, err := x.capture.readInput(ctxFor(t), x.capture.v.base.raw)
	firstRoundRequire(t, err)
	firstRoundRequire(t, x.capture.models.readErr)
	firstRoundRequire(t, x.capture.environment.readErr)
	fields := input.Fields()
	if fields.Tools == nil || len(fields.Tools) != 0 || len(fields.Skills.Bindings) != 1 || fields.Agent.Fields().Core.InjectAgentsMD || len(fields.Mounts.Mounts) != 0 {
		t.Fatal("formal preparation did not capture the supported direct-text profile")
	}
	counts, err := captureInputCounts(ctxFor(t), x.capture.v.base.raw, x.capture.created.ID.String())
	firstRoundRequire(t, err)
	if counts != [11]int64{1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1} {
		t.Fatal("formal preparation input and actual resource references were not committed together")
	}
	builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
	firstRoundRequire(t, err)
	built, err := builder.BuildContext(ctxFor(t), input)
	firstRoundRequire(t, err)
	return built
}

func firstRoundRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("real first-round dependency", err)
	}
}

func (x *firstRoundFixture) close(t *testing.T) {
	t.Helper()
	x.closed.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// This is fixture-wide fallback only. Per-Execution completion must
		// independently join its own scoped call before writing terminal facts.
		if x.runtime != nil {
			x.runtime.StopAdmission()
			if err := x.runtime.Drain(ctx); err != nil || !x.runtime.Joined() {
				t.Error("original shared Model Runtime did not join", err)
				return
			}
		}
		if x.budget != nil {
			if err := x.budget.Force(ctx); err != nil || !x.budget.Joined() {
				t.Error("original D04 transport did not join", err)
				return
			}
		}
		if x.network != nil && x.scenario != "" {
			if err := x.network.Release(ctx, x.scenario); err != nil {
				t.Error("owned wire release", err)
			}
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				state, err := x.network.State(ctx, x.scenario)
				settled := err == nil && state.ActiveHandlers == 0 && state.CompletedHandlers == len(state.Requests)
				for _, request := range state.Requests {
					settled = settled && state.Closed[request.Connection]
				}
				if settled {
					break
				}
				select {
				case <-tick.C:
				case <-ctx.Done():
					t.Error("owned D04 handler and connection tails did not join")
					return
				}
			}
		}
	})
}
