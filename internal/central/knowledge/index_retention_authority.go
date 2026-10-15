package knowledge

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func (a *IndexAuthority) retentionWitness(ctx context.Context, r mc.ServingSnapshotRetentionRequest) (*indexRetention, indexRetentionTarget, error) {
	if ctx == nil || a == nil || r.Validate() != nil {
		return nil, indexRetentionTarget{}, fault(f.Forbidden)
	}
	g, _ := ctx.Value(indexRetentionKey{}).(*indexRetention)
	if g == nil || g.service == nil || g.service.authority != a || !r.Actor.Equal(g.actor) {
		return nil, indexRetentionTarget{}, fault(f.Forbidden)
	}
	g.service.mu.Lock()
	_, active := g.service.retentions[g]
	g.service.mu.Unlock()
	if !active {
		return nil, indexRetentionTarget{}, fault(f.Forbidden)
	}
	target := g.target
	if r.Owner.GenerationID != target.Profile.ID {
		if g.old == nil || r.Action != mc.ReleaseServingSnapshot || r.Owner.GenerationID != g.old.Profile.ID {
			return nil, indexRetentionTarget{}, fault(f.Forbidden)
		}
		target = *g.old
	}
	want, err := mc.ServingSnapshotRetentionBinding(g.request(target, r.Action))
	got, actualErr := mc.ServingSnapshotRetentionBinding(r)
	if err != nil || actualErr != nil || want != got || target.Operation == "" || target.Process.Validate() != nil {
		return nil, indexRetentionTarget{}, fault(f.Forbidden)
	}
	return g, target, nil
}
func (a *IndexAuthority) DiscoverServingSnapshot(ctx context.Context, r mc.ServingSnapshotRetentionRequest) (mc.ConsumerDependencies, error) {
	if hasIndexProjectCleanup(ctx) {
		return a.discoverIndexProjectRelease(ctx, r)
	}
	g, target, err := a.retentionWitness(ctx, r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ServingSnapshotRetentionBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	mapping, err := g.mapping(target)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	locks, err := g.locks()
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: mapping, Locks: locks})
}
func (a *IndexAuthority) ValidateServingSnapshotInTx(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies) error {
	return a.validateRetention(ctx, tx, r, plan, false)
}
func (a *IndexAuthority) ValidateServingSnapshotObservationInTx(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies) error {
	return a.validateRetention(ctx, tx, r, plan, true)
}
func (a *IndexAuthority) validateRetention(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies, observation bool) error {
	if hasIndexProjectCleanup(ctx) {
		return a.validateIndexProjectRelease(ctx, tx, r, plan, observation)
	}
	g, target, err := a.retentionWitness(ctx, r)
	if err != nil {
		return err
	}
	current, err := a.DiscoverServingSnapshot(ctx, r)
	if err != nil {
		return err
	}
	if plan.Validate() != nil || !plan.Matches(a.issuer, current.Details().Binding, current.Details().Mapping) {
		return fault(f.ResourceBusy)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, current.RequiredLocks()); err != nil {
		return portError(err)
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	cleanup := ctx.Value(indexRetentionCleanupKey{}) == g
	if !cleanup || !observation && (r.Action != mc.ReleaseServingSnapshot || target.Profile.ID != g.target.Profile.ID) {
		if err = g.service.documents.projectCallTx(ctx, tx, target.Profile.Project); err != nil {
			return err
		}
		if err := g.requireOwnerInTx(ctx, tx); err != nil {
			return err
		}
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	var version int64
	var snapshot, state, retention, source string
	var dimension int
	err = x.QueryRow(ctx, `SELECT version,COALESCE(snapshot_id::text,''),dimension,state,retention_state,COALESCE(source_operation_id::text,'') FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2`, target.Profile.Project.String(), target.Profile.ID).Scan(&version, &snapshot, &dimension, &state, &retention, &source)
	if err != nil {
		return unavailable(err)
	}
	beforePin := observation && r.Action == mc.RetainServingSnapshot && state == "building" && retention == "unretained" && snapshot == "" && source == ""
	if f.Version(version) != target.Profile.Version || dimension != target.Profile.Dimension || !beforePin && (snapshot != target.Profile.Snapshot.String() || source != target.Operation) {
		return fault(f.Forbidden)
	}
	if r.Action == mc.RetainServingSnapshot {
		if !beforePin && ((state != "building" && state != "ready" && state != "active") || retention != "retained") {
			return fault(f.InvalidState)
		}
	} else {
		terminal := (state == "retired" || state == "failed") && (retention == "retained" || retention == "released")
		before := observation && (state == "building" || state == "ready" || state == "active") && retention == "retained"
		if !terminal && !before {
			return fault(f.InvalidState)
		}
	}
	var matched bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.model_operations WHERE id=$1 AND project_id=$2 AND profile_id=$3 AND generation_id=$3 AND process_id=$4 AND kind='profile' AND snapshot_id=$5)`, target.Operation, target.Profile.Project.String(), target.Profile.ID, target.Process.String(), target.Profile.Snapshot.String()).Scan(&matched)
	if err != nil {
		return unavailable(err)
	}
	if !matched {
		return fault(f.Forbidden)
	}
	return nil
}

var _ mc.ServingSnapshotAuthority = (*IndexAuthority)(nil)
