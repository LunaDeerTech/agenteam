package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func taskFixture(t *testing.T) Task {
	m := testMilestone(t)
	return Task{ID: testID[Task](t, 20), ProjectID: m.ProjectID, MilestoneID: m.ID, SprintID: testID[pc.Sprint](t, 6), Title: " task title ", Description: "description\t\n\r", Type: TaskTypeFeature, Priority: TaskPriorityHigh, State: TaskStateBacklog, Plan: "plan\n", ManualRank: m.ManualRank, Version: 1, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}
func taskCreateFixture(t *testing.T) TaskCreate {
	v := taskFixture(t)
	return TaskCreate{TaskID: v.ID, SprintID: v.SprintID, Title: v.Title, Description: v.Description, Type: v.Type, Priority: v.Priority, Plan: v.Plan}
}
func taskMutationFixture(t *testing.T) TaskMutation {
	id := testID[TaskEvent](t, 21)
	return TaskMutation{Task: taskFixture(t), Changed: true, TaskEventID: &id, EventIDs: []event.EventID{testID[event.EventIdentity](t, 22)}}
}
func taskRaw(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func taskJSONChange(t *testing.T, raw []byte, key string, replacement json.RawMessage) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if replacement == nil {
		delete(m, key)
	} else {
		m[key] = replacement
	}
	return taskRaw(t, m)
}

func TestTaskCanonicalFieldsAndEnums(t *testing.T) {
	base := taskFixture(t)
	for _, s := range []TaskState{TaskStateBacklog, TaskStateTodo, TaskStateInProgress, TaskStateInReview, TaskStateBlocked, TaskStateDone, TaskStateCancelled} {
		v := base
		v.State = s
		if s == TaskStateTodo || s == TaskStateInProgress || s == TaskStateInReview {
			id := testID[i.Agent](t, 80)
			v.AssigneeAgentID = &id
		}
		var got Task
		if err := json.Unmarshal(taskRaw(t, v), &got); err != nil || !reflect.DeepEqual(got, v) {
			t.Fatal("canonical state lost", err)
		}
	}
	for n, bad := range []func(*Task){func(v *Task) { v.ID = TaskID{} }, func(v *Task) { v.ProjectID = ProjectID{} }, func(v *Task) { v.MilestoneID = MilestoneID{} }, func(v *Task) { v.SprintID = SprintID{} }, func(v *Task) { v.Version = 0 }, func(v *Task) { v.CreatedAt, _ = f.NewInstant(v.UpdatedAt.Time().Add(time.Second)) }, func(v *Task) { v.UpdatedAt = f.Instant{} }, func(v *Task) { v.Type = "Feature" }, func(v *Task) { v.Priority = "urgent" }, func(v *Task) { v.State = "BACKLOG" }, func(v *Task) { v.ManualRank = strings.Repeat("f", 32) }, func(v *Task) { v.State = TaskStateTodo }, func(v *Task) { v.State = TaskStateInProgress }, func(v *Task) { v.State = TaskStateInReview }, func(v *Task) { v.AssigneeAgentID = new(i.AgentID) }, func(v *Task) { v.Title = "\u0085" }, func(v *Task) { v.Description = "\x00" }, func(v *Task) { v.Plan = "\x1f" }, func(v *Task) { v.Title = strings.Repeat("😀", 257) }, func(v *Task) { v.Plan = strings.Repeat("x", 32769) }, func(v *Task) { v.Description = string([]byte{255}) }} {
		v := base
		bad(&v)
		if _, err := json.Marshal(v); err == nil {
			t.Fatalf("invalid canonical task encoded case %d", n)
		}
	}
	raw := taskRaw(t, base)
	for _, key := range strings.Fields("id project_id milestone_id sprint_id title description type priority state assignee_agent_id plan manual_rank version created_at updated_at") {
		if json.Unmarshal(taskJSONChange(t, raw, key, nil), new(Task)) == nil {
			t.Fatal("missing canonical field", key)
		}
		if key != "assignee_agent_id" && json.Unmarshal(taskJSONChange(t, raw, key, json.RawMessage("null")), new(Task)) == nil {
			t.Fatal("null canonical field", key)
		}
	}
	for _, version := range []string{`0`, `1`, `"0"`, `"01"`, `"+1"`, `"9223372036854775808"`} {
		if json.Unmarshal(taskJSONChange(t, raw, "version", json.RawMessage(version)), new(Task)) == nil {
			t.Fatal("invalid version", version)
		}
	}
	v := base
	v.Version = f.Version(math.MaxInt64)
	if v.Validate() != nil {
		t.Fatal("maximum version rejected")
	}
	for _, s := range []string{`"01900000-0000-4000-8000-000000000001"`, `"00000000-0000-0000-0000-000000000000"`, `"01900000-0000-7000-8000-00000000000A"`, `12`} {
		if json.Unmarshal(taskJSONChange(t, raw, "id", json.RawMessage(s)), new(Task)) == nil {
			t.Fatal("invalid task id", s)
		}
	}
	for n, s := range []TaskState{TaskStateBacklog, TaskStateTodo, TaskStateInProgress, TaskStateInReview, TaskStateBlocked, TaskStateDone, TaskStateCancelled} {
		if s.Order() != n {
			t.Fatal("state order")
		}
	}
	for n, p := range []TaskPriority{TaskPriorityCritical, TaskPriorityHigh, TaskPriorityMedium, TaskPriorityLow} {
		if p.Order() != n {
			t.Fatal("priority order")
		}
	}
	for _, bad := range []any{TaskType(""), TaskPriority("HIGH"), TaskState("deleted"), TaskCommandName("work.sprint.create")} {
		if _, err := json.Marshal(bad); err == nil {
			t.Fatal("unknown enum encoded")
		}
	}
}
func TestTaskRequestStrictPresenceAndAtomicDecode(t *testing.T) {
	good := taskCreateFixture(t)
	raw := taskRaw(t, good)
	for _, key := range strings.Fields("task_id sprint_id title type priority") {
		if json.Unmarshal(taskJSONChange(t, raw, key, nil), new(TaskCreate)) == nil {
			t.Fatal("missing required create field", key)
		}
	}
	for _, key := range strings.Fields("task_id sprint_id title type priority description plan initial_state assignee_agent_id") {
		if json.Unmarshal(taskJSONChange(t, raw, key, json.RawMessage("null")), new(TaskCreate)) == nil {
			t.Fatal("null create field", key)
		}
	}
	for _, key := range []string{"Title", "state", "milestone_id", "manual_rank"} {
		if json.Unmarshal(taskJSONChange(t, raw, key, json.RawMessage(`"bad"`)), new(TaskCreate)) == nil {
			t.Fatal("unknown field", key)
		}
	}
	bad := [][]byte{append(bytes.Clone(raw), []byte(" {}")...), append(bytes.Clone(raw[:len(raw)-1]), []byte(`,"title":"again"}`)...), taskJSONChange(t, raw, "title", json.RawMessage(`"\ud800"`)), taskJSONChange(t, raw, "title", json.RawMessage(`"\udc00"`)), taskJSONChange(t, raw, "title", json.RawMessage(`"\ud800\u0041"`)), []byte(`null`), []byte(`[]`)}
	invalidUTF8 := bytes.Replace(raw, []byte("task title"), []byte{0xff}, 1)
	bad = append(bad, invalidUTF8)
	for _, r := range bad {
		v := good.Clone()
		if v.UnmarshalJSON(r) == nil || !reflect.DeepEqual(v, good) {
			t.Fatal("invalid decode accepted or mutated receiver")
		}
	}
	for _, field := range []string{"description", "plan", "initial_state"} {
		raw = taskJSONChange(t, raw, field, nil)
	}
	var omitted TaskCreate
	if err := json.Unmarshal(raw, &omitted); err != nil || omitted.Description != "" || omitted.Plan != "" || omitted.EffectiveState() != TaskStateBacklog {
		t.Fatal("create omission semantics", err)
	}
	if json.Unmarshal([]byte(`{"title":"\ud83d\ude00"}`), new(TaskFieldsUpdate)) != nil {
		t.Fatal("valid surrogate pair")
	}
	for _, r := range []string{`{}`, `{"title":null}`, `{"description":null}`, `{"plan":null}`, `{"type":null}`, `{"priority":null}`, `{"state":"backlog"}`, `{"assignee_agent_id":null}`, `{"before_id":null}`, `{"type":""}`, `{"priority":"HIGH"}`, `{"title":"x","title":"y"}`} {
		if json.Unmarshal([]byte(r), new(TaskFieldsUpdate)) == nil {
			t.Fatal("invalid update", r)
		}
	}
	var patch TaskFieldsUpdate
	if json.Unmarshal([]byte(`{"description":"","plan":""}`), &patch) != nil || patch.Description == nil || patch.Plan == nil || *patch.Plan != "" || patch.Title != nil {
		t.Fatal("clear presence lost")
	}
	for _, r := range []string{`{"before_id":null}`, `{"after_id":"x"}`, `{"manual_rank":"x"}`, `{"before_id":0}`} {
		if json.Unmarshal([]byte(r), new(TaskReorder)) == nil {
			t.Fatal("invalid reorder", r)
		}
	}
	if json.Unmarshal([]byte(`{}`), new(TaskReorder)) != nil {
		t.Fatal("tail rejected")
	}
	a := testActor(t, 1, 2)
	m := testMeta(t, new(f.Version))
	*m.ExpectedVersion = 1
	_, err := TaskReorderDigest(a, m, goodProject(t), good.TaskID, TaskReorder{BeforeID: &good.TaskID})
	requireCode(t, err, f.InvalidArgument)
}
func goodProject(t *testing.T) ProjectID { return taskFixture(t).ProjectID }
func TestTaskFilterThreeStateAndBoundaries(t *testing.T) {
	wires := []string{`{}`, `{"assignee_agent_id":null}`, `{"assignee_agent_id":"` + testID[i.Agent](t, 8).String() + `"}`}
	for n, raw := range wires {
		var v TaskFilter
		if json.Unmarshal([]byte(raw), &v) != nil || v.AssigneeAgentID.Present != (n > 0) || (v.AssigneeAgentID.AgentID != nil) != (n == 2) {
			t.Fatal("assignee filter state", n)
		}
		if string(taskRaw(t, v)) != raw {
			t.Fatal("filter roundtrip", n)
		}
	}
	for _, r := range []string{`{"state":null}`, `{"type":null}`, `{"priority":null}`, `{"milestone_id":null}`, `{"sprint_id":null}`, `{"text":null}`, `{"text":""}`, `{"text":"  "}`, `{"text":"a\u0085b"}`, `{"STATE":"backlog"}`, `{"assignee_agent_id":false}`, `{"state":["backlog"]}`, `{"sort":"priority"}`} {
		if json.Unmarshal([]byte(r), new(TaskFilter)) == nil {
			t.Fatal("invalid filter", r)
		}
	}
	for _, s := range []string{"%_\\", strings.Repeat("😀", 256), " e\u0301 "} {
		v := TaskFilter{Text: &s}
		if v.Validate() != nil {
			t.Fatal("valid literal text rejected")
		}
		var got TaskFilter
		if json.Unmarshal(taskRaw(t, v), &got) != nil || *got.Text != s {
			t.Fatal("text bytes changed")
		}
	}
	id := testID[i.Agent](t, 8)
	if (TaskAssigneeFilter{AgentID: &id}).Validate() == nil {
		t.Fatal("hidden assignee predicate")
	}
	s := strings.Repeat("x", 257)
	if (TaskFilter{Text: &s}).Validate() == nil {
		t.Fatal("over scalar filter accepted")
	}
}
func TestTaskEscapingCapsAndFullPage(t *testing.T) {
	for _, unit := range []string{"<>&", "\"\\", "\t\n\r"} {
		t.Run(fmt.Sprintf("escaping-%x", unit), func(t *testing.T) {
			text := strings.Repeat(unit, 32768/len(unit)+1)[:32768]
			request := taskCreateFixture(t)
			request.Description = text
			request.Plan = text
			request.Title = strings.Repeat("<", 256)
			raw := taskRaw(t, request)
			var restored TaskCreate
			if json.Unmarshal(raw, &restored) != nil || restored.Description != text || restored.Plan != text {
				t.Fatal("request max text roundtrip")
			}
			receipt := taskMutationFixture(t)
			receipt.Task.Title = request.Title
			receipt.Task.Description = text
			receipt.Task.Plan = text
			lookup := TaskCommandLookup{Status: LookupCommitted, Receipt: &receipt}
			raw = taskRaw(t, lookup)
			var got TaskCommandLookup
			if json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, lookup) {
				t.Fatal("lookup max text roundtrip")
			}
			if len(raw) > MaxTaskResultBytes {
				t.Fatal("lookup escaped cap")
			}
		})
	}
	request := taskCreateFixture(t)
	receipt := taskMutationFixture(t)
	lookup := TaskCommandLookup{Status: LookupCommitted, Receipt: &receipt}
	filter := TaskFilter{}
	lookupRequest := TaskCommandLookupRequest{ProjectID: requestProject(t), Command: TaskCommandCreate, IdempotencyKey: "key", SemanticDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}
	cases := []struct {
		v      any
		cap    int
		decode func([]byte) error
	}{{request, MaxTaskRequestBytes, new(TaskCreate).UnmarshalJSON}, {TaskFieldsUpdate{Title: &request.Title}, MaxTaskRequestBytes, new(TaskFieldsUpdate).UnmarshalJSON}, {TaskReorder{}, MaxTaskRequestBytes, new(TaskReorder).UnmarshalJSON}, {receipt.Task, MaxTaskResultBytes, new(Task).UnmarshalJSON}, {receipt, MaxTaskResultBytes, new(TaskMutation).UnmarshalJSON}, {lookup, MaxTaskResultBytes, new(TaskCommandLookup).UnmarshalJSON}, {filter, MaxTaskFilterBytes, new(TaskFilter).UnmarshalJSON}, {lookupRequest, MaxTaskLookupRequestBytes, new(TaskCommandLookupRequest).UnmarshalJSON}}
	for _, tc := range cases {
		raw := taskRaw(t, tc.v)
		at := append([]byte(strings.Repeat(" ", tc.cap-len(raw))), raw...)
		if tc.decode(at) != nil || tc.decode(append([]byte(" "), at...)) == nil {
			t.Fatal("direct raw cap", reflect.TypeOf(tc.v))
		}
		if json.Unmarshal(append([]byte(" "), at...), reflect.New(reflect.TypeOf(tc.v)).Interface()) != nil {
			t.Fatal("standard outer whitespace visibility")
		}
	}
	pageTask := receipt.Task
	pageTask.Description = strings.Repeat("<", 32768)
	pageTask.Plan = strings.Repeat("&", 32768)
	items := make([]Task, 3)
	for n := range items {
		items[n] = pageTask
	}
	raw := taskRaw(t, f.Page[Task]{Items: items})
	if len(raw) <= MaxTaskResultBytes {
		t.Fatal("page accidentally uses one-result cap")
	}
}
func requestProject(t *testing.T) ProjectID { return testID[i.Project](t, 5) }
func TestTaskThreeDigestsBindEverySemanticField(t *testing.T) {
	a := testActor(t, 1, 2)
	renewed := testActor(t, 1, 92)
	p := goodProject(t)
	r := taskCreateFixture(t)
	meta := testMeta(t, nil)
	base, err := TaskCreateDigest(a, meta, p, r)
	if err != nil {
		t.Fatal(err)
	}
	canonical := `{"actor_user_id":"01900000-0000-7000-8000-000000000001","command":"work.task.create","expected_version":null,"format":"work-task-planning-command-v1","project_id":"01900000-0000-7000-8000-000000000005","request":{"assignee_agent_id":null,"description":"description\t\n\r","initial_state":"backlog","plan":"plan\n","priority":"high","sprint_id":"01900000-0000-7000-8000-000000000006","task_id":"01900000-0000-7000-8000-000000000014","title":" task title ","type":"feature"},"target_id":"01900000-0000-7000-8000-000000000014"}`
	// A separately written canonical object fixes the protocol, not just self-consistency.
	if string(base) != taskDigestLiteral(canonical) {
		t.Fatal("digest protocol differs")
	}
	for _, change := range []func(*TaskCreate){func(v *TaskCreate) { v.TaskID = testID[Task](t, 90) }, func(v *TaskCreate) { v.SprintID = testID[pc.Sprint](t, 90) }, func(v *TaskCreate) { v.Title += " " }, func(v *TaskCreate) { v.Description += " " }, func(v *TaskCreate) { v.Type = TaskTypeBug }, func(v *TaskCreate) { v.Priority = TaskPriorityLow }, func(v *TaskCreate) { v.Plan += " " }, func(v *TaskCreate) { v.InitialState = TaskStateTodo }, func(v *TaskCreate) { id := testID[i.Agent](t, 90); v.AssigneeAgentID = &id }} {
		next := r.Clone()
		change(&next)
		got, e := TaskCreateDigest(a, meta, p, next)
		if e != nil || got == base {
			t.Fatal("create semantic omitted", e)
		}
	}
	r.InitialState = TaskStateBacklog
	same, e := TaskCreateDigest(a, meta, p, r)
	if e != nil || same != base {
		t.Fatal("backlog omission differs")
	}
	v := f.Version(3)
	updateMeta := testMeta(t, &v)
	title := "title"
	description := "description"
	plan := "plan"
	typ := TaskTypeFeature
	priority := TaskPriorityHigh
	patch := TaskFieldsUpdate{Title: &title, Description: &description, Plan: &plan, Type: &typ, Priority: &priority}
	anchor := testID[Task](t, 30)
	for _, tc := range []struct {
		m   f.CommandMeta
		run func(i.Actor, f.CommandMeta, ProjectID) (f.Digest, error)
	}{{meta, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) { return TaskCreateDigest(a, m, p, r) }}, {updateMeta, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) {
		return TaskUpdateDigest(a, m, p, r.TaskID, patch)
	}}, {updateMeta, func(a i.Actor, m f.CommandMeta, p ProjectID) (f.Digest, error) {
		return TaskReorderDigest(a, m, p, r.TaskID, TaskReorder{BeforeID: &anchor})
	}}} {
		want, e := tc.run(a, tc.m, p)
		if e != nil {
			t.Fatal(e)
		}
		m := tc.m
		m.RequestID = testID[f.Request](t, 95)
		m.IdempotencyKey = "other-key"
		got, e := tc.run(renewed, m, p)
		if e != nil || got != want {
			t.Fatal("transport identity entered digest")
		}
		for _, other := range []struct {
			a i.Actor
			p ProjectID
		}{{testActor(t, 8, 9), p}, {a, testID[i.Project](t, 90)}} {
			got, e = tc.run(other.a, m, other.p)
			if e != nil || got == want {
				t.Fatal("owner/project omitted")
			}
		}
		_, e = tc.run(i.Actor{}, m, p)
		requireCode(t, e, f.Unauthenticated)
	}
	want, e := TaskUpdateDigest(a, updateMeta, p, r.TaskID, patch)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*TaskFieldsUpdate){func(v *TaskFieldsUpdate) { v.Title = nil }, func(v *TaskFieldsUpdate) { v.Description = nil }, func(v *TaskFieldsUpdate) { v.Plan = nil }, func(v *TaskFieldsUpdate) { v.Type = nil }, func(v *TaskFieldsUpdate) { v.Priority = nil }, func(v *TaskFieldsUpdate) { *v.Title += " " }, func(v *TaskFieldsUpdate) { *v.Description = "" }, func(v *TaskFieldsUpdate) { *v.Plan = "" }, func(v *TaskFieldsUpdate) { *v.Type = TaskTypeBug }, func(v *TaskFieldsUpdate) { *v.Priority = TaskPriorityCritical }} {
		next := patch.Clone()
		change(&next)
		got, e := TaskUpdateDigest(a, updateMeta, p, r.TaskID, next)
		if e != nil || got == want {
			t.Fatal("update presence/value omitted")
		}
	}
	tail, e := TaskReorderDigest(a, updateMeta, p, r.TaskID, TaskReorder{})
	before, e2 := TaskReorderDigest(a, updateMeta, p, r.TaskID, TaskReorder{BeforeID: &anchor})
	if e != nil || e2 != nil || tail == before {
		t.Fatal("reorder before omitted")
	}
	for _, run := range []func(f.CommandMeta, TaskID) (f.Digest, error){func(m f.CommandMeta, id TaskID) (f.Digest, error) { return TaskUpdateDigest(a, m, p, id, patch) }, func(m f.CommandMeta, id TaskID) (f.Digest, error) {
		return TaskReorderDigest(a, m, p, id, TaskReorder{})
	}} {
		want, _ := run(updateMeta, r.TaskID)
		m := updateMeta
		next := v + 1
		m.ExpectedVersion = &next
		got, e := run(m, r.TaskID)
		if e != nil || want == got {
			t.Fatal("expected version omitted")
		}
		got, e = run(updateMeta, testID[Task](t, 99))
		if e != nil || want == got {
			t.Fatal("target omitted")
		}
		m.ExpectedVersion = nil
		_, e = run(m, r.TaskID)
		requireCode(t, e, f.InvalidArgument)
	}
	badMeta := meta
	badMeta.ExpectedVersion = &v
	_, e = TaskCreateDigest(a, badMeta, p, r)
	requireCode(t, e, f.InvalidArgument)
}
func TestTaskCloneUnionAndSafeFormatting(t *testing.T) {
	receipt := taskMutationFixture(t)
	agent := testID[i.Agent](t, 80)
	receipt.Task.AssigneeAgentID = &agent
	lookup := TaskCommandLookup{Status: LookupCommitted, Receipt: &receipt}
	copy := lookup.Clone()
	copy.Receipt.Task.Title = "changed"
	*copy.Receipt.Task.AssigneeAgentID = testID[i.Agent](t, 81)
	*copy.Receipt.TaskEventID = testID[TaskEvent](t, 82)
	copy.Receipt.EventIDs[0] = testID[event.EventIdentity](t, 83)
	if reflect.DeepEqual(copy, lookup) || receipt.Task.Title == "changed" || *receipt.Task.AssigneeAgentID != agent || receipt.EventIDs[0] != testID[event.EventIdentity](t, 22) {
		t.Fatal("receipt clone aliases")
	}
	for _, v := range []TaskMutation{{Task: taskFixture(t), EventIDs: nil}, {Task: taskFixture(t), Changed: true, EventIDs: []event.EventID{}}, {Task: taskFixture(t), Changed: false, EventIDs: receipt.EventIDs}, {Task: taskFixture(t), Changed: false, TaskEventID: receipt.TaskEventID, EventIDs: []event.EventID{}}} {
		if v.Validate() == nil {
			t.Fatal("invalid mutation union")
		}
	}
	noop := TaskMutation{Task: taskFixture(t), EventIDs: []event.EventID{}}
	if noop.Validate() != nil || string(taskRaw(t, noop)) == "" {
		t.Fatal("noop invalid")
	}
	for _, v := range []TaskCommandLookup{{Status: LookupCommitted}, {Status: LookupInProgress, Receipt: &receipt}, {Status: LookupNotObserved, Receipt: &receipt}, {Status: "unknown"}} {
		if v.Validate() == nil {
			t.Fatal("invalid lookup union")
		}
	}
	secret := "sensitive-task-body"
	key := "sensitive-task-key"
	task := taskFixture(t)
	task.Title = secret
	task.Plan = secret
	request := taskCreateFixture(t)
	request.Title = secret
	patch := TaskFieldsUpdate{Title: &secret}
	filter := TaskFilter{Text: &secret}
	query := TaskCommandLookupRequest{ProjectID: task.ProjectID, Command: TaskCommandCreate, IdempotencyKey: f.IdempotencyKey(key), SemanticDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}
	for _, value := range []any{task, &task, request, patch, filter, query, receipt, lookup} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			for _, enclosing := range []any{value, struct{ Value any }{value}, []any{value}, map[string]any{"value": value}} {
				out := fmt.Sprintf(format, enclosing)
				if strings.Contains(out, secret) || strings.Contains(out, key) || strings.Contains(out, string(query.SemanticDigest)) {
					t.Fatal("fmt exposed task data")
				}
			}
		}
		var buf bytes.Buffer
		slog.New(slog.NewJSONHandler(&buf, nil)).Info("safe", "value", value)
		if strings.Contains(buf.String(), secret) || strings.Contains(buf.String(), key) {
			t.Fatal("slog exposed task data")
		}
	}
	if !bytes.Contains(taskRaw(t, task), []byte(secret)) {
		t.Fatal("explicit typed JSON lost business fields")
	}
}

func taskDigestLiteral(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}
