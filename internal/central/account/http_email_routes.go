package account

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func (h *accountHTTP) emailRoutes() []accountHTTPRoute {
	var out []accountHTTPRoute
	for _, route := range []struct{ path, kind string }{
		{"/me/passkeys/recovery", "passkey_recovery"},
		{"/me/email-change", "email_change"},
		{"/me/email-initialization", "initialization"},
	} {
		out = append(out, accountHTTPRoute{"POST", route.path + "/request", "session", true, func(w http.ResponseWriter, r *http.Request, in httpRequest) { h.httpEmailRequest(w, r, in, route.kind) }})
		out = append(out, accountHTTPRoute{"POST", route.path + "/resend", "session", true, func(w http.ResponseWriter, r *http.Request, in httpRequest) { h.httpEmailResend(w, r, in, route.kind) }})
		out = append(out, accountHTTPRoute{"POST", route.path + "/confirm", "session", true, func(w http.ResponseWriter, r *http.Request, in httpRequest) { h.httpEmailConfirm(w, r, in, route.kind) }})
	}
	return out
}
func (h *accountHTTP) httpEmailRequest(w http.ResponseWriter, r *http.Request, in httpRequest, kind string) {
	input := EmailProofRequest{Actor: in.actor, Key: in.key, Kind: kind}
	switch kind {
	case "passkey_recovery":
		var body struct {
			Password httpSecret `json:"password"`
		}
		defer body.Password.httpDestroy()
		if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
			httpProblem(w, r, e)
			return
		}
		if e := body.Password.httpRequired(); e != nil {
			httpProblem(w, r, e)
			return
		}
		input.Password = body.Password.material
	case "email_change":
		var body struct {
			GrantID         string             `json:"grant_id"`
			Email           string             `json:"email"`
			ExpectedVersion foundation.Version `json:"expected_version"`
		}
		if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
			httpProblem(w, r, e)
			return
		}
		input.GrantID = body.GrantID
		input.Email = body.Email
		input.ExpectedVersion = body.ExpectedVersion
	case "initialization":
		var body struct {
			Password      httpSecret `json:"password"`
			Authorization httpSecret `json:"authorization"`
		}
		defer body.Password.httpDestroy()
		defer body.Authorization.httpDestroy()
		if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
			httpProblem(w, r, e)
			return
		}
		if body.Password.httpRequired() != nil || body.Authorization.httpRequired() != nil {
			httpProblem(w, r, invalid())
			return
		}
		input.Password = body.Password.material
		input.Authorization = body.Authorization.material
	}
	out, e := h.core.RequestEmailProof(r.Context(), input)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusAccepted, out)
}
func (h *accountHTTP) httpEmailResend(w http.ResponseWriter, r *http.Request, in httpRequest, kind string) {
	var body struct {
		RequestID string `json:"request_id"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.RequestEmailProof(r.Context(), EmailProofRequest{Actor: in.actor, Key: in.key, Kind: kind, RequestID: body.RequestID})
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusAccepted, out)
}
func (h *accountHTTP) httpEmailConfirm(w http.ResponseWriter, r *http.Request, in httpRequest, kind string) {
	var body struct {
		RequestID string     `json:"request_id"`
		Code      httpSecret `json:"code"`
	}
	defer body.Code.httpDestroy()
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	if e := body.Code.httpRequired(); e != nil {
		httpProblem(w, r, field("/code", "INVALID_OR_EXPIRED"))
		return
	}
	out, e := h.core.ConfirmEmailProof(r.Context(), in.actor, in.key, kind, body.RequestID, body.Code.material)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, out)
}
