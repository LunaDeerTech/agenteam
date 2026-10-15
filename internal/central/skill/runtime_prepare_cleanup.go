package skill

import (
	"bytes"
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func preparationUncertain(err error) bool {
	if err == nil {
		return false
	}
	var faultValue *f.Fault
	return !errors.As(err, &faultValue) || faultValue.CommitState == f.Unknown || faultValue.CommitState == f.Committed
}

func preparationCommandUnknown(err error) error {
	if preparationUncertain(err) {
		return err
	}
	// Transfer mutators also read status after committing. Their returned
	// error alone cannot distinguish that read failure from a pre-commit error.
	return f.NewFault(f.CommitUnknown, f.Unknown).WithCause(err)
}

func (s *Preparer) observePreparationTransfer(ctx context.Context, original oc.TransferAccessRequest, digest f.Digest, unknown error) (oc.TransferCommandObservationDetails, error) {
	r, err := oc.NewTransferObservationRequest(oc.TransferObservationDetails{Original: original, EvidenceDigest: digest})
	if err != nil {
		return oc.TransferCommandObservationDetails{}, unknown
	}
	out, err := s.state().observer.LookupTransferCommand(ctx, r)
	if err != nil || !out.SettledFor(r) {
		return oc.TransferCommandObservationDetails{}, unknown
	}
	return out.Details(), nil
}

// Re-entering Join after an Unknown only observes that original command. In
// particular, a rolled-back Cancel/Confirm is not automatically reapplied.
func (s *Preparer) retirePreparationTransfer(ctx context.Context, w *preparationWitness) error {
	if w.transfer.Validate() != nil {
		return nil
	} // No Issue authority/transaction was reached.
	var status oc.TransferStatusView
	var err error
	if w.cleanupUnknown != nil {
		out, e := s.observePreparationTransfer(ctx, w.stopRequest, w.retirementDigest, w.cleanupUnknown)
		if e != nil {
			return e
		}
		if !out.Applied || out.Status == nil || out.Status.LeaseActive {
			return w.cleanupUnknown
		}
		if w.issueError != nil {
			return s.prepareUnissuedResult(w)
		}
		return nil
	}
	if w.cancelUnknown != nil {
		out, e := s.observePreparationTransfer(ctx, w.cancelRequest, "", w.cancelUnknown)
		if e != nil {
			return e
		}
		if !out.Applied || out.Status == nil {
			return w.cancelUnknown
		}
		status = *out.Status
		w.cancelled = true
		w.cancelUnknown = nil
	} else if w.issueError != nil {
		out, e := s.observePreparationTransfer(ctx, w.issueRequest, "", w.issueError)
		if e != nil {
			return e
		}
		if out.Status == nil {
			return s.prepareUnissuedResult(w)
		}
		if !out.Applied {
			return w.issueError
		}
		status = *out.Status
	} else {
		status, err = s.state().transfers.InspectTransfer(ctx, w.invocation.Invocation.Actor, w.transfer)
		if err != nil {
			return err
		}
	}
	if status.LeaseActive {
		if !w.completed && !w.cancelled {
			_, err = s.state().transfers.CancelTransfer(ctx, w.invocation.Invocation.Actor, w.transfer, w.cancelCommand)
			if err != nil {
				w.cancelUnknown = preparationCommandUnknown(err)
				return w.cancelUnknown
			}
			w.cancelled = true
		}
		w.retirementDigest, _, err = captureDigest([]any{w.invocation.Invocation.OperationID, w.invocation.Invocation.AttemptID, string(w.invocation.RequestID), w.transfer.String(), w.metadata.Object.ID.String(), "original-handler-joined"})
		if err != nil {
			return err
		}
		status, err = s.state().transfers.ConfirmStopped(ctx, w.invocation.Invocation.Actor, w.transfer, w.retirement)
		if err != nil {
			w.cleanupUnknown = preparationCommandUnknown(err)
			return w.cleanupUnknown
		}
		if status.LeaseActive {
			// The original confirmation committed but retirement is still pending.
			// Keep that command for read-only observation on the next Join.
			w.cleanupUnknown = fault(f.ResourceBusy)
			return w.cleanupUnknown
		}
	}
	if w.issueError != nil {
		return s.prepareUnissuedResult(w)
	}
	return nil
}

func (s *Preparer) prepareUnissuedResult(w *preparationWitness) error {
	if !w.lookupNeeded {
		return nil
	}
	// Issue never returned material, so this retained call never dispatched.
	if w.issued || w.terminal != nil {
		return fault(f.InvalidState)
	}
	r := sc.PrepareUnissuedResult{SkillID: w.binding.SkillID, RevisionID: w.binding.RevisionID, Revision: w.binding.Revision, MountID: w.args.MountID, Outcome: "failure"}
	r.Error.Code = f.DependencyUnavailable
	var err error
	w.resultBody, err = sc.EncodePrepareUnissuedResult(r)
	return err
}

// Lookup consumes only an original, same-process, returned result after all
// physical owners joined. The Runtime adapter supplies its retained private
// cleanup witness; there is no cold receipt, new admission or re-dispatch.
func (s *Preparer) Lookup(ctx context.Context, inv sc.PreparationInvocation) ([]byte, bool, error) {
	if s.state() == nil || inv.Validate() != nil {
		return nil, false, invalid()
	}
	if err := s.state().authority.runtime.(sc.InvocationRetirementAuthority).RequireSkillInvocationRetirement(ctx, inv.Invocation); err != nil {
		return nil, false, portError(err)
	}
	key := preparationKeyFor(inv)
	s.state().mu.Lock()
	w := s.state().pending[key]
	s.state().mu.Unlock()
	if w == nil {
		return nil, false, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.resourcesJoined || !w.lookupNeeded || !w.returned || len(w.resultBody) == 0 {
		return nil, false, fault(f.ResourceBusy)
	}
	out := bytes.Clone(w.resultBody)
	clear(w.resultBody)
	s.state().mu.Lock()
	delete(s.state().pending, key)
	s.state().mu.Unlock()
	return out, true, nil
}
