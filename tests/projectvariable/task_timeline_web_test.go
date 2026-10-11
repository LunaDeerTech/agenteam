//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// All history is written by the original Human Work services before the
// browser reads it. This profile does not launch an Execution or seed SQL facts.
func TestTaskTimelineWeb(t *testing.T) {
	t.Run("recent-older-and-refresh", func(t *testing.T) {
		d := newTaskTransitionFixture(t)
		w := newTaskTimelineWeb(t, d)
		defer func() {
			w.shell.mu.Lock()
			failure := w.shell.failure
			w.shell.mu.Unlock()
			if failure != nil {
				t.Error("owned timeline observation failed", failure)
			}
		}()
		w.shell.browser(t)
		w.shell.close(t)
		w.verify(t)
	})
}

type timelineWebRequest struct{ index int }
type timelineWebRequestKey struct{}

// Each entry is one real browser action, checked before forwarding and again
// against persisted facts after its original HTTP response fully completes.
var taskTimelineReadSteps = [...]struct {
	order, eventType string
	page             int
}{
	{"desc", "", 0}, // Initial recent page.
	{"desc", "", 1}, // Its original older-page cursor.
	{"desc", "", 0}, // Explicit refresh.
	{"desc", "comment", 0},
	{"desc", "blocker_added", 0}, // Empty in the unchanged 51-event seed.
	{"desc", "", 0},              // Clear the event filter.
	{"asc", "", 0},
	{"asc", "", 1}, // Its original newer-page cursor.
}

type taskTimelineWeb struct {
	shell      *taskReviewWeb
	domain     *taskTransitionFixture
	current    wc.Task
	comment    string
	planning   map[wc.TaskEventID]wc.TaskMutation
	ready      wc.TaskTransitionMutation
	stored     []wc.TaskTimelineEvent // Independent persisted facts, ascending.
	requested  int
	completed  int
	nextCursor string
}

