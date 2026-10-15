package memory

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/jackc/pgx/v5"
)

func (a *ModelAuthority) generationWitness(ctx context.Context, r mc.ServingSnapshotRetentionRequest) (*retainedGeneration, generationTarget, error) {
	if checkContext(ctx) != nil || a == nil || r.Validate() != nil {
		return nil, generationTarget{}, fault(f.Forbidden)
	}
	g, _ := ctx.Value(generationKey{}).(*retainedGeneration)
	if g == nil || g.models.authority != a || g.models.domain != a.domain || g.entry.service != a.domain {
		return nil, generationTarget{}, fault(f.Forbidden)
	}
	g.models.mu.Lock()
	_, active := g.models.builds[g]
	g.models.mu.Unlock()
	if !active || !r.Actor.Equal(g.actor) {
		return nil, generationTarget{}, fault(f.Forbidden)
	}
	target := g.target
	if r.Owner.GenerationID != target.serving.GenerationID {
		if g.old == nil || r.Action != mc.ReleaseServingSnapshot || r.Owner.GenerationID != g.old.serving.GenerationID {
			return nil, generationTarget{}, fault(f.Forbidden)
		}
		target = *g.old
	}
	expected := g.request(target, r.Action)
	got, err := mc.ServingSnapshotRetentionBinding(r)
	want, _ := mc.ServingSnapshotRetentionBinding(expected)
	if err != nil || got != want {
		return nil, generationTarget{}, fault(f.Forbidden)
	}
	return g, target, nil
}
func (a *ModelAuthority) DiscoverServingSnapshot(ctx context.Context, r mc.ServingSnapshotRetentionRequest) (mc.ConsumerDependencies, error) {
	g, target, err := a.generationWitness(ctx, r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ServingSnapshotRetentionBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	raw, err := encode(struct {
		Call, Process, Operation string
		SourceVersion            f.Version
	}{target.call.String(), target.process.String(), g.entry.operation.String(), g.source.Version})
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	locks, err := g.locks()
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: digest(raw), Locks: locks})
}
func (a *ModelAuthority) ValidateServingSnapshotInTx(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, p mc.ConsumerDependencies) error {
	return a.validateGeneration(ctx, tx, r, p, false)
}

// The observation gate accepts only the before/after facts of this original
// operation. It cannot authorize Apply or a new embedding after an Unknown.
func (a *ModelAuthority) ValidateServingSnapshotObservationInTx(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, p mc.ConsumerDependencies) error {
	return a.validateGeneration(ctx, tx, r, p, true)
}
func (a *ModelAuthority) validateGeneration(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, p mc.ConsumerDependencies, observation bool) error {
	g, target, err := a.generationWitness(ctx, r)
	if err != nil {
		return err
	}
	current, err := a.DiscoverServingSnapshot(ctx, r)
	if err != nil {
		return err
	}
	if p.Validate() != nil || !p.Matches(a.issuer, current.Details().Binding, current.Details().Mapping) {
		return fault(f.Forbidden)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, current.RequiredLocks()); err != nil {
		return portError(err)
	}
	if a.guard == nil {
		return fault(f.DependencyUnbound)
	}
	if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	cleanup := ctx.Value(generationCleanupKey{}) == g
	if !cleanup || !observation && (r.Action != mc.ReleaseServingSnapshot || target.serving.GenerationID != g.target.serving.GenerationID) {
		if err := a.domain.requireAccess(ctx, tx, g.entry, false); err != nil {
			return err
		}
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	var project, agent, snapshot, status, retention, call, process string
	var dimension int
	var version, namespace int64
	err = x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,snapshot_id::text,dimension,status,owner_version,retention_state,source_call_id::text,process_id::text,namespace_version FROM agenteam_memory.generations WHERE generation_id=$1`, target.serving.GenerationID).Scan(&project, &agent, &snapshot, &dimension, &status, &version, &retention, &call, &process, &namespace)
	if errors.Is(err, pgx.ErrNoRows) && observation && r.Action == mc.RetainServingSnapshot {
		return nil
	}
	if err != nil {
		return portError(err)
	}
	if project != g.entry.namespace.ProjectID.String() || agent != g.entry.namespace.AgentID.String() || snapshot != target.serving.SnapshotID.String() || dimension != target.serving.Dimension || call != target.call.String() || process != target.process.String() || target.serving.GenerationID == g.target.serving.GenerationID && f.Version(namespace) != g.source.Version {
		return fault(f.Forbidden)
	}
	if r.Action == mc.RetainServingSnapshot {
		if version != 1 || status != "building" && status != "ready" || retention != "retained" {
			return fault(f.InvalidState)
		}
	} else {
		terminal := version == 2 && status == "retired" && (retention == "retained" || retention == "released")
		before := observation && version == 1 && (status == "building" || status == "ready") && retention == "retained"
		if !terminal && !before {
			return fault(f.InvalidState)
		}
	}
	// A caller cannot assign somebody else's snapshot to a fabricated generation.
	var matched bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_memory.model_calls WHERE call_id=$1 AND project_id=$2 AND agent_id=$3 AND snapshot_id=$4 AND process_id=$5 AND purpose='memory_embedding')`, target.call.String(), project, agent, snapshot, process).Scan(&matched)
	if err != nil {
		return portError(err)
	}
	if !matched {
		return fault(f.Forbidden)
	}
	return nil
}

var _ mc.ServingSnapshotAuthority = (*ModelAuthority)(nil)
