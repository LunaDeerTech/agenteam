package execution

import (
	"bytes"
	"context"
	"errors"
	"math"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type toolResultConsumption struct {
	receipt c.ToolResultConsumptionReceipt
	binding []byte
	message []byte
}

// This is an explicit persistence projection. Binding's default JSON is
// intentionally redacted. Actor is uniquely AgentRun(P,A,E) by Validate; no
// caller identity or borrowed content is inferred from these plain fields.
func toolResultBindingJSON(v tc.ReadFileResultBinding) ([]byte, error) {
	if v.Validate() != nil {
		return nil, invalid()
	}
	return sealedToolResultBindingJSON(sealedToolResultBinding(v))
}

func sealedToolResultBindingJSON(v sealedToolResultBinding) ([]byte, error) {
	if v.validate() != nil {
		return nil, invalid()
	}
	b := v.Call
	return directTextJSON(map[string]any{
		"project_id": b.ProjectID.String(), "agent_id": b.AgentID.String(), "execution_id": b.ExecutionID.String(),
		"round_id": b.RoundID, "snapshot_id": b.SnapshotID, "input_binding_id": b.InputBindingID,
		"logical_call_id": b.LogicalCallID.String(), "invocation_id": b.InvocationID.String(), "call_id": b.CallID,
		"model_visible_name": b.ModelVisibleName, "tool_id": b.Spec.ToolID.String(), "spec_revision": int64(b.Spec.SpecRevision),
		"operation_name": b.RunnerBinding.OperationName, "operation_revision": b.RunnerBinding.OperationRevision, "protocol_major": b.RunnerBinding.ProtocolMajor,
		"payload_id": b.Input.PayloadID, "payload_digest": string(b.Input.SHA256), "payload_bytes": int64(b.Input.Bytes), "canonical_arguments": string(b.CanonicalArguments),
		"operation_id": v.OperationID.String(), "attempt_id": v.AttemptID.String(), "request_id": string(v.RequestID),
		"input_digest": string(v.InputDigest), "fingerprint": string(v.Fingerprint), "terminal_digest": string(v.TerminalDigest),
		"outcome": v.Outcome, "code": v.Code, "attempted": v.Attempted, "result_digest": string(v.ResultDigest), "result_bytes": int64(v.ResultBytes),
	})
}

func sameToolResultConsumption(a, b *toolResultConsumption) bool {
	return a != nil && b != nil && a.receipt == b.receipt && bytes.Equal(a.binding, b.binding) && bytes.Equal(a.message, b.message)
}

func (s *ToolResultConsumer) transactToolResult(ctx context.Context, e i.ExecutionID, write bool) (*toolResultConsumption, bool, f.CommitResult, error) {
	if s.commandResults != nil {
		return s.transactRunCommandResult(ctx, e, write)
	}
	if s.writeResults != nil {
		return s.transactWriteFileResult(ctx, e, write)
	}
	var zero f.CommitResult
	plan, err := s.results.DiscoverReadFileResult(ctx, e)
	if err != nil {
		return nil, false, zero, portError(err)
	}
	if nilPort(plan) || plan.Binding().Validate() != nil || plan.Binding().Call.ExecutionID != e {
		return nil, false, zero, fault(f.Forbidden)
	}
	binding := plan.Binding()
	// Discover our own private canonical plan as well. An implementation of the
	// Tool port cannot replace actual Execution/current authority with a DTO.
	call, err := s.driver.DiscoverToolCall(ctx, binding.Call)
	if err != nil {
		return nil, false, zero, err
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
		if err = s.driver.RequireToolCallInTx(ctx, tx, binding.Call, call); err != nil {
			return err
		}
		loans := 0
		var loanErr error
		err = s.results.WithReadFileResultInTx(ctx, tx, plan, func(ctx context.Context, actual tc.ReadFileResultBinding, raw []byte) error {
			loans++
			if loans != 1 {
				loanErr = fault(f.InvalidState)
				return loanErr
			}
			candidate, found, loanErr = consumeToolResultInTx(ctx, x, binding, actual, raw, write)
			return loanErr
		})
		if err != nil {
			return portError(err)
		}
		if loans != 1 || loanErr != nil {
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

func consumeToolResultInTx(ctx context.Context, x postgres.SQLExecutor, expected, actual tc.ReadFileResultBinding, raw []byte, write bool) (*toolResultConsumption, bool, error) {
	expectedJSON, err := toolResultBindingJSON(expected)
	if err != nil {
		return nil, false, err
	}
	actualJSON, err := toolResultBindingJSON(actual)
	if err != nil || !bytes.Equal(expectedJSON, actualJSON) || !sameToolCallBinding(expected.Call, actual.Call) {
		return nil, false, fault(f.ConfirmationStale)
	}
	projection, err := agentloop.ProjectReadFileToolResult(ctx, actual, raw)
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
	canonical, err := toolBinding(stored)
	if err != nil || !sameToolCallBinding(b, canonical) || row == nil || row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil {
		return nil, false, fault(f.ConfirmationStale)
	}
	previous, err := loadToolResultConsumption(ctx, x, actual, actualJSON, projection, row.summary.Version)
	if err != nil || previous != nil || !write {
		return previous, previous != nil, err
	}
	return insertToolResultConsumption(ctx, x, sealedToolResultBinding(actual), actualJSON, projection, row, stored)
}

func insertToolResultConsumption(ctx context.Context, x postgres.SQLExecutor, actual sealedToolResultBinding, actualJSON []byte, projection agentloop.ToolResultProjection, row *executionRecord, stored *directTextRecord) (*toolResultConsumption, bool, error) {
	b := actual.Call
	if row.summary.Version == math.MaxInt64 || row.summary.Version < stored.toolPending.version {
		return nil, false, fault(f.InvalidState)
	}
	at, err := f.NewInstant(time.Now())
	if err != nil {
		return nil, false, err
	}
	// Preserve monotonic checkpoint time even if the wall clock moves backward.
	if at.Time().Before(stored.toolPending.at.Time()) {
		at = stored.toolPending.at
	}
	round, err := f.ParseID[c.Round](b.RoundID)
	if err != nil {
		return nil, false, invalid()
	}
	v := &toolResultConsumption{binding: actualJSON, message: projection.CanonicalBytes(), receipt: c.ToolResultConsumptionReceipt{
		ExecutionID: b.ExecutionID, RoundID: round, CallID: b.CallID, OperationID: actual.OperationID, AttemptID: actual.AttemptID,
		Version: row.summary.Version + 1, TranscriptThrough: 3, MessageDigest: projection.Digest(), ResultDigest: actual.ResultDigest, CreatedAt: at,
	}}
	if v.receipt.Validate() != nil {
		return nil, false, fault(f.InvalidState)
	}
	if err = insertDirectTextEntry(ctx, x, b.ExecutionID, round, 3, "tool_result", projection.Message(), at); err != nil {
		return v, false, err
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.tool_result_consumptions(execution_id,round_id,invocation_id,call_id,operation_id,attempt_id,request_id,input_digest,fingerprint,terminal_digest,result_digest,result_bytes,binding,binding_digest,message_digest,transcript_sequence,checkpoint_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,3,$16,$17)`, b.ExecutionID.String(), b.RoundID, b.InvocationID.String(), b.CallID, actual.OperationID.String(), actual.AttemptID.String(), string(actual.RequestID), string(actual.InputDigest), string(actual.Fingerprint), string(actual.TerminalDigest), string(actual.ResultDigest), int64(actual.ResultBytes), v.binding, string(c.TriggerInputDigest(v.binding)), string(v.receipt.MessageDigest), int64(v.receipt.Version), at.Time())
	if err != nil {
		return v, false, unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return v, false, unavailable(nil)
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_execution.executions SET version=version+1,updated_at=GREATEST($3,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='running' AND cancel_requested_at IS NULL`, b.ExecutionID.String(), int64(row.summary.Version), at.Time())
	if err != nil {
		return v, false, unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return v, false, fault(f.ConfirmationStale)
	}
	return v, true, ctx.Err()
}

func loadToolResultConsumption(ctx context.Context, x postgres.SQLExecutor, b tc.ReadFileResultBinding, binding []byte, projection agentloop.ToolResultProjection, current f.Version) (*toolResultConsumption, error) {
	return loadSealedToolResultConsumption(ctx, x, sealedToolResultBinding(b), binding, projection, current)
}

func loadSealedToolResultConsumption(ctx context.Context, x postgres.SQLExecutor, b sealedToolResultBinding, binding []byte, projection agentloop.ToolResultProjection, current f.Version) (*toolResultConsumption, error) {
	if b.validate() != nil {
		return nil, invalid()
	}
	var round, invocation, call, operation, attempt, request, input, fingerprint, terminal, result, bindingDigest, messageDigest string
	var resultBytes, sequence, version int64
	var raw []byte
	var created time.Time
	err := x.QueryRow(ctx, `SELECT round_id::text,invocation_id::text,call_id,operation_id::text,attempt_id::text,request_id::text,input_digest,fingerprint,terminal_digest,result_digest,result_bytes,binding,binding_digest,message_digest,transcript_sequence,checkpoint_version,created_at FROM agenteam_execution.tool_result_consumptions WHERE execution_id=$1`, b.Call.ExecutionID.String()).Scan(&round, &invocation, &call, &operation, &attempt, &request, &input, &fingerprint, &terminal, &result, &resultBytes, &raw, &bindingDigest, &messageDigest, &sequence, &version, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if round != b.Call.RoundID || invocation != b.Call.InvocationID.String() || call != b.Call.CallID || operation != b.OperationID.String() || attempt != b.AttemptID.String() || request != string(b.RequestID) || input != string(b.InputDigest) || fingerprint != string(b.Fingerprint) || terminal != string(b.TerminalDigest) || result != string(b.ResultDigest) || resultBytes != int64(b.ResultBytes) || !bytes.Equal(raw, binding) || bindingDigest != string(c.TriggerInputDigest(raw)) || messageDigest != string(projection.Digest()) || sequence != 3 || current < f.Version(version) {
		return nil, fault(f.ConfirmationStale)
	}
	roundID, err := f.ParseID[c.Round](round)
	if err != nil {
		return nil, unavailable(nil)
	}
	at, err := f.NewInstant(created)
	if err != nil {
		return nil, unavailable(nil)
	}
	v := &toolResultConsumption{receipt: c.ToolResultConsumptionReceipt{ExecutionID: b.Call.ExecutionID, RoundID: roundID, CallID: call,
		OperationID: b.OperationID, AttemptID: b.AttemptID, Version: f.Version(version), TranscriptThrough: 3,
		MessageDigest: projection.Digest(), ResultDigest: b.ResultDigest, CreatedAt: at}, binding: raw, message: projection.CanonicalBytes()}
	if v.receipt.Validate() != nil {
		return nil, unavailable(nil)
	}
	if err = checkDirectTextEntry(ctx, x, b.Call.ExecutionID, roundID, 3, "tool_result", projection.Message(), at); err != nil {
		return nil, err
	}
	return v, ctx.Err()
}
