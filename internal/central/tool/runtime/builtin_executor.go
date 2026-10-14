package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type BuiltinExecutor struct {
	authority  *BuiltinAuthority
	executions tc.BuiltinInputAuthority
	registry   *registry.Registry
	schemas    *RegistrySchemaValidator
	backends   map[tc.BuiltinBinding]BuiltinBackend
	mu         sync.Mutex
	stopped    bool
	calls      map[tc.ToolCallIdentity]*builtinCall
	lookups    map[*builtinCall]struct{}
	changed    chan struct{}
}

type builtinCall struct {
	binding        tc.ToolCallBinding
	plan           tc.ToolCallPlan
	locks          []f.LockRequest
	cause          f.TransactionCause
	record         builtinRecord
	arguments      []byte
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	lookup         bool
	domainReturned bool
	resolving      bool // guarded by the executor mutex after done closes
	// These fields are published before done closes. Retained calls remain in
	// the owner map until canonical lookup confirms their exact terminal.
	terminal   *builtinTerminal
	body       []byte
	retirement error
}

func NewBuiltinExecutor(authority *BuiltinAuthority, executions tc.BuiltinInputAuthority, reg *registry.Registry, backends ...BuiltinBackend) (*BuiltinExecutor, error) {
	if authority == nil || nilPort(executions) || reg == nil || len(backends) == 0 {
		return nil, fail(f.DependencyUnbound)
	}
	if committed, ok := executions.(tc.CommittedBuiltinInputAuthority); !ok || nilPort(committed) {
		return nil, fail(f.DependencyUnbound)
	}
	schemas, err := NewRegistrySchemaValidator(authority.store, reg)
	if err != nil {
		return nil, err
	}
	ports := make(map[tc.BuiltinBinding]BuiltinBackend, len(backends))
	for _, backend := range backends {
		if nilPort(backend) {
			return nil, fail(f.DependencyUnbound)
		}
		registration := backend.Registration()
		if registration.Validate(context.Background()) != nil || !tc.WorkArtifactBuiltinBinding(registration.Binding) || !registration.Active || ports[registration.Binding] != nil {
			return nil, fail(f.InvalidArgument)
		}
		ports[registration.Binding] = backend
	}
	return &BuiltinExecutor{authority: authority, executions: executions, registry: reg, schemas: schemas, backends: ports, calls: map[tc.ToolCallIdentity]*builtinCall{}, lookups: map[*builtinCall]struct{}{}, changed: make(chan struct{})}, nil
}

