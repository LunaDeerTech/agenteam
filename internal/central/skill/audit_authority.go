package skill

import (
	"bytes"
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// InitializationAuditFacts combines Skill's exact original mapping with the
// actual same-Store Object witness checker. The interface permits controlled
// domain tests; a nonnil interface does not certify the production composition.
type InitializationAuditFacts struct {
	authority *Authority
	objects   ac.ProjectFactAuthority
}

func NewInitializationAuditFacts(authority *Authority, objects ac.ProjectFactAuthority) (*InitializationAuditFacts, error) {
	if authority.state() == nil || nilPort(objects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &InitializationAuditFacts{authority: authority, objects: objects}, nil
}

type skillAuditMetadata struct {
	ObjectID             string          `json:"object_id"`
	InitiatorKind        id.ActorKind    `json:"initiator_kind"`
	InitiatorID          string          `json:"initiator_id"`
	InitiatorExecutionID string          `json:"initiator_execution_id"`
	Phase                ac.ContentPhase `json:"phase"`
	Reason               ac.Reason       `json:"reason"`
}

func (a *InitializationAuditFacts) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a == nil || a.authority.state() == nil || nilPort(a.objects) {
		return fault(f.DependencyUnbound)
	}
	if !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	e, k := entry.Fields(), key.Details()
	actor, scope := e.Actor.Details(), e.Scope.Details()
	if k.Producer != ac.ObjectProducer || scope.Kind != id.ProjectScope || actor.Kind != id.Service || actor.ServiceName != id.ObjectService || actor.CauseRef != k.CauseRef || actor.ProjectID != scope.ProjectID || e.Resource.Details().Kind != ac.ObjectResource || e.Associations != (ac.Associations{}) {
		return fault(f.Forbidden)
	}
	var meta skillAuditMetadata
	if err := json.Unmarshal(e.Metadata.JSON(), &meta); err != nil {
		return unavailable(err)
	}
	if meta.InitiatorKind != id.Service || meta.InitiatorExecutionID != "" || meta.ObjectID != e.Resource.Details().ID {
		return fault(f.Forbidden)
	}
	project, err := f.ParseID[id.Project](scope.ProjectID)
	if err != nil {
		return fault(f.Forbidden)
	}
	creation, err := f.ParseID[pc.Creation](meta.InitiatorID)
	if err != nil {
		return fault(f.Forbidden)
	}
	object, err := f.ParseID[oc.StoredObject](meta.ObjectID)
	if err != nil {
		return fault(f.Forbidden)
	}
	state := a.authority.state()
	if err = state.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive), objectLock(object, f.Exclusive)}); err != nil {
		return portError(err)
	}
	x, err := state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadInitialization(ctx, x, project)
	if err != nil {
		return err
	}
	if row == nil || row.request.ProjectID != project || row.request.CreationID != creation || row.object != object || row.upload.Validate() != nil || row.phase == initializationPlanned {
		return fault(f.Forbidden)
	}
	locks, err := row.locks(f.Exclusive, object)
	if err != nil {
		return err
	}
	if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	registration, err := id.RegisterService(id.ProjectInitialization)
	if err != nil {
		return unavailable(err)
	}
	initiator, err := registration.Actor(creation.String(), e.Scope)
	if err != nil {
		return unavailable(err)
	}
	if e.Action == ac.ObjectUploadComplete {
		if row.phase != initializationReserved || k.CauseRef != row.upload.String() || k.Ordinal != 0 || e.Outcome != ac.Success || meta.Phase != ac.PublishedPhase || meta.Reason != "" {
			return fault(f.Forbidden)
		}
		if err = state.projects.ValidateInitializationInTx(ctx, tx, initiator, creation, project, row.request.InitializationKey); err != nil {
			return portError(err)
		}
		existence, _, err := skillExistence(ctx, x, *row)
		if err != nil {
			return err
		}
		if existence != oc.ExistingOwner {
			return fault(f.Forbidden)
		}
	} else {
		if err = state.projects.ValidateInitializationConvergenceInTx(ctx, tx, initiator, row.request); err != nil {
			return portError(err)
		}
		switch e.Action {
		case ac.ObjectUploadFailed:
			attempt, parseErr := f.ParseID[oc.Attempt](k.CauseRef)
			if parseErr != nil || (k.Ordinal != 0 && k.Ordinal != 1) || e.Outcome != ac.Unknown || meta.Phase != ac.FailedPhase || (meta.Reason != ac.PayloadMissing && meta.Reason != ac.IntegrityMismatch && meta.Reason != ac.StorageUnavailable) {
				return fault(f.Forbidden)
			}
			// An old candidate may finish after a newer one was reserved. Only
			// its retained exact mapping authorizes that attempt's failure fact.
			var matches bool
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.object_attempts WHERE attempt_id=$1 AND project_id=$2 AND creation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7)`, attempt.String(), project.String(), creation.String(), row.skill.String(), row.revision.String(), object.String(), row.upload.String()).Scan(&matches)
			if err != nil {
				return unavailable(err)
			}
			if !matches {
				return fault(f.Forbidden)
			}
		case ac.ObjectDelete:
			if f.Digest(k.CauseRef).Validate() != nil || k.Ordinal != 1 || e.Outcome != ac.Success || meta.Phase != ac.DeletedPhase || meta.Reason != "" {
				return fault(f.Forbidden)
			}
		default:
			return fault(f.Forbidden)
		}
	}
	// Compare the complete typed metadata, including measured package size and
	// media; the Object checker separately proves original native witness facts.
	expected, err := ac.ObjectMetadata(e.Action, ac.ObjectMetadataFields{ObjectID: object.String(), InitiatorKind: id.Service, InitiatorID: creation.String(), MediaType: sc.PackageMediaType, ByteSize: row.bundle.size, Phase: meta.Phase, Reason: meta.Reason})
	if err != nil || !bytes.Equal(expected.JSON(), e.Metadata.JSON()) {
		return fault(f.Forbidden)
	}
	return portError(a.objects.CheckProjectAuditInTx(ctx, tx, entry, key))
}

var _ ac.ProjectFactAuthority = (*InitializationAuditFacts)(nil)