func newTaskTimelineWeb(t *testing.T, d *taskTransitionFixture) *taskTimelineWeb {
	t.Helper()
	w := &taskTimelineWeb{domain: d, current: d.task.Clone(), planning: map[wc.TaskEventID]wc.TaskMutation{},
		comment: "Ready for the next step.\n完整保留这条人工说明。\nLiteral text: <review> & \"quoted\" — end."}
	if w.current.Version != 1 || w.current.State != wc.TaskStateBacklog || w.current.AssigneeAgentID != nil {
		t.Fatal("timeline setup requires the original unassigned backlog")
	}
	var raw []byte
	err := d.base.raw.QueryRow(ctxFor(t), `SELECT receipt FROM agenteam_work.task_commands
 WHERE project_id=$1::text::uuid AND command_name='work.task.create' AND idempotency_key='transition-task'
 AND state='completed' AND request->'create'->>'task_id'=$2::text`, d.base.project.ID.String(), d.task.ID.String()).Scan(&raw)
	firstRoundRequire(t, err)
	var created wc.TaskMutation
	firstRoundRequire(t, json.Unmarshal(raw, &created))
	if created.Validate() != nil || !created.Changed || created.TaskEventID == nil || !reflect.DeepEqual(created.Task, w.current) {
		t.Fatal("timeline original Create receipt is absent or differs")
	}
	w.planning[*created.TaskEventID] = created.Clone()
	for n := 1; n <= 47; n++ {
		title := fmt.Sprintf("Timeline real change %02d", n)
		before := w.current.Clone()
		receipt, err := d.tasks.UpdateTask(ctxFor(t), d.base.ownerBrowser.actor,
			meta(t, fmt.Sprintf("timeline-web-update-%02d", n), &before.Version), before.ProjectID, before.ID,
			wc.TaskFieldsUpdate{Title: &title})
		firstRoundRequire(t, err)
		if receipt.Validate() != nil || !receipt.Changed || receipt.TaskEventID == nil || len(receipt.EventIDs) != 1 ||
			receipt.Task.Version != before.Version+1 || receipt.Task.Title != title || receipt.Task.State != wc.TaskStateBacklog ||
			receipt.Task.AssigneeAgentID != nil {
			t.Fatal("timeline Update did not commit one real changed version")
		}
		if _, duplicate := w.planning[*receipt.TaskEventID]; duplicate {
			t.Fatal("timeline Update reused a history identity")
		}
		w.planning[*receipt.TaskEventID] = receipt.Clone()
		w.current = receipt.Task.Clone()
	}
	before := w.current.Clone()
	w.ready, err = d.transitions.TransferTask(ctxFor(t), d.base.ownerBrowser.actor,
		meta(t, "timeline-web-ready-with-comment", &before.Version), before.ProjectID, before.ID,
		wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &d.agentID, Comment: &w.comment,
			AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	firstRoundRequire(t, err)
	if w.ready.Validate() != nil || len(w.ready.TaskEventIDs) != 3 || len(w.ready.EventIDs) != 1 ||
		w.ready.Task.Version != 49 || w.ready.Task.State != wc.TaskStateTodo || w.ready.Task.AssigneeAgentID == nil ||
		*w.ready.Task.AssigneeAgentID != d.agentID {
		t.Fatal("timeline Human Ready did not commit state, assignee and comment together")
	}
	w.current = w.ready.Task.Clone()
	w.stored, err = w.readFacts(ctxFor(t))
	firstRoundRequire(t, err)
	ids := make([]string, len(w.stored))
	for n, value := range w.stored {
		header, err := value.Header()
		firstRoundRequire(t, err)
		ids[n] = header.ID.String()
	}
	ready := make(chan struct{})
	w.shell = newTaskWebShell(t, d, "recent-older-and-refresh", taskWebProfile{
		envPrefix: "AGENTEAM_TASK_TIMELINE_WEB", filePrefix: "task-timeline", configName: "task-timeline.config.js", logName: "Task Timeline",
		observe:  func(r *http.Request) error { <-ready; return w.observeRequest(r) },
		response: func(r *http.Response) error { <-ready; return w.response(r) },
		material: func(username string) map[string]any {
			return map[string]any{"mode": "recent-older-and-refresh", "cookie": d.base.ownerBrowser.cookie,
				"project_id": w.current.ProjectID, "task_id": w.current.ID, "sprint_id": w.current.SprintID, "worker_id": d.agentID,
				"route": "/" + username + "/" + d.base.project.NormalizedName + "/tasks/" + w.current.ID.String(),
				"title": w.current.Title, "comment": w.comment, "version": w.current.Version, "event_ids": ids}
		},
	})
	close(ready)
	return w
}

func (w *taskTimelineWeb) path() string {
	return "/api/v1/projects/" + w.current.ProjectID.String() + "/tasks/" + w.current.ID.String() + "/events"
}

func (w *taskTimelineWeb) observeRequest(r *http.Request) error {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return errors.New("timeline browser must not write or retry a command")
	}
	if !strings.HasSuffix(r.URL.Path, "/events") {
		return nil
	}
	if r.Method != http.MethodGet || r.URL.Path != w.path() {
		return errors.New("timeline request escaped its original Project/Task")
	}
	q := r.URL.Query()
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()
	index := w.requested
	if index >= len(taskTimelineReadSteps) || index != w.completed || len(q["limit"]) != 1 || q.Get("limit") != "50" {
		return errors.New("timeline request lost the original serialized page contract")
	}
	step := taskTimelineReadSteps[index]
	keys := 2
	if len(q["order"]) != 1 || q.Get("order") != step.order {
		return errors.New("timeline action did not request its exact time direction")
	}
	if step.eventType != "" {
		keys++
		if len(q["types"]) != 1 || q.Get("types") != step.eventType {
			return errors.New("timeline action did not request its exact event filter")
		}
	} else if q.Has("types") {
		return errors.New("all-events action retained a previous event filter")
	}
	if step.page == 1 {
		keys++
		if len(q["cursor"]) != 1 || w.nextCursor == "" || q.Get("cursor") != w.nextCursor {
			return errors.New("timeline continuation did not reuse its own original opaque cursor")
		}
	} else if q.Has("cursor") {
		return errors.New("timeline condition change or refresh retained an old cursor")
	}
	if len(q) != keys {
		return errors.New("timeline request contains an unexpected query field")
	}
	w.requested++
	*r = *r.WithContext(context.WithValue(r.Context(), timelineWebRequestKey{}, timelineWebRequest{index}))
	return nil
}

