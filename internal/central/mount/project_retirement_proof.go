package mount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// The proof preserves the original nonempty configuration. It is not an Agent
// mutation or a synthetic empty postimage. Only the original Agent Applied
// witness can consume it, in the canonical Agent deletion transaction.
type projectAgentRetirementProof struct {
	project      i.ProjectID
	operation    pc.OperationID
	version      f.Version
	agent        i.AgentID
	ownerVersion f.Version
	before       ac.AgentConfig
	beforeDigest f.Digest
	snapshot     projectMountSnapshot
	raw          []byte
	digest       f.Digest
	process      oc.ProcessID
}

func newProjectAgentProof(call *projectLifecycleControl, process oc.ProcessID, b *projectMountBatch) (*projectAgentRetirementProof, error) {
	d, err := ac.ProjectRetirementBeforeDigest(b.before)
	if err != nil {
		return nil, err
	}
	return &projectAgentRetirementProof{project: call.scope.ProjectID, operation: call.cause.OperationID, version: call.cause.ProjectVersion, agent: b.agent, ownerVersion: b.before.Fields().Core.Version, before: b.before.Clone(), beforeDigest: d, snapshot: b.snapshot, raw: bytes.Clone(b.raw), digest: projectJSONDigest(b.raw), process: process}, nil
}
func (p *projectAgentRetirementProof) matches(c ac.ProjectRetirementChange) bool {
	if p == nil || c.Validate() != nil {
		return false
	}
	d, err := ac.ProjectRetirementBeforeDigest(c.Before)
	return err == nil && p.project == c.Request.ProjectID && p.operation == c.Request.Cause.OperationID && p.version == c.Request.Cause.ProjectVersion && p.agent == c.Request.AgentID && p.ownerVersion == c.Request.ExpectedVersion && d == p.beforeDigest
}
func sameProjectAgentProof(a, b *projectAgentRetirementProof) bool {
	return a != nil && b != nil && a.project == b.project && a.operation == b.operation && a.version == b.version && a.agent == b.agent && a.ownerVersion == b.ownerVersion && a.beforeDigest == b.beforeDigest && a.digest == b.digest && a.process == b.process && bytes.Equal(a.raw, b.raw)
}
func readProjectAgentProof(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, a i.AgentID) (*projectAgentRetirementProof, error) {
	var project, operation, agent, process string
	var version, ownerVersion int64
	var before, raw []byte
	proof := &projectAgentRetirementProof{}
	err := x.QueryRow(ctx, `SELECT project_id::text,operation_id::text,project_version,agent_id::text,owner_version,before_config,before_digest,mount_snapshot,snapshot_digest,process_id::text FROM agenteam_mount.project_agent_retirements WHERE project_id=$1::uuid AND agent_id=$2::uuid`, p.String(), a.String()).Scan(&project, &operation, &version, &agent, &ownerVersion, &before, &proof.beforeDigest, &raw, &proof.digest, &process)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	if len(before) > projectMountBytes || len(raw) > projectMountBytes {
		return nil, fault(f.InvalidState)
	}
	proof.project, err = f.ParseID[i.Project](project)
	if err != nil || proof.project != p {
		return nil, fault(f.InvalidState)
	}
	proof.agent, err = f.ParseID[i.Agent](agent)
	if err != nil || proof.agent != a {
		return nil, fault(f.InvalidState)
	}
	proof.operation, err = f.ParseID[pc.Operation](operation)
	if err != nil {
		return nil, fault(f.InvalidState)
	}
	proof.process, err = f.ParseID[oc.Process](process)
	if err != nil {
		return nil, fault(f.InvalidState)
	}
	proof.version, proof.ownerVersion = f.Version(version), f.Version(ownerVersion)
	if proof.version.Validate() != nil || proof.ownerVersion.Validate() != nil || json.Unmarshal(before, &proof.before) != nil {
		return nil, fault(f.InvalidState)
	}
	d, err := ac.ProjectRetirementBeforeDigest(proof.before)
	if err != nil || d != proof.beforeDigest {
		return nil, fault(f.InvalidState)
	}
	core := proof.before.Fields().Core
	if core.ID != a || core.ProjectID != p || core.Version != proof.ownerVersion {
		return nil, fault(f.InvalidState)
	}
	proof.raw, err = canonicalProjectJSON(raw)
	if err != nil || projectJSONDigest(proof.raw) != proof.digest || json.Unmarshal(proof.raw, &proof.snapshot) != nil {
		return nil, fault(f.InvalidState)
	}
	if err = validateProjectMountSnapshot(proof.snapshot, proof.before); err != nil {
		return nil, err
	}
	return proof, ctx.Err()
}
func insertProjectAgentProof(ctx context.Context, x postgres.SQLExecutor, p *projectAgentRetirementProof) error {
	before, err := json.Marshal(p.before)
	if err != nil {
		return portError(err)
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_mount.project_agent_retirements(project_id,operation_id,project_version,agent_id,owner_version,before_config,before_digest,mount_snapshot,snapshot_digest,process_id) VALUES($1::uuid,$2::uuid,$3,$4::uuid,$5,$6::jsonb,$7,$8::jsonb,$9,$10::uuid)`, p.project.String(), p.operation.String(), int64(p.version), p.agent.String(), int64(p.ownerVersion), before, string(p.beforeDigest), p.raw, string(p.digest), p.process.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}
func projectProofResourcesAbsent(ctx context.Context, x postgres.SQLExecutor, p *projectAgentRetirementProof) error {
	current, _, err := readProjectMountSnapshot(ctx, x, p.project, p.agent)
	if err != nil {
		return err
	}
	a, err := canonicalProjectJSON(current.Head)
	if err != nil {
		return err
	}
	b, err := canonicalProjectJSON(p.snapshot.Head)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) || len(current.References) != 0 || len(current.Workspaces) != 0 || len(current.Definitions) != 0 {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func requireProjectMountsRetired(ctx context.Context, x postgres.SQLExecutor, call *projectLifecycleControl) error {
	if err := noProjectExecutionMounts(ctx, x, call.scope.ProjectID); err != nil {
		return err
	}
	var remaining bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mount.mounts WHERE project_id=$1::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces WHERE project_id=$1::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_refs WHERE project_id=$1::uuid) OR EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads h LEFT JOIN agenteam_mount.project_agent_retirements r ON r.project_id=h.project_id AND r.agent_id=h.agent_id WHERE h.project_id=$1::uuid AND (r.agent_id IS NULL OR r.owner_version<>h.owner_version OR r.operation_id<>$2::uuid OR r.project_version<>$3)) OR EXISTS(SELECT 1 FROM agenteam_mount.project_agent_retirements r LEFT JOIN agenteam_mount.agent_mount_heads h ON h.project_id=r.project_id AND h.agent_id=r.agent_id WHERE r.project_id=$1::uuid AND (h.agent_id IS NULL OR r.operation_id<>$2::uuid OR r.project_version<>$3))`, call.scope.ProjectID.String(), call.cause.OperationID.String(), int64(call.cause.ProjectVersion)).Scan(&remaining)
	if err != nil {
		return portError(err)
	}
	if remaining {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}
