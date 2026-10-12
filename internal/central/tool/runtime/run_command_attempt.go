package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// Report availability is explicit and independently checked against immutable
// report/stream storage. Generic wire success means a completed invocation;
// only our separate classification establishes Tool success.
type runCommandTerminal struct {
	Format          int
	Operation       tc.OperationID
	Attempt         tc.AttemptID
	Request         p.ID
	Outcome         string
	Code            string
	Attempted       bool
	ReportAvailable bool `json:"report_available"`
	Result          *runCommandResultReference
}

func (v runCommandTerminal) phases() (operation, attempt, outcome string) {
	switch v.Outcome {
	case "success":
		return "succeeded", "succeeded", "success"
	case "failure":
		return "failed", "failed", "error"
	case "cancelled":
		return "cancelled", "cancelled", "cancelled"
	case "unknown":
		return "failed", "unknown", "unknown"
	}
	return "", "", ""
}
func (v runCommandTerminal) validate(r runCommandOperationRecord) error {
	if v.Format != 1 || v.Operation != r.ID || v.Attempt != r.Attempt || v.Attempt.Validate() != nil || !v.Request.Valid() || v.ReportAvailable != (v.Result != nil) {
		return fail(f.InvalidState)
	}
	if v.Result != nil && (!v.Attempted || v.Result.validate() != nil || v.Result.Operation != v.Operation || v.Result.Attempt != v.Attempt || v.Result.Request != v.Request) {
		return fail(f.InvalidState)
	}
	switch v.Outcome {
	case "success":
		if !v.Attempted || v.Code != "" || !v.ReportAvailable {
			return fail(f.InvalidState)
		}
	case "failure":
		if !v.Attempted {
			if v.Code != "not_dispatched" || v.ReportAvailable {
				return fail(f.InvalidState)
			}
		} else if v.ReportAvailable {
			if v.Code != "command_exit_nonzero" && v.Code != "command_signaled" && v.Code != "output_incomplete" && v.Code != string(p.InternalError) {
				return fail(f.InvalidState)
			}
		} else if p.Code(v.Code).SafeMessage() == "" || p.Code(v.Code) == p.CancelledCode {
			return fail(f.InvalidState)
		}
	case "cancelled":
		if !v.Attempted || (p.Code(v.Code) != p.CancelledCode && p.Code(v.Code) != p.Timeout) {
			return fail(f.InvalidState)
		}
	case "unknown":
		if !v.Attempted || v.ReportAvailable || (v.Code != "" && v.Code != "backend_contract_violation") {
			return fail(f.InvalidState)
		}
	default:
		return fail(f.InvalidState)
	}
	return nil
}

func terminalRunCommand(h *runCommandHandoff, observed runnerhttp.CommandDispatchResult, dispatchErr error) (runCommandTerminal, []byte, []byte, []byte) {
	v := runCommandTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Request: h.request, Outcome: "unknown", Attempted: observed.Attempted}
	if !observed.Attempted && observed.Terminal == nil {
		v.Outcome, v.Code = "failure", "not_dispatched"
		return v, nil, nil, nil
	}
	v.Attempted = true
	r := observed.Terminal
	if r == nil {
		return v, nil, nil, nil
	}
	if dispatchErr != nil || r.RequestID != h.request || r.OperationID != p.ID(h.record.ID.String()) {
		v.Code = "backend_contract_violation"
		return v, nil, nil, nil
	}
	switch r.Outcome {
	case p.Success:
		if r.Code != nil || r.SafeMessage != nil {
			break
		}
		decoded, err := p.DecodeRunCommandResult(r.Payload)
		if err != nil {
			break
		}
		ref, raw, err := encodeRunCommandStoredResult(h.record.ID, h.attempt, h.request, decoded, observed.Stdout, observed.Stderr)
		if err != nil {
			clear(raw)
			break
		}
		v.Outcome, v.Code = classifyRunCommandResult(decoded)
		v.ReportAvailable, v.Result = true, &ref
		return v, raw, slices.Clone(observed.Stdout), slices.Clone(observed.Stderr)
	case p.Failure, p.Cancelled:
		if r.Code == nil || r.Code.SafeMessage() == "" || r.SafeMessage == nil || *r.SafeMessage != r.Code.SafeMessage() || r.Payload != nil || len(observed.Stdout) != 0 || len(observed.Stderr) != 0 {
			break
		}
		if r.Outcome == p.Failure && *r.Code == p.CancelledCode || r.Outcome == p.Cancelled && *r.Code != p.CancelledCode && *r.Code != p.Timeout {
			break
		}
		v.Outcome, v.Code = string(r.Outcome), string(*r.Code)
		return v, nil, nil, nil
	case p.Unknown:
		if r.Code == nil && r.Payload == nil && r.SafeMessage != nil && *r.SafeMessage == p.UnknownMessage {
			return v, nil, nil, nil
		}
	}
	v.Code = "backend_contract_violation"
	return v, nil, nil, nil
}