func (w *taskTimelineWeb) response(r *http.Response) error {
	raw, err := w.shell.completedResponse(r)
	if err != nil {
		return err
	}
	request, ok := r.Request.Context().Value(timelineWebRequestKey{}).(timelineWebRequest)
	if !ok {
		return nil
	}
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("original timeline returned HTTP %d", r.StatusCode)
	}
	id := r.Header.Get("X-Request-ID")
	requestID, err := f.ParseID[f.Request](id)
	if err != nil || len(r.Header.Values("X-Request-ID")) != 1 || requestID.String() != id {
		return errors.New("original timeline response identity invalid")
	}
	var page f.Page[wc.TaskTimelineEvent]
	if err := json.Unmarshal(raw, &page); err != nil {
		return err
	}
	want := slices.Clone(w.stored)
	step := taskTimelineReadSteps[request.index]
	if step.eventType != "" {
		filtered := make([]wc.TaskTimelineEvent, 0)
		for _, event := range want {
			header, err := event.Header()
			if err != nil {
				return err
			}
			if string(header.Type) == step.eventType {
				filtered = append(filtered, event)
			}
		}
		want = filtered
	}
	if step.order == "desc" {
		slices.Reverse(want)
	}
	if step.page == 1 {
		want = want[50:]
	}
	if len(want) > 50 {
		want = want[:50]
		if page.NextCursor == "" || len(page.NextCursor) > 8192 {
			return errors.New("actual timeline page lost its opaque continuation")
		}
	} else if page.NextCursor != "" {
		return errors.New("exhausted actual timeline page did not end")
	}
	// Planning payloads retain their original JSON bytes. PostgreSQL JSONB
	// includes insignificant whitespace which the formal HTTP encoder compacts.
	// Encode both complete typed arrays through that same public wire contract;
	// retain every field, payload value, comment byte and event position.
	actualWire, err := json.Marshal(page.Items)
	if err != nil {
		return err
	}
	wantWire, err := json.Marshal(want)
	if err != nil {
		return err
	}
	if !bytes.Equal(actualWire, wantWire) {
		return errors.New("timeline HTTP page differs from original persisted typed facts")
	}
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()
	if request.index != w.completed {
		return errors.New("timeline responses did not complete in original request order")
	}
	if request.index == 0 || request.index == 6 {
		w.nextCursor = page.NextCursor
	}
	w.completed++
	return nil
}

