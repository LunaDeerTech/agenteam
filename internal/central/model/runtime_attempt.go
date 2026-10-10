package model

import (
	"context"
	"errors"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// The registry owns this handle before its first SQL write. An operation gate
// serializes Next/Chat/Close without holding a mutex across callbacks or I/O.
type runtimeCall struct {
	runtime                                  *runtimeState
	request                                  mc.ModelRequest
	mode                                     wire.ResponseMode
	ctx                                      context.Context
	cancel                                   context.CancelFunc
	gate                                     chan struct{}
	mu                                       sync.Mutex
	record                                   *runtimeRecord
	pending                                  *runtimeMutation
	retirement                               *runtimeRetirement
	exchange                                 *wire.Exchange
	material                                 sc.SecretMaterial
	materialOwned                            bool
	ioRetired                                bool
	accepted, handoff, startReturned, joined bool
	terminalPublished                        bool
	frameSequence                            f.Sequence
	frames                                   []mc.ModelFrame
	text                                     []byte
	result                                   *mc.ModelResponse
	err                                      error
}

func runtimePortError(err error) error {
	if err == nil {
		return nil
	}
	var faultValue *f.Fault
	if errors.As(err, &faultValue) {
		// Preserve the original chain/Unknown attempt for explicit inspection,
		// but do not expose an outer error string or untrusted field paths.
		return f.NewFault(faultValue.Code.Safe(), faultValue.CommitState.Safe()).WithCause(err)
	}
	var modelError *mc.ModelError
	if errors.As(err, &modelError) && modelError.Validate() == nil {
		copy := *modelError
		return &copy
	}
	return unavailable(err)
}

func (c *runtimeCall) enter(ctx context.Context, wait bool) error {
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	if !wait {
		select {
		case <-c.gate:
			return nil
		default:
			return fault(f.ResourceBusy)
		}
	}
	select {
	case <-c.gate:
		return nil
	case <-ctx.Done():
		return unavailable(ctx.Err())
	}
}
func (c *runtimeCall) leave() { c.gate <- struct{}{} }

func (r *Runtime) begin(ctx context.Context, request mc.ModelRequest, mode wire.ResponseMode) (*runtimeCall, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, err
	}
	s := r.state()
	if s == nil {
		return nil, fault(f.DependencyUnbound)
	}
	ctx, stop := context.WithCancel(ctx)
	admission := &runtimeAdmission{cancel: stop, done: make(chan struct{})}
	s.mu.Lock()
	if s.stopped || !s.ready {
		s.mu.Unlock()
		stop()
		return nil, fault(f.ShuttingDown)
	}
	s.admissions[admission] = struct{}{}
	s.mu.Unlock()
	transferred := false
	defer func() {
		if !transferred {
			stop()
		}
		s.mu.Lock()
		delete(s.admissions, admission)
		close(admission.done)
		s.mu.Unlock()
	}()
	request, err := prepareRuntimeInput(ctx, request, mode)
	if err != nil {
		return nil, err
	}
	consumer := runtimeConsumerRequest(request, mc.InvokeConsumer, nil, nil)
	plan, err := s.authority.discoverConsumer(ctx, consumer)
	if err != nil {
		return nil, err
	}
	policy, err := runtimeRetryPolicy(request, plan)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	if policy.Deadline.Time().Before(deadline) {
		deadline = policy.Deadline.Time()
	}
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	if !time.Now().Before(deadline) {
		return nil, unavailable(context.DeadlineExceeded)
	}
	ctx, cancelDeadline := context.WithDeadline(ctx, deadline)
	defer func() {
		if !transferred {
			cancelDeadline()
		}
	}()
	process, err := s.authority.state().auth.Process.CurrentProcess()
	if err != nil {
		return nil, runtimePortError(err)
	}
	invocation, err := f.NewID[mc.Invocation]()
	if err != nil {
		return nil, unavailable(err)
	}
	// Current consumer authorization precedes the protected snapshot read.
	p, at, err := runtimePreflight(ctx, s, request, invocation, consumer, plan)
	if err != nil {
		return nil, err
	}
	snapshotBytes, err := encoded(p.Draft.Snapshot)
	if err != nil {
		return nil, err
	}
	binding := runtimeBinding{Format: 1, CallID: request.CallID, Consumer: request.Consumer.Clone(), Input: request.Input.Clone(), Initiator: runtimeStableActor(request.Actor), SnapshotID: request.Model.Snapshot.ID, SnapshotDigest: hash(snapshotBytes), PreparationID: p.ID, LeaseID: request.Model.CredentialLease.LeaseID, Owner: request.Model.LeaseOwner.Details(), Policy: policy, Mode: mode}
	if err = binding.validate(); err != nil {
		return nil, err
	}
	raw, err := encoded(binding)
	if err != nil {
		return nil, err
	}
	value := uc.Invocation{ID: invocation, CallID: request.CallID, AttemptIndex: 1, Consumer: request.Consumer.Clone(), SnapshotID: request.Model.Snapshot.ID, Identity: request.Model.Snapshot.Identity, ProcessID: process, Fence: 1, Dispatch: uc.Reserved, StartedAt: at, Usage: mc.Usage{Source: mc.UnknownUsage}}
	cancel := func() { cancelDeadline(); stop() }
	c := &runtimeCall{runtime: s, request: request, mode: mode, ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1), record: &runtimeRecord{binding: binding, digest: hash(raw), value: value, sequence: 1, phase: "accepted", version: 1}}
	c.gate <- struct{}{}
	s.mu.Lock()
	if s.stopped || !s.ready || process != s.process {
		s.mu.Unlock()
		cancel()
		return nil, fault(f.ShuttingDown)
	}
	if s.calls[request.CallID] != nil {
		s.mu.Unlock()
		cancel()
		return nil, c.duplicate(ctx, consumer, plan)
	}
	count := 0
	for _, active := range s.calls {
		if active.request.Consumer.ProjectID == request.Consumer.ProjectID {
			count++
		}
	}
	if len(s.calls) >= 64 || count >= 8 {
		s.mu.Unlock()
		cancel()
		return nil, fault(f.ResourceBusy)
	}
	s.calls[request.CallID] = c
	transferred = true
	s.mu.Unlock()
	// Start itself owns this gate until all acceptance/material/handoff work
	// has returned. Stop/Drain may cancel it but must join that original work.
	<-c.gate
	err = c.start(consumer, plan)
	c.leave()
	if err != nil {
		c.mu.Lock()
		c.err = runtimePortError(err)
		c.mu.Unlock()
		return nil, runtimePortError(err)
	}
	return c, nil
}

