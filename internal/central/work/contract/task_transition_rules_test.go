package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func transitionStates() []TaskState {
	return []TaskState{TaskStateBacklog, TaskStateTodo, TaskStateInProgress, TaskStateInReview, TaskStateBlocked, TaskStateDone, TaskStateCancelled}
}

func transitionRequireFault(t *testing.T, err error, code f.Code) *f.Fault {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault == nil || fault.Code != code || !fault.Code.Known() ||
		fault.Code.Safe() != code || fault.CommitState != f.NotStarted || fault.Unwrap() != nil ||
		fault.SafeMessage != "" || fault.CauseID != "" || fault.RetryHint != "" {
		t.Fatalf("expected safe %s/not_started, got %v", code, err)
	}
	if code != f.InvalidArgument && len(fault.FieldErrors) != 0 {
		t.Fatal("business refusal exposed extra fields")
	}
	return fault
}

func TestTaskTransitionRulesMatrix(t *testing.T) {
	// This literal oracle follows the fifteen specification rows. It never calls
	// the implementation's role helper or state classification to form results.
	type edge struct {
		from, to TaskState
		roles    string
		reviewer bool
	}
	edges := []edge{
		{TaskStateBacklog, TaskStateTodo, "HA", false},
		{TaskStateBacklog, TaskStateCancelled, "HA", false},
		{TaskStateTodo, TaskStateInProgress, "C", false},
		{TaskStateTodo, TaskStateBlocked, "HAS", false},
		{TaskStateTodo, TaskStateCancelled, "HA", false},
		{TaskStateInProgress, TaskStateTodo, "B", false},
		{TaskStateInProgress, TaskStateInReview, "HA", false},
		{TaskStateInProgress, TaskStateBlocked, "HAS", false},
		{TaskStateInProgress, TaskStateCancelled, "HA", false},
		{TaskStateInReview, TaskStateDone, "HA", true},
		{TaskStateInReview, TaskStateTodo, "HA", true},
		{TaskStateInReview, TaskStateBlocked, "HAS", true},
		{TaskStateInReview, TaskStateCancelled, "HA", false},
		{TaskStateBlocked, TaskStateTodo, "HAQ", false},
		{TaskStateBlocked, TaskStateCancelled, "HA", false},
	}
	roles := []struct {
		role TaskTransitionRole
		key  string
	}{
		{TaskTransitionHumanOwner, "H"}, {TaskTransitionAgentRun, "A"},
		{TaskTransitionSystemBlock, "S"}, {TaskTransitionSchedulerClaim, "C"},
		{TaskTransitionAgentBusyCompensation, "B"}, {TaskTransitionSchedulerReconcile, "Q"},
	}
	agent, other := testID[identity.Agent](t, 70), testID[identity.Agent](t, 71)
	terminal, unlisted, legal := 0, 0, 0
	for _, from := range transitionStates() {
		for _, to := range transitionStates() {
			n := slices.IndexFunc(edges, func(e edge) bool { return e.from == from && e.to == to })
			isTerminal := from == TaskStateDone || from == TaskStateCancelled
			switch {
			case isTerminal:
				terminal++
			case n < 0:
				unlisted++
			default:
				legal++
			}
			for _, role := range roles {
				for variant, assignee := range []*identity.AgentID{&agent, &other, nil} {
					t.Run(fmt.Sprintf("%s/%s/%s/%d", from, to, role.key, variant), func(t *testing.T) {
						in := TaskTransitionRuleInput{FromState: from, ToState: to, Role: role.role, CurrentAssigneeAgentID: assignee}
						if role.key == "A" {
							in.ActorAgentID = agent
						}
						want := f.Code("")
						switch {
						case isTerminal:
							want = f.TaskTerminalImmutable
						case n < 0:
							want = f.InvalidState
						case !strings.Contains(edges[n].roles, role.key):
							want = f.Forbidden
						case role.key == "A" && edges[n].reviewer && variant != 0:
							want = f.Forbidden
						}
						err := CheckTaskTransitionRule(in)
						if want != "" {
							transitionRequireFault(t, err, want)
						} else if err != nil {
							t.Fatalf("allowed edge/role rejected: %v", err)
						}
					})
				}
			}
		}
	}
	if terminal != 14 || unlisted != 20 || legal != 15 {
		t.Fatalf("matrix partition %d/%d/%d", terminal, unlisted, legal)
	}
}

