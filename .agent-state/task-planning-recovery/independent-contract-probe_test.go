package contract_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func independentID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("01920000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal("independent ID fixture failed")
	}
	return id
}
func independentTask(t *testing.T) c.Task {
	t.Helper()
	at, err := f.NewInstant(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal("independent time fixture failed")
	}
	return c.Task{ID: independentID[c.Task](t, 1), ProjectID: independentID[i.Project](t, 2), MilestoneID: independentID[c.Milestone](t, 3), SprintID: independentID[pc.Sprint](t, 4), Title: "title", Type: c.TaskTypeTask, Priority: c.TaskPriorityMedium, State: c.TaskStateBacklog, ManualRank: strings.Repeat("7", 32), Version: 1, CreatedAt: at, UpdatedAt: at}
}
func independentJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal("fixture encoding rejected", err)
	}
	return raw
}

func TestIndependentTaskPresenceAndStrictWire(t *testing.T) {
	agent := independentID[i.Agent](t, 6)
	for _, tc := range []struct {
		raw               string
		present, assigned bool
	}{
		{`{}`, false, false}, {`{"assignee_agent_id":null}`, true, false}, {`{"assignee_agent_id":"` + agent.String() + `"}`, true, true},
	} {
		var v c.TaskFilter
		if err := v.UnmarshalJSON([]byte(tc.raw)); err != nil {
			t.Fatal("valid three-state filter rejected")
		}
		if v.AssigneeAgentID.Present != tc.present || (v.AssigneeAgentID.AgentID != nil) != tc.assigned {
			t.Error("filter presence collapsed")
		}
		if string(independentJSON(t, v)) != tc.raw {
			t.Error("filter wire presence changed")
		}
	}
	badFilters := []string{`null`, `{"State":"backlog"}`, `{"state":null}`, `{"text":"\ud800"}`, `{"text":"x","\u0074ext":"y"}`, `{"text":"x"} {}`, `{"assignee_agent_id":false}`, `{"assignee_agent_id":"00000000-0000-0000-0000-000000000000"}`}
	for _, raw := range badFilters {
		if new(c.TaskFilter).UnmarshalJSON([]byte(raw)) == nil {
			t.Error("invalid filter admitted")
		}
	}
	if (c.TaskAssigneeFilter{AgentID: &agent}).Validate() == nil {
		t.Error("absent assigned filter admitted")
	}
	task := independentTask(t)
	create := c.TaskCreate{TaskID: task.ID, SprintID: task.SprintID, Title: task.Title, Type: task.Type, Priority: task.Priority}
	raw := independentJSON(t, create)
	for _, field := range []string{"description", "plan", "initial_state", "assignee_agent_id"} {
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			t.Fatal("fixture map decode failed")
		}
		obj[field] = json.RawMessage("null")
		if new(c.TaskCreate).UnmarshalJSON(independentJSON(t, obj)) == nil {
			t.Error("create explicit null admitted")
		}
	}
	for _, raw := range []string{`{}`, `{"title":null}`, `{"description":null}`, `{"state":"backlog"}`, `{"priority":"MEDIUM"}`, `{"plan":"\udc00"}`} {
		if new(c.TaskFieldsUpdate).UnmarshalJSON([]byte(raw)) == nil {
			t.Error("invalid update admitted")
		}
	}
	var update c.TaskFieldsUpdate
	if update.UnmarshalJSON([]byte(`{"description":"","plan":""}`)) != nil || update.Description == nil || update.Plan == nil {
		t.Error("explicit clear lost")
	}
	if new(c.TaskReorder).UnmarshalJSON([]byte(`{"before_id":null}`)) == nil {
		t.Error("reorder null admitted")
	}
	original := independentJSON(t, task)
	bad := bytes.Replace(original, []byte(`"version":"1"`), []byte(`"version":1`), 1)
	if new(c.Task).UnmarshalJSON(bad) == nil {
		t.Error("numeric version admitted")
	}
	bad = bytes.Replace(original, []byte(`"title":"title"`), []byte("\"title\":\"\xff\""), 1)
	if new(c.Task).UnmarshalJSON(bad) == nil {
		t.Error("invalid UTF8 admitted")
	}
	if new(c.Task).UnmarshalJSON(append(bytes.Repeat([]byte(" "), c.MaxTaskResultBytes), original...)) == nil {
		t.Error("direct method raw cap not enforced")
	}
	for _, state := range []c.TaskState{c.TaskStateTodo, c.TaskStateInProgress, c.TaskStateInReview} {
		task.State = state
		if task.Validate() == nil {
			t.Error("active unassigned canonical task admitted")
		}
		task.AssigneeAgentID = &agent
		if task.Validate() != nil {
			t.Error("valid assigned canonical task rejected")
		}
		task.AssigneeAgentID = nil
	}
}

