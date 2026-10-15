package runtime

import (
	"bytes"
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/authorization"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

// This is a new process's lookup owner. It is never an installHandoff and its
// done channel describes only its own actual synchronous lookup transaction.
// The old backend's exit is proved exclusively by ProcessGuard.ConfirmStopped.
type installColdCall struct {
	dispatcher *InstallDispatcher
	binding    tc.ToolCallBinding
	record     operationRecord
	origin     installColdOrigin
	proved     bool
	busy       bool // Protected by dispatcher.mu, as are cancel and done.
	cancel     context.CancelFunc
	done       chan struct{}
	// Only a real final-write callback can populate these recovery fields.
	terminal *installTerminal
	finalRaw []byte
	writeErr error
}

type installColdOrigin struct {
	attempt tc.AttemptID
	process oc.ProcessID
	request f.ID[f.Request]
}

func loadInstallColdOrigin(ctx context.Context, x postgres.SQLExecutor, b tc.ToolCallBinding, row operationRecord) (installColdOrigin, error) {
	var out installColdOrigin
	if row.Phase != "running" || row.Version != 2 {
		return out, fail(f.ConfirmationStale)
	}
	var attempt, process, request string
	var ordinal, fence int64
	err := x.QueryRow(ctx, `SELECT a.attempt_id,a.process_id,a.backend_request_id,a.ordinal,a.fence FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.operation_id=o.operation_id AND a.attempt_id=o.active_attempt_id AND a.execution_id=o.execution_id AND a.project_id=o.project_id AND a.agent_id=o.agent_id WHERE o.operation_id=$1 AND o.execution_id=$2 AND o.project_id=$3 AND o.agent_id=$4 AND o.phase='running' AND o.version=2 AND a.phase='running' AND NOT a.retired`, row.ID.String(), b.ExecutionID.String(), b.ProjectID.String(), b.AgentID.String()).Scan(&attempt, &process, &request, &ordinal, &fence)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fail(f.ConfirmationStale)
	}
	if err != nil {
		return out, portError(err)
	}
	out.attempt, err = f.ParseID[tc.Attempt](attempt)
	if err != nil {
		return out, fail(f.InvalidState)
	}
	out.process, err = f.ParseID[oc.Process](process)
	if err != nil {
		return out, fail(f.InvalidState)
	}
	out.request, err = f.ParseID[f.Request](request)
	if err != nil || ordinal != 1 || fence != 1 {
		return out, fail(f.InvalidState)
	}
	return out, nil
}

func sameInstallColdRecord(a, b operationRecord) bool {
	return a.ID == b.ID && a.SkillID == b.SkillID && a.Key == b.Key && a.InputDigest == b.InputDigest && a.Fingerprint == b.Fingerprint
}

// The raw arguments and newly discovered current authorization live only for
// this synchronous lookup. A retained final-write owner stores no package body.
type installColdRead struct {
	call       *installColdCall
	issuer     *installAuthorityState
	executor   *installExecutorData
	ctx        context.Context
	raw        []byte
	toolPlan   tc.ToolCallPlan
	input      authorization.InstallInput
	permission authorization.InstallPlan
	locks      []f.LockRequest
	cause      f.TransactionCause
}

func (d *InstallDispatcher) beginColdInstall(ctx context.Context, b tc.ToolCallBinding) (*installColdCall, context.Context, func(bool), error) {
	return d.beginColdInstallOwner(ctx, b, nil)
}

