// Package agenthttp exposes the current Human Owner's safe Agent directory.
package agenthttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const directoryPrefix = "/api/v1/projects/"
const readBudget = 2 * time.Second

type directoryBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (i.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type directoryHandler struct {
	reader   c.DirectoryQueries
	boundary directoryBoundary
}

func NewDirectoryHTTPHandler(reader *agent.DirectoryReader, boundary *account.HTTPBoundary) (http.Handler, error) {
	if reader == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &directoryHandler{reader, boundary}, nil
}

type directoryRoute struct {
	project, target string
	valid           bool
}

func parseDirectoryRoute(path string) directoryRoute {
	if !strings.HasPrefix(path, directoryPrefix) {
		return directoryRoute{}
	}
	parts := strings.Split(strings.TrimPrefix(path, directoryPrefix), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[1] != "agents" {
		return directoryRoute{}
	}
	for _, p := range parts {
		if p == "" {
			return directoryRoute{}
		}
	}
	out := directoryRoute{project: parts[0], valid: true}
	if len(parts) == 3 {
		out.target = parts[2]
	}
	return out
}
func HandlesDirectoryPath(path string) bool { return parseDirectoryRoute(path).valid }
func (h *directoryHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	route := directoryRoute{}
	if request.URL != nil {
		route = parseDirectoryRoute(request.URL.Path)
	}
	if route.valid {
		request.Pattern = directoryPrefix + "{project_id}/agents"
		if route.target != "" {
			request.Pattern += "/{agent_id}"
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), readBudget)
	defer cancel()
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
func (h *directoryHandler) execute(w http.ResponseWriter, r *http.Request, route directoryRoute) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
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
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return nil, invalidInput()
	}
	if !route.valid {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[i.Project](route.project)
	if err != nil {
		return nil, invalidInput()
	}
	page, err := parseDirectoryQuery(r, route.target == "")
	if err != nil {
		return nil, err
	}
	if err = emptyBody(r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if route.target == "" {
		out, err := h.reader.ListAgents(r.Context(), actor, project, page)
		if err != nil {
			return nil, err
		}
		if err = validateDirectoryPage(out, project, page); err != nil {
			return nil, err
		}
		return encodeDirectory(r.Context(), out, 13<<20)
	}
	target, err := f.ParseID[i.Agent](route.target)
	if err != nil {
		return nil, invalidInput()
	}
	out, err := h.reader.GetAgent(r.Context(), actor, project, target)
	if err != nil {
		return nil, err
	}
	if out.Validate() != nil || out.ProjectID != project || out.ID != target {
		return nil, badProjection()
	}
	return encodeDirectory(r.Context(), out, c.MaxDirectoryEntryBytes)
}
