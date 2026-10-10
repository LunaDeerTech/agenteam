package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// Structurally valid input only. This fixture never claims a real consumer,
// ProcessGuard, snapshot, lease or accepted persistent call.
func runtimePureRequest(t *testing.T) mc.ModelRequest {
	t.Helper()
	resolve := meetingResolutionTestRequest(t)
	modelID := mustID[mc.Model](t)
	resolve.ModelRef = &modelID
	draft := resolutionTestDraft(t, resolve)
	snapshot, err := draft.Snapshot.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Capabilities.Streaming = true
	call, _ := f.ParseID[mc.Call](resolve.LeaseOwner.Details().ID)
	lease, _ := f.ParseID[sc.Lease](draft.LeaseID)
	messages := runtimeTextMessages("runtime-private-input-canary")
	digest, err := TextInputDigest(messages)
	if err != nil {
		t.Fatal(err)
	}
	r := mc.ModelRequest{Actor: resolve.Actor, CallID: call, Consumer: resolve.Consumer.Clone(),
		Model: mc.ResolvedModel{Snapshot: snapshot, Consumer: resolve.Consumer.Clone(), LeaseOwner: resolve.LeaseOwner,
			CredentialLease: &sc.CredentialLease{LeaseID: lease, CredentialRef: *snapshot.CredentialRef}},
		Input: mc.InputIdentity{OperationID: resolve.Consumer.OperationID, Digest: digest, SchemaVersion: 1}, Messages: messages,
		ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.BoundedRetry}
	if err := r.Validate(); err != nil {
		t.Fatal("pure request shape", err)
	}
	return r
}

func runtimePureAuthority(t *testing.T, store Store, consumer mc.ConsumerAuthority) *RuntimeAuthority {
	t.Helper()
	model, _ := id.RegisterService(id.ModelRuntime)
	secret, _ := id.RegisterService(id.SecretService)
	outbound, _ := id.RegisterService(id.OutboundService)
	a, err := NewRuntimeAuthority(store, RuntimeAuthorizations{Consumers: consumer, Process: &object.ProcessGuard{}, ModelRuntime: model, SecretService: secret, OutboundService: outbound})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRuntimeRequestShapeAndOwnedClone(t *testing.T) {
	r := runtimePureRequest(t)
	accepted, err := prepareRuntimeInput(context.Background(), r, wire.SSEResponse)
	if err != nil {
		t.Fatal(err)
	}
	r.Messages[0].Parts[0].Text.Text = "changed"
	r.Model.Snapshot.Capabilities.InputModalities[0] = "file"
	r.Model.Snapshot.Parameters[0] = '['
	if accepted.Messages[0].Parts[0].Text.Text != "runtime-private-input-canary" || accepted.Model.Snapshot.Capabilities.InputModalities[0] != "text" || string(accepted.Model.Snapshot.Parameters) != "{}" {
		t.Fatal("caller retained mutable input aliases")
	}
	for _, change := range []func(*mc.ModelRequest){
		func(r *mc.ModelRequest) { r.Input.Digest = hash([]byte("different input")) },
		func(r *mc.ModelRequest) {
			r.Model.Snapshot.Identity.AdapterRevision = wire.OpenAIChatStructuredRevision
		},
		func(r *mc.ModelRequest) { r.Model.Snapshot.Capabilities.ToolCalls = true },
		func(r *mc.ModelRequest) { r.Model.CredentialLease = nil; r.Model.Snapshot.CredentialRef = nil },
	} {
		r := runtimePureRequest(t)
		change(&r)
		out, err := prepareRuntimeInput(context.Background(), r, wire.JSONResponse)
		if err == nil || len(out.Messages) != 0 || out.CallID.Validate() == nil {
			t.Fatal("unsupported shape returned accepted material")
		}
	}
}

type runtimeConsumerFunc func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error)

func (fn runtimeConsumerFunc) Discover(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	return fn(ctx, r)
}
func (runtimeConsumerFunc) ValidateInTx(context.Context, f.Tx, mc.ConsumerRequest, mc.ConsumerDependencies) error {
	panic("unexpected acceptance or SQL")
}

func runtimePureService(a *RuntimeAuthority) *Runtime {
	s := &runtimeState{store: a.state().store, authority: a, ready: true, calls: make(map[mc.CallID]*runtimeCall), admissions: make(map[*runtimeAdmission]struct{})}
	return &Runtime{data: func() *runtimeState { return s }}
}