func startRunCommandAttempt(ctx context.Context, d *runCommandAuthorityState, h *runCommandHandoff, schemas *RegistrySchemaValidator) (f.CommitResult, error) {
	result, err := runCommandTransaction(ctx, d, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeRunCommandInput(ctx, tx, d, h, func(authorized context.Context, raw []byte) error {
			if err := schemas.ValidateRunCommandInputInTx(authorized, tx, h.binding.Spec, raw); err != nil {
				return err
			}
			row, err := loadRunCommandOperation(authorized, x, h.record.Input.Binding)
			if err != nil {
				return err
			}
			if row == nil || row.ID != h.record.ID || row.InputDigest != h.record.InputDigest || row.Fingerprint != h.record.Fingerprint || row.Phase != "created" || row.Version != 1 {
				return fail(f.ConfirmationStale)
			}
			b := h.binding
			tag, err := x.Exec(authorized, `INSERT INTO agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id,ordinal,process_id,fence,backend_request_id,phase) VALUES($1,$2,$3,$4,$5,1,$6,1,$7,'running')`, h.attempt.String(), row.ID.String(), b.ExecutionID.String(), b.ProjectID.String(), b.AgentID.String(), d.process.String(), string(h.request))
			if err != nil {
				return portError(err)
			}
			if tag.RowsAffected() != 1 {
				return fail(f.InvalidState)
			}
			tag, err = x.Exec(authorized, `UPDATE agenteam_tool.operations SET phase='running',version=2,active_attempt_id=$2 WHERE operation_id=$1 AND operation_kind='runner.run-command' AND phase='created' AND version=1 AND active_attempt_id IS NULL`, row.ID.String(), h.attempt.String())
			if err != nil {
				return portError(err)
			}
			if tag.RowsAffected() != 1 {
				return fail(f.ResourceBusy)
			}
			return nil
		})
	})
	if err == nil {
		h.record.Phase, h.record.Version, h.record.Attempt = "running", 2, h.attempt
	}
	return result, err
}

