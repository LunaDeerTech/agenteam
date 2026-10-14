//go:build integration && linux

package projectvariable_test

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"testing"
)

// This first slice uses the real current Execution/Agent/Mount/Tool owners and
// the actual Runner wire. It verifies operation reservation and reuse, not the
// separate Human decision, waiting recovery or cancellation acceptance.
func TestApprovalOperationReservation(t *testing.T) {
	v := newOperationCommandFixture(t)
	v.expectedRequests = 1
	executor, _, store := v.executor(t, false, false)
	execution := v.facts.capture.created.ID
	binding, err := v.facts.driver.ReadToolCall(ctxFor(t), execution)
	readFileAdapterOK(t, err, "original command binding")
	first, err := executor.ReserveApprovalOperation(ctxFor(t), binding)
	readFileAdapterOK(t, err, "reserve original command")
	again, err := executor.ReserveApprovalOperation(ctxFor(t), binding)
	readFileAdapterOK(t, err, "observe same command reservation")
	unattempted := func() error {
		cause, err := f.NewRecoveryCause("approval-reservation-test", execution.String(), "")
		readFileAdapterOK(t, err, "cold wait proof cause")
		result := store.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
			if err := store.AcquireAll(ctx, tx, first.RequiredLocks()); err != nil {
				return err
			}
			return executor.RequireUnattemptedApprovalOperationInTx(ctx, tx, first)
		})
		if result.State() != f.Committed {
			if result.State() != f.NotCommitted || result.Fault() == nil {
				t.Fatal("cold waiting proof lost original transaction outcome")
			}
			return result.Fault()
		}
		return nil
	}
	readFileAdapterOK(t, unattempted(), "Tool-owned cold waiting zero-attempt proof")
	if first.OperationID != again.OperationID || first.Fingerprint != again.Fingerprint {
		t.Fatal("reservation changed original operation")
	}
	var count, attempts int64
	var phase string
	err = v.facts.capture.v.base.raw.QueryRow(ctxFor(t), `SELECT count(*),min(phase),(SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$1) FROM agenteam_tool.operations WHERE execution_id=$1`, execution.String()).Scan(&count, &phase, &attempts)
	readFileAdapterOK(t, err, "reserved operation observation")
	frames, valid := v.wire.snapshot()
	if count != 1 || phase != "created" || attempts != 0 || len(frames) != 0 || !valid {
		t.Fatal("reservation created a side effect or duplicate")
	}
	forged := binding
	forged.CanonicalArguments = oc.DigestBytes([]byte("different argv"))
	_, err = executor.ReserveApprovalOperation(ctxFor(t), forged)
	if err == nil {
		t.Fatal("changed arguments reserved under original operation")
	}
	done := startOperationCommand(t, executor, execution, store)
	pending := v.sql(t)
	if pending.operation != first.OperationID.String() || pending.fingerprint != string(first.Fingerprint) {
		t.Fatal("Execute replaced approved logical operation")
	}
	v.requireWire(t, 0, pending)
	requireCode(t, unattempted(), f.ConfirmationStale)
	store.resume()
	completed := waitOperationCommand(t, done)
	readFileAdapterOK(t, completed.err, "original command completed")
	final := v.sql(t)
	requireOperationCommandIdentity(t, pending, final)
	v.requireTerminal(t, final, true)
	v.requireWire(t, 1, final)
	observed, found, err := executor.LookupRunCommand(ctxFor(t), execution)
	readFileAdapterOK(t, err, "same operation lookup")
	if !found || observed.OperationID() != first.OperationID || observed.AttemptID().Validate() != nil {
		t.Fatal("lookup lost original operation")
	}
	_, err = executor.ReserveApprovalOperation(ctxFor(t), binding)
	requireCode(t, err, f.ConfirmationStale)
	v.requireWire(t, 1, v.sql(t))
}
