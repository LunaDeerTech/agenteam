package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

const maxStoppedInvocationOwners = 64

func (s *Service) stoppedInvocationReady(ctx context.Context, r uc.StoppedInvocationRequest) (uc.StoppedInvocationFacts, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s.state() == nil || s.state().ownership == nil || s.state().authority.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	facts, ok := s.state().authority.state().auth.Invocations.(uc.StoppedInvocationFacts)
	if !ok || nilPort(facts) || nilPort(s.state().ownership.proof) {
		return nil, fault(f.DependencyUnbound)
	}
	if r.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if r.Identity.Attempt.ProcessID == s.state().ownership.process {
		return nil, fault(f.ResourceBusy)
	}
	return facts, nil
}

// Only identity/locks are borrowed from this value. It is never passed to
// the live-Actor writer or used to reserve an Invocation.
func stoppedIdentity(r uc.StoppedInvocationRequest) uc.InvocationRequest {
	return uc.InvocationRequest{Identity: r.Identity.Clone()}
}

func stoppedOwners(ctx context.Context, x postgres.SQLExecutor, r uc.StoppedInvocationRequest) ([]projectCallRecord, error) {
	rows, err := x.Query(ctx, `SELECT `+projectCallProjection+` FROM agenteam_model.usage_project_calls WHERE project_id=$1 AND lane='invocation' AND call_id=$2 AND process_id=$3 ORDER BY id LIMIT 65`, r.Identity.Consumer.ProjectID.String(), r.Identity.Attempt.CallID.String(), r.Identity.Attempt.ProcessID.String())
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	var out []projectCallRecord
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, dbError(err)
		}
		v, err := decodeProjectCall(raw)
		if err != nil {
			return nil, err
		}
		if v.Project != r.Identity.Consumer.ProjectID || v.Process != r.Identity.Attempt.ProcessID || v.CallID != r.Identity.Attempt.CallID.String() || v.Lane != projectCallInvocation || len(out) == maxStoppedInvocationOwners {
			return nil, fault(f.InvalidState)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, dbError(err)
	}
	return out, contextError(ctx)
}

func stoppedMapping(dependencies uc.InvocationDependencies, locks []f.LockRequest, rows []projectCallRecord) f.Digest {
	// Monotonic closure is observable with the original plan. Every original
	// owner identity, registration and process remains part of the mapping.
	identities := append([]projectCallRecord(nil), rows...)
	for j := range identities {
		identities[j].Phase, identities[j].Closed = "running", ""
	}
	raw, _ := json.Marshal(struct {
		Mapping f.Digest
		Owners  []projectCallRecord
	}{dependencies.Details().Mapping, identities})
	return planMapping(hash(raw), locks)
}

func (s *Service) DiscoverStoppedInvocation(ctx context.Context, r uc.StoppedInvocationRequest) (uc.StoppedInvocationPlan, error) {
	facts, err := s.stoppedInvocationReady(ctx, r)
	if err != nil {
		return uc.StoppedInvocationPlan{}, err
	}
	r = r.Clone()
	binding, _ := uc.StoppedInvocationBinding(r)
	dependencies, err := facts.DiscoverStoppedInvocationFacts(ctx, r)
	if err != nil {
		return uc.StoppedInvocationPlan{}, portError(err)
	}
	if dependencies.Validate() != nil || dependencies.Details().Binding != binding {
		return uc.StoppedInvocationPlan{}, fault(f.Forbidden)
	}
	identity := stoppedIdentity(r)
	locks := unionLocks(append(localLocks(identity), dependencies.RequiredLocks()...))
	cause := invocationCause(identity)
	var owners []projectCallRecord
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.state().ownership.proof.RequireCurrentProcessInTx(ctx, tx, s.state().ownership.process); err != nil {
			return portError(err)
		}
		if err := facts.ValidateStoppedInvocationFactsInTx(ctx, tx, r, dependencies); err != nil {
			return portError(err)
		}
		x, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if _, err = stoppedOriginal(ctx, x, r); err != nil {
			return err
		}
		owners, err = stoppedOwners(ctx, x, r)
		return err
	})
	if err = completedRead(ctx, result); err != nil {
		return uc.StoppedInvocationPlan{}, err
	}
	// The real Guard proves death outside SQL. Only the private issuer below
	// can turn this completed proof into a plan usable in a later transaction.
	if err = s.state().ownership.proof.ConfirmStopped(ctx, r.Identity.Attempt.ProcessID); err != nil {
		return uc.StoppedInvocationPlan{}, portError(err)
	}
	return uc.NewStoppedInvocationPlan(s.state().issuer, uc.InvocationPlanDetails{Binding: binding, Mapping: stoppedMapping(dependencies, locks, owners), Locks: locks, Facts: dependencies, Cause: cause})
}

