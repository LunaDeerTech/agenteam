package model

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// Reuse the existing text fixture, changing only typed consumer identities.
// This does not establish an Execution, lease, persistent call or authority.
func agentRuntimePureRequest(t *testing.T) mc.ModelRequest {
	t.Helper()
	r := runtimePureRequest(t)
	a, e := mustID[id.Agent](t), mustID[id.Execution](t)
	var err error
	r.Actor, err = id.NewAgentRun(r.Consumer.ProjectID, a, e)
	if err != nil {
		t.Fatal(err)
	}
	r.Consumer = mc.Consumer{Kind: mc.AgentConsumer, Purpose: mc.AgentGeneration, ProjectID: r.Consumer.ProjectID, AgentID: &a, ExecutionID: &e}
	r.Model.Consumer = r.Consumer.Clone()
	r.Model.LeaseOwner, err = sc.NewCredentialLeaseOwner(sc.ExecutionOwner, e.String())
	if err != nil {
		t.Fatal(err)
	}
	r.Input = mc.InputIdentity{ExecutionID: &e, RoundID: mustID[struct{}](t).String(), Digest: r.Input.Digest, SchemaVersion: 1}
	r.RetryClass = mc.AgentRetry
	if err = r.Validate(); err != nil {
		t.Fatal("Agent fixture shape", err)
	}
	return r
}
func agentRuntimePurePlan(t *testing.T, r mc.ModelRequest, p *mc.RetryPolicy) mc.ConsumerDependencies {
	t.Helper()
	request := runtimeConsumerRequest(r, mc.InvokeConsumer, nil, nil)
	binding, err := mc.ConsumerBinding(request)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := mc.NewConsumerDependencies(mc.NewPlanIssuer(), mc.ConsumerDependencyDetails{Binding: binding, Mapping: hash([]byte("controlled-agent-policy")), Locks: []f.LockRequest{projectLock(r.Consumer.ProjectID.String())}, RetryPolicy: p})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func agentRuntimeTiming() mc.AgentRetryTimingFields {
	return mc.AgentRetryTimingFields{InitialRequestTimeout: time.Second, MaxRequestTimeout: time.Minute, TimeoutMultiplier: 2, InitialBackoff: time.Millisecond, MaxBackoff: time.Second}
}

func TestRuntimeAgentTimingGrowthSaturates(t *testing.T) {
	const max = time.Duration(math.MaxInt64)
	for _, v := range []struct {
		value, cap time.Duration
		multiplier uint32
		want       time.Duration
	}{
		{100 * time.Millisecond, time.Second, 2, 200 * time.Millisecond},
		{700 * time.Millisecond, time.Second, 2, time.Second},
		{time.Second, time.Second, 2, time.Second},
		{max/2 + 1, max, 2, max},
		{max / 3, max, 3, max - max%3},
		{time.Nanosecond, max, math.MaxUint32, time.Duration(math.MaxUint32)},
		{max / 4, max, math.MaxUint32, max},
	} {
		got := runtimeGrowDuration(v.value, v.cap, v.multiplier)
		if got != v.want || got <= 0 || got > v.cap {
			t.Fatal("duration growth overflowed or missed cap", v, got)
		}
	}
	value := 100 * time.Millisecond
	for _, want := range []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, time.Second, time.Second} {
		value = runtimeGrowDuration(value, time.Second, 2)
		if value != want {
			t.Fatal("progressive delay differs", value, want)
		}
	}
}

func TestRuntimeAgentAndBoundedConsumerPoliciesStaySeparate(t *testing.T) {
	agent, bounded := agentRuntimePureRequest(t), runtimePureRequest(t)
	for _, request := range []mc.ModelRequest{agent, bounded} {
		if !runtimeSupportedOwner(request) {
			t.Fatal("supported owner rejected")
		}
		for _, mode := range []wire.ResponseMode{wire.JSONResponse, wire.SSEResponse} {
			out, err := prepareRuntimeInput(context.Background(), request, mode)
			if err != nil || out.RetryClass != request.RetryClass || !out.Model.LeaseOwner.Equal(request.Model.LeaseOwner) || out.Input.Digest != request.Input.Digest {
				t.Fatal("runtime changed original owner/profile", err)
			}
		}
	}
	for _, change := range []func(*mc.ModelRequest){
		func(v *mc.ModelRequest) { v.RetryClass = mc.BoundedRetry },
		func(v *mc.ModelRequest) {
			v.Model.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, v.CallID.String())
		},
		func(v *mc.ModelRequest) { v.Model.Snapshot.Capabilities.ToolCalls = true },
		func(v *mc.ModelRequest) {
			v.Consumer.Purpose = mc.AgentCompaction
			v.Model.Consumer = v.Consumer.Clone()
		},
	} {
		changed := agent.Clone()
		change(&changed)
		if out, err := prepareRuntimeInput(context.Background(), changed, wire.JSONResponse); err == nil || out.CallID.Validate() == nil {
			t.Fatal("unsupported Agent profile got through")
		}
	}
	agentPolicy := mc.RetryPolicy{Class: mc.AgentRetry, Categories: []mc.ErrorCategory{"network", "timeout"}}
	plan := agentRuntimePurePlan(t, agent, &agentPolicy)
	got, err := runtimeRetryPolicy(agent, plan)
	if err != nil || got.Class != mc.AgentRetry || got.MaxAttempts != nil || got.Deadline != nil || len(got.Categories) != 2 {
		t.Fatal("Agent policy acquired a bounded attempt/deadline", err)
	}
	got.Categories[0] = "provider_error"
	if plan.Details().RetryPolicy.Categories[0] != "network" {
		t.Fatal("policy returned shared category memory")
	}
	deadline, err := f.NewInstant(time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	one := f.Sequence(1)
	boundedPolicy := mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &one, Categories: []mc.ErrorCategory{}}
	if got, err := runtimeRetryPolicy(bounded, agentRuntimePurePlan(t, bounded, &boundedPolicy)); err != nil || got.Class != mc.BoundedRetry || got.MaxAttempts == nil || *got.MaxAttempts != 1 || got.Deadline == nil {
		t.Fatal("legacy one-attempt policy changed", err)
	}
	for _, pair := range []struct {
		request mc.ModelRequest
		policy  *mc.RetryPolicy
	}{{agent, nil}, {agent, &boundedPolicy}, {bounded, &agentPolicy}} {
		if _, err := runtimeRetryPolicy(pair.request, agentRuntimePurePlan(t, pair.request, pair.policy)); err == nil {
			t.Fatal("missing/cross-consumer retry policy accepted")
		}
	}
	two := f.Sequence(2)
	boundedPolicy.MaxAttempts = &two
	_, err = runtimeRetryPolicy(bounded, agentRuntimePurePlan(t, bounded, &boundedPolicy))
	requireCode(t, err, f.CapabilityUnsupported)
	// Legacy Runtime construction has no Agent timing opt-in. Rejection must
	// precede even a controlled consumer callback, not just protected SQL.
	service := runtimePureService(runtimePureAuthority(t, &noIOStore{}, runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		panic("Agent admission without explicit timing reached consumer")
	})))
	_, err = service.Chat(context.Background(), agent)
	requireCode(t, err, f.CapabilityUnsupported)
	service.StopAdmission()
	if err := service.Drain(context.Background()); err != nil || !service.Joined() {
		t.Fatal("pre-admission denial retained runtime ownership", err)
	}
}

