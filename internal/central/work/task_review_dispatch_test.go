package work

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These controls use the original protected provider paths and controlled
// ports. They do not issue real Scheduler grants or claim physical SQL commit.
func reviewRelaunchControl(t *testing.T) *relaunchTestFixture {
	t.Helper()
	v := newRelaunchControl(t)
	reviewer := pureID[i.Agent](t, 150)
	v.request.Purpose = "task/review"
	v.request.AgentID = reviewer
	v.store.task.State = c.TaskStateInReview
	v.store.task.AssigneeAgentID = &reviewer
	v.intents.request = v.request
	v.agents.value.AgentID = reviewer
	return v
}

func TestTaskReviewDispatchUsesCurrentReviewerOrigin(t *testing.T) {
	v := reviewRelaunchControl(t)
	before := v.store.task.Clone()
	plan := v.discover(t)
	if v.store.writes != 0 {
		t.Fatal("review discovery mutated Work")
	}
	v.store.tx = f.NewTx()
	v.store.required = plan.RequiredLocks()
	applied, err := v.service.RecordTaskRelaunchInTx(v.ctx, v.store.tx, v.actor, v.request, plan)
	if err != nil || applied == nil || applied.Source().Request != v.request || v.store.origin == nil || v.store.origin.Task.State != c.TaskStateInReview || v.store.writes != 1 || !sameValue(before, v.store.task) {
		t.Fatal("review did not record only its actual immutable origin", err)
	}
	if err = v.service.CheckTaskRelaunchAppliedInTx(v.ctx, v.store.tx, v.actor, v.request, plan, applied); err != nil {
		t.Fatal(err)
	}
	source := applied.Source()
	request := ec.LaunchRequest{ProjectID: v.request.ProjectID, AgentID: v.request.AgentID,
		Trigger: ec.Trigger{Kind: "task", TaskID: v.request.TaskID.String()}, Purpose: "task/review",
		Policy:  ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}},
		Lineage: ec.Lineage{DispatchID: v.request.DispatchID},
		Meta:    f.CommandMeta{RequestID: v.request.RequestID, IdempotencyKey: f.IdempotencyKey("scheduler_dispatch:" + v.request.DispatchID)}}
	proof := &taskLaunchTestAuthority{request: request, intent: c.TaskLaunchIntent{ProjectID: v.request.ProjectID,
		TaskID: v.request.TaskID, AgentID: v.request.AgentID, SprintID: v.request.CurrentSprintID,
		DispatchID: v.request.DispatchID, Origin: c.TaskDispatchRelaunch, Relaunch: &source}}
	provider, err := NewTaskLaunchProvider(v.store, v.service.deps.Authority, proof)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(v.ctx, taskLaunchTestProof{}, proof)
	launchPlan, err := provider.DiscoverLaunch(ctx, v.actor, request)
	if err != nil || launchPlan == nil {
		t.Fatal("actual review origin did not supply Launch source", err)
	}
	permit, err := provider.ValidateLaunchInTx(ctx, v.store.tx, v.actor, request, launchPlan)
	if err != nil || !permit.Matches(request) || v.store.writes != 1 || !sameValue(before, v.store.task) || v.store.origin.Request.Purpose != "task/review" {
		t.Fatal("review Launch changed Task or borrowed todo claim", err)
	}
	// A current title may change before discovery, but never after this plan's
	// exact current source was frozen.
	v.store.task.Title = "Later review requirements"
	v.store.task.Version++
	permit, err = provider.ValidateLaunchInTx(ctx, v.store.tx, v.actor, request, launchPlan)
	pureCode(t, err, f.ConfirmationStale)
	if permit.Matches(request) {
		t.Fatal("changed review source retained permit")
	}
	v.store.task = before.Clone()
	v.store.hideOrigin = true
	if _, err = provider.ValidateLaunchInTx(ctx, v.store.tx, v.actor, request, launchPlan); err == nil {
		t.Fatal("review accepted a projection without its Work parent")
	}
}