func requireRunCommandAttempt(ctx context.Context, x postgres.SQLExecutor, d *runCommandAuthorityState, h *runCommandHandoff) error {
	var process, request, input, fingerprint string
	var ordinal, fence int64
	var version f.Version
	b := h.binding
	err := x.QueryRow(ctx, `SELECT a.process_id,a.backend_request_id,a.ordinal,a.fence,o.version,o.input_digest,o.fingerprint FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id AND a.execution_id=o.execution_id AND a.project_id=o.project_id AND a.agent_id=o.agent_id WHERE o.operation_id=$1 AND o.execution_id=$2 AND o.project_id=$3 AND o.agent_id=$4 AND o.active_attempt_id=$5 AND o.operation_kind='runner.run-command' AND o.phase='running' AND a.phase='running' AND NOT a.retired`, h.record.ID.String(), b.ExecutionID.String(), b.ProjectID.String(), b.AgentID.String(), h.attempt.String()).Scan(&process, &request, &ordinal, &fence, &version, &input, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(f.ConfirmationStale)
	}
	if err != nil {
		return portError(err)
	}
	if process != d.process.String() || request != string(h.request) || ordinal != 1 || fence != 1 || version != 2 || input != string(h.record.InputDigest) || fingerprint != string(h.record.Fingerprint) {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}

func finishRunCommandAttempt(ctx context.Context, d *runCommandAuthorityState, h *runCommandHandoff, schemas *RegistrySchemaValidator, terminal *runCommandTerminal, payload, stdout, stderr []byte) (f.CommitResult, error) {
	candidate := *terminal
	publicationReady := false
	result, err := runCommandTransaction(ctx, d, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		// Recording the already joined original result is not execution admission;
		// current revocation must not erase an actual result or fake retirement.
		row, err := loadRunCommandOperation(ctx, x, h.record.Input.Binding)
		if err != nil {
			return err
		}
		if row == nil || row.ID != h.record.ID || row.InputDigest != h.record.InputDigest || row.Attempt != h.attempt {
			return fail(f.ConfirmationStale)
		}
		if row.Phase != "running" {
			observed, err := loadRunCommandTerminal(ctx, x, *row)
			if err != nil {
				return err
			}
			if observed.terminal == nil || !sameRunCommandTerminal(*terminal, *observed.terminal) {
				return fail(f.ConfirmationStale)
			}
			return nil
		}
		if err := requireRunCommandAttempt(ctx, x, d, h); err != nil {
			return err
		}
		candidate, err = runCommandOutputCandidate(ctx, tx, schemas, h.binding.Spec, *terminal, payload, stdout, stderr)
		if err != nil {
			return err
		}
		if err := candidate.validate(h.record); err != nil {
			return err
		}
		raw, err := encode(candidate)
		if err != nil {
			return err
		}
		opPhase, attemptPhase, outcome := candidate.phases()
		if candidate.Result != nil {
			complete := h.record
			complete.Phase, complete.Version = opPhase, 3
			if err = insertRunCommandStoredResult(ctx, x, complete, *candidate.Result, payload, stdout, stderr); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_tool.attempts SET phase=$2,completed_at=clock_timestamp(),retired=true,outcome_data=$3 WHERE attempt_id=$1 AND operation_id=$4 AND phase='running' AND NOT retired`, h.attempt.String(), attemptPhase, raw, h.record.ID.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		tag, err = x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase=$2,version=3,completed_at=clock_timestamp(),outcome=$3,terminal_data=$4 WHERE operation_id=$1 AND operation_kind='runner.run-command' AND phase='running' AND version=2 AND active_attempt_id=$5`, h.record.ID.String(), opPhase, outcome, raw, h.attempt.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		publicationReady = true
		return nil
	})
	// Unknown may already have published this exact candidate, so preserve it
	// for strict observation. A known rollback never changes the original.
	if publicationReady && result.State() != f.NotCommitted {
		*terminal = candidate
	}
	return result, err
}

func loadRunCommandTerminal(ctx context.Context, x postgres.SQLExecutor, r runCommandOperationRecord) (runCommandObservation, error) {
	out := runCommandObservation{record: r}
	if r.validate() != nil {
		return out, fail(f.InvalidState)
	}
	if r.Phase == "created" || r.Phase == "running" {
		return out, nil
	}
	var raw, attemptRaw []byte
	var request, phase, outcome string
	var retired bool
	var ordinal, fence int64
	err := x.QueryRow(ctx, `SELECT o.terminal_data,o.outcome,a.outcome_data,a.backend_request_id,a.phase,a.retired,a.ordinal,a.fence FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id AND a.execution_id=o.execution_id AND a.project_id=o.project_id AND a.agent_id=o.agent_id WHERE o.operation_id=$1 AND o.active_attempt_id=$2 AND o.execution_id=$3 AND o.project_id=$4 AND o.agent_id=$5 AND o.operation_kind='runner.run-command'`, r.ID.String(), r.Attempt.String(), r.Input.Binding.Execution.String(), r.Input.Binding.Project.String(), r.Input.Binding.Agent.String()).Scan(&raw, &outcome, &attemptRaw, &request, &phase, &retired, &ordinal, &fence)
	if err != nil {
		return out, portError(err)
	}
	if !retired || ordinal != 1 || fence != 1 || len(raw) < 2 || len(raw) > 32768 || !bytes.Equal(raw, attemptRaw) {
		return out, fail(f.InvalidState)
	}
	var terminal runCommandTerminal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&terminal) != nil || terminal.validate(r) != nil || string(terminal.Request) != request {
		return out, fail(f.InvalidState)
	}
	canonical, err := encode(terminal)
	if err != nil || !bytes.Equal(raw, canonical) {
		return out, fail(f.InvalidState)
	}
	opPhase, attemptPhase, wantOutcome := terminal.phases()
	if r.Phase != opPhase || phase != attemptPhase || outcome != wantOutcome {
		return out, fail(f.InvalidState)
	}
	out.terminal = &terminal
	if terminal.Result != nil {
		out.result, out.stdout, out.stderr, err = loadRunCommandStoredResult(ctx, x, r, *terminal.Result)
		if err != nil {
			return runCommandObservation{}, err
		}
		classified, code := classifyRunCommandResult(out.result)
		if terminal.Outcome != classified || terminal.Code != code {
			clear(out.stdout)
			clear(out.stderr)
			return runCommandObservation{}, fail(f.InvalidState)
		}
	}
	return out, nil
}
