package work

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func pureSprintComplete(t *testing.T) (*sprintCompleteRecord, i.Actor, event.Summary) {
	t.Helper()
	actor := pureActor(t, 2)
	at, err := f.NewInstant(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	p := pc.ProjectRef{ID: pureID[i.Project](t, 3), OwnerUserID: pureID[i.User](t, 1), Name: "Demo", NormalizedName: "demo", Description: "project", Lifecycle: pc.Active, Version: 3, CreatedAt: at, UpdatedAt: at}
	before := c.Sprint{ID: pureID[pc.Sprint](t, 4), ProjectID: p.ID, MilestoneID: pureID[c.Milestone](t, 5), Title: "Sprint", Description: "description", ManualRank: "7fffffffffffffffffffffffffffffff", State: c.Current, Version: 2, CreatedAt: at, UpdatedAt: at}
	p.CurrentSprintID = &before.ID
	before.StartedAt = &at
	before.StartedBy = &c.ActorHistory{Kind: i.Human, UserID: p.OwnerUserID.String()}
	r := &sprintCompleteRecord{ID: pureID[c.SprintCompleteCommand](t, 6), Input: sprintCompleteInput{p.ID, before.ID, p.OwnerUserID, before.Version}, Key: "start-key", Revision: 1, State: "planned", Created: at}
	r.Semantic, err = r.Input.semantic(actor, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	after, err := deriveSprintComplete(r.Input, before, p, at, pureID[event.EventIdentity](t, 7))
	if err != nil {
		t.Fatal(err)
	}
	h, err := sprintCompleteHeader(after)
	if err != nil {
		t.Fatal(err)
	}
	events, err := c.RegisterSprintCompletionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	ev, err := events.NewSprintCompleted(h, c.SprintCompleted{CommandID: r.ID, ActorUserID: r.Input.User, MilestoneID: before.MilestoneID, ProjectVersion: after.Project.Version})
	if err != nil {
		t.Fatal(err)
	}
	r.Plan = sprintCompletePlan{Before: before, Project: p, After: after, Header: ev.Header(), Payload: ev.PayloadBytes()}
	if err = validateSprintCompleteRecord(r, actor); err != nil {
		t.Fatal(err)
	}
	return r, actor, ev.Summary()
}
func TestSprintCompletePlanBindsCurrentScopeAndVersions(t *testing.T) {
	r, actor, summary := pureSprintComplete(t)
	// The durable postimage does not alter Tasks or Scheduler enabled; the command checks all members in its final transaction.
	out, err := deriveSprintComplete(r.Input, r.Plan.Before, r.Plan.Project, r.Created, r.Plan.After.EventID)
	if err != nil || out.Sprint.State != c.Completed || out.Sprint.Version != 3 || out.Project.Version != 4 || out.Project.CurrentSprintID != nil || out.Sprint.ManualRank != r.Plan.Before.ManualRank {
		t.Fatal("completion changed unrelated facts", err)
	}
	for _, mutate := range []func(*sprintCompleteRecord){
		func(x *sprintCompleteRecord) { x.Plan.After.Sprint.Title = "forged" },
		func(x *sprintCompleteRecord) { x.Plan.After.Project.Description = "forged" },
		func(x *sprintCompleteRecord) { x.Plan.After.Sprint.Version++ },
		func(x *sprintCompleteRecord) { x.Plan.After.Project.Version++ },
		func(x *sprintCompleteRecord) { x.Input.Expected++ },
	} {
		fresh, _, _ := pureSprintComplete(t)
		mutate(fresh)
		if validateSprintCompleteRecord(fresh, actor) == nil {
			t.Fatal("forged plan accepted")
		}
	}
	for _, mutate := range []func(*c.Sprint, *pc.ProjectRef){
		func(_ *c.Sprint, p *pc.ProjectRef) { id := pureID[pc.Sprint](t, 90); p.CurrentSprintID = &id },
		func(b *c.Sprint, _ *pc.ProjectRef) {
			b.State = c.Planned
			b.StartedAt = nil
			b.StartedBy = nil
		},
		func(_ *c.Sprint, p *pc.ProjectRef) { p.Version = f.Version(math.MaxInt64) },
	} {
		b, p := r.Plan.Before.Clone(), r.Plan.Project.Clone()
		mutate(&b, &p)
		if _, err = deriveSprintComplete(r.Input, b, p, r.Created, r.Plan.After.EventID); err == nil {
			t.Fatal("invalid completion state accepted")
		}
	}
	binding, locks, opaque, err := sprintCompleteEventBinding(r, actor, summary)
	if err != nil || binding.Validate() != nil || len(locks) != 5 || len(opaque) == 0 {
		t.Fatal("event binding", err)
	}
	expected, _ := r.Input.locks(r.Key)
	if !sameLocks(locks, expected) {
		t.Fatal("missing original full union")
	}
	other := pureActor(t, 99)
	second, _, _, err := sprintCompleteEventBinding(r, other, summary)
	if err != nil || second == binding {
		t.Fatal("event plan did not bind current Session")
	}
	completed := r.Plan.After.Clone()
	r.Receipt = &completed
	r.State = "completed"
	at := r.Created
	r.Committed = &at
	if validateSprintCompleteRecord(r, other) != nil {
		t.Fatal("historical original-user receipt rejected under new current Session")
	}
	r.Receipt.Project.Version++
	if validateSprintCompleteRecord(r, other) == nil {
		t.Fatal("changed receipt accepted")
	}
}
func TestSprintCompleteRequiresPrivateOriginalWrite(t *testing.T) {
	r, actor, _ := pureSprintComplete(t)
	store, projects, authority, _ := purePorts(t)
	adapter, err := project.NewSprintCompletion(projects, authority)
	if err != nil {
		t.Fatal(err)
	}
	events, err := c.RegisterSprintCompletionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	startAdapter, err := project.NewSprintLifecycle(projects, authority)
	if err != nil {
		t.Fatal(err)
	}
	startEvents, err := c.RegisterSprintLifecycleEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	deps := SprintLifecycleDependencies{Authority: authority, Projects: startAdapter, Events: &denialEvents{}, SprintEvents: startEvents, Activity: &denialActivity{}, Completion: &SprintCompletionDependencies{Projects: adapter, Occupancy: &denialTransitionOccupancy{}, Pending: &denialTransitionPending{}, Events: events}}
	service, err := NewSprintLifecycle(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, remove := range []func(*SprintCompletionDependencies){
		func(d *SprintCompletionDependencies) { d.Projects = nil },
		func(d *SprintCompletionDependencies) { d.Occupancy = nil },
		func(d *SprintCompletionDependencies) { d.Pending = nil },
		func(d *SprintCompletionDependencies) { d.Events = c.SprintCompletionEvents{} },
	} {
		bad := deps
		incomplete := *deps.Completion
		remove(&incomplete)
		bad.Completion = &incomplete
		_, err = NewSprintLifecycle(store, bad)
		pureCode(t, err, f.DependencyUnbound)
	}
	legacyDeps := deps
	legacyDeps.Completion = nil
	legacy, err := NewSprintLifecycle(store, legacyDeps)
	if err != nil || !legacy.bound() {
		t.Fatal("legacy Start changed", err)
	}
	_, err = legacy.CompleteSprint(context.Background(), actor, f.CommandMeta{}, r.Input.Project, r.Input.Sprint)
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewSprintLifecycle(&denialStore{}, deps)
	pureCode(t, err, f.DependencyUnbound)
	change, err := sprintCompleteChange(r)
	if err != nil {
		t.Fatal(err)
	}
	err = authority.CheckSprintCompleteAppliedInTx(context.Background(), f.NewTx(), actor, change)
	pureCode(t, err, f.Forbidden)
	tx := f.NewTx()
	for _, witness := range []sprintCompleteWrite{
		{authority: authority, tx: f.NewTx(), actor: actor.Details(), id: r.ID, revision: r.Revision},
		{authority: authority, tx: tx, actor: actor.Details(), id: r.ID, revision: r.Revision + 1},
		{authority: authority, tx: tx, actor: pureActor(t, 99).Details(), id: r.ID, revision: r.Revision},
	} {
		ctx := context.WithValue(context.Background(), sprintCompleteWriteKey{}, witness)
		pureCode(t, authority.CheckSprintCompleteAppliedInTx(ctx, tx, actor, change), f.Forbidden)
	}
	if store.touches.Load() != 0 {
		t.Fatal("public values reached Work without original private witness")
	}
	run, entry, done, err := service.base.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	confirm, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.base.state().mu.Lock()
	entry.confirmations[&confirmation{cancel: cancel}] = struct{}{}
	service.base.state().mu.Unlock()
	service.Stop()
	if run.Err() != context.Canceled || confirm.Err() != context.Canceled {
		t.Fatal("Stop lost original calls")
	}
	timeout, stop := context.WithCancel(context.Background())
	stop()
	if !errors.Is(service.Drain(timeout), context.Canceled) {
		t.Fatal("Drain pretended join")
	}
	done()
	if err = service.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = service.base.begin(context.Background())
	pureCode(t, err, f.ShuttingDown)
}
func TestSprintCompleteDigestAndHistoricalReceipt(t *testing.T) {
	r, actor, _ := pureSprintComplete(t)
	meta := f.CommandMeta{RequestID: pureID[f.Request](t, 11), IdempotencyKey: r.Key, ExpectedVersion: &r.Input.Expected}
	value, err := c.CompleteSprintDigest(actor, meta, r.Input.Project, r.Input.Sprint)
	if err != nil || value != r.Semantic {
		t.Fatal("digest drift", err)
	}
	meta.RequestID = pureID[f.Request](t, 12)
	same, err := c.CompleteSprintDigest(pureActor(t, 22), meta, r.Input.Project, r.Input.Sprint)
	if err != nil || same != value {
		t.Fatal("delivery metadata entered original intent")
	}
	next := r.Input.Expected + 1
	meta.ExpectedVersion = &next
	different, err := c.CompleteSprintDigest(actor, meta, r.Input.Project, r.Input.Sprint)
	if err != nil || different == value {
		t.Fatal("expected version not bound")
	}
	raw, err := json.Marshal(r.Plan.After)
	if err != nil {
		t.Fatal(err)
	}
	var receipt c.SprintCompleteMutation
	if err = json.Unmarshal(raw, &receipt); err != nil || !sameValue(receipt, r.Plan.After) {
		t.Fatal("receipt round trip", err)
	}
	bad := strings.Replace(string(raw), `"event_id":`, `"extra":1,"event_id":`, 1)
	if json.Unmarshal([]byte(bad), &receipt) == nil {
		t.Fatal("receipt accepted unknown field")
	}
	copy := r.Plan.After.Clone()
	changed := pureID[pc.Sprint](t, 77)
	copy.Project.CurrentSprintID = &changed
	copy.Sprint.CompletedBy.UserID = pureID[i.User](t, 78).String()
	if r.Plan.After.Project.CurrentSprintID != nil || r.Plan.After.Sprint.CompletedBy.UserID != r.Input.User.String() {
		t.Fatal("receipt alias")
	}
}

func TestSprintCompleteUnknownKeepsOriginalAttempt(t *testing.T) {
	r, actor, _ := pureSprintComplete(t)
	identity, err := c.SprintCompleteIdentity(r.Input.Project, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	original := f.UnknownResult(pureID[f.TransactionAttempt](t, 91), commandCause(identity))
	confirmationAttempt := f.UnknownResult(pureID[f.TransactionAttempt](t, 92), commandCause(identity))
	for _, failure := range []f.CommitResult{confirmationAttempt, f.NotCommittedResult(f.NewFault(f.SessionRevoked, f.NotCommitted))} {
		store := &confirmationFailureStore{result: failure}
		state := &serviceState{store: store, calls: map[*call]struct{}{}, changed: make(chan struct{})}
		service := &SprintLifecycleService{base: &Service{data: func() *serviceState { return state }}}
		ctx, entry, done, err := service.base.begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.confirmCompletion(ctx, entry, actor, c.SprintCompleteLookupRequest{ProjectID: r.Input.Project, Key: r.Key, Semantic: r.Semantic}, original)
		done()
		var public *f.Fault
		var private commitFailure
		if !errors.As(err, &public) || !errors.As(err, &private) || public.Code != f.CommitUnknown || public.CommitState != f.Unknown || public.RetryHint != "lookup" || public.CauseID != original.AttemptID().String() || private.result.AttemptID() != original.AttemptID() || private.result.Cause().Details().Primary.Canonical() != identity.Canonical() {
			t.Fatal("original uncertain attempt replaced")
		}
		if len(entry.confirmations) != 0 {
			t.Fatal("confirmation not actually deregistered")
		}
	}
}
