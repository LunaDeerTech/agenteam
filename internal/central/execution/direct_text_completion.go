package execution

import (
	"context"
	"errors"
	"math"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func (s *directTextState) callAndFinish(ctx context.Context, run *directTextCall, recovering bool) (c.DirectTextReceipt, error) {
	var response mc.ModelResponse
	var callErr error
	s.mu.Lock()
	run.phase = "calling"
	s.mu.Unlock()
	if run.ctx.Err() != nil && !recovering {
		callErr = run.ctx.Err()
		run.invocation.session.Stop()
	} else {
		if recovering {
			response, callErr = run.invocation.session.Result(ctx)
		} else {
			// A startup observation may use a fresh wait context, but the first
			// Model call still belongs to the original run's cancellation and
			// private values. Join the temporary cancellation bridge; do not
			// cancel that original context merely because Result was Unknown.
			exited := make(chan struct{})
			stop := context.AfterFunc(ctx, func() { run.cancel(); close(exited) })
			if ctx.Err() != nil {
				run.cancel()
			}
			response, callErr = run.invocation.session.Result(run.ctx)
			if !stop() {
				<-exited
			}
		}
	}
	if callErr != nil && (!recovering && directTextUnknown(callErr) || recovering && !run.invocation.session.Joined()) {
		s.mu.Lock()
		run.uncertainty = "model"
		run.invocation.callError = callErr
		s.mu.Unlock()
		s.retain(run, callErr)
		return s.receipt(run), run.unresolved
	}
	s.mu.Lock()
	run.phase = "closing"
	run.invocation.callError = callErr
	s.mu.Unlock()
	if !run.invocation.session.Joined() {
		wait, cancel := context.WithTimeout(context.WithoutCancel(run.ctx), 3*time.Second)
		drainErr := run.invocation.session.Drain(wait)
		cancel()
		if drainErr != nil || !run.invocation.session.Joined() {
			if drainErr == nil {
				drainErr = fault(f.ResourceBusy)
			}
			s.mu.Lock()
			run.uncertainty = "model"
			s.mu.Unlock()
			s.retain(run, errors.Join(callErr, drainErr))
			return s.receipt(run), run.unresolved
		}
	}
	s.mu.Lock()
	run.allJoined = true
	// The original handle has actually retired. Any next uncertainty belongs
	// to the terminal checkpoint; do not mislabel it with an already observed
	// startup/Model physical attempt. callError still preserves its cause.
	run.unresolved = nil
	run.uncertainty = ""
	s.mu.Unlock()
	if callErr == nil && !stableDirectTextResponse(response, run.round.Fields().CallID) {
		callErr = fault(f.InvalidState)
		run.invocation.callError = callErr
	}
	if callErr == nil {
		owned := response.Clone()
		run.invocation.response = &owned
	}
	return s.finish(run)
}

func (s *directTextState) finish(run *directTextCall) (c.DirectTextReceipt, error) {
	wait, cancel := context.WithTimeout(context.WithoutCancel(run.ctx), 5*time.Second)
	defer cancel()
	if run.terminal == nil {
		if err := s.prepareTerminal(wait, run); err != nil {
			s.mu.Lock()
			run.uncertainty = "closing"
			s.mu.Unlock()
			s.retain(run, err)
			return s.receipt(run), err
		}
	}
	if err := s.planRetirement(wait, run); err != nil {
		s.mu.Lock()
		run.uncertainty = "closing"
		s.mu.Unlock()
		s.retain(run, err)
		return s.receipt(run), err
	}
	err := s.commitTerminal(wait, run)
	if err != nil {
		s.mu.Lock()
		run.uncertainty = "closing"
		if directTextUnknown(err) {
			run.uncertainty = "terminal"
		}
		s.mu.Unlock()
		s.retain(run, err)
		return s.receipt(run), err
	}
	s.mu.Lock()
	run.unresolved = nil
	run.uncertainty = ""
	s.mu.Unlock()
	return s.receipt(run), run.invocation.callError
}

func (s *directTextState) prepareTerminal(ctx context.Context, run *directTextCall) error {
	cause, _ := f.NewRecoveryCause("execution.direct-text.terminal-plan", run.request.ExecutionID.String(), run.snapshot.Fields().StartID.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, run.runtimeLocks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || row.summary.Status != c.Running || !sameDirectText(stored, run.snapshot, run.round) || stored.terminal != nil || row.summary.Version == f.Version(math.MaxInt64) {
			return fault(f.ConfirmationStale)
		}
		at, err := captureTime(ctx, x)
		if err != nil {
			return err
		}
		status, reason, through := c.Succeeded, "completed", f.Sequence(2)
		response := run.invocation.response
		if row.summary.CancelRequestedAt != nil || run.ctx.Err() != nil || errors.Is(run.invocation.callError, context.Canceled) || errors.Is(run.invocation.callError, context.DeadlineExceeded) {
			status, reason, through, response = c.Cancelled, "cancelled", 1, nil
		} else if run.invocation.callError != nil || response == nil {
			status, reason, through, response = c.Failed, "model_failed", 1, nil
		}
		t := &directTextTerminal{status: status, reason: reason, before: row.summary.Version, version: row.summary.Version + 1, through: through, at: at}
		if response != nil {
			owned := response.Clone()
			t.response = &owned
			t.responseBytes, err = directTextJSON(owned)
			if err != nil {
				return err
			}
		}
		t.event, err = s.lifecycleEvent(run, status, reason, t.version, through, at)
		if err != nil {
			return err
		}
		t.eventID = t.event.Header().EventID
		s.mu.Lock()
		run.terminal = t
		run.summary = row.summary.Clone()
		run.phase = "closing"
		run.event = t.event
		s.mu.Unlock()
		return ctx.Err()
	})
	return commitError(result)
}

