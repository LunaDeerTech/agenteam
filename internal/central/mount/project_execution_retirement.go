package mount

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This provider releases historical Execution references, never a Mount,
// Runner workspace, directory or live operation. Prepared is supplied by the
// original consumer after its real owners and borrowed resources returned.
// A terminal receipt alone is not authority to retire an unowned Runner call.
type ProjectExecutionRetirementReferences struct {
	store Store
	owner ec.ProjectExecutionRetirementOwner
}

func NewProjectExecutionRetirementReferences(store Store, owner ec.ProjectExecutionRetirementOwner) (*ProjectExecutionRetirementReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectExecutionRetirementReferences{store: store, owner: owner}, nil
}

func (*ProjectExecutionRetirementReferences) ProjectExecutionRetirementReferenceKind() ec.ProjectExecutionRetirementReferenceKind {
	return ec.ProjectExecutionMountReferences
}

type projectExecutionReferencePlan struct {
	issuer                   *ProjectExecutionRetirementReferences
	request                  ec.ProjectExecutionRetirementRequest
	before                   []byte
	mounts                   []i.MountID
	capture, use, retirement bool
	locks                    []f.LockRequest
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
	_, _ = w.Write([]byte("mount_execution_retirement_plan"))
}
func (*projectExecutionReferencePlan) LogValue() slog.Value {
	return slog.StringValue("mount_execution_retirement_plan")
}
func (*projectExecutionReferencePlan) MarshalJSON() ([]byte, error) {
	return []byte(`"mount_execution_retirement_plan"`), nil
}

func projectExecutionRetirementLocks(r ec.ProjectExecutionRetirementRequest) []f.LockRequest {
	p, _ := f.ProjectLock(r.Binding.ProjectID.String())
	s, _ := f.ProjectScheduleLock(r.Binding.ProjectID.String())
	a, _ := f.AgentLock(r.Binding.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.Binding.ExecutionID.String())
	return []f.LockRequest{{Key: p, Mode: f.Exclusive}, {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Exclusive}, {Key: e, Mode: f.Exclusive}}
}

func (p *ProjectExecutionRetirementReferences) executor(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, applied bool) (postgres.SQLExecutor, error) {
	if p == nil || nilPort(p.store) || nilPort(p.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || r.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	x, err := p.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, projectExecutionRetirementLocks(r)); err != nil {
		return nil, portError(err)
	}
	if applied {
		err = p.owner.RequireProjectExecutionRetirementAppliedInTx(ctx, tx, r)
	} else {
		err = p.owner.RequireProjectExecutionRetirementPreparedInTx(ctx, tx, r)
	}
	if err != nil {
		return nil, portError(err)
	}
	return x, portError(ctx.Err())
}

type projectExecutionMountSnapshot struct {
	Capture    json.RawMessage   `json:"capture"`
	References []json.RawMessage `json:"references"`
	Use        json.RawMessage   `json:"use"`
	Retirement json.RawMessage   `json:"retirement"`
}

