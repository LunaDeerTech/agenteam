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
	"path/filepath"
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The original Owner creates a real unassigned backlog during setup. Only the
// browser edits or readies it; no Task/command/history facts are SQL-seeded.
func TestTaskEditWeb(t *testing.T) {
	for _, mode := range []string{"changed-noop-clear-and-ready", "lost-edit-confirmation-and-ready"} {
		t.Run(mode, func(t *testing.T) {
			d := newTaskTransitionFixture(t)
			d.configureProject(t)
			startSchedulerSprint(t, d)
			w := newTaskEditWeb(t, d, mode)
			w.shell.browser(t)
			w.shell.close(t)
			w.verify(t)
		})
	}
}

type editWebInput struct {
	Expected f.Version           `json:"expected_version"`
	Request  wc.TaskFieldsUpdate `json:"request"`
}
type editWebObservation struct {
	kind          string
	key           f.IdempotencyKey
	input         editWebInput
	before        wc.Task
	transfer      wc.TaskTransfer
	requestDigest [sha256.Size]byte
	keyDigest     [sha256.Size]byte
}
type editWebObservationKey struct{}
type taskEditWeb struct {
	shell                *taskReviewWeb
	domain               *taskTransitionFixture
	initial, current     wc.Task
	inputs               []editWebInput
	keys                 []f.IdempotencyKey
	receipts             []wc.TaskMutation
	ready                wc.TaskTransitionMutation
	lookups, assignments int
	confirmed            bool
}

func newTaskEditWeb(t *testing.T, d *taskTransitionFixture, mode string) *taskEditWeb {
	t.Helper()
	initial, err := d.taskReader.GetTask(ctxFor(t), d.base.ownerBrowser.actor, d.base.project.ID, d.task.ID)
	firstRoundRequire(t, err)
	if initial.State != wc.TaskStateBacklog || initial.AssigneeAgentID != nil || initial.Version != 1 || initial.Description == "" || initial.Plan == "" {
		t.Fatal("edit setup did not produce the original unassigned backlog")
	}
	title, description, plan, empty := initial.Title+" — edited", "Browser saved description.", "Browser saved plan.", ""
	kind, priority := wc.TaskTypeFeature, wc.TaskPriorityHigh
	inputs := []editWebInput{
		{1, wc.TaskFieldsUpdate{Title: &title, Description: &description, Type: &kind, Priority: &priority, Plan: &plan}},
		{2, wc.TaskFieldsUpdate{Title: &title}},
		{2, wc.TaskFieldsUpdate{Description: &empty, Plan: &empty}},
	}
	if mode == "lost-edit-confirmation-and-ready" {
		inputs = []editWebInput{{1, wc.TaskFieldsUpdate{Title: &title, Description: &empty}}}
	}
	w := &taskEditWeb{domain: d, initial: initial, current: initial.Clone(), inputs: inputs, confirmed: true}
	ready := make(chan struct{})
	w.shell = newTaskWebShell(t, d, mode, taskWebProfile{
		envPrefix: "AGENTEAM_TASK_EDIT_WEB", filePrefix: "task-edit", configName: "task-edit.config.js", logName: "Task Edit",
		observe:  func(r *http.Request) error { <-ready; return w.observeRequest(r) },
		response: func(r *http.Response) error { <-ready; return w.response(r) },
		material: func(username string) map[string]any {
			return map[string]any{"mode": mode, "cookie": d.base.ownerBrowser.cookie, "project_id": initial.ProjectID, "sprint_id": initial.SprintID, "task_id": initial.ID, "worker_id": d.agentID, "route": "/" + username + "/" + d.base.project.NormalizedName + "/tasks", "initial": map[string]string{"title": initial.Title, "description": initial.Description, "plan": initial.Plan}}
		},
	})
	// The shell performs no synchronous HTTP callback while constructing. Publish
	// all fields before either callback can read this original owner.
	close(ready)
	return w
}

