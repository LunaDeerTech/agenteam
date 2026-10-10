// Package skillhttp exposes the bounded current Human Owner Skill directory.
package skillhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const projectPrefix = "/api/v1/projects/"
const readBudget = 2 * time.Second

type reader interface {
	ListSkills(context.Context, id.Actor, id.ProjectID) ([]sc.Metadata, error)
	GetSkill(context.Context, id.Actor, id.ProjectID, sc.SkillID) (sc.Metadata, error)
}
type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type handler struct {
	reader   reader
	boundary accountBoundary
}

// NewHTTPHandler retains the caller's real instances. It neither starts their
// lifecycle nor installs another authority, source resolver or package reader.
func NewHTTPHandler(service *skill.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{service, boundary}, nil
}

type route struct {
	project, target string
	matched         bool
}

func resourceRoute(path string) route {
	if !strings.HasPrefix(path, projectPrefix) {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if (len(parts) != 2 && len(parts) != 3) || parts[1] != "skills" {
		return route{}
	}
	for _, part := range parts {
		if part == "" {
			return route{}
		}
	}
	r := route{project: parts[0], matched: true}
	if len(parts) == 3 {
		r.target = parts[2]
	}
	return r
}

// HandlesPath identifies only this adapter's two structural routes. Identifier
// validation and current authorization still occur during ServeHTTP.
func HandlesPath(path string) bool { return resourceRoute(path).matched }
func (r route) pattern() string {
	if !r.matched {
		return ""
	}
	p := projectPrefix + "{project_id}/skills"
	if r.target != "" {
		p += "/{skill_id}"
	}
	return p
}

func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(request.Context(), readBudget)
	defer cancel()
	route := route{}
	if request.URL != nil {
		route = resourceRoute(request.URL.Path)
	}
	request.Pattern = route.pattern()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	budget := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.execute(w, r, route)
	if expired(ctx) || budget.closeBody() != nil || expired(ctx) {
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
	if expired(ctx) || budget.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *handler) execute(w http.ResponseWriter, r *http.Request, route route) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	a, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if a.Validate() != nil || a.Details().Kind != id.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return nil, invalidInput()
	}
	if !route.matched {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	if r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	var target sc.SkillID
	if route.target != "" {
		target, err = f.ParseID[pc.Skill](route.target)
		if err != nil || target.String() != route.target {
			return nil, invalidInput()
		}
	}
	if err := emptyBody(r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if route.target == "" {
		items, err := h.reader.ListSkills(r.Context(), a, project)
		if err != nil {
			return nil, err
		}
		return encodeDirectory(r.Context(), project, items)
	}
	item, err := h.reader.GetSkill(r.Context(), a, project, target)
	if err != nil {
		return nil, err
	}
	return encodeDetail(r.Context(), project, target, item)
}
