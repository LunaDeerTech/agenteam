package project

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func (a *Authority) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	if a.state() == nil {
		return fault(foundation.DependencyUnbound)
	}
	if !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	f, k := entry.Fields(), key.Details()
	if !audit.ProjectAction(f.Action) {
		return fault(foundation.DependencyUnbound)
	}
	if f.Scope.Details().Kind != identity.ProjectScope || k.Producer != audit.ProjectProducer || k.Ordinal != 0 || f.Associations != (audit.Associations{}) {
		return fault(foundation.Forbidden)
	}
	project, e := parseID[identity.Project](f.Scope.Details().ProjectID)
	if e != nil {
		return e
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{projectLock(project, foundation.Exclusive)}); e != nil {
		return unavailable(e)
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	p, e := loadProject(ctx, x, project)
	if e != nil {
		return e
	}
	if p == nil {
		return fault(foundation.NotFound)
	}
	metadata, e := f.Metadata.ProjectFields()
	if e != nil {
		return e
	}
	if metadata.ProjectID != project.String() || metadata.InitiatorID != p.ref.OwnerUserID.String() {
		return fault(foundation.Forbidden)
	}
	switch f.Action {
	case audit.ProjectCreateAccepted, audit.ProjectCreateCompleted:
		r, e := projectCreation(ctx, x, project)
		if e != nil {
			return e
		}
		if r == nil || p.creation != r.operation.ID {
			return fault(foundation.Forbidden)
		}
		if e = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{commandLock(r.identity())}); e != nil {
			return unavailable(e)
		}
		v := r.operation.Version
		expected, e := audit.ProjectMetadata(f.Action, audit.ProjectMetadataFields{ProjectID: project.String(), InitiatorID: r.owner.String(), ProjectVersion: 1, CreationID: r.operation.ID.String(), CreationVersion: &v})
		if e != nil {
			return e
		}
		if !sameMetadata(expected, f.Metadata) {
			return fault(foundation.Forbidden)
		}
		if f.Action == audit.ProjectCreateAccepted {
			if _, e = a.current(ctx, tx, f.Actor, project, foundation.Exclusive); e != nil {
				return e
			}
			if e = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{userLock(f.Actor.Details().UserID, foundation.Exclusive)}); e != nil {
				return unavailable(e)
			}
			if f.Actor.Details().UserID != r.owner.String() || r.operation.State != c.CreationAccepted || r.operation.Version != 1 || p.initialized || p.ref.Lifecycle != c.Active || k.CauseRef != commandAuditCause(r.identity()) {
				return fault(foundation.Forbidden)
			}
		} else {
			if k.CauseRef != r.operation.ID.String() || r.operation.State != c.CreationCompleted || r.protectedSkill == nil || r.revision == nil || !p.initialized || p.ref.Version != 1 {
				return fault(foundation.Forbidden)
			}
			if e = a.ValidateInitializationInTx(ctx, tx, f.Actor, r.operation.ID, project, r.initializationKey); e != nil {
				return e
			}
		}
		return nil
	case audit.ProjectUpdate:
		if _, e = a.current(ctx, tx, f.Actor, project, foundation.Exclusive); e != nil {
			return e
		}
		if e = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{userLock(f.Actor.Details().UserID, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		if p.ref.OwnerUserID.String() != f.Actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		if e = c.CheckOwnerGate(p.ref.Lifecycle, c.Initialized, identity.Mutate); e != nil {
			return e
		}
		if !p.initialized {
			return fault(foundation.InvalidState)
		}
		var keyValue foundation.IdempotencyKey
		e = x.QueryRow(ctx, `SELECT key FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND audit_cause=$2`, project.String(), k.CauseRef).Scan(&keyValue)
		if errors.Is(e, pgx.ErrNoRows) {
			return fault(foundation.Forbidden)
		}
		if e != nil {
			return unavailable(e)
		}
		cmd, e := loadCommand(ctx, x, project, c.UpdateCommand, keyValue)
		if e != nil {
			return e
		}
		if cmd == nil || cmd.state != "planned" || cmd.plan == nil || cmd.user != f.Actor.Details().UserID || !reflect.DeepEqual(cmd.plan.Project, p.ref) || k.CauseRef != commandAuditCause(cmd.identity()) {
			return fault(foundation.Forbidden)
		}
		if e = a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{commandLock(cmd.identity())}); e != nil {
			return unavailable(e)
		}
		changed := make([]audit.ProjectChangedField, len(cmd.plan.Changed))
		for i, v := range cmd.plan.Changed {
			changed[i] = audit.ProjectChangedField(v)
		}
		expected, e := audit.ProjectMetadata(f.Action, audit.ProjectMetadataFields{ProjectID: project.String(), InitiatorID: cmd.user, ProjectVersion: p.ref.Version, ChangedFields: changed})
		if e != nil {
			return e
		}
		if !sameMetadata(expected, f.Metadata) {
			return fault(foundation.Forbidden)
		}
		return nil
	case audit.ProjectArchiveAccepted, audit.ProjectDeleteAccepted:
		return a.checkLifecycleAcceptedAudit(ctx, tx, x, p, entry, key)
	default:
		// B03 owns lifecycle acceptance/completion and other domain cause
		// validators. Registering the typed schema cannot stand in for them.
		return fault(foundation.DependencyUnbound)
	}
}
func sameMetadata(a, b audit.Metadata) bool {
	x, e := json.Marshal(a)
	if e != nil {
		return false
	}
	y, e := json.Marshal(b)
	return e == nil && string(x) == string(y)
}
func (a *Authority) CheckServiceLookup(ctx context.Context, actor identity.Actor, scope identity.Scope, key audit.AppendKey) error {
	// B02 uses Append's same-Tx idempotency, not privileged Audit browsing.
	// Future service inspection must bind its exact cause through B03.
	return fault(foundation.DependencyUnbound)
}
func (a *Authority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause audit.LifecycleCause, project identity.ProjectID) error {
	// Cleanup sequencing belongs to the later participant/finalizer adapter.
	return fault(foundation.DependencyUnbound)
}

var _ audit.ProjectAuthority = (*Authority)(nil)
