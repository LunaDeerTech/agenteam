package project

import (
	"context"
	"errors"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func (a *Authority) lifecycleEventFact(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, summary event.Summary, project c.ProjectID) (eventFact, error) {
	var name c.CommandName
	var key foundation.IdempotencyKey
	err := x.QueryRow(ctx, `SELECT command_name,key FROM agenteam_project.commands WHERE project_id=$1 AND command_name IN ('archive','delete') AND event_id=$2`, project.String(), summary.Header.EventID.String()).Scan(&name, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventFact{}, fault(foundation.Forbidden)
	}
	if err != nil {
		return eventFact{}, unavailable(err)
	}
	r, err := loadLifecycleCommand(ctx, x, project, name, key)
	if err != nil {
		return eventFact{}, err
	}
	if r == nil || actor.Details().Kind != identity.Human || r.user != actor.Details().UserID {
		return eventFact{}, fault(foundation.Forbidden)
	}
	if err = exactEvent(summary, r.plan.Header, r.plan.Payload); err != nil {
		return eventFact{}, err
	}
	return eventFact{lifecycle: r, project: project, identity: r.identity(), opaque: r.id, locks: []foundation.LockRequest{userLock(r.user, foundation.Exclusive)}}, nil
}

func (a *Authority) validateLifecycleEvent(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, actor identity.Actor, p *projectRecord, command *lifecycleCommandRecord, stage oc.Stage) error {
	// Lifecycle state is intentionally not an ordinary Read/Mutate grant.
	// Current Session and Owner are still checked before either append stage.
	if _, err := a.current(ctx, tx, actor, command.project, foundation.Exclusive); err != nil {
		return err
	}
	if !p.initialized || p.ref.OwnerUserID.String() != actor.Details().UserID || command.user != actor.Details().UserID {
		return fault(foundation.NotFound)
	}
	if stage == oc.NewFact {
		return validateLifecycleAcceptedFact(ctx, x, p, command)
	}
	return nil
}

func validateLifecycleAcceptedFact(ctx context.Context, x postgres.SQLExecutor, p *projectRecord, command *lifecycleCommandRecord) error {
	plan := command.plan
	if command.state != "planned" || !p.initialized || p.ref.ID != command.project || p.ref.OwnerUserID.String() != command.user || p.ref.Lifecycle != plan.To || p.ref.Version != *command.header.AggregateVersion || p.operation == nil || *p.operation != plan.OperationID || !p.ref.UpdatedAt.Time().Equal(command.header.OccurredAt.Time()) {
		return fault(foundation.InvalidState)
	}
	r, err := loadLifecycleOperation(ctx, x, command.project, plan.OperationID)
	if err != nil {
		return err
	}
	if r == nil || r.owner != command.user || r.operation.Action != plan.Action || r.operation.State != c.OperationAccepted || r.operation.Version != 1 || r.operation.ProjectVersion != p.ref.Version || r.digest != plan.ManifestDigest || r.operation.SafeReason != "" || !r.operation.CreatedAt.Time().Equal(command.header.OccurredAt.Time()) || !r.operation.UpdatedAt.Time().Equal(r.operation.CreatedAt.Time()) {
		return fault(foundation.InvalidState)
	}
	return nil
}
