package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// LookupResolution confirms the original resolution under its command lock.
// It does not prepare, acquire a lease or invoke a provider. The consumer must
// still authorize the original call/generation, including its recovery state.
func (s *Service) LookupResolution(ctx context.Context, request mc.ResolveRequest) (mc.ResolvedModel, bool, error) {
	empty := mc.ResolvedModel{}
	r := request.Clone()
	if err := s.resolutionState(ctx, r); err != nil {
		return empty, false, err
	}
	identity, err := resolutionIdentity(r)
	if err != nil {
		return empty, false, err
	}
	row, readErr := loadResolutionPreparation(ctx, s.state().store, identity.Canonical())
	snapshot, err := f.NewID[mc.Snapshot]()
	if err != nil {
		return empty, false, unavailable(err)
	}
	if r.ServingSnapshotID != nil {
		snapshot = *r.ServingSnapshotID
	}
	draft := resolutionDraft{}
	lease := ""
	if row != nil {
		snapshot = row.SnapshotID
		draft = row.Draft
		lease = row.Draft.LeaseID
	}
	consumerRequest, err := resolutionConsumerRequest(r, snapshot, lease)
	if err != nil {
		return empty, false, err
	}
	a := s.state().authority
	consumer, err := a.state().auth.Resolution.Consumers.Discover(ctx, consumerRequest)
	if err != nil {
		return empty, false, portError(err)
	}
	binding, err := mc.ConsumerBinding(consumerRequest)
	if err != nil || consumer.Validate() != nil || consumer.Details().Binding != binding {
		return empty, false, fault(f.Forbidden)
	}
	locks, err := resolutionUnion(resolutionBaseLocks(r, identity, snapshot, draft), consumer.RequiredLocks())
	if err != nil {
		return empty, false, err
	}
	candidate := &resolutionCandidate{authority: a, request: r, identity: identity, row: row, draft: draft, consumerRequest: consumerRequest, consumer: consumer, locks: locks}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return empty, false, err
	}
	var out mc.ResolvedModel
	found := false
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := a.validateResolutionConsumer(ctx, tx, candidate); err != nil {
			return err
		}
		if readErr != nil {
			return readErr
		}
		x, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := loadResolutionPreparation(ctx, x, identity.Canonical())
		if err != nil {
			return err
		}
		if !resolutionEqual(current, row) {
			return fault(f.ResourceBusy)
		}
		if current == nil {
			return nil
		}
		if current.Semantic != resolutionSemantic(r) {
			return fault(f.IdempotencyKeyReused)
		}
		if current.Phase != "committed" {
			return nil
		}
		if err := loadCommittedResolution(ctx, x, current); err != nil {
			return err
		}
		snapshot, err := current.Draft.Snapshot.snapshot()
		if err != nil {
			return err
		}
		out = mc.ResolvedModel{Snapshot: snapshot, Consumer: r.Consumer.Clone(), LeaseOwner: r.LeaseOwner}
		if current.Draft.LeaseID != "" {
			lease, err := f.ParseID[sc.Lease](current.Draft.LeaseID)
			if err != nil {
				return unavailable(err)
			}
			out.CredentialLease = &sc.CredentialLease{LeaseID: lease, CredentialRef: *snapshot.CredentialRef}
		}
		if out.Validate() != nil {
			return unavailable(nil)
		}
		found = true
		return nil
	})
	if err = commitError(result); err != nil {
		return empty, false, err
	}
	if err = resolutionContextError(ctx); err != nil {
		return empty, false, err
	}
	return out.Clone(), found, nil
}

var _ mc.ResolutionLookup = (*Service)(nil)
