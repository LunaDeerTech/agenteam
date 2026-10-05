package account

import (
	"net/http"
	"path"
	"sort"
	"strings"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type HTTPOptions struct {
	PublicOrigin string
	System       *SystemHTTPFacade
}

type accountHTTP struct {
	core     *Service
	profiles c.ProfilePort
	system   *SystemHTTPFacade
	csrf     csrfBoundary
}
type httpRequest struct {
	actor   identity.Actor
	browser c.BrowserIdentity
	key     foundation.IdempotencyKey
}
type accountHTTPRoute struct {
	method, path, authority string
	command                 bool
	serve                   func(http.ResponseWriter, *http.Request, httpRequest)
}

func httpValidateCore(core *Service) error {
	if core == nil || core.data == nil {
		return fault(foundation.DependencyUnbound, nil)
	}
	s := core.state()
	if s == nil || nilPort(s.store) || s.keys.Validate() != nil || s.hasher == nil || s.deps.Authority == nil || s.deps.Authority.data == nil || nilPort(s.deps.Audit) || s.deps.Secrets == nil || nilPort(s.deps.Events) || nilPort(s.deps.Challenges) || nilPort(s.deps.Processes) || s.deps.RecoveryLog == nil || s.deps.SessionsRevoked.Schema().EventType != c.SessionsRevokedType || s.deps.DeliveryRequested.Schema().EventType != c.DeliveryRequestedType {
		return fault(foundation.DependencyUnbound, nil)
	}
	return nil
}

// NewHTTPHandler installs the account surface only. Construction does no I/O;
// the composition root must initialize and start all real capabilities first
// and install the shared HTTP trace/recovery middleware exactly once outside it.
func NewHTTPHandler(core *Service, profiles c.ProfilePort, options HTTPOptions) (http.Handler, error) {
	if e := httpValidateCore(core); e != nil {
		return nil, e
	}
	if nilPort(profiles) || options.System == nil || options.System.core != core || options.System.pagination.Validate() != nil {
		return nil, fault(foundation.DependencyUnbound, nil)
	}
	b, e := csrfNewBoundary(options.PublicOrigin)
	if e != nil {
		return nil, e
	}
	h := &accountHTTP{core: core, profiles: profiles, system: options.System, csrf: b}
	return h.httpHandler(), nil
}
func (h *accountHTTP) httpRoutes() []accountHTTPRoute {
	return []accountHTTPRoute{
		{"GET", "/auth/bootstrap", "public", false, h.httpBootstrap},
		{"GET", "/session", "session", false, h.httpSession},
		{"POST", "/sessions/login", "browser", true, h.httpLogin},
		{"POST", "/sessions/logout", "session", true, h.httpLogout},
		{"POST", "/auth/challenges", "browser", false, h.httpChallenge},
		{"POST", "/auth/challenges/verify", "browser", false, h.httpVerifyChallenge},
		{"POST", "/password-resets/request", "browser", true, h.httpResetRequest},
		{"POST", "/password-resets/inspect", "browser", false, h.httpResetInspect},
		{"POST", "/password-resets/complete", "browser", true, h.httpResetComplete},
		{"POST", "/invitations/inspect", "browser", false, h.httpInviteInspect},
		{"POST", "/invitations/redeem", "browser", true, h.httpInviteRedeem},
		{"GET", "/me", "session", false, h.httpGetProfile},
		{"PATCH", "/me", "session", true, h.httpUpdateProfile},
		{"GET", "/me/preferences", "session", false, h.httpGetPreferences},
		{"PUT", "/me/preferences", "session", true, h.httpSetPreferences},
		{"POST", "/me/change-password", "session", true, h.httpChangePassword},
		{"GET", "/me/avatar", "session", false, h.httpGetAvatar},
		{"HEAD", "/me/avatar", "session", false, h.httpGetAvatar},
		{"PUT", "/me/avatar", "session", true, h.httpPutAvatar},
		{"DELETE", "/me/avatar", "session", true, h.httpDeleteAvatar},
		{"GET", "/system/users", "admin", false, h.httpListUsers},
		{"GET", "/system/invitations", "admin", false, h.httpListInvitations},
		{"POST", "/system/invitations", "admin", true, h.httpCreateInvitation},
		{"POST", "/system/invitations/{id}/resend", "admin", true, h.httpResendInvitation},
		{"POST", "/system/invitations/{id}/revoke", "admin", true, h.httpRevokeInvitation},
		{"GET", "/system/account-settings", "admin", false, h.httpGetAccountSettings},
		{"PUT", "/system/account-settings", "admin", true, h.httpSetAccountSettings},
		{"GET", "/system/smtp", "admin", false, h.httpGetSMTP},
		{"PUT", "/system/smtp", "admin", true, h.httpSetSMTP},
		{"POST", "/system/smtp/unconfigure", "admin", true, h.httpUnconfigureSMTP},
		{"POST", "/system/smtp/test", "admin", true, h.httpTestSMTP},
		{"GET", "/system/mail-jobs", "admin", false, h.httpListMailJobs},
		{"GET", "/system/mail-jobs/{id}", "admin", false, h.httpGetMailJob},
		{"POST", "/system/mail-jobs/{id}/retry", "admin", true, h.httpRetryMailJob},
	}
}
func (h *accountHTTP) httpHandler() http.Handler {
	mux := http.NewServeMux()
	groups := map[string][]accountHTTPRoute{}
	for _, route := range h.httpRoutes() {
		groups[route.path] = append(groups[route.path], route)
	}
	for p, routes := range groups {
		mux.HandleFunc("/api/v1"+p, func(w http.ResponseWriter, r *http.Request) {
			for _, route := range routes {
				if r.Method == route.method {
					h.httpDispatch(w, r, route)
					return
				}
			}
			allowed := make([]string, 0, len(routes))
			for _, route := range routes {
				allowed = append(allowed, route.method)
			}
			sort.Strings(allowed)
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			httpProblem(w, r, fault(foundation.MethodNotAllowed, nil))
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { httpProblem(w, r, fault(foundation.NotFound, nil)) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpSecurityHeaders(w)
		if e := h.csrf.csrfCheck(r); e != nil {
			httpProblem(w, r, e)
			return
		}
		// Refuse ambiguous/normalized API paths instead of redirecting a browser
		// request before its security checks or reflecting a pasted capability.
		if r.URL == nil || r.URL.Path != path.Clean(r.URL.Path) || strings.ContainsAny(r.URL.Path, "\\\x00") || r.URL.RawPath != "" {
			httpProblem(w, r, invalid())
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (h *accountHTTP) httpDispatch(w http.ResponseWriter, r *http.Request, route accountHTTPRoute) {
	var input httpRequest
	var e error
	switch route.authority {
	case "browser":
		input.browser, e = h.csrf.csrfBrowser(h.core, r)
	case "session", "admin":
		input.actor, e = h.csrf.csrfSession(h.core, r, csrfUnsafe(r.Method))
		if e == nil && route.authority == "admin" {
			intent := identity.Read
			if csrfUnsafe(r.Method) {
				intent = identity.Mutate
			}
			_, e = h.core.state().deps.Authority.AuthorizeSystem(r.Context(), foundation.Tx{}, input.actor, intent)
		}
	}
	if e != nil {
		h.httpProblem(w, r, e, route.authority != "browser")
		return
	}
	if route.command {
		input.key, e = httpCommandKey(r)
		if e != nil {
			httpProblem(w, r, e)
			return
		}
	}
	list := route.method == http.MethodGet && (route.path == "/system/users" || route.path == "/system/invitations" || route.path == "/system/mail-jobs")
	if !list && (r.URL.RawQuery != "" || r.URL.ForceQuery) {
		httpProblem(w, r, invalid())
		return
	}
	route.serve(w, r, input)
}
func (h *accountHTTP) httpProblem(w http.ResponseWriter, r *http.Request, e error, session bool) {
	if session && (hasFaultCode(e, foundation.Unauthenticated) || hasFaultCode(e, foundation.SessionRevoked)) {
		h.csrf.csrfClearSession(w)
	}
	httpProblem(w, r, e)
}
