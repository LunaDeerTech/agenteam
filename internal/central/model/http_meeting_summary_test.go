package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const summaryHTTPTestID = "01900000-0000-7000-8000-000000000001"
const summaryHTTPTestModel = "01900000-0000-7000-8000-000000000002"
const summaryHTTPTestBody = `{"id":"` + summaryHTTPTestID + `","expected_version":"9007199254740993","model":"` + summaryHTTPTestModel + `"}`

type summaryHTTPBoundary struct {
	actor id.Actor
	auth  func(*http.Request, id.AccessIntent) (id.Actor, error)
	calls atomic.Int32
}

func (b *summaryHTTPBoundary) RequireSystem(r *http.Request, intent id.AccessIntent) (id.Actor, error) {
	b.calls.Add(1)
	if b.auth != nil {
		return b.auth(r, intent)
	}
	return b.actor, nil
}
func (*summaryHTTPBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, e error) {
	(&account.HTTPBoundary{}).WriteProblem(w, r, e)
}

type summaryHTTPService struct {
	get           func(context.Context, id.Actor) (mc.MeetingSummarySelection, error)
	update        func(context.Context, mc.UpdateMeetingSummarySelectionRequest) (mc.CommandReceipt, error)
	gets, updates atomic.Int32
}

func (s *summaryHTTPService) GetMeetingSummarySelection(ctx context.Context, a id.Actor) (mc.MeetingSummarySelection, error) {
	s.gets.Add(1)
	if s.get != nil {
		return s.get(ctx, a)
	}
	return mc.MeetingSummarySelection{ID: summaryHTTPTestID, Version: 9007199254740993}, nil
}
func (s *summaryHTTPService) UpdateMeetingSummarySelection(ctx context.Context, r mc.UpdateMeetingSummarySelectionRequest) (mc.CommandReceipt, error) {
	s.updates.Add(1)
	if s.update != nil {
		return s.update(ctx, r)
	}
	return mc.CommandReceipt{Kind: "model.selection.update", ResourceID: r.SelectionID, Version: r.ExpectedVersion + 1}, nil
}
func summaryHTTPFixture(t *testing.T) (meetingSummaryHTTP, *summaryHTTPBoundary, *summaryHTTPService) {
	t.Helper()
	b := &summaryHTTPBoundary{actor: testActor(t)}
	s := &summaryHTTPService{}
	return meetingSummaryHTTP{s, b}, b, s
}

// Controlled writer: these checks prove lifecycle ordering, not native socket I/O.
type summaryHTTPWriter struct {
	*httptest.ResponseRecorder
	mu                sync.Mutex
	reads, writes     []time.Time
	setRead, setWrite func(time.Time) error
	write             func([]byte) (int, error)
	flush             func() error
	flushes           int
}

func summaryWriter() *summaryHTTPWriter {
	return &summaryHTTPWriter{ResponseRecorder: httptest.NewRecorder()}
}
func (w *summaryHTTPWriter) SetReadDeadline(at time.Time) error {
	w.mu.Lock()
	w.reads = append(w.reads, at)
	w.mu.Unlock()
	if w.setRead != nil {
		return w.setRead(at)
	}
	return nil
}
func (w *summaryHTTPWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	w.writes = append(w.writes, at)
	w.mu.Unlock()
	if w.setWrite != nil {
		return w.setWrite(at)
	}
	return nil
}
func (w *summaryHTTPWriter) Write(p []byte) (int, error) {
	if w.write != nil {
		return w.write(p)
	}
	return w.ResponseRecorder.Write(p)
}
func (w *summaryHTTPWriter) FlushError() error {
	w.flushes++
	if w.flush != nil {
		return w.flush()
	}
	return nil
}
func (w *summaryHTTPWriter) cleared(t *testing.T) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.reads) < 2 || len(w.writes) < 2 || !w.reads[len(w.reads)-1].IsZero() || !w.writes[len(w.writes)-1].IsZero() {
		t.Fatal("native deadline not retired")
	}
}

type summaryHTTPBody struct {
	io.Reader
	close  func() error
	closes atomic.Int32
}

func (b *summaryHTTPBody) Close() error {
	b.closes.Add(1)
	if b.close != nil {
		return b.close()
	}
	return nil
}

type summaryHTTPRead func([]byte) (int, error)

