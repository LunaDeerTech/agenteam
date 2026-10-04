package contract

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type DeliveryIntent struct{}
type MailJob struct{}
type IntentID = foundation.ID[DeliveryIntent]
type JobID = foundation.ID[MailJob]

type InvitationCreateFields struct {
	Actor identity.Actor
	Key   foundation.IdempotencyKey
	Email string
}
type InvitationCreate struct{ data func() InvitationCreateFields }

func NewInvitationCreate(f InvitationCreateFields) (InvitationCreate, error) {
	if f.Actor.Validate() != nil || f.Actor.Details().Kind != identity.Human || f.Key.Validate() != nil {
		return InvitationCreate{}, Invalid()
	}
	return InvitationCreate{func() InvitationCreateFields { return f }}, nil
}
func (r InvitationCreate) Fields() InvitationCreateFields {
	if r.data == nil {
		return InvitationCreateFields{}
	}
	return r.data()
}
func (r InvitationCreate) Validate() error {
	if r.data == nil {
		return Invalid()
	}
	return nil
}
func (r InvitationCreate) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "invitation_create") }
func (r InvitationCreate) MarshalJSON() ([]byte, error) { return []byte(`"invitation_create"`), nil }
func (*InvitationCreate) UnmarshalJSON([]byte) error    { return Invalid() }
func (r InvitationCreate) LogValue() slog.Value         { return slog.StringValue("invitation_create") }

type InvitationReceipt struct {
	ID      InvitationID       `json:"id"`
	JobID   JobID              `json:"job_id"`
	Version foundation.Version `json:"version"`
}
type InvitationInspection struct {
	Email     string             `json:"email"`
	ExpiresAt foundation.Instant `json:"expires_at"`
}

func (r InvitationInspection) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "invitation_inspection")
}
func (r InvitationInspection) LogValue() slog.Value { return slog.StringValue("invitation_inspection") }

type TokenKind string

const (
	InvitationToken    TokenKind = "invitation"
	PasswordResetToken TokenKind = "password_reset"
)

type LinkToken struct {
	data func() (TokenKind, string, sc.SecretMaterial)
}

func NewInvitationToken(id InvitationID, value sc.SecretMaterial) (LinkToken, error) {
	if id.Validate() != nil {
		return LinkToken{}, Invalid()
	}
	return LinkToken{func() (TokenKind, string, sc.SecretMaterial) { return InvitationToken, id.String(), value }}, nil
}
func NewResetToken(id ResetID, value sc.SecretMaterial) (LinkToken, error) {
	if id.Validate() != nil {
		return LinkToken{}, Invalid()
	}
	return LinkToken{func() (TokenKind, string, sc.SecretMaterial) { return PasswordResetToken, id.String(), value }}, nil
}
func (t LinkToken) Fields() (TokenKind, string, sc.SecretMaterial) {
	if t.data == nil {
		return "", "", sc.SecretMaterial{}
	}
	return t.data()
}
func (t LinkToken) Validate() error {
	if t.data == nil {
		return Invalid()
	}
	return nil
}
func (t LinkToken) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_link_token") }
func (t LinkToken) MarshalJSON() ([]byte, error) { return []byte(`"account_link_token"`), nil }
func (*LinkToken) UnmarshalJSON([]byte) error    { return Invalid() }
func (t LinkToken) LogValue() slog.Value         { return slog.StringValue("account_link_token") }

type RedeemFields struct {
	Browser                BrowserIdentity
	Key                    foundation.IdempotencyKey
	Token                  LinkToken
	Username, DisplayName  string
	Password, Confirmation sc.SecretMaterial
}
type InvitationRedeem struct{ data func() RedeemFields }

func NewInvitationRedeem(f RedeemFields) (InvitationRedeem, error) {
	kind, _, _ := f.Token.Fields()
	if f.Browser.Validate() != nil || f.Key.Validate() != nil || kind != InvitationToken {
		return InvitationRedeem{}, Invalid()
	}
	return InvitationRedeem{func() RedeemFields { return f }}, nil
}
func (r InvitationRedeem) Fields() RedeemFields {
	if r.data == nil {
		return RedeemFields{}
	}
	return r.data()
}
func (r InvitationRedeem) Validate() error {
	if r.data == nil {
		return Invalid()
	}
	return nil
}
func (r InvitationRedeem) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "invitation_redeem") }
func (r InvitationRedeem) MarshalJSON() ([]byte, error) { return []byte(`"invitation_redeem"`), nil }
func (*InvitationRedeem) UnmarshalJSON([]byte) error    { return Invalid() }
func (r InvitationRedeem) LogValue() slog.Value         { return slog.StringValue("invitation_redeem") }

type InvitationRevoke struct {
	Actor           identity.Actor
	Key             foundation.IdempotencyKey
	ID              InvitationID
	ExpectedVersion foundation.Version
}
type RedemptionReceipt struct {
	Completed bool `json:"completed"`
}

// InvitationResend names the existing live link explicitly; its version is
// part of the command semantics and can never create a replacement link.
type InvitationResend struct {
	Actor           identity.Actor
	Key             foundation.IdempotencyKey
	ID              InvitationID
	ExpectedVersion foundation.Version
}
