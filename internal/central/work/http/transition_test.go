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
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These controlled ports exercise transport and original-intent correlation,
// not SQL persistence or real current Owner/CSRF authorization.
type transitionPort struct {
	result          c.TaskTransitionMutation
	status          c.LookupState
	err             error
	writes, lookups int
	actor           id.Actor
	meta            f.CommandMeta
	project         c.ProjectID
	target          c.TaskID
	request         c.TaskTransfer
	query           c.TaskTransitionLookupRequest
	deadline        time.Time
}

func (p *transitionPort) TransferTask(ctx context.Context, actor id.Actor, meta f.CommandMeta, project c.ProjectID, task c.TaskID, request c.TaskTransfer) (c.TaskTransitionMutation, error) {
	p.writes++
	p.actor, p.meta, p.project, p.target, p.request = actor, meta, project, task, request.Clone()
	p.deadline, _ = ctx.Deadline()
	return p.result.Clone(), p.err
}
func (p *transitionPort) LookupTaskTransition(ctx context.Context, actor id.Actor, query c.TaskTransitionLookupRequest) (c.TaskTransitionLookup, error) {
	p.lookups++
	p.actor, p.query = actor, query
	p.deadline, _ = ctx.Deadline()
	v := c.TaskTransitionLookup{Status: p.status}
	if p.status == c.LookupCommitted {
		r := p.result.Clone()
		v.Receipt = &r
	}
	return v, p.err
}
func transitionFixture() (*handler, *testBoundary, *testPorts, *transitionPort) {
	h, boundary, old := testHandler()
	task := testTask()
	task.Version, task.State, task.AssigneeAgentID = 2, c.TaskStateTodo, ptr(testID[id.Agent](30))
	p := &transitionPort{status: c.LookupCommitted, result: c.TaskTransitionMutation{
		Task: task, TaskEventIDs: []c.TaskEventID{testID[c.TaskEvent](40), testID[c.TaskEvent](41)},
		EventIDs: []event.EventID{testID[event.EventIdentity](42)},
	}}
	h.transitions = p
	return h, boundary, old, p
}
func transitionBody(t *testing.T, lookup bool) string {
	t.Helper()
	v := map[string]any{"expected_version": "1", "request": map[string]any{
		"target_state": "todo", "assignee_agent_id": testID[id.Agent](30),
	}}
	if lookup {
		v["command"], v["target_id"] = "work.task.transfer", testTask().ID
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func transitionPath(lookup bool) string {
	if lookup {
		return "/task-transition-commands/lookup"
	}
	return "/tasks/" + testTask().ID.String() + "/transfer"
}

func reviewTransitionRequests() []c.TaskTransfer {
	comment := "  Review: 保留原文\r\n\tReady.  "
	return []c.TaskTransfer{
		{TargetState: c.TaskStateInReview, AssigneeAgentID: ptr(testID[id.Agent](31)), Comment: &comment},
		{TargetState: c.TaskStateInReview, AssigneeAgentID: ptr(testID[id.Agent](30)), Comment: &comment},
		{TargetState: c.TaskStateDone, Comment: &comment},
		{TargetState: c.TaskStateDone, AssigneeAgentID: ptr(testID[id.Agent](31)), Comment: &comment},
		{TargetState: c.TaskStateTodo, AssigneeAgentID: ptr(testID[id.Agent](32)), Comment: &comment},
	}
}

func reviewTransitionBody(t *testing.T, request c.TaskTransfer, lookup bool) string {
	t.Helper()
	v := map[string]any{"expected_version": "1", "request": request}
	if lookup {
		v["command"], v["target_id"] = "work.task.transfer", testTask().ID
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestWorkHTTPReviewTransitionsAndOriginalLookup(t *testing.T) {
	// The domain port supplies controlled receipts. This verifies the existing
	// transport; current state, reviewer eligibility and required presence are
	// checked by the real Work transition service in its transaction tests.
	for n, request := range reviewTransitionRequests() {
		h, _, old, p := transitionFixture()
		p.result.Task.State = request.TargetState
		if request.AssigneeAgentID != nil {
			p.result.Task.AssigneeAgentID = ptr(*request.AssigneeAgentID)
		}
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", transitionPath(false), reviewTransitionBody(t, request, false)), w) || w.Code != 200 || p.writes != 1 || p.lookups != 0 || old.calls != 0 {
			t.Fatal("review intent did not reach the original transition once", n, w.Code)
		}
		if p.request.TargetState != request.TargetState || p.request.Comment == nil || *p.request.Comment != *request.Comment || !sameAgent(p.request.AssigneeAgentID, request.AssigneeAgentID) {
			t.Fatal("review transport changed assignee presence or comment bytes", n)
		}
		original := append([]byte(nil), w.Body.Bytes()...)
		digest, err := c.TaskTransferDigest(p.actor, p.meta, p.project, p.target, request)
		if err != nil {
			t.Fatal(err)
		}
		w = newTestWriter()
		if serveTest(h, commandRequest("POST", transitionPath(true), reviewTransitionBody(t, request, true)), w) || w.Code != 200 || p.writes != 1 || p.lookups != 1 || old.calls != 0 || p.query.SemanticDigest != digest || p.query.IdempotencyKey != p.meta.IdempotencyKey {
			t.Fatal("review Lookup lost the original intent or invoked another operation", n, w.Code)
		}
		found, err := c.DecodeTaskTransitionLookup(w.Body.Bytes())
		if err != nil || found.Status != c.LookupCommitted || found.Receipt == nil {
			t.Fatal("review Lookup lost the original committed receipt", n, err)
		}
		raw, err := json.Marshal(found.Receipt)
		if err != nil || !bytes.Equal(raw, original) {
			t.Fatal("review Lookup changed historical Task or event identities", n, err)
		}
		if request.AssigneeAgentID != nil {
			p.result.Task.AssigneeAgentID = ptr(testID[id.Agent](99))
			for _, lookup := range []bool{false, true} {
				w = newTestWriter()
				aborted := serveTest(h, commandRequest("POST", transitionPath(lookup), reviewTransitionBody(t, request, lookup)), w)
				if lookup && (aborted || w.Code != 503) || !lookup && (!aborted || w.Body.Len() != 0) {
					t.Fatal("review response published a different explicitly requested assignee", n, lookup)
				}
			}
		}
	}
	for _, lookup := range []bool{false, true} {
		for _, field := range []string{"assignee_agent_id", "comment", "reviewer_agent_id"} {
			body := wireObject(t, []byte(reviewTransitionBody(t, reviewTransitionRequests()[0], lookup)))
			var value any
			if field == "reviewer_agent_id" {
				value = testID[id.Agent](31).String()
			}
			body["request"].(map[string]any)[field] = value
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			h, _, old, p := transitionFixture()
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", transitionPath(lookup), string(raw)), w) || w.Code != 400 || p.writes+p.lookups+old.calls != 0 {
				t.Fatal("review transport admitted explicit null or a second reviewer field", field, lookup, w.Code)
			}
		}
		h, _, old, p := transitionFixture()
		request := reviewTransitionRequests()[0]
		request.Comment = nil
		p.err = f.NewFault(f.CommentRequired, f.NotStarted).WithCause(fmt.Errorf("private-review-comment-canary"))
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", transitionPath(lookup), reviewTransitionBody(t, request, lookup)), w) || w.Code != 409 || p.writes+p.lookups != 1 || old.calls != 0 || strings.Contains(w.Body.String(), "canary") {
			t.Fatal("required-comment domain rejection lost its safe transport boundary", lookup, w.Code)
		}
		problem := wireObject(t, w.Body.Bytes())
		if problem["code"] != string(f.CommentRequired) || problem["commit_state"] != "not_started" || lookup && p.writes != 0 || !lookup && p.lookups != 0 {
			t.Fatal("required-comment rejection retried or changed the original result", lookup)
		}
	}
}

func TestWorkHTTPTaskTransitionAndOriginalLookup(t *testing.T) {
	h, _, old, p := transitionFixture()
	w := newTestWriter()
	if serveTest(h, commandRequest("POST", transitionPath(false), transitionBody(t, false)), w) || w.Code != 200 || p.writes != 1 || p.lookups != 0 || old.calls != 0 {
		t.Fatal("transfer did not call only the original domain method", w.Code)
	}
	if !p.actor.Equal(testActor()) || p.project != testTask().ProjectID || p.target != testTask().ID || p.meta.RequestID.Validate() != nil || p.meta.ExpectedVersion == nil || *p.meta.ExpectedVersion != 1 || p.meta.IdempotencyKey != "saved-original-intent" {
		t.Fatal("mutation lost actor, target or original metadata")
	}
	if remaining := time.Until(p.deadline); remaining <= 0 || remaining > mutationBudget {
		t.Fatal("mutation deadline changed")
	}
	originalReceipt := append([]byte(nil), w.Body.Bytes()...)
	want, err := c.TaskTransferDigest(p.actor, p.meta, p.project, p.target, p.request)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []c.LookupState{c.LookupCommitted, c.LookupInProgress, c.LookupNotObserved} {
		h, _, old, p := transitionFixture()
		p.status = state
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", transitionPath(true), transitionBody(t, true)), w) || w.Code != 200 || p.writes != 0 || p.lookups != 1 || old.calls != 0 {
			t.Fatal("Lookup wrote or consulted current GET", state, w.Code)
		}
		if p.query.Command != c.TaskTransitionTransfer || p.query.SemanticDigest != want || p.query.IdempotencyKey != "saved-original-intent" || p.query.ProjectID != testTask().ProjectID || !p.actor.Equal(testActor()) {
			t.Fatal("Lookup changed saved original intent")
		}
		if remaining := time.Until(p.deadline); remaining <= 0 || remaining > readBudget {
			t.Fatal("Lookup budget changed")
		}
		got, err := c.DecodeTaskTransitionLookup(w.Body.Bytes())
		if err != nil || got.Status != state || (got.Receipt != nil) != (state == c.LookupCommitted) {
			t.Fatal("Lookup union changed", err)
		}
		if got.Receipt != nil && got.Receipt.Task.Version != 2 {
			t.Fatal("Lookup reconstructed current version")
		}
		if got.Receipt != nil {
			raw, err := json.Marshal(got.Receipt)
			if err != nil || !bytes.Equal(raw, originalReceipt) {
				t.Fatal("Lookup changed the historical receipt or event IDs")
			}
		}
		w.cleared(t)
	}
}

