package project

import (
	"context"
	"encoding/json"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// NewInitializationAuditAuthority adds only the original initialization's
// Object audit route. facts must combine real Skill initialization/key and
// exact object/attempt mapping with the same-Store Object audit checker. A
// non-nil interface alone cannot establish that composition; callers must not
// bind this wrapper in production until those dependencies are available.
//
// The provider receives the original context (including Object's private
// witness), transaction, entry and key. Neither this wrapper nor its provider
// may acquire omitted locks, own another transaction or treat a Service name
// as proof of initialization. Ordinary authority methods remain unchanged.
func NewInitializationAuditAuthority(authority *Authority, facts audit.ProjectFactAuthority) (audit.ProjectAuthority, error) {
	if authority.state() == nil || nilPort(facts) {
		return nil, fault(foundation.DependencyUnbound)
	}
	return &initializationAuditAuthority{authority: authority, facts: facts}, nil
}

type initializationAuditAuthority struct {
	authority *Authority
	facts     audit.ProjectFactAuthority
}

func (a *initializationAuditAuthority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, project identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	return a.authority.AuthorizeProject(ctx, tx, actor, project, intent)
}

func (a *initializationAuditAuthority) CheckServiceLookup(ctx context.Context, actor identity.Actor, scope identity.Scope, key audit.AppendKey) error {
	return a.authority.CheckServiceLookup(ctx, actor, scope, key)
}

func (a *initializationAuditAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause audit.LifecycleCause, project identity.ProjectID) error {
	return a.authority.CheckCleanupInTx(ctx, tx, actor, cause, project)
}

// This is a projection of already validated, immutable typed metadata, never
// an alternate JSON admission path or a source of transferable authority.
type initializationAuditMetadata struct {
	ObjectID             string             `json:"object_id"`
	InitiatorKind        identity.ActorKind `json:"initiator_kind"`
	InitiatorID          string             `json:"initiator_id"`
	InitiatorExecutionID string             `json:"initiator_execution_id"`
	Phase                audit.ContentPhase `json:"phase"`
	Reason               audit.Reason       `json:"reason"`
}

func (a *initializationAuditAuthority) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
	if !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	f, k := entry.Fields(), key.Details()
	if k.Producer != audit.ObjectProducer || f.Action != audit.ObjectUploadComplete && f.Action != audit.ObjectUploadFailed && f.Action != audit.ObjectDelete {
		return a.authority.CheckAppendInTx(ctx, tx, entry, key)
	}
	var metadata initializationAuditMetadata
	if err := json.Unmarshal(f.Metadata.JSON(), &metadata); err != nil {
		return unavailable(err)
	}
	if metadata.InitiatorKind != identity.Service {
		return a.authority.CheckAppendInTx(ctx, tx, entry, key)
	}
	actor, scope, resource := f.Actor.Details(), f.Scope.Details(), f.Resource.Details()
	if scope.Kind != identity.ProjectScope || actor.Kind != identity.Service || actor.ServiceName != identity.ObjectService || actor.ProjectID != scope.ProjectID || actor.CauseRef != k.CauseRef || resource.Kind != audit.ObjectResource || resource.ID != metadata.ObjectID || f.Associations != (audit.Associations{}) || metadata.InitiatorExecutionID != "" || !initializationAuditShape(f, k, metadata) {
		return fault(foundation.Forbidden)
	}
	projectID, err := foundation.ParseID[identity.Project](scope.ProjectID)
	if err != nil {
		return fault(foundation.Forbidden)
	}
	creationID, err := foundation.ParseID[c.Creation](metadata.InitiatorID)
	if err != nil {
		return fault(foundation.Forbidden)
	}
	store := a.authority.state().store
	if err := store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{projectLock(projectID, foundation.Exclusive)}); err != nil {
		return unavailable(err)
	}
	x, err := store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	creation, err := loadCreation(ctx, x, creationID)
	if err != nil {
		return err
	}
	if creation == nil {
		return fault(foundation.Forbidden)
	}
	if creation.operation.ID != creationID {
		return unavailable(nil)
	}
	if creation.operation.ProjectID != projectID {
		return fault(foundation.Forbidden)
	}
	project, err := loadProject(ctx, x, projectID)
	if err != nil {
		return err
	}
	if project == nil {
		return fault(foundation.Forbidden)
	}
	if project.ref.ID != projectID || project.creation != creationID || project.ref.OwnerUserID != creation.owner {
		return unavailable(nil)
	}
	if project.ref.Lifecycle != c.Active {
		return fault(foundation.Forbidden)
	}
	if err := initializationConvergenceFacts(creation, project); err != nil {
		return err
	}
	if f.Action == audit.ObjectUploadComplete && creation.operation.State != c.CreationInitializing && creation.operation.State != c.CreationCompleted {
		return fault(foundation.InvalidState)
	}
	// The scanner validates the persisted initialization key. Its equality to
	// the Skill command's original key must be established by facts using the
	// original Project initialization gates, not by comparing this row to itself.
	return portError(a.facts.CheckProjectAuditInTx(ctx, tx, entry, key))
}

func initializationAuditShape(f audit.EntryFields, key audit.AppendKeyDetails, metadata initializationAuditMetadata) bool {
	switch f.Action {
	case audit.ObjectUploadComplete:
		_, err := foundation.ParseID[struct{}](key.CauseRef)
		return err == nil && key.Ordinal == 0 && f.Outcome == audit.Success && metadata.Phase == audit.PublishedPhase && metadata.Reason == ""
	case audit.ObjectUploadFailed:
		_, err := foundation.ParseID[struct{}](key.CauseRef)
		return err == nil && (key.Ordinal == 0 || key.Ordinal == 1) && f.Outcome == audit.Unknown && metadata.Phase == audit.FailedPhase && (metadata.Reason == audit.PayloadMissing || metadata.Reason == audit.IntegrityMismatch || metadata.Reason == audit.StorageUnavailable)
	case audit.ObjectDelete:
		return foundation.Digest(key.CauseRef).Validate() == nil && key.Ordinal == 1 && f.Outcome == audit.Success && metadata.Phase == audit.DeletedPhase && metadata.Reason == ""
	default:
		return false
	}
}

var _ audit.ProjectAuthority = (*initializationAuditAuthority)(nil)
