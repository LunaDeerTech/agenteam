package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

type installInputLoan interface {
	WithInstallToolCallInTx(context.Context, f.Tx, tc.ToolCallBinding, tc.ToolCallPlan, func(context.Context, tc.ToolCallBinding, []byte) error) error
}

// InstallDispatcher adapts the original install ledger and retained executor.
// Its local call map owns physical cancellation/join only, never publication.
type InstallDispatcher struct {
	executor  *InstallExecutor
	current   installInputLoan
	committed tc.CommittedBuiltinInputAuthority
	mu        sync.Mutex
	stopped   bool
	calls     map[tc.ToolCallIdentity]*installDispatchCall
	cold      map[tc.ToolCallIdentity]*installColdCall
}
type installDispatchCall struct {
	binding   tc.ToolCallBinding
	cancel    context.CancelFunc
	done      chan struct{}
	operation tc.OperationID
	err       error
}

func (d *InstallDispatcher) setCallError(call *installDispatchCall, err error) {
	d.mu.Lock()
	call.err = err
	d.mu.Unlock()
}

func NewInstallDispatcher(executor *InstallExecutor) (*InstallDispatcher, error) {
	if executor == nil || executor.data == nil {
		return nil, fail(f.DependencyUnbound)
	}
	x := executor.data().core.data().executions
	current, ok := x.(installInputLoan)
	if !ok {
		return nil, fail(f.DependencyUnbound)
	}
	committed, ok := x.(tc.CommittedBuiltinInputAuthority)
	if !ok {
		return nil, fail(f.DependencyUnbound)
	}
	return &InstallDispatcher{executor: executor, current: current, committed: committed, calls: map[tc.ToolCallIdentity]*installDispatchCall{}}, nil
}
func (d *InstallDispatcher) valid(b tc.ToolCallBinding) error {
	if d == nil || d.executor == nil {
		return fail(f.DependencyUnbound)
	}
	if b.Validate() != nil || b.Binding != (tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}) || b.RunnerBinding != (tc.RunnerBinding{}) || b.MCPBinding != (tc.MCPBinding{}) {
		return fail(f.Forbidden)
	}
	return nil
}
func (d *InstallDispatcher) readTx(ctx context.Context, b tc.ToolCallBinding, committed bool, extra []f.LockRequest, consume func(context.Context, f.Tx, postgres.SQLExecutor, tc.ToolCallPlan, []byte) error) error {
	if err := d.valid(b); err != nil {
		return err
	}
	state := d.executor.data().core.data()
	plan, err := state.executions.DiscoverToolCall(ctx, b)
	if err != nil {
		return err
	}
	if nilPort(plan) {
		return fail(f.DependencyUnbound)
	}
	cause, err := inputCause(b)
	if err != nil {
		return err
	}
	locks, err := baseLocks(b, cause, append(plan.RequiredLocks(), extra...))
	if err != nil {
		return err
	}
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		a := d.executor.data().source.authority.data()
		if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
			return portError(err)
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		loan := d.current.WithInstallToolCallInTx
		if committed {
			loan = d.committed.WithCommittedBuiltinToolCallInTx
		}
		calls := 0
		err = loan(ctx, tx, b, plan, func(current context.Context, actual tc.ToolCallBinding, raw []byte) error {
			calls++
			if calls != 1 || !sameRunnerCallBinding(actual, b) {
				return fail(f.Forbidden)
			}
			return consume(current, tx, x, plan, raw)
		})
		if err != nil {
			return err
		}
		if calls != 1 {
			return fail(f.Forbidden)
		}
		return ctx.Err()
	})
	return commitError(result)
}
func (d *InstallDispatcher) Execute(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerOutcome, error) {
	if err := d.valid(b); err != nil {
		return tc.RunnerOutcome{}, err
	}
	if err := contextError(ctx); err != nil {
		return tc.RunnerOutcome{}, err
	}
	run, cancel := context.WithCancel(ctx)
	call := &installDispatchCall{binding: b, cancel: cancel, done: make(chan struct{})}
	d.mu.Lock()
	if d.stopped || d.calls[b.Identity()] != nil {
		d.mu.Unlock()
		cancel()
		return tc.RunnerOutcome{}, fail(f.ResourceBusy)
	}
	d.calls[b.Identity()] = call
	d.mu.Unlock()
	defer func() { cancel(); close(call.done) }()
	var raw []byte
	err := d.readTx(run, b, false, nil, func(current context.Context, _ f.Tx, x postgres.SQLExecutor, _ tc.ToolCallPlan, body []byte) error {
		if prior, err := loadOperation(current, x, b); err != nil {
			return err
		} else if prior != nil {
			return fail(f.ResourceBusy)
		}
		raw = bytes.Clone(body)
		return nil
	})
	defer clear(raw)
	if err != nil {
		d.setCallError(call, err)
		return tc.RunnerOutcome{}, err
	}
	prepared, err := d.executor.data().core.PrepareInstall(run, b, raw)
	if err != nil {
		d.setCallError(call, err)
		return tc.RunnerOutcome{}, err
	}
	d.mu.Lock()
	call.operation = prepared.data().ID
	d.mu.Unlock()
	_, err = d.executor.ExecuteInstall(run, prepared, raw)
	if err != nil {
		// ExecuteInstall has actually returned, including its physical backend
		// and original final transaction. A new observation can settle that
		// exact finalization; it never invokes the adapter again.
		if builtinUnknown(err) {
			observe, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			out, found, observedErr := d.Lookup(observe, b)
			stop()
			if observedErr == nil && found && out.ResultAvailable {
				d.setCallError(call, nil)
				return out, nil
			}
			err = d.recordInstallRecoveryError(call, err)
		}
		d.setCallError(call, err)
		return tc.RunnerOutcome{OperationID: call.operation, Outcome: "unknown"}, err
	}
	out, found, err := d.Lookup(run, b)
	if !found && err == nil {
		err = fail(f.InvalidState)
	}
	d.setCallError(call, err)
	return out, err
}

