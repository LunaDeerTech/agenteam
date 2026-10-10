package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

// Do not marshal InstallReceipt itself: its default JSON is deliberately safe.
// This exact private projection preserves the original full committed receipt.
type installReceiptData struct {
	Installation skill.InstallationID
	Skill        sc.SkillID
	Project      id.ProjectID
	RevisionID   sc.RevisionID
	Revision     f.Revision
	Version      f.Version
	Object       oc.ObjectID
	Package      f.Digest
}

func projectInstallReceipt(r skill.InstallReceipt) installReceiptData {
	return installReceiptData{r.InstallationID, r.SkillID, r.ProjectID, r.RevisionID, r.Revision, r.Version, r.ObjectID, r.PackageSHA256}
}
func (r installReceiptData) receipt() skill.InstallReceipt {
	return skill.InstallReceipt{InstallationID: r.Installation, SkillID: r.Skill, ProjectID: r.Project, RevisionID: r.RevisionID, Revision: r.Revision, Version: r.Version, ObjectID: r.Object, PackageSHA256: r.Package}
}

type installTerminal struct {
	Format    int
	Operation tc.OperationID
	Attempt   tc.AttemptID
	Status    string
	Category  string
	Known     bool
	Receipt   *installReceiptData
	Output    json.RawMessage
	Unknown   *installCommitProvenance
}

// Exact safe transaction lookup material, never the original error string.
// CommandIdentity has intentionally safe default JSON; its explicit canonical
// projection is necessary here to retain the real same-key recovery identity.
type installCommitProvenance struct {
	Attempt                             f.ID[f.TransactionAttempt]
	Kind                                f.CauseKind
	Primary                             string
	Related                             []string
	JobType, JobID, JobAttemptID        string
	EventID, HandlerName                string
	Owner, RecoveryRunID, CheckpointRef string
}

func installUnknown(err error) *installCommitProvenance {
	r, ok := skill.UnknownAttempt(err)
	if !ok {
		var own CommitFailure
		if errors.As(err, &own) {
			r, ok = own.Result(), true
		}
	}
	if !ok || r.State() != f.Unknown || r.AttemptID().Validate() != nil || r.Cause().Validate() != nil {
		return nil
	}
	d := r.Cause().Details()
	v := &installCommitProvenance{Attempt: r.AttemptID(), Kind: d.Kind, Primary: d.Primary.Canonical(), JobType: d.JobType, JobID: d.JobID, JobAttemptID: d.JobAttemptID, EventID: d.EventID, HandlerName: d.HandlerName, Owner: d.Owner, RecoveryRunID: d.RecoveryRunID, CheckpointRef: d.CheckpointRef}
	for _, command := range d.Related {
		v.Related = append(v.Related, command.Canonical())
	}
	return v
}

func (v installTerminal) clone() installTerminal {
	v.Output = bytes.Clone(v.Output)
	if v.Receipt != nil {
		copy := *v.Receipt
		v.Receipt = &copy
	}
	if v.Unknown != nil {
		copy := *v.Unknown
		copy.Related = append([]string(nil), copy.Related...)
		v.Unknown = &copy
	}
	return v
}

func (v installTerminal) validate(h *installHandoff) error {
	if v.Format != 1 || v.Operation != h.record.ID || v.Attempt != h.attempt || len(v.Output) > 256 {
		return fail(f.InvalidState)
	}
	if v.Receipt != nil {
		r := v.Receipt.receipt()
		if r.Validate() != nil || r.ProjectID != h.binding.ProjectID || r.SkillID != h.record.SkillID || r.PackageSHA256 != h.record.Input.PackageDigest {
			return fail(f.InvalidState)
		}
	}
	if v.Status == "success" {
		if !v.Known || v.Category != "" || v.Receipt == nil || !json.Valid(v.Output) {
			return fail(f.InvalidState)
		}
		return nil
	}
	if v.Status != "error" || len(v.Output) != 0 {
		return fail(f.InvalidState)
	}
	switch v.Category {
	case "unknown_outcome":
		if v.Known {
			return fail(f.InvalidState)
		}
	case "backend_contract_violation", "authorization_denied", "capability_unsupported", "not_found", "conflict", "invalid_arguments", "backend_unavailable", "business_rule_violation":
	default:
		return fail(f.InvalidState)
	}
	return nil
}