func TestRuntimeAgentRetryNeedsWireFailureAndAuthorizedCategory(t *testing.T) {
	for _, mode := range []string{"wire-network", "request-timeout", "category-not-allowed", "nonretryable", "no-wire-failure", "unknown-dispatch", "bounded", "canceled-parent", "nil-error", "provider-canceled"} {
		ctx, cancel := context.WithCancel(context.Background())
		attempt, cancelAttempt := context.WithCancel(ctx)
		timing := agentRuntimeTiming()
		call := &runtimeCall{ctx: ctx, attemptCtx: attempt, wireFailure: true, record: &runtimeRecord{binding: runtimeBinding{AgentTiming: &timing, Policy: mc.RetryPolicy{Class: mc.AgentRetry, Categories: []mc.ErrorCategory{"network", "timeout"}}}}}
		failure := &mc.ModelError{Category: "network", Retryable: true}
		dispatch, want := uc.NotSent, false
		switch mode {
		case "wire-network":
			want = true
			dispatch = uc.Sent
		case "request-timeout":
			// A retired per-attempt timeout leaves the original call alive.
			cancelAttempt()
			failure = &mc.ModelError{Category: "timeout", Retryable: false}
			want = true
		case "category-not-allowed":
			failure.Category = "rate_limited"
		case "nonretryable":
			failure.Retryable = false
		case "no-wire-failure":
			call.wireFailure = false
		case "unknown-dispatch":
			dispatch = uc.DispatchUnknown
		case "bounded":
			call.record.binding.AgentTiming = nil
		case "canceled-parent":
			cancel()
		case "nil-error":
			failure = nil
		case "provider-canceled":
			failure = &mc.ModelError{Category: "cancelled"}
		}
		if got := call.mayRetry(failure, dispatch); got != want {
			t.Fatal("wrong wire/category/cancel decision", mode, got)
		}
		cancelAttempt()
		cancel()
	}
}

func TestRuntimeAgentCanceledBackoffCreatesNoNextAttempt(t *testing.T) {
	request := agentRuntimePureRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	timing := agentRuntimeTiming()
	original := mustID[mc.Invocation](t)
	// A controlled post-finalization boundary, not fabricated persistent Usage
	// evidence. Any next authority/SQL/I/O call panics, so cancellation must
	// return before reserving a new Invocation or changing the original record.
	call := &runtimeCall{ctx: ctx, cancel: cancel, request: request, retryPending: true, ioRetired: true, retryBackoff: time.Hour,
		runtime: &runtimeState{store: &noIOStore{}, authority: runtimePureAuthority(t, &noIOStore{}, runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
			panic("canceled backoff discovered another attempt")
		}))},
		record: &runtimeRecord{phase: "retry_wait", version: 4, binding: runtimeBinding{AgentTiming: &timing, Policy: mc.RetryPolicy{Class: mc.AgentRetry, Categories: []mc.ErrorCategory{"timeout"}}}, value: uc.Invocation{ID: original, CallID: request.CallID, AttemptIndex: 7, Final: &uc.Final{Status: uc.Failed, Error: &mc.ModelError{Category: "timeout"}}}},
	}
	err := call.nextAgentAttempt(ctx)
	if !errors.Is(err, context.Canceled) || call.record.value.ID != original || call.record.value.AttemptIndex != 7 || call.record.version != 4 || call.record.phase != "retry_wait" || call.pending != nil || call.retirement != nil || call.exchange != nil {
		t.Fatal("cancellation advanced or lost the original attempt", err)
	}
}