func TestIndependentTaskCloneIsolation(t *testing.T) {
	task := independentTask(t)
	agent := independentID[i.Agent](t, 6)
	task.AssigneeAgentID = &agent
	eventID := independentID[c.TaskEvent](t, 7)
	receipt := c.TaskMutation{Task: task, Changed: true, TaskEventID: &eventID, EventIDs: []e.EventID{independentID[e.EventIdentity](t, 8)}}
	lookup := c.TaskCommandLookup{Status: c.LookupCommitted, Receipt: &receipt}
	clone := lookup.Clone()
	*clone.Receipt.Task.AssigneeAgentID = independentID[i.Agent](t, 9)
	*clone.Receipt.TaskEventID = independentID[c.TaskEvent](t, 10)
	clone.Receipt.EventIDs[0] = independentID[e.EventIdentity](t, 11)
	clone.Receipt.Task.Plan = "changed"
	if *lookup.Receipt.Task.AssigneeAgentID != agent || *lookup.Receipt.TaskEventID != eventID || lookup.Receipt.EventIDs[0] == clone.Receipt.EventIDs[0] || lookup.Receipt.Task.Plan == "changed" {
		t.Error("lookup clone aliases mutable receipt")
	}
	title, plan := "title", "plan"
	typ := c.TaskTypeTask
	priority := c.TaskPriorityMedium
	update := c.TaskFieldsUpdate{Title: &title, Description: &plan, Type: &typ, Priority: &priority, Plan: &plan}
	copy := update.Clone()
	*copy.Title = "changed"
	*copy.Description = "changed"
	*copy.Type = c.TaskTypeBug
	*copy.Priority = c.TaskPriorityHigh
	*copy.Plan = "changed"
	if *update.Title != "title" || *update.Description != "plan" || *update.Type != c.TaskTypeTask || *update.Priority != c.TaskPriorityMedium || *update.Plan != "plan" {
		t.Error("update clone aliases pointers")
	}
	filter := c.TaskFilter{AssigneeAgentID: c.TaskAssigneeFilter{Present: true, AgentID: &agent}, Text: &plan, SprintID: &task.SprintID}
	fc := filter.Clone()
	*fc.AssigneeAgentID.AgentID = independentID[i.Agent](t, 12)
	*fc.Text = "changed"
	*fc.SprintID = independentID[pc.Sprint](t, 13)
	if *filter.AssigneeAgentID.AgentID != agent || *filter.Text != "plan" || *filter.SprintID != task.SprintID {
		t.Error("filter clone aliases pointers")
	}
}

func TestIndependentTaskMaximumEncodingAndPage(t *testing.T) {
	task := independentTask(t)
	for _, pattern := range []string{"<>&", "\"\\\t\n\r"} {
		text := strings.Repeat(pattern, 32768/len(pattern)+1)[:32768]
		task.Description = text
		task.Plan = text
		task.Title = strings.Repeat("<", 256)
		create := c.TaskCreate{TaskID: task.ID, SprintID: task.SprintID, Title: task.Title, Type: task.Type, Priority: task.Priority, Description: text, Plan: text}
		raw := independentJSON(t, create)
		var round c.TaskCreate
		if round.UnmarshalJSON(raw) != nil || round.Description != text || round.Plan != text {
			t.Error("max request changed content")
		}
		history := c.TaskCommandLookup{Status: c.LookupCommitted, Receipt: &c.TaskMutation{Task: task, Changed: false, EventIDs: []e.EventID{}}}
		raw = independentJSON(t, history)
		var lr c.TaskCommandLookup
		if len(raw) > c.MaxTaskResultBytes || lr.UnmarshalJSON(raw) != nil || lr.Receipt.Task.Plan != text || lr.Receipt.Task.Description != text {
			t.Error("max lookup encoding failed")
		}
	}
	task.Description = strings.Repeat("<", 32768)
	task.Plan = strings.Repeat("&", 32768)
	page := f.Page[c.Task]{Items: make([]c.Task, 200)}
	for n := range page.Items {
		page.Items[n] = task
	}
	raw := independentJSON(t, page)
	if len(raw) <= c.MaxTaskResultBytes || len(raw) > 200*c.MaxTaskResultBytes+16384 {
		t.Error("page size bound disagreed")
	}
	var decoded struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(raw, &decoded) != nil || len(decoded.Items) != 200 {
		t.Error("full 200 item page lost items")
	}
	var last c.Task
	if last.UnmarshalJSON(decoded.Items[199]) != nil || last.Plan != task.Plan {
		t.Error("last maximum-sized page item damaged")
	}
}