func (d *InstallDispatcher) beginColdInstallOwner(ctx context.Context, b tc.ToolCallBinding, expected *installColdCall) (*installColdCall, context.Context, func(bool), error) {
	if err := d.valid(b); err != nil {
		return nil, nil, nil, err
	}
	if err := contextError(ctx); err != nil {
		return nil, nil, nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.calls[b.Identity()] != nil {
		return nil, nil, nil, nil
	}
	c := d.cold[b.Identity()]
	// Join can only continue the owner whose actual return it awaited. Check
	// this under the admission lock: a completed lookup must not create a new
	// owner, including after Stop or another concurrent join settled it.
	if expected != nil && c != expected {
		if c == nil {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, fail(f.ResourceBusy)
	}
	if c == nil {
		if d.stopped {
			return nil, nil, nil, fail(f.ShuttingDown)
		}
		c = &installColdCall{dispatcher: d, binding: b}
		if d.cold == nil {
			d.cold = make(map[tc.ToolCallIdentity]*installColdCall)
		}
		d.cold[b.Identity()] = c
	}
	if c.busy || !sameRunnerCallBinding(c.binding, b) {
		return nil, nil, nil, fail(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	c.busy, c.cancel, c.done = true, cancel, make(chan struct{})
	return c, run, func(retain bool) {
		cancel()
		d.mu.Lock()
		defer d.mu.Unlock()
		c.busy = false
		close(c.done)
		if !retain && d.cold[b.Identity()] == c {
			delete(d.cold, b.Identity())
		}
	}, nil
}

// Only Lookup calls this method. No branch enters ExecuteInstall or the upload
// adapter. Missing/reserved publication never produces a terminal Tool result.
func (d *InstallDispatcher) recoverColdInstall(ctx context.Context, b tc.ToolCallBinding) (err error) {
	return d.recoverColdInstallOwner(ctx, b, nil)
}

func (d *InstallDispatcher) recoverColdInstallOwner(ctx context.Context, b tc.ToolCallBinding, expected *installColdCall) (err error) {
	c, run, end, err := d.beginColdInstallOwner(ctx, b, expected)
	if err != nil {
		// Even cancelled admission cannot turn a retained physical write into
		// a known failure. Do not inspect a concurrently active owner.
		if d != nil {
			d.mu.Lock()
			pending := d.cold[b.Identity()]
			if pending != nil && (expected == nil || pending == expected) && !pending.busy && sameRunnerCallBinding(pending.binding, b) && pending.writeErr != nil {
				err = pending.writeErr
			}
			d.mu.Unlock()
		}
		return err
	}
	if c == nil {
		return err
	}
	retain := c.proved
	defer func() { end(retain) }()
	originalWrite := c.writeErr
	defer func() {
		// A failed read cannot replace an earlier physical write Unknown. A
		// later actual final write updates c.writeErr to its own identity.
		if err != nil && originalWrite != nil && c.writeErr == originalWrite {
			err = originalWrite
		}
	}()
	e := d.executor.data()
	r := &installColdRead{call: c, issuer: e.source.authority.data(), executor: e, ctx: run}
	defer func() { clear(r.raw); r.input = authorization.InstallInput{} }()
	var observed *installObserved
	var origin installColdOrigin
	err = d.readTx(run, b, true, nil, func(current context.Context, _ f.Tx, x postgres.SQLExecutor, plan tc.ToolCallPlan, raw []byte) error {
		var err error
		observed, err = loadInstallObserved(current, x, b)
		if err != nil || observed == nil || observed.record.Version != 2 {
			return err
		}
		origin, err = loadInstallColdOrigin(current, x, b, observed.record)
		if err != nil {
			return err
		}
		r.raw, r.toolPlan = bytes.Clone(raw), plan
		return nil
	})
	if err != nil {
		return err
	}
	if observed == nil || observed.record.Version != 2 {
		if c.proved {
			if !coldInstallTerminalMatches(c, observed) {
				return fail(f.ConfirmationStale)
			}
			retain = false
		}
		return nil
	}
	if c.proved && (!sameInstallColdRecord(c.record, observed.record) || c.origin != origin) {
		return fail(f.ConfirmationStale)
	}
	if origin.process == r.issuer.process {
		return fail(f.ResourceBusy)
	}
	input, err := prepareInput(run, b, r.raw)
	if err != nil {
		return err
	}
	raw, err := encode(input)
	if err != nil || digest(raw) != observed.record.InputDigest {
		return fail(f.ConfirmationStale)
	}
	pkg, _, err := parseInstall(run, r.raw)
	if err != nil {
		return err
	}
	call, err := builtin.NewSkillInstallCall(run, b.Actor, b.Spec, observed.record.ID.String(), observed.record.SkillID, pkg)
	if err != nil {
		return err
	}
	r.input = authorization.InstallInput{Binding: b, Call: call, Fingerprint: observed.record.Fingerprint}
	r.permission, err = e.permission.DiscoverInstall(run, r.input)
	if err != nil {
		return err
	}
	r.cause, err = inputCause(b)
	if err != nil {
		return err
	}
	operationLock, _ := f.AggregateLock(f.OperationAggregate, observed.record.ID.String())
	extra := append(r.permission.RequiredLocks(), r.toolPlan.RequiredLocks()...)
	r.locks, err = baseLocks(b, r.cause, append(extra, f.LockRequest{Key: operationLock, Mode: f.Exclusive}))
	if err != nil {
		return err
	}
	// No transaction or database lock is held while proving the old process.
	if err = r.issuer.guard.ConfirmStopped(run, origin.process); err != nil {
		return portError(err)
	}
	c.record, c.origin = observed.record, origin
	result := r.transaction(run, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return r.requireRunning(ctx, tx, x)
	})
	if err = commitError(result); err != nil {
		return err
	}
	c.proved, retain = true, true
	if c.terminal == nil {
		borrow, release := newInstallColdLookup(run, r)
		value, lookupErr := e.source.adapter.Lookup(borrow, call, origin.request)
		release()
		if lookupErr != nil {
			return lookupErr
		}
		receipt := projectInstallReceipt(value.Receipt())
		terminal := installTerminal{Format: 1, Operation: c.record.ID, Attempt: origin.attempt, Status: "success", Known: true, Receipt: &receipt, Output: value.ModelOutput()}
		if err = terminal.validateOrigin(c.record, b, origin.attempt); err != nil {
			return err
		}
		c.terminal = &terminal
	}
	if err = r.finish(run); err != nil {
		return err
	}
	retain = false
	return nil
}

func coldInstallTerminalMatches(c *installColdCall, observed *installObserved) bool {
	return observed != nil && observed.record.Version == 3 && sameInstallColdRecord(c.record, observed.record) && observed.attempt == c.origin.attempt && string(observed.request) == c.origin.request.String() && observed.process == c.origin.process.String() && observed.terminal.Known && len(c.finalRaw) > 0 && bytes.Equal(c.finalRaw, observed.raw)
}

func (r *installColdRead) transaction(ctx context.Context, action func(context.Context, f.Tx, postgres.SQLExecutor) error) f.CommitResult {
	return r.issuer.store.WithinTx(ctx, r.cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.issuer.store.AcquireAll(ctx, tx, r.locks); err != nil {
			return portError(err)
		}
		x, err := r.requireCurrent(ctx, tx)
		if err != nil {
			return err
		}
		return action(ctx, tx, x)
	})
}

func (r *installColdRead) requireCurrent(ctx context.Context, tx f.Tx) (postgres.SQLExecutor, error) {
	a, d := r.issuer, r.call.dispatcher
	if err := a.store.RequireHeldLocks(ctx, tx, r.locks); err != nil {
		return nil, portError(err)
	}
	if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return nil, portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if _, err = r.executor.core.data().store.InTx(tx); err != nil {
		return nil, portError(err)
	}
	calls := 0
	err = d.current.WithInstallToolCallInTx(ctx, tx, r.call.binding, r.toolPlan, func(current context.Context, actual tc.ToolCallBinding, raw []byte) error {
		calls++
		if calls != 1 || !sameRunnerCallBinding(actual, r.call.binding) || !bytes.Equal(raw, r.raw) {
			return fail(f.ConfirmationStale)
		}
		return r.executor.permission.AuthorizeInstallInTx(current, tx, r.input, r.permission)
	})
	if err != nil {
		return nil, err
	}
	if calls != 1 {
		return nil, fail(f.Forbidden)
	}
	return x, nil
}

func (r *installColdRead) requireRunning(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
	row, err := loadInstallImmutableRecord(ctx, x, r.call.binding)
	if err != nil {
		return err
	}
	if row == nil || !sameInstallColdRecord(*row, r.call.record) {
		return fail(f.ConfirmationStale)
	}
	origin, err := loadInstallColdOrigin(ctx, x, r.call.binding, *row)
	if err != nil {
		return err
	}
	if origin != r.call.origin {
		return fail(f.ConfirmationStale)
	}
	return nil
}

func (r *installColdRead) finish(ctx context.Context) error {
	c := r.call
	original := c.writeErr
	wrote := false
	result := r.transaction(ctx, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		observed, err := loadInstallObserved(ctx, x, c.binding)
		if err != nil {
			return err
		}
		if coldInstallTerminalMatches(c, observed) {
			return nil
		}
		if err = r.requireRunning(ctx, tx, x); err != nil {
			return err
		}
		// Complete original locks settle any earlier final transaction. Exact
		// running/v2 now permits only these same validated bytes to be written.
		if len(c.finalRaw) == 0 {
			if err = r.executor.schemas.ValidateInstallOutputInTx(ctx, tx, c.binding.Spec, c.terminal.Output); err != nil {
				return err
			}
			if err = c.terminal.validateOrigin(c.record, c.binding, c.origin.attempt); err != nil {
				return err
			}
			c.finalRaw, err = encode(c.terminal)
			if err != nil {
				return err
			}
		}
		wrote = true
		return publishInstallTerminalFor(ctx, x, c.record.ID, c.origin.attempt, c.terminal, c.finalRaw)
	})
	err := commitError(result)
	if err != nil {
		if wrote && result.State() == f.Unknown {
			c.writeErr = err
			return err
		}
		if original != nil {
			return original
		}
		return err
	}
	c.writeErr = nil
	return nil
}

func (d *InstallDispatcher) joinColdInstall(ctx context.Context, b tc.ToolCallBinding) error {
	d.mu.Lock()
	c := d.cold[b.Identity()]
	var done chan struct{}
	if c != nil {
		done = c.done
	}
	d.mu.Unlock()
	if c == nil {
		return nil
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return d.recoverColdInstallOwner(ctx, b, c)
}
