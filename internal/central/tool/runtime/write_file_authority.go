package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// WriteFileAuthority is explicitly composed with the real Execution producer,
// Store and process guard before the Mount resolver/dispatcher are constructed.
// Construction is not a dispatch grant or a default application registration.
type WriteFileAuthority struct {
	data func() *writeFileAuthorityState
}

type writeFileAuthorityState struct {
	store      Store
	guard      *object.ProcessGuard
	process    oc.ProcessID
	executions tc.WriteFileExecutionAuthority
	mu         sync.Mutex
	stopped    bool
	active     map[i.ExecutionID]*writeFileHandoff
	lookups    map[*writeFileLookupCall]struct{}
}

type writeFileLookupCall struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (d *writeFileAuthorityState) beginLookup(ctx context.Context) (context.Context, func(), error) {
	callCtx, cancel := context.WithCancel(ctx)
	call := &writeFileLookupCall{cancel: cancel, done: make(chan struct{})}
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		cancel()
		return nil, nil, fail(f.ShuttingDown)
	}
	if d.lookups == nil {
		d.lookups = make(map[*writeFileLookupCall]struct{})
	}
	d.lookups[call] = struct{}{}
	d.mu.Unlock()
	return callCtx, func() {
		cancel()
		d.mu.Lock()
		delete(d.lookups, call)
		close(call.done)
		d.mu.Unlock()
	}, nil
}

type writeFileHandoff struct {
	mu        sync.Mutex
	issuer    *writeFileAuthorityState
	executor  *writeFileExecutorData
	execution i.ExecutionID
	plan      tc.WriteFileCallPlan
	binding   tc.ToolCallBinding
	arguments tc.WriteFileToolArguments
	record    writeFileOperationRecord
	locks     []f.LockRequest
	cause     f.TransactionCause
	attempt   tc.AttemptID
	request   p.ID
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	// Admission is opened only by this original executor, after the relevant
	// real transaction has returned Committed. No caller can supply these bits.
	admission  string
	retirement error
	terminal   *writeFileTerminal
	payload    []byte
}

type writeFileHandoffKey struct{}

func NewWriteFileAuthority(store Store, guard *object.ProcessGuard, process oc.ProcessID, executions tc.WriteFileExecutionAuthority) (*WriteFileAuthority, error) {
	if nilPort(store) || guard == nil || process.Validate() != nil || nilPort(executions) {
		return nil, fail(f.DependencyUnbound)
	}
	d := &writeFileAuthorityState{store: store, guard: guard, process: process, executions: executions, active: make(map[i.ExecutionID]*writeFileHandoff)}
	return &WriteFileAuthority{data: func() *writeFileAuthorityState { return d }}, nil
}

func (h *writeFileHandoff) intent() mc.WriteFileIntent {
	b := h.binding
	return mc.WriteFileIntent{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID,
		OperationID: h.record.ID, MountID: h.arguments.MountID, Arguments: h.arguments.Arguments}
}

func (h *writeFileHandoff) matches(in mc.WriteFileIntent) bool {
	want := h.intent()
	return in.Validate() == nil && in.Actor.Equal(want.Actor) && in.ProjectID == want.ProjectID && in.AgentID == want.AgentID &&
		in.ExecutionID == want.ExecutionID && in.OperationID == want.OperationID && in.MountID == want.MountID && sameWriteFileArguments(in.Arguments, want.Arguments)
}

func (d *writeFileAuthorityState) requireProcess() error {
	process, err := d.guard.CurrentProcess()
	if err != nil || process != d.process {
		return fail(f.DependencyUnavailable)
	}
	return nil
}

