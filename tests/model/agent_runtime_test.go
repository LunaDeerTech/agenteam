//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// This is a Model-domain consumer fixture, not production Execution authority.
// It owns only immutable test inputs/identities. Model snapshots, Secret leases,
// calls, attempts, Usage and wire outcomes all come from their real services.
// No Execution, Agent, Round or running-state SQL material is manufactured.
type agentRuntimeConsumer struct {
	base     *textRuntimeConsumer
	issuer   mc.PlanIssuer
	mu       sync.Mutex
	owner    id.Actor
	source   mc.ResolveRequest
	resolved mc.ResolvedModel
	inputs   map[mc.CallID]mc.InputIdentity
	policy   mc.RetryPolicy
}

type agentRuntimeSource struct {
	Resolve         json.RawMessage
	Snapshot, Lease string
	Input           *mc.InputIdentity
	Policy          mc.RetryPolicy
}

func (c *agentRuntimeConsumer) sourceFor(r mc.ConsumerRequest) (agentRuntimeSource, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	deny := func() (agentRuntimeSource, error) { return agentRuntimeSource{}, f.NewFault(f.Forbidden, f.NotStarted) }
	if r.Validate() != nil || !r.Consumer.Equal(c.source.Consumer) || !r.LeaseOwner.Equal(c.source.LeaseOwner) || r.Action == mc.RetireConsumer {
		return deny()
	}
	v := agentRuntimeSource{Resolve: resolutionStable(c.source), Policy: c.policy.Clone()}
	if r.Action == mc.ResolveConsumer {
		if r.Resolve == nil || !bytes.Equal(resolutionStable(*r.Resolve), v.Resolve) {
			return deny()
		}
		return v, nil
	}
	if r.CallID == nil || c.resolved.CredentialLease == nil {
		return deny()
	}
	input, ok := c.inputs[*r.CallID]
	if !ok || r.Input == nil || !reflect.DeepEqual(input, *r.Input) || r.SnapshotID != c.resolved.Snapshot.ID || r.LeaseID == nil || *r.LeaseID != c.resolved.CredentialLease.LeaseID {
		return deny()
	}
	v.Snapshot = c.resolved.Snapshot.ID.String()
	v.Lease = c.resolved.CredentialLease.LeaseID.String()
	clone := input.Clone()
	v.Input = &clone
	return v, nil
}

