package execution

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Recovery belongs to the original Associated traversal. It creates neither a
// second worker nor a replacement preparation, Model request or Tool operation.
// The retained candidate is needed only while its own terminal commit is
// unknown; subsequent attempts observe it and never replay that writer.
type executionRecoveryCall struct {
	owner           *associatedExecutorState
	job             *associatedCall
	request         c.PreparationRequest
	before          c.Summary
	claim           *preparationClaim
	input           *preparationInputRecord
	model           mc.ExecutionModelRetirementRequest
	modelPlan       mc.ExecutionModelRetirementPlan
	environmentPlan pvc.EnvironmentRetirementPlan
	locks           []f.LockRequest
	event           event.Event
	plan            oc.AppendPlan
	at              f.Instant
	tx              f.Tx
	active          bool
	written         bool
	retired         bool
	unknown         error
}

type executionRecoveryContextKey struct{}

func (s *associatedExecutorState) recoverCancelledPreparation(ctx context.Context, job *associatedCall) (summary c.Summary, handled bool, err error) {
	if s == nil || s.preparation == nil || s.preparation.state == nil || s.direct == nil || s.direct.state == nil {
		return summary, false, nil
	}
	if ctx == nil || job == nil || ctx.Value(associatedContextKey{}) != job {
		return summary, true, fault(f.Forbidden)
	}
	s.mu.Lock()
	valid := s.jobs[job.dispatch.ExecutionID] == job && job.active
	w := job.recovery
	s.mu.Unlock()
	if !valid {
		return summary, true, fault(f.Forbidden)
	}
	if w != nil && w.unknown != nil {
		return s.observePreparationCancellation(ctx, w)
	}
	store := s.preparation.state.store
	row, err := loadExecution(ctx, store, job.dispatch.ExecutionID)
	if err != nil {
		return summary, true, err
	}
	if row == nil {
		return summary, true, fault(f.NotFound)
	}
	if row.summary.CancelRequestedAt == nil || (row.summary.Status != c.Created && row.summary.Status != c.Preparing) {
		return row.summary.Clone(), false, nil
	}
	if row.summary.ProjectID != job.project || row.summary.AgentID != job.dispatch.AgentID || row.digest != job.dispatch.Digest || row.launch.Lineage.DispatchID != job.dispatch.DispatchID || row.launch.Meta.IdempotencyKey != job.dispatch.Key {
		return summary, true, fault(f.ConfirmationStale)
	}
	claim, err := loadPreparationClaim(ctx, store, row.summary.ID)
	if err != nil {
		return summary, true, err
	}
	if err = s.confirmPreparationReturned(ctx, row, claim); err != nil {
		return row.summary.Clone(), true, err
	}
	w = &executionRecoveryCall{owner: s, job: job, request: c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch.Clone()}, before: row.summary.Clone(), claim: claim}
	w.locks, err = preparationLocks(w.request)
	if err != nil {
		return summary, true, err
	}
	s.mu.Lock()
	job.recovery, w.active = w, true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		w.active = false
		if w.unknown == nil && job.recovery == w {
			job.recovery = nil
		}
		s.mu.Unlock()
	}()
	ctx = context.WithValue(ctx, executionRecoveryContextKey{}, w)
	if err = s.planPreparationCancellation(ctx, w); err != nil {
		return w.before.Clone(), true, err
	}
	actor, _ := i.NewAgentRun(w.request.Launch.ProjectID, w.request.Launch.AgentID, w.request.ExecutionID)
	if w.input != nil {
		if err = s.planPreparationRetirement(ctx, w, actor); err != nil {
			return w.before.Clone(), true, err
		}
	}
	w.plan, err = s.direct.state.deps.Events.PrepareAppend(ctx, actor, w.event)
	if err != nil {
		return w.before.Clone(), true, portError(err)
	}
	w.locks, err = oc.NormalizeLocks(append(w.locks, w.plan.Locks()...))
	if err != nil {
		return w.before.Clone(), true, portError(err)
	}
	result := s.commitPreparationCancellation(ctx, w, actor)
	if err = commitError(result); err != nil {
		if result.State() == f.Unknown {
			s.mu.Lock()
			w.unknown = err
			s.mu.Unlock()
		}
		return w.before.Clone(), true, err
	}
	return w.cancelledSummary(), true, nil
}