func TestWorkHTTPTaskTransitionStrictIntent(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		good := transitionBody(t, lookup)
		bad := []string{
			`{}`, good + `{}`, strings.Replace(good, `"expected_version":"1"`, `"expected_version":1`, 1),
			strings.Replace(good, `"expected_version":"1"`, `"expected_version":"01"`, 1),
			strings.Replace(good, `"expected_version":"1"`, `"expected_version":null`, 1),
			strings.Replace(good, `"expected_version":"1"`, `"expected_version":"1","expected_version":"1"`, 1),
			strings.Replace(good, `"request":{`, `"request":{"actor":"human",`, 1),
			strings.Replace(good, `"request":{`, `"request":{"comment":"\ud800",`, 1),
			strings.Replace(good, `"request":{`, `"request":{"add_blockers":null,`, 1),
			strings.Replace(good, `"target_state":"todo"`, `"Target_state":"todo"`, 1),
			strings.Replace(good, `"target_state":"todo"`, `"target_state":"todo","target_state":"todo"`, 1),
			strings.Replace(good, `"target_state":"todo"`, `"target_state":null`, 1),
			strings.Replace(good, `"request":`, `"semantic_digest":"caller-digest","request":`, 1),
		}
		if lookup {
			bad = append(bad, strings.Replace(good, "work.task.transfer", "work.task.update", 1), strings.Replace(good, `"target_id":`, `"Task_id":`, 1))
		} else {
			bad = append(bad, strings.Replace(good, `"request":`, `"command":"work.task.transfer","request":`, 1))
		}
		for _, body := range bad {
			h, _, old, p := transitionFixture()
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", transitionPath(lookup), body), w) || w.Code != 400 || p.writes+p.lookups+old.calls != 0 {
				t.Fatal("malformed intent reached a provider", lookup, w.Code)
			}
		}
		for _, edit := range []func(*http.Request){
			func(r *http.Request) { r.Header.Del("Idempotency-Key") },
			func(r *http.Request) { r.Header.Add("Idempotency-Key", "second") },
			func(r *http.Request) { r.URL.ForceQuery = true },
			func(r *http.Request) { r.Header["Content-Encoding"] = []string{""} },
			func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		} {
			h, _, old, p := transitionFixture()
			r := commandRequest("POST", transitionPath(lookup), good)
			edit(r)
			w := newTestWriter()
			if serveTest(h, r, w) || (w.Code != 400 && w.Code != 415) || p.writes+p.lookups+old.calls != 0 {
				t.Fatal("invalid transport reached a provider", w.Code)
			}
		}
	}
}

