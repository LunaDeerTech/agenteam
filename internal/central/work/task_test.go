package work

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This pure constructor/lifetime dependency rejects every fact request. It
// does not represent an empty Scheduler or prove a real claim. PG fixtures
// inject the same Store's actual scheduler.PendingAuthority instead.
type denialTaskPending struct{}

func (*denialTaskPending) RequireNoPendingGroupsInTx(context.Context, f.Tx, i.ProjectID, []ec.PendingClaimGroup) error {
	return f.NewFault(f.Forbidden, f.NotStarted)
}

func pureTaskPorts(t *testing.T) (*denialStore, *Authority, *Reader, TaskDependencies) {
	t.Helper()
	store, _, authority, _ := purePorts(t)
	structure, err := NewReader(store, authority, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	events, err := c.RegisterTaskEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return store, authority, structure, TaskDependencies{Authority: authority, Structure: structure, TaskEvents: events, Events: &denialEvents{}, Activity: &denialActivity{}, Pending: &denialTaskPending{}}
}

func TestTaskConstructorsRequireCompleteExactBindings(t *testing.T) {
	store, authority, structure, deps := pureTaskPorts(t)
	if _, err := NewTask(store, deps); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTaskReader(store, authority, structure, pureKeys(t)); err != nil {
		t.Fatal(err)
	}
	var missingEvents *denialEvents
	var missingActivity *denialActivity
	var missingPending *denialTaskPending
	var missingStore *denialStore
	_, err := NewTask(missingStore, deps)
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewTaskReader(missingStore, authority, structure, pureKeys(t))
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewTaskReader(store, nil, structure, pureKeys(t))
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewTaskReader(store, authority, nil, pureKeys(t))
	pureCode(t, err, f.DependencyUnbound)
	for name, change := range map[string]func(*TaskDependencies){
		"authority":      func(d *TaskDependencies) { d.Authority = nil },
		"structure":      func(d *TaskDependencies) { d.Structure = nil },
		"events":         func(d *TaskDependencies) { d.Events = nil },
		"typed-events":   func(d *TaskDependencies) { d.Events = missingEvents },
		"catalog":        func(d *TaskDependencies) { d.TaskEvents = c.TaskEvents{} },
		"activity":       func(d *TaskDependencies) { d.Activity = nil },
		"typed-activity": func(d *TaskDependencies) { d.Activity = missingActivity },
		"pending":        func(d *TaskDependencies) { d.Pending = nil },
		"typed-pending":  func(d *TaskDependencies) { d.Pending = missingPending },
	} {
		t.Run(name, func(t *testing.T) {
			bad := deps
			change(&bad)
			_, err := NewTask(store, bad)
			pureCode(t, err, f.DependencyUnbound)
		})
	}
	other, otherAuthority, otherReader, _ := pureTaskPorts(t)
	_, err = NewTask(other, deps)
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewTaskReader(store, otherAuthority, structure, pureKeys(t))
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewTaskReader(store, authority, otherReader, pureKeys(t))
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewTaskReader(store, authority, structure, cursor.Keyring{})
	pureCode(t, err, f.DependencyUnbound)
	// A second authority over the same Store is still a different binding.
	second, err := NewAuthority(store, authority.state().projects)
	if err != nil {
		t.Fatal(err)
	}
	secondReader, err := NewReader(store, second, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	bad := deps
	bad.Structure = secondReader
	_, err = NewTask(store, bad)
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewTaskReader(store, authority, secondReader, pureKeys(t))
	pureCode(t, err, f.DependencyUnbound)
	if store.touches.Load() != 0 || other.touches.Load() != 0 {
		t.Fatal("constructor performed database work")
	}
}

func TestTaskStopDrainWaitsForOwnedCallAndConfirmation(t *testing.T) {
	store, _, _, deps := pureTaskPorts(t)
	service, err := NewTask(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	run, entry, done, err := service.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	confirm, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.state().mu.Lock()
	entry.confirmations[&confirmation{cancel: cancel}] = struct{}{}
	service.state().mu.Unlock()
	service.Stop()
	service.Stop()
	if run.Err() != context.Canceled || confirm.Err() != context.Canceled {
		t.Fatal("Stop failed to cancel registered work")
	}
	closed, close := context.WithCancel(context.Background())
	close()
	if err = service.Drain(closed); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was treated as a join", err)
	}
	_, _, _, err = service.begin(context.Background())
	pureCode(t, err, f.ShuttingDown)
	done()
	done()
	if err = service.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.touches.Load() != 0 {
		t.Fatal("Stop closed or used the shared Store")
	}
}

func TestTaskReaderCancellationAndMembershipForeignTxFailClosed(t *testing.T) {
	store, authority, structure, _ := pureTaskPorts(t)
	reader, err := NewTaskReader(store, authority, structure, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	project := pureID[i.Project](t, 5)
	task := pureID[c.Task](t, 6)
	got, err := reader.GetTask(ctx, pureActor(t, 2), project, task)
	if !errors.Is(err, context.Canceled) || got.ID.Validate() == nil {
		t.Fatal("canceled Get returned a result", err)
	}
	page, err := reader.ListTasks(ctx, pureActor(t, 2), project, c.TaskFilter{}, f.DefaultPageRequest())
	if !errors.Is(err, context.Canceled) || page.Items != nil {
		t.Fatal("canceled List became an empty page", err)
	}
	member, err := reader.HasTasksInSprintInTx(context.Background(), f.NewTx(), pureActor(t, 2), project, pureID[pc.Sprint](t, 7))
	if err == nil || member || store.touches.Load() != 1 {
		t.Fatal("foreign transaction reached membership facts")
	}
}

func TestTaskLockRawCeilingOrderAndCounterBoundaries(t *testing.T) {
	p := pureID[i.Project](t, 5)
	g := taskGroup{pureID[pc.Sprint](t, 6), c.TaskStateBacklog, c.TaskPriorityMedium}
	key := taskRankLock(p, g)
	for _, n := range []int{512, 513} {
		raw := make([]f.LockRequest, n)
		for j := range raw {
			raw[j] = key
		}
		locks, err := taskNormalize(raw)
		if n == 512 {
			if err != nil || len(locks) != 1 {
				t.Fatal("legal raw union failed", err)
			}
		} else {
			pureCode(t, err, f.ResourceBusy)
		}
	}
	priority := c.TaskPriorityCritical
	in := taskInput{Command: taskUpdate, Project: p, Target: pureID[c.Task](t, 7), User: pureID[i.User](t, 1), Update: &c.TaskFieldsUpdate{Priority: &priority}}
	locks, err := in.locks("task-locks", g)
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 8 {
		t.Fatalf("two-group union has %d locks", len(locks))
	}
	for j := 1; j < len(locks); j++ {
		if f.CompareLockKeys(locks[j-1].Key, locks[j].Key) >= 0 {
			t.Fatal("unsorted full lock union")
		}
	}
	for _, n := range []int64{0, -1, math.MaxInt64} {
		_, err = taskCounter(n)
		pureCode(t, err, f.InvalidState)
	}
	if n, err := taskCounter(math.MaxInt64 - 1); err != nil || n != math.MaxInt64 {
		t.Fatal("last legal counter increment rejected")
	}
}

func TestTaskListBindingPreservesAllPredicatePresence(t *testing.T) {
	p := pureID[i.Project](t, 5)
	user := pureID[i.User](t, 1).String()
	state := c.TaskStateBacklog
	priority := c.TaskPriorityMedium
	kind := c.TaskTypeTask
	agent := pureID[i.Agent](t, 8)
	milestone := pureID[c.Milestone](t, 9)
	sprint := pureID[pc.Sprint](t, 10)
	text := "%_\\Literal"
	filters := []c.TaskFilter{{}, {State: &state}, {Priority: &priority}, {Type: &kind}, {AssigneeAgentID: c.TaskAssigneeFilter{Present: true}}, {AssigneeAgentID: c.TaskAssigneeFilter{Present: true, AgentID: &agent}}, {MilestoneID: &milestone}, {SprintID: &sprint}, {Text: &text}}
	seen := map[f.Digest]bool{}
	for _, filter := range filters {
		binding, err := taskListBinding(p, user, filter)
		if err != nil {
			t.Fatal(err)
		}
		if seen[binding.QueryDigest] {
			t.Fatal("distinct predicates share a digest")
		}
		seen[binding.QueryDigest] = true
		if binding.Order != taskPageOrder {
			t.Fatal("incorrect stable order")
		}
	}
	base, err := taskListBinding(p, user, c.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := taskListBinding(p, pureID[i.User](t, 2).String(), c.TaskFilter{})
	if err != nil || base.QueryDigest == other.QueryDigest {
		t.Fatal("owner not bound")
	}
	other, err = taskListBinding(pureID[i.Project](t, 50), user, c.TaskFilter{})
	if err != nil || base.QueryDigest == other.QueryDigest {
		t.Fatal("project not bound")
	}
	if a, b := pureActor(t, 2), pureActor(t, 3); a.Details().UserID != b.Details().UserID {
		t.Fatal("bad same-owner session fixture")
	}
}

func TestTaskCursorRequiresExactFiveScalarsAndGeneration(t *testing.T) {
	keys := pureKeys(t)
	p := pureID[i.Project](t, 5)
	binding, err := taskListBinding(p, pureID[i.User](t, 1).String(), c.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	sprint, _ := cursor.UUID(pureID[pc.Sprint](t, 6).String())
	id, _ := cursor.UUID(pureID[c.Task](t, 7).String())
	rank, _ := cursor.Text("7fffffffffffffffffffffffffffffff")
	gen := int64(3)
	valid := cursor.Position{Scalars: []cursor.Scalar{sprint, cursor.Integer(0), cursor.Integer(2), rank, id}, OrderGeneration: &gen}
	token, err := keys.Sign(binding, valid)
	if err != nil {
		t.Fatal(err)
	}
	position, err := taskPageAfter(keys, token, binding, 3)
	if err != nil || position.sprint != sprint.Value() || position.priority != 2 {
		t.Fatal("valid position rejected", err)
	}
	_, err = taskPageAfter(keys, token, binding, 4)
	pureCode(t, err, f.CursorStale)
	for _, change := range []func(*cursor.Position){func(v *cursor.Position) { v.OrderGeneration = nil }, func(v *cursor.Position) { v.Scalars = v.Scalars[:4] }, func(v *cursor.Position) { v.Scalars[0] = rank }, func(v *cursor.Position) { v.Scalars[1] = cursor.Integer(7) }, func(v *cursor.Position) { v.Scalars[1] = cursor.Integer(-1) }, func(v *cursor.Position) { v.Scalars[2] = cursor.Integer(4) }, func(v *cursor.Position) { v.Scalars[3], _ = cursor.Text("0") }, func(v *cursor.Position) { v.Scalars[4] = rank }} {
		bad := cursor.Position{Scalars: append([]cursor.Scalar{}, valid.Scalars...), OrderGeneration: &gen}
		change(&bad)
		token, err := keys.Sign(binding, bad)
		if err != nil {
			t.Fatal(err)
		}
		_, err = taskPageAfter(keys, token, binding, 3)
		pureCode(t, err, f.CursorInvalid)
	}
	other := binding
	other.Order = "manual_rank:asc,id:asc"
	_, err = taskPageAfter(keys, token, other, 3)
	pureCode(t, err, f.CursorInvalid)
}

func TestTaskUnknownConfirmationCannotReplaceWriterProvenance(t *testing.T) {
	a := pureActor(t, 2)
	p := pureID[i.Project](t, 5)
	id, err := c.TaskIdentity(p, c.TaskCommandUpdate, "private-task-key")
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(id)
	if err != nil {
		t.Fatal(err)
	}
	original := f.UnknownResult(pureID[f.TransactionAttempt](t, 90), cause)
	for _, denied := range []f.CommitResult{f.NotCommittedResult(f.NewFault(f.SessionRevoked, f.NotCommitted)), f.UnknownResult(pureID[f.TransactionAttempt](t, 91), cause)} {
		store := &confirmationFailureStore{result: denied}
		st := &taskServiceState{store: store, calls: map[*call]struct{}{}, changed: make(chan struct{})}
		service := &TaskService{data: func() *taskServiceState { return st }}
		ctx, entry, done, err := service.begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.confirmTaskUnknown(ctx, entry, a, c.TaskCommandLookupRequest{ProjectID: p, Command: c.TaskCommandUpdate, IdempotencyKey: "private-task-key", SemanticDigest: f.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")}, original)
		done()
		var public *f.Fault
		var private commitFailure
		if !errors.As(err, &public) || !errors.As(err, &private) || public.Code != f.CommitUnknown || public.CommitState != f.Unknown || public.CauseID != original.AttemptID().String() || private.result.AttemptID() != original.AttemptID() || private.result.Cause().Details().Primary.Canonical() != cause.Details().Primary.Canonical() {
			t.Fatal("confirmation replaced original writer", err)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := taskTxError(cancelled, f.CommittedResult()); err != nil {
		t.Fatal("delivery cancellation overturned commit", err)
	}
	if err := taskTxError(cancelled, f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))); !errors.Is(err, context.Canceled) {
		t.Fatal("known rollback lost cancellation", err)
	}
	if err := taskTxError(cancelled, original); err == nil {
		t.Fatal("unknown cancellation became success")
	}
}

func pureTaskRecord(t *testing.T) (*taskRecord, i.Actor, event.Summary) {
	t.Helper()
	a := pureActor(t, 2)
	projectID, id := pureID[i.Project](t, 5), pureID[c.Task](t, 6)
	sprintID, milestoneID := pureID[pc.Sprint](t, 7), pureID[c.Milestone](t, 8)
	input := taskInput{Command: taskCreate, Project: projectID, Target: id, User: pureID[i.User](t, 1), Create: &c.TaskCreate{TaskID: id, SprintID: sprintID, Title: "private title", Description: "private description", Plan: "private plan", Type: c.TaskTypeFeature, Priority: c.TaskPriorityHigh}}
	d, err := input.semantic(a, "private-task-key")
	if err != nil {
		t.Fatal(err)
	}
	at, err := f.NewInstant(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	evID, historyID, commandID := pureID[event.EventIdentity](t, 9), pureID[c.TaskEvent](t, 10), pureID[c.TaskCommand](t, 11)
	task := c.Task{ID: id, ProjectID: projectID, MilestoneID: milestoneID, SprintID: sprintID, Title: input.Create.Title, Description: input.Create.Description, Plan: input.Create.Plan, Type: input.Create.Type, Priority: input.Create.Priority, State: c.TaskStateBacklog, ManualRank: "7fffffffffffffffffffffffffffffff", Version: 1, CreatedAt: at, UpdatedAt: at}
	h, err := taskHeader(evID, task, at)
	if err != nil {
		t.Fatal(err)
	}
	fields := []c.TaskChangedField{c.TaskDescriptionChanged, c.TaskRankChanged, c.TaskPlanChanged, c.TaskPriorityChanged, c.TaskTitleChanged, c.TaskTypeChanged}
	payload := c.TaskChanged{CommandID: commandID, ActorUserID: input.User, TaskEventID: historyID, MilestoneID: milestoneID, SprintID: sprintID, Change: c.TaskCreatedChange, ChangedFields: fields, Position: &c.TaskPosition{SprintID: sprintID, State: c.TaskStateBacklog, Priority: c.TaskPriorityHigh, OrderGeneration: 2}}
	typed, err := c.RegisterTaskEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	ev, err := typed.NewTaskChanged(h, payload)
	if err != nil {
		t.Fatal(err)
	}
	historyPayload, err := json.Marshal(c.TaskCreatedPayload{InitialState: c.TaskStateBacklog, MilestoneID: milestoneID, SprintID: sprintID, Type: c.TaskTypeFeature, Priority: c.TaskPriorityHigh})
	if err != nil {
		t.Fatal(err)
	}
	history, err := json.Marshal(c.TaskEvent{ID: historyID, ProjectID: projectID, TaskID: id, TaskVersion: 1, Type: c.TaskEventCreated, Actor: c.TaskEventActor{Type: i.Human, UserID: input.User, Source: "task_domain"}, OperationID: commandID, CorrelationID: commandID, Payload: historyPayload, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	record := &taskRecord{ID: commandID, Project: projectID, User: input.User, Command: taskCreate, Key: "private-task-key", Semantic: d, Input: input, Revision: 1, State: "planned", TaskEventID: &historyID, EventID: &evID, Created: at, Plan: &taskPlan{After: c.TaskMutation{Task: task, Changed: true, TaskEventID: &historyID, EventIDs: []event.EventID{evID}}, Placement: taskPlacement{milestoneID, sprintID, c.Planned}, Groups: []taskGroupPlan{{Group: groupForTask(task), Before: []rankItem{}, After: []rankItem{{ID: id.String(), Rank: task.ManualRank}}, Generation: 1}}, QueryGeneration: 1, TaskEvent: history, Header: h, Payload: ev.PayloadBytes()}}
	if err = validateTaskRecord(record, a); err != nil {
		t.Fatal("valid compact Task plan rejected", err)
	}
	return record, a, ev.Summary()
}

func TestTaskPlanBindsIssuerSessionRevisionAndImmutableFacts(t *testing.T) {
	r, a, summary := pureTaskRecord(t)
	binding, locks, opaque, err := taskEventBinding(r, a, summary)
	if err != nil {
		t.Fatal(err)
	}
	issuer := oc.NewPlanIssuer()
	deps, err := oc.NewDependencies(issuer, binding, locks, opaque)
	if err != nil {
		t.Fatal(err)
	}
	if !deps.Matches(issuer, binding) || deps.Matches(oc.NewPlanIssuer(), binding) {
		t.Fatal("issuer not bound")
	}
	copied := deps.Opaque()
	copied[0] = 'x'
	if string(copied) == string(deps.Opaque()) {
		t.Fatal("opaque aliases caller storage")
	}
	cloned := deps.Locks()
	cloned[0].Mode = f.Shared
	if !sameTaskLocks(deps.Locks(), locks) {
		t.Fatal("lock result aliases plan")
	}
	other, _, _, err := taskEventBinding(r, pureActor(t, 3), summary)
	if err != nil || other == binding {
		t.Fatal("current Session not bound", err)
	}
	r.Revision++
	other, _, _, err = taskEventBinding(r, a, summary)
	if err != nil || other == binding {
		t.Fatal("plan revision not bound", err)
	}
	for name, change := range map[string]func(*taskRecord){
		"title":            func(r *taskRecord) { r.Plan.After.Task.Title = "forged" },
		"rank":             func(r *taskRecord) { r.Plan.After.Task.ManualRank = "00000000000000000000000000000001" },
		"query-generation": func(r *taskRecord) { r.Plan.QueryGeneration = 0 },
		"group-generation": func(r *taskRecord) { r.Plan.Groups[0].Generation = 2 },
		"missing-history":  func(r *taskRecord) { r.Plan.TaskEvent = nil },
		"wrong-history":    func(r *taskRecord) { r.Plan.TaskEvent = []byte(`{}`) },
		"event-id":         func(r *taskRecord) { id := pureID[event.EventIdentity](t, 100); r.EventID = &id },
		"header":           func(r *taskRecord) { r.Plan.Header.AggregateType = "work.milestone" },
		"command-time": func(r *taskRecord) {
			r.Created, _ = f.NewInstant(r.Plan.Header.OccurredAt.Time().Add(time.Second))
		},
		"input": func(r *taskRecord) { r.Input.Create.Description = "another private text" },
	} {
		t.Run(name, func(t *testing.T) {
			record, actor, _ := pureTaskRecord(t)
			change(record)
			if validateTaskRecord(record, actor) == nil {
				t.Fatal("immutable forged plan accepted")
			}
		})
	}
}

func TestTaskProducerRejectsBeforeFirstPrivateFactRead(t *testing.T) {
	record, actor, summary := pureTaskRecord(t)
	binding, locks, opaque, err := taskEventBinding(record, actor, summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foreign-issuer", "missing-locks", "revoked-session", "foreign-tx"} {
		t.Run(name, func(t *testing.T) {
			store := &gateReadStore{tx: f.NewTx(), held: name != "missing-locks"}
			sessions := &gateRevokedSession{}
			projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: sessions})
			if err != nil {
				t.Fatal(err)
			}
			authority, err := NewAuthority(store, projects)
			if err != nil {
				t.Fatal(err)
			}
			issuer := authority.state().issuer
			if name == "foreign-issuer" {
				issuer = oc.NewPlanIssuer()
			}
			deps, err := oc.NewDependencies(issuer, binding, locks, opaque)
			if err != nil {
				t.Fatal(err)
			}
			tx := store.tx
			if name == "foreign-tx" {
				tx = f.NewTx()
			}
			err = authority.ValidateAppendInTx(context.Background(), tx, actor, summary, deps, oc.CurrentAccess)
			if name == "revoked-session" {
				pureCode(t, err, f.SessionRevoked)
				if sessions.calls != 1 {
					t.Fatal("current session missing")
				}
			} else {
				pureCode(t, err, f.Forbidden)
				if sessions.calls != 0 {
					t.Fatal("issuer/Tx/lock failure reached Session")
				}
			}
			if store.workQueries != 0 {
				t.Fatal("Work fact queried before current gate")
			}
		})
	}
}

func TestTaskReorderPlanRequiresExactTargetPreimageRank(t *testing.T) {
	r, actor, _ := pureTaskRecord(t)
	before := r.Plan.After.Task.Clone()
	before.ManualRank = "3fffffffffffffffffffffffffffffff"
	after := before.Clone()
	after.Version = 2
	after.ManualRank = "bfffffffffffffffffffffffffffffff"
	after.UpdatedAt, _ = f.NewInstant(before.UpdatedAt.Time().Add(time.Second))
	spectator := pureID[c.Task](t, 50)
	version := f.Version(1)
	r.Command = taskReorder
	r.Input.Command, r.Input.Create, r.Input.Reorder, r.Input.Expected = taskReorder, nil, &c.TaskReorder{}, &version
	var err error
	r.Semantic, err = r.Input.semantic(actor, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	r.Plan.Before, r.Plan.After.Task = &before, after
	r.Plan.Groups[0].Before = []rankItem{{before.ID.String(), before.ManualRank}, {spectator.String(), "7fffffffffffffffffffffffffffffff"}}
	r.Plan.Groups[0].After = []rankItem{{spectator.String(), "7fffffffffffffffffffffffffffffff"}, {before.ID.String(), after.ManualRank}}
	position := &c.TaskPosition{SprintID: after.SprintID, State: c.TaskStateBacklog, Priority: after.Priority, PreviousID: &spectator, OrderGeneration: 2}
	fields := []c.TaskChangedField{c.TaskRankChanged}
	r.Plan.Header, err = taskHeader(*r.EventID, after, after.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	r.Plan.Payload, err = json.Marshal(c.TaskChanged{CommandID: r.ID, ActorUserID: r.User, TaskEventID: *r.TaskEventID, MilestoneID: after.MilestoneID, SprintID: after.SprintID, Change: c.TaskReorderedChange, ChangedFields: fields, Position: position})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(c.TaskFieldsUpdatedPayload{ChangedFields: fields, Position: position})
	if err != nil {
		t.Fatal(err)
	}
	r.Plan.TaskEvent, err = json.Marshal(c.TaskEvent{ID: *r.TaskEventID, ProjectID: r.Project, TaskID: after.ID, TaskVersion: 2, Type: c.TaskEventFieldsUpdated, Actor: c.TaskEventActor{Type: i.Human, UserID: r.User, Source: "task_domain"}, OperationID: r.ID, CorrelationID: r.ID, Payload: payload, CreatedAt: after.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if err = validateTaskRecord(r, actor); err != nil {
		t.Fatal("valid reorder plan rejected", err)
	}
	// Removing the target before insertion can otherwise hide this forged old
	// physical rank: the resulting order and new rank would still be identical.
	r.Plan.Groups[0].Before[0].Rank = "00000000000000000000000000000001"
	if validateTaskRecord(r, actor) == nil {
		t.Fatal("forged target preimage rank accepted")
	}
}

func TestTaskPrivatePlansRejectNestedAliasesMissingKeysAndNull(t *testing.T) {
	r, actor, _ := pureTaskRecord(t)
	raw, err := json.Marshal(r.Plan)
	if err != nil {
		t.Fatal(err)
	}
	decode := func(raw []byte) error {
		plan, err := decodePrivate[taskPlan](raw, taskPlanCap, taskPlanFields)
		if err != nil {
			return err
		}
		copy := *r
		copy.Plan = &plan
		return validateTaskRecord(&copy, actor)
	}
	if err = decode(raw); err != nil {
		t.Fatal("canonical private plan rejected", err)
	}
	for name, transform := range map[string]func(string) string{
		"placement-case": func(s string) string { return strings.Replace(s, `"state":"planned"`, `"STATE":"planned"`, 1) },
		"placement-alias": func(s string) string {
			return strings.Replace(s, `"state":"planned"`, `"state":"planned","STATE":"planned"`, 1)
		},
		"placement-missing": func(s string) string { return strings.Replace(s, `,"state":"planned"`, "", 1) },
		"placement-null":    func(s string) string { return strings.Replace(s, `"state":"planned"`, `"state":null`, 1) },
		"placement-unknown": func(s string) string {
			return strings.Replace(s, `"state":"planned"`, `"state":"planned","extra":true`, 1)
		},
		"group-case":               func(s string) string { return strings.Replace(s, `"group":`, `"GROUP":`, 1) },
		"group-alias":              func(s string) string { return strings.Replace(s, `"generation":1`, `"generation":1,"Generation":1`, 1) },
		"group-missing-generation": func(s string) string { return strings.Replace(s, `,"generation":1`, "", 1) },
		"group-null-before":        func(s string) string { return strings.Replace(s, `"before":[]`, `"before":null`, 1) },
		"rank-id-case":             func(s string) string { return strings.Replace(s, `"ID":`, `"id":`, 1) },
		"rank-case":                func(s string) string { return strings.Replace(s, `"Rank":`, `"rank":`, 1) },
		"rank-alias": func(s string) string {
			return strings.Replace(s, `"Rank":"7fffffffffffffffffffffffffffffff"`, `"Rank":"7fffffffffffffffffffffffffffffff","rank":"7fffffffffffffffffffffffffffffff"`, 1)
		},
		"rank-missing": func(s string) string { return strings.Replace(s, `,"Rank":"7fffffffffffffffffffffffffffffff"`, "", 1) },
		"rank-null": func(s string) string {
			return strings.Replace(s, `"Rank":"7fffffffffffffffffffffffffffffff"`, `"Rank":null`, 1)
		},
		"rank-duplicate": func(s string) string {
			return strings.Replace(s, `"Rank":"7fffffffffffffffffffffffffffffff"`, `"Rank":"7fffffffffffffffffffffffffffffff","Rank":"7fffffffffffffffffffffffffffffff"`, 1)
		},
		"rank-surrogate": func(s string) string {
			return strings.Replace(s, `"Rank":"7fffffffffffffffffffffffffffffff"`, `"Rank":"\ud800"`, 1)
		},
		"header-case":       func(s string) string { return strings.Replace(s, `"event_type":`, `"EVENT_TYPE":`, 1) },
		"header-scope-case": func(s string) string { return strings.Replace(s, `"scope":`, `"SCOPE":`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := transform(string(raw))
			if changed == string(raw) {
				t.Fatal("test did not alter private wire")
			}
			if decode([]byte(changed)) == nil {
				t.Fatal("noncanonical nested private plan accepted")
			}
		})
	}
}

func TestTaskPrivateInputAndOpaqueStayStrict(t *testing.T) {
	r, _, _ := pureTaskRecord(t)
	raw, err := json.Marshal(r.Input)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{strings.Replace(string(raw), `"command":`, `"COMMAND":`, 1), strings.Replace(string(raw), `"title":"private title"`, `"title":"private title","TITLE":"private title"`, 1), strings.Replace(string(raw), `"title":"private title"`, `"title":"\ud800"`, 1)} {
		if _, err := decodePrivate[taskInput]([]byte(changed), taskRequestCap, taskInputFields); err == nil {
			t.Fatal("noncanonical private input accepted")
		}
	}
	opaque := taskOpaque{Kind: "task_planning", CommandID: r.ID, Revision: 1, TaskEventID: *r.TaskEventID}
	raw, err = json.Marshal(opaque)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{strings.Replace(string(raw), `"kind":`, `"KIND":`, 1), strings.Replace(string(raw), `"plan_revision":"1"`, `"plan_revision":"1","Plan_Revision":"1"`, 1), strings.Replace(string(raw), `"kind":"task_planning"`, `"kind":null`, 1)} {
		if _, err := decodePrivate[taskOpaque]([]byte(changed), 16384, []string{"kind", "command_id", "plan_revision", "task_event_id"}); err == nil {
			t.Fatal("noncanonical opaque accepted")
		}
	}
}

func TestTaskProjectGateAcceptsOnlyExactNewTriple(t *testing.T) {
	_, projects, _, _ := purePorts(t)
	record, actor, summary := pureTaskRecord(t)
	request, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: record.Project, Actor: actor, Stage: oc.CurrentAccess, Event: summary})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := projects.Discover(context.Background(), request)
	if err != nil || deps.Validate() != nil {
		t.Fatal("Task Project gate unavailable", err)
	}
	for _, change := range []func(*event.Summary){func(s *event.Summary) { s.Header.EventType = "work.task_unbound" }, func(s *event.Summary) { s.Header.AggregateType = "work.sprint" }, func(s *event.Summary) { s.Header.SchemaVersion = 2 }} {
		bad := summary
		change(&bad)
		request, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: record.Project, Actor: actor, Stage: oc.CurrentAccess, Event: bad})
		if err != nil {
			t.Fatal(err)
		}
		_, err = projects.Discover(context.Background(), request)
		pureCode(t, err, f.DependencyUnbound)
	}
}
