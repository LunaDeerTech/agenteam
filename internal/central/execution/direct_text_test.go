package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// These candidates use the existing controlled capture fixture. Encoding a
// candidate is not a sealed Snapshot, an admitted Loop or a database commit.
func directTextChangedJSON(t *testing.T, raw []byte, change func(map[string]json.RawMessage)) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	change(fields)
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = cursor.CanonicalJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestExecutionDirectTextCodecsKeepExactIdentityAndSafeOutput(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	snapshot, round := facts.Snapshot, facts.Round
	decodedSnapshot, err := ec.DecodeDirectTextSnapshot(snapshot.CanonicalBytes())
	if err != nil || decodedSnapshot.Digest() != snapshot.Digest() || !bytes.Equal(decodedSnapshot.CanonicalBytes(), snapshot.CanonicalBytes()) || decodedSnapshot.Fields().Context.InputDigest() != snapshot.Fields().Context.InputDigest() {
		t.Fatal("Snapshot candidate lost fixed input", err)
	}
	decodedRound, err := ec.DecodeDirectTextRound(round.CanonicalBytes())
	if err != nil || decodedRound.Digest() != round.Digest() || !bytes.Equal(decodedRound.CanonicalBytes(), round.CanonicalBytes()) || decodedRound.Fields().Input.Digest != round.Fields().Input.Digest {
		t.Fatal("Round candidate lost exact input", err)
	}
	for _, pair := range []struct {
		raw    []byte
		digest f.Digest
	}{{snapshot.CanonicalBytes(), snapshot.Digest()}, {round.CanonicalBytes(), round.Digest()}} {
		canonical, err := cursor.CanonicalJSON(pair.raw)
		if err != nil || !bytes.Equal(canonical, pair.raw) || ec.TriggerInputDigest(pair.raw) != pair.digest {
			t.Fatal("candidate encoding is not canonical")
		}
	}
	fields := round.Fields()
	fields.Messages[0].Parts[0].Text.Text = "changed-round-private-canary"
	*fields.Input.ExecutionID = newTestID[i.Execution](t)
	if round.Fields().Messages[0].Parts[0].Text.Text == fields.Messages[0].Parts[0].Text.Text || *round.Fields().Input.ExecutionID == *fields.Input.ExecutionID {
		t.Fatal("round candidate exposed mutable fields")
	}
	rawSnapshot, rawRound := snapshot.CanonicalBytes(), round.CanonicalBytes()
	rawSnapshot[0], rawRound[0] = '!', '!'
	if ec.TriggerInputDigest(snapshot.CanonicalBytes()) != snapshot.Digest() || ec.TriggerInputDigest(round.CanonicalBytes()) != round.Digest() {
		t.Fatal("candidate exposed mutable canonical bytes")
	}
	for _, value := range []any{snapshot, round} {
		implicit, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, output := range []string{string(implicit), fmt.Sprintf("%+v %#v", value, value)} {
			if strings.Contains(output, "canary") || strings.Contains(output, "captured system") || strings.Contains(output, snapshot.Fields().Context.Input().Fields().Model.Snapshot.Endpoint) {
				t.Fatal("default output exposed captured content")
			}
		}
	}
	if snapshot.LogValue().String() != "execution_snapshot" || round.LogValue().String() != "execution_round_input" {
		t.Fatal("log output lost safe default")
	}
	var implicitSnapshot ec.DirectTextSnapshot
	var implicitRound ec.DirectTextRound
	if json.Unmarshal(snapshot.CanonicalBytes(), &implicitSnapshot) == nil || json.Unmarshal(round.CanonicalBytes(), &implicitRound) == nil {
		t.Fatal("implicit decoder bypassed explicit canonical codec")
	}
	for _, source := range [][]byte{snapshot.CanonicalBytes(), round.CanonicalBytes()} {
		for _, change := range []func(map[string]json.RawMessage){
			func(v map[string]json.RawMessage) { v["unexpected"] = json.RawMessage(`true`) },
			func(v map[string]json.RawMessage) { v["schema_version"] = json.RawMessage(`2`) },
		} {
			bad := directTextChangedJSON(t, source, change)
			if got, err := ec.DecodeDirectTextSnapshot(bad); err == nil || got.Validate() == nil {
				t.Fatal("invalid Snapshot envelope accepted")
			}
			if got, err := ec.DecodeDirectTextRound(bad); err == nil || got.Validate() == nil {
				t.Fatal("invalid Round envelope accepted")
			}
		}
	}
	if _, err = ec.DecodeDirectTextSnapshot(append([]byte(" "), snapshot.CanonicalBytes()...)); err == nil {
		t.Fatal("noncanonical Snapshot accepted")
	}
	if _, err = ec.DecodeDirectTextRound(append(round.CanonicalBytes(), []byte(" {}")...)); err == nil {
		t.Fatal("trailing Round envelope accepted")
	}
	badSnapshot := snapshot.Fields()
	badSnapshot.CreatedAt, _ = f.NewInstant(badSnapshot.Context.Input().Fields().CapturedAt.Time().Add(-time.Microsecond))
	if _, err = ec.NewDirectTextSnapshot(badSnapshot); err == nil {
		t.Fatal("Snapshot predates captured input")
	}
	for _, mutate := range []func(*ec.DirectTextRoundFields){
		func(v *ec.DirectTextRoundFields) { v.Input.RoundID = newTestID[ec.Round](t).String() },
		func(v *ec.DirectTextRoundFields) { v.Input.ExecutionID = nil },
		func(v *ec.DirectTextRoundFields) { v.Messages[0].Role = "assistant" },
	} {
		bad := round.Fields()
		mutate(&bad)
		if _, err = ec.NewDirectTextRound(bad); err == nil {
			t.Fatal("Round identity or role mismatch accepted")
		}
	}
}

