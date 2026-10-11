package agenthttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

type runnerMountQueries interface {
	ListAvailableRunners(context.Context, i.Actor, i.ProjectID, f.PageRequest) (f.Page[rc.ConfigurationCandidate], error)
	GetRunnerMounts(context.Context, i.Actor, i.ProjectID, i.AgentID) (c.AgentRunnerMounts, error)
}
type runnerMountHandler struct {
	reader   runnerMountQueries
	boundary directoryBoundary
}

func NewRunnerMountHTTPHandler(reader *agent.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if reader == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &runnerMountHandler{reader, boundary}, nil
}

type runnerMountRoute struct {
	project, target string
	valid           bool
}

func parseRunnerMountRoute(path string) runnerMountRoute {
	if !strings.HasPrefix(path, directoryPrefix) {
		return runnerMountRoute{}
	}
	parts := strings.Split(strings.TrimPrefix(path, directoryPrefix), "/")
	if len(parts) == 2 && parts[1] == "available-runners" && parts[0] != "" {
		return runnerMountRoute{project: parts[0], valid: true}
	}
	if len(parts) == 4 && parts[1] == "agents" && parts[3] == "runner-mounts" && parts[0] != "" && parts[2] != "" {
		return runnerMountRoute{project: parts[0], target: parts[2], valid: true}
	}
	out := runnerMountRoute{}

	return out
}
func HandlesRunnerMountPath(path string) bool { return parseRunnerMountRoute(path).valid }
func (h *runnerMountHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	route := runnerMountRoute{}
	if request.URL != nil {
		route = parseRunnerMountRoute(request.URL.Path)
	}
	if route.valid {
		request.Pattern = directoryPrefix + "{project_id}/available-runners"
		if route.target != "" {
			request.Pattern = directoryPrefix + "{project_id}/agents/{agent_id}/runner-mounts"
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
func (h *runnerMountHandler) execute(w http.ResponseWriter, r *http.Request, route runnerMountRoute) ([]byte, error) {
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
	if page.Limit > 50 {
		return nil, invalidInput()
	}
	if err = emptyBody(r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if route.target == "" {
		out, err := h.reader.ListAvailableRunners(r.Context(), actor, project, page)
		if err != nil {
			return nil, err
		}
		if err = validateRunnerCandidatePage(out, page); err != nil {
			return nil, err
		}
		return encodeDirectory(r.Context(), out, 2<<20)
	}
	target, err := f.ParseID[i.Agent](route.target)
	if err != nil {
		return nil, invalidInput()
	}
	out, err := h.reader.GetRunnerMounts(r.Context(), actor, project, target)
	if err != nil {
		return nil, err
	}
	if out.Validate() != nil || out.ProjectID != project || out.AgentID != target {
		return nil, badProjection()
	}
	return encodeDirectory(r.Context(), out, c.MaxAgentCoreBytes)
}

func validateRunnerCandidatePage(out f.Page[rc.ConfigurationCandidate], page f.PageRequest) error {
	if out.Items == nil || len(out.Items) > page.Limit || page.Limit > 50 || len(out.NextCursor) > cursor.MaxTokenBytes || !utf8.ValidString(out.NextCursor) || strings.ContainsRune(out.NextCursor, 0) || out.NextCursor != "" && len(out.Items) != page.Limit {
		return badProjection()
	}
	for n, item := range out.Items {
		if item.Validate() != nil || n > 0 && out.Items[n-1].ID.String() >= item.ID.String() {
			return badProjection()
		}
	}
	return nil
}
