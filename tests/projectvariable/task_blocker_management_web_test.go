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

	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This fixture seeds through original Owner services/HTTP, retires their owners,
// and exercises the production SPA against the genuine defaultApp. Runtime is
// disabled; it proves assigned Todo management, not a live Execution or blocked
// state transition. The last-blocker guard is covered at the domain boundary.
const blockerManagementDescription = "等待人工确认。\n保留 <script>globalThis.__blocker_executed = true</script> 原文。"
const blockerManagementResolution = "已由原 Owner 明确核对。"
const blockerManagementDependency = "依赖同项目原始任务；保持负责人和排序。"

func TestTaskBlockerManagementWeb(t *testing.T) {
	t.Run("assigned-todo-human-and-dependency", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Task Blocker Management App handoff incomplete")
			}
		})
		v := assembleTaskHumanHTTPFixture(t, true)
		a, base := v.domain.agent, v.domain.base
		body, _, key := v.intent(t)
		assigned := v.requireTransition(t, v.request(t, base.ownerBrowser, "POST", v.transferPath(), body, key)).Task
		if assigned.State != wc.TaskStateTodo || assigned.AssigneeAgentID == nil {
			t.Fatal("formal assigned Todo seed missing")
		}
		related, err := v.domain.tasks.CreateTask(ctxFor(t), base.ownerBrowser.actor, meta(t, "blocker-management-related", nil), base.project.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: assigned.SprintID, Title: "Original dependency target", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium})
		agentAppRequire(t, err)
		if related.Validate() != nil || !related.Changed {
			t.Fatal("formal related Task missing")
		}
		sprintCompleteStart(t, v, sprintCompleteGet(t, v, assigned.SprintID), "blocker-management-current")
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
			w := newTaskBlockerManagementWeb(t, a, base, assigned, related.Task, username)
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

type blockerManagementIntentKey struct{}
type taskBlockerManagementWeb struct {
	*sprintStartWeb
	base                                                   *variableHTTPFixture
	initial, current, related                              wc.Task
	intents                                                []humanBlockerIntent
	receipts                                               []wc.TaskBlockerMutation
	blockers                                               []wc.TaskBlocker
	queryGeneration                                        int64
	taskReads, blockerReads, timelineReads, candidateReads int
	baselineEvents                                         int
	after                                                  string
	responseFailure                                        map[string]any
	browserStarted                                         time.Time
	browserReturned                                        bool
}