func (e *BuiltinExecutor) begin(ctx context.Context, b tc.ToolCallBinding) (*builtinCall, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if e == nil || b.Validate() != nil || !tc.WorkArtifactBuiltinBinding(b.Binding) {
		return nil, fail(f.InvalidArgument)
	}
	if nilPort(e.backends[b.Binding]) {
		return nil, fail(f.DependencyUnbound)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return nil, fail(f.ShuttingDown)
	}
	if e.calls[b.Identity()] != nil {
		return nil, fail(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	call := &builtinCall{binding: b, ctx: run, cancel: cancel, done: make(chan struct{})}
	e.calls[b.Identity()] = call
	return call, nil
}

func (e *BuiltinExecutor) returned(call *builtinCall) {
	call.cancel()
	e.mu.Lock()
	defer e.mu.Unlock()
	if call.retirement == nil {
		clear(call.arguments)
		clear(call.body)
		delete(e.calls, call.binding.Identity())
	}
	close(call.done)
	close(e.changed)
	e.changed = make(chan struct{})
}

func builtinOperationLock(id tc.OperationID) (f.LockRequest, error) {
	key, err := f.AggregateLock(f.OperationAggregate, id.String())
	return f.LockRequest{Key: key, Mode: f.Exclusive}, err
}

func (e *BuiltinExecutor) discover(ctx context.Context, call *builtinCall) error {
	plan, err := e.executions.DiscoverToolCall(ctx, call.binding)
	if err != nil {
		return err
	}
	if nilPort(plan) {
		return fail(f.DependencyUnbound)
	}
	call.plan = plan
	call.cause, err = inputCause(call.binding)
	if err != nil {
		return err
	}
	call.locks, err = baseLocks(call.binding, call.cause, plan.RequiredLocks())
	return err
}

func (e *BuiltinExecutor) transaction(ctx context.Context, call *builtinCall, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	r := e.authority.store.WithinTx(ctx, call.cause, func(ctx context.Context, tx f.Tx) error {
		if err := e.authority.store.AcquireAll(ctx, tx, call.locks); err != nil {
			return portError(err)
		}
		if err := e.authority.guard.RequireCurrentProcessInTx(ctx, tx, e.authority.process); err != nil {
			return portError(err)
		}
		x, err := e.authority.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	})
	return commitError(r)
}

func (e *BuiltinExecutor) input(ctx context.Context, tx f.Tx, call *builtinCall, consume func(context.Context, []byte) error) error {
	calls := 0
	loan := e.executions.WithBuiltinToolCallInTx
	if call.lookup {
		committed, ok := e.executions.(tc.CommittedBuiltinInputAuthority)
		if !ok {
			return fail(f.DependencyUnbound)
		}
		loan = committed.WithCommittedBuiltinToolCallInTx
	}
	err := loan(ctx, tx, call.binding, call.plan, func(current context.Context, b tc.ToolCallBinding, raw []byte) error {
		calls++
		if calls != 1 || !sameRunnerCallBinding(call.binding, b) {
			return fail(f.Forbidden)
		}
		if _, _, err := builtinInput(current, b, raw); err != nil {
			return err
		}
		return consume(current, raw)
	})
	if err != nil {
		return err
	}
	if calls != 1 {
		return fail(f.Forbidden)
	}
	return nil
}

func (e *BuiltinExecutor) Execute(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerOutcome, error) {
	call, err := e.begin(ctx, b)
	if err != nil {
		return tc.RunnerOutcome{}, err
	}
	defer e.returned(call)
	ctx = call.ctx
	if err = e.discover(ctx, call); err != nil {
		return tc.RunnerOutcome{}, err
	}
	var existing *builtinRecord
	err = e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.input(ctx, tx, call, func(current context.Context, raw []byte) error {
			var err error
			existing, err = loadBuiltinRecord(current, x, b)
			return err
		})
	})
	if err != nil {
		return tc.RunnerOutcome{}, err
	}
	if existing != nil {
		call.record = *existing
		return e.observe(ctx, call)
	}
	operation, err := f.NewID[tc.Operation]()
	if err != nil {
		return tc.RunnerOutcome{}, portError(err)
	}
	attempt, err := f.NewID[tc.Attempt]()
	if err != nil {
		return tc.RunnerOutcome{}, portError(err)
	}
	request, err := f.NewID[f.Request]()
	if err != nil {
		return tc.RunnerOutcome{}, portError(err)
	}
	lock, err := builtinOperationLock(operation)
	if err != nil {
		return tc.RunnerOutcome{}, err
	}
	call.locks, err = unionLocks(append(call.locks, lock))
	if err != nil {
		return tc.RunnerOutcome{}, err
	}
	backend := e.backends[b.Binding]
	err = e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.input(ctx, tx, call, func(current context.Context, raw []byte) error {
			old, err := loadBuiltinRecord(current, x, b)
			if err != nil {
				return err
			}
			if old != nil {
				return fail(f.ResourceBusy)
			}
			reg := backend.Registration()
			if err = e.registry.RequireCurrentWorkArtifactBuiltinInTx(current, tx, b.Spec, b.Binding, reg.ScopeResolverID, reg.RiskClassifierID); err != nil {
				return err
			}
			if err = e.schemas.validate(current, tx, b.Spec, raw, false); err != nil {
				return err
			}
			in, encoded, err := builtinInput(current, b, raw)
			if err != nil {
				return err
			}
			call.record = builtinRecord{ID: operation, Input: in, InputDigest: digest(encoded), Fingerprint: digest(encoded), Phase: "created", Version: 1, Attempt: attempt, Request: p.ID(request.String())}
			call.arguments = bytes.Clone(raw)
			return startBuiltinRecord(current, x, call.record, e.authority.process)
		})
	})
	if err != nil {
		if builtinUnknown(err) {
			call.retirement = err
		}
		return tc.RunnerOutcome{}, err
	}
	call.record.Phase, call.record.Version = "running", 2
	body, _, domainErr := e.invokeBackend(ctx, call, backend, false)
	call.domainReturned = true
	if builtinUncertainDomain(domainErr) {
		call.retirement = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(domainErr)
		clear(body)
		return tc.RunnerOutcome{OperationID: operation, AttemptID: attempt, Phase: "running", Outcome: "unknown", Attempted: true}, call.retirement
	}
	terminal := builtinTerminal{Format: 1, Operation: operation, Attempt: attempt, Request: p.ID(request.String()), Outcome: "success"}
	if domainErr != nil {
		var fault *f.Fault
		if !errors.As(domainErr, &fault) {
			call.retirement = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(domainErr)
			return tc.RunnerOutcome{OperationID: operation, AttemptID: attempt, Phase: "running", Outcome: "unknown", Attempted: true}, call.retirement
		}
		terminal.Outcome, terminal.Code = "failure", string(fault.Code.Safe())
		body, _ = json.Marshal(struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}{Error: struct {
			Code string `json:"code"`
		}{terminal.Code}})
	}
	if len(body) < 2 || len(body) > 1<<20 || !json.Valid(body) {
		call.retirement = fail(f.DependencyUnavailable)
		return tc.RunnerOutcome{}, call.retirement
	}
	terminal.ResultDigest, terminal.ResultBytes = digest(body), f.Progress(len(body))
	call.terminal = &terminal
	call.body = bytes.Clone(body)
	clear(body)
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = e.publish(finishCtx, call)
	if err != nil {
		call.retirement = err
		return tc.RunnerOutcome{}, err
	}
	phase, _ := terminal.phases()
	return tc.RunnerOutcome{OperationID: operation, AttemptID: attempt, Phase: phase, Outcome: terminal.Outcome, Attempted: true, ResultAvailable: true}, nil
}

