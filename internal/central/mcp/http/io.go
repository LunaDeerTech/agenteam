package mcphttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

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
