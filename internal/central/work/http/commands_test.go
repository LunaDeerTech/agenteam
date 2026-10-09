package workhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type commandPorts struct {
	*testPorts
	structureResult c.StructureMutation
	taskResult      c.TaskMutation
	blockerResult   c.TaskBlockerMutation
	lookupState     c.LookupState
	name, target    string
	meta            f.CommandMeta
	request         any
	digest          f.Digest
}

func commandFixture() (*handler, *commandPorts) {
	h, _, p := testHandler()
	cp := &commandPorts{testPorts: p, lookupState: c.LookupCommitted}
	h.structure = cp
	h.tasks = cp
	h.blockers = cp
	return h, cp
}
func (p *commandPorts) record(ctx context.Context, name, target string, m f.CommandMeta, q any) {
	p.enter(ctx)
	p.name, p.target, p.meta, p.request = name, target, m, q
}
func (p *commandPorts) CreateMilestone(ctx context.Context, _ id.Actor, _m f.CommandMeta, _ c.ProjectID, q c.CreateMilestoneRequest) (c.StructureMutation, error) {
	p.record(ctx, string(c.MilestoneCreate), q.MilestoneID.String(), _m, q)
	return p.structureResult, p.err
}
func (p *commandPorts) UpdateMilestone(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.MilestoneID, q c.UpdateFields) (c.StructureMutation, error) {
	p.record(ctx, string(c.MilestoneUpdate), target.String(), m, q)
	return p.structureResult, p.err
}
func (p *commandPorts) ReorderMilestone(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.MilestoneID, q c.ReorderMilestoneRequest) (c.StructureMutation, error) {
	p.record(ctx, string(c.MilestoneReorder), target.String(), m, q)
	return p.structureResult, p.err
}
func (p *commandPorts) CreateSprint(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, q c.CreateSprintRequest) (c.StructureMutation, error) {
	p.record(ctx, string(c.SprintCreate), q.SprintID.String(), m, q)
	return p.structureResult, p.err
}
func (p *commandPorts) UpdateSprint(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.SprintID, q c.UpdateFields) (c.StructureMutation, error) {
	p.record(ctx, string(c.SprintUpdate), target.String(), m, q)
	return p.structureResult, p.err
}
func (p *commandPorts) ReorderSprint(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.SprintID, q c.ReorderSprintRequest) (c.StructureMutation, error) {
	p.record(ctx, string(c.SprintReorder), target.String(), m, q)
	return p.structureResult, p.err
}
func (p *commandPorts) LookupCommand(ctx context.Context, _ id.Actor, q c.CommandLookupRequest) (c.CommandLookup, error) {
	p.enter(ctx)
	p.name = string(q.Command)
	p.digest = q.Semantic
	p.meta.IdempotencyKey = q.Key
	v := c.CommandLookup{State: p.lookupState}
	if p.lookupState == c.LookupCommitted {
		v.Result = &p.structureResult
	}
	return v, p.err
}
func (p *commandPorts) CreateTask(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, q c.TaskCreate) (c.TaskMutation, error) {
	p.record(ctx, string(c.TaskCommandCreate), q.TaskID.String(), m, q)
	return p.taskResult, p.err
}
func (p *commandPorts) UpdateTask(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.TaskID, q c.TaskFieldsUpdate) (c.TaskMutation, error) {
	p.record(ctx, string(c.TaskCommandUpdate), target.String(), m, q)
	return p.taskResult, p.err
}
func (p *commandPorts) ReorderTask(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.TaskID, q c.TaskReorder) (c.TaskMutation, error) {
	p.record(ctx, string(c.TaskCommandReorder), target.String(), m, q)
	return p.taskResult, p.err
}
func (p *commandPorts) LookupTaskCommand(ctx context.Context, _ id.Actor, q c.TaskCommandLookupRequest) (c.TaskCommandLookup, error) {
	p.enter(ctx)
	p.name = string(q.Command)
	p.digest = q.SemanticDigest
	p.meta.IdempotencyKey = q.IdempotencyKey
	v := c.TaskCommandLookup{Status: p.lookupState}
	if p.lookupState == c.LookupCommitted {
		v.Receipt = &p.taskResult
	}
	return v, p.err
}
func (p *commandPorts) AddTaskBlocker(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.TaskID, q c.TaskBlockerCreate) (c.TaskBlockerMutation, error) {
	p.record(ctx, string(c.TaskBlockerCommandAdd), target.String(), m, q)
	return p.blockerResult, p.err
}
func (p *commandPorts) ResolveTaskBlocker(ctx context.Context, _ id.Actor, m f.CommandMeta, _ c.ProjectID, target c.TaskID, q c.TaskBlockerResolve) (c.TaskBlockerMutation, error) {
	p.record(ctx, string(c.TaskBlockerCommandResolve), target.String(), m, q)
	return p.blockerResult, p.err
}
func (p *commandPorts) LookupTaskBlockerCommand(ctx context.Context, _ id.Actor, q c.TaskBlockerCommandLookupRequest) (c.TaskBlockerCommandLookup, error) {
	p.enter(ctx)
	p.name = string(q.Command)
	p.digest = q.SemanticDigest
	p.meta.IdempotencyKey = q.IdempotencyKey
	v := c.TaskBlockerCommandLookup{Status: p.lookupState}
	if p.lookupState == c.LookupCommitted {
		v.Receipt = &p.blockerResult
	}
	return v, p.err
}

