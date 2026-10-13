package skillhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type assignmentCommands interface {
	ExecuteAssignment(context.Context, i.Actor, f.CommandMeta, skill.AssignmentRequest) (sc.AssignmentMutation, error)
	LookupAssignment(context.Context, i.Actor, f.CommandMeta, skill.AssignmentRequest) (skill.AssignmentLookup, error)
}
type assignmentReader interface {
	List(context.Context, i.Actor, i.ProjectID, i.AgentID, skill.OwnerAssignmentQuery) (skill.OwnerAssignmentPage, error)
}
type assignmentHandler struct {
	commands  assignmentCommands
	directory assignmentReader
	boundary  accountBoundary
}

func NewAssignmentHTTPHandler(commands *skill.AssignmentCommands, keys cursor.Keyring, boundary *account.HTTPBoundary) (http.Handler, error) {
	if commands == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	directory, err := skill.NewOwnerAssignments(commands, keys)
	if err != nil {
		return nil, err
	}
	return &assignmentHandler{commands: commands, directory: directory, boundary: boundary}, nil
}

type assignmentRoute struct{ project, agent, kind string }

func parseAssignmentRoute(path string) assignmentRoute {
	if !strings.HasPrefix(path, projectPrefix) {
		return assignmentRoute{}
	}
	parts := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(parts) < 4 || parts[0] == "" || parts[1] != "agents" || parts[2] == "" || parts[3] != "skills" {
		return assignmentRoute{}
	}
	route := assignmentRoute{project: parts[0], agent: parts[2]}
	switch {
	case len(parts) == 4:
		route.kind = "directory"
	case len(parts) == 5 && (parts[4] == "assign" || parts[4] == "remove"):
		route.kind = parts[4]
	case len(parts) == 6 && parts[4] == "commands" && parts[5] == "lookup":
		route.kind = "lookup"
	}
	return route
}

func HandlesAssignmentRequest(r *http.Request) bool {
	return r != nil && r.URL != nil && parseAssignmentRoute(r.URL.Path).kind != ""
}

func assignmentPattern(route assignmentRoute) string {
	if route.kind == "" {
		return ""
	}
	base := projectPrefix + "{project_id}/agents/{agent_id}/skills"
	if route.kind == "directory" {
		return base
	}
	if route.kind == "lookup" {
		return base + "/commands/lookup"
	}
	return base + "/" + route.kind
}

func (h *assignmentHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	route := assignmentRoute{}
	if request.URL != nil {
		route = parseAssignmentRoute(request.URL.Path)
	}
	duration := installBudget
	if route.kind == "directory" {
		duration = readBudget
	}
	request.Pattern = assignmentPattern(route)
	ctx, cancel := context.WithTimeout(request.Context(), duration)
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

func (h *assignmentHandler) execute(w http.ResponseWriter, r *http.Request, route assignmentRoute) ([]byte, error) {
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
	if route.kind == "" {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	allowed, methods := r.Method == http.MethodPost, "POST"
	if route.kind == "directory" {
		allowed, methods = r.Method == http.MethodGet || r.Method == http.MethodHead, "GET, HEAD"
	}
	if !allowed {
		w.Header().Set("Allow", methods)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[i.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	agent, err := f.ParseID[i.Agent](route.agent)
	if err != nil || agent.String() != route.agent {
		return nil, invalidInput()
	}
	if route.kind == "directory" {
		query, err := catalogQuery(r)
		if err != nil {
			return nil, err
		}
		if err = emptyBody(r); err != nil {
			return nil, err
		}
		input := skill.OwnerAssignmentQuery{Limit: query.Limit, Cursor: query.Cursor}
		page, err := h.directory.List(r.Context(), actor, project, agent, input)
		if err != nil {
			return nil, err
		}
		return encodeAssignments(r.Context(), project, agent, input, page)
	}
	meta, input, err := decodeAssignment(w, r, route, project, agent)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if route.kind == "lookup" {
		out, err := h.commands.LookupAssignment(r.Context(), actor, meta, input)
		if err != nil {
			return nil, err
		}
		if out.State != "not_observed" && out.State != "committed" || (out.State == "committed") != (out.Receipt != nil) {
			return nil, badProjection()
		}
		if out.Receipt != nil {
			if err = validateAssignmentMutation(meta, input, *out.Receipt); err != nil {
				return nil, err
			}
		}
		return encodeManagement(r.Context(), struct {
			State   string                 `json:"state"`
			Receipt *sc.AssignmentMutation `json:"receipt"`
		}{out.State, out.Receipt}, assignmentRepresentationLimit)
	}
	out, err := h.commands.ExecuteAssignment(r.Context(), actor, meta, input)
	if err != nil {
		return nil, err
	}
	if err = validateAssignmentMutation(meta, input, out); err != nil {
		return nil, err
	}
	return encodeManagement(r.Context(), out, assignmentRepresentationLimit)
}
