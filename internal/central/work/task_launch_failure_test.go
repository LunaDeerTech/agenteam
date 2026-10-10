package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestTaskLaunchFailureMatchesOnlyOriginalResourceConstraintRejection(t *testing.T) {
	p, s, _, _, ctx, actor, r := newTaskLaunchControl(t)
	r.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{"limit":"private-canary"}`)}
	_, err := p.DiscoverLaunch(ctx, actor, r)
	pureCode(t, err, f.DependencyUnbound)
	reason, ok := c.MatchTaskLaunchFailure(err, r)
	if !ok || reason != c.TaskLaunchFailureUnsupportedResourceConstraints || s.queries != 0 {
		t.Fatal("actual rejection not narrowly classified")
	}
	if strings.Contains(fmt.Sprintf("%+v", err), "private-canary") {
		t.Fatal("marker leaked policy")
	}
	if _, ok = c.MatchTaskLaunchFailure(fmt.Errorf("safe wrapper: %w", err), r); !ok {
		t.Fatal("cause chain lost")
	}
	for _, mutate := range []func(*ec.LaunchRequest){func(x *ec.LaunchRequest) { x.Meta.RequestID = pureID[f.Request](t, 122) }, func(x *ec.LaunchRequest) {
		x.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{"different":true}`)}
	}, func(x *ec.LaunchRequest) { x.Policy.DeniedToolIDs = []i.ToolID{pureID[i.Tool](t, 122)} }, func(x *ec.LaunchRequest) { x.Policy.AllowedResourceConstraints = []json.RawMessage{} }} {
		x := r.Clone()
		mutate(&x)
		if _, ok = c.MatchTaskLaunchFailure(err, x); ok {
			t.Fatal("marker replayed against changed request")
		}
	}
	if _, ok = c.MatchTaskLaunchFailure(fault(f.DependencyUnbound), r); ok {
		t.Fatal("ordinary unbound classified final")
	}
	alternate := r.Clone()
	retry := pureID[i.Execution](t, 123)
	alternate.Lineage.RetryOf = &retry
	_, e := p.DiscoverLaunch(ctx, actor, alternate)
	if e == nil {
		t.Fatal("alternate lineage allowed")
	}
	if _, ok = c.MatchTaskLaunchFailure(e, alternate); ok {
		t.Fatal("lineage became policy marker")
	}
}
func failureControlRecord(t *testing.T) (taskFailureRecord, i.Actor) {
	t.Helper()
	busy, actor := busyControlRecord(t)
	before := busy.Before.Clone()
	before.Title = "User's retained title"
	before.Plan = "User's retained plan"
	before.Version++
	target := groupForTask(before)
	target.State = c.TaskStateBlocked
	groups := []taskGroupPlan{{Group: groupForTask(before), Before: []rankItem{{before.ID.String(), before.ManualRank}}, Generation: 5}, {Group: target, Before: []rankItem{}, Generation: 2}}
	request := c.TaskLaunchFailureRequest{Claim: busy.Request.Claim, DispatchVersion: 3, LaunchAttempt: 1}
	facts := c.TaskLaunchFailureFacts{Guard: busy.Claim.Guard, Reason: c.TaskLaunchFailureUnsupportedResourceConstraints, OccurredAt: before.UpdatedAt}
	r, e := buildTaskFailureRecord(request, facts, busy.Claim, before, busy.Sprint, busy.CurrentSprintID, groups, 6, pureID[c.TaskBlockerIdentity](t, 110), []c.TaskEventID{pureID[c.TaskEvent](t, 111), pureID[c.TaskEvent](t, 112)}, pureID[event.EventIdentity](t, 113), before.UpdatedAt)
	if e != nil {
		t.Fatal(e)
	}
	return r, actor
}
func TestTaskLaunchFailureUsesCurrentPreimageAndTypedBlocker(t *testing.T) {
	r, _ := failureControlRecord(t)
	if !r.Changed || r.After.Version != r.Before.Version+1 || r.After.State != c.TaskStateBlocked || r.After.Title != r.Before.Title || r.After.Plan != r.Before.Plan || r.After.Priority != r.Before.Priority || !sameValue(r.After.AssigneeAgentID, r.Before.AssigneeAgentID) || len(r.History) != 2 || r.Blocker.Technical.ReferenceID != r.Request.Claim.DispatchID {
		t.Fatal("failure overwrote current user facts")
	}
	if len(r.Groups[0].After) != 0 || len(r.Groups[1].After) != 1 || r.Groups[1].After[0].ID != r.Before.ID.String() {
		t.Fatal("state group movement")
	}
	raw, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var decoded taskFailureRecord
	if e = json.Unmarshal(raw, &decoded); e != nil || !sameValue(r, decoded) {
		t.Fatal("immutable result roundtrip", e)
	}
	changed := r
	changed.After.Title = "lost user title"
	if validateTaskFailureRecord(&changed) == nil {
		t.Fatal("postimage not bound")
	}
	before := r.Before.Clone()
	before.State = c.TaskStateBlocked
	before.Version++
	already, e := buildTaskFailureRecord(r.Request, r.Facts, r.Claim, before, r.Sprint, r.CurrentSprintID, []taskGroupPlan{}, 7, r.Blocker.ID, []c.TaskEventID{r.History[0].ID}, r.Header.EventID, r.CreatedAt)
	if e != nil || !already.Changed || len(already.History) != 1 || already.History[0].Type != "blocker_added" || already.After.ManualRank != before.ManualRank || already.Event.SourcePosition != nil || len(already.Groups) != 0 {
		t.Fatal("already blocked invented state change", e)
	}
}
func TestTaskLaunchFailurePreservesInapplicableCurrentTask(t *testing.T) {
	r, _ := failureControlRecord(t)
	for _, change := range []func(*c.Task){func(x *c.Task) { x.AssigneeAgentID = nil; x.State = c.TaskStateBacklog }, func(x *c.Task) { other := pureID[i.Agent](t, 119); x.AssigneeAgentID = &other }, func(x *c.Task) { x.State = c.TaskStateDone }} {
		before := r.Before.Clone()
		before.Version++
		change(&before)
		out, e := buildTaskFailureRecord(r.Request, r.Facts, r.Claim, before, r.Sprint, r.CurrentSprintID, []taskGroupPlan{}, 0, c.TaskBlockerID{}, []c.TaskEventID{}, event.EventID{}, r.CreatedAt)
		if e != nil || out.Changed || !sameValue(out.Before, out.After) || out.Blocker != nil || len(out.History) != 0 || out.Event != nil || out.Header != nil {
			t.Fatal("inapplicable task changed", e)
		}
	}
	corrupt := r.Claim.After.Clone()
	corrupt.Title = "same version corruption"
	if _, e := buildTaskFailureRecord(r.Request, r.Facts, r.Claim, corrupt, r.Sprint, r.CurrentSprintID, r.Groups, r.QueryGeneration, r.Blocker.ID, []c.TaskEventID{r.History[0].ID, r.History[1].ID}, r.Header.EventID, r.CreatedAt); e == nil {
		t.Fatal("unversioned corruption accepted")
	}
}

