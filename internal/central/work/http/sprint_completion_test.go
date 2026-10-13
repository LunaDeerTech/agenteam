package workhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Controlled domain ports establish the transport's exact calls and output
// gates. These controls do not prove real Owner/SQL/Complete publication.
type sprintCompletionPort struct {
	result          c.SprintCompleteMutation
	state           c.LookupState
	lookupOverride  *c.SprintCompleteLookup
	err             error
	writes, lookups int
	actor           id.Actor
	meta            f.CommandMeta
	project         c.ProjectID
	target          c.SprintID
	query           c.SprintCompleteLookupRequest
	deadline        time.Time
}

func (p *sprintCompletionPort) CompleteSprint(ctx context.Context, actor id.Actor, meta f.CommandMeta, project c.ProjectID, sprint c.SprintID) (c.SprintCompleteMutation, error) {
	p.writes++
	p.actor, p.meta, p.project, p.target = actor, meta, project, sprint
	p.deadline, _ = ctx.Deadline()
	return p.result.Clone(), p.err
}
func (p *sprintCompletionPort) LookupCompleteSprint(ctx context.Context, actor id.Actor, q c.SprintCompleteLookupRequest) (c.SprintCompleteLookup, error) {
	p.lookups++
	p.actor, p.query = actor, q
	p.deadline, _ = ctx.Deadline()
	if p.lookupOverride != nil {
		return *p.lookupOverride, p.err
	}
	v := c.SprintCompleteLookup{State: p.state}
	if p.state == c.LookupCommitted {
		r := p.result.Clone()
		v.Result = &r
	}
	return v, p.err
}
func sprintCompletionFixture() (*handler, *testBoundary, *testPorts, *sprintCompletionPort) {
	h, boundary, old := testHandler()
	s := testSprint()
	s.Version, s.State = 3, c.Completed
	s.StartedAt = ptr(testAt())
	s.StartedBy = &c.ActorHistory{Kind: id.Human, UserID: testID[id.User](99).String()}
	s.CompletedAt = ptr(testAt())
	s.CompletedBy = &c.ActorHistory{Kind: id.Human, UserID: testActor().Details().UserID}
	p := &sprintCompletionPort{state: c.LookupCommitted, result: c.SprintCompleteMutation{
		Sprint: s, Project: pc.ProjectRef{ID: s.ProjectID, OwnerUserID: testID[id.User](1), Name: "Sprint-Complete", NormalizedName: "sprint-complete", Description: "", Lifecycle: pc.Active, Version: 3, CurrentSprintID: nil, CreatedAt: testAt(), UpdatedAt: testAt()},
		EventID: testID[event.EventIdentity](51),
	}}
	h.sprintCompletion = p
	return h, boundary, old, p
}
func sprintCompleteBody(lookup bool) string {
	if lookup {
		return `{"command":"work.sprint.complete","target_id":"` + testSprint().ID.String() + `","expected_version":"2","request":{}}`
	}
	return `{"expected_version":"2","request":{}}`
}
func sprintCompletePath(lookup bool) string {
	if lookup {
		return "/sprint-lifecycle-commands/lookup"
	}
	return "/sprints/" + testSprint().ID.String() + "/complete"
}

func TestWorkHTTPSprintCompleteAndOriginalLookup(t *testing.T) {
	h, _, old, p := sprintCompletionFixture()
	w := newTestWriter()
	if serveTest(h, commandRequest("POST", sprintCompletePath(false), sprintCompleteBody(false)), w) || w.Code != 200 || p.writes != 1 || p.lookups != 0 || old.calls != 0 {
		t.Fatal("Complete did not call the original service exactly once", w.Code)
	}
	if !p.actor.Equal(testActor()) || p.project != testSprint().ProjectID || p.target != testSprint().ID || p.meta.RequestID.Validate() != nil || p.meta.ExpectedVersion == nil || *p.meta.ExpectedVersion != 2 || p.meta.IdempotencyKey != "saved-original-intent" {
		t.Fatal("Complete changed the original actor, target or metadata")
	}
	if remaining := time.Until(p.deadline); remaining <= 0 || remaining > mutationBudget {
		t.Fatal("Complete budget changed")
	}
	w.cleared(t)
	original := append([]byte(nil), w.Body.Bytes()...)
	want, err := c.CompleteSprintDigest(p.actor, p.meta, p.project, p.target)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []c.LookupState{c.LookupCommitted, c.LookupInProgress, c.LookupNotObserved} {
		h, _, old, p := sprintCompletionFixture()
		p.state = state
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", sprintCompletePath(true), sprintCompleteBody(true)), w) || w.Code != 200 || p.writes != 0 || p.lookups != 1 || old.calls != 0 {
			t.Fatal("Lookup wrote or used current GET", state, w.Code)
		}
		if p.query.ProjectID != testSprint().ProjectID || p.query.Key != "saved-original-intent" || p.query.Semantic != want || !p.actor.Equal(testActor()) {
			t.Fatal("Lookup changed the saved original intent")
		}
		if remaining := time.Until(p.deadline); remaining <= 0 || remaining > readBudget {
			t.Fatal("Lookup budget changed")
		}
		value := wireObject(t, w.Body.Bytes())
		if len(value) != 2 || value["status"] != string(state) || (value["receipt"] != nil) != (state == c.LookupCommitted) {
			t.Fatal("Lookup did not project the closed public union")
		}
		if state == c.LookupCommitted {
			var decoded sprintCompleteLookupWire
			if json.Unmarshal(w.Body.Bytes(), &decoded) != nil {
				t.Fatal("receipt decode")
			}
			raw, err := json.Marshal(decoded.Receipt)
			if err != nil || !bytes.Equal(raw, original) {
				t.Fatal("Lookup altered the historical receipt")
			}
		}
		w.cleared(t)
	}
}

