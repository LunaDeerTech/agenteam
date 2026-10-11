// Package schedulerhttp exposes the existing Owner Project scheduler configuration.
// It owns transport only; scheduling and command lifetime remain with Project.
package schedulerhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const (
	projectPrefix   = "/api/v1/projects/"
	schedulerSuffix = "/scheduler"
	readBudget      = 2 * time.Second
	writeBudget     = 30 * time.Second
)

type service interface {
	GetSchedulerConfig(context.Context, i.Actor, pc.ProjectID) (pc.SchedulerProject, error)
	UpdateProject(context.Context, i.Actor, f.CommandMeta, pc.ProjectID, pc.UpdateProjectRequest) (pc.ProjectRef, error)
}
type boundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (i.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type handler struct {
	service  service
	boundary boundary
}

// NewHTTPHandler retains the root's concrete Service and Account boundary.
// It creates no runtime, service, background task, or authority.
func NewHTTPHandler(core *project.Service, auth *account.HTTPBoundary) (http.Handler, error) {
	if core == nil || auth == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{service: core, boundary: auth}, nil
}

func resource(path string) (string, bool) {
	relative, ok := strings.CutPrefix(path, projectPrefix)
	if !ok {
		return "", false
	}
	target, ok := strings.CutSuffix(relative, schedulerSuffix)
	return target, ok && target != "" && !strings.Contains(target, "/")
}

// HandlesPath owns all methods at exactly this resource, including its 405s.
func HandlesPath(path string) bool { _, ok := resource(path); return ok }

func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	budget := readBudget
	if request.Method == http.MethodPatch {
		budget = writeBudget
	}
	request.Pattern = ""
	if request.URL != nil && HandlesPath(request.URL.Path) {
		request.Pattern = projectPrefix + "{project_id}" + schedulerSuffix
	}
	ctx, cancel := context.WithTimeout(request.Context(), budget)
	defer cancel()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	writer := &deadlineWriter{ResponseWriter: w}
	owner := requestIO{controller: http.NewResponseController(writer), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := owner.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !writer.prepare() || owner.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.execute(w, r)
	if expired(ctx) || owner.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
	if expired(ctx) || owner.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *handler) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if r.URL == nil {
		return nil, invalid()
	}
	raw, ok := resource(r.URL.Path)
	if !ok {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPatch {
		w.Header().Set("Allow", "GET, HEAD, PATCH")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	target, err := f.ParseID[i.Project](raw)
	if err != nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalid()
	}
	if r.Method != http.MethodPatch {
		if err := emptyBody(r); err != nil {
			return nil, err
		}
		if expired(r.Context()) {
			panic(http.ErrAbortHandler)
		}
		value, err := h.service.GetSchedulerConfig(r.Context(), actor, target)
		if err != nil {
			return nil, err
		}
		return encodeCurrent(r.Context(), actor, target, value)
	}
	meta, input, err := decodeUpdate(w, r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	// Keep the original call and any bounded Project confirmation tail owned
	// until they actually return. Do not refresh here or synthesize a receipt.
	value, err := h.service.UpdateProject(r.Context(), actor, meta, target, input)
	if err != nil {
		return nil, err
	}
	return encodeReceipt(r.Context(), actor, target, meta, value)
}