func (w *taskEditWeb) observeRequest(r *http.Request) error {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		return nil
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	w.shell.mu.Lock()
	w.shell.keys = append(w.shell.keys, string(key))
	w.shell.mu.Unlock()
	raw, err := io.ReadAll(io.LimitReader(r.Body, (512<<10)+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 512<<10 || key.Validate() != nil {
		return errors.New("original edit intent incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	prefix := "/api/v1/projects/" + w.initial.ProjectID.String()
	path := prefix + "/tasks/" + w.initial.ID.String()
	var observation editWebObservation
	switch {
	case r.Method == http.MethodPatch && r.URL.Path == path:
		var input editWebInput
		if err := intakeDecode(raw, &input); err != nil {
			return err
		}
		w.shell.mu.Lock()
		index := len(w.keys)
		valid := w.confirmed && index < len(w.inputs) && len(w.receipts) == index && reflect.DeepEqual(input, w.inputs[index])
		for _, old := range w.keys {
			if old == key {
				valid = false
			}
		}
		before := w.current.Clone()
		if valid {
			w.keys = append(w.keys, key)
			w.confirmed = false
		}
		w.shell.mu.Unlock()
		if !valid {
			return errors.New("edit did not preserve exact version/presence or was retransmitted")
		}
		current, err := w.domain.taskReader.GetTask(r.Context(), w.domain.base.ownerBrowser.actor, w.initial.ProjectID, w.initial.ID)
		if err != nil || !reflect.DeepEqual(current, before) {
			return errors.New("edit did not start from the confirmed original Task")
		}
		observation = editWebObservation{kind: "update", key: key, input: input, before: before}
	case r.Method == http.MethodPost && r.URL.Path == prefix+"/task-commands/lookup":
		var envelope struct {
			Command  wc.TaskCommandName  `json:"command"`
			Target   wc.TaskID           `json:"target_id"`
			Expected f.Version           `json:"expected_version"`
			Request  wc.TaskFieldsUpdate `json:"request"`
		}
		if err := intakeDecode(raw, &envelope); err != nil {
			return err
		}
		w.shell.mu.Lock()
		valid := w.shell.mode == "lost-edit-confirmation-and-ready" && w.lookups == 0 && len(w.keys) == 1 && len(w.receipts) == 1 && !w.confirmed && w.shell.cut == 1 && key == w.keys[0] && envelope.Command == wc.TaskCommandUpdate && envelope.Target == w.initial.ID && envelope.Expected == w.inputs[0].Expected && reflect.DeepEqual(envelope.Request, w.inputs[0].Request)
		if valid {
			w.lookups++
		}
		w.shell.mu.Unlock()
		if !valid {
			return errors.New("Lookup changed original edit key, version or full presence patch")
		}
		observation = editWebObservation{kind: "lookup", key: key}
	case r.Method == http.MethodPost && r.URL.Path == path+"/transfer":
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
		w.shell.mu.Lock()
		before := w.current.Clone()
		valid := w.confirmed && len(w.receipts) == len(w.inputs) && w.assignments == 0 && envelope.Expected == before.Version && request.TargetState == wc.TaskStateTodo && request.AssigneeAgentID != nil && *request.AssigneeAgentID == w.domain.agentID && request.Comment == nil && len(request.AddBlockers) == 0 && len(request.ResolveBlockerIDs) == 0
		for _, old := range w.keys {
			if old == key {
				valid = false
			}
		}
		if valid {
			w.assignments++
		}
		w.shell.mu.Unlock()
		if !valid {
			return errors.New("ready was not a separate confirmed original intent")
		}
		current, err := w.domain.taskReader.GetTask(r.Context(), w.domain.base.ownerBrowser.actor, w.initial.ProjectID, w.initial.ID)
		if err != nil || !reflect.DeepEqual(current, before) {
			return errors.New("ready did not start from confirmed edited backlog")
		}
		observation = editWebObservation{kind: "ready", key: key, before: before, transfer: request.Clone()}
	default:
		return errors.New("unexpected edit fixture mutation")
	}
	// Bind these original bytes and key before forwarding; response proof must
	// not reconstruct the browser's presence-sensitive command.
	observation.requestDigest = sha256.Sum256(raw)
	observation.keyDigest = sha256.Sum256([]byte(key))
	*r = *r.WithContext(context.WithValue(r.Context(), editWebObservationKey{}, observation))
	return nil
}

func (w *taskEditWeb) response(r *http.Response) error {
	raw, err := w.shell.completedResponse(r)
	if err != nil {
		return err
	}
	o, ok := r.Request.Context().Value(editWebObservationKey{}).(editWebObservation)
	if !ok {
		return nil
	}
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("original edit %s returned HTTP %d", o.kind, r.StatusCode)
	}
	id := r.Header.Get("X-Request-ID")
	requestID, err := f.ParseID[f.Request](id)
	if err != nil || len(r.Header.Values("X-Request-ID")) != 1 || requestID.String() != id {
		return errors.New("original edit response identity invalid")
	}
	meta := f.CommandMeta{RequestID: requestID, IdempotencyKey: o.key}
	switch o.kind {
	case "update":
		var receipt wc.TaskMutation
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return err
		}
		meta.ExpectedVersion = &o.input.Expected
		if err := w.updateFacts(r.Request.Context(), meta, o, receipt); err != nil {
			return err
		}
		w.shell.mu.Lock()
		w.current = receipt.Task.Clone()
		w.receipts = append(w.receipts, receipt.Clone())
		lost := w.shell.mode == "lost-edit-confirmation-and-ready"
		w.confirmed = !lost
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
		valid := found.Status == wc.LookupCommitted && found.Receipt != nil && reflect.DeepEqual(*found.Receipt, w.receipts[0])
		if valid {
			w.confirmed = true
		}
		w.shell.mu.Unlock()
		if !valid {
			return errors.New("Lookup did not return exact original edit receipt")
		}
	case "ready":
		receipt, err := wc.DecodeTaskTransitionMutation(raw)
		if err != nil {
			return err
		}
		meta.ExpectedVersion = &o.before.Version
		digest, err := wc.TaskTransferDigest(w.domain.base.ownerBrowser.actor, meta, w.initial.ProjectID, w.initial.ID, o.transfer)
		if err != nil {
			return err
		}
		var matches bool
		if err = w.domain.base.raw.QueryRow(r.Request.Context(), `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND idempotency_key=$3 AND actor_user_id=$4::text::uuid AND semantic_digest=$5 AND state='completed')`, w.initial.ProjectID.String(), w.initial.ID.String(), string(o.key), w.domain.base.ownerBrowser.actor.Details().UserID, digest.String()).Scan(&matches); err != nil {
			return err
		}
		if !matches {
			return errors.New("ready lost original actor/key/semantic identity")
		}
		factsDomain := *w.domain
		factsDomain.task = o.before.Clone()
		stored, err := factsDomain.committedFacts(r.Request.Context(), w.domain.base.raw, o.key)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(stored, receipt) {
			return errors.New("ready response differs from actual Task/history/Outbox")
		}
		w.shell.mu.Lock()
		w.ready = receipt.Clone()
		w.shell.mu.Unlock()
	default:
		return errors.New("unexpected edit response kind")
	}
	return w.committedProof(r, raw, o)
}

func (w *taskEditWeb) committedProof(r *http.Response, raw []byte, observation editWebObservation) error {
	// completedResponse has joined the original verified-TLS EOF/Close and
	// handler. The exact command's stored receipt/facts have also matched above.
	// The declared lost Update returns reviewWebDrop before reaching this point.
	validMethod := observation.kind == "update" && r.Request.Method == http.MethodPatch ||
		(observation.kind == "ready" || observation.kind == "lookup") && r.Request.Method == http.MethodPost
	if !validMethod || r.StatusCode != http.StatusOK {
		return errors.New("edit proof requires the original successful command response")
	}
	id := r.Header.Get("X-Request-ID")
	w.shell.mu.Lock()
	tail := w.shell.tails[id]
	returned := tail != nil && tail.returned
	w.shell.mu.Unlock()
	if !returned {
		return errors.New("original edit handler did not return")
	}
	select {
	case <-tail.done:
	default:
		return errors.New("original edit handler tail still open")
	}
	record := struct {
		InputHash         string `json:"input_hash"`
		RequestID         string `json:"request_id"`
		Method            string `json:"method"`
		URLDigest         string `json:"url_sha256"`
		Status            int    `json:"status"`
		Length            int    `json:"content_length"`
		Digest            string `json:"body_sha256"`
		OriginalEOF       bool   `json:"original_eof"`
		OriginalClosed    bool   `json:"original_closed"`
		HandlerReturned   bool   `json:"handler_returned"`
		Kind              string `json:"kind"`
		RequestBodyDigest string `json:"request_body_sha256"`
		IdempotencyDigest string `json:"idempotency_key_sha256"`
		CommittedVerified bool   `json:"committed_verified"`
	}{
		InputHash: w.shell.inputHash, RequestID: id, Method: r.Request.Method,
		URLDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))),
		Status:    r.StatusCode, Length: len(raw), Digest: fmt.Sprintf("%x", sha256.Sum256(raw)),
		OriginalEOF: true, OriginalClosed: true, HandlerReturned: returned,
		Kind: observation.kind, RequestBodyDigest: fmt.Sprintf("%x", observation.requestDigest),
		IdempotencyDigest: fmt.Sprintf("%x", observation.keyDigest), CommittedVerified: true,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(w.shell.evidence, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.shell.evidence, w.shell.profile.filePrefix+"-"+w.shell.mode+"-response-"+id+".json"), encoded, 0600)
}

