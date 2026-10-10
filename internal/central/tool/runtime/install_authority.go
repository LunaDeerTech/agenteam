package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/authorization"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// InstallAuthority is the actual producer for Skill's consumer-owned port. It
// can be composed before Skill.Service, breaking the construction cycle without
// a mutable Bind function. Only this package's synchronous InstallExecutor can
// create its active private dispatch context, after a known committed Attempt.
type InstallAuthority struct{ data func() *installAuthorityState }
type installAuthorityState struct {
	store   Store
	guard   *object.ProcessGuard
	process oc.ProcessID
	mu      sync.Mutex
	stopped bool
	active  map[tc.OperationID]*installHandoff
}

type installHandoff struct {
	mu         sync.Mutex
	issuer     *installAuthorityState
	executor   *installExecutorData
	record     operationRecord
	binding    tc.ToolCallBinding
	call       builtin.SkillInstallCall
	input      authorization.InstallInput
	permission authorization.InstallPlan
	locks      []f.LockRequest
	attempt    tc.AttemptID
	request    f.ID[f.Request]
	cause      f.TransactionCause
	cancel     context.CancelFunc
	ctx        context.Context
	done       chan struct{}
	live       bool
	// retirement is nonnil if durable completion was not confirmed. A returned
	// backend call alone cannot make Drain/Joined claim all ownership retired.
	retirement error
	terminal   *installTerminal
}

type installHandoffKey struct{}
type installHandoffContext struct {
	issuer *installAuthorityState
	call   *installHandoff
}

// NewInstallAuthority fixes the real Store, guard and intended process before
// Object.Initialize binds that guard. Construction issues no grant and does not
// claim the process is ready. Registry binding and each dispatch recheck the
// same guard's current bound process.
func NewInstallAuthority(store Store, guard *object.ProcessGuard, expectedProcess oc.ProcessID) (*InstallAuthority, error) {
	if nilPort(store) || guard == nil || expectedProcess.Validate() != nil {
		return nil, fail(f.DependencyUnbound)
	}
	d := &installAuthorityState{store: store, guard: guard, process: expectedProcess, active: map[tc.OperationID]*installHandoff{}}
	return &InstallAuthority{data: func() *installAuthorityState { return d }}, nil
}

func (a *InstallAuthority) handoff(ctx context.Context) (*installAuthorityState, *installHandoff, error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	if a == nil || a.data == nil {
		return nil, nil, fail(f.DependencyUnbound)
	}
	d := a.data()
	witness, ok := ctx.Value(installHandoffKey{}).(installHandoffContext)
	if !ok || witness.issuer != d || witness.call == nil || witness.call.issuer != d {
		return nil, nil, fail(f.Forbidden)
	}
	if !witness.call.boundExecutor(d) || witness.call.ctx == nil {
		return nil, nil, fail(f.InvalidState)
	}
	if err := witness.call.ctx.Err(); err != nil {
		return nil, nil, err
	}
	d.mu.Lock()
	current := d.active[witness.call.record.ID] == witness.call
	d.mu.Unlock()
	if !current {
		return nil, nil, fail(f.Forbidden)
	}
	return d, witness.call, nil
}

func (h *installHandoff) matches(request sc.InstallExecutionRequest) bool {
	r := h.record
	return request.Validate() == nil && request.Actor.Equal(h.binding.Actor) && request.ProjectID == h.binding.ProjectID &&
		request.Command.Key() == r.Key && request.RequestID == h.request && request.SkillID == r.SkillID &&
		request.NormalizedName == r.Input.NormalizedName && request.PackageSHA256 == r.Input.PackageDigest &&
		request.ManifestSHA256 == r.Input.ManifestDigest && request.ByteSize == r.Input.PackageBytes
}

type installExecutionPlan struct {
	data func() installExecutionPlanData
}
type installExecutionPlanData struct {
	issuer  *installAuthorityState
	handoff *installHandoff
	request sc.InstallExecutionRequest
	locks   []f.LockRequest
	binding sc.InstallExecutionBinding
}

func (p installExecutionPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}
func (p installExecutionPlan) Binding() sc.InstallExecutionBinding {
	if p.data == nil {
		return sc.InstallExecutionBinding{}
	}
	return p.data().binding
}

func (a *InstallAuthority) DiscoverInstallExecution(ctx context.Context, request sc.InstallExecutionRequest) (sc.InstallExecutionPlan, error) {
	d, h, err := a.handoff(ctx)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.live || !h.matches(request) {
		return nil, fail(f.Forbidden)
	}
	b := sc.InstallExecutionBinding{OperationID: h.record.ID.String(), AttemptID: h.attempt.String(), ToolID: h.binding.Spec.ToolID, SpecRevision: h.binding.Spec.SpecRevision, HandlerID: h.binding.Binding.HandlerID, ContractRevision: h.binding.Binding.ContractRevision, Fingerprint: h.record.Fingerprint}
	if !b.Matches(request) {
		return nil, fail(f.InvalidState)
	}
	p := installExecutionPlanData{d, h, request, slices.Clone(h.locks), b}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return installExecutionPlan{data: func() installExecutionPlanData { return p }}, nil
}