func (fn summaryHTTPRead) Read(p []byte) (int, error) { return fn(p) }
func summaryAborts(fn func()) (aborted bool) {
	defer func() {
		if p := recover(); p != nil {
			if p != http.ErrAbortHandler {
				panic(p)
			}
			aborted = true
		}
	}()
	fn()
	return false
}
func summaryJoined(t *testing.T, c <-chan bool) bool {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(time.Second):
		t.Fatal("handler actual return missing")
		return false
	}
}
func summaryRequest(method, body string) *http.Request {
	r := httptest.NewRequest(method, "https://summary.example/api/v1"+meetingSummaryHTTPPath, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-summary-key")
	return r
}

func TestMeetingSummaryHTTPStrictInput(t *testing.T) {
	cases := []struct {
		name, body string
		change     func(*http.Request)
		status     int
	}{
		{"exact", summaryHTTPTestBody, nil, 200},
		{"missing_id", strings.Replace(summaryHTTPTestBody, `"id":"`+summaryHTTPTestID+`",`, "", 1), nil, 400},
		{"missing_version", strings.Replace(summaryHTTPTestBody, `"expected_version":"9007199254740993",`, "", 1), nil, 400},
		{"missing_model", `{"id":"` + summaryHTTPTestID + `","expected_version":"1"}`, nil, 400},
		{"null_model", strings.Replace(summaryHTTPTestBody, `"`+summaryHTTPTestModel+`"`, "null", 1), nil, 400},
		{"empty_model", strings.Replace(summaryHTTPTestBody, summaryHTTPTestModel, "", 1), nil, 400},
		{"numeric_version", strings.Replace(summaryHTTPTestBody, `"9007199254740993"`, `9007199254740993`, 1), nil, 400},
		{"leading_zero", strings.Replace(summaryHTTPTestBody, "9007199254740993", "01", 1), nil, 400},
		{"max_no_successor", strings.Replace(summaryHTTPTestBody, "9007199254740993", "9223372036854775807", 1), nil, 400},
		{"overflow", strings.Replace(summaryHTTPTestBody, "9007199254740993", "9223372036854775808", 1), nil, 400},
		{"case_alias", strings.Replace(summaryHTTPTestBody, `"model"`, `"Model"`, 1), nil, 400},
		{"unknown", strings.TrimSuffix(summaryHTTPTestBody, "}") + `,"scope":"system"}`, nil, 400},
		{"duplicate", strings.TrimSuffix(summaryHTTPTestBody, "}") + `,"model":"` + summaryHTTPTestModel + `"}`, nil, 400},
		{"trailing", summaryHTTPTestBody + `{}`, nil, 400},
		{"utf8", summaryHTTPTestBody + string([]byte{0xff}), nil, 400},
		{"too_large", summaryHTTPTestBody + strings.Repeat(" ", 16<<10), nil, 413},
		{"wrong_media", summaryHTTPTestBody, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"encoding", summaryHTTPTestBody, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, 415},
		{"query", summaryHTTPTestBody, func(r *http.Request) { r.URL.RawQuery = "private=canary" }, 400},
		{"force_query", summaryHTTPTestBody, func(r *http.Request) { r.URL.ForceQuery = true }, 400},
		{"missing_key", summaryHTTPTestBody, func(r *http.Request) { r.Header.Del("Idempotency-Key") }, 400},
		{"duplicate_key", summaryHTTPTestBody, func(r *http.Request) { r.Header.Add("Idempotency-Key", "other") }, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, b, s := summaryHTTPFixture(t)
			r := summaryRequest("PUT", tc.body)
			if tc.change != nil {
				tc.change(r)
			}
			w := summaryWriter()
			s.update = func(ctx context.Context, got mc.UpdateMeetingSummarySelectionRequest) (mc.CommandReceipt, error) {
				if got.Validate() != nil || !got.Actor.Equal(b.actor) || got.SelectionID != summaryHTTPTestID || got.ExpectedVersion != 9007199254740993 || got.Model.String() != summaryHTTPTestModel || got.Key != "original-summary-key" {
					t.Fatal("command changed")
				}
				return mc.CommandReceipt{Kind: "model.selection.update", ResourceID: got.SelectionID, Version: got.ExpectedVersion + 1}, nil
			}
			if summaryAborts(func() { h.serveHTTP(w, r) }) || w.Code != tc.status || w.flushes != 1 || s.gets.Load() != 0 {
				t.Fatal("strict dispatch", w.Code, tc.status)
			}
			if (tc.status == 200 && s.updates.Load() != 1) || (tc.status != 200 && s.updates.Load() != 0) {
				t.Fatal("unexpected service dispatch")
			}
			if tc.status == 200 && !strings.Contains(w.Body.String(), `"version":"9007199254740994"`) {
				t.Fatal("lost exact receipt")
			}
			w.cleared(t)
		})
	}
}