func TestTaskReviewDispatchRejectsCrossPhaseAndBindsFailureMarker(t *testing.T) {
	for _, purpose := range []string{"task/work", "task/review"} {
		v := newRelaunchControl(t)
		v.request.Purpose = purpose
		v.intents.request = v.request
		if purpose == "task/work" {
			v.store.task.State = c.TaskStateInReview
		}
		plan, err := v.service.DiscoverTaskRelaunch(v.ctx, v.actor, v.request)
		pureCode(t, err, f.InvalidState)
		if plan != nil || v.store.writes != 0 {
			t.Fatal("purpose accepted the other current phase")
		}
	}
	v := reviewRelaunchControl(t)
	plan := v.discover(t)
	// The reviewer still passes through the real Agent port at final time.
	v.agents.err = fault(f.InvalidState)
	v.store.tx = f.NewTx()
	v.store.required = plan.RequiredLocks()
	if applied, err := v.service.RecordTaskRelaunchInTx(v.ctx, v.store.tx, v.actor, v.request, plan); err == nil || applied != nil || v.store.writes != 0 {
		t.Fatal("invalid current reviewer recorded an origin")
	}

	provider, store, _, proof, ctx, actor, request := newTaskLaunchControl(t)
	request.Purpose = "task/review"
	proof.request = request.Clone()
	if plan, err := provider.DiscoverLaunch(ctx, actor, request); err == nil || plan != nil {
		t.Fatal("historical todo claim authorized review")
	}
	queries := store.queries
	request.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{}`)}
	_, err := provider.DiscoverLaunch(ctx, actor, request)
	pureCode(t, err, f.DependencyUnbound)
	reason, ok := c.MatchTaskLaunchFailure(err, request)
	if !ok || reason != c.TaskLaunchFailureUnsupportedResourceConstraints || store.queries != queries {
		t.Fatal("exact review policy rejection lost private request identity")
	}
	work := request.Clone()
	work.Purpose = "task/work"
	if _, ok := c.MatchTaskLaunchFailure(err, work); ok {
		t.Fatal("review rejection matched another phase")
	}
	if _, ok := c.MatchTaskLaunchFailure(fault(f.DependencyUnbound), request); ok {
		t.Fatal("ordinary fault acquired review finality")
	}
}

func TestTaskReviewFailurePreservesOtherPhaseAndKeepsSchemaFour(t *testing.T) {
	old, _ := failureControlRecord(t)
	request, _ := relaunchControlRequest(t)
	_, current, _, _, _, _, _ := newTaskLaunchControl(t)
	before := old.Before.Clone()
	before.State = c.TaskStateInReview
	request.Purpose = "task/review"
	request.ExpectedTaskVersion = before.Version
	origin := taskRelaunchRecord{Request: request, Task: before.Clone(), Sprint: old.Sprint.Clone(), Milestone: current.milestone, CreatedAt: old.CreatedAt}
	source := origin.source()
	r := c.TaskLaunchFailureRequest{Relaunch: &request, DispatchVersion: 3, LaunchAttempt: 1}
	facts := c.TaskLaunchFailureFacts{Relaunch: &source, Reason: old.Facts.Reason, OccurredAt: old.Facts.OccurredAt}
	groups := []taskGroupPlan{old.Groups[0], old.Groups[1]}
	groups[0].Group.State = c.TaskStateInReview
	record, err := buildTaskFailureRecord(r, facts, schedulerClaimRecord{}, before, old.Sprint, old.CurrentSprintID,
		groups, old.QueryGeneration, old.Blocker.ID, old.Event.TaskEventIDs, old.Header.EventID, old.CreatedAt, &origin)
	if err != nil || !record.Changed || record.After.State != c.TaskStateBlocked || record.After.Title != before.Title || record.After.Plan != before.Plan || len(record.History) != 2 || record.RelaunchEvent == nil || record.Event != nil || record.RelaunchEvent.FromState != c.TaskStateInReview {
		t.Fatal("review failure did not retain current facts and emit its real phase", err)
	}
	if record.History[1].State == nil || record.History[1].State.FromState != c.TaskStateInReview || record.History[1].Validate() != nil {
		t.Fatal("review failure history was not typed")
	}
	raw, err := canonical(record)
	var decoded taskFailureRecord
	if err != nil || json.Unmarshal(raw, &decoded) != nil || !sameValue(record, decoded) {
		t.Fatal("review failure record did not roundtrip", err)
	}
	factory, err := c.RegisterTaskLaunchFailureEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	value, err := record.event(factory)
	if err != nil || value.Header().SchemaVersion != c.TaskRelaunchFailureSchemaVersion {
		t.Fatal("review failed publication", err)
	}
	restored, err := factory.Restore(*record.Header, value.PayloadBytes())
	if err != nil || !bytes.Equal(restored.PayloadBytes(), value.PayloadBytes()) {
		t.Fatal("schema5 review bytes changed", err)
	}
	bad := record.RelaunchEvent.Clone()
	bad.Source.Request.Purpose = "task/work"
	if bad.Validate() == nil {
		t.Fatal("schema5 accepted a failure from another phase")
	}
	legacy, err := old.event(factory)
	legacyBytes, canonicalErr := canonical(old.Event)
	if err != nil || canonicalErr != nil || !bytes.Equal(legacy.PayloadBytes(), legacyBytes) {
		t.Fatal("schema4 historical bytes changed", err, canonicalErr)
	}
	oldPayload := old.Event.Clone()
	oldPayload.FromState = c.TaskStateInReview
	if oldPayload.Validate() == nil {
		t.Fatal("todo claim schema4 admitted review")
	}

	// A known failure remains bound to its original phase. Neither late work
	// failure nor late review failure can overwrite the user's next phase.
	changed := old.Before.Clone()
	changed.State = c.TaskStateInReview
	changed.Version++
	preserved, err := buildTaskFailureRecord(old.Request, old.Facts, old.Claim, changed, old.Sprint, old.CurrentSprintID,
		[]taskGroupPlan{}, 0, c.TaskBlockerID{}, []c.TaskEventID{}, event.EventID{}, old.CreatedAt)
	if err != nil || preserved.Changed || !sameValue(changed, preserved.After) || len(preserved.History) != 0 || preserved.Header != nil {
		t.Fatal("old work failure overwrote review", err)
	}
	for _, state := range []c.TaskState{c.TaskStateInProgress, c.TaskStateTodo, c.TaskStateDone} {
		changed = before.Clone()
		changed.State = state
		changed.Version++
		preserved, err = buildTaskFailureRecord(r, facts, schedulerClaimRecord{}, changed, old.Sprint, old.CurrentSprintID,
			[]taskGroupPlan{}, 0, c.TaskBlockerID{}, []c.TaskEventID{}, event.EventID{}, old.CreatedAt, &origin)
		if err != nil || preserved.Changed || !sameValue(changed, preserved.After) || preserved.Blocker != nil || preserved.RelaunchEvent != nil || len(preserved.History) != 0 {
			t.Fatal("old review failure overwrote later Task phase", state, err)
		}
	}
	changed = before.Clone()
	changed.State = c.TaskStateBlocked
	changed.Version++
	already, err := buildTaskFailureRecord(r, facts, schedulerClaimRecord{}, changed, old.Sprint, old.CurrentSprintID,
		[]taskGroupPlan{}, old.QueryGeneration, old.Blocker.ID, []c.TaskEventID{old.History[0].ID}, old.Header.EventID, old.CreatedAt, &origin)
	if err != nil || !already.Changed || len(already.History) != 1 || already.History[0].Type != "blocker_added" || already.RelaunchEvent.SourcePosition != nil || already.After.ManualRank != changed.ManualRank {
		t.Fatal("already blocked review invented a state change", err)
	}
}
