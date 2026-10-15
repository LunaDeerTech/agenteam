package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthFixtureCodeAndRefreshAreSingleUse(t *testing.T) {
	mux := http.NewServeMux()
	delegated := 0
	registerOAuth(mux, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { delegated++; w.WriteHeader(204) }), "nonce")
	request := func(method, path, body, auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if path == "/oauth/token" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	registered := request("POST", "/oauth/register", `{"redirect_uris":["https://app.fixture.test/mcp/oauth/callback"],"token_endpoint_auth_method":"none"}`, "")
	var client struct {
		ID string `json:"client_id"`
	}
	if registered.Code != 201 || json.Unmarshal(registered.Body.Bytes(), &client) != nil || client.ID == "" {
		t.Fatal("registration failed")
	}
	verifier := strings.Repeat("v", 43)
	hash := sha256.Sum256([]byte(verifier))
	values := url.Values{"client_id": {client.ID}, "redirect_uri": {"https://app.fixture.test/mcp/oauth/callback"}, "response_type": {"code"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "resource": {oauthResource}, "state": {"state"}}
	authorized := request("GET", "/oauth/authorize?"+values.Encode(), "", "")
	location, err := url.Parse(authorized.Header().Get("Location"))
	if authorized.Code != 302 || err != nil || location.Query().Get("state") != "state" || location.Query().Get("iss") != oauthIssuer {
		t.Fatal("authorization failed")
	}
	form := url.Values{"client_id": {client.ID}, "redirect_uri": {"https://app.fixture.test/mcp/oauth/callback"}, "grant_type": {"authorization_code"}, "code": {location.Query().Get("code")}, "resource": {oauthResource}, "code_verifier": {"wrong"}}
	if request("POST", "/oauth/token", form.Encode(), "").Code != 400 {
		t.Fatal("wrong PKCE accepted")
	}
	form.Set("code_verifier", verifier)
	token := request("POST", "/oauth/token", form.Encode(), "")
	var credentials struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if token.Code != 200 || json.Unmarshal(token.Body.Bytes(), &credentials) != nil || credentials.Access == "" || credentials.Refresh == "" {
		t.Fatal("exchange failed")
	}
	if request("POST", "/oauth/token", form.Encode(), "").Code != 400 {
		t.Fatal("authorization code replay accepted")
	}
	if request("POST", "/oauth/mcp", "", credentials.Access).Code != 401 || delegated != 0 {
		t.Fatal("bare credential accepted")
	}
	if request("POST", "/oauth/mcp", "", "Bearer "+credentials.Access).Code != 204 || delegated != 1 {
		t.Fatal("authorized SDK delegation failed")
	}
	refresh := url.Values{"client_id": {client.ID}, "grant_type": {"refresh_token"}, "refresh_token": {credentials.Refresh}, "resource": {oauthResource}}
	if request("POST", "/oauth/token", refresh.Encode(), "").Code != 200 || request("POST", "/oauth/token", refresh.Encode(), "").Code != 400 {
		t.Fatal("refresh rotation failed")
	}
}
