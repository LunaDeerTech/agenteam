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
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Real Owner and Work HTTP shell, not default App/SPA lifecycle acceptance.
// The browser writes only Add/Resolve. Ready is a UI gate: no Transfer is sent.
func TestTaskHumanBlockerWeb(t *testing.T) {
	for _, mode := range []string{"human-blocker-wide", "human-blocker-narrow"} {
		t.Run(mode, func(t *testing.T) {
			d := newTaskTransitionFixture(t)
			d.configureProject(t)
			startSchedulerSprint(t, d)
			w := newTaskHumanBlockerWeb(t, d, mode)
			w.browser(t)
			w.shell.close(t)
			w.verify(t)
		})
	}
}

const humanBlockerDescription = "等待人工确认。\n保留 原始说明。"
const humanBlockerResolution = "已由原 Owner 核对。\n允许继续规划。"

type humanBlockerIntent struct {
	kind                     string
	key                      f.IdempotencyKey
	expected                 f.Version
	add                      *wc.TaskBlockerCreate
	resolve                  *wc.TaskBlockerResolve
	before                   wc.Task
	blockerBefore            *wc.TaskBlocker
	requestDigest, keyDigest [sha256.Size]byte
	requestID                f.ID[f.Request]
}
type humanBlockerIntentKey struct{}
type taskHumanBlockerWeb struct {
	shell                                  *taskReviewWeb
	domain                                 *taskTransitionFixture
	initial, current, foreign              wc.Task
	blocker                                *wc.TaskBlocker
	writes                                 []humanBlockerIntent
	receipts                               []wc.TaskBlockerMutation
	queryGeneration                        int64
	fixed                                  []byte
	taskReads, blockerReads, timelineReads int
}

