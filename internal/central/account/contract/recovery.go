package contract

import (
	"fmt"
	"io"
	"log/slog"
	"net/netip"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type ResetRequestFields struct {
	Browser  BrowserIdentity
	Key      foundation.IdempotencyKey
	Email    string
	ClientIP netip.Addr
}
type ResetRequest struct{ data func() ResetRequestFields }

func NewResetRequest(f ResetRequestFields) (ResetRequest, error) {
	if f.Browser.Validate() != nil || f.Key.Validate() != nil || !f.ClientIP.IsValid() || f.ClientIP.Zone() != "" {
		return ResetRequest{}, Invalid()
	}
	return ResetRequest{func() ResetRequestFields { return f }}, nil
}
func (r ResetRequest) Fields() ResetRequestFields {
	if r.data == nil {
		return ResetRequestFields{}
	}
	return r.data()
}
func (r ResetRequest) Validate() error {
	if r.data == nil {
		return Invalid()
	}
	return nil
}
func (r ResetRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "password_reset_request") }
func (r ResetRequest) MarshalJSON() ([]byte, error) { return []byte(`"password_reset_request"`), nil }
func (*ResetRequest) UnmarshalJSON([]byte) error    { return Invalid() }
func (r ResetRequest) LogValue() slog.Value         { return slog.StringValue("password_reset_request") }

type ResetAccepted struct {
	Accepted        bool   `json:"accepted"`
	DeliveryChannel string `json:"delivery_channel"`
}
type ResetInspection struct {
	Valid     bool               `json:"valid"`
	ExpiresAt foundation.Instant `json:"expires_at"`
}
type ResetCompleteFields struct {
	Browser                BrowserIdentity
	Key                    foundation.IdempotencyKey
	Token                  LinkToken
	Password, Confirmation sc.SecretMaterial
}
type ResetComplete struct{ data func() ResetCompleteFields }

func NewResetComplete(f ResetCompleteFields) (ResetComplete, error) {
	kind, _, _ := f.Token.Fields()
	if f.Browser.Validate() != nil || f.Key.Validate() != nil || kind != PasswordResetToken {
		return ResetComplete{}, Invalid()
	}
	return ResetComplete{func() ResetCompleteFields { return f }}, nil
}
func (r ResetComplete) Fields() ResetCompleteFields {
	if r.data == nil {
		return ResetCompleteFields{}
	}
	return r.data()
}
func (r ResetComplete) Validate() error {
	if r.data == nil {
		return Invalid()
	}
	return nil
}
func (r ResetComplete) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "password_reset_complete")
}
func (r ResetComplete) MarshalJSON() ([]byte, error) { return []byte(`"password_reset_complete"`), nil }
func (*ResetComplete) UnmarshalJSON([]byte) error    { return Invalid() }
func (r ResetComplete) LogValue() slog.Value         { return slog.StringValue("password_reset_complete") }

type PasswordChangeFields struct {
	Actor                               identity.Actor
	Key                                 foundation.IdempotencyKey
	ExpectedVersion                     foundation.Version
	OldPassword, Password, Confirmation sc.SecretMaterial
}
type PasswordChange struct{ data func() PasswordChangeFields }

func NewPasswordChange(f PasswordChangeFields) (PasswordChange, error) {
	if f.Actor.Validate() != nil || f.Actor.Details().Kind != identity.Human || f.Key.Validate() != nil || f.ExpectedVersion.Validate() != nil {
		return PasswordChange{}, Invalid()
	}
	return PasswordChange{func() PasswordChangeFields { return f }}, nil
}
func (r PasswordChange) Fields() PasswordChangeFields {
	if r.data == nil {
		return PasswordChangeFields{}
	}
	return r.data()
}
func (r PasswordChange) Validate() error {
	if r.data == nil {
		return Invalid()
	}
	return nil
}
func (r PasswordChange) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "password_change") }
func (r PasswordChange) MarshalJSON() ([]byte, error) { return []byte(`"password_change"`), nil }
func (*PasswordChange) UnmarshalJSON([]byte) error    { return Invalid() }
func (r PasswordChange) LogValue() slog.Value         { return slog.StringValue("password_change") }
