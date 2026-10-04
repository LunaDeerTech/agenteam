package account

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (h *accountHTTP) httpBootstrap(w http.ResponseWriter, r *http.Request, _ httpRequest) {
	channel, e := h.system.httpDeliveryChannel(r.Context())
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.NewAnonymousContext(r.Context())
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	defer out.Cookie.Destroy()
	defer out.CSRF.Destroy()
	e = out.CSRF.Use(func(raw []byte) error {
		dto := struct {
			CSRF    string   `json:"csrf_token"`
			Modes   []string `json:"challenge_modes"`
			Channel string   `json:"delivery_channel"`
		}{string(raw), []string{"rotate"}, channel}
		h.csrf.httpCookieJSON(w, r, http.StatusOK, dto, h.csrf.browserName, out.Identity.ExpiresAt().Time(), out.Cookie.Use)
		return nil
	})
	if e != nil {
		httpProblem(w, r, unavailable(e))
	}

}
func (h *accountHTTP) httpSession(w http.ResponseWriter, r *http.Request, _ httpRequest) {
	cookie, e := csrfCookie(r, h.csrf.sessionName)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	defer cookie.Destroy()
	view, e := h.core.GetSession(r.Context(), cookie)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	defer view.CSRF.Destroy()
	var body []byte
	e = view.CSRF.Use(func(raw []byte) error {
		var e error
		body, e = httpEncode(struct {
			User    httpUser    `json:"user"`
			Session httpSession `json:"session"`
			CSRF    string      `json:"csrf_token"`
		}{httpUserDTO(view.User), httpSessionDTO(view.Session), string(raw)})
		return e
	})
	defer clear(body)
	if e != nil {
		httpProblem(w, r, unavailable(e))
		return
	}
	if httpWriteEncoded(w, r, http.StatusOK, body) != nil {
		panic(http.ErrAbortHandler)
	}
}
func (h *accountHTTP) httpLogin(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Email    string     `json:"email"`
		Password httpSecret `json:"password"`
		Pass     httpSecret `json:"challenge_pass"`
	}
	defer func() { body.Password.httpDestroy(); body.Pass.httpDestroy() }()
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	if body.Password.httpRequired() != nil {
		httpProblem(w, r, field("/password", "REQUIRED"))
		return
	}
	ip, e := csrfClientIP(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	command, e := c.NewLoginRequest(c.LoginFields{Browser: input.browser, Key: input.key, Email: body.Email, Password: body.Password.material, ClientIP: ip, ChallengePass: body.Pass.material})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	response, e := h.core.Login(r.Context(), command)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	defer response.Close(context.Background())
	dto := struct {
		User    httpUser    `json:"user"`
		Session httpSession `json:"session"`
		Next    string      `json:"next_path"`
	}{httpUserDTO(response.User()), httpSessionDTO(response.Session()), "/"}
	h.csrf.httpCookieJSON(w, r, http.StatusOK, dto, h.csrf.sessionName, response.Session().AbsoluteExpiresAt.Time(), response.UseCookie)

}
func (h *accountHTTP) httpLogout(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct{}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	if e := h.core.Logout(r.Context(), LogoutRequest{Actor: input.actor, Key: input.key}); e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	h.csrf.csrfClearSession(w)
	httpNoContent(w)
}

type httpChallengeInput struct {
	Mode     string                    `json:"mode"`
	Email    string                    `json:"email"`
	LoginKey foundation.IdempotencyKey `json:"login_key"`
}

