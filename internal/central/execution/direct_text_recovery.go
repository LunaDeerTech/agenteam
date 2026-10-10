package execution

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ResolveUnknown resumes only an original retained owner. Startup and terminal
// uncertainty are read-only observations of exact candidate bytes. A Model
// uncertainty uses the same accepted session/JSON handle; it cannot BeginChat
// again. A known failed cleanup checkpoint may be retried after a fresh read,
// without repeating the Model call. Absence never proves rollback.
func (d *DirectTextDriver) ResolveUnknown(ctx context.Context, execution i.ExecutionID) (receipt c.DirectTextReceipt, err error) {
	if d == nil || d.state == nil {
		return receipt, fault(f.DependencyUnbound)
	}
	if ctx == nil || execution.Validate() != nil {
		return receipt, invalid()
	}
	if err = ctx.Err(); err != nil {
		return receipt, err
	}
	s := d.state
	s.mu.Lock()
	run := s.calls[execution]
	if run == nil || !run.returned || run.resolving || run.unresolved == nil {
		s.mu.Unlock()
		return receipt, fault(f.InvalidState)
	}
	run.resolving = true
	run.returned = false
	stage := run.uncertainty
	original := run.unresolved
	s.mu.Unlock()
	defer s.returned(run)
	wait, done := directTextRecoveryContext(run.ctx, ctx)
	defer done()
	switch stage {
	case "start":
		found, observeErr := s.observeStart(wait, run)
		if observeErr != nil {
			return s.receipt(run), errors.Join(original, observeErr)
		}
		if !found {
			return s.receipt(run), original
		}
		s.mu.Lock()
		run.unresolved = nil
		run.uncertainty = ""
		s.mu.Unlock()
		return s.callAndFinish(wait, run, false)
	case "model":
		return s.callAndFinish(wait, run, true)
	case "terminal":
		found, observeErr := s.observeTerminal(wait, run)
		if observeErr != nil {
			return s.receipt(run), errors.Join(original, observeErr)
		}
		if !found {
			return s.receipt(run), original
		}
		s.mu.Lock()
		run.unresolved = nil
		run.uncertainty = ""
		s.mu.Unlock()
		return s.receipt(run), run.invocation.callError
	case "closing":
		// No uncertain writer exists on this branch: either a read-only plan
		// failed, or the original terminal transaction reported NotCommitted.
		// Replan from canonical current facts (including cancellation/version).
		s.mu.Lock()
		run.phase = "closing"
		run.terminal = nil
		run.invocation.modelPlan = nil
		run.invocation.environmentPlan = nil
		s.mu.Unlock()
		return s.finish(run)
	default:
		return s.receipt(run), original
	}
}

func (s *directTextState) observeStart(ctx context.Context, run *directTextCall) (bool, error) {
	if run.snapshot.Validate() != nil || run.invocation.session == nil {
		return false, fault(f.InvalidState)
	}
	cause, _ := f.NewRecoveryCause("execution.direct-text.start", run.request.ExecutionID.String(), run.snapshot.Fields().StartID.String())
	var found bool
	var observed c.Summary
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, run.locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if s.processes.CurrentProcess() != s.process {
			return fault(f.InvalidState)
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || stored == nil {
			return nil
		}
		if row.summary.Status != c.Running || row.summary.SnapshotID == nil || *row.summary.SnapshotID != run.snapshot.Fields().ID || !sameDirectText(stored, run.snapshot, run.round) || stored.startedVersion != run.summary.Version+1 || stored.startedEvent != run.invocation.startPlan.Details().Event.Header().EventID || stored.terminal != nil || row.summary.Version < stored.startedVersion {
			return nil
		}
		input, err := loadPreparationInput(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if !samePreparationInput(input, run.input) {
			return nil
		}
		found = true
		observed = row.summary.Clone()
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return false, err
	}
	if found {
		s.mu.Lock()
		run.started = true
		run.phase = "calling"
		run.summary = observed
		s.mu.Unlock()
	}
	return found, nil
}

func (s *directTextState) observeTerminal(ctx context.Context, run *directTextCall) (bool, error) {
	if run.terminal == nil || !run.allJoined || !run.invocation.session.Joined() {
		return false, fault(f.InvalidState)
	}
	cause, _ := f.NewRecoveryCause("execution.direct-text.terminal", run.request.ExecutionID.String(), run.snapshot.Fields().StartID.String())
	var found bool
	var observed c.Summary
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, run.locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if s.processes.CurrentProcess() != s.process {
			return fault(f.InvalidState)
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if !sameDirectText(stored, run.snapshot, run.round) || !directTextTerminalMatches(row, stored, run.terminal) {
			return nil
		}
		s.mu.Lock()
		run.phase = "terminal"
		run.tx = tx
		s.mu.Unlock()
		defer func() { s.mu.Lock(); run.tx = f.Tx{}; run.phase = "closing"; s.mu.Unlock() }()
		modelRetired, err := s.deps.Models.ExecutionModelRetiredInTx(ctx, tx, run.invocation.modelRetirement, run.invocation.modelPlan)
		if err != nil {
			return portError(err)
		}
		environmentRetired, err := s.deps.Environment.EnvironmentRetiredInTx(ctx, tx, run.input.input.Fields().Environment, run.invocation.environmentPlan)
		if err != nil {
			return portError(err)
		}
		if !modelRetired || !environmentRetired {
			return nil
		}
		found = true
		observed = row.summary.Clone()
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return false, err
	}
	if found {
		s.mu.Lock()
		run.summary = observed
		run.phase = "finished"
		s.mu.Unlock()
	}
	return found, nil
}

// Retain the original private values while waiting with the recovery caller's
// cancellation. Startup also checks run.ctx.Err before any first Model call;
// removing cancellation here authorizes only observation/owned cleanup.
func directTextRecoveryContext(original, wait context.Context) (context.Context, func()) {
	base := context.WithoutCancel(original)
	var ctx context.Context
	var cancel context.CancelFunc
	if deadline, ok := wait.Deadline(); ok {
		ctx, cancel = context.WithDeadline(base, deadline)
	} else {
		ctx, cancel = context.WithCancel(base)
	}
	exited := make(chan struct{})
	stop := context.AfterFunc(wait, func() { cancel(); close(exited) })
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
