package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func transferFixture(t *testing.T) TaskTransfer {
	t.Helper()
	agent, comment := testID[i.Agent](t, 40), " exact <comment> 界\t\n\r "
	a, b := blockerFixture(t, TaskBlockerRelyOn), blockerFixture(t, TaskBlockerWaitingForHuman)
	a.BlockerID, b.BlockerID = testID[TaskBlockerIdentity](t, 41), testID[TaskBlockerIdentity](t, 42)
	return TaskTransfer{TargetState: TaskStateBlocked, AssigneeAgentID: &agent, Comment: &comment,
		AddBlockers: []TaskBlockerCreate{b, a}, ResolveBlockerIDs: []TaskBlockerID{testID[TaskBlockerIdentity](t, 44), testID[TaskBlockerIdentity](t, 43)}}
}

func transferReceipt(t *testing.T) TaskTransitionMutation {
	t.Helper()
	task := taskFixture(t)
	task.Version = 2
	return TaskTransitionMutation{Task: task, TaskEventIDs: []TaskEventID{testID[TaskEvent](t, 50)}, EventIDs: []event.EventID{testID[event.EventIdentity](t, 51)}}
}

func rejectTransfer(t *testing.T, raw []byte, code f.Code) {
	t.Helper()
	v := transferFixture(t)
	before := v.Clone()
	transitionRequireFault(t, v.UnmarshalJSON(raw), code)
	if !reflect.DeepEqual(v, before) {
		t.Fatal("failure changed request receiver")
	}
	got, err := DecodeTaskTransfer(raw)
	transitionRequireFault(t, err, code)
	if !reflect.DeepEqual(got, TaskTransfer{}) {
		t.Fatal("failure returned partial request")
	}
}

