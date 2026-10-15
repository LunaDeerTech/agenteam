package execution

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *associatedExecutorState) planPreparationCancellation(ctx context.Context, w *executionRecoveryCall) error {
	store := s.preparation.state.store
	cause, _ := f.NewRecoveryCause("execution.cancel-before-start.plan", w.request.ExecutionID.String(), w.request.Launch.Meta.RequestID.String())
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, w.locks); err != nil {
			return portError(err)
		}
		if err := w.require(ctx); err != nil {
			return err
		}
		if err := requireProjectCallInTx(ctx, tx, store); err != nil {
			return err
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = s.requirePreparationCancellation(ctx, x, w, true); err != nil {
			return err
		}
		if w.input != nil {
			if err = s.preparation.state.verifyMountMaterial(ctx, tx, w.input); err != nil {
				return err
			}
		}
		w.at, err = captureTime(ctx, x)
		return err
	})
	if err := commitError(result); err != nil {
		return err
	}
	id, err := f.NewID[event.EventIdentity]()
	if err != nil {
		return unavailable(err)
	}
	project, _ := f.ParseID[event.Project](w.request.Launch.ProjectID.String())
	aggregate, _ := f.ParseID[event.Aggregate](w.request.ExecutionID.String())
	version := w.before.Version + 1
	w.event, err = s.direct.state.deps.Lifecycle.New(event.Header{EventID: id, EventType: c.ExecutionCancelledName, SchemaVersion: 1, OccurredAt: w.at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: c.ExecutionAggregate, AggregateID: aggregate, AggregateVersion: &version}, c.DirectTextLifecycle{ExecutionID: w.request.ExecutionID, AgentID: w.request.Launch.AgentID, Status: c.Cancelled, Reason: "cancelled_before_start"})
	return err
}

