package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// ProjectExecutionRetirementReferences releases immutable metadata references
// only after the original Execution consumer proves its complete returned
// ownership. Tool operations and their physical owners are not covered here.
type ProjectExecutionRetirementReferences struct {
	store Store
	owner ec.ProjectExecutionRetirementOwner
}

func NewProjectExecutionRetirementReferences(store Store, owner ec.ProjectExecutionRetirementOwner) (*ProjectExecutionRetirementReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fail(f.DependencyUnbound)
	}
	return &ProjectExecutionRetirementReferences{store: store, owner: owner}, nil
}

func (*ProjectExecutionRetirementReferences) ProjectExecutionRetirementReferenceKind() ec.ProjectExecutionRetirementReferenceKind {
	return ec.ProjectExecutionToolReferences
}

type projectExecutionReferencePlan struct {
	issuer  *ProjectExecutionRetirementReferences
	request ec.ProjectExecutionRetirementRequest
	before  []byte
	tools   []i.ToolID
	exists  bool
	locks   []f.LockRequest
}

func (p *projectExecutionReferencePlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (p *projectExecutionReferencePlan) ProjectExecutionRetirementReferenceDigest() f.Digest {
	if p == nil {
		return ""
	}
	return f.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(p.before)))
}
func (*projectExecutionReferencePlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("tool_execution_retirement_plan"))
}
func (*projectExecutionReferencePlan) LogValue() slog.Value {
	return slog.StringValue("tool_execution_retirement_plan")
}
func (*projectExecutionReferencePlan) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_execution_retirement_plan"`), nil
}

func projectExecutionRetirementLocks(r ec.ProjectExecutionRetirementRequest) []f.LockRequest {
	p, _ := f.ProjectLock(r.Binding.ProjectID.String())
	s, _ := f.ProjectScheduleLock(r.Binding.ProjectID.String())
	a, _ := f.AgentLock(r.Binding.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.Binding.ExecutionID.String())
	return []f.LockRequest{{Key: p, Mode: f.Exclusive}, {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Exclusive}, {Key: e, Mode: f.Exclusive}}
}

func (p *ProjectExecutionRetirementReferences) executor(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, locks []f.LockRequest, applied bool) (postgres.SQLExecutor, error) {
	if p == nil || nilPort(p.store) || nilPort(p.owner) {
		return nil, fail(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || r.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, captureError(err)
	}
	x, err := p.store.InTx(tx)
	if err != nil {
		return nil, captureError(err)
	}
	if nilPort(x) {
		return nil, fail(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, captureError(err)
	}
	if applied {
		err = p.owner.RequireProjectExecutionRetirementAppliedInTx(ctx, tx, r)
	} else {
		err = p.owner.RequireProjectExecutionRetirementPreparedInTx(ctx, tx, r)
	}
	if err != nil {
		return nil, captureError(err)
	}
	return x, captureContext(ctx)
}

// An empty operation table is only a rejection check. The concrete consumer's
// private Prepared witness, including actual return/ProcessGuard evidence,
// remains mandatory even for an Execution that captured no Tool metadata.
func noProjectExecutionToolOperations(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID) error {
	var exists bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_tool.operations WHERE execution_id=$1) OR EXISTS(SELECT 1 FROM agenteam_tool.attempts WHERE execution_id=$1) OR EXISTS(SELECT 1 FROM agenteam_tool.read_file_results WHERE execution_id=$1) OR EXISTS(SELECT 1 FROM agenteam_tool.write_file_results WHERE execution_id=$1) OR EXISTS(SELECT 1 FROM agenteam_tool.command_results WHERE execution_id=$1)`, execution.String()).Scan(&exists)
	if err != nil {
		return captureError(err)
	}
	if exists {
		return fail(f.ResourceBusy)
	}
	return captureContext(ctx)
}

func readProjectExecutionToolReferences(ctx context.Context, x postgres.SQLExecutor, r ec.ProjectExecutionRetirementRequest) ([]byte, []i.ToolID, bool, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT jsonb_build_object('head',(SELECT to_jsonb(h) FROM agenteam_tool.execution_configurations h WHERE h.execution_id=$1),'references',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.tool_id COLLATE "C") FROM (SELECT * FROM agenteam_tool.execution_references WHERE execution_id=$1 ORDER BY tool_id COLLATE "C" LIMIT 129) r),'[]'::jsonb))`, r.Binding.ExecutionID.String()).Scan(&raw)
	if err != nil {
		return nil, nil, false, captureError(err)
	}
	var snapshot struct {
		Head       json.RawMessage   `json:"head"`
		References []json.RawMessage `json:"references"`
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &snapshot) != nil || snapshot.References == nil || len(snapshot.References) > 128 {
		return nil, nil, false, fail(f.InvalidState)
	}
	if bytes.Equal(snapshot.Head, []byte("null")) {
		if len(snapshot.References) != 0 {
			return nil, nil, false, fail(f.InvalidState)
		}
		return bytes.Clone(raw), nil, false, captureContext(ctx)
	}
	var head struct {
		Execution string   `json:"execution_id"`
		Project   string   `json:"project_id"`
		Agent     string   `json:"agent_id"`
		Version   int64    `json:"agent_version"`
		Attempt   f.Digest `json:"attempt_binding"`
		Digest    f.Digest `json:"capture_digest"`
		Count     int      `json:"tool_count"`
	}
	if json.Unmarshal(snapshot.Head, &head) != nil || head.Execution != r.Binding.ExecutionID.String() || head.Project != r.Binding.ProjectID.String() || head.Agent != r.Binding.AgentID.String() || f.Version(head.Version).Validate() != nil || head.Attempt.Validate() != nil || head.Digest.Validate() != nil || head.Count != len(snapshot.References) {
		return nil, nil, false, fail(f.ConfirmationStale)
	}
	tools := make([]i.ToolID, 0, len(snapshot.References))
	for _, rawReference := range snapshot.References {
		var reference struct {
			Execution string `json:"execution_id"`
			Tool      string `json:"tool_id"`
			Revision  int64  `json:"spec_revision"`
		}
		if json.Unmarshal(rawReference, &reference) != nil || reference.Execution != head.Execution || f.Version(reference.Revision).Validate() != nil {
			return nil, nil, false, fail(f.InvalidState)
		}
		tool, err := f.ParseID[i.Tool](reference.Tool)
		if err != nil || len(tools) != 0 && tools[len(tools)-1].String() >= tool.String() {
			return nil, nil, false, fail(f.InvalidState)
		}
		tools = append(tools, tool)
	}
	return bytes.Clone(raw), tools, true, captureContext(ctx)
}

