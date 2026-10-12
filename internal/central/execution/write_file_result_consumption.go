package execution

import (
	"bytes"
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// This explicit constructor consumes completed write receipts only. It shares
// the existing actual-call/Unknown owner but never a current dispatch grant,
// executor or process guard. The original Read constructor remains unchanged.
func NewWriteFileResultConsumer(driver *ToolCallDriver, results tc.WriteFileResultAuthority) (*ToolResultConsumer, error) {
	if driver == nil || driver.driver == nil || driver.driver.state == nil || nilPort(results) {
		return nil, fault(f.DependencyUnbound)
	}
	if driver.driver.state.tools.profile() != tc.WriteFileRegistration().Binding {
		return nil, fault(f.Forbidden)
	}
	return &ToolResultConsumer{driver: driver, writeResults: results, store: driver.driver.state.store,
		active: make(map[i.ExecutionID]context.CancelFunc), pending: make(map[i.ExecutionID]*toolResultUnknown), changed: make(chan struct{})}, nil
}

func (s *ToolResultConsumer) transactWriteFileResult(ctx context.Context, e i.ExecutionID, write bool) (*toolResultConsumption, bool, f.CommitResult, error) {
	var zero f.CommitResult
	plan, err := s.writeResults.DiscoverWriteFileResult(ctx, e)
	if err != nil {
		return nil, false, zero, portError(err)
	}
	if nilPort(plan) || plan.Binding().Validate() != nil || plan.Binding().Call.ExecutionID != e {
		return nil, false, zero, fault(f.Forbidden)
	}
	binding := plan.Binding()
	// Independently require this Execution driver's private history plan. An
	// arbitrary result port cannot replace the original committed call/args.
	call, err := s.driver.DiscoverCommittedWriteFileCall(ctx, e)
	if err != nil {
		return nil, false, zero, err
	}
	if !sameToolCallBinding(call.Binding(), binding.Call) {
		return nil, false, zero, fault(f.Forbidden)
	}
	locks := append(call.RequiredLocks(), plan.RequiredLocks()...)
	locks = append(locks, executionLock(e))
	cause, err := f.NewRecoveryCause("execution.tool-result.lookup", e.String(), binding.AttemptID.String())
	if write {
		cause, err = f.NewJobCause("execution.tool-result.consume", e.String(), binding.AttemptID.String())
	}
	if err != nil {
		return nil, false, zero, err
	}
	var candidate *toolResultConsumption
	found, completed, callbacks := false, false, 0
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		callbacks++
		if callbacks != 1 {
			return fault(f.InvalidState)
		}
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		var loanErr error
		calls, loans := 0, 0
		err = s.driver.WithCommittedWriteFileCallInTx(ctx, tx, call, func(ctx context.Context, original tc.ToolCallBinding, args []byte) error {
			calls++
			if calls != 1 || !sameToolCallBinding(original, binding.Call) {
				return fault(f.Forbidden)
			}
			return s.writeResults.WithWriteFileResultInTx(ctx, tx, plan, func(ctx context.Context, actual tc.WriteFileResultBinding, raw []byte) error {
				loans++
				if loans != 1 {
					loanErr = fault(f.InvalidState)
					return loanErr
				}
				if loanErr = validateWriteReceiptArguments(ctx, args, raw); loanErr != nil {
					return loanErr
				}
				candidate, found, loanErr = s.consumeWriteFileResultInTx(ctx, tx, x, binding, actual, raw, write)
				return loanErr
			})
		})
		if err != nil {
			return portError(err)
		}
		if calls != 1 || loans != 1 || loanErr != nil {
			if loanErr != nil {
				return loanErr
			}
			return fault(f.InvalidState)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if result.State() == f.Committed && (!completed || callbacks != 1) {
		return nil, false, result, fault(f.InvalidState)
	}
	return candidate, found, result, nil
}

func validateWriteReceiptArguments(ctx context.Context, original, receipt []byte) error {
	args, err := tc.DecodeWriteFileToolArguments(ctx, original)
	if err != nil {
		return err
	}
	defer clear(args.Arguments.Data)
	result, err := p.DecodeWriteFileResult(receipt)
	if err != nil || result.Path != args.Arguments.Path || result.BytesWritten != len(args.Arguments.Data) {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (s *ToolResultConsumer) consumeWriteFileResultInTx(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, expected, actual tc.WriteFileResultBinding, raw []byte, write bool) (*toolResultConsumption, bool, error) {
	if expected.Validate() != nil || actual.Validate() != nil {
		return nil, false, invalid()
	}
	bound := sealedToolResultBinding(actual)
	expectedJSON, err := sealedToolResultBindingJSON(sealedToolResultBinding(expected))
	if err != nil {
		return nil, false, err
	}
	actualJSON, err := sealedToolResultBindingJSON(bound)
	if err != nil || !bytes.Equal(expectedJSON, actualJSON) || !sameToolCallBinding(expected.Call, actual.Call) {
		return nil, false, fault(f.ConfirmationStale)
	}
	projection, err := agentloop.ProjectWriteFileToolResult(ctx, actual, raw)
	if err != nil {
		return nil, false, portError(err)
	}
	b := actual.Call
	row, err := loadExecution(ctx, x, b.ExecutionID)
	if err != nil {
		return nil, false, err
	}
	stored, err := loadDirectText(ctx, x, b.ExecutionID)
	if err != nil {
		return nil, false, err
	}
	if stored != nil && stored.toolPending != nil {
		defer clear(stored.toolPending.original)
		defer clear(stored.toolPending.canonical)
	}
	canonical, err := toolBinding(stored)
	if err != nil || !sameToolCallBinding(b, canonical) || row == nil {
		return nil, false, fault(f.ConfirmationStale)
	}
	// Completed history is not renewed mutation authority. A later cancellation
	// or removal of a Tool/Mount grant cannot change this original receipt.
	previous, err := loadSealedToolResultConsumption(ctx, x, bound, actualJSON, projection, row.summary.Version)
	if err != nil || previous != nil || !write {
		return previous, previous != nil, err
	}
	if row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil {
		return nil, false, fault(f.ConfirmationStale)
	}
	if _, err = s.driver.driver.state.deps.Projects.RequirePreparingProjectInTx(ctx, tx, b.ProjectID); err != nil {
		return nil, false, portError(err)
	}
	return insertToolResultConsumption(ctx, x, bound, actualJSON, projection, row, stored)
}
