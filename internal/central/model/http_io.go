package model

import (
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"io"
	"net/http"
	"time"
)

func modelHTTPEmptyBody(r *http.Request) error {
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

func modelHTTPExpired(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return ctx.Err() != nil || ok && !time.Now().Before(deadline)
}

type modelAbortWriter struct{ http.ResponseWriter }

func (w modelAbortWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w modelAbortWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if err != nil || n != len(data) {
		panic(http.ErrAbortHandler)
	}
	return n, nil
}

type modelRequestIO struct {
	controller   *http.ResponseController
	ctx          context.Context
	body         io.ReadCloser
	bodyClosed   bool
	stop         func() bool
	callbackDone chan struct{}
	callbackErr  error // Only read after callbackDone has actually joined.
}

func (b *modelRequestIO) start() error {
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
func (b *modelRequestIO) closeBody() (err error) {
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
func (b *modelRequestIO) finish(normal bool) error {
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
	if normal && modelHTTPExpired(b.ctx) {
		ctxErr = context.DeadlineExceeded
	}
	return errors.Join(abortRead, abortWrite, closeErr, b.callbackErr, readErr, writeErr, ctxErr)
}
