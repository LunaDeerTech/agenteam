package workhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func testID[K any](n int) f.ID[K] {
	v, e := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if e != nil {
		panic(e)
	}
	return v
}
func testAt() f.Instant {
	v, e := f.ParseInstant("2026-10-09T01:02:03.123456Z")
	if e != nil {
		panic(e)
	}
	return v
}
func testActor() id.Actor {
	a, e := id.NewHuman(testID[id.User](1), testID[id.Session](2))
	if e != nil {
		panic(e)
	}
	return a
}
func testMilestone() c.Milestone {
	return c.Milestone{ID: testID[c.Milestone](3), ProjectID: testID[id.Project](4), Title: "milestone", Description: "description canary", ManualRank: "7fffffffffffffffffffffffffffffff", Version: 1, CreatedAt: testAt(), UpdatedAt: testAt()}
}
func testSprint() c.Sprint {
	m := testMilestone()
	return c.Sprint{ID: testID[pc.Sprint](5), ProjectID: m.ProjectID, MilestoneID: m.ID, Title: "sprint", Description: "description canary", State: c.Planned, ManualRank: "7fffffffffffffffffffffffffffffff", Version: 1, CreatedAt: testAt(), UpdatedAt: testAt()}
}
func testTask() c.Task {
	s := testSprint()
	return c.Task{ID: testID[c.Task](6), ProjectID: s.ProjectID, MilestoneID: s.MilestoneID, SprintID: s.ID, Title: "task", Description: "description canary", Plan: "plan canary", Type: c.TaskTypeTask, Priority: c.TaskPriorityMedium, State: c.TaskStateBacklog, ManualRank: "7fffffffffffffffffffffffffffffff", Version: 1, CreatedAt: testAt(), UpdatedAt: testAt()}
}
func testBlocker() c.TaskBlocker {
	v := testTask()
	return c.TaskBlocker{ID: testID[c.TaskBlockerIdentity](7), ProjectID: v.ProjectID, TaskID: v.ID, Type: c.TaskBlockerWaitingForHuman, Description: "blocker canary", Metadata: c.TaskBlockerMetadata{WaitingForHuman: &c.TaskBlockerWaitingForHumanMetadata{}}, CreatedAt: testAt(), CreatedBy: c.TaskEventActor{Type: id.Human, UserID: testID[id.User](1), Source: "task_domain"}}
}
func testPath(s string) string { return projectPrefix + testMilestone().ProjectID.String() + s }

type testBoundary struct {
	check, auth   func(*http.Request) error
	checks, auths atomic.Int32
}

func (b *testBoundary) CheckRequest(_ http.ResponseWriter, r *http.Request) error {
	b.checks.Add(1)
	if b.check != nil {
		return b.check(r)
	}
	return nil
}
func (b *testBoundary) RequireHuman(r *http.Request) (id.Actor, error) {
	b.auths.Add(1)
	if b.auth != nil {
		if err := b.auth(r); err != nil {
			return id.Actor{}, err
		}
	}
	return testActor(), nil
}
func (*testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	(&account.HTTPBoundary{}).WriteProblem(w, r, err)
}

// These private ports test the transport's handling of typed values and bad
// dependencies. They do not prove database, real authentication, or app wiring.
type testPorts struct {
	c.Reader
	c.TaskReader
	c.Commands
	c.TaskCommands
	c.TaskBlockerCommands
	c.TaskBlockerPageReader
	calls    int
	before   func(context.Context)
	err      error
	m        c.Milestone
	s        c.Sprint
	t        c.Task
	b        c.TaskBlocker
	ml       f.Page[c.Milestone]
	sl       f.Page[c.Sprint]
	tl       f.Page[c.Task]
	bl       f.Page[c.TaskBlocker]
	gotQuery query
}

