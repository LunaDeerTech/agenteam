// Package workhttp exposes current Human Owner planning through the existing
// Work services. It owns HTTP transport only; each service reauthorizes in Tx.
package workhttp

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

const projectPrefix = "/api/v1/projects/"
const readBudget = 2 * time.Second
const mutationBudget = 30 * time.Second

// Bindings preserves the exact production objects assembled with one Store,
// Authority, catalog and Activity authority. Test ports stay package-private.
type Bindings struct {
	Structure       *work.Service
	StructureReader *work.Reader
	Tasks           *work.TaskService
	TaskReader      *work.TaskReader
	Blockers        *work.BlockerService
	BlockerReader   *work.BlockerReader
}

type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type handler struct {
	structure       c.Commands
	structureReader c.Reader
	tasks           c.TaskCommands
	taskReader      c.TaskReader
	blockers        c.TaskBlockerCommands
	blockerReader   c.TaskBlockerPageReader
	boundary        accountBoundary
}

func NewHTTPHandler(b Bindings, boundary *account.HTTPBoundary) (http.Handler, error) {
	if b.Structure == nil || b.StructureReader == nil || b.Tasks == nil || b.TaskReader == nil || b.Blockers == nil || b.BlockerReader == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{b.Structure, b.StructureReader, b.Tasks, b.TaskReader, b.Blockers, b.BlockerReader, boundary}, nil
}

type resource uint8

const (
	noResource resource = iota
	milestones
	milestone
	milestoneReorder
	sprints
	sprint
	sprintReorder
	structureLookup
	tasks
	task
	taskReorder
	taskLookup
	blockers
	blockerResolve
	blockerLookup
)

type route struct {
	kind            resource
	project, target string
}

func parseRoute(path string) route {
	if !strings.HasPrefix(path, projectPrefix) {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(parts) < 2 || len(parts) > 4 {
		return route{}
	}
	for _, p := range parts {
		if p == "" {
			return route{}
		}
	}
	r := route{project: parts[0]}
	if len(parts) > 2 {
		r.target = parts[2]
	}
	switch parts[1] {
	case "milestones":
		switch {
		case len(parts) == 2:
			r.kind = milestones
		case len(parts) == 3:
			r.kind = milestone
		case parts[3] == "reorder":
			r.kind = milestoneReorder
		}
	case "sprints":
		switch {
		case len(parts) == 2:
			r.kind = sprints
		case len(parts) == 3:
			r.kind = sprint
		case parts[3] == "reorder":
			r.kind = sprintReorder
		}
	case "tasks":
		switch {
		case len(parts) == 2:
			r.kind = tasks
		case len(parts) == 3:
			r.kind = task
		case parts[3] == "reorder":
			r.kind = taskReorder
		case parts[3] == "blockers":
			r.kind = blockers
		}
	case "structure-commands":
		if len(parts) == 3 && parts[2] == "lookup" {
			r.kind = structureLookup
			r.target = ""
		}
	case "task-commands":
		if len(parts) == 3 && parts[2] == "lookup" {
			r.kind = taskLookup
			r.target = ""
		}
	}
	return r
}

// HandlesPath deliberately excludes all other Project/Model/Usage routes.
func HandlesPath(path string) bool { return resourceRoute(path).kind != noResource }
func resourceRoute(path string) route {
	// The two five-segment resources are matched separately, without cleaning or
	// decoding paths. Account owns canonical path and RawPath rejection.
	for suffix, kind := range map[string]resource{"/blockers/resolve": blockerResolve, "/blocker-commands/lookup": blockerLookup} {
		if strings.HasSuffix(path, suffix) {
			r := parseRoute(strings.TrimSuffix(path, suffix))
			if r.kind == task {
				r.kind = kind
				return r
			}
		}
	}
	return parseRoute(path)
}
func (r route) lookup() bool {
	return r.kind == structureLookup || r.kind == taskLookup || r.kind == blockerLookup
}
func (r route) allow() string {
	switch r.kind {
	case milestones, sprints, tasks, blockers:
		return "GET, HEAD, POST"
	case milestone, sprint, task:
		return "GET, HEAD, PATCH"
	case milestoneReorder, sprintReorder, taskReorder, structureLookup, taskLookup, blockerResolve, blockerLookup:
		return "POST"
	}
	return ""
}
func (r route) pattern() string {
	suffix := map[resource]string{milestones: "milestones", milestone: "milestones/{milestone_id}", milestoneReorder: "milestones/{milestone_id}/reorder", sprints: "sprints", sprint: "sprints/{sprint_id}", sprintReorder: "sprints/{sprint_id}/reorder", structureLookup: "structure-commands/lookup", tasks: "tasks", task: "tasks/{task_id}", taskReorder: "tasks/{task_id}/reorder", taskLookup: "task-commands/lookup", blockers: "tasks/{task_id}/blockers", blockerResolve: "tasks/{task_id}/blockers/resolve", blockerLookup: "tasks/{task_id}/blocker-commands/lookup"}[r.kind]
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
