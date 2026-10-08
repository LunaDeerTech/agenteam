package audithttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const projectAuditHTTPBudget = 3 * time.Second

type projectAuditReader interface {
	ListProject(context.Context, identity.Actor, identity.ProjectID, c.Filter, foundation.PageRequest) (foundation.Page[c.SafeRecord], error)
	GetProject(context.Context, identity.Actor, identity.ProjectID, c.ID) (c.SafeRecord, error)
}
type projectAuditBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (identity.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type projectAuditHTTP struct {
	auditor  projectAuditReader
	boundary projectAuditBoundary
}

// NewProjectHTTPHandler retains the root's one real Audit and browser boundary.
// The public constructor performs no I/O and installs no background owner.
func NewProjectHTTPHandler(auditor *audit.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if auditor == nil || boundary == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	return &projectAuditHTTP{auditor, boundary}, nil
}

// The complete Audit subtree is owned, including malformed suffixes; nearby
// Project resources continue through the root's original dispatcher.
func HandlesProjectHTTPPath(path string) bool {
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	_, rest, ok := strings.Cut(strings.TrimPrefix(path, prefix), "/")
	return ok && (rest == "audit" || strings.HasPrefix(rest, "audit/"))
}
func projectAuditPath(path string) (project, auditID string, list, exists bool) {
	const prefix = "/api/v1/projects/"
	if !strings.HasPrefix(path, prefix) {
		return
	}
	project, rest, ok := strings.Cut(strings.TrimPrefix(path, prefix), "/")
	if !ok || project == "" {
		return "", "", false, false
	}
	if rest == "audit" {
		return project, "", true, true
	}
	if !strings.HasPrefix(rest, "audit/") {
		return "", "", false, false
	}
	auditID = strings.TrimPrefix(rest, "audit/")
	return project, auditID, false, auditID != "" && !strings.Contains(auditID, "/")
}
func projectAuditNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// The capability adapter is controller-only. Business writes retain the
// original tracked writer and its stateOf/committed/code ownership chain.
type projectAuditIOWriter struct {
	http.ResponseWriter
	read, write func(time.Time) error
	flush       func() error
}

func (w *projectAuditIOWriter) prepare() bool {
	current := w.ResponseWriter
	for depth := 0; depth < 64 && !projectAuditNil(current); depth++ {
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
		v, ok := current.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return w.read != nil && w.write != nil && w.flush != nil
		}
		current = v.Unwrap()
	}
	return false
}
func projectAuditSafeDeadline(call func(time.Time) error, at time.Time) (err error) {
	defer func() {
		if recover() != nil {
			err = http.ErrAbortHandler
		}
	}()
	if call == nil {
		return http.ErrNotSupported
	}
	return call(at)
}
func (w *projectAuditIOWriter) SetReadDeadline(at time.Time) error {
	return projectAuditSafeDeadline(w.read, at)
}
func (w *projectAuditIOWriter) SetWriteDeadline(at time.Time) error {
	return projectAuditSafeDeadline(w.write, at)
}
func (w *projectAuditIOWriter) FlushError() error {
	if w.flush == nil {
		return http.ErrNotSupported
	}
	return w.flush()
}

type projectAuditRequestIO struct {
	controller   *http.ResponseController
	ctx          context.Context
	body         io.ReadCloser
	closed       bool
	closeErr     error
	stop         func() bool
	callbackDone chan struct{}
	callbackErr  error // Only consumed after stop succeeds or callbackDone joins.
}

func (b *projectAuditRequestIO) deadlines(at time.Time) error {
	// Both safe receivers run even when the first one fails or panics.
	r := b.controller.SetReadDeadline(at)
	w := b.controller.SetWriteDeadline(at)
	return errors.Join(r, w)
}
func (b *projectAuditRequestIO) start() error {
	deadline, ok := b.ctx.Deadline()
	if !ok {
		return http.ErrNotSupported
	}
	if err := b.deadlines(deadline); err != nil {
		return err
	}
	b.callbackDone = make(chan struct{})
	b.stop = context.AfterFunc(b.ctx, func() { defer close(b.callbackDone); b.callbackErr = b.deadlines(time.Now()) })
	return nil
}
func (b *projectAuditRequestIO) closeBody() (err error) {
	if b.closed {
		return b.closeErr
	}
	b.closed = true
	defer func() {
		if recover() != nil {
			err = http.ErrAbortHandler
		}
		b.closeErr = err
	}()
	if b.body != nil {
		return b.body.Close()
	}
	return nil
}
func (b *projectAuditRequestIO) finish(normal bool) error {
	var abortErr, resetErr error
	if !normal {
		abortErr = b.deadlines(time.Now())
	}
	closeErr := b.closeBody()
	if b.stop != nil && !b.stop() {
		<-b.callbackDone
	}
	var ctxErr error
	if projectAuditExpired(b.ctx) {
		ctxErr = context.DeadlineExceeded
	}
	if normal && closeErr == nil && b.callbackErr == nil && ctxErr == nil {
		resetErr = b.deadlines(time.Time{})
	}
	return errors.Join(abortErr, closeErr, b.callbackErr, ctxErr, resetErr)
}
func projectAuditExpired(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return ctx.Err() != nil || ok && !time.Now().Before(deadline)
}
func projectAuditCheckBudget(ctx context.Context) {
	if projectAuditExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
}

