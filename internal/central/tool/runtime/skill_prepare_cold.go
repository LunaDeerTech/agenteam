package runtime

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type skillPreparationColdPlan struct {
	issuer     *BuiltinAuthority
	witness    *builtinWitness
	proof      *builtinColdProof
	invocation sc.PreparationInvocation
	locks      []f.LockRequest
}

func (p *skillPreparationColdPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

func (a *BuiltinAuthority) skillPreparationColdWitness(ctx context.Context, inv sc.PreparationInvocation) (*builtinWitness, error) {
	if inv.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	w, err := a.witness(ctx, inv.Invocation.Actor, inv.Invocation.ProjectID)
	if err != nil {
		return nil, err
	}
	b := w.call.binding
	i := inv.Invocation
	if !w.call.lookup || w.cleanupOnly.Load() || w.call.cold == nil || !w.call.cold.permits(a.process, w.call.record) ||
		b.Binding != (tc.BuiltinBinding{HandlerID: string(sc.PrepareSkill), ContractRevision: 1}) ||
		b.AgentID != i.AgentID || b.ExecutionID != i.ExecutionID || b.Input.PayloadID != i.ToolCallID || b.Spec.ToolID != i.ToolID || b.Spec.SpecRevision != i.SpecRevision ||
		w.call.record.ID.String() != i.OperationID || w.call.record.Attempt.String() != i.AttemptID || w.call.record.Request != inv.RequestID || w.call.record.Phase != "running" || w.call.record.Version != 2 {
		return nil, fail(f.Forbidden)
	}
	if _, ok := w.owner.executions.(skillToolBindings); !ok {
		return nil, fail(f.DependencyUnbound)
	}
	return w, nil
}

func (a *BuiltinAuthority) DiscoverSkillPreparationCold(ctx context.Context, inv sc.PreparationInvocation) (sc.InvocationPlan, error) {
	w, err := a.skillPreparationColdWitness(ctx, inv)
	if err != nil {
		return nil, err
	}
	schedule, _ := f.ProjectScheduleLock(inv.Invocation.ProjectID.String())
	agent, _ := f.AgentLock(inv.Invocation.AgentID.String())
	locks, err := ob.NormalizeLocks(append(slices.Clone(w.call.locks), f.LockRequest{Key: schedule, Mode: f.Exclusive}, f.LockRequest{Key: agent, Mode: f.Exclusive}))
	if err != nil {
		return nil, err
	}
	return &skillPreparationColdPlan{issuer: a, witness: w, proof: w.call.cold, invocation: inv, locks: locks}, nil
}

func (a *BuiltinAuthority) WithSkillPreparationColdInTx(ctx context.Context, tx f.Tx, inv sc.PreparationInvocation, plan sc.InvocationPlan, consume func(context.Context, sc.PreparationColdInput) error) error {
	w, err := a.skillPreparationColdWitness(ctx, inv)
	if err != nil {
		return err
	}
	p, ok := plan.(*skillPreparationColdPlan)
	if !ok || p == nil || p.issuer != a || p.witness != w || p.proof != w.call.cold || p.invocation.RequestID != inv.RequestID || !sameSkillInvocation(p.invocation.Invocation, inv.Invocation) || consume == nil {
		return fail(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	return a.withCurrent(ctx, tx, w, func(current context.Context, raw []byte) error {
		bindings, err := w.owner.executions.(skillToolBindings).ReadSkillToolBindingsInTx(current, tx, w.call.binding, w.call.plan)
		if err != nil {
			return err
		}
		if bindings == nil {
			return fail(f.InvalidState)
		}
		for n, binding := range bindings {
			if binding.Validate() != nil || n > 0 && bindings[n-1].SkillID.String() >= binding.SkillID.String() {
				return fail(f.InvalidState)
			}
		}
		return consume(current, sc.PreparationColdInput{Arguments: raw, Bindings: slices.Clone(bindings), OperationVersion: w.call.record.Version, OldProcess: p.proof.process})
	})
}

func skillPrepareColdLookup(ctx context.Context) bool {
	w, _ := ctx.Value(builtinWitnessKey{}).(*builtinWitness)
	return w != nil && w.call != nil && w.call.cold != nil
}

var _ sc.PreparationColdAuthority = (*BuiltinAuthority)(nil)