func builtinUncertainDomain(err error) bool {
	if err == nil {
		return false
	}
	var fault *f.Fault
	// A committed domain fault without its canonical receipt is not evidence
	// of a failed mutation. Preserve the call for target-only receipt lookup.
	return !errors.As(err, &fault) || fault.CommitState == f.Unknown || fault.CommitState == f.Committed
}

func builtinUnknown(err error) bool {
	if err == nil {
		return false
	}
	var fault *f.Fault
	return errors.As(err, &fault) && fault.CommitState == f.Unknown
}

func (e *BuiltinExecutor) publish(ctx context.Context, call *builtinCall) error {
	if call.terminal == nil {
		return fail(f.InvalidState)
	}
	return e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		r, err := loadBuiltinRecord(ctx, x, call.binding)
		if err != nil {
			return err
		}
		if r == nil || r.ID != call.record.ID || r.Attempt != call.record.Attempt || r.InputDigest != call.record.InputDigest {
			return fail(f.ConfirmationStale)
		}
		if r.Version == 3 {
			terminal, body, _, err := loadBuiltinTerminal(ctx, x, *r)
			if err != nil {
				return err
			}
			if terminal != *call.terminal || !bytes.Equal(body, call.body) {
				return fail(f.Forbidden)
			}
			return nil
		}
		if call.terminal.Outcome == "success" {
			if err = e.schemas.validate(ctx, tx, call.binding.Spec, call.body, true); err != nil {
				return err
			}
		}
		return finishBuiltinRecord(ctx, x, e.authority.process, call.record, *call.terminal, call.body)
	})
}

func (e *BuiltinExecutor) observe(ctx context.Context, call *builtinCall) (tc.RunnerOutcome, error) {
	lock, err := builtinOperationLock(call.record.ID)
	if err != nil {
		return tc.RunnerOutcome{}, err
	}
	call.locks, err = unionLocks(append(call.locks, lock))
	if err != nil {
		return tc.RunnerOutcome{}, err
	}
	var out tc.RunnerOutcome
	err = e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.input(ctx, tx, call, func(current context.Context, _ []byte) error {
			r, err := loadBuiltinRecord(current, x, call.binding)
			if err != nil {
				return err
			}
			if r == nil {
				return fail(f.NotFound)
			}
			out = tc.RunnerOutcome{OperationID: r.ID, AttemptID: r.Attempt, Phase: r.Phase}
			if r.Version != 3 {
				return nil
			}
			terminal, body, _, err := loadBuiltinTerminal(current, x, *r)
			defer clear(body)
			if err != nil {
				return err
			}
			call.terminal = &terminal
			out.Outcome, out.Attempted, out.ResultAvailable = terminal.Outcome, true, true
			return nil
		})
	})
	return out, err
}