func TestExecutionDirectTextLifecycleRequiresClosedTypedEvents(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	catalog := event.NewCatalog()
	types, err := ec.RegisterDirectTextEvents(catalog)
	if err != nil || !types.Valid() || len(catalog.Schemas()) != 4 {
		t.Fatal("lifecycle catalog", err)
	}
	if err = catalog.Seal(); err != nil {
		t.Fatal(err)
	}
	request := facts.Snapshot.Fields().Context.Input().Fields().Request
	run := &directTextCall{request: request, snapshot: facts.Snapshot, round: facts.Round}
	state := &directTextState{deps: DirectTextDependencies{Lifecycle: types}}
	for _, entry := range []struct {
		status ec.Status
		reason string
	}{{ec.Running, "started"}, {ec.Succeeded, "completed"}, {ec.Cancelled, "cancelled"}, {ec.Failed, "model_failed"}, {ec.Failed, "incomplete_response"}, {ec.Failed, "runtime_failed"}} {
		value, err := state.lifecycleEvent(run, entry.status, entry.reason, 4, 1, facts.Snapshot.Fields().CreatedAt)
		if err != nil || value.Validate() != nil || !catalog.Owns(value) {
			t.Fatal("typed lifecycle event", err)
		}
		var payload ec.DirectTextLifecycle
		if err = json.Unmarshal(value.PayloadBytes(), &payload); err != nil || payload.Validate() != nil || payload.ExecutionID != request.ExecutionID || payload.Status != entry.status || payload.Reason != entry.reason {
			t.Fatal("lifecycle payload identity", err)
		}
		restored, err := catalog.Restore(ec.ExecutionProducer, value.Header(), value.PayloadBytes())
		if err != nil || !bytes.Equal(restored.PayloadBytes(), value.PayloadBytes()) {
			t.Fatal("closed event restoration", err)
		}
		bad := payload
		bad.Reason = "private-provider-error-canary"
		if _, err = types.New(value.Header(), bad); err == nil {
			t.Fatal("arbitrary error text entered lifecycle")
		}
		header := value.Header()
		header.AggregateID = newTestID[event.Aggregate](t)
		if _, err = types.New(header, payload); err == nil {
			t.Fatal("foreign aggregate admitted")
		}
		header = value.Header()
		header.AggregateVersion = nil
		if _, err = types.New(header, payload); err == nil {
			t.Fatal("missing canonical version admitted")
		}
		badJSON := directTextChangedJSON(t, value.PayloadBytes(), func(v map[string]json.RawMessage) { v["body"] = json.RawMessage(`"private-output-canary"`) })
		if _, err = catalog.Restore(ec.ExecutionProducer, value.Header(), badJSON); err == nil {
			t.Fatal("unknown lifecycle payload field admitted")
		}
	}
	if _, err = state.lifecycleEvent(run, ec.Preparing, "started", 4, 1, facts.Snapshot.Fields().CreatedAt); err == nil {
		t.Fatal("preparing mistaken for Started")
	}
}

