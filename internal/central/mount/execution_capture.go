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
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

type ExecutionCapture struct {
	store   Store
	owner   mc.ExecutionMountCaptureAuthority
	runners rc.ExecutionMetadataProvider
}

// The original two-argument constructor remains valid for empty captures.
// A nonempty capture needs the actual Runner metadata provider as well.
func NewExecutionCapture(store Store, owner mc.ExecutionMountCaptureAuthority, runners ...rc.ExecutionMetadataProvider) (*ExecutionCapture, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	if len(runners) > 1 {
		return nil, fault(f.InvalidArgument)
	}
	p := &ExecutionCapture{store: store, owner: owner}
	if len(runners) == 1 {
		if nilPort(runners[0]) {
			return nil, fault(f.DependencyUnbound)
		}
		p.runners = runners[0]
	}
	return p, nil
}

type executionMountPlan struct {
	owner   *ExecutionCapture
	request mc.ExecutionMountCaptureRequest
	attempt f.Digest
	version f.Version
	locks   []f.LockRequest
	source  executionMountSource
	runners []rc.ExecutionMetadata
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
	if err = p.store.RequireHeldLocks(ctx, tx, slices.Clone(locks)); err != nil {
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
func runnerMetadataRequest(r mc.ExecutionMountCaptureRequest, source executionMountSource) rc.ExecutionMetadataRequest {
	ids := make([]rc.RunnerID, 0, len(source.mounts))
	for _, m := range source.mounts {
		ids = append(ids, m.RunnerID)
	}
	slices.SortFunc(ids, func(a, b rc.RunnerID) int {
		if a.String() < b.String() {
			return -1
		}
		if a == b {
			return 0
		}
		return 1
	})
	return rc.ExecutionMetadataRequest{ProjectID: r.ProjectID, AgentID: r.AgentID, ExecutionID: r.ExecutionID, RunnerIDs: slices.Compact(ids)}
}
func (p *ExecutionCapture) readRunners(ctx context.Context, tx f.Tx, r rc.ExecutionMetadataRequest) ([]rc.ExecutionMetadata, error) {
	if nilPort(p.runners) {
		return nil, fault(f.DependencyUnbound)
	}
	rows, err := p.runners.ReadExecutionMetadataInTx(ctx, tx, r)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, portError(err)
	}
	if rows == nil || len(rows) != len(r.RunnerIDs) {
		return nil, fault(f.DependencyUnavailable)
	}
	for n, row := range rows {
		if row.Validate() != nil || row.ID != r.RunnerIDs[n] {
			return nil, fault(f.DependencyUnavailable)
		}
	}
	return slices.Clone(rows), nil
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
	var source executionMountSource
	result := p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := p.store.AcquireAll(ctx, tx, slices.Clone(locks)); err != nil {
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
		source, err = readExecutionMountSource(ctx, x, r)
		return err
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	selected := &executionMountPlan{owner: p, request: r, attempt: scope.AttemptBinding, version: source.head.version, locks: locks, source: source}
	if len(source.mounts) == 0 {
		return selected, nil
	}
	if nilPort(p.runners) {
		return nil, fault(f.DependencyUnbound)
	}
	request := runnerMetadataRequest(r, source)
	runnerLocks, err := p.runners.ExecutionMetadataLocks(slices.Clone(request.RunnerIDs))
	if err != nil {
		return nil, portError(err)
	}
	if len(runnerLocks)+len(locks) > 512 {
		return nil, fault(f.InvalidArgument)
	}
	union, err := oc.NormalizeLocks(append(slices.Clone(locks), runnerLocks...))
	if err != nil {
		return nil, portError(err)
	}
	// The first transaction has actually returned. Runner global locks may rank
	// before Agent/Execution locks: acquire the complete new union in a new Tx.
	result = p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := p.store.AcquireAll(ctx, tx, slices.Clone(union)); err != nil {
			return portError(err)
		}
		x, err := p.executor(ctx, tx, union)
		if err != nil {
			return err
		}
		now, err := p.owner.RequireMountCaptureDiscoveryInTx(ctx, tx, r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return portError(err)
		}
		if err = mountScope(r, now); err != nil {
			return err
		}
		if now.AttemptBinding != selected.attempt {
			return fault(f.ConfirmationStale)
		}
		current, err := readExecutionMountSource(ctx, x, r)
		if err != nil {
			return err
		}
		if !sameExecutionMountSource(source, current) {
			return fault(f.ConfirmationStale)
		}
		selected.runners, err = p.readRunners(ctx, tx, request)
		return err
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	selected.locks = union
	return selected, nil
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
	if facts.AllowedMountIDs == nil {
		return zero, fault(f.DependencyUnbound)
	}
	if !slices.Equal(facts.AllowedMountIDs, selected.source.head.ids) {
		return zero, fault(f.ConfirmationStale)
	}
	current, err := readExecutionMountSource(ctx, x, r)
	if err != nil {
		return zero, err
	}
	if !sameExecutionMountSource(selected.source, current) {
		return zero, fault(f.ConfirmationStale)
	}
	value := mc.ExecutionMountSet{Request: r, AgentVersion: current.head.version, Mounts: []mc.ExecutionMountMetadata{}}
	if len(current.mounts) > 0 {
		rows, err := p.readRunners(ctx, tx, runnerMetadataRequest(r, current))
		if err != nil {
			return zero, err
		}
		if !slices.Equal(rows, selected.runners) {
			return zero, fault(f.ConfirmationStale)
		}
		value.AttemptBinding = selected.attempt
		value.Mounts = cloneExecutionMounts(current.mounts)
		for n := range value.Mounts {
			for _, runner := range rows {
				if value.Mounts[n].RunnerID == runner.ID {
					value.Mounts[n].RunnerVersion = runner.Version
					value.Mounts[n].RunnerName = runner.Name
					value.Mounts[n].RunnerDescription = runner.Description
					break
				}
			}
		}
		if value.Validate() != nil {
			return zero, fault(f.InvalidState)
		}
		if err = persistExecutionMountMaterial(ctx, x, value); err != nil {
			return zero, err
		}
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return value.Clone(), nil
}

func (p *ExecutionCapture) VerifyExecutionMountMaterialInTx(ctx context.Context, tx f.Tx, expected mc.ExecutionMountSet) error {
	if err := p.valid(ctx, expected.Request); err != nil {
		return err
	}
	if expected.Validate() != nil || len(expected.Mounts) == 0 {
		return fault(f.InvalidArgument)
	}
	expected = expected.Clone()
	owner, ok := p.owner.(mc.ExecutionMountMaterialAuthority)
	if !ok || nilPort(owner) {
		return fault(f.DependencyUnbound)
	}
	x, err := p.executor(ctx, tx, mountCaptureLocks(expected.Request))
	if err != nil {
		return err
	}
	if err = owner.RequireMountMaterialInTx(ctx, tx, expected.Clone()); ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	// Historical integrity is deliberately independent of current Agent,
	// definition and Runner metadata. The exact input is proved by Execution.
	return verifyExecutionMountMaterial(ctx, x, expected)
}

var _ mc.ExecutionMountCaptureProvider = (*ExecutionCapture)(nil)
var _ mc.ExecutionMountMaterialVerifier = (*ExecutionCapture)(nil)
