//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Project membership comes from real Create/Skill publication and Archive
// acceptance. SQL only observes those facts. The manager uses real runners;
// the transparent Task reader below never changes a returned value or grant.
func TestProjectRunnerManager(t *testing.T) {
	t.Run("candidate-pages-and-project-facts", func(t *testing.T) {
		x := newSkillInstallationFixture(t)
		archiveManagerBaseProject(t, x)
		second := createManagerProject(t, x, "manager-empty-enabled")
		current, err := x.creator.GetSchedulerConfig(ctxFor(t), x.base.ownerBrowser.actor, second.ID)
		firstRoundRequire(t, err)
		config := current.Config.Clone()
		config.Enabled = true
		second, err = x.creator.UpdateProject(ctxFor(t), x.base.ownerBrowser.actor,
			meta(t, "manager-empty-enable", &current.Project.Version), second.ID, pc.UpdateProjectRequest{Scheduler: &config})
		firstRoundRequire(t, err)
		uninitialized := createManagerAcceptedProject(t, x)
		directory, err := project.NewSchedulerProjects(x.base.projectAuthority)
		firstRoundRequire(t, err)
		want := []pc.ProjectID{x.project.ID, second.ID}
		sort.Slice(want, func(a, b int) bool { return want[a].String() < want[b].String() })
		request := pc.SchedulerProjectPageRequest{Limit: 1}
		for n, id := range want {
			page, err := directory.ListSchedulerProjects(ctxFor(t), request)
			firstRoundRequire(t, err)
			firstRoundRequire(t, page.ValidateFor(request))
			if !reflect.DeepEqual(page.ProjectIDs, []pc.ProjectID{id}) || page.Through == nil || *page.Through != want[1] || page.Complete != (n == 1) {
				t.Fatal("real candidate keyset omitted paused/no-Sprint membership or included an ineligible Project")
			}
			last := id
			request = pc.SchedulerProjectPageRequest{After: &last, Through: page.Through, Limit: 1}
		}
		var paused, noSprint, pending, nonactive bool
		err = x.base.raw.QueryRow(ctxFor(t), `SELECT
 EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$1 AND initialized_at IS NOT NULL AND lifecycle='active' AND NOT scheduler_enabled AND current_sprint_id IS NULL),
 EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$2 AND initialized_at IS NOT NULL AND lifecycle='active' AND scheduler_enabled AND current_sprint_id IS NULL),
 EXISTS(SELECT 1 FROM agenteam_project.projects p JOIN agenteam_project.creations c ON c.project_id=p.id WHERE p.id=$3 AND p.initialized_at IS NULL AND p.lifecycle='active' AND c.state='accepted'),
 EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$4 AND initialized_at IS NOT NULL AND lifecycle='archiving')`,
			x.project.ID.String(), second.ID.String(), uninitialized.String(), x.base.project.ID.String()).Scan(&paused, &noSprint, &pending, &nonactive)
		firstRoundRequire(t, err)
		if !paused || !noSprint || !pending || !nonactive {
			t.Fatal("candidate fixture did not retain its formally produced initialization/lifecycle/configuration facts")
		}
	})
	t.Run("cross-project-capacity-and-borrowed-executor-join", func(t *testing.T) {
		x := newSchedulerExecutionFixture(t, true)
		c, v := x.round.capture, x.round.capture.v
		p2 := v.agent.p2
		archiveManagerBaseProject(t, p2)
		emptyA := createManagerProject(t, p2, "manager-empty-a")
		emptyB := createManagerProject(t, p2, "manager-empty-b")
		legacy := x.runner(t, false)
		launched, err := legacy.RunTraversal(ctxFor(t))
		firstRoundRequire(t, err)
		if len(launched.Visits) != 1 || launched.Visits[0].Err != nil || len(launched.Executions) != 0 {
			t.Fatal("original real runner did not leave exactly one undelivered association")
		}
		dispatch := launched.Visits[0].Dispatch
		x.bindOriginal(t, dispatch)
		stopSchedulerExecutionRunner(t, legacy)
		setPendingVisitEnabled(t, v, false, "manager-pause")
		x.round.requireWire(t, 0, true)
		x.start(t)

		directory, err := project.NewSchedulerProjects(v.base.projectAuthority)
		firstRoundRequire(t, err)
		reader, err := work.NewSchedulerTaskReader(v.base.tracked, v.authority)
		firstRoundRequire(t, err)
		observed := &managerTaskReads{SchedulerTaskReader: reader, seen: make(map[pc.ProjectID]wc.SchedulerTaskSnapshot)}
		agents, err := agent.NewSchedulerCurrent(v.agent.providers.Agents, v.pending)
		firstRoundRequire(t, err)
		occupancy, err := execution.NewWorkOccupancy(v.base.tracked)
		firstRoundRequire(t, err)
		writer, err := work.NewTaskRelaunch(v.base.tracked, work.TaskRelaunchDependencies{
			Authority: v.authority, Scheduler: v.pending, Agents: agents, Pending: v.pending, Occupancy: occupancy,
		})
		firstRoundRequire(t, err)
		t.Cleanup(func() {
			writer.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := writer.Drain(ctx); err != nil || !writer.Joined() {
				t.Error("original Work relaunch provider did not join", err)
			}
		})
		relaunch, err := scheduler.NewRelaunchCoordinator(c.claims, writer, reader, occupancy, 1)
		firstRoundRequire(t, err)
		t.Cleanup(func() {
			relaunch.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := relaunch.Drain(ctx); err != nil || !relaunch.Joined() {
				t.Error("original relaunch owner did not join", err)
			}
		})
		deps := scheduler.ProjectRunnersDependencies{Projects: directory, Coordinator: c.claims, Visitor: x.visitor, Tasks: observed, Relaunch: relaunch, Executions: x.executor}
		options := scheduler.ProjectRunnersOptions{MaxProjects: 1, ProjectPageSize: 1, ExecutionPageSize: 1,
			DiscoveryInterval: 20 * time.Millisecond, TickInterval: 20 * time.Millisecond, RelaunchSkipCount: 1, LaunchPolicy: c.launchPolicy}
		limited, err := scheduler.NewProjectRunners(deps, options)
		firstRoundRequire(t, err)
		limitedRun := runProjectManager(t, limited)
		awaitProjectManager(t, limited, func(o scheduler.ProjectRunnersObservation) bool {
			return len(o.Projects) == 1 && o.Projects[0].Running && o.Deferred == 2
		})
		limitedRun.stop(t)
		if x.executor.Joined() || c.claims.Joined() || x.visitor.Joined() || relaunch.Joined() {
			t.Fatal("manager capacity shutdown retired a borrowed domain owner")
		}

		options.MaxProjects = 3
		manager, err := scheduler.NewProjectRunners(deps, options)
		firstRoundRequire(t, err)
		run := runProjectManager(t, manager)
		want := []pc.ProjectID{v.base.project.ID, emptyA.ID, emptyB.ID}
		sort.Slice(want, func(a, b int) bool { return want[a].String() < want[b].String() })
		awaitProjectManager(t, manager, func(o scheduler.ProjectRunnersObservation) bool {
			if len(o.Projects) != len(want) || o.Deferred != 0 {
				return false
			}
			for n, p := range o.Projects {
				if p.ProjectID != want[n] || !p.Running || p.Retained || p.Stopping || p.Err != nil {
					return false
				}
			}
			return observed.hasAll(want)
		})
		x.round.requireWire(t, 1, false)
		for _, p := range []pc.ProjectID{emptyA.ID, emptyB.ID} {
			snapshot := observed.snapshot(p)
			if snapshot.ProjectID != p || snapshot.CurrentSprintID != nil || snapshot.Entries == nil || len(snapshot.Entries) != 0 {
				t.Fatal("empty Project did not traverse its real current facts")
			}
		}
		run.stop(t)
		if x.executor.Joined() {
			t.Fatal("manager Stop/Drain joined the borrowed executor")
		}
		select {
		case <-x.runDone:
			t.Fatal("manager Stop ended the original executor Run lifetime")
		default:
		}
		x.round.requireWire(t, 1, false)
		var running bool
		err = v.base.raw.QueryRow(ctxFor(t), `SELECT status='running' AND completed_at IS NULL FROM agenteam_execution.executions WHERE id=$1 AND project_id=$2 AND agent_id=$3`,
			c.created.ID.String(), c.created.ProjectID.String(), c.created.AgentID.String()).Scan(&running)
		firstRoundRequire(t, err)
		if !running {
			t.Fatal("manager shutdown changed the active borrowed Execution")
		}
		x.stop(t)
		x.requireTerminal(t, ec.Cancelled)
		current, err := v.taskReader.GetTask(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, v.task.ID)
		firstRoundRequire(t, err)
		if current.State != wc.TaskStateInProgress || current.AssigneeAgentID == nil || *current.AssigneeAgentID != v.agentID {
			t.Fatal("manager or executor shutdown changed the independently owned Task")
		}
	})
}

