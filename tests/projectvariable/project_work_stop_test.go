//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This explicit factory owns five command lanes. Acceptance resolves the
// fixture manifest only; unavailable participant ports are never invoked.
// No independent Reader, scheduler borrower, foreign death or Work cleanup is
// claimed. Persisted Work history remains after the actual Stop.
func TestProjectWorkStop(t *testing.T) {
	t.Run("five-lanes-upgrade-and-original-return", testProjectWorkStopReturn)
	t.Run("technical-unknown-readonly-confirmation", testProjectWorkStopUnknown)
	t.Run("archive-keeps-original-read-and-replay", testProjectWorkStopArchive)
}

type workStopPG struct {
	skills        *skillsCleanupPG
	base          *variableHTTPFixture
	projects      *project.Service
	authority     *project.Authority
	life          *project.LifecycleAuthority
	deps          work.ProjectCallDependencies
	pair          *work.ProjectCallOwnership
	pairs         []*work.ProjectCallOwnership
	driver        *project.LifecycleStopDriver
	step          func(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
	cause         pc.LifecycleCause
	scope         pc.ScopeRef
	actor         i.Actor
	last          pc.StopReport
	target        pc.ProjectRef
	calls         []*skillsCleanupPGCall
	barriers      []*schedulerStopBarrier
	milestone     wc.CreateMilestoneRequest
	milestoneMeta f.CommandMeta
	lookup        wc.CommandLookupRequest
	receipt       wc.StructureMutation
	bLookup       wc.CommandLookupRequest
	business      string
}
type workStopPGTag struct{}
type workStopHeld struct {
	barrier  *schedulerStopBarrier
	selected atomic.Bool
	held     atomic.Bool
	mu       sync.Mutex
	ctx      context.Context
}

func workStopRequire(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		agentRetirementDiagnostic(t, "work-"+stage, err)
		t.Fatalf("original Work chain failed at %s", stage)
	}
}

