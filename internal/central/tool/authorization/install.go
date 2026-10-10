// Package authorization owns the fixed Tool authorization decision. Runtime
// owns operations and dispatch; Execution and Registry remain fact providers.
package authorization

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// ExecutionAuthority uses the exact plan issued by DiscoverToolCall. The policy
// is read from current Agent configuration in this same Store/Tx after the
// original call's current Capability intersected with Execution Policy passes.
// Neither a caller DTO nor immutable Snapshot policy is a current grant.
type ExecutionAuthority interface {
	tc.OperationExecutionAuthority
	ToolApprovalPolicyInTx(context.Context, f.Tx, tc.ToolCallBinding, tc.ToolCallPlan) (ac.ApprovalPolicy, error)
}

// RegistryAuthority proves the selected immutable spec AND its current actual
// code binding. It uses this Store's live caller Tx, requires the pre-collected
// Registry SH and ToolSpec SH locks and never adds locks or opens a transaction.
// A persisted registration or nonnil Backend alone does not satisfy this port.
type RegistryAuthority interface {
	RequireCurrentBuiltinInTx(context.Context, f.Tx, tc.SpecRef, tc.BuiltinBinding, tc.ScopeResolverID, tc.RiskClassifierID) error
}

type Store interface {
	InTx(f.Tx) (postgres.SQLExecutor, error)
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

type InstallInput struct {
	Binding     tc.ToolCallBinding
	Call        builtin.SkillInstallCall
	Fingerprint f.Digest
}

func (v InstallInput) Validate() error {
	if err := v.Binding.Validate(); err != nil {
		return err
	}
	if v.Fingerprint.Validate() != nil || v.Call.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	d, err := v.Call.Details()
	if err != nil {
		return err
	}
	a, err := v.Call.Actor()
	if err != nil || !a.Equal(v.Binding.Actor) || d.ProjectID != v.Binding.ProjectID || d.AgentID != v.Binding.AgentID || d.ExecutionID != v.Binding.ExecutionID || d.Spec != v.Binding.Spec || d.Binding != v.Binding.Binding {
		return fail(f.InvalidArgument)
	}
	return nil
}

type Service struct{ data func() *state }
type state struct {
	store      Store
	executions ExecutionAuthority
	registry   RegistryAuthority
}

func New(store Store, executions ExecutionAuthority, registrations RegistryAuthority) (*Service, error) {
	if nilPort(store) || nilPort(executions) || nilPort(registrations) {
		return nil, fail(f.DependencyUnbound)
	}
	d := &state{store, executions, registrations}
	return &Service{data: func() *state { return d }}, nil
}

type InstallPlan struct{ data func() planData }
type planData struct {
	issuer    *state
	input     InstallInput
	execution tc.ToolCallPlan
	locks     []f.LockRequest
}

func (p InstallPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}

func (s *Service) DiscoverInstall(ctx context.Context, input InstallInput) (InstallPlan, error) {
	if err := contextError(ctx); err != nil {
		return InstallPlan{}, err
	}
	if err := input.Validate(); err != nil {
		return InstallPlan{}, err
	}
	if s == nil || s.data == nil {
		return InstallPlan{}, fail(f.DependencyUnbound)
	}
	d := s.data()
	execution, err := d.executions.DiscoverToolCall(ctx, input.Binding)
	if err != nil {
		return InstallPlan{}, portError(err)
	}
	if nilPort(execution) {
		return InstallPlan{}, fail(f.DependencyUnbound)
	}
	b := input.Binding
	p, _ := f.ProjectLock(b.ProjectID.String())
	a, _ := f.AgentLock(b.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, b.ExecutionID.String())
	t, _ := f.AggregateLock(f.ToolSpecAggregate, b.Spec.ToolID.String())
	locks, err := normalize(append(execution.RequiredLocks(), registry.RegistryLock(f.Shared),
		f.LockRequest{Key: p, Mode: f.Shared}, f.LockRequest{Key: a, Mode: f.Shared},
		f.LockRequest{Key: e, Mode: f.Exclusive}, f.LockRequest{Key: t, Mode: f.Shared}))
	if err != nil {
		return InstallPlan{}, err
	}
	if err = ctx.Err(); err != nil {
		return InstallPlan{}, err
	}
	plan := planData{d, input, execution, locks}
	return InstallPlan{data: func() planData { return plan }}, nil
}

// AuthorizeInstallInTx implements only the publish-only v1 direct branch.
// nil is the current decision for this transaction, not a reusable capability.
// Existing Approval matches cannot broaden base permission; for the fixed empty
// risk set all three valid policies take the same direct branch. It neither
// asserts that no Approval exists nor fabricates an Approval/Inbox reference.
// A future needs_approval branch must bind its real workflow; it is not denied.
func (s *Service) AuthorizeInstallInTx(ctx context.Context, tx f.Tx, input InstallInput, plan InstallPlan) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := input.Validate(); err != nil {
		return err
	}
	if s == nil || s.data == nil {
		return fail(f.DependencyUnbound)
	}
	if plan.data == nil {
		return fail(f.InvalidArgument)
	}
	d, p := s.data(), plan.data()
	if p.issuer != d || !sameInput(p.input, input) {
		return fail(f.InvalidArgument)
	}
	if _, err := d.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := d.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	if err := d.executions.RequireToolCallInTx(ctx, tx, input.Binding, p.execution); err != nil {
		return portError(err)
	}
	if err := d.registry.RequireCurrentBuiltinInTx(ctx, tx, input.Binding.Spec, input.Binding.Binding, builtin.SkillInstallScopeID, builtin.SkillInstallRiskID); err != nil {
		return portError(err)
	}
	// These are the actual code-bound resolver/classifier, not request risks.
	scope, err := input.Call.CurrentScope(ctx)
	if err != nil {
		return portError(err)
	}
	if scope.ProjectID != input.Binding.ProjectID || scope.AgentID != input.Binding.AgentID || scope.ExecutionID != input.Binding.ExecutionID || scope.Action != "skill.install.create" {
		return fail(f.Forbidden)
	}
	risks, err := input.Call.Risks(ctx)
	if err != nil {
		return portError(err)
	}
	policy, err := d.executions.ToolApprovalPolicyInTx(ctx, tx, input.Binding, p.execution)
	if err != nil {
		return portError(err)
	}
	if policy.Validate() != nil {
		return fail(f.DependencyUnavailable)
	}
	if len(risks) != 0 {
		// This slice has no Approval writer/model. Never convert needs_approval
		// to deny or manufacture waiting_for_approval without a durable owner.
		return fail(f.DependencyUnbound)
	}
	return ctx.Err()
}

