// Package projectvariablehttp exposes current Human Owner ordinary variables.
// Every library operation reauthorizes the current Session/Project in its Tx.
package projectvariablehttp

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const projectPrefix = "/api/v1/projects/"
const readBudget = 2 * time.Second
const mutationBudget = 30 * time.Second

type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type variablePort interface {
	c.Commands
	c.Queries
}
type handler struct {
	variables variablePort
	boundary  accountBoundary
}

func NewHTTPHandler(service *projectvariable.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{service, boundary}, nil
}

type resource uint8

const (
	noResource resource = iota
	variables
	variable
	commandLookup
)

type route struct {
	kind            resource
	project, target string
}

func resourceRoute(path string) route {
	if !strings.HasPrefix(path, projectPrefix) {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(parts) < 2 || len(parts) > 4 || parts[1] != "variables" {
		return route{}
	}
	for _, part := range parts {
		if part == "" {
			return route{}
		}
	}
	r := route{project: parts[0]}
	switch len(parts) {
	case 2:
		r.kind = variables
	case 3:
		r.kind = variable
		r.target = parts[2]
	case 4:
		if parts[2] == "commands" && parts[3] == "lookup" {
			r.kind = commandLookup
		}
	}
	return r
}
func HandlesPath(path string) bool { return resourceRoute(path).kind != noResource }
func (r route) lookup() bool       { return r.kind == commandLookup }
func (r route) allow() string {
	switch r.kind {
	case variables:
		return "GET, HEAD, POST"
	case variable:
		return "GET, HEAD, PATCH, DELETE"
	case commandLookup:
		return "POST"
	}
	return ""
}
func (r route) pattern() string {
	suffix := map[resource]string{variables: "variables", variable: "variables/{variable_id}", commandLookup: "variables/commands/lookup"}[r.kind]
	if suffix == "" {
		return ""
	}
	return projectPrefix + "{project_id}/" + suffix
}
func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	route := route{}
	if request.URL != nil {
		route = resourceRoute(request.URL.Path)
	}
	request.Pattern = route.pattern()
	limit := mutationBudget
	if request.Method == http.MethodGet || request.Method == http.MethodHead || route.lookup() || route.kind == noResource {
		limit = readBudget
	}
	ctx, cancel := context.WithTimeout(request.Context(), limit)
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
	if route.kind == noResource {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if !slices.Contains(strings.Split(route.allow(), ", "), r.Method) {
		w.Header().Set("Allow", route.allow())
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	p, err := f.ParseID[id.Project](route.project)
	if err != nil {
		return nil, invalidInput()
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return h.read(r, a, p, route)
	}
	return h.command(w, r, a, p, route)
}