func TestExecutionDirectTextRejectsUnboundAndForeignOwners(t *testing.T) {
	r := newLaunchControl(t)
	if driver, err := NewDirectTextDriver(r.store, r.authority, DirectTextDependencies{}); err == nil || driver != nil {
		t.Fatal("missing real dependencies admitted")
	}
	if driver, err := NewDirectTextDriver(nil, r.authority, DirectTextDependencies{}); err == nil || driver != nil {
		t.Fatal("missing store admitted")
	}
	if _, err := NewDirectTextEventAuthority(nil); err == nil {
		t.Fatal("missing event owner admitted")
	}
	events, err := NewDirectTextEventAuthority(r.authority)
	if err != nil {
		t.Fatal(err)
	}
	facts, request := runtimeModelCandidate(t)
	receipt := ec.DirectTextReceipt{ExecutionID: facts.Round.Fields().ExecutionID, SnapshotID: facts.Snapshot.Fields().ID, RoundID: facts.Round.Fields().ID, Status: ec.Running, Version: 3}
	// Even package-local fragments lack actual registry admission. These are
	// negative controls, never a fake successful runtime or terminal proof.
	unregistered := &directTextCall{request: facts.Snapshot.Fields().Context.Input().Fields().Request, owner: &directTextState{store: r.store, authority: r.authority, calls: map[i.ExecutionID]*directTextCall{}}}
	contexts := []context.Context{nil, context.Background(), context.WithValue(context.Background(), directTextContextKey{}, receipt), context.WithValue(context.Background(), directTextContextKey{}, unregistered)}
	for _, ctx := range contexts {
		if owner, err := r.authority.directTextOwner(ctx); err == nil || owner != nil {
			t.Fatal("public or unregistered owner admitted")
		}
		if value, err := r.authority.directTextModelPlanningScope(ctx, false); err == nil || value.Snapshot.Validate() == nil {
			t.Fatal("foreign context obtained Model planning facts")
		}
		if value, err := r.authority.directTextRetirementScope(ctx, f.NewTx()); err == nil || value.Snapshot.Validate() == nil {
			t.Fatal("foreign context obtained terminal retirement proof")
		}
		if _, err := events.DiscoverAppend(ctx, request.Actor, event.Summary{}); err == nil {
			t.Fatal("foreign context authorized lifecycle event")
		}
	}
	if r.store.writes != 0 {
		t.Fatal("rejected owner wrote state")
	}
	if _, err := ec.NewDirectTextSnapshot(ec.DirectTextSnapshotFields{}); err == nil {
		t.Fatal("empty Snapshot promoted to proof")
	}
	// A returned call with an uncertain outcome still owns its registry entry.
	// This isolates bookkeeping/Stop, not SQL observation or Loop success.
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	uncertain := f.NewFault(f.CommitUnknown, f.Unknown)
	state := &directTextState{calls: map[i.ExecutionID]*directTextCall{}, changed: make(chan struct{})}
	run := &directTextCall{owner: state, ctx: runCtx, cancel: cancel, request: ec.PreparationRequest{ExecutionID: receipt.ExecutionID}, unresolved: uncertain, invocation: &directTextInvocation{}}
	state.calls[receipt.ExecutionID] = run
	driver := &DirectTextDriver{state: state}
	state.returned(run)
	if state.calls[receipt.ExecutionID] != run || runCtx.Err() != nil || !run.returned || driver.Joined() {
		t.Fatal("returned Unknown retired its original owner")
	}
	driver.Stop()
	if !errors.Is(runCtx.Err(), context.Canceled) || driver.Joined() {
		t.Fatal("Stop did not cancel, or falsely joined retained Unknown")
	}
	if err := driver.Drain(context.Background()); !errors.Is(err, uncertain) || state.calls[receipt.ExecutionID] != run {
		t.Fatal("Drain lost original Unknown ownership", err)
	}
	// Model only the registry step following an independent exact observation.
	// Clearing this field is not evidence that such an observation occurred.
	state.mu.Lock()
	run.unresolved = nil
	state.mu.Unlock()
	state.returned(run)
	if !driver.Joined() || len(state.calls) != 0 || driver.Drain(context.Background()) != nil {
		t.Fatal("resolved and actually returned owner did not retire")
	}
	directTextRetainedDrainControl(t, false)
	directTextRetainedDrainControl(t, true)
}

