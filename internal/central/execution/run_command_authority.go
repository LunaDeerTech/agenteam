package execution

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// NewRunCommandToolCallDriver only captures the explicit command profile. The
// original read/write constructors remain separate; capturing is not approval.
func NewRunCommandToolCallDriver(store Store, a *Authority, deps ToolCallDependencies) (*ToolCallDriver, error) {
	if nilPort(deps.CurrentEnvironment) {
		return nil, fault(f.DependencyUnbound)
	}
	return newProfileToolCallDriver(store, a, deps, tc.RunCommandRegistration().Binding)
}

type runCommandCallPlan struct{ call *toolCallPlan }

func (p *runCommandCallPlan) RequiredLocks() []f.LockRequest {
	if p == nil || p.call == nil {
		return nil
	}
	return p.call.RequiredLocks()
}
func (p *runCommandCallPlan) Binding() tc.ToolCallBinding {
	if p == nil || p.call == nil {
		return tc.ToolCallBinding{}
	}
	return p.call.binding
}
func (d *ToolCallDriver) DiscoverRunCommandCall(ctx context.Context, e i.ExecutionID) (tc.RunCommandCallPlan, error) {
	return d.discoverRunCommandCall(ctx, e, nil)
}
func (d *ToolCallDriver) DiscoverRunCommandCallFor(ctx context.Context, id tc.ToolCallIdentity) (tc.RunCommandCallPlan, error) {
	if id.Validate() != nil {
		return nil, invalid()
	}
	return d.discoverRunCommandCall(ctx, id.ExecutionID, &id)
}
func (d *ToolCallDriver) discoverRunCommandCall(ctx context.Context, e i.ExecutionID, id *tc.ToolCallIdentity) (tc.RunCommandCallPlan, error) {
	if d == nil || d.driver == nil || ctx == nil || e.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !d.driver.state.acceptsRunnerBinding(tc.RunCommandRegistration().Binding) {
		return nil, fault(f.Forbidden)
	}
	round := ""
	if id != nil {
		round = id.RoundID
	}
	stored, err := loadDirectTextRound(ctx, d.driver.state.store, e, round)
	if err != nil {
		return nil, err
	}
	binding, err := toolBinding(stored)
	if err != nil {
		return nil, err
	}
	if id != nil && !id.Matches(binding) {
		return nil, fault(f.Forbidden)
	}
	if binding.RunnerBinding != tc.RunCommandRegistration().Binding {
		return nil, fault(f.Forbidden)
	}
	plan, err := d.DiscoverToolCall(ctx, binding)
	if err != nil {
		return nil, err
	}
	return &runCommandCallPlan{call: plan.(*toolCallPlan)}, nil
}
func (d *ToolCallDriver) WithRunCommandCallInTx(ctx context.Context, tx f.Tx, plan tc.RunCommandCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	if d == nil || d.driver == nil || ctx == nil || consume == nil {
		return invalid()
	}
	p, ok := plan.(*runCommandCallPlan)
	if !ok || p == nil || p.call == nil || p.call.owner != d || p.call.binding.RunnerBinding != tc.RunCommandRegistration().Binding {
		return fault(f.Forbidden)
	}
	return d.withToolCallInTx(ctx, tx, p.call.binding, p.call, consume)
}

