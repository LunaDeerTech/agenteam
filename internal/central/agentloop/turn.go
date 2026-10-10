package agentloop

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// Result performs one logical JSON request, with all retries owned by Model.
// Its first ctx must carry the original, live Execution runtime proof. The
// Executor calls it only after the exact RoundInput has known committed.
//
// Following an error, another Result only closes/confirms the retained handle
// and reads its cached outcome. It never calls BeginChat again. Cancellation or
// an unresolved Close leaves the session owned until a later actual join.
func (s *DirectTextSession) Result(ctx context.Context) (mc.ModelResponse, error) {
	var zero mc.ModelResponse
	if s == nil || s.state == nil {
		return zero, loopFault(f.DependencyUnbound)
	}
	run := s.state()
	if err := run.enter(ctx, false); err != nil {
		return zero, err
	}
	defer run.leave()
	run.mu.Lock()
	started, joined, stopping := run.started, run.joined, run.stopping
	if !started && stopping {
		run.joined, run.err = true, context.Canceled
		joined = true
	}
	if !started && !joined && !stopping {
		run.parent = ctx
		run.ctx, run.cancel = context.WithCancel(ctx)
		run.started = true
	}
	run.mu.Unlock()
	if joined && (run.response != nil && run.err == nil || !run.consumed) {
		return run.outcome()
	}
	if stopping {
		return zero, context.Canceled
	}
	if started {
		return run.recoverResult(ctx)
	}
	if err := run.cancellation(); err != nil {
		run.err = err
		run.markJoined()
		return zero, err
	}
	// Marked started before invoking the dependency: even an error or a late
	// return cannot cause a second BeginChat for this accepted session.
	call, err := run.owner.models.BeginChat(run.ctx, run.request.Request())
	if !nilLoopPort(call) {
		run.call = call
	}
	if err != nil || run.call == nil {
		if err == nil {
			err = loopFault(f.InvalidState)
		}
		run.err = loopPortError(err)
		if run.call == nil || run.call.Joined() {
			run.markJoined()
		}
		return zero, run.err
	}
	if err := run.cancellation(); err != nil {
		run.err = err
		return zero, err
	}
	run.consumed = true
	response, err := run.call.Result(run.ctx)
	if err != nil {
		run.err = loopPortError(err)
		if run.call.Joined() {
			run.markJoined()
		}
		// Preserve this original failure, notably its Unknown identity. Recovery
		// is a separate caller action on the retained handle, never an automatic retry.
		return zero, run.err
	}
	run.err = validateTextResponse(run.request.Request().CallID, response)
	if run.err == nil {
		owned := response.Clone()
		run.response = &owned
	}
	closeErr := run.call.Close(run.ctx)
	if run.call.Joined() {
		run.markJoined()
	}
	if closeErr != nil {
		run.err = loopPortError(closeErr)
		return zero, run.err
	}
	if !run.isJoined() {
		run.err = loopFault(f.ResourceBusy)
		return zero, run.err
	}
	if err := run.cancellation(); err != nil {
		run.response, run.err = nil, err
	}
	return run.outcome()
}

