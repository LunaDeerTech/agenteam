package memory

import (
	"context"
	"errors"
	"slices"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	"github.com/jackc/pgx/v5"
)

type generationTarget struct {
	serving rc.ServingIdentity
	call    mc.CallID
	process oc.ProcessID
}

// A pending build owns its reference cleanup independently of a shorter Model
// call. Only publishing or confirmed release can retire this private owner.
type retainedGeneration struct {
	mu             sync.Mutex
	models         *Models
	entry          *call
	actor          i.Actor
	source         generationSource
	profile        mc.EmbeddingProfile
	target         generationTarget
	old            *generationTarget
	retainPlan     mc.ServingSnapshotRetentionPlan
	releasePlan    mc.ServingSnapshotRetentionPlan
	oldReleasePlan mc.ServingSnapshotRetentionPlan
	retainUnknown  bool
	releaseUnknown bool
	publishUnknown bool
	retained       bool
	done           bool
	building       bool
}
type generationKey struct{}
type generationCleanupKey struct{}

func (g *retainedGeneration) request(target generationTarget, action mc.ServingSnapshotRetentionAction) mc.ServingSnapshotRetentionRequest {
	version := f.Version(1)
	if action == mc.ReleaseServingSnapshot {
		version = 2
	}
	agent := g.entry.namespace.AgentID
	return mc.ServingSnapshotRetentionRequest{Actor: g.actor, Owner: mc.ServingSnapshotOwner{Kind: mc.MemoryConsumer, ProjectID: g.entry.namespace.ProjectID, AgentID: &agent, GenerationID: target.serving.GenerationID}, SnapshotID: target.serving.SnapshotID, OwnerVersion: version, Action: action}
}
func (g *retainedGeneration) context(ctx context.Context) context.Context {
	return context.WithValue(context.WithValue(ctx, callKey{}, g.entry), generationKey{}, g)
}
func (m *Models) newGeneration(ctx context.Context, entry *call, source generationSource, profile mc.EmbeddingProfile) (*retainedGeneration, error) {
	if profile.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	id, err := f.NewID[struct{}]()
	if err != nil {
		return nil, portError(err)
	}
	scope, _ := i.InProject(entry.namespace.ProjectID)
	actor, err := m.authority.registration.Actor(entry.operation.String(), scope)
	if err != nil {
		return nil, err
	}
	g := &retainedGeneration{models: m, entry: entry, actor: actor, source: source, profile: profile, building: true, target: generationTarget{serving: rc.ServingIdentity{GenerationID: id.String(), Dimension: profile.Dimensions}, process: m.authority.process}}
	cause, err := readCause("generation-owner")
	if err != nil {
		return nil, err
	}
	err = m.domain.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		var old generationTarget
		var snapshot, call, process string
		err := x.QueryRow(ctx, `SELECT g.generation_id::text,g.snapshot_id::text,g.dimension,g.source_call_id::text,g.process_id::text FROM agenteam_memory.namespaces n JOIN agenteam_memory.generations g ON g.generation_id=n.serving_generation_id WHERE n.project_id=$1 AND n.agent_id=$2 AND g.status='ready' AND g.owner_version=1 AND g.retention_state='retained'`, entry.namespace.ProjectID.String(), entry.namespace.AgentID.String()).Scan(&old.serving.GenerationID, &snapshot, &old.serving.Dimension, &call, &process)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return portError(err)
		}
		old.serving.SnapshotID, err = f.ParseID[mc.Snapshot](snapshot)
		if err != nil {
			return portError(err)
		}
		old.call, err = f.ParseID[mc.Call](call)
		if err != nil {
			return portError(err)
		}
		old.process, err = f.ParseID[oc.Process](process)
		if err != nil {
			return portError(err)
		}
		g.old = &old
		return nil
	})
	return g, err
}
func (g *retainedGeneration) locks(plans ...mc.ServingSnapshotRetentionPlan) ([]f.LockRequest, error) {
	locks := append(slices.Clone(g.entry.locks), namespaceLock(g.entry.namespace, f.Exclusive))
	for _, p := range plans {
		if p != nil {
			locks = append(locks, p.RequiredLocks()...)
		}
	}
	return normalizeLocks(locks)
}
func (g *retainedGeneration) transaction(ctx context.Context, name string, cleanup bool, plans []mc.ServingSnapshotRetentionPlan, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	run := g.context(ctx)
	if cleanup {
		run = context.WithValue(run, generationCleanupKey{}, g)
	}
	locks, err := g.locks(plans...)
	if err != nil {
		return err
	}
	cause, err := readCause(name)
	if err != nil {
		return err
	}
	result := g.models.domain.store.WithinTx(run, cause, func(ctx context.Context, tx f.Tx) error {
		s := g.models.domain
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := g.models.authority.guard.RequireCurrentProcessInTx(ctx, tx, g.models.authority.process); err != nil {
			return portError(err)
		}
		if !cleanup {
			if err := s.requireAccess(ctx, tx, g.entry, false); err != nil {
				return err
			}
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	})
	return transactionError(result)
}

func (g *retainedGeneration) pin(ctx context.Context, p *modelPending) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	profileData, err := encode(g.profile)
	if err != nil {
		return err
	}
	profileDigest, err := g.profile.Digest()
	if err != nil || !profileMatchesSnapshot(g.profile, p.resolved.Snapshot) {
		return fault(f.VersionConflict)
	}
	g.target.serving.SnapshotID = p.resolved.Snapshot.ID
	g.target.call = p.inv.id
	m := g.models
	m.mu.Lock()
	m.builds[g] = struct{}{}
	m.mu.Unlock()
	request := g.request(g.target, mc.RetainServingSnapshot)
	plan, err := m.deps.ServingRetention.DiscoverServingSnapshotRetention(g.context(ctx), request)
	if err != nil {
		g.complete()
		return err
	}
	g.retainPlan = plan
	err = g.transaction(ctx, "generation-retain", false, []mc.ServingSnapshotRetentionPlan{plan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		var version int64
		if err := x.QueryRow(ctx, `SELECT version FROM agenteam_memory.namespaces WHERE project_id=$1 AND agent_id=$2`, g.entry.namespace.ProjectID.String(), g.entry.namespace.AgentID.String()).Scan(&version); err != nil {
			return portError(err)
		}
		if f.Version(version) != g.source.Version {
			return fault(f.VersionConflict)
		}
		_, err := x.Exec(ctx, `INSERT INTO agenteam_memory.generations(generation_id,project_id,agent_id,snapshot_id,dimension,schema_version,namespace_version,status,created_at,owner_version,retention_state,source_call_id,process_id,profile_data,profile_digest) VALUES($1,$2,$3,$4,$5,2,$6,'building',$7,1,'retained',$8,$9,$10::jsonb,$11)`, g.target.serving.GenerationID, g.entry.namespace.ProjectID.String(), g.entry.namespace.AgentID.String(), g.target.serving.SnapshotID.String(), g.target.serving.Dimension, int64(g.source.Version), now().Time(), g.target.call.String(), g.target.process.String(), string(profileData), string(profileDigest))
		if err != nil {
			return portError(err)
		}
		return m.deps.ServingRetention.ApplyServingSnapshotRetentionInTx(ctx, tx, request, plan)
	})
	if isUnknown(err) {
		g.retainUnknown = true
		return err
	}
	if err != nil {
		g.complete()
		return err
	}
	g.retained = true
	return nil
}

