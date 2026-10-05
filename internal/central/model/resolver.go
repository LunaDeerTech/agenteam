package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

var _ mc.Resolver = (*Service)(nil)

func (s *Service) resolutionState(ctx context.Context, r mc.ResolveRequest) error {
	if e := resolutionContextError(ctx); e != nil {
		return e
	}
	if r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if s.state() == nil || s.state().authority.state() == nil || s.state().authority.state().auth.Resolution == nil {
		return fault(f.DependencyUnbound)
	}
	return resolutionVariant(r)
}
func (s *Service) SelectModel(ctx context.Context, actor id.Actor, r mc.SelectionRequest) (mc.SelectionResult, error) {
	empty := mc.SelectionResult{}
	if e := resolutionContextError(ctx); e != nil {
		return empty, e
	}
	r = r.Clone()
	if r.Validate() != nil || actor.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	if actor.Details().Kind != id.Human {
		return empty, fault(f.DependencyUnbound)
	}
	if s.state() == nil {
		return empty, fault(f.DependencyUnbound)
	}
	request := mc.ResolveRequest{Actor: actor, Consumer: r.Consumer, Purpose: r.Consumer.Purpose, Source: mc.CurrentSelectionSource, ModelRef: r.ModelRef, Selection: &r.Selection}
	if e := resolutionVariant(request); e != nil {
		return empty, e
	}
	snapshot, e := f.NewID[mc.Snapshot]()
	if e != nil {
		return empty, unavailable(e)
	}
	candidate, candidateErr := currentResolutionDraft(ctx, s.state().store, request, snapshot)
	locks := []f.LockRequest{}
	if r.ModelRef != nil {
		locks = append(locks, aggregateLock(f.ModelConfigAggregate, r.ModelRef.String(), f.Shared))
	}
	if r.Selection.Kind == "platform" {
		locks = append(locks, systemLock("model-platform-selection", f.Shared))
	}
	if candidate.Snapshot.Identity.ProviderID.Validate() == nil {
		locks = append(locks, aggregateLock(f.ProviderAggregate, candidate.Snapshot.Identity.ProviderID.String(), f.Shared))
	}
	if candidate.Snapshot.Identity.ModelID.Validate() == nil {
		locks = append(locks, aggregateLock(f.ModelConfigAggregate, candidate.Snapshot.Identity.ModelID.String(), f.Shared))
	}
	scope, _ := id.InProject(r.Consumer.ProjectID)
	var out mc.SelectionResult
	e = s.readScope(ctx, actor, scope, locks, func(ctx context.Context, x postgres.SQLExecutor) error {
		draft, err := currentResolutionDraft(ctx, x, request, snapshot)
		if err != nil {
			return err
		}
		if candidateErr != nil || !resolutionEqual(draft, candidate) {
			return fault(f.ResourceBusy)
		}
		mid, version := draft.Snapshot.Identity.ModelID, draft.ModelVersion
		out = mc.SelectionResult{Selected: &mid, ModelVersion: &version, SelectionVersion: draft.Snapshot.SelectionVersion}
		return out.ValidateFor(r)
	})
	if e != nil {
		return empty, e
	}
	if e = ctx.Err(); e != nil {
		return empty, e
	}
	return out.Clone(), nil
}

