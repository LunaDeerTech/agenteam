package runtime

import (
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

// ReadFileAuthority is explicitly composed with the real Execution producer,
// Store and process guard before the Mount resolver/dispatcher are constructed.
// Construction is not a dispatch grant or a default application registration.
type ReadFileAuthority struct {
	data func() *readFileAuthorityState
}

type readFileAuthorityState struct {
	store      Store
	guard      *object.ProcessGuard
	process    oc.ProcessID
	executions tc.ReadFileExecutionAuthority
	mu         sync.Mutex
	stopped    bool
	active     map[i.ExecutionID]*readFileHandoff
}

type readFileHandoff struct {
	mu        sync.Mutex
	issuer    *readFileAuthorityState
	executor  *readFileExecutorData
	execution i.ExecutionID
	plan      tc.ReadFileCallPlan
	binding   tc.ToolCallBinding
	arguments tc.ReadFileToolArguments
	record    readFileOperationRecord
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
	terminal   *readFileTerminal
	payload    []byte
	resolving  bool
}

type readFileHandoffKey struct{}

func NewReadFileAuthority(store Store, guard *object.ProcessGuard, process oc.ProcessID, executions tc.ReadFileExecutionAuthority) (*ReadFileAuthority, error) {
	if nilPort(store) || guard == nil || process.Validate() != nil || nilPort(executions) {
		return nil, fail(f.DependencyUnbound)
	}
	d := &readFileAuthorityState{store: store, guard: guard, process: process, executions: executions, active: make(map[i.ExecutionID]*readFileHandoff)}
	return &ReadFileAuthority{data: func() *readFileAuthorityState { return d }}, nil
}

func (h *readFileHandoff) intent() mc.ReadFileIntent {
	b := h.binding
	return mc.ReadFileIntent{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID,
		OperationID: h.record.ID, MountID: h.arguments.MountID, Arguments: h.arguments.Arguments}
}

func (h *readFileHandoff) matches(in mc.ReadFileIntent) bool {
	want := h.intent()
	return in.Validate() == nil && in.Actor.Equal(want.Actor) && in.ProjectID == want.ProjectID && in.AgentID == want.AgentID &&
		in.ExecutionID == want.ExecutionID && in.OperationID == want.OperationID && in.MountID == want.MountID && in.Arguments == want.Arguments
}

func (d *readFileAuthorityState) requireProcess() error {
	process, err := d.guard.CurrentProcess()
	if err != nil || process != d.process {
		return fail(f.DependencyUnavailable)
	}
	return nil
}

func (a *ReadFileAuthority) ReadFileLocks(in mc.ReadFileIntent) ([]f.LockRequest, error) {
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
func (a *ReadFileAuthority) WithReadFileInTx(ctx context.Context, tx f.Tx, in mc.ReadFileIntent, consume func(context.Context) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if a == nil || a.data == nil || consume == nil {
		return fail(f.DependencyUnbound)
	}
	d := a.data()
	h, ok := ctx.Value(readFileHandoffKey{}).(*readFileHandoff)
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
	row, err := loadReadFileOperation(ctx, x, h.record.Input.Binding)
	if err != nil {
		return err
	}
	if row == nil || row.ID != h.record.ID || row.InputDigest != h.record.InputDigest || row.Fingerprint != h.record.Fingerprint || row.Phase != h.admission {
		return fail(f.ConfirmationStale)
	}
	if h.admission == "running" {
		if err = requireReadFileAttempt(ctx, x, d, h); err != nil {
			return err
		}
	} else if h.admission != "created" {
		return fail(f.Forbidden)
	}
	return consumeReadFileInput(ctx, tx, d, h, func(authorized context.Context, _ []byte) error { return consume(authorized) })
}

func (a *ReadFileAuthority) Stop() {
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

func (a *ReadFileAuthority) Drain(ctx context.Context) error {
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
	calls := make([]*readFileHandoff, 0, len(d.active))
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

func (a *ReadFileAuthority) Joined() bool {
	if a == nil || a.data == nil {
		return false
	}
	d := a.data()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopped && len(d.active) == 0
}

func (ReadFileAuthority) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_read_file_authority")
}
func (ReadFileAuthority) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_read_file_authority"`), nil
}
func (ReadFileAuthority) LogValue() slog.Value { return slog.StringValue("tool_read_file_authority") }

var _ mc.ReadFileAuthority = (*ReadFileAuthority)(nil)
