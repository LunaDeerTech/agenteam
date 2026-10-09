package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type transitionDataCase struct {
	h              event.Header
	before, after  Task
	request        TaskTransfer
	history        []TaskTransitionEvent
	source, target TaskTransitionPosition
}

func transitionTestPtr[T any](v T) *T { return &v }

func transitionDataFixture(t *testing.T, full bool) transitionDataCase {
	t.Helper()
	before := taskFixture(t)
	before.State = TaskStateInProgress
	before.AssigneeAgentID = transitionTestPtr(testID[i.Agent](t, 80))
	after := before.Clone()
	after.State, after.Version = TaskStateInReview, 2
	after.AssigneeAgentID = transitionTestPtr(testID[i.Agent](t, 81))
	after.UpdatedAt, _ = f.NewInstant(before.UpdatedAt.Time().Add(time.Second))
	comment := " review\t中文\n "
	x := transitionDataCase{before: before, after: after,
		request: TaskTransfer{TargetState: after.State, AssigneeAgentID: taskClonePtr(after.AssigneeAgentID), Comment: &comment},
		source:  TaskTransitionPosition{SprintID: before.SprintID, State: before.State, Priority: before.Priority, PreviousID: transitionTestPtr(testID[Task](t, 70)), NextID: transitionTestPtr(testID[Task](t, 71)), OrderGeneration: 3},
		target:  TaskTransitionPosition{SprintID: after.SprintID, State: after.State, Priority: after.Priority, PreviousID: transitionTestPtr(testID[Task](t, 72)), OrderGeneration: 8},
	}
	x.h = event.Header{EventID: testID[event.EventIdentity](t, 90), EventType: TaskTransitionedName, SchemaVersion: TaskTransitionSchemaVersion,
		OccurredAt: after.UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: testID[event.Project](t, 5)},
		AggregateType: TaskAggregate, AggregateID: testID[event.Aggregate](t, 20), AggregateVersion: transitionTestPtr(after.Version)}
	x.history = []TaskTransitionEvent{
		{Type: TaskTransitionStateChanged, Payload: TaskTransitionFactPayload{StateChanged: &TaskStateChangedPayload{FromState: before.State, ToState: after.State}}},
		{Type: TaskTransitionAssigneeChanged, Payload: TaskTransitionFactPayload{AssigneeChanged: &TaskAssigneeChangedPayload{FromAgentID: taskClonePtr(before.AssigneeAgentID), ToAgentID: *after.AssigneeAgentID}}},
	}
	if full {
		resolveA, resolveB := testID[TaskBlockerIdentity](t, 100), testID[TaskBlockerIdentity](t, 101)
		addA := TaskBlockerCreate{BlockerID: testID[TaskBlockerIdentity](t, 102), Type: TaskBlockerWaitingForHuman, Description: "human", Metadata: TaskBlockerMetadata{WaitingForHuman: &TaskBlockerWaitingForHumanMetadata{}}}
		addB := TaskBlockerCreate{BlockerID: testID[TaskBlockerIdentity](t, 103), Type: TaskBlockerRelyOn, Description: "dependency", Metadata: TaskBlockerMetadata{RelyOn: &TaskBlockerRelyOnMetadata{RelatedTaskID: testID[Task](t, 99)}}}
		x.request.ResolveBlockerIDs = []TaskBlockerID{resolveB, resolveA}
		x.request.AddBlockers = []TaskBlockerCreate{addB, addA}
		x.history = append(x.history,
			TaskTransitionEvent{Type: TaskTransitionBlockerResolved, Payload: TaskTransitionFactPayload{BlockerResolved: &TaskBlockerResolvedPayload{BlockerID: resolveA, BlockerType: TaskBlockerWaitingForHuman}}},
			TaskTransitionEvent{Type: TaskTransitionBlockerResolved, Payload: TaskTransitionFactPayload{BlockerResolved: &TaskBlockerResolvedPayload{BlockerID: resolveB, BlockerType: TaskBlockerRelyOn}}},
			TaskTransitionEvent{Type: TaskTransitionBlockerAdded, Payload: TaskTransitionFactPayload{BlockerAdded: &TaskBlockerAddedPayload{BlockerID: addA.BlockerID, BlockerType: addA.Type}}},
			TaskTransitionEvent{Type: TaskTransitionBlockerAdded, Payload: TaskTransitionFactPayload{BlockerAdded: &TaskBlockerAddedPayload{BlockerID: addB.BlockerID, BlockerType: addB.Type}}},
		)
	}
	x.history = append(x.history, TaskTransitionEvent{Type: TaskTransitionComment, Payload: TaskTransitionFactPayload{Comment: &TaskCommentPayload{Body: comment}}})
	for n := range x.history {
		x.history[n].ID = testID[TaskEvent](t, 120+n)
		x.history[n].ProjectID, x.history[n].TaskID = after.ProjectID, after.ID
		x.history[n].TaskVersion = after.Version
		x.history[n].Actor = TaskTransitionActor{UserID: testID[i.User](t, 1)}
		x.history[n].OperationID = testID[TaskTransitionCommand](t, 110)
		x.history[n].CorrelationID = x.history[n].OperationID
		x.history[n].CreatedAt = after.UpdatedAt
	}
	return x
}

