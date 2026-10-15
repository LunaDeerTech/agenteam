package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

func loadBuiltinRecord(ctx context.Context, x postgres.SQLExecutor, call tc.ToolCallBinding) (*builtinRecord, error) {
	var r builtinRecord
	var id, project, agent, execution, logical, invocation, providerCall, tool, kind, attempt, request string
	var revision f.Version
	var raw []byte
	err := x.QueryRow(ctx, `SELECT o.operation_id,o.project_id,o.agent_id,o.execution_id,o.logical_call_id,o.invocation_id,o.call_id,o.tool_id,o.spec_revision,o.input_data,o.input_digest,o.fingerprint,o.phase,o.version,o.operation_kind,COALESCE(o.active_attempt_id::text,''),COALESCE(a.backend_request_id::text,'') FROM agenteam_tool.operations o LEFT JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id WHERE o.execution_id=$1 AND o.logical_call_id=$2 AND o.invocation_id=$3 AND o.call_id=$4`, call.ExecutionID.String(), call.LogicalCallID.String(), call.InvocationID.String(), call.CallID).Scan(&id, &project, &agent, &execution, &logical, &invocation, &providerCall, &tool, &revision, &raw, &r.InputDigest, &r.Fingerprint, &r.Phase, &r.Version, &kind, &attempt, &request)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	r.ID, err = f.ParseID[tc.Operation](id)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	r.Input, err = decodeBuiltinInput(raw)
	if err != nil {
		return nil, err
	}
	if attempt != "" {
		r.Attempt, err = f.ParseID[tc.Attempt](attempt)
		if err != nil {
			return nil, fail(f.InvalidState)
		}
		r.Request = p.ID(request)
	}
	expectedInput, e := encode(bindingProjection(call))
	if e != nil || !bytes.Equal(raw, expectedInput) || project != call.ProjectID.String() || agent != call.AgentID.String() || execution != call.ExecutionID.String() ||
		logical != call.LogicalCallID.String() || invocation != call.InvocationID.String() || providerCall != call.CallID || tool != call.Spec.ToolID.String() || revision != call.Spec.SpecRevision || kind != directOperationKind(r.Input) {
		return nil, fail(f.Forbidden)
	}
	if err = r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

func startBuiltinRecord(ctx context.Context, x postgres.SQLExecutor, r builtinRecord, process oc.ProcessID) error {
	if r.Phase != "created" || r.Version != 1 || r.ID.Validate() != nil || r.Attempt.Validate() != nil || !r.Request.Valid() || process.Validate() != nil {
		return fail(f.InvalidState)
	}
	raw, err := encode(r.Input)
	if err != nil {
		return err
	}
	b := r.Input
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.operations(operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,backend_key,fingerprint,phase,version,operation_kind) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULL,NULL,$12,'created',1,$13)`, r.ID.String(), b.Project.String(), b.Agent.String(), b.Execution.String(), b.LogicalCall.String(), b.Invocation.String(), b.Call, b.Spec.ToolID.String(), int64(b.Spec.SpecRevision), raw, string(r.InputDigest), string(r.Fingerprint), directOperationKind(b))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	tag, err = x.Exec(ctx, `INSERT INTO agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id,ordinal,process_id,fence,backend_request_id,phase) VALUES($1,$2,$3,$4,$5,1,$6,1,$7,'running')`, r.Attempt.String(), r.ID.String(), b.Execution.String(), b.Project.String(), b.Agent.String(), process.String(), string(r.Request))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase='running',version=2,active_attempt_id=$2 WHERE operation_id=$1 AND phase='created' AND version=1`, r.ID.String(), r.Attempt.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	return nil
}

// readBuiltinAttemptProcess observes the original attempt identity without
// granting authority to use it. Its caller must already hold the original
// operation/input locks and validate the complete persisted binding.
func readBuiltinAttemptProcess(ctx context.Context, x postgres.SQLExecutor, r builtinRecord) (oc.ProcessID, error) {
	if r.validate() != nil || r.Phase != "running" {
		return oc.ProcessID{}, fail(f.InvalidState)
	}
	var processID, request, input, fingerprint string
	var ordinal, fence int64
	err := x.QueryRow(ctx, `SELECT a.process_id,a.backend_request_id,a.ordinal,a.fence,o.input_digest,o.fingerprint FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id AND a.execution_id=o.execution_id AND a.project_id=o.project_id AND a.agent_id=o.agent_id WHERE o.operation_id=$1 AND o.active_attempt_id=$2 AND o.execution_id=$3 AND o.operation_kind=$4 AND o.phase='running' AND o.version=2 AND a.phase='running' AND NOT a.retired`, r.ID.String(), r.Attempt.String(), r.Input.Execution.String(), directOperationKind(r.Input)).Scan(&processID, &request, &ordinal, &fence, &input, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return oc.ProcessID{}, fail(f.ConfirmationStale)
	}
	if err != nil {
		return oc.ProcessID{}, portError(err)
	}
	process, err := f.ParseID[oc.Process](processID)
	if err != nil || request != string(r.Request) || ordinal != 1 || fence != 1 || input != string(r.InputDigest) || fingerprint != string(r.Fingerprint) {
		return oc.ProcessID{}, fail(f.Forbidden)
	}
	return process, ctx.Err()
}

