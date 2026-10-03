package httpapi

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"testing"
)

type basicWriter struct {
	header   http.Header
	statuses []int
	body     bytes.Buffer
}

func (w *basicWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *basicWriter) WriteHeader(status int)      { w.statuses = append(w.statuses, status) }
func (w *basicWriter) Write(b []byte) (int, error) { return w.body.Write(b) }

type flushOnlyWriter struct {
	*basicWriter
	flushed bool
}

func (w *flushOnlyWriter) Flush() { w.flushed = true }

type hijackOnlyWriter struct {
	*basicWriter
	conn net.Conn
}

func (w *hijackOnlyWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

type unwrapWriter struct{ http.ResponseWriter }

func (w unwrapWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestResponseWriterCapabilitiesAndInformationalStatus(t *testing.T) {
	base := &basicWriter{}
	w, state := trackResponse(base)
	if _, ok := w.(http.Flusher); ok {
		t.Fatal("unsupported Flush advertised")
	}
	if _, ok := w.(http.Hijacker); ok {
		t.Fatal("unsupported Hijack advertised")
	}
	if !errors.Is(http.NewResponseController(w).Flush(), http.ErrNotSupported) {
		t.Fatal("unsupported Flush succeeded")
	}
	if _, _, err := http.NewResponseController(w).Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatal("unsupported Hijack succeeded")
	}
	if w.(interface{ Unwrap() http.ResponseWriter }).Unwrap() != base {
		t.Fatal("Unwrap lost writer")
	}
	w.WriteHeader(http.StatusEarlyHints)
	w.WriteHeader(http.StatusContinue)
	if state.committed || state.status != 0 {
		t.Fatal("informational response marked final")
	}
	w.WriteHeader(http.StatusCreated)
	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte("body")); err != nil {
		t.Fatal(err)
	}
	if state.status != 201 || state.bytes != 4 || !state.committed || len(base.statuses) != 3 {
		t.Fatalf("wrong final state: %+v", state)
	}

	flushBase := &flushOnlyWriter{basicWriter: &basicWriter{}}
	flusher, flushState := trackResponse(unwrapWriter{flushBase})
	if _, ok := flusher.(http.Flusher); !ok {
		t.Fatal("supported Flush lost")
	}
	if _, ok := flusher.(http.Hijacker); ok {
		t.Fatal("unsupported Hijack added")
	}
	if err := http.NewResponseController(flusher).Flush(); err != nil || !flushBase.flushed || flushState.status != 200 || !flushState.committed {
		t.Fatal("Flush was not tracked")
	}

	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	hijacker, hijackState := trackResponse(&hijackOnlyWriter{basicWriter: &basicWriter{}, conn: left})
	if _, ok := hijacker.(http.Hijacker); !ok {
		t.Fatal("supported Hijack lost")
	}
	if _, ok := hijacker.(http.Flusher); ok {
		t.Fatal("unsupported Flush added")
	}
	conn, _, err := http.NewResponseController(hijacker).Hijack()
	if err != nil || conn != left || !hijackState.committed || hijackState.finalStatus() != 0 {
		t.Fatal("Hijack not tracked")
	}

	switchWriter, switchState := trackResponse(&basicWriter{})
	switchWriter.WriteHeader(http.StatusSwitchingProtocols)
	if !switchState.committed || switchState.status != 101 {
		t.Fatal("101 must finalize HTTP headers")
	}
}

type partialWriter struct{ basicWriter }

func (w *partialWriter) Write(b []byte) (int, error) { return 2, errors.New("private writer error") }
func TestResponseWriterCountsAcceptedBytes(t *testing.T) {
	w, state := trackResponse(&partialWriter{})
	n, err := w.Write([]byte("abcdef"))
	if n != 2 || err == nil || state.bytes != 2 || state.status != 200 {
		t.Fatal("write count assumed requested length")
	}
}

func TestRecoverPreservesAbortHandler(t *testing.T) {
	w := &basicWriter{}
	defer func() {
		if got := recover(); got != http.ErrAbortHandler {
			t.Fatalf("abort not propagated: %v", got)
		}
		if w.body.Len() != 0 || len(w.statuses) != 0 {
			t.Fatal("Problem appended to existing abort")
		}
	}()
	Recover(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })).ServeHTTP(w, &http.Request{})
}
