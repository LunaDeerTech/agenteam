package model

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const meetingSummaryHTTPPath = "/system/model-selection/meeting-summary"
const meetingSummaryHTTPReadBudget = 3 * time.Second
const meetingSummaryHTTPWriteBudget = 30 * time.Second
const meetingSummaryHTTPBodyLimit = 16 << 10

// These private ports are supplied only by the existing concrete System handler.
// They neither replace Account authorization nor expose another production root.
type meetingSummaryHTTPBoundary interface {
	RequireSystem(*http.Request, id.AccessIntent) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type meetingSummaryHTTPService interface {
	GetMeetingSummarySelection(context.Context, id.Actor) (mc.MeetingSummarySelection, error)
	UpdateMeetingSummarySelection(context.Context, mc.UpdateMeetingSummarySelectionRequest) (mc.CommandReceipt, error)
}
type meetingSummaryHTTP struct {
	core     meetingSummaryHTTPService
	boundary meetingSummaryHTTPBoundary
}

// CheckRequest already ran at the outer System boundary. Everything from the
// first RequireSystem through the actual output Flush belongs to this budget.
func (h meetingSummaryHTTP) serveHTTP(w http.ResponseWriter, request *http.Request) {
	duration := meetingSummaryHTTPReadBudget
	if request.Method == http.MethodPut {
		duration = meetingSummaryHTTPWriteBudget
	}
	ctx, cancel := context.WithTimeout(request.Context(), duration)
	defer cancel() // Last: do not truncate a successful keepalive response.
	r := request.WithContext(ctx)
	w = meetingSummaryAbortWriter{w}
	budget := meetingSummaryRequestIO{controller: http.NewResponseController(w), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			// The outer Recover must never append an unbounded replacement Problem.
			panic(http.ErrAbortHandler)
		}
	}()
	if err := budget.start(); err != nil {
		panic(http.ErrAbortHandler)
	}
	output, err := h.execute(w, r)
	if meetingSummaryHTTPExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if closeErr := budget.closeBody(); closeErr != nil || meetingSummaryHTTPExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else if err := httpapi.WriteJSON(w, r, http.StatusOK, output); err != nil {
		panic(http.ErrAbortHandler)
	}
	if meetingSummaryHTTPExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
	// HEAD also owns a real headers Flush; WriteJSON alone can leave them buffered.
	if err := budget.controller.Flush(); err != nil || meetingSummaryHTTPExpired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h meetingSummaryHTTP) execute(w http.ResponseWriter, r *http.Request) (any, error) {
	intent := id.Read
	if r.Method == http.MethodPut {
		intent = id.Mutate
	}
	actor, err := h.boundary.RequireSystem(r, intent)
	if err != nil {
		return nil, err
	}
	if meetingSummaryHTTPExpired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, fault(f.InvalidArgument)
	}
	if r.Method != http.MethodPut {
		if err = meetingSummaryHTTPEmptyBody(r); err != nil {
			return nil, err
		}
		if meetingSummaryHTTPExpired(r.Context()) {
			panic(http.ErrAbortHandler)
		}
		value, err := h.core.GetMeetingSummarySelection(r.Context(), actor)
		if err != nil {
			return nil, err
		}
		if value.Validate() != nil {
			return nil, fault(f.DependencyUnavailable)
		}
		return value.Clone(), nil
	}
	key, err := httpCommandKey(r)
	if err != nil {
		return nil, err
	}
	var input struct {
		ID       f.ID[struct{}] `json:"id"`
		Expected f.Version      `json:"expected_version"`
		Model    mc.ModelID     `json:"model"`
	}
	if err = httpapi.DecodeJSON(w, r, &input, meetingSummaryHTTPBodyLimit); err != nil {
		return nil, err
	}
	if input.ID.Validate() != nil || input.Expected.Validate() != nil || input.Expected == f.Version(math.MaxInt64) || input.Model.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if meetingSummaryHTTPExpired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	// One original command; no retry or follow-up GET can replace its receipt.
	return h.core.UpdateMeetingSummarySelection(r.Context(), mc.UpdateMeetingSummarySelectionRequest{
		CommandMeta: mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: key},
		SelectionID: input.ID.String(), ExpectedVersion: input.Expected, Model: input.Model,
	})
}

func meetingSummaryHTTPEmptyBody(r *http.Request) error {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return fault(f.InvalidArgument)
	}
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	var data [1]byte
	n, err := r.Body.Read(data[:])
	if n != 0 || err != io.EOF {
		return fault(f.InvalidArgument)
	}
	return nil
}

func meetingSummaryHTTPExpired(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return ctx.Err() != nil || ok && !time.Now().Before(deadline)
}

type meetingSummaryAbortWriter struct{ http.ResponseWriter }

func (w meetingSummaryAbortWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w meetingSummaryAbortWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if err != nil || n != len(data) {
		panic(http.ErrAbortHandler)
	}
	return n, nil
}

type meetingSummaryRequestIO struct {
	controller   *http.ResponseController
	ctx          context.Context
	body         io.ReadCloser
	bodyClosed   bool
	stop         func() bool
	callbackDone chan struct{}
	callbackErr  error // Only read after callbackDone has actually joined.
}

func (b *meetingSummaryRequestIO) start() error {
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
func (b *meetingSummaryRequestIO) closeBody() (err error) {
	if b.bodyClosed {
		return nil
	}
	b.bodyClosed = true
	defer func() {
		if recover() != nil {
			err = http.ErrAbortHandler
		}
	}()
	if b.body != nil {
		return b.body.Close()
	}
	return nil
}
func (b *meetingSummaryRequestIO) finish(normal bool) error {
	var abortRead, abortWrite error
	if !normal {
		// Bound native drain before synchronously closing a failed input.
		abortRead = b.controller.SetReadDeadline(time.Now())
		abortWrite = b.controller.SetWriteDeadline(time.Now())
	}
	closeErr := b.closeBody()
	if b.stop != nil && !b.stop() {
		<-b.callbackDone
	}
	readErr := b.controller.SetReadDeadline(time.Time{})
	writeErr := b.controller.SetWriteDeadline(time.Time{})
	var ctxErr error
	if normal && meetingSummaryHTTPExpired(b.ctx) {
		ctxErr = context.DeadlineExceeded
	}
	return errors.Join(abortRead, abortWrite, closeErr, b.callbackErr, readErr, writeErr, ctxErr)
}
