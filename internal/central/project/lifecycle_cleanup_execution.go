package project

import (
	"context"
	"errors"
	"math"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// An original round owns registration, provider and checkpoint uncertainty.
// The small accepted checkpoint is not a public proof of the provider's return.
type executionCleanupRound struct {
	round                                    lifecycleCleanupRound
	plan                                     ec.ProjectExecutionRetirementPlan
	appliedCheckpoint                        *c.CleanupCheckpoint
	acceptedCheckpoint                       *c.CleanupCheckpoint
	registrationUnknown                      *f.CommitResult
	unknown                                  *f.CommitResult
	registered, restored, accepted, complete bool
	mutationAttempt                          bool
	priorAttempts                            int64
}

func NewLifecycleCleanupDriverForExecutions(store Store, a *LifecycleAuthority, processes oc.ProcessAuthority, consumer ec.ProjectExecutionRetirementConsumer) (*LifecycleCleanupDriver, error) {
	if nilPort(store) || !a.bound() || !sameStore(store, a.store) || nilPort(processes) || nilPort(consumer) {
		return nil, fault(f.DependencyUnbound)
	}
	process := processes.CurrentProcess()
	if process.Validate() != nil || consumer.ProjectExecutionRetirementIdentity().Validate() != nil {
		return nil, invalid()
	}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		return nil, portError(err)
	}
	return &LifecycleCleanupDriver{state: &lifecycleCleanupState{store: store, authority: a, processes: processes, process: process, executions: consumer, registration: registration, calls: map[c.ProjectID]context.CancelFunc{}, owners: map[c.OperationID]lifecycleCleanupOwner{}, changed: make(chan struct{})}}, nil
}

