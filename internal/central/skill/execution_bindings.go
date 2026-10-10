package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// ExecutionBindings owns only same-transaction Skill metadata and protection.
// It performs no Object I/O, creates no worker and does not commit a Snapshot.
type ExecutionBindings struct {
	store      Store
	executions sc.SkillCaptureAuthority
}

func NewExecutionBindings(authority *Authority, executions sc.SkillCaptureAuthority) (*ExecutionBindings, error) {
	if authority.state() == nil || nilPort(executions) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ExecutionBindings{store: authority.state().store, executions: executions}, nil
}

type initialBindingsPlan struct {
	owner   *ExecutionBindings
	request sc.SkillCaptureRequest
	attempt f.Digest
	source  skillCaptureSource
	digest  f.Digest
	locks   []f.LockRequest
}

func (p *initialBindingsPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*initialBindingsPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_initial_bindings_plan")
}
func (*initialBindingsPlan) LogValue() slog.Value {
	return slog.StringValue("skill_initial_bindings_plan")
}

func captureError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return portError(err)
}
func captureDigest(v any) (f.Digest, []byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", nil, unavailable(err)
	}
	h := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(h[:])), raw, nil
}
func captureLocks(r sc.SkillCaptureRequest, skill *sc.SkillID) ([]f.LockRequest, error) {
	agent, _ := f.AgentLock(r.AgentID.String())
	execution, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	schedule, _ := f.ProjectScheduleLock(r.ProjectID.String())
	locks := []f.LockRequest{projectLock(r.ProjectID, f.Shared), {Key: schedule, Mode: f.Exclusive}, {Key: agent, Mode: f.Shared}, {Key: execution, Mode: f.Exclusive}}
	if skill != nil {
		locks = append(locks, skillLock(*skill, f.Shared))
	}
	return ob.NormalizeLocks(locks)
}
func (p *ExecutionBindings) valid(ctx context.Context, r sc.SkillCaptureRequest) error {
	if ctx == nil || r.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || nilPort(p.store) || nilPort(p.executions) {
		return fault(f.DependencyUnbound)
	}
	return nil
}
func (p *ExecutionBindings) scope(ctx context.Context, tx f.Tx, r sc.SkillCaptureRequest, locks []f.LockRequest, final bool) (postgres.SQLExecutor, sc.SkillCaptureScope, error) {
	var zero sc.SkillCaptureScope
	x, err := p.store.InTx(tx)
	if err != nil {
		return nil, zero, captureError(err)
	}
	if nilPort(x) {
		return nil, zero, fault(f.DependencyUnbound)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, zero, captureError(err)
	}
	var scope sc.SkillCaptureScope
	if final {
		scope, err = p.executions.RequireSkillCaptureInTx(ctx, tx, r)
	} else {
		scope, err = p.executions.RequireSkillCaptureDiscoveryInTx(ctx, tx, r)
	}
	if ctx.Err() != nil {
		return nil, zero, ctx.Err()
	}
	if err != nil {
		return nil, zero, captureError(err)
	}
	if scope.Project.Validate() != nil || scope.Project.ID != r.ProjectID || scope.Project.Lifecycle != pc.Active || scope.AttemptBinding.Validate() != nil {
		return nil, zero, fault(f.Forbidden)
	}
	return x, scope, nil
}

func (p *ExecutionBindings) DiscoverInitialBindings(ctx context.Context, r sc.SkillCaptureRequest) (sc.InitialBindingsPlan, error) {
	if err := p.valid(ctx, r); err != nil {
		return nil, err
	}
	base, err := captureLocks(r, nil)
	if err != nil {
		return nil, captureError(err)
	}
	key, err := f.NewID[f.TransactionAttempt]()
	if err != nil {
		return nil, unavailable(err)
	}
	cause, err := f.NewJobCause("skill-execution-capture", r.ExecutionID.String(), key.String())
	if err != nil {
		return nil, err
	}
	var head *agentInitializationRecord
	var attempt f.Digest
	// First observe only the initialized assignment identity under its real
	// owner gate. Release this Tx before adding the discovered Skill lock.
	result := p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := p.store.AcquireAll(ctx, tx, base); e != nil {
			return captureError(e)
		}
		x, scope, e := p.scope(ctx, tx, r, base, false)
		if e != nil {
			return e
		}
		attempt = scope.AttemptBinding
		head, e = loadAgentInitialization(ctx, x, r.ProjectID, r.AgentID)
		if e != nil {
			return e
		}
		if head == nil {
			return fault(f.DependencyUnbound)
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if head == nil {
		return nil, unavailable(nil)
	}
	locks, err := captureLocks(r, &head.skill)
	if err != nil {
		return nil, captureError(err)
	}
	var source skillCaptureSource
	result = p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := p.store.AcquireAll(ctx, tx, locks); e != nil {
			return captureError(e)
		}
		x, scope, e := p.scope(ctx, tx, r, locks, false)
		if e != nil {
			return e
		}
		if scope.AttemptBinding != attempt {
			return fault(f.ConfirmationStale)
		}
		source, e = loadSkillCaptureSource(ctx, x, r, head.skill)
		if e != nil {
			return e
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	d, _, err := captureDigest(source)
	if err != nil {
		return nil, err
	}
	return &initialBindingsPlan{p, r, attempt, source, d, locks}, nil
}

func (p *ExecutionBindings) ResolveInitialBindingsInTx(ctx context.Context, tx f.Tx, r sc.SkillCaptureRequest, plan sc.InitialBindingsPlan) (sc.InitialSkillBindings, error) {
	var zero sc.InitialSkillBindings
	if err := p.valid(ctx, r); err != nil {
		return zero, err
	}
	selected, ok := plan.(*initialBindingsPlan)
	if !ok || selected == nil || selected.owner != p || selected.request != r {
		return zero, fault(f.Forbidden)
	}
	x, scope, err := p.scope(ctx, tx, r, selected.locks, true)
	if err != nil {
		return zero, err
	}
	if scope.AttemptBinding != selected.attempt {
		return zero, fault(f.ConfirmationStale)
	}
	source, err := loadSkillCaptureSource(ctx, x, r, selected.source.InitialSkillID)
	if err != nil {
		return zero, captureError(err)
	}
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	d, _, err := captureDigest(source)
	if err != nil {
		return zero, err
	}
	if d != selected.digest {
		return zero, fault(f.ConfirmationStale)
	}
	if err = saveSkillCapture(ctx, x, selected); err != nil {
		return zero, captureError(err)
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return source.Result.Clone(), nil
}

var _ sc.InitialBindingsProvider = (*ExecutionBindings)(nil)
