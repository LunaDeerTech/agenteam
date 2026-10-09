package knowledge

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func mutationLocks(request kc.TreeMutation) ([]f.LockRequest, error) {
	if request.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	d := request.Details()
	locks, err := scopeLocks(d.Actor, d.ProjectID, true)
	if err != nil {
		return nil, err
	}
	identity, err := kc.CommandIdentity(d.ProjectID, d.Command, d.Meta.IdempotencyKey)
	if err != nil {
		return nil, portError(err)
	}
	key, err := f.CommandLock(identity)
	if err != nil {
		return nil, portError(err)
	}
	return ob.NormalizeLocks(append(locks, f.LockRequest{Key: key, Mode: f.Exclusive}))
}

func (s *Service) discoverMove(ctx context.Context, request kc.TreeMutation) (kc.MutationPlan, error) {
	if request.Validate() != nil || request.Details().Command != kc.Move {
		return kc.MutationPlan{}, fault(f.InvalidArgument)
	}
	d := request.Details()
	if err := readInput(ctx, d.Actor, d.ProjectID); err != nil {
		return kc.MutationPlan{}, err
	}
	semantic, err := kc.MoveDigest(d.Actor, d.Meta, d.ProjectID, d.DocumentID, *d.Move)
	if err != nil {
		return kc.MutationPlan{}, err
	}
	var mapping f.Digest
	err = s.read(ctx, d.Actor, d.ProjectID, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := loadCommand(ctx, x, d.ProjectID, kc.Move, d.Meta.IdempotencyKey)
		if err != nil {
			return err
		}
		if row != nil {
			if row.digest != semantic || row.user.String() != d.Actor.Details().UserID {
				return fault(f.IdempotencyKeyReused)
			}
			if row.state == kc.Committed {
				mapping = ob.DigestBytes([]byte("knowledge.completed:" + row.id.String() + ":" + semantic.String()))
				return nil
			}
		}
		_, facts, err := moveFacts(ctx, x, d.ProjectID, d.DocumentID, *d.Move)
		if err != nil {
			return err
		}
		if _, err = kc.CheckMove(d.ProjectID, d.DocumentID, *d.Move, facts); err != nil {
			return err
		}
		mapping, err = moveMapping(facts)
		return err
	})
	if err != nil {
		return kc.MutationPlan{}, err
	}
	locks, err := mutationLocks(request)
	if err != nil {
		return kc.MutationPlan{}, err
	}
	return kc.NewMutationPlan(s.state().issuer, kc.MutationPlanDetails{Request: request, DomainMapping: mapping, Locks: locks})
}

func (s *Service) AcquireMutationInTx(ctx context.Context, tx f.Tx, plan kc.MutationPlan, extra []f.LockRequest) (kc.LockedMutation, error) {
	st := s.state()
	if st == nil {
		return kc.LockedMutation{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || !plan.IssuedBy(st.issuer) {
		return kc.LockedMutation{}, fault(f.InvalidArgument)
	}
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return kc.LockedMutation{}, err
	}
	defer done()
	if _, err = st.store.InTx(tx); err != nil {
		return kc.LockedMutation{}, portError(err)
	}
	union, err := oc.NormalizeAccessLocks(append(plan.Locks(), extra...))
	if err != nil {
		return kc.LockedMutation{}, portError(err)
	}
	var access oc.LockedAccess
	if len(plan.Details().ObjectPlans) > 0 {
		access, err = st.deps.Objects.AcquireAccessPlansInTx(ctx, tx, plan.Details().ObjectPlans, union)
	} else {
		err = st.store.AcquireAll(ctx, tx, union)
	}
	if err != nil {
		return kc.LockedMutation{}, portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, union); err != nil {
		return kc.LockedMutation{}, portError(err)
	}
	return kc.NewLockedMutation(st.issuer, kc.LockedMutationDetails{Tx: tx, Plan: plan, ExtraLocks: extra, ObjectAccess: access})
}
