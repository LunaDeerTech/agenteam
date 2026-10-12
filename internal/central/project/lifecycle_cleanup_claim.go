package project

import (
	"context"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type lifecycleCleanupRound struct {
	claim                      lifecycleStopClaim
	previous                   *lifecycleStopClaim
	cause                      c.LifecycleCause
	operationVersion           f.Version
	rowVersion                 int64
	participant                c.ParticipantName
	checkpoint                 *c.CleanupCheckpoint
	agentStep                  bool
	variablesStep              bool
	terminalStep               bool
	terminalConfirm            bool
	terminalUnapplied          bool
	terminalUnappliedConfirmed bool
	secretParentStep           bool
	secretParentCheckpoint     *c.CleanupCheckpoint
}

func (st *lifecycleCleanupState) canJoin(ctx context.Context, claim *lifecycleStopClaim) error {
	if claim == nil || claim.phase == "terminal" {
		return nil
	}
	if claim.process == st.process {
		st.mu.Lock()
		owner, ok := st.owners[claim.operation]
		st.mu.Unlock()
		if !ok || (owner.claim == nil || *owner.claim != *claim) && (owner.prior == nil || *owner.prior != *claim) {
			return fault(f.ResourceBusy)
		}
		return nil
	}
	return portError(st.processes.ConfirmStopped(ctx, claim.process))
}

func (st *lifecycleCleanupState) current(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, operation c.OperationID) (c.LifecycleCause, *lifecycleRecord, c.ParticipantName, bool, error) {
	r, err := loadLifecycleOperation(ctx, x, project, operation)
	if err != nil {
		return c.LifecycleCause{}, nil, "", false, err
	}
	if r == nil {
		return c.LifecycleCause{}, nil, "", false, fault(f.NotFound)
	}
	cause := c.LifecycleCause{OperationID: operation, Action: r.operation.Action, ProjectVersion: r.operation.ProjectVersion}
	if cause.Validate() != nil || cause.Action != c.Delete {
		return c.LifecycleCause{}, nil, "", false, fault(f.Forbidden)
	}
	participant, replay, err := st.cleanupProgress(r)
	if err != nil {
		return c.LifecycleCause{}, nil, "", false, err
	}
	if _, err = st.authority.lifecycleFact(ctx, x, project, cause, participant); err != nil {
		return c.LifecycleCause{}, nil, "", false, err
	}
	return cause, r, participant, replay, nil
}

func (st *lifecycleCleanupState) start(ctx context.Context, project c.ProjectID, operation c.OperationID) (*lifecycleCleanupRound, bool, error) {
	return st.startWithSecretParent(ctx, project, operation, nil)
}

func (st *lifecycleCleanupState) startWithSecretParent(ctx context.Context, project c.ProjectID, operation c.OperationID, parent *lifecycleCleanupRound) (*lifecycleCleanupRound, bool, error) {
	previous, err := loadLifecycleStopClaim(ctx, st.store, operation)
	if err != nil {
		return nil, false, err
	}
	if previous != nil && previous.project != project {
		return nil, false, fault(f.Forbidden)
	}
	if err = st.canJoin(ctx, previous); err != nil {
		return nil, false, err
	}
	if err = ctx.Err(); err != nil {
		return nil, false, portError(err)
	}
	attempt, err := f.NewID[struct{}]()
	if err != nil {
		return nil, false, unavailable(err)
	}
	round := &lifecycleCleanupRound{claim: lifecycleStopClaim{project: project, operation: operation, process: st.process, attempt: attempt.String(), fence: 1, phase: "running"}, previous: previous}
	if previous != nil {
		if previous.fence == math.MaxInt64 {
			return nil, false, fault(f.InvalidState)
		}
		round.claim.fence = previous.fence + 1
	}
	validated, replay := false, false
	result := st.store.WithinTx(ctx, round.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
			return unavailable(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, project)
		if err != nil {
			return err
		}
		var r *lifecycleRecord
		round.cause, r, round.participant, replay, err = st.current(ctx, x, project, operation)
		if err != nil {
			return err
		}
		actual, err := loadLifecycleStopClaim(ctx, x, operation)
		if err != nil {
			return err
		}
		if (actual == nil) != (previous == nil) || actual != nil && *actual != *previous {
			return fault(f.ResourceBusy)
		}
		if replay && round.participant == c.SecretParticipant {
			_, attempts, cp, e := cleanupParticipantRow(ctx, x, *round)
			if e != nil {
				return e
			}
			if attempts == 0 || cp == nil {
				return unavailable(nil)
			}
			selected, e := st.selectSecretParent(ctx, x, round, actual, attempts, r.operation.Version, parent)
			if e != nil {
				return e
			}
			if selected {
				return unavailable(nil)
			}
		}
		if replay {
			// A completed checkpoint atomically terminates its original claim.
			// A running foreign claim is not turned into a local success report.
			if actual == nil || actual.phase != "terminal" {
				return fault(f.ResourceBusy)
			}
			validated = true
			return nil
		}
		rowVersion, attempts, checkpoint, err := cleanupParticipantRow(ctx, x, *round)
		if err != nil {
			return err
		}
		if round.participant == c.SecretParticipant {
			selected, e := st.selectSecretParent(ctx, x, round, actual, attempts, r.operation.Version, parent)
			if e != nil {
				return e
			}
			if selected {
				validated = true
				return nil
			}
		}
		if round.participant == c.SkillsParticipant {
			if st.terminalAgents != nil {
				selected, e := st.selectTerminalRound(round, actual, checkpoint, r.operation.Version, rowVersion)
				if e != nil {
					return e
				}
				if selected && round.terminalConfirm {
					validated = true
					return nil
				}
			}
			var substepReplay bool
			if !round.terminalStep {
				round.agentStep, round.variablesStep, substepReplay, err = st.selectDomainStep(checkpoint, round.cause, project)
			}
			if err != nil {
				return err
			}
			if substepReplay {
				for _, row := range r.participants {
					if row.name == c.SkillsParticipant && row.cleanup != "pending" {
						return unavailable(nil)
					}
				}
				if actual == nil || actual.phase != "terminal" {
					return fault(f.ResourceBusy)
				}
				// Confirm only this completed substep. The combined participant
				// remains pending/domains and no downstream owner is invoked.
				validated, replay = true, true
				return nil
			}
		}
		if rowVersion == math.MaxInt64 || attempts == math.MaxInt64 {
			return fault(f.InvalidState)
		}
		if actual == nil {
			_, err = x.Exec(ctx, `INSERT INTO agenteam_project.work_claims(work_kind,work_id,project_id,process_id,attempt_id,fence,phase) VALUES('lifecycle',$1,$2,$3,$4,1,'running')`, operation.String(), project.String(), round.claim.process.String(), round.claim.attempt)
		} else {
			tag, e := x.Exec(ctx, `UPDATE agenteam_project.work_claims SET process_id=$2,attempt_id=$3,fence=$4,phase='running' WHERE work_kind='lifecycle' AND work_id=$1 AND project_id=$5 AND process_id=$6 AND attempt_id=$7 AND fence=$8 AND phase=$9`, operation.String(), round.claim.process.String(), round.claim.attempt, round.claim.fence, project.String(), previous.process.String(), previous.attempt, previous.fence, previous.phase)
			err = affected(tag, e)
		}
		if err != nil {
			return unavailable(err)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET attempt_count=attempt_count+1,version=version+1,next_attempt_at=NULL WHERE operation_id=$1 AND participant_name=$3 AND version=$2 AND cleanup_state IN ('required','pending')`, operation.String(), rowVersion, string(round.participant))
		if err = affected(tag, err); err != nil {
			return err
		}
		round.operationVersion, round.rowVersion = r.operation.Version, rowVersion+1
		round.checkpoint = checkpoint
		validated = true
		return nil
	})
	if err = commitError(result); err != nil {
		if result.State() == f.Unknown && !replay {
			return round, false, err
		}
		return nil, false, err
	}
	if !validated {
		return nil, false, unavailable(nil)
	}
	if err = ctx.Err(); err != nil {
		if replay {
			return nil, false, portError(err)
		}
		return round, false, portError(err)
	}
	if replay {
		return nil, true, nil
	}
	return round, false, nil
}
