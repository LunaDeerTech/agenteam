package execution

import (
	"bytes"
	"context"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type preparationInputRecord struct {
	claim preparationClaim
	input c.PreparationInput
}

const preparationInputColumns = `execution_id::text,project_id::text,agent_id::text,attempt_id::text,process_id::text,fence,launch_digest,request_id::text,command_identity,attempt_binding,schema_version,input,input_digest,captured_at`
const preparationInputInsertColumns = `execution_id,project_id,agent_id,attempt_id,process_id,fence,launch_digest,request_id,command_identity,attempt_binding,schema_version,input,input_digest,captured_at`

func loadPreparationInput(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID) (*preparationInputRecord, error) {
	return scanPreparationInput(x.QueryRow(ctx, `SELECT `+preparationInputColumns+` FROM agenteam_execution.preparation_inputs WHERE execution_id=$1`, execution.String()))
}

func scanPreparationInput(row postgres.Row) (*preparationInputRecord, error) {
	var execution, project, agent, attempt, process, launch, requestID, command, binding, digest string
	var fence, schema int64
	var raw []byte
	var captured time.Time
	err := row.Scan(&execution, &project, &agent, &attempt, &process, &fence, &launch, &requestID, &command, &binding, &schema, &raw, &digest, &captured)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r := &preparationInputRecord{}
	if r.claim.execution, err = f.ParseID[i.Execution](execution); err != nil {
		return nil, unavailable(nil)
	}
	if r.claim.project, err = f.ParseID[i.Project](project); err != nil {
		return nil, unavailable(nil)
	}
	if r.claim.agent, err = f.ParseID[i.Agent](agent); err != nil {
		return nil, unavailable(nil)
	}
	if r.claim.attempt, err = f.ParseID[preparationAttempt](attempt); err != nil {
		return nil, unavailable(nil)
	}
	if r.claim.process, err = f.ParseID[oc.Process](process); err != nil {
		return nil, unavailable(nil)
	}
	r.claim.fence, r.claim.phase = fence, "running"
	if fence < 1 || schema != int64(c.PreparationInputSchemaVersion) {
		return nil, unavailable(nil)
	}
	r.input, err = c.DecodePreparationInput(raw)
	if err != nil {
		return nil, unavailable(nil)
	}
	v := r.input.Fields()
	launchDigest, err := v.Request.Launch.Digest()
	cmd, commandErr := v.Request.Launch.Command()
	expectedBinding, bindingErr := preparationResourceBinding(v.Request, r.claim, v.Project)
	if err != nil || commandErr != nil || bindingErr != nil || v.Request.ExecutionID != r.claim.execution || v.Request.Launch.ProjectID != r.claim.project || v.Request.Launch.AgentID != r.claim.agent || string(launchDigest) != launch || v.Request.Launch.Meta.RequestID.String() != requestID || cmd.Canonical() != command || string(v.AttemptBinding) != binding || v.AttemptBinding != expectedBinding || string(r.input.Digest()) != digest || !v.CapturedAt.Time().Equal(captured) || !bytes.Equal(r.input.CanonicalBytes(), raw) {
		return nil, unavailable(nil)
	}
	return r, nil
}

func samePreparationInput(a, b *preparationInputRecord) bool {
	return a != nil && b != nil && a.claim == b.claim && a.input.Digest() == b.input.Digest() && bytes.Equal(a.input.CanonicalBytes(), b.input.CanonicalBytes())
}

func captureTime(ctx context.Context, x postgres.SQLExecutor) (f.Instant, error) {
	var now time.Time
	if err := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return f.Instant{}, unavailable(err)
	}
	at, err := f.NewInstant(now)
	if err != nil {
		return f.Instant{}, unavailable(nil)
	}
	return at, nil
}

func (w *preparationWitness) saveInput(ctx context.Context, input c.PreparationInput) error {
	if input.Validate() != nil || !w.resourcesOpen.Load() || !w.agentCaptured || !w.sourceCaptured {
		return fault(f.InvalidState)
	}
	v := input.Fields()
	if !v.Request.Equal(w.request) {
		return fault(f.ConfirmationStale)
	}
	if err := w.require(ctx, w.tx, w.request); err != nil {
		return err
	}
	binding, err := preparationResourceBinding(w.request, w.claim, w.project)
	if err != nil || v.AttemptBinding != binding {
		return fault(f.ConfirmationStale)
	}
	x, err := w.driver.store.InTx(w.tx)
	if err != nil {
		return portError(err)
	}
	record := &preparationInputRecord{claim: w.claim, input: input}
	// Save exact immutable expected bytes before the physical COMMIT. Recovery
	// cannot substitute a new capture or infer rollback from an absent row.
	w.driver.mu.Lock()
	run := w.driver.calls[w.request.ExecutionID]
	if run == nil || run.returned || run.claim == nil || *run.claim != w.claim || run.unresolved != nil {
		w.driver.mu.Unlock()
		return fault(f.Forbidden)
	}
	run.input = record
	w.driver.mu.Unlock()
	digest, _ := w.request.Launch.Digest()
	command, _ := w.request.Launch.Command()
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.preparation_inputs(`+preparationInputInsertColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, w.claim.execution.String(), w.claim.project.String(), w.claim.agent.String(), w.claim.attempt.String(), w.claim.process.String(), w.claim.fence, string(digest), w.request.Launch.Meta.RequestID.String(), command.Canonical(), string(binding), int64(c.PreparationInputSchemaVersion), input.CanonicalBytes(), string(input.Digest()), v.CapturedAt.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return ctx.Err()
}

// A replay reads the complete immutable capture under the original Execution
// gates, never calls providers and never creates another preparation attempt.
// This transaction has no writes; even an unknown physical read completion
// cannot own a new durable preparation or authorize a retry of its writer.
func (s *preparationState) capturedInput(ctx context.Context, request c.PreparationRequest) (bool, error) {
	locks, err := preparationLocks(request)
	if err != nil {
		return false, err
	}
	key, err := f.NewID[struct{}]()
	if err != nil {
		return false, unavailable(err)
	}
	cause, err := f.NewRecoveryCause("execution.preparation-input", request.ExecutionID.String(), key.String())
	if err != nil {
		return false, err
	}
	found := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
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
		record, err := loadPreparationInput(ctx, x, request.ExecutionID)
		if err != nil {
			return err
		}
		if record != nil {
			if current.summary.Status != c.Preparing || !record.input.Fields().Request.Equal(request) {
				return fault(f.ConfirmationStale)
			}
			found = true
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return false, err
	}
	return found, nil
}
