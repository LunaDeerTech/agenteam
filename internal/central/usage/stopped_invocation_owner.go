package usage

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func (s *Service) stoppedOwnerFacts(ctx context.Context) (uc.StoppedInvocationOwnerFacts, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s.state() == nil || s.state().ownership == nil || s.state().authority.state() == nil || nilPort(s.state().ownership.proof) {
		return nil, fault(f.DependencyUnbound)
	}
	facts, ok := s.state().authority.state().auth.Invocations.(uc.StoppedInvocationOwnerFacts)
	if !ok || nilPort(facts) {
		return nil, fault(f.DependencyUnbound)
	}
	return facts, nil
}

func (s *Service) DiscoverStoppedInvocationOwners(ctx context.Context) ([]uc.StoppedInvocationOwner, error) {
	facts, err := s.stoppedOwnerFacts(ctx)
	if err != nil {
		return nil, err
	}
	cause, err := f.NewRecoveryCause("usage.invocation-owners", s.state().ownership.process.String(), "")
	if err != nil {
		return nil, err
	}
	var out []uc.StoppedInvocationOwner
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().ownership.proof.RequireCurrentProcessInTx(ctx, tx, s.state().ownership.process); err != nil {
			return portError(err)
		}
		if err := facts.RequireStoppedInvocationStartupInTx(ctx, tx); err != nil {
			return portError(err)
		}
		x, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		rows, err := x.Query(ctx, `SELECT DISTINCT project_id::text,call_id::text,process_id::text FROM agenteam_model.usage_project_calls WHERE lane='invocation' AND phase='running' AND process_id<>$1 ORDER BY project_id::text,call_id::text,process_id::text LIMIT 64`, s.state().ownership.process.String())
		if err != nil {
			return dbError(err)
		}
		defer rows.Close()
		for rows.Next() {
			var p, c, process string
			if err = rows.Scan(&p, &c, &process); err != nil {
				return dbError(err)
			}
			project, pErr := f.ParseID[id.Project](p)
			call, cErr := f.ParseID[mc.Call](c)
			old, oldErr := f.ParseID[oc.Process](process)
			if pErr != nil || cErr != nil || oldErr != nil || len(out) >= maxStoppedInvocationOwners {
				return fault(f.InvalidState)
			}
			out = append(out, uc.StoppedInvocationOwner{ProjectID: project, CallID: call, ProcessID: old})
		}
		return dbError(rows.Err())
	})
	if err = completedRead(ctx, result); err != nil {
		return nil, err
	}
	return out, nil
}

func stoppedOwnerCause(r uc.StoppedInvocationOwner) f.TransactionCause {
	c, _ := f.NewCommandIdentity("model.usage", []string{r.ProjectID.String()}, "call-ledger", f.IdempotencyKey(r.CallID.String()))
	out, _ := f.NewCommandsCause(c)
	return out
}
func stoppedOwnerLocks(r uc.StoppedInvocationOwner, deps uc.InvocationDependencies) []f.LockRequest {
	k, _ := f.CommandLock(stoppedOwnerCause(r).Details().Primary)
	return unionLocks(append(deps.RequiredLocks(), projectLock(r.ProjectID), f.LockRequest{Key: k, Mode: f.Exclusive}))
}
func stoppedOwnerRows(ctx context.Context, x postgres.SQLExecutor, r uc.StoppedInvocationOwner) ([]projectCallRecord, error) {
	// Reuse the exact original row decoder and bounded tuple query. No live
	// Invocation or Actor is constructed or validated by this identity carrier.
	var q uc.StoppedInvocationRequest
	q.Identity.Consumer.ProjectID = r.ProjectID
	q.Identity.Attempt.CallID, q.Identity.Attempt.ProcessID = r.CallID, r.ProcessID
	return stoppedOwners(ctx, x, q)
}

