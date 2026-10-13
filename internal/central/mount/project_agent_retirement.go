package mount

import (
	"context"
	"encoding/json"
	"reflect"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This first retirement path supports the actual empty initialized index.
// Managed Mounts, Runner workspaces and historical uses remain their owners'
// responsibility; no filesystem operation or resource deletion is attempted.
type ProjectAgentRetirementReferences struct {
	store   Store
	owner   ac.ProjectRetirementOwner
	managed *ProjectLifecycleOwnership
}

func NewProjectAgentRetirementReferences(store Store, owner ac.ProjectRetirementOwner) (*ProjectAgentRetirementReferences, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectAgentRetirementReferences{store: store, owner: owner}, nil
}

// The explicit constructor consumes RunnerManaged's durable exact preimage.
// Legacy empty-index retirement keeps its original reads and behavior.
func NewProjectAgentRetirementReferencesWithRunnerManaged(store Store, owner ac.ProjectRetirementOwner, original *ProjectLifecycleOwnership) (*ProjectAgentRetirementReferences, error) {
	if original == nil || nilPort(store) || reflect.TypeOf(store) != reflect.TypeOf(original.store) || !reflect.TypeOf(store).Comparable() || store != original.store {
		return nil, fault(f.DependencyUnbound)
	}
	p, err := NewProjectAgentRetirementReferences(store, owner)
	if err != nil {
		return nil, err
	}
	p.managed = original
	return p, nil
}
func (*ProjectAgentRetirementReferences) ProjectRetirementReferenceKind() ac.ProjectRetirementReferenceKind {
	return ac.ProjectRetirementMountReferences
}
func (s *ProjectAgentRetirementReferences) ProjectRetirementLocks(c ac.ProjectRetirementChange) ([]f.LockRequest, error) {
	if s == nil || nilPort(s.store) || nilPort(s.owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c.Request.RequiredLocks(), nil
}
func (s *ProjectAgentRetirementReferences) executor(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) (postgres.SQLExecutor, error) {
	locks, err := s.ProjectRetirementLocks(c)
	if err != nil {
		return nil, err
	}
	if ctx == nil || !tx.Valid() {
		return nil, fault(f.InvalidArgument)
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if err = s.owner.RequireProjectRetirementAppliedInTx(ctx, tx, c.Clone()); err != nil {
		return nil, portError(err)
	}
	return x, nil
}
func noProjectAgentMountResources(ctx context.Context, x postgres.SQLExecutor, c ac.ProjectRetirementChange) error {
	var resources bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.mounts WHERE agent_id=$1::text::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces WHERE agent_id=$1::text::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.execution_captures WHERE agent_id=$1::text) OR EXISTS(SELECT 1 FROM agenteam_mount.execution_uses WHERE agent_id=$1::text)`, c.Request.AgentID.String()).Scan(&resources)
	if err != nil {
		return portError(err)
	}
	if resources || len(c.Before.Fields().AllowedMountIDs) != 0 {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func (s *ProjectAgentRetirementReferences) RetireProjectAgentReferencesInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	if s.managed != nil {
		return s.retireManaged(ctx, x, c)
	}
	r := c.Request
	before, err := loadReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return err
	}
	if !before.exists || before.version != r.ExpectedVersion || len(before.ids) != 0 {
		return fault(f.ResourceBusy)
	}
	if err = noProjectAgentMountResources(ctx, x, c); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_mount.agent_mount_heads WHERE agent_id=$1 AND project_id=$2 AND owner_version=$3`, r.AgentID.String(), r.ProjectID.String(), int64(r.ExpectedVersion))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}
func (s *ProjectAgentRetirementReferences) RequireProjectAgentReferencesRetiredInTx(ctx context.Context, tx f.Tx, c ac.ProjectRetirementChange) error {
	x, err := s.executor(ctx, tx, c)
	if err != nil {
		return err
	}
	if s.managed != nil {
		return s.requireManagedRetired(ctx, x, c)
	}
	if err = noProjectAgentMountResources(ctx, x, c); err != nil {
		return err
	}
	var remaining bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE agent_id=$1) OR EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_refs WHERE agent_id=$1)`, c.Request.AgentID.String()).Scan(&remaining)
	if err != nil {
		return portError(err)
	}
	if remaining {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

func (s *ProjectAgentRetirementReferences) managedAvailable(c ac.ProjectRetirementChange) error {
	s.managed.mu.Lock()
	defer s.managed.mu.Unlock()
	if s.managed.controls[c.Request.ProjectID] != nil || !s.managed.agents.ProjectControlJoined(c.Request.ProjectID) || !s.managed.executions.ProjectControlJoined(c.Request.ProjectID) {
		return fault(f.ResourceBusy)
	}
	return nil
}
func (s *ProjectAgentRetirementReferences) retireManaged(ctx context.Context, x postgres.SQLExecutor, c ac.ProjectRetirementChange) error {
	if err := s.managedAvailable(c); err != nil {
		return err
	}
	p, err := readProjectAgentProof(ctx, x, c.Request.ProjectID, c.Request.AgentID)
	if err != nil {
		return err
	}
	if !p.matches(c) {
		return fault(f.ConfirmationStale)
	}
	if err = projectProofResourcesAbsent(ctx, x, p); err != nil {
		return err
	}
	if err = noProjectExecutionMounts(ctx, x, c.Request.ProjectID); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `DELETE FROM agenteam_mount.agent_mount_heads h WHERE h.agent_id=$1::uuid AND h.project_id=$2::uuid AND h.owner_version=$3 AND to_jsonb(h)=$4::jsonb`, c.Request.AgentID.String(), c.Request.ProjectID.String(), int64(c.Request.ExpectedVersion), []byte(p.snapshot.Head))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	before, err := json.Marshal(p.before)
	if err != nil {
		return portError(err)
	}
	tag, err = x.Exec(ctx, `DELETE FROM agenteam_mount.project_agent_retirements WHERE project_id=$1::uuid AND agent_id=$2::uuid AND operation_id=$3::uuid AND project_version=$4 AND owner_version=$5 AND before_digest=$6 AND snapshot_digest=$7 AND process_id=$8::uuid AND before_config=$9::jsonb AND mount_snapshot=$10::jsonb`, p.project.String(), p.agent.String(), p.operation.String(), int64(p.version), int64(p.ownerVersion), string(p.beforeDigest), string(p.digest), p.process.String(), before, p.raw)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}
func (s *ProjectAgentRetirementReferences) requireManagedRetired(ctx context.Context, x postgres.SQLExecutor, c ac.ProjectRetirementChange) error {
	if err := s.managedAvailable(c); err != nil {
		return err
	}
	if err := noProjectExecutionMounts(ctx, x, c.Request.ProjectID); err != nil {
		return err
	}
	var remains bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE agent_id=$1::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_refs WHERE agent_id=$1::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces WHERE agent_id=$1::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.mounts WHERE agent_id=$1::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.project_agent_retirements WHERE agent_id=$1::uuid)`, c.Request.AgentID.String()).Scan(&remains)
	if err != nil {
		return portError(err)
	}
	if remains {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

var _ ac.ProjectRetirementReferences = (*ProjectAgentRetirementReferences)(nil)