// Admission is deliberately separate from observing the original durable call.
// Only Resolve/final Claim invoke it, inside the real With callback. A changed
// environment or approval policy must not erase an already committed result.
func (d *ToolCallDriver) RequireRunCommandAdmissionInTx(ctx context.Context, tx f.Tx, plan tc.RunCommandCallPlan) error {
	if d == nil || d.driver == nil || ctx == nil {
		return invalid()
	}
	p, ok := plan.(*runCommandCallPlan)
	if !ok || p == nil || p.call == nil || p.call.owner != d {
		return fault(f.Forbidden)
	}
	w, err := d.driver.state.authority.commandWitness(ctx, tx)
	if err != nil {
		return err
	}
	if w.driver != d || w.plan != p.call || !sameToolCallBinding(w.binding, p.call.binding) {
		return fault(f.Forbidden)
	}
	if w.approval != ac.ApprovalAllow {
		s := d.driver.state
		if s.multi == nil || nilPort(s.multi.approvals) {
			return fault(f.CapabilityUnsupported)
		}
		if err = s.multi.approvals.RequireApprovedInTx(ctx, tx, w.binding); err != nil {
			return portError(err)
		}
	}
	provider := d.driver.state.tools.currentEnvironment
	if nilPort(provider) {
		return fault(f.DependencyUnbound)
	}
	if err = provider.RequireEmptyInTx(ctx, tx, w.request); err != nil {
		return portError(err)
	}
	// A synchronous participant cannot retire the original owner then publish
	// admission. Recheck the same live witness after its actual return.
	after, err := d.driver.state.authority.commandWitness(ctx, tx)
	if err != nil {
		return err
	}
	if after != w {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

type commandCurrentKey struct{}
type commandCurrentWitness struct {
	driver   *ToolCallDriver
	plan     *toolCallPlan
	tx       f.Tx
	binding  tc.ToolCallBinding
	request  pvc.CurrentProcessEnvironmentRequest
	facts    pvc.CurrentProcessEnvironmentFacts
	approval ac.ApprovalPolicy
	active   atomic.Bool
}

func (w *commandCurrentWitness) retire() { w.active.Store(false) }
func newCommandCurrentWitness(d *ToolCallDriver, tx f.Tx, p *toolCallPlan, agent ac.AgentConfig) (*commandCurrentWitness, error) {
	if d == nil || p == nil || p.owner != d || agent.Validate() != nil || p.binding.RunnerBinding != tc.RunCommandRegistration().Binding {
		return nil, fault(f.Forbidden)
	}
	request, err := commandEnvironmentRequest(p.binding)
	if err != nil {
		return nil, err
	}
	v := agent.Fields()
	if v.Core.ProjectID != p.binding.ProjectID || v.Core.ID != p.binding.AgentID {
		return nil, fault(f.Forbidden)
	}
	facts := pvc.CurrentProcessEnvironmentFacts{AgentVersion: v.Core.Version, AllowedSecretVariableIDs: v.AllowedSecretVariableIDs}.Clone()
	if facts.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	return &commandCurrentWitness{driver: d, plan: p, tx: tx, binding: p.binding, request: request, facts: facts, approval: v.Core.ApprovalPolicy}, nil
}
func (a *Authority) commandWitness(ctx context.Context, tx f.Tx) (*commandCurrentWitness, error) {
	if ctx == nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w, ok := ctx.Value(commandCurrentKey{}).(*commandCurrentWitness)
	if !ok || w == nil || !w.active.Load() || w.driver == nil || w.driver.driver == nil || w.plan == nil || w.tx != tx || w.plan.owner != w.driver || !sameToolCallBinding(w.binding, w.plan.binding) {
		return nil, fault(f.Forbidden)
	}
	s := w.driver.driver.state
	if a == nil || a.state == nil || s.authority.state != a.state || s.store != a.state.store || !s.acceptsRunnerBinding(tc.RunCommandRegistration().Binding) {
		return nil, fault(f.Forbidden)
	}
	request, err := commandEnvironmentRequest(w.binding)
	if err != nil || request != w.request {
		return nil, fault(f.Forbidden)
	}
	if _, err = s.store.InTx(tx); err != nil {
		return nil, portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.plan.locks); err != nil {
		return nil, portError(err)
	}
	if !w.active.Load() {
		return nil, fault(f.Forbidden)
	}
	return w, ctx.Err()
}
func (a *Authority) RequireCurrentProcessEnvironmentInTx(ctx context.Context, tx f.Tx, request pvc.CurrentProcessEnvironmentRequest) (pvc.CurrentProcessEnvironmentFacts, error) {
	var zero pvc.CurrentProcessEnvironmentFacts
	if request.Validate() != nil {
		return zero, invalid()
	}
	w, err := a.commandWitness(ctx, tx)
	if err != nil {
		return zero, err
	}
	if request != w.request {
		return zero, fault(f.Forbidden)
	}
	facts := w.facts.Clone()
	if !w.active.Load() {
		return zero, fault(f.Forbidden)
	}
	return facts, ctx.Err()
}

func commandEnvironmentRequest(b tc.ToolCallBinding) (pvc.CurrentProcessEnvironmentRequest, error) {
	if b.Validate() != nil || b.RunnerBinding != tc.RunCommandRegistration().Binding {
		return pvc.CurrentProcessEnvironmentRequest{}, fault(f.Forbidden)
	}
	// ToolCallBinding and ToolInputReference intentionally redact MarshalJSON.
	// Use an explicit versioned scalar projection of EVERY original field.
	a := b.Actor.Details()
	raw, err := json.Marshal([]string{"command-current-environment-v1", string(a.Kind), a.UserID, a.SessionID, a.ProjectID, a.AgentID, a.ExecutionID, string(a.ServiceName), a.CauseRef,
		b.ProjectID.String(), b.AgentID.String(), b.ExecutionID.String(), b.RoundID, b.SnapshotID, b.InputBindingID, b.LogicalCallID.String(), b.InvocationID.String(), b.CallID, b.ModelVisibleName,
		b.Spec.ToolID.String(), strconv.FormatInt(int64(b.Spec.SpecRevision), 10), b.Binding.HandlerID, strconv.FormatInt(int64(b.Binding.ContractRevision), 10), b.RunnerBinding.OperationName, b.RunnerBinding.OperationRevision, strconv.FormatUint(uint64(b.RunnerBinding.ProtocolMajor), 10),
		b.Input.PayloadID, string(b.Input.SHA256), strconv.FormatInt(int64(b.Input.Bytes), 10), string(b.CanonicalArguments)})
	if err != nil {
		return pvc.CurrentProcessEnvironmentRequest{}, err
	}
	return pvc.CurrentProcessEnvironmentRequest{ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, CallBindingDigest: c.TriggerInputDigest(raw)}, nil
}

var _ tc.RunCommandExecutionAuthority = (*ToolCallDriver)(nil)
var _ pvc.CurrentProcessEnvironmentAuthority = (*Authority)(nil)
