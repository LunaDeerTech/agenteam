package account

import (
	"net/http"
	"net/url"
	"strconv"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func httpListQuery(r *http.Request) (HTTPListRequest, error) {
	query, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return HTTPListRequest{}, invalid()
	}
	out := HTTPListRequest{Limit: 25}
	for k, values := range query {
		if len(values) != 1 || values[0] == "" {
			return HTTPListRequest{}, invalid()
		}
		switch k {
		case "cursor":
			if len(values[0]) > cursor.MaxTokenBytes {
				return HTTPListRequest{}, fault(foundation.CursorInvalid, nil)
			}
			out.Cursor = values[0]
		case "limit":
			n, e := strconv.Atoi(values[0])
			if e != nil || n < 1 || n > 100 || strconv.Itoa(n) != values[0] {
				return HTTPListRequest{}, field("/limit", "INVALID")
			}
			out.Limit = n
		default:
			return HTTPListRequest{}, invalid()
		}
	}
	return out, nil
}

type httpSystemUser struct {
	httpUser
	CreatedAt foundation.Instant `json:"created_at"`
}

func (h *accountHTTP) httpListUsers(w http.ResponseWriter, r *http.Request, input httpRequest) {
	query, e := httpListQuery(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.system.ListUsers(r.Context(), input.actor, query)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	items := make([]httpSystemUser, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, httpSystemUser{httpUserDTO(item.User), item.CreatedAt})
	}
	httpJSON(w, r, http.StatusOK, struct {
		Items []httpSystemUser `json:"items"`
		Next  string           `json:"next_cursor,omitempty"`
	}{items, out.NextCursor})
}

type httpInvitation struct {
	ID      c.InvitationID     `json:"id"`
	Email   string             `json:"email"`
	Version foundation.Version `json:"version"`
	Created foundation.Instant `json:"created_at"`
	Expires foundation.Instant `json:"expires_at"`
}

