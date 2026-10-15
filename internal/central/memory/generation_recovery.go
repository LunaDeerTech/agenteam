package memory

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type abandonedGeneration struct {
	target  generationTarget
	version f.Version
}

// recoverGenerations retires only an unpublished generation whose source Model
// call has already retired and whose exact former process claim is unlocked.
// It never reconstructs or dispatches a Model call. The current original Tool
// owns this cleanup; Unknown retains that owner for the existing Join/Drain.
func (m *Models) recoverGenerations(ctx context.Context, entry *call) error {
	if m == nil || m.authority == nil || m.authority.guard == nil {
		return fault(f.DependencyUnbound)
	}
	cause, err := readCause("generation-recovery-candidates")
	if err != nil {
		return err
	}
	candidates := []abandonedGeneration{}
	err = m.domain.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		rows, err := x.Query(ctx, `SELECT g.generation_id::text,g.snapshot_id::text,g.dimension,g.namespace_version,g.source_call_id::text,g.process_id::text
 FROM agenteam_memory.generations g JOIN agenteam_memory.model_calls c ON c.call_id=g.source_call_id
 WHERE g.project_id=$1 AND g.agent_id=$2 AND g.status='building' AND g.retention_state='retained' AND g.owner_version=1 AND g.process_id<>$3
 AND c.project_id=g.project_id AND c.agent_id=g.agent_id AND c.snapshot_id=g.snapshot_id AND c.process_id=g.process_id AND c.purpose='memory_embedding' AND c.retired
 AND NOT EXISTS(SELECT 1 FROM agenteam_memory.namespaces n WHERE n.serving_generation_id=g.generation_id)
 ORDER BY g.created_at,g.generation_id LIMIT 128`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), m.authority.process.String())
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var candidate abandonedGeneration
			var snapshot, call, process string
			var version int64
			if err := rows.Scan(&candidate.target.serving.GenerationID, &snapshot, &candidate.target.serving.Dimension, &version, &call, &process); err != nil {
				return portError(err)
			}
			candidate.version = f.Version(version)
			candidate.target.serving.SnapshotID, err = f.ParseID[mc.Snapshot](snapshot)
			if err != nil {
				return portError(err)
			}
			candidate.target.call, err = f.ParseID[mc.Call](call)
			if err != nil {
				return portError(err)
			}
			candidate.target.process, err = f.ParseID[oc.Process](process)
			if err != nil || candidate.target.serving.Validate() != nil || candidate.version.Validate() != nil {
				return fault(f.DependencyUnavailable)
			}
			candidates = append(candidates, candidate)
		}
		return portError(rows.Err())
	})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if err := m.recoverGeneration(ctx, entry, candidate); err != nil {
			return err
		}
	}
	return nil
}

func (m *Models) recoverGeneration(ctx context.Context, entry *call, candidate abandonedGeneration) error {
	if candidate.target.process.Validate() != nil || candidate.target.call.Validate() != nil || candidate.target.serving.Validate() != nil || candidate.version.Validate() != nil || candidate.target.process == m.authority.process {
		return fault(f.Forbidden)
	}
	requestOwner, err := m.authority.profileRequest(entry)
	if err != nil {
		return err
	}
	if _, err := m.authority.profileWitness(ctx, requestOwner); err != nil {
		return err
	}
	// ConfirmStopped verifies the old claim, deployment, host and exact flock;
	// a different process ID or elapsed time alone never authorizes takeover.
	if err := m.authority.guard.ConfirmStopped(ctx, candidate.target.process); err != nil {
		return portError(err)
	}
	scope, _ := i.InProject(entry.namespace.ProjectID)
	actor, err := m.authority.registration.Actor(entry.operation.String(), scope)
	if err != nil {
		return err
	}
	g := &retainedGeneration{models: m, entry: entry, actor: actor, source: generationSource{Version: candidate.version}, target: candidate.target, retained: true}
	g.mu.Lock()
	defer g.mu.Unlock()
	m.mu.Lock()
	m.builds[g] = struct{}{}
	m.mu.Unlock()
	request := g.request(g.target, mc.ReleaseServingSnapshot)
	g.releasePlan, err = m.deps.ServingRetention.DiscoverServingSnapshotRetention(g.context(ctx), request)
	if err != nil {
		g.complete()
		return err
	}
	err = g.transaction(ctx, "generation-recover", false, []mc.ServingSnapshotRetentionPlan{g.releasePlan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := m.authority.guard.ConfirmStopped(ctx, candidate.target.process); err != nil {
			return portError(err)
		}
		var status, retention string
		var version int64
		var retired bool
		err := x.QueryRow(ctx, `SELECT g.status,g.owner_version,g.retention_state,c.retired
 FROM agenteam_memory.generations g JOIN agenteam_memory.model_calls c ON c.call_id=g.source_call_id
 WHERE g.generation_id=$1 AND g.project_id=$2 AND g.agent_id=$3 AND g.snapshot_id=$4 AND g.dimension=$5 AND g.namespace_version=$6 AND g.source_call_id=$7 AND g.process_id=$8
 AND c.project_id=g.project_id AND c.agent_id=g.agent_id AND c.snapshot_id=g.snapshot_id AND c.process_id=g.process_id AND c.purpose='memory_embedding'
 AND NOT EXISTS(SELECT 1 FROM agenteam_memory.namespaces n WHERE n.serving_generation_id=g.generation_id)`, g.target.serving.GenerationID, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), g.target.serving.SnapshotID.String(), g.target.serving.Dimension, int64(g.source.Version), g.target.call.String(), g.target.process.String()).Scan(&status, &version, &retention, &retired)
		if errors.Is(err, pgx.ErrNoRows) {
			return fault(f.VersionConflict)
		}
		if err != nil {
			return portError(err)
		}
		if !retired || status != "building" || version != 1 || retention != "retained" {
			return fault(f.InvalidState)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_memory.generations SET status='retired',owner_version=2,retention_state='released' WHERE generation_id=$1`, g.target.serving.GenerationID)
		if err != nil {
			return portError(err)
		}
		return m.deps.ServingRetention.ApplyServingSnapshotRetentionInTx(ctx, tx, request, g.releasePlan)
	})
	if isUnknown(err) {
		// Existing cleanup first observes this exact original release plan;
		// it never repeats embedding or publishes the abandoned generation.
		g.releaseUnknown = true
		return err
	}
	g.complete()
	return err
}
