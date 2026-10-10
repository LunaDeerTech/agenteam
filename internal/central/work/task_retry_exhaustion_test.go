package work

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These controls reuse the actual Work record builder/codec and event factory.
// They do not establish Scheduler policy, a temporary Launch observation or a
// physical commit; those belong to the private authority and real retry case.
func TestTaskRetryExhaustionReusesCurrentFailureFacts(t *testing.T) {
	old, actor := failureControlRecord(t)
	facts := old.Facts.Clone()
	facts.Reason = c.TaskLaunchFailureRetryExhausted
	ids := []c.TaskEventID{old.History[0].ID, old.History[1].ID}
	r, err := buildTaskFailureRecord(old.Request, facts, old.Claim, old.Before, old.Sprint, old.CurrentSprintID, old.Groups, old.QueryGeneration, old.Blocker.ID, ids, old.Header.EventID, old.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Changed || r.After.Title != old.Before.Title || r.After.Plan != old.Before.Plan || r.After.State != c.TaskStateBlocked || r.After.Version != old.Before.Version+1 || r.Blocker.Technical.ReferenceID != old.Request.Claim.DispatchID || r.Event.Reason != c.TaskLaunchFailureRetryExhausted {
		t.Fatal("exhaustion changed current-preimage or technical provenance semantics")
	}
	oldRaw, err := canonical(old)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical(r)
	if err != nil || !bytes.Equal(oldRaw, bytes.ReplaceAll(raw, []byte(c.TaskLaunchFailureRetryExhausted), []byte(c.TaskLaunchFailureUnsupportedResourceConstraints))) {
		t.Fatal("new reason changed the original persistent shape", err)
	}
	var roundtrip taskFailureRecord
	if err = json.Unmarshal(raw, &roundtrip); err != nil || !sameValue(roundtrip, r) {
		t.Fatal("exhausted result not recoverable", err)
	}
	events, err := c.RegisterTaskLaunchFailureEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = events.NewTaskLaunchFailed(*r.Header, *r.Event); err != nil {
		t.Fatal("existing typed event factory rejected exhaustion", err)
	}
	payload, err := json.Marshal(r.Event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = events.Restore(*r.Header, payload); err != nil {
		t.Fatal("schema4 could not restore new closed reason", err)
	}
	bad := r.Event.Clone()
	bad.Reason = "launch_lock_timeout_v1"
	if _, err = events.NewTaskLaunchFailed(*r.Header, bad); err == nil {
		t.Fatal("temporary observation alone became a final event")
	}
	for _, h := range r.History {
		data, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = decodeTaskTriggerEvent(data); err != nil {
			t.Fatal("unchanged typed history became unreadable", err)
		}
		if bytes.Contains(data, []byte(c.TaskLaunchFailureRetryExhausted)) {
			t.Fatal("Outbox reason leaked into the separate history codec")
		}
	}
	blocked := old.Before.Clone()
	blocked.State = c.TaskStateBlocked
	blocked.Version++
	already, err := buildTaskFailureRecord(old.Request, facts, old.Claim, blocked, old.Sprint, old.CurrentSprintID, []taskGroupPlan{}, old.QueryGeneration, old.Blocker.ID, ids[:1], old.Header.EventID, old.CreatedAt)
	if err != nil || !already.Changed || len(already.History) != 1 || already.History[0].Type != "blocker_added" || !sameValue(already.Groups, []taskGroupPlan{}) {
		t.Fatal("already blocked exhaustion invented another transition", err)
	}
	other := old.Before.Clone()
	other.Version++
	otherAgent := pureID[i.Agent](t, 126)
	other.AssigneeAgentID = &otherAgent
	preserved, err := buildTaskFailureRecord(old.Request, facts, old.Claim, other, old.Sprint, old.CurrentSprintID, []taskGroupPlan{}, 0, c.TaskBlockerID{}, []c.TaskEventID{}, event.EventID{}, old.CreatedAt)
	if err != nil || preserved.Changed || !sameValue(preserved.Before, preserved.After) || preserved.Blocker != nil || len(preserved.History) != 0 || preserved.Event != nil {
		t.Fatal("exhaustion overwrote a later assignment", err)
	}
	// A reason, count or same-shaped projection cannot replace Work's real
	// writer evidence or Scheduler's private original-transaction authority.
	store, deps := failureControlPorts(t)
	service, err := NewTaskLaunchFailure(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApplyTaskLaunchFailureInTx(context.Background(), f.NewTx(), actor, r.Request, forgedFailurePlan{}); err == nil {
		t.Fatal("public exhaustion input bypassed the original plan")
	}
	if store.touches.Load() != 0 {
		t.Fatal("forged exhausted plan reached storage")
	}
}
