package model

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ExecutionCapture adds only current Agent-source/attempt binding around the
// existing Resolver. It has no worker, network adapter or new persistence.
type ExecutionCapture struct {
	models *Service
	owner  mc.ExecutionModelCaptureAuthority
}

func NewExecutionCapture(models *Service, owner mc.ExecutionModelCaptureAuthority) (*ExecutionCapture, error) {
	if models.state() == nil || models.state().authority.state() == nil || models.state().authority.state().auth.Resolution == nil || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ExecutionCapture{models, owner}, nil
}

type executionModelPlan struct {
	owner      *ExecutionCapture
	request    mc.ExecutionModelCaptureRequest
	attempt    f.Digest
	source     executionModelSource
	resolve    mc.ResolveRequest
	resolution mc.ResolutionPlan
	locks      []f.LockRequest
}

func (p *executionModelPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*executionModelPlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("execution_model_capture_plan"))
}
func (*executionModelPlan) LogValue() slog.Value {
	return slog.StringValue("execution_model_capture_plan")
}
func executionCaptureError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return portError(err)
}
func executionModelLocks(r mc.ExecutionModelCaptureRequest) []f.LockRequest {
	p, _ := f.ProjectLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	s, _ := f.ProjectScheduleLock(r.ProjectID.String())
	locks, _ := resolutionUnion([]f.LockRequest{{Key: p, Mode: f.Shared}, {Key: a, Mode: f.Shared}, {Key: e, Mode: f.Exclusive}, {Key: s, Mode: f.Exclusive}, systemLock("model-references", f.Shared), recordLock(f.ReferenceRecordLock, "model:agent:"+r.AgentID.String())})
	return locks
}
func (p *ExecutionCapture) valid(ctx context.Context, r mc.ExecutionModelCaptureRequest) error {
	if ctx == nil || r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.models.state() == nil || nilPort(p.owner) {
		return fault(f.DependencyUnbound)
	}
	return nil
}
func (p *ExecutionCapture) executor(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	store := p.models.state().store
	x, err := store.InTx(tx)
	if err != nil {
		return nil, executionCaptureError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, executionCaptureError(err)
	}
	return x, nil
}
func modelCaptureScope(r mc.ExecutionModelCaptureRequest, scope mc.ExecutionModelCaptureScope) error {
	if scope.Project.Validate() != nil || scope.Project.ID != r.ProjectID || scope.Project.Lifecycle != pc.Active || scope.AttemptBinding.Validate() != nil {
		return fault(f.Forbidden)
	}
	return nil
}
func (p *ExecutionCapture) DiscoverExecutionModel(ctx context.Context, r mc.ExecutionModelCaptureRequest) (mc.ExecutionModelCapturePlan, error) {
	if err := p.valid(ctx, r); err != nil {
		return nil, err
	}
	locks := executionModelLocks(r)
	cause, err := readCause("execution-capture")
	if err != nil {
		return nil, err
	}
	var scope mc.ExecutionModelCaptureScope
	var source executionModelSource
	store := p.models.state().store
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return executionCaptureError(err)
		}
		x, err := p.executor(ctx, tx, locks)
		if err != nil {
			return err
		}
		scope, err = p.owner.RequireModelCaptureDiscoveryInTx(ctx, tx, r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return executionCaptureError(err)
		}
		if err = modelCaptureScope(r, scope); err != nil {
			return err
		}
		source, err = readExecutionModelSource(ctx, x, r)
		return err
	})
	if err = commitError(result); err != nil {
		if result.State() == f.Unknown && scope.AttemptBinding.Validate() == nil {
			return nil, &executionCaptureUnknown{owner: p, request: r, attempt: scope.AttemptBinding, source: source, original: err, locks: slices.Clone(locks)}
		}
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	request, err := executionModelResolveRequest(r, source)
	if err != nil {
		return nil, err
	}
	resolution, err := p.models.DiscoverResolve(ctx, request)
	if err != nil {
		var unknown *resolutionDiscoveryUnknown
		if errors.As(err, &unknown) {
			return nil, &executionCaptureUnknown{owner: p, request: r, attempt: scope.AttemptBinding, source: source, original: err, discovery: unknown, locks: slices.Clone(locks)}
		}
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	locks, err = resolutionUnion(locks, resolution.RequiredLocks())
	if err != nil {
		return nil, err
	}
	return &executionModelPlan{p, r, scope.AttemptBinding, source, request, resolution, locks}, nil
}
func (p *ExecutionCapture) ResolveExecutionModelInTx(ctx context.Context, tx f.Tx, r mc.ExecutionModelCaptureRequest, plan mc.ExecutionModelCapturePlan) (mc.ResolvedModel, error) {
	var zero mc.ResolvedModel
	if err := p.valid(ctx, r); err != nil {
		return zero, err
	}
	selected, ok := plan.(*executionModelPlan)
	if !ok || selected == nil || selected.owner != p || selected.request != r {
		return zero, fault(f.Forbidden)
	}
	x, err := p.executor(ctx, tx, selected.locks)
	if err != nil {
		return zero, err
	}
	facts, err := p.owner.RequireModelCaptureInTx(ctx, tx, r)
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if err != nil {
		return zero, executionCaptureError(err)
	}
	if err = modelCaptureScope(r, facts.Scope); err != nil {
		return zero, err
	}
	if facts.Scope.AttemptBinding != selected.attempt || facts.AgentVersion != selected.source.Version || !facts.Selection.Equal(selected.source.Selection) {
		return zero, fault(f.ConfirmationStale)
	}
	source, err := readExecutionModelSource(ctx, x, r)
	if err != nil {
		return zero, executionCaptureError(err)
	}
	if !source.equal(selected.source) {
		return zero, fault(f.ConfirmationStale)
	}
	out, err := p.models.ResolveModelInTx(ctx, tx, selected.resolve, selected.resolution)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if out.Validate() != nil || !out.Consumer.Equal(selected.resolve.Consumer) || !out.LeaseOwner.Equal(selected.resolve.LeaseOwner) || out.Snapshot.ID != selected.resolution.Details().SnapshotID || out.Snapshot.Identity.ModelID != selected.source.Selection.ModelID {
		return zero, unavailable(nil)
	}
	return out.Clone(), nil
}

var _ mc.ExecutionModelCaptureProvider = (*ExecutionCapture)(nil)
