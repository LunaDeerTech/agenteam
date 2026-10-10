package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

func parseCaptureIDs(raw []string) ([]id.ToolID, error) {
	if len(raw) > 128 {
		return nil, fail(f.PayloadTooLarge)
	}
	ids := make([]id.ToolID, 0, len(raw))
	for n, v := range raw {
		parsed, err := f.ParseID[id.Tool](v)
		if err != nil || n > 0 && raw[n-1] >= v {
			return nil, fail(f.InvalidState)
		}
		ids = append(ids, parsed)
	}
	return ids, nil
}

// A bounded SQL array keeps the real head and its complete ordered reference
// set in one statement. A 129th row is an error, never a truncated selection.
func loadCaptureAgentReferences(ctx context.Context, x postgres.SQLExecutor, r tc.ExecutionToolCaptureRequest) (f.Version, []id.ToolID, error) {
	var project string
	var version int64
	var raw []string
	err := x.QueryRow(ctx, `SELECT h.project_id,h.config_version,ARRAY(SELECT r.tool_id::text FROM agenteam_tool.agent_references r WHERE r.agent_id=h.agent_id ORDER BY r.tool_id COLLATE "C" LIMIT 129) FROM agenteam_tool.agent_configurations h WHERE h.agent_id=$1`, r.AgentID.String()).Scan(&project, &version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, fail(f.DependencyUnbound)
	}
	if err != nil {
		return 0, nil, captureError(err)
	}
	if project != r.ProjectID.String() || f.Version(version).Validate() != nil {
		return 0, nil, fail(f.InvalidState)
	}
	ids, err := parseCaptureIDs(raw)
	if err != nil {
		return 0, nil, err
	}
	return f.Version(version), ids, captureContext(ctx)
}
func loadCaptureCoreTools(ctx context.Context, x postgres.SQLExecutor) ([]id.ToolID, error) {
	var raw []string
	err := x.QueryRow(ctx, `SELECT ARRAY(SELECT tool_id::text FROM agenteam_tool.registrations WHERE class='core' ORDER BY tool_id COLLATE "C" LIMIT 129)`).Scan(&raw)
	if err != nil {
		return nil, captureError(err)
	}
	ids, err := parseCaptureIDs(raw)
	if err != nil {
		return nil, err
	}
	return ids, captureContext(ctx)
}

// Agent configuration's existing currentTool deliberately rejects Core tools.
// Capture has a separate class-aware read, retaining the same exact current
// registration, immutable bytes and real Source binding checks for both classes.
func (r *Registry) currentCaptureTool(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, tool id.ToolID) (currentTool, bool, error) {
	var v currentTool
	var revision, contract int64
	var raw []byte
	err := x.QueryRow(ctx, `SELECT i.stable_key,r.spec_revision,s.definition,r.handler_id,r.contract_revision,r.scope_resolver_id,r.risk_classifier_id,r.class FROM agenteam_tool.identities i JOIN agenteam_tool.registrations r ON r.tool_id=i.tool_id AND r.spec_revision=i.latest_revision JOIN agenteam_tool.spec_revisions s ON s.tool_id=r.tool_id AND s.spec_revision=r.spec_revision WHERE i.tool_id=$1`, tool.String()).Scan(&v.Key, &revision, &raw, &v.Binding.HandlerID, &contract, &v.Scope, &v.Risk, &v.Class)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentTool{}, false, nil
	}
	if err != nil {
		return currentTool{}, false, captureError(err)
	}
	v.Ref = tc.SpecRef{ToolID: tool, SpecRevision: f.Version(revision)}
	v.Binding.ContractRevision = f.Version(contract)
	if v.Ref.Validate() != nil {
		return currentTool{}, false, fail(f.InvalidState)
	}
	v.Definition, err = decodeDefinition(ctx, raw, v.Key)
	if err != nil {
		return currentTool{}, false, captureError(err)
	}
	source, canonical, err := r.source(ctx, tx, v.Key)
	if err != nil {
		return currentTool{}, false, captureError(err)
	}
	if !source.Active {
		return currentTool{}, false, fail(f.InvalidState)
	}
	if !bytes.Equal(canonical, v.Definition) || source.Binding != v.Binding || source.ScopeResolverID != v.Scope || source.RiskClassifierID != v.Risk || source.Class != v.Class {
		return currentTool{}, false, fail(f.InvalidState)
	}
	return v, true, captureContext(ctx)
}

