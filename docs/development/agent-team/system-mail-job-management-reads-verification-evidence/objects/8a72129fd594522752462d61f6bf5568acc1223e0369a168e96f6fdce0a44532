package account

import (
	"context"
	"net/http"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Keep the legacy MailJob wire object unchanged. The management object is a
// separate flat projection, with required nullable current-attempt facts.
type httpMailJobManagement struct {
	httpMailJob
	Kind           c.DeliveryKind    `json:"kind"`
	AttemptChannel *string           `json:"attempt_channel"`
	AttemptResult  *c.DeliveryResult `json:"attempt_result"`
}

func httpMailJobManagementDTO(v HTTPMailJobManagement) httpMailJobManagement {
	return httpMailJobManagement{httpMailJobDTO(v.HTTPMailJob), v.Kind, v.AttemptChannel, v.AttemptResult}
}

func httpMailJobManagementRoute(route accountHTTPRoute) bool {
	return route.method == http.MethodGet && (route.path == "/system/mail-jobs/management" || route.path == "/system/mail-jobs/{id}/management")
}

func httpMailJobManagementRequest(r *http.Request) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), httpMailJobManagementTimeout)
	return r.WithContext(ctx), cancel
}

func httpMailJobManagementEmptyBody(r *http.Request) bool {
	return r.ContentLength == 0 && len(r.TransferEncoding) == 0 && (r.Body == nil || r.Body == http.NoBody)
}

func (h *accountHTTP) httpListMailJobManagement(w http.ResponseWriter, r *http.Request, input httpRequest) {
	if !httpMailJobManagementEmptyBody(r) {
		httpProblem(w, r, invalid())
		return
	}
	query, e := httpListQuery(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.system.ListMailJobManagement(r.Context(), input.actor, query)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	items := make([]httpMailJobManagement, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, httpMailJobManagementDTO(item))
	}
	httpMailJobManagementJSON(w, r, struct {
		Items      []httpMailJobManagement `json:"items"`
		NextCursor string                  `json:"next_cursor,omitempty"`
	}{items, out.NextCursor})
}

func (h *accountHTTP) httpGetMailJobManagement(w http.ResponseWriter, r *http.Request, input httpRequest) {
	if !httpMailJobManagementEmptyBody(r) {
		httpProblem(w, r, invalid())
		return
	}
	id, e := foundation.ParseID[c.MailJob](r.PathValue("id"))
	if e != nil {
		httpProblem(w, r, invalid())
		return
	}
	out, e := h.system.GetMailJobManagement(r.Context(), input.actor, id)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpMailJobManagementJSON(w, r, httpMailJobManagementDTO(out))
}

func httpMailJobManagementJSON(w http.ResponseWriter, r *http.Request, value any) {
	if e := r.Context().Err(); e != nil {
		httpProblem(w, r, unavailable(e))
		return
	}
	encoded, e := httpEncode(value)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	defer clear(encoded)
	// Encoding can consume the remainder of the budget. Once WriteHeader starts,
	// the ordinary HTTP writer rules apply; a partial write cannot be withdrawn.
	if e = r.Context().Err(); e != nil {
		httpProblem(w, r, unavailable(e))
		return
	}
	if e = httpWriteEncoded(w, r, http.StatusOK, encoded); e != nil {
		panic(http.ErrAbortHandler)
	}
}
