package runtime

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// These repository functions are private to Tool. Their SQLExecutor is borrowed
// from the owner-validated original transaction; none starts a transaction or
// substitutes a persisted row for current Execution/Operation authorization.
func loadRunCommandOperation(ctx context.Context, x postgres.SQLExecutor, b runCommandBindingData) (*runCommandOperationRecord, error) {
	if b.validate() != nil {
		return nil, fail(f.InvalidState)
	}
	var r runCommandOperationRecord
	var operation, project, agent, execution, logical, invocation, call, tool, kind, skill, key, attempt string
	var revision int64
	var raw []byte
	err := x.QueryRow(ctx, `SELECT operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,COALESCE(skill_id::text,''),COALESCE(backend_key,''),fingerprint,phase,version,COALESCE(active_attempt_id::text,''),operation_kind FROM agenteam_tool.operations WHERE execution_id=$1 AND logical_call_id=$2 AND invocation_id=$3 AND call_id=$4`, b.Execution.String(), b.LogicalCall.String(), b.Invocation.String(), b.Call).Scan(&operation, &project, &agent, &execution, &logical, &invocation, &call, &tool, &revision, &raw, &r.InputDigest, &skill, &key, &r.Fingerprint, &r.Phase, &r.Version, &attempt, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	if kind != runCommandOperationKind || skill != "" || key != "" {
		return nil, fail(f.InvalidState)
	}
	r.Input, err = decodeRunCommandInput(raw)
	if err != nil {
		return nil, err
	}
	r.ID, err = f.ParseID[tc.Operation](operation)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	if attempt != "" {
		r.Attempt, err = f.ParseID[tc.Attempt](attempt)
		if err != nil {
			return nil, fail(f.InvalidState)
		}
	}
	v := r.Input.Binding
	if v != b || v.Project.String() != project || v.Agent.String() != agent || v.Execution.String() != execution ||
		v.LogicalCall.String() != logical || v.Invocation.String() != invocation || v.Call != call ||
		v.Spec.ToolID.String() != tool || int64(v.Spec.SpecRevision) != revision {
		return nil, fail(f.InvalidState)
	}
	if err := r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

func insertRunCommandOperation(ctx context.Context, x postgres.SQLExecutor, r runCommandOperationRecord) error {
	if r.validate() != nil || r.Phase != "created" {
		return fail(f.InvalidState)
	}
	raw, err := encodeRunCommandInput(r.Input)
	if err != nil {
		return err
	}
	b := r.Input.Binding
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.operations(operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,backend_key,fingerprint,phase,version,operation_kind) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULL,NULL,$12,'created',1,'runner.run-command')`, r.ID.String(), b.Project.String(), b.Agent.String(), b.Execution.String(), b.LogicalCall.String(), b.Invocation.String(), b.Call, b.Spec.ToolID.String(), int64(b.Spec.SpecRevision), raw, string(r.InputDigest), string(r.Fingerprint))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	return nil
}

func insertRunCommandStoredResult(ctx context.Context, x postgres.SQLExecutor, r runCommandOperationRecord, ref runCommandResultReference, raw, stdout, stderr []byte) error {
	if r.validate() != nil || r.Phase != "succeeded" && r.Phase != "failed" && r.Phase != "cancelled" {
		return fail(f.InvalidState)
	}
	if _, err := decodeRunCommandStoredResult(ref, raw, stdout, stderr, r.ID, r.Attempt, ref.Request); err != nil {
		return err
	}
	b := r.Input.Binding
	// An empty captured stream is a real zero-length bytea, never SQL NULL.
	if stdout == nil {
		stdout = []byte{}
	}
	if stderr == nil {
		stderr = []byte{}
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.command_results(operation_id,attempt_id,execution_id,project_id,agent_id,request_id,body_sha256,body_bytes,body_data,stdout_sha256,stdout_bytes,stdout_data,stderr_sha256,stderr_bytes,stderr_data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, r.ID.String(), r.Attempt.String(), b.Execution.String(), b.Project.String(), b.Agent.String(), string(ref.Request), string(ref.SHA256), int64(ref.Bytes), raw, string(ref.StdoutSHA256), int64(ref.StdoutBytes), stdout, string(ref.StderrSHA256), int64(ref.StderrBytes), stderr)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	return nil
}

func loadRunCommandStoredResult(ctx context.Context, x postgres.SQLExecutor, r runCommandOperationRecord, ref runCommandResultReference) (p.RunCommandResult, []byte, []byte, error) {
	if r.validate() != nil || r.Phase != "succeeded" && r.Phase != "failed" && r.Phase != "cancelled" || ref.validate() != nil || ref.Operation != r.ID || ref.Attempt != r.Attempt {
		return p.RunCommandResult{}, nil, nil, fail(f.InvalidState)
	}
	var project, agent, execution, request string
	var stored runCommandResultReference
	var raw, stdout, stderr []byte
	stored.Operation, stored.Attempt = r.ID, r.Attempt
	err := x.QueryRow(ctx, `SELECT execution_id,project_id,agent_id,request_id,body_sha256,body_bytes,body_data,stdout_sha256,stdout_bytes,stdout_data,stderr_sha256,stderr_bytes,stderr_data FROM agenteam_tool.command_results WHERE operation_id=$1 AND attempt_id=$2`, r.ID.String(), r.Attempt.String()).Scan(&execution, &project, &agent, &request, &stored.SHA256, &stored.Bytes, &raw, &stored.StdoutSHA256, &stored.StdoutBytes, &stdout, &stored.StderrSHA256, &stored.StderrBytes, &stderr)
	defer clear(raw)
	failResult := func(err error) (p.RunCommandResult, []byte, []byte, error) {
		clear(stdout)
		clear(stderr)
		return p.RunCommandResult{}, nil, nil, err
	}
	if err != nil {
		return failResult(portError(err))
	}
	stored.Request = p.ID(request)
	b := r.Input.Binding
	if stored != ref || project != b.Project.String() || agent != b.Agent.String() || execution != b.Execution.String() {
		return failResult(fail(f.InvalidState))
	}
	report, err := decodeRunCommandStoredResult(ref, raw, stdout, stderr, r.ID, r.Attempt, ref.Request)
	if err != nil {
		return failResult(err)
	}
	return report, stdout, stderr, nil
}
