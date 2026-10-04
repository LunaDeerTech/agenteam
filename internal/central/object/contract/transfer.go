package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type Runner struct{}
type Operation struct{}
type Transfer struct{}
type TransferEvidence struct{}
type RunnerID = foundation.ID[Runner]
type OperationID = foundation.ID[Operation]
type TransferID = foundation.ID[Transfer]
type EvidenceID = foundation.ID[TransferEvidence]

type TransferDirection string

const (
	TransferGET TransferDirection = "get"
	TransferPUT TransferDirection = "put"
)

type TransferManifest struct {
	data func() TransferManifestDetails
}
type TransferManifestDetails struct {
	MediaType string
	Length    int64
	SHA256    foundation.Digest
}

func NewTransferManifest(d TransferManifestDetails) (TransferManifest, error) {
	m, err := NormalizeMediaType(d.MediaType)
	if err != nil || m != d.MediaType || d.Length < 0 || d.Length > MaxObjectSize || d.SHA256.Validate() != nil {
		return TransferManifest{}, bad()
	}
	return TransferManifest{func() TransferManifestDetails { return d }}, nil
}
func (m TransferManifest) Validate() error {
	if m.data == nil {
		return bad()
	}
	return nil
}
func (m TransferManifest) Details() TransferManifestDetails {
	if m.data == nil {
		return TransferManifestDetails{}
	}
	return m.data()
}
func (m TransferManifest) Equal(other TransferManifest) bool {
	return m.Validate() == nil && other.Validate() == nil && m.Details() == other.Details()
}
func (m TransferManifest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "transfer_manifest") }
func (m TransferManifest) MarshalJSON() ([]byte, error) { return []byte(`"transfer_manifest"`), nil }
func (*TransferManifest) UnmarshalJSON([]byte) error    { return bad() }
func (m TransferManifest) LogValue() slog.Value         { return slog.StringValue("transfer_manifest") }

type TransferSpec struct{ data func() TransferSpecDetails }
type TransferSpecDetails struct {
	RunnerID         RunnerID
	OperationID      OperationID
	Direction        TransferDirection
	Owner            ObjectOwner
	ObjectID         ObjectID
	UploadCommand    *foundation.CommandMeta
	Manifest         TransferManifest
	ExpiresInSeconds int64
}

func NewTransferSpec(d TransferSpecDetails) (TransferSpec, error) {
	if d.RunnerID.Validate() != nil || d.OperationID.Validate() != nil || d.Owner.Validate() != nil || d.Owner.Details().ProjectID == "" {
		return TransferSpec{}, bad()
	}
	if d.ExpiresInSeconds == 0 {
		d.ExpiresInSeconds = 60
	}
	if d.ExpiresInSeconds < 1 || d.ExpiresInSeconds > 300 {
		return TransferSpec{}, bad()
	}
	switch d.Direction {
	case TransferGET:
		if d.ObjectID.Validate() != nil || d.UploadCommand != nil || d.Manifest.data != nil {
			return TransferSpec{}, bad()
		}
	case TransferPUT:
		if d.ObjectID != (ObjectID{}) || d.UploadCommand == nil || d.UploadCommand.Validate() != nil || d.Manifest.Validate() != nil {
			return TransferSpec{}, bad()
		}
	default:
		return TransferSpec{}, bad()
	}
	d = copyTransferSpec(d)
	return TransferSpec{func() TransferSpecDetails { return copyTransferSpec(d) }}, nil
}
func copyTransferCommand(c *foundation.CommandMeta) *foundation.CommandMeta {
	if c == nil {
		return nil
	}
	v := *c
	if v.ExpectedVersion != nil {
		n := *v.ExpectedVersion
		v.ExpectedVersion = &n
	}
	return &v
}
func copyTransferSpec(d TransferSpecDetails) TransferSpecDetails {
	d.UploadCommand = copyTransferCommand(d.UploadCommand)
	return d
}
func (s TransferSpec) Validate() error {
	if s.data == nil {
		return bad()
	}
	return nil
}
func (s TransferSpec) Details() TransferSpecDetails {
	if s.data == nil {
		return TransferSpecDetails{}
	}
	return s.data()
}
func (s TransferSpec) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "transfer_spec") }
func (s TransferSpec) MarshalJSON() ([]byte, error) { return []byte(`"transfer_spec"`), nil }
func (*TransferSpec) UnmarshalJSON([]byte) error    { return bad() }
func (s TransferSpec) LogValue() slog.Value         { return slog.StringValue("transfer_spec") }

type TransferEvidenceKind string

