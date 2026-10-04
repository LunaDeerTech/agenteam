package account

import (
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type csrfBoundary struct {
	origin, host, sessionName, browserName string
	secure                                 bool
}

func csrfNewBoundary(raw string) (csrfBoundary, error) {
	// Reuse the accepted origin parser. Options are deployment input, never
	// inferred from Host or forwarded headers.
	u, e := url.Parse(raw)
	if e != nil || strings.ContainsAny(raw, "#\\") {
		return csrfBoundary{}, invalid()
	}
	u.Scheme = strings.ToLower(u.Scheme)
	configuredHost := strings.ToLower(u.Hostname())
	o, e := outbound.ParseOrigin(u.String())
	if e != nil {
		return csrfBoundary{}, invalid()
	}
	ip, ipErr := netip.ParseAddr(o.Host())
	local := configuredHost == "localhost" || ipErr == nil && ip.Unmap().IsLoopback()
	if o.Scheme() != "https" && !local {
		return csrfBoundary{}, invalid()
	}
	canonical := o.String()
	if o.Scheme() == "https" && o.Port() == 443 {
		canonical = strings.TrimSuffix(canonical, ":443")
	} else if o.Scheme() == "http" && o.Port() == 80 {
		canonical = strings.TrimSuffix(canonical, ":80")
	}
	u, e = url.Parse(canonical)
	if e != nil {
		return csrfBoundary{}, invalid()
	}
	// Unlike outbound connection pooling, browser origin identity preserves a
	// DNS trailing dot. Match PUBLIC_ORIGIN's existing browser serialization.
	if strings.HasSuffix(configuredHost, ".") {
		u.Host = strings.Replace(u.Host, o.Host(), o.Host()+".", 1)
		canonical = u.String()
	}
	b := csrfBoundary{origin: canonical, host: u.Host, secure: o.Scheme() == "https", sessionName: "__Host-agenteam_session", browserName: "__Host-agenteam_browser"}
	if !b.secure {
		b.sessionName = "agenteam_local_session"
		b.browserName = "agenteam_local_browser"
	}
	return b, nil
}
func csrfUnsafe(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
func (b csrfBoundary) csrfCheck(r *http.Request) error {
	// This runs outside routing, path normalization, identity lookup and body
	// decoding. A proxy must preserve this canonical authority.
	if r.Host != b.host {
		return fault(foundation.OriginDenied, nil)
	}
	fetch := r.Header.Values("Sec-Fetch-Site")
	if len(fetch) > 1 || len(fetch) == 1 && (fetch[0] == "cross-site" || fetch[0] != "same-origin" && fetch[0] != "same-site" && fetch[0] != "none") {
		return fault(foundation.OriginDenied, nil)
	}
	origins := r.Header.Values("Origin")
	if len(origins) > 1 || len(origins) == 1 && origins[0] != b.origin || csrfUnsafe(r.Method) && len(origins) != 1 {
		return fault(foundation.OriginDenied, nil)
	}
	return nil
}
func csrfCookie(r *http.Request, name string) (sc.SecretMaterial, error) {
	var found *http.Cookie
	for _, v := range r.Cookies() {
		if v.Name != name {
			continue
		}
		if found != nil || v.Value == "" || len(v.Value) > 2048 {
			return sc.SecretMaterial{}, fault(foundation.Unauthenticated, nil)
		}
		found = v
	}
	if found == nil {
		return sc.SecretMaterial{}, fault(foundation.Unauthenticated, nil)
	}
	v, e := sc.NewSecretMaterial([]byte(found.Value))
	if e != nil {
		return v, fault(foundation.Unauthenticated, nil)
	}
	return v, nil
}
func csrfToken(r *http.Request) (sc.SecretMaterial, error) {
	v, e := httpSingleHeader(r, "X-CSRF-Token")
	if e != nil || len(v) > 128 {
		return sc.SecretMaterial{}, fault(foundation.CSRFFailed, nil)
	}
	s, e := sc.NewSecretMaterial([]byte(v))
	if e != nil {
		return s, fault(foundation.CSRFFailed, nil)
	}
	return s, nil
}
func (b csrfBoundary) csrfSession(core *Service, r *http.Request, write bool) (identity.Actor, error) {
	cookie, e := csrfCookie(r, b.sessionName)
	if e != nil {
		return identity.Actor{}, e
	}
	defer cookie.Destroy()
	if !write {
		return core.Authenticate(r.Context(), cookie)
	}
	token, e := csrfToken(r)
	if e != nil {
		return identity.Actor{}, e
	}
	defer token.Destroy()
	actor, e := core.ValidateSessionCSRF(r.Context(), cookie, token)
	if hasFaultCode(e, foundation.Forbidden) {
		e = fault(foundation.CSRFFailed, nil)
	}
	return actor, e
}
func (b csrfBoundary) csrfBrowser(core *Service, r *http.Request) (c.BrowserIdentity, error) {
	cookie, e := csrfCookie(r, b.browserName)
	if e != nil {
		return c.BrowserIdentity{}, fault(foundation.CSRFFailed, nil)
	}
	defer cookie.Destroy()
	token, e := csrfToken(r)
	if e != nil {
		return c.BrowserIdentity{}, e
	}
	defer token.Destroy()
	id, e := core.VerifyAnonymousContext(r.Context(), cookie, token)
	if hasFaultCode(e, foundation.Forbidden) || hasFaultCode(e, foundation.Unauthenticated) {
		e = fault(foundation.CSRFFailed, nil)
	}
	return id, e
}
func (b csrfBoundary) csrfMakeCookie(name, value string, expires time.Time) (*http.Cookie, error) {
	cookie := &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: b.secure, SameSite: http.SameSiteLaxMode, Expires: expires}
	if e := cookie.Valid(); e != nil {
		return nil, unavailable(e)
	}
	return cookie, nil
}
func (b csrfBoundary) csrfClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: b.sessionName, Path: "/", HttpOnly: true, Secure: b.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0).UTC()})
}
func csrfClientIP(r *http.Request) (netip.Addr, error) {
	a, e := netip.ParseAddrPort(r.RemoteAddr)
	if e != nil || a.Addr().Zone() != "" {
		return netip.Addr{}, invalid()
	}
	return a.Addr().Unmap(), nil
}