func newWorkStopPG(t *testing.T, upgrade bool, withReaders ...bool) *workStopPG {
	t.Helper()
	var base *variableHTTPFixture
	if upgrade {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00079"))
		raw := openStore(t, db.Config(t, nil))
		base = assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
	} else {
		base = newVariableHTTPFixture(t)
	}
	v := &workStopPG{base: base}
	v.skills = newSkillsCleanupPG(t, base)
	v.target = v.skills.p2.project
	agentID := v.skills.disabledAgent(t) // Formal configuration and original owner return.
	entries := v.skills.p2.manifest.Entries()
	entries = append(entries, pc.ParticipantRegistration{Name: pc.WorkParticipant, ContractVersion: 1, OwnerModule: "work", ReferenceKinds: []pc.ReferenceKind{"work-call"}})
	for n := range entries {
		if entries[n].Name == pc.OutboxParticipant || entries[n].Name == pc.AuditParticipant {
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, pc.WorkParticipant)
		}
	}
	manifest, err := pc.NewRequiredManifest(entries)
	workStopRequire(t, "manifest", err)
	v.life, err = project.NewLifecycleAuthority(base.tracked, manifest, nil)
	workStopRequire(t, "lifecycle", err)
	source, err := toolruntime.NewInstallSource(v.skills.installer, v.skills.p2.service)
	workStopRequire(t, "original-install-source", err)
	graph, err := assembly.New(base.tracked, assembly.Options{Accounts: base.accounts, Lifecycle: v.life, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	workStopRequire(t, "real-authorities", err)
	v.authority = graph.Providers().Projects
	wa, err := work.NewAuthority(base.tracked, v.authority)
	workStopRequire(t, "authority", err)
	reader, err := work.NewReader(base.tracked, wa, base.keys)
	workStopRequire(t, "structure-reader", err)
	pending, err := scheduler.NewPendingAuthority(base.tracked)
	workStopRequire(t, "pending-owner", err)
	occupancy, err := execution.NewWorkOccupancy(base.tracked)
	workStopRequire(t, "execution-occupancy", err)
	pointer, err := project.NewSprintLifecycle(v.authority, wa)
	workStopRequire(t, "sprint-project-pointer", err)
	catalog := event.NewCatalog()
	structures, err := wc.RegisterWorkEvents(catalog)
	workStopRequire(t, "structure-events", err)
	tasks, err := wc.RegisterTaskEvents(catalog)
	workStopRequire(t, "task-events", err)
	transitions, err := wc.RegisterTaskTransitionEvents(catalog)
	workStopRequire(t, "transition-events", err)
	blockers, err := wc.RegisterTaskBlockerEvents(catalog)
	workStopRequire(t, "blocker-events", err)
	sprints, err := wc.RegisterSprintLifecycleEvents(catalog)
	workStopRequire(t, "sprint-events", err)
	projects, err := pc.RegisterProjectEvents(catalog)
	workStopRequire(t, "project-events", err)
	auditing, err := audit.New(base.tracked, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: graph.Providers().AuditProjects})
	workStopRequire(t, "audit", err)
	processes := captureProviderProcesses{guard: v.skills.guard}
	box, err := outbox.New(base.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: wa, pc.ProjectProducer: v.authority}, Sessions: base.accounts, System: base.accounts, Projects: v.authority, Audit: auditing, Cursors: base.keys, Processes: processes})
	workStopRequire(t, "outbox", err)
	v.deps = work.ProjectCallDependencies{
		Structure:   work.Dependencies{Authority: wa, Events: box, WorkEvents: structures, Activity: base.accounts},
		Tasks:       work.TaskDependencies{Authority: wa, Structure: reader, Events: box, TaskEvents: tasks, Activity: base.accounts, Pending: pending},
		Transitions: work.TaskTransitionDependencies{Authority: wa, Structure: reader, Events: box, TaskEvents: transitions, Activity: base.accounts, Pending: pending, Occupancy: occupancy, Agents: graph.Providers().Agents},
		Blockers:    work.BlockerDependencies{Authority: wa, Structure: reader, Events: box, BlockerEvents: blockers, Activity: base.accounts},
		Sprints:     work.SprintLifecycleDependencies{Authority: wa, Projects: pointer, Events: box, SprintEvents: sprints, Activity: base.accounts},
	}
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range manifest.Entries() {
		p := &unavailableParticipant{name: entry.Name}
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: p})
		t.Cleanup(func() {
			if p.calls.Load() != 0 {
				t.Error("acceptance-only Registry invoked an unavailable participant")
			}
		})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	workStopRequire(t, "acceptance-registry", err)
	v.projects, err = project.New(base.tracked, project.Dependencies{Authority: v.authority, Activity: base.accounts, Audit: auditing, Events: box, ProjectEvents: projects, Initializer: v.skills.p2.service, Processes: processes, Cursors: base.keys, LifecycleRegistry: registry}, project.DefaultConfig())
	workStopRequire(t, "project-service", err)
	t.Cleanup(func() {
		v.projects.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.projects.Drain(ctx); err != nil {
			t.Error("original Project calls did not return", err)
		}
	})
	if upgrade {
		legacy, err := work.New(base.tracked, v.deps.Structure)
		workStopRequire(t, "legacy79-service", err)
		t.Cleanup(func() { legacy.Stop(); workStopRequire(t, "legacy79-final-drain", legacy.Drain(ctxFor(t))) })
		_, err = legacy.CreateMilestone(ctxFor(t), base.ownerBrowser.actor, meta(t, "legacy79-milestone", nil), v.target.ID, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Original 79 call returned"})
		workStopRequire(t, "legacy79-command", err)
		legacy.Stop()
		workStopRequire(t, "legacy79-original-drain", legacy.Drain(ctxFor(t)))
		before := v.snapshot(t, false)
		for n := 0; n < 2; n++ {
			migrate(t, base.db, migrationPrefix(t, "00080"))
			if v.snapshot(t, false) != before {
				t.Fatal("migration80 or repeat changed original returned business facts")
			}
			var calls int
			workStopRequire(t, "migration80-no-invented-owner", base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.project_calls`).Scan(&calls))
			if calls != 0 {
				t.Fatal("migration manufactured ownership for a legacy call")
			}
		}
	}
	v.pair = v.newPair(t, withReaders...)
	v.driver, err = project.NewLifecycleStopDriver(base.tracked, v.life, processes, func(ctx context.Context, a i.Actor, c pc.LifecycleCause, s pc.ScopeRef) error {
		if c != v.cause || !s.Equal(v.scope) || v.step == nil {
			return errors.New("Work original Stop scope changed")
		}
		v.actor = a
		return v.step(ctx, a, c, s)
	})
	workStopRequire(t, "stop-driver", err)
	t.Cleanup(func() { v.close(t) })
	v.seed(t, agentID)
	return v
}
func (v *workStopPG) newPair(t *testing.T, withReaders ...bool) *work.ProjectCallOwnership {
	t.Helper()
	a, err := project.NewWorkStopAuthority(v.life, v.base.tracked)
	workStopRequire(t, "stop-authority", err)
	var p *work.ProjectCallOwnership
	if len(withReaders) == 1 && withReaders[0] {
		p, err = work.NewProjectCallOwnershipWithReaders(v.base.tracked, v.deps, v.base.keys, v.skills.guard, v.skills.process, a)
	} else {
		p, err = work.NewProjectCallOwnership(v.base.tracked, v.deps, v.skills.guard, v.skills.process, a)
	}
	workStopRequire(t, "owned-factory", err)
	v.pairs = append(v.pairs, p)
	return p
}
func (v *workStopPG) seed(t *testing.T, agentID i.AgentID) {
	t.Helper()
	a, p := v.base.ownerBrowser.actor, v.target.ID
	v.milestone = wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Owned five lanes"}
	v.milestoneMeta = meta(t, "owned-milestone", nil)
	var err error
	v.receipt, err = v.pair.Service().CreateMilestone(ctxFor(t), a, v.milestoneMeta, p, v.milestone)
	workStopRequire(t, "create-milestone", err)
	digest, err := wc.CreateMilestoneDigest(a, v.milestoneMeta, p, v.milestone)
	workStopRequire(t, "milestone-digest", err)
	v.lookup = wc.CommandLookupRequest{ProjectID: p, Command: wc.MilestoneCreate, Key: v.milestoneMeta.IdempotencyKey, Semantic: digest}
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: v.milestone.MilestoneID, Title: "Original Sprint"}
	sr, err := v.pair.Service().CreateSprint(ctxFor(t), a, meta(t, "owned-sprint", nil), p, sprint)
	workStopRequire(t, "create-sprint", err)
	var task [2]wc.Task
	for n := range task {
		r, err := v.pair.TaskService().CreateTask(ctxFor(t), a, meta(t, fmt.Sprintf("owned-task-%d", n), nil), p, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.SprintID, Title: fmt.Sprintf("Owned task %d", n), Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "No Execution is started"})
		workStopRequire(t, "create-task", err)
		if r.Validate() != nil || !r.Changed {
			t.Fatal("actual Task creation receipt invalid")
		}
		task[n] = r.Task
	}
	blocked, err := v.pair.BlockerService().AddTaskBlocker(ctxFor(t), a, meta(t, "owned-blocker", &task[1].Version), p, task[1].ID, wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: "Original human wait", Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}})
	workStopRequire(t, "add-blocker", err)
	if blocked.Validate() != nil {
		t.Fatal("actual blocker receipt invalid")
	}
	moved, err := v.pair.TaskTransitionService().TransferTask(ctxFor(t), a, meta(t, "owned-transition", &task[0].Version), p, task[0].ID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &agentID, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	workStopRequire(t, "transfer-task", err)
	if moved.Validate() != nil || moved.Task.State != wc.TaskStateTodo {
		t.Fatal("actual transition receipt invalid")
	}
	if sr.Sprint == nil {
		t.Fatal("original sprint receipt missing")
	}
	started, err := v.pair.SprintLifecycleService().StartSprint(ctxFor(t), a, meta(t, "owned-start", &sr.Sprint.Version), p, sprint.SprintID)
	workStopRequire(t, "start-sprint", err)
	if started.Validate() != nil {
		t.Fatal("actual Sprint start receipt invalid")
	}
	v.target = started.Project
	br := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Independent B"}
	bm := meta(t, "owned-project-b", nil)
	_, err = v.pair.Service().CreateMilestone(ctxFor(t), a, bm, v.base.project.ID, br)
	workStopRequire(t, "create-project-b-work", err)
	bd, err := wc.CreateMilestoneDigest(a, bm, v.base.project.ID, br)
	workStopRequire(t, "b-digest", err)
	v.bLookup = wc.CommandLookupRequest{ProjectID: v.base.project.ID, Command: wc.MilestoneCreate, Key: bm.IdempotencyKey, Semantic: bd}
	var lanes, active, instances int
	workStopRequire(t, "five-lane-original-ledger", v.base.raw.QueryRow(ctxFor(t), `SELECT count(DISTINCT lane),count(*) FILTER(WHERE phase<>'business_closed'),count(DISTINCT instance_id) FROM agenteam_work.project_calls WHERE project_id=$1`, p.String()).Scan(&lanes, &active, &instances))
	if lanes != 5 || active != 0 || instances != 1 {
		t.Fatal("five original command lanes did not actually return", lanes, active, instances)
	}
	v.business = v.snapshot(t, false)
}
func (v *workStopPG) accept(t *testing.T, action pc.LifecycleAction) {
	t.Helper()
	var err error
	var op pc.LifecycleOperation
	if action == pc.Archive {
		op, err = v.projects.BeginArchive(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "work-owned-archive", &v.target.Version), v.target.ID)
	} else {
		var path string
		v.base.tx(t, ownerLocks(v.base.ownerBrowser.actor, v.target.ID), func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			r, e := v.base.accounts.CurrentUserRouteInTx(ctx, tx, v.base.ownerBrowser.actor)
			if e != nil {
				return e
			}
			path, e = pc.NormalizeProjectPath(r.Username, v.target.Name)
			return e
		})
		var r pc.LifecycleResult
		r, err = v.projects.BeginDeleteProject(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "work-owned-delete", &v.target.Version), v.target.ID, pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true})
		if err == nil {
			if r.Operation == nil {
				t.Fatal("formal Delete operation absent")
			}
			op = *r.Operation
		}
	}
	workStopRequire(t, "formal-lifecycle-acceptance", err)
	v.cause = pc.LifecycleCause{OperationID: op.ID, Action: action, ProjectVersion: op.ProjectVersion}
	v.scope = pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.target.ID}
}
func (v *workStopPG) start(fn func(context.Context) error) *skillsCleanupPGCall {
	c := &skillsCleanupPGCall{done: make(chan struct{})}
	v.calls = append(v.calls, c)
	go func() {
		defer close(c.done)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c.err = fn(ctx)
	}()
	return c
}
func (v *workStopPG) stop(t *testing.T, p *work.ProjectCallOwnership, inspect bool) *skillsCleanupPGCall {
	t.Helper()
	v.step = func(ctx context.Context, a i.Actor, c pc.LifecycleCause, s pc.ScopeRef) error {
		var r pc.StopReport
		var err error
		if inspect {
			r, err = p.InspectProjectStop(ctx, a, c, s)
		} else {
			r, err = p.RequestProjectStop(ctx, a, c, s)
		}
		if err != nil {
			return err
		}
		if !r.Matches(pc.WorkParticipant, c, s) {
			return errors.New("Work stop report changed original identity")
		}
		v.last = r
		return nil
	}
	return v.start(func(ctx context.Context) error { return v.driver.Run(ctx, v.target.ID, v.cause.OperationID) })
}
func (v *workStopPG) observed(t *testing.T, p *work.ProjectCallOwnership, inspect bool, want pc.StopState) {
	t.Helper()
	c := v.stop(t, p, inspect)
	outboxCleanupWait(t, c.done, "original Work Stop return")
	workStopRequire(t, "stop-return", c.err)
	if v.last.Details().State != want {
		t.Fatal("original Work Stop state differs", v.last.Details().State, want)
	}
}
func (v *workStopPG) barrier() *schedulerStopBarrier {
	b := newSchedulerStopBarrier()
	v.barriers = append(v.barriers, b)
	return b
}
func (v *workStopPG) await(t *testing.T, c *skillsCleanupPGCall, b *schedulerStopBarrier) {
	t.Helper()
	select {
	case r := <-b.entered:
		if r.State() != f.Committed {
			t.Fatal("Work original Store did not commit")
		}
	case <-c.done:
		workStopRequire(t, "before-original-return", c.err)
		t.Fatal("selected Work original return was not held")
	case <-time.After(5 * time.Second):
		t.Fatal("Work original return barrier not reached")
	}
}
func (v *workStopPG) notReturned(t *testing.T, c *skillsCleanupPGCall) {
	t.Helper()
	select {
	case <-c.done:
		t.Fatal("original Work call returned while Store was held")
	default:
	}
}
func (v *workStopPG) clear() {
	s := v.base.tracked
	s.mu.Lock()
	s.enter = nil
	s.before = nil
	s.after = nil
	s.afterResult = nil
	s.transformResult = nil
	s.mu.Unlock()
}
func (v *workStopPG) other(t *testing.T) {
	t.Helper()
	r, err := v.pair.Service().LookupCommand(ctxFor(t), v.base.ownerBrowser.actor, v.bLookup)
	workStopRequire(t, "independent-b-read", err)
	if r.Validate() != nil || r.State != wc.LookupCommitted {
		t.Fatal("other Project's original Work call was blocked")
	}
}
func (v *workStopPG) snapshot(t *testing.T, ledger bool) string {
	t.Helper()
	tables := []string{"milestones", "sprints", "milestone_order_groups", "sprint_order_groups", "structure_commands", "tasks", "task_order_groups", "task_query_generations", "task_commands", "task_events", "task_transition_commands", "task_blockers", "task_blocker_commands", "sprint_start_commands"}
	if ledger {
		tables = append(tables, "project_calls")
	}
	var parts []string
	for _, table := range tables {
		parts = append(parts, fmt.Sprintf("(SELECT jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text) FROM agenteam_work.%s x WHERE project_id=$1::uuid)", table))
	}
	var value string
	workStopRequire(t, "business-snapshot", v.base.raw.QueryRow(ctxFor(t), "SELECT jsonb_build_array("+strings.Join(parts, ",")+")::text", v.target.ID.String()).Scan(&value))
	return value
}
func (v *workStopPG) close(t *testing.T) {
	t.Helper()
	for _, b := range v.barriers {
		b.open()
	}
	for _, c := range v.calls {
		outboxCleanupWait(t, c.done, "original Work calls before shutdown")
	}
	v.clear()
	// Drain explicitly settles only original retained technical owners. It does
	// not erase ledger rows or infer a foreign instance returned.
	for _, p := range v.pairs {
		p.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := p.Drain(ctx)
		cancel()
		if err != nil || !p.Joined() {
			t.Error("Work original pair did not join", err)
			_ = p.Drain(context.Background())
		}
	}
	v.driver.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := v.driver.Drain(ctx); err != nil {
		t.Error("original Work Stop driver did not return", err)
		_ = v.driver.Drain(context.Background())
	}
}

func (v *workStopPG) holdBusiness(t *testing.T, read bool, tag string) (*skillsCleanupPGCall, *workStopHeld) {
	t.Helper()
	s := v.base.tracked
	h := &workStopHeld{barrier: v.barrier()}
	request := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Held original mutation"}
	m := meta(t, "held-"+tag, nil)
	identity, err := wc.Identity(v.target.ID, wc.MilestoneCreate, m.IdempotencyKey)
	workStopRequire(t, "held-identity", err)
	if read {
		identity, err = wc.Identity(v.target.ID, v.lookup.Command, v.lookup.Key)
		workStopRequire(t, "held-read-identity", err)
	}
	matches := func(c f.TransactionCause) bool {
		return c.Kind() == f.CommandsCause && c.Details().Primary.Canonical() == identity.Canonical()
	}
	s.mu.Lock()
	s.enter = func(ctx context.Context, c f.TransactionCause) {
		if matches(c) && ctx.Value(workStopPGTag{}) == tag {
			h.mu.Lock()
			h.ctx = ctx
			h.mu.Unlock()
		}
	}
	s.after = func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !matches(c) || ctx.Value(workStopPGTag{}) != tag {
			return nil
		}
		x, e := s.InTx(tx)
		if e != nil {
			return e
		}
		var complete bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.structure_commands WHERE project_id=$1 AND idempotency_key=$2 AND state='completed')`, v.target.ID.String(), string(identity.Key())).Scan(&complete); e != nil {
			return e
		}
		if complete {
			h.selected.Store(true)
		}
		return nil
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if matches(c) && h.selected.Load() && r.State() == f.Committed && h.held.CompareAndSwap(false, true) {
			h.barrier.hold(r)
		}
	}
	s.mu.Unlock()
	c := v.start(func(ctx context.Context) error {
		ctx = context.WithValue(ctx, workStopPGTag{}, tag)
		if read {
			r, e := v.pair.Service().LookupCommand(ctx, v.base.ownerBrowser.actor, v.lookup)
			if e == nil && (r.Validate() != nil || r.State != wc.LookupCommitted) {
				return errors.New("original held read lost its committed receipt")
			}
			return e
		}
		r, e := v.pair.Service().CreateMilestone(ctx, v.base.ownerBrowser.actor, m, v.target.ID, request)
		if e == nil && r.Validate() != nil {
			return errors.New("original held mutation receipt invalid")
		}
		return e
	})
	v.await(t, c, h.barrier)
	h.mu.Lock()
	live := h.ctx != nil && h.ctx.Err() == nil
	h.mu.Unlock()
	if !live {
		t.Fatal("actual owned business context was not live before Stop")
	}
	return c, h
}
func testProjectWorkStopReturn(t *testing.T) {
	v := newWorkStopPG(t, true)
	c, h := v.holdBusiness(t, false, "delete")
	v.accept(t, pc.Delete)
	v.observed(t, v.pair, false, pc.StopPending)
	v.notReturned(t, c)
	v.other(t)
	h.mu.Lock()
	canceled := errors.Is(h.ctx.Err(), context.Canceled)
	h.mu.Unlock()
	if !canceled {
		t.Fatal("Delete failed to cancel the actual mutation context")
	}
	h.barrier.open()
	outboxCleanupWait(t, c.done, "original Work mutation return")
	workStopRequire(t, "held-committed-mutation", c.err)
	v.clear()
	v.observed(t, v.pair, false, pc.Stopped)
	stable := v.snapshot(t, true)
	other := v.newPair(t)
	v.observed(t, other, false, pc.StopPending)
	if v.snapshot(t, true) != stable {
		t.Fatal("another same-process instance rewrote original ledger or history")
	}
	v.other(t)
	t.Log("PROJECT_WORK_STOP_RETURN five_real_command_lanes=true migration79_80_repeat=true legacy_actual_return=true original_store_return=true delete_pending_then_stopped=true other_project_live=true different_instance_pending=true work_history_retained=true no_default_registry=true")
}

type workStopPGUnknown struct {
	original f.CommitResult
	id       string
	owner    string
}

func (v *workStopPG) maskTechnical(t *testing.T, owner string) workStopPGUnknown {
	t.Helper()
	v.clear()
	s := v.base.tracked
	var mu sync.Mutex
	var selected string
	var original f.CommitResult
	var once atomic.Bool
	tag := owner
	s.mu.Lock()
	s.enter = func(ctx context.Context, c f.TransactionCause) {
		d := c.Details()
		if ctx.Value(workStopPGTag{}) == tag && c.Kind() == f.RecoveryCause && d.Owner == owner && d.CheckpointRef == v.target.ID.String() {
			mu.Lock()
			selected = d.RecoveryRunID
			mu.Unlock()
		}
	}
	s.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		mu.Lock()
		match := selected != "" && c.Kind() == f.RecoveryCause && c.Details().Owner == owner && c.Details().RecoveryRunID == selected
		mu.Unlock()
		if match && r.State() == f.Committed && once.CompareAndSwap(false, true) {
			original = f.UnknownResult(id[f.TransactionAttempt](t), c)
			return original
		}
		return r
	}
	s.mu.Unlock()
	c := v.start(func(ctx context.Context) error {
		r, e := v.pair.Service().LookupCommand(context.WithValue(ctx, workStopPGTag{}, tag), v.base.ownerBrowser.actor, v.lookup)
		if e == nil && (r.Validate() != nil || r.State != wc.LookupCommitted) {
			return errors.New("technical mask changed original business lookup")
		}
		return e
	})
	outboxCleanupWait(t, c.done, "original Work technical acknowledgement mask")
	v.clear()
	if !once.Load() || original.State() != f.Unknown {
		t.Fatal("real technical transaction was not selected")
	}
	if owner == "work-call-register" {
		var fault *f.Fault
		if !errors.As(c.err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != original.AttemptID().String() {
			agentRetirementDiagnostic(t, "work-register-unknown", c.err)
			t.Fatal("original registration Unknown identity was lost")
		}
	} else {
		workStopRequire(t, "return-unknown-keeps-business-receipt", c.err)
	}
	return workStopPGUnknown{original: original, id: selected, owner: owner}
}

const workStopWritesSQL = `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname='agenteam_work'`

func (v *workStopPG) confirmTechnical(t *testing.T, u workStopPGUnknown) {
	t.Helper()
	v.clear()
	beforeSnapshot := v.snapshot(t, true)
	s := v.base.tracked
	b := v.barrier()
	var baseline sync.Map
	var seen atomic.Int64
	exact := func(c f.TransactionCause) bool {
		d := c.Details()
		o := u.original.Cause().Details()
		return c.Kind() == f.RecoveryCause && d.Owner == o.Owner && d.RecoveryRunID == o.RecoveryRunID && d.CheckpointRef == o.CheckpointRef
	}
	s.mu.Lock()
	s.before = func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !exact(c) {
			return nil
		}
		x, e := s.InTx(tx)
		if e != nil {
			return e
		}
		var n int64
		if e = x.QueryRow(ctx, workStopWritesSQL).Scan(&n); e != nil {
			return e
		}
		baseline.Store(tx, n)
		return nil
	}
	s.after = func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if !exact(c) {
			return nil
		}
		old, ok := baseline.LoadAndDelete(tx)
		if !ok {
			return errors.New("Work confirmation lost its original Tx baseline")
		}
		x, e := s.InTx(tx)
		if e != nil {
			return e
		}
		var n int64
		if e = x.QueryRow(ctx, workStopWritesSQL).Scan(&n); e != nil {
			return e
		}
		if n-old.(int64) != 0 {
			return errors.New("original technical Unknown confirmation wrote Work facts")
		}
		seen.Add(1)
		return nil
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		if exact(c) && seen.Load() == 1 && r.State() == f.Committed {
			b.hold(r)
		}
	}
	s.mu.Unlock()
	c := v.stop(t, v.pair, false)
	v.await(t, c, b)
	v.notReturned(t, c)
	if seen.Load() != 1 || v.snapshot(t, true) != beforeSnapshot {
		t.Fatal("technical confirmation changed original facts before actual return")
	}
	v.other(t)
	b.open()
	outboxCleanupWait(t, c.done, "original Work read-only technical confirmation")
	workStopRequire(t, "technical-confirmation", c.err)
	if v.last.Details().State != pc.StopPending || seen.Load() != 1 || v.snapshot(t, true) != beforeSnapshot {
		t.Fatal("technical confirmation skipped Pending or changed original facts")
	}
	v.clear()
}
func (v *workStopPG) phase(t *testing.T, id string) string {
	t.Helper()
	var phase, process, instance string
	workStopRequire(t, "original-ledger-phase", v.base.raw.QueryRow(ctxFor(t), `SELECT phase,process_id::text,instance_id::text FROM agenteam_work.project_calls WHERE id=$1 AND project_id=$2`, id, v.target.ID.String()).Scan(&phase, &process, &instance))
	if process != v.skills.process.String() || instance == "" {
		t.Fatal("original technical owner identity changed")
	}
	return phase
}
func testProjectWorkStopUnknown(t *testing.T) {
	v := newWorkStopPG(t, false)
	registration := v.maskTechnical(t, "work-call-register")
	returned := v.maskTechnical(t, "work-call-return")
	if registration.id == returned.id || v.phase(t, registration.id) != "running" || v.phase(t, returned.id) != "business_closed" {
		t.Fatal("original technical outcomes differ")
	}
	v.accept(t, pc.Delete)
	// The owner selects the smallest actual ID; UUIDv7 creation order does
	// not guarantee that order within one millisecond.
	if returned.id < registration.id {
		v.confirmTechnical(t, returned)
	}
	v.confirmTechnical(t, registration) // Original register cause, no new mutation.
	if v.phase(t, registration.id) != "running" {
		t.Fatal("confirmation closed the registration in the same round")
	}
	v.observed(t, v.pair, false, pc.StopPending) // Explicit original close only.
	if v.phase(t, registration.id) != "business_closed" {
		t.Fatal("next explicit Request did not close the confirmed original call")
	}
	if registration.id < returned.id {
		v.confirmTechnical(t, returned)
	}
	v.observed(t, v.pair, false, pc.Stopped)
	if v.snapshot(t, false) != v.business {
		t.Fatal("technical custody confirmation changed original business history")
	}
	v.other(t)
	t.Log("PROJECT_WORK_STOP_UNKNOWN real_register_and_return_commit=true local_ack_masks=true original_attempt_cause=true confirmation_same_tx_zero_writes=true confirmation_actual_return=true next_explicit_close=true business_receipt_preserved=true other_project_live=true no_network_lost_ack=true")
}
func testProjectWorkStopArchive(t *testing.T) {
	v := newWorkStopPG(t, false)
	mutation, mh := v.holdBusiness(t, false, "archive-mutation")
	read, rh := v.holdBusiness(t, true, "archive-read")
	v.accept(t, pc.Archive)
	v.observed(t, v.pair, false, pc.StopPending)
	mh.mu.Lock()
	mutationCanceled := errors.Is(mh.ctx.Err(), context.Canceled)
	mh.mu.Unlock()
	rh.mu.Lock()
	readLive := rh.ctx != nil && rh.ctx.Err() == nil
	rh.mu.Unlock()
	if !mutationCanceled || !readLive {
		t.Fatal("Archive failed to distinguish the actual mutation and read contexts")
	}
	v.notReturned(t, mutation)
	v.notReturned(t, read)
	v.other(t)
	mh.barrier.open()
	outboxCleanupWait(t, mutation.done, "original archived mutation Store return")
	workStopRequire(t, "archive-mutation-return", mutation.err)
	v.observed(t, v.pair, false, pc.Stopped)
	v.notReturned(t, read)
	before := v.snapshot(t, false)
	replay, err := v.pair.Service().CreateMilestone(ctxFor(t), v.base.ownerBrowser.actor, v.milestoneMeta, v.target.ID, v.milestone)
	workStopRequire(t, "archive-original-completed-replay", err)
	if !bytes.Equal(jsonBytes(t, replay), jsonBytes(t, v.receipt)) {
		t.Fatal("Archive replay changed its complete original receipt")
	}
	_, err = v.pair.Service().CreateMilestone(ctxFor(t), v.base.ownerBrowser.actor, meta(t, "archive-new-command", nil), v.target.ID, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "Must remain forbidden"})
	if !outboxCleanupFault(err, f.ProjectNotActive) {
		t.Fatal("Archive allowed a new Work mutation", err)
	}
	if v.snapshot(t, false) != before {
		t.Fatal("Archive replay or rejected new command changed business history")
	}
	rh.mu.Lock()
	readLive = rh.ctx != nil && rh.ctx.Err() == nil
	rh.mu.Unlock()
	if !readLive {
		t.Fatal("Archive or replay canceled the permitted original read")
	}
	rh.barrier.open()
	outboxCleanupWait(t, read.done, "original permitted read Store return")
	workStopRequire(t, "archive-read-return", read.err)
	v.clear()
	v.observed(t, v.pair, false, pc.Stopped)
	v.other(t)
	var reads int
	workStopRequire(t, "archive-read-classification", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.project_calls WHERE project_id=$1 AND lane='structure' AND class='read' AND phase='business_closed'`, v.target.ID.String()).Scan(&reads))
	if reads != 3 {
		t.Fatal("original read, completed replay and rejected new command lacked exact read custody", reads)
	}
	t.Log("PROJECT_WORK_STOP_ARCHIVE mutation_canceled=true original_read_uncanceled=true stopped_waits_only_mutation=true completed_receipt_replay_read=true new_mutation_rejected=true original_read_return=true other_project_live=true no_reader_or_scheduler_owner_claim=true")
}
