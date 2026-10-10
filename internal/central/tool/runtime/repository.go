package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

func loadOperation(ctx context.Context, x postgres.SQLExecutor, b tc.ToolCallBinding) (*operationRecord, error) {
	var r operationRecord
	var operation, skill, project, agent, execution, logical, invocation, call, tool string
	var revision int64
	var raw []byte
	err := x.QueryRow(ctx, `SELECT operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,backend_key,fingerprint,phase,version FROM agenteam_tool.operations WHERE execution_id=$1 AND logical_call_id=$2 AND invocation_id=$3 AND call_id=$4`, b.ExecutionID.String(), b.LogicalCallID.String(), b.InvocationID.String(), b.CallID).Scan(&operation, &project, &agent, &execution, &logical, &invocation, &call, &tool, &revision, &raw, &r.InputDigest, &skill, &r.Key, &r.Fingerprint, &r.Phase, &r.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	if len(raw) == 0 || len(raw) > 32768 {
		return nil, fail(f.InvalidState)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&r.Input); err != nil {
		return nil, fail(f.InvalidState)
	}
	canonical, err := encode(r.Input)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, fail(f.InvalidState)
	}
	if r.ID, err = f.ParseID[tc.Operation](operation); err != nil {
		return nil, fail(f.InvalidState)
	}
	if r.SkillID, err = f.ParseID[pc.Skill](skill); err != nil {
		return nil, fail(f.InvalidState)
	}
	v := r.Input.Binding
	if v.Project.String() != project || v.Agent.String() != agent || v.Execution.String() != execution || v.LogicalCall.String() != logical || v.Invocation.String() != invocation || v.Call != call || v.Spec.ToolID.String() != tool || int64(v.Spec.SpecRevision) != revision {
		return nil, fail(f.InvalidState)
	}
	if err = r.validate(); err != nil {
		return nil, err
	}
	return &r, nil
}

func insertOperation(ctx context.Context, x postgres.SQLExecutor, r operationRecord) error {
	if err := r.validate(); err != nil {
		return err
	}
	raw, err := encode(r.Input)
	if err != nil {
		return err
	}
	b := r.Input.Binding
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.operations(operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,backend_key,fingerprint,phase,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'created',1)`, r.ID.String(), b.Project.String(), b.Agent.String(), b.Execution.String(), b.LogicalCall.String(), b.Invocation.String(), b.Call, b.Spec.ToolID.String(), int64(b.Spec.SpecRevision), raw, string(r.InputDigest), r.SkillID.String(), r.Key.String(), string(r.Fingerprint))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	return nil
}