func (s *sessionState) recoverResult(ctx context.Context) (mc.ModelResponse, error) {
	var zero mc.ModelResponse
	if s.call == nil {
		return s.outcome()
	}
	wait, done := sessionCleanupContext(s.ctx, ctx)
	defer done()
	closeErr := s.call.Close(wait)
	joined := s.call.Joined()
	if joined {
		s.markJoined()
	}
	if closeErr != nil {
		// A failed cleanup wait says nothing new about the original physical
		// outcome. Until this exact handle joins, retain its Unknown and cause.
		var original *f.Fault
		if !joined && errors.As(s.err, &original) && original.CommitState == f.Unknown {
			return zero, s.err
		}
		s.err = loopPortError(closeErr)
		return zero, s.err
	}
	if !joined {
		return zero, retainedError(s.err, loopFault(f.ResourceBusy))
	}
	if !s.consumed {
		// BeginChat itself failed. Confirmation/retirement cannot authorize a
		// first network dispatch that the original invocation never performed.
		return zero, s.err
	}
	// The JSON contract guarantees that a Joined handle only reads its cache;
	// no new wire invocation, attempt or provider retry is possible here.
	response, err := s.call.Result(wait)
	if err != nil {
		s.response, s.err = nil, loopPortError(err)
		return zero, s.err
	}
	if err := validateTextResponse(s.request.Request().CallID, response); err != nil {
		s.response, s.err = nil, err
		return zero, err
	}
	if err := s.cancellation(); err != nil {
		s.response, s.err = nil, err
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	owned := response.Clone()
	s.response, s.err = &owned, nil
	return owned.Clone(), nil
}

func (s *sessionState) drain(ctx context.Context) error {
	s.stop()
	if err := s.enter(ctx, true); err != nil {
		return err
	}
	defer s.leave()
	if s.isJoined() {
		return nil
	}
	if s.call == nil {
		return loopFault(f.InvalidState)
	}
	wait, done := sessionCleanupContext(s.ctx, ctx)
	defer done()
	err := s.call.Close(wait)
	if s.call.Joined() {
		s.markJoined()
	}
	if err != nil {
		return loopPortError(err)
	}
	if !s.isJoined() {
		return retainedError(s.err, loopFault(f.ResourceBusy))
	}
	return ctx.Err()
}

func (s *sessionState) outcome() (mc.ModelResponse, error) {
	if s.response != nil && s.err == nil && s.isJoined() {
		return s.response.Clone(), nil
	}
	if s.err != nil {
		return mc.ModelResponse{}, s.err
	}
	return mc.ModelResponse{}, loopFault(f.InvalidState)
}

func (s *sessionState) cancellation() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return context.Canceled
	}
	if s.parent != nil {
		return s.parent.Err()
	}
	return nil
}

func (s *sessionState) isJoined() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.joined
}

func (s *sessionState) markJoined() {
	s.mu.Lock()
	s.joined = true
	s.mu.Unlock()
}

// Use the original owner values even after cancellation. Only the explicit
// cleanup caller's cancellation/deadline controls this wait. The tiny cancel
// callback is also joined before returning; no detached cleanup worker exists.
func sessionCleanupContext(original, wait context.Context) (context.Context, func()) {
	base := context.WithoutCancel(original)
	var ctx context.Context
	var cancel context.CancelFunc
	if deadline, ok := wait.Deadline(); ok {
		ctx, cancel = context.WithDeadline(base, deadline)
	} else {
		ctx, cancel = context.WithCancel(base)
	}
	exited := make(chan struct{})
	stop := context.AfterFunc(wait, func() {
		cancel()
		close(exited)
	})
	if wait.Err() != nil {
		cancel()
	}
	return ctx, func() {
		if !stop() {
			<-exited
		}
		cancel()
	}
}

func validateTextResponse(call mc.CallID, response mc.ModelResponse) error {
	if response.Validate() != nil || response.CallID != call {
		return loopFault(f.InvalidState)
	}
	if response.FinishReason != "stop" {
		return unsupported()
	}
	for _, part := range response.Message.Parts {
		if part.Text == nil {
			return unsupported()
		}
	}
	return nil
}

func retainedError(original, fallback error) error {
	if original != nil {
		return original
	}
	return fallback
}

func loopPortError(err error) error {
	if err == nil {
		return nil
	}
	var fault *f.Fault
	if errors.As(err, &fault) {
		return f.NewFault(fault.Code.Safe(), fault.CommitState.Safe()).WithCause(err)
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var modelError *mc.ModelError
	if errors.As(err, &modelError) && modelError.Validate() == nil {
		owned := *modelError
		return &owned
	}
	return loopFault(f.DependencyUnavailable).WithCause(err)
}
