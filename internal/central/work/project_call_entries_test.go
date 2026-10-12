package work

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These controls stop at the real registration Store call. No successful SQL,
// current-user grant, lifecycle stop loan or business mutation is fabricated.
type entryRegistrationStore struct {
	denialStore
	calls        int
	records      []projectCallRecord
	beforeReturn func(context.Context)
}

func (s *entryRegistrationStore) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	if w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); ok && w.owner != nil {
		s.records = append(s.records, w.owner.record)
	}
	if s.beforeReturn != nil {
		s.beforeReturn(ctx)
	}
	return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
}

type entryProcessProof struct{}

func (entryProcessProof) RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error {
	return fault(f.Forbidden)
}
func (entryProcessProof) ConfirmStopped(context.Context, oc.ProcessID) error {
	return fault(f.Forbidden)
}

type entryStopAuthority struct{ c.ProjectStopAuthority }

func newEntryOwnership(t *testing.T) (*ProjectCallOwnership, *entryRegistrationStore) {
	t.Helper()
	store := &entryRegistrationStore{}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: denialSession{}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(store, a, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	structure, err := c.RegisterWorkEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := c.RegisterTaskEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	blockers, err := c.RegisterTaskBlockerEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	sprints, err := c.RegisterSprintLifecycleEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	deps := ProjectCallDependencies{
		Structure:   Dependencies{Authority: a, Events: &denialEvents{}, WorkEvents: structure, Activity: &denialActivity{}},
		Tasks:       TaskDependencies{Authority: a, Structure: reader, Pending: &denialTaskPending{}, Events: &denialEvents{}, TaskEvents: tasks, Activity: &denialActivity{}},
		Transitions: TaskTransitionDependencies{Authority: a, Structure: reader, Pending: &denialTransitionPending{}, Occupancy: &denialTransitionOccupancy{}, Agents: &denialTransitionAgents{}, Events: &denialEvents{}, TaskEvents: transitions, Activity: &denialActivity{}},
		Blockers:    BlockerDependencies{Authority: a, Structure: reader, Events: &denialEvents{}, BlockerEvents: blockers, Activity: &denialActivity{}},
		Sprints:     SprintLifecycleDependencies{Authority: a, Projects: projects, Events: &denialEvents{}, SprintEvents: sprints, Activity: &denialActivity{}},
	}
	pair, err := newProjectCallOwnership(store, deps, entryProcessProof{}, pureID[oc.Process](t, 80), &entryStopAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	return pair, store
}

type entryInvocation struct {
	name    string
	lane    projectCallLane
	class   projectCallClass
	project c.ProjectID
	run     func(context.Context) error
}

func entryInvocations(t *testing.T, pair *ProjectCallOwnership) []entryInvocation {
	t.Helper()
	sr, actor, _ := pureRecord(t)
	tr, _, _ := pureTaskRecord(t)
	br, _, _ := pureBlockerRecord(t, false)
	xr, _, _ := pureTransitionRecord(t)
	sp, _, _ := pureSprintStart(t)
	return []entryInvocation{
		{"structure-write", projectCallStructure, projectCallMutation, sr.Project, func(ctx context.Context) error {
			_, e := pair.Service().CreateMilestone(ctx, actor, f.CommandMeta{IdempotencyKey: sr.Key}, sr.Project, *sr.Input.CreateMilestone)
			return e
		}},
		{"structure-read", projectCallStructure, projectCallRead, sr.Project, func(ctx context.Context) error {
			_, e := pair.Service().LookupCommand(ctx, actor, c.CommandLookupRequest{ProjectID: sr.Project, Command: sr.Command, Key: sr.Key, Semantic: sr.Semantic})
			return e
		}},
		{"task-write", projectCallTask, projectCallMutation, tr.Project, func(ctx context.Context) error {
			_, e := pair.TaskService().CreateTask(ctx, actor, f.CommandMeta{IdempotencyKey: tr.Key}, tr.Project, *tr.Input.Create)
			return e
		}},
		{"task-read", projectCallTask, projectCallRead, tr.Project, func(ctx context.Context) error {
			_, e := pair.TaskService().LookupTaskCommand(ctx, actor, c.TaskCommandLookupRequest{ProjectID: tr.Project, Command: tr.Command, IdempotencyKey: tr.Key, SemanticDigest: tr.Semantic})
			return e
		}},
		{"blocker-write", projectCallBlocker, projectCallMutation, br.Input.Project, func(ctx context.Context) error {
			_, e := pair.BlockerService().AddTaskBlocker(ctx, actor, f.CommandMeta{IdempotencyKey: br.Key, ExpectedVersion: &br.Input.Expected}, br.Input.Project, br.Input.Target, *br.Input.Add)
			return e
		}},
		{"blocker-read", projectCallBlocker, projectCallRead, br.Input.Project, func(ctx context.Context) error {
			_, e := pair.BlockerService().LookupTaskBlockerCommand(ctx, actor, c.TaskBlockerCommandLookupRequest{ProjectID: br.Input.Project, Command: br.Input.Command, IdempotencyKey: br.Key, SemanticDigest: br.Semantic})
			return e
		}},
		{"blocker-list", projectCallBlocker, projectCallRead, br.Input.Project, func(ctx context.Context) error {
			_, e := pair.BlockerService().ListTaskBlockers(ctx, actor, br.Input.Project, br.Input.Target, c.TaskBlockersUnresolved)
			return e
		}},
		{"transition-prepare", projectCallTransition, projectCallMutation, xr.Input.Project, func(ctx context.Context) error {
			_, e := pair.TaskTransitionService().PrepareTaskTransition(ctx, actor, f.CommandMeta{IdempotencyKey: xr.Key, ExpectedVersion: &xr.Input.Expected}, xr.Input.Project, xr.Input.Task, xr.Input.Request)
			return e
		}},
		{"transition-write", projectCallTransition, projectCallMutation, xr.Input.Project, func(ctx context.Context) error {
			_, e := pair.TaskTransitionService().TransferTask(ctx, actor, f.CommandMeta{IdempotencyKey: xr.Key, ExpectedVersion: &xr.Input.Expected}, xr.Input.Project, xr.Input.Task, xr.Input.Request)
			return e
		}},
		{"transition-read", projectCallTransition, projectCallRead, xr.Input.Project, func(ctx context.Context) error {
			_, e := pair.TaskTransitionService().LookupTaskTransition(ctx, actor, c.TaskTransitionLookupRequest{ProjectID: xr.Input.Project, Command: c.TaskTransitionTransfer, IdempotencyKey: xr.Key, SemanticDigest: xr.Semantic})
			return e
		}},
		{"sprint-write", projectCallSprint, projectCallMutation, sp.Input.Project, func(ctx context.Context) error {
			_, e := pair.SprintLifecycleService().StartSprint(ctx, actor, f.CommandMeta{IdempotencyKey: sp.Key, ExpectedVersion: &sp.Input.Expected}, sp.Input.Project, sp.Input.Sprint)
			return e
		}},
		{"sprint-read", projectCallSprint, projectCallRead, sp.Input.Project, func(ctx context.Context) error {
			_, e := pair.SprintLifecycleService().LookupStartSprint(ctx, actor, c.SprintStartLookupRequest{ProjectID: sp.Input.Project, Key: sp.Key, Semantic: sp.Semantic})
			return e
		}},
	}
}

func TestWorkProjectEntriesRegisterBeforeBusiness(t *testing.T) {
	// Every route uses legal existing request fixtures and reaches the same
	// rejected registration; no later business transaction may start.
	for index := range 12 {
		pair, store := newEntryOwnership(t)
		v := entryInvocations(t, pair)[index]
		t.Run(v.name, func(t *testing.T) {
			pureCode(t, v.run(context.Background()), f.Forbidden)
			if store.calls != 1 || len(store.records) != 1 || store.touches.Load() != 0 {
				t.Fatal("entry skipped registration or reached business", store.calls)
			}
			r := store.records[0]
			if !r.valid() || r.Project != v.project || r.Lane != v.lane || r.Class != v.class {
				t.Fatal("entry changed Project/lane/class")
			}
			if len(pair.state.owners) != 0 {
				t.Fatal("known registration failure retained returned call")
			}
			pair.Stop()
			if !pair.Joined() || pair.Drain(context.Background()) != nil {
				t.Fatal("original entry did not finish")
			}
		})
	}
}

func TestWorkProjectEntryPreparationOriginalReturn(t *testing.T) {
	pair, store := newEntryOwnership(t)
	v := entryInvocations(t, pair)[7]
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	store.beforeReturn = func(context.Context) { close(entered); <-release }
	returned := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("original Prepare call did not return")
			}
		}
	})
	go func() { returned <- v.run(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("planned-command preparation was not registered")
	}
	pair.Stop()
	if pair.Joined() || len(pair.state.owners) != 1 || store.records[0].Class != projectCallMutation {
		t.Fatal("Stop skipped original registration return")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(pair.TaskTransitionService().Drain(ctx), context.Canceled) {
		t.Fatal("cancel substituted for preparation return")
	}
	select {
	case <-returned:
		joined = true
		t.Fatal("Prepare returned while original Store held")
	default:
	}
	unblock()
	select {
	case err := <-returned:
		joined = true
		pureCode(t, err, f.Forbidden)
	case <-time.After(5 * time.Second):
		t.Fatal("Prepare did not join")
	}
	if !pair.Joined() || len(pair.state.transitions.state().calls) != 0 || store.calls != 1 {
		t.Fatal("known original return left a caller")
	}
}

func TestWorkProjectEntryBorrowedTransitionRemainsClosed(t *testing.T) {
	for _, owned := range []bool{false, true} {
		name := "legacy"
		if owned {
			name = "owned"
		}
		t.Run(name, func(t *testing.T) {
			pair, store := newEntryOwnership(t)
			service := pair.TaskTransitionService()
			if !owned {
				service.state().ownership = nil
			}
			_, err := service.TransferTaskInTx(context.Background(), f.NewTx(), pureActor(t, 2), nil)
			want := f.Forbidden
			if owned {
				want = f.DependencyUnbound
			}
			pureCode(t, err, want)
			if store.calls != 0 || store.touches.Load() != 0 || len(service.state().calls) != 0 {
				t.Fatal("borrowed Tx invented an owned outer return")
			}
		})
	}
}

func TestWorkProjectEntryLateConfirmations(t *testing.T) {
	for _, lane := range []projectCallLane{projectCallStructure, projectCallTask, projectCallBlocker, projectCallTransition, projectCallSprint} {
		t.Run(string(lane), func(t *testing.T) {
			pair, store := newEntryOwnership(t)
			actor, p := pureActor(t, 2), pureID[i.Project](t, 5)
			var begin func(context.Context) (context.Context, *call, func(), error)
			switch lane {
			case projectCallStructure:
				begin = pair.Service().begin
			case projectCallTask:
				begin = pair.TaskService().begin
			case projectCallBlocker:
				begin = pair.BlockerService().begin
			case projectCallTransition:
				begin = pair.TaskTransitionService().begin
			case projectCallSprint:
				begin = pair.SprintLifecycleService().base.begin
			}
			ctx, entry, done, err := begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer done()
			// Seed only the original private call's stop mark, not an authorization
			// or SQL success. The real late confirmation must register its token.
			o := &projectCallOwner{record: projectCallRecord{ID: pureID[projectCallID](t, 90), Project: p, Process: pair.state.process, Instance: pair.state.instance, Lane: lane, Class: projectCallMutation, Phase: "running"}, entry: entry, ctx: ctx, registration: f.NotCommittedResult(fault(f.Forbidden)), stopping: true, confirmations: map[*confirmation]struct{}{}}
			entry.owner = o
			pair.state.owners[o.record.ID] = o
			ctx = context.WithValue(ctx, projectCallContextKey{}, projectCallWitness{pair.state, o})
			store.beforeReturn = func(confirm context.Context) {
				if !errors.Is(confirm.Err(), context.Canceled) {
					t.Error("late confirmation escaped Project stop")
				}
			}
			cause, e := f.NewJobCause("work-test", p.String(), pureID[struct{}](t, 91).String())
			if e != nil {
				t.Fatal(e)
			}
			original := f.UnknownResult(pureID[f.TransactionAttempt](t, 92), cause)
			semantic := digest([]byte("entry confirmation"))
			switch lane {
			case projectCallStructure:
				_, err = pair.Service().confirmUnknown(ctx, entry, actor, c.CommandLookupRequest{ProjectID: p, Command: c.MilestoneCreate, Key: "key", Semantic: semantic}, original)
			case projectCallTask:
				_, err = pair.TaskService().confirmTaskUnknown(ctx, entry, actor, c.TaskCommandLookupRequest{ProjectID: p, Command: taskCreate, IdempotencyKey: "key", SemanticDigest: semantic}, original)
			case projectCallBlocker:
				_, err = pair.BlockerService().confirmBlockerUnknown(ctx, entry, actor, c.TaskBlockerCommandLookupRequest{ProjectID: p, Command: c.TaskBlockerCommandAdd, IdempotencyKey: "key", SemanticDigest: semantic}, original)
			case projectCallTransition:
				_, err = pair.TaskTransitionService().confirmTransitionUnknown(ctx, entry, actor, c.TaskTransitionLookupRequest{ProjectID: p, Command: c.TaskTransitionTransfer, IdempotencyKey: "key", SemanticDigest: semantic}, original)
			case projectCallSprint:
				_, err = pair.SprintLifecycleService().confirm(ctx, entry, actor, c.SprintStartLookupRequest{ProjectID: p, Key: "key", Semantic: semantic}, original)
			}
			var failure commitFailure
			if !errors.As(err, &failure) || failure.result.AttemptID() != original.AttemptID() {
				t.Fatal("confirmation replaced original writer Unknown", err)
			}
			if store.calls != 1 || len(entry.confirmations) != 0 || len(o.confirmations) != 0 {
				t.Fatal("confirmation skipped actual call or leaked token")
			}
			done()
			if len(pair.state.owners) != 0 {
				t.Fatal("controlled rejected owner not retired by original done")
			}
		})
	}
}

func TestWorkProjectEntrySprintDrainUsesOwnLane(t *testing.T) {
	pair, store := newEntryOwnership(t)
	entry := &call{}
	o := &projectCallOwner{record: projectCallRecord{ID: pureID[projectCallID](t, 95), Lane: projectCallStructure}, entry: entry, registration: f.NotCommittedResult(fault(f.Forbidden))}
	entry.owner = o
	pair.state.owners[o.record.ID] = o
	t.Cleanup(func() { pair.state.returned(entry) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pair.SprintLifecycleService().Drain(ctx); err != nil {
		t.Fatal("Sprint drain borrowed Structure retained lane", err)
	}
	if err := pair.Service().Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("Structure drain ignored its actual retained lane", err)
	}
	pair.state.returned(entry)
	if len(pair.state.owners) != 0 || store.calls != 0 {
		t.Fatal("lane control performed SQL or leaked fixture owner")
	}
}
