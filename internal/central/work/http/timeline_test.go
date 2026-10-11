package workhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The private port exercises HTTP projection and ownership of its call. These
// typed records do not claim to prove a database writer or current Owner grant.
type timelineHTTPPort struct {
	page   f.Page[c.TaskTimelineEvent]
	err    error
	calls  int
	filter c.TaskTimelineFilter
	query  f.PageRequest
	before func(context.Context)
}

func (p *timelineHTTPPort) ListTaskEvents(ctx context.Context, actor id.Actor, project c.ProjectID, task c.TaskID, filter c.TaskTimelineFilter, page f.PageRequest) (f.Page[c.TaskTimelineEvent], error) {
	p.calls++
	if actor.Details() != testActor().Details() || project != testTask().ProjectID || task != testTask().ID {
		return f.Page[c.TaskTimelineEvent]{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	p.filter, p.query = filter.Clone(), page
	if p.before != nil {
		p.before(ctx)
	}
	return p.page, p.err
}

func timelineHTTPFixture(items []c.TaskTimelineEvent) (*handler, *timelineHTTPPort) {
	h, _, _ := testHandler()
	p := &timelineHTTPPort{page: f.Page[c.TaskTimelineEvent]{Items: items}}
	h.timelineReader = p
	return h, p
}

func timelineHTTPPath() string { return testPath("/tasks/" + testTask().ID.String() + "/events") }

func timelineHTTPJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal("invalid controlled event payload", err)
	}
	return raw
}

