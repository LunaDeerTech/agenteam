// Package executionhttp exposes Task-scoped execution history to current Human Owners.
package executionhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const projectPrefix = "/api/v1/projects/"
const requestBudget = 2 * time.Second

type executionReader interface {
	List(context.Context, id.Actor, id.ProjectID, string, f.PageRequest) (ec.TaskExecutionPage, error)
	Get(context.Context, id.Actor, id.ProjectID, string, id.ExecutionID) (ec.TaskExecution, error)
	GetRuntimeView(context.Context, id.Actor, id.ProjectID, string, id.ExecutionID) (ec.TaskRuntimeView, error)
}
type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type handler struct {
	reader   executionReader
	boundary accountBoundary
	controls ec.TaskExecutionCancellation
}

// NewHTTPHandler retains the original caller-owned service and boundary. It
// starts no workers and creates no replacement domain or lifecycle owner.
func NewHTTPHandler(service *execution.TaskExecutionReader, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{reader: service, boundary: boundary}, nil
}

func NewHTTPHandlerWithControls(service *execution.TaskExecutionReader, controls ec.TaskExecutionCancellation, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || controls == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{reader: service, boundary: boundary, controls: controls}, nil
}

type route struct {
	project, task, execution string
	valid, read              bool
	runtime                  bool
	cancel, lookup           bool
}

func executionRoute(path string) route {
	if !strings.HasPrefix(path, projectPrefix) {
		return route{}
	}
	p := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if (len(p) < 4 || len(p) > 7) || p[0] == "" || p[1] != "tasks" || p[2] == "" || p[3] != "executions" {
		return route{}
	}
	v := route{project: p[0], task: p[2], valid: true}
	if len(p) >= 5 {
		if p[4] == "" {
			return route{}
		}
		v.execution = p[4]
		v.read = true
	}
	if len(p) >= 6 {
		if p[5] != "runtime-view" && p[5] != "cancel" {
			return route{}
		}
		v.runtime = p[5] == "runtime-view"
		v.cancel = p[5] == "cancel"
	}
	if len(p) == 7 {
		if !v.cancel || p[6] != "lookup" {
			return route{}
		}
		v.lookup = true
	}
	return v
}
func HandlesPath(path string) bool { return executionRoute(path).valid }

func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(request.Context(), requestBudget)
	defer cancel()
	var route route
	if request.URL != nil {
		route = executionRoute(request.URL.Path)
	}
	request.Pattern = ""
	if route.valid {
		request.Pattern = projectPrefix + "{project_id}/tasks/{task_id}/executions"
		if route.read {
			request.Pattern += "/{execution_id}"
		}
		if route.runtime {
			request.Pattern += "/runtime-view"
		}
		if route.cancel {
			request.Pattern += "/cancel"
			if route.lookup {
				request.Pattern += "/lookup"
			}
		}
	}
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
	// The original reader has synchronously returned from its owned Store calls
	// before execute returns. Cancellation is not a substitute for that return.
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
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return nil, invalidInput()
	}
	if !route.valid {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if route.cancel {
		return h.cancelCommand(w, r, actor, route)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	task, err := f.ParseID[struct{}](route.task)
	if err != nil || task.String() != route.task {
		return nil, invalidInput()
	}
	for _, name := range []string{"Content-Encoding", "Range", "If-Range"} {
		if len(r.Header.Values(name)) != 0 {
			return nil, invalidInput()
		}
	}
	if err = emptyBody(r); err != nil {
		return nil, err
	}
	page, err := readQuery(r, route.read)
	if err != nil {
		return nil, err
	}
	if !route.read {
		out, err := h.reader.List(r.Context(), actor, project, route.task, page)
		if err != nil {
			return nil, err
		}
		return encodePage(project, route.task, page, out)
	}
	execution, err := f.ParseID[id.Execution](route.execution)
	if err != nil || execution.String() != route.execution {
		return nil, invalidInput()
	}
	if route.runtime {
		out, err := h.reader.GetRuntimeView(r.Context(), actor, project, route.task, execution)
		if err != nil {
			return nil, err
		}
		if out.Execution.ID != execution || validExecution(out.Execution, project, route.task) != nil || out.ValidateProjection() != nil {
			return nil, unavailableOutput()
		}
		raw, err := json.Marshal(out)
		if err != nil || len(raw) > ec.MaxRuntimeViewBytes {
			return nil, unavailableOutput()
		}
		return raw, nil
	}
	out, err := h.reader.Get(r.Context(), actor, project, route.task, execution)
	if err != nil {
		return nil, err
	}
	if out.ID != execution || validExecution(out, project, route.task) != nil {
		return nil, unavailableOutput()
	}
	return json.Marshal(struct {
		Execution ec.TaskExecution `json:"execution"`
	}{out})
}
