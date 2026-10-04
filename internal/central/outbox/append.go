package outbox

import (
	"context"
	"errors"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func (s *Service) PrepareAppend(ctx context.Context, actor identity.Actor, e event.Event) (oc.AppendPlan, error) {
	r := s.state()
	if actor.Validate() != nil || !r.catalog.Owns(e) {
		return oc.AppendPlan{}, invalid()
	}
	summary := e.Summary()
	provider := r.auth.Producers[summary.Producer]
	if nilPort(provider) {
		return oc.AppendPlan{}, failure(foundation.DependencyUnbound, nil)
	}
	producer, err := provider.DiscoverAppend(ctx, actor, summary)
	if err != nil {
		return oc.AppendPlan{}, portError(err)
	}
	if producer.Validate() != nil {
		return oc.AppendPlan{}, unavailable(nil)
	}
	locks, err := actorLocks(actor)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	locks = append(locks, foundation.LockRequest{Key: registryLock(), Mode: foundation.Shared}, foundation.LockRequest{Key: eventLock(summary.Header.EventID), Mode: foundation.Exclusive})
	locks = append(locks, producer.Locks()...)
	var project oc.Dependencies
	if summary.Header.Scope.Kind == event.ProjectScope {
		if nilPort(r.auth.Projects) {
			return oc.AppendPlan{}, failure(foundation.DependencyUnbound, nil)
		}
		request, err := appendProjectRequest(actor, summary, oc.CurrentAccess)
		if err != nil {
			return oc.AppendPlan{}, err
		}
		project, err = r.auth.Projects.Discover(ctx, request)
		if err != nil {
			return oc.AppendPlan{}, portError(err)
		}
		if project.Validate() != nil {
			return oc.AppendPlan{}, unavailable(nil)
		}
		key, _ := foundation.ProjectLock(summary.Header.Scope.ProjectID.String())
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
		locks = append(locks, project.Locks()...)
	}
	digest, err := oc.SemanticDigest(actor, e)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	return oc.NewAppendPlan(r.issuer, oc.AppendPlanDetails{Event: e, Semantic: digest, Producer: producer, Project: project, Locks: locks})
}
func AppendLocks(plan oc.AppendPlan) []foundation.LockRequest { return plan.Locks() }
func appendProjectRequest(actor identity.Actor, summary event.Summary, stage oc.Stage) (oc.ProjectRequest, error) {
	id, err := foundation.ParseID[identity.Project](summary.Header.Scope.ProjectID.String())
	if err != nil {
		return oc.ProjectRequest{}, invalid()
	}
	return oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: id, Actor: actor, Stage: stage, Event: summary})
}
func (s *Service) validateAppend(ctx context.Context, tx foundation.Tx, actor identity.Actor, e event.Event, d oc.AppendPlanDetails, stage oc.Stage) error {
	r := s.state()
	p := r.auth.Producers[e.Summary().Producer]
	if nilPort(p) {
		return failure(foundation.DependencyUnbound, nil)
	}
	if err := p.ValidateAppendInTx(ctx, tx, actor, e.Summary(), d.Producer, stage); err != nil {
		return portError(err)
	}
	if e.Header().Scope.Kind == event.ProjectScope {
		if nilPort(r.auth.Projects) {
			return failure(foundation.DependencyUnbound, nil)
		}
		request, err := appendProjectRequest(actor, e.Summary(), stage)
		if err != nil {
			return err
		}
		if err = r.auth.Projects.ValidateInTx(ctx, tx, request, d.Project); err != nil {
			return portError(err)
		}
	}
	return nil
}
func (s *Service) AppendEventInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, e event.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	r := s.state()
	if !r.catalog.Owns(e) || !plan.Matches(r.issuer, actor, e) {
		return oc.AppendReceipt{}, invalid()
	}
	x, err := executor(s, tx)
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	d := plan.Details()
	if err = r.store.RequireHeldLocks(ctx, tx, d.Locks); err != nil {
		return oc.AppendReceipt{}, unavailable(err)
	}
	if err = s.validateAppend(ctx, tx, actor, e, d, oc.CurrentAccess); err != nil {
		return oc.AppendReceipt{}, err
	}
	if err = appendLocalGate(ctx, x, e.Header().Scope, actor); err != nil {
		return oc.AppendReceipt{}, err
	}
	var sequence int64
	var saved string
	err = x.QueryRow(ctx, `SELECT sequence,semantic_digest FROM agenteam_outbox.events WHERE id=$1`, e.Header().EventID.String()).Scan(&sequence, &saved)
	if err == nil {
		if saved != d.Semantic.String() {
			return oc.AppendReceipt{}, failure(foundation.IdempotencyKeyReused, nil)
		}
		if sequence < 1 {
			return oc.AppendReceipt{}, unavailable(nil)
		}
		return oc.AppendReceipt{EventID: e.Header().EventID, Sequence: foundation.Sequence(sequence)}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return oc.AppendReceipt{}, unavailable(err)
	}
	if err = s.validateAppend(ctx, tx, actor, e, d, oc.NewFact); err != nil {
		return oc.AppendReceipt{}, err
	}
	header, err := e.HeaderJSON()
	if err != nil {
		return oc.AppendReceipt{}, invalid()
	}
	actorKey, err := oc.StableActor(actor)
	if err != nil {
		return oc.AppendReceipt{}, err
	}
	h := e.Header()
	var project any
	if h.Scope.Kind == event.ProjectScope {
		project = h.Scope.ProjectID.String()
	}
	var version, aggregateSequence any
	if h.AggregateVersion != nil {
		version = int64(*h.AggregateVersion)
	}
	if h.AggregateSequence != nil {
		aggregateSequence = int64(*h.AggregateSequence)
	}
	err = x.QueryRow(ctx, `INSERT INTO agenteam_outbox.events(id,producer,event_type,schema_version,scope,project_id,aggregate_type,aggregate_id,aggregate_version,aggregate_sequence,occurred_at,header,payload,semantic_digest,stable_actor) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$14,$15) RETURNING sequence`, h.EventID.String(), string(e.Summary().Producer), string(h.EventType), int64(h.SchemaVersion), string(h.Scope.Kind), project, string(h.AggregateType), h.AggregateID.String(), version, aggregateSequence, h.OccurredAt.Time(), string(header), e.PayloadBytes(), d.Semantic.String(), actorKey).Scan(&sequence)
	if err != nil {
		return oc.AppendReceipt{}, unavailable(err)
	}
	rows, err := x.Query(ctx, `SELECT handler_id FROM agenteam_outbox.subscriptions WHERE event_type=$1 AND accepted_after_sequence<$2 ORDER BY handler_id LIMIT 129`, string(h.EventType), sequence)
	if err != nil {
		return oc.AppendReceipt{}, unavailable(err)
	}
	var handlers []string
	for rows.Next() {
		var handler string
		if err = rows.Scan(&handler); err != nil {
			rows.Close()
			return oc.AppendReceipt{}, unavailable(err)
		}
		handlers = append(handlers, handler)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(handlers) > 128 {
		return oc.AppendReceipt{}, unavailable(err)
	}
	for _, handler := range handlers {
		id, err := foundation.NewID[oc.Delivery]()
		if err != nil {
			return oc.AppendReceipt{}, unavailable(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.deliveries(id,event_id,handler_id,scope,project_id) VALUES($1,$2,$3,$4,$5)`, id.String(), h.EventID.String(), handler, string(h.Scope.Kind), project); err != nil {
			return oc.AppendReceipt{}, unavailable(err)
		}
	}
	return oc.AppendReceipt{EventID: h.EventID, Sequence: foundation.Sequence(sequence)}, nil
}
func appendLocalGate(ctx context.Context, e postgres.SQLExecutor, scope event.Scope, actor identity.Actor) error {
	if scope.Kind != event.ProjectScope {
		return nil
	}
	var action, phase string
	err := e.QueryRow(ctx, `SELECT action,phase FROM agenteam_outbox.project_lifecycle WHERE project_id=$1`, scope.ProjectID.String()).Scan(&action, &phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	if action == "delete" {
		if phase == "cleaning" || phase == "completed" {
			return failure(foundation.ResourceDeleted, nil)
		}
		if actor.Details().Kind != identity.Service {
			return failure(foundation.ProjectNotActive, nil)
		}
	}
	// Archive is an old operation checkpoint, not an irreversible gate. The
	// current Project provider decides active/archived/restore at each stage.
	return nil
}