// Read-only independent facts; no positive history is inserted through SQL and
// the timeline Reader is not used to construct its own expected output.
func (w *taskTimelineWeb) readFacts(ctx context.Context) ([]wc.TaskTimelineEvent, error) {
	d := w.domain
	rows, err := d.base.raw.Query(ctx, `SELECT operation_id::text,transition_operation_id::text,
 blocker_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND failure_operation_id IS NULL,
 jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'task_version',task_version::text,
 'type',type,'actor',actor,'operation_id',coalesce(operation_id,transition_operation_id),'correlation_id',correlation_id,
 'payload',payload,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))
 FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid ORDER BY created_at,id`,
		w.current.ProjectID.String(), w.current.ID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []wc.TaskTimelineEvent{}
	seen := map[wc.TaskEventID]bool{}
	for rows.Next() {
		var planning, transition *string
		var otherEmpty bool
		var raw []byte
		if err := rows.Scan(&planning, &transition, &otherEmpty, &raw); err != nil {
			return nil, err
		}
		var value wc.TaskTimelineEvent
		switch {
		case otherEmpty && planning != nil && transition == nil:
			var event wc.TaskEvent
			if err := json.Unmarshal(raw, &event); err != nil {
				return nil, err
			}
			receipt, ok := w.planning[event.ID]
			if !ok || event.OperationID.String() != *planning || event.TaskVersion != receipt.Task.Version ||
				event.Actor.UserID.String() != d.base.ownerBrowser.actor.Details().UserID {
				return nil, errors.New("timeline Planning row lost its actual command/receipt/Owner")
			}
			value.Planning = &event
		case otherEmpty && planning == nil && transition != nil:
			event, err := wc.DecodeTaskTransitionEvent(raw)
			if err != nil {
				return nil, err
			}
			index := slices.Index(w.ready.TaskEventIDs, event.ID)
			if index < 0 || event.OperationID.String() != *transition || event.TaskVersion != w.ready.Task.Version ||
				event.Actor.UserID.String() != d.base.ownerBrowser.actor.Details().UserID {
				return nil, errors.New("timeline Human row lost its actual command/receipt/Owner")
			}
			valid := index == 0 && event.Type == wc.TaskTransitionStateChanged && event.Payload.StateChanged != nil &&
				event.Payload.StateChanged.FromState == wc.TaskStateBacklog && event.Payload.StateChanged.ToState == wc.TaskStateTodo ||
				index == 1 && event.Type == wc.TaskTransitionAssigneeChanged && event.Payload.AssigneeChanged != nil &&
					event.Payload.AssigneeChanged.FromAgentID == nil && event.Payload.AssigneeChanged.ToAgentID == d.agentID ||
				index == 2 && event.Type == wc.TaskTransitionComment && event.Payload.Comment != nil && event.Payload.Comment.Body == w.comment
			if !valid {
				return nil, errors.New("timeline Human state/assignment/comment changed")
			}
			value.Transition = &event
		default:
			return nil, errors.New("timeline fixture history has an unexpected operation source")
		}
		header, err := value.Header()
		if err != nil || header.ProjectID != w.current.ProjectID || header.TaskID != w.current.ID ||
			header.TaskVersion > w.current.Version || seen[header.ID] {
			return nil, errors.New("timeline persisted scope/version/identity invalid")
		}
		seen[header.ID] = true
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(values) != 51 || len(w.planning) != 48 {
		return nil, errors.New("timeline did not persist exactly 51 real history facts")
	}
	var facts [8]int64
	err = d.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.create' AND state='completed'),
 (SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.update' AND state='completed'),
 (SELECT count(*) FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND state='completed'),
 (SELECT count(*) FROM agenteam_work.task_events h JOIN agenteam_work.task_commands c ON c.id=h.operation_id WHERE h.project_id=$1::text::uuid AND h.task_id=$2::text::uuid AND c.state='completed' AND c.receipt->>'task_event_id'=h.id::text AND c.receipt->'task'->>'version'=h.task_version::text),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND producer='work' AND event_type='work.task_changed'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND producer='work' AND event_type='work.task_transitioned'),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`,
		w.current.ProjectID.String(), w.current.ID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5], &facts[6], &facts[7])
	if err != nil {
		return nil, err
	}
	if facts != ([8]int64{1, 47, 1, 48, 48, 1, 0, 0}) {
		return nil, fmt.Errorf("timeline original command/history/Outbox counts differ: %v", facts)
	}
	var readyRaw []byte
	err = d.base.raw.QueryRow(ctx, `SELECT receipt FROM agenteam_work.task_transition_commands
 WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND idempotency_key='timeline-web-ready-with-comment' AND state='completed'`,
		w.current.ProjectID.String(), w.current.ID.String()).Scan(&readyRaw)
	if err != nil {
		return nil, err
	}
	ready, err := wc.DecodeTaskTransitionMutation(readyRaw)
	if err != nil || !reflect.DeepEqual(ready, w.ready) {
		return nil, errors.New("timeline Ready differs from the original complete stored receipt")
	}
	current, err := d.taskReader.GetTask(ctx, d.base.ownerBrowser.actor, w.current.ProjectID, w.current.ID)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(current, w.current) {
		return nil, errors.New("timeline reads changed the original Task")
	}
	return values, nil
}

func (w *taskTimelineWeb) verify(t *testing.T) {
	t.Helper()
	w.shell.mu.Lock()
	failure := w.shell.failure
	requested, completed := w.requested, w.completed
	for _, tail := range w.shell.tails {
		if !tail.returned {
			failure = errors.New("original timeline HTTP handler did not return")
		}
	}
	w.shell.mu.Unlock()
	if failure != nil {
		t.Fatal("owned timeline observation failed", failure)
	}
	if requested != len(taskTimelineReadSteps) || completed != len(taskTimelineReadSteps) {
		t.Fatal("timeline browser did not finish every original filter and time-direction page")
	}
	stored, err := w.readFacts(ctxFor(t))
	firstRoundRequire(t, err)
	if !reflect.DeepEqual(stored, w.stored) {
		t.Fatal("timeline read-only browser changed the original history")
	}
}
