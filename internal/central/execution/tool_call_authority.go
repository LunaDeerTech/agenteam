package execution

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"sync/atomic"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type toolCallPlan struct {
	owner           *ToolCallDriver
	binding         tc.ToolCallBinding
	locks           []f.LockRequest
	snapshot, round f.Digest
}

func (p *toolCallPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type toolCurrentKey struct{}
type toolCurrentWitness struct {
	driver  *ToolCallDriver
	tx      f.Tx
	request ac.ExecutionConfigurationRequest
	locks   []f.LockRequest
	active  atomic.Bool
}

// ReadToolCall returns a safe immutable projection after real current checks.
// Protected bytes have no public getter; this is not a bearer dispatch ticket.
func (d *ToolCallDriver) ReadToolCall(ctx context.Context, e i.ExecutionID) (tc.ToolCallBinding, error) {
	if d == nil || d.driver == nil || ctx == nil || e.Validate() != nil {
		return tc.ToolCallBinding{}, invalid()
	}
	s := d.driver.state
	stored, err := loadDirectText(ctx, s.store, e)
	if err != nil {
		return tc.ToolCallBinding{}, err
	}
	b, err := toolBinding(stored)
	if err != nil {
		return tc.ToolCallBinding{}, err
	}
	plan, err := d.DiscoverToolCall(ctx, b)
	if err != nil {
		return tc.ToolCallBinding{}, err
	}
	cause, _ := f.NewRecoveryCause("execution.tool-call.read", e.String(), b.Input.PayloadID)
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return portError(err)
		}
		return d.RequireToolCallInTx(ctx, tx, b, plan)
	})
	if err := commitError(result); err != nil {
		return tc.ToolCallBinding{}, err
	}
	return b, nil
}

// Discovery is canonical fact planning, not a runtime binding. The returned
// private plan can only be consumed by this driver in the same Store, after
// the complete union is held. Actual Operation creation remains unbound here.
func (d *ToolCallDriver) DiscoverToolCall(ctx context.Context, b tc.ToolCallBinding) (tc.ToolCallPlan, error) {
	if d == nil || d.driver == nil || ctx == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.Validate() != nil || b.RunnerBinding.Validate() != nil {
		return nil, invalid()
	}
	stored, err := loadDirectText(ctx, d.driver.state.store, b.ExecutionID)
	if err != nil {
		return nil, err
	}
	expected, err := toolBinding(stored)
	if err != nil {
		return nil, err
	}
	if !sameToolCallBinding(expected, b) {
		return nil, fault(f.Forbidden)
	}
	input := stored.snapshot.Fields().Context.Input()
	locks := toolFactsRequest(input, input.Fields().Agent).RequiredLocks()
	return &toolCallPlan{owner: d, binding: b, locks: locks, snapshot: stored.snapshot.Digest(), round: stored.round.Digest()}, ctx.Err()
}

func (d *ToolCallDriver) RequireToolCallInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, plan tc.ToolCallPlan) error {
	return d.withToolCallInTx(ctx, tx, b, plan, nil)
}

