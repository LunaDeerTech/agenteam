package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const oauthOrigin = "https://mcp.fixture.test:8443"
const oauthIssuer = oauthOrigin + "/oauth"
const oauthResource = oauthOrigin + "/oauth/mcp"

type oauthClient struct{ redirect string }
type oauthCode struct{ client, redirect, challenge string }
type oauthAccess struct {
	client string
	until  time.Time
}
type oauthFixture struct {
	mu                                                      sync.Mutex
	nonce                                                   string
	clients                                                 map[string]oauthClient
	codes                                                   map[string]oauthCode
	access                                                  map[string]oauthAccess
	refresh                                                 map[string]string
	lifetime, registrations, exchanges, refreshes, accepted int
}

func oauthRandom() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("owned OAuth entropy unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}
func oauthJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func oauthReject(w http.ResponseWriter) {
	oauthJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
}

// The protected handler remains the real official SDK server. This peer only
// implements its OAuth authorization server and validates actual bearer bytes.
func registerOAuth(mux *http.ServeMux, handler http.Handler, nonce string) {
	f := &oauthFixture{nonce: nonce, clients: map[string]oauthClient{}, codes: map[string]oauthCode{}, access: map[string]oauthAccess{}, refresh: map[string]string{}, lifetime: 3600}
	mux.HandleFunc("/.well-known/oauth-protected-resource/oauth/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		oauthJSON(w, 200, map[string]any{"resource": oauthResource, "authorization_servers": []string{oauthIssuer}, "bearer_methods_supported": []string{"header"}, "scopes_supported": []string{"mcp:read"}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/oauth", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		oauthJSON(w, 200, map[string]any{"issuer": oauthIssuer, "authorization_endpoint": oauthIssuer + "/authorize", "token_endpoint": oauthIssuer + "/token", "registration_endpoint": oauthIssuer + "/register", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "authorization_response_iss_parameter_supported": true})
	})
	mux.HandleFunc("/oauth/register", f.register)
	mux.HandleFunc("/oauth/authorize", f.authorize)
	mux.HandleFunc("/oauth/token", f.token)
	mux.HandleFunc("/oauth/control", f.control)
	mux.HandleFunc("/oauth/mcp", func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		raw := strings.TrimPrefix(header, "Bearer ")
		f.mu.Lock()
		token, ok := f.access[raw]
		valid := ok && time.Now().Before(token.until)
		valid = valid && strings.HasPrefix(header, "Bearer ")
		if valid {
			f.accepted++
		}
		f.mu.Unlock()
		if !valid {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+oauthOrigin+`/.well-known/oauth-protected-resource/oauth/mcp"`)
			w.WriteHeader(401)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func (f *oauthFixture) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var body struct {
		Redirects []string `json:"redirect_uris"`
		Method    string   `json:"token_endpoint_auth_method"`
	}
	defer r.Body.Close()
	if json.NewDecoder(io.LimitReader(r.Body, 65537)).Decode(&body) != nil || len(body.Redirects) != 1 || body.Method != "none" {
		oauthReject(w)
		return
	}
	u, err := url.Parse(body.Redirects[0])
	if err != nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Path != "/mcp/oauth/callback" || (u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "127.0.0.1")) {
		oauthReject(w)
		return
	}
	client := oauthRandom()
	f.mu.Lock()
	f.clients[client] = oauthClient{body.Redirects[0]}
	f.registrations++
	f.mu.Unlock()
	oauthJSON(w, 201, map[string]any{"client_id": client, "redirect_uris": body.Redirects, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
}

func (f *oauthFixture) authorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	q := r.URL.Query()
	f.mu.Lock()
	client, ok := f.clients[q.Get("client_id")]
	challenge, err := base64.RawURLEncoding.DecodeString(q.Get("code_challenge"))
	if !ok || client.redirect != q.Get("redirect_uri") || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("resource") != oauthResource || q.Get("state") == "" || len(q.Get("state")) > 512 || err != nil || len(challenge) != 32 {
		f.mu.Unlock()
		oauthReject(w)
		return
	}
	code := oauthRandom()
	f.codes[code] = oauthCode{q.Get("client_id"), client.redirect, q.Get("code_challenge")}
	f.mu.Unlock()
	redirect, _ := url.Parse(client.redirect)
	result := redirect.Query()
	result.Set("code", code)
	result.Set("state", q.Get("state"))
	result.Set("iss", oauthIssuer)
	redirect.RawQuery = result.Encode()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", redirect.String())
	w.WriteHeader(302)
}

func (f *oauthFixture) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	defer r.Body.Close()
	if r.ParseForm() != nil || r.Form.Get("resource") != oauthResource {
		oauthReject(w)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	client := r.Form.Get("client_id")
	if _, ok := f.clients[client]; !ok {
		oauthReject(w)
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		value, ok := f.codes[r.Form.Get("code")]
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])
		if !ok || value.client != client || value.redirect != r.Form.Get("redirect_uri") || subtle.ConstantTimeCompare([]byte(value.challenge), []byte(challenge)) != 1 {
			oauthReject(w)
			return
		}
		delete(f.codes, r.Form.Get("code"))
		f.exchanges++
	case "refresh_token":
		old, ok := f.refresh[r.Form.Get("refresh_token")]
		if !ok || old != client {
			oauthReject(w)
			return
		}
		delete(f.refresh, r.Form.Get("refresh_token"))
		f.refreshes++
	default:
		oauthReject(w)
		return
	}
	access, refresh := oauthRandom(), oauthRandom()
	f.access[access] = oauthAccess{client, time.Now().Add(time.Duration(f.lifetime) * time.Second)}
	f.refresh[refresh] = client
	oauthJSON(w, 200, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": f.lifetime, "scope": "mcp:read"})
}

func (f *oauthFixture) control(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Fixture-Nonce")), []byte(f.nonce)) != 1 {
		w.WriteHeader(403)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodPost {
		var body struct {
			ExpiresIn int  `json:"expires_in"`
			Expire    bool `json:"expire"`
		}
		defer r.Body.Close()
		if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body) != nil || body.ExpiresIn < 0 || body.ExpiresIn > 3600 {
			oauthReject(w)
			return
		}
		if body.ExpiresIn != 0 {
			f.lifetime = body.ExpiresIn
		}
		if body.Expire {
			for key, value := range f.access {
				value.until = time.Now().Add(-time.Second)
				f.access[key] = value
			}
		}
	} else if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	oauthJSON(w, 200, map[string]int{"registrations": f.registrations, "exchanges": f.exchanges, "refreshes": f.refreshes, "accepted": f.accepted})
}