func (c *runtimeCall) duplicate(ctx context.Context, consumer mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	locks, err := runtimeCallLocks(c.request.Consumer.ProjectID, c.request.CallID, c.record.value.ID)
	if err != nil {
		return err
	}
	locks, err = resolutionUnion(locks, plan.RequiredLocks())
	if err != nil {
		return err
	}
	cause, err := runtimeCallCause(c.request.Consumer.ProjectID, c.request.CallID)
	if err != nil {
		return err
	}
	result := c.runtime.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := c.runtime.store.AcquireAll(ctx, tx, locks); err != nil {
			return runtimePortError(err)
		}
		if err := c.runtime.authority.validateConsumer(ctx, tx, consumer, plan); err != nil {
			return err
		}
		x, err := c.runtime.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		old, err := loadRuntimeRecord(ctx, x, c.request.CallID)
		if err != nil {
			return err
		}
		c.runtime.mu.Lock()
		active := c.runtime.calls[c.request.CallID]
		c.runtime.mu.Unlock()
		if active != nil {
			active.mu.Lock()
			pending := active.pending != nil || active.retirement != nil
			digest := active.record.digest
			active.mu.Unlock()
			if digest != c.record.digest {
				return fault(f.IdempotencyKeyReused)
			}
			if pending {
				return f.NewFault(f.CommitUnknown, f.Unknown)
			}
		}
		return runtimeDuplicateError(old, c.record.digest)
	})
	return commitError(result)
}

func runtimeDuplicateError(old *runtimeRecord, digest f.Digest) error {
	if old == nil {
		return fault(f.ResourceBusy)
	}
	if old.digest != digest {
		return fault(f.IdempotencyKeyReused)
	}
	if old.value.Final != nil {
		return fault(f.InvalidState)
	}
	return fault(f.ResourceBusy)
}

