package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type indexRetentionKey struct{}
type indexRetentionCleanupKey struct{}
type indexRetentionTarget struct {
	Profile   indexProfile
	Operation string
	Process   oc.ProcessID
}

// An unpublished profile owns its reference cleanup independently of the
// shorter Model calls. An Unknown never grants another publish or Model call.
type indexRetention struct {
	mu                                         sync.Mutex
	service                                    *IndexService
	owner                                      id.Actor
	build                                      *indexBuild
	actor                                      id.Actor
	target                                     indexRetentionTarget
	old                                        *indexRetentionTarget
	projectCall                                *projectCallEntry
	running, done, pinned, published           bool
	pinUnknown, releaseUnknown, publishUnknown bool
	pinPlan, releasePlan, oldReleasePlan       mc.ServingSnapshotRetentionPlan
}

func (g *indexRetention) context(ctx context.Context) context.Context {
	if g.build != nil {
		ctx = context.WithValue(ctx, indexBuildKey{}, g.build)
	}
	return context.WithValue(ctx, indexRetentionKey{}, g)
}
func (g *indexRetention) request(target indexRetentionTarget, action mc.ServingSnapshotRetentionAction) mc.ServingSnapshotRetentionRequest {
	return mc.ServingSnapshotRetentionRequest{Actor: g.actor, Owner: mc.ServingSnapshotOwner{Kind: mc.KnowledgeConsumer, ProjectID: target.Profile.Project, GenerationID: target.Profile.ID}, SnapshotID: target.Profile.Snapshot, OwnerVersion: target.Profile.Version, Action: action}
}
func (g *indexRetention) locks(plans ...mc.ServingSnapshotRetentionPlan) ([]f.LockRequest, error) {
	var locks []f.LockRequest
	var err error
	if g.build != nil {
		locks, err = g.build.locks(true)
	} else {
		locks, err = scopeLocks(g.owner, g.target.Profile.Project, true)
	}
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if !nilPort(plan) {
			locks = append(locks, plan.RequiredLocks()...)
		}
	}
	return ob.NormalizeLocks(locks)
}
func (g *indexRetention) transaction(ctx context.Context, cleanup bool, plans []mc.ServingSnapshotRetentionPlan, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	run := g.context(ctx)
	if cleanup {
		run = context.WithValue(run, indexRetentionCleanupKey{}, g)
	}
	locks, err := g.locks(plans...)
	if err != nil {
		return err
	}
	cause, err := readCause()
	if err != nil {
		return err
	}
	s := g.service
	result := s.store.WithinTx(run, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.authority.guard.RequireCurrentProcessInTx(ctx, tx, s.authority.process); err != nil {
			return portError(err)
		}
		if !cleanup {
			if err := s.documents.projectCallTx(ctx, tx, g.target.Profile.Project); err != nil {
				return err
			}
			if err := g.requireOwnerInTx(ctx, tx); err != nil {
				return err
			}
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	})
	return txError(result)
}
func indexRetentionUnknown(err error) bool {
	var known *f.Fault
	return errors.As(err, &known) && known.CommitState == f.Unknown
}
func (g *indexRetention) complete() {
	if g.done {
		return
	}
	g.done = true
	s := g.service
	if g.projectCall != nil {
		// The parent Rebuild may already have returned an Unknown. Its original
		// Project stop barrier remains live until this exact child is returned.
		s.documents.projectCallDone(g.projectCall, nil)
	}
	s.mu.Lock()
	delete(s.retentions, g)
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}
func (g *indexRetention) signal() {
	s := g.service
	s.mu.Lock()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}
func (g *indexRetention) mapping(target indexRetentionTarget) (f.Digest, error) {
	raw, err := json.Marshal(struct {
		SourceOperation, SourceProcess, Transition string
		Version                                    f.Version
	}{target.Operation, target.Process.String(), g.target.Profile.ID, target.Profile.Version})
	if err != nil {
		return "", internal(err)
	}
	return ob.DigestBytes(raw), nil
}

func (s *IndexService) newRetention(owner id.Actor, profile indexProfile) (*indexRetention, error) {
	scope, _ := id.InProject(profile.Project)
	actor, err := s.authority.registration.Actor(profile.ID, scope)
	if err != nil {
		return nil, err
	}
	g := &indexRetention{service: s, owner: owner, actor: actor, running: true,
		target: indexRetentionTarget{Profile: profile, Process: s.authority.process}}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, fault(f.ShuttingDown)
	}
	// A publication whose COMMIT result is unknown still owns the project.
	// No later build may obscure its original before/after observation.
	for existing := range s.retentions {
		if existing.target.Profile.Project == profile.Project {
			return nil, fault(f.ResourceBusy)
		}
	}
	s.retentions[g] = struct{}{}
	return g, nil
}

func (g *indexRetention) captureOld(ctx context.Context, x postgres.SQLExecutor) error {
	var old indexRetentionTarget
	var snapshot, process string
	var version int64
	old.Profile.Project = g.target.Profile.Project
	err := x.QueryRow(ctx, `SELECT p.id::text,p.version,p.snapshot_id::text,p.dimension,p.source_operation_id::text,o.process_id::text
 FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p ON p.project_id=st.project_id AND p.id=st.serving_profile_id
 JOIN agenteam_knowledge.model_operations o ON o.id=p.source_operation_id AND o.project_id=p.project_id
 WHERE st.project_id=$1 AND p.state='active' AND p.retention_state='retained'`, old.Profile.Project.String()).Scan(&old.Profile.ID, &version, &snapshot, &old.Profile.Dimension, &old.Operation, &process)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	old.Profile.Version = f.Version(version)
	old.Profile.Snapshot, err = f.ParseID[mc.Snapshot](snapshot)
	if err != nil {
		return internal(err)
	}
	old.Process, err = f.ParseID[oc.Process](process)
	if err != nil {
		return internal(err)
	}
	g.old = &old
	return nil
}

