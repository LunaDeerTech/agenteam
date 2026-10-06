// Package audithttp exposes only the current-administrator System Audit reads.
package audithttp

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const auditPath = "/api/v1/system/audit"
const readBudget = 3 * time.Second

type SystemHTTPOptions struct{ PublicOrigin string }

type auditReader interface {
	ListSystem(context.Context, identity.Actor, c.Filter, foundation.PageRequest) (foundation.Page[c.SafeRecord], error)
	GetSystem(context.Context, identity.Actor, c.ID) (c.SafeRecord, error)
}

type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireSystem(*http.Request, identity.AccessIntent) (identity.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}

type systemHTTP struct {
	auditor  auditReader
	boundary accountBoundary
}

// NewSystemHTTPHandler performs no I/O and retains the exact root services.
func NewSystemHTTPHandler(auditor *audit.Service, accounts *account.Service, options SystemHTTPOptions) (http.Handler, error) {
	if auditor == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	boundary, err := account.NewHTTPBoundary(accounts, options.PublicOrigin)
	if err != nil {
		return nil, err
	}
	return &systemHTTP{auditor: auditor, boundary: boundary}, nil
}

func (h *systemHTTP) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	// This deadline includes even the first Account boundary/authentication call.
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
	// net/http can buffer a complete Write. Flush is part of this request's
	// owned I/O, before its deadline and cancellation callback are retired.
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
	if err := h.boundary.CheckRequest(w, r); err != nil {
		problem(err)
		return
	}
	list, id, exists := resourcePath(r.URL.Path)
	if !exists {
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
	var body []byte
	if list {
		filter, page, parseErr := query(r.URL.RawQuery, r.URL.ForceQuery)
		if parseErr != nil {
			problem(parseErr)
			return
		}
		if err = emptyBody(r); err == nil {
			var result foundation.Page[c.SafeRecord]
			if expired(r.Context()) {
				panic(http.ErrAbortHandler)
			}
			result, err = h.auditor.ListSystem(r.Context(), actor, filter, page)
			if err == nil {
				body, err = encodePage(result)
			}
		}
	} else {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			problem(fault(foundation.InvalidArgument))
			return
		}
		parsed, parseErr := foundation.ParseID[c.Record](id)
		if parseErr != nil {
			problem(fault(foundation.InvalidArgument))
			return
		}
		if err = emptyBody(r); err == nil {
			var result c.SafeRecord
			if expired(r.Context()) {
				panic(http.ErrAbortHandler)
			}
			result, err = h.auditor.GetSystem(r.Context(), actor, parsed)
			if err == nil {
				body, err = encodeRecord(result)
			}
		}
	}
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

func resourcePath(path string) (list bool, id string, exists bool) {
	if path == auditPath {
		return true, "", true
	}
	if !strings.HasPrefix(path, auditPath+"/") {
		return false, "", false
	}
	id = strings.TrimPrefix(path, auditPath+"/")
	return false, id, id != "" && !strings.Contains(id, "/")
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