func createManagerProject(t *testing.T, x *skillInstallationFixture, name string) pc.ProjectRef {
	t.Helper()
	request := pc.CreateProjectRequest{ProjectID: id[i.Project](t), Name: name, Description: "real manager discovery input"}
	created, err := x.creator.CreateProject(ctxFor(t), x.base.ownerBrowser.actor, meta(t, name, nil), request)
	firstRoundRequire(t, err)
	if created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != request.ProjectID {
		t.Fatal("real Project and Skill initialization did not publish the requested candidate")
	}
	var published bool
	err = x.base.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_skill.initializations i JOIN agenteam_skill.skills s ON s.project_id=i.project_id AND s.creation_id=i.creation_id AND s.id=i.skill_id AND s.revision_id=i.revision_id WHERE i.project_id=$1 AND i.phase='published' AND s.protected AND s.serving)`, request.ProjectID.String()).Scan(&published)
	firstRoundRequire(t, err)
	if !published {
		t.Fatal("candidate has no actual protected Skill publication")
	}
	return *created.Project
}

func archiveManagerBaseProject(t *testing.T, x *skillInstallationFixture) {
	t.Helper()
	// The inherited Account/PV helper's controlled initializer is not a real
	// positive candidate. Formal Archive acceptance makes it ineligible; its
	// unavailable participant registry is never represented as fully joined.
	p, err := x.base.projects.GetProject(ctxFor(t), x.base.ownerBrowser.actor, x.base.project.ID)
	firstRoundRequire(t, err)
	_, err = x.base.projects.BeginArchive(ctxFor(t), x.base.ownerBrowser.actor, meta(t, "manager-archive-base", &p.Version), p.ID)
	firstRoundRequire(t, err)
}

func createManagerAcceptedProject(t *testing.T, x *skillInstallationFixture) pc.ProjectID {
	t.Helper()
	request := pc.CreateProjectRequest{ProjectID: id[i.Project](t), Name: "manager-uninitialized", Description: "cancel after physical acceptance"}
	metadata := meta(t, "manager-accept-only", nil)
	command, err := pc.CommandIdentity(request.ProjectID, pc.CreateCommand, metadata.IdempotencyKey)
	firstRoundRequire(t, err)
	ctx, cancel := context.WithCancel(ctxFor(t))
	defer cancel()
	store := x.base.tracked
	committed := false
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() && result.State() == f.Committed {
			committed = true
			cancel()
		}
	}
	store.mu.Unlock()
	clear := func() { store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
	defer clear()
	created, err := x.creator.CreateProject(ctx, x.base.ownerBrowser.actor, metadata, request)
	clear()
	firstRoundRequire(t, err)
	if !committed || !errors.Is(ctx.Err(), context.Canceled) || created.Validate() != nil || created.State != pc.CreationPending || created.Operation == nil || created.Operation.State != pc.CreationAccepted {
		t.Fatal("original acceptance was not committed before caller cancellation")
	}
	return request.ProjectID
}

type managerTaskReads struct {
	wc.SchedulerTaskReader
	mu   sync.Mutex
	seen map[pc.ProjectID]wc.SchedulerTaskSnapshot
}

func (r *managerTaskReads) SnapshotInTx(ctx context.Context, tx f.Tx, p wc.ProjectID) (wc.SchedulerTaskSnapshot, error) {
	value, err := r.SchedulerTaskReader.SnapshotInTx(ctx, tx, p)
	if err == nil {
		r.mu.Lock()
		r.seen[p] = value.Clone()
		r.mu.Unlock()
	}
	return value, err
}

func (r *managerTaskReads) hasAll(projects []pc.ProjectID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range projects {
		if _, ok := r.seen[p]; !ok {
			return false
		}
	}
	return true
}

func (r *managerTaskReads) snapshot(p pc.ProjectID) wc.SchedulerTaskSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[p].Clone()
}

type managerRunFixture struct {
	manager *scheduler.ProjectRunners
	cancel  context.CancelFunc
	done    chan error
	once    sync.Once
}

func runProjectManager(t *testing.T, manager *scheduler.ProjectRunners) *managerRunFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(ctxFor(t))
	run := &managerRunFixture{manager: manager, cancel: cancel, done: make(chan error, 1)}
	go func() { run.done <- manager.Run(ctx) }()
	t.Cleanup(func() { run.stop(t) })
	return run
}

func (r *managerRunFixture) stop(t *testing.T) {
	t.Helper()
	r.once.Do(func() {
		defer r.cancel()
		r.manager.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.manager.Drain(ctx); err != nil || !r.manager.Joined() {
			t.Error("original Project manager did not join its own runners", err)
		}
		select {
		case err := <-r.done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error("original Project manager returned a retained owner", err)
			}
		case <-ctx.Done():
			t.Error("original Project manager Run did not return after Drain")
		}
	})
}

func awaitProjectManager(t *testing.T, manager *scheduler.ProjectRunners, accept func(scheduler.ProjectRunnersObservation) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctxFor(t), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		observed := manager.Observation()
		if observed.DiscoveryError != nil {
			firstRoundRequire(t, observed.DiscoveryError)
		}
		if observed.Started && !observed.Stopped && accept(observed) {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("manager did not reach the required bounded, real Project observation", observed)
		}
	}
}
