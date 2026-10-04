package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type TransferOperation string

const (
	TransferIssue            TransferOperation = "issue"
	TransferMaterialize      TransferOperation = "material"
	TransferInspect          TransferOperation = "inspect"
	TransferCapture          TransferOperation = "capture"
	TransferReserveCandidate TransferOperation = "reserve_candidate"
	TransferPublish          TransferOperation = "publish"
	TransferCancel           TransferOperation = "cancel"
	TransferConfirmTerminal  TransferOperation = "confirm_terminal"
	TransferCleanup          TransferOperation = "cleanup"
)

type TransferAccessRequest struct {
	data        func() TransferAccessDetails
	fingerprint func() string
}
type TransferAccessDetails struct {
	Operation              TransferOperation
	Actor                  identity.Actor
	IssueCommand           foundation.CommandMeta
	Spec                   TransferSpec
	ID                     TransferID
	ObjectID               ObjectID
	UploadID               UploadID
	StagingID, CandidateID AttemptID
	LeaseID, SourceLeaseID LeaseID
	Manifest               TransferManifest
	Evidence               *TransferEvidenceRef
	CancelCommand          *foundation.CommandMeta
	CleanupCause           CleanupID
	Prepared               PreparedPayload
	Version                foundation.Version
}

func NewTransferAccessRequest(d TransferAccessDetails) (TransferAccessRequest, error) {
	if d.Actor.Validate() != nil || d.IssueCommand.Validate() != nil || d.Spec.Validate() != nil || d.ID.Validate() != nil || d.ObjectID.Validate() != nil || d.Manifest.Validate() != nil {
		return TransferAccessRequest{}, bad()
	}
	switch d.Operation {
	case TransferIssue, TransferMaterialize, TransferInspect, TransferCapture, TransferReserveCandidate, TransferPublish, TransferCancel, TransferConfirmTerminal, TransferCleanup:
	default:
		return TransferAccessRequest{}, bad()
	}
	if d.Operation == TransferIssue {
		if d.Version != 0 {
			return TransferAccessRequest{}, bad()
		}
	} else if d.Version.Validate() != nil {
		return TransferAccessRequest{}, bad()
	}
	if d.Operation == TransferCapture || d.Operation == TransferReserveCandidate || d.Operation == TransferPublish || d.Operation == TransferConfirmTerminal {
		if d.Evidence == nil || d.Evidence.Validate() != nil {
			return TransferAccessRequest{}, bad()
		}
		if d.Operation != TransferConfirmTerminal && d.Evidence.Kind != TransferCompletedEvidence {
			return TransferAccessRequest{}, bad()
		}
	} else if d.Evidence != nil {
		return TransferAccessRequest{}, bad()
	}
	if d.Operation == TransferCancel {
		if d.CancelCommand == nil || d.CancelCommand.Validate() != nil {
			return TransferAccessRequest{}, bad()
		}
	} else if d.CancelCommand != nil {
		return TransferAccessRequest{}, bad()
	}
	if d.Operation == TransferCleanup {
		if d.CleanupCause.Validate() != nil {
			return TransferAccessRequest{}, bad()
		}
	} else if d.CleanupCause != (CleanupID{}) {
		return TransferAccessRequest{}, bad()
	}
	if d.Operation == TransferReserveCandidate {
		if d.Prepared.Validate() != nil {
			return TransferAccessRequest{}, bad()
		}
	} else if d.Prepared.data != nil {
		return TransferAccessRequest{}, bad()
	}
	if d.Spec.Details().Direction == TransferGET {
		if d.ObjectID != d.Spec.Details().ObjectID || d.UploadID != (UploadID{}) || d.StagingID != (AttemptID{}) || d.CandidateID != (AttemptID{}) || d.SourceLeaseID != (LeaseID{}) || d.Operation == TransferReserveCandidate {
			return TransferAccessRequest{}, bad()
		}
	} else {
		if !d.Manifest.Equal(d.Spec.Details().Manifest) || d.UploadID.Validate() != nil || d.StagingID.Validate() != nil {
			return TransferAccessRequest{}, bad()
		}
	}
	if d.CandidateID != (AttemptID{}) && d.CandidateID.Validate() != nil || d.LeaseID.Validate() != nil || d.SourceLeaseID != (LeaseID{}) && d.SourceLeaseID.Validate() != nil {
		return TransferAccessRequest{}, bad()
	}
	d = copyTransferAccess(d)
	f := transferFingerprint(d)
	return TransferAccessRequest{func() TransferAccessDetails { return copyTransferAccess(d) }, func() string { return f }}, nil
}
func copyTransferAccess(d TransferAccessDetails) TransferAccessDetails {
	d.IssueCommand = *copyTransferCommand(&d.IssueCommand)
	d.CancelCommand = copyTransferCommand(d.CancelCommand)
	if d.Evidence != nil {
		e := *d.Evidence
		d.Evidence = &e
	}
	return d
}
func transferCommandProjection(c *foundation.CommandMeta) any {
	if c == nil {
		return nil
	}
	expected := ""
	if c.ExpectedVersion != nil {
		expected = c.ExpectedVersion.String()
	}
	return []any{c.RequestID.String(), string(c.IdempotencyKey), expected}
}
func TransferSpecProjection(spec TransferSpec) any {
	d := spec.Details()
	m := d.Manifest.Details()
	return []any{d.RunnerID.String(), d.OperationID.String(), string(d.Direction), d.Owner.Details(), d.ObjectID.String(), transferCommandProjection(d.UploadCommand), m.MediaType, m.Length, m.SHA256.String(), d.ExpiresInSeconds}
}
func transferFingerprint(d TransferAccessDetails) string {
	m := d.Manifest.Details()
	p := d.Prepared.Details()
	var evidence any
	if d.Evidence != nil {
		evidence = []any{d.Evidence.ID.String(), d.Evidence.Kind}
	}
	b, err := json.Marshal([]any{d.Operation, d.Actor.Details(), transferCommandProjection(&d.IssueCommand), TransferSpecProjection(d.Spec), d.ID.String(), d.ObjectID.String(), d.UploadID.String(), d.StagingID.String(), d.CandidateID.String(), d.LeaseID.String(), d.SourceLeaseID.String(), m.MediaType, m.Length, m.SHA256.String(), evidence, transferCommandProjection(d.CancelCommand), d.CleanupCause.String(), p.ID.String(), p.MediaType, p.Length, p.SHA256.String(), int64(d.Version)})
	if err != nil {
		panic("primitive transfer request")
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (r TransferAccessRequest) Validate() error {
	if r.data == nil || r.fingerprint == nil {
		return bad()
	}
	return nil
}
func (r TransferAccessRequest) Details() TransferAccessDetails {
	if r.data == nil {
		return TransferAccessDetails{}
	}
	return r.data()
}
func (r TransferAccessRequest) Fingerprint() string {
	if r.fingerprint == nil {
		return ""
	}
	return r.fingerprint()
}
func (r TransferAccessRequest) Equal(other TransferAccessRequest) bool {
	return r.Validate() == nil && other.Validate() == nil && r.Fingerprint() == other.Fingerprint()
}
func (r TransferAccessRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "transfer_access")
}
func (r TransferAccessRequest) MarshalJSON() ([]byte, error) { return []byte(`"transfer_access"`), nil }
func (*TransferAccessRequest) UnmarshalJSON([]byte) error    { return bad() }
func (r TransferAccessRequest) LogValue() slog.Value         { return slog.StringValue("transfer_access") }

// Completion and retirement are independent facts. Completed never asserts
// that another request using the same bearer has stopped or cannot begin.
type TransferCompletion struct {
	Evidence TransferEvidenceRef
	Digest   foundation.Digest
	Length   int64
	SHA256   foundation.Digest
}
type TransferRetirement struct {
	Evidence                                  TransferEvidenceRef
	Digest                                    foundation.Digest
	AllRequestsJoined, StorageAdmissionClosed bool
}
type TransferAuthorizationDetails struct {
	Request                            TransferAccessRequest
	RunnerGeneration, OperationVersion foundation.Version
	ExecutionID                        identity.ExecutionID
	Completed                          *TransferCompletion
	Retirement                         *TransferRetirement
}
type TransferAuthorization struct {
	data func() TransferAuthorizationDetails
}

func NewTransferAuthorization(d TransferAuthorizationDetails) (TransferAuthorization, error) {
	if d.Request.Validate() != nil || d.RunnerGeneration.Validate() != nil || d.OperationVersion.Validate() != nil || d.ExecutionID.Validate() != nil {
		return TransferAuthorization{}, bad()
	}
	r := d.Request.Details()
	m := r.Manifest.Details()
	if c := d.Completed; c != nil {
		if r.Evidence == nil || c.Evidence != *r.Evidence || c.Evidence.Kind != TransferCompletedEvidence || c.Digest.Validate() != nil || c.Length != m.Length || c.SHA256 != m.SHA256 {
			return TransferAuthorization{}, bad()
		}
	}
	if c := d.Retirement; c != nil {
		if r.Evidence == nil || c.Evidence != *r.Evidence || c.Evidence.Validate() != nil || c.Digest.Validate() != nil || !c.AllRequestsJoined || !c.StorageAdmissionClosed {
			return TransferAuthorization{}, bad()
		}
	}
	d = copyTransferAuthorization(d)
	return TransferAuthorization{func() TransferAuthorizationDetails { return copyTransferAuthorization(d) }}, nil
}
func copyTransferAuthorization(d TransferAuthorizationDetails) TransferAuthorizationDetails {
	if d.Completed != nil {
		v := *d.Completed
		d.Completed = &v
	}
	if d.Retirement != nil {
		v := *d.Retirement
		d.Retirement = &v
	}
	return d
}
func (a TransferAuthorization) Matches(r TransferAccessRequest) bool {
	return a.data != nil && a.data().Request.Equal(r)
}
func (a TransferAuthorization) Details() TransferAuthorizationDetails {
	if a.data == nil {
		return TransferAuthorizationDetails{}
	}
	return a.data()
}
func (a TransferAuthorization) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "transfer_authorization")
}
func (a TransferAuthorization) MarshalJSON() ([]byte, error) {
	return []byte(`"transfer_authorization"`), nil
}
func (*TransferAuthorization) UnmarshalJSON([]byte) error { return bad() }
func (a TransferAuthorization) LogValue() slog.Value {
	return slog.StringValue("transfer_authorization")
}

type RunnerTransferAuthority interface {
	Discover(context.Context, TransferAccessRequest) (AccessDependencies, error)
	ValidateInTx(context.Context, foundation.Tx, TransferAccessRequest, AccessDependencies) (TransferAuthorization, error)
}