type projectAuditAbortWriter struct {
	http.ResponseWriter
	ctx  context.Context
	head bool
}

func (w projectAuditAbortWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w projectAuditAbortWriter) WriteHeader(status int) {
	projectAuditCheckBudget(w.ctx)
	w.ResponseWriter.WriteHeader(status)
}
func (w projectAuditAbortWriter) Write(data []byte) (int, error) {
	projectAuditCheckBudget(w.ctx)
	if w.head {
		return len(data), nil
	}
	n, err := w.ResponseWriter.Write(data)
	if err != nil || n != len(data) {
		panic(http.ErrAbortHandler)
	}
	projectAuditCheckBudget(w.ctx)
	return n, nil
}

func (h *projectAuditHTTP) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request == nil || request.URL == nil {
		panic(http.ErrAbortHandler)
	}
	_, _, list, exists := projectAuditPath(request.URL.Path)
	request.Pattern = "unknown_route"
	if exists {
		request.Pattern = "/api/v1/projects/{project_id}/audit"
		if !list {
			request.Pattern += "/{audit_id}"
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), projectAuditHTTPBudget)
	defer cancel()
	r := request.WithContext(ctx)
	adapter := &projectAuditIOWriter{ResponseWriter: writer}
	budget := projectAuditRequestIO{controller: http.NewResponseController(adapter), ctx: ctx, body: r.Body}
	normal := false
	// Ownership precedes even capability resolution: an Unwrap panic still
	// closes the original Body, retires any callback and attempts safe deadlines.
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !adapter.prepare() || budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	w := projectAuditAbortWriter{writer, ctx, r.Method == http.MethodHead}
	body, err := h.execute(w, r)
	projectAuditCheckBudget(ctx)
	if budget.closeBody() != nil {
		panic(http.ErrAbortHandler)
	}
	projectAuditCheckBudget(ctx)
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
	projectAuditCheckBudget(ctx)
	if budget.controller.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
	projectAuditCheckBudget(ctx)
	normal = true
}
func (h *projectAuditHTTP) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	projectAuditCheckBudget(r.Context())
	rawProject, rawID, list, exists := projectAuditPath(r.URL.Path)
	if !exists {
		return nil, fault(foundation.NotFound)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, fault(foundation.MethodNotAllowed)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	projectAuditCheckBudget(r.Context())
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return nil, fault(foundation.Unauthenticated)
	}
	project, err := foundation.ParseID[identity.Project](rawProject)
	if err != nil {
		return nil, fault(foundation.InvalidArgument)
	}
	if list {
		filter, page, err := query(r.URL.RawQuery, r.URL.ForceQuery)
		if err != nil {
			return nil, err
		}
		if err = projectAuditEmptyBody(r); err != nil {
			return nil, err
		}
		projectAuditCheckBudget(r.Context())
		result, err := h.auditor.ListProject(r.Context(), actor, project, filter, page)
		if err != nil {
			return nil, err
		}
		return projectAuditEncodePage(r.Context(), project, filter, page, result)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, fault(foundation.InvalidArgument)
	}
	auditID, err := foundation.ParseID[c.Record](rawID)
	if err != nil {
		return nil, fault(foundation.InvalidArgument)
	}
	if err = projectAuditEmptyBody(r); err != nil {
		return nil, err
	}
	projectAuditCheckBudget(r.Context())
	result, err := h.auditor.GetProject(r.Context(), actor, project, auditID)
	if err != nil {
		return nil, err
	}
	return projectAuditEncodeRecord(r.Context(), project, auditID, result)
}
func projectAuditEmptyBody(r *http.Request) error {
	if r.ContentLength > 0 {
		return fault(foundation.InvalidArgument)
	}
	if r.Body == nil {
		return nil
	}
	var byte [1]byte
	n, err := io.ReadFull(r.Body, byte[:])
	projectAuditCheckBudget(r.Context())
	if n != 0 || err != io.EOF {
		return fault(foundation.InvalidArgument)
	}
	return nil
}
