package contract

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type ChallengeRecord struct{}
type ChallengeID = foundation.ID[ChallengeRecord]

// ChallengeRequest is a trusted browser adapter input. It cannot be decoded
// directly from untrusted JSON because Browser is a verified capability.
type ChallengeRequest struct{ data func() ChallengeFields }
type ChallengeFields struct {
	Browser  BrowserIdentity
	Email    string
	LoginKey foundation.IdempotencyKey
}

func NewChallengeRequest(f ChallengeFields) (ChallengeRequest, error) {
	if f.Browser.Validate() != nil || f.LoginKey.Validate() != nil {
		return ChallengeRequest{}, Invalid()
	}
	return ChallengeRequest{func() ChallengeFields { return f }}, nil
}
func (r ChallengeRequest) Fields() ChallengeFields {
	if r.data == nil {
		return ChallengeFields{}
	}
	return r.data()
}
func (r ChallengeRequest) Validate() error {
	if r.data == nil {
		return Invalid()
	}
	return nil
}
func (r ChallengeRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "account_challenge_request")
}
func (r ChallengeRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"account_challenge_request"`), nil
}
func (*ChallengeRequest) UnmarshalJSON([]byte) error { return Invalid() }
func (r ChallengeRequest) LogValue() slog.Value      { return slog.StringValue("account_challenge_request") }

// Challenge contains only the two rendered puzzle images. Neither the source
// image nor the answer/verification parameters are a wire field.
type Challenge struct {
	ID        ChallengeID        `json:"id"`
	Mode      string             `json:"mode"`
	Master    string             `json:"master"`
	Thumb     string             `json:"thumb"`
	ExpiresAt foundation.Instant `json:"expires_at"`
}

func (r Challenge) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "account_challenge") }
func (r Challenge) LogValue() slog.Value       { return slog.StringValue("account_challenge") }