type installObserved struct {
	record   operationRecord
	terminal installTerminal
	request  p.ID
	raw      []byte
	body     []byte
	phase    string
	code     string
	attempt  tc.AttemptID
	process  string
}

func loadInstallObserved(ctx context.Context, x postgres.SQLExecutor, b tc.ToolCallBinding) (*installObserved, error) {
	row, err := loadInstallImmutableRecord(ctx, x, b)
	if err != nil || row == nil {
		return nil, err
	}
	if !(row.Version == 1 && row.Phase == "created" || row.Version == 2 && row.Phase == "running" || row.Version == 3 && (row.Phase == "succeeded" || row.Phase == "failed")) {
		return nil, fail(f.InvalidState)
	}
	original, err := row.Input.Binding.request()
	if err != nil || !sameRunnerCallBinding(original, b) {
		return nil, fail(f.Forbidden)
	}
	out := &installObserved{record: *row, phase: row.Phase}
	if row.Version < 3 {
		return out, nil
	}
	var attempt, request, phase, active, process, outcome string
	var retired bool
	var body []byte
	var fence, ordinal int64
	err = x.QueryRow(ctx, `SELECT a.attempt_id,a.backend_request_id,a.process_id,a.fence,a.ordinal,a.phase,a.retired,a.outcome_data,o.terminal_data,o.outcome,o.active_attempt_id FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.operation_id=o.operation_id AND a.attempt_id=o.active_attempt_id WHERE o.operation_id=$1 AND a.execution_id=$2 AND a.project_id=$3 AND a.agent_id=$4`, row.ID.String(), b.ExecutionID.String(), b.ProjectID.String(), b.AgentID.String()).Scan(&attempt, &request, &process, &fence, &ordinal, &phase, &retired, &body, &out.raw, &outcome, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fail(f.InvalidState)
	}
	if err != nil {
		return nil, portError(err)
	}
	out.attempt, err = f.ParseID[tc.Attempt](attempt)
	if err != nil || active != attempt || !retired || fence != 1 || ordinal != 1 || !bytes.Equal(body, out.raw) || len(body) > 32768 {
		return nil, fail(f.InvalidState)
	}
	if _, err = f.ParseID[struct{}](process); err != nil {
		return nil, fail(f.InvalidState)
	}
	out.process = process
	out.request = p.ID(request)
	if !out.request.Valid() {
		return nil, fail(f.InvalidState)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out.terminal) != nil {
		return nil, fail(f.InvalidState)
	}
	canonical, err := encode(out.terminal)
	if err != nil || !bytes.Equal(canonical, body) {
		return nil, fail(f.InvalidState)
	}
	if out.terminal.validateOrigin(*row, b, out.attempt) != nil {
		return nil, fail(f.InvalidState)
	}
	if !out.terminal.Known {
		if phase != "unknown" || outcome != "unknown" || row.Phase != "failed" {
			return nil, fail(f.InvalidState)
		}
		out.phase = "failed"
		return out, nil
	}
	if out.terminal.Status == "success" {
		if phase != "succeeded" || outcome != "success" || row.Phase != "succeeded" {
			return nil, fail(f.InvalidState)
		}
		out.body = bytes.Clone(out.terminal.Output)
	} else {
		if phase != "failed" || outcome != "error" || row.Phase != "failed" {
			return nil, fail(f.InvalidState)
		}
		out.code = string(installFailureCode(out.terminal.Category))
		out.body, _ = json.Marshal(struct {
			Error string `json:"error"`
		}{out.code})
	}
	return out, nil
}
func installFailureCode(category string) f.Code {
	switch category {
	case "authorization_denied":
		return f.Forbidden
	case "capability_unsupported":
		return f.CapabilityUnsupported
	case "not_found":
		return f.NotFound
	case "conflict":
		return f.ConfirmationStale
	case "invalid_arguments":
		return f.InvalidArgument
	case "backend_contract_violation":
		return f.InvalidState
	default:
		return f.DependencyUnavailable
	}
}
func (v *installObserved) outcome() tc.RunnerOutcome {
	out := tc.RunnerOutcome{OperationID: v.record.ID, AttemptID: v.attempt, Phase: v.phase, Attempted: v.record.Version >= 2}
	if v.record.Version < 3 || !v.terminal.Known {
		out.Outcome = "unknown"
		return out
	}
	out.Outcome = "success"
	if v.terminal.Status != "success" {
		out.Outcome = "failure"
	}
	out.ResultAvailable = true
	return out
}
func (v *installObserved) binding(b tc.ToolCallBinding) tc.RunnerResultBinding {
	return tc.RunnerResultBinding{Call: b, OperationID: v.record.ID, AttemptID: v.attempt, RequestID: v.request, InputDigest: v.record.InputDigest, Fingerprint: v.record.Fingerprint, TerminalDigest: digest(v.raw), Outcome: v.outcome().Outcome, Code: v.code, Attempted: true, ResultDigest: digest(v.body), ResultBytes: f.Progress(len(v.body))}
}
func (d *InstallDispatcher) Lookup(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerOutcome, bool, error) {
	if err := d.recoverReturnedInstall(ctx, b); err != nil {
		return tc.RunnerOutcome{}, false, err
	}
	if err := d.recoverColdInstall(ctx, b); err != nil {
		return tc.RunnerOutcome{}, false, err
	}
	var observed *installObserved
	err := d.readTx(ctx, b, true, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, _ tc.ToolCallPlan, _ []byte) error {
		var e error
		observed, e = loadInstallObserved(ctx, x, b)
		return e
	})
	if err != nil || observed == nil {
		return tc.RunnerOutcome{}, false, err
	}
	return observed.outcome(), true, nil
}

