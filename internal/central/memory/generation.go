package memory

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

type generationSource struct {
	Version f.Version
	Records []c.Record
}
type generationVector struct {
	Target c.Target
	Vector []float64
}

func (s *Service) captureGeneration(ctx context.Context, entry *call) (generationSource, error) {
	out := generationSource{Records: []c.Record{}}
	cause, err := readCause("index-source")
	if err != nil {
		return out, err
	}
	err = s.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		var version int64
		if err := x.QueryRow(ctx, `SELECT version FROM agenteam_memory.namespaces WHERE project_id=$1 AND agent_id=$2`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String()).Scan(&version); err != nil {
			return portError(err)
		}
		out.Version = f.Version(version)
		rows, err := x.Query(ctx, recordSelect+` WHERE m.project_id=$1 AND m.agent_id=$2 AND m.status='active' AND r.status='active' ORDER BY m.memory_id`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String())
		if err != nil {
			return portError(err)
		}
		defer rows.Close()
		for rows.Next() {
			record, err := scanRecord(rows, entry.namespace)
			if err != nil {
				return err
			}
			out.Records = append(out.Records, *record)
		}
		return portError(rows.Err())
	})
	if err != nil {
		return generationSource{}, err
	}
	return out, nil
}

// publishGeneration is the single profile-switch boundary. A build remains
// invisible unless every captured active revision is still current. Failed or
// stale builds never retire the old serving embedding space.
func (g *retainedGeneration) publish(ctx context.Context, vectors []generationVector) (rc.ServingIdentity, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, source, snapshot, dimension := g.entry, g.source, g.target.serving.SnapshotID, g.target.serving.Dimension
	var out rc.ServingIdentity
	if !g.retained || g.done || g.retainUnknown || g.publishUnknown {
		return out, fault(f.InvalidState)
	}
	if source.Version.Validate() != nil || snapshot.Validate() != nil || dimension < 1 || dimension > 4096 || len(source.Records) != len(vectors) {
		return out, fault(f.InvalidArgument)
	}
	encoded := make([]string, len(vectors))
	seen := map[c.ID]bool{}
	for n, record := range source.Records {
		if record.Validate() != nil || record.Namespace != entry.namespace || record.Current.Status != c.Active || vectors[n].Target.MemoryID != record.Current.MemoryID || vectors[n].Target.Revision != record.Current.Revision || seen[record.Current.MemoryID] {
			return out, fault(f.InvalidArgument)
		}
		seen[record.Current.MemoryID] = true
		var err error
		encoded[n], err = vectorText(vectors[n].Vector, dimension)
		if err != nil {
			return out, err
		}
	}
	out = g.target.serving
	if g.old != nil {
		var err error
		g.oldReleasePlan, err = g.models.deps.ServingRetention.DiscoverServingSnapshotRetention(g.context(ctx), g.request(*g.old, mc.ReleaseServingSnapshot))
		if err != nil {
			return rc.ServingIdentity{}, err
		}
	}
	err := g.transaction(ctx, "index-publish", false, []mc.ServingSnapshotRetentionPlan{g.retainPlan, g.oldReleasePlan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		retained, err := g.models.deps.ServingRetention.ServingSnapshotRetentionAppliedInTx(ctx, tx, g.request(g.target, mc.RetainServingSnapshot), g.retainPlan)
		if err != nil {
			return err
		}
		if !retained {
			return fault(f.InvalidState)
		}
		n := entry.namespace
		var version, count int64
		if err := x.QueryRow(ctx, `SELECT version FROM agenteam_memory.namespaces WHERE project_id=$1 AND agent_id=$2`, n.ProjectID.String(), n.AgentID.String()).Scan(&version); err != nil {
			return portError(err)
		}
		if f.Version(version) != source.Version {
			return fault(f.VersionConflict)
		}
		if err := x.QueryRow(ctx, `SELECT count(*) FROM agenteam_memory.memories WHERE project_id=$1 AND agent_id=$2 AND status='active'`, n.ProjectID.String(), n.AgentID.String()).Scan(&count); err != nil {
			return portError(err)
		}
		if count != int64(len(source.Records)) {
			return fault(f.VersionConflict)
		}
		for _, record := range source.Records {
			current, err := readRecord(ctx, x, n, record.Current.MemoryID)
			if err != nil {
				return err
			}
			if current == nil || current.Current.Status != c.Active || current.Current.Revision != record.Current.Revision {
				return fault(f.VersionConflict)
			}
		}
		for index, record := range source.Records {
			_, err = x.Exec(ctx, `INSERT INTO agenteam_memory.projections(generation_id,project_id,agent_id,memory_id,revision,embedding,index_text) VALUES($1,$2,$3,$4,$5,$6::vector,$7)`, out.GenerationID, n.ProjectID.String(), n.AgentID.String(), record.Current.MemoryID.String(), int64(record.Current.Revision), encoded[index], record.Current.Text)
			if err != nil {
				return portError(err)
			}
		}
		if g.old != nil {
			changed, err := x.Exec(ctx, `UPDATE agenteam_memory.generations SET status='retired',owner_version=2,retention_state='released' WHERE generation_id=$1 AND status='ready' AND owner_version=1 AND retention_state='retained'`, g.old.serving.GenerationID)
			if err != nil {
				return portError(err)
			}
			if changed.RowsAffected() != 1 {
				return fault(f.VersionConflict)
			}
			if err := g.models.deps.ServingRetention.ApplyServingSnapshotRetentionInTx(ctx, tx, g.request(*g.old, mc.ReleaseServingSnapshot), g.oldReleasePlan); err != nil {
				return err
			}
		}
		changed, err := x.Exec(ctx, `UPDATE agenteam_memory.generations SET status='ready' WHERE generation_id=$1 AND status='building' AND retention_state='retained' AND owner_version=1`, out.GenerationID)
		if err != nil {
			return portError(err)
		}
		if changed.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		var previous *string
		if g.old != nil {
			value := g.old.serving.GenerationID
			previous = &value
		}
		changed, err = x.Exec(ctx, `UPDATE agenteam_memory.namespaces SET serving_generation_id=$3 WHERE project_id=$1 AND agent_id=$2 AND serving_generation_id IS NOT DISTINCT FROM $4::uuid`, n.ProjectID.String(), n.AgentID.String(), out.GenerationID, previous)
		if err != nil {
			return portError(err)
		}
		if changed.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		_, err = x.Exec(ctx, `DELETE FROM agenteam_memory.index_intents WHERE project_id=$1 AND agent_id=$2`, n.ProjectID.String(), n.AgentID.String())
		return portError(err)
	})
	if isUnknown(err) {
		g.publishUnknown = true
	}
	if err != nil {
		return rc.ServingIdentity{}, err
	}
	g.complete()
	return out, nil
}
