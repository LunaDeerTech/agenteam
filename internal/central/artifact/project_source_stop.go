package artifact

import (
	"context"
	"errors"
	"sort"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type artifactSourceAcquisitionKey struct{}

type artifactProjectSource struct {
	command          f.CommandIdentity
	source           oc.ResolvedSource
	lease            oc.SourceLease
	target           i.ProjectID
	call             f.ID[artifactCallID]
	active, retained bool
	original         f.CommitResult
}

func NewProjectCallOwnershipWithSourceStop(store Store, owners *OwnerProvider, objects oc.Objects, uploads oc.Uploads, cleaner oc.Cleaner, reads oc.SourceReads, resolver oc.SourceResolver, auditing au.Appender, cursors cursor.Keyring, guard *object.ProcessGuard, expected oc.ProcessID, authority oc.ProjectStopAuthority, sources oc.ProjectSourceStopAuthority) (*ProjectCallOwnership, error) {
	concrete, ok := objects.(*object.Service)
	reader, rok := reads.(*object.SourceReads)
	if !ok || !rok || reader.RequireProjectStopBinding(concrete, sources) != nil {
		return nil, failure(f.DependencyUnbound, nil)
	}
	pair, err := NewProjectCallOwnership(store, owners, objects, uploads, cleaner, reads, resolver, auditing, cursors, guard, expected, authority)
	if err != nil {
		return nil, err
	}
	pair.state.sourceAuthority = sources
	return pair, nil
}

// Only the original owned creation's acquisition Tx attaches the two Project
// relationships. Neither discovery nor a later stop can invent this binding.
func (s *Service) bindProjectSource(ctx context.Context, tx f.Tx, req createRequest, source oc.ResolvedSource, lease oc.SourceLease) error {
	st := s.state().ownership
	if st == nil || nilPort(st.sourceAuthority) {
		return nil
	}
	reads, ok := s.state().reads.(*object.SourceReads)
	w, wok := ctx.Value(artifactCallContextKey{}).(artifactCallWitness)
	if !ok || !wok || w.state != st || w.call == nil || w.call.record.Project != req.invocation.Details().ProjectID {
		return failure(f.Forbidden, nil)
	}
	if err := reads.BindProjectSourceInTx(ctx, tx, lease, req.invocation.Details().ProjectID, req.identity); err != nil {
		return err
	}
	sourceP, err := f.ParseID[i.Project](source.Details().Owner.Details().ProjectID)
	if err != nil {
		return failure(f.Forbidden, nil)
	}
	key := digestBytes([]byte(req.identity.Canonical()))
	item := &artifactProjectSource{command: req.identity, source: source, lease: lease, target: req.invocation.Details().ProjectID, call: w.call.record.ID}
	r := s.state()
	r.mu.Lock()
	if r.projectSources == nil {
		r.projectSources = map[f.Digest]*artifactProjectSource{}
	}
	prior := r.projectSources[key]
	if prior != nil && (prior.lease.ID() != lease.ID() || prior.call != item.call) {
		r.mu.Unlock()
		return failure(f.ResourceBusy, nil)
	}
	if prior == nil {
		r.projectSources[key] = item
	}
	r.mu.Unlock()
	x, err := r.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_artifact.project_work SET command_hash=$2,source_project_id=$3,source_lease_id=$4 WHERE id=$1 AND project_id=$5 AND process_id=$6 AND instance_id=$7 AND joined_at IS NULL AND (source_lease_id IS NULL OR source_lease_id=$4)`, item.call.String(), digestRaw(key), sourceP.String(), lease.ID().String(), item.target.String(), st.process.String(), st.instance.String())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return failure(f.InvalidState, nil)
	}
	return ctx.Err()
}

func (s *Service) sourceRequest(actor i.Actor, cause oc.ProjectStopCause, item *artifactProjectSource) (oc.ProjectSourceStopRequest, error) {
	p, err := f.ParseID[i.Project](item.source.Details().Owner.Details().ProjectID)
	if err != nil {
		return oc.ProjectSourceStopRequest{}, unavailable(err)
	}
	return oc.NewProjectSourceStopRequest(oc.ProjectSourceStopDetails{Actor: actor, Cause: cause, Command: item.command, Lease: item.lease, SourceProjectID: p, TargetProjectID: item.target, ObjectID: item.source.Details().Meta.ID})
}

// This follows the original committed Stop gate. With supplies one synchronous
// borrowed scope; neither its callback nor its request is a reusable permission.
func (s *Service) convergeProjectSources(ctx context.Context, actor i.Actor, cause oc.ProjectStopCause) (bool, error) {
	st, r := s.state().ownership, s.state()
	if st == nil || nilPort(st.sourceAuthority) {
		return false, nil
	}
	r.mu.Lock()
	var items []*artifactProjectSource
	for _, item := range r.projectSources {
		if item.target == cause.Details().ProjectID || item.source.Details().Owner.Details().ProjectID == cause.Details().ProjectID.String() {
			items = append(items, item)
		}
	}
	r.mu.Unlock()
	sort.Slice(items, func(a, b int) bool { return items[a].command.Canonical() < items[b].command.Canonical() })
	pending := false
	for _, item := range items {
		if cause.Details().Action == oc.ProjectStopArchive && item.target != cause.Details().ProjectID {
			pending = true
			continue
		}
		r.mu.Lock()
		if item.active {
			r.mu.Unlock()
			pending = true
			continue
		}
		item.active = true
		r.mu.Unlock()
		request, err := s.sourceRequest(actor, cause, item)
		done := false
		if err == nil {
			err = st.sourceAuthority.WithProjectSourceStop(ctx, request, func(run context.Context) error {
				ready, err := s.checkProjectSource(run, request, item, false)
				if err != nil || !ready {
					return err
				}
				reads, ok := r.reads.(*object.SourceReads)
				if !ok {
					return failure(f.DependencyUnbound, nil)
				}
				closed, err := reads.StopProjectSource(run, request)
				if err != nil || !closed {
					return err
				}
				done, err = s.checkProjectSource(run, request, item, true)
				return err
			})
		}
		r.mu.Lock()
		item.active = false
		r.mu.Unlock()
		if err != nil {
			return true, err
		}
		if done {
			s.forgetProjectSource(item)
		} else {
			pending = true
		}
	}
	return pending, nil
}

// Check and source-return checkpoint touch only Artifact's own command/work.
// Object independently checks its exact issuer/lease/process in every Tx.
func (s *Service) checkProjectSource(ctx context.Context, request oc.ProjectSourceStopRequest, item *artifactProjectSource, mark bool) (bool, error) {
	st, r := s.state().ownership, s.state()
	r.mu.Lock()
	retained, original := item.retained, item.original
	r.mu.Unlock()
	locks, err := request.RequiredLocks()
	if err != nil {
		return false, err
	}
	locks = append(locks, createLocks(item.command, "")...)
	cause, _ := f.NewRecoveryCause("artifact-source-owner", item.lease.ID().String(), request.Details().Cause.Details().OperationID.String())
	valid := false
	result := r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := st.requireProcess(ctx, tx); err != nil {
			return portError(err)
		}
		if err := st.sourceAuthority.RequireProjectSourceStopInTx(ctx, tx, request); err != nil {
			return portError(err)
		}
		x, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		command, found, err := loadCommand(ctx, x, item.command)
		if err != nil {
			return err
		}
		if !found || command.ref.ProjectID != item.target || command.sourceLease != item.lease.ID() || !sameSource(command.source, item.source) {
			return failure(f.InvalidState, nil)
		}
		var matches, joined bool
		err = x.QueryRow(ctx, `SELECT command_hash=$2 AND project_id=$3 AND source_project_id=$4 AND source_lease_id=$5 AND process_id=$6 AND instance_id=$7,source_joined_at IS NOT NULL FROM agenteam_artifact.project_work WHERE id=$1`, item.call.String(), digestRaw(digestBytes([]byte(item.command.Canonical()))), item.target.String(), request.Details().SourceProjectID.String(), item.lease.ID().String(), st.process.String(), st.instance.String()).Scan(&matches, &joined)
		if err != nil {
			return unavailable(err)
		}
		if !matches {
			return failure(f.InvalidState, nil)
		}
		if mark && !retained && !joined {
			tag, err := x.Exec(ctx, `UPDATE agenteam_artifact.project_work SET source_joined_at=GREATEST(clock_timestamp(),created_at) WHERE id=$1 AND source_lease_id=$2 AND source_joined_at IS NULL`, item.call.String(), item.lease.ID().String())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return failure(f.InvalidState, nil)
			}
		}
		valid = ctx.Err() == nil
		return ctx.Err()
	})
	if retained {
		if err = artifactJoinedError(ctx, result, valid); err != nil {
			return false, errors.Join(projectCleanupUnknown(original), err)
		}
		r.mu.Lock()
		item.retained = false
		r.mu.Unlock()
		// Even if the original checkpoint committed, this invocation only confirms.
		return false, nil
	}
	if result.State() == f.Unknown {
		r.mu.Lock()
		item.retained = true
		item.original = result
		r.mu.Unlock()
	}
	if err = artifactJoinedError(ctx, result, valid); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) forgetProjectSource(item *artifactProjectSource) {
	reads, ok := s.state().reads.(*object.SourceReads)
	if !ok || reads.ForgetProjectSource(item.lease) != nil {
		return
	}
	r := s.state()
	key := digestBytes([]byte(item.command.Canonical()))
	r.mu.Lock()
	if r.projectSources[key] == item {
		delete(r.projectSources, key)
		delete(r.leases, key)
		delete(r.leaseProjects, key)
		delete(r.leaseSources, key)
	}
	r.mu.Unlock()
	r.ownership.mu.Lock()
	r.ownership.signalLocked()
	r.ownership.mu.Unlock()
}

// Normal public return uses its original admitted call, not a lifecycle grant.
// The Object handle must already have returned its Close/release/join tail.
func (s *Service) returnedProjectSource(ctx context.Context, lease oc.SourceLease) error {
	r, st := s.state(), s.state().ownership
	if st == nil || nilPort(st.sourceAuthority) {
		return nil
	}
	r.mu.Lock()
	var item *artifactProjectSource
	for _, candidate := range r.projectSources {
		if candidate.lease.ID() == lease.ID() {
			item = candidate
			break
		}
	}
	if item == nil || item.active || item.retained {
		r.mu.Unlock()
		return failure(f.ResourceBusy, nil)
	}
	item.active = true
	r.mu.Unlock()
	defer func() { r.mu.Lock(); item.active = false; r.mu.Unlock() }()
	p, err := f.ParseID[i.Project](item.source.Details().Owner.Details().ProjectID)
	if err != nil {
		return unavailable(err)
	}
	locks := []f.LockRequest{artifactProjectLock(item.target, f.Exclusive), artifactProjectLock(p, f.Exclusive)}
	cause, _ := f.NewRecoveryCause("artifact-source-return", lease.ID().String(), item.call.String())
	valid := false
	result := r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := st.requireProcess(ctx, tx); err != nil {
			return portError(err)
		}
		st.mu.Lock()
		local := st.owners[item.call]
		owned := local != nil && !local.returned
		st.mu.Unlock()
		if !owned {
			return failure(f.InvalidState, nil)
		}
		x, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_artifact.project_work SET source_joined_at=GREATEST(clock_timestamp(),created_at) WHERE id=$1 AND process_id=$2 AND instance_id=$3 AND project_id=$4 AND source_project_id=$5 AND source_lease_id=$6 AND command_hash=$7`, item.call.String(), st.process.String(), st.instance.String(), item.target.String(), p.String(), lease.ID().String(), digestRaw(digestBytes([]byte(item.command.Canonical()))))
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return failure(f.InvalidState, nil)
		}
		valid = ctx.Err() == nil
		return ctx.Err()
	})
	if result.State() == f.Unknown {
		r.mu.Lock()
		item.retained = true
		item.original = result
		r.mu.Unlock()
	}
	if err = artifactJoinedError(ctx, result, valid); err != nil {
		return err
	}
	s.forgetProjectSource(item)
	return nil
}

// Called only after the actual acquisition returned NotCommitted. The original
// Object issuer independently checks that same outcome before local retirement.
func (s *Service) discardProjectSourceAcquisition(lease oc.SourceLease) {
	r := s.state()
	if r.ownership == nil || nilPort(r.ownership.sourceAuthority) {
		return
	}
	reads, ok := r.reads.(*object.SourceReads)
	if !ok || reads.ForgetProjectSource(lease) != nil {
		return
	}
	r.mu.Lock()
	for key, current := range r.leases {
		if current.ID() == lease.ID() {
			delete(r.projectSources, key)
			delete(r.leases, key)
			delete(r.leaseProjects, key)
			delete(r.leaseSources, key)
		}
	}
	r.mu.Unlock()
	r.ownership.mu.Lock()
	r.ownership.signalLocked()
	r.ownership.mu.Unlock()
}
