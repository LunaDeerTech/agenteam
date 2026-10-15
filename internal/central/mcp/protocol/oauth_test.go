package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

func oauthTestSecret() oauthSecret {
	return oauthSecret{Format: 1, Kind: "flow", Metadata: OAuthMetadata{Resource: "https://mcp.example.test/mcp", Issuer: "https://auth.example.test/tenant", AuthorizationEndpoint: "https://auth.example.test/authorize", TokenEndpoint: "https://auth.example.test/token", RegistrationEndpoint: "https://auth.example.test/register", TokenAuthMethod: "client_secret_post"}, ClientID: "test-client", ClientSecret: "test-private-client", RedirectURI: "https://central.example.test/oauth/callback", Verifier: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"}
}

func TestMCPOAuthMetadataRejectsIssuerAndPKCEDowngrade(t *testing.T) {
	r := &oauthex.ProtectedResourceMetadata{Resource: "https://mcp.example.test/mcp", ScopesSupported: []string{"read", "write"}}
	s := &oauthex.AuthServerMeta{Issuer: "https://auth.example.test/tenant", AuthorizationEndpoint: "https://auth.example.test/authorize", TokenEndpoint: "https://auth.example.test/token", ResponseTypesSupported: []string{"code"}, CodeChallengeMethodsSupported: []string{"S256"}, TokenEndpointAuthMethodsSupported: []string{"none"}}
	if _, err := normalizeOAuthMetadata(r, s, s.Issuer); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeOAuthMetadata(r, s, "https://other.example.test/tenant"); err == nil {
		t.Fatal("foreign issuer accepted")
	}
	s.CodeChallengeMethodsSupported = []string{"plain"}
	if _, err := normalizeOAuthMetadata(r, s, s.Issuer); err == nil {
		t.Fatal("PKCE downgrade accepted")
	}
	s.CodeChallengeMethodsSupported = []string{"S256"}
	s.TokenEndpoint = "http://auth.example.test/token"
	if _, err := normalizeOAuthMetadata(r, s, s.Issuer); err == nil {
		t.Fatal("plaintext token endpoint accepted")
	}
	if _, err := oauthIssuerMetadataURLs("https://auth.example.test/tenant?issuer=other"); err == nil {
		t.Fatal("ambiguous issuer query accepted")
	}
	urls, err := oauthIssuerMetadataURLs("https://auth.example.test/tenant")
	if err != nil || len(urls) != 3 || urls[0] != "https://auth.example.test/.well-known/oauth-authorization-server/tenant" {
		t.Fatal("RFC 8414 path discovery lost")
	}
}

func TestMCPOAuthTokenMaterialRotationAndResourceBinding(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	secret := oauthTestSecret()
	material, info, err := finishOAuthToken(secret, []byte(`{"access_token":"actual-private-access","token_type":"Bearer","refresh_token":"actual-private-refresh","expires_in":3600}`), now)
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	if !info.Refreshable || !info.Expiry.Equal(now.Add(time.Hour)) {
		t.Fatal("token lifecycle metadata lost")
	}
	token, err := decodeOAuthSecret(material, "token")
	if err != nil || token.Verifier != "" || token.RefreshToken != "actual-private-refresh" {
		t.Fatal("flow verifier survived exchange or refresh token lost")
	}
	access, _, err := OAuthAccess(material, secret.Metadata.Resource, now)
	if err != nil {
		t.Fatal(err)
	}
	defer access.Destroy()
	_ = access.Use(func(raw []byte) error {
		if string(raw) != "actual-private-access" {
			t.Fatal("wrong projected credential")
		}
		return nil
	})
	if _, _, err := OAuthAccess(material, "https://foreign.example.test/mcp", now); err == nil {
		t.Fatal("credential projected for another resource")
	}
	if _, _, err := OAuthAccess(material, secret.Metadata.Resource, now.Add(time.Hour)); err == nil {
		t.Fatal("expired credential projected")
	}
	rotated, _, err := finishOAuthToken(token, []byte(`{"access_token":"new-private-access","token_type":"bearer","expires_in":300}`), now)
	if err != nil {
		t.Fatal(err)
	}
	defer rotated.Destroy()
	got, err := decodeOAuthSecret(rotated, "token")
	if err != nil || got.RefreshToken != token.RefreshToken {
		t.Fatal("omitted refresh token failed to retain original")
	}
	for _, body := range []string{`{"access_token":"private","token_type":"DPoP"}`, `{"access_token":"private","token_type":"Bearer","expires_in":-1}`, `{"access_token":"private","token_type":"Bearer","expires_in":99999999999999}`} {
		if _, _, err := finishOAuthToken(token, []byte(body), now); err == nil {
			t.Fatal("unsupported token accepted")
		}
	}
}

func TestMCPOAuthPreparedAndFailuresDoNotExposeMaterial(t *testing.T) {
	material, _ := sc.NewSecretMaterial([]byte("test-only-private-token"))
	defer material.Destroy()
	prepared := OAuthPrepared{AuthorizationURL: "https://auth.example.test?state=test-only-private-state", Material: material}
	raw, _ := json.Marshal(prepared)
	for _, value := range []string{fmt.Sprintf("%+v", prepared), string(raw), prepared.LogValue().String()} {
		if strings.Contains(value, "test-only-private") {
			t.Fatal("OAuth value leaked")
		}
	}
	if _, err := decodeOAuthSecret(material, "token"); err == nil || strings.Contains(err.Error(), "test-only-private-token") {
		t.Fatal("invalid token exposed or accepted")
	}
}