func stoppedOriginal(ctx context.Context, x postgres.SQLExecutor, r uc.StoppedInvocationRequest) (*invocationRecord, error) {
	old, err := loadInvocation(ctx, x, r.Identity.Attempt.InvocationID)
	if err != nil {
		return nil, err
	}
	if old == nil || !old.header.Identity.Equal(r.Identity) || validLedgerValue(old.value) != nil {
		return nil, fault(f.Forbidden)
	}
	if old.sequence == r.BeforeSequence && sameValue(old.value, r.Before) || old.sequence == r.AfterSequence && sameValue(old.value, r.After) {
		return old, nil
	}
	return nil, fault(f.VersionConflict)
}

func (s *Service) validateStoppedInvocation(ctx context.Context, tx f.Tx, r uc.StoppedInvocationRequest, p uc.StoppedInvocationPlan) (postgres.SQLExecutor, *invocationRecord, []projectCallRecord, error) {
	facts, err := s.stoppedInvocationReady(ctx, r)
	if err != nil {
		return nil, nil, nil, err
	}
	d := p.Details()
	binding, _ := uc.StoppedInvocationBinding(r)
	identity := stoppedIdentity(r)
	locks := unionLocks(append(localLocks(identity), d.Facts.RequiredLocks()...))
	if p.Validate() != nil || d.Facts.Validate() != nil || d.Facts.Details().Binding != binding || !locksEqual(locks, p.RequiredLocks()) || d.Cause.Details().Primary.Canonical() != invocationCause(identity).Details().Primary.Canonical() {
		return nil, nil, nil, fault(f.Forbidden)
	}
	if err = s.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, nil, nil, portError(err)
	}
	if err = s.state().ownership.proof.RequireCurrentProcessInTx(ctx, tx, s.state().ownership.process); err != nil {
		return nil, nil, nil, portError(err)
	}
	if err = facts.ValidateStoppedInvocationFactsInTx(ctx, tx, r, d.Facts); err != nil {
		return nil, nil, nil, portError(err)
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return nil, nil, nil, portError(err)
	}
	owners, err := stoppedOwners(ctx, x, r)
	if err != nil {
		return nil, nil, nil, err
	}
	if !p.Matches(s.state().issuer, binding, stoppedMapping(d.Facts, locks, owners)) {
		return nil, nil, nil, fault(f.Forbidden)
	}
	old, err := stoppedOriginal(ctx, x, r)
	return x, old, owners, err
}

func stoppedCloseSQL(rows []projectCallRecord, args []any, dependency string) (string, []any) {
	var matches []string
	for _, r := range rows {
		if r.Phase == "business_closed" {
			continue
		}
		n := len(args)
		args = append(args, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), r.CallID, r.Registered)
		matches = append(matches, fmt.Sprintf("(id=$%d::uuid AND project_id=$%d::uuid AND process_id=$%d::uuid AND instance_id=$%d::uuid AND call_id=$%d::uuid AND registered_at=$%d::timestamptz)", n+1, n+2, n+3, n+4, n+5, n+6))
	}
	if len(matches) == 0 {
		return "SELECT 1", args
	}
	q := `UPDATE agenteam_model.usage_project_calls SET phase='business_closed',closed_at=GREATEST(clock_timestamp(),registered_at) WHERE lane='invocation' AND phase='running' AND (` + strings.Join(matches, " OR ") + `)`
	if dependency != "" {
		q += ` AND EXISTS(SELECT 1 FROM ` + dependency + `)`
	}
	return q + ` RETURNING id`, args
}

