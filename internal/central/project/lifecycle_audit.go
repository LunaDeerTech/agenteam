package project

import (
	"context"
	"errors"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func lifecycleAcceptedMetadata(command *lifecycleCommandRecord) (audit.Action, audit.Metadata, error) {
	action := audit.ProjectArchiveAccepted
	if command.name == c.DeleteCommand {
		action = audit.ProjectDeleteAccepted
	}
	version := foundation.Version(1)
	metadata, err := audit.ProjectMetadata(action, audit.ProjectMetadataFields{
		ProjectID: command.project.String(), InitiatorID: command.user,
		ProjectVersion: *command.header.AggregateVersion, OperationID: command.plan.OperationID.String(), OperationVersion: &version,
		From: string(command.plan.From), To: string(command.plan.To), Action: string(command.plan.Action),
	})
	return action, metadata, err
}

func (s *Service) appendLifecycleAcceptedAudit(ctx context.Context, tx foundation.Tx, actor identity.Actor, command *lifecycleCommandRecord) error {
	action, metadata, err := lifecycleAcceptedMetadata(command)
	if err != nil {
		return err
	}
	resource, err := audit.NewResource(audit.ProjectOperationResource, command.plan.OperationID.String())
	if err != nil {
		return err
	}
	scope, _ := identity.InProject(command.project)
	entry, err := audit.NewEntry(audit.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: audit.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		return err
	}
	key, err := auditCommandKey(command.identity(), 0)
	if err != nil {
		return err
	}
	_, err = s.state().deps.Audit.AppendInTx(ctx, tx, entry, key)
	return portError(err)
}

func (a *Authority) checkLifecycleAcceptedAudit(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, p *projectRecord, entry audit.Entry, key audit.AppendKey) error {
	f, k := entry.Fields(), key.Details()
	if _, err := a.current(ctx, tx, f.Actor, p.ref.ID, foundation.Exclusive); err != nil {
		return err
	}
	if p.ref.OwnerUserID.String() != f.Actor.Details().UserID {
		return fault(foundation.NotFound)
	}
	name := c.ArchiveCommand
	if f.Action == audit.ProjectDeleteAccepted {
		name = c.DeleteCommand
	}
	var commandKey foundation.IdempotencyKey
	err := x.QueryRow(ctx, `SELECT key FROM agenteam_project.commands WHERE project_id=$1 AND command_name=$2 AND audit_cause=$3`, p.ref.ID.String(), string(name), k.CauseRef).Scan(&commandKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return fault(foundation.Forbidden)
	}
	if err != nil {
		return unavailable(err)
	}
	command, err := loadLifecycleCommand(ctx, x, p.ref.ID, name, commandKey)
	if err != nil {
		return err
	}
	if command == nil || command.user != f.Actor.Details().UserID || k.CauseRef != commandAuditCause(command.identity()) {
		return fault(foundation.Forbidden)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{commandLock(command.identity()), userLock(command.user, foundation.Exclusive)}); err != nil {
		return unavailable(err)
	}
	if err = validateLifecycleAcceptedFact(ctx, x, p, command); err != nil {
		return err
	}
	action, expected, err := lifecycleAcceptedMetadata(command)
	if err != nil {
		return err
	}
	if action != f.Action || !sameMetadata(expected, f.Metadata) {
		return fault(foundation.Forbidden)
	}
	return nil
}