func (s *directTextState) planRetirement(ctx context.Context, run *directTextCall) error {
	if run.terminal == nil || !run.allJoined {
		return fault(f.Forbidden)
	}
	actor, err := i.NewAgentRun(run.request.Launch.ProjectID, run.request.Launch.AgentID, run.request.ExecutionID)
	if err != nil {
		return err
	}
	v := run.input.input.Fields()
	run.invocation.modelRetirement = mc.ExecutionModelRetirementRequest{Actor: actor, Model: v.Model.Clone(), TerminalVersion: run.terminal.version}
	if nilPort(run.invocation.modelPlan) {
		run.invocation.modelPlan, err = s.deps.Models.DiscoverExecutionModelRetirement(ctx, run.invocation.modelRetirement)
		if err != nil {
			return portError(err)
		}
	}
	if nilPort(run.invocation.environmentPlan) {
		run.invocation.environmentPlan, err = s.deps.Environment.DiscoverEnvironmentRetirement(ctx, v.Environment)
		if err != nil {
			return portError(err)
		}
	}
	if nilPort(run.invocation.modelPlan) || nilPort(run.invocation.environmentPlan) {
		return fault(f.DependencyUnavailable)
	}
	if run.terminal.plan.Details().Event.Validate() != nil {
		run.terminal.plan, err = s.deps.Events.PrepareAppend(ctx, actor, run.terminal.event)
		if err != nil {
			return portError(err)
		}
	}
	locks, err := preparationLocks(run.request)
	if err != nil {
		return err
	}
	locks = append(locks, run.invocation.modelPlan.RequiredLocks()...)
	locks = append(locks, run.invocation.environmentPlan.RequiredLocks()...)
	locks = append(locks, run.terminal.plan.Locks()...)
	run.locks, err = oc.NormalizeLocks(locks)
	return err
}

func (s *directTextState) commitTerminal(ctx context.Context, run *directTextCall) error {
	cause, _ := f.NewJobCause("execution-direct-text-terminal", run.request.ExecutionID.String(), run.snapshot.Fields().StartID.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, run.locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if s.processes.CurrentProcess() != s.process || !run.allJoined || !run.invocation.session.Joined() {
			return fault(f.Forbidden)
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || row.summary.Status != c.Running || row.summary.Version != run.terminal.before || !sameDirectText(stored, run.snapshot, run.round) || stored.terminal != nil {
			return fault(f.ConfirmationStale)
		}
		if run.terminal.status == c.Succeeded && (run.ctx.Err() != nil || row.summary.CancelRequestedAt != nil) {
			return fault(f.ConfirmationStale)
		}
		s.mu.Lock()
		run.phase = "terminal"
		run.tx = tx
		run.retired = false
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			run.tx = f.Tx{}
			if run.phase == "terminal" {
				run.phase = "closing"
			}
			s.mu.Unlock()
		}()
		if err = saveDirectTextTerminal(ctx, x, run); err != nil {
			return err
		}
		if err = s.deps.Models.ReleaseExecutionModelInTx(ctx, tx, run.invocation.modelRetirement, run.invocation.modelPlan); err != nil {
			return portError(err)
		}
		if err = s.deps.Environment.RetireEnvironmentInTx(ctx, tx, run.input.input.Fields().Environment, run.invocation.environmentPlan); err != nil {
			return portError(err)
		}
		s.mu.Lock()
		run.retired = true
		s.mu.Unlock()
		actor, _ := i.NewAgentRun(run.request.Launch.ProjectID, run.request.Launch.AgentID, run.request.ExecutionID)
		if _, err = s.deps.Events.AppendEventInTx(ctx, tx, actor, run.terminal.event, run.terminal.plan); err != nil {
			return portError(err)
		}
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return err
	}
	s.mu.Lock()
	run.summary.Status = run.terminal.status
	run.summary.Version = run.terminal.version
	at := run.terminal.at
	run.summary.CompletedAt = &at
	run.phase = "finished"
	s.mu.Unlock()
	return nil
}

func (a *Authority) CheckEnvironmentRetirementPlan(ctx context.Context, capture pvc.EnvironmentCapture) error {
	if _, err := a.directTextModelPlanningScope(ctx, true); err != nil {
		return err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return err
	}
	return directTextEnvironmentMatches(run, capture)
}
func (a *Authority) CheckEnvironmentRetirementInTx(ctx context.Context, tx f.Tx, capture pvc.EnvironmentCapture) error {
	if _, err := a.directTextRetirementScope(ctx, tx); err != nil {
		return err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return err
	}
	return directTextEnvironmentMatches(run, capture)
}
func directTextEnvironmentMatches(run *directTextCall, capture pvc.EnvironmentCapture) error {
	actual, err := pvc.EnvironmentRetirementBinding(capture)
	if err != nil {
		return err
	}
	expected, err := pvc.EnvironmentRetirementBinding(run.input.input.Fields().Environment)
	if err != nil || actual != expected {
		return fault(f.Forbidden)
	}
	return nil
}

var _ pvc.ExecutionEnvironmentRetirementAuthority = (*Authority)(nil)
