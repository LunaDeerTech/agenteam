package execution

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

// Typed codec candidates and an actual Loop session; no invented SQL commit.
func directTextMountCandidate(t *testing.T) (directTextModelFacts, agentloop.DirectTextRequest) {
	t.Helper()
	facts, _ := directTextDrainCandidate(t)
	fields := facts.Snapshot.Fields().Context.Input().Fields()
	m := mt.ExecutionMountMetadata{ID: newTestID[i.Mount](t), WorkspaceID: newTestID[mt.Workspace](t), RunnerID: newTestID[rc.Runner](t), RunnerVersion: 2, RunnerName: "captured runner", Name: "alias", Workspace: "logical-directory", Version: 1}
	af := fields.Agent.Fields()
	af.AllowedMountIDs = []i.MountID{m.ID}
	var err error
	fields.Agent, err = ac.NewAgentConfig(af)
	if err != nil {
		t.Fatal(err)
	}
	fields.Mounts.Mounts, fields.Mounts.AttemptBinding = []mt.ExecutionMountMetadata{m}, fields.AttemptBinding
	input, err := ec.NewPreparationInput(fields)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewContextBuilder(work.TaskContextBuilder{})
	if err != nil {
		t.Fatal(err)
	}
	captured, err := builder.BuildContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := i.NewAgentRun(fields.Request.Launch.ProjectID, fields.Request.Launch.AgentID, fields.Request.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	rf := facts.Round.Fields()
	request, err := agentloop.BuildDirectTextRequest(context.Background(), captured, actor, rf.ID.String(), rf.CallID)
	if err != nil {
		t.Fatal(err)
	}
	sf := facts.Snapshot.Fields()
	sf.Context = captured
	facts.Snapshot, err = ec.NewDirectTextSnapshot(sf)
	if err != nil {
		t.Fatal(err)
	}
	rf.ContextDigest, rf.Input, rf.Messages = captured.Digest(), request.Request().Input, request.Request().Messages
	facts.Round, err = ec.NewDirectTextRound(rf)
	if err != nil {
		t.Fatal(err)
	}
	return facts, request
}

type directTextMountPort struct {
	mt.ExecutionMountUseProvider
	verify      func(context.Context, f.Tx, mt.ExecutionMountUse, *mt.ExecutionMountRetirement) error
	material    func(context.Context, f.Tx, mt.ExecutionMountSet) error
	retireCalls int
}

func (p *directTextMountPort) VerifyExecutionMountMaterialInTx(ctx context.Context, tx f.Tx, set mt.ExecutionMountSet) error {
	return p.material(ctx, tx, set)
}
func (p *directTextMountPort) VerifyExecutionMountUseInTx(ctx context.Context, tx f.Tx, use mt.ExecutionMountUse, retired *mt.ExecutionMountRetirement) error {
	return p.verify(ctx, tx, use, retired)
}
func (p *directTextMountPort) RetireExecutionMountUseInTx(context.Context, f.Tx, mt.ExecutionMountUse, mt.ExecutionMountRetirement) error {
	p.retireCalls++
	return fault(f.Forbidden)
}

func TestExecutionDirectTextMountHistoryProof(t *testing.T) {
	// Controlled same-Store rows isolate the actual private authority. SQL
	// constraints and whole transaction rollback are the real PG fixture's job.
	x := newLaunchControl(t)
	facts, _ := directTextMountCandidate(t)
	input := facts.Snapshot.Fields().Context.Input()
	request := input.Fields().Request
	processes := &preparationProcesses{process: facts.ProcessID}
	state := &directTextState{store: x.store, authority: x.authority, processes: processes, process: facts.ProcessID, calls: map[i.ExecutionID]*directTextCall{}, changed: make(chan struct{})}
	run := &directTextCall{owner: state, request: request, snapshot: facts.Snapshot, input: &preparationInputRecord{input: input}}
	state.calls[request.ExecutionID] = run
	ctx := context.WithValue(context.Background(), directTextContextKey{}, run)
	use := directTextMountUse(facts.Snapshot)
	var saved context.Context
	var savedTx f.Tx
	var expectedRetirement *mt.ExecutionMountRetirement
	visits := 0
	port := &directTextMountPort{}
	port.verify = func(c context.Context, tx f.Tx, value mt.ExecutionMountUse, retired *mt.ExecutionMountRetirement) error {
		visits++
		if !reflect.DeepEqual(value, use) || !reflect.DeepEqual(retired, expectedRetirement) {
			t.Fatal("historical identity/state changed")
		}
		if err := x.authority.RequireExecutionMountUseVerifyInTx(c, tx, value, retired); err != nil {
			t.Fatal(err)
		}
		if err := x.authority.RequireMountMaterialInTx(c, tx, value.Mounts); err != nil {
			t.Fatal(err)
		}
		requireCode(t, x.authority.RequireExecutionMountUseVerifyInTx(context.Background(), tx, value, retired), f.Forbidden)
		requireCode(t, x.authority.RequireExecutionMountUseVerifyInTx(c, f.NewTx(), value, retired), f.Forbidden)
		wrong := value.Clone()
		wrong.Mounts.AttemptBinding = ec.TriggerInputDigest([]byte("foreign attempt"))
		requireCode(t, x.authority.RequireExecutionMountUseVerifyInTx(c, tx, wrong, retired), f.Forbidden)
		wrong = value.Clone()
		wrong.SnapshotDigest = ec.TriggerInputDigest([]byte("foreign snapshot"))
		requireCode(t, x.authority.RequireExecutionMountUseVerifyInTx(c, tx, wrong, retired), f.Forbidden)
		other := &mt.ExecutionMountRetirement{Version: 7, EventID: newTestID[event.EventIdentity](t)}
		requireCode(t, x.authority.RequireExecutionMountUseVerifyInTx(c, tx, value, other), f.Forbidden)
		requireCode(t, x.authority.RequireExecutionMountUseAcquireInTx(c, tx, value), f.Forbidden)
		requireCode(t, x.authority.RequireExecutionMountUseRetireInTx(c, tx, value, *other), f.Forbidden)
		saved, savedTx = c, tx
		return nil
	}
	state.deps.Mounts = port
	row := &executionRecord{launch: request.Launch, summary: ec.Summary{ID: request.ExecutionID, Status: ec.Running, Version: 3}}
	snapshotID := facts.Snapshot.Fields().ID
	row.summary.SnapshotID = &snapshotID
	stored := &directTextRecord{snapshot: facts.Snapshot, round: facts.Round}
	runInTx := func(fn func(context.Context, f.Tx) error) {
		t.Helper()
		cause, _ := f.NewRecoveryCause("execution.mount-proof.control", request.ExecutionID.String(), request.Launch.Meta.RequestID.String())
		result := x.store.WithinTx(ctx, cause, func(c context.Context, tx f.Tx) error {
			locks, err := preparationLocks(request)
			if err != nil {
				return err
			}
			if err = x.store.AcquireAll(c, tx, locks); err != nil {
				return err
			}
			return fn(c, tx)
		})
		if result.State() != f.Committed {
			t.Fatal("controlled transaction failed", result)
		}
	}
	t.Run("running-live-proof-and-expired-witness", func(t *testing.T) {
		runInTx(func(c context.Context, tx f.Tx) error {
			return state.verifyDirectTextMountUse(c, tx, run, row, stored, run.input)
		})
		requireCode(t, x.authority.RequireExecutionMountUseVerifyInTx(context.WithoutCancel(saved), savedTx, use, nil), f.Forbidden)
		if visits != 1 {
			t.Fatal("running historical read skipped provider")
		}
	})
	t.Run("terminal-exact-retirement-and-fail-closed", func(t *testing.T) {
		at := input.Fields().CapturedAt
		terminal := &directTextTerminal{status: ec.Succeeded, version: 4, eventID: newTestID[event.EventIdentity](t), at: at}
		stored.terminal = terminal
		row.summary.Status, row.summary.Version, row.summary.CompletedAt = terminal.status, terminal.version, &at
		expectedRetirement = directTextMountRetirement(terminal)
		runInTx(func(c context.Context, tx f.Tx) error {
			return state.verifyDirectTextMountUse(c, tx, run, row, stored, run.input)
		})
		if visits != 2 {
			t.Fatal("terminal historical read skipped retirement")
		}
		port.verify = func(context.Context, f.Tx, mt.ExecutionMountUse, *mt.ExecutionMountRetirement) error {
			return fault(f.DependencyUnavailable)
		}
		runInTx(func(c context.Context, tx f.Tx) error {
			requireCode(t, state.verifyDirectTextMountUse(c, tx, run, row, stored, run.input), f.DependencyUnavailable)
			stored.terminal = nil
			requireCode(t, state.verifyDirectTextMountUse(c, tx, run, row, stored, run.input), f.InvalidState)
			return nil
		})
	})
}

func TestExecutionDirectTextMountRequiredMaterial(t *testing.T) {
	facts, _ := directTextMountCandidate(t)
	input := facts.Snapshot.Fields().Context.Input()
	run := &directTextCall{request: input.Fields().Request}
	state := &directTextState{}
	requireCode(t, state.verifyDirectTextMountMaterial(context.Background(), f.NewTx(), run, input), f.DependencyUnbound)
	port := &directTextMountPort{material: func(context.Context, f.Tx, mt.ExecutionMountSet) error { return fault(f.DependencyUnavailable) }}
	state.deps.Mounts = port
	requireCode(t, state.verifyDirectTextMountMaterial(context.Background(), f.NewTx(), run, input), f.DependencyUnavailable)
	// The historical empty profile neither needs nor calls a new provider.
	empty, _ := directTextDrainCandidate(t)
	e := empty.Snapshot.Fields().Context.Input()
	run.request = e.Fields().Request
	state.deps.Mounts = nil
	if err := state.verifyDirectTextMountMaterial(context.Background(), f.NewTx(), run, e); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionDirectTextMountWaitsForActualJoin(t *testing.T) {
	facts, request := directTextMountCandidate(t)
	model := &directTextDrainControl{t: t}
	controller, err := agentloop.NewDirectTextController(model)
	if err != nil {
		t.Fatal(err)
	}
	session, err := controller.Accept(request)
	if err != nil {
		t.Fatal(err)
	}
	store, port := &directTextTerminalRejectStore{}, &directTextMountPort{}
	state := &directTextState{store: store, deps: DirectTextDependencies{Mounts: port}, calls: map[i.ExecutionID]*directTextCall{}, changed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := facts.Snapshot.Fields().Context.Input()
	run := &directTextCall{owner: state, cancel: cancel, request: input.Fields().Request, input: &preparationInputRecord{input: input}, snapshot: facts.Snapshot, round: facts.Round, started: true, invocation: &directTextInvocation{session: session}}
	run.ctx = context.WithValue(ctx, directTextContextKey{}, run)
	state.calls[run.request.ExecutionID] = run
	driver := &DirectTextDriver{state: state}
	_, err = state.callAndFinish(run.ctx, run, false)
	state.returned(run)
	if err == nil || run.allJoined || session.Joined() || run.uncertainty != "draining" || port.retireCalls != 0 || store.attempts != 0 {
		t.Fatal("held Model reached retirement or lost original owner")
	}
	driver.Stop()
	if !errors.Is(run.ctx.Err(), context.Canceled) || driver.Joined() || port.retireCalls != 0 {
		t.Fatal("Stop was mistaken for actual join")
	}
	model.canJoin = true
	_, err = driver.ResolveUnknown(context.Background(), run.request.ExecutionID)
	if err == nil || !session.Joined() || !run.allJoined || model.begins != 1 || model.results != 1 || model.closes != 2 || store.attempts != 1 || port.retireCalls != 0 || run.uncertainty != "closing" || driver.Joined() {
		t.Fatal("same Model join was confused with terminal storage success", err)
	}
	// Actual handle joined, but original controlled Store explicitly refuses
	// the terminal transaction. No fictitious retirement/slot release follows.
	controller.Stop()
	if err = controller.Drain(context.Background()); err != nil || !controller.Joined() {
		t.Fatal("original Loop did not retire", err)
	}
}