func (s *Service) RecoverStoppedInvocationInTx(ctx context.Context, tx f.Tx, r uc.StoppedInvocationRequest, p uc.StoppedInvocationPlan) error {
	x, old, owners, err := s.validateStoppedInvocation(ctx, tx, r, p)
	if err != nil {
		return err
	}
	if old.value.Final != nil {
		if !sameValue(old.value, r.After) || old.sequence != r.AfterSequence {
			return fault(f.VersionConflict)
		}
		q, args := stoppedCloseSQL(owners, nil, "")
		_, err = x.Exec(ctx, q, args...)
		return dbError(err)
	}
	value := r.After.Clone()
	value.LiveModelID, value.LiveProviderID = old.value.Clone().LiveModelID, old.value.Clone().LiveProviderID
	var observe *uc.InvocationReceipt
	previous := old.value.Clone()
	if r.AfterSequence == r.BeforeSequence+2 {
		intermediate := value.Clone()
		intermediate.Final = nil
		if err = transition(previous, intermediate, uc.ObserveAction); err != nil {
			return err
		}
		v := uc.InvocationReceipt{Action: uc.ObserveAction, Sequence: r.BeforeSequence + 1, Digest: digestFact(old.header, uc.ObserveAction, r.BeforeSequence+1, intermediate), Value: historyValue(intermediate)}
		observe, previous = &v, intermediate
	}
	if err = transition(previous, value, uc.FinalizeAction); err != nil || validLedgerValue(value) != nil {
		return fault(f.InvalidState)
	}
	now, err := dbNow(ctx, x)
	if err != nil {
		return err
	}
	final := uc.InvocationReceipt{Action: uc.FinalizeAction, Sequence: r.AfterSequence, Digest: digestFact(old.header, uc.FinalizeAction, r.AfterSequence, value), Value: historyValue(value)}
	if final.Validate() != nil || observe != nil && observe.Validate() != nil {
		return fault(f.InvalidState)
	}
	raw, err := encode(receiptDTO{1, final})
	if err != nil || len(raw) > 16<<10 {
		return fault(f.InvalidState)
	}
	summary, err := nextSummary(ctx, x, value, &old.value, now)
	if err != nil {
		return err
	}
	q, args, err := invocationStatement(old.header, value, r.AfterSequence, now, raw, final.Digest, uc.FinalizeAction, summary)
	if err != nil {
		return err
	}
	q = strings.TrimSuffix(q, ` SELECT invocation_id FROM observation_written`)
	if observe != nil {
		raw, err = encode(receiptDTO{1, *observe})
		if err != nil || len(raw) > 16<<10 {
			return fault(f.InvalidState)
		}
		n := len(args)
		args = append(args, int64(observe.Sequence), string(observe.Digest), raw)
		q += fmt.Sprintf(`, stopped_observation AS (INSERT INTO agenteam_model.invocation_observations(invocation_id,project_id,sequence,action,fact_digest,receipt_data,recorded_at) SELECT id,$4,$%d,'observe',$%d,$%d,$27 FROM invocation_written RETURNING invocation_id)`, n+1, n+2, n+3)
	}
	closeSQL, args := stoppedCloseSQL(owners, args, "observation_written")
	q += `, stopped_owners AS (` + closeSQL + `) SELECT invocation_id FROM observation_written`
	if err = contextError(ctx); err != nil {
		return err
	}
	// Header, observations, summary and borrower closure are one SQL statement;
	// the original Initiator is copied from Usage, never reconstructed as Actor.
	_, err = x.Exec(ctx, q, args...)
	return dbError(err)
}

func (s *Service) StoppedInvocationAppliedInTx(ctx context.Context, tx f.Tx, r uc.StoppedInvocationRequest, p uc.StoppedInvocationPlan) (bool, error) {
	x, old, owners, err := s.validateStoppedInvocation(ctx, tx, r, p)
	if err != nil {
		return false, err
	}
	if old.sequence != r.AfterSequence || !sameValue(old.value, r.After) {
		return false, nil
	}
	for _, owner := range owners {
		if owner.Phase != "business_closed" {
			return false, nil
		}
	}
	request := uc.InvocationRequest{Identity: r.Identity.Clone(), Sequence: r.AfterSequence}
	receipt, err := loadReceipt(ctx, x, request, old.header)
	if err != nil || receipt == nil {
		return false, err
	}
	return receipt.Action == uc.FinalizeAction && receipt.Sequence == r.AfterSequence && receipt.Digest == digestFact(old.header, uc.FinalizeAction, r.AfterSequence, r.After) && sameValue(receipt.Value, r.After), nil
}

var _ uc.StoppedInvocationRecovery = (*Service)(nil)