func (c *agentRuntimeConsumer) locks() []f.LockRequest {
	var locks []f.LockRequest
	add := func(key f.LockKey, err error) {
		if err != nil {
			panic("invalid owned Agent consumer lock")
		}
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	add(f.UserLock(c.owner.Details().UserID))
	add(f.ProjectLock(c.source.Consumer.ProjectID.String()))
	add(f.AgentLock(c.source.Consumer.AgentID.String()))
	add(f.AggregateLock(f.ExecutionAggregate, c.source.Consumer.ExecutionID.String()))
	sort.Slice(locks, func(i, j int) bool { return f.CompareLockKeys(locks[i].Key, locks[j].Key) < 0 })
	return locks
}

func (c *agentRuntimeConsumer) Discover(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if err := ctx.Err(); err != nil {
		return mc.ConsumerDependencies{}, err
	}
	v, err := c.sourceFor(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ConsumerBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(c.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: resolutionDigest(v), Locks: c.locks(), RetryPolicy: &v.Policy})
}

func (c *agentRuntimeConsumer) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	deny := func() error { return f.NewFault(f.Forbidden, f.NotStarted) }
	v, err := c.sourceFor(r)
	if err != nil {
		return err
	}
	x, err := c.base.store.InTx(tx)
	if err != nil {
		return err
	}
	if err = c.base.store.RequireHeldLocks(ctx, tx, c.locks()); err != nil {
		return err
	}
	binding, err := mc.ConsumerBinding(r)
	if err != nil || !plan.Matches(c.issuer, binding, resolutionDigest(v)) {
		return deny()
	}
	// A real current Owner gates this explicitly test-only source. This does
	// not assert that production Execution has granted an Invoke capability.
	if err = c.base.accounts.RequireCurrentSession(ctx, tx, c.owner); err != nil {
		return err
	}
	if _, err = c.base.projects.RequireOwnerInTx(ctx, tx, c.owner, r.Consumer.ProjectID, id.Mutate); err != nil {
		return err
	}
	a := r.Actor.Details()
	if r.Action != mc.FinalizeConsumer {
		if !r.Actor.Equal(c.source.Actor) {
			return deny()
		}
	} else if a.Kind != id.Service || a.ServiceName != id.ModelRuntime || a.ProjectID != r.Consumer.ProjectID.String() {
		return deny()
	}
	if r.Action == mc.ResolveConsumer || r.Action == mc.InvokeConsumer {
		return nil
	}
	// Credential reads and finalization must name the actual current attempt
	// produced by Runtime, including each retry's new Invocation and ordinal.
	var invocation, process, snapshot, lease, agent, execution string
	var fence, ordinal int64
	var retired bool
	err = x.QueryRow(ctx, `SELECT c.invocation_id::text,c.process_id::text,c.fence,a.ordinal,c.retired,c.snapshot_id::text,c.lease_id::text,c.request_data#>>'{Initiator,AgentID}',c.request_data#>>'{Initiator,ExecutionID}' FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id AND a.call_id=c.id AND a.project_id=c.project_id AND a.process_id=c.process_id AND a.fence=c.fence WHERE c.id=$1 AND c.project_id=$2`, r.CallID.String(), r.Consumer.ProjectID.String()).Scan(&invocation, &process, &fence, &ordinal, &retired, &snapshot, &lease, &agent, &execution)
	if err != nil {
		return f.NewFault(f.Forbidden, f.NotStarted).WithCause(err)
	}
	actual, err := c.base.guard.CurrentProcess()
	if err != nil || actual.String() != process || retired || snapshot != v.Snapshot || lease != v.Lease || agent != r.Consumer.AgentID.String() || execution != r.Consumer.ExecutionID.String() || r.Attempt == nil || r.Attempt.InvocationID.String() != invocation || r.Attempt.ProcessID.String() != process || int64(r.Attempt.Fence) != fence || int64(r.Attempt.AttemptIndex) != ordinal {
		return deny()
	}
	if r.Action == mc.FinalizeConsumer && a.CauseRef != invocation {
		return deny()
	}
	return nil
}

func newAgentRuntimeFixture(t *testing.T, backoff time.Duration) (*textRuntimeFixture, *agentRuntimeConsumer) {
	t.Helper()
	timing, err := mc.NewAgentRetryTiming(mc.AgentRetryTimingFields{InitialRequestTimeout: 5 * time.Second, MaxRequestTimeout: 10 * time.Second, TimeoutMultiplier: 2, InitialBackoff: backoff, MaxBackoff: backoff})
	requireTextRuntime(t, err)
	var consumer *agentRuntimeConsumer
	v := newTextRuntimeFixtureWithPorts(t, func(base *textRuntimeConsumer) mc.ConsumerAuthority {
		consumer = &agentRuntimeConsumer{base: base, issuer: mc.NewPlanIssuer(), inputs: make(map[mc.CallID]mc.InputIdentity), policy: mc.RetryPolicy{Class: mc.AgentRetry, Categories: []mc.ErrorCategory{"provider_unavailable"}}}
		return consumer
	}, func(store model.Store, authority *model.RuntimeAuthority, deps model.RuntimeDependencies) (*model.Runtime, error) {
		return model.NewRuntimeWithAgentRetry(store, authority, deps, timing)
	})
	consumer.owner = v.base.owner
	v.wire.allow(t, true)
	return v, consumer
}

