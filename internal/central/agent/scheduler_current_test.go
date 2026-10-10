package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type schedulerCurrentIntentControl struct {
	check func(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID) (pc.SchedulerIntent, error)
}

func (p *schedulerCurrentIntentControl) RequireSchedulerCurrentIntentInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID) (pc.SchedulerIntent, error) {
	return p.check(ctx, tx, actor, project, agent)
}

type schedulerCurrentProjectControl struct {
	pc.ProjectAuthority
	check func(context.Context, f.Tx, i.ProjectID) (pc.SchedulerProject, error)
}

func (p *schedulerCurrentProjectControl) RequireSchedulerProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.SchedulerProject, error) {
	return p.check(ctx, tx, project)
}

type schedulerCurrentControl struct {
	reader                   *SchedulerCurrent
	store                    *executionConfigurationStore
	projects                 *schedulerCurrentProjectControl
	intents                  *schedulerCurrentIntentControl
	witness                  pc.SchedulerIntent
	current                  pc.SchedulerProject
	proofErr, projectErr     error
	proofCalls, projectCalls int
}

// These are controlled owner/Store boundaries. The successful gate path reaches
// the real initializedAgent point read and returns NotFound; it does not fake a
// completed Agent creation receipt, Scheduler private witness or PostgreSQL.
func newSchedulerCurrentControl(t *testing.T) *schedulerCurrentControl {
	t.Helper()
	r, _, grant := executionConfigurationFixture(t)
	scope, _ := i.InProject(r.ProjectID)
	registration, _ := i.RegisterService(i.Scheduler)
	dispatch := "01900000-0000-7000-8000-000000000030"
	r.Actor, _ = registration.Actor(dispatch, scope)
	ctx := context.WithValue(context.Background(), struct{}{}, "original-scheduler-call")
	x := &schedulerCurrentControl{}
	x.store = &executionConfigurationStore{t: t, ctx: ctx, tx: f.NewTx(), request: r, locks: schedulerCurrentLocks(r.ProjectID, r.AgentID), live: true}
	sprint := commandID[pc.Sprint](t, "01900000-0000-7000-8000-000000000031")
	x.witness = pc.SchedulerIntent{ProjectID: r.ProjectID, AgentID: r.AgentID, DispatchID: dispatch, SprintID: sprint}
	project := grant.Project()
	project.CurrentSprintID = &sprint
	x.current = pc.SchedulerProject{Project: project, Config: pc.ProjectSchedulerConfig{Enabled: true}}
	x.intents = &schedulerCurrentIntentControl{check: func(got context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID) (pc.SchedulerIntent, error) {
		x.proofCalls++
		if got != x.store.ctx || tx != x.store.tx || !actor.Equal(r.Actor) || project != r.ProjectID || agent != r.AgentID || x.store.reads != 0 {
			t.Fatal("private proof lost original caller identity or preceded lock/read order")
		}
		return x.witness, x.proofErr
	}}
	x.projects = &schedulerCurrentProjectControl{check: func(got context.Context, tx f.Tx, project i.ProjectID) (pc.SchedulerProject, error) {
		x.projectCalls++
		if got != x.store.ctx || tx != x.store.tx || project != r.ProjectID || x.proofCalls == 0 || x.proofErr != nil || x.store.reads != 0 {
			t.Fatal("Project gate bypassed original private intent")
		}
		x.store.providerPassed = x.projectErr == nil
		return x.current.Clone(), x.projectErr
	}}
	authority, err := NewAuthority(x.store, x.projects)
	if err != nil {
		t.Fatal(err)
	}
	x.reader, err = NewSchedulerCurrent(authority, x.intents)
	if err != nil || x.proofCalls != 0 || x.projectCalls != 0 || x.store.reads != 0 {
		t.Fatal("constructor performed I/O", err)
	}
	return x
}

func (x *schedulerCurrentControl) read() (c.AgentRef, error) {
	r := x.store.request
	return x.reader.RequireSchedulerCurrentInTx(x.store.ctx, x.store.tx, r.Actor, r.ProjectID, r.AgentID)
}

func requireZeroSchedulerRef(t *testing.T, ref c.AgentRef, err error, code f.Code) {
	t.Helper()
	if ref != (c.AgentRef{}) {
		t.Fatal("failed Scheduler read returned Agent facts")
	}
	requireCode(t, err, code)
}