func TestWorkHTTPTaskTransitionBoundaryAndUnknown(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		for _, stage := range []string{"csrf", "session", "owner", "unknown", "unbound", "scheduler-edge"} {
			h, boundary, old, p := transitionFixture()
			code, calls := 403, 0
			switch stage {
			case "csrf":
				boundary.check = func(*http.Request) error { return f.NewFault(f.CSRFFailed, f.NotStarted) }
			case "session":
				code = 401
				boundary.auth = func(*http.Request) error { return f.NewFault(f.Unauthenticated, f.NotStarted) }
			case "owner", "scheduler-edge":
				calls = 1
				p.err = f.NewFault(f.Forbidden, f.NotStarted)
			case "unknown":
				code, calls = 503, 1
				p.err = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(fmt.Errorf("private-transition-canary"))
			case "unbound":
				code = 503
				h.transitions = nil
			}
			body := transitionBody(t, lookup)
			if stage == "scheduler-edge" {
				body = strings.Replace(body, `"todo"`, `"in_progress"`, 1)
			}
			w := newTestWriter()
			if serveTest(h, commandRequest("POST", transitionPath(lookup), body), w) || w.Code != code || p.writes+p.lookups != calls || old.calls != 0 {
				t.Fatal("authorization/domain failure changed", stage, lookup, w.Code)
			}
			if lookup && p.writes != 0 || !lookup && p.lookups != 0 || strings.Contains(w.Body.String(), "canary") {
				t.Fatal("error triggered a second operation or exposed private cause")
			}
			if stage == "unknown" {
				v := wireObject(t, w.Body.Bytes())
				if v["commit_state"] != "unknown" || v["retry_hint"] != "lookup" {
					t.Fatal("unknown was replaced by a terminal result")
				}
			}
		}
	}
}