func TestTaskTransferPresenceAndSets(t *testing.T) {
	v := transferFixture(t)
	before := v.Clone()
	raw, err := v.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeTaskTransfer(raw)
	if err != nil || !reflect.DeepEqual(got, v.normalized()) || !reflect.DeepEqual(v, before) {
		t.Fatal("request roundtrip or caller mutation", err)
	}
	for _, state := range []TaskState{TaskStateBacklog, TaskStateTodo, TaskStateInProgress, TaskStateInReview, TaskStateBlocked, TaskStateDone, TaskStateCancelled} {
		minimal, err := DecodeTaskTransfer([]byte(`{"target_state":` + string(taskRaw(t, state)) + `}`))
		if err != nil || minimal.AddBlockers == nil || minimal.ResolveBlockerIDs == nil || minimal.AssigneeAgentID != nil || minimal.Comment != nil {
			t.Fatal("target-only shape rejected or presence changed", err)
		}
		if string(taskRaw(t, minimal)) != `{"target_state":`+string(taskRaw(t, state))+`,"add_blockers":[],"resolve_blocker_ids":[]}` {
			t.Fatal("empty sets not expanded")
		}
	}
	for _, key := range []string{"target_state", "assignee_agent_id", "comment", "add_blockers", "resolve_blocker_ids"} {
		rejectTransfer(t, taskJSONChange(t, raw, key, []byte("null")), f.InvalidArgument)
	}
	rejectTransfer(t, taskJSONChange(t, raw, "target_state", nil), f.InvalidArgument)
	for _, bad := range []string{
		`{}`, `null`, `[]`, `{"target_state":"TODO"}`, `{"target_state":1}`, `{"Target_state":"todo"}`,
		`{"target_state":"todo","target_state":"todo"}`, `{"target_state":"todo","\u0074arget_state":"todo"}`,
		`{"target_state":"todo","unknown":true}`, `{"target_state":"todo"} {}`,
		`{"target_state":"todo","comment":"\ud800"}`, `{"target_state":"todo","comment":"\udfff"}`,
		`{"target_state":"todo","add_blockers":{}}`, `{"target_state":"todo","resolve_blocker_ids":[1]}`,
		`{"target_state":"todo","assignee_agent_id":0}`, `{"target_state":"todo","assignee_agent_id":""}`,
	} {
		rejectTransfer(t, []byte(bad), f.InvalidArgument)
	}
	rejectTransfer(t, []byte("{\"target_state\":\"todo\",\"comment\":\"\xff\"}"), f.InvalidArgument)
	for _, comment := range []string{"", " \t\n\r\u2003", "x\x00", "x\x7f", "x\u0085", strings.Repeat("x", MaxTaskTransitionCommentBytes+1)} {
		bad := v.Clone()
		bad.Comment = &comment
		transitionRequireFault(t, bad.Validate(), f.InvalidArgument)
	}
	clone := v.Clone()
	*clone.AssigneeAgentID = testID[i.Agent](t, 99)
	*clone.Comment = "changed"
	clone.AddBlockers[1].Metadata.RelyOn.RelatedTaskID = testID[Task](t, 99)
	clone.ResolveBlockerIDs[0] = testID[TaskBlockerIdentity](t, 99)
	if !reflect.DeepEqual(v, before) {
		t.Fatal("clone borrowed pointer or slice")
	}
	max := TaskTransfer{TargetState: TaskStateBlocked}
	for n := 0; n < 16; n++ {
		b := blockerFixture(t, TaskBlockerWaitingForHuman)
		b.BlockerID = testID[TaskBlockerIdentity](t, 100+n)
		max.AddBlockers = append(max.AddBlockers, b)
		max.ResolveBlockerIDs = append(max.ResolveBlockerIDs, testID[TaskBlockerIdentity](t, 120+n))
	}
	if max.Validate() != nil {
		t.Fatal("32 entries rejected")
	}
	for _, change := range []func(*TaskTransfer){
		func(x *TaskTransfer) { x.AddBlockers = append(x.AddBlockers, x.AddBlockers[0]) },
		func(x *TaskTransfer) { x.ResolveBlockerIDs = append(x.ResolveBlockerIDs, x.ResolveBlockerIDs[0]) },
		func(x *TaskTransfer) { x.AddBlockers[1].BlockerID = x.AddBlockers[0].BlockerID },
		func(x *TaskTransfer) { x.ResolveBlockerIDs[1] = x.ResolveBlockerIDs[0] },
		func(x *TaskTransfer) { x.ResolveBlockerIDs[0] = x.AddBlockers[0].BlockerID },
		func(x *TaskTransfer) { x.ResolveBlockerIDs[0] = TaskBlockerID{} },
	} {
		x := max.Clone()
		change(&x)
		transitionRequireFault(t, x.Validate(), f.InvalidArgument)
		if out, err := x.MarshalJSON(); err == nil || out != nil {
			t.Fatal("bad set marshaled")
		}
	}
	for _, kind := range blockerUnboundTypes() {
		unbound := blockerFixture(t, kind)
		x := TaskTransfer{TargetState: TaskStateTodo, AddBlockers: []TaskBlockerCreate{unbound}}
		transitionRequireFault(t, x.Validate(), f.DependencyUnbound)
		_, err := x.MarshalJSON()
		transitionRequireFault(t, err, f.DependencyUnbound)
		item := string(blockerWire(t, kind, "", `{}`))
		rejectTransfer(t, []byte(`{"target_state":"todo","add_blockers":[`+item+`]}`), f.DependencyUnbound)
		bad := blockerFixture(t, TaskBlockerRelyOn)
		bad.BlockerID = TaskBlockerID{}
		for _, reverse := range []bool{false, true} {
			x.AddBlockers = []TaskBlockerCreate{unbound, bad}
			if reverse {
				slices.Reverse(x.AddBlockers)
			}
			transitionRequireFault(t, x.Validate(), f.InvalidArgument)
			items := item + `,{"blocker_id":null,"type":"rely_on","description":"","metadata":{}}`
			if reverse {
				items = `{"blocker_id":null,"type":"rely_on","description":"","metadata":{}},` + item
			}
			rejectTransfer(t, []byte(`{"target_state":"todo","add_blockers":[`+items+`]}`), f.InvalidArgument)
		}
	}
	for _, metadata := range []string{`{"meeting_id":"x"}`, `{"execution_id":"x"}`, `{"other":true}`} {
		rejectTransfer(t, []byte(`{"target_state":"todo","add_blockers":[`+string(blockerWire(t, TaskBlockerWaitingForHuman, "", metadata))+`]}`), f.InvalidArgument)
	}
	item := blockerWire(t, TaskBlockerWaitingForHuman, "", `{}`)
	oversizeItem := `{` + strings.Repeat(" ", MaxTaskBlockerCreateBytes-len(item)+1) + string(item[1:])
	rejectTransfer(t, []byte(`{"target_state":"todo","add_blockers":[`+oversizeItem+`]}`), f.InvalidArgument)
	metadata := `{` + strings.Repeat(" ", MaxTaskBlockerMetadataBytes-1) + `}`
	rejectTransfer(t, []byte(`{"target_state":"todo","add_blockers":[`+string(blockerWire(t, TaskBlockerWaitingForHuman, "", metadata))+`]}`), f.InvalidArgument)
}

