package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	w "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"math"
	"reflect"
	"strings"
	"testing"
)

func probeTransitionID[K any](t *testing.T, suffix string) f.ID[K] {
	t.Helper()
	v, err := f.ParseID[K]("01928adc-3567-7abc-83fe-0000000000" + suffix)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func probeTransitionFault(t *testing.T, err error, code f.Code, path string) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code || !fault.Code.Known() || fault.Code.Safe() != code || fault.CommitState != f.NotStarted || fault.Unwrap() != nil || fault.SafeMessage != "" || fault.CauseID != "" || fault.RetryHint != "" {
		t.Fatalf("unexpected failure projection: %v; wanted %s", err, code)
	}
	if path != "" {
		if !reflect.DeepEqual(fault.FieldErrors, []f.FieldError{{Path: path, Code: "INVALID_TASK_TRANSITION_INPUT"}}) {
			t.Fatal("wrong shape error priority")
		}
	} else if code != f.InvalidArgument && len(fault.FieldErrors) != 0 {
		t.Fatal("business error leaked fields")
	}
}

func TestIndependentTaskTransitionRoles(t *testing.T) {
	// Role-oriented oracle transcribed from the accepted business table.
	ordinary := strings.Fields("backlog>todo backlog>cancelled todo>blocked todo>cancelled in_progress>in_review in_progress>blocked in_progress>cancelled in_review>done in_review>todo in_review>blocked in_review>cancelled blocked>todo blocked>cancelled")
	allowed := map[w.TaskTransitionRole][]string{
		w.TaskTransitionHumanOwner: ordinary, w.TaskTransitionAgentRun: ordinary,
		w.TaskTransitionSystemBlock:           strings.Fields("todo>blocked in_progress>blocked in_review>blocked"),
		w.TaskTransitionSchedulerClaim:        {"todo>in_progress"},
		w.TaskTransitionAgentBusyCompensation: {"in_progress>todo"},
		w.TaskTransitionSchedulerReconcile:    {"blocked>todo"},
	}
	allEdges := map[string]bool{}
	for _, edges := range allowed {
		for _, edge := range edges {
			allEdges[edge] = true
		}
	}
	if len(allEdges) != 15 {
		t.Fatal("independent oracle is incomplete")
	}
	agent, other := probeTransitionID[identity.Agent](t, "21"), probeTransitionID[identity.Agent](t, "22")
	states := strings.Fields("backlog todo in_progress in_review blocked done cancelled")
	for variant, current := range []*identity.AgentID{&agent, &other, nil} {
		t.Run(fmt.Sprintf("current-%d", variant), func(t *testing.T) {
			for role, edges := range allowed {
				for _, from := range states {
					for _, to := range states {
						input := w.TaskTransitionRuleInput{FromState: w.TaskState(from), ToState: w.TaskState(to), Role: role, CurrentAssigneeAgentID: current}
						if role == w.TaskTransitionAgentRun {
							input.ActorAgentID = agent
						}
						edge := from + ">" + to
						permitted := false
						for _, candidate := range edges {
							if edge == candidate {
								permitted = true
							}
						}
						if role == w.TaskTransitionAgentRun && variant != 0 && from == "in_review" && (to == "done" || to == "todo" || to == "blocked") {
							permitted = false
						}
						err := w.CheckTaskTransitionRule(input)
						switch {
						case from == "done" || from == "cancelled":
							probeTransitionFault(t, err, f.TaskTerminalImmutable, "")
						case !allEdges[edge]:
							probeTransitionFault(t, err, f.InvalidState, "")
						case !permitted:
							probeTransitionFault(t, err, f.Forbidden, "")
						case err != nil:
							t.Fatalf("role %d edge %s unexpectedly rejected", role, edge)
						}
					}
				}
			}
		})
	}
}