func (c *agentRuntimeConsumer) request(t *testing.T, v *textRuntimeFixture, key string) mc.ModelRequest {
	t.Helper()
	credential := v.base.credential(t, sc.Model)
	provider := projectProviderInput()
	provider.BaseURL = "https://fixture.test:8443/case/" + key
	provider.CredentialRef = &credential.CredentialRef
	created, err := v.base.service.CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), Input: provider})
	requireTextRuntime(t, err)
	providerID, err := f.ParseID[mc.Provider](created.ResourceID)
	requireTextRuntime(t, err)
	input := projectModelInput()
	input.ProviderModelID = "fixture-agent-text"
	input.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}
	created, err = v.base.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), ProviderID: providerID, Input: input})
	requireTextRuntime(t, err)
	modelID, err := f.ParseID[mc.Model](created.ResourceID)
	requireTextRuntime(t, err)
	agent, execution := newID[id.Agent](t), newID[id.Execution](t)
	actor, err := id.NewAgentRun(v.base.project.ID, agent, execution)
	requireTextRuntime(t, err)
	owner, err := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, execution.String())
	requireTextRuntime(t, err)
	c.source = mc.ResolveRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.AgentConsumer, ProjectID: v.base.project.ID, AgentID: &agent, ExecutionID: &execution, Purpose: mc.AgentGeneration}, Purpose: mc.AgentGeneration, Source: mc.CurrentSelectionSource, ModelRef: &modelID, Selection: &mc.SelectionRef{Kind: "direct"}, LeaseOwner: owner}
	plan, err := v.base.service.DiscoverResolve(testContext(t), c.source)
	requireTextRuntime(t, err)
	var resolved mc.ResolvedModel
	result := v.base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		var err error
		resolved, err = v.base.service.ResolveModelInTx(ctx, tx, c.source, plan)
		return err
	})
	if result.State() != f.Committed || resolved.CredentialLease == nil {
		t.Fatal("real Agent Model snapshot/ExecutionOwner lease did not commit", result.State())
	}
	c.resolved = resolved
	return c.nextRequest(t)
}

func (c *agentRuntimeConsumer) nextRequest(t *testing.T) mc.ModelRequest {
	t.Helper()
	messages := []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: textRuntimeInputCanary}}}}}
	digest, err := model.TextInputDigest(messages)
	requireTextRuntime(t, err)
	request := mc.ModelRequest{Actor: c.source.Actor, CallID: newID[mc.Call](t), Consumer: c.source.Consumer.Clone(), Model: c.resolved.Clone(), Input: mc.InputIdentity{ExecutionID: c.source.Consumer.ExecutionID, RoundID: newID[struct{}](t).String(), Digest: digest, SchemaVersion: 1}, Messages: messages, ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry}
	requireTextRuntime(t, request.Validate())
	c.mu.Lock()
	c.inputs[request.CallID] = request.Input.Clone()
	c.mu.Unlock()
	return request
}

func agentRuntimeScenario(t *testing.T, v *textRuntimeFixture, status int) string {
	t.Helper()
	first := netfixture.WireScenario{Status: status, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{[]byte(`{"error":{"message":"owned-private-provider-error","type":"fixture"}}`)}}
	second := wireJSON(wireReply(textRuntimeAnswerCanary, "stop", json.RawMessage(`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`), nil))
	key, err := v.wire.net.Create(testContext(t), netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{first, second}})
	requireTextRuntime(t, err)
	v.wire.cases = append(v.wire.cases, key)
	return key
}

func agentRuntimeSuccess(t *testing.T, request mc.ModelRequest, response mc.ModelResponse, err error) {
	t.Helper()
	if err != nil || response.Validate() != nil || response.CallID != request.CallID || len(response.Message.Parts) != 1 || response.Message.Parts[0].Text == nil || response.Message.Parts[0].Text.Text != textRuntimeAnswerCanary {
		t.Fatal("real Agent Runtime final response mismatch", err)
	}
}

