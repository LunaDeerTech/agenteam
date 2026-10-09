package skill

import (
	"bytes"
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// LifecycleAuditAuthority is an immutable outer route for exact Skills Object
// deletion. Its embedded delegate keeps all other ordinary/initialization
// append, lookup, project-read and Audit-cleanup behavior unchanged.
type LifecycleAuditAuthority struct {
	ac.ProjectAuthority
	authority *Authority
	objects   ac.ProjectFactAuthority
}

func NewLifecycleAuditAuthority(delegate ac.ProjectAuthority, authority *Authority, objects ac.ProjectFactAuthority) (*LifecycleAuditAuthority, error) {
	if nilPort(delegate) || authority.state() == nil || nilPort(objects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &LifecycleAuditAuthority{ProjectAuthority: delegate, authority: authority, objects: objects}, nil
}

func (a *LifecycleAuditAuthority) CheckAppendInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a == nil || nilPort(a.ProjectAuthority) || a.authority.state() == nil || nilPort(a.objects) {
		return fault(f.DependencyUnbound)
	}
	if entry.Validate() != nil || key.Validate() != nil || !tx.Valid() {
		return invalid()
	}
	e, k := entry.Fields(), key.Details()
	if e.Action != ac.ObjectDelete || k.Producer != ac.ObjectProducer {
		return a.ProjectAuthority.CheckAppendInTx(ctx, tx, entry, key)
	}
	var meta skillAuditMetadata
	if err := json.Unmarshal(e.Metadata.JSON(), &meta); err != nil {
		return unavailable(err)
	}
	if meta.InitiatorKind != id.Service {
		return a.ProjectAuthority.CheckAppendInTx(ctx, tx, entry, key)
	}
	object, err := f.ParseID[oc.StoredObject](e.Resource.Details().ID)
	if err != nil {
		return fault(f.Forbidden)
	}
	state := a.authority.state()
	if err = state.store.RequireHeldLocks(ctx, tx, []f.LockRequest{objectLock(object, f.Exclusive)}); err != nil {
		return portError(err)
	}
	x, err := state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	// Route by the actual retained initialization, never a caller's owner tag.
	project, err := cleanupProjectForObject(ctx, x, object)
	if err != nil {
		if known, ok := err.(*f.Fault); ok && known.Code == f.DependencyUnbound {
			return a.ProjectAuthority.CheckAppendInTx(ctx, tx, entry, key)
		}
		return err
	}
	c, err := loadCleanup(ctx, x, project)
	if err != nil {
		return err
	}
	if c == nil {
		return a.ProjectAuthority.CheckAppendInTx(ctx, tx, entry, key)
	}
	r, err := loadInitialization(ctx, x, project)
	if err != nil {
		return err
	}
	if r == nil || c.phase != cleanupGated && c.phase != cleanupPending || !c.matches(*r) {
		return fault(f.Forbidden)
	}
	if err = a.authority.checkCleanupRowInTx(ctx, tx, *r, *c); err != nil {
		return err
	}
	actor, scope := e.Actor.Details(), e.Scope.Details()
	if scope.Kind != id.ProjectScope || scope.ProjectID != project.String() || actor.Kind != id.Service || actor.ServiceName != id.ObjectService || actor.ProjectID != project.String() || actor.CauseRef != k.CauseRef || f.Digest(k.CauseRef).Validate() != nil || k.Ordinal != 1 || e.Resource.Details().Kind != ac.ObjectResource || e.Associations != (ac.Associations{}) || e.Outcome != ac.Success || meta.ObjectID != object.String() || meta.InitiatorID != r.request.CreationID.String() || meta.InitiatorExecutionID != "" || meta.Phase != ac.DeletedPhase || meta.Reason != "" {
		return fault(f.Forbidden)
	}
	expected, err := ac.ObjectMetadata(ac.ObjectDelete, ac.ObjectMetadataFields{ObjectID: object.String(), InitiatorKind: id.Service, InitiatorID: r.request.CreationID.String(), MediaType: sc.PackageMediaType, ByteSize: r.bundle.size, Phase: ac.DeletedPhase})
	if err != nil || !bytes.Equal(expected.JSON(), e.Metadata.JSON()) {
		return fault(f.Forbidden)
	}
	// Only the actual D05 checker can prove its native call, earliest original
	// cause/digest and physical facts. Keep context, Tx and opaque entries exact.
	return portError(a.objects.CheckProjectAuditInTx(ctx, tx, entry, key))
}

var _ ac.ProjectAuthority = (*LifecycleAuditAuthority)(nil)