func TestIndependentTaskTransitionPrecedence(t *testing.T) {
	actor := probeTransitionID[identity.Agent](t, "21")
	in := w.TaskTransitionRuleInput{FromState: "private-state-sentinel", ToState: "BAD", Role: 255, CurrentAssigneeAgentID: new(identity.AgentID)}
	for _, stage := range []struct {
		path   string
		repair func()
	}{
		{"/from_state", func() { in.FromState = w.TaskStateDone }},
		{"/to_state", func() { in.ToState = w.TaskStateTodo }},
		{"/role", func() { in.Role = w.TaskTransitionAgentRun }},
		{"/actor_agent_id", func() { in.ActorAgentID = actor }},
		{"/current_assignee_agent_id", func() { in.CurrentAssigneeAgentID = nil }},
	} {
		err := w.CheckTaskTransitionRule(in)
		probeTransitionFault(t, err, f.InvalidArgument, stage.path)
		raw, _ := json.Marshal(err)
		if bytes.Contains(raw, []byte("private-state-sentinel")) {
			t.Fatal("input leaked into fault")
		}
		stage.repair()
	}
	probeTransitionFault(t, w.CheckTaskTransitionRule(in), f.TaskTerminalImmutable, "")
	in.FromState = w.TaskStateBacklog
	in.ToState = w.TaskStateDone
	probeTransitionFault(t, w.CheckTaskTransitionRule(in), f.InvalidState, "")
	in.FromState = w.TaskStateTodo
	in.ToState = w.TaskStateInProgress
	probeTransitionFault(t, w.CheckTaskTransitionRule(in), f.Forbidden, "")
	for _, role := range []w.TaskTransitionRole{w.TaskTransitionHumanOwner, w.TaskTransitionSystemBlock, w.TaskTransitionSchedulerClaim, w.TaskTransitionAgentBusyCompensation, w.TaskTransitionSchedulerReconcile} {
		in.Role = role
		probeTransitionFault(t, w.CheckTaskTransitionRule(in), f.InvalidArgument, "/actor_agent_id")
	}
}

const probePositionRaw = `{"sprint_id":"01928adc-3567-7abc-83fe-000000000031","state":"in_review","priority":"high","previous_id":"01928adc-3567-7abc-83fe-000000000032","next_id":"01928adc-3567-7abc-83fe-000000000033","order_generation":"9223372036854775807"}`

