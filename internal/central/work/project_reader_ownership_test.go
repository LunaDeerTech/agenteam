package work

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func readerPairDependencies(pair *ProjectCallOwnership) ProjectCallDependencies {
	s := pair.state
	return ProjectCallDependencies{
		Structure: s.structure.state().deps, Tasks: s.tasks.state().deps,
		Transitions: s.transitions.state().deps, Blockers: s.blockers.state().deps,
		Sprints: SprintLifecycleDependencies{Authority: s.sprints.base.state().deps.Authority, Projects: s.sprints.projects, Events: s.sprints.base.state().deps.Events, SprintEvents: s.sprints.events, Activity: s.sprints.base.state().deps.Activity},
	}
}
func newReaderEntryPair(t *testing.T) (*ProjectCallOwnership, *entryRegistrationStore) {
	t.Helper()
	template, store := newEntryOwnership(t)
	pair, err := newProjectCallOwnershipWithReaders(store, readerPairDependencies(template), pureKeys(t), entryProcessProof{}, template.state.process, &entryStopAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	return pair, store
}
func readerInvocations(t *testing.T, pair *ProjectCallOwnership, p i.ProjectID) []entryInvocation {
	t.Helper()
	a := pureActor(t, 2)
	m, s, task := pureID[c.Milestone](t, 11), pureID[pc.Sprint](t, 12), pureID[c.Task](t, 13)
	page := f.DefaultPageRequest()
	return []entryInvocation{
		{"get-milestone", projectCallStructure, projectCallRead, p, func(ctx context.Context) error { _, err := pair.Reader().GetMilestone(ctx, a, p, m); return err }},
		{"get-sprint", projectCallStructure, projectCallRead, p, func(ctx context.Context) error { _, err := pair.Reader().GetSprint(ctx, a, p, s); return err }},
		{"list-milestones", projectCallStructure, projectCallRead, p, func(ctx context.Context) error { _, err := pair.Reader().ListMilestones(ctx, a, p, page); return err }},
		{"list-sprints", projectCallStructure, projectCallRead, p, func(ctx context.Context) error { _, err := pair.Reader().ListSprints(ctx, a, p, m, page); return err }},
		{"get-task", projectCallTask, projectCallRead, p, func(ctx context.Context) error { _, err := pair.TaskReader().GetTask(ctx, a, p, task); return err }},
		{"list-tasks", projectCallTask, projectCallRead, p, func(ctx context.Context) error {
			_, err := pair.TaskReader().ListTasks(ctx, a, p, c.TaskFilter{}, page)
			return err
		}},
		{"task-timeline", projectCallTask, projectCallRead, p, func(ctx context.Context) error {
			_, err := pair.TaskReader().ListTaskEvents(ctx, a, p, task, c.TaskTimelineFilter{}, page)
			return err
		}},
		{"blocker-page", projectCallBlocker, projectCallRead, p, func(ctx context.Context) error {
			_, err := pair.BlockerReader().ListTaskBlockersPage(ctx, a, p, task, c.TaskBlockersAll, page)
			return err
		}},
	}
}

func TestWorkProjectReaderConstructionIsolation(t *testing.T) {
	legacy, store := newEntryOwnership(t)
	deps := readerPairDependencies(legacy)
	original := deps.Tasks.Structure
	pair, err := newProjectCallOwnershipWithReaders(store, deps, pureKeys(t), entryProcessProof{}, legacy.state.process, &entryStopAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := newProjectCallOwnershipWithReaders(store, deps, pureKeys(t), entryProcessProof{}, legacy.state.process, &entryStopAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Reader() != nil || legacy.TaskReader() != nil || legacy.BlockerReader() != nil || original.state().ownership != nil || legacy.state.tasks.state().deps.Structure != original {
		t.Fatal("legacy graph was silently enrolled or changed")
	}
	if pair.Reader() == original || pair.Reader() == second.Reader() || pair.TaskReader() == second.TaskReader() || pair.BlockerReader() == second.BlockerReader() || pair.state.instance == second.state.instance {
		t.Fatal("separate bundles share reader identity")
	}
	if pair.Reader().state().ownership != pair.state || pair.TaskReader().state().ownership != pair.state || pair.BlockerReader().state().ownership != pair.state || pair.TaskReader().state().structure != pair.Reader() || pair.state.tasks.state().deps.Structure != pair.Reader() || pair.state.blockers.state().deps.Structure != pair.Reader() || pair.state.transitions.state().deps.Structure != pair.Reader() {
		t.Fatal("public and borrowed readers are different graphs")
	}
	_, err = newProjectCallOwnershipWithReaders(store, deps, cursor.Keyring{}, entryProcessProof{}, legacy.state.process, &entryStopAuthority{})
	pureCode(t, err, f.InvalidArgument)
	_, err = newProjectCallOwnershipWithReaders(&entryRegistrationStore{}, deps, pureKeys(t), entryProcessProof{}, legacy.state.process, &entryStopAuthority{})
	pureCode(t, err, f.DependencyUnbound)
	var nilPair *ProjectCallOwnership
	if nilPair.Reader() != nil || nilPair.TaskReader() != nil || nilPair.BlockerReader() != nil || store.calls != 0 {
		t.Fatal("construction performed I/O or nil getter exposed a reader")
	}
	pair.Stop()
	for _, invocation := range readerInvocations(t, pair, pureID[i.Project](t, 5)) {
		pureCode(t, invocation.run(context.Background()), f.ShuttingDown)
	}
	if !pair.Joined() || store.calls != 0 {
		t.Fatal("global stop missed a public reader")
	}
	pureCode(t, readerInvocations(t, second, pureID[i.Project](t, 6))[0].run(context.Background()), f.Forbidden)
	if store.calls != 1 {
		t.Fatal("stopping one bundle stopped the second")
	}
}

func TestWorkProjectReaderEntriesRegisterBeforeBusiness(t *testing.T) {
	for index := range 8 {
		pair, store := newReaderEntryPair(t)
		invocation := readerInvocations(t, pair, pureID[i.Project](t, 5))[index]
		t.Run(invocation.name, func(t *testing.T) {
			pureCode(t, invocation.run(context.Background()), f.Forbidden)
			if store.calls != 1 || len(store.records) != 1 || store.touches.Load() != 0 {
				t.Fatal("read reached business before registration")
			}
			r := store.records[0]
			if r.Project != invocation.project || r.Lane != invocation.lane || r.Class != projectCallRead || r.Process != pair.state.process || r.Instance != pair.state.instance {
				t.Fatal("reader registered a different project/lane/instance")
			}
			pair.Stop()
			if !pair.Joined() {
				t.Fatal("known rejected registration retained a returned reader")
			}
		})
	}
}

// Reuse the existing controlled technical-row and Task-row adapters. These
// tests execute real public readers and their SQL callbacks; Project grants,
// database commits and the stop loan are controlled, not real PG evidence.
// Rows-based pagination remains covered by the real integration test.
type readerOwnerStore struct {
	*callOwnerStore
	tasks         map[string]c.Task
	businessReads atomic.Int64
}

func (s *readerOwnerStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.callOwnerStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *readerOwnerStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "agenteam_work.project_calls") {
		return s.callOwnerStore.QueryRow(ctx, query, args...)
	}
	s.businessReads.Add(1)
	if len(args) == 0 {
		return titleUpdateRowError{fault(f.Forbidden)}
	}
	p, ok := args[0].(string)
	if !ok {
		return titleUpdateRowError{fault(f.Forbidden)}
	}
	task, ok := s.tasks[p]
	if !ok {
		return titleUpdateRowError{fault(f.NotFound)}
	}
	return (&titleUpdateStore{task: task, generation: 7}).QueryRow(ctx, query, args...)
}

type readerOwnerProjects struct {
	pc.ProjectAuthority
	store *readerOwnerStore
	actor i.Actor
	refs  map[i.ProjectID]pc.ProjectRef
}

func (p *readerOwnerProjects) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, id i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(id, f.Shared)}); err != nil {
		return pc.ProjectAccess{}, err
	}
	ref, ok := p.refs[id]
	if !ok || !actor.Equal(p.actor) || intent != i.Read || ref.Lifecycle == pc.Deleting {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	return pc.NewProjectAccess(actor, ref, ref.UpdatedAt)
}