func TestIndependentTaskDigestSemantics(t *testing.T) {
	task := independentTask(t)
	user := independentID[i.User](t, 15)
	actor, err := i.NewHuman(user, independentID[i.Session](t, 16))
	if err != nil {
		t.Fatal("actor fixture failed")
	}
	meta := f.CommandMeta{RequestID: independentID[f.Request](t, 17), IdempotencyKey: "independent-key"}
	request := c.TaskCreate{TaskID: task.ID, SprintID: task.SprintID, Title: task.Title, Type: task.Type, Priority: task.Priority}
	digest, err := c.TaskCreateDigest(actor, meta, task.ProjectID, request)
	if err != nil {
		t.Fatal("digest fixture failed")
	}
	// Every scalar in this independent expected object is ASCII. encoding/json's
	// sorted map keys produce exactly canonical-v1 here without the product oracle.
	expected := map[string]any{"format": "work-task-planning-command-v1", "command": "work.task.create", "project_id": task.ProjectID.String(), "target_id": task.ID.String(), "actor_user_id": user.String(), "expected_version": nil, "request": map[string]any{"task_id": task.ID.String(), "sprint_id": task.SprintID.String(), "title": "title", "description": "", "plan": "", "type": "task", "priority": "medium", "initial_state": "backlog", "assignee_agent_id": nil}}
	sum := sha256.Sum256(independentJSON(t, expected))
	if string(digest) != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Error("digest differs from specified canonical object")
	}
	actor2, _ := i.NewHuman(user, independentID[i.Session](t, 18))
	meta2 := meta
	meta2.RequestID = independentID[f.Request](t, 19)
	meta2.IdempotencyKey = "another-key"
	request2 := request
	request2.InitialState = c.TaskStateBacklog
	d2, err := c.TaskCreateDigest(actor2, meta2, task.ProjectID, request2)
	if err != nil || d2 != digest {
		t.Error("transport/session or default presence changed semantic digest")
	}
	for _, change := range []func(*c.TaskCreate){func(r *c.TaskCreate) { r.TaskID = independentID[c.Task](t, 20) }, func(r *c.TaskCreate) { r.SprintID = independentID[pc.Sprint](t, 21) }, func(r *c.TaskCreate) { r.Title = "changed" }, func(r *c.TaskCreate) { r.Description = "changed" }, func(r *c.TaskCreate) { r.Plan = "changed" }, func(r *c.TaskCreate) { r.Type = c.TaskTypeBug }, func(r *c.TaskCreate) { r.Priority = c.TaskPriorityHigh }, func(r *c.TaskCreate) { r.InitialState = c.TaskStateTodo }, func(r *c.TaskCreate) { id := independentID[i.Agent](t, 22); r.AssigneeAgentID = &id }} {
		r := request
		change(&r)
		d, err := c.TaskCreateDigest(actor, meta, task.ProjectID, r)
		if err != nil || d == digest {
			t.Error("create field omitted from digest")
		}
	}
	v := f.Version(1)
	meta.ExpectedVersion = &v
	title, empty := "title", ""
	ud1, err := c.TaskUpdateDigest(actor, meta, task.ProjectID, task.ID, c.TaskFieldsUpdate{Title: &title})
	if err != nil {
		t.Fatal("update digest fixture failed")
	}
	ud2, err := c.TaskUpdateDigest(actor, meta, task.ProjectID, task.ID, c.TaskFieldsUpdate{Title: &title, Plan: &empty})
	if err != nil || ud1 == ud2 {
		t.Error("update field presence collapsed")
	}
	if _, err := c.TaskCreateDigest(actor, meta, task.ProjectID, request); err == nil {
		t.Error("create expected version admitted")
	}
	if _, err := c.TaskReorderDigest(actor, meta, task.ProjectID, task.ID, c.TaskReorder{BeforeID: &task.ID}); err == nil {
		t.Error("self anchor admitted")
	}
	meta.ExpectedVersion = nil
	if _, err := c.TaskUpdateDigest(actor, meta, task.ProjectID, task.ID, c.TaskFieldsUpdate{Title: &title}); err == nil {
		t.Error("update missing expected admitted")
	}
}