func TestWorkHTTPSprintCompleteStrictIntent(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		good := sprintCompleteBody(lookup)
		bad := []string{`{}`, good + `{}`,
			strings.Replace(good, `"expected_version":"2"`, `"expected_version":2`, 1),
			strings.Replace(good, `"expected_version":"2"`, `"expected_version":null`, 1),
			strings.Replace(good, `"expected_version":"2"`, `"expected_version":"02"`, 1),
			strings.Replace(good, `"expected_version":"2"`, `"expected_version":"2","expected_version":"2"`, 1),
			strings.Replace(good, `"expected_version"`, `"Expected_version"`, 1),
			strings.Replace(good, `"request":{}`, `"request":null`, 1),
			strings.Replace(good, `"request":{}`, `"request":[]`, 1),
			strings.Replace(good, `,"request":{}`, ``, 1),
			strings.Replace(good, `"request":{}`, `"request":{"scheduler_enabled":true}`, 1),
			strings.Replace(good, `"request":{}`, `"request":{"actor":"human"}`, 1),
			strings.Replace(good, `"request":{}`, `"request":{"x":"\ud800"}`, 1),
			strings.Replace(good, `"request":{}`, `"semantic_digest":"caller-digest","request":{}`, 1),
		}
		if lookup {
			bad = append(bad, strings.Replace(good, "work.sprint.complete", "work.sprint.unknown", 1), strings.Replace(good, `"target_id"`, `"sprint_id"`, 1), strings.Replace(good, testSprint().ID.String(), "invalid", 1))
		} else {
			bad = append(bad, strings.Replace(good, `"request":{}`, `"command":"work.sprint.complete","request":{}`, 1))
		}
		for _, body := range bad {
			h, _, old, p := sprintCompletionFixture()
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", sprintCompletePath(lookup), body), w) || w.Code != 400 || p.writes+p.lookups+old.calls != 0 {
				t.Fatal("malformed Complete intent reached a provider", lookup, w.Code)
			}
		}
		for _, edit := range []func(*http.Request){
			func(r *http.Request) { r.Header.Del("Idempotency-Key") },
			func(r *http.Request) { r.Header.Add("Idempotency-Key", "second") },
			func(r *http.Request) { r.URL.ForceQuery = true },
			func(r *http.Request) { r.URL.RawQuery = "ignored=true" },
			func(r *http.Request) { r.Header["Content-Encoding"] = []string{""} },
			func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		} {
			h, _, old, p := sprintCompletionFixture()
			r := commandRequest("POST", sprintCompletePath(lookup), good)
			edit(r)
			w := newTestWriter()
			if serveTest(h, r, w) || (w.Code != 400 && w.Code != 415) || p.writes+p.lookups+old.calls != 0 {
				t.Fatal("invalid transport reached a provider", w.Code)
			}
		}
		h, _, old, p := sprintCompletionFixture()
		p.err = f.NewFault(f.VersionConflict, f.NotStarted)
		w := newTestWriter()
		maximum := strings.Replace(good, `"expected_version":"2"`, `"expected_version":"9223372036854775807"`, 1)
		if serveTest(h, commandRequest("POST", sprintCompletePath(lookup), maximum), w) || w.Code != 409 || p.writes+p.lookups != 1 || old.calls != 0 {
			t.Fatal("HTTP rejected a canonical original version before the domain")
		}
	}
}