type commandSample struct {
	name, method, path, lookupPath, schema, lookupSchema string
	request                                              any
	expected                                             *f.Version
	target                                               string
	prepare                                              func(*commandPorts)
	digest                                               func(f.CommandMeta) (f.Digest, error)
}

func ptr[T any](v T) *T { return &v }
func commandSamples() []commandSample {
	m, s, task, blocker := testMilestone(), testSprint(), testTask(), testBlocker()
	p := m.ProjectID
	a := testActor()
	ev := testID[event.EventIdentity](20)
	te := testID[c.TaskEvent](21)
	structure := func(name c.CommandName, version f.Version) func(*commandPorts) {
		return func(p *commandPorts) {
			v := c.StructureMutation{Command: name, Changed: true, EventID: &ev}
			if name.IsSprint() {
				x := s
				x.Version = version
				v.Sprint = &x
			} else {
				x := m
				x.Version = version
				v.Milestone = &x
			}
			p.structureResult = v
		}
	}
	tasks := func(version f.Version) func(*commandPorts) {
		return func(p *commandPorts) {
			x := task
			x.Version = version
			p.taskResult = c.TaskMutation{Task: x, Changed: true, TaskEventID: &te, EventIDs: []event.EventID{ev}}
		}
	}
	bresult := func(resolve bool) func(*commandPorts) {
		return func(p *commandPorts) {
			x := task
			x.Version = 2
			b := blocker.Clone()
			if resolve {
				b.ResolvedAt = ptr(testAt())
				b.ResolvedBy = ptr(b.CreatedBy)
			}
			p.blockerResult = c.TaskBlockerMutation{Task: x, Blocker: b, TaskEventID: te, EventIDs: []event.EventID{ev}}
		}
	}
	mc := c.CreateMilestoneRequest{MilestoneID: m.ID, Title: m.Title, Description: m.Description}
	mu := c.UpdateFields{Title: &m.Title}
	mr := c.ReorderMilestoneRequest{}
	sc := c.CreateSprintRequest{SprintID: s.ID, MilestoneID: s.MilestoneID, Title: s.Title, Description: s.Description}
	su := c.UpdateFields{Title: &s.Title}
	sr := c.ReorderSprintRequest{MilestoneID: s.MilestoneID}
	tc := c.TaskCreate{TaskID: task.ID, SprintID: task.SprintID, Title: task.Title, Description: task.Description, Type: task.Type, Priority: task.Priority, Plan: task.Plan}
	tu := c.TaskFieldsUpdate{Title: &task.Title}
	tr := c.TaskReorder{}
	bc := c.TaskBlockerCreate{BlockerID: blocker.ID, Type: blocker.Type, Description: blocker.Description, Metadata: blocker.Metadata}
	br := c.TaskBlockerResolve{BlockerID: blocker.ID}
	mPath := "/milestones/" + m.ID.String()
	sPath := "/sprints/" + s.ID.String()
	tPath := "/tasks/" + task.ID.String()
	bl := tPath + "/blocker-commands/lookup"
	return []commandSample{
		{string(c.MilestoneCreate), "POST", "/milestones", "/structure-commands/lookup", "MilestoneCreateBody", "StructureLookupRequest", mc, nil, m.ID.String(), structure(c.MilestoneCreate, 1), func(meta f.CommandMeta) (f.Digest, error) { return c.CreateMilestoneDigest(a, meta, p, mc) }},
		{string(c.MilestoneUpdate), "PATCH", mPath, "/structure-commands/lookup", "MilestoneUpdateBody", "StructureLookupRequest", mu, ptr(f.Version(1)), m.ID.String(), structure(c.MilestoneUpdate, 2), func(meta f.CommandMeta) (f.Digest, error) { return c.UpdateMilestoneDigest(a, meta, p, m.ID, mu) }},
		{string(c.MilestoneReorder), "POST", mPath + "/reorder", "/structure-commands/lookup", "MilestoneReorderBody", "StructureLookupRequest", mr, ptr(f.Version(1)), m.ID.String(), structure(c.MilestoneReorder, 2), func(meta f.CommandMeta) (f.Digest, error) { return c.ReorderMilestoneDigest(a, meta, p, m.ID, mr) }},
		{string(c.SprintCreate), "POST", "/sprints", "/structure-commands/lookup", "SprintCreateBody", "StructureLookupRequest", sc, nil, s.ID.String(), structure(c.SprintCreate, 1), func(meta f.CommandMeta) (f.Digest, error) { return c.CreateSprintDigest(a, meta, p, sc) }},
		{string(c.SprintUpdate), "PATCH", sPath, "/structure-commands/lookup", "SprintUpdateBody", "StructureLookupRequest", su, ptr(f.Version(1)), s.ID.String(), structure(c.SprintUpdate, 2), func(meta f.CommandMeta) (f.Digest, error) { return c.UpdateSprintDigest(a, meta, p, s.ID, su) }},
		{string(c.SprintReorder), "POST", sPath + "/reorder", "/structure-commands/lookup", "SprintReorderBody", "StructureLookupRequest", sr, ptr(f.Version(1)), s.ID.String(), structure(c.SprintReorder, 2), func(meta f.CommandMeta) (f.Digest, error) { return c.ReorderSprintDigest(a, meta, p, s.ID, sr) }},
		{string(c.TaskCommandCreate), "POST", "/tasks", "/task-commands/lookup", "TaskCreateBody", "TaskLookupRequest", tc, nil, task.ID.String(), tasks(1), func(meta f.CommandMeta) (f.Digest, error) { return c.TaskCreateDigest(a, meta, p, tc) }},
		{string(c.TaskCommandUpdate), "PATCH", tPath, "/task-commands/lookup", "TaskUpdateBody", "TaskLookupRequest", tu, ptr(f.Version(1)), task.ID.String(), tasks(2), func(meta f.CommandMeta) (f.Digest, error) { return c.TaskUpdateDigest(a, meta, p, task.ID, tu) }},
		{string(c.TaskCommandReorder), "POST", tPath + "/reorder", "/task-commands/lookup", "TaskReorderBody", "TaskLookupRequest", tr, ptr(f.Version(1)), task.ID.String(), tasks(2), func(meta f.CommandMeta) (f.Digest, error) { return c.TaskReorderDigest(a, meta, p, task.ID, tr) }},
		{string(c.TaskBlockerCommandAdd), "POST", tPath + "/blockers", bl, "BlockerAddBody", "BlockerLookupRequest", bc, ptr(f.Version(1)), task.ID.String(), bresult(false), func(meta f.CommandMeta) (f.Digest, error) { return c.TaskBlockerAddDigest(a, meta, p, task.ID, bc) }},
		{string(c.TaskBlockerCommandResolve), "POST", tPath + "/blockers/resolve", bl, "BlockerResolveBody", "BlockerLookupRequest", br, ptr(f.Version(1)), task.ID.String(), bresult(true), func(meta f.CommandMeta) (f.Digest, error) { return c.TaskBlockerResolveDigest(a, meta, p, task.ID, br) }},
	}
}
func sampleBody(t *testing.T, s commandSample, lookup bool) string {
	t.Helper()
	v := map[string]any{"request": s.request}
	if s.expected != nil {
		v["expected_version"] = s.expected
	}
	if lookup {
		v["command"] = s.name
		if s.expected != nil && s.lookupSchema != "BlockerLookupRequest" {
			v["target_id"] = s.target
		}
	}
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func commandRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, testPath(path), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "saved-original-intent")
	return r
}