func (s *Service) DiscoverResolve(ctx context.Context, request mc.ResolveRequest) (mc.ResolutionPlan, error) {
	empty := mc.ResolutionPlan{}
	r := request.Clone()
	if e := s.resolutionState(ctx, r); e != nil {
		return empty, e
	}
	identity, e := resolutionIdentity(r)
	if e != nil {
		return empty, e
	}
	row, readErr := loadResolutionPreparation(ctx, s.state().store, identity.Canonical())
	snapshot, e := f.NewID[mc.Snapshot]()
	if e != nil {
		return empty, unavailable(e)
	}
	if row != nil {
		snapshot = row.SnapshotID
	}
	c := &resolutionCandidate{authority: s.state().authority, request: r, identity: identity, row: row, version: 1}
	if row != nil && row.Phase == "committed" {
		c.draft = row.Draft
		if e = loadCommittedResolution(ctx, s.state().store, row); e != nil && readErr == nil {
			readErr = e
		}
	} else {
		c.draft, e = currentResolutionDraft(ctx, s.state().store, r, snapshot)
		if e != nil && readErr == nil {
			readErr = e
		}
	}
	if readErr == nil && c.draft.Snapshot.CredentialID != "" {
		canonical, err := canonicalResolutionLease(ctx, s.state().store, r, c.draft)
		if err != nil {
			readErr = err
		} else if canonical != "" {
			c.draft.LeaseID = canonical
		} else if row != nil && row.Draft.Snapshot.CredentialID == c.draft.Snapshot.CredentialID && row.Draft.Snapshot.CredentialProject == c.draft.Snapshot.CredentialProject {
			c.draft.LeaseID = row.Draft.LeaseID
		} else {
			lease, err := f.NewID[sc.Lease]()
			if err != nil {
				return empty, unavailable(err)
			}
			c.draft.LeaseID = lease.String()
		}
	}
	lease := ""
	if readErr == nil {
		lease = c.draft.LeaseID
	}
	c.consumerRequest, e = resolutionConsumerRequest(r, snapshot, lease)
	if e != nil {
		return empty, portError(e)
	}
	c.consumer, e = s.state().authority.state().auth.Resolution.Consumers.Discover(ctx, c.consumerRequest)
	if e != nil {
		return empty, portError(e)
	}
	cb, e := mc.ConsumerBinding(c.consumerRequest)
	if e != nil || c.consumer.Validate() != nil || c.consumer.Details().Binding != cb {
		return empty, fault(f.Forbidden)
	}
	c.locks, e = resolutionUnion(resolutionBaseLocks(r, identity, snapshot, c.draft), c.consumer.RequiredLocks())
	if e != nil {
		return empty, e
	}
	cause, e := f.NewCommandsCause(identity)
	if e != nil {
		return empty, e
	}
	// Candidate reads cannot reveal existence before current business authority.
	preflight := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, c.locks); e != nil {
			return portError(e)
		}
		if e := c.authority.validateResolutionConsumer(ctx, tx, c); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		current, e := loadResolutionPreparation(ctx, x, identity.Canonical())
		if e != nil {
			return e
		}
		// An original writer may have completed while this attempt collected
		// candidates. Never expose an earlier live-config error over its result.
		if !resolutionEqual(current, row) {
			return fault(f.ResourceBusy)
		}
		return nil
	})
	if e = commitError(preflight); e != nil {
		return empty, e
	}
	if e = ctx.Err(); e != nil {
		return empty, e
	}
	if readErr != nil {
		return empty, readErr
	}
	if row != nil && row.Semantic != resolutionSemantic(r) {
		return empty, fault(f.IdempotencyKeyReused)
	}
	if row == nil {
		key, err := newID()
		if err != nil {
			return empty, err
		}
		c.row = &resolutionPreparation{ID: key, SnapshotID: snapshot}
	} else {
		c.version = row.Version
		if row.Phase == "prepared" && !resolutionEqual(row.Draft, c.draft) {
			c.version++
			if c.version.Validate() != nil {
				return empty, fault(f.InvalidState)
			}
		}
	}
	if e = s.prepareResolutionSecret(ctx, c); e != nil {
		return empty, e
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, c.locks); e != nil {
			return portError(e)
		}
		if e := c.authority.validateResolutionConsumer(ctx, tx, c); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		old, e := loadResolutionPreparation(ctx, x, identity.Canonical())
		if e != nil {
			return e
		}
		if old != nil && old.Semantic != resolutionSemantic(r) {
			return fault(f.IdempotencyKeyReused)
		}
		if (old == nil) != (c.row.Phase == "") || old != nil && (old.ID != c.row.ID || old.Version != c.row.Version || !resolutionEqual(old.Draft, c.row.Draft) || old.Phase != c.row.Phase) {
			return fault(f.ResourceBusy)
		}
		if old != nil && old.Phase == "committed" {
			if e = loadCommittedResolution(ctx, x, old); e != nil {
				return e
			}
		} else if e = checkCurrentResolution(ctx, x, c); e != nil {
			return e
		}
		if e = checkCanonicalResolution(ctx, x, c); e != nil {
			return e
		}
		if e = saveResolutionPreparation(ctx, x, c, old); e != nil {
			return e
		}
		return nil
	})
	if e = commitError(result); e != nil {
		return empty, e
	}
	if e = ctx.Err(); e != nil {
		return empty, e
	}
	d, e := resolutionDetails(c)
	if e != nil {
		return empty, e
	}
	return mc.NewResolutionPlan(c.authority.state().resolutionIssuer, d)
}
func resolutionDetails(c *resolutionCandidate) (mc.ResolutionPlanDetails, error) {
	if e := c.draft.validate(); e != nil {
		return mc.ResolutionPlanDetails{}, e
	}
	b, e := mc.ResolveBinding(c.request)
	if e != nil {
		return mc.ResolutionPlanDetails{}, e
	}
	s, e := c.draft.Snapshot.snapshot()
	if e != nil {
		return mc.ResolutionPlanDetails{}, e
	}
	pv, mv := c.draft.ProviderVersion, c.draft.ModelVersion
	d := mc.ResolutionPlanDetails{Binding: b, Mapping: c.mapping(), Locks: c.locks, SnapshotID: s.ID, ProviderID: s.Identity.ProviderID, ModelID: s.Identity.ModelID, ProviderVersion: &pv, ModelVersion: &mv, SelectionVersion: s.SelectionVersion, CredentialRef: s.CredentialRef, Consumer: c.consumer, Secret: c.secret}
	if c.draft.LeaseID != "" {
		v, e := f.ParseID[sc.Lease](c.draft.LeaseID)
		if e != nil {
			return d, unavailable(e)
		}
		d.LeaseID = &v
	}
	return d, nil
}
func (a *Authority) validateResolutionConsumer(ctx context.Context, tx f.Tx, c *resolutionCandidate) error {
	if e := resolutionContextError(ctx); e != nil {
		return e
	}
	if a.state() == nil || a.state().auth.Resolution == nil || c == nil || c.authority != a {
		return fault(f.DependencyUnbound)
	}
	if _, e := a.state().store.InTx(tx); e != nil {
		return portError(e)
	}
	if e := a.state().store.RequireHeldLocks(ctx, tx, c.locks); e != nil {
		return portError(e)
	}
	return portError(a.state().auth.Resolution.Consumers.ValidateInTx(ctx, tx, c.consumerRequest, c.consumer))
}
func checkCurrentResolution(ctx context.Context, x postgres.SQLExecutor, c *resolutionCandidate) error {
	d, e := currentResolutionDraft(ctx, x, c.request, c.draft.Snapshot.ID)
	if e != nil {
		return e
	}
	d.LeaseID = c.draft.LeaseID
	if !resolutionEqual(d, c.draft) {
		return fault(f.ResourceBusy)
	}
	return nil
}
func checkCanonicalResolution(ctx context.Context, x postgres.SQLExecutor, c *resolutionCandidate) error {
	canonical, e := canonicalResolutionLease(ctx, x, c.request, c.draft)
	if e != nil {
		return e
	}
	if canonical != "" && canonical != c.draft.LeaseID {
		return fault(f.ResourceBusy)
	}
	return nil
}