type readerOwnerFixture struct {
	*callOwnerFixture
	db       *readerOwnerStore
	projects *readerOwnerProjects
}

func newReaderOwnerFixture(t *testing.T) *readerOwnerFixture {
	t.Helper()
	template, _ := newEntryOwnership(t)
	_, business, oldProjects, _, actor, _ := titleUpdateFixture(t)
	technical := &callOwnerStore{live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}}
	taskB := business.task.Clone()
	taskB.ProjectID, taskB.ID = pureID[i.Project](t, 66), pureID[c.Task](t, 67)
	store := &readerOwnerStore{callOwnerStore: technical, tasks: map[string]c.Task{business.task.ProjectID.String(): business.task, taskB.ProjectID.String(): taskB}}
	bRef := oldProjects.ref
	bRef.ID = taskB.ProjectID
	projects := &readerOwnerProjects{store: store, actor: actor, refs: map[i.ProjectID]pc.ProjectRef{oldProjects.ref.ID: oldProjects.ref, bRef.ID: bRef}}
	authority, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(store, authority, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	deps := readerPairDependencies(template)
	deps.Structure.Authority, deps.Tasks.Authority, deps.Transitions.Authority, deps.Blockers.Authority, deps.Sprints.Authority = authority, authority, authority, authority, authority
	deps.Tasks.Structure, deps.Transitions.Structure, deps.Blockers.Structure = reader, reader, reader
	proof := &callOwnerProof{store: technical}
	pair, err := newProjectCallOwnershipWithReaders(store, deps, pureKeys(t), proof, template.state.process, &callOwnerAuthority{store: technical})
	if err != nil {
		t.Fatal(err)
	}
	v := &readerOwnerFixture{callOwnerFixture: &callOwnerFixture{pair: pair, store: technical, proof: proof, actor: actor, p: business.task.ProjectID, b: taskB.ProjectID, cause: pc.LifecycleCause{OperationID: pureID[pc.Operation](t, 70), Action: pc.Delete, ProjectVersion: 2}}, db: store, projects: projects}
	t.Cleanup(func() {
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pair.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	return v
}
func (v *readerOwnerFixture) readTask(ctx context.Context, p i.ProjectID) error {
	expected := v.db.tasks[p.String()]
	got, err := v.pair.TaskReader().GetTask(ctx, v.actor, p, expected.ID)
	if err == nil && !sameValue(got, expected) {
		return errors.New("public read returned different task")
	}
	return err
}
func readerWaitError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not return")
		return nil
	}
}