type installResultPlan struct {
	owner  *InstallDispatcher
	input  tc.ToolCallPlan
	locks  []f.LockRequest
	result tc.RunnerResultBinding
}

func (p *installResultPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (p *installResultPlan) Binding() tc.RunnerResultBinding {
	if p == nil {
		return tc.RunnerResultBinding{}
	}
	return p.result
}
func (d *InstallDispatcher) DiscoverRunnerResult(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerResultPlan, error) {
	var out *installResultPlan
	err := d.readTx(ctx, b, false, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, plan tc.ToolCallPlan, _ []byte) error {
		observed, err := loadInstallObserved(ctx, x, b)
		if err != nil {
			return err
		}
		if observed == nil || !observed.outcome().ResultAvailable {
			return fail(f.InvalidState)
		}
		lock, _ := f.AggregateLock(f.OperationAggregate, observed.record.ID.String())
		cause, _ := inputCause(b)
		locks, err := baseLocks(b, cause, append(plan.RequiredLocks(), f.LockRequest{Key: lock, Mode: f.Shared}))
		if err != nil {
			return err
		}
		out = &installResultPlan{d, plan, locks, observed.binding(b)}
		return out.result.Validate()
	})
	return out, err
}
func (d *InstallDispatcher) WithRunnerResultInTx(ctx context.Context, tx f.Tx, plan tc.RunnerResultPlan, consume func(context.Context, tc.RunnerResultBinding, mc.ToolResultPart) error) error {
	v, ok := plan.(*installResultPlan)
	if !ok || v == nil || v.owner != d || consume == nil {
		return fail(f.Forbidden)
	}
	state := d.executor.data().core.data()
	if err := state.store.RequireHeldLocks(ctx, tx, v.locks); err != nil {
		return portError(err)
	}
	x, err := state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	return d.current.WithInstallToolCallInTx(ctx, tx, v.result.Call, v.input, func(current context.Context, b tc.ToolCallBinding, _ []byte) error {
		observed, err := loadInstallObserved(current, x, b)
		if err != nil {
			return err
		}
		if observed == nil || !sameRunnerResultBinding(observed.binding(b), v.result) {
			return fail(f.ConfirmationStale)
		}
		part := mc.ToolResultPart{CallID: b.CallID, IsError: v.result.Outcome != "success", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(observed.body)}}}}
		return consume(current, v.result, part)
	})
}
func (d *InstallDispatcher) CancelCall(ctx context.Context, b tc.ToolCallBinding) error {
	if err := d.valid(b); err != nil {
		return err
	}
	d.mu.Lock()
	call := d.calls[b.Identity()]
	if cold := d.cold[b.Identity()]; cold != nil && cold.cancel != nil {
		cold.cancel()
	}
	d.mu.Unlock()
	if call != nil {
		call.cancel()
	}
	return contextError(ctx)
}
func (d *InstallDispatcher) JoinCall(ctx context.Context, b tc.ToolCallBinding) error {
	if err := d.valid(b); err != nil {
		return err
	}
	d.mu.Lock()
	call := d.calls[b.Identity()]
	d.mu.Unlock()
	if call == nil {
		return d.joinColdInstall(ctx, b)
	}
	select {
	case <-call.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	a := d.executor.data().source.authority.data()
	a.mu.Lock()
	h := a.active[call.operation]
	a.mu.Unlock()
	if h != nil {
		select {
		case <-h.done:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := recoverInstallFinalization(ctx, a, h); err != nil {
			return err
		}
		h.mu.Lock()
		err := h.retirement
		h.mu.Unlock()
		if err != nil {
			return err
		}
		a.mu.Lock()
		retained := a.active[call.operation] == h
		a.mu.Unlock()
		if retained {
			return fail(f.ResourceBusy)
		}
		d.setCallError(call, nil)
	}
	d.mu.Lock()
	err := call.err
	d.mu.Unlock()
	if builtinUnknown(err) {
		// A prior observer (including Authority.Drain) may already have retired
		// this original handoff. Require the exact canonical known result before
		// clearing its local error; absent/unknown results keep the owner.
		out, found, observeErr := d.Lookup(ctx, b)
		if observeErr == nil && found && out.ResultAvailable {
			d.setCallError(call, nil)
			return nil
		}
		return err
	}
	return nil
}
func (d *InstallDispatcher) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.stopped = true
	for _, call := range d.calls {
		call.cancel()
	}
	for _, call := range d.cold {
		if call.cancel != nil {
			call.cancel()
		}
	}
	d.mu.Unlock()
	d.executor.data().source.authority.Stop()
}
func (d *InstallDispatcher) Drain(ctx context.Context) error {
	if d == nil {
		return fail(f.DependencyUnbound)
	}
	d.Stop()
	d.mu.Lock()
	calls := make([]*installDispatchCall, 0, len(d.calls))
	for _, call := range d.calls {
		calls = append(calls, call)
	}
	cold := make([]tc.ToolCallBinding, 0, len(d.cold))
	for _, call := range d.cold {
		cold = append(cold, call.binding)
	}
	d.mu.Unlock()
	for _, call := range calls {
		if err := d.JoinCall(ctx, call.binding); err != nil {
			return err
		}
	}
	for _, binding := range cold {
		if err := d.joinColdInstall(ctx, binding); err != nil {
			return err
		}
	}
	return d.executor.data().source.authority.Drain(ctx)
}
func (d *InstallDispatcher) Joined() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.stopped || len(d.cold) != 0 {
		return false
	}
	for _, call := range d.calls {
		select {
		case <-call.done:
		default:
			return false
		}
		if builtinUnknown(call.err) {
			return false
		}
	}
	return d.executor.data().source.authority.Joined()
}

var _ tc.RunnerDispatcher = (*InstallDispatcher)(nil)
var _ tc.RunnerResultAuthority = (*InstallDispatcher)(nil)
var _ tc.RunnerCallLifecycle = (*InstallDispatcher)(nil)