func TestIndependentTaskEventsAndHeader(t *testing.T) {
	task := independentTask(t)
	catalog := e.NewCatalog()
	events, err := c.RegisterTaskEvents(catalog)
	if err != nil {
		t.Fatal("catalog fixture failed")
	}
	if _, err := c.RegisterTaskEvents(catalog); err == nil {
		t.Error("duplicate schema registered")
	}
	version := f.Version(1)
	header := e.Header{EventID: independentID[e.EventIdentity](t, 30), EventType: c.TaskChangedName, SchemaVersion: 1, OccurredAt: task.UpdatedAt, Scope: e.Scope{Kind: e.ProjectScope, ProjectID: independentID[e.Project](t, 2)}, AggregateType: c.TaskAggregate, AggregateID: independentID[e.Aggregate](t, 1), AggregateVersion: &version}
	p := c.TaskChanged{CommandID: independentID[c.TaskCommand](t, 31), ActorUserID: independentID[i.User](t, 15), TaskEventID: independentID[c.TaskEvent](t, 32), MilestoneID: task.MilestoneID, SprintID: task.SprintID, Change: c.TaskCreatedChange, ChangedFields: []c.TaskChangedField{c.TaskDescriptionChanged, c.TaskRankChanged, c.TaskPlanChanged, c.TaskPriorityChanged, c.TaskTitleChanged, c.TaskTypeChanged}, Position: &c.TaskPosition{SprintID: task.SprintID, State: c.TaskStateBacklog, Priority: task.Priority, OrderGeneration: 1}}
	event, err := events.NewTaskChanged(header, p)
	if err != nil {
		t.Fatal("valid event rejected")
	}
	raw := event.PayloadBytes()
	p.ChangedFields[0] = c.TaskTitleChanged
	p.Position.OrderGeneration = 99
	decoded, err := events.DecodeTaskChanged(event)
	if err != nil || decoded.Position.OrderGeneration != 1 || decoded.ChangedFields[0] != c.TaskDescriptionChanged {
		t.Error("typed event borrows payload")
	}
	for _, alter := range []func(*e.Header){func(h *e.Header) { h.SchemaVersion = 2 }, func(h *e.Header) { h.EventType = "work.unknown" }, func(h *e.Header) { h.AggregateType = "work.sprint" }, func(h *e.Header) { h.Scope = e.Scope{Kind: e.SystemScope} }, func(h *e.Header) { h.AggregateVersion = nil }, func(h *e.Header) { v := f.Version(2); h.AggregateVersion = &v }, func(h *e.Header) { s := f.Sequence(1); h.AggregateSequence = &s }} {
		h := event.Header()
		alter(&h)
		if _, err := events.Restore(h, raw); err == nil {
			t.Error("invalid event header restored")
		}
	}
	foreign, _ := c.RegisterTaskEvents(e.NewCatalog())
	if _, err := foreign.DecodeTaskChanged(event); err == nil {
		t.Error("foreign catalog decoded event")
	}
	for _, alter := range []func(*c.TaskChanged){func(v *c.TaskChanged) { v.ChangedFields = []c.TaskChangedField{c.TaskTitleChanged, c.TaskTitleChanged} }, func(v *c.TaskChanged) { v.Position.NextID = &task.ID }, func(v *c.TaskChanged) { v.Position.PreviousID = &task.ID }, func(v *c.TaskChanged) { v.Position.SprintID = independentID[pc.Sprint](t, 33) }, func(v *c.TaskChanged) { v.Position.State = c.TaskStateTodo }} {
		v := decoded.Clone()
		alter(&v)
		if _, err := events.NewTaskChanged(header, v); err == nil {
			t.Error("invalid task event payload admitted")
		}
	}
	for _, raw := range []string{`{"changed_fields":["priority"],"type_change":null,"priority_change":{"from":"low","to":"low"},"position":null}`, `{"changed_fields":["plan"],"type_change":null,"priority_change":null,"position":null,"plan":"forbidden"}`} {
		if new(c.TaskFieldsUpdatedPayload).UnmarshalJSON([]byte(raw)) == nil {
			t.Error("invalid safe history admitted")
		}
	}
	created := c.TaskCreatedPayload{InitialState: c.TaskStateBacklog, MilestoneID: task.MilestoneID, SprintID: task.SprintID, Type: task.Type, Priority: task.Priority}
	history := c.TaskEvent{ID: decoded.TaskEventID, ProjectID: task.ProjectID, TaskID: task.ID, TaskVersion: 1, Type: c.TaskEventCreated, Actor: c.TaskEventActor{Type: i.Human, UserID: decoded.ActorUserID, Source: "task_domain"}, OperationID: decoded.CommandID, CorrelationID: decoded.CommandID, Payload: independentJSON(t, created), CreatedAt: task.CreatedAt}
	if history.Validate() != nil {
		t.Fatal("valid history rejected")
	}
	hc := history.Clone()
	hc.Payload[0] = '!'
	if history.Validate() != nil {
		t.Error("history clone aliases payload")
	}
	history.CorrelationID = independentID[c.TaskCommand](t, 34)
	if history.Validate() == nil {
		t.Error("history transport correlation admitted")
	}
	if err := catalog.Seal(); err != nil {
		t.Fatal("catalog seal failed")
	}
	if _, err := c.RegisterTaskEvents(catalog); err == nil {
		t.Error("sealed catalog registered schema")
	}
}

