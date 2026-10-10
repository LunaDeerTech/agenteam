package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MaxExecutionHandoffPage bounds historical association work in one traversal.
// A cursor advances through every page and wraps only at the observed end;
// terminal history cannot permanently hide later created Executions.
const MaxExecutionHandoffPage = 128

// ProjectExecutionVisit observes an already committed Dispatch association.
// Advance is admission/ownership information, not a Model or terminal receipt.
// Execution and Task lifecycles remain separate, including after Succeeded.
type ProjectExecutionVisit struct {
	DispatchID  DispatchID
	ExecutionID i.ExecutionID
	Execution   ec.Summary
	Advance     ec.ExecutionAdvance
	Err         error
}

func (ProjectExecutionVisit) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_execution_visit")
}
func (ProjectExecutionVisit) LogValue() slog.Value {
	return slog.StringValue("scheduler_execution_visit")
}
func (ProjectExecutionVisit) MarshalJSON() ([]byte, error) {
	return []byte(`"scheduler_execution_visit"`), nil
}

// NewProjectRunnerWithExecutions opts into delivery of reliable associations.
// It neither starts nor owns the executor's lifetime. Runner cancellation ends
// its original reads/admission calls; only the executor owner may stop its work.
// The existing constructor retains its original claim/Launch-only behavior.
func NewProjectRunnerWithExecutions(coordinator *Coordinator, visitor *PendingVisitor, options ProjectRunnerOptions, executions ec.AssociatedExecutor, pageSize int) (*ProjectRunner, error) {
	if pageSize < 1 || pageSize > MaxExecutionHandoffPage {
		return nil, invalid()
	}
	if nilPort(executions) {
		return nil, fault(f.DependencyUnbound)
	}
	runner, err := NewProjectRunner(coordinator, visitor, options)
	if err != nil {
		return nil, err
	}
	runner.executions = executions
	runner.executionPageSize = pageSize
	runner.readLaunched = loadTraversalLaunched
	return runner, nil
}

func (s *ProjectRunner) visitExecutionPage(ctx context.Context, out *ProjectRunResult, seen map[i.ExecutionID]DispatchID) error {
	if s.executions == nil {
		return nil
	}
	s.mu.Lock()
	after := ""
	if s.executionAfter != nil {
		after = s.executionAfter.String()
	}
	s.mu.Unlock()
	page, err := s.captureExecutionPage(ctx, after)
	if err != nil {
		return err
	}
	more := len(page) > s.executionPageSize
	if more {
		page = page[:s.executionPageSize]
	}
	for _, row := range page {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = s.advanceExecution(ctx, snapshot(row), out, seen)
		// Advance errors (including retained physical Unknown) leave this item
		// for the next explicit traversal. No failed lookup skips a page.
		if err == nil {
			id := row.id
			s.mu.Lock()
			s.executionAfter = &id
			s.mu.Unlock()
		}
		if waitErr := waitProjectTick(ctx, s.options.TickInterval); waitErr != nil {
			return errors.Join(err, waitErr)
		}
		if err != nil {
			return err
		}
	}
	if !more {
		s.mu.Lock()
		s.executionAfter = nil
		s.mu.Unlock()
	}
	return nil
}

func (s *ProjectRunner) advanceExecution(ctx context.Context, dispatch Dispatch, out *ProjectRunResult, seen map[i.ExecutionID]DispatchID) error {
	if s.executions == nil {
		return nil
	}
	if dispatch.data == nil {
		return fault(f.ConfirmationStale)
	}
	r := dispatch.data()
	if !validAssociatedDispatch(&r, s.options.ProjectID) {
		return fault(f.ConfirmationStale)
	}
	if prior, ok := seen[*r.execution]; ok {
		if prior != r.id {
			return fault(f.ConfirmationStale)
		}
		return nil
	}
	visit := ProjectExecutionVisit{DispatchID: r.id, ExecutionID: *r.execution}
	actual, summary, err := s.observeAssociatedExecution(ctx, &r)
	if err == nil {
		visit.Execution = summary.Clone()
		association := ec.AssociatedDispatch{ExecutionID: *actual.execution, AgentID: actual.agent, Key: actual.launch.Meta.IdempotencyKey, Digest: actual.digest, DispatchID: actual.id.String()}
		// No Task/Sprint/status filter precedes the original executor. Even a
		// now-terminal row may still have its exact terminal Unknown owner.
		visit.Advance, err = s.executions.Advance(ctx, s.options.ProjectID, association)
		if cancelled := ctx.Err(); cancelled != nil {
			err = errors.Join(err, cancelled)
		}
		if err == nil && !validExecutionAdvance(visit.Advance, s.options.ProjectID, *actual.execution) {
			err = unavailable(nil)
		}
	}
	visit.Err = err
	out.Executions = append(out.Executions, visit)
	if err == nil {
		seen[*r.execution] = r.id
	}
	return err
}

func validExecutionAdvance(v ec.ExecutionAdvance, project i.ProjectID, execution i.ExecutionID) bool {
	if v.ProjectID != project || v.ExecutionID != execution || v.Status != "" && !v.Status.Valid() {
		return false
	}
	switch v.Phase {
	case ec.ExecutionAccepted, ec.ExecutionPreparing, ec.ExecutionStarting, ec.ExecutionRetained, ec.ExecutionDeferred, ec.ExecutionTerminal:
		return true
	default:
		return false
	}
}