func (a *WriteFileAuthority) WriteFileLocks(in mc.WriteFileIntent) ([]f.LockRequest, error) {
	if a == nil || a.data == nil || in.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	d := a.data()
	d.mu.Lock()
	h := d.active[in.ExecutionID]
	d.mu.Unlock()
	if h == nil {
		return nil, fail(f.Forbidden)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.issuer != d || h.admission == "" || !h.matches(in) {
		return nil, fail(f.Forbidden)
	}
	return slices.Clone(h.locks), nil
}

// This callback is reached only from the original Mount Resolve or Claim.
// Current Execution permission and the Agent witness remain live through the
// actual Mount reader. A previously returned context is never reused as proof.
func (a *WriteFileAuthority) WithWriteFileInTx(ctx context.Context, tx f.Tx, in mc.WriteFileIntent, request p.ID, consume func(context.Context) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if a == nil || a.data == nil || consume == nil {
		return fail(f.DependencyUnbound)
	}
	d := a.data()
	h, ok := ctx.Value(writeFileHandoffKey{}).(*writeFileHandoff)
	if !ok || h == nil || h.issuer != d || h.executor == nil || h.executor.authority != d {
		return fail(f.Forbidden)
	}
	d.mu.Lock()
	current := d.active[h.execution] == h
	d.mu.Unlock()
	if !current {
		return fail(f.Forbidden)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.admission == "" || !h.matches(in) || h.ctx == nil || h.ctx.Err() != nil {
		return fail(f.Forbidden)
	}
	// Resolve requires the original created admission and no RID. Claim must
	// present the exact Prepare RID after our Attempt's Committed return.
	if h.admission == "created" && request != "" || h.admission == "running" && (!request.Valid() || request != h.request) {
		return fail(f.Forbidden)
	}
	x, err := d.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = d.store.RequireHeldLocks(ctx, tx, h.locks); err != nil {
		return portError(err)
	}
	if err = d.requireProcess(); err != nil {
		return err
	}
	row, err := loadWriteFileOperation(ctx, x, h.record.Input.Binding)
	if err != nil {
		return err
	}
	if row == nil || row.ID != h.record.ID || row.InputDigest != h.record.InputDigest || row.Fingerprint != h.record.Fingerprint || row.Phase != h.admission {
		return fail(f.ConfirmationStale)
	}
	if h.admission == "running" {
		if err = requireWriteFileAttempt(ctx, x, d, h); err != nil {
			return err
		}
	} else if h.admission != "created" {
		return fail(f.Forbidden)
	}
	return consumeWriteFileInput(ctx, tx, d, h, func(authorized context.Context, _ []byte) error { return consume(authorized) })
}

func (a *WriteFileAuthority) Stop() {
	if a == nil || a.data == nil {
		return
	}
	d := a.data()
	d.mu.Lock()
	d.stopped = true
	cancels := make([]context.CancelFunc, 0, len(d.active)+len(d.lookups))
	for _, h := range d.active {
		cancels = append(cancels, h.cancel)
	}
	for call := range d.lookups {
		cancels = append(cancels, call.cancel)
	}
	d.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (a *WriteFileAuthority) Drain(ctx context.Context) error {
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
	calls := make([]*writeFileHandoff, 0, len(d.active))
	for _, h := range d.active {
		calls = append(calls, h)
	}
	lookups := make([]*writeFileLookupCall, 0, len(d.lookups))
	for call := range d.lookups {
		lookups = append(lookups, call)
	}
	d.mu.Unlock()
	for _, call := range lookups {
		select {
		case <-call.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
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

func (a *WriteFileAuthority) Joined() bool {
	if a == nil || a.data == nil {
		return false
	}
	d := a.data()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopped && len(d.active) == 0 && len(d.lookups) == 0
}

func (WriteFileAuthority) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_write_file_authority")
}
func (WriteFileAuthority) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_write_file_authority"`), nil
}
func (WriteFileAuthority) LogValue() slog.Value { return slog.StringValue("tool_write_file_authority") }

var _ mc.WriteFileAuthority = (*WriteFileAuthority)(nil)

func sameWriteFileArguments(a, b p.WriteFileArguments) bool {
	return a.Path == b.Path && a.Overwrite == b.Overwrite && bytes.Equal(a.Data, b.Data)
}

func clearWriteFileArguments(h *writeFileHandoff) {
	clear(h.arguments.Arguments.Data)
	h.arguments.Arguments.Data = nil
}
