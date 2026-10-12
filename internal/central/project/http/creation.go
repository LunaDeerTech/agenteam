package projecthttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const creationPrefix = "/api/v1/project-creations/"
const creationBudget = 30 * time.Second

type projectCreator interface {
	CreateProject(context.Context, id.Actor, f.CommandMeta, pc.CreateProjectRequest) (pc.CreationResult, error)
	GetCreation(context.Context, id.Actor, pc.CreationID) (pc.CreationResult, error)
}

type projectCreationHTTP struct {
	service  projectCreator
	boundary accountBoundary
}

// NewCreationHTTPHandler binds only creation and current-owner status reads.
// Unknown recovery replays the original Create identity; no key enumeration
// or lifecycle capability is added by this handler.
func NewCreationHTTPHandler(service *project.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &projectCreationHTTP{service, boundary}, nil
}

func creationResource(path string) (raw string, status bool, ok bool) {
	if path == projectsPath {
		return "", false, true
	}
	if !strings.HasPrefix(path, creationPrefix) {
		return "", false, false
	}
	raw = strings.TrimPrefix(path, creationPrefix)
	return raw, true, raw != "" && !strings.Contains(raw, "/")
}

func HandlesCreationRequest(method, path string) bool {
	_, status, ok := creationResource(path)
	return ok && (status || method != http.MethodGet && method != http.MethodHead)
}

func (h *projectCreationHTTP) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	limit := creationBudget
	if request.URL != nil {
		_, status, ok := creationResource(request.URL.Path)
		if ok {
			request.Pattern = projectsPath
			if status {
				request.Pattern = creationPrefix + "{id}"
				limit = readBudget
			}
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), limit)
	defer cancel()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	io := &updateIOWriter{ResponseWriter: w}
	// Reuse the original stream owner, deadline capabilities and synchronous
	// cancellation/Close tail already used by Project metadata commands.
	budget := requestIO{controller: http.NewResponseController(io), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !io.prepare() || budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, code, location, err := h.execute(w, r)
	if expired(ctx) || budget.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(code)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
	if expired(ctx) || budget.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *projectCreationHTTP) execute(w http.ResponseWriter, r *http.Request) ([]byte, int, string, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, 0, "", err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	raw, status, ok := creationResource(r.URL.Path)
	if !ok {
		return nil, 0, "", f.NewFault(f.NotFound, f.NotStarted)
	}
	if status && r.Method != http.MethodGet && r.Method != http.MethodHead || !status && r.Method != http.MethodPost {
		allow := "GET, HEAD, POST"
		if status {
			allow = "GET, HEAD"
		}
		w.Header().Set("Allow", allow)
		return nil, 0, "", f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, 0, "", err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, 0, "", f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, 0, "", invalidQuery()
	}
	if status {
		creation, err := f.ParseID[pc.Creation](raw)
		if err != nil {
			return nil, 0, "", invalidQuery()
		}
		if err = emptyBody(r); err != nil {
			return nil, 0, "", err
		}
		if expired(r.Context()) {
			panic(http.ErrAbortHandler)
		}
		value, err := h.service.GetCreation(r.Context(), actor, creation)
		if err != nil {
			return nil, 0, "", err
		}
		body, _, err := encodeCreation(r.Context(), actor, pc.ProjectID{}, creation, value)
		return body, http.StatusOK, "", err
	}
	key, err := updateKey(r)
	if err != nil {
		return nil, 0, "", err
	}
	meta, input, err := decodeCreation(w, r, key)
	if err != nil {
		return nil, 0, "", err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	value, err := h.service.CreateProject(r.Context(), actor, meta, input)
	if err != nil {
		return nil, 0, "", err
	}
	body, location, err := encodeCreation(r.Context(), actor, input.ProjectID, pc.CreationID{}, value)
	code := http.StatusCreated
	if value.State == pc.CreationPending {
		code = http.StatusAccepted
	}
	return body, code, location, err
}
