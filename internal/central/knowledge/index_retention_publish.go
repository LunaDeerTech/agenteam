package knowledge

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (g *indexRetention) publish(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done || !g.running || !g.pinned || g.publishUnknown || g.published {
		return fault(f.InvalidState)
	}
	port := g.service.deps.ServingRetention
	if g.old != nil {
		var err error
		g.oldReleasePlan, err = port.DiscoverServingSnapshotRetention(g.context(ctx), g.request(*g.old, mc.ReleaseServingSnapshot))
		if err != nil {
			return err
		}
	}
	err := g.transaction(ctx, false, []mc.ServingSnapshotRetentionPlan{g.pinPlan, g.oldReleasePlan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		p := g.target.Profile
		var complete bool
		if err := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_knowledge.documents d WHERE d.project_id=$1 AND d.status='active' AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.index_generations g WHERE g.project_id=d.project_id AND g.document_id=d.id AND g.document_version=d.content_version AND g.profile_id=$2 AND g.state='ready' AND g.lexical_complete AND g.dense_complete)) AND EXISTS(SELECT 1 FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p ON p.project_id=st.project_id AND p.id=st.target_profile_id WHERE st.project_id=$1 AND p.id=$2 AND p.state='building' AND p.snapshot_id=$3 AND p.retention_state='retained')`, p.Project.String(), p.ID, p.Snapshot.String()).Scan(&complete); err != nil {
			return unavailable(err)
		}
		if !complete {
			return fault(f.VersionConflict)
		}
		var serving string
		if err := x.QueryRow(ctx, `SELECT COALESCE(serving_profile_id::text,'') FROM agenteam_knowledge.index_state WHERE project_id=$1`, p.Project.String()).Scan(&serving); err != nil {
			return unavailable(err)
		}
		if g.old == nil && serving != "" || g.old != nil && serving != g.old.Profile.ID {
			return fault(f.VersionConflict)
		}
		if g.old != nil {
			tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_profiles SET state='retired',retention_state='released' WHERE project_id=$1 AND id=$2 AND state='active' AND version=$3 AND retention_state='retained'`, p.Project.String(), g.old.Profile.ID, int64(g.old.Profile.Version))
			if err != nil {
				return portError(err)
			}
			if tag.RowsAffected() != 1 {
				return fault(f.VersionConflict)
			}
			if err := port.ApplyServingSnapshotRetentionInTx(ctx, tx, g.request(*g.old, mc.ReleaseServingSnapshot), g.oldReleasePlan); err != nil {
				return err
			}
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_profiles SET state='active',activated_at=clock_timestamp() WHERE project_id=$1 AND id=$2 AND state='building' AND retention_state='retained'`, p.Project.String(), p.ID); err != nil {
			return portError(err)
		}
		if _, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_state SET serving_profile_id=$2,version=version+1 WHERE project_id=$1 AND target_profile_id=$2`, p.Project.String(), p.ID); err != nil {
			return portError(err)
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET indexing_status='ready' WHERE project_id=$1 AND status='active'`, p.Project.String())
		return portError(err)
	})
	if indexRetentionUnknown(err) {
		g.publishUnknown = true
	}
	if err == nil {
		g.published = true
	}
	return err
}

// Caller holds g.mu; this is a read-only confirmation of the original publish.
func (g *indexRetention) observePublication(ctx context.Context) error {
	published := false
	err := g.transaction(ctx, true, []mc.ServingSnapshotRetentionPlan{g.pinPlan, g.oldReleasePlan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		p := g.target.Profile
		var state, serving string
		if err := x.QueryRow(ctx, `SELECT p.state,COALESCE(st.serving_profile_id::text,'') FROM agenteam_knowledge.index_profiles p JOIN agenteam_knowledge.index_state st USING(project_id) WHERE p.project_id=$1 AND p.id=$2`, p.Project.String(), p.ID).Scan(&state, &serving); err != nil {
			return unavailable(err)
		}
		if state == "building" && serving != p.ID {
			return nil
		}
		if state != "active" || serving != p.ID {
			return fault(f.InvalidState)
		}
		port := g.service.deps.ServingRetention
		retained, err := port.ServingSnapshotRetentionAppliedInTx(ctx, tx, g.request(g.target, mc.RetainServingSnapshot), g.pinPlan)
		if err != nil {
			return err
		}
		if !retained {
			return fault(f.InvalidState)
		}
		if g.old != nil {
			released, err := port.ServingSnapshotRetentionAppliedInTx(ctx, tx, g.request(*g.old, mc.ReleaseServingSnapshot), g.oldReleasePlan)
			if err != nil {
				return err
			}
			if !released {
				return fault(f.InvalidState)
			}
		}
		published = true
		return nil
	})
	if err != nil {
		return err
	}
	g.publishUnknown, g.published = false, published
	return nil
}
