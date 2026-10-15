package protocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// The wire structs remain private to this adapter. Only encrypted Secret
// material crosses its boundary; neither SDK token values nor raw errors do.
type oauthSecret struct {
	Format       int           `json:"format"`
	Kind         string        `json:"kind"`
	Metadata     OAuthMetadata `json:"metadata"`
	ClientID     string        `json:"client_id"`
	ClientSecret string        `json:"client_secret,omitempty"`
	RedirectURI  string        `json:"redirect_uri"`
	Verifier     string        `json:"verifier,omitempty"`
	AccessToken  string        `json:"access_token,omitempty"`
	RefreshToken string        `json:"refresh_token,omitempty"`
	Expiry       time.Time     `json:"expiry,omitempty"`
}
type OAuthPrepared struct {
	AuthorizationURL string
	Material         sc.SecretMaterial
}
type OAuthTokenInfo struct {
	Issuer, Resource string
	Expiry           time.Time
	Refreshable      bool
}

func (OAuthPrepared) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mcp_oauth_prepared") }
func (OAuthPrepared) MarshalJSON() ([]byte, error) { return []byte(`"mcp_oauth_prepared"`), nil }
func (OAuthPrepared) LogValue() slog.Value         { return slog.StringValue("mcp_oauth_prepared") }

// PrepareOAuth registers a standard web client and builds an authorization
// code request with S256 PKCE and resource indicator. A durable original begin
// request must already exist; registration is a mutation and is never retried.
func (r *Runtime) PrepareOAuth(ctx context.Context, metadata OAuthMetadata, redirectURI, state string, call outbound.CallContext) (OAuthPrepared, error) {
	if validateOAuthMetadata(metadata) != nil || metadata.RegistrationEndpoint == "" {
		return OAuthPrepared{}, failure(CapabilityUnsupported, false)
	}
	if _, err := oauthURL(redirectURI); err != nil {
		return OAuthPrepared{}, err
	}
	stateBytes, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil || len(stateBytes) != 32 {
		return OAuthPrepared{}, failure(InvalidRequest, false)
	}
	c, err := r.oauthClient(call, outbound.CredentialBinding{})
	if err != nil {
		return OAuthPrepared{}, err
	}
	registered, err := oauthex.RegisterClient(ctx, metadata.RegistrationEndpoint, &oauthex.ClientRegistrationMetadata{RedirectURIs: []string{redirectURI}, TokenEndpointAuthMethod: metadata.TokenAuthMethod, GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, ClientName: "agenteam", ApplicationType: "web", Scope: strings.Join(metadata.Scopes, " ")}, c)
	if err != nil {
		return OAuthPrepared{}, classify(err, true)
	}
	if registered == nil || !oauthCredential(registered.ClientID, 4096) || !slices.Equal(registered.RedirectURIs, []string{redirectURI}) || registered.TokenEndpointAuthMethod != metadata.TokenAuthMethod || !slices.Contains(registered.GrantTypes, "authorization_code") || !slices.Contains(registered.ResponseTypes, "code") {
		return OAuthPrepared{}, failure(InvalidResponse, true)
	}
	if metadata.TokenAuthMethod == "none" && registered.ClientSecret != "" || metadata.TokenAuthMethod != "none" && !oauthCredential(registered.ClientSecret, 4096) || !registered.ClientSecretExpiresAt.IsZero() && !registered.ClientSecretExpiresAt.After(time.Now().Add(time.Minute)) {
		return OAuthPrepared{}, failure(InvalidResponse, true)
	}
	secret := oauthSecret{Format: 1, Kind: "flow", Metadata: metadata, ClientID: registered.ClientID, ClientSecret: registered.ClientSecret, RedirectURI: redirectURI, Verifier: oauth2.GenerateVerifier()}
	material, err := encodeOAuthSecret(secret)
	if err != nil {
		return OAuthPrepared{}, err
	}
	config := oauth2.Config{ClientID: secret.ClientID, RedirectURL: redirectURI, Scopes: slices.Clone(metadata.Scopes), Endpoint: oauth2.Endpoint{AuthURL: metadata.AuthorizationEndpoint, TokenURL: metadata.TokenEndpoint}}
	return OAuthPrepared{AuthorizationURL: config.AuthCodeURL(state, oauth2.S256ChallengeOption(secret.Verifier), oauth2.SetAuthURLParam("resource", metadata.Resource)), Material: material}, nil
}

