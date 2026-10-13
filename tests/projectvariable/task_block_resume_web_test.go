//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This scope uses the original Owner API seed and full defaultApp. Runtime is
// disabled, and the Project retains its real disabled Scheduler default. The
// native companion owns the final-reason refusal and historical Lookup/replay.
func TestTaskBlockResumeWeb(t *testing.T) {
	t.Run("assigned-todo-block-and-resume", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Task Block Resume App handoff incomplete")
			}
		})
		v := assembleTaskHumanHTTPFixture(t, true)
		a, base := v.domain.agent, v.domain.base
		body, _, key := v.intent(t)
		assigned := v.requireTransition(t, v.request(t, base.ownerBrowser, "POST", v.transferPath(), body, key)).Task
		if assigned.State != wc.TaskStateTodo || assigned.AssigneeAgentID == nil {
			t.Fatal("formal assigned Todo seed missing")
		}
		sprintCompleteStart(t, v, sprintCompleteGet(t, v, assigned.SprintID), "block-resume-current")
		config, err := base.projects.GetSchedulerConfig(ctxFor(t), base.ownerBrowser.actor, base.project.ID)
		agentAppRequire(t, err)
		if config.Config.Enabled {
			t.Fatal("browser seed must retain the formal disabled Scheduler default")
		}
		process, err := a.guard.CurrentProcess()
		agentAppRequire(t, err)
		var username string
		agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, base.ownerBrowser.actor.Details().UserID).Scan(&username))
		if a.p2.base.afterOwners != nil {
			t.Fatal("original fixture handoff occupied")
		}
		a.p2.base.afterOwners = func() {
			entered = true
			if !a.p2.base.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
				t.Fatal("original setup owners not joined")
			}
			if _, err := a.guard.CurrentProcess(); err == nil {
				t.Fatal("original setup ProcessGuard held")
			}
			var stopped bool
			agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
			if !stopped {
				t.Fatal("original setup process not retired")
			}
			w := newTaskBlockResumeWeb(t, a, base, assigned, username)
			defer w.dispose(t)
			withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
				defer w.close(t)
				w.startObserved(t, address, w.observe, w.response)
				w.browser(t, ctx)
				w.close(t)
				w.verify(t, ctx)
			})
			completed = true
		}
	})
}

type taskBlockResumeIntent struct {
	kind                     string
	key                      f.IdempotencyKey
	before                   wc.Task
	requestID                f.ID[f.Request]
	requestDigest, keyDigest [32]byte
	add                      *wc.TaskBlockerCreate
	transfer                 *wc.TaskTransfer
}
type taskBlockResumeIntentKey struct{}
type taskBlockResumeWeb struct {
	*sprintStartWeb
	base                                                   *variableHTTPFixture
	initial, current                                       wc.Task
	intents                                                []taskBlockResumeIntent
	addReceipt                                             wc.TaskBlockerMutation
	transitions                                            []wc.TaskTransitionMutation
	blocker                                                *wc.TaskBlocker
	history                                                []wc.TaskTransitionEvent
	queryGeneration                                        int64
	baselineEvents, taskReads, blockerReads, timelineReads int
	after                                                  string
	responseFailure                                        map[string]any
	browserStarted                                         time.Time
	browserReturned                                        bool
}