func requireBuiltinAttempt(ctx context.Context, x postgres.SQLExecutor, process oc.ProcessID, r builtinRecord, cold ...*builtinColdProof) error {
	if len(cold) > 1 {
		return fail(f.Forbidden)
	}
	expected := process
	if len(cold) == 1 && cold[0] != nil {
		if !cold[0].permits(process, r) {
			return fail(f.Forbidden)
		}
		expected = cold[0].process
	}
	actual, err := readBuiltinAttemptProcess(ctx, x, r)
	if err != nil {
		return err
	}
	if actual != expected {
		return fail(f.Forbidden)
	}
	return nil
}

func finishBuiltinRecord(ctx context.Context, x postgres.SQLExecutor, process oc.ProcessID, r builtinRecord, terminal builtinTerminal, body []byte, cold ...*builtinColdProof) error {
	if err := terminal.validate(r, body); err != nil {
		return err
	}
	if err := requireBuiltinAttempt(ctx, x, process, r, cold...); err != nil {
		return err
	}
	raw, err := encode(terminal)
	if err != nil {
		return err
	}
	b := r.Input
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.builtin_results(operation_id,attempt_id,execution_id,project_id,agent_id,request_id,body_sha256,body_bytes,body_data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.ID.String(), r.Attempt.String(), b.Execution.String(), b.Project.String(), b.Agent.String(), string(r.Request), string(terminal.ResultDigest), int64(terminal.ResultBytes), body)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	phase, outcome := terminal.phases()
	tag, err = x.Exec(ctx, `UPDATE agenteam_tool.attempts SET phase=$2,completed_at=clock_timestamp(),retired=true,outcome_data=$3 WHERE attempt_id=$1 AND operation_id=$4 AND phase='running' AND NOT retired`, r.Attempt.String(), terminal.attemptPhase(), raw, r.ID.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase=$2,version=3,completed_at=clock_timestamp(),outcome=$3,terminal_data=$4 WHERE operation_id=$1 AND operation_kind=$5 AND phase='running' AND version=2 AND active_attempt_id=$6`, r.ID.String(), phase, outcome, raw, directOperationKind(b), r.Attempt.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	return nil
}

func loadBuiltinTerminal(ctx context.Context, x postgres.SQLExecutor, r builtinRecord) (builtinTerminal, []byte, []byte, error) {
	var terminal builtinTerminal
	if r.validate() != nil || r.Version != 3 {
		return terminal, nil, nil, fail(f.InvalidState)
	}
	var raw, attemptRaw, body []byte
	var request, outcome, phase, project, agent, execution string
	var sha f.Digest
	var size, ordinal, fence int64
	var retired bool
	err := x.QueryRow(ctx, `SELECT o.terminal_data,o.outcome,a.outcome_data,a.backend_request_id,a.phase,a.retired,a.ordinal,a.fence,r.project_id,r.agent_id,r.execution_id,r.body_sha256,r.body_bytes,r.body_data FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id AND a.operation_id=o.operation_id JOIN agenteam_tool.builtin_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id AND r.request_id=a.backend_request_id WHERE o.operation_id=$1 AND o.active_attempt_id=$2`, r.ID.String(), r.Attempt.String()).Scan(&raw, &outcome, &attemptRaw, &request, &phase, &retired, &ordinal, &fence, &project, &agent, &execution, &sha, &size, &body)
	if err != nil {
		return terminal, nil, nil, portError(err)
	}
	if !retired || ordinal != 1 || fence != 1 || !bytes.Equal(raw, attemptRaw) || len(raw) > 32768 || json.Unmarshal(raw, &terminal) != nil || terminal.validate(r, body) != nil ||
		request != string(r.Request) || project != r.Input.Project.String() || agent != r.Input.Agent.String() || execution != r.Input.Execution.String() || sha != terminal.ResultDigest || size != int64(terminal.ResultBytes) {
		return terminal, nil, nil, fail(f.InvalidState)
	}
	expectedPhase, expectedOutcome := terminal.phases()
	canonical, err := encode(terminal)
	if err != nil || !bytes.Equal(raw, canonical) || phase != terminal.attemptPhase() || r.Phase != expectedPhase || outcome != expectedOutcome {
		return terminal, nil, nil, fail(f.InvalidState)
	}
	return terminal, body, raw, nil
}
