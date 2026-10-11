package registry

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func writeRunnerExecutionReference(ctx context.Context, x postgres.SQLExecutor, r tc.ExecutionToolCaptureRequest, v tc.ExecutionTool) error {
	if v.Validate() != nil || v.BindingSnapshot.RunnerBinding.Validate() != nil {
		return fail(f.InvalidState)
	}
	b := v.BindingSnapshot
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.execution_references(execution_id,tool_id,spec_revision,model_visible_name,handler_id,contract_revision,scope_resolver_id,risk_classifier_id,class,binding_kind,operation_name,operation_revision,protocol_major) VALUES($1,$2,$3,$4,NULL,NULL,$5,$6,$7,'runner',$8,$9,$10)`, r.ExecutionID.String(), v.ToolID.String(), int64(v.SpecRevision), v.ModelVisibleName, string(b.ScopeResolverID), string(b.RiskClassifierID), string(b.Class), b.RunnerBinding.OperationName, b.RunnerBinding.OperationRevision, int(b.RunnerBinding.ProtocolMajor))
	if err != nil {
		return captureError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	return captureContext(ctx)
}

func checkRunnerExecutionReference(ctx context.Context, x postgres.SQLExecutor, r tc.ExecutionToolCaptureRequest, want tc.ExecutionTool) error {
	var revision, major int64
	var name, scope, risk, class, kind, operation, operationRevision string
	var noBuiltin bool
	err := x.QueryRow(ctx, `SELECT spec_revision,model_visible_name,scope_resolver_id,risk_classifier_id,class,binding_kind,operation_name,operation_revision,protocol_major,(handler_id IS NULL AND contract_revision IS NULL) FROM agenteam_tool.execution_references WHERE execution_id=$1 AND tool_id=$2`, r.ExecutionID.String(), want.ToolID.String()).Scan(&revision, &name, &scope, &risk, &class, &kind, &operation, &operationRevision, &major, &noBuiltin)
	if err != nil {
		return captureError(err)
	}
	b := want.BindingSnapshot
	if !noBuiltin || revision != int64(want.SpecRevision) || name != want.ModelVisibleName || scope != string(b.ScopeResolverID) || risk != string(b.RiskClassifierID) || class != string(b.Class) || kind != "runner" || operation != b.RunnerBinding.OperationName || operationRevision != b.RunnerBinding.OperationRevision || major != int64(b.RunnerBinding.ProtocolMajor) {
		return fail(f.InvalidState)
	}
	return captureContext(ctx)
}

// RequireExecutionToolFactsInTx is a same-Store metadata read. It does not
// acquire locks, begin/commit, authorize the Execution or supply dispatch rights.
// The Execution owner must first prove its original live run, current Agent,
// policy, call and protected input, using its complete outer lock union.
func (r *Registry) RequireExecutionToolFactsInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolFactsRequest) error {
	if err := captureContext(ctx); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	x, err := r.executor(ctx, tx, request.RequiredLocks())
	if err != nil {
		return err
	}
	version, allowed, err := loadCaptureAgentReferences(ctx, x, request.Capture)
	if err != nil {
		return err
	}
	if version != request.AgentVersion || !slices.Equal(allowed, request.AllowedToolIDs) {
		return fail(f.VersionConflict)
	}
	var project, agent, binding, digest string
	var capturedVersion, count int64
	err = x.QueryRow(ctx, `SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,tool_count FROM agenteam_tool.execution_configurations WHERE execution_id=$1`, request.Capture.ExecutionID.String()).Scan(&project, &agent, &capturedVersion, &binding, &digest, &count)
	if err != nil {
		return captureError(err)
	}
	if project != request.Capture.ProjectID.String() || agent != request.Capture.AgentID.String() || f.Version(capturedVersion).Validate() != nil || count != int64(len(request.Tools)) {
		return fail(f.InvalidState)
	}
	if _, err = f.ParseDigest(binding); err != nil {
		return fail(f.InvalidState)
	}
	if _, err = f.ParseDigest(digest); err != nil {
		return fail(f.InvalidState)
	}
	if err = checkExecutionToolReferences(ctx, x, request.Capture, request.Tools); err != nil {
		return err
	}
	for _, want := range request.Tools {
		v, exists, e := r.currentCaptureTool(ctx, tx, x, want.ToolID)
		if e != nil {
			return e
		}
		if !exists {
			return fail(f.NotFound)
		}
		b := want.BindingSnapshot
		if v.Ref != (tc.SpecRef{ToolID: want.ToolID, SpecRevision: want.SpecRevision}) || v.Binding != b.Binding || v.RunnerBinding != b.RunnerBinding || v.Scope != b.ScopeResolverID || v.Risk != b.RiskClassifierID || v.Class != b.Class {
			return fail(f.VersionConflict)
		}
		if v.Class != tc.CoreTool && !slices.Contains(allowed, want.ToolID) {
			return fail(f.Forbidden)
		}
	}
	return captureContext(ctx)
}

var _ tc.ExecutionToolFacts = (*Registry)(nil)