func startInstallAttempt(ctx context.Context, d *installAuthorityState, h *installHandoff, schemas InstallSchemaValidator, raw []byte) f.CommitResult {
	store := d.store
	return store.WithinTx(ctx, h.cause, func(ctx context.Context, tx f.Tx) error {
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if !h.boundExecutor(d) {
			return fail(f.Forbidden)
		}
		if _, err = h.executor.core.data().store.InTx(tx); err != nil {
			return portError(err)
		}
		if err = store.AcquireAll(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		if err = store.RequireHeldLocks(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		process, err := d.guard.CurrentProcess()
		if err != nil || process != d.process {
			return fail(f.DependencyUnavailable)
		}
		if err = h.executor.permission.AuthorizeInstallInTx(ctx, tx, h.input, h.permission); err != nil {
			return portError(err)
		}
		if err = schemas.ValidateInstallInputInTx(ctx, tx, h.binding.Spec, raw); err != nil {
			return portError(err)
		}
		row, err := loadOperation(ctx, x, h.binding)
		if err != nil {
			return err
		}
		if row == nil || row.ID != h.record.ID || row.InputDigest != h.record.InputDigest || row.Fingerprint != h.record.Fingerprint || row.SkillID != h.record.SkillID {
			return fail(f.ConfirmationStale)
		}
		b := h.binding
		// running is a dispatch intent, not evidence of Backend I/O. A physical
		// unknown commit here cannot execute or be retried as a new attempt.
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id,ordinal,process_id,fence,backend_request_id,phase) VALUES($1,$2,$3,$4,$5,1,$6,1,$7,'running')`, h.attempt.String(), row.ID.String(), b.ExecutionID.String(), b.ProjectID.String(), b.AgentID.String(), d.process.String(), h.request.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		tag, err = x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase='running',version=2,active_attempt_id=$2 WHERE operation_id=$1 AND phase='created' AND version=1 AND active_attempt_id IS NULL`, row.ID.String(), h.attempt.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ResourceBusy)
		}
		return ctx.Err()
	})
}

func requireRunningInstall(ctx context.Context, x postgres.SQLExecutor, d *installAuthorityState, h *installHandoff) error {
	var process, request, input, fingerprint string
	var ordinal, fence int64
	var version f.Version
	b := h.binding
	err := x.QueryRow(ctx, `SELECT a.process_id,a.backend_request_id,a.ordinal,a.fence,o.version,o.input_digest,o.fingerprint FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id AND a.execution_id=o.execution_id AND a.project_id=o.project_id AND a.agent_id=o.agent_id WHERE o.operation_id=$1 AND o.execution_id=$2 AND o.project_id=$3 AND o.agent_id=$4 AND o.active_attempt_id=$5 AND o.phase='running' AND a.phase='running' AND NOT a.retired`, h.record.ID.String(), b.ExecutionID.String(), b.ProjectID.String(), b.AgentID.String(), h.attempt.String()).Scan(&process, &request, &ordinal, &fence, &version, &input, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(f.ConfirmationStale)
	}
	if err != nil {
		return portError(err)
	}
	if process != d.process.String() || request != h.request.String() || ordinal != 1 || fence != 1 || version != 2 || input != string(h.record.InputDigest) || fingerprint != string(h.record.Fingerprint) {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}

func finishInstallAttempt(ctx context.Context, d *installAuthorityState, h *installHandoff, schemas InstallSchemaValidator, terminal *installTerminal) f.CommitResult {
	store := d.store
	return store.WithinTx(ctx, h.cause, func(ctx context.Context, tx f.Tx) error {
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if !h.boundExecutor(d) {
			return fail(f.Forbidden)
		}
		if _, err = h.executor.core.data().store.InTx(tx); err != nil {
			return portError(err)
		}
		if err = store.AcquireAll(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		if err = store.RequireHeldLocks(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		process, err := d.guard.CurrentProcess()
		if err != nil || process != d.process {
			return fail(f.DependencyUnavailable)
		}
		// Recording an already-returned result is not another permission to
		// execute. Revocation cannot erase the actual committed receipt.
		if err = requireRunningInstall(ctx, x, d, h); err != nil {
			return err
		}
		if terminal.Status == "success" {
			if err = schemas.ValidateInstallOutputInTx(ctx, tx, h.binding.Spec, terminal.Output); err != nil {
				terminal.Status, terminal.Category = "error", "backend_contract_violation"
				terminal.Output = nil
				// Preserve the complete known receipt even if model output cannot
				// satisfy the original declared schema. No raw error is persisted.
			}
		}
		if err = terminal.validate(h); err != nil {
			return err
		}
		raw, err := encode(terminal)
		if err != nil {
			return err
		}
		attemptPhase, operationPhase, outcome := "failed", "failed", "error"
		if terminal.Status == "success" {
			attemptPhase, operationPhase, outcome = "succeeded", "succeeded", "success"
		} else if !terminal.Known {
			attemptPhase, outcome = "unknown", "unknown"
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_tool.attempts SET phase=$2,completed_at=clock_timestamp(),retired=true,outcome_data=$3 WHERE attempt_id=$1 AND phase='running' AND NOT retired`, h.attempt.String(), attemptPhase, raw)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		tag, err = x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase=$2,version=3,completed_at=clock_timestamp(),outcome=$3,terminal_data=$4 WHERE operation_id=$1 AND phase='running' AND version=2 AND active_attempt_id=$5`, h.record.ID.String(), operationPhase, outcome, raw, h.attempt.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		return ctx.Err()
	})
}
