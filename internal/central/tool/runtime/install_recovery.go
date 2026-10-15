package runtime

import (
	"bytes"
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// An observer error is not a new write. Only the original retained handoff can
// replace the dispatcher's uncertain result, after a known terminal and its
// validated bytes have reached another final Tool write. In particular, do not
// join the already-settled publication/final-write Unknown ahead of that write's
// current physical Attempt.
func (d *InstallDispatcher) recordInstallRecoveryError(call *installDispatchCall, original error) error {
	err := d.retainedInstallWriteError(call, original)
	d.setCallError(call, err)
	return err
}

func (d *InstallDispatcher) retainedInstallWriteError(call *installDispatchCall, original error) error {
	d.mu.Lock()
	current := call != nil && d.calls[call.binding.Identity()] == call
	var operation tc.OperationID
	if current {
		operation = call.operation
	}
	d.mu.Unlock()
	if !current || d.executor == nil || d.executor.data == nil {
		return original
	}
	e := d.executor.data()
	a := e.source.authority.data()
	a.mu.Lock()
	h := a.active[operation]
	a.mu.Unlock()
	if h == nil || h.executor != e || !h.boundExecutor(a) || h.record.ID != operation || !sameRunnerCallBinding(h.binding, call.binding) {
		return original
	}
	h.recoveryMu.Lock()
	defer h.recoveryMu.Unlock()
	select {
	case <-h.done:
	default:
		return original
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.live || h.terminal == nil || !h.terminal.Known || h.terminal.validate(h) != nil || len(h.finalRaw) == 0 {
		return original
	}
	raw, err := encode(h.terminal.clone())
	if err != nil || !bytes.Equal(raw, h.finalRaw) {
		return original
	}
	var written CommitFailure
	if !errors.As(h.retirement, &written) {
		return original
	}
	result := written.Result()
	cause, err := inputCause(call.binding)
	if err != nil || result.State() != f.Unknown || result.AttemptID().Validate() != nil || result.Cause().Validate() != nil {
		return original
	}
	facts := result.Cause().Details()
	if facts.Kind != f.CommandsCause || facts.Primary.Canonical() != cause.Details().Primary.Canonical() || len(facts.Related) != 0 {
		return original
	}
	return h.retirement
}

// recoverReturnedInstall borrows only a retained handoff from this dispatcher.
// Neither a ToolCallBinding nor a canonical receipt creates a recovery owner.
func (d *InstallDispatcher) recoverReturnedInstall(ctx context.Context, b tc.ToolCallBinding) error {
	if err := d.valid(b); err != nil {
		return err
	}
	d.mu.Lock()
	call := d.calls[b.Identity()]
	var operation tc.OperationID
	if call != nil {
		operation = call.operation
	}
	d.mu.Unlock()
	if call == nil {
		return nil // Ordinary durable lookup, not cold recovery.
	}
	if !sameRunnerCallBinding(call.binding, b) {
		return fail(f.Forbidden)
	}
	a := d.executor.data().source.authority.data()
	a.mu.Lock()
	h := a.active[operation]
	a.mu.Unlock()
	if h == nil {
		return nil
	}
	if !sameRunnerCallBinding(h.binding, b) {
		return fail(f.Forbidden)
	}
	select {
	case <-h.done:
	default:
		return fail(f.ResourceBusy)
	}
	if err := recoverInstallFinalization(ctx, a, h); err != nil {
		return err
	}
	d.setCallError(call, nil)
	return nil
}

// recoverInstallFinalization never restarts installation. The original
// synchronous executor must have returned. An uncertain publication first needs
// the original canonical receipt; a known backend result instead needs its
// uncertain final Tool transaction observed. Taking that complete lock set
// waits for its actual database transaction to settle; only the exact original
// running/v2 tuple then proves that the final write did not commit. Missing or
// changed rows do not provide that proof.
func recoverInstallFinalization(ctx context.Context, d *installAuthorityState, h *installHandoff) error {
	if ctx == nil {
		return fail(f.InvalidArgument)
	}
	if d == nil || h == nil || h.issuer != d || !h.boundExecutor(d) {
		return fail(f.Forbidden)
	}
	h.recoveryMu.Lock()
	defer h.recoveryMu.Unlock()
	d.mu.Lock()
	active := d.active[h.record.ID] == h
	d.mu.Unlock()
	if !active {
		h.mu.Lock()
		retired := h.retirement == nil
		h.mu.Unlock()
		if retired {
			return nil
		}
		return fail(f.Forbidden)
	}
	select {
	case <-h.done:
	default:
		return fail(f.ResourceBusy)
	}
	h.mu.Lock()
	err := h.retirement
	terminal := h.terminal
	finalRaw := bytes.Clone(h.finalRaw)
	live := h.live
	h.mu.Unlock()
	provenance := installUnknown(err)
	if !live && provenance != nil && terminal != nil && !terminal.Known {
		return recoverInstallPublicationLocked(ctx, d, h, err)
	}
	if live || provenance == nil || provenance.Kind != h.cause.Details().Kind || provenance.Primary != h.cause.Details().Primary.Canonical() || terminal == nil || !terminal.Known || terminal.validate(h) != nil || len(finalRaw) == 0 {
		if err != nil {
			return err
		}
		return fail(f.ResourceBusy)
	}
	if ctx.Err() != nil {
		return err // No observation can replace the original uncertain write.
	}
	// A private copy survives a rolled-back or Unknown recovery transaction.
	// It is the original backend's result, never a newly reconstructed receipt.
	want := terminal.clone()
	raw, encodeErr := encode(want)
	if encodeErr != nil || !bytes.Equal(raw, finalRaw) {
		return fail(f.InvalidState)
	}
	attemptedFinalWrite := false
	result := d.store.WithinTx(ctx, h.cause, func(ctx context.Context, tx f.Tx) error {
		if err := d.store.AcquireAll(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		if err := d.store.RequireHeldLocks(ctx, tx, h.locks); err != nil {
			return portError(err)
		}
		if err := d.guard.RequireCurrentProcessInTx(ctx, tx, d.process); err != nil {
			return portError(err)
		}
		if _, err := h.executor.core.data().store.InTx(tx); err != nil {
			return portError(err)
		}
		x, err := d.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		settled, err := observeInstallFinalization(ctx, x, d, h, want)
		if err != nil || settled {
			return err
		}
		// The observer checked the exact running Attempt under this same full
		// lock set, after the original WithinTx returned. This is a determined
		// rollback of finalization, not permission inferred from Unknown.
		attemptedFinalWrite = true
		return publishInstallTerminal(ctx, x, h, &want, finalRaw)
	})
	if recoveryErr := commitError(result); recoveryErr != nil {
		if !attemptedFinalWrite {
			// Read/cancellation failures and an uncertain read-only COMMIT do
			// not determine the original final write's outcome.
			return err
		}
		if result.State() == f.Unknown {
			// This was a new physical finalization after the exact original
			// rollback was proven. Preserve its own Attempt for later lookup.
			h.mu.Lock()
			h.retirement = recoveryErr
			h.mu.Unlock()
		}
		return recoveryErr
	}
	h.mu.Lock()
	h.retirement = nil
	h.mu.Unlock()
	d.mu.Lock()
	if d.active[h.record.ID] == h {
		delete(d.active, h.record.ID)
	}
	d.mu.Unlock()
	return nil
}

// The false result is deliberately narrow: the exact original running tuple,
// never an absent operation/attempt or a different terminal result.
func observeInstallFinalization(ctx context.Context, x postgres.SQLExecutor, d *installAuthorityState, h *installHandoff, want installTerminal) (bool, error) {
	observed, err := loadInstallObserved(ctx, x, h.binding)
	if err != nil {
		return false, err
	}
	if observed == nil || observed.record.ID != h.record.ID || observed.record.InputDigest != h.record.InputDigest || observed.record.Fingerprint != h.record.Fingerprint || observed.record.Key != h.record.Key || observed.record.SkillID != h.record.SkillID {
		return false, fail(f.ConfirmationStale)
	}
	if observed.record.Version == 2 && observed.record.Phase == "running" {
		return false, requireRunningInstall(ctx, x, d, h)
	}
	raw, err := encode(want)
	if err != nil {
		return false, err
	}
	if observed.record.Version != 3 || observed.attempt != h.attempt || string(observed.request) != h.request.String() || observed.process != d.process.String() || !bytes.Equal(observed.raw, raw) {
		return false, fail(f.ConfirmationStale)
	}
	return true, nil
}
