package meetinghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type deletionService interface {
	Delete(context.Context, i.Actor, c.DeleteRequest) (c.DeleteReceipt, error)
	LookupDelete(context.Context, i.Actor, c.DeleteRequest) (c.DeleteReceipt, bool, error)
}

type deletionHandler struct {
	service  deletionService
	boundary boundary
}

// The response records the original Owner command. Accepted/cancelling is
// distinct from completed deletion; cleanup remains the original runtime's job.
func NewDeletionHTTPHandler(s *meeting.Service, b *account.HTTPBoundary) (http.Handler, error) {
	if s == nil || b == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &deletionHandler{service: s, boundary: b}, nil
}

func deletionRouteFor(path string) route {
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 4 && len(parts) != 5 || parts[1] != "meetings" || parts[3] != "deletion" {
		return route{}
	}
	for _, part := range parts {
		if part == "" {
			return route{}
		}
	}
	if len(parts) == 4 {
		return route{project: parts[0], meeting: parts[2], action: "deletion"}
	}
	if parts[4] == "lookup" {
		return route{project: parts[0], meeting: parts[2], action: "deletion-lookup"}
	}
	return route{}
}

func HandlesDeletionPath(path string) bool { return deletionRouteFor(path).action != "" }

func (h *deletionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	path := route{}
	if r.URL != nil {
		path = deletionRouteFor(r.URL.Path)
	}
	if path.action != "" {
		r.Pattern = "/api/v1/projects/{project_id}/meetings/{meeting_id}/deletion"
		if path.action == "deletion-lookup" {
			r.Pattern += "/lookup"
		}
	}
	r = r.WithContext(ctx)
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	request := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := request.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || request.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.execute(w, r, path)
	if expired(ctx) || request.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
	if expired(ctx) || request.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *deletionHandler) execute(w http.ResponseWriter, r *http.Request, path route) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if path.action == "" {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalid()
	}
	project, err := f.ParseID[i.Project](path.project)
	if err != nil {
		return nil, invalid()
	}
	meetingID, err := f.ParseID[c.Meeting](path.meeting)
	if err != nil {
		return nil, invalid()
	}
	keys := headerValues(r, "Idempotency-Key")
	if len(keys) != 1 {
		return nil, invalid()
	}
	meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0])}
	if meta.Validate() != nil {
		return nil, invalid()
	}
	var body struct {
		Expected f.Version `json:"expected_version"`
	}
	if err := httpapi.DecodeJSON(w, r, &body, 16<<10); err != nil {
		return nil, err
	}
	command := c.DeleteRequest{ProjectID: project, MeetingID: meetingID, ExpectedVersion: body.Expected, Meta: meta}
	if err := command.Validate(); err != nil {
		return nil, err
	}
	var receipt c.DeleteReceipt
	found := true
	if path.action == "deletion" {
		receipt, err = h.service.Delete(r.Context(), actor, command)
	} else {
		receipt, found, err = h.service.LookupDelete(r.Context(), actor, command)
	}
	if err != nil {
		return nil, err
	}
	if found && (receipt.Validate() != nil || receipt.MeetingID != meetingID || receipt.AcceptedVersion != body.Expected+1) {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	var value any = receipt
	if path.action == "deletion-lookup" {
		if found {
			value = struct {
				Status  string          `json:"status"`
				Receipt c.DeleteReceipt `json:"receipt"`
			}{"committed", receipt}
		} else {
			value = struct {
				Status string `json:"status"`
			}{"not_found"}
		}
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 64<<10 {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return raw, nil
}