func TestIndependentTaskTransitionPosition(t *testing.T) {
	base, err := w.DecodeTaskTransitionPosition([]byte(probePositionRaw))
	if err != nil {
		t.Fatal(err)
	}
	if base.OrderGeneration != math.MaxInt64 {
		t.Fatal("generation truncated")
	}
	for _, state := range strings.Fields("backlog todo in_progress in_review blocked done cancelled") {
		v := base.Clone()
		v.State = w.TaskState(state)
		for _, priority := range strings.Fields("low medium high critical") {
			v.Priority = w.TaskPriority(priority)
			raw, err := v.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			got, err := w.DecodeTaskTransitionPosition(raw)
			if err != nil || !reflect.DeepEqual(got, v) {
				t.Fatal("public codec roundtrip")
			}
			old := w.TaskPosition{SprintID: v.SprintID, State: v.State, Priority: v.Priority, PreviousID: v.PreviousID, NextID: v.NextID, OrderGeneration: v.OrderGeneration}
			_, marshalErr := old.MarshalJSON()
			var decoded w.TaskPosition
			errs := []error{old.Validate(), marshalErr, decoded.UnmarshalJSON(raw)}
			for _, err := range errs {
				if state == "backlog" {
					if err != nil {
						t.Fatal("legacy positive broken")
					}
				} else {
					probeTransitionFault(t, err, f.InvalidArgument, "")
				}
			}
		}
	}
	// Complete raw includes exterior whitespace; standard json.Unmarshal is not
	// used as the cap oracle because it strips that whitespace before dispatch.
	atCap := append([]byte(probePositionRaw), bytes.Repeat([]byte(" "), 8192-len(probePositionRaw))...)
	if _, err := w.DecodeTaskTransitionPosition(atCap); err != nil {
		t.Fatal("8192-byte valid input rejected")
	}
	var direct w.TaskTransitionPosition
	if err := direct.UnmarshalJSON(atCap); err != nil {
		t.Fatal(err)
	}
	bad := [][]byte{append(bytes.Clone(atCap), ' '), []byte("null"), []byte(probePositionRaw + " {}"), []byte(strings.Replace(probePositionRaw, `"state":"in_review"`, `"state":"in_review","state":"todo"`, 1)), []byte(strings.Replace(probePositionRaw, `"state":"in_review"`, `"STATE":"in_review"`, 1)), []byte(strings.Replace(probePositionRaw, `"state":"in_review"`, `"state":"\ud800"`, 1))}
	for _, value := range []string{`1`, `"0"`, `"01"`, `"+1"`, `"1e0"`, `"9223372036854775808"`} {
		bad = append(bad, []byte(strings.Replace(probePositionRaw, `"9223372036854775807"`, value, 1)))
	}
	for _, key := range strings.Fields("sprint_id state priority previous_id next_id order_generation") {
		var fields map[string]json.RawMessage
		json.Unmarshal([]byte(probePositionRaw), &fields)
		delete(fields, key)
		raw, _ := json.Marshal(fields)
		bad = append(bad, raw)
		fields[key] = json.RawMessage("null")
		raw, _ = json.Marshal(fields)
		if key == "previous_id" || key == "next_id" {
			if _, err := w.DecodeTaskTransitionPosition(raw); err != nil {
				t.Fatal("nullable neighbor refused")
			}
		} else {
			bad = append(bad, raw)
		}
	}
	for _, raw := range bad {
		receiver := base.Clone()
		before := receiver.Clone()
		probeTransitionFault(t, receiver.UnmarshalJSON(raw), f.InvalidArgument, "")
		if !reflect.DeepEqual(receiver, before) {
			t.Fatal("failed decoder changed receiver")
		}
		got, err := w.DecodeTaskTransitionPosition(raw)
		probeTransitionFault(t, err, f.InvalidArgument, "")
		if !reflect.DeepEqual(got, w.TaskTransitionPosition{}) {
			t.Fatal("failed public Decode returned partial value")
		}
	}
	var absent *w.TaskTransitionPosition
	probeTransitionFault(t, absent.UnmarshalJSON([]byte(probePositionRaw)), f.InvalidArgument, "")
}

func TestIndependentTaskTransitionValueSafety(t *testing.T) {
	base, err := w.DecodeTaskTransitionPosition([]byte(probePositionRaw))
	if err != nil {
		t.Fatal(err)
	}
	target := probeTransitionID[w.Task](t, "34")
	for _, id := range []w.TaskID{{}, *base.PreviousID, *base.NextID} {
		probeTransitionFault(t, base.ValidateTarget(id), f.InvalidArgument, "")
	}
	if err := base.ValidateTarget(target); err != nil {
		t.Fatal(err)
	}
	duplicate := base.Clone()
	duplicate.NextID = duplicate.PreviousID
	probeTransitionFault(t, duplicate.ValidateTarget(target), f.InvalidArgument, "")
	in := w.TaskTransitionRuleInput{FromState: "sensitive-stale-value"}
	if fmt.Sprintf("%#v", in) != "work_task_transition" || in.LogValue().String() != "work_task_transition" || fmt.Sprintf("%+v", base) != "work_task_transition" || base.LogValue().String() != "work_task_transition" {
		t.Fatal("direct diagnostic projection widened")
	}
	for n := range 6 {
		t.Run(fmt.Sprintf("reader-%d", n), func(t *testing.T) {
			t.Parallel()
			for range 32 {
				copy := base.Clone()
				if copy.PreviousID == base.PreviousID || copy.NextID == base.NextID || copy.PreviousID == copy.NextID {
					t.Fatal("neighbor pointer alias")
				}
				*copy.PreviousID = target
				*copy.NextID = target
				if base.PreviousID.String() == target.String() || base.NextID.String() == target.String() {
					t.Fatal("clone mutated original")
				}
				raw, err := base.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				got, err := w.DecodeTaskTransitionPosition(raw)
				if err != nil || !reflect.DeepEqual(base, got) {
					t.Fatal("parallel value codec changed input")
				}
			}
		})
	}
}
