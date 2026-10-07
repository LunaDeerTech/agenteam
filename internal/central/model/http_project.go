package model

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const projectHTTPPrefix = "/api/v1/projects/"
const projectHTTPReadBudget = 2 * time.Second

type projectHTTPKind uint8

const (
	projectHTTPNone projectHTTPKind = iota
	projectHTTPProviders
	projectHTTPProvider
	projectHTTPModels
	projectHTTPModel
	projectHTTPAvailable
)

type projectHTTPReader interface {
	ListProjectProviders(context.Context, id.Actor, id.ProjectID, ProjectQuery) (ProviderPage, error)
	GetProjectProvider(context.Context, id.Actor, id.ProjectID, mc.ProviderID) (mc.ProviderView, error)
	ListProjectModels(context.Context, id.Actor, id.ProjectID, ProjectQuery) (ModelPage, error)
	GetProjectModel(context.Context, id.Actor, id.ProjectID, mc.ModelID) (mc.ModelView, error)
	ListAvailableChatModels(context.Context, id.Actor, id.ProjectID, ProjectQuery) (AvailableChatModelPage, error)
}
type projectHTTPBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type projectHTTP struct {
	core     projectHTTPReader
	boundary projectHTTPBoundary
}

// NewProjectHTTPHandler adds only the five current-Owner read operations. It
// retains the root's existing Model service and Account browser boundary.
func NewProjectHTTPHandler(core *Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	s := core.state()
	if s == nil || boundary == nil || nilPort(s.store) || s.authority.state() == nil ||
		nilPort(s.deps.Secret) || nilPort(s.deps.Audit) || nilPort(s.deps.Events) ||
		s.deps.Cursors.Validate() != nil || !s.deps.ConfigurationEvents.valid() {
		return nil, fault(f.DependencyUnbound)
	}
	a := s.authority.state()
	if nilPort(a.store) || !sameStore(s.store, a.store) || nilPort(a.auth.Sessions) ||
		nilPort(a.auth.System) || nilPort(a.auth.Projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &projectHTTP{core: core, boundary: boundary}, nil
}

// HandlesProjectHTTPPath performs no decoding or identity validation. Invalid
// IDs in an owned resource shape must reach its boundary, not another handler.
func HandlesProjectHTTPPath(path string) bool {
	kind, _, _ := projectHTTPResource(path)
	return kind != projectHTTPNone
}
func projectHTTPResource(path string) (projectHTTPKind, string, string) {
	if !strings.HasPrefix(path, projectHTTPPrefix) {
		return projectHTTPNone, "", ""
	}
	project, rest, ok := strings.Cut(strings.TrimPrefix(path, projectHTTPPrefix), "/")
	if !ok || project == "" {
		return projectHTTPNone, "", ""
	}
	switch rest {
	case "model-providers":
		return projectHTTPProviders, project, ""
	case "models":
		return projectHTTPModels, project, ""
	case "available-chat-models":
		return projectHTTPAvailable, project, ""
	}
	collection, target, ok := strings.Cut(rest, "/")
	if !ok || target == "" || strings.Contains(target, "/") {
		return projectHTTPNone, "", ""
	}
	switch collection {
	case "model-providers":
		return projectHTTPProvider, project, target
	case "models":
		return projectHTTPModel, project, target
	}
	return projectHTTPNone, "", ""
}
func projectHTTPPattern(kind projectHTTPKind) string {
	const root = projectHTTPPrefix + "{project_id}/"
	switch kind {
	case projectHTTPProviders:
		return root + "model-providers"
	case projectHTTPProvider:
		return root + "model-providers/{provider_id}"
	case projectHTTPModels:
		return root + "models"
	case projectHTTPModel:
		return root + "models/{model_id}"
	case projectHTTPAvailable:
		return root + "available-chat-models"
	}
	return "unknown_route"
}

// Capture real I/O capabilities without calling Flush (which could publish an
// empty 200). The finite unwrap walk also rejects malformed wrapper cycles.
// This follows the accepted Project Update boundary without changing it.
type projectHTTPIOWriter struct {
	http.ResponseWriter
	read  func(time.Time) error
	write func(time.Time) error
	flush func() error
}

func (w *projectHTTPIOWriter) prepare() bool {
	current := w.ResponseWriter
	for depth := 0; depth < 64 && current != nil; depth++ {
		if w.read == nil {
			if v, ok := current.(interface{ SetReadDeadline(time.Time) error }); ok {
				w.read = v.SetReadDeadline
			}
		}
		if w.write == nil {
			if v, ok := current.(interface{ SetWriteDeadline(time.Time) error }); ok {
				w.write = v.SetWriteDeadline
			}
		}
		if w.flush == nil {
			switch v := current.(type) {
			case interface{ FlushError() error }:
				w.flush = v.FlushError
			case http.Flusher:
				w.flush = func() error { v.Flush(); return nil }
			}
		}
		if w.read != nil && w.write != nil && w.flush != nil {
			return true
		}
		v, ok := current.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		current = v.Unwrap()
	}
	return false
}

// Deadline setters also run inside the cancellation callback and finish. A
// broken writer must not panic out of that goroutine or skip Close/actual join.
func (w *projectHTTPIOWriter) SetReadDeadline(at time.Time) (err error) {
	defer func() {
		if recover() != nil {
			err = http.ErrAbortHandler
		}
	}()
	if w.read == nil {
		return http.ErrNotSupported
	}
	return w.read(at)
}
func (w *projectHTTPIOWriter) SetWriteDeadline(at time.Time) (err error) {
	defer func() {
		if recover() != nil {
			err = http.ErrAbortHandler
		}
	}()
	if w.write == nil {
		return http.ErrNotSupported
	}
	return w.write(at)
}
func (w *projectHTTPIOWriter) FlushError() error {
	if w.flush == nil {
		return http.ErrNotSupported
	}
	return w.flush()
}

func (h *projectHTTP) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil || request.URL == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(request.Context(), projectHTTPReadBudget)
	defer cancel()
	kind, _, _ := projectHTTPResource(request.URL.Path)
	request.Pattern = projectHTTPPattern(kind)
	r := request.WithContext(ctx)
	io := &projectHTTPIOWriter{ResponseWriter: w}
	// Only the controller uses the capability adapter. Keep the business writer
	// on the original middleware chain so Problem retains RequestID, code and
	// committed-response ownership through the existing Unwrap contract.
	w = meetingSummaryAbortWriter{ResponseWriter: w}
	budget := meetingSummaryRequestIO{controller: http.NewResponseController(io), ctx: ctx, body: r.Body}
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
	body, err := h.execute(w, r)
	if meetingSummaryHTTPExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if budget.closeBody() != nil || meetingSummaryHTTPExpired(ctx) {
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
	if meetingSummaryHTTPExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if budget.controller.Flush() != nil || meetingSummaryHTTPExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *projectHTTP) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if meetingSummaryHTTPExpired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	kind, rawProject, rawTarget := projectHTTPResource(r.URL.Path)
	if kind == projectHTTPNone {
		return nil, fault(f.NotFound)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, fault(f.MethodNotAllowed)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if meetingSummaryHTTPExpired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, fault(f.Unauthenticated)
	}
	project, err := f.ParseID[id.Project](rawProject)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	list := kind == projectHTTPProviders || kind == projectHTTPModels || kind == projectHTTPAvailable
	q, err := projectHTTPQuery(r, list)
	if err != nil {
		return nil, err
	}
	if err = meetingSummaryHTTPEmptyBody(r); err != nil {
		return nil, err
	}
	if meetingSummaryHTTPExpired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	switch kind {
	case projectHTTPProviders:
		value, err := h.core.ListProjectProviders(r.Context(), actor, project, q)
		if err != nil {
			return nil, err
		}
		return projectHTTPEncodeProviders(r.Context(), project, q, value)
	case projectHTTPProvider:
		target, err := f.ParseID[mc.Provider](rawTarget)
		if err != nil {
			return nil, fault(f.InvalidArgument)
		}
		value, err := h.core.GetProjectProvider(r.Context(), actor, project, target)
		if err != nil {
			return nil, err
		}
		return projectHTTPEncodeProvider(r.Context(), project, target, value)
	case projectHTTPModels:
		value, err := h.core.ListProjectModels(r.Context(), actor, project, q)
		if err != nil {
			return nil, err
		}
		return projectHTTPEncodeModels(r.Context(), project, q, value)
	case projectHTTPModel:
		target, err := f.ParseID[mc.Model](rawTarget)
		if err != nil {
			return nil, fault(f.InvalidArgument)
		}
		value, err := h.core.GetProjectModel(r.Context(), actor, project, target)
		if err != nil {
			return nil, err
		}
		return projectHTTPEncodeModel(r.Context(), project, target, value)
	case projectHTTPAvailable:
		value, err := h.core.ListAvailableChatModels(r.Context(), actor, project, q)
		if err != nil {
			return nil, err
		}
		return projectHTTPEncodeAvailable(r.Context(), project, q, value)
	}
	return nil, fault(f.NotFound)
}