func (s *associatedExecutorState) requirePreparationCancellation(ctx context.Context, x postgres.SQLExecutor, w *executionRecoveryCall, freeze bool) error {
	row, err := loadExecution(ctx, x, w.request.ExecutionID)
	if err != nil {
		return err
	}
	if row == nil || !(c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(w.request) || row.summary.Status != w.before.Status || row.summary.Version != w.before.Version || row.summary.Version == f.Version(math.MaxInt64) || row.summary.CancelRequestedAt == nil || w.before.CancelRequestedAt == nil || *row.summary.CancelRequestedAt != *w.before.CancelRequestedAt || row.summary.SnapshotID != nil || row.summary.StartedAt != nil || row.summary.CompletedAt != nil {
		return fault(f.ConfirmationStale)
	}
	claim, err := loadPreparationClaim(ctx, x, row.summary.ID)
	if err != nil {
		return err
	}
	if !samePreparationClaim(claim, w.claim) {
		return fault(f.ConfirmationStale)
	}
	if owners := s.ownedCalls(w.job); owners.preparation || owners.direct || owners.foreign {
		return fault(f.ResourceBusy)
	}
	// All captured Model/Environment/Mount resources and this immutable input
	// commit in the same original capture transaction. Its owner has returned
	// (or died), and its Execution EX lock is now held. A discovery-only Model
	// intention is historical planning, not a captured credential lease.
	input, err := loadPreparationInput(ctx, x, row.summary.ID)
	if err != nil {
		return err
	}
	if input != nil {
		if row.summary.Status != c.Preparing || claim == nil || !input.input.Fields().Request.Equal(w.request) {
			return fault(f.ConfirmationStale)
		}
		expected := *claim
		expected.phase = input.claim.phase
		if expected != input.claim {
			return fault(f.ConfirmationStale)
		}
	}
	if freeze {
		w.input = input
	} else if input == nil && w.input != nil || input != nil && !samePreparationInput(input, w.input) {
		return fault(f.ConfirmationStale)
	}
	stored, err := loadDirectText(ctx, x, row.summary.ID)
	if err != nil {
		return err
	}
	if stored != nil {
		return fault(f.ResourceBusy)
	}
	return requireNoUnsupportedRetirementHistory(ctx, x, row.summary.ID)
}

func (s *associatedExecutorState) commitPreparationCancellation(ctx context.Context, w *executionRecoveryCall, actor i.Actor) f.CommitResult {
	store := s.preparation.state.store
	cause, _ := f.NewJobCause("execution.cancel-before-start", w.request.ExecutionID.String(), w.event.Header().EventID.String())
	return store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, w.locks); err != nil {
			return portError(err)
		}
		if err := w.require(ctx); err != nil {
			return err
		}
		if err := requireProjectCallInTx(ctx, tx, store); err != nil {
			return err
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = s.requirePreparationCancellation(ctx, x, w, false); err != nil {
			return err
		}
		if w.claim != nil && w.claim.phase == "running" {
			if err = markPreparationReturned(ctx, x, *w.claim); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET status='cancelled',version=version+1,completed_at=$3,updated_at=GREATEST($3,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status IN('created','preparing') AND cancel_requested_at IS NOT NULL AND snapshot_id IS NULL AND started_at IS NULL AND completed_at IS NULL`, w.request.ExecutionID.String(), int64(w.before.Version), w.at.Time())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		w.tx, w.written, w.retired = tx, true, false
		defer func() { w.tx, w.written, w.retired = f.Tx{}, false, false }()
		if w.input != nil {
			if err = s.direct.state.deps.Models.ReleaseExecutionModelInTx(ctx, tx, w.model, w.modelPlan); err != nil {
				return portError(err)
			}
			if err = s.direct.state.deps.Environment.RetireEnvironmentInTx(ctx, tx, w.input.input.Fields().Environment, w.environmentPlan); err != nil {
				return portError(err)
			}
			if err = releaseMCPRetirement(ctx, tx, s.direct.state.deps.MCP, w.mcp, w.mcpPlan); err != nil {
				return err
			}
		}
		w.retired = true
		_, err = s.direct.state.deps.Events.AppendEventInTx(ctx, tx, actor, w.event, w.plan)
		return portError(err)
	})
}

func preparationCancellationMatches(row *executionRecord, w *executionRecoveryCall) bool {
	return row != nil && (c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(w.request) && row.summary.Status == c.Cancelled && row.summary.Version == w.before.Version+1 && row.summary.SnapshotID == nil && row.summary.StartedAt == nil && row.summary.CompletedAt != nil && *row.summary.CompletedAt == w.at && row.summary.CancelRequestedAt != nil && w.before.CancelRequestedAt != nil && *row.summary.CancelRequestedAt == *w.before.CancelRequestedAt
}

func (s *associatedExecutorState) readPreparationCancellation(ctx context.Context, w *executionRecoveryCall) (bool, error) {
	store := s.preparation.state.store
	cause, _ := f.NewRecoveryCause("execution.cancel-before-start.observe", w.request.ExecutionID.String(), w.event.Header().EventID.String())
	var found bool
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, w.locks); err != nil {
			return portError(err)
		}
		if err := w.require(ctx); err != nil {
			return err
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
		found = preparationCancellationMatches(row, w)
		if found && w.input != nil {
			w.tx, w.written = tx, true
			defer func() { w.tx, w.written = f.Tx{}, false }()
			model, err := s.direct.state.deps.Models.ExecutionModelRetiredInTx(ctx, tx, w.model, w.modelPlan)
			if err != nil {
				return portError(err)
			}
			environment, err := s.direct.state.deps.Environment.EnvironmentRetiredInTx(ctx, tx, w.input.input.Fields().Environment, w.environmentPlan)
			if err != nil {
				return portError(err)
			}
			mcp, err := observeMCPRetirement(ctx, tx, s.direct.state.deps.MCP, w.mcp, w.mcpPlan)
			if err != nil {
				return err
			}
			found = model && environment && mcp
		}
		return ctx.Err()
	})
	return found, commitError(result)
}

func readRecoveryCallOwners(ctx context.Context, x postgres.SQLExecutor, current projectCallRecord) ([]projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(`+projectCallProjection+` ORDER BY id),'[]'::jsonb) FROM agenteam_execution.project_calls WHERE project_id=$1 AND lane='direct' AND invocation_identity=$2 AND invocation_digest=$3`, current.Project.String(), current.Identity, current.Digest.String()).Scan(&raw)
	if err != nil {
		return nil, portError(err)
	}
	var owners []projectCallRecord
	if json.Unmarshal(raw, &owners) != nil || owners == nil {
		return nil, unavailable(nil)
	}
	for n, owner := range owners {
		if !owner.valid() || owner.Project != current.Project || owner.Lane != projectLaneDirect || owner.Identity != current.Identity || owner.Digest != current.Digest || n > 0 && owners[n-1].ID.String() >= owner.ID.String() {
			return nil, unavailable(nil)
		}
	}
	return owners, ctx.Err()
}

func (s *directTextState) readColdLoopCheckpoint(ctx context.Context, run *directTextCall) error {
	w := run.cold
	cause, _ := f.NewRecoveryCause("execution.loop.cold-checkpoint", run.request.ExecutionID.String(), run.round.Fields().ID.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, run.locks); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, s.store); err != nil {
			return err
		}
		s.mu.Lock()
		valid := s.calls[run.request.ExecutionID] == run && run.cold == w && w.owner == s && w.run == run && !w.verified
		s.mu.Unlock()
		if !valid || s.processes.CurrentProcess() != s.process {
			return fault(f.Forbidden)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		owners, err := readRecoveryCallOwners(ctx, x, w.call)
		if err != nil {
			return err
		}
		if !slices.Equal(owners, w.owners) {
			return fault(f.ConfirmationStale)
		}
		for _, old := range owners {
			if old.ID != w.call.ID && old.Process == s.projectCalls.process && !s.recoveryLocalCallReturned(old) {
				return fault(f.ResourceBusy)
			}
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || stored == nil || (row.summary.Status != c.Running && row.summary.Status != c.Waiting) || !sameDirectText(stored, run.snapshot, run.round) || stored.toolPending == nil || stored.terminal != nil || row.summary.Version < stored.toolPending.version || !(c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(run.request) {
			return fault(f.ConfirmationStale)
		}
		input, err := loadPreparationInput(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if input == nil || input.input.Digest() != stored.snapshot.Fields().Context.InputDigest() || !input.input.Fields().Request.Equal(run.request) {
			return fault(f.ConfirmationStale)
		}
		var reason, reference *string
		var at *time.Time
		if err = x.QueryRow(ctx, `SELECT waiting_reason,waiting_reference::text,waiting_at FROM agenteam_execution.executions WHERE id=$1`, run.request.ExecutionID.String()).Scan(&reason, &reference, &at); err != nil {
			return portError(err)
		}
		projection := c.TaskExecution{ID: row.summary.ID, Status: row.summary.Status, StartedAt: row.summary.StartedAt}
		if err = projectTaskWaiting(&projection, reason, reference, at); err != nil {
			return err
		}
		if err = validColdWaiting(row, stored, reference); err != nil {
			return err
		}
		if err = s.verifyDirectTextMountUse(ctx, tx, run, row, stored, input); err != nil {
			return err
		}
		s.mu.Lock()
		run.input, run.summary, run.toolFacts = input, row.summary.Clone(), stored.toolPending
		s.mu.Unlock()
		return ctx.Err()
	})
	return commitError(result)
}