// Formal codecs supply a valid Loop candidate; the controlled values do not
// represent real Work writes, runtime authorization, or a startup commit.
func directTextDrainCandidate(t *testing.T) (directTextModelFacts, agentloop.DirectTextRequest) {
	t.Helper()
	facts, _ := runtimeModelCandidate(t)
	v := facts.Snapshot.Fields().Context.Input().Fields()
	agent := v.Agent.Fields()
	agent.Core.InjectAgentsMD, agent.Core.ReasoningEffort = false, nil
	var err error
	v.Agent, err = ac.NewAgentConfig(agent)
	if err != nil {
		t.Fatal(err)
	}
	v.Tools = []tc.ExecutionTool{}
	v.Model.Snapshot.Identity.AdapterRevision = adapter.OpenAIChatTextRevision
	taskID, err := f.ParseID[wc.Task](v.Request.Launch.Trigger.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	p, a, at := v.Request.Launch.ProjectID, v.Request.Launch.AgentID, v.CapturedAt
	sprintID, milestoneID := newTestID[pc.Sprint](t), newTestID[wc.Milestone](t)
	v.Project.CurrentSprintID = &sprintID
	actor := wc.ActorHistory{Kind: i.Human, UserID: v.Project.OwnerUserID.String()}
	task := wc.Task{ID: taskID, ProjectID: p, MilestoneID: milestoneID, SprintID: sprintID, Title: "drain-control", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityHigh, State: wc.TaskStateInProgress, AssigneeAgentID: &a, ManualRank: "80000000000000000000000000000000", Version: 2, CreatedAt: at, UpdatedAt: at}
	if v.Request.Launch.Purpose == "task/review" {
		task.State = wc.TaskStateInReview
	}
	sprint := wc.Sprint{ID: sprintID, ProjectID: p, MilestoneID: milestoneID, Title: "drain-sprint", ManualRank: task.ManualRank, Version: 2, CreatedAt: at, UpdatedAt: at, StartedAt: &at, StartedBy: &actor, State: wc.Current}
	milestone := wc.Milestone{ID: milestoneID, ProjectID: p, Title: "drain-milestone", ManualRank: task.ManualRank, Version: 1, CreatedAt: at, UpdatedAt: at}
	payload, err := json.Marshal(wc.TaskCreatedPayload{InitialState: wc.TaskStateBacklog, MilestoneID: milestoneID, SprintID: sprintID, Type: task.Type, Priority: task.Priority})
	if err != nil {
		t.Fatal(err)
	}
	command := newTestID[wc.TaskCommand](t)
	history, err := json.Marshal(wc.TaskEvent{ID: newTestID[wc.TaskEvent](t), ProjectID: p, TaskID: taskID, TaskVersion: 1, Type: wc.TaskEventCreated, Actor: wc.TaskEventActor{Type: i.Human, UserID: v.Project.OwnerUserID, Source: "task_domain"}, OperationID: command, CorrelationID: command, Payload: payload, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	type taskSource struct {
		SchemaVersion      f.Version         `json:"schema_version"`
		Task               wc.Task           `json:"task"`
		Sprint             wc.Sprint         `json:"sprint"`
		Milestone          wc.Milestone      `json:"milestone"`
		Purpose            string            `json:"purpose"`
		UnresolvedBlockers []wc.TaskBlocker  `json:"unresolved_blockers"`
		RecentTaskEvents   []json.RawMessage `json:"recent_task_events"`
	}
	digest, err := v.Request.Launch.Digest()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(struct {
		SchemaVersion f.Version     `json:"schema_version"`
		ExecutionID   i.ExecutionID `json:"execution_id"`
		LaunchDigest  f.Digest      `json:"launch_digest"`
		Input         taskSource    `json:"input"`
	}{1, v.Request.ExecutionID, digest, taskSource{1, task, sprint, milestone, v.Request.Launch.Purpose, []wc.TaskBlocker{}, []json.RawMessage{history}}})
	if err != nil {
		t.Fatal(err)
	}
	v.Trigger, err = ec.NewCapturedTriggerInput(ec.TriggerInputRef{ProviderType: "task", SchemaVersion: 1, InputID: newTestID[ec.TriggerInput](t), Digest: ec.TriggerInputDigest(raw)}, raw)
	if err != nil {
		t.Fatal(err)
	}
	input, err := ec.NewPreparationInput(v)
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
	runActor, err := i.NewAgentRun(p, a, v.Request.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	request, err := agentloop.BuildDirectTextRequest(context.Background(), captured, runActor, facts.Round.Fields().ID.String(), facts.Round.Fields().CallID)
	if err != nil {
		t.Fatal(err)
	}
	sf := facts.Snapshot.Fields()
	sf.Context = captured
	facts.Snapshot, err = ec.NewDirectTextSnapshot(sf)
	if err != nil {
		t.Fatal(err)
	}
	rf := facts.Round.Fields()
	rf.ContextDigest = captured.Digest()
	rf.Input = request.Request().Input
	rf.Messages = request.Request().Messages
	facts.Round, err = ec.NewDirectTextRound(rf)
	if err != nil {
		t.Fatal(err)
	}
	return facts, request
}

type directTextDrainControl struct {
	t                       *testing.T
	begins, results, closes int
	canJoin, joined         bool
	owner                   context.Context
	closeOutcome            error
}

func (v *directTextDrainControl) BeginChat(ctx context.Context, _ mc.ModelRequest) (mc.JSONCall, error) {
	v.begins++
	v.owner = ctx
	return v, nil
}
func (v *directTextDrainControl) Result(context.Context) (mc.ModelResponse, error) {
	v.results++
	return mc.ModelResponse{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
}
func (v *directTextDrainControl) Close(ctx context.Context) error {
	v.closes++
	if ctx.Value(directTextContextKey{}) != v.owner.Value(directTextContextKey{}) {
		v.t.Fatal("cleanup replaced original owner")
	}
	if !v.canJoin {
		return context.DeadlineExceeded
	}
	v.joined = true
	return v.closeOutcome
}
func (v *directTextDrainControl) Joined() bool { return v.joined }

type directTextTerminalRejectStore struct {
	Store
	attempts int
}

func (s *directTextTerminalRejectStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	s.attempts++
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}
func directTextRetainedDrainControl(t *testing.T, cancelledOutcome bool) {
	t.Helper()
	facts, request := directTextDrainCandidate(t)
	model := &directTextDrainControl{t: t}
	controller, err := agentloop.NewDirectTextController(model)
	if err != nil {
		t.Fatal(err)
	}
	session, err := controller.Accept(request)
	if err != nil {
		t.Fatal(err)
	}
	store := &directTextTerminalRejectStore{}
	state := &directTextState{store: store, calls: map[i.ExecutionID]*directTextCall{}, changed: make(chan struct{})}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := &directTextCall{owner: state, cancel: cancel, request: facts.Snapshot.Fields().Context.Input().Fields().Request, snapshot: facts.Snapshot, round: facts.Round, started: true, invocation: &directTextInvocation{session: session}}
	run.ctx = context.WithValue(runCtx, directTextContextKey{}, run)
	state.calls[run.request.ExecutionID] = run
	driver := &DirectTextDriver{state: state}
	_, err = state.callAndFinish(run.ctx, run, false)
	state.returned(run)
	if err == nil || run.uncertainty != "draining" || run.allJoined || session.Joined() || model.begins != 1 || model.results != 1 || model.closes != 1 || store.attempts != 0 {
		t.Fatal("failed Close lost original draining owner", err)
	}
	if cancelledOutcome {
		// A joined Model handle may still report its original business outcome.
		// Cancellation belongs to the original run, not the recovery wait.
		cancel()
		model.closeOutcome = &mc.ModelError{Category: "cancelled", Code: "wire_cancelled"}
	}
	model.canJoin = true
	_, err = driver.ResolveUnknown(context.Background(), run.request.ExecutionID)
	// Terminal storage deliberately reports NotCommitted without running its
	// callback: this proves join advancement, never a fictional terminal commit.
	if err == nil || !session.Joined() || !run.allJoined || model.begins != 1 || model.results != 1 || model.closes != 2 || store.attempts != 1 || run.uncertainty != "closing" || state.calls[run.request.ExecutionID] != run {
		t.Fatal("recovery did not drain the same handle without redispatch", err)
	}
	if cancelledOutcome {
		var outcome *mc.ModelError
		var terminalFault *f.Fault
		if !errors.Is(run.invocation.callError, context.Canceled) || !errors.As(run.invocation.callError, &outcome) || outcome.Validate() != nil || outcome.Category != "cancelled" || outcome.Code != "wire_cancelled" {
			t.Fatal("joined cleanup lost the original cancellation outcome", run.invocation.callError)
		}
		if !errors.As(err, &terminalFault) || terminalFault.CommitState != f.NotCommitted || run.terminal != nil {
			t.Fatal("joined business outcome bypassed terminal storage refusal", err)
		}
	}
	controller.Stop()
	if err = controller.Drain(context.Background()); err != nil || !controller.Joined() {
		t.Fatal("original Loop handle did not retire", err)
	}
	if driver.Joined() {
		t.Fatal("Model join falsely claimed terminal persistence")
	}
}