func readerOriginalOwner(pair *ProjectCallOwnership, ctx context.Context, cause f.TransactionCause) *projectCallOwner {
	if w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); ok && w.state == pair.state {
		return w.owner
	}
	// The technical close intentionally uses the original call's uncancelled
	// context, not its business witness. Match its exact original recovery ID.
	if cause.Details().Owner != "work-call-return" {
		return nil
	}
	id, err := f.ParseID[projectCallID](cause.Details().RecoveryRunID)
	if err != nil {
		return nil
	}
	pair.state.mu.Lock()
	defer pair.state.mu.Unlock()
	return pair.state.owners[id]
}

func TestWorkProjectReaderOriginalReturnAndSelection(t *testing.T) {
	for _, stage := range []string{"work.task.read", "work-call-return"} {
		t.Run(stage, func(t *testing.T) {
			v := newReaderOwnerFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			returned := make(chan error, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					_ = readerWaitError(t, returned)
				}
				v.store.after = nil
			})
			var owner *projectCallOwner
			var original context.Context
			var held atomic.Bool
			v.store.after = func(ctx context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				o := readerOriginalOwner(v.pair, ctx, cause)
				if o != nil && o.record.Project == v.p && cause.Details().Owner == stage && held.CompareAndSwap(false, true) {
					if result.State() != f.Committed {
						t.Error("read/checkpoint did not reach committed callback")
					}
					owner, original = o, o.ctx
					close(entered)
					<-release
				}
				return result
			}
			go func() { returned <- v.readTask(context.Background(), v.p) }()
			waitCall(t, entered, "public reader missed original Store tail")
			v.cause.Action = pc.Archive
			r, err := v.stop(context.Background(), v.p, true)
			requireCallStop(t, r, err, pc.Stopped)
			if original.Err() != nil {
				t.Fatal("Archive cancelled an original read")
			}
			if err = v.readTask(context.Background(), v.b); err != nil {
				t.Fatal("B read failed while A held", err)
			}
			v.cause.Action = pc.Delete
			r, err = v.stop(context.Background(), v.p, true)
			requireCallStop(t, r, err, pc.StopPending)
			v.pair.state.mu.Lock()
			stillOwned := v.pair.state.owners[owner.record.ID] == owner && !owner.returned
			v.pair.state.mu.Unlock()
			if !stillOwned || original.Err() != context.Canceled {
				t.Fatal("cancel/closed row replaced the original reader return")
			}
			unblock()
			if err = readerWaitError(t, returned); err != nil {
				t.Fatal("already committed read changed", err)
			}
			joined = true
			r, err = v.stop(context.Background(), v.p, false)
			requireCallStop(t, r, err, pc.Stopped)
			v.store.after = nil
			ref := v.projects.refs[v.p]
			ref.Lifecycle = pc.Deleting
			v.projects.refs[v.p] = ref
			before := v.db.businessReads.Load()
			pureCode(t, v.readTask(context.Background(), v.p), f.Forbidden)
			if v.db.businessReads.Load() != before {
				t.Fatal("late deleting read reached business")
			}
		})
	}
}

