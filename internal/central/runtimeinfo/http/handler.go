// Package runtimeinfohttp exposes the current-administrator runtime snapshot.
package runtimeinfohttp

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runtimeinfo"
)

const runtimePath = "/api/v1/system/runtime-information"
const readBudget = 3 * time.Second

type SystemHTTPOptions struct{ PublicOrigin string }

type runtimeReader interface {
	GetSystem(context.Context, identity.Actor) (runtimeinfo.Snapshot, error)
}

type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireSystem(*http.Request, identity.AccessIntent) (identity.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}

type systemHTTP struct {
	service  runtimeReader
	boundary accountBoundary
}

// NewSystemHTTPHandler is pure and retains the supplied services from one root.
func NewSystemHTTPHandler(service *runtimeinfo.Service, accounts *account.Service, options SystemHTTPOptions) (http.Handler, error) {
	if service == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	boundary, err := account.NewHTTPBoundary(accounts, options.PublicOrigin)
	if err != nil {
		return nil, err
	}
	return &systemHTTP{service: service, boundary: boundary}, nil
}

func (h *systemHTTP) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	// Include boundary checks and preauthentication, not merely the service call.
	ctx, cancel := context.WithTimeout(request.Context(), readBudget)
	defer cancel()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	controller, finish := connectionBudget(w, ctx)
	normal := false
	defer func() { finish(r.Body, normal) }()
	h.serve(w, r)
	if expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	// Write may only fill a net/http buffer. Own the actual Flush before retiring
	// this request's deadlines and cancellation callback.
	if err := controller.Flush(); err != nil {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *systemHTTP) serve(w http.ResponseWriter, r *http.Request) {
	problem := func(err error) {
		if expired(r.Context()) {
			panic(http.ErrAbortHandler)
		}
		h.boundary.WriteProblem(w, r, err)
	}
	// The original boundary checks Host/Origin/Fetch Metadata before Path/RawPath.
	if err := h.boundary.CheckRequest(w, r); err != nil {
		problem(err)
		return
	}
	if r.URL.Path != runtimePath {
		problem(fault(foundation.NotFound))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		problem(fault(foundation.MethodNotAllowed))
		return
	}
	actor, err := h.boundary.RequireSystem(r, identity.Read)
	if err != nil {
		problem(err)
		return
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		problem(fault(foundation.InvalidArgument))
		return
	}
	if err := emptyBody(r); err != nil {
		problem(err)
		return
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	value, err := h.service.GetSystem(r.Context(), actor)
	if err != nil {
		problem(err)
		return
	}
	body, err := encodeSnapshot(value)
	if err != nil {
		problem(err)
		return
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func emptyBody(r *http.Request) error {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return fault(foundation.InvalidArgument)
	}
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(data) != 0 {
		return fault(foundation.InvalidArgument)
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

func connectionBudget(w http.ResponseWriter, ctx context.Context) (*http.ResponseController, func(io.ReadCloser, bool)) {
	controller := http.NewResponseController(w)
	deadline, ok := ctx.Deadline()
	if !ok || controller.SetReadDeadline(deadline) != nil {
		panic(http.ErrAbortHandler)
	}
	if controller.SetWriteDeadline(deadline) != nil {
		_ = controller.SetReadDeadline(time.Time{})
		panic(http.ErrAbortHandler)
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		_ = controller.SetReadDeadline(time.Now())
		_ = controller.SetWriteDeadline(time.Now())
	})
	return controller, func(body io.ReadCloser, normal bool) {
		if !normal {
			// Abort paths must not drain an unwanted body until the full budget.
			_ = controller.SetReadDeadline(time.Now())
			_ = controller.SetWriteDeadline(time.Now())
		}
		var closeErr error
		if body != nil {
			closeErr = body.Close()
		}
		if !stop() {
			<-done
		}
		readErr := controller.SetReadDeadline(time.Time{})
		writeErr := controller.SetWriteDeadline(time.Time{})
		if closeErr != nil || readErr != nil || writeErr != nil || normal && expired(ctx) {
			panic(http.ErrAbortHandler)
		}
	}
}

func fault(code foundation.Code) *foundation.Fault {
	return foundation.NewFault(code, foundation.NotStarted)
}