func TestIndependentTaskSafeFormattingBoundary(t *testing.T) {
	marker := "independent-private-body-marker"
	task := independentTask(t)
	task.Title = marker
	task.Description = marker
	task.Plan = marker
	forms := []any{task, &task, struct{ Task c.Task }{task}, []c.Task{task}, map[string]c.Task{"task": task}}
	for n, value := range forms {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, value), marker) {
				t.Errorf("supported formatting form %d leaked body", n)
			}
		}
	}
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("probe", "task", task)
	if strings.Contains(log.String(), marker) {
		t.Error("direct slog leaked body")
	}
	// This deliberately reproduces the original specification's universal
	// enclosing claim. fmt cannot call Formatter through an unexported field.
	hidden := struct{ task c.Task }{task}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, hidden), marker) {
			t.Errorf("unexported enclosing field bypasses Formatter (%s)", format)
		}
	}
}

func TestIndependentTaskFaultCodes(t *testing.T) {
	private := errors.New("independent-private-error-marker")
	for _, code := range []f.Code{f.TaskNotFound, f.TaskVersionConflict, f.TaskStateInvalid, f.TaskAssigneeRequired, f.TaskSprintInvalid, f.TaskTerminalImmutable} {
		fault := f.NewFault(code, f.NotCommitted).WithCause(private)
		if !code.Known() || code.Safe() != code || fault.Error() != string(code) || !errors.Is(fault, private) {
			t.Error("task fault metadata/unwrap lost")
		}
		for _, value := range []any{fault, *fault, struct{ Fault *f.Fault }{fault}} {
			for _, format := range []string{"%v", "%+v", "%#v"} {
				if strings.Contains(fmt.Sprintf(format, value), private.Error()) {
					t.Error("fault implicit formatting leaked cause")
				}
			}
		}
		raw := independentJSON(t, fault)
		if bytes.Contains(raw, []byte(private.Error())) || !bytes.Contains(raw, []byte(code)) {
			t.Error("fault JSON unsafe or lost code")
		}
	}
	if f.Code("TASK_FUTURE").Safe() != f.InternalError {
		t.Error("unknown future task code admitted")
	}
}

func TestIndependentTaskNestedSlogBoundary(t *testing.T) {
	marker := "independent-private-body-marker"
	task := independentTask(t)
	task.Plan = marker
	forms := []any{struct{ Task c.Task }{task}, []c.Task{task}, map[string]c.Task{"task": task}}
	for n, value := range forms {
		var output bytes.Buffer
		slog.New(slog.NewJSONHandler(&output, nil)).Info("probe", "value", value)
		if strings.Contains(output.String(), marker) {
			t.Errorf("JSON slog nested form %d reaches business MarshalJSON", n)
		}
	}
}

func TestIndependentTaskSupportedLogProjection(t *testing.T) {
	marker := "independent-private-body-marker"
	task := independentTask(t)
	task.Title, task.Description, task.Plan = marker, marker, marker
	request := c.TaskCreate{TaskID: task.ID, SprintID: task.SprintID, Title: marker, Description: marker, Plan: marker, Type: task.Type, Priority: task.Priority}
	patch := c.TaskFieldsUpdate{Plan: &marker}
	filter := c.TaskFilter{Text: &marker}
	query := c.TaskCommandLookupRequest{ProjectID: task.ProjectID, Command: c.TaskCommandCreate, IdempotencyKey: f.IdempotencyKey(marker), SemanticDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}
	for _, value := range []any{task, &task, request, &request, patch, filter, query} {
		for _, form := range []any{value, struct{ Value any }{value}, []any{value}, map[string]any{"value": value}} {
			for _, format := range []string{"%v", "%+v", "%#v"} {
				output := fmt.Sprintf(format, form)
				if strings.Contains(output, marker) || strings.Contains(output, string(query.SemanticDigest)) {
					t.Error("supported fmt projection leaked data")
				}
			}
		}
		var output bytes.Buffer
		slog.New(slog.NewJSONHandler(&output, nil)).Info("probe", "value", value)
		if strings.Contains(output.String(), marker) || strings.Contains(output.String(), string(query.SemanticDigest)) {
			t.Error("top-level LogValuer projection leaked data")
		}
	}
}
