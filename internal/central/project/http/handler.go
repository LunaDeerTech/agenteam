package projecthttp

import (
	"context"
	"errors"
	"io"
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

const projectsPath = "/api/v1/projects"
const projectPrefix = projectsPath + "/"
const resolvePath = projectPrefix + "resolve"
const readBudget = 2 * time.Second

type resource uint8

const (
	noResource resource = iota
	listResource
	detailResource
)

type projectReader interface {
	GetProject(context.Context, id.Actor, pc.ProjectID) (pc.ProjectRef, error)
	ListOwnedProjects(context.Context, id.Actor, pc.ListOwnedProjectsRequest, f.PageRequest) (f.Page[pc.ProjectListItem], error)
}
type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type projectReadHTTP struct {
	reader   projectReader
	boundary accountBoundary
}

// NewHTTPHandler is pure and preserves the root's exact Reader and Account
// boundary. Controlled reader/boundary interfaces remain private to this package.
func NewHTTPHandler(reader *project.Reader, boundary *account.HTTPBoundary) (http.Handler, error) {
	if reader == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &projectReadHTTP{reader: reader, boundary: boundary}, nil
}
func HandlesPath(path string) bool { kind, _ := resourcePath(path); return kind != noResource }
func resourcePath(path string) (resource, string) {
	if path == projectsPath {
		return listResource, ""
	}
	if !strings.HasPrefix(path, projectPrefix) || path == resolvePath {
		return noResource, ""
	}
	raw := strings.TrimPrefix(path, projectPrefix)
	if raw == "" || strings.Contains(raw, "/") {
		return noResource, ""
	}
	return detailResource, raw
}
func resourcePattern(kind resource) string {
	switch kind {
	case listResource:
		return projectsPath
	case detailResource:
		return projectPrefix + "{id}"
	}
	return ""
}

func (h *projectReadHTTP) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	// Start before browser boundary/authentication, inheriting any earlier parent.
	ctx, cancel := context.WithTimeout(request.Context(), readBudget)
	defer cancel()
	kind := noResource
	if request.URL != nil {
		kind, _ = resourcePath(request.URL.Path)
	}
	// Mutate the request seen by the outer logger, never its path/query values.
	request.Pattern = resourcePattern(kind)
	r := request.WithContext(ctx)
	w = abortWriter{w}
	budget := requestIO{controller: http.NewResponseController(w), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		// The one outer Recover must not publish an unbounded replacement after
		// this request has retired its connection budget, even on an unexpected
		// adapter panic. Partial writes always end with the native abort sentinel.
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if err := budget.start(); err != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.read(w, r)
	if expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	// Complete input ownership before publishing either success or Problem.
	if closeErr := budget.closeBody(); closeErr != nil {
		panic(http.ErrAbortHandler)
	}
	if expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		// Preserve the original Fault/CommitState, including a read's Unknown.
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
	if expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	// A complete Write can still be buffered by net/http. Flush belongs to this
	// request, before joining the cancellation callback and clearing deadlines.
	if err := budget.controller.Flush(); err != nil {
		panic(http.ErrAbortHandler)
	}
	if expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *projectReadHTTP) read(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	kind, rawID := resourcePath(r.URL.Path)
	if kind == noResource {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
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
	var projectID id.ProjectID
	if kind == detailResource {
		projectID, err = f.ParseID[id.Project](rawID)
		if err != nil {
			return nil, invalidQuery()
		}
	}
	request, page, err := projectQuery(r, kind == listResource)
	if err != nil {
		return nil, err
	}
	if err = emptyBody(r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if kind == listResource {
		value, err := h.reader.ListOwnedProjects(r.Context(), actor, request, page)
		if err != nil {
			return nil, err
		}
		return encodeList(r.Context(), request, page, value)
	}
	value, err := h.reader.GetProject(r.Context(), actor, projectID)
	if err != nil {
		return nil, err
	}
	return encodeProject(r.Context(), actor, projectID, value)
}

func emptyBody(r *http.Request) error {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return invalidQuery()
	}
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	// Do not trust Content-Length. At most one actual byte is read, under the
	// same native deadline and cancellation callback as authentication/services.
	var data [1]byte
	n, err := r.Body.Read(data[:])
	// A zero-progress adapter has not proved EOF. Do not spin through unbounded
	// (0, nil) reads, even if the adapter ignores its native read deadline.
	if n != 0 || err != io.EOF {
		return invalidQuery()
	}
	return nil
}
func expired(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return ctx.Err() != nil || ok && !time.Now().Before(deadline)
}

type abortWriter struct{ http.ResponseWriter }

func (w abortWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w abortWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if err != nil || n != len(data) {
		panic(http.ErrAbortHandler)
	}
	return n, nil
}

type requestIO struct {
	controller   *http.ResponseController
	ctx          context.Context
	body         io.ReadCloser
	bodyClosed   bool
	stop         func() bool
	callbackDone chan struct{}
	callbackErr  error // read only after the callbackDone join
}

func (b *requestIO) start() error {
	deadline, ok := b.ctx.Deadline()
	if !ok {
		return http.ErrNotSupported
	}
	if err := b.controller.SetReadDeadline(deadline); err != nil {
		return err
	}
	if err := b.controller.SetWriteDeadline(deadline); err != nil {
		return err
	}
	b.callbackDone = make(chan struct{})
	b.stop = context.AfterFunc(b.ctx, func() {
		defer close(b.callbackDone)
		readErr := b.controller.SetReadDeadline(time.Now())
		writeErr := b.controller.SetWriteDeadline(time.Now())
		b.callbackErr = errors.Join(readErr, writeErr)
	})
	return nil
}
func (b *requestIO) closeBody() error {
	if b.bodyClosed {
		return nil
	}
	b.bodyClosed = true
	if b.body != nil {
		return b.body.Close()
	}
	return nil
}
func (b *requestIO) finish(normal bool) error {
	var abortReadErr, abortWriteErr error
	if !normal {
		// Bound native request draining on every abort, including initialization
		// failure, while retaining ownership of the synchronous Close call.
		abortReadErr = b.controller.SetReadDeadline(time.Now())
		abortWriteErr = b.controller.SetWriteDeadline(time.Now())
	}
	closeErr := b.closeBody()
	if b.stop != nil && !b.stop() {
		<-b.callbackDone
	}
	// Reset only after Close and the started callback have actually returned.
	readErr := b.controller.SetReadDeadline(time.Time{})
	writeErr := b.controller.SetWriteDeadline(time.Time{})
	var ctxErr error
	if normal && expired(b.ctx) {
		ctxErr = context.DeadlineExceeded
	}
	return errors.Join(abortReadErr, abortWriteErr, closeErr, b.callbackErr, readErr, writeErr, ctxErr)
}