func (h *accountHTTP) httpListInvitations(w http.ResponseWriter, r *http.Request, input httpRequest) {
	query, e := httpListQuery(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.system.ListInvitations(r.Context(), input.actor, query)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	items := make([]httpInvitation, 0, len(out.Items))
	for _, v := range out.Items {
		items = append(items, httpInvitation{v.ID, v.Email, v.Version, v.CreatedAt, v.ExpiresAt})
	}
	httpJSON(w, r, http.StatusOK, struct {
		Items []httpInvitation `json:"items"`
		Next  string           `json:"next_cursor,omitempty"`
	}{items, out.NextCursor})
}
func httpInvitationReceipt(out c.InvitationReceipt) any {
	return struct {
		ID      c.InvitationID     `json:"id"`
		Job     c.JobID            `json:"job_id"`
		Version foundation.Version `json:"version"`
	}{out.ID, out.JobID, out.Version}
}
func (h *accountHTTP) httpCreateInvitation(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Email string `json:"email"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	command, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: input.actor, Key: input.key, Email: body.Email})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.CreateInvitation(r.Context(), command)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusCreated, httpInvitationReceipt(out))
}
func (h *accountHTTP) httpResendInvitation(w http.ResponseWriter, r *http.Request, input httpRequest) {
	id, e := foundation.ParseID[c.Invitation](r.PathValue("id"))
	if e != nil {
		httpProblem(w, r, invalid())
		return
	}
	var body httpVersionInput
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.ResendInvitation(r.Context(), c.InvitationResend{Actor: input.actor, Key: input.key, ID: id, ExpectedVersion: body.Version})
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusAccepted, httpInvitationReceipt(out))
}
func (h *accountHTTP) httpRevokeInvitation(w http.ResponseWriter, r *http.Request, input httpRequest) {
	id, e := foundation.ParseID[c.Invitation](r.PathValue("id"))
	if e != nil {
		httpProblem(w, r, invalid())
		return
	}
	var body httpVersionInput
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	if e = h.core.RevokeInvitation(r.Context(), c.InvitationRevoke{Actor: input.actor, Key: input.key, ID: id, ExpectedVersion: body.Version}); e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpNoContent(w)
}
func httpSettingsDTO(v c.Settings) any {
	return struct {
		ID            c.SettingsID        `json:"id"`
		Version       foundation.Version  `json:"version"`
		Idle          foundation.Progress `json:"session_idle_seconds"`
		Absolute      foundation.Progress `json:"session_absolute_seconds"`
		Reset         foundation.Progress `json:"password_reset_seconds"`
		Challenge     foundation.Progress `json:"challenge_after_failures"`
		LifetimeScope string              `json:"lifetime_changes_apply_to"`
	}{v.ID, v.Version, v.SessionIdleSeconds, v.SessionAbsoluteSeconds, v.PasswordResetSeconds, v.ChallengeAfterFailures, "newly_issued_sessions_and_tokens"}
}
func (h *accountHTTP) httpGetAccountSettings(w http.ResponseWriter, r *http.Request, input httpRequest) {
	out, e := h.system.GetAccountSettings(r.Context(), input.actor)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpSettingsDTO(out))
}
func (h *accountHTTP) httpSetAccountSettings(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Version   foundation.Version  `json:"version"`
		Idle      foundation.Progress `json:"session_idle_seconds"`
		Absolute  foundation.Progress `json:"session_absolute_seconds"`
		Reset     foundation.Progress `json:"password_reset_seconds"`
		Challenge foundation.Progress `json:"challenge_after_failures"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.system.UpdateAccountSettings(r.Context(), HTTPAccountSettingsUpdate{Actor: input.actor, Key: input.key, ExpectedVersion: body.Version, SessionIdleSeconds: body.Idle, SessionAbsoluteSeconds: body.Absolute, PasswordResetSeconds: body.Reset, ChallengeAfterFailures: body.Challenge})
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpSettingsDTO(out))
}
func httpSMTPDTO(v c.SMTPSettings) any {
	return struct {
		ID                c.SettingsID        `json:"id"`
		Version           foundation.Version  `json:"version"`
		Configured        bool                `json:"configured"`
		Host              string              `json:"host"`
		Port              int                 `json:"port"`
		Encryption        string              `json:"encryption"`
		Username          string              `json:"username"`
		SenderEmail       string              `json:"sender_email"`
		SenderName        string              `json:"sender_name"`
		CredentialPresent bool                `json:"credential_present"`
		RetryCount        foundation.Progress `json:"auto_retry_count"`
		RetryInterval     foundation.Progress `json:"retry_interval_seconds"`
	}{v.ID, v.Version, v.Configured, v.Host, v.Port, v.TLSMode, v.Username, v.SenderEmail, v.SenderName, v.CredentialPresent, v.RetryCount, v.RetryIntervalSeconds}
}

