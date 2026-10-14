package runtime

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (e *RunCommandExecutor) ReserveApprovalOperation(ctx context.Context, b tc.ToolCallBinding) (tc.ApprovalOperation, error) {
	var zero tc.ApprovalOperation
	if e == nil || e.data == nil {
		return zero, fail(f.DependencyUnbound)
	}
	ctx, err := exactRunnerBinding(ctx, b)
	if err != nil {
		return zero, err
	}
	d := e.data()
	ctx, done, err := d.authority.beginLookup(ctx)
	if err != nil {
		return zero, err
	}
	defer done()
	h, err := discoverRunCommand(ctx, d, b.ExecutionID)
	if err != nil {
		return zero, err
	}
	defer clearRunCommandArguments(h)
	var old *runCommandOperationRecord
	_, err = runCommandTransaction(ctx, d.authority, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeRunCommandInput(ctx, tx, d.authority, h, func(ctx context.Context, _ []byte) error {
			var err error
			old, err = loadRunCommandOperation(ctx, x, runCommandBindingProjection(b))
			return err
		})
	})
	if err != nil {
		return zero, err
	}
	operation, err := f.NewID[tc.Operation]()
	if err != nil {
		return zero, portError(err)
	}
	if old != nil {
		operation = old.ID
	}
	if err = bindRunCommandOperationLock(h, operation); err != nil {
		return zero, err
	}
	_, err = runCommandTransaction(ctx, d.authority, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeRunCommandInput(ctx, tx, d.authority, h, func(ctx context.Context, raw []byte) error {
			current, err := loadRunCommandOperation(ctx, x, runCommandBindingProjection(b))
			if err != nil {
				return err
			}
			if current != nil {
				if current.ID != operation || current.Phase != "created" {
					return fail(f.ConfirmationStale)
				}
				h.record = *current
				return nil
			}
			if old != nil {
				return fail(f.ConfirmationStale)
			}
			if err = d.schemas.ValidateRunCommandInputInTx(ctx, tx, b.Spec, raw); err != nil {
				return err
			}
			target, err := d.mounts.CurrentTargetInTx(ctx, tx, mc.CommandTargetRequest{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, MountID: h.arguments.MountID})
			if err != nil {
				return portError(err)
			}
			input := runCommandInput{Format: 1, Binding: runCommandBindingProjection(b), Operation: b.RunnerBinding.OperationName, Revision: b.RunnerBinding.OperationRevision, ProtocolMajor: int(b.RunnerBinding.ProtocolMajor), Target: runCommandTargetProjection(target)}
			rawInput, err := encodeRunCommandInput(input)
			if err != nil {
				return err
			}
			fingerprint, err := runCommandFingerprint(input)
			if err != nil {
				return err
			}
			h.record = runCommandOperationRecord{ID: operation, Input: input, InputDigest: digest(rawInput), Fingerprint: fingerprint, Phase: "created", Version: 1}
			return insertRunCommandOperation(ctx, x, h.record)
		})
	})
	if err != nil {
		return zero, err
	}
	return tc.ApprovalOperation{Binding: b, OperationID: h.record.ID, Fingerprint: h.record.Fingerprint}, nil
}
func (e *RunCommandExecutor) RequireApprovalOperationInTx(ctx context.Context, tx f.Tx, v tc.ApprovalOperation) error {
	if e == nil || e.data == nil {
		return fail(f.DependencyUnbound)
	}
	if ctx == nil || v.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	d := e.data().authority
	x, err := d.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = d.store.RequireHeldLocks(ctx, tx, v.RequiredLocks()); err != nil {
		return portError(err)
	}
	row, err := loadRunCommandOperation(ctx, x, runCommandBindingProjection(v.Binding))
	if err != nil {
		return err
	}
	if row == nil || row.ID != v.OperationID || row.Fingerprint != v.Fingerprint || (row.Phase != "created" && row.Phase != "running") {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}

// RequireUnattemptedApprovalOperationInTx proves a durable wait has never
// reached an Attempt, including a locally started attempt with no RPC yet.
// Unlike the final authorization gate it cannot accept a running operation.
func (e *RunCommandExecutor) RequireUnattemptedApprovalOperationInTx(ctx context.Context, tx f.Tx, v tc.ApprovalOperation) error {
	if err := e.RequireApprovalOperationInTx(ctx, tx, v); err != nil {
		return err
	}
	x, err := e.data().authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	var unattempted bool
	err = x.QueryRow(ctx, `SELECT o.phase='created' AND o.version=1 AND o.active_attempt_id IS NULL AND NOT EXISTS(SELECT 1 FROM agenteam_tool.attempts a WHERE a.operation_id=o.operation_id) FROM agenteam_tool.operations o WHERE o.operation_id=$1`, v.OperationID.String()).Scan(&unattempted)
	if err != nil {
		return portError(err)
	}
	if !unattempted {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (e *RunCommandExecutor) RejectApprovalOperationInTx(ctx context.Context, tx f.Tx, v tc.ApprovalOperation, request string) error {
	if _, err := f.ParseID[struct{}](request); err != nil {
		return fail(f.InvalidArgument)
	}
	if err := e.RequireApprovalOperationInTx(ctx, tx, v); err != nil {
		return err
	}
	d := e.data().authority
	x, err := d.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadRunCommandOperation(ctx, x, runCommandBindingProjection(v.Binding))
	if err != nil {
		return err
	}
	if row == nil || row.Phase != "created" {
		return fail(f.ConfirmationStale)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_tool.approval_denials(operation_id,request_id,fingerprint) VALUES($1,$2,$3)`, v.OperationID.String(), request, string(v.Fingerprint))
	if err != nil {
		return portError(err)
	}
	raw, _ := json.Marshal(struct {
		Kind      string `json:"kind"`
		Request   string `json:"request_id"`
		Available bool   `json:"report_available"`
	}{"approval_rejected", request, false})
	tag, err := x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase='failed',version=2,outcome='error',completed_at=GREATEST(clock_timestamp(),created_at),terminal_data=$2 WHERE operation_id=$1 AND phase='created' AND version=1 AND active_attempt_id IS NULL`, v.OperationID.String(), raw)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}
func (e *RunCommandExecutor) RequireRejectedOperationInTx(ctx context.Context, tx f.Tx, v tc.ApprovalOperation, request string) error {
	if e == nil || e.data == nil || ctx == nil || v.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	if _, err := f.ParseID[struct{}](request); err != nil {
		return fail(f.InvalidArgument)
	}
	d := e.data().authority
	x, err := d.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = d.store.RequireHeldLocks(ctx, tx, v.RequiredLocks()); err != nil {
		return portError(err)
	}
	r, err := loadRunCommandOperation(ctx, x, runCommandBindingProjection(v.Binding))
	if err != nil {
		return err
	}
	if r == nil || r.ID != v.OperationID || r.Fingerprint != v.Fingerprint {
		return fail(f.ConfirmationStale)
	}
	return requireRunCommandDenial(ctx, x, *r, request)
}
func requireRunCommandDenial(ctx context.Context, x postgres.SQLExecutor, r runCommandOperationRecord, request string) error {
	if r.Phase != "failed" || r.Version != 2 || r.Attempt != (tc.AttemptID{}) {
		return fail(f.ConfirmationStale)
	}
	var id, fingerprint string
	var raw []byte
	var count int64
	err := x.QueryRow(ctx, `SELECT d.request_id,d.fingerprint,o.terminal_data,(SELECT count(*) FROM agenteam_tool.attempts a WHERE a.operation_id=o.operation_id) FROM agenteam_tool.approval_denials d JOIN agenteam_tool.operations o ON o.operation_id=d.operation_id WHERE d.operation_id=$1`, r.ID.String()).Scan(&id, &fingerprint, &raw, &count)
	if err != nil {
		return portError(err)
	}
	expected, _ := json.Marshal(struct {
		Kind      string `json:"kind"`
		Request   string `json:"request_id"`
		Available bool   `json:"report_available"`
	}{"approval_rejected", id, false})
	if fingerprint != string(r.Fingerprint) || request != "" && request != id || count != 0 || string(raw) != string(expected) {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}