const (
	TransferCompletedEvidence TransferEvidenceKind = "completed"
	TransferStoppedEvidence   TransferEvidenceKind = "stopped"
)

type TransferEvidenceRef struct {
	ID   EvidenceID
	Kind TransferEvidenceKind
}

func (e TransferEvidenceRef) Validate() error {
	if e.ID.Validate() != nil || e.Kind != TransferCompletedEvidence && e.Kind != TransferStoppedEvidence {
		return bad()
	}
	return nil
}

type TransferState string

const (
	TransferPending  TransferState = "pending"
	TransferComplete TransferState = "complete"
	TransferFailed   TransferState = "failed"
	TransferUnknown  TransferState = "unknown"
)

type TransferStatusView struct {
	ID          TransferID         `json:"transfer_id"`
	RunnerID    RunnerID           `json:"runner_id"`
	OperationID OperationID        `json:"operation_id"`
	ObjectID    ObjectID           `json:"object_id"`
	Direction   TransferDirection  `json:"direction"`
	State       TransferState      `json:"state"`
	Version     foundation.Version `json:"version"`
	ExpiresAt   foundation.Instant `json:"expires_at"`
	Revoked     bool               `json:"revoked"`
	LeaseActive bool               `json:"lease_active"`
	Cleanup     CleanupState       `json:"cleanup_state"`
	Code        foundation.Code    `json:"code,omitempty"`
}
type TransferGrant struct {
	Status    TransferStatusView  `json:"status"`
	MediaType string              `json:"media_type"`
	ByteSize  foundation.Progress `json:"byte_size"`
	SHA256    foundation.Digest   `json:"sha256"`
	Material  TransferMaterial    `json:"-"`
}

type TransferMaterial struct{ data func() transferMaterial }
type transferMaterial struct {
	runner      RunnerID
	transfer    TransferID
	method, url string
	headers     http.Header
	expires     foundation.Instant
}
type RunnerTransferMaterial struct {
	Method        string
	URL           string
	Headers       http.Header
	WireExpiresAt foundation.Instant
}

// NewTransferMaterial is a trusted storage adapter constructor. This value is
// not an authorization. Only the issuing service may return it after its
// final current-authority transaction has committed.
func NewTransferMaterial(runner RunnerID, id TransferID, method, raw string, headers http.Header, expires foundation.Instant) (TransferMaterial, error) {
	u, err := url.Parse(raw)
	if runner.Validate() != nil || id.Validate() != nil || expires.Validate() != nil || err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" || len(raw) > 16<<10 || method != http.MethodGet && method != http.MethodPut {
		return TransferMaterial{}, bad()
	}
	for key, values := range headers {
		if strings.ContainsAny(key, "\r\n") {
			return TransferMaterial{}, bad()
		}
		for _, v := range values {
			if strings.ContainsAny(v, "\r\n") {
				return TransferMaterial{}, bad()
			}
		}
	}
	d := transferMaterial{runner, id, method, raw, headers.Clone(), expires}
	return TransferMaterial{func() transferMaterial { v := d; v.headers = d.headers.Clone(); return v }}, nil
}

// ForRunner is the explicit trusted D17-channel projection, never a model,
// transcript or log DTO. Runner authentication is performed by that adapter.
func (m TransferMaterial) ForRunner(runner RunnerID, transfer TransferID) (RunnerTransferMaterial, error) {
	if m.data == nil {
		return RunnerTransferMaterial{}, bad()
	}
	d := m.data()
	if runner != d.runner || transfer != d.transfer {
		return RunnerTransferMaterial{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return RunnerTransferMaterial{d.method, d.url, d.headers, d.expires}, nil
}
func (m TransferMaterial) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "private_transfer_material")
}
func (m TransferMaterial) MarshalJSON() ([]byte, error) {
	return []byte(`"private_transfer_material"`), nil
}
func (*TransferMaterial) UnmarshalJSON([]byte) error { return bad() }
func (m TransferMaterial) LogValue() slog.Value      { return slog.StringValue("private_transfer_material") }

type Transfers interface {
	IssueTransfer(context.Context, identity.Actor, foundation.CommandMeta, TransferSpec) (TransferGrant, error)
	InspectTransfer(context.Context, identity.Actor, TransferID) (TransferStatusView, error)
	CompleteTransfer(context.Context, identity.Actor, TransferID, TransferEvidenceRef) (TransferStatusView, error)
	CancelTransfer(context.Context, identity.Actor, TransferID, foundation.CommandMeta) (TransferStatusView, error)
	ConfirmStopped(context.Context, identity.Actor, TransferID, TransferEvidenceRef) (TransferStatusView, error)
}