func TestTaskTransitionRulesInputAndPrecedence(t *testing.T) {
	agent := testID[identity.Agent](t, 70)
	base := TaskTransitionRuleInput{FromState: TaskStateDone, ToState: TaskStateBacklog, Role: TaskTransitionAgentRun, ActorAgentID: agent}
	cases := []struct {
		name, path string
		change     func(*TaskTransitionRuleInput)
	}{
		{"zero_from", "/from_state", func(v *TaskTransitionRuleInput) { v.FromState = "" }},
		{"unknown_from", "/from_state", func(v *TaskTransitionRuleInput) { v.FromState = "private-input" }},
		{"case_from", "/from_state", func(v *TaskTransitionRuleInput) { v.FromState = "DONE" }},
		{"zero_to", "/to_state", func(v *TaskTransitionRuleInput) { v.ToState = "" }},
		{"unknown_to", "/to_state", func(v *TaskTransitionRuleInput) { v.ToState = "private-input" }},
		{"case_to", "/to_state", func(v *TaskTransitionRuleInput) { v.ToState = "TODO" }},
		{"role_zero", "/role", func(v *TaskTransitionRuleInput) { v.Role = 0 }},
		{"role_seven", "/role", func(v *TaskTransitionRuleInput) { v.Role = 7 }},
		{"role_max", "/role", func(v *TaskTransitionRuleInput) { v.Role = 255 }},
		{"missing_actor", "/actor_agent_id", func(v *TaskTransitionRuleInput) { v.ActorAgentID = identity.AgentID{} }},
		{"zero_assignee", "/current_assignee_agent_id", func(v *TaskTransitionRuleInput) { v.CurrentAssigneeAgentID = new(identity.AgentID) }},
		{"from_first", "/from_state", func(v *TaskTransitionRuleInput) { v.FromState, v.ToState, v.Role = "private-input", "bad", 0 }},
		{"to_before_role", "/to_state", func(v *TaskTransitionRuleInput) { v.ToState, v.Role = "private-input", 0 }},
		{"role_before_ids", "/role", func(v *TaskTransitionRuleInput) {
			v.Role, v.ActorAgentID, v.CurrentAssigneeAgentID = 0, identity.AgentID{}, new(identity.AgentID)
		}},
		{"actor_before_assignee", "/actor_agent_id", func(v *TaskTransitionRuleInput) {
			v.ActorAgentID, v.CurrentAssigneeAgentID = identity.AgentID{}, new(identity.AgentID)
		}},
	}
	for _, role := range []TaskTransitionRole{TaskTransitionHumanOwner, TaskTransitionSystemBlock, TaskTransitionSchedulerClaim, TaskTransitionAgentBusyCompensation, TaskTransitionSchedulerReconcile} {
		cases = append(cases, struct {
			name, path string
			change     func(*TaskTransitionRuleInput)
		}{fmt.Sprintf("non_agent_%d", role), "/actor_agent_id", func(v *TaskTransitionRuleInput) { v.Role = role }})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.change(&in)
			fault := transitionRequireFault(t, CheckTaskTransitionRule(in), f.InvalidArgument)
			if !reflect.DeepEqual(fault.FieldErrors, []f.FieldError{{Path: tc.path, Code: "INVALID_TASK_TRANSITION_INPUT"}}) {
				t.Fatalf("wrong safe field projection: %+v", fault.FieldErrors)
			}
			if strings.Contains(string(taskRaw(t, fault)), "private-input") {
				t.Fatal("fault exposed input")
			}
		})
	}
	transitionRequireFault(t, CheckTaskTransitionRule(base), f.TaskTerminalImmutable)
	base.FromState, base.ToState = TaskStateBacklog, TaskStateDone
	transitionRequireFault(t, CheckTaskTransitionRule(base), f.InvalidState)
	base.FromState, base.ToState = TaskStateTodo, TaskStateInProgress
	transitionRequireFault(t, CheckTaskTransitionRule(base), f.Forbidden)
}

