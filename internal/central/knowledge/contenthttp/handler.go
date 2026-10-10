// Package contenthttp exposes bounded current Knowledge content to Human Owners.
package contenthttp

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
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

const projectPrefix = "/api/v1/projects/"
const contentBudget = 2 * time.Second

type contentReader interface {
	ReadDocument(context.Context, id.Actor, id.ProjectID, kc.DocumentID, kc.ReadRequest) (kc.DocumentContent, error)
}
type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type handler struct {
	reader contentReader
	boundary accountBoundary
}

// NewHTTPHandler binds real caller-owned services without starting their work.
func NewHTTPHandler(service *knowledge.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{service, boundary}, nil
}

type route struct {
	project, document string
	valid bool
}

func contentRoute(path string) route {
	if !strings.HasPrefix(path, projectPrefix) {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(parts) != 5 || parts[1] != "knowledge" || parts[2] != "documents" || parts[4] != "content" || parts[0] == "" || parts[3] == "" {
		return route{}
	}
	return route{parts[0], parts[3], true}
}

func HandlesPath(path string) bool { return contentRoute(path).valid }

func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(request.Context(), contentBudget)
	defer cancel()
	var route route
	if request.URL != nil {
		route = contentRoute(request.URL.Path)
	}
	request.Pattern = ""
	if route.valid {
		request.Pattern = projectPrefix + "{project_id}/knowledge/documents/{document_id}/content"
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
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	document, err := f.ParseID[kc.Document](route.document)
	if err != nil || document.String() != route.document {
		return nil, invalidInput()
	}
	query, err := parseQuery(r)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Content-Encoding", "Range", "If-Range"} {
		if len(r.Header.Values(name)) != 0 {
			return nil, invalidInput()
		}
	}
	if err := emptyBody(r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	content, err := h.reader.ReadDocument(r.Context(), actor, project, document, query)
	if err != nil {
		return nil, err
	}
	return encodeContent(r.Context(), project, document, query, content)
}
