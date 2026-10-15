package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"sync"
	"testing"
)

const updateCaseID = "0123456789abcdef0123456789abcdef"

func updateState(t *testing.T) *serverState {
	t.Helper()
	cfg := config{Mode: "openai_chat_wire", WireSequence: []wireScenario{{Status: 200, Chunks: [][]byte{[]byte("first")}}, {Status: 200, Chunks: [][]byte{[]byte("second")}}}}
	if !validWire(&cfg, updateCaseID) {
		t.Fatal("invalid fixture")
	}
	released := make(chan struct{})
	close(released)
	return &serverState{nonce: "owned", cases: map[string]*scenario{updateCaseID: {Config: cfg, release: released}}}
}
func updateFuture(s *serverState, nonce string, ordinal int, wire wireScenario) int {
	body, _ := json.Marshal(struct {
		Ordinal int
		Wire    wireScenario
	}{ordinal, wire})
	r := httptest.NewRequest("POST", "/update-future-wire/"+updateCaseID, bytes.NewReader(body))
	r.Header.Set("X-Fixture-Nonce", nonce)
	w := httptest.NewRecorder()
	s.control(w, r)
	return w.Code
}

type updateBlockedBody struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *updateBlockedBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return 0, io.EOF
}
func (*updateBlockedBody) Close() error { return nil }
func serveUpdate(s *serverState, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/case/"+updateCaseID+"/chat/completions", body)
	r = r.WithContext(context.WithValue(r.Context(), connectionKey{}, int64(1)))
	w := httptest.NewRecorder()
	s.serve(w, r)
	return w
}
func TestFutureWireCannotRewriteCapturedRequest(t *testing.T) {
	s := updateState(t)
	body := &updateBlockedBody{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serveUpdate(s, body) }()
	<-body.started
	replacement := wireScenario{Status: 200, Chunks: [][]byte{[]byte("replacement")}}
	if got := updateFuture(s, "wrong", 2, replacement); got != 403 {
		t.Fatalf("nonce status %d", got)
	}
	if got := updateFuture(s, "owned", 1, replacement); got != 409 {
		t.Fatalf("captured request status %d", got)
	}
	if got := updateFuture(s, "owned", 2, replacement); got != 204 {
		t.Fatalf("future status %d", got)
	}
	close(body.release)
	if got := (<-done).Body.String(); got != "first" {
		t.Fatalf("captured reply changed: %q", got)
	}
	if got := serveUpdate(s, bytes.NewReader(nil)).Body.String(); got != "replacement" {
		t.Fatalf("future reply missing: %q", got)
	}
	if got := updateFuture(s, "owned", 2, replacement); got != 409 {
		t.Fatalf("already served status %d", got)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.cases[updateCaseID]
	if len(v.Requests) != 2 || v.WireCaptured != 2 || v.ActiveHandlers != 0 || v.CompletedHandlers != 2 {
		t.Fatal("request/owner counts changed")
	}
}
func TestFutureWireUpdateAndCaptureAreSerialized(t *testing.T) {
	for n := 0; n < 32; n++ {
		s := updateState(t)
		serveUpdate(s, bytes.NewReader(nil))
		start := make(chan struct{})
		status := make(chan int, 1)
		response := make(chan string, 1)
		go func() {
			<-start
			status <- updateFuture(s, "owned", 2, wireScenario{Status: 200, Chunks: [][]byte{[]byte("updated")}})
		}()
		go func() { <-start; response <- serveUpdate(s, bytes.NewReader(nil)).Body.String() }()
		close(start)
		code, body := <-status, <-response
		if !(code == 204 && body == "updated" || code == 409 && body == "second") {
			t.Fatalf("non-atomic update/capture: %d %q", code, body)
		}
	}
}