func executionCaptureDigest(facts tc.ExecutionToolCaptureFacts, tools []tc.ExecutionTool) (f.Digest, error) {
	raw, err := json.Marshal(struct {
		Version   f.Version
		Selection tc.ToolSnapshotSelection
		Tools     []tc.ExecutionTool
	}{facts.AgentVersion, facts.Selection.Clone(), slices.Clone(tools)})
	if err != nil {
		return "", fail(f.InvalidState)
	}
	sum := sha256.Sum256(raw)
	return f.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
}

// Writes only Tool-owned metadata/ref rows in this caller transaction. The
// Execution scope parent and immutable ToolSpec revision are real database FKs.
// There is no replacement, autonomous commit, lease acquisition or backend I/O.
func writeExecutionToolReferences(ctx context.Context, x postgres.SQLExecutor, r tc.ExecutionToolCaptureRequest, facts tc.ExecutionToolCaptureFacts, tools []tc.ExecutionTool) error {
	digest, err := executionCaptureDigest(facts, tools)
	if err != nil {
		return err
	}
	var project, agent, binding, storedDigest string
	var version, count int64
	err = x.QueryRow(ctx, `SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,tool_count FROM agenteam_tool.execution_configurations WHERE execution_id=$1`, r.ExecutionID.String()).Scan(&project, &agent, &version, &binding, &storedDigest, &count)
	if err == nil {
		if project != r.ProjectID.String() || agent != r.AgentID.String() || version != int64(facts.AgentVersion) || binding != facts.AttemptBinding.String() || storedDigest != digest.String() || count != int64(len(tools)) {
			return fail(f.VersionConflict)
		}
		return checkExecutionToolReferences(ctx, x, r, tools)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return captureError(err)
	}
	if err = captureContext(ctx); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.execution_configurations(execution_id,project_id,agent_id,agent_version,attempt_binding,capture_digest,tool_count) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.ExecutionID.String(), r.ProjectID.String(), r.AgentID.String(), int64(facts.AgentVersion), facts.AttemptBinding.String(), digest.String(), len(tools))
	if err != nil {
		return captureError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	for _, v := range tools {
		b := v.BindingSnapshot
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_tool.execution_references(execution_id,tool_id,spec_revision,model_visible_name,handler_id,contract_revision,scope_resolver_id,risk_classifier_id,class) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.ExecutionID.String(), v.ToolID.String(), int64(v.SpecRevision), v.ModelVisibleName, b.Binding.HandlerID, int64(b.Binding.ContractRevision), string(b.ScopeResolverID), string(b.RiskClassifierID), string(b.Class))
		if err != nil {
			return captureError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
	}
	return captureContext(ctx)
}

func checkExecutionToolReferences(ctx context.Context, x postgres.SQLExecutor, r tc.ExecutionToolCaptureRequest, tools []tc.ExecutionTool) error {
	var count int64
	if err := x.QueryRow(ctx, `SELECT count(*) FROM agenteam_tool.execution_references WHERE execution_id=$1`, r.ExecutionID.String()).Scan(&count); err != nil {
		return captureError(err)
	}
	if count != int64(len(tools)) {
		return fail(f.InvalidState)
	}
	for _, want := range tools {
		var revision, contract int64
		var name, handler, scope, risk, class string
		err := x.QueryRow(ctx, `SELECT spec_revision,model_visible_name,handler_id,contract_revision,scope_resolver_id,risk_classifier_id,class FROM agenteam_tool.execution_references WHERE execution_id=$1 AND tool_id=$2`, r.ExecutionID.String(), want.ToolID.String()).Scan(&revision, &name, &handler, &contract, &scope, &risk, &class)
		if err != nil {
			return captureError(err)
		}
		b := want.BindingSnapshot
		if revision != int64(want.SpecRevision) || name != want.ModelVisibleName || handler != b.Binding.HandlerID || contract != int64(b.Binding.ContractRevision) || scope != string(b.ScopeResolverID) || risk != string(b.RiskClassifierID) || class != string(b.Class) {
			return fail(f.InvalidState)
		}
	}
	return captureContext(ctx)
}
