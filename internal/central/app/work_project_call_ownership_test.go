package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This port controls the original Store return, not a successful database
// transaction. The real unbound ProcessGuard rejects callbacks before SQL.
// Unknown is a controlled port outcome, never evidence of a PostgreSQL commit.
type workOwnedAppStore struct {
	*postgres.Store
	mu                   sync.Mutex
	causes               []f.TransactionCause
	sql                  int
	gate                 string
	entered              chan context.Context
	release              chan struct{}
	unknownFirst, masked bool
	attempt              f.ID[f.TransactionAttempt]
	checkGuard           bool
}

func (s *workOwnedAppStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.causes = append(s.causes, cause)
	owner := cause.Details().Owner
	gate, entered, release, checkGuard := s.gate, s.entered, s.release, s.checkGuard
	unknown := s.unknownFirst && !s.masked && owner == "work-call-register"
	if unknown {
		s.masked = true
	}
	s.mu.Unlock()
	if owner == gate && entered != nil {
		entered <- ctx
		<-release // Even cancellation cannot replace this actual return.
	}
	if unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	if checkGuard && (owner == "work-call-register" || owner == "work-call-return") {
		var fault *f.Fault
		if errors.As(callback(ctx, f.NewTx()), &fault) {
			return f.NotCommittedResult(fault)
		}
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func (*workOwnedAppStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (s *workOwnedAppStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	s.sql++
	s.mu.Unlock()
	return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
}
func (s *workOwnedAppStore) snapshot() ([]f.TransactionCause, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]f.TransactionCause(nil), s.causes...), s.sql
}
func (s *workOwnedAppStore) hold(t *testing.T) func() {
	t.Helper()
	s.mu.Lock()
	s.gate, s.entered, s.release = "work-call-register", make(chan context.Context, 16), make(chan struct{})
	released := s.release
	s.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	return release
}

type workOwnedAppBundle struct {
	owned, local, legacy *workPlanningAssembly
	lifecycle            *project.LifecycleAuthority
	objects              *objectAssembly
	pending              *scheduler.PendingAuthority
	makeOwned            func(*project.LifecycleAuthority, *objectAssembly, *scheduler.PendingAuthority) (*workPlanningAssembly, error)
}

func newWorkOwnedAppBundle(t *testing.T, store *workOwnedAppStore) *workOwnedAppBundle {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := createProjectLifecycleAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsageAuthorityWithLifecycle(store, accounts, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := agent.NewAuthority(store, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := createWorkPlanningAuthority(store, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	events, err := defineWorkPlanningEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	objects := &objectAssembly{process: updateRootID[objc.Process](), guard: &object.ProcessGuard{}}
	journal, err := outbox.New(store, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: authority},
		Projects:  projects.projects, Sessions: accounts, System: accounts, Audit: auditor,
		Processes: outboxProcessAuthority{process: objects.process, guard: objects.guard}, Cursors: cfg.CursorKeyring(),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := scheduler.NewPendingAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	makeOwned := func(life *project.LifecycleAuthority, o *objectAssembly, p *scheduler.PendingAuthority) (*workPlanningAssembly, error) {
		return createOwnedWorkPlanningWithProviders(cfg, store, authority, accounts, journal, events, p, agents, projects.projects, life, o)
	}
	owned, err := makeOwned(lifecycle, objects, pending)
	if err != nil {
		t.Fatal(err)
	}
	local, err := createOwnedWorkPlanningWithAgents(cfg, store, authority, accounts, journal, events, agents, projects.projects, lifecycle, objects)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := createWorkPlanningWithProviders(cfg, store, authority, accounts, journal, events, pending, agents, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	if causes, sql := store.snapshot(); len(causes) != 0 || sql != 0 {
		t.Fatal("construction performed transaction work")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for _, b := range []*workPlanningAssembly{owned, local, legacy} {
			b.StopAdmission()
			// An explicitly Unknown owner stays unresolved with an unbound
			// guard; all test goroutines still have actual return cleanups.
			_ = b.Drain(ctx)
		}
	})
	return &workOwnedAppBundle{owned, local, legacy, lifecycle, objects, pending, makeOwned}
}

type workOwnedAppEntry struct {
	name string
	call func() error
}

// Actual public entries from each published view; business is never reached
// when registration is rejected. The task view exercises a mutation entry.
func workOwnedAppEntries(b *workPlanningAssembly, actor i.Actor, p i.ProjectID) []workOwnedAppEntry {
	ctx := context.Background()
	digest := f.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	return []workOwnedAppEntry{
		{"structure", func() error {
			_, err := b.structure.LookupCommand(ctx, actor, wc.CommandLookupRequest{ProjectID: p, Command: wc.MilestoneCreate, Key: "owned-structure", Semantic: digest})
			return err
		}},
		{"task-mutation", func() error {
			_, err := b.tasks.CreateTask(ctx, actor, f.CommandMeta{RequestID: updateRootID[f.Request](), IdempotencyKey: "owned-task"}, p, wc.TaskCreate{
				TaskID: updateRootID[wc.Task](), SprintID: updateRootID[pc.Sprint](), Title: "Owned task", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Controlled registration",
			})
			return err
		}},
		{"transition", func() error {
			_, err := b.transitions.LookupTaskTransition(ctx, actor, wc.TaskTransitionLookupRequest{ProjectID: p, Command: wc.TaskTransitionTransfer, IdempotencyKey: "owned-transition", SemanticDigest: digest})
			return err
		}},
		{"blocker", func() error {
			_, err := b.blockers.LookupTaskBlockerCommand(ctx, actor, wc.TaskBlockerCommandLookupRequest{ProjectID: p, Command: wc.TaskBlockerCommandAdd, IdempotencyKey: "owned-blocker", SemanticDigest: digest})
			return err
		}},
		{"sprint", func() error {
			_, err := b.sprints.LookupStartSprint(ctx, actor, wc.SprintStartLookupRequest{ProjectID: p, Key: "owned-sprint", Semantic: digest})
			return err
		}},
		{"structure-reader", func() error {
			_, err := b.structureReader.GetMilestone(ctx, actor, p, updateRootID[wc.Milestone]())
			return err
		}},
		{"task-reader", func() error {
			_, err := b.taskReader.GetTask(ctx, actor, p, updateRootID[wc.Task]())
			return err
		}},
		{"blocker-reader", func() error {
			_, err := b.blockerReader.ListTaskBlockersPage(ctx, actor, p, updateRootID[wc.Task](), wc.TaskBlockersAll, f.DefaultPageRequest())
			return err
		}},
	}
}

func TestWorkProjectCallAppConstructionAndLegacyViews(t *testing.T) {
	s := &workOwnedAppStore{checkGuard: true}
	v := newWorkOwnedAppBundle(t, s)
	for _, b := range []*workPlanningAssembly{v.owned, v.local} {
		p := b.ownership
		if p == nil || b.structure != p.Service() || b.tasks != p.TaskService() || b.transitions != p.TaskTransitionService() || b.blockers != p.BlockerService() || b.sprints != p.SprintLifecycleService() || b.structureReader != p.Reader() || b.taskReader != p.TaskReader() || b.blockerReader != p.BlockerReader() || len(b.commands) != 1 || b.commands[0] != p {
			t.Fatal("default graph lost the original pair or a published view")
		}
		root := &accountAssembly{planning: b}
		if works := root.works(); len(works) != 1 || works[0] != b || b.Joined() {
			t.Fatal("construction duplicated or prematurely retired the owner")
		}
	}
	if v.legacy.ownership != nil || len(v.legacy.commands) != 5 {
		t.Fatal("legacy constructor changed its original service ownership")
	}
	foreign, err := createProjectLifecycleAuthority(&workRootStore{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		life    *project.LifecycleAuthority
		objects *objectAssembly
		pending *scheduler.PendingAuthority
	}{
		{"missing-life", nil, v.objects, v.pending},
		{"foreign-life-store", foreign, v.objects, v.pending},
		{"missing-objects", v.lifecycle, nil, v.pending},
		{"missing-guard", v.lifecycle, &objectAssembly{process: v.objects.process}, v.pending},
		{"invalid-process", v.lifecycle, &objectAssembly{guard: v.objects.guard}, v.pending},
		{"missing-pending", v.lifecycle, v.objects, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if b, err := v.makeOwned(tc.life, tc.objects, tc.pending); err == nil || b != nil {
				t.Fatal("incomplete original graph accepted")
			}
		})
	}
	actor, p := variableAppActor(t), updateRootID[i.Project]()
	if workOwnedAppEntries(v.owned, actor, p)[5].call() == nil {
		t.Fatal("concrete unbound guard allowed owned business")
	}
	if workOwnedAppEntries(v.legacy, actor, p)[5].call() == nil {
		t.Fatal("controlled legacy business result was ignored")
	}
	causes, sql := s.snapshot()
	if len(causes) != 2 || sql != 0 || causes[0].Details().Owner != "work-call-register" || causes[1].Details().Owner == "work-call-register" {
		t.Fatal("legacy gained registration or owned entry bypassed its guard")
	}
	for _, b := range []*workPlanningAssembly{v.owned, v.local, v.legacy} {
		if err := b.Drain(context.Background()); err != nil || !b.Joined() {
			t.Fatal("returned known rejection did not join", err)
		}
	}
}

func TestWorkProjectCallAppAllViewsHoldOriginalReturn(t *testing.T) {
	s := &workOwnedAppStore{}
	v := newWorkOwnedAppBundle(t, s)
	release := s.hold(t)
	actor, p := variableAppActor(t), updateRootID[i.Project]()
	entries := workOwnedAppEntries(v.owned, actor, p)
	var done []<-chan error
	var contexts []context.Context
	for _, entry := range entries {
		done = append(done, variableAppAsync(t, release, entry.call))
		contexts = append(contexts, variableAppReceive(t, s.entered))
	}
	provider := &runtimeExternalProbe{}
	root := &accountAssembly{planning: v.owned, projects: provider}
	root.StopAdmission()
	for n, ctx := range contexts {
		if ctx.Err() == nil {
			t.Fatalf("Stop missed original %s context", entries[n].name)
		}
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if root.Force(expired) == nil || root.Joined() || v.owned.Joined() || provider.forced != 0 {
		t.Fatal("Force retired the provider before original Store returns")
	}
	resources := &resources{stopping: true, accountService: root}
	guard := &runtimeGuardProbe{}
	resources.forceObjects(expired, guard)
	if guard.forced != 0 {
		t.Fatal("held Work return released original guard")
	}
	for n, ch := range done {
		select {
		case <-ch:
			t.Fatalf("cancellation replaced actual %s return", entries[n].name)
		default:
		}
		if entries[n].call() == nil {
			t.Fatal("stopped view accepted a fresh call")
		}
	}
	release()
	for _, ch := range done {
		if variableAppReceive(t, ch) == nil {
			t.Fatal("controlled registration rejection became business success")
		}
	}
	if err := root.Drain(context.Background()); err != nil || !root.Joined() || !v.owned.Joined() {
		t.Fatal("actual original returns did not join", err)
	}
	resources.forceObjects(expired, guard)
	if guard.forced != 1 {
		t.Fatal("actual join did not open the original guard gate")
	}
	causes, sql := s.snapshot()
	if len(causes) != 8 || sql != 0 {
		t.Fatal("stopped admission or rejected registration ran business")
	}
	ids := map[string]bool{}
	for _, cause := range causes {
		d := cause.Details()
		if d.Owner != "work-call-register" || d.CheckpointRef != p.String() || d.RecoveryRunID == "" || ids[d.RecoveryRunID] {
			t.Fatal("original registrations lost project or unique call identity")
		}
		ids[d.RecoveryRunID] = true
	}
}

func TestWorkProjectCallAppUnknownKeepsOriginalOwner(t *testing.T) {
	for _, entryIndex := range []int{1, 5} {
		name := "mutation"
		if entryIndex == 5 {
			name = "reader"
		}
		t.Run(name, func(t *testing.T) {
			s := &workOwnedAppStore{unknownFirst: true, attempt: updateRootID[f.TransactionAttempt](), checkGuard: true}
			v := newWorkOwnedAppBundle(t, s)
			entry := workOwnedAppEntries(v.owned, variableAppActor(t), updateRootID[i.Project]())[entryIndex]
			original := variableAppFault(t, entry.call(), f.CommitUnknown)
			if original.CommitState != f.Unknown || original.CauseID != s.attempt.String() {
				t.Fatal("entry replaced its original registration outcome")
			}
			release := s.hold(t)
			provider := &runtimeExternalProbe{}
			root := &accountAssembly{planning: v.owned, projects: provider}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := variableAppAsync(t, release, func() error { return root.Force(ctx) })
			if got := variableAppReceive(t, s.entered); got != ctx {
				t.Fatal("settlement replaced the caller's remaining Force context")
			}
			if root.Joined() || v.owned.Joined() || provider.forced != 0 {
				t.Fatal("held original confirmation released provider")
			}
			resources := &resources{stopping: true, accountService: root}
			guard := &runtimeGuardProbe{}
			resources.forceObjects(ctx, guard)
			if guard.forced != 0 {
				t.Fatal("held original confirmation released guard")
			}
			cancel()
			select {
			case <-done:
				t.Fatal("cancellation replaced actual settlement return")
			default:
			}
			release()
			retained := variableAppFault(t, variableAppReceive(t, done), f.CommitUnknown)
			if retained.CauseID != original.CauseID || retained.CommitState != f.Unknown || root.Joined() || v.owned.Joined() || provider.forced != 0 {
				t.Fatal("failed original confirmation discarded Unknown custody")
			}
			resources.forceObjects(ctx, guard)
			if guard.forced != 0 {
				t.Fatal("unresolved owner released guard after actual return")
			}
			causes, sql := s.snapshot()
			if len(causes) != 2 || sql != 0 || causes[0].Details().Owner != "work-call-register" || !reflect.DeepEqual(causes[1].Details(), causes[0].Details()) {
				t.Fatal("confirmation did not retain the exact original cause")
			}
		})
	}
}

func TestWorkProjectCallAppLateInstallUsesOriginalForce(t *testing.T) {
	s := &workOwnedAppStore{unknownFirst: true, attempt: updateRootID[f.TransactionAttempt](), checkGuard: true}
	v := newWorkOwnedAppBundle(t, s)
	entry := workOwnedAppEntries(v.owned, variableAppActor(t), updateRootID[i.Project]())[5]
	original := variableAppFault(t, entry.call(), f.CommitUnknown)
	root := &accountAssembly{constructing: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := root.Force(ctx); err != nil {
		t.Fatal(err)
	}
	release := s.hold(t)
	provider := &runtimeExternalProbe{}
	done := variableAppAsync(t, release, func() error {
		if root.install(context.Background(), func() { root.planning, root.projects = v.owned, provider }) {
			return errors.New("late Work owner escaped stopped construction")
		}
		return nil
	})
	if got := variableAppReceive(t, s.entered); got != ctx {
		t.Fatal("late installation renewed the original Force context")
	}
	if works := root.works(); len(works) != 2 || works[0] != v.owned || works[1] != provider || root.Joined() || v.owned.Joined() || provider.forced != 0 {
		t.Fatal("late installation lost unique ownership or provider ordering")
	}
	select {
	case <-done:
		t.Fatal("installation escaped its original Store return")
	default:
	}
	cancel()
	release()
	if err := variableAppReceive(t, done); err != nil {
		t.Fatal(err)
	}
	root.constructionDone()
	if root.Joined() || v.owned.Joined() || provider.forced != 0 || root.forced != ctx {
		t.Fatal("late unresolved owner retired or replaced its remaining budget")
	}
	resources := &resources{stopping: true, accountService: root}
	guard := &runtimeGuardProbe{}
	resources.forceObjects(ctx, guard)
	if guard.forced != 0 || original.CauseID != s.attempt.String() || original.CommitState != f.Unknown {
		t.Fatal("late actual return discarded original guard or outcome")
	}
	causes, sql := s.snapshot()
	if len(causes) != 2 || sql != 0 || causes[0].Details().Owner != "work-call-register" || !reflect.DeepEqual(causes[1].Details(), causes[0].Details()) {
		t.Fatal("late installation dispatched new work or changed the original cause")
	}
}