func newTestPorts() *testPorts {
	m, s, t, b := testMilestone(), testSprint(), testTask(), testBlocker()
	return &testPorts{m: m, s: s, t: t, b: b, ml: f.Page[c.Milestone]{Items: []c.Milestone{m}}, sl: f.Page[c.Sprint]{Items: []c.Sprint{s}}, tl: f.Page[c.Task]{Items: []c.Task{t}}, bl: f.Page[c.TaskBlocker]{Items: []c.TaskBlocker{b}}}
}
func (p *testPorts) enter(ctx context.Context) {
	p.calls++
	if p.before != nil {
		p.before(ctx)
	}
}
func (p *testPorts) GetMilestone(ctx context.Context, _ id.Actor, _ c.ProjectID, _ c.MilestoneID) (c.Milestone, error) {
	p.enter(ctx)
	return p.m, p.err
}
func (p *testPorts) ListMilestones(ctx context.Context, _ id.Actor, _ c.ProjectID, q f.PageRequest) (f.Page[c.Milestone], error) {
	p.enter(ctx)
	p.gotQuery.page = q
	return p.ml, p.err
}
func (p *testPorts) GetSprint(ctx context.Context, _ id.Actor, _ c.ProjectID, _ c.SprintID) (c.Sprint, error) {
	p.enter(ctx)
	return p.s, p.err
}
func (p *testPorts) ListSprints(ctx context.Context, _ id.Actor, _ c.ProjectID, m c.MilestoneID, q f.PageRequest) (f.Page[c.Sprint], error) {
	p.enter(ctx)
	p.gotQuery.milestone = m
	p.gotQuery.page = q
	return p.sl, p.err
}
func (p *testPorts) GetTask(ctx context.Context, _ id.Actor, _ c.ProjectID, _ c.TaskID) (c.Task, error) {
	p.enter(ctx)
	return p.t, p.err
}
func (p *testPorts) ListTasks(ctx context.Context, _ id.Actor, _ c.ProjectID, filter c.TaskFilter, q f.PageRequest) (f.Page[c.Task], error) {
	p.enter(ctx)
	p.gotQuery.filter = filter
	p.gotQuery.page = q
	return p.tl, p.err
}
func (p *testPorts) ListTaskBlockersPage(ctx context.Context, _ id.Actor, _ c.ProjectID, _ c.TaskID, status c.TaskBlockerStatus, q f.PageRequest) (f.Page[c.TaskBlocker], error) {
	p.enter(ctx)
	p.gotQuery.status = status
	p.gotQuery.page = q
	return p.bl, p.err
}
func testHandler() (*handler, *testBoundary, *testPorts) {
	b, p := &testBoundary{}, newTestPorts()
	return &handler{structure: p, structureReader: p, tasks: p, taskReader: p, blockers: p, blockerReader: p, boundary: b}, b, p
}

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

