package account

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const accountHTTPJSONLimit int64 = 16 << 10
const accountHTTPSMTPLimit int64 = 32 << 10
const accountHTTPAvatarLimit int64 = 5 << 20

// httpSecret is an input-only wire scalar. Ordinary formatting and JSON never
// reveal it. Only the adapter's explicit synchronous material callbacks may do so.
type httpSecret struct {
	material sc.SecretMaterial
	present  bool
	empty    bool
}

func (s *httpSecret) UnmarshalJSON(b []byte) error {
	var v string
	if len(b) < 2 || b[0] != '"' || json.Unmarshal(b, &v) != nil {
		return invalid()
	}
	s.present, s.empty = true, v == ""
	if s.empty {
		return nil
	}
	var e error
	s.material, e = sc.NewSecretMaterial([]byte(v))
	return e
}
func (s httpSecret) MarshalJSON() ([]byte, error) { return []byte(`"account_http_secret"`), nil }
func (s httpSecret) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_http_secret") }
func (s httpSecret) LogValue() slog.Value         { return slog.StringValue("account_http_secret") }
func (s httpSecret) httpDestroy()                 { s.material.Destroy() }
func (s httpSecret) httpRequired() error {
	if !s.present || s.empty {
		return invalid()
	}
	return nil
}

// An optional string keeps null distinct from absence. JSON null is never a
// request to clear a profile field; display_name:"" is the explicit clear.
type httpOptionalString struct{ value *string }

func (s *httpOptionalString) UnmarshalJSON(b []byte) error {
	var v string
	if len(b) < 2 || b[0] != '"' || json.Unmarshal(b, &v) != nil {
		return invalid()
	}
	s.value = &v
	return nil
}

type httpUser struct {
	ID                        c.UserID           `json:"id"`
	Email                     string             `json:"email"`
	Username                  string             `json:"username"`
	DisplayName               string             `json:"display_name"`
	Role                      c.Role             `json:"role"`
	Theme                     c.Theme            `json:"theme"`
	Version                   foundation.Version `json:"version"`
	InitialPasswordSuggestion bool               `json:"initial_password_suggestion"`
}

func httpUserDTO(u c.User) httpUser {
	return httpUser{u.ID, u.Email, u.Username, u.DisplayName, u.Role, u.Theme, u.Version, u.InitialPasswordSuggestion}
}

type httpSession struct {
	ID                c.SessionID        `json:"id"`
	IssuedAt          foundation.Instant `json:"issued_at"`
	AbsoluteExpiresAt foundation.Instant `json:"absolute_expires_at"`
	IdleExpiresAt     foundation.Instant `json:"idle_expires_at"`
}

func httpSessionDTO(s c.Session) httpSession {
	return httpSession{s.ID, s.IssuedAt, s.AbsoluteExpiresAt, s.IdleExpiresAt}
}

type httpAvatar struct {
	MediaType string              `json:"media_type"`
	ByteSize  foundation.Progress `json:"byte_size"`
	SHA256    foundation.Digest   `json:"sha256"`
}
type httpProfile struct {
	User   httpUser    `json:"user"`
	Avatar *httpAvatar `json:"avatar"`
}

func httpProfileDTO(v c.ProfileView) httpProfile {
	out := httpProfile{User: httpUserDTO(v.User)}
	if v.Avatar != nil {
		out.Avatar = &httpAvatar{v.Avatar.MediaType, v.Avatar.ByteSize, v.Avatar.SHA256}
	}
	return out
}

type httpVersionInput struct {
	Version foundation.Version `json:"version"`
}

func httpSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
func httpProblem(w http.ResponseWriter, r *http.Request, e error) {
	// Never reflect an unrecognized URL (which might contain a pasted link
	// capability) into Problem.instance. Query strings are never projected.
	copyRequest := *r
	u := *r.URL
	u.Path = "/api/v1"
	u.RawPath = ""
	copyRequest.URL = &u
	httpSecurityHeaders(w)
	httpapi.WriteProblem(w, &copyRequest, e)
}
func httpEncode(dto any) ([]byte, error) {
	b, e := json.Marshal(dto)
	if e != nil {
		return nil, unavailable(e)
	}
	return b, nil
}
func httpWriteEncoded(w http.ResponseWriter, r *http.Request, status int, b []byte) error {
	httpSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return nil
	}
	_, e := w.Write(b)
	return e
}
func httpJSON(w http.ResponseWriter, r *http.Request, status int, dto any) {
	b, e := httpEncode(dto)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	defer clear(b)
	if e = httpWriteEncoded(w, r, status, b); e != nil {
		panic(http.ErrAbortHandler)
	}
}

// httpCookieJSON fully encodes the explicit response before asking an opaque
// capability for cookie bytes. A failed encode/read never sets a cookie. The
// callback remains synchronous so the capability's owner can close/join it.
func (b csrfBoundary) httpCookieJSON(w http.ResponseWriter, r *http.Request, status int, dto any, name string, expires time.Time, use func(func([]byte) error) error) {
	encoded, e := httpEncode(dto)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	defer clear(encoded)
	committed := false
	e = use(func(raw []byte) error {
		cookie, e := b.csrfMakeCookie(name, string(raw), expires)
		if e != nil {
			return e
		}
		http.SetCookie(w, cookie)
		committed = true
		return httpWriteEncoded(w, r, status, encoded)
	})
	if e != nil {
		if committed {
			panic(http.ErrAbortHandler)
		}
		httpProblem(w, r, portError(e))
	}
}
func httpNoContent(w http.ResponseWriter) {
	httpSecurityHeaders(w)
	w.Header().Del("Content-Type")
	w.Header().Del("Content-Length")
	w.WriteHeader(http.StatusNoContent)
}
func httpSingleHeader(r *http.Request, name string) (string, error) {
	v := r.Header.Values(name)
	if len(v) != 1 || v[0] == "" {
		return "", invalid()
	}
	return v[0], nil
}
func httpCommandKey(r *http.Request) (foundation.IdempotencyKey, error) {
	s, e := httpSingleHeader(r, "Idempotency-Key")
	k := foundation.IdempotencyKey(s)
	if e != nil || k.Validate() != nil {
		return "", field("/idempotency_key", "INVALID")
	}
	return k, nil
}
func httpParseLink(input httpSecret, kind c.TokenKind) (c.LinkToken, sc.SecretMaterial, error) {
	var out c.LinkToken
	var token sc.SecretMaterial
	if input.httpRequired() != nil {
		return out, token, fault(foundation.ResourceDeleted, nil)
	}
	e := input.material.Use(func(raw []byte) error {
		parts := strings.Split(string(raw), ".")
		if len(parts) != 2 {
			return fault(foundation.ResourceDeleted, nil)
		}
		id, e := foundation.ParseID[c.Invitation](parts[0])
		if e != nil {
			return fault(foundation.ResourceDeleted, nil)
		}
		if _, e = tokenVerifier(kind, []byte(parts[1])); e != nil {
			return e
		}
		token, e = sc.NewSecretMaterial([]byte(parts[1]))
		if e != nil {
			return invalid()
		}
		if kind == c.InvitationToken {
			out, e = c.NewInvitationToken(id, token)
		} else {
			rid, e2 := foundation.ParseID[c.PasswordReset](parts[0])
			if e2 != nil {
				return e2
			}
			out, e = c.NewResetToken(rid, token)
		}
		return e
	})
	if e != nil {
		token.Destroy()
	}
	return out, token, e
}
