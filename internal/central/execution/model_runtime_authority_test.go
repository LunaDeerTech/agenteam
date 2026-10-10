package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	model "github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Complete typed candidates from the existing controlled capture fixture.
// They are not a sealed database Snapshot or a private live runtime grant.
func runtimeModelCandidate(t *testing.T) (directTextModelFacts, mc.ConsumerRequest) {
	t.Helper()
	fields := contextBuilderInput(t).Fields()
	ref, err := sc.NewCredentialRef(newTestID[sc.Credential](t), i.SystemScope())
	if err != nil {
		t.Fatal(err)
	}
	fields.Model.Snapshot.CredentialRef = &ref
	fields.Model.CredentialLease = &sc.CredentialLease{LeaseID: newTestID[sc.Lease](t), CredentialRef: ref}
	input, err := ec.NewPreparationInput(fields)
	if err != nil {
		t.Fatal(err)
	}
	builder, _ := NewContextBuilder(contextBuilderProvider(t))
	captured, err := builder.BuildContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.NewInstant(fields.CapturedAt.Time().Add(time.Second))
	process := newTestID[oc.Process](t)
	snapshot, err := ec.NewDirectTextSnapshot(ec.DirectTextSnapshotFields{ID: newTestID[ec.Snapshot](t), StartID: newTestID[ec.DirectTextStart](t), ProcessID: process, Context: captured, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	messages := []mc.Message{{Role: "system", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "captured system"}}}}, {Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "captured task"}}}}}
	digest, err := model.TextInputDigest(messages)
	if err != nil {
		t.Fatal(err)
	}
	roundID := newTestID[ec.Round](t)
	identity := mc.InputIdentity{ExecutionID: &fields.Request.ExecutionID, RoundID: roundID.String(), Digest: digest, SchemaVersion: 1}
	round, err := ec.NewDirectTextRound(ec.DirectTextRoundFields{ID: roundID, InputBindingID: newTestID[ec.InputBinding](t), ExecutionID: fields.Request.ExecutionID, SnapshotID: snapshot.Fields().ID, StartID: snapshot.Fields().StartID, CallID: newTestID[mc.Call](t), ContextDigest: captured.Digest(), Input: identity, Messages: messages, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := i.NewAgentRun(fields.Request.Launch.ProjectID, fields.Request.Launch.AgentID, fields.Request.ExecutionID)
	call := round.Fields().CallID
	r := mc.ConsumerRequest{Action: mc.InvokeConsumer, Actor: actor, Consumer: fields.Model.Consumer.Clone(), SnapshotID: fields.Model.Snapshot.ID, LeaseOwner: fields.Model.LeaseOwner, LeaseID: &fields.Model.CredentialLease.LeaseID, CallID: &call, Input: &identity}
	return directTextModelFacts{Snapshot: snapshot, Round: round, ProcessID: process}, r
}

func TestExecutionRuntimeModelMatchesExactRoundAndRetirement(t *testing.T) {
	facts, request := runtimeModelCandidate(t)
	if err := runtimeModelRequestMatches(request, facts, true); err != nil {
		t.Fatal("complete candidate shape rejected", err)
	}
	for _, mutate := range []func(*mc.ConsumerRequest){
		func(r *mc.ConsumerRequest) { v := newTestID[mc.Call](t); r.CallID = &v },
		func(r *mc.ConsumerRequest) { r.Input.Digest = ec.TriggerInputDigest([]byte("changed")) },
		func(r *mc.ConsumerRequest) { v := newTestID[sc.Lease](t); r.LeaseID = &v },
		func(r *mc.ConsumerRequest) { r.SnapshotID = newTestID[mc.Snapshot](t) },
	} {
		changed := request.Clone()
		mutate(&changed)
		if err := runtimeModelRequestMatches(changed, facts, true); err == nil {
			t.Fatal("changed captured identity accepted")
		}
	}
	attempt := mc.AttemptIdentity{CallID: *request.CallID, InvocationID: newTestID[mc.Invocation](t), AttemptIndex: 1, ProcessID: facts.ProcessID, Fence: 1}
	final := request.Clone()
	final.Action, final.Attempt = mc.FinalizeConsumer, &attempt
	registration, _ := i.RegisterService(i.ModelRuntime)
	scope, _ := i.InProject(request.Consumer.ProjectID)
	final.Actor, _ = registration.Actor(attempt.InvocationID.String(), scope)
	if err := runtimeModelRequestMatches(final, facts, true); err != nil {
		t.Fatal("original technical finalize rejected", err)
	}
	final.Actor = request.Actor
	if err := runtimeModelRequestMatches(final, facts, true); err == nil {
		t.Fatal("Agent actor replaced technical finalize lineage")
	}
	retire := mc.ExecutionModelRetirementRequest{Actor: request.Actor, Model: facts.Snapshot.Fields().Context.Input().Fields().Model, TerminalVersion: 5}.ConsumerRequest()
	facts.Summary.Status, facts.Summary.Version = ec.Running, 5
	if err := runtimeModelRequestMatches(retire, facts, true); err == nil {
		t.Fatal("running row admitted shared lease retirement")
	}
	at := facts.Snapshot.Fields().CreatedAt
	facts.Summary.Status, facts.Summary.CompletedAt = ec.Succeeded, &at
	if err := runtimeModelRequestMatches(retire, facts, true); err != nil {
		t.Fatal("exact terminal candidate shape rejected", err)
	}
	facts.Summary.Version++
	if err := runtimeModelRequestMatches(retire, facts, true); err == nil {
		t.Fatal("different terminal version accepted")
	}
}

func TestExecutionRuntimeModelDoesNotGrantFromPublicCandidates(t *testing.T) {
	_, request := runtimeModelCandidate(t)
	r := newLaunchControl(t)
	plan, err := r.authority.Discover(context.Background(), request)
	requireCode(t, err, f.DependencyUnbound)
	if plan.Validate() == nil || r.store.writes != 0 {
		t.Fatal("public candidate obtained a plan or wrote runtime state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, err = r.authority.Discover(ctx, request)
	if !errors.Is(err, context.Canceled) || plan.Validate() == nil || r.store.writes != 0 {
		t.Fatal("cancelled caller retained a runtime grant")
	}
}
