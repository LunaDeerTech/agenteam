package mount

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ExecutionCapture struct {
	store Store
	owner mc.ExecutionMountCaptureAuthority
}

func NewExecutionCapture(store Store, owner mc.ExecutionMountCaptureAuthority) (*ExecutionCapture, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ExecutionCapture{store, owner}, nil
}

type executionMountPlan struct {
	owner   *ExecutionCapture
	request mc.ExecutionMountCaptureRequest
	attempt f.Digest
	version f.Version
	locks   []f.LockRequest
}

func (p *executionMountPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*executionMountPlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("execution_mount_plan"))
}
func (*executionMountPlan) LogValue() slog.Value { return slog.StringValue("execution_mount_plan") }
func mountCaptureLocks(r mc.ExecutionMountCaptureRequest) []f.LockRequest {
	p, _ := f.ProjectLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	s, _ := f.ProjectScheduleLock(r.ProjectID.String())
	locks, _ := oc.NormalizeLocks([]f.LockRequest{{Key: p, Mode: f.Shared}, {Key: a, Mode: f.Shared}, {Key: e, Mode: f.Exclusive}, {Key: s, Mode: f.Exclusive}})
	return locks
}
func (p *ExecutionCapture) valid(ctx context.Context, r mc.ExecutionMountCaptureRequest) error {
	if ctx == nil || r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || nilPort(p.store) || nilPort(p.owner) {
		return fault(f.DependencyUnbound)
	}
	return nil
}
func (p *ExecutionCapture) executor(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	x, err := p.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	return x, nil
}
func mountScope(r mc.ExecutionMountCaptureRequest, s mc.ExecutionMountCaptureScope) error {
	if s.Project.Validate() != nil || s.Project.ID != r.ProjectID || s.Project.Lifecycle != pc.Active || s.AttemptBinding.Validate() != nil {
		return fault(f.Forbidden)
	}
	return nil
}
func readEmptyMountSource(ctx context.Context, x postgres.SQLExecutor, r mc.ExecutionMountCaptureRequest) (referenceState, error) {
	state, err := loadReferences(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return referenceState{}, err
	}
	if !state.exists {
		return referenceState{}, fault(f.DependencyUnbound)
	}
	if len(state.ids) != 0 {
		return referenceState{}, fault(f.DependencyUnbound)
	}
	return state, nil
}
func (p *ExecutionCapture) DiscoverExecutionMounts(ctx context.Context, r mc.ExecutionMountCaptureRequest) (mc.ExecutionMountCapturePlan, error) {
	if err := p.valid(ctx, r); err != nil {
		return nil, err
	}
	locks := mountCaptureLocks(r)
	attempt, err := f.NewID[f.TransactionAttempt]()
	if err != nil {
		return nil, portError(err)
	}
	cause, err := f.NewJobCause("mount-execution-capture", r.ExecutionID.String(), attempt.String())
	if err != nil {
		return nil, err
	}
	var scope mc.ExecutionMountCaptureScope
	var state referenceState
	result := p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := p.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := p.executor(ctx, tx, locks)
		if err != nil {
			return err
		}
		scope, err = p.owner.RequireMountCaptureDiscoveryInTx(ctx, tx, r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return portError(err)
		}
		if err = mountScope(r, scope); err != nil {
			return err
		}
		state, err = readEmptyMountSource(ctx, x, r)
		return err
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &executionMountPlan{p, r, scope.AttemptBinding, state.version, locks}, nil
}
func (p *ExecutionCapture) CaptureExecutionMountsInTx(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest, plan mc.ExecutionMountCapturePlan) (mc.ExecutionMountSet, error) {
	var zero mc.ExecutionMountSet
	if err := p.valid(ctx, r); err != nil {
		return zero, err
	}
	selected, ok := plan.(*executionMountPlan)
	if !ok || selected == nil || selected.owner != p || selected.request != r {
		return zero, fault(f.Forbidden)
	}
	x, err := p.executor(ctx, tx, selected.locks)
	if err != nil {
		return zero, err
	}
	facts, err := p.owner.RequireMountCaptureInTx(ctx, tx, r)
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if err != nil {
		return zero, portError(err)
	}
	if err = mountScope(r, facts.Scope); err != nil {
		return zero, err
	}
	if facts.Scope.AttemptBinding != selected.attempt || facts.AgentVersion != selected.version {
		return zero, fault(f.ConfirmationStale)
	}
	if facts.AllowedMountIDs == nil || len(facts.AllowedMountIDs) != 0 {
		return zero, fault(f.DependencyUnbound)
	}
	state, err := readEmptyMountSource(ctx, x, r)
	if err != nil {
		return zero, err
	}
	if state.version != selected.version {
		return zero, fault(f.ConfirmationStale)
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return mc.ExecutionMountSet{Request: r, AgentVersion: state.version, Mounts: []mc.ExecutionMountMetadata{}}, nil
}

var _ mc.ExecutionMountCaptureProvider = (*ExecutionCapture)(nil)
