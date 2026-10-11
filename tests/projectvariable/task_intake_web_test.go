//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Setup supplies real domain objects, a Current Sprint and a current Agent.
// Only the browser creates and assigns the target Task. No execution is needed
// to prove these Human operations, and no Task/command/history is SQL-seeded.
func TestTaskIntakeWeb(t *testing.T) {
	for _, mode := range []string{"create-and-ready", "lost-create-confirmation-and-ready"} {
		t.Run(mode, func(t *testing.T) {
			d := newTaskTransitionFixture(t)
			d.configureProject(t)
			startSchedulerSprint(t, d)
			w := newTaskIntakeWeb(t, d, mode)
			w.shell.browser(t)
			w.shell.close(t)
			w.verify(t)
		})
	}
}

type intakeWebObservation struct {
	kind     string
	key      f.IdempotencyKey
	transfer wc.TaskTransfer
}
type intakeWebObservationKey struct{}
type taskIntakeWeb struct {
	shell                         *taskReviewWeb
	domain                        *taskTransitionFixture
	draft                         wc.TaskCreate // TaskID is learned from the original browser request.
	baseline                      wc.Task
	createKey, readyKey           f.IdempotencyKey
	created                       wc.TaskMutation
	ready                         wc.TaskTransitionMutation
	creates, lookups, assignments int
	confirmed                     bool
}

func newTaskIntakeWeb(t *testing.T, d *taskTransitionFixture, mode string) *taskIntakeWeb {
	t.Helper()
	baseline, err := d.taskReader.GetTask(ctxFor(t), d.base.ownerBrowser.actor, d.base.project.ID, d.task.ID)
	firstRoundRequire(t, err)
	w := &taskIntakeWeb{domain: d, baseline: baseline, draft: wc.TaskCreate{SprintID: d.task.SprintID, Title: "Browser intake: " + mode, Description: "A real backlog Task, created before assignment.", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Confirm creation, then independently select the current Agent."}}
	w.shell = newTaskWebShell(t, d, mode, taskWebProfile{
		envPrefix: "AGENTEAM_TASK_INTAKE_WEB", filePrefix: "task-intake", configName: "task-intake.config.js", logName: "Task Intake",
		observe: w.observeRequest, response: w.response,
		material: func(username string) map[string]any {
			return map[string]any{"mode": mode, "cookie": d.base.ownerBrowser.cookie, "project_id": d.base.project.ID, "sprint_id": w.draft.SprintID, "worker_id": d.agentID, "route": "/" + username + "/" + d.base.project.NormalizedName + "/tasks", "draft": map[string]any{"title": w.draft.Title, "description": w.draft.Description, "plan": w.draft.Plan, "type": w.draft.Type, "priority": w.draft.Priority}}
		},
	})
	return w
}

func intakeDecode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing browser command bytes")
	}
	return nil
}

