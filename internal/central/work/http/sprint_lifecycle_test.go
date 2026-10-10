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
// gates. These controls do not prove real Owner/SQL/Start publication.
type sprintLifecyclePort struct {
	result          c.SprintStartMutation
	state           c.LookupState
	lookupOverride  *c.SprintStartLookup
	err             error
	writes, lookups int
	actor           id.Actor
	meta            f.CommandMeta
	project         c.ProjectID
	target          c.SprintID
	query           c.SprintStartLookupRequest
	deadline        time.Time
}

func (p *sprintLifecyclePort) StartSprint(ctx context.Context, actor id.Actor, meta f.CommandMeta, project c.ProjectID, sprint c.SprintID) (c.SprintStartMutation, error) {
	p.writes++
	p.actor, p.meta, p.project, p.target = actor, meta, project, sprint
	p.deadline, _ = ctx.Deadline()
	return p.result.Clone(), p.err
}
func (p *sprintLifecyclePort) LookupStartSprint(ctx context.Context, actor id.Actor, q c.SprintStartLookupRequest) (c.SprintStartLookup, error) {
	p.lookups++
	p.actor, p.query = actor, q
	p.deadline, _ = ctx.Deadline()
	if p.lookupOverride != nil {
		return *p.lookupOverride, p.err
	}
	v := c.SprintStartLookup{State: p.state}
	if p.state == c.LookupCommitted {
		r := p.result.Clone()
		v.Result = &r
	}
	return v, p.err
}
func sprintLifecycleFixture() (*handler, *testBoundary, *testPorts, *sprintLifecyclePort) {
	h, boundary, old := testHandler()
	s := testSprint()
	s.Version, s.State = 2, c.Current
	s.StartedAt = ptr(testAt())
	s.StartedBy = &c.ActorHistory{Kind: id.Human, UserID: testActor().Details().UserID}
	p := &sprintLifecyclePort{state: c.LookupCommitted, result: c.SprintStartMutation{
		Sprint: s, Project: pc.ProjectRef{ID: s.ProjectID, OwnerUserID: testID[id.User](1), Name: "Sprint-Start", NormalizedName: "sprint-start", Description: "", Lifecycle: pc.Active, Version: 2, CurrentSprintID: ptr(s.ID), CreatedAt: testAt(), UpdatedAt: testAt()},
		EventID: testID[event.EventIdentity](51),
	}}
	h.sprintLifecycle = p
	return h, boundary, old, p
}
func sprintStartBody(lookup bool) string {
	if lookup {
		return `{"command":"work.sprint.start","target_id":"` + testSprint().ID.String() + `","expected_version":"1","request":{}}`
	}
	return `{"expected_version":"1","request":{}}`
}
func sprintStartPath(lookup bool) string {
	if lookup {
		return "/sprint-lifecycle-commands/lookup"
	}
	return "/sprints/" + testSprint().ID.String() + "/start"
}

