package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/netip"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type LoginFields struct {
	Browser       BrowserIdentity
	Key           foundation.IdempotencyKey
	Email         string
	Password      sc.SecretMaterial
	ClientIP      netip.Addr
	ChallengePass sc.SecretMaterial
}
type LoginRequest struct{ data func() LoginFields }

func NewLoginRequest(f LoginFields) (LoginRequest, error) {
	if f.Browser.Validate() != nil || f.Key.Validate() != nil || !f.ClientIP.IsValid() || f.ClientIP.Zone() != "" {
		return LoginRequest{}, Invalid()
	}
	return LoginRequest{func() LoginFields { return f }}, nil
}

// Fields is exclusively a trusted adapter projection, never a log object.
func (r LoginRequest) Fields() LoginFields {
	if r.data == nil {
		return LoginFields{}
	}
	return r.data()
}
func (r LoginRequest) Validate() error {
	if r.data == nil {
		return Invalid()
	}
	return nil
}
func (r LoginRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_login_request") }
func (r LoginRequest) MarshalJSON() ([]byte, error) { return []byte(`"account_login_request"`), nil }
func (*LoginRequest) UnmarshalJSON([]byte) error    { return Invalid() }
func (r LoginRequest) LogValue() slog.Value         { return slog.StringValue("account_login_request") }

// ChallengeAuthority is the mandatory current challenge gate once a failure
// counter requires it. B02 supplies the actual one-use implementation.
type ChallengeAuthority interface {
	PreviewLoginInTx(context.Context, foundation.Tx, LoginRequest) error
	CheckLoginInTx(context.Context, foundation.Tx, LoginRequest) error
}
