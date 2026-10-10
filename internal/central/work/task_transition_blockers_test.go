package work

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type unblockErrorRow struct{ err error }

func (r unblockErrorRow) Scan(...any) error { return r.err }

// This executor controls just the newly added reads/writes. Real transaction
// rollback, authorization and Outbox assembly belong to the separate PG case.
type unblockSQL struct {
	postgres.SQLExecutor
	t               *testing.T
	rows            map[string]taskTriggerRow
	count, linked   int64
	err             error
	cancel          context.CancelFunc
	queries, writes int
	rowCount        int64
	lastQuery       string
	lastArgs        []any
}

func (s *unblockSQL) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	s.queries++
	if s.cancel != nil {
		s.cancel()
	}
	if s.err != nil {
		return unblockErrorRow{s.err}
	}
	if !strings.Contains(q, "FROM agenteam_work.task_blockers") {
		s.t.Fatal("unexpected read")
	}
	if strings.Contains(q, "FILTER") {
		return taskTriggerRow{s.linked, s.count}
	}
	if strings.Contains(q, "count(*)") {
		return taskTriggerRow{s.count}
	}
	if len(args) != 3 {
		s.t.Fatal("unscoped blocker read")
	}
	if r, ok := s.rows[args[2].(string)]; ok {
		return r
	}
	return unblockErrorRow{pgx.ErrNoRows}
}
func (s *unblockSQL) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	s.writes++
	s.lastQuery = q
	s.lastArgs = append([]any(nil), args...)
	if s.err != nil {
		return pgconn.CommandTag{}, s.err
	}
	if s.rowCount == 0 {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func unblockRow(t *testing.T, b c.TaskBlocker, created string, resolved, transition *string) taskTriggerRow {
	t.Helper()
	var metadata, actor, resolver []byte
	var done *time.Time
	if b.Type == c.TaskBlockerTechnical {
		metadata, _ = json.Marshal(b.Technical)
		actor, _ = json.Marshal(b.SchedulerCreatedBy)
	} else {
		metadata, _ = json.Marshal(b.Metadata.WaitingForHuman)
		actor, _ = json.Marshal(b.CreatedBy)
	}
	if b.ResolvedAt != nil {
		at := b.ResolvedAt.Time()
		done = &at
		resolver, _ = json.Marshal(b.ResolvedBy)
	}
	return taskTriggerRow{b.ID.String(), b.ProjectID.String(), b.TaskID.String(), b.Type, b.Description, metadata, b.CreatedAt.Time(), actor, created, done, resolver, b.ResolutionComment, resolved, transition}
}
func unblockInput(t *testing.T) (transitionInput, c.TaskBlocker, i.Actor) {
	t.Helper()
	failure, _ := failureControlRecord(t)
	human := pureActor(t, 2)
	user, err := f.ParseID[i.User](human.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	in := transitionInput{Project: failure.After.ProjectID, Task: failure.After.ID, User: user, Expected: failure.After.Version, Request: c.TaskTransfer{TargetState: c.TaskStateTodo, AddBlockers: []c.TaskBlockerCreate{}, ResolveBlockerIDs: []c.TaskBlockerID{failure.Blocker.ID}}}
	return in, failure.Blocker.Clone(), human
}
func unblockRecord(t *testing.T) (*transitionRecord, i.Actor) {
	t.Helper()
	failure, _ := failureControlRecord(t)
	in, b, actor := unblockInput(t)
	before := failure.After.Clone()
	r := &transitionRecord{ID: pureID[c.TaskTransitionCommand](t, 151), Key: "unblock-key", Input: in, Revision: 1, State: "planned", Created: before.UpdatedAt}
	r.Semantic, _ = in.semantic(actor, r.Key)
	x := &unblockSQL{t: t, rows: map[string]taskTriggerRow{b.ID.String(): unblockRow(t, b, b.Technical.ReferenceID, nil, nil)}, count: 1}
	resolutions, at, err := prepareTransitionResolutions(context.Background(), x, in, before.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	after := before.Clone()
	after.State = c.TaskStateTodo
	after.Version++
	after.UpdatedAt = at
	after.ManualRank = "7fffffffffffffffffffffffffffffff"
	source, target := groupForTask(before), groupForTask(after)
	sp, _ := transitionPosition(source, 3, "", "")
	tp, _ := transitionPosition(target, 2, "", "")
	h := []c.TaskTransitionEvent{
		{ID: pureID[c.TaskEvent](t, 152), Type: c.TaskTransitionStateChanged, Payload: c.TaskTransitionFactPayload{StateChanged: &c.TaskStateChangedPayload{FromState: c.TaskStateBlocked, ToState: c.TaskStateTodo}}},
		{ID: pureID[c.TaskEvent](t, 153), Type: c.TaskTransitionBlockerResolved, Payload: c.TaskTransitionFactPayload{BlockerResolved: &c.TaskBlockerResolvedPayload{BlockerID: b.ID, BlockerType: c.TaskBlockerTechnical}}},
	}
	for n := range h {
		h[n].ProjectID = in.Project
		h[n].TaskID = in.Task
		h[n].TaskVersion = after.Version
		h[n].Actor = c.TaskTransitionActor{UserID: in.User}
		h[n].OperationID = r.ID
		h[n].CorrelationID = r.ID
		h[n].CreatedAt = at
	}
	header, err := taskHeader(pureID[event.EventIdentity](t, 154), after, at)
	if err != nil {
		t.Fatal(err)
	}
	header.EventType = c.TaskTransitionedName
	header.SchemaVersion = c.TaskTransitionSchemaVersion
	factory, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	mutation, ev, err := factory.NewTaskTransitionData(header, before, after, in.Request, h, sp, tp)
	if err != nil {
		t.Fatal(err)
	}
	r.Plan = transitionPlan{Before: before, After: mutation, Placement: taskPlacement{before.MilestoneID, before.SprintID, c.Current}, Agent: ac.AgentRef{ProjectID: in.Project, AgentID: *before.AssigneeAgentID, ConfigVersion: 1}, Groups: []taskGroupPlan{{source, []rankItem{{before.ID.String(), before.ManualRank}}, []rankItem{}, 2}, {target, []rankItem{}, []rankItem{{after.ID.String(), after.ManualRank}}, 1}}, QueryGeneration: 3, History: h, Source: sp, Target: tp, Header: ev.Header(), Payload: ev.PayloadBytes(), Resolutions: resolutions}
	if err = validateTransitionRecord(r, actor); err != nil {
		t.Fatal("unblock record", err)
	}
	return r, actor
}
func TestTaskUnblockFrozenResolutionsAndLegacyPlans(t *testing.T) {
	old, actor, _ := pureTransitionRecord(t)
	// Fixed pre-48 shape: no optional extension, unchanged ordering and bytes.
	type legacyPlan struct {
		Before          c.Task                   `json:"before"`
		After           c.TaskTransitionMutation `json:"after"`
		Placement       taskPlacement            `json:"placement"`
		Agent           ac.AgentRef              `json:"agent"`
		Groups          []taskGroupPlan          `json:"groups"`
		QueryGeneration int64                    `json:"query_generation"`
		History         []c.TaskTransitionEvent  `json:"history"`
		Source          c.TaskTransitionPosition `json:"source"`
		Target          c.TaskTransitionPosition `json:"target"`
		Header          event.Header             `json:"header"`
		Payload         json.RawMessage          `json:"payload"`
	}
	p := old.Plan
	legacy, err := json.Marshal(legacyPlan{p.Before, p.After, p.Placement, p.Agent, p.Groups, p.QueryGeneration, p.History, p.Source, p.Target, p.Header, p.Payload})
	if err != nil {
		t.Fatal(err)
	}
	var decoded transitionPlan
	if err = json.Unmarshal(legacy, &decoded); err != nil {
		t.Fatal(err)
	}
	old.Plan = decoded
	if err = validateTransitionRecord(old, actor); err != nil {
		t.Fatal(err)
	}
	current, _ := json.Marshal(old.Plan)
	if !bytes.Equal(current, legacy) || bytes.Contains(current, []byte("blocker_resolutions")) {
		t.Fatal("old plan bytes changed")
	}
	receipt, _ := json.Marshal(old.Plan.After)
	old.State = "completed"
	old.Receipt = &old.Plan.After
	if err = validateTransitionRecord(old, actor); err != nil {
		t.Fatal(err)
	}
	replay, _ := json.Marshal(old.Receipt)
	if !bytes.Equal(receipt, replay) {
		t.Fatal("old receipt changed")
	}
	r, human := unblockRecord(t)
	raw, err := json.Marshal(r.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &decoded); err != nil || !sameValue(decoded, r.Plan) {
		t.Fatal("new plan roundtrip", err)
	}
	for _, mutate := range []func(*transitionRecord){
		func(v *transitionRecord) { v.Plan.Resolutions[0].After.Description = "changed creation" },
		func(v *transitionRecord) { v.Plan.Resolutions[0].After.ResolvedBy.UserID = pureID[i.User](t, 155) },
		func(v *transitionRecord) { v.Plan.Resolutions[0].Before.ResolvedAt = &v.Created },
		func(v *transitionRecord) { v.Plan.Resolutions[0].FailureOperation = nil },
		func(v *transitionRecord) {
			v.Plan.History[1].Payload.BlockerResolved.BlockerType = c.TaskBlockerWaitingForHuman
		},
	} {
		copy := *r
		if err = json.Unmarshal(raw, &copy.Plan); err != nil {
			t.Fatal(err)
		}
		mutate(&copy)
		if validateTransitionRecord(&copy, human) == nil {
			t.Fatal("changed resolution plan accepted")
		}
	}
	before := r.Plan.Before
	request := r.Input.Request.Clone()
	if transitionEdge(before, request) != nil || *transitionAgent(before, request) != *before.AssigneeAgentID {
		t.Fatal("omitted assignee not retained")
	}
	other := pureID[i.Agent](t, 156)
	request.AssigneeAgentID = &other
	if transitionEdge(before, request) != nil || *transitionAgent(before, request) != other {
		t.Fatal("explicit replacement not selected")
	}
	in := r.Input
	in.Request = request
	locks, err := in.locks(r.Key, before)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []i.AgentID{*before.AssigneeAgentID, other} {
		key, _ := f.AgentLock(id.String())
		found := false
		for _, lock := range locks {
			if lock.Key.Canonical() == key.Canonical() && lock.Mode == f.Shared {
				found = true
			}
		}
		if !found {
			t.Fatal("old/new Agent lock omitted")
		}
	}
	before.AssigneeAgentID = nil
	request.AssigneeAgentID = nil
	pureCode(t, transitionEdge(before, request), f.TaskAssigneeRequired)
}
func TestTaskUnblockResolutionReadsRejectPartialOrStaleSets(t *testing.T) {
	in, b, _ := unblockInput(t)
	fresh := func() *unblockSQL {
		return &unblockSQL{t: t, rows: map[string]taskTriggerRow{b.ID.String(): unblockRow(t, b, b.Technical.ReferenceID, nil, nil)}, count: 1}
	}
	x := fresh()
	out, _, err := prepareTransitionResolutions(context.Background(), x, in, b.CreatedAt)
	if err != nil || len(out) != 1 || out[0].After.ResolvedBy.UserID != in.User || out[0].After.ResolutionComment != nil || !sameValue(out[0].Before, b) || x.writes != 0 {
		t.Fatal("resolution preimage", err)
	}
	for _, item := range []struct {
		setup func(*unblockSQL)
		code  f.Code
	}{
		{func(x *unblockSQL) { x.count = 2 }, f.InvalidState},
		{func(x *unblockSQL) { delete(x.rows, b.ID.String()) }, f.BlockerNotFound},
		{func(x *unblockSQL) {
			r := out[0].After
			op := pureID[c.TaskTransitionCommand](t, 151).String()
			x.rows[b.ID.String()] = unblockRow(t, r, b.Technical.ReferenceID, nil, &op)
		}, f.BlockerAlreadyResolved},
		{func(x *unblockSQL) { x.rows[b.ID.String()][1] = pureID[i.Project](t, 157).String() }, f.InternalError},
	} {
		x := fresh()
		item.setup(x)
		got, _, err := prepareTransitionResolutions(context.Background(), x, in, b.CreatedAt)
		pureCode(t, err, item.code)
		if got != nil || x.writes != 0 {
			t.Fatal("partial/stale set returned")
		}
	}
	x = fresh()
	x.err = errors.New("private SQL canary")
	got, _, err := prepareTransitionResolutions(context.Background(), x, in, b.CreatedAt)
	if err == nil || got != nil || x.writes != 0 || strings.Contains(err.Error(), "private SQL canary") {
		t.Fatal("SQL error returned data/material")
	}
	x = fresh()
	ctx, cancel := context.WithCancel(context.Background())
	x.cancel = cancel
	got, _, err = prepareTransitionResolutions(ctx, x, in, b.CreatedAt)
	if err == nil || !errors.Is(err, context.Canceled) || got != nil || x.writes != 0 {
		t.Fatal("cancel returned resolutions")
	}
	// A legitimate Human-created blocker keeps its independent command source.
	human := b.Clone()
	human.Type = c.TaskBlockerWaitingForHuman
	human.SchedulerCreatedBy = nil
	human.Technical = nil
	human.CreatedBy = c.TaskEventActor{Type: i.Human, UserID: in.User, Source: "task_domain"}
	human.Metadata = c.TaskBlockerMetadata{WaitingForHuman: &c.TaskBlockerWaitingForHumanMetadata{}}
	x = fresh()
	created := pureID[c.TaskBlockerCommand](t, 158)
	x.rows[b.ID.String()] = unblockRow(t, human, created.String(), nil, nil)
	got, _, err = prepareTransitionResolutions(context.Background(), x, in, b.CreatedAt)
	if err != nil || got[0].CreatedOperation == nil || *got[0].CreatedOperation != created || got[0].FailureOperation != nil {
		t.Fatal("Human origin lost", err)
	}
}
func TestTaskUnblockResolverWritesAndPostimage(t *testing.T) {
	r, _ := unblockRecord(t)
	v := r.Plan.Resolutions[0]
	x := &unblockSQL{t: t, rowCount: 1}
	if err := applyTransitionResolutions(context.Background(), x, r); err != nil {
		t.Fatal(err)
	}
	if x.writes != 1 || !strings.Contains(x.lastQuery, "resolved_transition_operation_id=$6") || !strings.Contains(x.lastQuery, "resolved_at IS NULL") || x.lastArgs[5] != r.ID.String() || x.lastArgs[6] != nil || x.lastArgs[7] != v.FailureOperation.String() {
		t.Fatal("wrong resolution parent or missing CAS")
	}
	for _, failed := range []*unblockSQL{{t: t, rowCount: 0}, {t: t, rowCount: 1, err: context.Canceled}} {
		if err := applyTransitionResolutions(context.Background(), failed, r); err == nil {
			t.Fatal("failed write reported success")
		}
	}
	op := r.ID.String()
	row := unblockRow(t, v.After, v.FailureOperation.String(), nil, &op)
	x = &unblockSQL{t: t, rows: map[string]taskTriggerRow{v.Before.ID.String(): row}, linked: 1}
	if err := verifyTransitionResolutions(context.Background(), x, r); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*unblockSQL){
		func(x *unblockSQL) { x.count = 1 }, func(x *unblockSQL) { x.linked = 2 },
		func(x *unblockSQL) {
			other := pureID[c.TaskTransitionCommand](t, 159).String()
			x.rows[v.Before.ID.String()][13] = &other
		},
		func(x *unblockSQL) {
			old := pureID[c.TaskBlockerCommand](t, 159).String()
			x.rows[v.Before.ID.String()][12] = &old
		},
	} {
		copy := *x
		copy.rows = map[string]taskTriggerRow{v.Before.ID.String(): append(taskTriggerRow(nil), row...)}
		change(&copy)
		if err := verifyTransitionResolutions(context.Background(), &copy, r); err == nil {
			t.Fatal("incorrect persisted resolution accepted")
		}
	}
}