func readProjectExecutionMounts(ctx context.Context, x postgres.SQLExecutor, r ec.ProjectExecutionRetirementRequest) (*projectExecutionReferencePlan, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT jsonb_build_object('capture',(SELECT (to_jsonb(c)-'material')||jsonb_build_object('material_hex',encode(c.material,'hex')) FROM agenteam_mount.execution_captures c WHERE c.execution_id=$1),'references',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.mount_id) FROM (SELECT * FROM agenteam_mount.execution_references WHERE execution_id=$1 ORDER BY mount_id LIMIT 129) r),'[]'::jsonb),'use',(SELECT to_jsonb(u) FROM agenteam_mount.execution_uses u WHERE u.execution_id=$1),'retirement',(SELECT to_jsonb(t) FROM agenteam_mount.execution_use_retirements t WHERE t.execution_id=$1))`, r.Binding.ExecutionID.String()).Scan(&raw)
	if err != nil {
		return nil, portError(err)
	}
	var snapshot projectExecutionMountSnapshot
	if len(raw) > 2*maxExecutionMountMaterialBytes+(1<<20) || json.Unmarshal(raw, &snapshot) != nil || snapshot.References == nil || len(snapshot.References) > mc.MaxExecutionMounts {
		return nil, fault(f.InvalidState)
	}
	plan := &projectExecutionReferencePlan{before: bytes.Clone(raw), capture: !bytes.Equal(snapshot.Capture, []byte("null")), use: !bytes.Equal(snapshot.Use, []byte("null")), retirement: !bytes.Equal(snapshot.Retirement, []byte("null"))}
	if !plan.capture {
		if len(snapshot.References) != 0 || plan.use || plan.retirement {
			return nil, fault(f.InvalidState)
		}
		return plan, portError(ctx.Err())
	}
	var head struct {
		Execution string   `json:"execution_id"`
		Project   string   `json:"project_id"`
		Agent     string   `json:"agent_id"`
		Version   int64    `json:"agent_version"`
		Attempt   f.Digest `json:"attempt_binding"`
		Digest    f.Digest `json:"capture_digest"`
		Count     int      `json:"mount_count"`
		Material  string   `json:"material_hex"`
	}
	if json.Unmarshal(snapshot.Capture, &head) != nil || head.Execution != r.Binding.ExecutionID.String() || head.Project != r.Binding.ProjectID.String() || head.Agent != r.Binding.AgentID.String() || f.Version(head.Version).Validate() != nil || head.Attempt.Validate() != nil || head.Digest.Validate() != nil || head.Count < 1 || head.Count != len(snapshot.References) {
		return nil, fault(f.ConfirmationStale)
	}
	material, err := hex.DecodeString(head.Material)
	if err != nil || len(material) > maxExecutionMountMaterialBytes {
		return nil, fault(f.InvalidState)
	}
	var captured mc.ExecutionMountSet
	if json.Unmarshal(material, &captured) != nil || captured.Validate() != nil || captured.Request.ProjectID != r.Binding.ProjectID || captured.Request.AgentID != r.Binding.AgentID || captured.Request.ExecutionID != r.Binding.ExecutionID || int64(captured.AgentVersion) != head.Version || captured.AttemptBinding != head.Attempt || len(captured.Mounts) != head.Count {
		return nil, fault(f.ConfirmationStale)
	}
	canonical, digest, err := executionMountMaterial(captured)
	if err != nil || !bytes.Equal(canonical, material) || digest != head.Digest {
		return nil, fault(f.InvalidState)
	}
	plan.mounts = make([]i.MountID, 0, len(snapshot.References))
	for n, rawReference := range snapshot.References {
		var reference struct {
			Execution     string `json:"execution_id"`
			Mount         string `json:"mount_id"`
			Workspace     string `json:"workspace_id"`
			Runner        string `json:"runner_id"`
			MountVersion  int64  `json:"mount_version"`
			RunnerVersion int64  `json:"runner_version"`
		}
		if json.Unmarshal(rawReference, &reference) != nil {
			return nil, fault(f.InvalidState)
		}
		m := captured.Mounts[n]
		if reference.Execution != head.Execution || reference.Mount != m.ID.String() || reference.Workspace != m.WorkspaceID.String() || reference.Runner != m.RunnerID.String() || reference.MountVersion != int64(m.Version) || reference.RunnerVersion != int64(m.RunnerVersion) {
			return nil, fault(f.ConfirmationStale)
		}
		plan.mounts = append(plan.mounts, m.ID)
	}
	if !plan.use {
		if plan.retirement {
			return nil, fault(f.InvalidState)
		}
		return plan, portError(ctx.Err())
	}
	var use struct {
		Execution string   `json:"execution_id"`
		Project   string   `json:"project_id"`
		Agent     string   `json:"agent_id"`
		Version   int64    `json:"agent_version"`
		Attempt   f.Digest `json:"attempt_binding"`
		Capture   f.Digest `json:"capture_digest"`
		Input     f.Digest `json:"input_digest"`
		Snapshot  f.Digest `json:"snapshot_digest"`
	}
	if json.Unmarshal(snapshot.Use, &use) != nil || use.Execution != head.Execution || use.Project != head.Project || use.Agent != head.Agent || use.Version != head.Version || use.Attempt != head.Attempt || use.Capture != head.Digest || use.Input.Validate() != nil || use.Snapshot.Validate() != nil {
		return nil, fault(f.ConfirmationStale)
	}
	if !plan.retirement {
		return nil, fault(f.ResourceBusy)
	}
	var retired struct {
		Execution string `json:"execution_id"`
		Version   int64  `json:"terminal_version"`
		Event     string `json:"terminal_event_id"`
	}
	if json.Unmarshal(snapshot.Retirement, &retired) != nil || retired.Execution != head.Execution || retired.Version != int64(r.Binding.Version) {
		return nil, fault(f.ConfirmationStale)
	}
	if _, err := f.ParseID[struct{}](retired.Event); err != nil {
		return nil, fault(f.InvalidState)
	}
	return plan, portError(ctx.Err())
}

func (p *ProjectExecutionRetirementReferences) DiscoverProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementReferencePlan, error) {
	x, err := p.executor(ctx, tx, r, false)
	if err != nil {
		return nil, err
	}
	plan, err := readProjectExecutionMounts(ctx, x, r)
	if err != nil {
		return nil, err
	}
	plan.issuer, plan.request, plan.locks = p, r, projectExecutionRetirementLocks(r)
	return plan, nil
}

func (p *ProjectExecutionRetirementReferences) plan(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, supplied ec.ProjectExecutionRetirementReferencePlan, applied bool) (*projectExecutionReferencePlan, postgres.SQLExecutor, error) {
	plan, ok := supplied.(*projectExecutionReferencePlan)
	if !ok || plan == nil || plan.issuer != p || plan.request.Binding != r.Binding || plan.request.Cause != r.Cause || !plan.request.Actor.Equal(r.Actor) {
		return nil, nil, fault(f.Forbidden)
	}
	x, err := p.executor(ctx, tx, r, applied)
	return plan, x, err
}

func (p *ProjectExecutionRetirementReferences) RetireProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, supplied ec.ProjectExecutionRetirementReferencePlan) error {
	plan, x, err := p.plan(ctx, tx, r, supplied, false)
	if err != nil {
		return err
	}
	current, err := readProjectExecutionMounts(ctx, x, r)
	if err != nil {
		return err
	}
	if !bytes.Equal(current.before, plan.before) {
		return fault(f.ConfirmationStale)
	}
	remove := func(query string, args ...any) error {
		tag, err := x.Exec(ctx, query, args...)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		return portError(ctx.Err())
	}
	e := r.Binding.ExecutionID.String()
	if plan.retirement {
		if err = remove(`DELETE FROM agenteam_mount.execution_use_retirements WHERE execution_id=$1`, e); err != nil {
			return err
		}
	}
	if plan.use {
		if err = remove(`DELETE FROM agenteam_mount.execution_uses WHERE execution_id=$1 AND project_id=$2 AND agent_id=$3`, e, r.Binding.ProjectID.String(), r.Binding.AgentID.String()); err != nil {
			return err
		}
	}
	for _, mount := range plan.mounts {
		if err = remove(`DELETE FROM agenteam_mount.execution_references WHERE execution_id=$1 AND mount_id=$2`, e, mount.String()); err != nil {
			return err
		}
	}
	if plan.capture {
		if err = remove(`DELETE FROM agenteam_mount.execution_captures WHERE execution_id=$1 AND project_id=$2 AND agent_id=$3`, e, r.Binding.ProjectID.String(), r.Binding.AgentID.String()); err != nil {
			return err
		}
	}
	return portError(ctx.Err())
}

func (p *ProjectExecutionRetirementReferences) RequireProjectExecutionReferencesRetiredInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, supplied ec.ProjectExecutionRetirementReferencePlan) error {
	_, x, err := p.plan(ctx, tx, r, supplied, true)
	if err != nil {
		return err
	}
	current, err := readProjectExecutionMounts(ctx, x, r)
	if err != nil {
		return err
	}
	if current.capture || current.use || current.retirement || len(current.mounts) != 0 {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

var _ ec.ProjectExecutionRetirementReferences = (*ProjectExecutionRetirementReferences)(nil)
