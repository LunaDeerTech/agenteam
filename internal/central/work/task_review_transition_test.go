package work

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These are controlled canonical facts, not a Human grant or a real claim.
// Physical transitions, caller rollback and replay use the separate PG case.
func reviewTransitionRecord(t *testing.T, from, to c.TaskState, explicit, different bool) (*transitionRecord, i.Actor) {
	t.Helper()
	old, actor, _ := pureTransitionRecord(t)
	before := old.Plan.After.Task.Clone()
	before.State = from
	agent := *before.AssigneeAgentID
	if different {
		agent = pureID[i.Agent](t, 310)
	}
	comment := "  Review result\nKeep these exact bytes.\t"
	request := c.TaskTransfer{TargetState: to, Comment: &comment, AddBlockers: []c.TaskBlockerCreate{}, ResolveBlockerIDs: []c.TaskBlockerID{}}
	if explicit {
		request.AssigneeAgentID = &agent
	}
	in := transitionInput{Project: before.ProjectID, Task: before.ID, User: old.Input.User, Expected: before.Version, Request: request}
	r := &transitionRecord{ID: pureID[c.TaskTransitionCommand](t, 311), Key: "review-transition", Input: in, Revision: 1, State: "planned", Created: before.UpdatedAt}
	var err error
	r.Semantic, err = in.semantic(actor, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	after := before.Clone()
	after.State = to
	after.Version++
	after.AssigneeAgentID = &agent
	ranks, err := rankFor([]rankItem{}, after.ID.String(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	after.ManualRank = ranks.Rank
	source, target := groupForTask(before), groupForTask(after)
	sp, err := transitionPosition(source, 3, "", "")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := transitionPosition(target, 2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	history := []c.TaskTransitionEvent{{Type: c.TaskTransitionStateChanged, Payload: c.TaskTransitionFactPayload{StateChanged: &c.TaskStateChangedPayload{FromState: from, ToState: to}}}}
	if agent != *before.AssigneeAgentID {
		history = append(history, c.TaskTransitionEvent{Type: c.TaskTransitionAssigneeChanged, Payload: c.TaskTransitionFactPayload{AssigneeChanged: &c.TaskAssigneeChangedPayload{FromAgentID: before.AssigneeAgentID, ToAgentID: agent}}})
	}
	history = append(history, c.TaskTransitionEvent{Type: c.TaskTransitionComment, Payload: c.TaskTransitionFactPayload{Comment: &c.TaskCommentPayload{Body: comment}}})
	for n := range history {
		history[n].ID = pureID[c.TaskEvent](t, 320+n)
		history[n].ProjectID, history[n].TaskID = in.Project, in.Task
		history[n].TaskVersion = after.Version
		history[n].Actor = c.TaskTransitionActor{UserID: in.User}
		history[n].OperationID, history[n].CorrelationID = r.ID, r.ID
		history[n].CreatedAt = after.UpdatedAt
	}
	header, err := taskHeader(pureID[event.EventIdentity](t, 330), after, after.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	header.EventType, header.SchemaVersion = c.TaskTransitionedName, c.TaskTransitionSchemaVersion
	factory, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	mutation, ev, err := factory.NewTaskTransitionData(header, before, after, request, history, sp, tp)
	if err != nil {
		t.Fatal(err)
	}
	var current *ac.AgentRef
	if explicit || to != c.TaskStateDone {
		current = &ac.AgentRef{ProjectID: in.Project, AgentID: agent, ConfigVersion: 1}
	}
	r.Plan = transitionPlan{Before: before, After: mutation, Placement: old.Plan.Placement, Agent: current,
		Groups:          []taskGroupPlan{{source, []rankItem{{before.ID.String(), before.ManualRank}}, []rankItem{}, 2}, {target, []rankItem{}, ranks.Items, 1}},
		QueryGeneration: 2, History: history, Source: sp, Target: tp, Header: header, Payload: ev.PayloadBytes()}
	if err = validateTransitionRecord(r, actor); err != nil {
		t.Fatal("controlled review record", err)
	}
	return r, actor
}

func TestTaskHumanReviewPresenceAndActiveCapability(t *testing.T) {
	for _, edge := range [][2]c.TaskState{{c.TaskStateInProgress, c.TaskStateInReview}, {c.TaskStateInReview, c.TaskStateDone}, {c.TaskStateInReview, c.TaskStateTodo}} {
		r, _ := reviewTransitionRecord(t, edge[0], edge[1], true, false)
		before, request := r.Plan.Before, r.Input.Request.Clone()
		if err := transitionEdge(before, request); err != nil {
			t.Fatal(err)
		}
		request.Comment = nil
		pureCode(t, transitionEdge(before, request), f.CommentRequired)
		request = r.Input.Request.Clone()
		request.AssigneeAgentID = nil
		if edge[1] == c.TaskStateDone {
			if transitionEdge(before, request) != nil || !transitionRetainsReviewer(before, request) || *transitionAgent(before, request) != *before.AssigneeAgentID {
				t.Fatal("omitted reviewer not retained")
			}
		} else {
			pureCode(t, transitionEdge(before, request), f.TaskAssigneeRequired)
		}
		request = r.Input.Request.Clone()
		blank := " \t\n"
		request.Comment = &blank
		pureCode(t, request.Validate(), f.InvalidArgument)
		request = r.Input.Request.Clone()
		empty := ec.ExecutionOccupancy{Active: []ec.ActiveTaskExecution{}, HistoryTaskIDs: []string{before.ID.String()}}
		if checkTransitionOccupancy(before, request, empty) != nil {
			t.Fatal("real empty active set with history rejected")
		}
		pureCode(t, checkTransitionOccupancy(before, request, ec.ExecutionOccupancy{}), f.InternalError)
		for _, status := range []ec.Status{ec.Created, ec.Preparing, ec.Running, ec.Waiting} {
			occupied := empty
			occupied.Active = []ec.ActiveTaskExecution{{TaskID: before.ID.String(), AgentID: *before.AssigneeAgentID, ExecutionID: pureID[i.Execution](t, 331), Status: status}}
			pureCode(t, checkTransitionOccupancy(before, request, occupied), f.DependencyUnbound)
			old := before.Clone()
			old.State = c.TaskStateBlocked
			oldRequest := request.Clone()
			oldRequest.TargetState = c.TaskStateTodo
			pureCode(t, checkTransitionOccupancy(old, oldRequest, occupied), f.ResourceBusy)
		}
	}
}

func TestTaskHumanReviewCanonicalHistoryAndRetainedReviewer(t *testing.T) {
	for _, tc := range []struct {
		from, to            c.TaskState
		explicit, different bool
	}{{c.TaskStateInProgress, c.TaskStateInReview, true, true}, {c.TaskStateInProgress, c.TaskStateInReview, true, false}, {c.TaskStateInReview, c.TaskStateTodo, true, true}, {c.TaskStateInReview, c.TaskStateDone, false, false}, {c.TaskStateInReview, c.TaskStateDone, true, true}, {c.TaskStateInReview, c.TaskStateDone, true, false}} {
		r, actor := reviewTransitionRecord(t, tc.from, tc.to, tc.explicit, tc.different)
		wantHistory := 2
		if tc.different {
			wantHistory++
		}
		if len(r.Plan.History) != wantHistory || r.Plan.History[0].Type != c.TaskTransitionStateChanged || r.Plan.History[wantHistory-1].Type != c.TaskTransitionComment || r.Plan.History[wantHistory-1].Payload.Comment.Body != *r.Input.Request.Comment {
			t.Fatal("review facts/order/comment changed")
		}
		raw, err := json.Marshal(r.Plan)
		if err != nil {
			t.Fatal(err)
		}
		var decoded transitionPlan
		if err = json.Unmarshal(raw, &decoded); err != nil || !sameValue(decoded, r.Plan) {
			t.Fatal("review plan roundtrip", err)
		}
		retained := tc.to == c.TaskStateDone && !tc.explicit
		if bytes.Contains(raw, []byte(`"agent":null`)) != retained {
			t.Fatal("current Agent fact fabricated or lost")
		}
		r.Plan = decoded
		receipt, err := json.Marshal(r.Plan.After)
		if err != nil {
			t.Fatal(err)
		}
		r.State, r.Receipt = "completed", &r.Plan.After
		if err = validateTransitionRecord(r, actor); err != nil {
			t.Fatal("historical review receipt", err)
		}
		replay, err := json.Marshal(r.Receipt)
		if err != nil || !bytes.Equal(receipt, replay) {
			t.Fatal("historical receipt was recomputed", err)
		}
		if retained {
			agent := *r.Plan.Before.AssigneeAgentID
			r.Input.Request.AssigneeAgentID = &agent
			r.Semantic, err = r.Input.semantic(actor, r.Key)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			r.Plan.Agent = nil
		}
		if validateTransitionRecord(r, actor) == nil {
			t.Fatal("explicit/current assignment borrowed omitted-reviewer proof")
		}
	}
}

func TestTaskHumanReviewBlockersUseTargetInvariant(t *testing.T) {
	in, blocker, _ := unblockInput(t)
	for _, target := range []c.TaskState{c.TaskStateInReview, c.TaskStateDone, c.TaskStateTodo} {
		in.Request.TargetState, in.Request.ResolveBlockerIDs = target, nil
		x := &unblockSQL{t: t, count: 1}
		got, _, err := prepareTransitionResolutions(context.Background(), x, in, blocker.CreatedAt)
		if target == c.TaskStateTodo {
			pureCode(t, err, f.InvalidState)
		} else if err != nil {
			t.Fatal("review/approval implicitly resolved or required no blockers", err)
		}
		if len(got) != 0 || x.writes != 0 || x.queries != 1 {
			t.Fatal("preserved blockers caused a mutation")
		}
	}
	// Resolving an explicit subset on approval preserves other unresolved facts.
	in.Request.TargetState, in.Request.ResolveBlockerIDs = c.TaskStateDone, []c.TaskBlockerID{blocker.ID}
	x := &unblockSQL{t: t, count: 2, rows: map[string]taskTriggerRow{blocker.ID.String(): unblockRow(t, blocker, blocker.Technical.ReferenceID, nil, nil)}}
	resolved, _, err := prepareTransitionResolutions(context.Background(), x, in, blocker.CreatedAt)
	if err != nil || len(resolved) != 1 || !sameValue(resolved[0].Before, blocker) || resolved[0].After.ResolutionComment != nil || x.writes != 0 {
		t.Fatal("explicit approval resolution", err)
	}
	r := &transitionRecord{ID: pureID[c.TaskTransitionCommand](t, 332), Input: in, Plan: transitionPlan{Resolutions: resolved}}
	op := r.ID.String()
	x = &unblockSQL{t: t, count: 1, linked: 1, rows: map[string]taskTriggerRow{blocker.ID.String(): unblockRow(t, resolved[0].After, blocker.Technical.ReferenceID, nil, &op)}}
	if err = verifyTransitionResolutions(context.Background(), x, r); err != nil {
		t.Fatal("approval postimage rejected preserved blocker", err)
	}
	r.Input.Request.TargetState = c.TaskStateTodo
	pureCode(t, verifyTransitionResolutions(context.Background(), x, r), f.Forbidden)
	ctx, cancel := context.WithCancel(context.Background())
	x = &unblockSQL{t: t, count: 1, cancel: cancel}
	in.Request.ResolveBlockerIDs = nil
	got, _, err := prepareTransitionResolutions(ctx, x, in, blocker.CreatedAt)
	if !errors.Is(err, context.Canceled) || got != nil || x.writes != 0 {
		t.Fatal("canceled approval produced a resolution plan", err)
	}
}
