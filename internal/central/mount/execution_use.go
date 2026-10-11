package mount

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// ExecutionUse tracks live consumers of immutable preparation material. Only
// the Execution owner can prove the original running/terminal transaction and
// actual consumer join. This provider performs no independent transaction.
type ExecutionUse struct {
	store Store
	owner mc.ExecutionMountUseAuthority
}

func NewExecutionUse(store Store, owner mc.ExecutionMountUseAuthority) (*ExecutionUse, error) {
	if nilPort(store) || nilPort(owner) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ExecutionUse{store: store, owner: owner}, nil
}

func (p *ExecutionUse) valid(ctx context.Context, mounts mc.ExecutionMountSet) error {
	if ctx == nil || mounts.Validate() != nil || len(mounts.Mounts) == 0 {
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

func (p *ExecutionUse) executor(ctx context.Context, tx f.Tx, mounts mc.ExecutionMountSet) (postgres.SQLExecutor, error) {
	x, err := p.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, mountCaptureLocks(mounts.Request)); err != nil {
		return nil, portError(err)
	}
	return x, nil
}

func useAuthorityResult(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	return nil
}

// Preparing recovery still needs only the historical material, not a live use.
func (p *ExecutionUse) VerifyExecutionMountMaterialInTx(ctx context.Context, tx f.Tx, mounts mc.ExecutionMountSet) error {
	if err := p.valid(ctx, mounts); err != nil {
		return err
	}
	mounts = mounts.Clone()
	x, err := p.executor(ctx, tx, mounts)
	if err != nil {
		return err
	}
	if err = useAuthorityResult(ctx, p.owner.RequireMountMaterialInTx(ctx, tx, mounts.Clone())); err != nil {
		return err
	}
	return verifyExecutionMountMaterial(ctx, x, mounts)
}

func (p *ExecutionUse) AcquireExecutionMountUseInTx(ctx context.Context, tx f.Tx, use mc.ExecutionMountUse) error {
	if err := p.valid(ctx, use.Mounts); err != nil {
		return err
	}
	if use.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	use = use.Clone()
	x, err := p.executor(ctx, tx, use.Mounts)
	if err != nil {
		return err
	}
	// The private owner proves that this exact Snapshot and running row have
	// already been written in this transaction. IDs alone are not authority.
	if err = useAuthorityResult(ctx, p.owner.RequireExecutionMountUseAcquireInTx(ctx, tx, use.Clone())); err != nil {
		return err
	}
	digest, err := verifyExecutionUseMaterial(ctx, x, use)
	if err != nil {
		return err
	}
	return acquireExecutionMountUse(ctx, x, use, digest)
}

func (p *ExecutionUse) VerifyExecutionMountUseInTx(ctx context.Context, tx f.Tx, use mc.ExecutionMountUse, retired *mc.ExecutionMountRetirement) error {
	if err := p.valid(ctx, use.Mounts); err != nil {
		return err
	}
	if use.Validate() != nil || retired != nil && retired.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	use = use.Clone()
	var expected *mc.ExecutionMountRetirement
	if retired != nil {
		copy := *retired
		expected = &copy
	}
	x, err := p.executor(ctx, tx, use.Mounts)
	if err != nil {
		return err
	}
	var ownerExpected *mc.ExecutionMountRetirement
	if expected != nil {
		copy := *expected
		ownerExpected = &copy
	}
	if err = useAuthorityResult(ctx, p.owner.RequireExecutionMountUseVerifyInTx(ctx, tx, use.Clone(), ownerExpected)); err != nil {
		return err
	}
	digest, err := verifyExecutionUseMaterial(ctx, x, use)
	if err != nil {
		return err
	}
	return verifyExecutionMountUse(ctx, x, use, digest, expected)
}

func (p *ExecutionUse) RetireExecutionMountUseInTx(ctx context.Context, tx f.Tx, use mc.ExecutionMountUse, retired mc.ExecutionMountRetirement) error {
	if err := p.valid(ctx, use.Mounts); err != nil {
		return err
	}
	if use.Validate() != nil || retired.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	use = use.Clone()
	x, err := p.executor(ctx, tx, use.Mounts)
	if err != nil {
		return err
	}
	if err = useAuthorityResult(ctx, p.owner.RequireExecutionMountUseRetireInTx(ctx, tx, use.Clone(), retired)); err != nil {
		return err
	}
	digest, err := verifyExecutionUseMaterial(ctx, x, use)
	if err != nil {
		return err
	}
	// The owner proves actual join and the matching terminal row/event. The
	// caller's original transaction commits or rolls back this append too.
	return retireExecutionMountUse(ctx, x, use, digest, retired)
}

var _ mc.ExecutionMountUseProvider = (*ExecutionUse)(nil)