func newTaskBlockerManagementWeb(t *testing.T, a *agentCreateFixture, base *variableHTTPFixture, task, related wc.Task, username string) *taskBlockerManagementWeb {
	t.Helper()
	const prefix = "AGENTEAM_TASK_BLOCKER_MANAGEMENT_WEB"
	dist, runtime, evidence := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) {
		t.Fatal("owned Task Blocker Management paths required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Task Blocker Management production assets missing")
	}
	dir, err := os.MkdirTemp(runtime, "tbm-")
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
	owner := &agentConfigurationWeb{a: a, mode: "assigned-todo-human-and-dependency", origin: "http://" + listener.Addr().String(), directory: dir, evidence: evidence, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &taskBlockerManagementWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: owner}, base: base, initial: task.Clone(), current: task.Clone(), related: related.Clone(), blockers: []wc.TaskBlocker{}}
	agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1`, task.ProjectID.String()).Scan(&w.queryGeneration))
	agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2`, task.ProjectID.String(), task.ID.String()).Scan(&w.baselineEvents))
	route := "/" + username + "/" + base.project.NormalizedName + "/tasks/" + task.ID.String()
	raw, err := json.Marshal(map[string]any{"mode": owner.mode, "origin": owner.origin, "cookie": base.ownerBrowser.cookie, "project_id": task.ProjectID, "task": task, "related": related, "description": blockerManagementDescription, "resolution": blockerManagementResolution, "dependency": blockerManagementDependency, "route": route})
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "task-blocker-management-material.json"), raw, 0600)
	}
	clear(raw)
	agentAppRequire(t, err)
	published = true
	return w
}
func (w *taskBlockerManagementWeb) path() string {
	return variableHTTPPath(w.initial.ProjectID, "/tasks/"+w.initial.ID.String())
}
func (w *taskBlockerManagementWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return errors.New("undeclared Blocker Management mutation")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > 16384 {
		return errors.New("Blocker request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("original Blocker key absent")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	index := len(w.intents)
	if index >= 4 || index != len(w.receipts) {
		return errors.New("Blocker writes repeated or not confirmed")
	}
	for _, old := range w.keys {
		if old == string(key) {
			return errors.New("distinct Blocker command reused key")
		}
	}
	in := humanBlockerIntent{key: key, before: w.current.Clone(), requestDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	if index%2 == 0 {
		var wire struct {
			Expected f.Version            `json:"expected_version"`
			Request  wc.TaskBlockerCreate `json:"request"`
		}
		if r.URL.Path != w.path()+"/blockers" || agentWebDecode(raw, &wire) != nil || wire.Expected != w.current.Version || wire.Request.Validate() != nil {
			return errors.New("Add lost original Task/version/body")
		}
		if index == 0 && (wire.Request.Type != wc.TaskBlockerWaitingForHuman || wire.Request.Description != blockerManagementDescription || wire.Request.Metadata.WaitingForHuman == nil || wire.Request.Metadata.RelyOn != nil) {
			return errors.New("human Add lost exact input")
		}
		if index == 2 && (wire.Request.Type != wc.TaskBlockerRelyOn || wire.Request.Description != blockerManagementDependency || wire.Request.Metadata.RelyOn == nil || wire.Request.Metadata.RelyOn.RelatedTaskID != w.related.ID) {
			return errors.New("dependency Add lost exact input")
		}
		in.kind, in.expected, in.add = "add", wire.Expected, &wire.Request
	} else {
		var wire struct {
			Expected f.Version             `json:"expected_version"`
			Request  wc.TaskBlockerResolve `json:"request"`
		}
		before := w.blockers[index/2].Clone()
		var comment *string
		if index == 1 {
			value := blockerManagementResolution
			comment = &value
		}
		if r.URL.Path != w.path()+"/blockers/resolve" || agentWebDecode(raw, &wire) != nil || wire.Expected != w.current.Version || wire.Request.Validate() != nil || wire.Request.BlockerID != before.ID || !reflect.DeepEqual(wire.Request.ResolutionComment, comment) {
			return errors.New("Resolve lost original version/blocker/comment")
		}
		in.kind, in.expected, in.resolve, in.blockerBefore = "resolve", wire.Expected, &wire.Request, &before
	}
	w.intents = append(w.intents, in)
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), blockerManagementIntentKey{}, in))
	return nil
}
func (w *taskBlockerManagementWeb) response(r *http.Response) (err error) {
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
		return fmt.Errorf("original Blocker HTTP returned %d", r.StatusCode)
	}
	in, write := r.Request.Context().Value(blockerManagementIntentKey{}).(humanBlockerIntent)
	w.mu.Lock()
	defer w.mu.Unlock()
	record := map[string]any{"request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": 200, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	if write {
		stage = "command-facts"
		in.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		var receipt wc.TaskBlockerMutation
		if json.Unmarshal(raw, &receipt) != nil {
			return errors.New("invalid original Blocker receipt")
		}
		index := len(w.receipts)
		if err = w.commandFacts(r.Request.Context(), in, receipt, index, true); err != nil {
			return err
		}
		w.intents[index] = in
		w.receipts = append(w.receipts, receipt.Clone())
		w.current = receipt.Task.Clone()
		if in.add != nil {
			w.blockers = append(w.blockers, receipt.Blocker.Clone())
		} else {
			w.blockers[index/2] = receipt.Blocker.Clone()
		}
		record["kind"] = fmt.Sprintf("%s-%d", in.kind, index/2)
		record["request_body_sha256"] = fmt.Sprintf("%x", in.requestDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", in.keyDigest)
		record["committed_verified"] = true
		if index == 3 {
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
	if err = os.WriteFile(filepath.Join(w.evidence, "task-blocker-management-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *taskBlockerManagementWeb) commandFacts(ctx context.Context, in humanBlockerIntent, result wc.TaskBlockerMutation, index int, canonical bool) error {
	if result.Validate() != nil || in.expected != in.before.Version {
		return errors.New("invalid original blocker receipt/version")
	}
	expected := in.before.Clone()
	expected.Version++
	expected.UpdatedAt = result.Task.UpdatedAt
	if expected.UpdatedAt.Time().Before(in.before.UpdatedAt.Time()) || !reflect.DeepEqual(expected, result.Task) {
		return errors.New("blocker write changed Task content/state/assignee/rank")
	}
	user, err := f.ParseID[i.User](w.base.ownerBrowser.actor.Details().UserID)
	if err != nil {
		return err
	}
	actor := wc.TaskEventActor{Type: i.Human, UserID: user, Source: "task_domain"}
	var expectedBlocker wc.TaskBlocker
	command := wc.TaskBlockerCommandAdd
	if in.add != nil {
		expectedBlocker = wc.TaskBlocker{ID: in.add.BlockerID, ProjectID: w.initial.ProjectID, TaskID: w.initial.ID, Type: in.add.Type, Description: in.add.Description, Metadata: in.add.Metadata.Clone(), CreatedAt: expected.UpdatedAt, CreatedBy: actor}
	} else {
		if in.blockerBefore == nil || in.resolve == nil {
			return errors.New("Resolve missing original blocker")
		}
		expectedBlocker = in.blockerBefore.Clone()
		at := expected.UpdatedAt
		expectedBlocker.ResolvedAt = &at
		expectedBlocker.ResolvedBy = &actor
		expectedBlocker.ResolutionComment = in.resolve.Clone().ResolutionComment
		command = wc.TaskBlockerCommandResolve
	}
	if !reflect.DeepEqual(expectedBlocker, result.Blocker) {
		return errors.New("blocker receipt lost original identity/content/actors/resolution")
	}
	meta := f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key, ExpectedVersion: &in.expected}
	var digest f.Digest
	if in.add != nil {
		digest, err = wc.TaskBlockerAddDigest(w.base.ownerBrowser.actor, meta, w.initial.ProjectID, w.initial.ID, *in.add)
	} else {
		digest, err = wc.TaskBlockerResolveDigest(w.base.ownerBrowser.actor, meta, w.initial.ProjectID, w.initial.ID, *in.resolve)
	}
	if err != nil {
		return err
	}
	var commandID, historyID, eventID, userID, semantic string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.base.raw.QueryRow(ctx, `SELECT id::text,task_event_id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid AND command_name=$2 AND idempotency_key=$3 AND state='completed' AND committed_at IS NOT NULL`, w.initial.ProjectID.String(), string(command), string(in.key)).Scan(&commandID, &historyID, &eventID, &userID, &semantic, &requestRaw, &receiptRaw, &planRaw, &revision)
	if err != nil {
		return err
	}
	var request, wantRequest map[string]any
	var receipt wc.TaskBlockerMutation
	want := map[string]any{"command": command, "project_id": w.initial.ProjectID, "task_id": w.initial.ID, "actor_user_id": user, "expected_version": in.expected, "add": in.add, "resolve": in.resolve}
	wantRaw, err := json.Marshal(want)
	if err != nil {
		return err
	}
	if revision < 1 || userID != user.String() || semantic != digest.String() || historyID != result.TaskEventID.String() || eventID != result.EventIDs[0].String() || json.Unmarshal(requestRaw, &request) != nil || json.Unmarshal(wantRaw, &wantRequest) != nil || !reflect.DeepEqual(request, wantRequest) || json.Unmarshal(receiptRaw, &receipt) != nil || !reflect.DeepEqual(receipt, result) {
		return errors.New("original blocker command identity/request/receipt differs")
	}
	var plan struct {
		Before        wc.Task                `json:"before"`
		BlockerBefore *wc.TaskBlocker        `json:"blocker_before"`
		After         wc.TaskBlockerMutation `json:"after"`
		Placement     struct {
			Milestone wc.MilestoneID `json:"milestone_id"`
			Sprint    wc.SprintID    `json:"sprint_id"`
			State     wc.SprintState `json:"state"`
		} `json:"placement"`
		Generation int64                  `json:"query_generation"`
		Event      wc.TaskBlockerEvent    `json:"task_event"`
		Header     eventc.Header          `json:"header"`
		Payload    wc.TaskBlockersChanged `json:"payload"`
	}
	if json.Unmarshal(planRaw, &plan) != nil || !reflect.DeepEqual(plan.Before, in.before) || !reflect.DeepEqual(plan.BlockerBefore, in.blockerBefore) || !reflect.DeepEqual(plan.After, result) || plan.Generation != w.queryGeneration+int64(index) || plan.Placement.Milestone != w.initial.MilestoneID || plan.Placement.Sprint != w.initial.SprintID || plan.Placement.State != wc.Current {
		return errors.New("blocker plan lost original before/after/placement/generation")
	}
	op, err := f.ParseID[wc.TaskBlockerCommand](commandID)
	if err != nil {
		return err
	}
	eventType, change := wc.TaskBlockerEventAdded, wc.TaskBlockerAddedChange
	payload := wc.TaskBlockerEventPayload{Added: &wc.TaskBlockerAddedPayload{BlockerID: result.Blocker.ID, BlockerType: result.Blocker.Type}}
	if in.resolve != nil {
		eventType, change = wc.TaskBlockerEventResolved, wc.TaskBlockerResolvedChange
		payload = wc.TaskBlockerEventPayload{Resolved: &wc.TaskBlockerResolvedPayload{BlockerID: result.Blocker.ID, BlockerType: result.Blocker.Type, ResolutionComment: in.resolve.ResolutionComment}}
	}
	expectedHistory := wc.TaskBlockerEvent{ID: result.TaskEventID, ProjectID: w.initial.ProjectID, TaskID: w.initial.ID, TaskVersion: expected.Version, Type: eventType, Actor: actor, OperationID: op, CorrelationID: op, Payload: payload, CreatedAt: expected.UpdatedAt}
	expectedPayload := wc.TaskBlockersChanged{OperationID: op, ActorUserID: user, TaskEventID: result.TaskEventID, BlockerID: result.Blocker.ID, Change: change}
	h := plan.Header
	if plan.Event.Validate() != nil || !reflect.DeepEqual(plan.Event, expectedHistory) || !reflect.DeepEqual(plan.Payload, expectedPayload) || h.Validate() != nil || h.EventID != result.EventIDs[0] || h.EventType != wc.TaskBlockersChangedName || h.AggregateType != wc.TaskAggregate || h.AggregateID.String() != w.initial.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != expected.Version || h.AggregateSequence != nil || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.initial.ProjectID.String() || h.SchemaVersion != wc.TaskBlockerSchemaVersion || h.OccurredAt != expected.UpdatedAt {
		return errors.New("blocker plan history/Outbox differs from independently expected facts")
	}
	var historyRaw, headerRaw, payloadRaw []byte
	var historyColumns, eventColumns bool
	err = w.base.raw.QueryRow(ctx, `SELECT operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND failure_operation_id IS NULL,
 jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'task_version',task_version::text,'type',type,'actor',actor,'operation_id',blocker_operation_id,'correlation_id',correlation_id,'payload',payload,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) FROM agenteam_work.task_events WHERE id=$1::text::uuid AND project_id=$2::text::uuid AND task_id=$3::text::uuid AND blocker_operation_id=$4::text::uuid`, historyID, w.initial.ProjectID.String(), w.initial.ID.String(), commandID).Scan(&historyColumns, &historyRaw)
	if err != nil {
		return err
	}
	var storedHistory wc.TaskBlockerEvent
	if !historyColumns || json.Unmarshal(historyRaw, &storedHistory) != nil || !reflect.DeepEqual(storedHistory, expectedHistory) {
		return errors.New("canonical Task history differs from original blocker plan")
	}
	err = w.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.task_blockers_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.task' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5 FROM agenteam_outbox.events WHERE id=$1::text::uuid AND project_id=$2::text::uuid AND aggregate_id=$3::text::uuid AND producer='work'`, eventID, w.initial.ProjectID.String(), w.initial.ID.String(), int64(expected.Version), expected.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil {
		return err
	}
	actualHeader, err := eventc.DecodeHeader(headerRaw)
	var actualPayload wc.TaskBlockersChanged
	if err != nil || !eventColumns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, expectedPayload) {
		return errors.New("canonical blocker Outbox differs from original plan")
	}
	if canonical {
		var sameTx bool
		err = w.base.raw.QueryRow(ctx, `SELECT c.xmin=t.xmin AND c.xmin=b.xmin AND c.xmin=h.xmin AND c.xmin=e.xmin
        AND t.state=$3 AND t.version=$4 AND t.manual_rank=$5 AND t.assignee_agent_id=$6::text::uuid AND t.updated_at=$7
        FROM agenteam_work.task_blocker_commands c JOIN agenteam_work.tasks t ON t.id::text=c.request->>'task_id' AND t.project_id=c.project_id
        JOIN agenteam_work.task_blockers b ON b.id=$8::text::uuid JOIN agenteam_work.task_events h ON h.id=c.task_event_id
        JOIN agenteam_outbox.events e ON e.id=c.event_id WHERE c.id=$1::text::uuid AND c.project_id=$2::text::uuid`, commandID, w.initial.ProjectID.String(), string(expected.State), int64(expected.Version), expected.ManualRank, expected.AssigneeAgentID.String(), expected.UpdatedAt.Time(), result.Blocker.ID.String()).Scan(&sameTx)
		if err != nil {
			return err
		}
		if !sameTx {
			return errors.New("Blocker postimage not atomic or changed assigned Task placement")
		}
		var blockerRaw []byte
		var operations bool
		var generation int64
		err = w.base.raw.QueryRow(ctx, `SELECT jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'type',type,'description',description,'metadata',metadata,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'created_by',created_by,'resolved_at',CASE WHEN resolved_at IS NULL THEN NULL ELSE to_char(resolved_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END,'resolved_by',resolved_by,'resolution_comment',resolution_comment),failure_operation_id IS NULL AND resolved_transition_operation_id IS NULL AND created_operation_id=(SELECT id FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid AND command_name='work.task.blocker.add' AND receipt->'blocker'->>'id'=$3::text) AND CASE WHEN $4 THEN resolved_operation_id=$5::text::uuid ELSE resolved_operation_id IS NULL END FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND id=$3::text::uuid`, w.initial.ProjectID.String(), w.initial.ID.String(), result.Blocker.ID.String(), in.resolve != nil, commandID).Scan(&blockerRaw, &operations)
		if err != nil {
			return err
		}
		var actual wc.TaskBlocker
		if !operations || json.Unmarshal(blockerRaw, &actual) != nil || !reflect.DeepEqual(actual, expectedBlocker) {
			return errors.New("canonical blocker differs from full original receipt/operation ownership")
		}
		err = w.base.raw.QueryRow(ctx, `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid`, w.initial.ProjectID.String()).Scan(&generation)
		if err != nil {
			return err
		}
		if generation != w.queryGeneration+int64(index)+1 {
			return errors.New("blocker query generation did not advance exactly once")
		}
	}
	return nil
}

func (w *taskBlockerManagementWeb) read(r *http.Request, raw []byte) error {
	switch r.URL.Path {
	case w.path():
		var task wc.Task
		if json.Unmarshal(raw, &task) != nil || !reflect.DeepEqual(task, w.current) {
			return errors.New("Task reread differs from original Blocker postimage")
		}
		w.taskReads++
	case w.path() + "/blockers":
		var page f.Page[wc.TaskBlocker]
		if json.Unmarshal(raw, &page) != nil || page.NextCursor != "" || len(page.Items) != len(w.blockers) {
			return errors.New("Blocker reread missing retained records")
		}
		for _, want := range w.blockers {
			found := 0
			for _, got := range page.Items {
				if reflect.DeepEqual(got, want) {
					found++
				}
			}
			if found != 1 {
				return errors.New("Blocker reread differs from original receipt")
			}
		}
		w.blockerReads++
	case w.path() + "/events":
		var page struct {
			Items []json.RawMessage `json:"items"`
			Next  *string           `json:"next_cursor"`
		}
		if json.Unmarshal(raw, &page) != nil || (page.Next != nil && *page.Next != "") || len(page.Items) != w.baselineEvents+len(w.receipts) {
			return errors.New("Timeline lost exact event cardinality")
		}
		for _, receipt := range w.receipts {
			var want []byte
			if err := w.base.raw.QueryRow(r.Context(), `SELECT plan->'task_event' FROM agenteam_work.task_blocker_commands WHERE task_event_id=$1`, receipt.TaskEventID.String()).Scan(&want); err != nil {
				return err
			}
			var expected wc.TaskBlockerEvent
			if json.Unmarshal(want, &expected) != nil {
				return errors.New("canonical Timeline plan invalid")
			}
			found := 0
			for _, item := range page.Items {
				var identity struct {
					ID wc.TaskEventID `json:"id"`
				}
				if json.Unmarshal(item, &identity) != nil {
					return errors.New("Timeline identity invalid")
				}
				if identity.ID == receipt.TaskEventID {
					var got wc.TaskBlockerEvent
					if json.Unmarshal(item, &got) != nil || !reflect.DeepEqual(got, expected) {
						return errors.New("Timeline original Blocker history differs")
					}
					found++
				}
			}
			if found != 1 {
				return errors.New("Timeline omitted or duplicated Blocker history")
			}
		}
		w.timelineReads++
	case variableHTTPPath(w.initial.ProjectID, "/tasks"):
		if r.URL.Query().Get("sprint_id") == "" {
			var page struct {
				Items []struct {
					ID      wc.TaskID    `json:"id"`
					State   wc.TaskState `json:"state"`
					Version f.Version    `json:"version"`
				} `json:"items"`
			}
			if json.Unmarshal(raw, &page) != nil {
				return errors.New("dependency candidates invalid")
			}
			found := false
			for _, item := range page.Items {
				if item.ID == w.related.ID && item.State == w.related.State && item.Version == w.related.Version {
					found = true
				}
			}
			if !found {
				return errors.New("formal related Task candidate absent")
			}
			w.candidateReads++
		}
	}
	return nil
}
func (w *taskBlockerManagementWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "task-blocker-management.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_TASK_BLOCKER_MANAGEMENT_WEB_DIST="+w.dist, "AGENTEAM_TASK_BLOCKER_MANAGEMENT_WEB_EVIDENCE="+w.evidence)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	w.mu.Lock()
	w.browserStarted = time.Now()
	w.mu.Unlock()
	if err = cmd.Start(); err != nil {
		t.Fatal("Task Blocker Management Node did not start")
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
	t.Logf("Task Blocker Management Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Fatal("original Task Blocker Management browser failed")
	}
}

func (w *taskBlockerManagementWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil || len(w.receipts) != 4 || len(w.intents) != 4 || w.taskReads < 5 || w.blockerReads < 5 || w.timelineReads < 5 || w.candidateReads < 1 {
		t.Fatal("Blocker Management original browser chain incomplete", w.failure)
	}
	for _, tail := range w.tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("Blocker Management handler tail open")
		}
		if !tail.returned {
			t.Fatal("Blocker Management original handler did not return")
		}
	}
	after, err := sprintCompleteSnapshotContext(ctx, w.base)
	agentAppRequire(t, err)
	if after != w.after {
		t.Fatal("Blocker final rereads changed durable business facts")
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "task-blocker-management-result.json"))
	agentAppRequire(t, err)
	var result struct {
		Completed bool `json:"completed"`
		Writes    int  `json:"writes"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Completed || result.Writes != 4 {
		t.Fatal("Blocker original browser consumers not retired")
	}
	t.Log("Task Blocker Management defaultApp runtime disabled; assigned Todo waiting_for_human/rely_on Add2 Resolve2; state/assignee/rank retained; exact receipts, Timeline and postimages; all original tails joined")
}

// Failure-only projection: never persist an error's arbitrary message, URL,
// response body, SQL text or credentials. The same original error is retained.
func blockerManagementSafeFailure(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "unexpected-eof"
	}
	if errors.Is(err, io.EOF) {
		return "eof"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "network-timeout"
		}
		return "network"
	}
	switch err.Error() {
	case "original App RoundTrip failed":
		return "app-roundtrip"
	case "original App response EOF/Close/length incomplete":
		return "app-response-incomplete"
	case "original App response identity invalid":
		return "app-response-identity"
	case "original App request identity repeated":
		return "app-response-duplicate"
	case "owned TLS downstream write failed":
		return "tls-downstream-write"
	case "original Sprint Start TLS EOF/Close/length incomplete":
		return "tls-response-incomplete"
	case "original Sprint Start response identity invalid":
		return "tls-response-identity"
	case "original Sprint Start handler absent":
		return "original-handler-absent"
	case "original Sprint Start handler has not returned":
		return "original-handler-open"
	}
	var sql interface{ SQLState() string }
	if errors.As(err, &sql) {
		code := sql.SQLState()
		if len(code) == 5 {
			valid := true
			for _, ch := range code {
				if !(ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'Z') {
					valid = false
				}
			}
			if valid {
				return "sql-" + code
			}
		}
		return "sql-other"
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return "child-exit"
	}
	return "other"
}
func (w *taskBlockerManagementWeb) saveFailure(t *testing.T, ctx context.Context, nodeErr error) {
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
	record := map[string]any{"observed_at_unix_ms": time.Now().UnixMilli(), "node_started_at_unix_ms": w.browserStarted.UnixMilli(), "node_returned": w.browserReturned, "node_error": blockerManagementSafeFailure(nodeErr), "browser_context": blockerManagementSafeFailure(ctx.Err()), "original_failure": blockerManagementSafeFailure(w.failure), "response_failure": w.responseFailure, "handler_returned": returned, "handler_open": open, "writes": len(w.intents), "receipts": len(w.receipts), "task_reads": w.taskReads, "blocker_reads": w.blockerReads, "timeline_reads": w.timelineReads, "candidate_reads": w.candidateReads}
	w.mu.Unlock()
	raw, err := json.MarshalIndent(record, "", "  ")
	if err == nil {
		err = os.MkdirAll(w.evidence, 0700)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(w.evidence, "task-blocker-management-go-failure.json"), raw, 0600)
	}
	if err != nil {
		t.Error("Blocker safe failure evidence could not be preserved")
	}
	t.Logf("Task Blocker Management safe failure: %s", raw)
}

func (w *taskBlockerManagementWeb) failureRequestKind(r *http.Request) string {
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
	if r.URL.Path == w.path()+"/blockers" {
		return "task-blockers"
	}
	if r.URL.Path == w.path()+"/events" {
		return "task-timeline"
	}
	return "other"
}