// The callback remains inside the original current-Agent witness lifetime.
// Returning from Require and then lending its context would expire that proof.
func (d *ToolCallDriver) withToolCallInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, plan tc.ToolCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if d == nil || d.driver == nil || ctx == nil || b.Validate() != nil {
		return invalid()
	}
	p, ok := plan.(*toolCallPlan)
	if !ok || p == nil || p.owner != d || !sameToolCallBinding(p.binding, b) {
		return fault(f.Forbidden)
	}
	s := d.driver.state
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	row, err := loadExecution(ctx, x, b.ExecutionID)
	if err != nil {
		return err
	}
	stored, err := loadDirectText(ctx, x, b.ExecutionID)
	if err != nil {
		return err
	}
	if row == nil || stored == nil || stored.toolPending == nil || row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil || row.summary.SnapshotID == nil || *row.summary.SnapshotID != stored.snapshot.Fields().ID || row.summary.Version < stored.toolPending.version || stored.snapshot.Digest() != p.snapshot || stored.round.Digest() != p.round {
		return fault(f.ConfirmationStale)
	}
	expected, err := toolBinding(stored)
	if err != nil || !sameToolCallBinding(expected, b) {
		return fault(f.Forbidden)
	}
	input, err := loadPreparationInput(ctx, x, b.ExecutionID)
	if err != nil {
		return err
	}
	if input == nil || input.input.Digest() != stored.snapshot.Fields().Context.InputDigest() || !input.input.Fields().Request.Equal(c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}) {
		return fault(f.ConfirmationStale)
	}
	if _, err = s.deps.Projects.RequirePreparingProjectInTx(ctx, tx, b.ProjectID); err != nil {
		return portError(err)
	}
	request := ac.ExecutionConfigurationRequest{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
	w := &toolCurrentWitness{driver: d, tx: tx, request: request, locks: p.RequiredLocks()}
	w.active.Store(true)
	defer w.active.Store(false)
	authorized := context.WithValue(ctx, toolCurrentKey{}, w)
	agent, err := s.deps.Agents.ReadExecutionConfigurationInTx(authorized, tx, request)
	if err != nil {
		return portError(err)
	}
	fields := input.input.Fields()
	if !slices.Contains(agent.Fields().AllowedToolIDs, b.Spec.ToolID) || slices.Contains(fields.Request.Launch.Policy.DeniedToolIDs, b.Spec.ToolID) || len(fields.Request.Launch.Policy.AllowedResourceConstraints) != 0 {
		return fault(f.Forbidden)
	}
	args, err := tc.DecodeReadFileToolArguments(ctx, stored.toolPending.original)
	if err != nil {
		return err
	}
	if !slices.Contains(agent.Fields().AllowedMountIDs, args.MountID) {
		return fault(f.Forbidden)
	}
	found := false
	for _, m := range fields.Mounts.Mounts {
		if m.ID == args.MountID {
			found = true
		}
	}
	if !found {
		return fault(f.Forbidden)
	}
	if err = s.tools.metadata.RequireExecutionToolFactsInTx(ctx, tx, toolFactsRequest(input.input, agent)); err != nil {
		return portError(err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if consume != nil {
		// Never lend repository-owned storage: mutation by the consumer cannot
		// replace the original protected bytes, and the loan expires on return.
		borrowed := bytes.Clone(stored.toolPending.original)
		defer clear(borrowed)
		if err = consume(authorized, expected, borrowed); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (a *Authority) requireToolCurrentConfiguration(ctx context.Context, tx f.Tx, request ac.ExecutionConfigurationRequest) error {
	w, ok := ctx.Value(toolCurrentKey{}).(*toolCurrentWitness)
	if !ok || w == nil || !w.active.Load() || w.driver == nil || w.driver.driver == nil || w.tx != tx || !sameToolCurrentRequest(w.request, request) {
		return fault(f.Forbidden)
	}
	s := w.driver.driver.state
	if a == nil || a.state == nil || s.authority.state != a.state || s.store != a.state.store {
		return fault(f.Forbidden)
	}
	if _, err := s.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	if !w.active.Load() {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

var _ tc.OperationExecutionAuthority = (*ToolCallDriver)(nil)

func sameToolCallBinding(a, b tc.ToolCallBinding) bool {
	if !a.Actor.Equal(b.Actor) {
		return false
	}
	a.Actor = i.Actor{}
	b.Actor = i.Actor{}
	return reflect.DeepEqual(a, b)
}
func sameToolCurrentRequest(a, b ac.ExecutionConfigurationRequest) bool {
	if !a.Actor.Equal(b.Actor) {
		return false
	}
	a.Actor = i.Actor{}
	b.Actor = i.Actor{}
	return reflect.DeepEqual(a, b)
}