func TestTaskTransferDigestAndLookup(t *testing.T) {
	project, _ := f.ParseID[i.Project]("00000000-0000-7000-8000-000000000002")
	task, _ := f.ParseID[Task]("00000000-0000-7000-8000-000000000003")
	user, _ := f.ParseID[i.User]("00000000-0000-7000-8000-000000000001")
	actor, _ := i.NewHuman(user, testID[i.Session](t, 9))
	version := f.Version(1)
	meta := testMeta(t, &version)
	minimal := TaskTransfer{TargetState: TaskStateTodo}
	golden, err := TaskTransferDigest(actor, meta, project, task, minimal)
	if err != nil || golden != "sha256:ca26dbe5269b1cb0355ab2008a5ddbf2a3f5f33cfdf2f4ea6c1e8443ae9df657" {
		t.Fatal("golden digest mismatch", err, string(golden))
	}
	digest := func(a i.Actor, m f.CommandMeta, p ProjectID, id TaskID, r TaskTransfer) f.Digest {
		t.Helper()
		d, err := TaskTransferDigest(a, m, p, id, r)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	r := transferFixture(t)
	base := digest(actor, meta, project, task, r)
	for _, change := range []func(*TaskTransfer){
		func(x *TaskTransfer) { x.TargetState = TaskStateDone },
		func(x *TaskTransfer) { x.AssigneeAgentID = nil },
		func(x *TaskTransfer) { *x.AssigneeAgentID = testID[i.Agent](t, 90) },
		func(x *TaskTransfer) { x.Comment = nil },
		func(x *TaskTransfer) { *x.Comment += " " },
		func(x *TaskTransfer) { x.AddBlockers[0].Description += " " },
		func(x *TaskTransfer) { x.AddBlockers[0].BlockerID = testID[TaskBlockerIdentity](t, 90) },
		func(x *TaskTransfer) { x.AddBlockers[1].Metadata.RelyOn.RelatedTaskID = testID[Task](t, 90) },
		func(x *TaskTransfer) {
			x.AddBlockers[1].Type = TaskBlockerWaitingForHuman
			x.AddBlockers[1].Metadata = TaskBlockerMetadata{WaitingForHuman: &TaskBlockerWaitingForHumanMetadata{}}
		},
		func(x *TaskTransfer) { x.ResolveBlockerIDs[0] = testID[TaskBlockerIdentity](t, 90) },
	} {
		x := r.Clone()
		change(&x)
		if digest(actor, meta, project, task, x) == base {
			t.Fatal("semantic field absent from digest")
		}
	}
	reordered := r.Clone()
	slices.Reverse(reordered.AddBlockers)
	slices.Reverse(reordered.ResolveBlockerIDs)
	if digest(actor, meta, project, task, reordered) != base {
		t.Fatal("set order affects digest")
	}
	empty := minimal.Clone()
	empty.AddBlockers = []TaskBlockerCreate{}
	empty.ResolveBlockerIDs = []TaskBlockerID{}
	if digest(actor, meta, project, task, empty) != golden {
		t.Fatal("nil/empty digest differs")
	}
	otherSession, _ := i.NewHuman(user, testID[i.Session](t, 10))
	otherMeta := meta
	otherMeta.RequestID = testID[f.Request](t, 91)
	otherMeta.IdempotencyKey = "other-key"
	if digest(otherSession, otherMeta, project, task, r) != base {
		t.Fatal("transport identity affects digest")
	}
	v2 := f.Version(2)
	m2 := meta
	m2.ExpectedVersion = &v2
	if digest(actor, m2, project, task, r) == base || digest(testActor(t, 99, 10), meta, project, task, r) == base || digest(actor, meta, testID[i.Project](t, 99), task, r) == base || digest(actor, meta, project, testID[Task](t, 99), r) == base {
		t.Fatal("subject/target/version absent from digest")
	}
	agent, _ := i.NewAgentRun(project, testID[i.Agent](t, 61), testID[i.Execution](t, 62))
	agent2, _ := i.NewAgentRun(project, testID[i.Agent](t, 61), testID[i.Execution](t, 63))
	agent3, _ := i.NewAgentRun(project, testID[i.Agent](t, 64), testID[i.Execution](t, 62))
	ad := digest(agent, meta, project, task, r)
	if ad == base || ad == digest(agent2, meta, project, task, r) || ad == digest(agent3, meta, project, task, r) {
		t.Fatal("AgentRun subject omitted")
	}
	serviceReg, _ := i.RegisterService(i.ModelRuntime)
	service, err := serviceReg.Actor(testID[f.Request](t, 71).String(), i.SystemScope())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		actor   i.Actor
		meta    f.CommandMeta
		project ProjectID
		request TaskTransfer
		code    f.Code
	}{
		{i.Actor{}, meta, project, minimal, f.Unauthenticated},
		{service, meta, project, minimal, f.Forbidden},
		{agent, meta, testID[i.Project](t, 99), minimal, f.Forbidden},
		{actor, f.CommandMeta{}, project, minimal, f.InvalidArgument},
		{actor, testMeta(t, nil), project, minimal, f.InvalidArgument},
		{i.Actor{}, meta, ProjectID{}, minimal, f.InvalidArgument},
	} {
		d, err := TaskTransferDigest(tc.actor, tc.meta, tc.project, task, tc.request)
		transitionRequireFault(t, err, tc.code)
		if d != "" {
			t.Fatal("failed digest nonempty")
		}
	}
	unbound := TaskTransfer{TargetState: TaskStateTodo, AddBlockers: []TaskBlockerCreate{blockerFixture(t, TaskBlockerTechnical)}}
	for _, a := range []i.Actor{actor, {}} {
		d, err := TaskTransferDigest(a, meta, project, task, unbound)
		transitionRequireFault(t, err, f.DependencyUnbound)
		if d != "" {
			t.Fatal("unbound digest nonempty")
		}
	}
	_, err = TaskTransferDigest(actor, f.CommandMeta{}, project, task, unbound)
	transitionRequireFault(t, err, f.InvalidArgument)
	identity, err := TaskTransitionIdentity(project, meta.IdempotencyKey)
	if err != nil || identity.Namespace() != "project" || !reflect.DeepEqual(identity.OwnerIDs(), []string{project.String()}) || identity.Command() != "work.task.transfer" || identity.Key() != meta.IdempotencyKey {
		t.Fatal("command identity", err)
	}
	for _, bad := range []struct {
		p ProjectID
		k f.IdempotencyKey
	}{{ProjectID{}, meta.IdempotencyKey}, {project, ""}} {
		_, err := TaskTransitionIdentity(bad.p, bad.k)
		transitionRequireFault(t, err, f.InvalidArgument)
	}
	receipt := transferReceipt(t)
	lookupRequest := TaskTransitionLookupRequest{ProjectID: project, Command: TaskTransitionTransfer, IdempotencyKey: meta.IdempotencyKey, SemanticDigest: golden}
	if got, err := DecodeTaskTransitionLookupRequest(taskRaw(t, lookupRequest)); err != nil || got != lookupRequest {
		t.Fatal("lookup request roundtrip", err)
	}
	for _, field := range []string{"project_id", "command", "idempotency_key", "semantic_digest"} {
		for _, replacement := range []json.RawMessage{nil, []byte("null")} {
			x := lookupRequest
			transitionRequireFault(t, x.UnmarshalJSON(taskJSONChange(t, taskRaw(t, lookupRequest), field, replacement)), f.InvalidArgument)
			if x != lookupRequest {
				t.Fatal("lookup request receiver changed")
			}
		}
	}
	for _, status := range []LookupState{LookupCommitted, LookupInProgress, LookupNotObserved} {
		x := TaskTransitionLookup{Status: status}
		if status == LookupCommitted {
			x.Receipt = &receipt
		}
		if got, err := DecodeTaskTransitionLookup(taskRaw(t, x)); err != nil || !reflect.DeepEqual(got, x) {
			t.Fatal("lookup roundtrip", err)
		}
		if x.Receipt == nil {
			x.Receipt = &receipt
		} else {
			x.Receipt = nil
		}
		transitionRequireFault(t, x.Validate(), f.InvalidArgument)
	}
	for _, bad := range []string{`{"status":"committed","receipt":null}`, `{"status":"not_observed"}`, `{"status":null,"receipt":null}`, `{"status":"other","receipt":null}`, `{"status":"not_observed","receipt":null,"unknown":0}`} {
		x := TaskTransitionLookup{Status: LookupCommitted, Receipt: &receipt}
		before := x.Clone()
		transitionRequireFault(t, x.UnmarshalJSON([]byte(bad)), f.InvalidArgument)
		if !reflect.DeepEqual(x, before) {
			t.Fatal("lookup receiver changed")
		}
		got, err := DecodeTaskTransitionLookup([]byte(bad))
		transitionRequireFault(t, err, f.InvalidArgument)
		if !reflect.DeepEqual(got, TaskTransitionLookup{}) {
			t.Fatal("partial lookup")
		}
	}
}

