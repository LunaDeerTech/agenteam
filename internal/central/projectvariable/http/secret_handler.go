package projectvariablehttp

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type secretPort interface {
	c.SecretCommands
	c.SecretQueries
}

type secretHandler struct {
	secrets  secretPort
	boundary accountBoundary
}

// NewSecretHTTPHandler consumes the existing Owner service and Account browser
// boundary. The caller owns SecretService.Stop/Drain and all shared dependencies.
func NewSecretHTTPHandler(service *projectvariable.SecretService, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &secretHandler{service, boundary}, nil
}

func secretRoute(path string) route {
	if !strings.HasPrefix(path, projectPrefix) {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(parts) < 2 || len(parts) > 4 || parts[1] != "secret-variables" {
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
		r.kind, r.target = variable, parts[2]
	case 4:
		if parts[2] == "commands" && parts[3] == "lookup" {
			r.kind = commandLookup
		}
	}
	return r
}

func HandlesSecretPath(path string) bool { return secretRoute(path).kind != noResource }

func secretPattern(r route) string {
	suffix := map[resource]string{variables: "secret-variables", variable: "secret-variables/{variable_id}", commandLookup: "secret-variables/commands/lookup"}[r.kind]
	if suffix == "" {
		return ""
	}
	return projectPrefix + "{project_id}/" + suffix
}

func (h *secretHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	var route route
	if request.URL != nil {
		route = secretRoute(request.URL.Path)
	}
	request.Pattern = secretPattern(route)
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
		h.boundary.WriteProblem(w, r, secretProblem(err))
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

func (h *secretHandler) execute(w http.ResponseWriter, r *http.Request, route route) ([]byte, error) {
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
	if route.kind == noResource {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if !slices.Contains(strings.Split(route.allow(), ", "), r.Method) {
		w.Header().Set("Allow", route.allow())
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](route.project)
	if err != nil {
		return nil, invalidInput()
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return h.read(r, actor, project, route)
	}
	return h.command(r, actor, project, route)
}