func TestRuntimeConsumerPolicyRejectsBeforeReservation(t *testing.T) {
	r := runtimePureRequest(t)
	consumer := runtimeConsumerFunc(func(_ context.Context, request mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		binding, err := mc.ConsumerBinding(request)
		if err != nil {
			t.Fatal(err)
		}
		deadline, _ := f.NewInstant(time.Now().Add(time.Minute))
		attempts := f.Sequence(2)
		return mc.NewConsumerDependencies(mc.NewPlanIssuer(), mc.ConsumerDependencyDetails{Binding: binding, Mapping: hash([]byte("pure-plan")),
			Locks: []f.LockRequest{projectLock(request.Consumer.ProjectID.String())}, RetryPolicy: &mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &attempts}})
	})
	service := runtimePureService(runtimePureAuthority(t, &noIOStore{}, consumer))
	_, err := service.Chat(context.Background(), r)
	requireCode(t, err, f.CapabilityUnsupported)
	service.StopAdmission()
	if err := service.Drain(context.Background()); err != nil || !service.Joined() {
		t.Fatal("rejected preflight retained ownership")
	}
}

func TestRuntimeStopJoinsOriginalConsumerPreflight(t *testing.T) {
	entered, cancelled, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan error, 1)
	consumer := runtimeConsumerFunc(func(ctx context.Context, _ mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return mc.ConsumerDependencies{}, ctx.Err()
	})
	service := runtimePureService(runtimePureAuthority(t, &noIOStore{}, consumer))
	request := runtimePureRequest(t)
	go func() { _, err := service.Chat(context.Background(), request); returned <- err }()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("preflight did not enter")
	}
	service.StopAdmission()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("original call not cancelled")
	}
	if service.Joined() {
		t.Fatal("cancel acknowledgement was mistaken for join")
	}
	short, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Drain(short); !errors.Is(err, context.Canceled) || service.Joined() {
		t.Fatal("expired drain retired held callback")
	}
	close(release)
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("original cancellation lost")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preflight did not return")
	}
	if err := service.Drain(context.Background()); err != nil || !service.Joined() {
		t.Fatal("actual preflight return did not retire")
	}
}

func TestRuntimeStreamFramesAndSafeNestedOutput(t *testing.T) {
	r := runtimePureRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &runtimeCall{ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1), request: r,
		record: &runtimeRecord{digest: r.Input.Digest, value: uc.Invocation{CallID: r.CallID, ID: mustID[mc.Invocation](t)}}, text: []byte("runtime-private-output-canary")}
	c.gate <- struct{}{}
	stream := &runtimeStream{data: func() *runtimeCall { return c }}
	frame, err := c.frame(mc.ModelFrame{Kind: "call_cancelled", CancelReason: "cancelled"})
	if err != nil || frame.Sequence != 1 || frame.CallID != r.CallID || frame.Validate() != nil {
		t.Fatal("invalid terminal shape")
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatal("terminal not unique")
	}
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	logger.Info("safe", "value", map[string]any{"nested": []any{struct{ Stream any }{stream}}})
	encoded, err := json.Marshal(map[string]any{"stream": stream})
	if err != nil {
		t.Fatal(err)
	}
	all := log.String() + string(encoded) + fmt.Sprintf("%+v %#v", stream, stream)
	for _, private := range []string{"runtime-private-input-canary", "runtime-private-output-canary", r.Input.Digest.String(), r.CallID.String()} {
		if strings.Contains(all, private) {
			t.Fatal("private call state appeared in default output")
		}
	}
}

func TestRuntimeErrorsPreserveCauseAndHideUntrustedText(t *testing.T) {
	original := errors.New("runtime-error-body-canary")
	err := runtimePortError(original)
	if !errors.Is(err, original) || strings.Contains(err.Error(), "canary") {
		t.Fatal("safe error lost cause or leaked text")
	}
	unknown := f.NewFault(f.CommitUnknown, f.Unknown)
	wrapped := fmt.Errorf("runtime-error-body-canary: %w", unknown)
	// Unknown must retain its original identity for recovery, while default
	// outward rendering must still use a safe foundation projection.
	projected := runtimePortError(wrapped)
	if !errors.Is(projected, unknown) || strings.Contains(projected.Error(), "canary") {
		t.Fatal("unknown identity lost or raw outer error leaked")
	}
	model := runtimeModelError(context.Canceled, true, true)
	if model.Category != "cancelled" || !model.Dispatched || !model.PartialOutput || model.Validate() != nil {
		t.Fatal("cancel observation changed")
	}
}

