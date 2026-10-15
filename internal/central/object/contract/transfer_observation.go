package contract

import (
	"context"
	"fmt"
	"io"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TransferObservationRequest identifies one original returned GET command.
// It is not proof of return or authorization; only the owning domain's private
// retained-call authority can provide those facts. It never admits signing,
// dispatch, body reads or a replacement command.
type TransferObservationRequest struct {
	data func() TransferObservationDetails
}
type TransferObservationDetails struct {
	Original       TransferAccessRequest
	EvidenceDigest f.Digest // Required only for the original ConfirmStopped proof.
}

func NewTransferObservationRequest(d TransferObservationDetails) (TransferObservationRequest, error) {
	if d.Original.Validate() != nil || d.Original.Details().Spec.Details().Direction != TransferGET {
		return TransferObservationRequest{}, bad()
	}
	switch d.Original.Details().Operation {
	case TransferIssue, TransferCancel:
		if d.EvidenceDigest != "" {
			return TransferObservationRequest{}, bad()
		}
	case TransferConfirmTerminal:
		if d.EvidenceDigest.Validate() != nil {
			return TransferObservationRequest{}, bad()
		}
	default:
		return TransferObservationRequest{}, bad()
	}
	return TransferObservationRequest{func() TransferObservationDetails { return d }}, nil
}
func (r TransferObservationRequest) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r TransferObservationRequest) Details() TransferObservationDetails {
	if r.data == nil {
		return TransferObservationDetails{}
	}
	return r.data()
}
func (r TransferObservationRequest) Equal(other TransferObservationRequest) bool {
	return r.Validate() == nil && other.Validate() == nil && r.Details().Original.Equal(other.Details().Original) && r.Details().EvidenceDigest == other.Details().EvidenceDigest
}
func (TransferObservationRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "transfer_observation")
}
func (TransferObservationRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"transfer_observation"`), nil
}
func (*TransferObservationRequest) UnmarshalJSON([]byte) error { return bad() }

// A nonzero observation is issued only after the original command lock and all
// owner/object/transfer/lease locks have been acquired and the original private
// authority revalidated. A zero value never represents settled absence.
type TransferCommandObservation struct {
	data func() TransferCommandObservationDetails
}
type TransferCommandObservationDetails struct {
	Request TransferObservationRequest
	Applied bool
	Status  *TransferStatusView // nil is allowed only for settled original Issue absence.
}

func NewTransferCommandObservation(d TransferCommandObservationDetails) (TransferCommandObservation, error) {
	if d.Request.Validate() != nil {
		return TransferCommandObservation{}, bad()
	}
	r := d.Request.Details().Original.Details()
	if d.Status == nil {
		if d.Applied || r.Operation != TransferIssue {
			return TransferCommandObservation{}, bad()
		}
	} else {
		s := *d.Status
		if s.ID != r.ID || s.ObjectID != r.ObjectID || s.RunnerID != r.Spec.Details().RunnerID || s.OperationID != r.Spec.Details().OperationID || s.Direction != TransferGET || s.Version.Validate() != nil || s.ExpiresAt.Validate() != nil || r.Operation == TransferIssue && !d.Applied {
			return TransferCommandObservation{}, bad()
		}
		d.Status = &s
	}
	return TransferCommandObservation{func() TransferCommandObservationDetails {
		out := d
		if d.Status != nil {
			s := *d.Status
			out.Status = &s
		}
		return out
	}}, nil
}
func (o TransferCommandObservation) SettledFor(r TransferObservationRequest) bool {
	return o.data != nil && o.data().Request.Equal(r)
}
func (o TransferCommandObservation) Details() TransferCommandObservationDetails {
	if o.data == nil {
		return TransferCommandObservationDetails{}
	}
	return o.data()
}

type TransferCommandObserver interface {
	LookupTransferCommand(context.Context, TransferObservationRequest) (TransferCommandObservation, error)
}

// Separate from RunnerTransferAuthority: observing a returned command cannot
// borrow the write grant for Issue/Cancel/Confirm or require current admission.
type TransferObservationAuthority interface {
	DiscoverTransferObservation(context.Context, TransferObservationRequest) (AccessDependencies, error)
	ValidateTransferObservationInTx(context.Context, f.Tx, TransferObservationRequest, AccessDependencies) (TransferObservationFacts, error)
}

// Returned only by the bound private authority in the original Store/Tx.
// These values compare the persisted original tuple; they grant no write.
type TransferObservationFacts struct {
	RunnerGeneration, OperationVersion f.Version
	ExecutionID                        id.ExecutionID
}

func (f TransferObservationFacts) Validate() error {
	if f.RunnerGeneration.Validate() != nil || f.OperationVersion.Validate() != nil || f.ExecutionID.Validate() != nil {
		return bad()
	}
	return nil
}
