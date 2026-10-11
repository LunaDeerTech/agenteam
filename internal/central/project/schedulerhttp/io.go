package schedulerhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// Preserve the existing Project transport ownership and bounded wrapper walk.
// These private helpers do not change or broaden the original metadata adapter.
type deadlineWriter struct {
	http.ResponseWriter
	read  func(time.Time) error
	write func(time.Time) error
	flush func() error
}

func (io *deadlineWriter) prepare() bool {
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
func (w *deadlineWriter) SetReadDeadline(at time.Time) error {
	if w.read == nil {
		return http.ErrNotSupported
	}
	return w.read(at)
}
func (w *deadlineWriter) SetWriteDeadline(at time.Time) error {
	if w.write == nil {
		return http.ErrNotSupported
	}
	return w.write(at)
}
func (w *deadlineWriter) FlushError() error {
	if w.flush == nil {
		return http.ErrNotSupported
	}
	return w.flush()
}

func emptyBody(r *http.Request) error {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return invalid()
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
		return invalid()
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