func TestWorkProjectReaderArchiveRead(t *testing.T) {
	for _, phase := range []pc.Lifecycle{pc.Archiving, pc.Archived} {
		t.Run(string(phase), func(t *testing.T) {
			v := newReaderOwnerFixture(t)
			ref := v.projects.refs[v.p]
			ref.Lifecycle = phase
			if phase == pc.Archived {
				at := ref.UpdatedAt
				ref.ArchivedAt = &at
			}
			v.projects.refs[v.p] = ref
			var owner *projectCallOwner
			v.store.after = func(ctx context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == "work-call-register" {
					w := ctx.Value(projectCallContextKey{}).(projectCallWitness)
					owner = w.owner
					v.pair.state.cancelProjectCalls(v.p, false)
					if ctx.Err() != nil {
						t.Error("Archive cancelled a classified read")
					}
				}
				return result
			}
			if err := v.readTask(context.Background(), v.p); err != nil {
				t.Fatal(err)
			}
			if owner == nil || owner.record.Class != projectCallRead || owner.replayActor != nil || !owner.returned || v.db.businessReads.Load() != 2 {
				t.Fatal("archived read became mutation replay or skipped data")
			}
			v.store.after = nil
		})
	}
}

func TestWorkProjectReaderTechnicalUnknown(t *testing.T) {
	for _, stage := range []string{"work-call-register", "work-call-return"} {
		t.Run(stage, func(t *testing.T) {
			v := newReaderOwnerFixture(t)
			var original f.CommitResult
			var owner *projectCallOwner
			v.store.after = func(ctx context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner != stage {
					return result
				}
				if result.State() != f.Committed {
					t.Fatal("Unknown injection preceded the real controlled callback")
				}
				owner = readerOriginalOwner(v.pair, ctx, cause)
				if owner == nil {
					t.Fatal("technical call lost its exact original owner")
				}
				original = f.UnknownResult(pureID[f.TransactionAttempt](t, 91), cause)
				return original
			}
			err := v.readTask(context.Background(), v.p)
			if stage == "work-call-register" {
				requireCallUnknown(t, err, original)
			} else if err != nil {
				t.Fatal(err)
			}
			v.store.after = nil
			if owner == nil || owner.unknown == nil || !owner.returned {
				t.Fatal("public method lost its technical Unknown")
			}
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = v.pair.state.settle(canceled, owner, nil, false)
			requireCallUnknown(t, err, original)
			entered, release := make(chan struct{}), make(chan struct{})
			returned := make(chan error, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					_ = readerWaitError(t, returned)
				}
				v.store.after = nil
			})
			_, _, beforeWrites := v.store.counts()
			beforeBusiness := v.db.businessReads.Load()
			v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				a, b := cause.Details(), original.Cause().Details()
				if a.Kind != b.Kind || a.Owner != b.Owner || a.RecoveryRunID != b.RecoveryRunID || a.CheckpointRef != b.CheckpointRef {
					t.Error("confirmation changed the original cause")
				}
				close(entered)
				<-release
				return result
			}
			go func() {
				r, e := v.stop(context.Background(), v.p, true)
				if e == nil && r.Details().State != pc.StopPending {
					e = errors.New("confirmation advanced stop in the same call")
				}
				returned <- e
			}()
			waitCall(t, entered, "Unknown confirmation did not reach original Store tail")
			v.pair.state.mu.Lock()
			retained := owner.unknown != nil && owner.unknown.AttemptID() == original.AttemptID()
			v.pair.state.mu.Unlock()
			if !retained {
				t.Fatal("confirmation callback replaced original Store return")
			}
			unblock()
			if err = readerWaitError(t, returned); err != nil {
				t.Fatal(err)
			}
			joined = true
			v.store.after = nil
			_, _, afterWrites := v.store.counts()
			if afterWrites != beforeWrites || v.db.businessReads.Load() != beforeBusiness {
				t.Fatal("readonly confirmation wrote or replayed business")
			}
			if stage == "work-call-register" {
				r, e := v.stop(context.Background(), v.p, true)
				requireCallStop(t, r, e, pc.StopPending)
			}
			r, e := v.stop(context.Background(), v.p, false)
			requireCallStop(t, r, e, pc.Stopped)
		})
	}
}

