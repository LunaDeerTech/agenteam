package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// ProcessAuthority is adapted by the composition root to the one shared
// ProcessGuard. Neither age nor an absent database connection proves death.
type ProcessAuthority interface {
	CurrentProcess() ProcessID
	ConfirmStopped(context.Context, ProcessID) error
}

// BrowserIdentity is produced after the account service verifies its signed
// anonymous context. A caller's independently minted issuer cannot match it.
type BrowserIssuer struct{ identity *byte }

func NewBrowserIssuer() BrowserIssuer { return BrowserIssuer{new(byte)} }

type BrowserIdentity struct{ data func() browserData }
type browserData struct {
	issuer  BrowserIssuer
	id      BrowserID
	kid     string
	expires foundation.Instant
}

func NewBrowserIdentity(issuer BrowserIssuer, id BrowserID, kid string, expires foundation.Instant) (BrowserIdentity, error) {
	if issuer.identity == nil || id.Validate() != nil || kid == "" || expires.Validate() != nil {
		return BrowserIdentity{}, Invalid()
	}
	d := browserData{issuer, id, kid, expires}
	return BrowserIdentity{func() browserData { return d }}, nil
}
func (b BrowserIdentity) IssuedBy(i BrowserIssuer) bool {
	return b.data != nil && i.identity != nil && b.data().issuer == i
}
func (b BrowserIdentity) ID() BrowserID {
	if b.data == nil {
		return BrowserID{}
	}
	return b.data().id
}
func (b BrowserIdentity) KeyID() string {
	if b.data == nil {
		return ""
	}
	return b.data().kid
}
func (b BrowserIdentity) ExpiresAt() foundation.Instant {
	if b.data == nil {
		return foundation.Instant{}
	}
	return b.data().expires
}
func (b BrowserIdentity) Validate() error {
	if b.data == nil {
		return Invalid()
	}
	return nil
}

// SameContext compares the original verified capability, including its private
// issuer. A caller-minted browser ID never equals a service-issued context.
func (b BrowserIdentity) SameContext(other BrowserIdentity) bool {
	if b.data == nil || other.data == nil {
		return false
	}
	x, y := b.data(), other.data()
	return x.issuer == y.issuer && x.id == y.id && x.kid == y.kid && x.expires == y.expires
}
func (b BrowserIdentity) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_browser") }
func (b BrowserIdentity) MarshalJSON() ([]byte, error) { return []byte(`"account_browser"`), nil }
func (*BrowserIdentity) UnmarshalJSON([]byte) error    { return Invalid() }
func (b BrowserIdentity) LogValue() slog.Value         { return slog.StringValue("account_browser") }