// ExchangeOAuth consumes the read copy of the original flow. The durable owner
// must claim the callback before calling this function and may only observe an
// unknown outcome afterwards, never submit the same authorization code again.
func (r *Runtime) ExchangeOAuth(ctx context.Context, material sc.SecretMaterial, code, issuer string, call outbound.CallContext) (sc.SecretMaterial, OAuthTokenInfo, error) {
	defer material.Destroy()
	secret, err := decodeOAuthSecret(material, "flow")
	if err != nil {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, err
	}
	if !oauthCredential(code, 8192) || secret.Metadata.RequireIssuerParameter && issuer == "" || issuer != "" && issuer != secret.Metadata.Issuer {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(InvalidRequest, false)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {secret.RedirectURI}, "code_verifier": {secret.Verifier}, "resource": {secret.Metadata.Resource}}
	return r.oauthToken(ctx, secret, form, call)
}

// RefreshOAuth makes one standard refresh_token request with the original
// resource. It does not install a lazy SDK TokenSource and cannot replay a Tool.
func (r *Runtime) RefreshOAuth(ctx context.Context, material sc.SecretMaterial, call outbound.CallContext) (sc.SecretMaterial, OAuthTokenInfo, error) {
	defer material.Destroy()
	secret, err := decodeOAuthSecret(material, "token")
	if err != nil {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, err
	}
	if secret.RefreshToken == "" {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(AuthenticationRequired, false)
	}
	return r.oauthToken(ctx, secret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {secret.RefreshToken}, "resource": {secret.Metadata.Resource}}, call)
}

func (r *Runtime) oauthToken(ctx context.Context, secret oauthSecret, form url.Values, call outbound.CallContext) (sc.SecretMaterial, OAuthTokenInfo, error) {
	var credentials outbound.CredentialBinding
	var basic sc.SecretMaterial
	defer basic.Destroy()
	if secret.Metadata.TokenAuthMethod == "client_secret_basic" {
		encoded := []byte(base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(secret.ClientID) + ":" + url.QueryEscape(secret.ClientSecret))))
		var err error
		basic, err = sc.NewSecretMaterial(encoded)
		clear(encoded)
		if err != nil {
			return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(InvalidRequest, false)
		}
		field, err := outbound.HeaderCredential("Authorization", "Basic ", basic)
		if err != nil {
			return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(InvalidRequest, false)
		}
		target, err := outbound.ParseTarget(secret.Metadata.TokenEndpoint)
		if err != nil {
			return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(InvalidRequest, false)
		}
		credentials, err = outbound.NewCredentialBinding(target.Origin(), field)
		if err != nil {
			return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(InvalidRequest, false)
		}
	} else {
		form.Set("client_id", secret.ClientID)
		if secret.Metadata.TokenAuthMethod == "client_secret_post" {
			form.Set("client_secret", secret.ClientSecret)
		}
	}
	c, err := r.oauthClient(call, credentials)
	if err != nil {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, err
	}
	// Unified outbound rejects every redirect for a POST body. The code,
	// verifier, client secret and refresh token cannot migrate to another URL.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, secret.Metadata.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(InvalidRequest, false)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := c.Do(request)
	if err != nil {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, classify(err, true)
	}
	body, err := oauthReadBody(response)
	if err != nil {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, err
	}
	defer clear(body)
	if response.StatusCode != http.StatusOK {
		code := ProtocolFailure
		if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized {
			code = AuthenticationRequired
		}
		return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(code, true)
	}
	return finishOAuthToken(secret, body, time.Now())
}

