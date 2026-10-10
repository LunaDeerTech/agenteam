package agent

import (
	"context"
	"errors"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// EventAuthority is registered only for Agent's one typed producer. It shares
// the canonical Authority; neither event shape nor Project approval can create
// a writer witness for NewFact.
type EventAuthority struct {
	owner  *Authority
	events c.AgentEvents
	issuer oc.PlanIssuer
}

func NewEventAuthority(owner *Authority, events c.AgentEvents) (*EventAuthority, error) {
	if owner == nil || owner.state == nil || !events.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	return &EventAuthority{owner, events, oc.NewPlanIssuer()}, nil
}
func eventForCommand(events c.AgentEvents, r *commandRecord) (ec.Event, error) {
	if r == nil || r.validate() != nil || len(r.ChangedFields) == 0 {
		return ec.Event{}, fault(f.Forbidden)
	}
	// Exactly one config event belongs to a changing command. Reusing its UUID
	// across typed identities gives a persistent event identity across re-plan
	// and confirmation without a second mutable event ledger.
	id, err := f.ParseID[ec.EventIdentity](r.ID.String())
	if err != nil {
		return ec.Event{}, unavailable(err)
	}
	project, err := f.ParseID[ec.Project](r.Project.String())
	if err != nil {
		return ec.Event{}, unavailable(err)
	}
	agent, err := f.ParseID[ec.Aggregate](r.Target.String())
	if err != nil {
		return ec.Event{}, unavailable(err)
	}
	core := r.After.Fields().Core
	version := core.Version
	header := ec.Header{EventID: id, EventType: c.AgentConfigChangedEvent, SchemaVersion: 1, OccurredAt: core.UpdatedAt, Scope: ec.Scope{Kind: ec.ProjectScope, ProjectID: project}, AggregateType: c.AgentAggregate, AggregateID: agent, AggregateVersion: &version}
	operation := c.ConfigUpdated
	if r.Name == c.CreateAgentCommand {
		operation = c.ConfigCreated
	}
	return events.NewConfigChanged(header, c.ConfigChangedPayload{CommandID: r.ID, ActorUserID: r.User, Operation: operation, ChangedFields: slices.Clone(r.ChangedFields)})
}
func eventCoordinates(actor i.Actor, summary ec.Summary) (i.ProjectID, i.AgentID, error) {
	if err := currentActor(actor); err != nil {
		return i.ProjectID{}, i.AgentID{}, err
	}
	if summary.Producer != c.AgentProducer || summary.PayloadDigest.Validate() != nil || c.ValidateAgentEventHeader(summary.Header) != nil {
		return i.ProjectID{}, i.AgentID{}, fault(f.Forbidden)
	}
	project, e1 := f.ParseID[i.Project](summary.Header.Scope.ProjectID.String())
	agent, e2 := f.ParseID[i.Agent](summary.Header.AggregateID.String())
	if e1 != nil || e2 != nil {
		return i.ProjectID{}, i.AgentID{}, fault(f.Forbidden)
	}
	return project, agent, nil
}
func loadEventCommand(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, id ec.EventID) (*commandRecord, error) {
	var name string
	var key f.IdempotencyKey
	err := x.QueryRow(ctx, `SELECT command_name,idempotency_key FROM agenteam_agent.commands WHERE project_id=$1 AND id=$2`, project.String(), id.String()).Scan(&name, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fault(f.Forbidden)
	}
	if err != nil {
		return nil, unavailable(err)
	}
	return loadCommand(ctx, x, project, c.CommandName(name), key)
}
func (a *EventAuthority) binding(actor i.Actor, summary ec.Summary, r *commandRecord) (f.Digest, []f.LockRequest, error) {
	if r == nil || r.User.String() != actor.Details().UserID || r.State != "planned" {
		return "", nil, fault(f.Forbidden)
	}
	expected, err := eventForCommand(a.events, r)
	if err != nil {
		return "", nil, err
	}
	if !sameValue(expected.Summary(), summary) {
		return "", nil, fault(f.Forbidden)
	}
	mapping, err := recordMapping(r)
	if err != nil {
		return "", nil, err
	}
	binding, err := hash(struct {
		Actor   i.ActorDetails
		Summary ec.Summary
		Mapping f.Digest
	}{actor.Details(), summary, mapping})
	if err != nil {
		return "", nil, err
	}
	locks, err := oc.NormalizeLocks(ownerLocks(actor, r.Project, r.Target, r.identity()))
	return binding, locks, err
}
func readCause(kind string) (f.TransactionCause, error) {
	id, err := f.NewID[f.Request]()
	if err != nil {
		return f.TransactionCause{}, unavailable(err)
	}
	return f.NewRecoveryCause("agent."+kind, id.String(), "")
}
func (a *EventAuthority) DiscoverAppend(ctx context.Context, actor i.Actor, summary ec.Summary) (oc.Dependencies, error) {
	if a == nil || a.owner == nil || a.owner.state == nil {
		return oc.Dependencies{}, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return oc.Dependencies{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return oc.Dependencies{}, err
	}
	project, agent, err := eventCoordinates(actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	cause, err := readCause("event-discovery")
	if err != nil {
		return oc.Dependencies{}, err
	}
	var deps oc.Dependencies
	result := a.owner.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		locks := []f.LockRequest{userLock(actor, f.Shared), projectLock(project, f.Shared), agentLock(agent, f.Shared)}
		if err = a.owner.state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		grant, err := a.owner.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		x, err := a.owner.state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		r, err := loadEventCommand(ctx, x, project, summary.Header.EventID)
		if err != nil {
			return err
		}
		binding, needed, err := a.binding(actor, summary, r)
		if err != nil {
			return err
		}
		deps, err = oc.NewDependencies(a.issuer, binding, needed, []byte(r.ID.String()))
		return err
	})
	if err := commitError(result); err != nil {
		return oc.Dependencies{}, err
	}
	return deps, nil
}
func (a *EventAuthority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary ec.Summary, deps oc.Dependencies, stage oc.Stage) error {
	if a == nil || a.owner == nil || a.owner.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	project, agent, err := eventCoordinates(actor, summary)
	if err != nil {
		return err
	}
	if !stage.Valid() || deps.Validate() != nil || !deps.Matches(a.issuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	x, err := a.owner.state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = a.owner.state.store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return portError(err)
	}
	base := []f.LockRequest{userLock(actor, f.Exclusive), projectLock(project, f.Shared), agentLock(agent, f.Exclusive)}
	if err = a.owner.state.store.RequireHeldLocks(ctx, tx, base); err != nil {
		return portError(err)
	}
	grant, err := a.owner.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	r, err := loadEventCommand(ctx, x, project, summary.Header.EventID)
	if err != nil {
		return err
	}
	binding, locks, err := a.binding(actor, summary, r)
	if err != nil {
		return err
	}
	if !deps.Matches(a.issuer, binding) || string(deps.Opaque()) != r.ID.String() || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	if stage == oc.CurrentAccess {
		return nil
	}
	mapping, err := recordMapping(r)
	if err != nil {
		return err
	}
	_, err = a.owner.checkApplied(ctx, tx, actor, project, agent, r.identity(), mapping)
	return err
}

var _ oc.ProducerAuthority = (*EventAuthority)(nil)
