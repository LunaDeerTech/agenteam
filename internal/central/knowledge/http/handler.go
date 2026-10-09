// Package knowledgehttp exposes only current Human Owner metadata and tree reads.
package knowledgehttp

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
const readBudget = 2 * time.Second

type reader interface {
	GetDocument(context.Context, id.Actor, id.ProjectID, kc.DocumentID) (kc.DocumentHead, error)
	ListDocuments(context.Context, id.Actor, id.ProjectID, kc.ListFilter, f.PageRequest) (f.Page[kc.DocumentRef], error)
	ListChildren(context.Context, id.Actor, id.ProjectID, *kc.DocumentID, kc.ListFilter, f.PageRequest) (f.Page[kc.DocumentRef], error)
	ReadAncestors(context.Context, id.Actor, id.ProjectID, kc.DocumentID) ([]kc.DocumentRef, error)
	SearchTitles(context.Context, id.Actor, id.ProjectID, string, f.PageRequest) (f.Page[kc.TitleHit], error)
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

// NewHTTPHandler binds the caller's real instances without starting work or
// installing a mutable provider. Their lifecycle remains owned by the caller.
func NewHTTPHandler(service *knowledge.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{service, boundary}, nil
}

type resource uint8

const (
	noResource resource = iota
	documents
	document
	children
	ancestors
	searchTitles
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
	if len(parts) < 3 || len(parts) > 5 || parts[1] != "knowledge" || parts[2] != "documents" {
		return route{}
	}
	for _, part := range parts {
		if part == "" {
			return route{}
		}
	}
	r := route{project: parts[0]}
	switch len(parts) {
	case 3:
		r.kind = documents
	case 4:
		switch parts[3] {
		case "children":
			r.kind = children
		case "search-titles":
			r.kind = searchTitles
		default:
			r.kind, r.target = document, parts[3]
		}
	case 5:
		if parts[4] == "ancestors" && parts[3] != "children" && parts[3] != "search-titles" {
			r.kind, r.target = ancestors, parts[3]
		}
	}
	return r
}
func HandlesPath(path string) bool { return resourceRoute(path).kind != noResource }
func (r route) pattern() string {
	prefix := projectPrefix + "{project_id}/knowledge/documents"
	switch r.kind {
	case documents:
		return prefix
	case document:
		return prefix + "/{document_id}"
	case children:
		return prefix + "/children"
	case ancestors:
		return prefix + "/{document_id}/ancestors"
	case searchTitles:
		return prefix + "/search-titles"
	}
	return ""
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
	if route.kind == noResource {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	p, err := f.ParseID[id.Project](route.project)
	if err != nil || p.String() != route.project {
		return nil, invalidInput()
	}
	q, err := parseQuery(r, route.kind)
	if err != nil {
		return nil, err
	}
	var target kc.DocumentID
	if route.target != "" {
		target, err = f.ParseID[kc.Document](route.target)
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
	switch route.kind {
	case documents, children:
		var page f.Page[kc.DocumentRef]
		if route.kind == documents {
			page, err = h.reader.ListDocuments(r.Context(), a, p, q.filter, q.page)
		} else {
			page, err = h.reader.ListChildren(r.Context(), a, p, q.parent, q.filter, q.page)
		}
		if err != nil {
			return nil, err
		}
		return encodePage(r.Context(), p, route.kind, q, page)
	case document:
		head, err := h.reader.GetDocument(r.Context(), a, p, target)
		if err != nil {
			return nil, err
		}
		return encodeHead(r.Context(), p, target, head)
	case ancestors:
		items, err := h.reader.ReadAncestors(r.Context(), a, p, target)
		if err != nil {
			return nil, err
		}
		return encodeAncestors(r.Context(), p, target, items)
	case searchTitles:
		page, err := h.reader.SearchTitles(r.Context(), a, p, q.filter.TitleQuery, q.page)
		if err != nil {
			return nil, err
		}
		return encodeHits(r.Context(), p, q, page)
	}
	return nil, invalidInput()
}
