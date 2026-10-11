package agenthttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func directoryID[T any](n int) f.ID[T] {
	v, e := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if e != nil {
		panic(e)
	}
	return v
}
func directorySample() c.DirectoryEntry {
	at, _ := f.ParseInstant("2026-10-09T01:02:03.000001Z")
	return c.DirectoryEntry{ID: directoryID[i.Agent](3), ProjectID: directoryID[i.Project](4), Name: "Agent-One", Description: "directory description", Version: 2, CreatedAt: at, UpdatedAt: at}
}
func directoryPath() string { return directoryPrefix + directoryID[i.Project](4).String() + "/agents" }

type directoryTestBoundary struct {
	authErr  error
	checkErr error
}

func (b *directoryTestBoundary) CheckRequest(http.ResponseWriter, *http.Request) error {
	return b.checkErr
}
func (b *directoryTestBoundary) RequireHuman(*http.Request) (i.Actor, error) {
	a, _ := i.NewHuman(directoryID[i.User](1), directoryID[i.Session](2))
	return a, b.authErr
}
func (*directoryTestBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, e error) {
	(&account.HTTPBoundary{}).WriteProblem(w, r, e)
}

type directoryTestReader struct {
	calls  int
	before func(context.Context)
	err    error
	value  c.DirectoryEntry
	page   f.Page[c.DirectoryEntry]
	query  f.PageRequest
}

func (p *directoryTestReader) enter(ctx context.Context) {
	p.calls++
	if p.before != nil {
		p.before(ctx)
	}
}
func (p *directoryTestReader) GetAgent(ctx context.Context, _ i.Actor, _ i.ProjectID, _ i.AgentID) (c.DirectoryEntry, error) {
	p.enter(ctx)
	return p.value, p.err
}
func (p *directoryTestReader) ListAgents(ctx context.Context, _ i.Actor, _ i.ProjectID, q f.PageRequest) (f.Page[c.DirectoryEntry], error) {
	p.enter(ctx)
	p.query = q
	return p.page, p.err
}
func directoryFixture() (*directoryHandler, *directoryTestBoundary, *directoryTestReader) {
	b := &directoryTestBoundary{}
	v := directorySample()
	p := &directoryTestReader{value: v, page: f.Page[c.DirectoryEntry]{Items: []c.DirectoryEntry{v}}}
	return &directoryHandler{p, b}, b, p
}

type directoryWriter struct {
	*httptest.ResponseRecorder
	mu            sync.Mutex
	reads, writes []time.Time
}

func (w *directoryWriter) SetReadDeadline(at time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reads = append(w.reads, at)
	return nil
}
func (w *directoryWriter) SetWriteDeadline(at time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, at)
	return nil
}
func (w *directoryWriter) FlushError() error { return nil }
func directoryServe(h http.Handler, r *http.Request) (w *directoryWriter, aborted bool) {
	w = &directoryWriter{ResponseRecorder: httptest.NewRecorder()}
	defer func() {
		if v := recover(); v != nil {
			if v != http.ErrAbortHandler {
				panic(v)
			}
			aborted = true
		}
	}()
	httpapi.WithRequestID(nil, h).ServeHTTP(w, r)
	return
}
func TestAgentDirectoryHTTPReadBoundaryAndProjection(t *testing.T) {
	h, b, p := directoryFixture()
	w, abort := directoryServe(h, httptest.NewRequest("GET", directoryPath(), nil))
	if abort || w.Code != 200 || p.calls != 1 || p.query.Limit != 50 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
		t.Fatal("GET representation")
	}
	head, abort := directoryServe(h, httptest.NewRequest("HEAD", directoryPath(), nil))
	if abort || head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != w.Header().Get("Content-Length") || p.calls != 2 {
		t.Fatal("HEAD skipped actual read or changed representation")
	}
	for _, path := range []string{directoryPath() + "?limit=0", directoryPath() + "?limit=201", directoryPath() + "?limit=01", directoryPath() + "?limit=1&limit=2", directoryPath() + "?%6cimit=1&limit=2", directoryPath() + "?cursor=", directoryPath() + "?search=x", directoryPath() + "?", directoryPath() + "?cursor=x;limit=1", directoryPath() + "/" + p.value.ID.String() + "?limit=1"} {
		before := p.calls
		w, aborted := directoryServe(h, httptest.NewRequest("GET", path, nil))
		if aborted || w.Code != 400 || p.calls != before {
			t.Fatalf("noncanonical query accepted: %s", path)
		}
	}
	for _, method := range []string{"POST", "PATCH", "DELETE", "OPTIONS"} {
		before := p.calls
		w, a := directoryServe(h, httptest.NewRequest(method, directoryPath(), nil))
		if a || w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || p.calls != before {
			t.Fatal("directory granted write")
		}
	}
	before := p.calls
	w, abort = directoryServe(h, httptest.NewRequest("GET", directoryPath(), strings.NewReader("x")))
	if abort || w.Code != 400 || p.calls != before {
		t.Fatal("GET body accepted")
	}
	b.authErr = f.NewFault(f.Unauthenticated, f.NotStarted)
	w, abort = directoryServe(h, httptest.NewRequest("GET", directoryPath(), nil))
	if abort || w.Code != 401 || p.calls != before {
		t.Fatal("read before current Session")
	}
	b.authErr = nil
	p.err = f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(errors.New("private-config-material"))
	w, abort = directoryServe(h, httptest.NewRequest("GET", directoryPath(), nil))
	if abort || w.Code != 503 || strings.Contains(w.Body.String(), "private-config-material") || strings.Contains(w.Body.String(), p.value.Description) {
		t.Fatal("error leaked projection/material")
	}
	p.err = nil
	p.page.Items = append(p.page.Items, p.value)
	w, abort = directoryServe(h, httptest.NewRequest("GET", directoryPath(), nil))
	if abort || w.Code != 503 || strings.Contains(w.Body.String(), p.value.Name) {
		t.Fatal("duplicate page published")
	}
	p.value.ProjectID = directoryID[i.Project](5)
	w, abort = directoryServe(h, httptest.NewRequest("GET", directoryPath()+"/"+p.value.ID.String(), nil))
	if abort || w.Code != 503 {
		t.Fatal("foreign detail published")
	}
}

type directoryBody struct {
	io.Reader
	closes atomic.Int32
}

func (b *directoryBody) Close() error { b.closes.Add(1); return nil }
func TestAgentDirectoryHTTPOriginalCallAndIOJoin(t *testing.T) {
	h, _, p := directoryFixture()
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	var once sync.Once
	p.before = func(ctx context.Context) { entered <- ctx; <-release }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &directoryBody{Reader: strings.NewReader("")}
	r := httptest.NewRequest("GET", directoryPath(), nil).WithContext(ctx)
	r.Body = body
	done := make(chan bool, 1)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	go func() { _, aborted := directoryServe(h, r); done <- aborted }()
	select {
	case original := <-entered:
		if deadline, ok := original.Deadline(); !ok || time.Until(deadline) > readBudget {
			t.Fatal("missing natural budget")
		}
	case <-time.After(time.Second):
		t.Fatal("original read absent")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("HTTP returned while original port held")
	default:
	}
	if body.closes.Load() != 0 {
		t.Fatal("body retired before original call")
	}
	once.Do(func() { close(release) })
	select {
	case aborted := <-done:
		if !aborted || body.closes.Load() != 1 {
			t.Fatal("cancel published success or wrong Close")
		}
	case <-time.After(time.Second):
		t.Fatal("original read did not join")
	}
}
