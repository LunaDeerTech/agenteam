package contenthttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

type abortWriter struct{ http.ResponseWriter }

func (w abortWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w abortWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if err != nil || n != len(p) {
		panic(http.ErrAbortHandler)
	}
	return n, nil
}

// Resolve capabilities before any authentication or body I/O. Never let
// ResponseController chase an unsupported wrapper cycle in the abort tail.
type nativeWriter struct {
	http.ResponseWriter
	read, write func(time.Time) error
	flush       func() error
}

func (w *nativeWriter) prepare() bool {
	next := w.ResponseWriter
	for depth := 0; depth < 64 && next != nil; depth++ {
		if w.read == nil {
			if v, ok := next.(interface{ SetReadDeadline(time.Time) error }); ok {
				w.read = v.SetReadDeadline
			}
		}
		if w.write == nil {
			if v, ok := next.(interface{ SetWriteDeadline(time.Time) error }); ok {
				w.write = v.SetWriteDeadline
			}
		}
		if w.flush == nil {
			switch v := next.(type) {
			case interface{ FlushError() error }:
				w.flush = v.FlushError
			case http.Flusher:
				w.flush = func() error { v.Flush(); return nil }
			}
		}
		if w.read != nil && w.write != nil && w.flush != nil {
			return true
		}
		v, ok := next.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		next = v.Unwrap()
	}
	return false
}
func (w *nativeWriter) SetReadDeadline(t time.Time) error {
	if w.read == nil {
		return http.ErrNotSupported
	}
	return w.read(t)
}
func (w *nativeWriter) SetWriteDeadline(t time.Time) error {
	if w.write == nil {
		return http.ErrNotSupported
	}
	return w.write(t)
}
func (w *nativeWriter) FlushError() error {
	if w.flush == nil {
		return http.ErrNotSupported
	}
	return w.flush()
}
func expired(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return ctx.Err() != nil || ok && !time.Now().Before(deadline)
}

type requestIO struct {
	controller   *http.ResponseController
	ctx          context.Context
	body         io.ReadCloser
	bodyClosed   bool
	stop         func() bool
	callbackDone chan struct{}
	callbackErr  error
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
		r := b.controller.SetReadDeadline(time.Now())
		w := b.controller.SetWriteDeadline(time.Now())
		b.callbackErr = errors.Join(r, w)
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
	var ar, aw error
	if !normal {
		ar = b.controller.SetReadDeadline(time.Now())
		aw = b.controller.SetWriteDeadline(time.Now())
	}
	closeErr := b.closeBody()
	if b.stop != nil && !b.stop() {
		<-b.callbackDone
	}
	var ctxErr error
	if normal && expired(b.ctx) {
		ctxErr = context.DeadlineExceeded
	}
	err := errors.Join(ar, aw, closeErr, b.callbackErr, ctxErr)
	if !normal || err != nil {
		return err
	}
	// Only a successful, joined request may make this connection reusable.
	r := b.controller.SetReadDeadline(time.Time{})
	w := b.controller.SetWriteDeadline(time.Time{})
	return errors.Join(r, w)
}
func emptyBody(r *http.Request) error {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return invalidInput()
	}
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	var data [1]byte
	n, err := r.Body.Read(data[:])
	if n != 0 || err != io.EOF {
		return invalidInput()
	}
	return nil
}