func TestWorkHTTPSprintStartAndOriginalLookup(t *testing.T) {
	h, _, old, p := sprintLifecycleFixture()
	w := newTestWriter()
	if serveTest(h, commandRequest("POST", sprintStartPath(false), sprintStartBody(false)), w) || w.Code != 200 || p.writes != 1 || p.lookups != 0 || old.calls != 0 {
		t.Fatal("Start did not call the original service exactly once", w.Code)
	}
	if !p.actor.Equal(testActor()) || p.project != testSprint().ProjectID || p.target != testSprint().ID || p.meta.RequestID.Validate() != nil || p.meta.ExpectedVersion == nil || *p.meta.ExpectedVersion != 1 || p.meta.IdempotencyKey != "saved-original-intent" {
		t.Fatal("Start changed the original actor, target or metadata")
	}
	if remaining := time.Until(p.deadline); remaining <= 0 || remaining > mutationBudget {
		t.Fatal("Start budget changed")
	}
	w.cleared(t)
	original := append([]byte(nil), w.Body.Bytes()...)
	want, err := c.StartSprintDigest(p.actor, p.meta, p.project, p.target)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []c.LookupState{c.LookupCommitted, c.LookupInProgress, c.LookupNotObserved} {
		h, _, old, p := sprintLifecycleFixture()
		p.state = state
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", sprintStartPath(true), sprintStartBody(true)), w) || w.Code != 200 || p.writes != 0 || p.lookups != 1 || old.calls != 0 {
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
			var decoded sprintStartLookupWire
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

func TestWorkHTTPSprintStartStrictIntent(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		good := sprintStartBody(lookup)
		bad := []string{`{}`, good + `{}`,
			strings.Replace(good, `"expected_version":"1"`, `"expected_version":1`, 1),
			strings.Replace(good, `"expected_version":"1"`, `"expected_version":null`, 1),
			strings.Replace(good, `"expected_version":"1"`, `"expected_version":"01"`, 1),
			strings.Replace(good, `"expected_version":"1"`, `"expected_version":"1","expected_version":"1"`, 1),
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
			bad = append(bad, strings.Replace(good, "work.sprint.start", "work.sprint.complete", 1), strings.Replace(good, `"target_id"`, `"sprint_id"`, 1), strings.Replace(good, testSprint().ID.String(), "invalid", 1))
		} else {
			bad = append(bad, strings.Replace(good, `"request":{}`, `"command":"work.sprint.start","request":{}`, 1))
		}
		for _, body := range bad {
			h, _, old, p := sprintLifecycleFixture()
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", sprintStartPath(lookup), body), w) || w.Code != 400 || p.writes+p.lookups+old.calls != 0 {
				t.Fatal("malformed Start intent reached a provider", lookup, w.Code)
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
			h, _, old, p := sprintLifecycleFixture()
			r := commandRequest("POST", sprintStartPath(lookup), good)
			edit(r)
			w := newTestWriter()
			if serveTest(h, r, w) || (w.Code != 400 && w.Code != 415) || p.writes+p.lookups+old.calls != 0 {
				t.Fatal("invalid transport reached a provider", w.Code)
			}
		}
		h, _, old, p := sprintLifecycleFixture()
		p.err = f.NewFault(f.VersionConflict, f.NotStarted)
		w := newTestWriter()
		maximum := strings.Replace(good, `"expected_version":"1"`, `"expected_version":"9223372036854775807"`, 1)
		if serveTest(h, commandRequest("POST", sprintStartPath(lookup), maximum), w) || w.Code != 409 || p.writes+p.lookups != 1 || old.calls != 0 {
			t.Fatal("HTTP rejected a canonical original version before the domain")
		}
	}
}

func TestWorkHTTPSprintStartBoundaryAndUnknown(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		for _, stage := range []string{"csrf", "session", "owner", "unknown", "unbound", "domain-state"} {
			h, boundary, old, p := sprintLifecycleFixture()
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
				p.err = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(fmt.Errorf("private-sprint-start-canary"))
			case "unbound":
				code = 503
				h.sprintLifecycle = nil
			case "domain-state":
				code, calls = 409, 1
				p.err = f.NewFault(f.InvalidState, f.NotStarted)
			}
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", sprintStartPath(lookup), sprintStartBody(lookup)), w) || w.Code != code || p.writes+p.lookups != calls || old.calls != 0 {
				t.Fatal("Start boundary failure changed", stage, lookup, w.Code)
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

func TestWorkHTTPSprintStartProjectionAndRoutes(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		for _, change := range []func(*c.SprintStartMutation){
			func(v *c.SprintStartMutation) { v.Sprint.ID = testID[pc.Sprint](90) },
			func(v *c.SprintStartMutation) { v.Sprint.ProjectID = testID[id.Project](90) },
			func(v *c.SprintStartMutation) { v.Sprint.Version = 3 },
			func(v *c.SprintStartMutation) { v.Sprint.StartedBy.UserID = testID[id.User](90).String() },
			func(v *c.SprintStartMutation) { v.Project.OwnerUserID = testID[id.User](90) },
			func(v *c.SprintStartMutation) { v.Project.CurrentSprintID = nil },
			func(v *c.SprintStartMutation) { v.Project.Version = 1 },
			func(v *c.SprintStartMutation) { v.EventID = event.EventID{} },
		} {
			h, _, _, p := sprintLifecycleFixture()
			change(&p.result)
			w := newTestWriter()
			aborted := serveTest(h, commandRequest("POST", sprintStartPath(lookup), sprintStartBody(lookup)), w)
			if lookup && (aborted || w.Code != 503) || !lookup && (!aborted || w.Body.Len() != 0) {
				t.Fatal("bad Start receipt became success or fictitious uncommitted Problem", lookup)
			}
		}
		h, _, old, p := sprintLifecycleFixture()
		w := newTestWriter()
		r := commandRequest("GET", sprintStartPath(lookup), "")
		if !HandlesPath(r.URL.Path) || serveTest(h, r, w) || w.Code != 405 || w.Header().Get("Allow") != "POST" || old.calls+p.writes+p.lookups != 0 {
			t.Fatal("Start route method boundary")
		}
		if HandlesPath(r.URL.Path+"/extra") || HandlesPath(testPath("/sprint-lifecycle-commands/delete")) || HandlesPath(testPath("/sprints/"+testSprint().ID.String()+"/complete")) {
			t.Fatal("Start captured unrelated lifecycle routes")
		}
	}
	for _, value := range []c.SprintStartLookup{{State: c.LookupCommitted}, {State: c.LookupState("invalid")}, {State: c.LookupInProgress, Result: &c.SprintStartMutation{}}} {
		h, _, _, p := sprintLifecycleFixture()
		p.lookupOverride = &value
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", sprintStartPath(true), sprintStartBody(true)), w) || w.Code != 503 {
			t.Fatal("malformed Lookup union escaped")
		}
	}
}
