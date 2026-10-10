package work

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func taskTriggerFixtureInput(t *testing.T) taskTriggerWire {
	t.Helper()
	r, _, _ := pureTaskRecord(t)
	task := r.Plan.After.Task
	milestone := c.Milestone{ID: task.MilestoneID, ProjectID: task.ProjectID, Title: "milestone", Description: "private milestone", ManualRank: task.ManualRank, Version: 1, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
	sprint := c.Sprint{ID: task.SprintID, ProjectID: task.ProjectID, MilestoneID: task.MilestoneID, Title: "sprint", Description: "private sprint", ManualRank: task.ManualRank, Version: 1, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt, State: c.Planned}
	return taskTriggerWire{1, task, sprint, milestone, "task/work", []c.TaskBlocker{}, []json.RawMessage{bytes.Clone(r.Plan.TaskEvent)}}
}
func taskTriggerFixtureRequest(t *testing.T) (i.ExecutionID, ec.LaunchRequest) {
	t.Helper()
	w := taskTriggerFixtureInput(t)
	return pureID[i.Execution](t, 90), ec.LaunchRequest{ProjectID: w.Task.ProjectID, AgentID: pureID[i.Agent](t, 91), Trigger: ec.Trigger{Kind: "task", TaskID: w.Task.ID.String()}, Purpose: "task/work", Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Meta: f.CommandMeta{IdempotencyKey: "task-trigger-test-key", RequestID: pureID[f.Request](t, 92)}}
}
func TestTaskTriggerInputRoundTripAndBoundedMaterial(t *testing.T) {
	w := taskTriggerFixtureInput(t)
	b, _, _ := pureBlockerRecord(t, false)
	w.Task = b.Plan.After.Task.Clone()
	w.UnresolvedBlockers = []c.TaskBlocker{b.Plan.After.Blocker.Clone()}
	ev, err := json.Marshal(b.Plan.TaskEvent)
	if err != nil {
		t.Fatal(err)
	}
	w.RecentTaskEvents = append(w.RecentTaskEvents, ev)
	v, err := newTaskTriggerInput(w)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := v.Data()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := DecodeTaskTriggerInput(raw)
	if err != nil || restored.Task().Plan != w.Task.Plan || restored.UnresolvedBlockers()[0].Description != w.UnresolvedBlockers[0].Description || len(restored.RecentTaskEvents()) != 2 {
		t.Fatal("complete input", err)
	}
	// Caller mutation cannot alter the immutable input or its history bytes.
	w.UnresolvedBlockers[0].Description = "changed"
	events := restored.RecentTaskEvents()
	events[0][0] = '!'
	blockers := restored.UnresolvedBlockers()
	blockers[0].Description = "changed"
	again, _ := restored.Data()
	if !bytes.Equal(raw, again) {
		t.Fatal("input alias")
	}
	for _, output := range []string{fmt.Sprintf("%+v %#v", v, v), v.LogValue().String()} {
		if strings.Contains(output, "private") {
			t.Fatal("material formatted")
		}
	}
	safe, _ := json.Marshal(v)
	if string(safe) != `"task_trigger_input"` {
		t.Fatal("implicit JSON exposed material")
	}
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"purpose":"task/work"`), []byte(`"purpose":"task/work","purpose":"task/work"`), 1),
		bytes.Replace(raw, []byte(`"purpose":"task/work"`), []byte(`"purpose":"task/work","extra":true`), 1),
		bytes.Replace(raw, []byte(`"purpose":"task/work"`), []byte(`"purpose":"\ud800"`), 1),
		append(bytes.Clone(raw), 'x'),
	} {
		if _, err := DecodeTaskTriggerInput(bad); err == nil {
			t.Fatal("accepted malformed material")
		}
	}
	bad := taskTriggerFixtureInput(t)
	bad.RecentTaskEvents = []json.RawMessage{}
	if _, err := newTaskTriggerInput(bad); err == nil {
		t.Fatal("missing history became empty success")
	}
	bad = taskTriggerFixtureInput(t)
	bad.Sprint.ProjectID = pureID[i.Project](t, 99)
	if _, err := newTaskTriggerInput(bad); err == nil {
		t.Fatal("foreign placement")
	}
	// Extra bytes cannot turn the hard complete-input bound into truncation.
	if _, err := DecodeTaskTriggerInput(append(bytes.Clone(raw), bytes.Repeat([]byte(" "), ec.MaxTriggerInputBytes)...)); err == nil {
		t.Fatal("over-limit input")
	}
}
func TestTaskTriggerCapturedDecoderUsesOnlyOriginalInput(t *testing.T) {
	execution, request := taskTriggerFixtureRequest(t)
	w := taskTriggerFixtureInput(t)
	// Controlled codec material, not a persisted assignment/transition or an
	// Execution proof. The real current Task writer cannot yet create this state.
	w.Task.AssigneeAgentID = &request.AgentID
	w.Task.State = c.TaskStateInProgress
	w.Sprint.State = c.Current
	at := w.Sprint.CreatedAt
	w.Sprint.StartedAt = &at
	w.Sprint.StartedBy = &c.ActorHistory{Kind: i.Human, UserID: pureID[i.User](t, 1).String()}
	d, _ := request.Digest()
	raw, err := json.Marshal(capturedTaskWire{1, execution, d, w})
	if err != nil {
		t.Fatal(err)
	}
	ref := ec.TriggerInputRef{ProviderType: "task", SchemaVersion: 1, InputID: pureID[ec.TriggerInput](t, 93), Digest: ec.TriggerInputDigest(raw)}
	captured, err := ec.NewCapturedTriggerInput(ref, raw)
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeCapturedTaskInput(captured, ec.PreparationRequest{ExecutionID: execution, Launch: request})
	if err != nil || input.Task().Version != 1 || input.Task().Plan != w.Task.Plan {
		t.Fatal("captured decode", err)
	}
	request.Purpose = "task/review"
	if _, err = DecodeCapturedTaskInput(captured, ec.PreparationRequest{ExecutionID: execution, Launch: request}); err == nil {
		t.Fatal("different original request")
	}
	if _, err = DecodeCapturedTaskInput(ec.CapturedTriggerInput{}, ec.PreparationRequest{ExecutionID: execution, Launch: request}); err == nil {
		t.Fatal("zero captured input")
	}
	original, _ := newTaskTriggerInput(taskTriggerFixtureInput(t))
	_, request = taskTriggerFixtureRequest(t)
	if err = taskCaptureEligible(original, request); err == nil {
		t.Fatal("backlog observation became capture")
	}
}