func (c *runtimeCall) start(consumer mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	if err := c.persist(c.ctx, uc.ReserveAction, c.record, consumer, plan); err != nil {
		c.mu.Lock()
		retained := c.pending != nil || c.accepted
		c.mu.Unlock()
		if !retained {
			c.releaseLocal()
		}
		return err
	}
	if err := c.readMaterial(c.ctx); err != nil {
		return c.failStart(c.ctx, err)
	}
	// Re-discover current consumer authority immediately before authorization.
	plan, err := c.runtime.authority.discoverConsumer(c.ctx, consumer)
	if err != nil {
		return c.failStart(c.ctx, err)
	}
	policy, err := runtimeRetryPolicy(c.request, plan)
	if err != nil || !resolutionEqual(policy, c.record.binding.Policy) {
		if err == nil {
			err = fault(f.ResourceBusy)
		}
		return c.failStart(c.ctx, err)
	}
	next := c.copyRecord()
	next.value.Dispatch, next.phase = uc.Authorized, "running"
	if err = runtimeAdvance(next); err != nil {
		return c.failStart(c.ctx, err)
	}
	if err = c.persist(c.ctx, uc.ObserveAction, next, consumer, plan); err != nil {
		return err
	}
	if err = runtimeContextError(c.ctx); err != nil {
		return c.failStart(c.ctx, err)
	}
	callContext, err := c.outboundContext()
	if err != nil {
		return c.failStart(c.ctx, err)
	}
	c.mu.Lock()
	if c.handoff {
		c.mu.Unlock()
		return fault(f.InvalidState)
	}
	c.handoff = true
	material := c.material
	c.mu.Unlock()
	x, err := c.runtime.deps.Adapter.Start(context.WithValue(c.ctx, runtimeHandoffKey{}, c), wire.Request{Snapshot: c.request.Model.Snapshot, Messages: c.request.Messages, ToolChoice: c.request.ToolChoice, ResponseFormat: c.request.ResponseFormat, Mode: c.mode}, c.callOptions(callContext, material))
	c.mu.Lock()
	c.startReturned, c.exchange = true, x
	c.mu.Unlock()
	if err != nil {
		return c.failStart(c.ctx, err)
	}
	return nil
}

func (c *runtimeCall) copyRecord() *runtimeRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	copy := *c.record
	copy.value = c.record.value.Clone()
	return &copy
}

func (c *runtimeCall) releaseLocal() {
	c.cancel()
	c.mu.Lock()
	c.joined = true
	c.request.Messages = nil
	clear(c.text)
	c.text = nil
	c.mu.Unlock()
	s := c.runtime
	s.mu.Lock()
	if s.calls[c.request.CallID] == c {
		delete(s.calls, c.request.CallID)
	}
	s.mu.Unlock()
}

func (c *runtimeCall) close(ctx context.Context) error {
	c.cancel()
	if err := c.enter(ctx, true); err != nil {
		return err
	}
	defer c.leave()
	defer func() { c.terminalPublished = true }()
	c.mu.Lock()
	joined := c.joined
	c.mu.Unlock()
	if joined {
		return nil
	}
	return c.finish(ctx, nil, unavailable(context.Canceled))
}

func (c *runtimeCall) technicalActor() (id.Actor, error) {
	scope, err := id.InProject(c.request.Consumer.ProjectID)
	if err != nil {
		return id.Actor{}, err
	}
	return c.runtime.authority.state().auth.ModelRuntime.Actor(c.copyRecord().value.ID.String(), scope)
}

func runtimePreflight(ctx context.Context, s *runtimeState, request mc.ModelRequest, invocation mc.InvocationID, consumer mc.ConsumerRequest, plan mc.ConsumerDependencies) (*resolutionPreparation, f.Instant, error) {
	var preparation *resolutionPreparation
	var at f.Instant
	locks, err := runtimeCallLocks(request.Consumer.ProjectID, request.CallID, invocation)
	if err != nil {
		return nil, at, err
	}
	locks, err = resolutionUnion(locks, plan.RequiredLocks())
	if err != nil {
		return nil, at, err
	}
	cause, err := runtimeCallCause(request.Consumer.ProjectID, request.CallID)
	if err != nil {
		return nil, at, err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return runtimePortError(err)
		}
		if err := s.authority.validateConsumer(ctx, tx, consumer, plan); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		preparation, err = loadRuntimeSnapshot(ctx, x, request)
		if err != nil {
			return err
		}
		at, err = dbNow(ctx, x)
		return err
	})
	if result.State() != f.Committed {
		return nil, f.Instant{}, commitError(result)
	}
	return preparation, at, nil
}
