package fixture

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const oauthOrigin = "https://mcp.fixture.test:8443"

// OAuthCallback contains the real authorization server response. Formatting
// never exposes its code or state, including in an assertion failure.
type OAuthCallback struct{ state, code, issuer string }

func (OAuthCallback) Format(w fmt.State, _ rune)             { _, _ = io.WriteString(w, "owned_oauth_callback") }
func (OAuthCallback) LogValue() slog.Value                   { return slog.StringValue("owned_oauth_callback") }
func (OAuthCallback) MarshalJSON() ([]byte, error)           { return []byte(`"owned_oauth_callback"`), nil }
func (c OAuthCallback) Fields() (state, code, issuer string) { return c.state, c.code, c.issuer }

type OAuthState struct {
	Registrations int `json:"registrations"`
	Exchanges     int `json:"exchanges"`
	Refreshes     int `json:"refreshes"`
	Accepted      int `json:"accepted"`
}

func (p *Provider) OAuthEndpoint() string { return oauthOrigin + "/oauth/mcp" }

func (p *Provider) oauthClient() (*http.Client, func()) {
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: p.roots, ServerName: "mcp.fixture.test", MinVersion: tls.VersionTLS12}, Proxy: nil}
	return &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, transport.CloseIdleConnections
}

// AuthorizeOAuth follows only this fixture's authorization URL. It does not
// follow the callback redirect or make the application callback request.
func (p *Provider) AuthorizeOAuth(ctx context.Context, authorization string) (OAuthCallback, error) {
	if p == nil || ctx == nil {
		return OAuthCallback{}, errors.New("owned OAuth authorization unavailable")
	}
	u, err := url.Parse(authorization)
	if err != nil || u.Scheme != "https" || u.Host != "mcp.fixture.test:8443" || u.User != nil || u.Path != "/oauth/authorize" || u.RawPath != "" || u.Fragment != "" || u.Opaque != "" {
		return OAuthCallback{}, errors.New("foreign OAuth authorization refused")
	}
	expected, err := url.Parse(u.Query().Get("redirect_uri"))
	if err != nil || expected.Host == "" || expected.Path != "/mcp/oauth/callback" || expected.User != nil || expected.RawQuery != "" || expected.Fragment != "" {
		return OAuthCallback{}, errors.New("owned OAuth redirect invalid")
	}
	u.Host = p.ip + ":8443"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return OAuthCallback{}, errors.New("owned OAuth authorization invalid")
	}
	req.Host = "mcp.fixture.test:8443"
	client, closeClient := p.oauthClient()
	defer closeClient()
	response, err := client.Do(req)
	if err != nil {
		return OAuthCallback{}, errors.New("owned OAuth authorization unavailable")
	}
	defer response.Body.Close()
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4097))
	location, parseErr := url.Parse(response.Header.Get("Location"))
	if response.StatusCode != http.StatusFound || readErr != nil || parseErr != nil || location.Scheme != expected.Scheme || location.Host != expected.Host || location.Path != expected.Path || location.User != nil || location.Fragment != "" {
		return OAuthCallback{}, errors.New("owned OAuth authorization rejected")
	}
	query := location.Query()
	if query.Get("code") == "" || query.Get("state") != u.Query().Get("state") || query.Get("iss") != oauthOrigin+"/oauth" || len(query) != 3 {
		return OAuthCallback{}, errors.New("owned OAuth callback invalid")
	}
	return OAuthCallback{query.Get("state"), query.Get("code"), query.Get("iss")}, nil
}

func (p *Provider) OAuthState(ctx context.Context) (OAuthState, error) {
	return p.oauthControl(ctx, nil)
}
func (p *Provider) ExpireOAuth(ctx context.Context) (OAuthState, error) {
	return p.oauthControl(ctx, []byte(`{"expire":true,"expires_in":3600}`))
}

// SetOAuthLifetime controls only future responses from this nonce-owned
// issuer. Existing tokens expire at their original real issuance deadline.
func (p *Provider) SetOAuthLifetime(ctx context.Context, seconds int) (OAuthState, error) {
	if seconds < 1 || seconds > 3600 {
		return OAuthState{}, errors.New("owned OAuth lifetime invalid")
	}
	body, err := json.Marshal(struct {
		Seconds int `json:"expires_in"`
	}{seconds})
	if err != nil {
		return OAuthState{}, errors.New("owned OAuth lifetime invalid")
	}
	return p.oauthControl(ctx, body)
}

func (p *Provider) oauthControl(ctx context.Context, body []byte) (OAuthState, error) {
	if p == nil || ctx == nil {
		return OAuthState{}, errors.New("owned OAuth control unavailable")
	}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://"+p.ip+":8443/oauth/control", bytes.NewReader(body))
	if err != nil {
		return OAuthState{}, errors.New("owned OAuth control invalid")
	}
	req.Header.Set("X-Fixture-Nonce", p.nonce)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client, closeClient := p.oauthClient()
	defer closeClient()
	response, err := client.Do(req)
	if err != nil {
		return OAuthState{}, errors.New("owned OAuth control unavailable")
	}
	defer response.Body.Close()
	var state OAuthState
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1025))
	decoder.DisallowUnknownFields()
	if response.StatusCode != 200 || decoder.Decode(&state) != nil {
		return OAuthState{}, errors.New("owned OAuth control rejected")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return OAuthState{}, errors.New("owned OAuth control shape invalid")
	}
	return state, nil
}
