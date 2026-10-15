package knowledge

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// This is a child of the original Project cleanup control, not an indexing
// admission. Its only Model action is releasing one captured serving reference.
type indexProjectRelease struct {
	state  *projectCallState
	call   *ownedProjectCleanup
	index  *IndexService
	target indexRetentionTarget
	before string
	actor  id.Actor
	plan   mc.ServingSnapshotRetentionPlan
}

func (g *indexProjectRelease) request() mc.ServingSnapshotRetentionRequest {
	p := g.target.Profile
	return mc.ServingSnapshotRetentionRequest{Actor: g.actor, Owner: mc.ServingSnapshotOwner{Kind: mc.KnowledgeConsumer, ProjectID: p.Project, GenerationID: p.ID}, SnapshotID: p.Snapshot, OwnerVersion: p.Version, Action: mc.ReleaseServingSnapshot}
}

// Model operations become retired/version 3 only after the original formal
// call-retirement transaction. An unfinished or cold operation blocks deletion;
// absence of a local owner is never evidence that its Model lease disappeared.
func requireIndexOperationsRetired(ctx context.Context, x postgres.SQLExecutor, p id.ProjectID) error {
	var pending bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.model_operations WHERE project_id=$1 AND (state<>'retired' OR version<>3))`, p.String()).Scan(&pending); err != nil {
		return unavailable(err)
	}
	if pending {
		return fault(f.ResourceBusy)
	}
	return nil
}

func (s *projectCallState) prepareIndexProjectCleanup(ctx context.Context, call *ownedProjectCleanup) (bool, error) {
	var target indexRetentionTarget
	var before string
	found := false
	result := s.cleanupTx(ctx, call, s.projectCleanupCause(call), func(run context.Context, x postgres.SQLExecutor) error {
		if !s.projectCleanupReturned(call) {
			return fault(f.ResourceBusy)
		}
		if err := requireIndexOperationsRetired(run, x, call.scope.ProjectID); err != nil {
			return err
		}
		var version int64
		var snapshot, process string
		target.Profile.Project = call.scope.ProjectID
		err := x.QueryRow(run, `SELECT p.id::text,p.version,p.snapshot_id::text,p.dimension,p.source_operation_id::text,o.process_id::text,p.state
 FROM agenteam_knowledge.index_profiles p
 JOIN agenteam_knowledge.model_operations o ON o.id=p.source_operation_id AND o.project_id=p.project_id AND o.profile_id=p.id AND o.kind='profile' AND o.generation_id=p.id AND o.snapshot_id=p.snapshot_id
 WHERE p.project_id=$1 AND p.retention_state='retained' ORDER BY p.id LIMIT 1`, call.scope.ProjectID.String()).Scan(&target.Profile.ID, &version, &snapshot, &target.Profile.Dimension, &target.Operation, &process, &before)
		if errors.Is(err, pgx.ErrNoRows) {
			// A malformed source must not make the join look like no retained pin.
			var retained bool
			if err = x.QueryRow(run, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND retention_state='retained')`, call.scope.ProjectID.String()).Scan(&retained); err != nil {
				return unavailable(err)
			}
			if retained {
				return fault(f.InvalidState)
			}
			return nil
		}
		if err != nil {
			return unavailable(err)
		}
		target.Profile.Version = f.Version(version)
		target.Profile.Snapshot, err = f.ParseID[mc.Snapshot](snapshot)
		if err != nil {
			return unavailable(err)
		}
		target.Process, err = f.ParseID[oc.Process](process)
		if err != nil {
			return unavailable(err)
		}
		found = true
		return nil
	})
	if result.State() == f.Unknown {
		return false, s.retainProjectCleanup(call, result)
	}
	if err := txError(result); err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	s.mu.Lock()
	index := s.index
	s.mu.Unlock()
	if index == nil || index.documents != s.service || nilPort(index.deps.ServingRetention) {
		return false, fault(f.DependencyUnbound)
	}
	scope, _ := id.InProject(call.scope.ProjectID)
	actor, err := index.authority.registration.Actor(call.cause.OperationID.String(), scope)
	if err != nil {
		return false, err
	}
	g := &indexProjectRelease{state: s, call: call, index: index, target: target, before: before, actor: actor}
	call.indexRelease = g
	plan, err := index.deps.ServingRetention.DiscoverServingSnapshotRetention(ctx, g.request())
	if err != nil {
		return false, err
	}
	if nilPort(plan) {
		return false, fault(f.DependencyUnbound)
	}
	g.plan = plan
	call.locks, err = ob.NormalizeLocks(append([]f.LockRequest{knowledgeProjectLock(call.scope.ProjectID, f.Exclusive)}, plan.RequiredLocks()...))
	if err != nil {
		return false, portError(err)
	}
	result = s.cleanupTx(ctx, call, s.projectCleanupCause(call), func(run context.Context, x postgres.SQLExecutor) error {
		current, err := listProjectCalls(run, x, call.scope.ProjectID)
		if err != nil {
			return err
		}
		if !equalProjectCalls(current, call.before) || !s.projectCleanupReturned(call) {
			return fault(f.ResourceBusy)
		}
		if err = requireIndexOperationsRetired(run, x, call.scope.ProjectID); err != nil {
			return err
		}
		tx, ok := run.Value(projectCleanupTxKey{}).(f.Tx)
		if !ok {
			return fault(f.DependencyUnbound)
		}
		p := target.Profile
		tag, err := x.Exec(run, `UPDATE agenteam_knowledge.index_profiles SET state='retired',retention_state='released'
 WHERE project_id=$1 AND id=$2 AND version=$3 AND snapshot_id=$4 AND source_operation_id=$5 AND state=$6 AND retention_state='retained'`, p.Project.String(), p.ID, int64(p.Version), p.Snapshot.String(), target.Operation, before)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		if err = index.deps.ServingRetention.ApplyServingSnapshotRetentionInTx(run, tx, g.request(), plan); err != nil {
			return err
		}
		return g.observe(run, tx, x)
	})
	if result.State() == f.Unknown {
		return false, s.retainProjectCleanup(call, result)
	}
	if err = txError(result); err != nil {
		return false, err
	}
	call.indexRelease = nil
	// One reference per explicit cleanup round. No physical deletion shares
	// this return or follows an Unknown confirmation.
	return false, nil
}

func (g *indexProjectRelease) observe(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
	applied, err := g.index.deps.ServingRetention.ServingSnapshotRetentionAppliedInTx(ctx, tx, g.request(), g.plan)
	if err != nil {
		return err
	}
	var state, retention string
	p := g.target.Profile
	err = x.QueryRow(ctx, `SELECT state,retention_state FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2 AND version=$3 AND snapshot_id=$4 AND source_operation_id=$5`, p.Project.String(), p.ID, int64(p.Version), p.Snapshot.String(), g.target.Operation).Scan(&state, &retention)
	if err != nil {
		return unavailable(err)
	}
	if applied && state == "retired" && retention == "released" || !applied && state == g.before && retention == "retained" {
		return nil
	}
	return fault(f.ConfirmationStale)
}