// The receipt identifies this command's applied version; settings is a fresh,
// currently authorized view and may have advanced after a replay/concurrent save.
func httpSMTPMutationDTO(receipt c.SMTPSettingsReceipt, current c.SMTPSettings) any {
	return struct {
		AppliedVersion foundation.Version `json:"applied_version"`
		Settings       any                `json:"settings"`
	}{receipt.Version, httpSMTPDTO(current)}
}
func (h *accountHTTP) httpGetSMTP(w http.ResponseWriter, r *http.Request, input httpRequest) {
	out, e := h.core.GetSMTPSettings(r.Context(), input.actor)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpSMTPDTO(out))
}
func (h *accountHTTP) httpSetSMTP(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Version       foundation.Version   `json:"version"`
		Host          string               `json:"host"`
		Port          int                  `json:"port"`
		Encryption    string               `json:"encryption"`
		Username      string               `json:"username"`
		SenderEmail   string               `json:"sender_email"`
		SenderName    string               `json:"sender_name"`
		Action        string               `json:"credential_action"`
		Password      httpSecret           `json:"password"`
		RetryCount    *foundation.Progress `json:"auto_retry_count"`
		RetryInterval *foundation.Progress `json:"retry_interval_seconds"`
	}
	defer func() { body.Password.httpDestroy() }()
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPSMTPLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	if body.RetryCount == nil || body.RetryInterval == nil {
		httpProblem(w, r, invalid())
		return
	}
	fields := c.SMTPUpdateFields{Actor: input.actor, Key: input.key, ExpectedVersion: body.Version, Configured: true, Host: body.Host, Port: body.Port, TLSMode: body.Encryption, Username: body.Username, SenderEmail: body.SenderEmail, SenderName: body.SenderName, CredentialAction: body.Action, RetryCount: int64(*body.RetryCount), RetryIntervalSeconds: int64(*body.RetryInterval)}
	if fields.CredentialAction == "" {
		fields.CredentialAction = "keep"
	}
	if body.Password.present && !body.Password.empty {
		fields.Password = &body.Password.material
	}
	command, e := c.NewSMTPUpdate(fields)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	receipt, e := h.core.UpdateSMTPSettings(r.Context(), command)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	// Re-read under current administrator authority; never serialize the update
	// command or its opaque credential. A failed read cannot create a success.
	out, e := h.core.GetSMTPSettings(r.Context(), input.actor)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpSMTPMutationDTO(receipt, out))
}
func (h *accountHTTP) httpUnconfigureSMTP(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Version       foundation.Version   `json:"version"`
		RetryCount    *foundation.Progress `json:"auto_retry_count"`
		RetryInterval *foundation.Progress `json:"retry_interval_seconds"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	if body.RetryCount == nil || body.RetryInterval == nil {
		httpProblem(w, r, invalid())
		return
	}
	command, e := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: input.actor, Key: input.key, ExpectedVersion: body.Version, Configured: false, CredentialAction: "remove", RetryCount: int64(*body.RetryCount), RetryIntervalSeconds: int64(*body.RetryInterval)})
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	receipt, e := h.core.UpdateSMTPSettings(r.Context(), command)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	out, e := h.core.GetSMTPSettings(r.Context(), input.actor)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpSMTPMutationDTO(receipt, out))
}
func (h *accountHTTP) httpTestSMTP(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Recipient string `json:"recipient"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.TestSMTP(r.Context(), c.SMTPTest{Actor: input.actor, Key: input.key, Recipient: body.Recipient})
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusAccepted, struct {
		Job c.JobID `json:"job_id"`
	}{out.JobID})
}

type httpMailJob struct {
	Job      c.JobID             `json:"job_id"`
	Phase    string              `json:"phase"`
	Attempts foundation.Progress `json:"attempts"`
	Version  foundation.Version  `json:"version"`
	Reason   c.DeliveryReason    `json:"reason,omitempty"`
	Channel  string              `json:"channel"`
	Created  foundation.Instant  `json:"created_at"`
}

func httpMailJobDTO(v HTTPMailJob) httpMailJob {
	return httpMailJob{v.Status.JobID, v.Status.Phase, v.Status.Attempts, v.Status.Version, v.Status.Reason, v.Channel, v.CreatedAt}
}
func (h *accountHTTP) httpListMailJobs(w http.ResponseWriter, r *http.Request, input httpRequest) {
	query, e := httpListQuery(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.system.ListMailJobs(r.Context(), input.actor, query)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	items := make([]httpMailJob, 0, len(out.Items))
	for _, v := range out.Items {
		items = append(items, httpMailJobDTO(v))
	}
	httpJSON(w, r, http.StatusOK, struct {
		Items []httpMailJob `json:"items"`
		Next  string        `json:"next_cursor,omitempty"`
	}{items, out.NextCursor})
}
func (h *accountHTTP) httpGetMailJob(w http.ResponseWriter, r *http.Request, input httpRequest) {
	id, e := foundation.ParseID[c.MailJob](r.PathValue("id"))
	if e != nil {
		httpProblem(w, r, invalid())
		return
	}
	out, e := h.system.GetMailJob(r.Context(), input.actor, id)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpMailJobDTO(out))
}
func (h *accountHTTP) httpRetryMailJob(w http.ResponseWriter, r *http.Request, input httpRequest) {
	id, e := foundation.ParseID[c.MailJob](r.PathValue("id"))
	if e != nil {
		httpProblem(w, r, invalid())
		return
	}
	var body httpVersionInput
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.core.RetryMailJob(r.Context(), c.MailJobRetry{Actor: input.actor, Key: input.key, JobID: id, ExpectedVersion: body.Version})
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusAccepted, struct {
		Job     c.JobID            `json:"job_id"`
		Version foundation.Version `json:"version"`
	}{out.JobID, out.Version})
}