func sameInput(a, b InstallInput) bool {
	x, y := a.Binding, b.Binding
	ad, ae := a.Call.Details()
	bd, be := b.Call.Details()
	return ae == nil && be == nil && ad == bd && a.Fingerprint == b.Fingerprint &&
		x.Actor.Equal(y.Actor) && x.ProjectID == y.ProjectID && x.AgentID == y.AgentID && x.ExecutionID == y.ExecutionID &&
		x.RoundID == y.RoundID && x.SnapshotID == y.SnapshotID && x.InputBindingID == y.InputBindingID &&
		x.LogicalCallID == y.LogicalCallID && x.InvocationID == y.InvocationID && x.CallID == y.CallID &&
		x.ModelVisibleName == y.ModelVisibleName && x.Spec == y.Spec && x.Binding == y.Binding && x.Input == y.Input && x.CanonicalArguments == y.CanonicalArguments
}

func normalize(locks []f.LockRequest) ([]f.LockRequest, error) {
	locks = slices.Clone(locks)
	for _, l := range locks {
		if l.Key.Validate() != nil || !l.Mode.Valid() {
			return nil, fail(f.InvalidArgument)
		}
	}
	slices.SortFunc(locks, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	n := 0
	for _, l := range locks {
		if n > 0 && f.CompareLockKeys(locks[n-1].Key, l.Key) == 0 {
			if l.Mode == f.Exclusive {
				locks[n-1].Mode = f.Exclusive
			}
			continue
		}
		locks[n] = l
		n++
	}
	return locks[:n], nil
}
func fail(code f.Code) error { return f.NewFault(code, f.NotStarted) }
func contextError(ctx context.Context) error {
	if ctx == nil {
		return fail(f.InvalidArgument)
	}
	return ctx.Err()
}
func portError(err error) error {
	if err == nil {
		return nil
	}
	var fault *f.Fault
	if errors.As(err, &fault) {
		return fault
	}
	return f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(err)
}
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func (InstallInput) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "tool_authorization_input") }
func (InstallInput) LogValue() slog.Value         { return slog.StringValue("tool_authorization_input") }
func (InstallInput) MarshalJSON() ([]byte, error) { return []byte(`"tool_authorization_input"`), nil }
func (InstallPlan) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "tool_authorization_plan") }
func (InstallPlan) LogValue() slog.Value          { return slog.StringValue("tool_authorization_plan") }
func (InstallPlan) MarshalJSON() ([]byte, error)  { return []byte(`"tool_authorization_plan"`), nil }
