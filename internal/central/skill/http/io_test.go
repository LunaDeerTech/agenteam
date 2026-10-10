package skillhttp

import (
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testWriter struct {
	*httptest.ResponseRecorder
	mu                sync.Mutex
	reads, writes     []time.Time
	setRead, setWrite func(time.Time) error
	write             func([]byte) (int, error)
	flush             func() error
}

func newTestWriter() *testWriter { return &testWriter{ResponseRecorder: httptest.NewRecorder()} }
func (w *testWriter) SetReadDeadline(at time.Time) error {
	w.mu.Lock()
	w.reads = append(w.reads, at)
	w.mu.Unlock()
	if w.setRead != nil {
		return w.setRead(at)
	}
	return nil
}
func (w *testWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.writes = append(w.writes, at)
	w.mu.Unlock()
	if w.setWrite != nil {
		return w.setWrite(at)
	}
	return nil
}
func (w *testWriter) Write(p []byte) (int, error) {
	if w.write != nil {
		return w.write(p)
	}
	return w.ResponseRecorder.Write(p)
}
func (w *testWriter) FlushError() error {
	if w.flush != nil {
		return w.flush()
	}
	return nil
}
func (w *testWriter) cleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.reads) == 0 || len(w.writes) == 0 || !w.reads[len(w.reads)-1].IsZero() || !w.writes[len(w.writes)-1].IsZero() {
		t.Fatal("native deadlines retained")
	}
}

type testBody struct {
	io.Reader
	closes atomic.Int32
	close  func() error
}

func (b *testBody) Close() error {
	b.closes.Add(1)
	if b.close != nil {
		return b.close()
	}
	return nil
}
func testAbort(run func()) (aborted bool) {
	defer func() {
		if v := recover(); v != nil {
			if v != http.ErrAbortHandler {
				panic(v)
			}
			aborted = true
		}
	}()
	run()
	return false
}
func serveTest(h http.Handler, r *http.Request, w http.ResponseWriter) bool {
	return testAbort(func() { httpapi.Handler(nil, h).ServeHTTP(w, r) })
}