func (st *lifecycleCleanupState) runExecutionRetirement(ctx context.Context, p c.ProjectID, op c.OperationID, e *executionCleanupRound) (*executionCleanupRound, bool, error) {
	if e != nil && e.registrationUnknown != nil {
		err := st.confirmExecutionRegistration(ctx, e)
		return e, false, err
	}
	if e == nil || e.accepted {
		var accepted *c.CleanupCheckpoint
		if e != nil {
			accepted = e.acceptedCheckpoint
			st.authority.releaseExecutionCleanup(e)
		}
		var err error
		prepared, err := st.prepareExecutionRound(ctx, p, op, accepted)
		if err != nil {
			return e, false, err
		}
		e = prepared
	}
	if !e.registered && !e.restored {
		result, validated := st.registerExecutionRound(ctx, e)
		if result.State() == f.Unknown {
			if !executionRetirementUnknown(result) {
				return e, false, unavailable(nil)
			}
			saved := result
			e.registrationUnknown = &saved
			return e, false, commitError(result)
		}
		if err := commitError(result); err != nil {
			st.authority.releaseExecutionCleanup(e)
			if e.acceptedCheckpoint != nil {
				e.accepted = true
				return e, false, err
			}
			return nil, false, err
		}
		if !validated {
			return e, false, unavailable(nil)
		}
		e.registered = true
	}
	scope, err := i.InProject(p)
	if err != nil {
		return e, false, invalid()
	}
	actor, err := st.registration.Actor(op.String(), scope)
	if err != nil {
		return e, false, portError(err)
	}
	target := c.ScopeRef{Kind: c.ProjectScope, ProjectID: p}
	run, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	witness := &executionRetirementScope{state: st, round: e, open: true, confirmation: e.unknown != nil || e.restored}
	run = context.WithValue(run, executionRetirementScopeKey{}, witness)
	defer func() { witness.mu.Lock(); witness.open = false; witness.mu.Unlock() }()
	confirmed, applied, mutationReturned := false, false, false
	result, callErr := st.executions.WithProjectExecutionRetirement(run, actor, e.round.cause, target, func(call context.Context, confirmation bool) (f.CommitResult, error) {
		witness.mu.Lock()
		witness.confirmation = confirmation || e.unknown != nil || e.restored
		confirm := witness.confirmation
		witness.mu.Unlock()
		if e.restored {
			cp, err := decodeExecutionRetirementCheckpoint(e.appliedCheckpoint, e.round.cause, p)
			if err != nil {
				return executionRetirementRejected(err), err
			}
			if cp.Owner != st.executions.ProjectExecutionRetirementIdentity() {
				err := fault(f.ResourceBusy)
				return executionRetirementRejected(err), err
			}
			if cp.Stage == "applied" {
				e.plan, err = st.executions.RestoreProjectExecutionRetirement(call, actor, e.round.cause, target, *cp.Binding, cp.Commit, cp.Owner)
				if err != nil {
					return executionRetirementRejected(err), err
				}
			}
		}
		// A discovery Unknown is read-only. Repeating discovery can settle only
		// that original read; it never authorizes retirement in this same Run.
		discoveryConfirmation := confirm && !e.mutationAttempt && !e.restored
		if !confirm || discoveryConfirmation {
			var err error
			e.plan, err = st.executions.DiscoverProjectExecutionRetirement(call, actor, e.round.cause, target)
			if err != nil {
				return executionRetirementRejected(err), err
			}
		}
		validated := false
		if !confirm {
			e.mutationAttempt = true
			e.appliedCheckpoint = nil
		}
		txResult := st.store.WithinTx(call, e.round.claim.cause(), func(txctx context.Context, tx f.Tx) error {
			locks := []f.LockRequest{projectLock(p, f.Exclusive), schedulerLock(p)}
			if e.plan != nil {
				locks = append(locks, e.plan.RequiredLocks()...)
			}
			if err := st.store.AcquireAll(txctx, tx, locks); err != nil {
				return portError(err)
			}
			if err := st.authority.validateExecutionRetirementInTx(txctx, tx, p, e.round.cause); err != nil {
				return err
			}
			if confirm {
				if discoveryConfirmation {
					confirmed = true
					validated = true
					return txctx.Err()
				}
				var err error
				if e.restored && e.plan == nil {
					err = st.executions.RequireProjectExecutionsEmptyInTx(txctx, tx, actor, e.round.cause, target)
					applied = err == nil
				} else {
					applied, err = st.executions.ConfirmProjectExecutionRetirementInTx(txctx, tx, e.plan)
				}
				if err != nil {
					return err
				}
				x, err := st.store.InTx(tx)
				if err != nil {
					return portError(err)
				}
				_, _, actual, err := cleanupParticipantRow(txctx, x, e.round)
				if err != nil {
					return err
				}
				after := e.appliedCheckpoint != nil && sameExecutionCheckpoint(actual, e.appliedCheckpoint)
				before := sameExecutionCheckpoint(actual, e.round.checkpoint)
				if e.plan == nil && !e.restored && before {
					applied = false
				}
				if applied && !after || !applied && !before {
					return fault(f.ResourceBusy)
				}
				confirmed = true
				validated = true
				return txctx.Err()
			}
			var binding *ec.ProjectExecutionRetirementBinding
			stage := "empty"
			if e.plan != nil {
				req := e.plan.Request()
				if req.Validate() != nil || !req.Actor.Equal(actor) || req.Cause != e.round.cause || req.Binding.ProjectID != p {
					return fault(f.Forbidden)
				}
				b := req.Binding
				binding = &b
				stage = "applied"
			}
			authority := ExecutionProjectRetirementAuthority{st.authority}
			commit, err := authority.ProjectExecutionRetirementCommitInTx(txctx, tx, actor, e.round.cause, target)
			if err != nil {
				return err
			}
			report, err := executionRetirementReport(e.round, executionRetirementCheckpoint{Stage: stage, Binding: binding, Commit: commit, Owner: st.executions.ProjectExecutionRetirementIdentity()})
			if err != nil {
				return err
			}
			e.appliedCheckpoint = report.Details().Checkpoint
			if e.plan != nil {
				if err := st.executions.RetireProjectExecutionInTx(txctx, tx, e.plan); err != nil {
					return err
				}
			} else {
				if err := st.executions.RetireProjectExecutionCallsInTx(txctx, tx, actor, e.round.cause, target); err != nil {
					return err
				}
				if err := st.executions.RequireProjectExecutionsEmptyInTx(txctx, tx, actor, e.round.cause, target); err != nil {
					return err
				}
			}
			if err = st.finishInTx(txctx, tx, e.round, report, nil); err != nil {
				return err
			}
			e.complete = stage == "empty"
			validated = true
			return txctx.Err()
		})
		if err := commitError(txResult); err != nil {
			return txResult, err
		}
		if !validated {
			return txResult, unavailable(nil)
		}
		mutationReturned = !confirm
		return txResult, portError(call.Err())
	})
	if result.State() == f.Unknown {
		if !executionRetirementUnknown(result) {
			if callErr != nil {
				return e, false, callErr
			}
			return e, false, unavailable(nil)
		}
		if e.unknown == nil {
			saved := result
			e.unknown = &saved
		}
		return e, false, commitError(*e.unknown)
	}
	if callErr != nil {
		if e.unknown != nil {
			return e, false, lifecycleStopRoundError(callErr, commitError(*e.unknown))
		}
		if result.State() == f.Committed && mutationReturned {
			e.accepted = true
			e.acceptedCheckpoint = e.appliedCheckpoint
			st.authority.releaseExecutionCleanup(e)
		}
		return e, mutationReturned && e.complete, callErr
	}
	if err = commitError(result); err != nil {
		return e, false, err
	}
	if confirmed {
		e.unknown = nil
		if applied {
			e.accepted = true
			e.acceptedCheckpoint = e.appliedCheckpoint
			st.authority.releaseExecutionCleanup(e)
		}
		if e.restored {
			e.complete = e.plan == nil
		}
		// Confirmation does not write. Even NotApplied resumes on a later Run.
		if !applied {
			e.appliedCheckpoint = nil
			e.plan = nil
			e.restored = false
			e.mutationAttempt = false
		}
		return e, applied && e.complete, nil
	}
	e.accepted = true
	e.acceptedCheckpoint = e.appliedCheckpoint
	st.authority.releaseExecutionCleanup(e)
	return e, e.complete, nil
}