func TestMeetingSummaryHTTPReadsAndFaults(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, mode := range []string{"null", "configured", "body", "hidden_body", "zero_progress", "unknown", "internal", "bad_fact"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				h, _, s := summaryHTTPFixture(t)
				r := summaryRequest(method, "")
				w := summaryWriter()
				status := 200
				switch mode {
				case "configured":
					s.get = func(context.Context, id.Actor) (mc.MeetingSummarySelection, error) {
						key, _ := f.ParseID[mc.Model](summaryHTTPTestModel)
						return mc.MeetingSummarySelection{ID: summaryHTTPTestID, Version: 1, Model: &key}, nil
					}
				case "body":
					r = summaryRequest(method, "x")
					status = 400
				case "hidden_body":
					r.Body = io.NopCloser(strings.NewReader("x"))
					status = 400
				case "zero_progress":
					r.Body = io.NopCloser(summaryHTTPRead(func([]byte) (int, error) { return 0, nil }))
					status = 400
				case "unknown":
					s.get = func(context.Context, id.Actor) (mc.MeetingSummarySelection, error) {
						return mc.MeetingSummarySelection{ID: summaryHTTPTestID, Version: 1}, f.NewFault(f.CommitUnknown, f.Unknown)
					}
					status = 503
				case "internal":
					s.get = func(context.Context, id.Actor) (mc.MeetingSummarySelection, error) {
						return mc.MeetingSummarySelection{}, f.NewFault(f.InternalError, f.NotCommitted)
					}
					status = 500
				case "bad_fact":
					s.get = func(context.Context, id.Actor) (mc.MeetingSummarySelection, error) {
						return mc.MeetingSummarySelection{}, nil
					}
					status = 503
				}
				if summaryAborts(func() { h.serveHTTP(w, r) }) || w.Code != status || w.flushes != 1 {
					t.Fatal("GET/HEAD lifecycle/status", w.Code, status)
				}
				if method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD body")
				}
				if method == "GET" && status == 200 {
					var value map[string]json.RawMessage
					if json.Unmarshal(w.Body.Bytes(), &value) != nil || len(value) != 3 || value["model"] == nil {
						t.Fatal("incomplete projection")
					}
				}
				if method == "GET" && mode == "unknown" {
					var p httpapi.Problem
					if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.CommitState != f.Unknown {
						t.Fatal("unknown state overwritten")
					}
				}
				w.cleared(t)
			})
		}
	}
}

func TestMeetingSummaryHTTPBudgetAndActualTails(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "PUT"} {
		for _, parent := range []time.Duration{time.Minute, 100 * time.Millisecond} {
			t.Run(method+"/"+parent.String(), func(t *testing.T) {
				h, b, _ := summaryHTTPFixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), parent)
				defer cancel()
				r := summaryRequest(method, "")
				if method == "PUT" {
					r = summaryRequest(method, summaryHTTPTestBody)
				}
				r = r.WithContext(ctx)
				p, _ := ctx.Deadline()
				before := time.Now()
				duration := meetingSummaryHTTPReadBudget
				if method == "PUT" {
					duration = meetingSummaryHTTPWriteBudget
				}
				b.auth = func(r *http.Request, intent id.AccessIntent) (id.Actor, error) {
					d, ok := r.Context().Deadline()
					if !ok || parent < duration && !d.Equal(p) || parent > duration && (d.Before(before.Add(duration)) || d.After(time.Now().Add(duration)) || !d.Before(p)) {
						t.Fatal("pre-auth budget")
					}
					if (method == "PUT") != (intent == id.Mutate) {
						t.Fatal("wrong intent")
					}
					return b.actor, nil
				}
				w := summaryWriter()
				if summaryAborts(func() { h.serveHTTP(w, r) }) {
					t.Fatal("normal abort")
				}
				w.cleared(t)
			})
		}
	}
	for _, tail := range []string{"auth", "body", "service", "close", "write", "flush"} {
		t.Run(tail, func(t *testing.T) {
			h, b, s := summaryHTTPFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			block := func() { close(entered); <-release }
			r := summaryRequest("GET", "").WithContext(ctx)
			w := summaryWriter()
			body := &summaryHTTPBody{Reader: strings.NewReader("")}
			r.Body = body
			switch tail {
			case "auth":
				b.auth = func(*http.Request, id.AccessIntent) (id.Actor, error) { block(); return b.actor, nil }
			case "body":
				body.Reader = summaryHTTPRead(func([]byte) (int, error) { block(); return 0, io.EOF })
			case "service":
				s.get = func(context.Context, id.Actor) (mc.MeetingSummarySelection, error) {
					block()
					return mc.MeetingSummarySelection{ID: summaryHTTPTestID, Version: 1}, nil
				}
			case "close":
				body.close = func() error { block(); return nil }
			case "write":
				w.write = func(p []byte) (int, error) { block(); return len(p), nil }
			case "flush":
				w.flush = func() error { block(); return nil }
			}
			done := make(chan bool, 1)
			go func() { done <- summaryAborts(func() { h.serveHTTP(w, r) }) }()
			<-entered
			cancel()
			select {
			case <-done:
				t.Fatal("returned while actual tail active")
			case <-time.After(20 * time.Millisecond):
			}
			w.mu.Lock()
			for _, at := range w.writes {
				if at.IsZero() {
					t.Error("reset before tail completed")
				}
			}
			w.mu.Unlock()
			unblock()
			if !summaryJoined(t, done) || body.closes.Load() != 1 {
				t.Fatal("failed to abort/join actual tail")
			}
			w.cleared(t)
		})
	}
}