func (h *accountHTTP) httpChallenge(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body httpChallengeInput
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	if body.Mode != "rotate" {
		httpProblem(w, r, invalid())
		return
	}
	command, e := c.NewChallengeRequest(c.ChallengeFields{Browser: input.browser, Email: body.Email, LoginKey: body.LoginKey})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.CreateChallenge(r.Context(), command)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	if len(out.Master)+len(out.Thumb) > 256<<10 {
		httpProblem(w, r, unavailable(nil))
		return
	}
	httpJSON(w, r, http.StatusCreated, struct {
		ID      c.ChallengeID      `json:"id"`
		Mode    string             `json:"mode"`
		Master  string             `json:"master"`
		Thumb   string             `json:"thumb"`
		Expires foundation.Instant `json:"expires_at"`
	}{out.ID, out.Mode, out.Master, out.Thumb, out.ExpiresAt})
}
func (h *accountHTTP) httpVerifyChallenge(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Email    string                    `json:"email"`
		LoginKey foundation.IdempotencyKey `json:"login_key"`
		ID       c.ChallengeID             `json:"challenge_id"`
		Proof    json.RawMessage           `json:"proof"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	proofRequest := r.Clone(r.Context())
	proofRequest.Header = r.Header.Clone()
	proofRequest.Header.Set("Content-Type", "application/json")
	proofRequest.Body = io.NopCloser(bytes.NewReader(body.Proof))
	var proof struct {
		Angle *int `json:"angle"`
	}
	if e := httpapi.DecodeJSON(w, proofRequest, &proof, 4<<10); e != nil {
		httpProblem(w, r, e)
		return
	}
	if proof.Angle == nil || *proof.Angle < 0 || *proof.Angle > 360 || body.ID.Validate() != nil {
		httpProblem(w, r, fault(foundation.ChallengeInvalid, nil))
		return
	}
	command, e := c.NewChallengeRequest(c.ChallengeFields{Browser: input.browser, Email: body.Email, LoginKey: body.LoginKey})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	pass, e := h.core.VerifyChallenge(r.Context(), command, body.ID, *proof.Angle)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	defer pass.Destroy()
	var encoded []byte
	e = pass.Use(func(raw []byte) error {
		var e error
		encoded, e = httpEncode(struct {
			Pass string `json:"pass"`
		}{string(raw)})
		return e
	})
	defer clear(encoded)
	if e != nil {
		httpProblem(w, r, unavailable(e))
		return
	}
	if httpWriteEncoded(w, r, http.StatusOK, encoded) != nil {
		panic(http.ErrAbortHandler)
	}
}
func (h *accountHTTP) httpResetRequest(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Email string `json:"email"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	ip, e := csrfClientIP(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	command, e := c.NewResetRequest(c.ResetRequestFields{Browser: input.browser, Key: input.key, Email: body.Email, ClientIP: ip})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.RequestPasswordReset(r.Context(), command)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusAccepted, struct {
		Accepted bool   `json:"accepted"`
		Channel  string `json:"delivery_channel"`
	}{out.Accepted, out.DeliveryChannel})
}
func (h *accountHTTP) httpResetInspect(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Token httpSecret `json:"token"`
	}
	defer func() { body.Token.httpDestroy() }()
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	token, material, e := httpParseLink(body.Token, c.PasswordResetToken)
	defer material.Destroy()
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.InspectPasswordReset(r.Context(), input.browser, token)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusOK, struct {
		Valid   bool               `json:"valid"`
		Expires foundation.Instant `json:"expires_at"`
	}{out.Valid, out.ExpiresAt})
}
func (h *accountHTTP) httpResetComplete(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Token        httpSecret `json:"token"`
		Password     httpSecret `json:"new_password"`
		Confirmation httpSecret `json:"confirmation"`
	}
	defer func() { body.Token.httpDestroy(); body.Password.httpDestroy(); body.Confirmation.httpDestroy() }()
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	token, material, e := httpParseLink(body.Token, c.PasswordResetToken)
	defer material.Destroy()
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	if body.Password.httpRequired() != nil || body.Confirmation.httpRequired() != nil {
		httpProblem(w, r, invalid())
		return
	}
	command, e := c.NewResetComplete(c.ResetCompleteFields{Browser: input.browser, Key: input.key, Token: token, Password: body.Password.material, Confirmation: body.Confirmation.material})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	if _, e = h.core.CompletePasswordReset(r.Context(), command); e != nil {
		httpProblem(w, r, e)
		return
	}
	httpNoContent(w)
}
func (h *accountHTTP) httpInvitationCurrent(r *http.Request) (*identity.Actor, error) {
	present := false
	for _, cookie := range r.Cookies() {
		if cookie.Name == h.csrf.sessionName {
			present = true
		}
	}
	if !present {
		return nil, nil
	}
	a, e := h.csrf.csrfSession(h.core, r, false)
	if e != nil {
		return nil, e
	}
	return &a, nil
}
func (h *accountHTTP) httpInviteInspect(w http.ResponseWriter, r *http.Request, input httpRequest) {
	current, e := h.httpInvitationCurrent(r)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	var body struct {
		Token httpSecret `json:"token"`
	}
	defer func() { body.Token.httpDestroy() }()
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	token, material, e := httpParseLink(body.Token, c.InvitationToken)
	defer material.Destroy()
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.InspectInvitation(r.Context(), input.browser, token, current)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, struct {
		Email   string             `json:"email"`
		Expires foundation.Instant `json:"expires_at"`
	}{out.Email, out.ExpiresAt})
}
func (h *accountHTTP) httpInviteRedeem(w http.ResponseWriter, r *http.Request, input httpRequest) {
	current, e := h.httpInvitationCurrent(r)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	var body struct {
		Token        httpSecret `json:"token"`
		Username     string     `json:"username"`
		DisplayName  string     `json:"display_name"`
		Password     httpSecret `json:"password"`
		Confirmation httpSecret `json:"confirmation"`
	}
	defer func() { body.Token.httpDestroy(); body.Password.httpDestroy(); body.Confirmation.httpDestroy() }()
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	token, material, e := httpParseLink(body.Token, c.InvitationToken)
	defer material.Destroy()
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	if current != nil {
		if _, e = h.core.InspectInvitation(r.Context(), input.browser, token, current); e != nil {
			h.httpProblem(w, r, e, true)
			return
		}
	}
	if body.Password.httpRequired() != nil || body.Confirmation.httpRequired() != nil {
		httpProblem(w, r, invalid())
		return
	}
	command, e := c.NewInvitationRedeem(c.RedeemFields{Browser: input.browser, Key: input.key, Token: token, Username: body.Username, DisplayName: body.DisplayName, Password: body.Password.material, Confirmation: body.Confirmation.material})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.RedeemInvitation(r.Context(), command)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	httpJSON(w, r, http.StatusCreated, struct {
		Completed     bool `json:"completed"`
		LoginRequired bool `json:"login_required"`
	}{out.Completed, out.Completed})
}
func (h *accountHTTP) httpChangePassword(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Version      foundation.Version `json:"version"`
		Current      httpSecret         `json:"current_password"`
		Password     httpSecret         `json:"new_password"`
		Confirmation httpSecret         `json:"confirmation"`
	}
	defer func() { body.Current.httpDestroy(); body.Password.httpDestroy(); body.Confirmation.httpDestroy() }()
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	if body.Current.httpRequired() != nil || body.Password.httpRequired() != nil || body.Confirmation.httpRequired() != nil {
		httpProblem(w, r, invalid())
		return
	}
	command, e := c.NewPasswordChange(c.PasswordChangeFields{Actor: input.actor, Key: input.key, ExpectedVersion: body.Version, OldPassword: body.Current.material, Password: body.Password.material, Confirmation: body.Confirmation.material})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	response, e := h.core.ChangePassword(r.Context(), command)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	defer response.Close(context.Background())
	dto := struct {
		Completed bool   `json:"completed"`
		Next      string `json:"next_path"`
	}{true, "/"}
	h.csrf.httpCookieJSON(w, r, http.StatusOK, dto, h.csrf.sessionName, time.Time{}, response.UseCookie)

}
