package outbound

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// ProxyGateway is the safe persisted gateway identity. Credentials never cross
// this projection; authentication material is borrowed separately per tunnel.
type ProxyGateway struct {
	Protocol          string `json:"protocol"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Username          string `json:"username"`
	CredentialPresent bool   `json:"credential_present"`
}
type ProxySettings struct {
	Version foundation.Version `json:"version"`
	Mode    string             `json:"mode"`
	Gateway *ProxyGateway      `json:"gateway"`
}
type ProxyUpdate struct {
	Mode             string
	Gateway          *ProxyGateway
	CredentialAction string
	Password         *sc.SecretMaterial
}

func (ProxyUpdate) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbound_proxy_update") }
func (ProxyUpdate) MarshalJSON() ([]byte, error) { return []byte(`"outbound_proxy_update"`), nil }
func (ProxyUpdate) LogValue() slog.Value         { return slog.StringValue("outbound_proxy_update") }

type ProxyUpdateResult struct {
	Version   foundation.Version `json:"version"`
	AuditID   ac.ID              `json:"audit_id"`
	CreatedAt foundation.Instant `json:"created_at"`
}

// ProxyProcessAuthority is bound to the original root ProcessGuard. Elapsed
// time or an absent connection never establishes that a foreign process died.
type ProxyProcessAuthority interface {
	CurrentProxyProcess() (string, error)
	ConfirmProxyProcessStopped(context.Context, string) error
}
type ProxyDependencies struct {
	Processes     ProxyProcessAuthority
	Secrets       *secret.Service
	Authority     *ProxySecretAuthority
	CurrentMACKey func() string
	MAC           func(string, ...[]byte) ([]byte, error)
}
type proxySnapshot struct {
	version    foundation.Version
	ref        string
	mode       string
	gateway    *ProxyGateway
	credential func(context.Context, func([]byte) error) error
}

func (s *PolicyService) proxySnapshot() (proxySnapshot, error) {
	st := s.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.available || !st.proxyAvailable {
		return proxySnapshot{}, unavailable(nil)
	}
	p := st.proxy
	if p.gateway != nil {
		g := *p.gateway
		p.gateway = &g
	}
	return p, nil
}
func normalizeProxyUpdate(input ProxyUpdate) (ProxyUpdate, error) {
	if input.Mode != "direct" && input.Mode != "proxy" || input.CredentialAction != "keep" && input.CredentialAction != "remove" {
		return input, invalid()
	}
	if input.CredentialAction == "remove" && input.Password != nil {
		return input, invalid()
	}
	if input.Gateway == nil {
		if input.Mode != "direct" || input.CredentialAction != "remove" || input.Password != nil {
			return input, invalid()
		}
		return input, nil
	}
	g := *input.Gateway
	input.Gateway = &g
	if g.Protocol != "http" && g.Protocol != "https" && g.Protocol != "socks5" || g.Port < 1 || g.Port > 65535 || len(g.Username) > 255 || !utf8.ValidString(g.Username) || strings.IndexFunc(g.Username, unicode.IsControl) >= 0 || strings.Contains(g.Username, ":") && g.Protocol != "socks5" {
		return input, invalid()
	}
	if input.CredentialAction == "remove" && g.Username != "" || input.Password != nil && g.Username == "" {
		return input, invalid()
	}
	if strings.ContainsAny(g.Host, "/@?#%") || len(g.Host) > 253 {
		return input, invalid()
	}
	target, e := ParseTarget("https://" + net.JoinHostPort(g.Host, strconv.Itoa(g.Port)) + "/")
	if e != nil {
		return input, invalid()
	}
	g.Host = target.Origin().Host()
	// Single-label corporate proxy names are explicitly configured deployment
	// gateways. Target parsing and the target DNS resolver remain unchanged.
	g.CredentialPresent = false
	if input.Password != nil {
		e = input.Password.Use(func(b []byte) error {
			if len(b) < 1 || len(b) > 255 || !utf8.Valid(b) {
				return invalid()
			}
			for _, c := range b {
				if c == 0 || c == '\r' || c == '\n' {
					return invalid()
				}
			}
			return nil
		})
		if e != nil {
			return input, e
		}
	}
	return input, nil
}
func sameProxyGateway(a, b *ProxyGateway) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Protocol == b.Protocol && a.Host == b.Host && a.Port == b.Port && a.Username == b.Username
}