func timelineHTTPEvents(t *testing.T) []c.TaskTimelineEvent {
	t.Helper()
	v := testTask()
	human := c.TaskEventActor{Type: id.Human, UserID: testID[id.User](1), Source: "task_domain"}
	command := testID[c.TaskCommand](80)
	created := c.TaskEvent{ID: testID[c.TaskEvent](100), ProjectID: v.ProjectID, TaskID: v.ID, TaskVersion: 1, Type: c.TaskEventCreated, Actor: human, OperationID: command, CorrelationID: command, CreatedAt: testAt(), Payload: timelineHTTPJSON(t, c.TaskCreatedPayload{InitialState: c.TaskStateBacklog, MilestoneID: v.MilestoneID, SprintID: v.SprintID, Type: v.Type, Priority: v.Priority})}
	updated := created.Clone()
	updated.ID, updated.TaskVersion, updated.Type = testID[c.TaskEvent](101), 2, c.TaskEventFieldsUpdated
	updated.Payload = timelineHTTPJSON(t, c.TaskFieldsUpdatedPayload{ChangedFields: []c.TaskChangedField{c.TaskTitleChanged}})
	bop := testID[c.TaskBlockerCommand](81)
	blocker := c.TaskBlockerEvent{ID: testID[c.TaskEvent](102), ProjectID: v.ProjectID, TaskID: v.ID, TaskVersion: 3, Type: c.TaskBlockerEventAdded, Actor: human, OperationID: bop, CorrelationID: bop, CreatedAt: testAt(), Payload: c.TaskBlockerEventPayload{Added: &c.TaskBlockerAddedPayload{BlockerID: testBlocker().ID, BlockerType: c.TaskBlockerWaitingForHuman}}}
	resolved := blocker.Clone()
	resolved.ID, resolved.TaskVersion, resolved.Type = testID[c.TaskEvent](103), 4, c.TaskBlockerEventResolved
	resolved.Payload = c.TaskBlockerEventPayload{Resolved: &c.TaskBlockerResolvedPayload{BlockerID: testBlocker().ID, BlockerType: c.TaskBlockerWaitingForHuman}}
	top := testID[c.TaskTransitionCommand](82)
	transition := c.TaskTransitionEvent{ID: testID[c.TaskEvent](104), ProjectID: v.ProjectID, TaskID: v.ID, TaskVersion: 5, Type: c.TaskTransitionStateChanged, Actor: c.TaskTransitionActor{UserID: testID[id.User](1)}, OperationID: top, CorrelationID: top, CreatedAt: testAt(), Payload: c.TaskTransitionFactPayload{StateChanged: &c.TaskStateChangedPayload{FromState: c.TaskStateInProgress, ToState: c.TaskStateInReview}}}
	assignee := transition.Clone()
	assignee.ID, assignee.Type = testID[c.TaskEvent](105), c.TaskTransitionAssigneeChanged
	assignee.Payload = c.TaskTransitionFactPayload{AssigneeChanged: &c.TaskAssigneeChangedPayload{ToAgentID: testID[id.Agent](83)}}
	comment := transition.Clone()
	comment.ID, comment.Type = testID[c.TaskEvent](106), c.TaskTransitionComment
	comment.Payload = c.TaskTransitionFactPayload{Comment: &c.TaskCommentPayload{Body: "review comment canary"}}
	claimID := testID[c.SchedulerClaim](84)
	system := c.SchedulerTaskActor{CauseID: claimID.String()}
	claim := c.SchedulerTaskEvent{ID: testID[c.TaskEvent](107), ProjectID: v.ProjectID, TaskID: v.ID, TaskVersion: 6, Type: c.TaskTransitionStateChanged, Actor: system, OperationID: claimID, CorrelationID: claimID, CreatedAt: testAt(), Payload: c.SchedulerStateChanged{FromState: c.TaskStateTodo, ToState: c.TaskStateInProgress, ReasonCode: "scheduler_claim"}}
	busy := c.TaskBusyTaskEvent{ID: testID[c.TaskEvent](108), ProjectID: v.ProjectID, TaskID: v.ID, TaskVersion: 7, Type: c.TaskTransitionStateChanged, Actor: system, OperationID: claimID, CorrelationID: claimID, CreatedAt: testAt(), Payload: c.TaskBusyStateChanged{FromState: c.TaskStateInProgress, ToState: c.TaskStateTodo, ReasonCode: "scheduler_agent_busy_compensation"}}
	failure := c.TaskFailureTaskEvent{ID: testID[c.TaskEvent](109), ProjectID: v.ProjectID, TaskID: v.ID, TaskVersion: 8, Type: "blocker_added", Actor: system, OperationID: claimID, CorrelationID: claimID, CreatedAt: testAt(), Blocker: &c.TaskFailureBlockerAdded{BlockerID: testBlocker().ID, BlockerType: c.TaskBlockerTechnical, ReasonCode: c.TaskLaunchFailureHistoryReason}}
	failedState := failure
	failedState.ID, failedState.Type, failedState.Blocker = testID[c.TaskEvent](110), "state_changed", nil
	failedState.State = &c.TaskFailureStateChanged{FromState: c.TaskStateInReview, ToState: c.TaskStateBlocked, ReasonCode: c.TaskLaunchFailureHistoryReason}
	items := []c.TaskTimelineEvent{{Planning: &created}, {Planning: &updated}, {Blocker: &blocker}, {Blocker: &resolved}, {Transition: &transition}, {Transition: &assignee}, {Transition: &comment}, {Claim: &claim}, {Compensation: &busy}, {Failure: &failure}, {Failure: &failedState}}
	for _, item := range items {
		if err := item.Validate(); err != nil {
			t.Fatal("invalid controlled history", err)
		}
	}
	return items
}