func TestMeetingSummaryHTTPFailuresAndCallbackJoin(t *testing.T) {
	for _, mode := range []string{"unsupported", "short", "write_error", "flush_error", "panic", "close_error", "close_panic", "reset_error"} {
		t.Run(mode, func(t *testing.T) {
			h, b, s := summaryHTTPFixture(t)
			r := summaryRequest("GET", "")
			w := summaryWriter()
			body := &summaryHTTPBody{Reader: strings.NewReader("")}
			r.Body = body
			var out http.ResponseWriter = w
			switch mode {
			case "unsupported":
				out = httptest.NewRecorder()
			case "short":
				w.write = func([]byte) (int, error) { return 1, nil }
			case "write_error":
				w.write = func([]byte) (int, error) { return 0, io.ErrClosedPipe }
			case "flush_error":
				w.flush = func() error { return io.ErrClosedPipe }
			case "panic":
				w.flush = func() error { panic("private adapter panic") }
			case "close_error":
				body.close = func() error { return io.ErrClosedPipe }
			case "close_panic":
				body.close = func() error { panic("private close panic") }
			case "reset_error":
				w.setWrite = func(at time.Time) error {
					if at.IsZero() {
						return io.ErrClosedPipe
					}
					return nil
				}
			}
			if !summaryAborts(func() { h.serveHTTP(out, r) }) || body.closes.Load() != 1 {
				t.Fatal("missing abort/close")
			}
			if mode == "unsupported" && (b.calls.Load() != 0 || s.gets.Load() != 0) {
				t.Fatal("unsupported writer reached authorization/service")
			}
			if strings.Contains(w.Body.String(), "application/problem") || strings.Contains(w.Body.String(), "private") {
				t.Fatal("second Problem")
			}
		})
	}
	t.Run("started_callback_join", func(t *testing.T) {
		h, _, _ := summaryHTTPFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		w := summaryWriter()
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		var count atomic.Int32
		w.setWrite = func(at time.Time) error {
			if !at.IsZero() && !at.After(time.Now()) && count.Add(1) == 1 {
				close(entered)
				<-release
			}
			return nil
		}
		w.flush = func() error { cancel(); <-entered; return nil }
		done := make(chan bool, 1)
		go func() { done <- summaryAborts(func() { h.serveHTTP(w, summaryRequest("HEAD", "").WithContext(ctx)) }) }()
		<-entered
		select {
		case <-done:
			t.Fatal("retired active callback")
		case <-time.After(20 * time.Millisecond):
		}
		w.mu.Lock()
		for _, at := range w.writes {
			if at.IsZero() {
				t.Error("reset raced callback")
			}
		}
		w.mu.Unlock()
		once.Do(func() { close(release) })
		if !summaryJoined(t, done) {
			t.Fatal("cancelled Flush returned success")
		}
		w.cleared(t)
	})
}