func (x transitionDataCase) build(factory TaskTransitionEvents) (TaskTransitionMutation, event.Event, error) {
	return factory.NewTaskTransitionData(x.h, x.before, x.after, x.request, x.history, x.source, x.target)
}

func transitionFactoryFixture(t *testing.T) (*event.Catalog, TaskTransitionEvents) {
	t.Helper()
	catalog := event.NewCatalog()
	factory, err := RegisterTaskTransitionEvents(catalog)
	if err != nil || !factory.Valid() {
		t.Fatal("register transition factory", err)
	}
	return catalog, factory
}

func transitionAssertZero(t *testing.T, result TaskTransitionMutation, e event.Event, err error) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(result, TaskTransitionMutation{}) || e.Validate() == nil {
		t.Fatal("failure must return zero receipt and event")
	}
	requireCode(t, err, f.InvalidArgument)
}

func TestTaskTransitionDataFactory(t *testing.T) {
	_, factory := transitionFactoryFixture(t)
	t.Run("complete facts and isolated outputs", func(t *testing.T) {
		x := transitionDataFixture(t, true)
		beforeRaw, afterRaw, requestRaw := taskRaw(t, x.before), taskRaw(t, x.after), taskRaw(t, x.request)
		historyRaw := taskRaw(t, x.history)
		originalAdd, originalResolve := x.request.AddBlockers[0].BlockerID, x.request.ResolveBlockerIDs[0]
		result, e, err := x.build(factory)
		if err != nil || len(result.TaskEventIDs) != 7 || len(result.EventIDs) != 1 || result.EventIDs[0] != x.h.EventID {
			t.Fatal("complete data", err)
		}
		p, err := factory.DecodeTaskTransitioned(e)
		if err != nil || !reflect.DeepEqual(result.Task, x.after) || !reflect.DeepEqual(p.TaskEventIDs, result.TaskEventIDs) || p.AssigneeChange == nil || p.CommandID != x.history[0].OperationID || p.Actor != x.history[0].Actor {
			t.Fatal("assembled envelope", err)
		}
		if !bytes.Equal(beforeRaw, taskRaw(t, x.before)) || !bytes.Equal(afterRaw, taskRaw(t, x.after)) || !bytes.Equal(requestRaw, taskRaw(t, x.request)) || !bytes.Equal(historyRaw, taskRaw(t, x.history)) || originalAdd != x.request.AddBlockers[0].BlockerID || originalResolve != x.request.ResolveBlockerIDs[0] {
			t.Fatal("factory mutated inputs")
		}
		*result.Task.AssigneeAgentID = testID[i.Agent](t, 999)
		result.TaskEventIDs[0] = testID[TaskEvent](t, 999)
		*p.AssigneeChange.FromAgentID = testID[i.Agent](t, 998)
		*p.SourcePosition.PreviousID = testID[Task](t, 998)
		p.TaskEventIDs[0] = testID[TaskEvent](t, 998)
		again, err := factory.DecodeTaskTransitioned(e)
		if err != nil || *again.AssigneeChange.FromAgentID != *x.before.AssigneeAgentID || *again.SourcePosition.PreviousID != *x.source.PreviousID || again.TaskEventIDs[0] != x.history[0].ID || *x.after.AssigneeAgentID == *result.Task.AssigneeAgentID {
			t.Fatal("output aliases input or event", err)
		}
	})
	t.Run("same agent handoff retains two facts", func(t *testing.T) {
		x := transitionDataFixture(t, false)
		x.after.AssigneeAgentID = taskClonePtr(x.before.AssigneeAgentID)
		x.request.AssigneeAgentID = taskClonePtr(x.before.AssigneeAgentID)
		x.history = append(x.history[:1], x.history[2:]...)
		result, e, err := x.build(factory)
		if err != nil || len(result.TaskEventIDs) != 2 {
			t.Fatal("same agent handoff", err)
		}
		p, err := factory.DecodeTaskTransitioned(e)
		if err != nil || p.AssigneeChange != nil {
			t.Fatal("same agent must not claim an assignee change", err)
		}
	})
	t.Run("optional comment and retained assignee", func(t *testing.T) {
		for _, unassigned := range []bool{false, true} {
			x := transitionDataFixture(t, false)
			x.before.State, x.after.State = TaskStateBacklog, TaskStateCancelled
			if unassigned {
				x.before.AssigneeAgentID = nil
			}
			x.after.AssigneeAgentID = taskClonePtr(x.before.AssigneeAgentID)
			x.request = TaskTransfer{TargetState: TaskStateCancelled}
			x.source.State, x.target.State = x.before.State, x.after.State
			x.history = x.history[:1]
			x.history[0].Payload.StateChanged = &TaskStateChangedPayload{FromState: x.before.State, ToState: x.after.State}
			result, e, err := x.build(factory)
			if err != nil || len(result.TaskEventIDs) != 1 {
				t.Fatal("optional comment or preserved assignee rejected", err)
			}
			payload, err := factory.DecodeTaskTransitioned(e)
			if err != nil || payload.AssigneeChange != nil {
				t.Fatal("retained assignee was reported as changed", err)
			}
		}
	})
	badCases := []struct {
		name   string
		change func(*transitionDataCase)
	}{
		{"missing fact", func(x *transitionDataCase) { x.history = x.history[:len(x.history)-1] }},
		{"extra fact", func(x *transitionDataCase) { x.history = append(x.history, x.history[len(x.history)-1].Clone()) }},
		{"wrong order", func(x *transitionDataCase) { x.history[2], x.history[3] = x.history[3], x.history[2] }},
		{"duplicate ID", func(x *transitionDataCase) { x.history[1].ID = x.history[0].ID }},
		{"descending ID", func(x *transitionDataCase) { x.history[1].ID = testID[TaskEvent](t, 119) }},
		{"wrong type", func(x *transitionDataCase) { x.history[0].Type = TaskTransitionComment }},
		{"other legal human edge", func(x *transitionDataCase) {
			x.history[0].Payload.StateChanged = &TaskStateChangedPayload{FromState: TaskStateBacklog, ToState: TaskStateCancelled}
		}},
		{"other legal assignee pair", func(x *transitionDataCase) {
			x.history[1].Payload.AssigneeChanged = &TaskAssigneeChangedPayload{FromAgentID: transitionTestPtr(testID[i.Agent](t, 82)), ToAgentID: testID[i.Agent](t, 83)}
		}},
		{"wrong assignee from value", func(x *transitionDataCase) {
			x.history[1].Payload.AssigneeChanged.FromAgentID = transitionTestPtr(testID[i.Agent](t, 82))
		}},
		{"wrong assignee from null", func(x *transitionDataCase) { x.history[1].Payload.AssigneeChanged.FromAgentID = nil }},
		{"wrong assignee to", func(x *transitionDataCase) { x.history[1].Payload.AssigneeChanged.ToAgentID = testID[i.Agent](t, 82) }},
		{"wrong resolve ID", func(x *transitionDataCase) {
			x.history[2].Payload.BlockerResolved.BlockerID = testID[TaskBlockerIdentity](t, 99)
		}},
		{"resolve comment", func(x *transitionDataCase) { s := "extra"; x.history[2].Payload.BlockerResolved.ResolutionComment = &s }},
		{"wrong add ID", func(x *transitionDataCase) {
			x.history[4].Payload.BlockerAdded.BlockerID = testID[TaskBlockerIdentity](t, 99)
		}},
		{"wrong add type", func(x *transitionDataCase) { x.history[4].Payload.BlockerAdded.BlockerType = TaskBlockerRelyOn }},
		{"wrong comment bytes", func(x *transitionDataCase) {
			x.history[6].Payload.Comment.Body = strings.TrimSpace(x.history[6].Payload.Comment.Body)
		}},
		{"wrong project", func(x *transitionDataCase) { x.history[1].ProjectID = testID[i.Project](t, 8) }},
		{"wrong task", func(x *transitionDataCase) { x.history[1].TaskID = testID[Task](t, 8) }},
		{"wrong actor", func(x *transitionDataCase) { x.history[1].Actor.UserID = testID[i.User](t, 8) }},
		{"wrong operation", func(x *transitionDataCase) {
			x.history[1].OperationID = testID[TaskTransitionCommand](t, 8)
			x.history[1].CorrelationID = x.history[1].OperationID
		}},
		{"wrong correlation", func(x *transitionDataCase) { x.history[1].CorrelationID = testID[TaskTransitionCommand](t, 8) }},
		{"wrong time", func(x *transitionDataCase) { x.history[1].CreatedAt = x.before.UpdatedAt }},
		{"wrong history version", func(x *transitionDataCase) { x.history[1].TaskVersion = 3 }},
		{"version skipped", func(x *transitionDataCase) { x.after.Version = 3 }},
		{"request state", func(x *transitionDataCase) { x.request.TargetState = TaskStateCancelled }},
		{"request assignee", func(x *transitionDataCase) { x.request.AssigneeAgentID = transitionTestPtr(testID[i.Agent](t, 82)) }},
		{"missing handoff assignee", func(x *transitionDataCase) { x.request.AssigneeAgentID = nil }},
		{"source mismatch", func(x *transitionDataCase) { x.source.State = TaskStateBacklog }},
		{"target mismatch", func(x *transitionDataCase) { x.target.State = TaskStateTodo }},
		{"source priority", func(x *transitionDataCase) { x.source.Priority = TaskPriorityLow }},
		{"target sprint", func(x *transitionDataCase) { x.target.SprintID = testID[pc.Sprint](t, 9) }},
		{"self source", func(x *transitionDataCase) { x.source.PreviousID = &x.before.ID }},
		{"non-tail target", func(x *transitionDataCase) { x.target.NextID = transitionTestPtr(testID[Task](t, 9)) }},
		{"header aggregate", func(x *transitionDataCase) { x.h.AggregateID = testID[event.Aggregate](t, 9) }},
		{"header project", func(x *transitionDataCase) { x.h.Scope.ProjectID = testID[event.Project](t, 9) }},
		{"header version", func(x *transitionDataCase) { x.h.AggregateVersion = transitionTestPtr(f.Version(3)) }},
		{"header time", func(x *transitionDataCase) { x.h.OccurredAt = x.before.UpdatedAt }},
		{"overflow", func(x *transitionDataCase) { x.before.Version = f.Version(math.MaxInt64); x.after.Version = 1 }},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			x := transitionDataFixture(t, true)
			tc.change(&x)
			result, e, err := x.build(factory)
			transitionAssertZero(t, result, e, err)
		})
	}
	for _, field := range []struct {
		name   string
		change func(*Task)
	}{
		{"id", func(v *Task) { v.ID = testID[Task](t, 9) }},
		{"project", func(v *Task) { v.ProjectID = testID[i.Project](t, 9) }},
		{"milestone", func(v *Task) { v.MilestoneID = testID[Milestone](t, 9) }},
		{"sprint", func(v *Task) { v.SprintID = testID[pc.Sprint](t, 9) }},
		{"title", func(v *Task) { v.Title += "changed" }},
		{"description", func(v *Task) { v.Description += "changed" }},
		{"type", func(v *Task) { v.Type = TaskTypeBug }},
		{"priority", func(v *Task) { v.Priority = TaskPriorityLow }},
		{"plan", func(v *Task) { v.Plan += "changed" }},
		{"created_at", func(v *Task) { v.CreatedAt, _ = f.NewInstant(v.CreatedAt.Time().Add(-time.Second)) }},
	} {
		t.Run("immutable "+field.name, func(t *testing.T) {
			x := transitionDataFixture(t, false)
			field.change(&x.after)
			result, e, err := x.build(factory)
			transitionAssertZero(t, result, e, err)
		})
	}
	t.Run("maximum successor version", func(t *testing.T) {
		x := transitionDataFixture(t, false)
		x.before.Version, x.after.Version = f.Version(math.MaxInt64-1), f.Version(math.MaxInt64)
		x.h.AggregateVersion = &x.after.Version
		for n := range x.history {
			x.history[n].TaskVersion = x.after.Version
		}
		if _, _, err := x.build(factory); err != nil {
			t.Fatal("maximum successor", err)
		}
	})
	for _, edge := range [][2]TaskState{{TaskStateInProgress, TaskStateInReview}, {TaskStateInReview, TaskStateTodo}, {TaskStateInReview, TaskStateDone}, {TaskStateInReview, TaskStateBlocked}} {
		t.Run(string(edge[0])+" to "+string(edge[1])+" requires comment", func(t *testing.T) {
			x := transitionDataFixture(t, false)
			x.before.State, x.after.State = edge[0], edge[1]
			x.request.TargetState = edge[1]
			x.source.State, x.target.State = edge[0], edge[1]
			x.history[0].Payload.StateChanged = &TaskStateChangedPayload{FromState: edge[0], ToState: edge[1]}
			if _, _, err := x.build(factory); err != nil {
				t.Fatal("legal edge data", err)
			}
			x.request.Comment = nil
			x.history = x.history[:len(x.history)-1]
			result, e, err := x.build(factory)
			transitionAssertZero(t, result, e, err)
		})
	}
	for _, edge := range [][2]TaskState{{TaskStateInProgress, TaskStateInReview}, {TaskStateInReview, TaskStateTodo}} {
		t.Run(string(edge[0])+" explicit handoff", func(t *testing.T) {
			x := transitionDataFixture(t, false)
			x.before.State, x.after.State = edge[0], edge[1]
			x.request.TargetState = edge[1]
			x.source.State, x.target.State = edge[0], edge[1]
			x.history[0].Payload.StateChanged = &TaskStateChangedPayload{FromState: edge[0], ToState: edge[1]}
			x.after.AssigneeAgentID = taskClonePtr(x.before.AssigneeAgentID)
			x.request.AssigneeAgentID = nil
			x.history = append(x.history[:1], x.history[2:]...)
			result, e, err := x.build(factory)
			transitionAssertZero(t, result, e, err)
		})
	}
	t.Run("null assignee preimage must bind exactly", func(t *testing.T) {
		x := transitionDataFixture(t, false)
		x.before.State, x.after.State = TaskStateBacklog, TaskStateTodo
		x.before.AssigneeAgentID = nil
		x.request.TargetState = TaskStateTodo
		x.source.State, x.target.State = x.before.State, x.after.State
		x.history[0].Payload.StateChanged = &TaskStateChangedPayload{FromState: x.before.State, ToState: x.after.State}
		x.history[1].Payload.AssigneeChanged.FromAgentID = nil
		if _, _, err := x.build(factory); err != nil {
			t.Fatal("null preimage", err)
		}
		x.history[1].Payload.AssigneeChanged.FromAgentID = transitionTestPtr(testID[i.Agent](t, 82))
		result, e, err := x.build(factory)
		transitionAssertZero(t, result, e, err)
	})
	t.Run("unbound blocker is not a successful fact", func(t *testing.T) {
		x := transitionDataFixture(t, true)
		x.request.AddBlockers[0].Type = TaskBlockerTechnical
		x.request.AddBlockers[0].Metadata = TaskBlockerMetadata{}
		result, e, err := x.build(factory)
		if !reflect.DeepEqual(result, TaskTransitionMutation{}) || e.Validate() == nil {
			t.Fatal("unbound produced data")
		}
		requireCode(t, err, f.DependencyUnbound)
	})
}

