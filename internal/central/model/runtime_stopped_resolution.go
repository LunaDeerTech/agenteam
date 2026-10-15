package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// LookupStoppedResolution settles only the original resolution. Both reads
// use the original resolution/call gates; the actual old flock is checked
// between them. It never prepares a resolution, acquires/releases a lease or
// resumes a call. The domain must separately retire any returned original lease.
func (r *Runtime) LookupStoppedResolution(ctx context.Context, request mc.StoppedResolutionRequest) (mc.StoppedResolutionObservation, error) {
	var empty mc.StoppedResolutionObservation
	if err := runtimeContextError(ctx); err != nil {
		return empty, err
	}
	s := r.state()
	if s == nil || s.authority.state() == nil {
		return empty, fault(f.DependencyUnbound)
	}
	consumer, ok := s.authority.state().auth.StoppedConsumers.(mc.StoppedResolutionAuthority)
	if !ok || nilPort(consumer) {
		return empty, fault(f.DependencyUnbound)
	}
	if request.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	request = request.Clone()
	ctx, cancel := context.WithCancel(ctx)
	admission := &runtimeAdmission{cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	if s.stopped || !s.ready {
		s.mu.Unlock()
		cancel()
		return empty, fault(f.ShuttingDown)
	}
	process := s.process
	s.admissions[admission] = struct{}{}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.admissions, admission)
		close(admission.done)
		s.mu.Unlock()
	}()
	plan, err := consumer.DiscoverStoppedResolution(ctx, request)
	if err != nil {
		return empty, runtimePortError(err)
	}
	binding, err := mc.StoppedResolutionBinding(request)
	if err != nil || plan.Validate() != nil || plan.Details().Binding != binding || plan.Details().RetryPolicy != nil {
		return empty, fault(f.Forbidden)
	}
	identity, err := resolutionIdentity(request.Resolve)
	if err != nil {
		return empty, err
	}
	cause, err := runtimeCallCause(request.Resolve.Consumer.ProjectID, request.CallID)
	if err != nil {
		return empty, err
	}
	// The unlocked read selects locks only; it conveys no settled/absence fact.
	before, err := loadResolutionPreparation(ctx, s.store, identity.Canonical())
	if err != nil {
		return empty, err
	}
	base := []f.LockRequest{commandLock(identity), commandLock(cause.Details().Primary), projectLock(request.Resolve.Consumer.ProjectID.String()), systemLock("model-references", f.Shared), recordLock(f.ReferenceRecordLock, "model-resolution:"+hash([]byte(identity.Canonical())).String())}
	if before != nil {
		base = append(base, resolutionBaseLocks(request.Resolve, identity, before.SnapshotID, before.Draft)...)
	}
	locks, err := resolutionUnion(base, plan.RequiredLocks())
	if err != nil {
		return empty, err
	}
	var originalCall *runtimeRecord
	var out mc.StoppedResolutionObservation
	read := func(confirm bool) error {
		result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
				return runtimePortError(err)
			}
			if err := s.authority.state().auth.Process.RequireCurrentProcessInTx(ctx, tx, process); err != nil {
				return runtimePortError(err)
			}
			if err := consumer.ValidateStoppedResolutionInTx(ctx, tx, request, plan); err != nil {
				return runtimePortError(err)
			}
			x, err := s.store.InTx(tx)
			if err != nil {
				return runtimePortError(err)
			}
			actual, err := loadResolutionPreparation(ctx, x, identity.Canonical())
			if err != nil {
				return err
			}
			if !resolutionEqual(actual, before) {
				return fault(f.ResourceBusy)
			}
			call, err := loadRuntimeRecord(ctx, x, request.CallID)
			if err != nil {
				return err
			}
			if confirm && (call == nil) != (originalCall == nil) || confirm && call != nil && !runtimeSameRecord(call, originalCall) {
				return fault(f.ResourceBusy)
			}
			if err := stoppedResolutionMatches(request, actual, call); err != nil {
				return err
			}
			if actual != nil && actual.Phase == "committed" {
				if err := loadCommittedResolution(ctx, x, actual); err != nil {
					return err
				}
				resolved, err := stoppedResolvedModel(request.Resolve, actual)
				if err != nil {
					return err
				}
				out.Model = &resolved
			}
			originalCall = call
			out.CallFound = call != nil
			return runtimeContextError(ctx)
		})
		return commitError(result)
	}
	if err = read(false); err != nil {
		return empty, err
	}
	// Row absence, a retired call, and a domain DTO are not death evidence.
	if err = s.authority.state().auth.Process.ConfirmStopped(ctx, request.PreparedProcessID); err != nil {
		return empty, runtimePortError(err)
	}
	if err = read(true); err != nil {
		return empty, err
	}
	if err = runtimeContextError(ctx); err != nil {
		return empty, err
	}
	out.Settled = true
	if out.Validate() != nil {
		return empty, unavailable(nil)
	}
	return out.Clone(), nil
}

func stoppedResolutionMatches(r mc.StoppedResolutionRequest, p *resolutionPreparation, call *runtimeRecord) error {
	if p != nil && (p.Semantic != resolutionSemantic(r.Resolve) || !resolutionEqual(p.Request, resolutionRequest(r.Resolve))) {
		return fault(f.IdempotencyKeyReused)
	}
	if call == nil {
		return nil
	}
	if p == nil || p.Phase != "committed" {
		return fault(f.InvalidState)
	}
	b := call.binding
	raw, err := encoded(p.Draft.Snapshot)
	if err != nil || call.validate() != nil || b.CallID != r.CallID || !b.Consumer.Equal(r.Resolve.Consumer) || !resolutionEqual(b.Input, r.Input) || call.value.ProcessID != r.PreparedProcessID || b.Owner != r.Resolve.LeaseOwner.Details() || b.PreparationID != p.ID || b.SnapshotID != p.SnapshotID || b.SnapshotDigest != hash(raw) || b.LeaseID.String() != p.Draft.LeaseID || !resolutionEqual(b.Initiator, p.Request.Actor) {
		return fault(f.Forbidden)
	}
	return nil
}

func stoppedResolvedModel(request mc.ResolveRequest, p *resolutionPreparation) (mc.ResolvedModel, error) {
	snapshot, err := p.Draft.Snapshot.snapshot()
	if err != nil {
		return mc.ResolvedModel{}, err
	}
	out := mc.ResolvedModel{Snapshot: snapshot, Consumer: request.Consumer.Clone(), LeaseOwner: request.LeaseOwner}
	if p.Draft.LeaseID != "" {
		lease, err := f.ParseID[sc.Lease](p.Draft.LeaseID)
		if err != nil || snapshot.CredentialRef == nil {
			return mc.ResolvedModel{}, unavailable(err)
		}
		out.CredentialLease = &sc.CredentialLease{LeaseID: lease, CredentialRef: *snapshot.CredentialRef}
	}
	if out.Validate() != nil {
		return mc.ResolvedModel{}, unavailable(nil)
	}
	return out, nil
}

var _ mc.StoppedResolutionObserver = (*Runtime)(nil)