func executionRetirementUnknown(result f.CommitResult) bool {
	return result.State() == f.Unknown && result.AttemptID().Validate() == nil && result.Cause().Validate() == nil
}

func executionRetirementRejected(err error) f.CommitResult {
	var original *f.Fault
	if errors.As(err, &original) {
		return f.NotCommittedResult(original)
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
}

func (st *lifecycleCleanupState) prepareExecutionRound(ctx context.Context, p c.ProjectID, op c.OperationID, accepted *c.CleanupCheckpoint) (*executionCleanupRound, error) {
	prior, err := loadLifecycleStopClaim(ctx, st.store, op)
	if err != nil {
		return nil, err
	}
	if prior != nil && prior.project != p {
		return nil, fault(f.Forbidden)
	}
	if err = st.canJoin(ctx, prior); err != nil {
		return nil, err
	}
	attempt, err := f.NewID[struct{}]()
	if err != nil {
		return nil, unavailable(err)
	}
	e := &executionCleanupRound{round: lifecycleCleanupRound{claim: lifecycleStopClaim{project: p, operation: op, process: st.process, attempt: attempt.String(), fence: 1, phase: "running"}, previous: prior, participant: c.ExecutionParticipant}, acceptedCheckpoint: accepted}
	if prior != nil {
		if prior.fence == math.MaxInt64 {
			return nil, fault(f.InvalidState)
		}
		e.round.claim.fence = prior.fence + 1
	}
	valid := false
	result := st.store.WithinTx(ctx, e.round.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(p, f.Exclusive), schedulerLock(p)}); err != nil {
			return portError(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, p)
		if err != nil {
			return err
		}
		cause, r, name, _, err := st.current(ctx, x, p, op)
		if err != nil {
			return err
		}
		if name != c.ExecutionParticipant {
			return fault(f.Forbidden)
		}
		actual, err := loadLifecycleStopClaim(ctx, x, op)
		if err != nil {
			return err
		}
		if !sameExecutionClaim(actual, prior) {
			return fault(f.ResourceBusy)
		}
		e.round.cause = cause
		version, attempts, cp, err := cleanupParticipantRow(ctx, x, e.round)
		if err != nil {
			return err
		}
		if version == math.MaxInt64 || attempts == math.MaxInt64 {
			return fault(f.InvalidState)
		}
		e.round.operationVersion = r.operation.Version
		e.round.rowVersion = version + 1
		e.round.checkpoint = cp
		e.priorAttempts = attempts
		if cp != nil && !sameExecutionCheckpoint(cp, accepted) {
			if prior == nil || prior.phase != "terminal" {
				return fault(f.ResourceBusy)
			}
			proof, err := decodeExecutionRetirementCheckpoint(cp, cause, p)
			if err != nil {
				return err
			}
			if proof.Commit.ProcessID != prior.process.String() || proof.Commit.AttemptID != prior.attempt || proof.Commit.Fence != prior.fence {
				return fault(f.ResourceBusy)
			}
			e.round.claim = *prior
			e.round.rowVersion = version
			e.appliedCheckpoint = cp
			e.restored = true
		} else if cleanupDomainCompleted(r, c.ExecutionParticipant) {
			return fault(f.ResourceBusy)
		}
		valid = true
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if !valid {
		return nil, unavailable(nil)
	}
	if err = st.authority.retainExecutionCleanup(e); err != nil {
		return nil, err
	}
	return e, nil
}

func sameExecutionClaim(a, b *lifecycleStopClaim) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (st *lifecycleCleanupState) registerExecutionRound(ctx context.Context, e *executionCleanupRound) (f.CommitResult, bool) {
	valid := false
	r := e.round
	result := st.store.WithinTx(ctx, r.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(r.claim.project, f.Exclusive), schedulerLock(r.claim.project)}); err != nil {
			return portError(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, r.claim.project)
		if err != nil {
			return err
		}
		cause, record, name, replay, err := st.current(ctx, x, r.claim.project, r.claim.operation)
		if err != nil {
			return err
		}
		if replay || cause != r.cause || name != c.ExecutionParticipant || record.operation.Version != r.operationVersion {
			return fault(f.ResourceBusy)
		}
		actual, err := loadLifecycleStopClaim(ctx, x, r.claim.operation)
		if err != nil {
			return err
		}
		if !sameExecutionClaim(actual, r.previous) {
			return fault(f.ResourceBusy)
		}
		version, attempts, cp, err := cleanupParticipantRow(ctx, x, r)
		if err != nil {
			return err
		}
		if version != r.rowVersion-1 || attempts != e.priorAttempts || !sameExecutionCheckpoint(cp, r.checkpoint) {
			return fault(f.ResourceBusy)
		}
		if actual == nil {
			tag, err := x.Exec(ctx, `INSERT INTO agenteam_project.work_claims(work_kind,work_id,project_id,process_id,attempt_id,fence,phase) VALUES('lifecycle',$1,$2,$3,$4,1,'running')`, r.claim.operation.String(), r.claim.project.String(), r.claim.process.String(), r.claim.attempt)
			if err = affected(tag, err); err != nil {
				return err
			}
		} else {
			tag, err := x.Exec(ctx, `UPDATE agenteam_project.work_claims SET process_id=$2,attempt_id=$3,fence=$4,phase='running' WHERE work_kind='lifecycle' AND work_id=$1 AND project_id=$5 AND process_id=$6 AND attempt_id=$7 AND fence=$8 AND phase=$9`, r.claim.operation.String(), r.claim.process.String(), r.claim.attempt, r.claim.fence, r.claim.project.String(), actual.process.String(), actual.attempt, actual.fence, actual.phase)
			if err = affected(tag, err); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET attempt_count=attempt_count+1,version=version+1,next_attempt_at=NULL WHERE operation_id=$1 AND participant_name='execution' AND version=$2 AND cleanup_state IN ('required','pending')`, r.claim.operation.String(), version)
		if err = affected(tag, err); err != nil {
			return err
		}
		valid = true
		return ctx.Err()
	})
	return result, valid
}

func (st *lifecycleCleanupState) confirmExecutionRegistration(ctx context.Context, e *executionCleanupRound) error {
	r := e.round
	valid, registered := false, false
	result := st.store.WithinTx(ctx, r.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(r.claim.project, f.Exclusive), schedulerLock(r.claim.project)}); err != nil {
			return portError(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, r.claim.project)
		if err != nil {
			return err
		}
		cause, record, name, replay, err := st.current(ctx, x, r.claim.project, r.claim.operation)
		if err != nil {
			return err
		}
		if replay || cause != r.cause || name != c.ExecutionParticipant || record.operation.Version != r.operationVersion {
			return fault(f.ResourceBusy)
		}
		actual, err := loadLifecycleStopClaim(ctx, x, r.claim.operation)
		if err != nil {
			return err
		}
		version, attempts, cp, err := cleanupParticipantRow(ctx, x, r)
		if err != nil {
			return err
		}
		if !sameExecutionCheckpoint(cp, r.checkpoint) {
			return fault(f.ResourceBusy)
		}
		registered = sameExecutionClaim(actual, &r.claim) && version == r.rowVersion && attempts == e.priorAttempts+1
		before := sameExecutionClaim(actual, r.previous) && version == r.rowVersion-1 && attempts == e.priorAttempts
		if !registered && !before {
			return fault(f.ResourceBusy)
		}
		valid = true
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return lifecycleStopRoundError(err, commitError(*e.registrationUnknown))
	}
	if !valid {
		return unavailable(nil)
	}
	e.registrationUnknown = nil
	e.registered = registered
	return portError(ctx.Err())
}
