package execution

import (
	"context"
	"encoding/json"
	"reflect"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type DirectTextEventAuthority struct {
	authority *Authority
	issuer    oc.PlanIssuer
}

func NewDirectTextEventAuthority(a *Authority) (*DirectTextEventAuthority, error) {
	if a == nil || a.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &DirectTextEventAuthority{a, oc.NewPlanIssuer()}, nil
}
func directTextEventBinding(actor i.Actor, summary event.Summary) f.Digest {
	raw, _ := json.Marshal(struct {
		Domain string
		Actor  i.ActorDetails
		Event  event.Summary
	}{"execution.direct-text.event.v1", actor.Details(), summary})
	return c.TriggerInputDigest(raw)
}
func (a *DirectTextEventAuthority) eventOwner(ctx context.Context, actor i.Actor, summary event.Summary) (*directTextCall, event.Event, error) {
	if a == nil || a.authority == nil {
		return nil, event.Event{}, fault(f.DependencyUnbound)
	}
	run, err := a.authority.directTextOwner(ctx)
	if err != nil {
		return nil, event.Event{}, err
	}
	s := run.owner
	s.mu.Lock()
	expected := run.event
	phase := run.phase
	s.mu.Unlock()
	actual, err := i.NewAgentRun(run.request.Launch.ProjectID, run.request.Launch.AgentID, run.request.ExecutionID)
	if err != nil || !actual.Equal(actor) || expected.Validate() != nil || !reflect.DeepEqual(expected.Summary(), summary) || !(phase == "planning" || phase == "starting" || phase == "closing" || phase == "terminal") {
		return nil, event.Event{}, fault(f.Forbidden)
	}
	return run, expected, nil
}
func (a *DirectTextEventAuthority) DiscoverAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	run, _, err := a.eventOwner(ctx, actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.issuer, directTextEventBinding(actor, summary), run.runtimeLocks, []byte("execution.direct-text.event.v1"))
}
func (a *DirectTextEventAuthority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	run, expected, err := a.eventOwner(ctx, actor, summary)
	if err != nil {
		return err
	}
	if !stage.Valid() || !deps.Matches(a.issuer, directTextEventBinding(actor, summary)) || string(deps.Opaque()) != "execution.direct-text.event.v1" {
		return fault(f.Forbidden)
	}
	s := run.owner
	s.mu.Lock()
	valid := run.tx == tx && (run.phase == "starting" || run.phase == "terminal")
	locks := run.locks
	s.mu.Unlock()
	if !valid {
		return fault(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	row, err := loadExecution(ctx, x, run.request.ExecutionID)
	if err != nil {
		return err
	}
	stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
	if err != nil {
		return err
	}
	if row == nil || !sameDirectText(stored, run.snapshot, run.round) || row.summary.Version != *expected.Header().AggregateVersion {
		return fault(f.ConfirmationStale)
	}
	if run.phase == "starting" {
		if row.summary.Status != c.Running || stored.startedEvent != summary.Header.EventID || stored.startedVersion != row.summary.Version || stored.terminal != nil {
			return fault(f.ConfirmationStale)
		}
	} else if !run.allJoined || !run.retired || !directTextTerminalMatches(row, stored, run.terminal) {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (s *directTextState) lifecycleEvent(run *directTextCall, status c.Status, reason string, version f.Version, through f.Sequence, at f.Instant) (event.Event, error) {
	id, err := f.NewID[event.EventIdentity]()
	if err != nil {
		return event.Event{}, unavailable(err)
	}
	project, err := f.ParseID[event.Project](run.request.Launch.ProjectID.String())
	if err != nil {
		return event.Event{}, err
	}
	aggregate, err := f.ParseID[event.Aggregate](run.request.ExecutionID.String())
	if err != nil {
		return event.Event{}, err
	}
	name := map[c.Status]event.StableName{c.Running: c.ExecutionStartedName, c.Succeeded: c.ExecutionSucceededName, c.Failed: c.ExecutionFailedName, c.Cancelled: c.ExecutionCancelledName}[status]
	snap, round := run.snapshot.Fields(), run.round.Fields()
	p := c.DirectTextLifecycle{ExecutionID: run.request.ExecutionID, AgentID: run.request.Launch.AgentID, StartID: snap.StartID, SnapshotID: snap.ID, RoundID: round.ID, CallID: round.CallID, Status: status, Reason: reason, TranscriptThrough: through}
	return s.deps.Lifecycle.New(event.Header{EventID: id, EventType: name, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: c.ExecutionAggregate, AggregateID: aggregate, AggregateVersion: &version}, p)
}

var _ oc.ProducerAuthority = (*DirectTextEventAuthority)(nil)