func readerPlacementLocks(actor i.Actor, p i.ProjectID, sprint c.SprintID) []f.LockRequest {
	return []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(p, f.Shared), taskScheduleLock(p, f.Exclusive), sprintLock(sprint.String(), f.Shared)}
}
func TestWorkProjectReaderBorrowedOuterReturn(t *testing.T) {
	v := newReaderOwnerFixture(t)
	// Mint through the original Task call set and registration. The outer Store
	// below is explicitly controlled; real command composition is a PG boundary.
	ctx, done, err := v.pair.state.beginRead(context.Background(), v.actor, v.p, projectCallTask)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	task := v.db.tasks[v.p.String()]
	entered, release := make(chan struct{}), make(chan struct{})
	returned := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			_ = readerWaitError(t, returned)
		}
		v.store.after = nil
	})
	v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "work.reader-borrow-control" {
			close(entered)
			<-release
		}
		return result
	}
	go func() {
		cause, _ := readCause("reader-borrow-control")
		result := v.db.WithinTx(ctx, cause, func(run context.Context, tx f.Tx) error {
			if e := v.db.AcquireAll(run, tx, readerPlacementLocks(v.actor, v.p, task.SprintID)); e != nil {
				return e
			}
			placement, e := v.pair.Reader().ReadPlacementInTx(run, tx, v.actor, v.p, task.SprintID)
			if e != nil {
				return e
			}
			if placement.Sprint.ID != task.SprintID || placement.Milestone.ID != task.MilestoneID {
				return errors.New("borrowed placement changed")
			}
			w := run.Value(projectCallContextKey{}).(projectCallWitness)
			v.pair.state.mu.Lock()
			retained := !w.owner.bodyReturned && !w.owner.returned
			v.pair.state.mu.Unlock()
			if !retained {
				return errors.New("borrowed method retired its caller")
			}
			return nil
		})
		done()
		returned <- txError(result)
	}()
	waitCall(t, entered, "borrowed call missed outer Store tail")
	r, err := v.stop(context.Background(), v.p, true)
	requireCallStop(t, r, err, pc.StopPending)
	unblock()
	if err = readerWaitError(t, returned); err != nil {
		t.Fatal(err)
	}
	joined = true
	v.store.after = nil
	r, err = v.stop(context.Background(), v.p, false)
	requireCallStop(t, r, err, pc.Stopped)
}

func TestWorkProjectReaderBorrowedScopeRejection(t *testing.T) {
	for _, name := range []string{"missing", "other-pair", "other-project", "wrong-lane", "returned", "ended-tx"} {
		t.Run(name, func(t *testing.T) {
			v := newReaderOwnerFixture(t)
			lane := projectCallTask
			if name == "wrong-lane" {
				lane = projectCallStructure
			}
			ctx, done, err := v.pair.state.beginRead(context.Background(), v.actor, v.p, lane)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(done)
			target := v.p
			switch name {
			case "missing":
				ctx = context.Background()
			case "other-pair":
				w := ctx.Value(projectCallContextKey{}).(projectCallWitness)
				w.state = &projectCallOwnershipState{}
				ctx = context.WithValue(ctx, projectCallContextKey{}, w)
			case "other-project":
				target = v.b
			case "returned":
				done()
				ctx = context.WithoutCancel(ctx)
			}
			task := v.db.tasks[target.String()]
			before := v.db.businessReads.Load()
			var tx f.Tx
			cause, _ := readCause("reader-borrow-rejection")
			result := v.db.WithinTx(ctx, cause, func(run context.Context, current f.Tx) error {
				tx = current
				if e := v.db.AcquireAll(run, current, readerPlacementLocks(v.actor, target, task.SprintID)); e != nil {
					return e
				}
				if name == "ended-tx" {
					return nil
				}
				_, e := v.pair.Reader().ReadPlacementInTx(run, current, v.actor, target, task.SprintID)
				if e == nil {
					return nil
				}
				if _, hasErr := v.pair.TaskReader().HasTasksInSprintInTx(run, current, v.actor, target, task.SprintID); hasErr == nil {
					return nil
				}
				return e
			})
			if name == "ended-tx" {
				_, err = v.pair.Reader().ReadPlacementInTx(ctx, tx, v.actor, target, task.SprintID)
				if _, hasErr := v.pair.TaskReader().HasTasksInSprintInTx(ctx, tx, v.actor, target, task.SprintID); hasErr == nil {
					err = nil
				}
			} else {
				err = txError(result)
			}
			if err == nil || v.db.businessReads.Load() != before {
				t.Fatal("unowned borrow read business", name, err)
			}
		})
	}
}