func TestTaskTransitionContractBoundsAndLegacy(t *testing.T) {
	r := transferFixture(t)
	comment := strings.Repeat("<", MaxTaskTransitionCommentBytes)
	r.Comment = &comment
	r.AddBlockers = nil
	r.ResolveBlockerIDs = nil
	for n := 0; n < 16; n++ {
		b := blockerFixture(t, TaskBlockerRelyOn)
		b.BlockerID = testID[TaskBlockerIdentity](t, 100+n)
		b.Description = strings.Repeat("<", MaxTaskBlockerDescriptionBytes)
		r.AddBlockers = append(r.AddBlockers, b)
		r.ResolveBlockerIDs = append(r.ResolveBlockerIDs, testID[TaskBlockerIdentity](t, 120+n))
	}
	receipt := transferReceipt(t)
	receipt.Task.Title = strings.Repeat("<", 256)
	receipt.Task.Description = strings.Repeat("<", 32768)
	receipt.Task.Plan = strings.Repeat("<", 32768)
	receipt.TaskEventIDs = nil
	for n := 0; n < 35; n++ {
		receipt.TaskEventIDs = append(receipt.TaskEventIDs, testID[TaskEvent](t, 200+n))
	}
	lookup := TaskTransitionLookup{Status: LookupCommitted, Receipt: &receipt}
	// Combine the largest legitimate values through the data factory as well
	// as each codec: state + assignee + 16 resolves + 16 adds + one comment.
	x := transitionDataFixture(t, false)
	x.before.Title, x.after.Title = receipt.Task.Title, receipt.Task.Title
	x.before.Description, x.after.Description = receipt.Task.Description, receipt.Task.Description
	x.before.Plan, x.after.Plan = receipt.Task.Plan, receipt.Task.Plan
	x.request.AddBlockers, x.request.ResolveBlockerIDs = r.Clone().AddBlockers, slices.Clone(r.ResolveBlockerIDs)
	x.request.Comment = &comment
	x.history = x.history[:2]
	for _, id := range x.request.ResolveBlockerIDs {
		x.history = append(x.history, TaskTransitionEvent{Type: TaskTransitionBlockerResolved,
			Payload: TaskTransitionFactPayload{BlockerResolved: &TaskBlockerResolvedPayload{BlockerID: id, BlockerType: TaskBlockerRelyOn}}})
	}
	for _, item := range x.request.AddBlockers {
		x.history = append(x.history, TaskTransitionEvent{Type: TaskTransitionBlockerAdded,
			Payload: TaskTransitionFactPayload{BlockerAdded: &TaskBlockerAddedPayload{BlockerID: item.BlockerID, BlockerType: item.Type}}})
	}
	x.history = append(x.history, TaskTransitionEvent{Type: TaskTransitionComment,
		Payload: TaskTransitionFactPayload{Comment: &TaskCommentPayload{Body: comment}}})
	for n := range x.history {
		x.history[n].ID = testID[TaskEvent](t, 300+n)
		x.history[n].ProjectID, x.history[n].TaskID, x.history[n].TaskVersion = x.after.ProjectID, x.after.ID, x.after.Version
		x.history[n].Actor, x.history[n].OperationID, x.history[n].CorrelationID = x.history[0].Actor, x.history[0].OperationID, x.history[0].CorrelationID
		x.history[n].CreatedAt = x.h.OccurredAt
		if _, err := DecodeTaskTransitionEvent(taskRaw(t, x.history[n])); err != nil {
			t.Fatal("maximum combined history codec", err)
		}
	}
	_, factory := transitionFactoryFixture(t)
	combined, envelope, err := x.build(factory)
	if err != nil || len(combined.TaskEventIDs) != 35 || len(combined.EventIDs) != 1 {
		t.Fatal("maximum combined data factory", err)
	}
	if _, err := factory.DecodeTaskTransitioned(envelope); err != nil {
		t.Fatal("maximum combined envelope", err)
	}
	if _, err := DecodeTaskTransitionLookup(taskRaw(t, TaskTransitionLookup{Status: LookupCommitted, Receipt: &combined})); err != nil {
		t.Fatal("maximum combined lookup", err)
	}
	for _, tc := range []struct {
		name   string
		value  any
		cap    int
		decode func([]byte) error
	}{
		{"request", r, MaxTaskTransferBytes, func(raw []byte) error { _, err := DecodeTaskTransfer(raw); return err }},
		{"receipt", receipt, MaxTaskTransitionResultBytes, func(raw []byte) error { _, err := DecodeTaskTransitionMutation(raw); return err }},
		{"lookup", lookup, MaxTaskTransitionLookupBytes, func(raw []byte) error { _, err := DecodeTaskTransitionLookup(raw); return err }},
		{"lookup_request", TaskTransitionLookupRequest{ProjectID: receipt.Task.ProjectID, Command: TaskTransitionTransfer, IdempotencyKey: "lookup", SemanticDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}, MaxTaskLookupRequestBytes, func(raw []byte) error { _, err := DecodeTaskTransitionLookupRequest(raw); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := taskRaw(t, tc.value)
			if len(raw) > tc.cap || tc.decode(raw) != nil {
				t.Fatal("maximum legitimate value fails cap")
			}
			padded := append(bytes.Clone(raw), bytes.Repeat([]byte(" "), tc.cap-len(raw))...)
			if tc.decode(padded) != nil {
				t.Fatal("exact raw cap rejected")
			}
			transitionRequireFault(t, tc.decode(append(padded, ' ')), f.InvalidArgument)
		})
	}
	commandRaw := []byte(`"work.task.transfer"`)
	commandRaw = append(commandRaw, bytes.Repeat([]byte(" "), MaxTaskTransitionNameBytes-len(commandRaw))...)
	var command TaskTransitionCommandName
	if command.UnmarshalJSON(commandRaw) != nil {
		t.Fatal("exact command cap")
	}
	transitionRequireFault(t, command.UnmarshalJSON(append(commandRaw, ' ')), f.InvalidArgument)
	for _, bad := range []string{`null`, `"work.task.create"`, `"WORK.TASK.TRANSFER"`, `1`, `"work.task.transfer" true`} {
		transitionRequireFault(t, command.UnmarshalJSON([]byte(bad)), f.InvalidArgument)
		if command != TaskTransitionTransfer {
			t.Fatal("command failure changed receiver")
		}
	}
	for _, bad := range []func(*TaskTransitionMutation){
		func(x *TaskTransitionMutation) { x.Task.Version = 1 },
		func(x *TaskTransitionMutation) { x.TaskEventIDs = nil },
		func(x *TaskTransitionMutation) { x.TaskEventIDs = append(x.TaskEventIDs, testID[TaskEvent](t, 500)) },
		func(x *TaskTransitionMutation) { x.TaskEventIDs[0] = TaskEventID{} },
		func(x *TaskTransitionMutation) { x.TaskEventIDs[1] = x.TaskEventIDs[0] },
		func(x *TaskTransitionMutation) { slices.Reverse(x.TaskEventIDs) },
		func(x *TaskTransitionMutation) { x.EventIDs = nil },
		func(x *TaskTransitionMutation) { x.EventIDs = append(x.EventIDs, x.EventIDs[0]) },
		func(x *TaskTransitionMutation) { x.EventIDs[0] = event.EventID{} },
	} {
		x := receipt.Clone()
		bad(&x)
		transitionRequireFault(t, x.Validate(), f.InvalidArgument)
	}
	for _, field := range []string{"task", "task_event_ids", "event_ids"} {
		for _, replacement := range []json.RawMessage{nil, []byte("null")} {
			x := receipt.Clone()
			before := x.Clone()
			raw := taskJSONChange(t, taskRaw(t, receipt), field, replacement)
			transitionRequireFault(t, x.UnmarshalJSON(raw), f.InvalidArgument)
			if !reflect.DeepEqual(x, before) {
				t.Fatal("mutation failure changed receiver")
			}
			got, err := DecodeTaskTransitionMutation(raw)
			transitionRequireFault(t, err, f.InvalidArgument)
			if !reflect.DeepEqual(got, TaskTransitionMutation{}) {
				t.Fatal("partial mutation")
			}
		}
	}
	clone := lookup.Clone()
	clone.Receipt.TaskEventIDs[0] = testID[TaskEvent](t, 900)
	clone.Receipt.EventIDs[0] = testID[event.EventIdentity](t, 900)
	clone.Receipt.Task.Title = "changed"
	if reflect.DeepEqual(clone, lookup) || lookup.Receipt.Task.Title != receipt.Task.Title || lookup.Receipt.TaskEventIDs[0] != receipt.TaskEventIDs[0] {
		t.Fatal("lookup clone alias")
	}
	for _, ptr := range []interface{ UnmarshalJSON([]byte) error }{(*TaskTransfer)(nil), (*TaskTransitionCommandName)(nil), (*TaskTransitionMutation)(nil), (*TaskTransitionLookupRequest)(nil), (*TaskTransitionLookup)(nil)} {
		transitionRequireFault(t, ptr.UnmarshalJSON([]byte(`{}`)), f.InvalidArgument)
	}
	for _, value := range []any{r, receipt, lookup, TaskTransitionTransfer, TaskTransitionLookupRequest{}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if fmt.Sprintf(format, value) != "work_task_transition" {
				t.Fatal("unsafe direct format")
			}
		}
		if value.(slog.LogValuer).LogValue().String() != "work_task_transition" {
			t.Fatal("unsafe direct log")
		}
	}
	if !bytes.Contains(taskRaw(t, r), []byte(`\u003c`)) {
		t.Fatal("business JSON lost comment")
	}
	transitionRequireFault(t, new(TaskCommandName).UnmarshalJSON([]byte(`"work.task.transfer"`)), f.InvalidArgument)
	transitionRequireFault(t, new(TaskCreate).UnmarshalJSON(taskRaw(t, r)), f.InvalidArgument)
	transitionRequireFault(t, new(TaskMutation).UnmarshalJSON(taskRaw(t, receipt)), f.InvalidArgument)
	transitionRequireFault(t, new(TaskEvent).UnmarshalJSON(taskRaw(t, transitionHistoryFixture(t, TaskTransitionStateChanged))), f.InvalidArgument)
	legacy := taskHistoryFixture(t)
	var oldHistory TaskEvent
	if err := oldHistory.UnmarshalJSON(taskRaw(t, legacy)); err != nil || !reflect.DeepEqual(oldHistory, legacy) {
		t.Fatal("legacy history regressed", err)
	}
	_, _, oldChanged := taskEventFixture(t)
	if oldChanged.Validate() != nil {
		t.Fatal("old positive envelope regressed")
	}
	transitionRequireFault(t, new(TaskChanged).UnmarshalJSON(taskJSONChange(t, taskRaw(t, oldChanged), "task_event_ids", taskRaw(t, receipt.TaskEventIDs))), f.InvalidArgument)
	transitionRequireFault(t, new(TaskEventActor).UnmarshalJSON([]byte(`{"type":"agent_run","user_id":"01900000-0000-7000-8000-000000000001","source":"task_domain"}`)), f.InvalidArgument)
	// Concurrent pure calls use only caller-owned clones and immutable fixtures.
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 8; k++ {
				x := r.Clone()
				if _, err := x.MarshalJSON(); err != nil {
					t.Error(err)
				}
				if _, err := DecodeTaskTransitionLookup(taskRaw(t, lookup)); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