// ResolveModelInTx returns a provisional value. The caller must propagate all
// errors and wait for its outer commit before publishing or using the value.
func (s *Service) ResolveModelInTx(ctx context.Context, tx f.Tx, request mc.ResolveRequest, plan mc.ResolutionPlan) (mc.ResolvedModel, error) {
	empty := mc.ResolvedModel{}
	r := request.Clone()
	if e := s.resolutionState(ctx, r); e != nil {
		return empty, e
	}
	b, e := mc.ResolveBinding(r)
	if e != nil {
		return empty, e
	}
	d := plan.Details()
	a := s.state().authority
	if plan.Validate() != nil || !plan.Matches(a.state().resolutionIssuer, b, d.Mapping) {
		return empty, fault(f.Forbidden)
	}
	identity, e := resolutionIdentity(r)
	if e != nil {
		return empty, e
	}
	lease := ""
	if d.LeaseID != nil {
		lease = d.LeaseID.String()
	}
	q, e := resolutionConsumerRequest(r, d.SnapshotID, lease)
	if e != nil {
		return empty, e
	}
	c := &resolutionCandidate{authority: a, request: r, identity: identity, consumerRequest: q, consumer: d.Consumer, locks: plan.RequiredLocks(), secret: d.Secret}
	if e = a.validateResolutionConsumer(ctx, tx, c); e != nil {
		return empty, e
	}
	x, e := s.state().store.InTx(tx)
	if e != nil {
		return empty, portError(e)
	}
	row, e := loadResolutionPreparation(ctx, x, identity.Canonical())
	if e != nil {
		return empty, e
	}
	if row == nil {
		return empty, fault(f.ResourceBusy)
	}
	if row.Semantic != resolutionSemantic(r) {
		return empty, fault(f.IdempotencyKeyReused)
	}
	c.row = row
	c.draft = row.Draft
	c.version = row.Version
	if c.mapping() != d.Mapping || c.draft.Snapshot.ID != d.SnapshotID || c.draft.Snapshot.Identity.ProviderID != d.ProviderID || c.draft.Snapshot.Identity.ModelID != d.ModelID || d.ProviderVersion == nil || *d.ProviderVersion != c.draft.ProviderVersion || d.ModelVersion == nil || *d.ModelVersion != c.draft.ModelVersion || !resolutionEqual(d.SelectionVersion, c.draft.Snapshot.SelectionVersion) || c.draft.LeaseID != lease {
		return empty, fault(f.ResourceBusy)
	}
	expected, e := resolutionUnion(resolutionBaseLocks(r, identity, d.SnapshotID, c.draft), c.consumer.RequiredLocks())
	if e != nil {
		return empty, e
	}
	if c.secret != nil {
		expected, e = resolutionUnion(expected, c.secret.RequiredLocks())
		if e != nil {
			return empty, e
		}
	}
	if e = s.state().store.RequireHeldLocks(ctx, tx, expected); e != nil {
		return empty, portError(e)
	}
	if row.Phase == "committed" {
		e = loadCommittedResolution(ctx, x, row)
	} else {
		e = checkCurrentResolution(ctx, x, c)
	}
	if e != nil {
		return empty, e
	}
	if e = checkCanonicalResolution(ctx, x, c); e != nil {
		return empty, e
	}
	if e = completeResolution(ctx, x, c); e != nil {
		return empty, e
	}
	if e = s.applyResolutionSecret(ctx, tx, c); e != nil {
		return empty, e
	}
	snapshot, e := c.draft.Snapshot.snapshot()
	if e != nil {
		return empty, e
	}
	out := mc.ResolvedModel{Snapshot: snapshot, Consumer: r.Consumer.Clone(), LeaseOwner: r.LeaseOwner}
	if d.LeaseID != nil {
		out.CredentialLease = &sc.CredentialLease{LeaseID: *d.LeaseID, CredentialRef: *snapshot.CredentialRef}
	}
	if out.Validate() != nil {
		return empty, unavailable(nil)
	}
	if e = ctx.Err(); e != nil {
		return empty, e
	}
	return out.Clone(), nil
}
func (s *Service) ResolveModel(ctx context.Context, r mc.ResolveRequest) (mc.ResolvedModel, error) {
	empty := mc.ResolvedModel{}
	r = r.Clone()
	p, e := s.DiscoverResolve(ctx, r)
	if e != nil {
		return empty, e
	}
	identity, e := resolutionIdentity(r)
	if e != nil {
		return empty, e
	}
	cause, e := f.NewCommandsCause(identity)
	if e != nil {
		return empty, e
	}
	var out mc.ResolvedModel
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
			return portError(e)
		}
		var err error
		out, err = s.ResolveModelInTx(ctx, tx, r, p)
		return err
	})
	if e = commitError(result); e != nil {
		return empty, e
	}
	if e = ctx.Err(); e != nil {
		return empty, e
	}
	return out.Clone(), nil
}
