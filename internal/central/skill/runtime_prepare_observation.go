package skill

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func (a *PreparationAuthority) observationWitness(ctx context.Context, r oc.TransferObservationRequest) (*preparationWitness, error) {
	w, err := a.witness(ctx)
	if err != nil {
		return nil, err
	}
	if r.Validate() != nil || !w.joining || !w.returned || !w.materialReleased || w.dispatch != nil && !w.dispatch.Retirement() {
		return nil, fault(f.Forbidden)
	}
	d := r.Details()
	var original oc.TransferAccessRequest
	switch d.Original.Details().Operation {
	case oc.TransferIssue:
		if w.issueError == nil {
			return nil, fault(f.Forbidden)
		}
		original = w.issueRequest
	case oc.TransferCancel:
		if w.cancelUnknown == nil {
			return nil, fault(f.Forbidden)
		}
		original = w.cancelRequest
	case oc.TransferConfirmTerminal:
		if w.cleanupUnknown == nil || d.EvidenceDigest != w.retirementDigest {
			return nil, fault(f.Forbidden)
		}
		original = w.stopRequest
	default:
		return nil, fault(f.Forbidden)
	}
	if !d.Original.Equal(original) {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (a *PreparationAuthority) DiscoverTransferObservation(ctx context.Context, r oc.TransferObservationRequest) (oc.AccessDependencies, error) {
	w, err := a.observationWitness(ctx, r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	digest, _, err := captureDigest([]any{w.invocation.Invocation.OperationID, w.invocation.Invocation.AttemptID, string(w.invocation.RequestID), r.Details().Original.Fingerprint(), string(r.Details().EvidenceDigest)})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest, w.locks)
}
func (a *PreparationAuthority) ValidateTransferObservationInTx(ctx context.Context, tx f.Tx, r oc.TransferObservationRequest, plan oc.AccessDependencies) (oc.TransferObservationFacts, error) {
	actual, err := a.DiscoverTransferObservation(ctx, r)
	if err != nil {
		return oc.TransferObservationFacts{}, err
	}
	if !actual.Equal(plan) {
		return oc.TransferObservationFacts{}, fault(f.Forbidden)
	}
	w, err := a.observationWitness(ctx, r)
	if err != nil {
		return oc.TransferObservationFacts{}, err
	}
	d := r.Details().Original.Details()
	if _, err = a.core.preparationObjectAuthorizationInTx(ctx, tx, d.Actor, d.Spec.Details().Owner, id.Converge); err != nil {
		return oc.TransferObservationFacts{}, err
	}
	return oc.TransferObservationFacts{RunnerGeneration: w.runnerGeneration, OperationVersion: w.operationVersion, ExecutionID: w.invocation.Invocation.ExecutionID}, nil
}

var _ oc.TransferObservationAuthority = (*PreparationAuthority)(nil)
