// Independent T0b probes prepared from the accepted engineering specification,
// before reading the implementation. Overlay into package contract_test.
package contract_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	w "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func t0bID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	v, err := f.ParseID[K](fmt.Sprintf("00000000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal("independent ID fixture", err)
	}
	return v
}
func t0bPointer[T any](v T) *T { return &v }
func t0bJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal("independent valid fixture encoding", err)
	}
	return b
}
func t0bFault(t *testing.T, err error, want f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault == nil || fault.Code != want || fault.CommitState != f.NotStarted || !fault.Code.Known() || fault.Code.Safe() != want {
		t.Fatalf("expected %s/not_started, got %v", want, err)
	}
	if strings.Contains(string(t0bJSON(t, fault)), "independent-sensitive") {
		t.Fatal("unsafe fault includes caller text")
	}
}
func t0bObjectChange(t *testing.T, raw []byte, key string, value json.RawMessage) []byte {
	t.Helper()
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		t.Fatal("probe requires an object")
	}
	if value == nil {
		delete(object, key)
	} else {
		object[key] = value
	}
	return t0bJSON(t, object)
}

type t0bFixture struct {
	before, after  w.Task
	request        w.TaskTransfer
	history        []w.TaskTransitionEvent
	source, target w.TaskTransitionPosition
	header         e.Header
	catalog        *e.Catalog
	factory        w.TaskTransitionEvents
}