func finishOAuthToken(secret oauthSecret, body []byte, now time.Time) (sc.SecretMaterial, OAuthTokenInfo, error) {
	var response struct {
		AccessToken  string      `json:"access_token"`
		TokenType    string      `json:"token_type"`
		RefreshToken string      `json:"refresh_token"`
		ExpiresIn    json.Number `json:"expires_in"`
		Scope        string      `json:"scope"`
	}
	if len(body) > sc.MaxValueBytes || json.Unmarshal(body, &response) != nil || !oauthCredential(response.AccessToken, 16384) || !strings.EqualFold(response.TokenType, "Bearer") || response.RefreshToken != "" && !oauthCredential(response.RefreshToken, 16384) {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(InvalidResponse, true)
	}
	if response.ExpiresIn != "" {
		seconds, err := response.ExpiresIn.Int64()
		if err != nil || seconds <= 0 || seconds > 31536000 {
			return sc.SecretMaterial{}, OAuthTokenInfo{}, failure(InvalidResponse, true)
		}
		secret.Expiry = now.Add(time.Duration(seconds) * time.Second)
	} else {
		secret.Expiry = time.Time{}
	}
	secret.Kind, secret.Verifier, secret.AccessToken = "token", "", response.AccessToken
	// RFC 6749: omission preserves the existing refresh token; a returned
	// replacement atomically replaces it in the new encrypted Secret version.
	if response.RefreshToken != "" {
		secret.RefreshToken = response.RefreshToken
	}
	material, err := encodeOAuthSecret(secret)
	return material, OAuthTokenInfo{Issuer: secret.Metadata.Issuer, Resource: secret.Metadata.Resource, Expiry: secret.Expiry, Refreshable: secret.RefreshToken != ""}, err
}

// OAuthAccess returns only the valid access credential, not its refresh/client
// secrets. The caller still owns the input Secret copy and must destroy it.
func OAuthAccess(material sc.SecretMaterial, resource string, now time.Time) (sc.SecretMaterial, OAuthTokenInfo, error) {
	secret, err := decodeOAuthSecret(material, "token")
	if err != nil {
		return sc.SecretMaterial{}, OAuthTokenInfo{}, err
	}
	info := OAuthTokenInfo{Issuer: secret.Metadata.Issuer, Resource: secret.Metadata.Resource, Expiry: secret.Expiry, Refreshable: secret.RefreshToken != ""}
	if secret.Metadata.Resource != resource {
		return sc.SecretMaterial{}, info, failure(InvalidRequest, false)
	}
	if !secret.Expiry.IsZero() && !secret.Expiry.After(now.Add(30*time.Second)) {
		return sc.SecretMaterial{}, info, failure(AuthenticationRequired, false)
	}
	access, err := sc.NewSecretMaterial([]byte(secret.AccessToken))
	if err != nil {
		return sc.SecretMaterial{}, info, failure(InvalidResponse, false)
	}
	return access, info, nil
}

func validateOAuthMetadata(m OAuthMetadata) error {
	for _, raw := range []string{m.Resource, m.Issuer, m.AuthorizationEndpoint, m.TokenEndpoint} {
		if _, err := oauthURL(raw); err != nil {
			return err
		}
	}
	if m.RegistrationEndpoint != "" {
		if _, err := oauthURL(m.RegistrationEndpoint); err != nil {
			return err
		}
	}
	if m.TokenAuthMethod != "none" && m.TokenAuthMethod != "client_secret_post" && m.TokenAuthMethod != "client_secret_basic" {
		return failure(CapabilityUnsupported, false)
	}
	return nil
}
func oauthCredential(v string, max int) bool {
	if len(v) == 0 || len(v) > max {
		return false
	}
	for _, c := range v {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}
func encodeOAuthSecret(value oauthSecret) (sc.SecretMaterial, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return sc.SecretMaterial{}, failure(InvalidResponse, false)
	}
	defer clear(raw)
	m, err := sc.NewSecretMaterial(raw)
	if err != nil {
		return sc.SecretMaterial{}, failure(TooLarge, false)
	}
	return m, nil
}
func decodeOAuthSecret(material sc.SecretMaterial, kind string) (oauthSecret, error) {
	var value oauthSecret
	err := material.Use(func(raw []byte) error {
		if len(raw) > sc.MaxValueBytes || json.Unmarshal(raw, &value) != nil {
			return failure(InvalidResponse, false)
		}
		return nil
	})
	if err != nil || value.Format != 1 || value.Kind != kind || validateOAuthMetadata(value.Metadata) != nil || !oauthCredential(value.ClientID, 4096) || value.Metadata.TokenAuthMethod != "none" && !oauthCredential(value.ClientSecret, 4096) {
		return oauthSecret{}, failure(InvalidResponse, false)
	}
	if _, err := oauthURL(value.RedirectURI); err != nil {
		return oauthSecret{}, failure(InvalidResponse, false)
	}
	if kind == "flow" && !oauthCredential(value.Verifier, 128) || kind == "token" && (!oauthCredential(value.AccessToken, 16384) || value.RefreshToken != "" && !oauthCredential(value.RefreshToken, 16384)) {
		return oauthSecret{}, failure(InvalidResponse, false)
	}
	return value, nil
}