func (w *taskEditWeb) updateFacts(ctx context.Context, meta f.CommandMeta, o editWebObservation, receipt wc.TaskMutation) error {
	if receipt.Validate() != nil {
		return errors.New("invalid actual update receipt")
	}
	want := o.before.Clone()
	u := o.input.Request
	fields := []string{}
	if u.Description != nil && *u.Description != want.Description {
		want.Description = *u.Description
		fields = append(fields, "description")
	}
	if u.Plan != nil && *u.Plan != want.Plan {
		want.Plan = *u.Plan
		fields = append(fields, "plan")
	}
	if u.Priority != nil && *u.Priority != want.Priority {
		want.Priority = *u.Priority
		want.ManualRank = receipt.Task.ManualRank
		fields = append(fields, "priority")
	}
	if u.Title != nil && *u.Title != want.Title {
		want.Title = *u.Title
		fields = append(fields, "title")
	}
	if u.Type != nil && *u.Type != want.Type {
		want.Type = *u.Type
		fields = append(fields, "type")
	}
	changed := len(fields) != 0
	if changed {
		want.Version++
		want.UpdatedAt = receipt.Task.UpdatedAt
	}
	if receipt.Changed != changed || !reflect.DeepEqual(want, receipt.Task) {
		return errors.New("update changed omitted fields or lost change/no-op semantics")
	}
	d := w.domain
	digest, err := wc.TaskUpdateDigest(d.base.ownerBrowser.actor, meta, w.initial.ProjectID, w.initial.ID, o.input.Request)
	if err != nil {
		return err
	}
	var operation string
	var storedRaw, patchRaw []byte
	err = d.base.raw.QueryRow(ctx, `SELECT id::text,receipt,request->'update' FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.update' AND idempotency_key=$2 AND actor_user_id=$3::text::uuid AND semantic_digest=$4 AND state='completed' AND committed_at IS NOT NULL AND request->>'target_id'=$5::text AND request->>'expected_version'=$6::text AND request->'create'='null'::jsonb AND request->'reorder'='null'::jsonb`, w.initial.ProjectID.String(), string(o.key), d.base.ownerBrowser.actor.Details().UserID, digest.String(), w.initial.ID.String(), o.input.Expected.String()).Scan(&operation, &storedRaw, &patchRaw)
	if err != nil {
		return err
	}
	var stored wc.TaskMutation
	var patch wc.TaskFieldsUpdate
	if json.Unmarshal(storedRaw, &stored) != nil || json.Unmarshal(patchRaw, &patch) != nil || !reflect.DeepEqual(stored, receipt) || !reflect.DeepEqual(patch, o.input.Request) {
		return errors.New("stored update lost original presence/receipt")
	}
	var history, events int64
	err = d.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND operation_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND convert_from(payload,'UTF8')::jsonb->>'command_id'=$3::text)`, w.initial.ProjectID.String(), w.initial.ID.String(), operation).Scan(&history, &events)
	if err != nil {
		return err
	}
	count := int64(0)
	if changed {
		count = 1
	}
	if history != count || events != count {
		return errors.New("update/no-op history or Outbox cardinality differs")
	}
	if changed {
		var historyFields, eventFields []byte
		err = d.base.raw.QueryRow(ctx, `SELECT h.payload->'changed_fields',convert_from(e.payload,'UTF8')::jsonb->'changed_fields'
 FROM agenteam_work.task_events h JOIN agenteam_outbox.events e ON e.id=$5::text::uuid
 WHERE h.id=$4::text::uuid AND h.project_id=$1::text::uuid AND h.task_id=$2::text::uuid AND h.operation_id=$3::text::uuid AND h.correlation_id=$3::text::uuid AND h.task_version=$6 AND h.type='fields_updated' AND h.actor->>'user_id'=$7::text
 AND e.project_id=$1::text::uuid AND e.aggregate_id=$2::text::uuid AND e.aggregate_version=$6 AND e.producer='work' AND e.event_type='work.task_changed' AND e.schema_version=1
 AND convert_from(e.payload,'UTF8')::jsonb->>'command_id'=$3::text AND convert_from(e.payload,'UTF8')::jsonb->>'change'='updated' AND convert_from(e.payload,'UTF8')::jsonb->>'task_event_id'=$4::text`, w.initial.ProjectID.String(), w.initial.ID.String(), operation, receipt.TaskEventID.String(), receipt.EventIDs[0].String(), int64(receipt.Task.Version), d.base.ownerBrowser.actor.Details().UserID).Scan(&historyFields, &eventFields)
		if err != nil {
			return err
		}
		var hf, ef []string
		if json.Unmarshal(historyFields, &hf) != nil || json.Unmarshal(eventFields, &ef) != nil || !reflect.DeepEqual(hf, fields) || !reflect.DeepEqual(ef, fields) {
			return errors.New("update history/Outbox changed-fields differ from actual patch")
		}
	}
	current, err := d.taskReader.GetTask(ctx, d.base.ownerBrowser.actor, w.initial.ProjectID, w.initial.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, receipt.Task) {
		return errors.New("update receipt differs from canonical Task")
	}
	return nil
}

func (w *taskEditWeb) verify(t *testing.T) {
	t.Helper()
	s := w.shell
	s.mu.Lock()
	failure := s.failure
	updates, receipts, assignments, lookups, cut := len(w.keys), len(w.receipts), w.assignments, w.lookups, s.cut
	ready := w.ready.Clone()
	for _, tail := range s.tails {
		if !tail.returned {
			failure = errors.New("original edit HTTP handler did not return")
		}
	}
	s.mu.Unlock()
	if failure != nil {
		t.Fatal("owned edit observation failed", failure)
	}
	wantLookup := 0
	changed := int64(2)
	if s.mode == "lost-edit-confirmation-and-ready" {
		wantLookup = 1
		changed = 1
	}
	if updates != len(w.inputs) || receipts != updates || assignments != 1 || lookups != wantLookup || cut != wantLookup || ready.Validate() != nil {
		t.Fatal("browser edit lost separate original commands/receipts")
	}
	current, err := w.domain.taskReader.GetTask(ctxFor(t), w.domain.base.ownerBrowser.actor, w.initial.ProjectID, w.initial.ID)
	firstRoundRequire(t, err)
	if !reflect.DeepEqual(current, ready.Task) {
		t.Fatal("refreshed Task differs from committed ready")
	}
	var facts [6]int64
	err = w.domain.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.create'),
 (SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1::text::uuid AND command_name='work.task.update' AND request->>'target_id'=$2::text AND state='completed'),
 (SELECT count(*) FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid)`, w.initial.ProjectID.String(), w.initial.ID.String()).Scan(&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5])
	firstRoundRequire(t, err)
	if facts != ([6]int64{1, 1, int64(updates), 1, 3 + changed, 2 + changed}) {
		t.Fatalf("edit/no-op/separate-ready cardinality differs: %v", facts)
	}
}