func TestTaskTransitionRulesInternalRoles(t *testing.T) {
	agent := testID[identity.Agent](t, 70)
	for _, role := range []TaskTransitionRole{TaskTransitionHumanOwner, TaskTransitionAgentRun, TaskTransitionSystemBlock, TaskTransitionSchedulerClaim, TaskTransitionAgentBusyCompensation, TaskTransitionSchedulerReconcile} {
		for _, tc := range []struct {
			from, to TaskState
			allowed  TaskTransitionRole
		}{
			{TaskStateTodo, TaskStateInProgress, TaskTransitionSchedulerClaim},
			{TaskStateInProgress, TaskStateTodo, TaskTransitionAgentBusyCompensation},
		} {
			in := TaskTransitionRuleInput{FromState: tc.from, ToState: tc.to, Role: role}
			if role == TaskTransitionAgentRun {
				in.ActorAgentID = agent
			}
			err := CheckTaskTransitionRule(in)
			if role == tc.allowed {
				if err != nil {
					t.Fatal("dedicated role rejected", err)
				}
			} else {
				transitionRequireFault(t, err, f.Forbidden)
			}
		}
	}
	for _, role := range []TaskTransitionRole{TaskTransitionSystemBlock, TaskTransitionSchedulerClaim, TaskTransitionAgentBusyCompensation, TaskTransitionSchedulerReconcile} {
		for _, to := range []TaskState{TaskStateTodo, TaskStateCancelled} {
			transitionRequireFault(t, CheckTaskTransitionRule(TaskTransitionRuleInput{FromState: TaskStateBacklog, ToState: to, Role: role}), f.Forbidden)
		}
	}
	for _, role := range []TaskTransitionRole{TaskTransitionSystemBlock, TaskTransitionSchedulerClaim, TaskTransitionAgentBusyCompensation} {
		transitionRequireFault(t, CheckTaskTransitionRule(TaskTransitionRuleInput{FromState: TaskStateBlocked, ToState: TaskStateTodo, Role: role}), f.Forbidden)
	}
}

func transitionPositionFixture(t *testing.T) TaskTransitionPosition {
	t.Helper()
	previous, next := testID[Task](t, 30), testID[Task](t, 31)
	v := taskFixture(t)
	return TaskTransitionPosition{SprintID: v.SprintID, State: TaskStateInReview, Priority: TaskPriorityHigh, PreviousID: &previous, NextID: &next, OrderGeneration: 2}
}