// Absence of an input is meaningful only after this original owner's callback
// has returned or ProcessGuard proved its process dead, and the following
// transaction has acquired the capture's complete Execution lock barrier.
func (s *associatedExecutorState) confirmPreparationReturned(ctx context.Context, row *executionRecord, claim *preparationClaim) error {
	if owners := s.ownedCalls(&associatedCall{dispatch: c.AssociatedDispatch{ExecutionID: row.summary.ID}}); owners.preparation || owners.direct || owners.foreign {
		return fault(f.ResourceBusy)
	}
	if row.summary.Status == c.Created {
		if claim != nil {
			return fault(f.ConfirmationStale)
		}
		return ctx.Err()
	}
	if claim == nil || claim.execution != row.summary.ID || claim.project != row.summary.ProjectID || claim.agent != row.summary.AgentID {
		return fault(f.ConfirmationStale)
	}
	p := s.preparation.state
	if claim.process != p.process {
		return portError(p.processes.ConfirmStopped(ctx, claim.process))
	}
	if claim.phase == "terminal" {
		return ctx.Err()
	}
	p.mu.Lock()
	returned, ok := p.returned[row.summary.ID]
	p.mu.Unlock()
	if !ok || returned != *claim {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

func (w *executionRecoveryCall) cancelledSummary() c.Summary {
	summary := w.before.Clone()
	summary.Status, summary.Version = c.Cancelled, summary.Version+1
	at := w.at
	summary.CompletedAt = &at
	return summary
}

func (w *executionRecoveryCall) require(ctx context.Context) error {
	if ctx == nil || w == nil || w.owner == nil || w.job == nil || ctx.Value(executionRecoveryContextKey{}) != w || ctx.Value(associatedContextKey{}) != w.job {
		return fault(f.Forbidden)
	}
	s := w.owner
	s.mu.Lock()
	valid := w.active && w.job.active && w.job.recovery == w && s.jobs[w.request.ExecutionID] == w.job
	s.mu.Unlock()
	if !valid {
		return fault(f.Forbidden)
	}
	if s.preparation.state.processes.CurrentProcess() != s.preparation.state.process || s.direct.state.processes.CurrentProcess() != s.direct.state.process {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}

func (a *DirectTextEventAuthority) recoveryEventOwner(ctx context.Context, actor i.Actor, summary event.Summary) (*executionRecoveryCall, error) {
	w, ok := ctx.Value(executionRecoveryContextKey{}).(*executionRecoveryCall)
	if !ok || w == nil || w.owner == nil || a == nil || a.authority == nil || w.owner.direct.state.authority != a.authority {
		return nil, fault(f.Forbidden)
	}
	if err := w.require(ctx); err != nil {
		return nil, err
	}
	expected, err := i.NewAgentRun(w.request.Launch.ProjectID, w.request.Launch.AgentID, w.request.ExecutionID)
	if err != nil || !actor.Equal(expected) || w.event.Validate() != nil || !reflect.DeepEqual(w.event.Summary(), summary) {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (a *DirectTextEventAuthority) discoverRecoveryAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	w, err := a.recoveryEventOwner(ctx, actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.issuer, directTextEventBinding(actor, summary), w.locks, []byte("execution.before-start.cancel.v1"))
}

func (a *DirectTextEventAuthority) validateRecoveryAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	w, err := a.recoveryEventOwner(ctx, actor, summary)
	if err != nil {
		return err
	}
	if !stage.Valid() || !deps.Matches(a.issuer, directTextEventBinding(actor, summary)) || string(deps.Opaque()) != "execution.before-start.cancel.v1" || w.tx != tx || !w.written || !w.retired {
		return fault(f.Forbidden)
	}
	store := w.owner.preparation.state.store
	if err = store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	if err = requireProjectCallInTx(ctx, tx, store); err != nil {
		return err
	}
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadExecution(ctx, x, w.request.ExecutionID)
	if err != nil {
		return err
	}
	if !preparationCancellationMatches(row, w) {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (s *associatedExecutorState) planPreparationRetirement(ctx context.Context, w *executionRecoveryCall, actor i.Actor) (err error) {
	deps := s.direct.state.deps
	if nilPort(deps.Models) || nilPort(deps.Environment) {
		return fault(f.DependencyUnbound)
	}
	v := w.input.input.Fields()
	w.model = mc.ExecutionModelRetirementRequest{Actor: actor, Model: v.Model.Clone(), TerminalVersion: w.before.Version + 1}
	w.modelPlan, err = deps.Models.DiscoverExecutionModelRetirement(ctx, w.model)
	if err != nil {
		return portError(err)
	}
	w.environmentPlan, err = deps.Environment.DiscoverEnvironmentRetirement(ctx, v.Environment)
	if err != nil {
		return portError(err)
	}
	if nilPort(w.modelPlan) || nilPort(w.environmentPlan) {
		return fault(f.DependencyUnbound)
	}
	w.locks, err = oc.NormalizeLocks(append(append(w.locks, w.modelPlan.RequiredLocks()...), w.environmentPlan.RequiredLocks()...))
	return portError(err)
}

func (a *Authority) preparationRetirementOwner(ctx context.Context) (*executionRecoveryCall, error) {
	if a == nil || a.state == nil || ctx == nil {
		return nil, fault(f.DependencyUnbound)
	}
	w, ok := ctx.Value(executionRecoveryContextKey{}).(*executionRecoveryCall)
	if !ok || w == nil || w.owner == nil || w.owner.preparation.state.authority != a || w.input == nil || w.input.input.Validate() != nil || w.before.Status != c.Preparing {
		return nil, fault(f.Forbidden)
	}
	if err := w.require(ctx); err != nil {
		return nil, err
	}
	return w, nil
}

func (a *Authority) discoverPreparationRetirement(ctx context.Context, request mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	w, err := a.preparationRetirementOwner(ctx)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := preparationRetirementBinding(w, request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.state.modelIssuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: w.input.input.Digest(), Locks: w.locks})
}

func preparationRetirementBinding(w *executionRecoveryCall, request mc.ConsumerRequest) (f.Digest, error) {
	if request.Action != mc.RetireConsumer {
		return "", fault(f.Forbidden)
	}
	actual, err := mc.ConsumerBinding(request)
	expected, expectedErr := mc.ConsumerBinding(w.model.ConsumerRequest())
	if err != nil || expectedErr != nil || actual != expected {
		return "", fault(f.Forbidden)
	}
	return actual, nil
}

func (a *Authority) validatePreparationRetirementInTx(ctx context.Context, tx f.Tx, request mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	w, err := a.preparationRetirementOwner(ctx)
	if err != nil {
		return err
	}
	binding, err := preparationRetirementBinding(w, request)
	if err != nil || !plan.Matches(a.state.modelIssuer, binding, w.input.input.Digest()) {
		return fault(f.Forbidden)
	}
	return a.requirePreparationRetirementInTx(ctx, tx, w)
}

func (a *Authority) requirePreparationRetirementInTx(ctx context.Context, tx f.Tx, w *executionRecoveryCall) error {
	if w.tx != tx || !w.written {
		return fault(f.Forbidden)
	}
	store := w.owner.preparation.state.store
	if err := store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	if err := requireProjectCallInTx(ctx, tx, store); err != nil {
		return err
	}
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadExecution(ctx, x, w.request.ExecutionID)
	if err != nil {
		return err
	}
	input, err := loadPreparationInput(ctx, x, w.request.ExecutionID)
	if err != nil {
		return err
	}
	if !preparationCancellationMatches(row, w) || !samePreparationInput(input, w.input) {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (a *Authority) checkPreparationEnvironmentRetirementPlan(ctx context.Context, capture pvc.EnvironmentCapture) error {
	w, err := a.preparationRetirementOwner(ctx)
	if err != nil {
		return err
	}
	actual, err := pvc.EnvironmentRetirementBinding(capture)
	expected, otherErr := pvc.EnvironmentRetirementBinding(w.input.input.Fields().Environment)
	if err != nil || otherErr != nil || actual != expected {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

func (a *Authority) checkPreparationEnvironmentRetirementInTx(ctx context.Context, tx f.Tx, capture pvc.EnvironmentCapture) error {
	if err := a.checkPreparationEnvironmentRetirementPlan(ctx, capture); err != nil {
		return err
	}
	w, err := a.preparationRetirementOwner(ctx)
	if err != nil {
		return err
	}
	return a.requirePreparationRetirementInTx(ctx, tx, w)
}

func (s *associatedExecutorState) observePreparationCancellation(ctx context.Context, w *executionRecoveryCall) (c.Summary, bool, error) {
	s.mu.Lock()
	w.active = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); w.active = false; s.mu.Unlock() }()
	ctx = context.WithValue(ctx, executionRecoveryContextKey{}, w)
	found, err := s.readPreparationCancellation(ctx, w)
	if err != nil || !found {
		return w.before.Clone(), true, errors.Join(w.unknown, err)
	}
	s.mu.Lock()
	w.unknown = nil
	w.job.recovery = nil
	s.mu.Unlock()
	return w.cancelledSummary(), true, nil
}

// A ToolCall checkpoint is written only after its exact Model session joined.
// ProcessGuard death and the original ProjectCall ledger additionally exclude
// every old borrower before this private proof can replace a lost Go session.
// It is never valid for an in-flight Model request or a missing checkpoint.
type executionColdCheckpoint struct {
	owner    *directTextState
	run      *directTextCall
	snapshot f.Digest
	round    f.Digest
	call     projectCallRecord
	owners   []projectCallRecord
	verified bool
}

func joinedLoopCheckpoint(run *directTextCall) bool {
	if run == nil || run.invocation == nil || !run.allJoined {
		return false
	}
	if run.invocation.session != nil {
		return run.invocation.session.Joined()
	}
	w := run.cold
	return w != nil && w.verified && w.owner == run.owner && w.run == run && run.started && run.snapshot.Validate() == nil && !nilPort(run.round) && run.round.Validate() == nil && w.snapshot == run.snapshot.Digest() && w.round == run.round.Digest() && run.toolPending && run.toolFacts != nil
}

func (s *directTextState) restoreLoopCheckpoint(ctx context.Context, run *directTextCall, row *executionRecord, stored *directTextRecord) (c.DirectTextReceipt, error) {
	if s.multi == nil || s.projectCalls == nil || row == nil || stored == nil || stored.toolPending == nil || stored.terminal != nil || !isLoopRound(stored.round) || (row.summary.Status != c.Waiting && row.summary.Status != c.Running) {
		return s.receipt(run), fault(f.ResourceBusy)
	}
	if stored.snapshot.Fields().ProcessID == s.process {
		// Releasing a waiting runtime within a live process is a separate
		// lifecycle action; losing its pointer cannot prove it has joined.
		return s.receipt(run), fault(f.ResourceBusy)
	}
	witness, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || witness.state != s.projectCalls || witness.entry == nil || witness.entry.execution != row.summary.ID || witness.entry.record.Lane != projectLaneDirect {
		return s.receipt(run), fault(f.Forbidden)
	}
	owners, err := readRecoveryCallOwners(ctx, s.store, witness.entry.record)
	if err != nil {
		return s.receipt(run), err
	}
	if err = s.processes.ConfirmStopped(ctx, stored.snapshot.Fields().ProcessID); err != nil {
		return s.receipt(run), portError(err)
	}
	if err = s.confirmRecoveryCallOwners(ctx, witness.entry.record, owners); err != nil {
		return s.receipt(run), err
	}
	locks, err := preparationLocks(run.request)
	if err != nil {
		return s.receipt(run), err
	}
	locks, err = oc.NormalizeLocks(append(locks, projectCallLocks(run.request.Launch.ProjectID)...))
	if err != nil {
		return s.receipt(run), portError(err)
	}
	runtimeLocks, err := directTextRuntimeLocks(run.request)
	if err != nil {
		return s.receipt(run), err
	}
	s.mu.Lock()
	run.snapshot, run.round, run.summary = stored.snapshot, stored.round, row.summary.Clone()
	run.locks, run.runtimeLocks = locks, runtimeLocks
	run.cold = &executionColdCheckpoint{owner: s, run: run, snapshot: stored.snapshot.Digest(), round: stored.round.Digest(), call: witness.entry.record, owners: owners}
	s.mu.Unlock()
	return s.recoverLoopCheckpoint(ctx, run)
}

func (s *directTextState) confirmRecoveryCallOwners(ctx context.Context, current projectCallRecord, owners []projectCallRecord) error {
	found := false
	confirmed := map[string]bool{}
	for _, old := range owners {
		if old.ID == current.ID {
			if !old.sameOwner(current) || old.Phase != "running" {
				return fault(f.ConfirmationStale)
			}
			found = true
			continue
		}
		if old.Process == s.projectCalls.process {
			if !s.recoveryLocalCallReturned(old) {
				return fault(f.ResourceBusy)
			}
			continue
		}
		if !confirmed[old.Process.String()] {
			if err := s.projectCalls.proof.ConfirmStopped(ctx, old.Process); err != nil {
				return portError(err)
			}
			confirmed[old.Process.String()] = true
		}
	}
	if !found {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (s *directTextState) recoveryLocalCallReturned(old projectCallRecord) bool {
	p := s.projectCalls
	p.mu.Lock()
	defer p.mu.Unlock()
	// Registry retirement happens after the original complete call returned.
	// A closed database phase alone is deliberately insufficient.
	return old.Process == p.process && old.Instance == p.instance && old.Phase == "business_closed" && p.owners[old.ID] == nil
}

func (s *directTextState) recoverLoopCheckpoint(ctx context.Context, run *directTextCall) (c.DirectTextReceipt, error) {
	if run == nil {
		return c.DirectTextReceipt{}, fault(f.Forbidden)
	}
	if run.cold == nil || run.cold.owner != s || run.cold.run != run {
		return s.receipt(run), fault(f.Forbidden)
	}
	if err := s.readColdLoopCheckpoint(ctx, run); err != nil {
		return s.loopFailure(run, "cold-checkpoint", err)
	}
	mr, err := loopModelRequest(ctx, run.snapshot, run.round)
	if err != nil {
		return s.loopFailure(run, "cold-checkpoint", err)
	}
	request, err := agentloop.NewLoopRequest(ctx, run.round.Fields().ContextDigest, mr)
	if err != nil {
		return s.loopFailure(run, "cold-checkpoint", portError(err))
	}
	s.mu.Lock()
	run.invocation = &directTextInvocation{request: request}
	run.started, run.allJoined, run.toolPending = true, true, true
	run.cold.verified = true
	// Running may already have dispatched an unsafe operation. Restoration
	// can only ask Lookup about that original operation, including absence.
	run.toolDispatched = run.summary.Status == c.Running
	run.phase, run.uncertainty, run.unresolved = "tool-pending", "", nil
	cancelled := run.summary.CancelRequestedAt != nil
	s.mu.Unlock()
	if cancelled {
		run.cancel()
		wait, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return s.advanceLoop(wait, run)
	}
	return s.advanceLoop(ctx, run)
}

func validColdWaiting(row *executionRecord, stored *directTextRecord, reference *string) error {
	if row.summary.Status != c.Waiting {
		if reference != nil {
			return fault(f.ConfirmationStale)
		}
		return nil
	}
	if reference == nil {
		return fault(f.ConfirmationStale)
	}
	if _, err := f.ParseID[struct{}](*reference); err != nil {
		return fault(f.ConfirmationStale)
	}
	binding, err := toolBinding(stored)
	if err != nil || binding.RunnerBinding != tc.RunCommandRegistration().Binding {
		return fault(f.ConfirmationStale)
	}
	return nil
}