type runtimeCurrentCheck func(context.Context, f.Tx, mc.ConsumerRequest, mc.ConsumerDependencies) error

func (runtimeCurrentCheck) Discover(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	panic("duplicate already owns its original plan")
}
func (fn runtimeCurrentCheck) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, p mc.ConsumerDependencies) error {
	return fn(ctx, tx, r, p)
}

func TestRuntimeActiveDuplicateKeepsOriginalAdmissionContext(t *testing.T) {
	request := runtimePureRequest(t)
	consumer := runtimeConsumerRequest(request, mc.InvokeConsumer, nil, nil)
	binding, err := mc.ConsumerBinding(consumer)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := mc.NewConsumerDependencies(mc.NewPlanIssuer(), mc.ConsumerDependencyDetails{Binding: binding, Mapping: hash([]byte("duplicate-plan")), Locks: []f.LockRequest{projectLock(request.Consumer.ProjectID.String())}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &projectScopeStore{}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	authority := runtimePureAuthority(t, store, runtimeCurrentCheck(func(actual context.Context, tx f.Tx, _ mc.ConsumerRequest, _ mc.ConsumerDependencies) error {
		if actual != ctx || actual.Err() != nil || tx != store.tx {
			return fault(f.InvalidState)
		}
		close(entered)
		<-release
		return fault(f.Forbidden) // current authority, never a fake positive grant
	}))
	service := runtimePureService(authority)
	s := service.state()
	active := &runtimeCall{request: request, record: &runtimeRecord{digest: hash([]byte("active"))}}
	s.calls[request.CallID] = active
	candidate := &runtimeCall{runtime: s, request: request, cancel: cancel, record: &runtimeRecord{value: uc.Invocation{ID: mustID[mc.Invocation](t)}}}
	go func() {
		defer cancel() // the untransferred admission's real retirement boundary
		transferred, err := candidate.admit(ctx, consumer, plan)
		if transferred {
			err = fault(f.InvalidState)
		}
		returned <- err
	}()
	select {
	case <-entered:
	case err := <-returned:
		t.Fatalf("duplicate cancelled before current check: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("duplicate did not enter original transaction")
	}
	if ctx.Err() != nil {
		t.Fatal("live duplicate admission was cancelled")
	}
	select {
	case <-returned:
		t.Fatal("duplicate returned before original current check")
	default:
	}
	close(release)
	select {
	case err := <-returned:
		requireCode(t, err, f.Forbidden)
	case <-time.After(5 * time.Second):
		t.Fatal("duplicate did not join original transaction")
	}
	if store.transactions != 1 || s.calls[request.CallID] != active {
		t.Fatal("duplicate replaced original call or opened another transaction")
	}
}

func TestRuntimeStartFailurePrecedesGateHandoff(t *testing.T) {
	first := errors.New("first-private-failure")
	tail := errors.New("later-private-close-failure")
	for _, seeded := range []bool{false, true} {
		t.Run(fmt.Sprint(seeded), func(t *testing.T) {
			c := &runtimeCall{gate: make(chan struct{})}
			want := tail
			if seeded {
				c.err = runtimePortError(first)
				want = first
			}
			returned := make(chan struct{})
			go func() { c.finishStart(tail); close(returned) }()
			// This is the next gate owner. It must see the published first error
			// as soon as ownership transfers, without waiting for another write.
			select {
			case <-c.gate:
			case <-time.After(5 * time.Second):
				t.Fatal("start gate was not returned")
			}
			c.mu.Lock()
			observed := c.err
			c.mu.Unlock()
			if !errors.Is(observed, want) {
				t.Fatal("gate transferred before first failure was preserved")
			}
			<-returned
			c.mu.Lock()
			final := c.err
			c.mu.Unlock()
			if !errors.Is(final, want) || strings.Contains(final.Error(), "private") {
				t.Fatal("late tail replaced or exposed original failure")
			}
		})
	}
}