// cleanup never resumes embedding or publication. Unknown is observed with the
// original plan first; a confirmed unpublished build is retired and released.
func (g *retainedGeneration) finish(ctx context.Context) error {
	g.mu.Lock()
	g.building = false
	g.mu.Unlock()
	return g.cleanup(ctx)
}
func (g *retainedGeneration) cleanup(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done {
		return nil
	}
	if g.building {
		return fault(f.ResourceBusy)
	}
	m := g.models
	if g.retainUnknown {
		applied := false
		err := g.transaction(ctx, "generation-retain-observe", true, []mc.ServingSnapshotRetentionPlan{g.retainPlan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			var err error
			applied, err = m.deps.ServingRetention.ServingSnapshotRetentionAppliedInTx(ctx, tx, g.request(g.target, mc.RetainServingSnapshot), g.retainPlan)
			return err
		})
		if err != nil {
			return err
		}
		g.retainUnknown = false
		g.retained = applied
	}
	if !g.retained {
		g.complete()
		return nil
	}
	if g.publishUnknown {
		published := false
		err := g.transaction(ctx, "generation-publish-observe", true, []mc.ServingSnapshotRetentionPlan{g.retainPlan, g.oldReleasePlan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			var status string
			var current *string
			err := x.QueryRow(ctx, `SELECT g.status,n.serving_generation_id::text FROM agenteam_memory.generations g JOIN agenteam_memory.namespaces n USING(project_id,agent_id) WHERE g.generation_id=$1`, g.target.serving.GenerationID).Scan(&status, &current)
			if err != nil {
				return portError(err)
			}
			if status == "ready" && current != nil && *current == g.target.serving.GenerationID {
				retained, err := m.deps.ServingRetention.ServingSnapshotRetentionAppliedInTx(ctx, tx, g.request(g.target, mc.RetainServingSnapshot), g.retainPlan)
				if err != nil {
					return err
				}
				if !retained {
					return fault(f.InvalidState)
				}
				if g.old != nil {
					released, err := m.deps.ServingRetention.ServingSnapshotRetentionAppliedInTx(ctx, tx, g.request(*g.old, mc.ReleaseServingSnapshot), g.oldReleasePlan)
					if err != nil {
						return err
					}
					if !released {
						return fault(f.InvalidState)
					}
				}
				published = true
				return nil
			}
			if status != "building" {
				return fault(f.InvalidState)
			}
			return nil
		})
		if err != nil {
			return err
		}
		g.publishUnknown = false
		if published {
			g.complete()
			return nil
		}
	}
	request := g.request(g.target, mc.ReleaseServingSnapshot)
	if g.releasePlan == nil {
		var err error
		g.releasePlan, err = m.deps.ServingRetention.DiscoverServingSnapshotRetention(context.WithValue(g.context(ctx), generationCleanupKey{}, g), request)
		if err != nil {
			return err
		}
	}
	confirmed := false
	err := g.transaction(ctx, "generation-release", true, []mc.ServingSnapshotRetentionPlan{g.releasePlan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if g.releaseUnknown {
			applied, err := m.deps.ServingRetention.ServingSnapshotRetentionAppliedInTx(ctx, tx, request, g.releasePlan)
			if err != nil {
				return err
			}
			if applied {
				confirmed = true
				return nil
			}
		}
		count, err := x.Exec(ctx, `UPDATE agenteam_memory.generations SET status='retired',owner_version=2,retention_state='released' WHERE generation_id=$1 AND status IN ('building','retired') AND retention_state='retained' AND NOT EXISTS(SELECT 1 FROM agenteam_memory.namespaces WHERE serving_generation_id=$1)`, g.target.serving.GenerationID)
		if err != nil {
			return portError(err)
		}
		if count.RowsAffected() != 1 {
			return fault(f.InvalidState)
		}
		if err := m.deps.ServingRetention.ApplyServingSnapshotRetentionInTx(ctx, tx, request, g.releasePlan); err != nil {
			return err
		}
		confirmed = true
		return nil
	})
	if isUnknown(err) {
		g.releaseUnknown = true
	}
	if err != nil {
		return err
	}
	if !confirmed {
		return fault(f.ResourceBusy)
	}
	g.complete()
	return nil
}
func (g *retainedGeneration) complete() {
	g.done = true
	m := g.models
	m.mu.Lock()
	delete(m.builds, g)
	end := m.releaseFinishedLocked(g.entry)
	m.mu.Unlock()
	if end {
		m.domain.end(g.entry)
	}
}