type failureControlAuthority struct{ facts c.TaskLaunchFailureFacts }

func (a *failureControlAuthority) RequireTaskLaunchFailureDiscoveryInTx(context.Context, f.Tx, i.Actor, c.TaskLaunchFailureRequest) (c.TaskLaunchFailureFacts, error) {
	return a.facts.Clone(), nil
}
func (a *failureControlAuthority) RequireTaskLaunchFailureInTx(context.Context, f.Tx, i.Actor, c.TaskLaunchFailureRequest, c.TaskLaunchFailurePlan) (c.TaskLaunchFailureFacts, error) {
	return c.TaskLaunchFailureFacts{}, fault(f.Forbidden)
}
func failureControlPorts(t *testing.T) (*denialStore, TaskLaunchFailureDependencies) {
	t.Helper()
	store, _, authority, _ := purePorts(t)
	ev, e := c.RegisterTaskLaunchFailureEvents(event.NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	return store, TaskLaunchFailureDependencies{Authority: authority, Scheduler: &failureControlAuthority{}, Pending: &denialTransitionPending{}, Events: &denialEvents{}, FailureEvents: ev}
}

type forgedFailurePlan struct{}

func (forgedFailurePlan) RequiredLocks() []f.LockRequest { return nil }

type forgedFailureApplied struct{}

func (forgedFailureApplied) Changed() bool { return true }

type failureReadTailStore struct{ busyReadTailStore }

func (s *failureReadTailStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *failureReadTailStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "FROM agenteam_work.task_launch_failures") {
		s.queries++
		return busyAbsentRow{}
	}
	return s.taskLaunchTestStore.QueryRow(ctx, q, args...)
}
func TestTaskLaunchFailurePrivateProofAndCancellationJoin(t *testing.T) {
	store, deps := failureControlPorts(t)
	s, e := NewTaskLaunchFailure(store, deps)
	if e != nil {
		t.Fatal(e)
	}
	r, actor := failureControlRecord(t)
	var typed *failureControlAuthority
	for _, port := range []c.SchedulerTaskLaunchFailureAuthority{nil, typed} {
		bad := deps
		bad.Scheduler = port
		if _, e = NewTaskLaunchFailure(store, bad); e == nil {
			t.Fatal("missing authority")
		}
	}
	if _, e = NewTaskLaunchFailure(&denialStore{}, deps); e == nil {
		t.Fatal("foreign Store")
	}
	_, e = s.ApplyTaskLaunchFailureInTx(context.Background(), f.NewTx(), actor, r.Request, forgedFailurePlan{})
	pureCode(t, e, f.Forbidden)
	locks, e := failureMutationLocks(r.Request, r)
	if e != nil {
		t.Fatal(e)
	}
	p := &taskFailurePlan{owner: s, actor: actor, request: r.Request, record: r, baseLocks: locks, locks: locks}
	e = s.CheckTaskLaunchFailureAppliedInTx(context.Background(), f.NewTx(), actor, r.Request, p, forgedFailureApplied{})
	pureCode(t, e, f.Forbidden)
	applied := &taskFailureApplied{owner: s, plan: p, tx: f.NewTx()}
	e = s.CheckTaskLaunchFailureAppliedInTx(context.Background(), f.NewTx(), actor, r.Request, p, applied)
	pureCode(t, e, f.Forbidden)
	owned, done, e := s.beginFailure(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	s.Stop()
	if owned.Err() != context.Canceled || s.Joined() {
		t.Fatal("Stop abandoned active caller")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(s.Drain(ctx), context.Canceled) {
		t.Fatal("cancelled Drain became join")
	}
	done()
	if e = s.Drain(context.Background()); e != nil || !s.Joined() {
		t.Fatal("actual caller not retired")
	}
	if store.touches.Load() != 0 {
		t.Fatal("forged proof reached Store")
	}
	for _, unknown := range []bool{false, true} {
		_, source, project, _, base, actor, _ := newTaskLaunchControl(t)
		source.task.State = c.TaskStateDone
		source.task.Version++
		ctx, cancel := context.WithCancel(base)
		cause, e := readCause("failure-read-tail")
		if e != nil {
			t.Fatal(e)
		}
		physical := f.CommittedResult()
		if unknown {
			physical = f.UnknownResult(pureID[f.TransactionAttempt](t, 124), cause)
		}
		read := &failureReadTailStore{busyReadTailStore{taskLaunchTestStore: source, cancel: cancel, physical: physical}}
		authority, e := NewAuthority(read, project)
		if e != nil {
			t.Fatal(e)
		}
		ports := deps
		ports.Authority = authority
		ports.Scheduler = &failureControlAuthority{facts: c.TaskLaunchFailureFacts{Guard: source.claim.Guard, Reason: c.TaskLaunchFailureUnsupportedResourceConstraints, OccurredAt: source.task.UpdatedAt}}
		reader, e := NewTaskLaunchFailure(read, ports)
		if e != nil {
			t.Fatal(e)
		}
		request := c.TaskLaunchFailureRequest{Claim: source.claim.Request, DispatchVersion: 3, LaunchAttempt: 1}
		plan, e := reader.DiscoverTaskLaunchFailure(ctx, actor, request)
		cancel()
		if plan != nil || !read.returned || read.queries != 5 || read.writes != 0 || len(reader.calls) != 0 {
			t.Fatal("cancelled preserved discovery escaped original tail", e)
		}
		if unknown {
			pureCode(t, e, f.CommitUnknown)
			var fault *f.Fault
			if !errors.As(e, &fault) || fault.CauseID != physical.AttemptID().String() {
				t.Fatal("Unknown attempt replaced")
			}
		} else if !errors.Is(e, context.Canceled) {
			t.Fatal("committed read ignored cancel", e)
		}
	}
}
func TestTaskLaunchFailureHistoryAndEventRemainSeparate(t *testing.T) {
	r, actor := failureControlRecord(t)
	for _, h := range r.History {
		raw, e := json.Marshal(h)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = decodeTaskTriggerEvent(raw); e != nil {
			t.Fatal("new strict history unreadable", e)
		}
		var old c.TaskBusyTaskEvent
		var human c.TaskTransitionEvent
		if json.Unmarshal(raw, &old) == nil || json.Unmarshal(raw, &human) == nil {
			t.Fatal("failure entered old codec")
		}
		actorRaw, _ := json.Marshal(h.Actor)
		var payload any = h.Blocker
		if h.Type == "state_changed" {
			payload = h.State
		}
		data, _ := json.Marshal(payload)
		op := h.OperationID.String()
		row := taskTriggerRow{h.ID.String(), h.ProjectID.String(), h.TaskID.String(), h.TaskVersion, h.Type, actorRaw, (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), &op, op, data, h.CreatedAt.Time()}
		if _, e = scanTaskTriggerEventWithFailure(row); e != nil {
			t.Fatal("failure storage arm", e)
		}
		row[10] = &op
		if _, e = scanTaskTriggerEventWithFailure(row); e == nil {
			t.Fatal("dual operation arm")
		}
	}
	b := r.Blocker
	meta, _ := json.Marshal(b.Technical)
	by, _ := json.Marshal(b.SchedulerCreatedBy)
	row := taskTriggerRow{b.ID.String(), b.ProjectID.String(), b.TaskID.String(), b.Type, b.Description, meta, b.CreatedAt.Time(), by, r.Request.Claim.DispatchID, nil, nil, nil, nil, nil}
	decoded, e := scanBlocker(row)
	if e != nil || !sameValue(decoded.Value, *b) || decoded.FailureOperation == nil || decoded.CreatedOperation.Validate() == nil {
		t.Fatal("technical row projection", e)
	}
	store, deps := failureControlPorts(t)
	s, e := NewTaskLaunchFailure(store, deps)
	if e != nil {
		t.Fatal(e)
	}
	locks, e := failureMutationLocks(r.Request, r)
	if e != nil {
		t.Fatal(e)
	}
	p := &taskFailurePlan{owner: s, actor: actor, request: r.Request, record: r, baseLocks: locks, locks: locks}
	ev, e := deps.FailureEvents.NewTaskLaunchFailed(*r.Header, *r.Event)
	if e != nil {
		t.Fatal(e)
	}
	_, e = deps.Authority.DiscoverAppend(context.Background(), actor, ev.Summary())
	pureCode(t, e, f.Forbidden)
	ctx := context.WithValue(context.Background(), taskFailureContextKey{}, taskFailureContext{plan: p})
	prepared, e := deps.Authority.DiscoverAppend(ctx, actor, ev.Summary())
	if e != nil {
		t.Fatal(e)
	}
	for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
		e = deps.Authority.ValidateAppendInTx(ctx, f.NewTx(), actor, ev.Summary(), prepared, stage)
		pureCode(t, e, f.Forbidden)
	}
	if store.touches.Load() != 0 {
		t.Fatal("plan replaced private actual writer")
	}
	catalog := event.NewCatalog()
	if _, e = c.RegisterTaskTransitionEvents(catalog); e != nil {
		t.Fatal(e)
	}
	if _, e = c.RegisterSchedulerClaimEvents(catalog); e != nil {
		t.Fatal(e)
	}
	if _, e = c.RegisterTaskBusyCompensationEvents(catalog); e != nil {
		t.Fatal(e)
	}
	if _, e = c.RegisterTaskLaunchFailureEvents(catalog); e != nil {
		t.Fatal(e)
	}
}