func (w *taskIntakeWeb) observeRequest(r *http.Request) error {
	if r.Method != http.MethodPost {
		return nil
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	w.shell.mu.Lock()
	w.shell.keys = append(w.shell.keys, string(key))
	w.shell.mu.Unlock()
	raw, err := io.ReadAll(io.LimitReader(r.Body, (512<<10)+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 512<<10 || key.Validate() != nil {
		return errors.New("original intake intent incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	prefix := "/api/v1/projects/" + w.domain.base.project.ID.String()
	var observation intakeWebObservation
	switch r.URL.Path {
	case prefix + "/tasks":
		var envelope struct {
			Request wc.TaskCreate `json:"request"`
		}
		if err := intakeDecode(raw, &envelope); err != nil {
			return err
		}
		request := envelope.Request
		w.shell.mu.Lock()
		want := w.draft
		want.TaskID = request.TaskID
		valid := w.creates == 0 && request.TaskID.Validate() == nil && request.TaskID != w.baseline.ID && reflect.DeepEqual(request, want)
		if valid {
			w.draft = request.Clone()
			w.createKey = key
			w.creates++
		}
		w.shell.mu.Unlock()
		if !valid {
			return errors.New("create changed fixed draft or was retransmitted")
		}
		var count int
		if err := w.domain.base.raw.QueryRow(r.Context(), `SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, w.domain.base.project.ID.String(), request.TaskID.String()).Scan(&count); err != nil || count != 0 {
			return errors.New("browser target existed before original Create")
		}
		observation = intakeWebObservation{kind: "create", key: key}
	case prefix + "/task-commands/lookup":
		var envelope struct {
			Command wc.TaskCommandName `json:"command"`
			Request wc.TaskCreate      `json:"request"`
		}
		if err := intakeDecode(raw, &envelope); err != nil {
			return err
		}
		w.shell.mu.Lock()
		valid := w.shell.mode == "lost-create-confirmation-and-ready" && w.lookups == 0 && w.creates == 1 && w.created.Validate() == nil && w.shell.cut == 1 && key == w.createKey && envelope.Command == wc.TaskCommandCreate && reflect.DeepEqual(envelope.Request, w.draft)
		if valid {
			w.lookups++
		}
		w.shell.mu.Unlock()
		if !valid {
			return errors.New("Lookup lost the original committed create identity")
		}
		observation = intakeWebObservation{kind: "lookup", key: key}
	default:
		w.shell.mu.Lock()
		original := w.draft.Clone()
		created := w.created.Clone()
		confirmed := w.confirmed
		createKey := w.createKey
		w.shell.mu.Unlock()
		if original.TaskID.Validate() != nil || r.URL.Path != prefix+"/tasks/"+original.TaskID.String()+"/transfer" {
			return errors.New("unexpected intake mutation")
		}
		var envelope struct {
			Expected f.Version       `json:"expected_version"`
			Request  json.RawMessage `json:"request"`
		}
		if err := intakeDecode(raw, &envelope); err != nil {
			return err
		}
		request, err := wc.DecodeTaskTransfer(envelope.Request)
		if err != nil {
			return err
		}
		if !confirmed || created.Validate() != nil || envelope.Expected != 1 || key == createKey || request.TargetState != wc.TaskStateTodo || request.AssigneeAgentID == nil || *request.AssigneeAgentID != w.domain.agentID || request.Comment != nil || len(request.AddBlockers) != 0 || len(request.ResolveBlockerIDs) != 0 {
			return errors.New("assignment did not form an independent explicit intent")
		}
		current, err := w.domain.taskReader.GetTask(r.Context(), w.domain.base.ownerBrowser.actor, w.domain.base.project.ID, original.TaskID)
		if err != nil || !reflect.DeepEqual(current, created.Task) {
			return errors.New("assignment did not start from the confirmed unassigned backlog")
		}
		w.shell.mu.Lock()
		w.assignments++
		n := w.assignments
		w.readyKey = key
		w.shell.mu.Unlock()
		if n != 1 {
			return errors.New("assignment was retransmitted")
		}
		observation = intakeWebObservation{kind: "ready", key: key, transfer: request.Clone()}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), intakeWebObservationKey{}, observation))
	return nil
}

func (w *taskIntakeWeb) response(r *http.Response) error {
	raw, err := w.shell.completedResponse(r)
	if err != nil {
		return err
	}
	observation, ok := r.Request.Context().Value(intakeWebObservationKey{}).(intakeWebObservation)
	if !ok {
		return nil
	}
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("original intake %s returned HTTP %d", observation.kind, r.StatusCode)
	}
	requestID, err := f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
	if err != nil {
		return err
	}
	meta := f.CommandMeta{RequestID: requestID, IdempotencyKey: observation.key}
	switch observation.kind {
	case "create":
		var receipt wc.TaskMutation
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return err
		}
		if err := w.createdFacts(r.Request.Context(), meta, receipt); err != nil {
			return err
		}
		w.shell.mu.Lock()
		w.created = receipt.Clone()
		w.confirmed = w.shell.mode == "create-and-ready"
		lost := !w.confirmed
		w.shell.mu.Unlock()
		if lost {
			return &reviewWebDrop{r.Header.Clone(), len(raw)}
		}
	case "lookup":
		var found wc.TaskCommandLookup
		if err := json.Unmarshal(raw, &found); err != nil {
			return err
		}
		w.shell.mu.Lock()
		valid := found.Status == wc.LookupCommitted && found.Receipt != nil && reflect.DeepEqual(*found.Receipt, w.created)
		if valid {
			w.confirmed = true
		}
		w.shell.mu.Unlock()
		if !valid {
			return errors.New("Lookup did not return the complete original creation receipt")
		}
	case "ready":
		receipt, err := wc.DecodeTaskTransitionMutation(raw)
		if err != nil {
			return err
		}
		w.shell.mu.Lock()
		created := w.created.Clone()
		w.shell.mu.Unlock()
		meta.ExpectedVersion = &created.Task.Version
		digest, err := wc.TaskTransferDigest(w.domain.base.ownerBrowser.actor, meta, w.domain.base.project.ID, created.Task.ID, observation.transfer)
		if err != nil {
			return err
		}
		var matches bool
		if err = w.domain.base.raw.QueryRow(r.Request.Context(), `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND idempotency_key=$3 AND actor_user_id=$4::text::uuid AND semantic_digest=$5 AND state='completed')`, w.domain.base.project.ID.String(), created.Task.ID.String(), string(observation.key), w.domain.base.ownerBrowser.actor.Details().UserID, digest.String()).Scan(&matches); err != nil || !matches {
			return errors.New("ready command lost original actor/key/semantic identity")
		}
		factsDomain := *w.domain
		factsDomain.task = created.Task
		stored, err := factsDomain.committedFacts(r.Request.Context(), w.domain.base.raw, observation.key)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(stored, receipt) {
			return errors.New("ready response differs from original Task/history/Outbox")
		}
		current, err := w.domain.taskReader.GetTask(r.Request.Context(), w.domain.base.ownerBrowser.actor, w.domain.base.project.ID, created.Task.ID)
		if err != nil || !reflect.DeepEqual(current, receipt.Task) {
			return errors.New("ready receipt differs from canonical current Task")
		}
		w.shell.mu.Lock()
		w.ready = receipt.Clone()
		w.shell.mu.Unlock()
	}
	return nil
}

func (w *taskIntakeWeb) createdFacts(ctx context.Context, meta f.CommandMeta, receipt wc.TaskMutation) error {
	d := w.domain
	request := w.draft.Clone()
	task := receipt.Task
	if receipt.Validate() != nil || !receipt.Changed || task.ID != request.TaskID || task.ProjectID != d.base.project.ID || task.SprintID != request.SprintID || task.State != wc.TaskStateBacklog || task.AssigneeAgentID != nil || task.Version != 1 || task.Title != request.Title || task.Description != request.Description || task.Plan != request.Plan || task.Type != request.Type || task.Priority != request.Priority {
		return errors.New("Create did not produce the exact unassigned backlog")
	}
	digest, err := wc.TaskCreateDigest(d.base.ownerBrowser.actor, meta, d.base.project.ID, request)
	if err != nil {
		return err
	}
	var operation string
	var storedRaw, requestRaw []byte
	err = d.base.raw.QueryRow(ctx, `SELECT id::text,receipt,request->'create' FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.create' AND idempotency_key=$2 AND actor_user_id=$3::text::uuid AND semantic_digest=$4 AND state='completed' AND committed_at IS NOT NULL`, d.base.project.ID.String(), string(meta.IdempotencyKey), d.base.ownerBrowser.actor.Details().UserID, digest.String()).Scan(&operation, &storedRaw, &requestRaw)
	if err != nil {
		return err
	}
	var stored wc.TaskMutation
	var storedRequest wc.TaskCreate
	if json.Unmarshal(storedRaw, &stored) != nil || json.Unmarshal(requestRaw, &storedRequest) != nil || !reflect.DeepEqual(stored, receipt) || !reflect.DeepEqual(storedRequest, request) {
		return errors.New("Create receipt/request differs from canonical command")
	}
	var facts [5]int64
	err = d.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid AND id=$2::text::uuid AND state='backlog' AND assignee_agent_id IS NULL AND version=1),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND id=$4::text::uuid AND operation_id=$3::text::uuid AND correlation_id=$3::text::uuid AND task_version=1 AND type='task_created' AND actor->>'user_id'=$6::text AND payload->>'initial_state'='backlog' AND payload->>'sprint_id'=$7::text),
 (SELECT count(*) FROM agenteam_outbox.events WHERE id=$5::text::uuid AND project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND aggregate_version=1 AND producer='work' AND event_type='work.task_changed' AND schema_version=1 AND convert_from(payload,'UTF8')::jsonb->>'command_id'=$3::text AND convert_from(payload,'UTF8')::jsonb->>'change'='created' AND convert_from(payload,'UTF8')::jsonb->>'task_event_id'=$4::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`, d.base.project.ID.String(), task.ID.String(), operation, receipt.TaskEventID.String(), receipt.EventIDs[0].String(), d.base.ownerBrowser.actor.Details().UserID, task.SprintID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4])
	if err != nil {
		return err
	}
	if facts != ([5]int64{1, 1, 1, 0, 0}) {
		return fmt.Errorf("original create atomic fact counts %v", facts)
	}
	current, err := d.taskReader.GetTask(ctx, d.base.ownerBrowser.actor, d.base.project.ID, task.ID)
	if err != nil || !reflect.DeepEqual(current, receipt.Task) {
		return errors.New("Create receipt differs from canonical Task")
	}
	return nil
}

func (w *taskIntakeWeb) verify(t *testing.T) {
	t.Helper()
	s := w.shell
	s.mu.Lock()
	failure := s.failure
	creates, lookups, assignments, cut := w.creates, w.lookups, w.assignments, s.cut
	created, ready := w.created.Clone(), w.ready.Clone()
	sameKey := w.createKey == w.readyKey
	for _, tail := range s.tails {
		if !tail.returned {
			failure = errors.New("original intake HTTP handler did not return")
		}
	}
	s.mu.Unlock()
	if failure != nil {
		t.Fatal("owned intake observation failed", failure)
	}
	wantLookup := 0
	if s.mode != "create-and-ready" {
		wantLookup = 1
	}
	if creates != 1 || assignments != 1 || lookups != wantLookup || cut != wantLookup || sameKey || created.Validate() != nil || ready.Validate() != nil {
		t.Fatal("browser intake lost separate original commands/receipts")
	}
	current, err := w.domain.taskReader.GetTask(ctxFor(t), w.domain.base.ownerBrowser.actor, w.domain.base.project.ID, created.Task.ID)
	firstRoundRequire(t, err)
	if !reflect.DeepEqual(current, ready.Task) {
		t.Fatal("refreshed Task differs from committed ready receipt")
	}
	baseline, err := w.domain.taskReader.GetTask(ctxFor(t), w.domain.base.ownerBrowser.actor, w.domain.base.project.ID, w.baseline.ID)
	firstRoundRequire(t, err)
	if !reflect.DeepEqual(baseline, w.baseline) {
		t.Fatal("intake modified the preexisting setup Task")
	}
	var facts [5]int64
	err = w.domain.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid AND id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.create' AND request->'create'->>'task_id'=$2::text),
 (SELECT count(*) FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid)`, w.domain.base.project.ID.String(), created.Task.ID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4])
	firstRoundRequire(t, err)
	if facts != ([5]int64{1, 1, 1, 3, 2}) {
		t.Fatalf("intake cardinality differs from one create plus one ready: %v", facts)
	}
}