func TestAgentSchedulerCurrentRequiresOriginalOwnersAndTransaction(t *testing.T) {
	x := newSchedulerCurrentControl(t)
	var absent *schedulerCurrentIntentControl
	if _, err := NewSchedulerCurrent(x.reader.agents, absent); err == nil {
		t.Fatal("typed nil private proof provider accepted")
	}
	missingProject, err := NewAuthority(x.store, &executionConfigurationProjects{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewSchedulerCurrent(missingProject, x.intents); err == nil {
		t.Fatal("missing same Project owner's Scheduler gate accepted")
	}
	for _, mode := range []string{"foreign-tx", "closed-tx", "missing-lock", "human", "other-service"} {
		t.Run(mode, func(t *testing.T) {
			x := newSchedulerCurrentControl(t)
			r := x.store.request
			tx := x.store.tx
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "closed-tx":
				x.store.live = false
			case "missing-lock":
				x.store.lockErr = fault(f.Forbidden)
			case "human":
				r.Actor, _ = i.NewHuman(commandID[i.User](t, "01900000-0000-7000-8000-000000000002"), commandID[i.Session](t, "01900000-0000-7000-8000-000000000003"))
			case "other-service":
				registration, _ := i.RegisterService(i.ProjectLifecycle)
				scope, _ := i.InProject(r.ProjectID)
				r.Actor, _ = registration.Actor(x.witness.DispatchID, scope)
			}
			ref, err := x.reader.RequireSchedulerCurrentInTx(x.store.ctx, tx, r.Actor, r.ProjectID, r.AgentID)
			requireZeroSchedulerRef(t, ref, err, f.Forbidden)
			if x.proofCalls+x.projectCalls+x.store.reads != 0 {
				t.Fatal("invalid caller reached provider or existence read")
			}
		})
	}
}

func TestAgentSchedulerCurrentProofProjectAndCanonicalOrder(t *testing.T) {
	for _, mode := range []string{"proof-error", "wrong-dispatch", "wrong-agent", "project-error", "paused", "sprint-changed", "inactive", "accepted"} {
		t.Run(mode, func(t *testing.T) {
			x := newSchedulerCurrentControl(t)
			want := f.InvalidState
			switch mode {
			case "proof-error":
				x.proofErr, want = fault(f.Forbidden), f.Forbidden
			case "wrong-dispatch":
				x.witness.DispatchID, want = "01900000-0000-7000-8000-000000000099", f.Forbidden
			case "wrong-agent":
				x.witness.AgentID, want = commandID[i.Agent](t, "01900000-0000-7000-8000-000000000099"), f.Forbidden
			case "project-error":
				x.projectErr, want = fault(f.DependencyUnavailable), f.DependencyUnavailable
			case "paused":
				x.current.Config.Enabled = false
			case "sprint-changed":
				x.current.Project.CurrentSprintID = nil
			case "inactive":
				x.current.Project.Lifecycle = pc.Archiving
			case "accepted":
				want = f.NotFound
			}
			ref, err := x.read()
			requireZeroSchedulerRef(t, ref, err, want)
			if x.proofCalls != 1 || (mode == "accepted") != (x.store.reads == 1) {
				t.Fatal("Agent existence read did not follow both current owners")
			}
			if (mode == "proof-error" || mode == "wrong-dispatch" || mode == "wrong-agent") && x.projectCalls != 0 {
				t.Fatal("untrusted intent reached Project gate")
			}
		})
	}
}

func TestAgentSchedulerCurrentCancellationWaitsForOriginalRead(t *testing.T) {
	x := newSchedulerCurrentControl(t)
	x.store.readErr = errors.New("scheduler-agent-private-storage")
	ref, err := x.read()
	requireZeroSchedulerRef(t, ref, err, f.DependencyUnavailable)
	if strings.Contains(fmt.Sprintf("%+v", err), "private-storage") {
		t.Fatal("raw storage material escaped")
	}
	x = newSchedulerCurrentControl(t)
	ctx, cancel := context.WithCancel(x.store.ctx)
	defer cancel()
	x.store.ctx = ctx
	entered, release := make(chan struct{}), make(chan struct{})
	x.store.onRead = func() { close(entered); <-release }
	type result struct {
		ref c.AgentRef
		err error
	}
	returned := make(chan result, 1)
	go func() { ref, err := x.read(); returned <- result{ref, err} }()
	<-entered
	cancel()
	select {
	case <-returned:
		close(release)
		t.Fatal("cancellation abandoned original SQL")
	default:
	}
	close(release)
	done := <-returned
	if !errors.Is(done.err, context.Canceled) || done.ref != (c.AgentRef{}) || !x.store.live {
		t.Fatal("cancelled current read lost actual join or original transaction", done.err)
	}
}