func (a *InstallAuthority) RequireInstallExecutionInTx(ctx context.Context, tx f.Tx, request sc.InstallExecutionRequest, plan sc.InstallExecutionPlan) error {
	d, h, err := a.handoff(ctx)
	if err != nil {
		return err
	}
	p, ok := plan.(installExecutionPlan)
	if !ok || p.data == nil {
		return fail(f.InvalidArgument)
	}
	v := p.data()
	if v.issuer != d || v.handoff != h || !v.request.Equal(request) {
		return fail(f.InvalidArgument)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.live || !h.matches(request) {
		return fail(f.Forbidden)
	}
	return requireInstallHandoff(ctx, tx, d, h)
}

// The Builtin adapter calls this before the one synchronous Service invocation.
// Skill still performs its own same-Tx check during Prepare/Reserve/Publish.
func (a *InstallAuthority) RequireCurrentSkillInstall(ctx context.Context, call builtin.SkillInstallCall) error {
	d, h, err := a.handoff(ctx)
	if err != nil {
		return err
	}
	want, err := h.call.Details()
	if err != nil {
		return err
	}
	got, err := call.Details()
	actor, actorErr := call.Actor()
	if err != nil || actorErr != nil || got != want || !actor.Equal(h.binding.Actor) {
		return fail(f.Forbidden)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.live {
		return fail(f.Forbidden)
	}
	store := d.store
	result := store.WithinTx(ctx, h.cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		return requireInstallHandoff(ctx, tx, d, h)
	})
	return commitError(result)
}

func requireInstallHandoff(ctx context.Context, tx f.Tx, d *installAuthorityState, h *installHandoff) error {
	if !h.boundExecutor(d) {
		return fail(f.Forbidden)
	}
	store := d.store
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if _, err = h.executor.core.data().store.InTx(tx); err != nil {
		return portError(err)
	}
	if err = store.RequireHeldLocks(ctx, tx, h.locks); err != nil {
		return portError(err)
	}
	process, err := d.guard.CurrentProcess()
	if err != nil || process != d.process {
		return fail(f.DependencyUnavailable)
	}
	if err = h.executor.permission.AuthorizeInstallInTx(ctx, tx, h.input, h.permission); err != nil {
		return portError(err)
	}
	return requireRunningInstall(ctx, x, d, h)
}

// Stop only closes admission and signals original callers. It does not release
// a ProcessGuard, delete an Attempt, or declare the actual Service call joined.
func (a *InstallAuthority) Stop() {
	if a == nil || a.data == nil {
		return
	}
	d := a.data()
	d.mu.Lock()
	d.stopped = true
	cancels := make([]context.CancelFunc, 0, len(d.active))
	for _, h := range d.active {
		cancels = append(cancels, h.cancel)
	}
	d.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (a *InstallAuthority) Drain(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if a == nil || a.data == nil {
		return fail(f.DependencyUnbound)
	}
	d := a.data()
	d.mu.Lock()
	if !d.stopped {
		d.mu.Unlock()
		return fail(f.InvalidState)
	}
	calls := make([]*installHandoff, 0, len(d.active))
	for _, h := range d.active {
		calls = append(calls, h)
	}
	d.mu.Unlock()
	for _, h := range calls {
		select {
		case <-h.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		h.mu.Lock()
		err := h.retirement
		h.mu.Unlock()
		if err != nil {
			return err
		}
	}
	if !a.Joined() {
		return fail(f.ResourceBusy)
	}
	return ctx.Err()
}

func (a *InstallAuthority) Joined() bool {
	if a == nil || a.data == nil {
		return false
	}
	d := a.data()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopped && len(d.active) == 0
}

var _ sc.InstallExecutionAuthority = (*InstallAuthority)(nil)
var _ builtin.SkillInstallOperationAuthority = (*InstallAuthority)(nil)

func (InstallAuthority) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_install_authority")
}
func (InstallAuthority) LogValue() slog.Value         { return slog.StringValue("tool_install_authority") }
func (InstallAuthority) MarshalJSON() ([]byte, error) { return []byte(`"tool_install_authority"`), nil }
func (installExecutionPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_install_execution_plan")
}
func (installExecutionPlan) LogValue() slog.Value {
	return slog.StringValue("tool_install_execution_plan")
}
func (installExecutionPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_install_execution_plan"`), nil
}