func TestWorkHTTPSprintCompleteBoundaryAndUnknown(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		for _, stage := range []string{"csrf", "session", "owner", "unknown", "unbound", "domain-state"} {
			h, boundary, old, p := sprintCompletionFixture()
			code, calls := 403, 0
			switch stage {
			case "csrf":
				boundary.check = func(*http.Request) error { return f.NewFault(f.CSRFFailed, f.NotStarted) }
			case "session":
				code = 401
				boundary.auth = func(*http.Request) error { return f.NewFault(f.Unauthenticated, f.NotStarted) }
			case "owner":
				code, calls = 404, 1
				p.err = f.NewFault(f.NotFound, f.NotStarted)
			case "unknown":
				code, calls = 503, 1
				p.err = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(fmt.Errorf("private-sprint-complete-canary"))
			case "unbound":
				code = 503
				h.sprintCompletion = nil
			case "domain-state":
				code, calls = 409, 1
				p.err = f.NewFault(f.InvalidState, f.NotStarted)
			}
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", sprintCompletePath(lookup), sprintCompleteBody(lookup)), w) || w.Code != code || p.writes+p.lookups != calls || old.calls != 0 {
				t.Fatal("Complete boundary failure changed", stage, lookup, w.Code)
			}
			if lookup && p.writes != 0 || !lookup && p.lookups != 0 || strings.Contains(w.Body.String(), "canary") {
				t.Fatal("failure triggered another call or exposed private cause")
			}
			if stage == "unknown" {
				v := wireObject(t, w.Body.Bytes())
				if v["commit_state"] != "unknown" || v["retry_hint"] != "lookup" {
					t.Fatal("unknown result changed")
				}
			}
		}
	}
}

func TestWorkHTTPSprintCompleteProjectionAndRoutes(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		for _, change := range []func(*c.SprintCompleteMutation){
			func(v *c.SprintCompleteMutation) { v.Sprint.ID = testID[pc.Sprint](90) },
			func(v *c.SprintCompleteMutation) { v.Sprint.ProjectID = testID[id.Project](90) },
			func(v *c.SprintCompleteMutation) { v.Sprint.Version = 4 },
			func(v *c.SprintCompleteMutation) { v.Sprint.CompletedBy.UserID = testID[id.User](90).String() },
			func(v *c.SprintCompleteMutation) { v.Project.OwnerUserID = testID[id.User](90) },
			func(v *c.SprintCompleteMutation) { v.Project.CurrentSprintID = ptr(v.Sprint.ID) },
			func(v *c.SprintCompleteMutation) { v.Project.Version = 1 },
			func(v *c.SprintCompleteMutation) { v.EventID = event.EventID{} },
		} {
			h, _, _, p := sprintCompletionFixture()
			change(&p.result)
			w := newTestWriter()
			aborted := serveTest(h, commandRequest("POST", sprintCompletePath(lookup), sprintCompleteBody(lookup)), w)
			if lookup && (aborted || w.Code != 503) || !lookup && (!aborted || w.Body.Len() != 0) {
				t.Fatal("bad Complete receipt became success or fictitious uncommitted Problem", lookup)
			}
		}
		h, _, old, p := sprintCompletionFixture()
		w := newTestWriter()
		r := commandRequest("GET", sprintCompletePath(lookup), "")
		if !HandlesPath(r.URL.Path) || serveTest(h, r, w) || w.Code != 405 || w.Header().Get("Allow") != "POST" || old.calls+p.writes+p.lookups != 0 {
			t.Fatal("Complete route method boundary")
		}
		if HandlesPath(r.URL.Path+"/extra") || HandlesPath(testPath("/sprint-lifecycle-commands/delete")) || HandlesPath(testPath("/sprints/"+testSprint().ID.String()+"/rollover")) {
			t.Fatal("Complete captured unrelated lifecycle routes")
		}
	}
	for _, value := range []c.SprintCompleteLookup{{State: c.LookupCommitted}, {State: c.LookupState("invalid")}, {State: c.LookupInProgress, Result: &c.SprintCompleteMutation{}}} {
		h, _, _, p := sprintCompletionFixture()
		p.lookupOverride = &value
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", sprintCompletePath(true), sprintCompleteBody(true)), w) || w.Code != 503 {
			t.Fatal("malformed Lookup union escaped")
		}
	}
}

func TestWorkHTTPSprintCompleteRefusalDetails(t *testing.T) {
	for _, test := range []struct {
		code   f.Code
		detail string
	}{{f.InvalidState, "UNFINISHED_TASKS"}, {f.ResourceBusy, "ACTIVE_EXECUTIONS"}, {f.ResourceBusy, "PENDING_DISPATCHES"}} {
		h, _, _, port := sprintCompletionFixture()
		rejection := f.NewFault(test.code, f.NotCommitted)
		rejection.FieldErrors = []f.FieldError{{Path: "/sprint_id", Code: test.detail}}
		port.err = rejection
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", sprintCompletePath(false), sprintCompleteBody(false)), w) || w.Code != 409 || port.writes != 1 || port.lookups != 0 {
			t.Fatal("refusal retried or changed status")
		}
		value := wireObject(t, w.Body.Bytes())
		fields, ok := value["field_errors"].([]any)
		if !ok || len(fields) != 1 || value["code"] != string(test.code) || value["commit_state"] != "not_committed" {
			t.Fatal("refusal lost closed problem fields")
		}
		field := fields[0].(map[string]any)
		if field["path"] != "/sprint_id" || field["code"] != test.detail {
			t.Fatal("refusal lost member/occupancy reason")
		}
	}
}
