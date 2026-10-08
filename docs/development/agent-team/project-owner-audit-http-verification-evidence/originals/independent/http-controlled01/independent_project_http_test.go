package audithttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type independentAuditWriter struct {
	*projectAuditIOTest
	headers atomic.Int32
	writes  atomic.Int32
}

func (w *independentAuditWriter) WriteHeader(code int) {
	w.headers.Add(1)
	w.budgetWriter.WriteHeader(code)
}
func (w *independentAuditWriter) Write(p []byte) (int, error) {
	w.writes.Add(1)
	return w.budgetWriter.Write(p)
}

// This is only an in-process controlled supplement, never a PG/socket claim.
func TestIndependentProjectAuditHTTPControls(t *testing.T) {
	t.Run("Close-panic-still-joins-started-callback", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		callbackEntered, finishReached, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		var expiredCalls atomic.Int32
		under := newBudgetWriter()
		writer := &independentAuditWriter{projectAuditIOTest: &projectAuditIOTest{budgetWriter: under}}
		writer.write = func(at time.Time) error {
			if !at.IsZero() && time.Until(at) < time.Second {
				switch expiredCalls.Add(1) {
				case 1:
					close(callbackEntered)
					<-release
				case 2:
					close(finishReached)
				}
			}
			return nil
		}
		body := &closeBody{Reader: strings.NewReader(""), close: func() error {
			cancel()
			<-callbackEntered
			panic("private-close-panic-never-format")
		}}
		request := httptest.NewRequest("GET", projectAuditTestPath, nil).WithContext(ctx)
		request.Body = body
		reader, boundary := &projectAuditReaderTest{}, &projectAuditBoundaryTest{}
		var aborted bool
		t.Cleanup(func() { cancel(); unblock(); <-done })
		go func() { defer close(done); aborted = projectAuditServe(writer, request, reader, boundary) }()
		select {
		case <-finishReached:
		case <-time.After(time.Second):
			t.Fatal("Close panic did not reach synchronous finish after callback began")
		}
		select {
		case <-done:
			t.Fatal("handler escaped the still-owned cancellation callback")
		default:
		}
		unblock()
		<-done
		if !aborted || expiredCalls.Load() != 2 || body.closed.Load() != 1 || reader.calls.Load() != 1 || boundary.calls.Load() != 1 || writer.headers.Load() != 0 || writer.writes.Load() != 0 || under.Body.Len() != 0 {
			t.Fatal("Close panic/callback join/zero publication invariants")
		}
	})
	t.Run("reset-read-panic-still-attempts-write-reset", func(t *testing.T) {
		var readResets, writeResets atomic.Int32
		under := newBudgetWriter()
		writer := &independentAuditWriter{projectAuditIOTest: &projectAuditIOTest{budgetWriter: under}}
		writer.read = func(at time.Time) error {
			if at.IsZero() {
				readResets.Add(1)
				panic("private-reset-panic-never-format")
			}
			return nil
		}
		writer.write = func(at time.Time) error {
			if at.IsZero() {
				writeResets.Add(1)
			}
			return nil
		}
		body := &closeBody{Reader: strings.NewReader("")}
		request := httptest.NewRequest("GET", projectAuditTestPath, nil)
		request.Body = body
		reader := &projectAuditReaderTest{}
		aborted := projectAuditServe(writer, request, reader, &projectAuditBoundaryTest{})
		if !aborted || body.closed.Load() != 1 || readResets.Load() != 1 || writeResets.Load() != 1 || reader.calls.Load() != 1 || writer.headers.Load() != 1 || writer.writes.Load() != 1 || under.flushes != 1 || under.Code != http.StatusOK || under.Body.String() != `{"items":[],"next_cursor":null}` {
			t.Fatal("reset panic stopped the other setter or wrote another response")
		}
	})
}