func TestWorkHTTPCommandsAndOriginalIntentLookup(t *testing.T) {
	for _, s := range commandSamples() {
		t.Run(s.name, func(t *testing.T) {
			h, p := commandFixture()
			s.prepare(p)
			w := newTestWriter()
			r := commandRequest(s.method, s.path, sampleBody(t, s, false))
			if serveTest(h, r, w) || w.Code != 200 || p.calls != 1 || p.name != s.name || p.target != s.target || p.meta.RequestID.Validate() != nil {
				t.Fatal("mutation dispatch", w.Code)
			}
			if p.meta.IdempotencyKey != "saved-original-intent" || (p.meta.ExpectedVersion == nil) != (s.expected == nil) || s.expected != nil && *p.meta.ExpectedVersion != *s.expected {
				t.Fatal("original meta changed")
			}
			want, e := s.digest(p.meta)
			if e != nil {
				t.Fatal(e)
			}
			for _, state := range []c.LookupState{c.LookupCommitted, c.LookupInProgress, c.LookupNotObserved} {
				lookup, lp := commandFixture()
				s.prepare(lp)
				lp.lookupState = state
				lw := newTestWriter()
				if serveTest(lookup, commandRequest("POST", s.lookupPath, sampleBody(t, s, true)), lw) || lw.Code != 200 || lp.calls != 1 || lp.digest != want || lp.name != s.name || lp.meta.IdempotencyKey != p.meta.IdempotencyKey {
					t.Fatal("Lookup did not preserve original intent", state, lw.Code)
				}
				var v map[string]json.RawMessage
				if json.Unmarshal(lw.Body.Bytes(), &v) != nil || len(v) != 2 {
					t.Fatal("Lookup wire is not closed")
				}
				field := "receipt"
				if s.lookupSchema == "StructureLookupRequest" {
					field = "result"
				}
				if raw, ok := v[field]; !ok || state != c.LookupCommitted && string(raw) != "null" {
					t.Fatal("Lookup null presence")
				}
			}
		})
	}
}
func TestWorkHTTPCommandEnvelopeAndInputRejection(t *testing.T) {
	s := commandSamples()[7]
	good := sampleBody(t, s, false)
	for _, raw := range []string{
		`{}`, `null`, `[]`, `{"expected_version":"1","request":null}`, `{"expected_version":"1","request":[]}`, `{"expected_version":"1","request":{"title":null}}`,
		`{"expected_version":1,"request":{"title":"x"}}`, `{"expected_version":"01","request":{"title":"x"}}`, `{"expected_version":"9223372036854775808","request":{"title":"x"}}`,
		`{"expected_version":null,"request":{"title":"x"}}`, `{"expected_version":"1","request":{"Title":"x"}}`, `{"expected_version":"1","request":{"title":"x","title":"y"}}`,
		`{"expected_version":"1","request":{"title":"\ud800"}}`, `{"Expected_version":"1","request":{"title":"x"}}`, `{"expected_version":"1","expected_version":"1","request":{"title":"x"}}`,
		good + `{}`, strings.Replace(good, `"request":`, `"caller":"x","request":`, 1), strings.Replace(good, `"request":`, `"command":"work.task.update","request":`, 1),
	} {
		h, p := commandFixture()
		w := newTestWriter()
		if serveTest(h, commandRequest(s.method, s.path, raw), w) || w.Code != 400 || p.calls != 0 {
			t.Fatal("bad envelope reached service", w.Code)
		}
	}
	for _, edit := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Idempotency-Key") }, func(r *http.Request) { r.Header.Add("Idempotency-Key", "duplicate") }, func(r *http.Request) { r.Header.Set("Idempotency-Key", " key ") },
		func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }, func(r *http.Request) { r.URL.ForceQuery = true }, func(r *http.Request) { r.URL.RawQuery = "key=x" },
	} {
		h, p := commandFixture()
		r := commandRequest(s.method, s.path, good)
		edit(r)
		w := newTestWriter()
		if serveTest(h, r, w) || w.Code < 400 || p.calls != 0 {
			t.Fatal("invalid framing/header reached service", w.Code)
		}
	}
	for _, s := range commandSamples() {
		h, p := commandFixture()
		raw := sampleBody(t, s, s.lookupSchema != "")
		var v map[string]any
		_ = json.Unmarshal([]byte(raw), &v)
		v["target_id"] = "invalid"
		bad, _ := json.Marshal(v)
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", s.lookupPath, string(bad)), w) || w.Code != 400 || p.calls != 0 {
			t.Fatal("wrong lookup target admitted")
		}
	}
}
func TestWorkHTTPBadCommittedProjectionAbortsAndLookupRejects(t *testing.T) {
	for _, s := range commandSamples() {
		t.Run(s.name, func(t *testing.T) {
			for _, lookup := range []bool{false, true} {
				h, p := commandFixture()
				s.prepare(p)
				if p.structureResult.Milestone != nil {
					p.structureResult.Milestone.ProjectID = testID[id.Project](99)
				}
				if p.structureResult.Sprint != nil {
					p.structureResult.Sprint.ProjectID = testID[id.Project](99)
				}
				p.taskResult.Task.ProjectID = testID[id.Project](99)
				p.blockerResult.Task.ProjectID = testID[id.Project](99)
				method, path := s.method, s.path
				if lookup {
					method, path = "POST", s.lookupPath
				}
				w := newTestWriter()
				aborted := serveTest(h, commandRequest(method, path, sampleBody(t, s, lookup)), w)
				if lookup {
					if aborted || w.Code != 503 {
						t.Fatal("bad Lookup projection published")
					}
				} else if !aborted || w.Body.Len() != 0 {
					t.Fatal("bad committed result fabricated Problem")
				}
			}
		})
	}
}
func TestWorkHTTPMaximumVersionNoopAndUnknown(t *testing.T) {
	s := commandSamples()[7]
	s.expected = ptr(f.Version(math.MaxInt64))
	h, p := commandFixture()
	s.prepare(p)
	p.taskResult.Task.Version = *s.expected
	p.taskResult.Changed = false
	p.taskResult.TaskEventID = nil
	p.taskResult.EventIDs = []event.EventID{}
	w := newTestWriter()
	if serveTest(h, commandRequest(s.method, s.path, sampleBody(t, s, false)), w) || w.Code != 200 {
		t.Fatal("MaxInt64 legitimate no-op rejected")
	}
	for _, s := range commandSamples() {
		h, p := commandFixture()
		p.err = f.NewFault(f.CommitUnknown, f.Unknown)
		w := newTestWriter()
		if serveTest(h, commandRequest(s.method, s.path, sampleBody(t, s, false)), w) || w.Code != 503 {
			t.Fatal("Unknown dispatch")
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		if v["commit_state"] != "unknown" || v["retry_hint"] != "lookup" {
			t.Fatal("Unknown projected as not committed")
		}
	}
}

func TestWorkHTTPContentEncodingPresenceRejected(t *testing.T) {
	s := commandSamples()[7]
	h, p := commandFixture()
	s.prepare(p)
	r := commandRequest(s.method, s.path, sampleBody(t, s, false))
	r.Header["Content-Encoding"] = []string{""}
	w := newTestWriter()
	if serveTest(h, r, w) || w.Code != 415 || p.calls != 0 {
		t.Fatal("empty Content-Encoding was accepted", w.Code)
	}
}

func TestWorkHTTPPrivateCommandLogs(t *testing.T) {
	for _, mode := range []string{"success", "reject", "unknown", "abort"} {
		t.Run(mode, func(t *testing.T) {
			s := commandSamples()[7]
			title := "private-title-canary"
			s.request = c.TaskFieldsUpdate{Title: &title}
			h, p := commandFixture()
			s.prepare(p)
			p.taskResult.Task.Title = title
			body := sampleBody(t, s, false)
			switch mode {
			case "reject":
				body = strings.Replace(body, `"request":`, `"private-unknown-canary":true,"request":`, 1)
			case "unknown":
				p.err = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(fmt.Errorf("private-cause-canary"))
			case "abort":
				p.taskResult.Task.ID = testID[c.Task](99)
			}
			r := commandRequest(s.method, s.path, body)
			r.Header.Set("Idempotency-Key", "private-key-canary")
			var logs bytes.Buffer
			w := newTestWriter()
			aborted := testAbort(func() { httpapi.Handler(slog.New(slog.NewJSONHandler(&logs, nil)), h).ServeHTTP(w, r) })
			if strings.Contains(logs.String(), "canary") || aborted != (mode == "abort") {
				t.Fatal("command output/log ownership")
			}
			if mode != "success" && strings.Contains(w.Body.String(), "canary") {
				t.Fatal("error leaked original intent")
			}
		})
	}
}