// pin runs while the original Resolve lease still exists. Profile publication
// and every later serving call use the retained immutable snapshot.
func (g *indexRetention) pin(ctx context.Context, op *indexOperation, resolved mc.ResolvedModel) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done || !g.running || op.kind != "profile" || op.profile.ID != g.target.Profile.ID || resolved.Validate() != nil {
		return fault(f.InvalidState)
	}
	g.target.Operation = op.id
	g.target.Profile.Snapshot = resolved.Snapshot.ID
	r := g.request(g.target, mc.RetainServingSnapshot)
	plan, err := g.service.deps.ServingRetention.DiscoverServingSnapshotRetention(g.context(ctx), r)
	if err != nil {
		return err
	}
	g.pinPlan = plan
	err = g.transaction(ctx, false, []mc.ServingSnapshotRetentionPlan{plan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		p := g.target.Profile
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_profiles SET snapshot_id=$3,source_operation_id=$4,retention_state='retained'
 WHERE project_id=$1 AND id=$2 AND version=$5 AND state='building' AND snapshot_id IS NULL AND retention_state='unretained'`, p.Project.String(), p.ID, p.Snapshot.String(), op.id, int64(p.Version))
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		return g.service.deps.ServingRetention.ApplyServingSnapshotRetentionInTx(ctx, tx, r, plan)
	})
	if indexRetentionUnknown(err) {
		g.pinUnknown = true
	}
	if err == nil {
		g.pinned = true
	}
	return err
}

func (g *indexRetention) finish(ctx context.Context) error {
	g.mu.Lock()
	g.running = false
	g.mu.Unlock()
	g.signal()
	return g.cleanup(ctx)
}

// cleanup only observes original unknown outcomes and releases unpublished
// work. It never resumes a Model request or publishes a profile.
func (g *indexRetention) cleanup(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.done {
		return nil
	}
	if g.running {
		return fault(f.ResourceBusy)
	}
	port := g.service.deps.ServingRetention
	if g.pinUnknown {
		var applied bool
		err := g.transaction(ctx, true, []mc.ServingSnapshotRetentionPlan{g.pinPlan}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			var err error
			applied, err = port.ServingSnapshotRetentionAppliedInTx(ctx, tx, g.request(g.target, mc.RetainServingSnapshot), g.pinPlan)
			return err
		})
		if err != nil {
			return err
		}
		g.pinUnknown, g.pinned = false, applied
	}
	if g.publishUnknown {
		if err := g.observePublication(ctx); err != nil {
			return err
		}
	}
	if g.published {
		g.complete()
		return nil
	}
	if !g.pinned {
		// Also confirms initial profile-admission Unknown: absent means no
		// derived state committed, while this exact private profile may fail.
		err := g.transaction(ctx, true, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			p := g.target.Profile
			var state, retention string
			err := x.QueryRow(ctx, `SELECT state,retention_state FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2`, p.Project.String(), p.ID).Scan(&state, &retention)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return unavailable(err)
			}
			if retention != "unretained" || state != "building" && state != "failed" {
				return fault(f.InvalidState)
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_knowledge.index_profiles SET state='failed' WHERE project_id=$1 AND id=$2 AND state='building' AND retention_state='unretained'`, p.Project.String(), p.ID)
			return portError(err)
		})
		if err != nil {
			return err
		}
		g.complete()
		return nil
	}
	r := g.request(g.target, mc.ReleaseServingSnapshot)
	if nilPort(g.releasePlan) {
		var err error
		g.releasePlan, err = port.DiscoverServingSnapshotRetention(context.WithValue(g.context(ctx), indexRetentionCleanupKey{}, g), r)
		if err != nil {
			return err
		}
	}
	if g.releaseUnknown {
		var applied bool
		err := g.transaction(ctx, true, []mc.ServingSnapshotRetentionPlan{g.releasePlan}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			var err error
			applied, err = port.ServingSnapshotRetentionAppliedInTx(ctx, tx, r, g.releasePlan)
			return err
		})
		if err != nil {
			return err
		}
		g.releaseUnknown = false
		if applied {
			g.complete()
			return nil
		}
	}
	err := g.transaction(ctx, true, []mc.ServingSnapshotRetentionPlan{g.releasePlan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		p := g.target.Profile
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.index_profiles SET state='failed',retention_state='released' WHERE project_id=$1 AND id=$2 AND version=$3 AND state IN ('building','failed') AND retention_state='retained' AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.index_state WHERE project_id=$1 AND serving_profile_id=$2)`, p.Project.String(), p.ID, int64(p.Version))
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.InvalidState)
		}
		return port.ApplyServingSnapshotRetentionInTx(ctx, tx, r, g.releasePlan)
	})
	if indexRetentionUnknown(err) {
		g.releaseUnknown = true
	}
	if err != nil {
		return err
	}
	g.complete()
	return nil
}

func (g *indexRetention) requireOwnerInTx(ctx context.Context, tx f.Tx) error {
	if g.build != nil {
		return g.build.authorizeInTx(ctx, tx)
	}
	grant, err := g.service.documents.state().deps.Projects.RequireOwnerInTx(ctx, tx, g.owner, g.target.Profile.Project, id.Mutate)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(g.owner, g.target.Profile.Project) {
		return fault(f.Forbidden)
	}
	return nil
}