func TestWorkHTTPTimelineReadsAndQueries(t *testing.T) {
	items := timelineHTTPEvents(t)
	for _, order := range []string{"asc", "desc"} {
		ordered := slices.Clone(items)
		if order == "desc" {
			slices.Reverse(ordered)
		}
		h, p := timelineHTTPFixture(ordered)
		var get []byte
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := newTestWriter()
			if serveTest(h, httptest.NewRequest(method, timelineHTTPPath()+"?order="+order, nil), w) || w.Code != 200 {
				t.Fatal("timeline read rejected", method, order, w.Code)
			}
			if method == http.MethodGet {
				get = bytes.Clone(w.Body.Bytes())
				want := timelineHTTPJSON(t, p.page)
				if !bytes.Equal(get, want) {
					t.Fatal("history was rewritten by transport")
				}
			} else if w.Body.Len() != 0 {
				t.Fatal("HEAD wrote history body")
			}
			if w.Header().Get("Content-Length") != strconv.Itoa(len(get)) || w.Header().Get("Cache-Control") != "no-store" || p.query.Limit != 50 || p.filter.Order != c.TaskTimelineOrder(order) || p.filter.Types != nil {
				t.Fatal("read headers or default query changed")
			}
			w.cleared(t)
		}
		if p.calls != 2 || !HandlesPath(timelineHTTPPath()) || resourceRoute(timelineHTTPPath()).pattern() != projectPrefix+"{project_id}/tasks/{task_id}/events" {
			t.Fatal("HEAD did not call original reader or route identity changed")
		}
	}
	h, p := timelineHTTPFixture([]c.TaskTimelineEvent{items[6]})
	p.page.NextCursor = "signed-next"
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath()+"?types=comment,state_changed&limit=200&cursor=original", nil), w) || w.Code != 200 || p.query.Cursor != "original" || p.filter.Order != c.TaskTimelineDescending || !slices.Equal(p.filter.Types, []c.TaskTimelineEventType{c.TaskTimelineComment, c.TaskTimelineStateChanged}) {
		t.Fatal("legal byte-short page or exact query rejected")
	}
	if validPage(p.query, len(p.page.Items), p.page.NextCursor) {
		t.Fatal("old planning page rule was relaxed")
	}
	for _, bad := range []string{"types=", "types=comment,", "types=comment,,state_changed", "types=comment,comment", "types=unknown", "types=comment&types=state_changed", "order=", "order=ASC", "order=asc&order=desc", "state=todo", "limit=01", "limit=201", "limit=0", "cursor=", "types=comment;order=asc"} {
		before := p.calls
		w = newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath()+"?"+bad, nil), w) || w.Code != 400 || p.calls != before {
			t.Fatal("invalid query reached reader", bad, w.Code)
		}
	}
	w = newTestWriter()
	before := p.calls
	if serveTest(h, httptest.NewRequest("POST", timelineHTTPPath(), nil), w) || w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || p.calls != before {
		t.Fatal("Timeline accepted a write")
	}
	p.page = f.Page[c.TaskTimelineEvent]{Items: []c.TaskTimelineEvent{}}
	w = newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath(), nil), w) || w.Code != 200 || w.Body.String() != `{"items":[]}` {
		t.Fatal("empty timeline is not an explicit empty page")
	}
}