func TestTaskTransitionPositionStrictCodec(t *testing.T) {
	base := transitionPositionFixture(t)
	for _, state := range transitionStates() {
		for _, priority := range []TaskPriority{TaskPriorityLow, TaskPriorityMedium, TaskPriorityHigh, TaskPriorityCritical} {
			for _, generation := range []int64{1, math.MaxInt64} {
				v := base
				v.State, v.Priority, v.OrderGeneration = state, priority, generation
				raw := taskRaw(t, v)
				got, err := DecodeTaskTransitionPosition(raw)
				if err != nil || !reflect.DeepEqual(v, got) || len(raw) > MaxTaskHistoryPayloadBytes {
					t.Fatal("position roundtrip or output cap", err)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 6 || string(fields["order_generation"]) != fmt.Sprintf("\"%d\"", generation) {
					t.Fatal("noncanonical wire shape/generation", err)
				}
			}
		}
	}
	raw := taskRaw(t, base)
	reject := func(t *testing.T, bad []byte) {
		t.Helper()
		got := base.Clone()
		before := got.Clone()
		transitionRequireFault(t, got.UnmarshalJSON(bad), f.InvalidArgument)
		if !reflect.DeepEqual(got, before) {
			t.Fatal("failed decode changed receiver")
		}
		decoded, err := DecodeTaskTransitionPosition(bad)
		transitionRequireFault(t, err, f.InvalidArgument)
		if !reflect.DeepEqual(decoded, TaskTransitionPosition{}) {
			t.Fatal("failed Decode returned nonzero result")
		}
	}
	for _, key := range strings.Fields("sprint_id state priority previous_id next_id order_generation") {
		t.Run("required_"+key, func(t *testing.T) { reject(t, taskJSONChange(t, raw, key, nil)) })
		t.Run("null_"+key, func(t *testing.T) {
			wire := taskJSONChange(t, raw, key, json.RawMessage("null"))
			if key == "previous_id" || key == "next_id" {
				if _, err := DecodeTaskTransitionPosition(wire); err != nil {
					t.Fatal("nullable neighbor", err)
				}
			} else {
				reject(t, wire)
			}
		})
		t.Run("case_"+key, func(t *testing.T) { reject(t, taskJSONChange(t, raw, strings.ToUpper(key), json.RawMessage(`"x"`))) })
		t.Run("duplicate_"+key, func(t *testing.T) {
			reject(t, []byte(string(raw[:len(raw)-1])+`,"`+key+`":null}`))
		})
	}
	for n, bad := range []string{"null", "[]", `"x"`, "{}", "", string(raw) + " {}",
		string(raw[:len(raw)-1]) + `,"unknown":1}`,
		string(raw[:len(raw)-1]) + `,"s\u0074ate":"todo"}`,
		string(raw[:len(raw)-1]) + `,"unknown":"\ud800"}`,
		string(raw[:len(raw)-1]) + `,"unknown":"\udc00"}`,
		string(raw[:len(raw)-1]) + ",\"unknown\":\"\xff\"}"} {
		t.Run(fmt.Sprintf("encoding_%d", n), func(t *testing.T) { reject(t, []byte(bad)) })
	}
	for _, bad := range []string{`1`, `0`, `"0"`, `"-1"`, `"01"`, `"+1"`, `"1e0"`, `"1.0"`, `" 1"`, `"1 "`, `"9223372036854775808"`, `true`, `[]`} {
		t.Run("generation_"+bad, func(t *testing.T) { reject(t, taskJSONChange(t, raw, "order_generation", json.RawMessage(bad))) })
	}
	for _, key := range []string{"sprint_id", "previous_id", "next_id"} {
		for _, bad := range []string{`1`, `""`, `"00000000-0000-0000-0000-000000000000"`, `"01900000-0000-4000-8000-00000000001e"`, `"01900000-0000-7000-8000-00000000001E"`} {
			t.Run(key+"_"+bad, func(t *testing.T) { reject(t, taskJSONChange(t, raw, key, json.RawMessage(bad))) })
		}
	}
	for _, kv := range []struct{ key, value string }{{"state", `"IN_REVIEW"`}, {"state", `"unknown"`}, {"state", `""`}, {"priority", `"HIGH"`}, {"priority", `"unknown"`}} {
		reject(t, taskJSONChange(t, raw, kv.key, json.RawMessage(kv.value)))
	}
	for n, change := range []func(*TaskTransitionPosition){
		func(v *TaskTransitionPosition) { v.SprintID = SprintID{} },
		func(v *TaskTransitionPosition) { v.State = "unknown" },
		func(v *TaskTransitionPosition) { v.Priority = "unknown" },
		func(v *TaskTransitionPosition) { v.OrderGeneration = 0 },
		func(v *TaskTransitionPosition) { v.OrderGeneration = -1 },
		func(v *TaskTransitionPosition) { v.PreviousID = new(TaskID) },
		func(v *TaskTransitionPosition) { v.NextID = new(TaskID) },
		func(v *TaskTransitionPosition) { v.NextID = v.PreviousID },
	} {
		t.Run(fmt.Sprintf("invalid_value_%d", n), func(t *testing.T) {
			v := base.Clone()
			change(&v)
			transitionRequireFault(t, v.Validate(), f.InvalidArgument)
			encoded, err := v.MarshalJSON()
			transitionRequireFault(t, err, f.InvalidArgument)
			if encoded != nil {
				t.Fatal("invalid value produced bytes")
			}
		})
	}
	atCap := append(bytes.Repeat([]byte(" "), MaxTaskHistoryPayloadBytes-len(raw)-1), raw...)
	atCap = append(atCap, '\n')
	if _, err := DecodeTaskTransitionPosition(atCap); err != nil {
		t.Fatal("exact raw cap rejected", err)
	}
	var direct TaskTransitionPosition
	if err := direct.UnmarshalJSON(atCap); err != nil {
		t.Fatal("direct exact raw cap rejected", err)
	}
	reject(t, append(bytes.Clone(atCap), ' '))
	var nilReceiver *TaskTransitionPosition
	transitionRequireFault(t, nilReceiver.UnmarshalJSON(raw), f.InvalidArgument)
}

func TestTaskTransitionPositionNeighborsAndClone(t *testing.T) {
	base := transitionPositionFixture(t)
	target := testID[Task](t, 32)
	for _, neighbors := range [][2]*TaskID{{nil, nil}, {base.PreviousID, nil}, {nil, base.NextID}, {base.PreviousID, base.NextID}} {
		v := base
		v.PreviousID, v.NextID = neighbors[0], neighbors[1]
		if err := v.ValidateTarget(target); err != nil {
			t.Fatal("valid logical neighbors rejected", err)
		}
		decoded, err := DecodeTaskTransitionPosition(taskRaw(t, v))
		if err != nil || !reflect.DeepEqual(decoded, v) {
			t.Fatal("neighbor null roundtrip", err)
		}
		copy := v.Clone()
		if copy.PreviousID != nil {
			if copy.PreviousID == v.PreviousID {
				t.Fatal("previous pointer borrowed")
			}
			*copy.PreviousID = target
		}
		if copy.NextID != nil {
			if copy.NextID == v.NextID || copy.NextID == copy.PreviousID {
				t.Fatal("next pointer borrowed or clone pointers aliased")
			}
			*copy.NextID = target
		}
		if v.PreviousID != nil && *v.PreviousID == target || v.NextID != nil && *v.NextID == target {
			t.Fatal("clone mutation changed original")
		}
	}
	for _, id := range []TaskID{{}, *base.PreviousID, *base.NextID} {
		fault := transitionRequireFault(t, base.ValidateTarget(id), f.InvalidArgument)
		if !reflect.DeepEqual(fault.FieldErrors, []f.FieldError{{Path: "/position", Code: "SELF_NEIGHBOR"}}) {
			t.Fatal("wrong target field error")
		}
	}
	duplicate := base.Clone()
	duplicate.NextID = duplicate.PreviousID
	fault := transitionRequireFault(t, duplicate.ValidateTarget(TaskID{}), f.InvalidArgument)
	if !reflect.DeepEqual(fault.FieldErrors, []f.FieldError{{Path: "/position", Code: "INVALID_POSITION"}}) {
		t.Fatal("target check preceded invalid position")
	}
	raw := taskRaw(t, base)
	bad := taskJSONChange(t, raw, "next_id", taskRaw(t, *base.PreviousID))
	_, err := DecodeTaskTransitionPosition(bad)
	transitionRequireFault(t, err, f.InvalidArgument)
	agent := testID[identity.Agent](t, 70)
	in := TaskTransitionRuleInput{FromState: TaskStateInReview, ToState: TaskStateDone, Role: TaskTransitionAgentRun, ActorAgentID: agent, CurrentAssigneeAgentID: &agent}
	for _, value := range []any{base, &base, in, &in} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			if got := fmt.Sprintf(format, value); got != "work_task_transition" {
				t.Fatalf("unsafe direct format: %q", got)
			}
		}
		var output bytes.Buffer
		slog.New(slog.NewJSONHandler(&output, nil)).Info("safe", "value", value)
		var record map[string]any
		if err := json.Unmarshal(output.Bytes(), &record); err != nil || record["value"] != "work_task_transition" {
			t.Fatal("unsafe direct slog", err)
		}
	}
	if !bytes.Contains(raw, []byte(base.SprintID.String())) || !bytes.Contains(raw, []byte(base.PreviousID.String())) {
		t.Fatal("logging projection replaced business JSON")
	}
	for n := range 8 {
		t.Run(fmt.Sprintf("parallel_%d", n), func(t *testing.T) {
			t.Parallel()
			for range 32 {
				if err := CheckTaskTransitionRule(in); err != nil {
					t.Fatal(err)
				}
				clone := base.Clone()
				*clone.PreviousID = target
				got, err := DecodeTaskTransitionPosition(raw)
				if err != nil || !reflect.DeepEqual(got, base) || !bytes.Equal(taskRaw(t, base), raw) {
					t.Fatal("parallel read mutated shared input", err)
				}
			}
		})
	}
}

