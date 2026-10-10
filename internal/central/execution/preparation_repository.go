package execution

import (
	"context"
	"errors"
	"math"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type preparationClaim struct {
	execution i.ExecutionID
	project   i.ProjectID
	agent     i.AgentID
	attempt   f.ID[preparationAttempt]
	process   oc.ProcessID
	fence     int64
	phase     string
}
type preparationAttempt struct{}

func (r preparationClaim) cause() f.TransactionCause {
	cause, _ := f.NewJobCause("execution-preparation", r.execution.String(), r.attempt.String())
	return cause
}

func scanPreparationClaim(row postgres.Row) (*preparationClaim, error) {
	var execution, project, agent, attempt, process string
	r := &preparationClaim{}
	err := row.Scan(&execution, &project, &agent, &attempt, &process, &r.fence, &r.phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if r.execution, err = f.ParseID[i.Execution](execution); err != nil {
		return nil, unavailable(nil)
	}
	if r.project, err = f.ParseID[i.Project](project); err != nil {
		return nil, unavailable(nil)
	}
	if r.agent, err = f.ParseID[i.Agent](agent); err != nil {
		return nil, unavailable(nil)
	}
	if r.attempt, err = f.ParseID[preparationAttempt](attempt); err != nil {
		return nil, unavailable(nil)
	}
	if r.process, err = f.ParseID[oc.Process](process); err != nil {
		return nil, unavailable(nil)
	}
	if r.fence < 1 || r.phase != "running" && r.phase != "terminal" {
		return nil, unavailable(nil)
	}
	return r, nil
}

func loadPreparationClaim(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID) (*preparationClaim, error) {
	return scanPreparationClaim(x.QueryRow(ctx, `SELECT c.execution_id::text,c.project_id::text,c.agent_id::text,c.attempt_id::text,c.process_id::text,c.fence,a.phase FROM agenteam_execution.preparation_claims c JOIN agenteam_execution.preparation_attempts a ON (a.execution_id,a.attempt_id,a.process_id,a.fence)=(c.execution_id,c.attempt_id,c.process_id,c.fence) WHERE c.execution_id=$1`, execution.String()))
}
func loadPreparationAttempt(ctx context.Context, x postgres.SQLExecutor, claim preparationClaim) (*preparationClaim, error) {
	return scanPreparationClaim(x.QueryRow(ctx, `SELECT execution_id::text,project_id::text,agent_id::text,attempt_id::text,process_id::text,fence,phase FROM agenteam_execution.preparation_attempts WHERE execution_id=$1 AND attempt_id=$2`, claim.execution.String(), claim.attempt.String()))
}
func samePreparationClaim(a, b *preparationClaim) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func preparationLocks(request c.PreparationRequest) ([]f.LockRequest, error) {
	command, err := request.Launch.Command()
	if err != nil {
		return nil, err
	}
	locks := []f.LockRequest{commandLock(command), projectLock(request.Launch.ProjectID), agentLock(request.Launch.AgentID, f.Exclusive), executionLock(request.ExecutionID)}
	if request.Launch.Trigger.Kind == "task" {
		key, _ := f.ProjectScheduleLock(request.Launch.ProjectID.String())
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
	}
	return oc.NormalizeLocks(locks)
}

func (s *preparationState) start(ctx context.Context, request c.PreparationRequest, run *preparationCall) error {
	previous, err := loadPreparationClaim(ctx, s.store, request.ExecutionID)
	if err != nil {
		return err
	}
	if previous != nil && (previous.project != request.Launch.ProjectID || previous.agent != request.Launch.AgentID) {
		return fault(f.InvalidState)
	}
	if previous != nil && previous.phase == "running" {
		if previous.process == s.process {
			s.mu.Lock()
			returned, known := s.returned[request.ExecutionID]
			s.mu.Unlock()
			if !known || returned != *previous {
				return fault(f.ResourceBusy)
			}
		} else if err = s.processes.ConfirmStopped(ctx, previous.process); err != nil {
			return portError(err)
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if previous != nil && previous.fence == math.MaxInt64 {
		return fault(f.InvalidState)
	}
	attempt, err := f.NewID[preparationAttempt]()
	if err != nil {
		return unavailable(err)
	}
	claim := preparationClaim{request.ExecutionID, request.Launch.ProjectID, request.Launch.AgentID, attempt, s.process, 1, "running"}
	if previous != nil {
		claim.fence = previous.fence + 1
	}
	locks, err := preparationLocks(request)
	if err != nil {
		return err
	}
	// Save the original identity before the physical commit is attempted. An
	// unknown result must keep this exact attempt and cannot issue callbacks.
	run.claim = &claim
	result := s.store.WithinTx(ctx, claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if s.processes.CurrentProcess() != s.process {
			return fault(f.InvalidState)
		}
		current, err := loadExecution(ctx, x, request.ExecutionID)
		if err != nil {
			return err
		}
		if err = preparingRecord(current, request, true); err != nil {
			return err
		}
		actual, err := loadPreparationClaim(ctx, x, request.ExecutionID)
		if err != nil {
			return err
		}
		if !samePreparationClaim(actual, previous) {
			return fault(f.ResourceBusy)
		}
		project, err := s.projects.RequirePreparingProjectInTx(ctx, tx, request.Launch.ProjectID)
		if err != nil {
			return portError(err)
		}
		if project.Validate() != nil || project.ID != request.Launch.ProjectID || project.Lifecycle != "active" {
			return fault(f.InvalidState)
		}
		if current.summary.Status == c.Created {
			tag, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET status='preparing',version=version+1,updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND status='created' AND version=$2 AND cancel_requested_at IS NULL`, request.ExecutionID.String(), int64(current.summary.Version))
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return fault(f.ConfirmationStale)
			}
		}
		// The prior callback really returned locally or its foreign process was
		// confirmed stopped. Mark its attempt returned under the exact same gate.
		if previous != nil && previous.phase == "running" {
			if err = markPreparationReturned(ctx, x, *previous); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.preparation_attempts(execution_id,project_id,agent_id,attempt_id,process_id,fence,phase,started_at) VALUES($1,$2,$3,$4,$5,$6,'running',clock_timestamp())`, claim.execution.String(), claim.project.String(), claim.agent.String(), claim.attempt.String(), claim.process.String(), claim.fence)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
		if previous == nil {
			tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.preparation_claims(execution_id,project_id,agent_id,attempt_id,process_id,fence) VALUES($1,$2,$3,$4,$5,$6)`, claim.execution.String(), claim.project.String(), claim.agent.String(), claim.attempt.String(), claim.process.String(), claim.fence)
		} else {
			tag, err = x.Exec(ctx, `UPDATE agenteam_execution.preparation_claims SET attempt_id=$2,process_id=$3,fence=$4 WHERE execution_id=$1 AND attempt_id=$5 AND process_id=$6 AND fence=$7`, claim.execution.String(), claim.attempt.String(), claim.process.String(), claim.fence, previous.attempt.String(), previous.process.String(), previous.fence)
		}
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		return ctx.Err()
	})
	return commitError(result)
}

func preparingRecord(row *executionRecord, request c.PreparationRequest, allowCreated bool) error {
	if row == nil {
		return fault(f.NotFound)
	}
	if !(c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(request) {
		return fault(f.ConfirmationStale)
	}
	if row.summary.CancelRequestedAt != nil || row.summary.Status != c.Preparing && (!allowCreated || row.summary.Status != c.Created) {
		return fault(f.InvalidState)
	}
	return nil
}
func markPreparationReturned(ctx context.Context, x postgres.SQLExecutor, claim preparationClaim) error {
	tag, err := x.Exec(ctx, `UPDATE agenteam_execution.preparation_attempts SET phase='terminal',returned_at=GREATEST(clock_timestamp(),started_at) WHERE execution_id=$1 AND attempt_id=$2 AND process_id=$3 AND fence=$4 AND phase='running'`, claim.execution.String(), claim.attempt.String(), claim.process.String(), claim.fence)
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	return nil
}

// finish is accounting only, after the original callback and its transaction
// have returned. Project cancellation/archival does not erase that obligation.
func (s *preparationState) finish(ctx context.Context, request c.PreparationRequest, claim preparationClaim, requireObserved bool) error {
	locks, err := preparationLocks(request)
	if err != nil {
		return err
	}
	result := s.store.WithinTx(ctx, claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if s.processes.CurrentProcess() != s.process {
			return fault(f.InvalidState)
		}
		actual, err := loadPreparationAttempt(ctx, x, claim)
		if err != nil {
			return err
		}
		if actual == nil {
			if requireObserved {
				return fault(f.CommitUnknown)
			}
			return fault(f.InvalidState)
		}
		expected := claim
		expected.phase = actual.phase
		if *actual != expected {
			return fault(f.ConfirmationStale)
		}
		if actual.phase == "terminal" {
			return ctx.Err()
		}
		current, err := loadPreparationClaim(ctx, x, claim.execution)
		if err != nil {
			return err
		}
		if !samePreparationClaim(current, &claim) {
			return fault(f.ConfirmationStale)
		}
		return markPreparationReturned(ctx, x, claim)
	})
	return commitError(result)
}