func agentRuntimeFacts(t *testing.T, v *textRuntimeFixture, request mc.ModelRequest, phase, failureCategory string, finals ...string) []string {
	t.Helper()
	var actualPhase, ownerKind, ownerID string
	var retired, released bool
	var attemptCount, usageCount int
	err := v.base.raw.QueryRow(testContext(t), `SELECT c.phase,c.retired,l.released,l.owner_kind,l.owner_id::text,(SELECT count(*) FROM agenteam_model.runtime_attempts WHERE call_id=c.id),(SELECT count(*) FROM agenteam_model.invocations WHERE call_id=c.id) FROM agenteam_model.calls c JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1 AND c.snapshot_id=$2 AND c.lease_id=$3`, request.CallID.String(), request.Model.Snapshot.ID.String(), request.Model.CredentialLease.LeaseID.String()).Scan(&actualPhase, &retired, &released, &ownerKind, &ownerID, &attemptCount, &usageCount)
	if err != nil || actualPhase != phase || !retired || released || ownerKind != string(sc.ExecutionOwner) || ownerID != request.Consumer.ExecutionID.String() || attemptCount != len(finals) || usageCount != len(finals) {
		t.Fatal("logical call did not retire while retaining original ExecutionOwner lease", err)
	}
	rows, err := v.base.raw.Query(testContext(t), `SELECT a.id::text,a.ordinal,a.dispatch,a.sequence,i.final_status,i.dispatch,i.last_sequence,(SELECT count(*) FROM agenteam_audit.audit_records au WHERE au.action='secret.resolve' AND au.request_id=a.id),i.usage_source,i.input_tokens,i.output_tokens,i.total_tokens,coalesce(i.error_data->>'category',''),coalesce((i.error_data->>'retryable')::boolean,false) FROM agenteam_model.runtime_attempts a JOIN agenteam_model.invocations i ON i.id=a.id AND i.call_id=a.call_id AND i.attempt_index=a.ordinal WHERE a.call_id=$1 ORDER BY a.ordinal`, request.CallID.String())
	requireTextRuntime(t, err)
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var invocation, dispatch, final, usageDispatch, usageSource, category string
		var ordinal, sequence, usageSequence int64
		var reads int
		var inputTokens, outputTokens, totalTokens *int64
		var retryable bool
		requireTextRuntime(t, rows.Scan(&invocation, &ordinal, &dispatch, &sequence, &final, &usageDispatch, &usageSequence, &reads, &usageSource, &inputTokens, &outputTokens, &totalTokens, &category, &retryable))
		if len(ids) >= len(finals) || ordinal != int64(len(ids)+1) || final != finals[len(ids)] || dispatch != "sent" || usageDispatch != dispatch || sequence != usageSequence || reads != 1 {
			t.Fatal("actual per-attempt Usage/Secret facts mismatch")
		}
		if final == "succeeded" {
			if usageSource != "provider" || inputTokens == nil || *inputTokens != 3 || outputTokens == nil || *outputTokens != 2 || totalTokens == nil || *totalTokens != 5 || category != "" {
				t.Fatal("successful attempt lost original provider Usage")
			}
		} else if usageSource != "unknown" || inputTokens != nil || outputTokens != nil || totalTokens != nil || category != failureCategory || retryable != (failureCategory == "provider_unavailable") {
			t.Fatal("failed attempt invented Usage or changed wire rejection")
		}
		for _, old := range ids {
			if old == invocation {
				t.Fatal("retry reused InvocationID")
			}
		}
		ids = append(ids, invocation)
	}
	requireTextRuntime(t, rows.Err())
	if len(ids) != len(finals) {
		t.Fatal("wrong actual attempt count")
	}
	var leaked bool
	err = v.base.raw.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_model.calls WHERE id=$1 AND (strpos(request_data::text,$2)>0 OR strpos(request_data::text,$3)>0 OR strpos(request_data::text,$4)>0)) OR EXISTS(SELECT 1 FROM agenteam_model.runtime_attempts WHERE call_id=$1 AND (strpos(fact_data::text,$2)>0 OR strpos(fact_data::text,$3)>0 OR strpos(fact_data::text,$4)>0))`, request.CallID.String(), textRuntimeInputCanary, textRuntimeAnswerCanary, "owned-test-credential-never-log").Scan(&leaked)
	if err != nil || leaked {
		t.Fatal("private content entered durable runtime facts", err)
	}
	return ids
}

func agentRuntimeJoined(t *testing.T, v *textRuntimeFixture, key string, requests int) {
	t.Helper()
	v.wire.settled(t, key)
	state := v.wire.state(t, key)
	if len(state.Requests) != requests {
		t.Fatal("actual wire count differs from retry/cancel decision")
	}
	for _, request := range state.Requests {
		if request.Headers.Get("Authorization") != "Bearer owned-test-credential-never-log" || !strings.Contains(request.Body, textRuntimeInputCanary) || request.Body != state.Requests[0].Body {
			t.Fatal("real credential/input handoff mismatch")
		}
	}
	v.reader.mu.Lock()
	defer v.reader.mu.Unlock()
	if len(v.reader.materials) != requests {
		t.Fatal("wrong actual committed credential read count")
	}
	for _, material := range v.reader.materials {
		called := false
		err := material.Use(func([]byte) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("attempt credential material survived wire retirement")
		}
	}
	v.core.StopAdmission()
	requireTextRuntime(t, v.core.Drain(testContext(t)))
	if !v.core.Joined() {
		t.Fatal("Agent Runtime call did not join")
	}
}

func TestModelAgentRetryRuntime(t *testing.T) {
	t.Run("retry-success-and-execution-lease-reuse", func(t *testing.T) {
		v, consumer := newAgentRuntimeFixture(t, 100*time.Millisecond)
		key := agentRuntimeScenario(t, v, 503)
		request := consumer.request(t, v, key)
		response, err := v.core.Chat(testContext(t), request)
		agentRuntimeSuccess(t, request, response, err)
		attempts := agentRuntimeFacts(t, v, request, "succeeded", "provider_unavailable", "failed", "succeeded")
		if response.InvocationID.String() != attempts[1] {
			t.Fatal("response did not name successful retry")
		}
		second := consumer.nextRequest(t)
		response, err = v.core.Chat(testContext(t), second)
		agentRuntimeSuccess(t, second, response, err)
		last := agentRuntimeFacts(t, v, second, "succeeded", "", "succeeded")
		if last[0] == attempts[0] || last[0] == attempts[1] || response.InvocationID.String() != last[0] {
			t.Fatal("new logical call reused prior Invocation")
		}
		agentRuntimeJoined(t, v, key, 3)
	})
	t.Run("cancel-prevents-next-attempt", func(t *testing.T) {
		v, consumer := newAgentRuntimeFixture(t, 5*time.Second)
		key := agentRuntimeScenario(t, v, 503)
		request := consumer.request(t, v, key)
		ctx, cancel := context.WithCancel(testContext(t))
		defer cancel()
		type outcome struct {
			response mc.ModelResponse
			err      error
		}
		done := make(chan outcome, 1)
		go func() { response, err := v.core.Chat(ctx, request); done <- outcome{response, err} }()
		observeCtx := testContext(t)
		// Observe the original first attempt's committed Usage terminal before
		// cancelling the same synchronous call; never sleep to guess timing.
		wireWait(t, "original retry wait after Usage retirement", func() bool {
			var count int
			err := v.base.raw.QueryRow(observeCtx, `SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id AND a.call_id=c.id JOIN agenteam_model.invocations i ON i.id=a.id WHERE c.id=$1 AND c.phase='retry_wait' AND NOT c.retired AND c.finished_at IS NULL AND a.ordinal=1 AND i.final_status='failed'`, request.CallID.String()).Scan(&count)
			return err == nil && count == 1
		})
		cancel()
		select {
		case got := <-done:
			if got.err == nil || got.response.CallID.Validate() == nil || len(got.response.Message.Parts) != 0 {
				t.Fatal("cancel returned a response")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("cancel did not join original Chat")
		}
		agentRuntimeJoined(t, v, key, 1)
		agentRuntimeFacts(t, v, request, "cancelled", "provider_unavailable", "failed")
	})
	t.Run("nonretryable-single-failure", func(t *testing.T) {
		v, consumer := newAgentRuntimeFixture(t, 100*time.Millisecond)
		key := agentRuntimeScenario(t, v, 401)
		request := consumer.request(t, v, key)
		response, err := v.core.Chat(testContext(t), request)
		if err == nil || response.CallID.Validate() == nil || len(response.Message.Parts) != 0 {
			t.Fatal("nonretryable provider rejection returned a response")
		}
		agentRuntimeFacts(t, v, request, "failed", "authentication", "failed")
		agentRuntimeJoined(t, v, key, 1)
	})
}

var _ mc.ConsumerAuthority = (*agentRuntimeConsumer)(nil)