func TestTaskTransitionLegacyIsolation(t *testing.T) {
	base := transitionPositionFixture(t)
	for _, state := range transitionStates() {
		v := base
		v.State = state
		old := TaskPosition{SprintID: v.SprintID, State: v.State, Priority: v.Priority, PreviousID: v.PreviousID, NextID: v.NextID, OrderGeneration: v.OrderGeneration}
		validateErr := old.Validate()
		_, marshalErr := old.MarshalJSON()
		var decoded TaskPosition
		decodeErr := decoded.UnmarshalJSON(taskRaw(t, v))
		if state == TaskStateBacklog {
			if validateErr != nil || marshalErr != nil || decodeErr != nil || !reflect.DeepEqual(decoded, old) {
				t.Fatal("legacy backlog compatibility lost")
			}
		} else {
			for _, err := range []error{validateErr, marshalErr, decodeErr} {
				transitionRequireFault(t, err, f.InvalidArgument)
			}
		}
	}
	create := taskCreateFixture(t)
	title := "updated title"
	update := TaskFieldsUpdate{Title: &title}
	for _, tc := range []struct {
		name   string
		value  any
		decode func([]byte) error
	}{
		{"create", create, new(TaskCreate).UnmarshalJSON},
		{"update", update, new(TaskFieldsUpdate).UnmarshalJSON},
		{"reorder", TaskReorder{}, new(TaskReorder).UnmarshalJSON},
	} {
		wire := taskRaw(t, tc.value)
		if err := tc.decode(wire); err != nil {
			t.Fatal("legacy request positive", tc.name, err)
		}
		for _, kv := range []struct{ key, value string }{{"target_state", `"todo"`}, {"state", `"todo"`}, {"comment", `"review"`}, {"add_blockers", `[]`}, {"resolve_blocker_ids", `[]`}, {"role", `4`}} {
			transitionRequireFault(t, tc.decode(taskJSONChange(t, wire, kv.key, json.RawMessage(kv.value))), f.InvalidArgument)
		}
	}
	transfer := TaskCommandName("work.task.transfer")
	transitionRequireFault(t, transfer.Validate(), f.InvalidArgument)
	_, err := transfer.MarshalJSON()
	transitionRequireFault(t, err, f.InvalidArgument)
	transitionRequireFault(t, new(TaskCommandName).UnmarshalJSON([]byte(`"work.task.transfer"`)), f.InvalidArgument)
	mutation := taskRaw(t, taskMutationFixture(t))
	transitionRequireFault(t, new(TaskMutation).UnmarshalJSON(taskJSONChange(t, mutation, "task_event_ids", json.RawMessage(`[]`))), f.InvalidArgument)
	transitionRequireFault(t, new(TaskMutation).UnmarshalJSON(taskJSONChange(t, mutation, "task_event_id", json.RawMessage(`[]`))), f.InvalidArgument)
	history := taskRaw(t, taskHistoryFixture(t))
	for _, kind := range []string{"state_changed", "assignee_changed", "blocker_added", "blocker_resolved", "comment"} {
		transitionRequireFault(t, new(TaskEvent).UnmarshalJSON(taskJSONChange(t, history, "type", taskRaw(t, kind))), f.InvalidArgument)
	}
	_, _, changed := taskEventFixture(t)
	changedRaw := taskRaw(t, changed)
	for _, key := range []string{"source_position", "target_position"} {
		transitionRequireFault(t, new(TaskChanged).UnmarshalJSON(taskJSONChange(t, changedRaw, key, taskRaw(t, base))), f.InvalidArgument)
	}
	transitionRequireFault(t, new(TaskChanged).UnmarshalJSON(taskJSONChange(t, changedRaw, "position", taskRaw(t, base))), f.InvalidArgument)
	transitionRequireFault(t, new(TaskChanged).UnmarshalJSON(taskJSONChange(t, changedRaw, "change", json.RawMessage(`"transitioned"`))), f.InvalidArgument)
	transitionRequireFault(t, new(TaskChanged).UnmarshalJSON(taskJSONChange(t, changedRaw, "task_event_ids", json.RawMessage(`[]`))), f.InvalidArgument)
}