func (s *Service) DiscoverStoppedInvocationOwner(ctx context.Context, r uc.StoppedInvocationOwner) (uc.StoppedInvocationOwnerPlan, error) {
	facts, err := s.stoppedOwnerFacts(ctx)
	if err != nil {
		return uc.StoppedInvocationOwnerPlan{}, err
	}
	if r.Validate() != nil {
		return uc.StoppedInvocationOwnerPlan{}, fault(f.InvalidArgument)
	}
	if r.ProcessID == s.state().ownership.process {
		return uc.StoppedInvocationOwnerPlan{}, fault(f.ResourceBusy)
	}
	binding, _ := uc.StoppedInvocationOwnerBinding(r)
	deps, err := facts.DiscoverStoppedInvocationOwnerFacts(ctx, r)
	if err != nil {
		return uc.StoppedInvocationOwnerPlan{}, portError(err)
	}
	if deps.Validate() != nil || deps.Details().Binding != binding {
		return uc.StoppedInvocationOwnerPlan{}, fault(f.Forbidden)
	}
	locks, cause := stoppedOwnerLocks(r, deps), stoppedOwnerCause(r)
	var owners []projectCallRecord
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.state().ownership.proof.RequireCurrentProcessInTx(ctx, tx, s.state().ownership.process); err != nil {
			return portError(err)
		}
		if err := facts.ValidateStoppedInvocationOwnerFactsInTx(ctx, tx, r, deps); err != nil {
			return portError(err)
		}
		x, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		owners, err = stoppedOwnerRows(ctx, x, r)
		if err == nil && len(owners) == 0 {
			return fault(f.NotFound)
		}
		return err
	})
	if err = completedRead(ctx, result); err != nil {
		return uc.StoppedInvocationOwnerPlan{}, err
	}
	if err = s.state().ownership.proof.ConfirmStopped(ctx, r.ProcessID); err != nil {
		return uc.StoppedInvocationOwnerPlan{}, portError(err)
	}
	return uc.NewStoppedInvocationOwnerPlan(s.state().issuer, uc.InvocationPlanDetails{Binding: binding, Mapping: stoppedMapping(deps, locks, owners), Locks: locks, Facts: deps, Cause: cause})
}

func (s *Service) validateStoppedOwner(ctx context.Context, tx f.Tx, r uc.StoppedInvocationOwner, p uc.StoppedInvocationOwnerPlan) (postgres.SQLExecutor, []projectCallRecord, error) {
	facts, err := s.stoppedOwnerFacts(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !tx.Valid() || r.Validate() != nil || p.Validate() != nil || r.ProcessID == s.state().ownership.process {
		return nil, nil, fault(f.Forbidden)
	}
	binding, _ := uc.StoppedInvocationOwnerBinding(r)
	d := p.Details()
	locks := stoppedOwnerLocks(r, d.Facts)
	if d.Binding != binding || d.Facts.Details().Binding != binding || !p.Matches(s.state().issuer, binding, d.Mapping) || !locksEqual(locks, p.RequiredLocks()) || d.Cause.Details().Primary.Canonical() != stoppedOwnerCause(r).Details().Primary.Canonical() {
		return nil, nil, fault(f.Forbidden)
	}
	if err = s.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, nil, portError(err)
	}
	if err = s.state().ownership.proof.RequireCurrentProcessInTx(ctx, tx, s.state().ownership.process); err != nil {
		return nil, nil, portError(err)
	}
	if err = facts.ValidateStoppedInvocationOwnerFactsInTx(ctx, tx, r, d.Facts); err != nil {
		return nil, nil, portError(err)
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return nil, nil, portError(err)
	}
	owners, err := stoppedOwnerRows(ctx, x, r)
	if err != nil {
		return nil, nil, err
	}
	if len(owners) == 0 || !p.Matches(s.state().issuer, binding, stoppedMapping(d.Facts, locks, owners)) {
		return nil, nil, fault(f.Forbidden)
	}
	return x, owners, nil
}

func (s *Service) CloseStoppedInvocationOwnerInTx(ctx context.Context, tx f.Tx, r uc.StoppedInvocationOwner, p uc.StoppedInvocationOwnerPlan) error {
	x, owners, err := s.validateStoppedOwner(ctx, tx, r, p)
	if err != nil {
		return err
	}
	q, args := stoppedCloseSQL(owners, nil, "")
	_, err = x.Exec(ctx, q, args...)
	return dbError(err)
}
func (s *Service) StoppedInvocationOwnerClosedInTx(ctx context.Context, tx f.Tx, r uc.StoppedInvocationOwner, p uc.StoppedInvocationOwnerPlan) (bool, error) {
	_, owners, err := s.validateStoppedOwner(ctx, tx, r, p)
	if err != nil {
		return false, err
	}
	for _, owner := range owners {
		if owner.Phase != "business_closed" {
			return false, nil
		}
	}
	return true, nil
}

var _ uc.StoppedInvocationOwnerRecovery = (*Service)(nil)
