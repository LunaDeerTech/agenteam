package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func TestRuntimeJSONCallRejectsBeforeOwnership(t *testing.T) {
	want := f.NewFault(f.Forbidden, f.NotCommitted)
	calls := 0
	service := runtimePureService(runtimePureAuthority(t, &noIOStore{}, runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		calls++
		return mc.ConsumerDependencies{}, want
	})))
	handle, err := service.BeginChat(context.Background(), runtimePureRequest(t))
	if handle != nil || !errors.Is(err, want) || calls != 1 {
		t.Fatal("rejected preflight transferred a call or changed its original error")
	}
	service.StopAdmission()
	if err := service.Drain(context.Background()); err != nil || !service.Joined() {
		t.Fatal("rejected preflight retained an owner")
	}
}

// These calls are local protocol fixtures, not accepted SQL invocations or
// wire calls. The held original gate supplies the observable return boundary.
func TestRuntimeJSONCallCloseJoinsOnlyItsOriginalOwner(t *testing.T) {
	r := runtimePureRequest(t)
	s := &runtimeState{calls: make(map[mc.CallID]*runtimeCall)}
	makeCall := func() *runtimeCall {
		ctx, cancel := context.WithCancel(context.Background())
		request := r
		request.CallID = mustID[mc.Call](t)
		c := &runtimeCall{runtime: s, request: request, ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1)}
		s.calls[request.CallID] = c
		return c
	}
	first, other := makeCall(), makeCall()
	defer other.cancel()
	h := &runtimeJSONCall{call: first, started: true}
	returned := make(chan error, 1)
	go func() { returned <- h.Close(context.Background()) }()
	select {
	case <-first.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel its original call")
	}
	if h.Joined() || other.ctx.Err() != nil {
		t.Fatal("cancellation was mistaken for join or reached another call")
	}
	select {
	case <-returned:
		t.Fatal("Close returned before the original gate owner")
	default:
	}
	first.leave() // the original controlled callback has actually returned
	select {
	case err := <-returned:
		if err != nil || !h.Joined() {
			t.Fatal("Close failed to retire the original local owner", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not join")
	}
	s.mu.Lock()
	retained := s.calls[other.request.CallID] == other && s.calls[first.request.CallID] == nil
	s.mu.Unlock()
	if !retained || other.ctx.Err() != nil {
		t.Fatal("scoped Close drained unrelated work")
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal("joined Close was not idempotent", err)
	}
}

func TestRuntimeJSONCallUnknownObservationDoesNotRedispatch(t *testing.T) {
	r := runtimePureRequest(t)
	unknown := f.NewFault(f.CommitUnknown, f.Unknown)
	response := mc.ModelResponse{CallID: r.CallID, InvocationID: mustID[mc.Invocation](t), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "private-json-result-canary"}}}}, FinishReason: "stop", Usage: mc.Usage{Source: mc.UnknownUsage}}
	if response.Validate() != nil {
		t.Fatal("invalid controlled response")
	}
	// No runtime/consumer/wire is installed: repeated Result must be a pure
	// observation, even while the original Usage commit remains unknown.
	c := &runtimeCall{request: r, gate: make(chan struct{}, 1), result: &response, record: &runtimeRecord{value: uc.Invocation{Final: &uc.Final{Status: uc.Succeeded}}}}
	c.gate <- struct{}{}
	h := &runtimeJSONCall{call: c, started: true, returned: true, err: unknown}
	for range 2 {
		actual, err := h.Result(context.Background())
		if !errors.Is(err, unknown) || actual.CallID.Validate() == nil {
			t.Fatal("unconfirmed candidate escaped or lost the original Unknown")
		}
	}
	// Simulate only the already-confirmed local retirement boundary. Actual
	// Usage confirmation/SQL is covered by the real Runtime integration path.
	c.mu.Lock()
	c.joined = true
	c.mu.Unlock()
	actual, err := h.Result(context.Background())
	if err != nil || actual.Validate() != nil || actual.Message.Parts[0].Text.Text != "private-json-result-canary" {
		t.Fatal("confirmed original result was lost", err)
	}
	actual.Message.Parts[0].Text.Text = "changed"
	again, err := h.Result(context.Background())
	if err != nil || again.Message.Parts[0].Text.Text != "private-json-result-canary" {
		t.Fatal("caller modified the retained result")
	}
	raw, err := json.Marshal(h)
	if err != nil || strings.Contains(string(raw)+fmt.Sprintf("%+v %#v", h, h), "canary") {
		t.Fatal("default handle projection exposed input or output")
	}
}