func TestWorkHTTPBindingAndExactRoutes(t *testing.T) {
	b := Bindings{Structure: &work.Service{}, StructureReader: &work.Reader{}, Tasks: &work.TaskService{}, TaskReader: &work.TaskReader{}, Blockers: &work.BlockerService{}, BlockerReader: &work.BlockerReader{}}
	if _, e := NewHTTPHandler(b, &account.HTTPBoundary{}); e != nil {
		t.Fatal(e)
	}
	for n := range 7 {
		v := b
		boundary := &account.HTTPBoundary{}
		switch n {
		case 0:
			v.Structure = nil
		case 1:
			v.StructureReader = nil
		case 2:
			v.Tasks = nil
		case 3:
			v.TaskReader = nil
		case 4:
			v.Blockers = nil
		case 5:
			v.BlockerReader = nil
		case 6:
			boundary = nil
		}
		if h, e := NewHTTPHandler(v, boundary); h != nil || e == nil {
			t.Fatal("nil binding admitted", n)
		}
	}
	for _, path := range []string{"/milestones", "/milestones/x", "/milestones/x/reorder", "/sprints", "/sprints/x", "/sprints/x/reorder", "/structure-commands/lookup", "/tasks", "/tasks/x", "/tasks/x/reorder", "/task-commands/lookup", "/tasks/x/blockers", "/tasks/x/blockers/resolve", "/tasks/x/blocker-commands/lookup"} {
		if !HandlesPath(testPath(path)) {
			t.Fatal("missing shape", path)
		}
	}
	for _, path := range []string{"", "/commands/lookup", "/model", "/usage", "/milestones/", "/tasks/x/blockers/x", "/tasks/x/rollover", "/tasks/x/blockers/resolve/", "/tasks//blockers"} {
		if HandlesPath(testPath(path)) {
			t.Fatal("claimed other route", path)
		}
	}
}
func TestWorkHTTPReadsAndHEAD(t *testing.T) {
	m, s, v := testMilestone(), testSprint(), testTask()
	for _, suffix := range []string{"/milestones", "/milestones/" + m.ID.String(), "/sprints?milestone_id=" + m.ID.String(), "/sprints/" + s.ID.String(), "/tasks", "/tasks/" + v.ID.String(), "/tasks/" + v.ID.String() + "/blockers"} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+suffix, func(t *testing.T) {
				h, b, p := testHandler()
				w := newTestWriter()
				r := httptest.NewRequest(method, testPath(suffix), nil)
				if serveTest(h, r, w) || w.Code != 200 || b.auths.Load() != 1 || p.calls != 1 {
					t.Fatal("read failed", w.Code)
				}
				if w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Length") == "" {
					t.Fatal("representation headers")
				}
				if method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD entity")
				}
				if method == "GET" && strings.Contains(w.Body.String(), `"items"`) && (strings.Contains(w.Body.String(), `"plan"`) || strings.Contains(w.Body.String(), "description canary")) {
					t.Fatal("large text escaped into summary")
				}
				if strings.HasSuffix(suffix, "/blockers") && p.gotQuery.status != c.TaskBlockersUnresolved {
					t.Fatal("default status")
				}
				w.cleared(t)
			})
		}
	}
}
func TestWorkHTTPReadRejectionAndNoPartialProjection(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*testPorts)
	}{
		{"invalid hidden description", func(p *testPorts) { p.tl.Items[0].Description = string([]byte{0xff}) }},
		{"wrong project", func(p *testPorts) { p.tl.Items[0].ProjectID = testID[id.Project](99) }},
		{"duplicate", func(p *testPorts) { p.tl.Items = append(p.tl.Items, p.tl.Items[0]) }},
		{"bad cursor", func(p *testPorts) { p.tl.NextCursor = "bad\x00" }},
		{"cursor without full page", func(p *testPorts) { p.tl.NextCursor = "opaque" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, p := testHandler()
			tc.edit(p)
			w := newTestWriter()
			if serveTest(h, httptest.NewRequest("GET", testPath("/tasks"), nil), w) || w.Code != 503 || strings.Contains(w.Body.String(), "plan canary") {
				t.Fatal("unsafe projection", w.Code)
			}
		})
	}
	for _, tc := range []struct {
		method, path string
		body         io.Reader
		status       int
	}{
		{"GET", "/tasks?limit=01", nil, 400}, {"GET", "/tasks/invalid", nil, 400}, {"GET", "/sprints", nil, 400}, {"GET", "/tasks/" + testTask().ID.String() + "?limit=1", nil, 400}, {"DELETE", "/tasks", nil, 405}, {"GET", "/foreign", nil, 404},
	} {
		h, _, p := testHandler()
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest(tc.method, testPath(tc.path), tc.body), w) || w.Code != tc.status || p.calls != 0 {
			t.Fatal("invalid request reached dependency", tc.path, w.Code)
		}
	}
	h, _, p := testHandler()
	w := newTestWriter()
	r := httptest.NewRequest("GET", testPath("/tasks"), nil)
	body := &testBody{Reader: strings.NewReader("x")}
	r.Body = body
	r.ContentLength = 0
	if serveTest(h, r, w) || w.Code != 400 || p.calls != 0 || body.closes.Load() != 1 {
		t.Fatal("actual CL0 body ignored")
	}
	// A missing outer identity cannot silently generate a new transport ID.
	h, _, p = testHandler()
	w = newTestWriter()
	if testAbort(func() { h.ServeHTTP(w, httptest.NewRequest("GET", testPath("/tasks"), nil)) }) || w.Code != 400 || p.calls != 0 {
		t.Fatal("missing RequestID")
	}
}
func TestWorkHTTPSafeReadLog(t *testing.T) {
	var logs bytes.Buffer
	h, _, p := testHandler()
	p.err = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(fmt.Errorf("private-request-canary"))
	r := httptest.NewRequest("GET", testPath("/tasks?text=private-query-canary"), nil)
	w := newTestWriter()
	testAbort(func() { httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(w, r) })
	if strings.Contains(logs.String(), "canary") || strings.Contains(w.Body.String(), "canary") {
		t.Fatal("private input leaked")
	}
	var result map[string]any
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result["commit_state"] != "unknown" {
		t.Fatal("Unknown lost")
	}
}