func newTaskHumanBlockerWeb(t *testing.T, d *taskTransitionFixture, mode string) *taskHumanBlockerWeb {
	t.Helper()
	initial, err := d.taskReader.GetTask(ctxFor(t), d.base.ownerBrowser.actor, d.task.ProjectID, d.task.ID)
	firstRoundRequire(t, err)
	if initial.State != wc.TaskStateBacklog || initial.AssigneeAgentID != nil || initial.Version != 1 {
		t.Fatal("human blocker requires original unassigned backlog")
	}
	w := &taskHumanBlockerWeb{domain: d, initial: initial, current: initial.Clone()}
	other, _, _ := d.base.createProject(t, d.base.ownerBrowser.actor, "human-blocker-other")
	parent := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Preserve other Project", Description: "Independent real parent"}
	_, err = d.structure.CreateMilestone(ctxFor(t), d.base.ownerBrowser.actor, meta(t, "human-blocker-other-parent", nil), other.ID, parent)
	firstRoundRequire(t, err)
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: parent.MilestoneID, Title: "Preserve other Sprint", Description: "Independent real Sprint"}
	_, err = d.structure.CreateSprint(ctxFor(t), d.base.ownerBrowser.actor, meta(t, "human-blocker-other-sprint", nil), other.ID, sprint)
	firstRoundRequire(t, err)
	created, err := d.tasks.CreateTask(ctxFor(t), d.base.ownerBrowser.actor, meta(t, "human-blocker-other-task", nil), other.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.SprintID, Title: "Other Project Task", Description: "Original foreign content", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Preserve the original plan"})
	firstRoundRequire(t, err)
	if created.Validate() != nil || !created.Changed || created.Task.ProjectID == initial.ProjectID {
		t.Fatal("formal B Task seed absent")
	}
	w.foreign = created.Task.Clone()
	var empty bool
	firstRoundRequire(t, d.base.raw.QueryRow(ctxFor(t), `SELECT NOT EXISTS(SELECT 1 FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid) AND NOT EXISTS(SELECT 1 FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid)`, initial.ProjectID.String(), initial.ID.String()).Scan(&empty))
	if !empty {
		t.Fatal("human blocker seed already contains blocker facts")
	}
	firstRoundRequire(t, d.base.raw.QueryRow(ctxFor(t), `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid`, initial.ProjectID.String()).Scan(&w.queryGeneration))
	ready := make(chan struct{})
	w.shell = newTaskWebShell(t, d, mode, taskWebProfile{
		envPrefix: "AGENTEAM_TASK_HUMAN_BLOCKER_WEB", filePrefix: "task-human-blocker", configName: "task-human-blocker.config.js", logName: "Task Human Blocker",
		observe: func(r *http.Request) error { <-ready; return w.observe(r) }, response: func(r *http.Response) error { <-ready; return w.response(r) },
		material: func(username string) map[string]any {
			return map[string]any{"mode": mode, "cookie": d.base.ownerBrowser.cookie, "project_id": initial.ProjectID, "sprint_id": initial.SprintID, "task_id": initial.ID, "worker_id": d.agentID, "other_project_id": other.ID, "foreign_id": w.foreign.ID, "initial": initial, "description": humanBlockerDescription, "resolution": humanBlockerResolution, "input_hash": os.Getenv("AGENTEAM_TASK_HUMAN_BLOCKER_WEB_INPUT_HASH"), "route": "/" + username + "/" + d.base.project.NormalizedName + "/tasks/" + initial.ID.String()}
		},
	})
	w.fixed, err = w.fixedFacts(ctxFor(t))
	firstRoundRequire(t, err)
	close(ready)
	return w
}
func (w *taskHumanBlockerWeb) path() string {
	return "/api/v1/projects/" + w.initial.ProjectID.String() + "/tasks/" + w.initial.ID.String()
}
func (w *taskHumanBlockerWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.URL.ForceQuery || (r.URL.Path != w.path()+"/blockers" && r.URL.Path != w.path()+"/blockers/resolve") {
		return errors.New("undeclared blocker mutation, Lookup or Transfer")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	closeErr := r.Body.Close()
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if err != nil || closeErr != nil || len(raw) > 16384 || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("original blocker request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()
	if len(w.writes) != len(w.receipts) || len(w.writes) >= 2 {
		return errors.New("blocker write repeated or not confirmed")
	}
	for _, old := range w.writes {
		if key == old.key {
			return errors.New("blocker write reused original key")
		}
	}
	in := humanBlockerIntent{key: key, before: w.current.Clone(), requestDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	if len(w.writes) == 0 {
		var wire struct {
			Expected f.Version            `json:"expected_version"`
			Request  wc.TaskBlockerCreate `json:"request"`
		}
		if r.URL.Path != w.path()+"/blockers" || intakeDecode(raw, &wire) != nil || wire.Expected != w.current.Version || wire.Request.Validate() != nil || wire.Request.Type != wc.TaskBlockerWaitingForHuman || wire.Request.Description != humanBlockerDescription || wire.Request.Metadata.WaitingForHuman == nil || wire.Request.Metadata.RelyOn != nil {
			return errors.New("Add lost original version, description or waiting_for_human metadata")
		}
		var exists bool
		if err = w.domain.base.raw.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_blockers WHERE id=$1::text::uuid)`, wire.Request.BlockerID.String()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return errors.New("browser Add reused an existing Blocker identity")
		}
		in.kind, in.expected, in.add = "add", wire.Expected, &wire.Request
	} else {
		var wire struct {
			Expected f.Version             `json:"expected_version"`
			Request  wc.TaskBlockerResolve `json:"request"`
		}
		var comment *string
		if w.shell.mode == "human-blocker-wide" {
			v := humanBlockerResolution
			comment = &v
		}
		if r.URL.Path != w.path()+"/blockers/resolve" || intakeDecode(raw, &wire) != nil || wire.Expected != w.current.Version || wire.Request.Validate() != nil || w.blocker == nil || wire.Request.BlockerID != w.blocker.ID || !reflect.DeepEqual(wire.Request.ResolutionComment, comment) {
			return errors.New("Resolve lost original version, blocker or exact comment/null")
		}
		before := w.blocker.Clone()
		in.kind, in.expected, in.resolve, in.blockerBefore = "resolve-blocker", wire.Expected, &wire.Request, &before
	}
	w.writes = append(w.writes, in)
	w.shell.keys = append(w.shell.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), humanBlockerIntentKey{}, in))
	return nil
}
func (w *taskHumanBlockerWeb) response(r *http.Response) error {
	raw, err := w.shell.completedResponse(r)
	if err != nil {
		return err
	}
	if r.StatusCode != http.StatusOK {
		return errors.New("original blocker/read HTTP did not succeed")
	}
	in, write := r.Request.Context().Value(humanBlockerIntentKey{}).(humanBlockerIntent)
	if !write {
		return w.observeRead(r.Request, raw)
	}
	w.shell.mu.Lock()
	tail := w.shell.tails[r.Header.Get("X-Request-ID")]
	returned := tail != nil && tail.returned
	w.shell.mu.Unlock()
	if !returned {
		return errors.New("original blocker write handler did not return")
	}
	select {
	case <-tail.done:
	default:
		return errors.New("original blocker write handler tail still open")
	}
	in.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
	if err != nil {
		return err
	}
	var result wc.TaskBlockerMutation
	if err = json.Unmarshal(raw, &result); err != nil {
		return err
	}
	w.shell.mu.Lock()
	index := len(w.receipts)
	w.shell.mu.Unlock()
	if err = w.commandFacts(r.Request.Context(), in, result, index, true); err != nil {
		return err
	}
	w.shell.mu.Lock()
	w.writes[index] = in
	w.receipts = append(w.receipts, result.Clone())
	w.current = result.Task.Clone()
	b := result.Blocker.Clone()
	w.blocker = &b
	w.shell.mu.Unlock()
	record := map[string]any{"input_hash": w.shell.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": returned, "kind": in.kind, "request_body_sha256": fmt.Sprintf("%x", in.requestDigest), "idempotency_key_sha256": fmt.Sprintf("%x", in.keyDigest), "committed_verified": true}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.shell.evidence, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.shell.evidence, "task-human-blocker-"+w.shell.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}
func (w *taskHumanBlockerWeb) commandFacts(ctx context.Context, in humanBlockerIntent, result wc.TaskBlockerMutation, index int, canonical bool) error {
	if result.Validate() != nil || in.expected != in.before.Version {
		return errors.New("invalid original blocker receipt/version")
	}
	expected := in.before.Clone()
	expected.Version++
	expected.UpdatedAt = result.Task.UpdatedAt
	if expected.UpdatedAt.Time().Before(in.before.UpdatedAt.Time()) || !reflect.DeepEqual(expected, result.Task) {
		return errors.New("blocker write changed Task content/state/assignee/rank")
	}
	user, err := f.ParseID[i.User](w.domain.base.ownerBrowser.actor.Details().UserID)
	if err != nil {
		return err
	}
	actor := wc.TaskEventActor{Type: i.Human, UserID: user, Source: "task_domain"}
	var expectedBlocker wc.TaskBlocker
	command := wc.TaskBlockerCommandAdd
	if in.add != nil {
		expectedBlocker = wc.TaskBlocker{ID: in.add.BlockerID, ProjectID: w.initial.ProjectID, TaskID: w.initial.ID, Type: wc.TaskBlockerWaitingForHuman, Description: in.add.Description, Metadata: in.add.Metadata.Clone(), CreatedAt: expected.UpdatedAt, CreatedBy: actor}
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
		digest, err = wc.TaskBlockerAddDigest(w.domain.base.ownerBrowser.actor, meta, w.initial.ProjectID, w.initial.ID, *in.add)
	} else {
		digest, err = wc.TaskBlockerResolveDigest(w.domain.base.ownerBrowser.actor, meta, w.initial.ProjectID, w.initial.ID, *in.resolve)
	}
	if err != nil {
		return err
	}
	var commandID, historyID, eventID, userID, semantic string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.domain.base.raw.QueryRow(ctx, `SELECT id::text,task_event_id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid AND command_name=$2 AND idempotency_key=$3 AND state='completed' AND committed_at IS NOT NULL`, w.initial.ProjectID.String(), string(command), string(in.key)).Scan(&commandID, &historyID, &eventID, &userID, &semantic, &requestRaw, &receiptRaw, &planRaw, &revision)
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
	payload := wc.TaskBlockerEventPayload{Added: &wc.TaskBlockerAddedPayload{BlockerID: result.Blocker.ID, BlockerType: wc.TaskBlockerWaitingForHuman}}
	if in.resolve != nil {
		eventType, change = wc.TaskBlockerEventResolved, wc.TaskBlockerResolvedChange
		payload = wc.TaskBlockerEventPayload{Resolved: &wc.TaskBlockerResolvedPayload{BlockerID: result.Blocker.ID, BlockerType: wc.TaskBlockerWaitingForHuman, ResolutionComment: in.resolve.ResolutionComment}}
	}
	expectedHistory := wc.TaskBlockerEvent{ID: result.TaskEventID, ProjectID: w.initial.ProjectID, TaskID: w.initial.ID, TaskVersion: expected.Version, Type: eventType, Actor: actor, OperationID: op, CorrelationID: op, Payload: payload, CreatedAt: expected.UpdatedAt}
	expectedPayload := wc.TaskBlockersChanged{OperationID: op, ActorUserID: user, TaskEventID: result.TaskEventID, BlockerID: result.Blocker.ID, Change: change}
	h := plan.Header
	if plan.Event.Validate() != nil || !reflect.DeepEqual(plan.Event, expectedHistory) || !reflect.DeepEqual(plan.Payload, expectedPayload) || h.Validate() != nil || h.EventID != result.EventIDs[0] || h.EventType != wc.TaskBlockersChangedName || h.AggregateType != wc.TaskAggregate || h.AggregateID.String() != w.initial.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != expected.Version || h.AggregateSequence != nil || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.initial.ProjectID.String() || h.SchemaVersion != wc.TaskBlockerSchemaVersion || h.OccurredAt != expected.UpdatedAt {
		return errors.New("blocker plan history/Outbox differs from independently expected facts")
	}
	var historyRaw, headerRaw, payloadRaw []byte
	var historyColumns, eventColumns bool
	err = w.domain.base.raw.QueryRow(ctx, `SELECT operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND failure_operation_id IS NULL,
 jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'task_version',task_version::text,'type',type,'actor',actor,'operation_id',blocker_operation_id,'correlation_id',correlation_id,'payload',payload,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) FROM agenteam_work.task_events WHERE id=$1::text::uuid AND project_id=$2::text::uuid AND task_id=$3::text::uuid AND blocker_operation_id=$4::text::uuid`, historyID, w.initial.ProjectID.String(), w.initial.ID.String(), commandID).Scan(&historyColumns, &historyRaw)
	if err != nil {
		return err
	}
	var storedHistory wc.TaskBlockerEvent
	if !historyColumns || json.Unmarshal(historyRaw, &storedHistory) != nil || !reflect.DeepEqual(storedHistory, expectedHistory) {
		return errors.New("canonical Task history differs from original blocker plan")
	}
	err = w.domain.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.task_blockers_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.task' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5 FROM agenteam_outbox.events WHERE id=$1::text::uuid AND project_id=$2::text::uuid AND aggregate_id=$3::text::uuid AND producer='work'`, eventID, w.initial.ProjectID.String(), w.initial.ID.String(), int64(expected.Version), expected.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil {
		return err
	}
	actualHeader, err := eventc.DecodeHeader(headerRaw)
	var actualPayload wc.TaskBlockersChanged
	if err != nil || !eventColumns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, expectedPayload) {
		return errors.New("canonical blocker Outbox differs from original plan")
	}
	if canonical {
		current, err := w.domain.taskReader.GetTask(ctx, w.domain.base.ownerBrowser.actor, w.initial.ProjectID, w.initial.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, expected) {
			return errors.New("canonical Task differs from blocker receipt")
		}
		var blockerRaw []byte
		var operations bool
		var generation int64
		err = w.domain.base.raw.QueryRow(ctx, `SELECT jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'type',type,'description',description,'metadata',metadata,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'created_by',created_by,'resolved_at',CASE WHEN resolved_at IS NULL THEN NULL ELSE to_char(resolved_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END,'resolved_by',resolved_by,'resolution_comment',resolution_comment),failure_operation_id IS NULL AND resolved_transition_operation_id IS NULL AND created_operation_id=(SELECT id FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid AND command_name='work.task.blocker.add' AND receipt->'blocker'->>'id'=$3::text) AND CASE WHEN $4 THEN resolved_operation_id=$5::text::uuid ELSE resolved_operation_id IS NULL END FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND id=$3::text::uuid`, w.initial.ProjectID.String(), w.initial.ID.String(), result.Blocker.ID.String(), in.resolve != nil, commandID).Scan(&blockerRaw, &operations)
		if err != nil {
			return err
		}
		var actual wc.TaskBlocker
		if !operations || json.Unmarshal(blockerRaw, &actual) != nil || !reflect.DeepEqual(actual, expectedBlocker) {
			return errors.New("canonical blocker differs from full original receipt/operation ownership")
		}
		err = w.domain.base.raw.QueryRow(ctx, `SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1::text::uuid`, w.initial.ProjectID.String()).Scan(&generation)
		if err != nil {
			return err
		}
		if generation != w.queryGeneration+int64(index)+1 {
			return errors.New("blocker query generation did not advance exactly once")
		}
	}
	return nil
}
func (w *taskHumanBlockerWeb) observeRead(r *http.Request, raw []byte) error {
	if r.Method != http.MethodGet {
		return errors.New("undeclared non-GET response")
	}
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()
	switch r.URL.Path {
	case w.path():
		var task wc.Task
		if json.Unmarshal(raw, &task) != nil || !reflect.DeepEqual(task, w.current) {
			return errors.New("current HTTP Task differs from original blocker postimage")
		}
		w.taskReads++
	case w.path() + "/blockers":
		q := r.URL.Query()
		if len(q) != 2 || q.Get("limit") != "50" || q.Get("status") != "all" {
			return errors.New("blocker read changed exact all-status query")
		}
		var page f.Page[wc.TaskBlocker]
		want := []wc.TaskBlocker{}
		if w.blocker != nil {
			want = append(want, w.blocker.Clone())
		}
		if json.Unmarshal(raw, &page) != nil || page.NextCursor != "" || !reflect.DeepEqual(page.Items, want) {
			return errors.New("current HTTP blockers differ from original full receipt")
		}
		w.blockerReads++
	case w.path() + "/events":
		q := r.URL.Query()
		if len(q) != 2 || q.Get("limit") != "50" || q.Get("order") != "desc" {
			return errors.New("timeline changed original recent-all query")
		}
		var page f.Page[wc.TaskTimelineEvent]
		if json.Unmarshal(raw, &page) != nil || page.Items == nil || page.NextCursor != "" || len(page.Items) != 1+len(w.receipts) {
			return errors.New("blocker timeline lost exact event cardinality")
		}
		rows, err := w.domain.base.raw.Query(r.Context(), `SELECT operation_id::text,blocker_operation_id::text,transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND failure_operation_id IS NULL,
 jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'task_version',task_version::text,'type',type,'actor',actor,'operation_id',coalesce(operation_id,blocker_operation_id),'correlation_id',correlation_id,'payload',payload,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid ORDER BY created_at DESC,id DESC`, w.initial.ProjectID.String(), w.initial.ID.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		expected := []wc.TaskTimelineEvent{}
		for rows.Next() {
			var planning, blocker *string
			var source bool
			var raw []byte
			if err = rows.Scan(&planning, &blocker, &source, &raw); err != nil {
				return err
			}
			var event wc.TaskTimelineEvent
			if !source {
				return errors.New("unexpected timeline operation family")
			}
			if planning != nil && blocker == nil {
				var value wc.TaskEvent
				if err = json.Unmarshal(raw, &value); err != nil {
					return err
				}
				event.Planning = &value
			} else if blocker != nil && planning == nil {
				var value wc.TaskBlockerEvent
				if err = json.Unmarshal(raw, &value); err != nil {
					return err
				}
				event.Blocker = &value
			} else {
				return errors.New("ambiguous timeline operation family")
			}
			if event.Validate() != nil {
				return errors.New("invalid stored timeline record")
			}
			expected = append(expected, event)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		a, err := json.Marshal(page.Items)
		if err != nil {
			return err
		}
		b, err := json.Marshal(expected)
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			return errors.New("HTTP timeline differs from independent full persisted facts")
		}
		w.timelineReads++
	}
	return nil
}
func (w *taskHumanBlockerWeb) fixedFacts(ctx context.Context) ([]byte, error) {
	var raw []byte
	err := w.domain.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'projects',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_project.projects p),
 'agents',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_agent.agents a),
 'agent_commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_agent.commands c),
 'milestones',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_work.milestones m),
 'sprints',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_work.sprints s),
 'structure_commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.structure_commands c),
 'starts',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.sprint_start_commands c),
 'milestone_order',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id) FROM agenteam_work.milestone_order_groups g),
 'sprint_order',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id,milestone_id) FROM agenteam_work.sprint_order_groups g),
 'target_fixed',(SELECT to_jsonb(t)-'version'-'updated_at' FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid AND id=$2::text::uuid),
 'other_tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE id<>$2::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_commands c),
 'transitions',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_transition_commands c),
 'other_blocker_commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY project_id,id) FROM agenteam_work.task_blocker_commands c WHERE project_id<>$1::text::uuid OR request->>'task_id'<>$2::text),
 'other_blockers',(SELECT jsonb_agg(to_jsonb(b) ORDER BY id) FROM agenteam_work.task_blockers b WHERE task_id<>$2::text::uuid),
 'old_events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY project_id,id) FROM agenteam_work.task_events e WHERE task_id<>$2::text::uuid OR blocker_operation_id IS NULL),
 'other_generations',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id) FROM agenteam_work.task_query_generations g WHERE project_id<>$1::text::uuid),
 'orders',(SELECT jsonb_agg(to_jsonb(g) ORDER BY project_id,sprint_id,state,priority) FROM agenteam_work.task_order_groups g),
 'old_outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE aggregate_id<>$2::text::uuid OR event_type<>'work.task_blockers_changed'),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a),
 'dispatches',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM agenteam_scheduler.dispatches d),
 'executions',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_execution.executions e))`, w.initial.ProjectID.String(), w.initial.ID.String()).Scan(&raw)
	return raw, err
}
func (w *taskHumanBlockerWeb) verify(t *testing.T) {
	t.Helper()
	w.shell.mu.Lock()
	failure := w.shell.failure
	writes := append([]humanBlockerIntent(nil), w.writes...)
	receipts := append([]wc.TaskBlockerMutation(nil), w.receipts...)
	current := w.current.Clone()
	gets, blockers, timeline := w.taskReads, w.blockerReads, w.timelineReads
	tails := make([]*reviewWebTail, 0, len(w.shell.tails))
	for _, tail := range w.shell.tails {
		tails = append(tails, tail)
	}
	w.shell.mu.Unlock()
	if failure != nil || w.shell.cut != 0 || len(writes) != 2 || len(receipts) != 2 || gets < 3 || blockers < 3 || timeline < 3 {
		t.Fatal("human blocker original browser chain incomplete", failure, gets, blockers, timeline)
	}
	for _, tail := range tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("original blocker HTTP handler remains")
		}
		if !tail.returned {
			t.Fatal("original blocker HTTP handler did not return")
		}
	}
	for n, in := range writes {
		firstRoundRequire(t, w.commandFacts(ctxFor(t), in, receipts[n], n, n == 1))
	}
	if current.Version != w.initial.Version+2 || current.State != w.initial.State || current.AssigneeAgentID != nil || current.ManualRank != w.initial.ManualRank || receipts[0].Blocker.ResolvedAt != nil || receipts[1].Blocker.ResolvedAt == nil {
		t.Fatal("two blocker writes changed Task lifecycle or lost add/resolve")
	}
	var facts [6]int64
	firstRoundRequire(t, w.domain.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_work.task_blocker_commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND resolved_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND blocker_operation_id IS NOT NULL),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND event_type='work.task_blockers_changed'),
 (SELECT count(*) FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid)`, w.initial.ProjectID.String(), w.initial.ID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5]))
	if facts != ([6]int64{2, 1, 1, 2, 2, 0}) {
		t.Fatal("blocker cancellation/read duplicated writes/history or sent a Transfer")
	}
	after, err := w.fixedFacts(ctxFor(t))
	firstRoundRequire(t, err)
	if !bytes.Equal(after, w.fixed) {
		t.Fatal("blocker writes changed Task ordering/content, original Project/Current/Agent, or B facts")
	}
	t.Logf("Task Human Blocker real facts mode=%s writes=2 versions=1,2,3 transfers=0 B_unchanged=true Ready_UI_gate_only=true", w.shell.mode)
}
func (owner *taskHumanBlockerWeb) browser(t *testing.T) {
	w := owner.shell
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	firstRoundRequire(t, err)
	// The browser owns its declared budget; ctxFor is a shorter DB operation budget.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, w.profile.configName))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME":
			continue
		}
		if key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for _, variable := range []struct{ key, directory string }{{"TMPDIR", "tmp"}, {"TMP", "tmp"}, {"TEMP", "tmp"}, {"NODE_COMPILE_CACHE", "node-compile-cache"}, {"PWTEST_CACHE_DIR", "playwright-cache"}, {"XDG_CACHE_HOME", "xdg-cache"}} {
		directory := filepath.Join(w.directory, variable.directory)
		firstRoundRequire(t, os.MkdirAll(directory, 0700))
		cmd.Env = append(cmd.Env, variable.key+"="+directory)
	}
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", w.profile.envPrefix+"_DIST="+os.Getenv(w.profile.envPrefix+"_DIST"), w.profile.envPrefix+"_CASE="+w.mode, w.profile.envPrefix+"_EVIDENCE="+w.evidence, w.profile.envPrefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal("owned Playwright did not start")
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
	t.Logf("%s Node actual_wait pid=%d success=%t", w.profile.logName, cmd.Process.Pid, err == nil)
	if err != nil {
		ctxErr := ctx.Err()
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		t.Logf("%s Node exit deadline=%t cancelled=%t exit_code=%d", w.profile.logName, errors.Is(ctxErr, context.DeadlineExceeded), errors.Is(ctxErr, context.Canceled), exitCode)
	}
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.domain.base.ownerBrowser.cookie, w.domain.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("original Playwright child failed and was waited")
	}
}
