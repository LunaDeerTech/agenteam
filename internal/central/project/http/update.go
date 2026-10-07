package projecthttp

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const updateBudget = 30 * time.Second
const updateLookupSuffix = "/commands/lookup"

type projectUpdater interface {
	UpdateProject(context.Context, id.Actor, f.CommandMeta, pc.ProjectID, pc.UpdateProjectRequest) (pc.ProjectRef, error)
	LookupCommand(context.Context, id.Actor, pc.CommandLookupRequest) (pc.CommandLookupResult, error)
}
type projectUpdateHTTP struct {
	service  projectUpdater
	boundary accountBoundary
}

// Resolve capabilities without performing I/O. ResponseController itself follows
// Unwrap without a bound; resolving here also keeps malformed wrapper cycles out
// of both initialization and the synchronous abort/Close tail.
type updateIOWriter struct {
	http.ResponseWriter
	read  func(time.Time) error
	write func(time.Time) error
	flush func() error
}

func (io *updateIOWriter) prepare() bool {
	w := io.ResponseWriter
	for depth := 0; depth < 64 && w != nil; depth++ {
		if io.read == nil {
			if v, ok := w.(interface{ SetReadDeadline(time.Time) error }); ok {
				io.read = v.SetReadDeadline
			}
		}
		if io.write == nil {
			if v, ok := w.(interface{ SetWriteDeadline(time.Time) error }); ok {
				io.write = v.SetWriteDeadline
			}
		}
		if io.flush == nil {
			switch v := w.(type) {
			case interface{ FlushError() error }:
				io.flush = v.FlushError
			case http.Flusher:
				io.flush = func() error { v.Flush(); return nil }
			}
		}
		if io.read != nil && io.write != nil && io.flush != nil {
			return true
		}
		v, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		w = v.Unwrap()
	}
	return false
}
func (w *updateIOWriter) SetReadDeadline(at time.Time) error {
	if w.read == nil {
		return http.ErrNotSupported
	}
	return w.read(at)
}
func (w *updateIOWriter) SetWriteDeadline(at time.Time) error {
	if w.write == nil {
		return http.ErrNotSupported
	}
	return w.write(at)
}
func (w *updateIOWriter) FlushError() error {
	if w.flush == nil {
		return http.ErrNotSupported
	}
	return w.flush()
}

// NewUpdateHTTPHandler binds only Owner metadata updates and update-key lookup.
func NewUpdateHTTPHandler(service *project.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &projectUpdateHTTP{service, boundary}, nil
}
func updateResource(path string) (raw string, lookup bool, ok bool) {
	if strings.HasSuffix(path, updateLookupSuffix) {
		kind, raw := resourcePath(strings.TrimSuffix(path, updateLookupSuffix))
		return raw, true, kind == detailResource
	}
	kind, raw := resourcePath(path)
	return raw, false, kind == detailResource
}

// HandlesUpdateRequest leaves every GET/HEAD detail with the existing Reader.
func HandlesUpdateRequest(method, path string) bool {
	_, lookup, ok := updateResource(path)
	return ok && (lookup || method != http.MethodGet && method != http.MethodHead)
}
func (h *projectUpdateHTTP) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	limit := updateBudget
	if request.URL != nil {
		_, lookup, ok := updateResource(request.URL.Path)
		if ok {
			request.Pattern = projectPrefix + "{id}"
			if lookup {
				request.Pattern += updateLookupSuffix
				limit = readBudget
			}
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), limit)
	defer cancel()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	io := &updateIOWriter{ResponseWriter: w}
	// Keep ownership of the original stream: DecodeJSON's MaxBytesReader only
	// wraps it and is never independently closed by this handler.
	budget := requestIO{controller: http.NewResponseController(io), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !io.prepare() {
		panic(http.ErrAbortHandler)
	}
	if err := budget.start(); err != nil {
		panic(http.ErrAbortHandler)
	}
	// This synchronous call also owns the library's original WithoutCancel
	// confirmation tail. Its completion cannot extend the publication deadline.
	body, err := h.execute(w, r)
	if expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if budget.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
	if expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if budget.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}
func (h *projectUpdateHTTP) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	raw, lookup, ok := updateResource(r.URL.Path)
	if !ok {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	wanted, allow := http.MethodPatch, "GET, HEAD, PATCH"
	if lookup {
		wanted, allow = http.MethodPost, "POST"
	}
	if r.Method != wanted {
		w.Header().Set("Allow", allow)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
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
	target, err := f.ParseID[id.Project](raw)
	if err != nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidQuery()
	}
	key, err := updateKey(r)
	if err != nil {
		return nil, err
	}
	if lookup {
		input, err := decodeUpdateLookup(w, r, target, key)
		if err != nil {
			return nil, err
		}
		if expired(r.Context()) {
			panic(http.ErrAbortHandler)
		}
		value, err := h.service.LookupCommand(r.Context(), actor, input)
		if err != nil {
			return nil, err
		}
		return encodeUpdateLookup(r.Context(), actor, target, value)
	}
	meta, input, err := decodeUpdate(w, r, key)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	value, err := h.service.UpdateProject(r.Context(), actor, meta, target, input)
	if err != nil {
		return nil, err
	}
	return encodeUpdate(r.Context(), actor, target, meta, input, value)
}