func TestWorkHTTPTaskTransitionProjectionAndRoutes(t *testing.T) {
	for _, lookup := range []bool{false, true} {
		for _, change := range []func(*c.TaskTransitionMutation){
			func(v *c.TaskTransitionMutation) { v.Task.ProjectID = testID[id.Project](90) },
			func(v *c.TaskTransitionMutation) { v.Task.ID = testID[c.Task](90) },
			func(v *c.TaskTransitionMutation) { v.Task.Version = 3 },
			func(v *c.TaskTransitionMutation) { v.Task.State = c.TaskStateBlocked },
			func(v *c.TaskTransitionMutation) { v.Task.AssigneeAgentID = ptr(testID[id.Agent](91)) },
			func(v *c.TaskTransitionMutation) { v.TaskEventIDs[1] = v.TaskEventIDs[0] },
			func(v *c.TaskTransitionMutation) { v.EventIDs = nil },
		} {
			h, _, _, p := transitionFixture()
			change(&p.result)
			w := newTestWriter()
			aborted := serveTest(h, commandRequest("POST", transitionPath(lookup), transitionBody(t, lookup)), w)
			if lookup && (aborted || w.Code != 503) || !lookup && (!aborted || w.Body.Len() != 0) {
				t.Fatal("bad receipt was published or committed mutation fabricated a Problem", lookup)
			}
		}
		h, _, old, p := transitionFixture()
		w := newTestWriter()
		r := commandRequest("GET", transitionPath(lookup), "")
		if !HandlesPath(r.URL.Path) || serveTest(h, r, w) || w.Code != 405 || w.Header().Get("Allow") != "POST" || old.calls+p.writes+p.lookups != 0 {
			t.Fatal("transition route method boundary")
		}
		if HandlesPath(r.URL.Path+"/extra") || HandlesPath(testPath("/task-transition-commands/delete")) {
			t.Fatal("transition captured an unrelated route")
		}
	}
}
