package account

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func (h *accountHTTP) passkeyRoutes() []accountHTTPRoute {
	return []accountHTTPRoute{
		{http.MethodGet, "/auth/passkeys/capabilities", "public", false, h.httpPasskeyCapabilities},
		{http.MethodGet, "/me/passkeys", "session", false, h.httpPasskeys},
		{http.MethodPost, "/auth/passkeys/login/options", "browser", false, h.httpPasskeyLoginOptions},
		{http.MethodPost, "/auth/passkeys/login/complete", "browser", true, h.httpPasskeyLoginComplete},
		{http.MethodPost, "/me/passkeys/reauth/options", "session", false, h.httpPasskeyReauthOptions},
		{http.MethodPost, "/me/passkeys/reauth/complete", "session", true, h.httpPasskeyReauthComplete},
		{http.MethodPost, "/me/passkeys/registration/options", "session", false, h.httpPasskeyRegistrationOptions},
		{http.MethodPost, "/me/passkeys/registration/complete", "session", true, h.httpPasskeyRegistrationComplete},
		{http.MethodPatch, "/me/passkeys/{id}", "session", true, h.httpPasskeyRename},
		{http.MethodDelete, "/me/passkeys/{id}", "session", true, h.httpPasskeyDelete},
	}
}
func (h *accountHTTP) httpPasskeyCapabilities(w http.ResponseWriter, r *http.Request, _ httpRequest) {
	httpJSON(w, r, http.StatusOK, h.passkeys.Capabilities())
}
func (h *accountHTTP) httpPasskeys(w http.ResponseWriter, r *http.Request, in httpRequest) {
	out, e := h.core.Passkeys(r.Context(), in.actor)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusOK, out)
}
func (h *accountHTTP) httpPasskeyLoginOptions(w http.ResponseWriter, r *http.Request, in httpRequest) {
	var body struct{}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.BeginPasskeyLogin(r.Context(), h.passkeys, in.browser)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusOK, out)
}

type httpPasskeyComplete struct {
	OperationID string          `json:"operation_id"`
	Credential  json.RawMessage `json:"credential"`
}

func (h *accountHTTP) httpPasskeyLoginComplete(w http.ResponseWriter, r *http.Request, in httpRequest) {
	var body httpPasskeyComplete
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	ip, e := csrfClientIP(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.CompletePasskeyLogin(r.Context(), h.passkeys, in.browser, in.key, ip, body.OperationID, body.Credential)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	defer out.Close(context.Background())
	dto := struct {
		User    httpUser    `json:"user"`
		Session httpSession `json:"session"`
		Next    string      `json:"next_path"`
	}{httpUserDTO(out.User()), httpSessionDTO(out.Session()), "/"}
	h.csrf.httpCookieJSON(w, r, http.StatusOK, dto, h.csrf.sessionName, out.Session().AbsoluteExpiresAt.Time(), out.UseCookie)
}
func (h *accountHTTP) httpPasskeyReauthOptions(w http.ResponseWriter, r *http.Request, in httpRequest) {
	var body struct {
		Purpose   string `json:"purpose"`
		Action    string `json:"action,omitempty"`
		PasskeyID string `json:"passkey_id,omitempty"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.BeginPasskeyOperation(r.Context(), h.passkeys, in.actor, PasskeyBegin{Purpose: body.Purpose, Action: body.Action, PasskeyID: body.PasskeyID})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusOK, out)
}
func (h *accountHTTP) httpPasskeyRegistrationOptions(w http.ResponseWriter, r *http.Request, in httpRequest) {
	var body struct {
		GrantID         string             `json:"grant_id"`
		Label           string             `json:"label"`
		ExpectedVersion foundation.Version `json:"expected_version"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.BeginPasskeyOperation(r.Context(), h.passkeys, in.actor, PasskeyBegin{Purpose: "register", GrantID: body.GrantID, Label: body.Label, ExpectedVersion: body.ExpectedVersion})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusOK, out)
}
func (h *accountHTTP) httpPasskeyReauthComplete(w http.ResponseWriter, r *http.Request, in httpRequest) {
	h.httpPasskeyComplete(w, r, in, "reauth")
}
func (h *accountHTTP) httpPasskeyRegistrationComplete(w http.ResponseWriter, r *http.Request, in httpRequest) {
	h.httpPasskeyComplete(w, r, in, "register")
}
func (h *accountHTTP) httpPasskeyComplete(w http.ResponseWriter, r *http.Request, in httpRequest, purpose string) {
	var body httpPasskeyComplete
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.CompletePasskeyOperation(r.Context(), h.passkeys, in.actor, in.key, purpose, body.OperationID, body.Credential)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusOK, out)
}
func (h *accountHTTP) httpPasskeyRename(w http.ResponseWriter, r *http.Request, in httpRequest) {
	var body struct {
		ExpectedVersion foundation.Version `json:"expected_version"`
		GrantID         string             `json:"grant_id"`
		Label           string             `json:"label"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.ChangePasskey(r.Context(), in.actor, in.key, r.PathValue("id"), body.GrantID, body.Label, body.ExpectedVersion, false)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusOK, out)
}
func (h *accountHTTP) httpPasskeyDelete(w http.ResponseWriter, r *http.Request, in httpRequest) {
	var body struct {
		ExpectedVersion foundation.Version `json:"expected_version"`
		GrantID         string             `json:"grant_id"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.ChangePasskey(r.Context(), in.actor, in.key, r.PathValue("id"), body.GrantID, "", body.ExpectedVersion, true)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusOK, out)
}
