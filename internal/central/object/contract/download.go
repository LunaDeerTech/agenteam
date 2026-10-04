package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type DownloadGrant struct{}
type DownloadAttempt struct{}
type DownloadGrantID = foundation.ID[DownloadGrant]
type DownloadAttemptID = foundation.ID[DownloadAttempt]

type DownloadMode string

const (
	DownloadAttachment DownloadMode = "download"
	DownloadPreview    DownloadMode = "preview"
)

func (m DownloadMode) Valid() bool { return m == DownloadAttachment || m == DownloadPreview }

// DownloadTarget is a provider's captured business identity, not a permission.
// It contains no storage locator. The provider must revalidate it in each Tx.
type DownloadTarget struct{ data func() DownloadTargetDetails }
type DownloadTargetDetails struct {
	Source   ResolvedSource
	Filename string
}

func NewDownloadTarget(d DownloadTargetDetails) (DownloadTarget, error) {
	if d.Source.Validate() != nil || ValidateFilename(d.Filename) != nil {
		return DownloadTarget{}, bad()
	}
	return DownloadTarget{func() DownloadTargetDetails { return d }}, nil
}
func ValidateFilename(value string) error {
	if len(value) == 0 || len(value) > 255 || !utf8.ValidString(value) || strings.ContainsAny(value, `/\`) {
		return bad()
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return bad()
		}
	}
	return nil
}
func (t DownloadTarget) Validate() error {
	if t.data == nil {
		return bad()
	}
	return nil
}
func (t DownloadTarget) Details() DownloadTargetDetails {
	if t.data == nil {
		return DownloadTargetDetails{}
	}
	return t.data()
}
func (t DownloadTarget) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "download_target") }
func (t DownloadTarget) MarshalJSON() ([]byte, error) { return []byte(`"download_target"`), nil }
func (*DownloadTarget) UnmarshalJSON([]byte) error    { return bad() }
func (t DownloadTarget) LogValue() slog.Value         { return slog.StringValue("download_target") }

type DownloadPhase string

const (
	DownloadIssued  DownloadPhase = "issued"
	DownloadStarted DownloadPhase = "started"
	DownloadSent    DownloadPhase = "sent"
	DownloadFailed  DownloadPhase = "failed"
)

type DownloadFailure string

const (
	DownloadReadFailed      DownloadFailure = "read_failed"
	DownloadWriteFailed     DownloadFailure = "write_failed"
	DownloadCancelled       DownloadFailure = "cancelled"
	DownloadIntegrityFailed DownloadFailure = "integrity_failed"
)

type DownloadEvent struct {
	GrantID   DownloadGrantID
	AttemptID DownloadAttemptID
	Phase     DownloadPhase
	SentBytes foundation.Progress
	Length    foundation.Progress
	Failure   DownloadFailure
}

func (e DownloadEvent) Validate() error {
	if e.GrantID.Validate() != nil || e.SentBytes.Validate() != nil || e.Length.Validate() != nil || e.Length > foundation.Progress(MaxObjectSize) || e.SentBytes > e.Length {
		return bad()
	}
	if e.Phase == DownloadIssued {
		if e.AttemptID != (DownloadAttemptID{}) || e.SentBytes != 0 || e.Failure != "" {
			return bad()
		}
		return nil
	}
	if e.AttemptID.Validate() != nil {
		return bad()
	}
	switch e.Phase {
	case DownloadStarted:
		if e.SentBytes != 0 || e.Failure != "" {
			return bad()
		}
	case DownloadSent:
		if e.SentBytes != e.Length || e.Failure != "" {
			return bad()
		}
	case DownloadFailed:
		switch e.Failure {
		case DownloadReadFailed, DownloadWriteFailed, DownloadCancelled, DownloadIntegrityFailed:
		default:
			return bad()
		}
	default:
		return bad()
	}
	return nil
}

// Providers own the actual domain permissions and matching typed Audit action.
// Missing providers fail DependencyUnbound, including uploaded receipts owned
// by an unbound domain. These methods never infer permission from a signature.
// InTx calls consume the same already-acquired source plan/token and must not
// acquire locks or open another transaction. Append errors abort that Tx.
type DownloadProvider interface {
	ResolveDownload(context.Context, identity.Actor, BusinessFileRef) (DownloadTarget, error)
	ValidateDownloadInTx(context.Context, foundation.Tx, identity.Actor, DownloadTarget, AccessLockPlan, LockedAccess) error
	AppendDownloadInTx(context.Context, foundation.Tx, identity.Actor, DownloadTarget, DownloadEvent, AccessLockPlan, LockedAccess) error
}

type PrivateSignedURL struct{ data func() privateURL }
type privateURL struct {
	user    identity.UserID
	path    string
	expires foundation.Instant
}

// NewPrivateSignedURL is a trusted service adapter constructor. It does not
// make the token valid; OpenDownload always verifies the signature and DB grant.
func NewPrivateSignedURL(user identity.UserID, token string, expires foundation.Instant) (PrivateSignedURL, error) {
	if user.Validate() != nil || expires.Validate() != nil || len(token) == 0 || len(token) > 8<<10 {
		return PrivateSignedURL{}, bad()
	}
	for _, c := range []byte(token) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return PrivateSignedURL{}, bad()
		}
	}
	d := privateURL{user, "/api/object-downloads/" + token, expires}
	return PrivateSignedURL{func() privateURL { return d }}, nil
}

type HumanDownloadResponse struct {
	URL       string             `json:"url"`
	ExpiresAt foundation.Instant `json:"expires_at"`
}

// ForHuman is an explicit response-only projection. The issuing service has
// already checked current Session and business access; handlers must use
// private, no-store and must never log this response or place it in Agent data.
func (p PrivateSignedURL) ForHuman(actor identity.Actor) (HumanDownloadResponse, error) {
	if p.data == nil || actor.Validate() != nil || actor.Details().Kind != identity.Human || actor.Details().UserID != p.data().user.String() {
		return HumanDownloadResponse{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	d := p.data()
	return HumanDownloadResponse{d.path, d.expires}, nil
}
func (p PrivateSignedURL) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "private_download_url")
}
func (p PrivateSignedURL) MarshalJSON() ([]byte, error) { return []byte(`"private_download_url"`), nil }
func (*PrivateSignedURL) UnmarshalJSON([]byte) error    { return bad() }
func (p PrivateSignedURL) LogValue() slog.Value         { return slog.StringValue("private_download_url") }
