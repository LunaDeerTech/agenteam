package knowledge

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func hasIndexProjectCleanup(ctx context.Context) bool {
	return ctx != nil && ctx.Value(projectCleanupContextKey{}) != nil
}

func (a *IndexAuthority) indexProjectReleaseWitness(ctx context.Context, r mc.ServingSnapshotRetentionRequest) (*indexProjectRelease, error) {
	if a == nil || ctx == nil || r.Validate() != nil || r.Action != mc.ReleaseServingSnapshot {
		return nil, fault(f.Forbidden)
	}
	w, ok := ctx.Value(projectCleanupContextKey{}).(projectCleanupWitness)
	if !ok || w.state == nil || w.call == nil {
		return nil, fault(f.Forbidden)
	}
	w.state.mu.Lock()
	g := w.call.indexRelease
	valid := g != nil && g.state == w.state && g.call == w.call && g.index != nil && g.index == w.state.index && g.index.authority == a &&
		w.state.cleanups[w.call.scope.ProjectID] == w.call && w.call.active && g.target.Profile.Project == w.call.scope.ProjectID
	w.state.mu.Unlock()
	if !valid {
		return nil, fault(f.Forbidden)
	}
	want, err := mc.ServingSnapshotRetentionBinding(g.request())
	got, actualErr := mc.ServingSnapshotRetentionBinding(r)
	if err != nil || actualErr != nil || want != got {
		return nil, fault(f.Forbidden)
	}
	return g, nil
}

func (a *IndexAuthority) discoverIndexProjectRelease(ctx context.Context, r mc.ServingSnapshotRetentionRequest) (mc.ConsumerDependencies, error) {
	g, err := a.indexProjectReleaseWitness(ctx, r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ServingSnapshotRetentionBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	// The original Delete control and immutable source tuple remain identical
	// on either side of release. Lifecycle state belongs in Tx validation.
	raw, err := json.Marshal(struct {
		Control, Delete, Source, SourceProcess, Instance string
		ProjectVersion                                   f.Version
	}{g.call.id.String(), g.call.cause.OperationID.String(), g.target.Operation, g.target.Process.String(), g.state.instance.String(), g.call.cause.ProjectVersion})
	if err != nil {
		return mc.ConsumerDependencies{}, internal(err)
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: ob.DigestBytes(raw), Locks: []f.LockRequest{knowledgeProjectLock(g.target.Profile.Project, f.Exclusive)}})
}

func (a *IndexAuthority) validateIndexProjectRelease(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies, observation bool) error {
	g, err := a.indexProjectReleaseWitness(ctx, r)
	if err != nil {
		return err
	}
	current, err := a.discoverIndexProjectRelease(ctx, r)
	if err != nil {
		return err
	}
	if plan.Validate() != nil || !plan.Matches(a.issuer, current.Details().Binding, current.Details().Mapping) {
		return fault(f.ConfirmationStale)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, current.RequiredLocks()); err != nil {
		return portError(err)
	}
	if err = g.index.documents.requireProjectCleanupCallInTx(ctx, tx, g.target.Profile.Project, g.call.cause.OperationID); err != nil {
		return err
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	p := g.target.Profile
	var state, retention string
	err = x.QueryRow(ctx, `SELECT p.state,p.retention_state FROM agenteam_knowledge.index_profiles p
 JOIN agenteam_knowledge.model_operations o ON o.id=p.source_operation_id AND o.project_id=p.project_id AND o.profile_id=p.id
 WHERE p.project_id=$1 AND p.id=$2 AND p.version=$3 AND p.snapshot_id=$4 AND p.dimension=$5 AND p.source_operation_id=$6
 AND o.process_id=$7 AND o.generation_id=p.id AND o.kind='profile' AND o.snapshot_id=p.snapshot_id AND o.state='retired' AND o.version=3`,
		p.Project.String(), p.ID, int64(p.Version), p.Snapshot.String(), p.Dimension, g.target.Operation, g.target.Process.String()).Scan(&state, &retention)
	if err != nil {
		return unavailable(err)
	}
	if state == "retired" && retention == "released" || observation && state == g.before && retention == "retained" {
		return nil
	}
	return fault(f.InvalidState)
}