func t0bFacts(t *testing.T, count int) t0bFixture {
	t.Helper()
	created, err := f.NewInstant(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := f.NewInstant(time.Date(2026, 10, 9, 0, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	agentA, agentB := t0bID[i.Agent](t, 8), t0bID[i.Agent](t, 9)
	before := w.Task{ID: t0bID[w.Task](t, 3), ProjectID: t0bID[i.Project](t, 2), MilestoneID: t0bID[w.Milestone](t, 4), SprintID: t0bID[p.Sprint](t, 5), Title: "independent task", Description: "preserve description", Plan: "preserve plan", Type: w.TaskTypeSpike, Priority: w.TaskPriorityHigh, State: w.TaskStateInProgress, AssigneeAgentID: &agentA, ManualRank: strings.Repeat("4", 32), Version: 9, CreatedAt: created, UpdatedAt: created}
	after := before.Clone()
	after.State = w.TaskStateInReview
	after.AssigneeAgentID = &agentB
	after.ManualRank = strings.Repeat("a", 32)
	after.Version = 10
	after.UpdatedAt = committed
	request := w.TaskTransfer{TargetState: w.TaskStateInReview, AssigneeAgentID: &agentB, Comment: t0bPointer(" independent-sensitive-review\n")}
	for n := count - 1; n >= 0; n-- {
		blocker := w.TaskBlockerCreate{BlockerID: t0bID[w.TaskBlockerIdentity](t, 300+n), Type: w.TaskBlockerWaitingForHuman, Description: fmt.Sprintf("independent blocker %d", n), Metadata: w.TaskBlockerMetadata{WaitingForHuman: &w.TaskBlockerWaitingForHumanMetadata{}}}
		if n%2 == 1 {
			blocker.Type = w.TaskBlockerRelyOn
			blocker.Metadata = w.TaskBlockerMetadata{RelyOn: &w.TaskBlockerRelyOnMetadata{RelatedTaskID: t0bID[w.Task](t, 400+n)}}
		}
		request.AddBlockers = append(request.AddBlockers, blocker)
		request.ResolveBlockerIDs = append(request.ResolveBlockerIDs, t0bID[w.TaskBlockerIdentity](t, 200+n))
	}
	var history []w.TaskTransitionEvent
	appendFact := func(kind w.TaskTransitionEventType, payload w.TaskTransitionFactPayload) {
		history = append(history, w.TaskTransitionEvent{ID: t0bID[w.TaskEvent](t, 1000+len(history)), ProjectID: before.ProjectID, TaskID: before.ID, TaskVersion: 10, Type: kind, Actor: w.TaskTransitionActor{UserID: t0bID[i.User](t, 1)}, OperationID: t0bID[w.TaskTransitionCommand](t, 600), CorrelationID: t0bID[w.TaskTransitionCommand](t, 600), Payload: payload, CreatedAt: committed})
	}
	appendFact(w.TaskTransitionStateChanged, w.TaskTransitionFactPayload{StateChanged: &w.TaskStateChangedPayload{FromState: w.TaskStateInProgress, ToState: w.TaskStateInReview}})
	appendFact(w.TaskTransitionAssigneeChanged, w.TaskTransitionFactPayload{AssigneeChanged: &w.TaskAssigneeChangedPayload{FromAgentID: &agentA, ToAgentID: agentB}})
	for n := 0; n < count; n++ {
		appendFact(w.TaskTransitionBlockerResolved, w.TaskTransitionFactPayload{BlockerResolved: &w.TaskBlockerResolvedPayload{BlockerID: t0bID[w.TaskBlockerIdentity](t, 200+n), BlockerType: w.TaskBlockerWaitingForHuman}})
	}
	for n := 0; n < count; n++ {
		kind := w.TaskBlockerWaitingForHuman
		if n%2 == 1 {
			kind = w.TaskBlockerRelyOn
		}
		appendFact(w.TaskTransitionBlockerAdded, w.TaskTransitionFactPayload{BlockerAdded: &w.TaskBlockerAddedPayload{BlockerID: t0bID[w.TaskBlockerIdentity](t, 300+n), BlockerType: kind}})
	}
	appendFact(w.TaskTransitionComment, w.TaskTransitionFactPayload{Comment: &w.TaskCommentPayload{Body: *request.Comment}})
	source := w.TaskTransitionPosition{SprintID: before.SprintID, State: before.State, Priority: before.Priority, PreviousID: t0bPointer(t0bID[w.Task](t, 70)), NextID: t0bPointer(t0bID[w.Task](t, 71)), OrderGeneration: 11}
	target := w.TaskTransitionPosition{SprintID: after.SprintID, State: after.State, Priority: after.Priority, PreviousID: t0bPointer(t0bID[w.Task](t, 72)), OrderGeneration: 23}
	header := e.Header{EventID: t0bID[e.EventIdentity](t, 700), EventType: w.TaskTransitionedName, SchemaVersion: w.TaskTransitionSchemaVersion, OccurredAt: committed, Scope: e.Scope{Kind: e.ProjectScope, ProjectID: t0bID[e.Project](t, 2)}, AggregateType: w.TaskAggregate, AggregateID: t0bID[e.Aggregate](t, 3), AggregateVersion: t0bPointer(f.Version(10))}
	catalog := e.NewCatalog()
	factory, err := w.RegisterTaskTransitionEvents(catalog)
	if err != nil {
		t.Fatal("register independent factory", err)
	}
	if before.Validate() != nil || after.Validate() != nil {
		t.Fatal("old Task fixture is invalid")
	}
	return t0bFixture{before, after, request, history, source, target, header, catalog, factory}
}
func (v t0bFixture) produce() (w.TaskTransitionMutation, e.Event, error) {
	return v.factory.NewTaskTransitionData(v.header, v.before, v.after, v.request, v.history, v.source, v.target)
}
func t0bHistoryIDs(history []w.TaskTransitionEvent) []w.TaskEventID {
	ids := make([]w.TaskEventID, len(history))
	for n := range history {
		ids[n] = history[n].ID
	}
	return ids
}

func TestIndependentT0bDigest(t *testing.T) {
	human, err := i.NewHuman(t0bID[i.User](t, 1), t0bID[i.Session](t, 11))
	if err != nil {
		t.Fatal(err)
	}
	meta := f.CommandMeta{RequestID: t0bID[f.Request](t, 12), IdempotencyKey: "independent/t0b", ExpectedVersion: t0bPointer(f.Version(1))}
	project, task := t0bID[i.Project](t, 2), t0bID[w.Task](t, 3)
	request := w.TaskTransfer{TargetState: w.TaskStateTodo}
	digest, err := w.TaskTransferDigest(human, meta, project, task, request)
	const golden = f.Digest("sha256:ca26dbe5269b1cb0355ab2008a5ddbf2a3f5f33cfdf2f4ea6c1e8443ae9df657")
	if err != nil || digest != golden {
		t.Fatal("fixed independent golden differs", err)
	}
	identity, err := w.TaskTransitionIdentity(project, meta.IdempotencyKey)
	if err != nil || identity.Namespace() != "project" || identity.Command() != "work.task.transfer" || !slices.Equal(identity.OwnerIDs(), []string{project.String()}) || identity.Key() != meta.IdempotencyKey {
		t.Fatal("command identity differs", err)
	}
	otherSession, _ := i.NewHuman(t0bID[i.User](t, 1), t0bID[i.Session](t, 13))
	transport := meta
	transport.RequestID = t0bID[f.Request](t, 14)
	transport.IdempotencyKey = "another:key"
	if got, err := w.TaskTransferDigest(otherSession, transport, project, task, request); err != nil || got != golden {
		t.Fatal("transport identity contaminated digest", err)
	}
	for name, change := range map[string]func(*w.TaskTransfer){"target": func(r *w.TaskTransfer) { r.TargetState = w.TaskStateCancelled }, "assignee": func(r *w.TaskTransfer) { r.AssigneeAgentID = t0bPointer(t0bID[i.Agent](t, 8)) }, "comment": func(r *w.TaskTransfer) { r.Comment = t0bPointer("comment") }} {
		changed := request.Clone()
		change(&changed)
		got, err := w.TaskTransferDigest(human, meta, project, task, changed)
		if err != nil || got == golden {
			t.Fatal("semantic change lost", name, err)
		}
	}
	for _, change := range []func(*f.CommandMeta, *i.Actor, *w.ProjectID, *w.TaskID){
		func(m *f.CommandMeta, _ *i.Actor, _ *w.ProjectID, _ *w.TaskID) {
			m.ExpectedVersion = t0bPointer(f.Version(2))
		},
		func(_ *f.CommandMeta, a *i.Actor, _ *w.ProjectID, _ *w.TaskID) {
			*a, _ = i.NewHuman(t0bID[i.User](t, 20), t0bID[i.Session](t, 11))
		},
		func(_ *f.CommandMeta, _ *i.Actor, p *w.ProjectID, _ *w.TaskID) { *p = t0bID[i.Project](t, 21) },
		func(_ *f.CommandMeta, _ *i.Actor, _ *w.ProjectID, id *w.TaskID) { *id = t0bID[w.Task](t, 22) },
	} {
		m, a, p, id := meta, human, project, task
		change(&m, &a, &p, &id)
		got, err := w.TaskTransferDigest(a, m, p, id, request)
		if err != nil || got == golden {
			t.Fatal("identity/version omitted from digest", err)
		}
	}
	set := t0bFacts(t, 2).request
	before := set.Clone()
	one, err := w.TaskTransferDigest(human, meta, project, task, set)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, set) {
		t.Fatal("digest reordered caller")
	}
	slices.Reverse(set.AddBlockers)
	slices.Reverse(set.ResolveBlockerIDs)
	two, err := w.TaskTransferDigest(human, meta, project, task, set)
	if err != nil || one != two {
		t.Fatal("set permutation changed digest", err)
	}
	empty := request.Clone()
	empty.AddBlockers = []w.TaskBlockerCreate{}
	empty.ResolveBlockerIDs = []w.TaskBlockerID{}
	if got, err := w.TaskTransferDigest(human, meta, project, task, empty); err != nil || got != golden {
		t.Fatal("nil and empty collections differ", err)
	}
	agent, _ := i.NewAgentRun(project, t0bID[i.Agent](t, 8), t0bID[i.Execution](t, 24))
	if got, err := w.TaskTransferDigest(agent, meta, project, task, request); err != nil || got == golden {
		t.Fatal("stable agent subject unsupported/collapsed", err)
	}
	foreign, _ := i.NewAgentRun(t0bID[i.Project](t, 25), t0bID[i.Agent](t, 8), t0bID[i.Execution](t, 24))
	_, err = w.TaskTransferDigest(foreign, meta, project, task, request)
	t0bFault(t, err, f.Forbidden)
	registration, err := i.RegisterService(i.ModelRuntime)
	if err != nil {
		t.Fatal(err)
	}
	service, err := registration.Actor(t0bID[f.Request](t, 30).String(), i.SystemScope())
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.TaskTransferDigest(service, meta, project, task, request)
	t0bFault(t, err, f.Forbidden)
	unbound := request.Clone()
	unbound.AddBlockers = []w.TaskBlockerCreate{{BlockerID: t0bID[w.TaskBlockerIdentity](t, 31), Type: w.TaskBlockerTechnical}}
	for _, actor := range []i.Actor{human, {}} {
		got, err := w.TaskTransferDigest(actor, meta, project, task, unbound)
		t0bFault(t, err, f.DependencyUnbound)
		if got != "" {
			t.Fatal("unbound digest emitted")
		}
	}
	badMeta := meta
	badMeta.ExpectedVersion = nil
	_, err = w.TaskTransferDigest(human, badMeta, project, task, unbound)
	t0bFault(t, err, f.InvalidArgument)
	for _, reverse := range []bool{false, true} {
		bad := unbound.Clone()
		bad.AddBlockers = append(bad.AddBlockers, w.TaskBlockerCreate{BlockerID: t0bID[w.TaskBlockerIdentity](t, 32), Type: w.TaskBlockerWaitingForHuman})
		if reverse {
			slices.Reverse(bad.AddBlockers)
		}
		_, err = w.TaskTransferDigest(i.Actor{}, meta, project, task, bad)
		t0bFault(t, err, f.InvalidArgument)
	}
	_, err = w.TaskTransferDigest(i.Actor{}, meta, project, task, request)
	t0bFault(t, err, f.Unauthenticated)
}

func TestIndependentT0bFactory(t *testing.T) {
	base := t0bFacts(t, 2)
	beforeRequest := base.request.Clone()
	beforeHistory := t0bJSON(t, base.history)
	receipt, event, err := base.produce()
	if err != nil || !reflect.DeepEqual(receipt.Task, base.after) || !slices.Equal(receipt.TaskEventIDs, t0bHistoryIDs(base.history)) || !slices.Equal(receipt.EventIDs, []e.EventID{base.header.EventID}) {
		t.Fatal("complete seven-fact construction failed", err)
	}
	if !reflect.DeepEqual(beforeRequest, base.request) || !bytes.Equal(beforeHistory, t0bJSON(t, base.history)) {
		t.Fatal("factory mutated input")
	}
	payload, err := base.factory.DecodeTaskTransitioned(event)
	if err != nil || payload.CommandID != base.history[0].OperationID || payload.Actor.UserID != base.history[0].Actor.UserID || payload.FromState != base.before.State || payload.ToState != base.after.State || !slices.Equal(payload.TaskEventIDs, t0bHistoryIDs(base.history)) || !reflect.DeepEqual(payload.AssigneeChange, base.history[1].Payload.AssigneeChanged) {
		t.Fatal("factory envelope differs from independent facts", err)
	}
	wantedAgent := *base.after.AssigneeAgentID
	*receipt.Task.AssigneeAgentID = t0bID[i.Agent](t, 1997)
	receipt.TaskEventIDs[0] = t0bID[w.TaskEvent](t, 1998)
	if *base.after.AssigneeAgentID != wantedAgent || base.history[0].ID != t0bID[w.TaskEvent](t, 1000) {
		t.Fatal("factory receipt aliases caller inputs")
	}
	again, err := base.factory.DecodeTaskTransitioned(event)
	if err != nil || again.TaskEventIDs[0] != base.history[0].ID || again.AssigneeChange.ToAgentID != wantedAgent {
		t.Fatal("mutating returned receipt changed immutable event", err)
	}
	cases := []struct {
		name   string
		change func(*t0bFixture)
	}{
		{"other-legal-edge", func(v *t0bFixture) {
			v.history[0].Payload.StateChanged = &w.TaskStateChangedPayload{FromState: w.TaskStateBacklog, ToState: w.TaskStateCancelled}
		}},
		{"other-assignee-pair", func(v *t0bFixture) {
			v.history[1].Payload.AssigneeChanged = &w.TaskAssigneeChangedPayload{FromAgentID: t0bPointer(t0bID[i.Agent](t, 40)), ToAgentID: t0bID[i.Agent](t, 41)}
		}},
		{"assignee-nil-mismatch", func(v *t0bFixture) { v.history[1].Payload.AssigneeChanged.FromAgentID = nil }},
		{"comment-not-request", func(v *t0bFixture) { v.history[len(v.history)-1].Payload.Comment.Body = "another valid body" }},
		{"resolve-id", func(v *t0bFixture) {
			v.history[2].Payload.BlockerResolved.BlockerID = t0bID[w.TaskBlockerIdentity](t, 50)
		}},
		{"resolution-comment", func(v *t0bFixture) {
			v.history[2].Payload.BlockerResolved.ResolutionComment = t0bPointer("valid but forbidden here")
		}},
		{"add-type", func(v *t0bFixture) { v.history[4].Payload.BlockerAdded.BlockerType = w.TaskBlockerRelyOn }},
		{"add-id", func(v *t0bFixture) { v.history[4].Payload.BlockerAdded.BlockerID = t0bID[w.TaskBlockerIdentity](t, 51) }},
		{"missing", func(v *t0bFixture) { v.history = v.history[:len(v.history)-1] }},
		{"extra", func(v *t0bFixture) {
			extra := v.history[len(v.history)-1].Clone()
			extra.ID = t0bID[w.TaskEvent](t, 1200)
			v.history = append(v.history, extra)
		}},
		{"order", func(v *t0bFixture) { v.history[2], v.history[3] = v.history[3], v.history[2] }},
		{"duplicate-id", func(v *t0bFixture) { v.history[3].ID = v.history[2].ID }},
		{"actor", func(v *t0bFixture) { v.history[2].Actor.UserID = t0bID[i.User](t, 52) }},
		{"operation", func(v *t0bFixture) {
			v.history[2].OperationID = t0bID[w.TaskTransitionCommand](t, 53)
			v.history[2].CorrelationID = v.history[2].OperationID
		}},
		{"history-time", func(v *t0bFixture) { v.history[2].CreatedAt = v.before.UpdatedAt }},
		{"history-version", func(v *t0bFixture) { v.history[2].TaskVersion = 9 }},
		{"project", func(v *t0bFixture) { v.history[2].ProjectID = t0bID[i.Project](t, 54) }},
		{"task", func(v *t0bFixture) { v.history[2].TaskID = t0bID[w.Task](t, 55) }},
		{"business-field", func(v *t0bFixture) { v.after.Plan = "unrelated new plan" }},
		{"version-step", func(v *t0bFixture) { v.after.Version = 11 }},
		{"missing-handoff", func(v *t0bFixture) {
			v.request.AssigneeAgentID = nil
			v.after.AssigneeAgentID = v.before.AssigneeAgentID
			v.history = append(v.history[:1], v.history[2:]...)
		}},
		{"missing-review", func(v *t0bFixture) { v.request.Comment = nil; v.history = v.history[:len(v.history)-1] }},
		{"target-next", func(v *t0bFixture) { v.target.NextID = t0bPointer(t0bID[w.Task](t, 90)) }},
		{"source-self", func(v *t0bFixture) { v.source.PreviousID = &v.before.ID }},
		{"source-state", func(v *t0bFixture) { v.source.State = w.TaskStateTodo }},
		{"target-priority", func(v *t0bFixture) { v.target.Priority = w.TaskPriorityLow }},
		{"header-aggregate", func(v *t0bFixture) { v.header.AggregateID = t0bID[e.Aggregate](t, 56) }},
		{"header-project", func(v *t0bFixture) { v.header.Scope.ProjectID = t0bID[e.Project](t, 57) }},
		{"header-version", func(v *t0bFixture) { v.header.AggregateVersion = t0bPointer(f.Version(11)) }},
		{"header-sequence", func(v *t0bFixture) { v.header.AggregateSequence = t0bPointer(f.Sequence(1)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := t0bFacts(t, 2)
			tc.change(&v)
			got, event, err := v.produce()
			t0bFault(t, err, f.InvalidArgument)
			if !reflect.DeepEqual(got, w.TaskTransitionMutation{}) || event.Validate() == nil {
				t.Fatal("failed pure factory returned a result")
			}
		})
	}
	same := t0bFacts(t, 0)
	same.after.AssigneeAgentID = t0bPointer(*same.before.AssigneeAgentID)
	same.request.AssigneeAgentID = t0bPointer(*same.before.AssigneeAgentID)
	same.history = append(same.history[:1], same.history[2:]...)
	if got, _, err := same.produce(); err != nil || len(got.TaskEventIDs) != 2 {
		t.Fatal("explicit same-agent handoff needs only state/comment", err)
	}
	ceiling := t0bFacts(t, 0)
	ceiling.before.Version = f.Version(math.MaxInt64 - 1)
	ceiling.after.Version = f.Version(math.MaxInt64)
	ceiling.header.AggregateVersion = t0bPointer(f.Version(math.MaxInt64))
	for n := range ceiling.history {
		ceiling.history[n].TaskVersion = f.Version(math.MaxInt64)
	}
	if _, _, err := ceiling.produce(); err != nil {
		t.Fatal("last legal version rejected", err)
	}
	ceiling.before.Version = f.Version(math.MaxInt64)
	_, _, err = ceiling.produce()
	t0bFault(t, err, f.InvalidArgument)
}

func TestIndependentT0bRawBoundaries(t *testing.T) {
	base := t0bFacts(t, 1)
	raw := t0bJSON(t, base.request)
	for _, field := range []string{"target_state", "assignee_agent_id", "comment", "add_blockers", "resolve_blocker_ids"} {
		for _, bad := range [][]byte{t0bObjectChange(t, raw, field, json.RawMessage("null")), t0bObjectChange(t, t0bObjectChange(t, raw, field, nil), strings.ToUpper(field), json.RawMessage(`"x"`)), append(bytes.Clone(raw[:len(raw)-1]), []byte(`,"`+field+`":null}`)...)} {
			v := base.request.Clone()
			before := v.Clone()
			t0bFault(t, v.UnmarshalJSON(bad), f.InvalidArgument)
			if !reflect.DeepEqual(v, before) {
				t.Fatal("failed request decode changed receiver")
			}
			zero, err := w.DecodeTaskTransfer(bad)
			t0bFault(t, err, f.InvalidArgument)
			if !reflect.DeepEqual(zero, w.TaskTransfer{}) {
				t.Fatal("partial request returned")
			}
		}
	}
	for _, wire := range []string{`{"target_state":"todo","comment":"\ud800"}`, `{"target_state":"todo","comment":"\udc00"}`, `{"target_state":"todo","\u0074arget_state":"done"}`, `{"target_state":"todo"} false`} {
		_, err := w.DecodeTaskTransfer([]byte(wire))
		t0bFault(t, err, f.InvalidArgument)
	}
	badUTF8 := append([]byte(`{"target_state":"todo","comment":"`), 0xff)
	badUTF8 = append(badUTF8, []byte(`"}`)...)
	_, err := w.DecodeTaskTransfer(badUTF8)
	t0bFault(t, err, f.InvalidArgument)
	for _, field := range []string{"assignee_agent_id", "comment"} {
		got, err := w.DecodeTaskTransfer(t0bObjectChange(t, raw, field, nil))
		if err != nil {
			t.Fatal("valid omitted optional", err)
		}
		if field == "comment" && got.Comment != nil || field == "assignee_agent_id" && got.AssigneeAgentID != nil {
			t.Fatal("presence invented")
		}
	}
	// Construct nested input without any marshaler compacting its token range.
	blockerID := t0bID[w.TaskBlockerIdentity](t, 81).String()
	for _, size := range []int{1024, 1025} {
		metadata := "{" + strings.Repeat(" ", size-2) + "}"
		item := `{"blocker_id":"` + blockerID + `","type":"waiting_for_human","description":"","metadata":` + metadata + `}`
		request := []byte(`{"target_state":"blocked","add_blockers":[` + item + `]}`)
		_, err := w.DecodeTaskTransfer(request)
		if size == 1024 {
			if err != nil {
				t.Fatal("exact nested metadata raw cap", err)
			}
		} else {
			t0bFault(t, err, f.InvalidArgument)
		}
	}
	item := `{"blocker_id":"` + blockerID + `","type":"waiting_for_human","description":"","metadata":{}}`
	for _, size := range []int{8192, 8193} {
		padded := item[:len(item)-1] + strings.Repeat("\n", size-len(item)) + "}"
		_, err := w.DecodeTaskTransfer([]byte(`{"target_state":"blocked","add_blockers":[` + padded + `]}`))
		if size == 8192 {
			if err != nil {
				t.Fatal("exact nested item raw cap", err)
			}
		} else {
			t0bFault(t, err, f.InvalidArgument)
		}
	}
	history := t0bJSON(t, base.history[0])
	actor := `{"type":"human","user_id":"` + t0bID[i.User](t, 1).String() + `","source":"task_domain"}`
	for _, size := range []int{1024, 1025} {
		padded := actor[:len(actor)-1] + strings.Repeat(" ", size-len(actor)) + "}"
		// Raw string replacement is essential: ObjectChange/json.Marshal would
		// compact the RawMessage and invalidate the lexical-cap negative.
		var fields map[string]json.RawMessage
		if json.Unmarshal(history, &fields) != nil {
			t.Fatal("fixture")
		}
		altered := bytes.Replace(history, fields["actor"], []byte(padded), 1)
		_, err := w.DecodeTaskTransitionEvent(altered)
		if size == 1024 {
			if err != nil {
				t.Fatal("exact nested actor cap", err)
			}
		} else {
			t0bFault(t, err, f.InvalidArgument)
		}
	}
	for _, replacement := range []json.RawMessage{json.RawMessage(`"manual"`), json.RawMessage(`0`)} {
		payload := t0bObjectChange(t, t0bJSON(t, base.history[0].Payload.StateChanged), "reason_code", replacement)
		_, err := w.DecodeTaskTransitionEvent(t0bObjectChange(t, history, "payload", payload))
		t0bFault(t, err, f.InvalidArgument)
	}
	for _, actor := range []string{`{"type":"agent","agent_id":"` + t0bID[i.Agent](t, 8).String() + `","execution_id":"` + t0bID[i.Execution](t, 9).String() + `","source":"task_domain"}`, `{"type":"system","service_name":"model-runtime","cause_id":"` + t0bID[f.Request](t, 1).String() + `","source":"executor"}`} {
		_, err := w.DecodeTaskTransitionEvent(t0bObjectChange(t, history, "actor", json.RawMessage(actor)))
		t0bFault(t, err, f.InvalidArgument)
	}
	_, err = w.DecodeTaskCommentPayload([]byte(`{"body":"accepted text followed by \ud800"}`))
	t0bFault(t, err, f.InvalidArgument)
	var historyFields map[string]json.RawMessage
	if json.Unmarshal(history, &historyFields) != nil {
		t.Fatal("history fixture")
	}
	payloadRaw := historyFields["payload"]
	for _, size := range []int{8192, 8193} {
		padded := append(bytes.Clone(payloadRaw[:len(payloadRaw)-1]), bytes.Repeat([]byte(" "), size-len(payloadRaw))...)
		padded = append(padded, '}')
		altered := bytes.Replace(history, payloadRaw, padded, 1)
		_, err := w.DecodeTaskTransitionEvent(altered)
		if size == 8192 {
			if err != nil {
				t.Fatal("exact nested history payload cap", err)
			}
		} else {
			t0bFault(t, err, f.InvalidArgument)
		}
	}
}

func TestIndependentT0bEnvelope(t *testing.T) {
	base := t0bFacts(t, 2)
	_, event, err := base.produce()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base.factory.DecodeTaskTransitioned(event)
	if err != nil {
		t.Fatal(err)
	}
	raw := t0bJSON(t, payload)
	for _, size := range []int{16384, 16385} {
		padded := append(bytes.Repeat([]byte(" "), size-len(raw)), raw...)
		got, err := base.factory.Restore(base.header, padded)
		if size == 16384 {
			if err != nil || got.Validate() != nil {
				t.Fatal("exact own Restore raw cap", err)
			}
		} else {
			t0bFault(t, err, f.InvalidArgument)
			if got.Validate() == nil {
				t.Fatal("invalid Restore yielded event")
			}
		}
	}
	// The neutral catalog canonicalizes before its domain codec. This positive
	// counterexample preserves the explicit limitation: Decode cannot prove
	// the length of raw bytes that the generic Restore already discarded.
	over := append(bytes.Repeat([]byte(" "), 16385-len(raw)), raw...)
	throughGeneric, err := base.catalog.Restore(w.WorkProducer, base.header, over)
	if err != nil {
		t.Fatal("shared Catalog's documented canonicalization boundary changed", err)
	}
	if _, err = base.factory.DecodeTaskTransitioned(throughGeneric); err != nil {
		t.Fatal("canonical event is valid; original raw cap cannot be recovered", err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("envelope fixture")
	}
	for _, key := range []string{"source_position", "target_position"} {
		position := fields[key]
		for _, size := range []int{8192, 8193} {
			padded := append(bytes.Clone(position[:len(position)-1]), bytes.Repeat([]byte("\t"), size-len(position))...)
			padded = append(padded, '}')
			altered := bytes.Replace(raw, position, padded, 1)
			_, err := base.factory.Restore(base.header, altered)
			if size == 8192 {
				if err != nil {
					t.Fatal("exact nested Position cap", key, err)
				}
			} else {
				t0bFault(t, err, f.InvalidArgument)
			}
		}
	}
	if _, err := base.factory.Restore(base.header, append(bytes.Clone(raw), []byte(` {}`)...)); err == nil {
		t.Fatal("Restore admitted trailing value")
	}
	// An unpaired surrogate in an ID or key must never be repaired into a
	// successful domain value by the shared catalog's canonicalization.
	bad := bytes.Replace(raw, []byte(`"from_state"`), []byte(`"from_state\ud800"`), 1)
	_, err = base.factory.Restore(base.header, bad)
	t0bFault(t, err, f.InvalidArgument)
	wrong := base.header
	wrong.SchemaVersion = 2
	_, err = base.factory.Restore(wrong, raw)
	t0bFault(t, err, f.SchemaUnsupported)
	otherCatalog := e.NewCatalog()
	other, err := w.RegisterTaskTransitionEvents(otherCatalog)
	if err != nil {
		t.Fatal(err)
	}
	_, err = other.DecodeTaskTransitioned(event)
	t0bFault(t, err, f.InvalidArgument)
	_, err = base.factory.DecodeTaskTransitioned(e.Event{})
	t0bFault(t, err, f.InvalidArgument)
	if _, err = w.RegisterTaskTransitionEvents(base.catalog); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	if err = otherCatalog.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err = w.RegisterTaskTransitionEvents(otherCatalog); err == nil {
		t.Fatal("sealed registration accepted")
	}
	if _, err = w.RegisterTaskTransitionEvents(nil); err == nil {
		t.Fatal("nil catalog accepted")
	}
	for _, change := range []func(*e.Header){func(h *e.Header) { h.EventType = w.TaskChangedName }, func(h *e.Header) { h.AggregateType = "work.sprint" }, func(h *e.Header) { h.Scope = e.Scope{Kind: e.SystemScope} }, func(h *e.Header) { h.AggregateVersion = nil }, func(h *e.Header) { h.AggregateVersion = t0bPointer(f.Version(1)) }, func(h *e.Header) { h.AggregateSequence = t0bPointer(f.Sequence(1)) }} {
		header := base.header
		change(&header)
		_, err := base.factory.NewTaskTransitioned(header, payload)
		t0bFault(t, err, f.InvalidArgument)
	}
	for _, position := range []*w.TaskTransitionPosition{&payload.SourcePosition, &payload.TargetPosition} {
		old := position.Clone()
		position.PreviousID = &base.after.ID
		_, err = base.factory.NewTaskTransitioned(base.header, payload)
		t0bFault(t, err, f.InvalidArgument)
		*position = old
	}
	canonical := event.PayloadBytes()
	sum := sha256.Sum256(canonical)
	if event.Summary().PayloadDigest != f.Digest("sha256:"+hex.EncodeToString(sum[:])) {
		t.Fatal("event payload digest is not over actual canonical business bytes")
	}
	if bytes.Equal(canonical, t0bJSON(t, event)) {
		t.Fatal("business payload collapsed into safety marker")
	}
	canonical[0] = 'x'
	if bytes.Equal(canonical, event.PayloadBytes()) {
		t.Fatal("Event.PayloadBytes aliases immutable event")
	}
	decoded, err := base.factory.DecodeTaskTransitioned(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded.TaskEventIDs[0] = t0bID[w.TaskEvent](t, 1999)
	*decoded.SourcePosition.PreviousID = t0bID[w.Task](t, 99)
	again, err := base.factory.DecodeTaskTransitioned(event)
	if err != nil || again.TaskEventIDs[0] != base.history[0].ID || *again.SourcePosition.PreviousID != *base.source.PreviousID {
		t.Fatal("typed decode aliases event", err)
	}
}

func TestIndependentT0bCapsAndClones(t *testing.T) {
	base := t0bFacts(t, 16)
	maximum := strings.Repeat("<&>", 32768/3) + "<&"
	if len(maximum) != 32768 {
		t.Fatal("independent byte construction")
	}
	base.before.Title = strings.Repeat("<", 256)
	base.before.Description = maximum
	base.before.Plan = maximum
	base.after.Title = base.before.Title
	base.after.Description = maximum
	base.after.Plan = maximum
	base.request.Comment = &maximum
	base.history[len(base.history)-1].Payload.Comment.Body = maximum
	for n := range base.request.AddBlockers {
		base.request.AddBlockers[n].Description = strings.Repeat("<", 1024)
	}
	receipt, event, err := base.produce()
	if err != nil || len(receipt.TaskEventIDs) != 35 {
		t.Fatal("maximum 35-fact pure result rejected", err)
	}
	lookup := w.TaskTransitionLookup{Status: w.LookupCommitted, Receipt: &receipt}
	comment := w.TaskCommentPayload{Body: maximum}
	lookupRequest := w.TaskTransitionLookupRequest{ProjectID: base.after.ProjectID, Command: w.TaskTransitionTransfer, IdempotencyKey: "independent/t0b-lookup", SemanticDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}
	for _, tc := range []struct {
		name   string
		value  any
		limit  int
		decode func([]byte) error
	}{
		{"request", base.request, 512 << 10, func(b []byte) error { _, err := w.DecodeTaskTransfer(b); return err }},
		{"result", receipt, 512 << 10, func(b []byte) error { _, err := w.DecodeTaskTransitionMutation(b); return err }},
		{"lookup", lookup, 512 << 10, func(b []byte) error { _, err := w.DecodeTaskTransitionLookup(b); return err }},
		{"lookup-request", lookupRequest, 16 << 10, func(b []byte) error { _, err := w.DecodeTaskTransitionLookupRequest(b); return err }},
		{"comment", comment, 256 << 10, func(b []byte) error { _, err := w.DecodeTaskCommentPayload(b); return err }},
		{"comment-record", base.history[len(base.history)-1], 272 << 10, func(b []byte) error { _, err := w.DecodeTaskTransitionEvent(b); return err }},
		{"state-record", base.history[0], 16 << 10, func(b []byte) error { _, err := w.DecodeTaskTransitionEvent(b); return err }},
	} {
		raw := t0bJSON(t, tc.value)
		if len(raw) > tc.limit {
			t.Fatal("legal maximal wire exceeds cap", tc.name, len(raw))
		}
		padded := append(bytes.Clone(raw), bytes.Repeat([]byte(" "), tc.limit-len(raw))...)
		if err := tc.decode(padded); err != nil {
			t.Fatal("exact full raw cap rejected", tc.name, err)
		}
		t0bFault(t, tc.decode(append(padded, ' ')), f.InvalidArgument)
	}
	if len(t0bJSON(t, comment)) != 196619 {
		t.Fatal("max mixed HTML comment escaping length changed")
	}
	requestBefore := t0bJSON(t, base.request)
	copyRequest := base.request.Clone()
	copyRequest.AddBlockers[0].Description = "copy only"
	copyRequest.ResolveBlockerIDs[0] = t0bID[w.TaskBlockerIdentity](t, 9000)
	*copyRequest.AssigneeAgentID = t0bID[i.Agent](t, 9001)
	for n := range copyRequest.AddBlockers {
		if copyRequest.AddBlockers[n].Metadata.RelyOn != nil {
			copyRequest.AddBlockers[n].Metadata.RelyOn.RelatedTaskID = t0bID[w.Task](t, 9002)
			break
		}
	}
	if base.request.AddBlockers[0].Description == "copy only" || base.request.ResolveBlockerIDs[0] == copyRequest.ResolveBlockerIDs[0] || *base.request.AssigneeAgentID == *copyRequest.AssigneeAgentID {
		t.Fatal("request Clone aliases mutable data")
	}
	if !bytes.Equal(requestBefore, t0bJSON(t, base.request)) {
		t.Fatal("request Clone aliases nested Blocker metadata")
	}
	historyBefore := t0bJSON(t, base.history)
	factCopy := base.history[1].Clone()
	*factCopy.Payload.AssigneeChanged.FromAgentID = t0bID[i.Agent](t, 9006)
	commentCopy := base.history[len(base.history)-1].Clone()
	commentCopy.Payload.Comment.Body = "copy only"
	if !bytes.Equal(historyBefore, t0bJSON(t, base.history)) {
		t.Fatal("history Clone aliases its typed payload union")
	}
	copyLookup := lookup.Clone()
	copyLookup.Receipt.TaskEventIDs[0] = t0bID[w.TaskEvent](t, 9003)
	*copyLookup.Receipt.Task.AssigneeAgentID = t0bID[i.Agent](t, 9004)
	copyLookup.Receipt.EventIDs[0] = t0bID[e.EventIdentity](t, 9005)
	if lookup.Receipt.TaskEventIDs[0] == copyLookup.Receipt.TaskEventIDs[0] || *lookup.Receipt.Task.AssigneeAgentID == *copyLookup.Receipt.Task.AssigneeAgentID || lookup.Receipt.EventIDs[0] == copyLookup.Receipt.EventIDs[0] {
		t.Fatal("lookup Clone aliases receipt")
	}
	for n := 0; n < 4; n++ {
		t.Run(fmt.Sprintf("reader-%d", n), func(t *testing.T) {
			t.Parallel()
			for j := 0; j < 8; j++ {
				value, err := base.factory.DecodeTaskTransitioned(event)
				if err != nil || len(value.TaskEventIDs) != 35 {
					t.Fatal("concurrent decode", err)
				}
				value.TaskEventIDs[0] = t0bID[w.TaskEvent](t, 9999)
				if base.request.Validate() != nil {
					t.Fatal("shared input invalidated")
				}
			}
		})
	}
}

func TestIndependentT0bLegacyAndLog(t *testing.T) {
	base := t0bFacts(t, 1)
	receipt, event, err := base.produce()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base.factory.DecodeTaskTransitioned(event)
	if err != nil {
		t.Fatal(err)
	}
	values := []any{base.request, &base.request, receipt, &receipt, base.history[0], &base.history[0], base.history[len(base.history)-1], base.history[0].Actor, base.history[0].Payload, *base.history[0].Payload.StateChanged, *base.history[1].Payload.AssigneeChanged, *base.history[len(base.history)-1].Payload.Comment, payload, &payload, base.factory, w.TaskTransitionTransfer, w.TaskTransitionComment, w.TaskTransitionLookup{Status: w.LookupCommitted, Receipt: &receipt}}
	for _, value := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if fmt.Sprintf(verb, value) != "work_task_transition" {
				t.Fatal("direct safe Formatter is not fixed")
			}
		}
		var out bytes.Buffer
		slog.New(slog.NewJSONHandler(&out, nil)).Info("probe", slog.Any("value", value))
		var parsed map[string]any
		if json.Unmarshal(out.Bytes(), &parsed) != nil || parsed["value"] != "work_task_transition" {
			t.Fatal("direct safe LogValuer changed")
		}
	}
	if !bytes.Contains(t0bJSON(t, base.request), []byte("independent-sensitive-review")) {
		t.Fatal("business JSON was incorrectly redacted")
	}
	// Outer reflection/JSON fallback logging is outside the accepted guarantee.
	t0bFault(t, w.TaskCommandName("work.task.transfer").Validate(), f.InvalidArgument)
	for _, history := range base.history {
		raw := t0bJSON(t, history)
		if new(w.TaskEvent).UnmarshalJSON(raw) == nil {
			t.Fatal("legacy TaskEvent admitted a transition")
		}
	}
	if new(w.TaskMutation).UnmarshalJSON(t0bJSON(t, receipt)) == nil || new(w.TaskChanged).UnmarshalJSON(t0bJSON(t, payload)) == nil {
		t.Fatal("legacy single-history schema widened")
	}
	if new(w.TaskPosition).UnmarshalJSON(t0bJSON(t, base.target)) == nil {
		t.Fatal("legacy backlog Position widened")
	}
	for _, old := range []interface{ UnmarshalJSON([]byte) error }{new(w.TaskCreate), new(w.TaskFieldsUpdate), new(w.TaskReorder)} {
		if old.UnmarshalJSON(t0bJSON(t, base.request)) == nil {
			t.Fatal("legacy request admitted transfer")
		}
	}
	for _, receiver := range []interface{ UnmarshalJSON([]byte) error }{(*w.TaskTransfer)(nil), (*w.TaskTransitionMutation)(nil), (*w.TaskTransitionLookupRequest)(nil), (*w.TaskTransitionLookup)(nil), (*w.TaskTransitionActor)(nil), (*w.TaskStateChangedPayload)(nil), (*w.TaskAssigneeChangedPayload)(nil), (*w.TaskCommentPayload)(nil), (*w.TaskTransitionEvent)(nil), (*w.TaskTransitioned)(nil)} {
		t0bFault(t, receiver.UnmarshalJSON([]byte(`{}`)), f.InvalidArgument)
	}
}
