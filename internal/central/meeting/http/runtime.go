package meetinghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type runtimeService interface {
	GetContributionRuntime(context.Context, i.Actor, i.ProjectID, c.MeetingID, c.ContributionID) (c.ContributionRuntimeView, error)
}
type runtimeHandler struct {
	service  runtimeService
	boundary boundary
}

func NewRuntimeHTTPHandler(service *meeting.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &runtimeHandler{service: service, boundary: boundary}, nil
}
func runtimeRouteFor(path string) route {
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 6 || parts[1] != "meetings" || parts[3] != "contributions" || parts[5] != "runtime" {
		return route{}
	}
	for _, part := range parts {
		if part == "" {
			return route{}
		}
	}
	return route{project: parts[0], meeting: parts[2], contribution: parts[4], action: "runtime"}
}
func HandlesRuntimePath(path string) bool { return runtimeRouteFor(path).action != "" }

func (h *runtimeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	path := route{}
	if r.URL != nil {
		path = runtimeRouteFor(r.URL.Path)
	}
	if path.action != "" {
		r.Pattern = "/api/v1/projects/{project_id}/meetings/{meeting_id}/contributions/{contribution_id}/runtime"
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
func (h *runtimeHandler) execute(w http.ResponseWriter, r *http.Request, path route) ([]byte, error) {
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
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
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
	contribution, err := f.ParseID[c.Contribution](path.contribution)
	if err != nil {
		return nil, invalid()
	}
	value, err := h.service.GetContributionRuntime(r.Context(), actor, project, meetingID, contribution)
	if err != nil {
		return nil, err
	}
	if value.Validate() != nil || value.Execution.ProjectID != project || value.Execution.MeetingID != meetingID || value.Execution.ContributionID != contribution {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > ec.MaxRuntimeViewBytes+(8<<10) {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return raw, nil
}