func newTaskBlockResumeWeb(t *testing.T, a *agentCreateFixture, base *variableHTTPFixture, task wc.Task, username string) *taskBlockResumeWeb {
	t.Helper()
	const prefix = "AGENTEAM_TASK_BLOCK_RESUME_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) {
		t.Fatal("owned Task Block Resume paths required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Task Block Resume production assets missing")
	}
	dir, err := os.MkdirTemp(runtime, "tbr-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(dir)
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	agentAppRequire(t, err)
	defer func() {
		if !published {
			_ = listener.Close()
		}
	}()
	owner := &agentConfigurationWeb{a: a, mode: "assigned-todo-block-and-resume", origin: "http://" + listener.Addr().String(), directory: dir, evidence: evidence, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &taskBlockResumeWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: owner}, base: base, initial: task.Clone(), current: task.Clone()}
	var quiet bool
	agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT NOT scheduler_enabled AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text) AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text) FROM agenteam_project.projects WHERE id=$1::text::uuid`, task.ProjectID.String()).Scan(&quiet))
	if !quiet {
		t.Fatal("original disabled Scheduler or empty occupancy missing")
	}
	agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1`, task.ProjectID.String()).Scan(&w.queryGeneration))
	agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2`, task.ProjectID.String(), task.ID.String()).Scan(&w.baselineEvents))
	route := "/" + username + "/" + base.project.NormalizedName + "/tasks/" + task.ID.String()
	raw, err := json.Marshal(map[string]any{"mode": owner.mode, "origin": owner.origin, "cookie": base.ownerBrowser.cookie, "project_id": task.ProjectID, "task": task, "description": blockerManagementDescription, "route": route})
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "task-block-resume-material.json"), raw, 0600)
	}
	clear(raw)
	agentAppRequire(t, err)
	published = true
	return w
}
func (w *taskBlockResumeWeb) path() string {
	return variableHTTPPath(w.initial.ProjectID, "/tasks/"+w.initial.ID.String())
}
func (w *taskBlockResumeWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return errors.New("undeclared Block Resume mutation")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > 16384 {
		return errors.New("Block Resume request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("Block Resume original key missing")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	index := len(w.intents)
	if index >= 3 || index > 0 && (w.addReceipt.Validate() != nil || len(w.transitions) != index-1) {
		return errors.New("Block Resume writes repeated or unconfirmed")
	}
	for _, old := range w.keys {
		if old == string(key) {
			return errors.New("distinct Block Resume intent reused key")
		}
	}
	in := taskBlockResumeIntent{key: key, before: w.current.Clone(), requestDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	var wire struct {
		Expected f.Version       `json:"expected_version"`
		Request  json.RawMessage `json:"request"`
	}
	if agentWebDecode(raw, &wire) != nil || wire.Expected != in.before.Version {
		return errors.New("Block Resume lost original Task/version")
	}
	if index == 0 {
		var add wc.TaskBlockerCreate
		if r.URL.Path != w.path()+"/blockers" || json.Unmarshal(wire.Request, &add) != nil || add.Validate() != nil || add.Type != wc.TaskBlockerWaitingForHuman || add.Description != blockerManagementDescription || add.Metadata.WaitingForHuman == nil || add.Metadata.RelyOn != nil {
			return errors.New("Block Resume Add lost human cause")
		}
		in.kind, in.add = "add", &add
	} else {
		request, e := wc.DecodeTaskTransfer(wire.Request)
		if e != nil || r.URL.Path != w.path()+"/transfer" || len(request.AddBlockers) != 0 || request.Comment != nil {
			return errors.New("Block Resume Transfer changed exact intent")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(wire.Request, &fields) != nil {
			return errors.New("Block Resume Transfer fields invalid")
		}
		if index == 1 {
			if in.before.State != wc.TaskStateTodo || request.TargetState != wc.TaskStateBlocked || request.AssigneeAgentID != nil || len(request.ResolveBlockerIDs) != 0 || len(fields) != 1 {
				return errors.New("mark Blocked added assignment/resolution")
			}
			in.kind = "block"
		} else {
			if in.before.State != wc.TaskStateBlocked || request.TargetState != wc.TaskStateTodo || len(request.ResolveBlockerIDs) != 1 || w.blocker == nil || request.ResolveBlockerIDs[0] != w.blocker.ID || request.AssigneeAgentID != nil && *request.AssigneeAgentID != *w.initial.AssigneeAgentID {
				return errors.New("resume lost original final blocker/Agent")
			}
			for name := range fields {
				if name != "target_state" && name != "resolve_blocker_ids" && name != "assignee_agent_id" {
					return errors.New("resume added undeclared fields")
				}
			}
			in.kind = "resume"
		}
		in.transfer = &request
	}
	w.intents = append(w.intents, in)
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), taskBlockResumeIntentKey{}, in))
	return nil
}
func (w *taskBlockResumeWeb) response(r *http.Response) (err error) {
	stage := "complete-response"
	defer func() {
		if err == nil {
			return
		}
		observed := time.Now()
		record := map[string]any{"observed_at_unix_ms": observed.UnixMilli(), "stage": stage, "error": blockerManagementSafeFailure(err), "context": blockerManagementSafeFailure(r.Request.Context().Err()), "status": r.StatusCode, "request_kind": w.failureRequestKind(r.Request)}
		w.mu.Lock()
		record["elapsed_node_ms"] = observed.Sub(w.browserStarted).Milliseconds()
		record["before_node_return"] = !w.browserReturned
		record["original_failure_already_set"] = w.failure != nil
		if w.responseFailure == nil {
			w.responseFailure = record
		}
		w.mu.Unlock()
	}()
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	stage = "http-status"
	if r.StatusCode != 200 {
		return fmt.Errorf("original Block Resume HTTP returned %d", r.StatusCode)
	}
	in, write := r.Request.Context().Value(taskBlockResumeIntentKey{}).(taskBlockResumeIntent)
	w.mu.Lock()
	defer w.mu.Unlock()
	record := map[string]any{"request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": 200, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	if write {
		stage = "command-facts"
		in.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		if in.add != nil {
			var receipt wc.TaskBlockerMutation
			if json.Unmarshal(raw, &receipt) != nil {
				return errors.New("Block Resume Add receipt invalid")
			}
			original := &taskBlockerManagementWeb{base: w.base, initial: w.initial, queryGeneration: w.queryGeneration}
			source := humanBlockerIntent{key: in.key, before: in.before, expected: in.before.Version, requestID: in.requestID, add: in.add}
			if err = original.commandFacts(r.Request.Context(), source, receipt, 0, true); err != nil {
				return err
			}
			w.addReceipt = receipt.Clone()
			w.current = receipt.Task.Clone()
			b := receipt.Blocker.Clone()
			w.blocker = &b
		} else {
			receipt, e := wc.DecodeTaskTransitionMutation(raw)
			if e != nil {
				return e
			}
			if err = w.transitionFacts(r.Request.Context(), in, receipt); err != nil {
				return err
			}
			w.transitions = append(w.transitions, receipt.Clone())
			w.current = receipt.Task.Clone()
		}
		w.intents[len(w.intents)-1] = in
		record["kind"] = in.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", in.requestDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", in.keyDigest)
		record["committed_verified"] = true
		if in.kind == "resume" {
			w.after, err = sprintCompleteSnapshotContext(r.Request.Context(), w.base)
			if err != nil {
				return err
			}
		}
	} else {
		stage = "read-facts"
		if err = w.read(r.Request, raw); err != nil {
			return err
		}
	}
	stage = "evidence-write"
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(w.evidence, "task-block-resume-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *taskBlockResumeWeb) transitionFacts(ctx context.Context, in taskBlockResumeIntent, result wc.TaskTransitionMutation) error {
	before := in.before
	expected := before.Clone()
	expected.Version++
	expected.State = in.transfer.TargetState
	expected.UpdatedAt, expected.ManualRank = result.Task.UpdatedAt, result.Task.ManualRank
	count := 1 + len(in.transfer.ResolveBlockerIDs)
	if result.Validate() != nil || !reflect.DeepEqual(result.Task, expected) || len(result.TaskEventIDs) != count || len(result.EventIDs) != 1 {
		return errors.New("Block Resume receipt changed retained Task fields/history")
	}
	semantic, err := wc.TaskTransferDigest(w.base.ownerBrowser.actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key, ExpectedVersion: &before.Version}, before.ProjectID, before.ID, *in.transfer)
	if err != nil {
		return err
	}
	var raw, requestRaw, payload []byte
	var exact bool
	var command string
	err = w.base.raw.QueryRow(ctx, `SELECT c.receipt,c.request->'request',e.payload,c.id::text,
 c.actor_user_id=$4::text::uuid AND c.expected_version=$5 AND c.semantic_digest=$6 AND c.state='completed'
 AND t.state=$7 AND t.version=$8 AND t.assignee_agent_id=$9::text::uuid AND t.sprint_id=$10::uuid AND t.milestone_id=$11::uuid AND t.updated_at=$12 AND t.manual_rank=$13
 AND c.xmin=t.xmin AND c.xmin=e.xmin AND c.committed_at=$12
 AND e.producer='work' AND e.event_type='work.task_transitioned' AND e.aggregate_type='work.task' AND e.schema_version=1 AND e.project_id=c.project_id AND e.aggregate_id=t.id AND e.aggregate_version=t.version
 AND (SELECT count(*) FROM agenteam_work.task_events h WHERE h.transition_operation_id=c.id)=$14
 AND NOT EXISTS(SELECT 1 FROM agenteam_work.task_events h WHERE h.transition_operation_id=c.id AND (h.xmin<>c.xmin OR h.task_version<>t.version OR h.created_at<>t.updated_at OR h.project_id<>c.project_id OR h.task_id<>c.task_id OR h.correlation_id<>c.id OR h.actor<>jsonb_build_object('type','human','user_id',$4::text,'source','task_domain')))
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text)
 FROM agenteam_work.task_transition_commands c JOIN agenteam_work.tasks t ON t.id=c.task_id AND t.project_id=c.project_id JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.task_id=$2::uuid AND c.idempotency_key=$3`, before.ProjectID.String(), before.ID.String(), string(in.key), w.base.ownerBrowser.actor.Details().UserID, int64(before.Version), semantic.String(), string(expected.State), int64(result.Task.Version), before.AssigneeAgentID.String(), before.SprintID.String(), before.MilestoneID.String(), result.Task.UpdatedAt.Time(), result.Task.ManualRank, count).Scan(&raw, &requestRaw, &payload, &command, &exact)
	if err != nil {
		return err
	}
	stored, err := wc.DecodeTaskTransitionMutation(raw)
	if err != nil {
		return err
	}
	request, err := wc.DecodeTaskTransfer(requestRaw)
	if err != nil {
		return err
	}
	event, err := wc.DecodeTaskTransitioned(payload)
	if err != nil {
		return err
	}
	if !exact || !reflect.DeepEqual(stored, result) || !reflect.DeepEqual(request, *in.transfer) || event.CommandID.String() != command || event.Actor.UserID.String() != w.base.ownerBrowser.actor.Details().UserID || event.MilestoneID != before.MilestoneID || event.SprintID != before.SprintID || event.FromState != before.State || event.ToState != expected.State || event.AssigneeChange != nil || !reflect.DeepEqual(event.TaskEventIDs, result.TaskEventIDs) {
		return errors.New("Block Resume lost original receipt/history/Outbox same transaction")
	}
	for _, position := range []wc.TaskTransitionPosition{event.SourcePosition, event.TargetPosition} {
		if position.SprintID != before.SprintID || position.Priority != before.Priority || position.PreviousID != nil || position.NextID != nil {
			return errors.New("single Task transition lost real source/target placement")
		}
		var generation int64
		if err = w.base.raw.QueryRow(ctx, `SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1::uuid AND sprint_id=$2::uuid AND state=$3 AND priority=$4`, before.ProjectID.String(), before.SprintID.String(), string(position.State), string(position.Priority)).Scan(&generation); err != nil {
			return err
		}
		if generation != position.OrderGeneration {
			return errors.New("transition position differs from persisted order generation")
		}
	}
	var generation int64
	if err = w.base.raw.QueryRow(ctx, `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::uuid`, before.ProjectID.String()).Scan(&generation); err != nil {
		return err
	}
	if generation != w.queryGeneration+int64(len(w.transitions))+2 {
		return errors.New("transition did not advance query generation exactly once")
	}
	for index, id := range result.TaskEventIDs {
		h := wc.TaskTransitionEvent{ID: id, ProjectID: before.ProjectID, TaskID: before.ID, TaskVersion: result.Task.Version, Actor: event.Actor, OperationID: event.CommandID, CorrelationID: event.CommandID, CreatedAt: result.Task.UpdatedAt}
		if index == 0 {
			h.Type = wc.TaskTransitionStateChanged
			h.Payload.StateChanged = &wc.TaskStateChangedPayload{FromState: before.State, ToState: expected.State}
		} else {
			h.Type = wc.TaskTransitionBlockerResolved
			h.Payload.BlockerResolved = &wc.TaskBlockerResolvedPayload{BlockerID: w.blocker.ID, BlockerType: wc.TaskBlockerWaitingForHuman}
		}
		if h.Validate() != nil {
			return errors.New("expected block/resume history invalid")
		}
		w.history = append(w.history, h)
	}
	if w.blocker == nil {
		return errors.New("original Blocker lost")
	}
	var blockRaw []byte
	var preserved bool
	err = w.base.raw.QueryRow(ctx, `SELECT jsonb_build_object('id',b.id,'project_id',b.project_id,'task_id',b.task_id,'type',b.type,'description',b.description,'metadata',b.metadata,'created_at',to_char(b.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'created_by',b.created_by,'resolved_at',CASE WHEN b.resolved_at IS NULL THEN NULL ELSE to_char(b.resolved_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END,'resolved_by',b.resolved_by,'resolution_comment',b.resolution_comment),
 b.created_operation_id=a.id AND b.failure_operation_id IS NULL AND b.resolved_operation_id IS NULL AND CASE WHEN $5 THEN b.resolved_transition_operation_id=c.id AND b.xmin=c.xmin ELSE b.resolved_transition_operation_id IS NULL END
 FROM agenteam_work.task_blockers b JOIN agenteam_work.task_blocker_commands a ON a.receipt->'blocker'->>'id'=b.id::text AND a.project_id=b.project_id AND a.command_name='work.task.blocker.add'
 JOIN agenteam_work.task_transition_commands c ON c.id=$4::uuid AND c.project_id=b.project_id AND c.task_id=b.task_id
 WHERE b.project_id=$1::uuid AND b.task_id=$2::uuid AND b.id=$3::uuid`, before.ProjectID.String(), before.ID.String(), w.blocker.ID.String(), command, in.kind == "resume").Scan(&blockRaw, &preserved)
	if err != nil {
		return err
	}
	want := w.blocker.Clone()
	if in.kind == "resume" {
		at := result.Task.UpdatedAt
		actor := w.addReceipt.Blocker.CreatedBy
		want.ResolvedAt = &at
		want.ResolvedBy = &actor
		want.ResolutionComment = nil
	}
	var actual wc.TaskBlocker
	if !preserved || json.Unmarshal(blockRaw, &actual) != nil || !reflect.DeepEqual(actual, want) {
		return errors.New("transition lost Blocker provenance or atomic final resolution")
	}
	w.blocker = &want
	return nil
}
func (w *taskBlockResumeWeb) read(r *http.Request, raw []byte) error {
	switch r.URL.Path {
	case w.path():
		var task wc.Task
		if json.Unmarshal(raw, &task) != nil || !reflect.DeepEqual(task, w.current) {
			return errors.New("Task read differs from committed block/resume")
		}
		w.taskReads++
	case w.path() + "/blockers":
		var page f.Page[wc.TaskBlocker]
		if json.Unmarshal(raw, &page) != nil || page.NextCursor != "" || page.Items == nil {
			return errors.New("Block Resume blocker page incomplete")
		}
		if w.blocker == nil {
			if len(page.Items) != 0 {
				return errors.New("undeclared initial blocker")
			}
		} else if len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], *w.blocker) {
			return errors.New("Block Resume blocker reread differs")
		}
		w.blockerReads++
	case w.path() + "/events":
		var page f.Page[json.RawMessage]
		added := 0
		if w.blocker != nil {
			added = 1
		}
		if json.Unmarshal(raw, &page) != nil || page.NextCursor != "" || len(page.Items) != w.baselineEvents+added+len(w.history) {
			return errors.New("Block Resume Timeline cardinality differs")
		}
		for _, expected := range w.history {
			found := 0
			for _, item := range page.Items {
				var id struct {
					ID wc.TaskEventID `json:"id"`
				}
				if json.Unmarshal(item, &id) != nil {
					return errors.New("Timeline identity invalid")
				}
				if id.ID == expected.ID {
					var h wc.TaskTransitionEvent
					if json.Unmarshal(item, &h) != nil || !reflect.DeepEqual(h, expected) {
						return errors.New("Block Resume original Timeline fact differs")
					}
					found++
				}
			}
			if found != 1 {
				return errors.New("Block Resume Timeline omitted or repeated fact")
			}
		}
		if w.blocker != nil {
			var wanted []byte
			if err := w.base.raw.QueryRow(r.Context(), `SELECT plan->'task_event' FROM agenteam_work.task_blocker_commands WHERE project_id=$1::uuid AND task_event_id=$2::uuid`, w.initial.ProjectID.String(), w.addReceipt.TaskEventID.String()).Scan(&wanted); err != nil {
				return err
			}
			var expected wc.TaskBlockerEvent
			if json.Unmarshal(wanted, &expected) != nil {
				return errors.New("Add original history invalid")
			}
			found := 0
			for _, item := range page.Items {
				var id struct {
					ID wc.TaskEventID `json:"id"`
				}
				_ = json.Unmarshal(item, &id)
				if id.ID == expected.ID {
					var actual wc.TaskBlockerEvent
					if json.Unmarshal(item, &actual) != nil || !reflect.DeepEqual(actual, expected) {
						return errors.New("Add Timeline changed after transition")
					}
					found++
				}
			}
			if found != 1 {
				return errors.New("Add Timeline missing")
			}
		}
		w.timelineReads++
	}
	return nil
}
func (w *taskBlockResumeWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "task-block-resume.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") && !strings.HasPrefix(key, "DEV_CHAT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_TASK_BLOCK_RESUME_WEB_DIST="+w.dist, "AGENTEAM_TASK_BLOCK_RESUME_WEB_EVIDENCE="+w.evidence)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	w.mu.Lock()
	w.browserStarted = time.Now()
	w.mu.Unlock()
	if err = cmd.Start(); err != nil {
		t.Fatal("Task Block Resume Node did not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
	}()
	err = <-done
	joined = true
	w.mu.Lock()
	w.browserReturned = true
	w.mu.Unlock()
	t.Logf("Task Block Resume Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.base.ownerBrowser.cookie, w.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		w.saveFailure(t, ctx, err)
		t.Fatal("original Task Block Resume browser failed")
	}
}

func (w *taskBlockResumeWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil || len(w.intents) != 3 || len(w.transitions) != 2 || w.taskReads != 4 || w.blockerReads != 4 || w.timelineReads != 4 || len(w.history) != 3 {
		t.Fatal("Block Resume original browser chain incomplete", w.failure)
	}
	for _, tail := range w.tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("Block Resume original tail open")
		}
		if !tail.returned {
			t.Fatal("Block Resume handler did not return")
		}
	}
	after, err := sprintCompleteSnapshotContext(ctx, w.base)
	agentAppRequire(t, err)
	if after != w.after {
		t.Fatal("final Block Resume rereads changed durable facts")
	}
	var counts bool
	agentAppRequire(t, w.base.raw.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text) AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text)`, w.initial.ProjectID.String()).Scan(&counts))
	if !counts {
		t.Fatal("disabled Scheduler fixture unexpectedly created dispatch/Execution")
	}
	var disabled bool
	agentAppRequire(t, w.base.raw.QueryRow(ctx, `SELECT NOT scheduler_enabled FROM agenteam_project.projects WHERE id=$1::uuid`, w.initial.ProjectID.String()).Scan(&disabled))
	if !disabled {
		t.Fatal("browser changed Scheduler configuration")
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "task-block-resume-result.json"))
	agentAppRequire(t, err)
	var result struct {
		Completed bool `json:"completed"`
		Writes    int  `json:"writes"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Completed || result.Writes != 3 {
		t.Fatal("Block Resume original browser consumers not retired")
	}
	t.Log("Task Block Resume defaultApp: assigned Todo Add→blocked→atomic human resolution/Todo; 3 original POSTs, exact receipts/history/provenance and original handlers; no dispatch or Execution")
}
func (w *taskBlockResumeWeb) saveFailure(t *testing.T, ctx context.Context, nodeErr error) {
	t.Helper()
	w.mu.Lock()
	returned, open := 0, 0
	for _, tail := range w.tails {
		if tail.returned {
			returned++
		} else {
			open++
		}
	}
	record := map[string]any{"observed_at_unix_ms": time.Now().UnixMilli(), "node_started_at_unix_ms": w.browserStarted.UnixMilli(), "node_returned": w.browserReturned, "node_error": blockerManagementSafeFailure(nodeErr), "browser_context": blockerManagementSafeFailure(ctx.Err()), "original_failure": blockerManagementSafeFailure(w.failure), "response_failure": w.responseFailure, "handler_returned": returned, "handler_open": open, "writes": len(w.intents), "receipts": len(w.transitions), "task_reads": w.taskReads, "blocker_reads": w.blockerReads, "timeline_reads": w.timelineReads}
	w.mu.Unlock()
	raw, err := json.MarshalIndent(record, "", "  ")
	if err == nil {
		err = os.MkdirAll(w.evidence, 0700)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(w.evidence, "task-block-resume-go-failure.json"), raw, 0600)
	}
	if err != nil {
		t.Error("Blocker safe failure evidence could not be preserved")
	}
	t.Logf("Task Block Resume safe failure: %s", raw)
}

func (w *taskBlockResumeWeb) failureRequestKind(r *http.Request) string {
	if r.URL.Path == variableHTTPPath(w.initial.ProjectID, "/tasks") {
		switch r.URL.Query().Get("state") {
		case "backlog", "todo", "in_progress", "in_review", "blocked", "done", "cancelled":
			return "task-list-" + r.URL.Query().Get("state")
		}
		return "task-list-other"
	}
	if r.URL.Path == w.path() {
		return "task-detail"
	}
	if r.URL.Path == w.path()+"/transfer" {
		return "task-transfer"
	}
	if r.URL.Path == w.path()+"/blockers" {
		return "task-blockers"
	}
	if r.URL.Path == w.path()+"/events" {
		return "task-timeline"
	}
	return "other"
}