func TestWorkHTTPTimelineRejectsInvalidPages(t *testing.T) {
	base := timelineHTTPEvents(t)[6]
	for _, corrupt := range []func(*timelineHTTPPort){
		func(p *timelineHTTPPort) { p.page.Items = nil },
		func(p *timelineHTTPPort) { p.page.Items = []c.TaskTimelineEvent{}; p.page.NextCursor = "next" },
		func(p *timelineHTTPPort) { p.page.NextCursor = strings.Repeat("x", maxCursorBytes+1) },
		func(p *timelineHTTPPort) { p.page.NextCursor = "bad\x00cursor" },
		func(p *timelineHTTPPort) { p.page.Items[0].Transition.ProjectID = testID[id.Project](400) },
		func(p *timelineHTTPPort) { p.page.Items[0].Transition.TaskID = testID[c.Task](400) },
		func(p *timelineHTTPPort) { p.page.Items = append(p.page.Items, p.page.Items[0].Clone()) },
		func(p *timelineHTTPPort) {
			v := p.page.Items[0].Clone()
			v.Transition.ID = testID[c.TaskEvent](500)
			p.page.Items = append(p.page.Items, v)
		},
		func(p *timelineHTTPPort) { p.page.Items[0].Transition.Payload.Comment.Body = "" },
		func(p *timelineHTTPPort) { p.page.Items[0].Planning = timelineHTTPEvents(t)[0].Planning },
	} {
		h, p := timelineHTTPFixture([]c.TaskTimelineEvent{base.Clone()})
		corrupt(p)
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath(), nil), w) || w.Code != 503 || strings.Contains(w.Body.String(), "review comment canary") {
			t.Fatal("invalid source published a partial timeline", w.Code)
		}
	}
	h, _ := timelineHTTPFixture([]c.TaskTimelineEvent{base.Clone()})
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath()+"?types=state_changed", nil), w) || w.Code != 503 {
		t.Fatal("reader result escaped requested type filter")
	}
	// Escaping is counted in encoded bytes. A complete comment may exceed the
	// older 16 KiB family cap; an oversized page must never be silently cut.
	large := make([]c.TaskTimelineEvent, 22)
	for n := range large {
		v := base.Clone()
		v.Transition.ID = testID[c.TaskEvent](700 - n)
		v.Transition.Payload.Comment.Body = strings.Repeat("<", c.MaxTaskTransitionCommentBytes)
		large[n] = v
	}
	h, p := timelineHTTPFixture(large[:20])
	p.page.NextCursor = "original-byte-page-token"
	w = newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath(), nil), w) || w.Code != 200 || w.Body.Len() <= 16<<10 || w.Body.Len() > c.MaxTaskTimelineResponseBytes {
		t.Fatal("complete large-comment short page rejected", w.Code)
	}
	p.page.Items = large
	w = newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath(), nil), w) || w.Code != 503 || strings.Contains(w.Body.String(), "original-byte-page-token") {
		t.Fatal("oversized source was truncated or exposed")
	}
}

func TestWorkHTTPTimelineCurrentErrorsAndCancellation(t *testing.T) {
	h, p := timelineHTTPFixture(timelineHTTPEvents(t)[:1])
	for _, failure := range []struct {
		code   f.Code
		status int
	}{{f.NotFound, 404}, {f.ProjectNotActive, 409}, {f.CursorInvalid, 400}, {f.CursorStale, 409}, {f.InternalError, 500}} {
		p.err = f.NewFault(failure.code, f.NotStarted)
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath(), nil), w) || w.Code != failure.status || !strings.Contains(w.Body.String(), string(failure.code)) {
			t.Fatal("original read error changed", failure.code, w.Code)
		}
	}
	p.err = nil
	boundary := h.boundary.(*testBoundary)
	boundary.auth = func(*http.Request) error { return f.NewFault(f.SessionRevoked, f.NotStarted) }
	before := p.calls
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", timelineHTTPPath(), nil), w) || w.Code != 401 || p.calls != before {
		t.Fatal("revoked session reached history reader")
	}
	boundary.auth = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := false
	p.before = func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > readBudget {
			t.Fatal("read budget did not cover domain call")
		}
		cancel()
		returned = true
	}
	body := &testBody{Reader: strings.NewReader("")}
	r := httptest.NewRequest("GET", timelineHTTPPath(), nil).WithContext(ctx)
	r.Body = body
	w = newTestWriter()
	if !serveTest(h, r, w) || !returned || body.closes.Load() != 1 || w.Body.Len() != 0 {
		t.Fatal("canceled original call published or failed to close body")
	}
	// GET must retain the existing empty-body rule.
	p.before = nil
	w = newTestWriter()
	r = httptest.NewRequest("GET", timelineHTTPPath(), strings.NewReader("x"))
	before = p.calls
	if serveTest(h, r, w) || w.Code != 400 || p.calls != before {
		t.Fatal("Timeline accepted a request body")
	}
}