func TestTaskTransitionTypedEnvelopeFactory(t *testing.T) {
	catalog, factory := transitionFactoryFixture(t)
	x := transitionDataFixture(t, false)
	_, original, err := x.build(factory)
	if err != nil {
		t.Fatal("envelope fixture", err)
	}
	p, err := factory.DecodeTaskTransitioned(original)
	if err != nil {
		t.Fatal("decode fixture", err)
	}
	raw := taskRaw(t, p)
	t.Run("registration and seal", func(t *testing.T) {
		if _, err := RegisterTaskEvents(catalog); err != nil {
			t.Fatal("transition registration sealed catalog or registered legacy schema", err)
		}
		if err := catalog.Seal(); err != nil {
			t.Fatal(err)
		}
		if e, err := factory.NewTaskTransitioned(x.h, p); err != nil || e.Validate() != nil {
			t.Fatal("sealed catalog must keep issuing known schema", err)
		}
		for _, bad := range []*event.Catalog{nil, {}, catalog} {
			got, err := RegisterTaskTransitionEvents(bad)
			if err == nil || got.Valid() {
				t.Fatal("invalid catalog registered")
			}
		}
		duplicate := event.NewCatalog()
		if _, err := RegisterTaskTransitionEvents(duplicate); err != nil {
			t.Fatal(err)
		}
		if got, err := RegisterTaskTransitionEvents(duplicate); err == nil || got.Valid() {
			t.Fatal("duplicate registered")
		}
		sealed := event.NewCatalog()
		if err := sealed.Seal(); err != nil {
			t.Fatal(err)
		}
		if got, err := RegisterTaskTransitionEvents(sealed); err == nil || got.Valid() {
			t.Fatal("sealed catalog registered")
		}
		conflict := event.NewCatalog()
		_, err := event.DefineEvent(conflict, event.Definition[TaskTransitioned]{Schema: event.Schema{Producer: "other", EventType: TaskTransitionedName, AggregateType: TaskAggregate, Version: 2}, Codec: event.JSONCodec[TaskTransitioned]{}, Validate: TaskTransitioned.Validate})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := RegisterTaskTransitionEvents(conflict); err == nil || got.Valid() {
			t.Fatal("producer conflict registered")
		}
	})
	t.Run("same catalog restore and canonical digest", func(t *testing.T) {
		e, err := factory.Restore(x.h, raw)
		if err != nil || !catalog.Owns(e) {
			t.Fatal("restore", err)
		}
		decoded, err := factory.DecodeTaskTransitioned(e)
		if err != nil || !reflect.DeepEqual(decoded, p) {
			t.Fatal("restore round trip", err)
		}
		sum := sha256.Sum256(e.PayloadBytes())
		if e.Summary().PayloadDigest != f.Digest("sha256:"+hex.EncodeToString(sum[:])) {
			t.Fatal("digest is not canonical payload digest")
		}
		if bytes.Equal(e.PayloadBytes(), taskRaw(t, e)) || string(taskRaw(t, e)) != `"domain_event"` {
			t.Fatal("event log projection confused with payload")
		}
		var compact bytes.Buffer
		if json.Compact(&compact, e.PayloadBytes()) != nil || !bytes.Equal(compact.Bytes(), e.PayloadBytes()) {
			t.Fatal("canonical payload contains whitespace")
		}
		copy := e.PayloadBytes()
		copy[0] = 'x'
		if e.PayloadBytes()[0] != '{' {
			t.Fatal("payload bytes alias event")
		}
	})
	t.Run("zero and foreign factories and events", func(t *testing.T) {
		var zero TaskTransitionEvents
		if zero.Valid() {
			t.Fatal("zero factory valid")
		}
		if e, err := zero.NewTaskTransitioned(x.h, p); err == nil || e.Validate() == nil {
			t.Fatal("zero New")
		}
		if e, err := zero.Restore(x.h, raw); err == nil || e.Validate() == nil {
			t.Fatal("zero Restore")
		}
		for _, factoryCase := range []TaskTransitionEvents{zero, factory} {
			got, err := factoryCase.DecodeTaskTransitioned(event.Event{})
			if err == nil || !reflect.DeepEqual(got, TaskTransitioned{}) {
				t.Fatal("zero event decoded")
			}
		}
		_, foreign := transitionFactoryFixture(t)
		foreignEvent, err := foreign.NewTaskTransitioned(x.h, p)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := factory.DecodeTaskTransitioned(foreignEvent); err == nil || !reflect.DeepEqual(got, TaskTransitioned{}) {
			t.Fatal("foreign catalog accepted")
		}
		if got, err := foreign.DecodeTaskTransitioned(original); err == nil || !reflect.DeepEqual(got, TaskTransitioned{}) {
			t.Fatal("reverse foreign catalog accepted")
		}
		mixed, own := transitionFactoryFixture(t)
		otherType, err := event.DefineEvent(mixed, event.Definition[TaskTransitioned]{Schema: event.Schema{Producer: "other", EventType: "other.task_transitioned", AggregateType: TaskAggregate, Version: 1}, Codec: event.JSONCodec[TaskTransitioned]{}, Validate: TaskTransitioned.Validate})
		if err != nil {
			t.Fatal(err)
		}
		h := x.h
		h.EventType = "other.task_transitioned"
		other, err := event.NewEvent(otherType, h, p)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := own.DecodeTaskTransitioned(other); err == nil || !reflect.DeepEqual(got, TaskTransitioned{}) {
			t.Fatal("same catalog wrong producer accepted")
		}
	})
	for _, tc := range []struct {
		name   string
		change func(*event.Header)
	}{
		{"event type", func(h *event.Header) { h.EventType = TaskChangedName }},
		{"aggregate type", func(h *event.Header) { h.AggregateType = "work.sprint" }},
		{"schema", func(h *event.Header) { h.SchemaVersion++ }},
		{"event ID", func(h *event.Header) { h.EventID = event.EventID{} }},
		{"aggregate ID", func(h *event.Header) { h.AggregateID = event.AggregateID{} }},
		{"scope", func(h *event.Header) { h.Scope = event.Scope{Kind: event.SystemScope} }},
		{"project", func(h *event.Header) { h.Scope.ProjectID = event.ProjectID{} }},
		{"missing version", func(h *event.Header) { h.AggregateVersion = nil }},
		{"version one", func(h *event.Header) { h.AggregateVersion = transitionTestPtr(f.Version(1)) }},
		{"sequence", func(h *event.Header) { h.AggregateSequence = transitionTestPtr(f.Sequence(1)) }},
	} {
		t.Run("header "+tc.name, func(t *testing.T) {
			h := x.h
			tc.change(&h)
			if e, err := factory.NewTaskTransitioned(h, p); err == nil || e.Validate() == nil {
				t.Fatal("invalid header accepted")
			}
			e, err := factory.Restore(h, raw)
			if err == nil || e.Validate() == nil {
				t.Fatal("invalid header restored")
			}
			if tc.name == "event type" || tc.name == "aggregate type" || tc.name == "schema" {
				requireCode(t, err, f.SchemaUnsupported)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		change func(*TaskTransitioned)
	}{
		{"source self previous", func(v *TaskTransitioned) { v.SourcePosition.PreviousID = &x.before.ID }},
		{"source self next", func(v *TaskTransitioned) { v.SourcePosition.NextID = &x.before.ID }},
		{"target self", func(v *TaskTransitioned) { v.TargetPosition.PreviousID = &x.before.ID }},
		{"target next", func(v *TaskTransitioned) { v.TargetPosition.NextID = transitionTestPtr(testID[Task](t, 85)) }},
		{"source state", func(v *TaskTransitioned) { v.SourcePosition.State = TaskStateBacklog }},
		{"target state", func(v *TaskTransitioned) { v.TargetPosition.State = TaskStateTodo }},
		{"priority mismatch", func(v *TaskTransitioned) { v.TargetPosition.Priority = TaskPriorityLow }},
		{"sprint mismatch", func(v *TaskTransitioned) { v.TargetPosition.SprintID = testID[pc.Sprint](t, 9) }},
		{"no history", func(v *TaskTransitioned) { v.TaskEventIDs = nil }},
		{"duplicate history", func(v *TaskTransitioned) { v.TaskEventIDs[1] = v.TaskEventIDs[0] }},
	} {
		t.Run("payload "+tc.name, func(t *testing.T) {
			bad := p.Clone()
			tc.change(&bad)
			if e, err := factory.NewTaskTransitioned(x.h, bad); err == nil || e.Validate() == nil {
				t.Fatal("invalid payload accepted")
			}
		})
	}
	t.Run("decode repeats Work header and target checks", func(t *testing.T) {
		for _, mode := range []string{"scope", "version", "sequence", "self"} {
			h, payload := x.h, p.Clone()
			switch mode {
			case "scope":
				h.Scope = event.Scope{Kind: event.SystemScope}
			case "version":
				h.AggregateVersion = transitionTestPtr(f.Version(1))
			case "sequence":
				h.AggregateSequence = transitionTestPtr(f.Sequence(1))
			case "self":
				payload.SourcePosition.PreviousID = &x.before.ID
			}
			e, err := catalog.Restore(WorkProducer, h, taskRaw(t, payload))
			if err != nil {
				t.Fatal("generic event fixture", mode, err)
			}
			if got, err := factory.DecodeTaskTransitioned(e); err == nil || !reflect.DeepEqual(got, TaskTransitioned{}) {
				t.Fatal("decode missed Work check", mode)
			}
		}
	})
	t.Run("strict raw codec atomicity and nested limits", func(t *testing.T) {
		for _, key := range strings.Fields("command_id actor task_event_ids milestone_id sprint_id from_state to_state assignee_change source_position target_position") {
			for _, replacement := range []json.RawMessage{nil, json.RawMessage("null")} {
				if key == "assignee_change" && string(replacement) == "null" {
					continue
				}
				bad := taskJSONChange(t, raw, key, replacement)
				old := p.Clone()
				receiver := p.Clone()
				if err := receiver.UnmarshalJSON(bad); err == nil || !reflect.DeepEqual(receiver, old) {
					t.Fatal("missing/null accepted or receiver mutated", key)
				}
				if got, err := DecodeTaskTransitioned(bad); err == nil || !reflect.DeepEqual(got, TaskTransitioned{}) {
					t.Fatal("failed decode is not zero", key)
				}
			}
		}
		for _, bad := range [][]byte{
			[]byte("null"), append(append([]byte(nil), raw...), []byte(" {}")...),
			append([]byte(`{"command_id":"`+p.CommandID.String()+`",`), raw[1:]...),
			taskJSONChange(t, raw, "unknown", []byte("1")),
			taskJSONChange(t, raw, "Command_id", taskRaw(t, p.CommandID)),
			taskJSONChange(t, raw, "command_id", []byte("1")),
			taskJSONChange(t, raw, "command_id", []byte(`"\ud800"`)),
			taskJSONChange(t, raw, "actor", []byte(`{"type":"human","user_id":"`+p.Actor.UserID.String()+`","source":"task_domain","source":"task_domain"}`)),
			bytes.Replace(raw, []byte("task_domain"), []byte{255}, 1),
		} {
			if e, err := factory.Restore(x.h, bad); err == nil || e.Validate() == nil {
				t.Fatal("malformed raw restored")
			}
		}
		if err := (*TaskTransitioned)(nil).UnmarshalJSON(raw); err == nil {
			t.Fatal("nil receiver accepted")
		}
		for _, nested := range []struct {
			key string
			cap int
		}{{"actor", MaxTaskTransitionActorBytes}, {"source_position", MaxTaskHistoryPayloadBytes}, {"target_position", MaxTaskHistoryPayloadBytes}, {"assignee_change", MaxTaskHistoryPayloadBytes}} {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			token := fields[nested.key]
			for _, extra := range []int{0, 1} {
				padded := append(append(append([]byte(nil), token[:len(token)-1]...), bytes.Repeat([]byte(" "), nested.cap-len(token)+extra)...), '}')
				// RawMessage marshaling compacts whitespace, so splice the original
				// token directly into the otherwise valid outer object.
				combined := bytes.Replace(raw, token, padded, 1)
				got, err := DecodeTaskTransitioned(combined)
				if extra == 0 && (err != nil || got.Validate() != nil) {
					t.Fatal("exact nested cap rejected", nested.key, err)
				}
				if extra == 1 && (err == nil || !reflect.DeepEqual(got, TaskTransitioned{})) {
					t.Fatal("nested raw cap bypassed", nested.key)
				}
			}
		}
	})
	t.Run("owned Restore bounds raw before generic canonicalization", func(t *testing.T) {
		atCap := append(append([]byte(nil), raw...), bytes.Repeat([]byte(" "), MaxTaskTransitionedBytes-len(raw))...)
		if e, err := factory.Restore(x.h, atCap); err != nil || e.Validate() != nil {
			t.Fatal("exact outer cap rejected", err)
		}
		over := append(atCap, ' ')
		if e, err := factory.Restore(x.h, over); err == nil || e.Validate() == nil {
			t.Fatal("full raw cap bypassed")
		}
		// The generic catalog canonicalizes before the payload codec. Its success
		// cannot establish the original raw boundary, and Decode cannot recover it.
		e, err := catalog.Restore(WorkProducer, x.h, over)
		if err != nil {
			t.Fatal("generic canonicalization fixture changed", err)
		}
		if _, err := factory.DecodeTaskTransitioned(e); err != nil {
			t.Fatal("canonical event decode", err)
		}
	})
	t.Run("clone and concurrent factory access", func(t *testing.T) {
		clone := p.Clone()
		*clone.AssigneeChange.FromAgentID = testID[i.Agent](t, 999)
		*clone.SourcePosition.PreviousID = testID[Task](t, 999)
		*clone.SourcePosition.NextID = testID[Task](t, 998)
		*clone.TargetPosition.PreviousID = testID[Task](t, 997)
		clone.TaskEventIDs[0] = testID[TaskEvent](t, 996)
		if !bytes.Equal(raw, taskRaw(t, p)) {
			t.Fatal("Clone aliases nested data")
		}
		start := make(chan struct{})
		failures := make(chan error, 8)
		var joined sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			joined.Add(1)
			go func() {
				defer joined.Done()
				<-start
				for iteration := 0; iteration < 20; iteration++ {
					built, err := factory.NewTaskTransitioned(x.h, p)
					if err != nil {
						failures <- err
						return
					}
					decoded, err := factory.DecodeTaskTransitioned(built)
					if err != nil {
						failures <- err
						return
					}
					decoded.TaskEventIDs[0] = TaskEventID{}
					if _, err := factory.Restore(x.h, raw); err != nil {
						failures <- err
						return
					}
				}
			}()
		}
		close(start)
		joined.Wait()
		close(failures)
		for err := range failures {
			t.Fatal("concurrent factory failed", err)
		}
		if !bytes.Equal(raw, taskRaw(t, p)) {
			t.Fatal("concurrent factory mutated shared payload")
		}
		for _, value := range []any{p, factory} {
			if fmt.Sprintf("%+v", value) != "work_task_transition" {
				t.Fatal("unsafe direct projection")
			}
		}
	})
}