func (p *ProjectExecutionRetirementReferences) DiscoverProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementReferencePlan, error) {
	x, err := p.executor(ctx, tx, r, projectExecutionRetirementLocks(r), false)
	if err != nil {
		return nil, err
	}
	if err = noProjectExecutionToolOperations(ctx, x, r.Binding.ExecutionID); err != nil {
		return nil, err
	}
	before, tools, exists, err := readProjectExecutionToolReferences(ctx, x, r)
	if err != nil {
		return nil, err
	}
	locks := append(projectExecutionRetirementLocks(r), RegistryLock(f.Exclusive))
	for _, tool := range tools {
		locks = append(locks, toolLock(tool, f.Shared))
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return nil, captureError(err)
	}
	return &projectExecutionReferencePlan{issuer: p, request: r, before: before, tools: tools, exists: exists, locks: locks}, nil
}

func (p *ProjectExecutionRetirementReferences) plan(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, supplied ec.ProjectExecutionRetirementReferencePlan, applied bool) (*projectExecutionReferencePlan, postgres.SQLExecutor, error) {
	plan, ok := supplied.(*projectExecutionReferencePlan)
	if !ok || plan == nil || plan.issuer != p || plan.request.Binding != r.Binding || plan.request.Cause != r.Cause || !plan.request.Actor.Equal(r.Actor) {
		return nil, nil, fail(f.Forbidden)
	}
	x, err := p.executor(ctx, tx, r, plan.locks, applied)
	if err != nil {
		return nil, nil, err
	}
	if err = noProjectExecutionToolOperations(ctx, x, r.Binding.ExecutionID); err != nil {
		return nil, nil, err
	}
	return plan, x, nil
}

func (p *ProjectExecutionRetirementReferences) RetireProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, supplied ec.ProjectExecutionRetirementReferencePlan) error {
	plan, x, err := p.plan(ctx, tx, r, supplied, false)
	if err != nil {
		return err
	}
	current, _, _, err := readProjectExecutionToolReferences(ctx, x, r)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, plan.before) {
		return fail(f.ConfirmationStale)
	}
	for _, tool := range plan.tools {
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_tool.execution_references WHERE execution_id=$1 AND tool_id=$2`, r.Binding.ExecutionID.String(), tool.String())
		if err != nil {
			return captureError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ResourceBusy)
		}
		if err = captureContext(ctx); err != nil {
			return err
		}
	}
	if plan.exists {
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_tool.execution_configurations WHERE execution_id=$1 AND project_id=$2 AND agent_id=$3`, r.Binding.ExecutionID.String(), r.Binding.ProjectID.String(), r.Binding.AgentID.String())
		if err != nil {
			return captureError(err)
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ResourceBusy)
		}
	}
	return captureContext(ctx)
}

func (p *ProjectExecutionRetirementReferences) RequireProjectExecutionReferencesRetiredInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, supplied ec.ProjectExecutionRetirementReferencePlan) error {
	_, x, err := p.plan(ctx, tx, r, supplied, true)
	if err != nil {
		return err
	}
	_, tools, exists, err := readProjectExecutionToolReferences(ctx, x, r)
	if err != nil {
		return err
	}
	if exists || len(tools) != 0 {
		return fail(f.ResourceBusy)
	}
	return captureContext(ctx)
}

var _ ec.ProjectExecutionRetirementReferences = (*ProjectExecutionRetirementReferences)(nil)
