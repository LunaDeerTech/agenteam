package secret

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// PrepareProjectVariableWrite runs outside the final business transaction. Its
// only possible durable change is the existing nonce-range reservation. The
// bound D10 provider has already discovered the opaque basis; CheckPlan verifies
// that exact plan without rediscovering or replacing a create's candidate Ref.
func (s *Service) PrepareProjectVariableWrite(ctx context.Context, intent sc.ProjectVariableIntent, plan sc.ProjectVariableWritePlan) (sc.PreparedProjectVariableWrite, error) {
	if ctx == nil || intent.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, unavailable(err)
	}
	authority, err := s.projectVariableAuthority()
	if err != nil {
		return nil, err
	}
	request := intent.Fields().Request
	if err = authority.CheckPlan(request, plan); err != nil {
		return nil, lookupError(err)
	}
	basis, err := plan.Basis()
	if err != nil {
		return nil, invalid()
	}
	locks, err := plan.RequiredLocks()
	if err != nil {
		return nil, invalid()
	}
	if err = s.writable(); err != nil {
		return nil, err
	}
	state := s.state()
	version, epoch, err := s.control(ctx, state.store)
	if err != nil {
		return nil, err
	}
	if version != state.keys.CurrentVersion() {
		return nil, failure(EpochChanged, f.InvalidState, nil)
	}
	digest, err := projectVariableIntentDigest(intent)
	if err != nil {
		return nil, err
	}
	defer clear(digest)
	var receiptID sc.ProjectVariableReceiptID
	if basis.Receipt.Observed() {
		result, err := basis.Receipt.Result()
		if err != nil {
			return nil, invalid()
		}
		receiptID = result.ReceiptID
	} else {
		receiptID, err = f.NewID[sc.ProjectVariableReceipt]()
		if err != nil {
			return nil, unavailable(err)
		}
	}
	payload, err := f.NewID[payloadMarker]()
	if err != nil {
		return nil, unavailable(err)
	}
	preparation, err := sc.NewProjectVariablePreparation(sc.ProjectVariablePreparationFields{Request: request, ReceiptID: receiptID, Ref: basis.Ref})
	if err != nil {
		return nil, invalid()
	}
	prepared := &projectVariablePreparedState{issuer: state, plan: plan, preparation: preparation, locks: locks, version: version, epoch: epoch}
	handle := &preparedProjectVariableWrite{data: func() *projectVariablePreparedState { return prepared }}
	keep := false
	defer func() {
		if !keep {
			handle.Destroy()
		}
	}()
	nonce, err := s.nextNonce(ctx, version)
	if err != nil {
		return nil, err
	}
	defer clear(nonce)
	prepared.receipt, err = seal(state.keys, version, nonce, basis.Ref.Details().Scope, projectVariableReceiptOwner, receiptID.String(), payload, digest)
	if err != nil {
		return nil, err
	}
	// An already-observed completed command only prepares comparison. Its
	// original value may now be replaced/deleted and must never be resealed as a
	// new business value. Metadata-only updates likewise create no value payload.
	metadata := intent.Fields()
	if !basis.Receipt.Observed() && metadata.ValuePresent {
		valueID, err := f.NewID[payloadMarker]()
		if err != nil {
			return nil, unavailable(err)
		}
		valueNonce, err := s.nextNonce(ctx, version)
		if err != nil {
			return nil, err
		}
		defer clear(valueNonce)
		err = intent.UseValue(func(value []byte) error {
			var sealErr error
			prepared.value, sealErr = seal(state.keys, version, valueNonce, basis.Ref.Details().Scope, valueOwner, basis.Ref.Details().ID.String(), valueID, value)
			return sealErr
		})
		if err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, unavailable(err)
	}
	keep = true
	return handle, nil
}
