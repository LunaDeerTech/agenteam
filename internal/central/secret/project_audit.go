package secret

import (
	"context"
	"errors"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// ProjectAuditAuthority verifies only Secret-owned facts. Its caller must first
// authorize the current Project and actor through the real owning authority.
// It can be constructed before Audit and Secret services, without a cycle.
type ProjectAuditAuthority struct{ data func() Store }

func NewProjectAuditAuthority(store Store) (*ProjectAuditAuthority, error) {
	if nilPort(store) {
		return nil, failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	if !reflect.ValueOf(store).Comparable() {
		return nil, invalid()
	}
	return &ProjectAuditAuthority{data: func() Store { return store }}, nil
}

func projectAuditDenied() error {
	return failure(AuthorizationRejected, foundation.Forbidden, nil)
}

func (a *ProjectAuditAuthority) CheckProjectAuditInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a == nil || a.data == nil || nilPort(a.data()) {
		return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	if !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	f, k := entry.Fields(), key.Details()
	if f.Scope.Details().Kind != identity.ProjectScope || k.Producer != ac.SecretProducer || k.Ordinal != 0 || f.Resource.Details().Kind != ac.SecretResource || f.Outcome != ac.Success {
		return projectAuditDenied()
	}
	if f.Action != ac.SecretCreate && f.Action != ac.SecretUpdate && f.Action != ac.SecretDelete && f.Action != ac.SecretResolve {
		return projectAuditDenied()
	}
	w, ok := ctx.Value(projectAuditWitnessKey{}).(projectAuditWitness)
	store := a.data()
	if !ok || !sameProjectAuditStore(store, w.store) || w.tx != tx || !sameProjectAuditEntry(w.entry, entry) || w.key.Validate() != nil || w.key.Details() != k {
		return projectAuditDenied()
	}
	x, err := store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	if f.Action == ac.SecretResolve {
		if w.resolution == nil || w.mutation != nil {
			return projectAuditDenied()
		}
		return checkResolutionAudit(ctx, store, tx, x, entry, key, *w.resolution)
	}
	if w.mutation == nil || w.resolution != nil {
		return projectAuditDenied()
	}
	return checkMutationAudit(ctx, store, tx, x, entry, key, *w.mutation)
}

func checkMutationAudit(ctx context.Context, store Store, tx foundation.Tx, x postgres.SQLExecutor, entry ac.Entry, key ac.AppendKey, w projectMutationAudit) error {
	request := sc.WriteRequest{Actor: w.actor, Scope: w.ref.Details().Scope, Identity: w.command, Kind: w.kind, Ref: w.ref, ExpectedVersion: w.expected, Purpose: w.purpose}
	if w.kind == sc.Create {
		request.Ref = sc.CredentialRef{}
	}
	if validateWrite(request) != nil || w.ref.Validate() != nil || w.receiptID.Validate() != nil || w.receiptPayload.Validate() != nil || !w.result.Metadata.CredentialRef.Equal(w.ref) || w.result.Metadata.Purpose != w.purpose || w.result.Metadata.Version.Validate() != nil || w.result.Deleted != (w.kind == sc.Delete) {
		return projectAuditDenied()
	}
	expectedKey, err := audit.CommandAppendKey(ac.SecretProducer, w.command, 0)
	if err != nil || expectedKey.Details() != key.Details() {
		return projectAuditDenied()
	}
	locks, err := mutationLocks(w.command, w.ref, w.actor)
	if err != nil {
		return projectAuditDenied()
	}
	if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return unavailable(err)
	}
	action, changed := ac.SecretCreate, []ac.ChangedField{ac.ValueChanged, ac.PurposeChanged}
	if w.kind == sc.Create {
		if w.result.Metadata.Version != 1 || w.prior.CredentialRef.Validate() == nil || w.prior.Version != 0 || w.prior.Purpose != "" {
			return projectAuditDenied()
		}
	} else {
		if !w.prior.CredentialRef.Equal(w.ref) || !w.prior.Purpose.Valid() || w.prior.Version != w.expected || w.result.Metadata.Version <= w.expected || w.result.Metadata.Version-w.expected != 1 {
			return projectAuditDenied()
		}
		action, changed = ac.SecretUpdate, []ac.ChangedField{ac.ValueChanged}
		if w.prior.Purpose != w.purpose {
			changed = append(changed, ac.PurposeChanged)
		}
		if w.kind == sc.Delete {
			action, changed = ac.SecretDelete, nil
		}
	}
	metadata, err := ac.SecretMutationMetadata(action, w.result.Metadata.Version, changed)
	if err != nil {
		return projectAuditDenied()
	}
	resource, _ := ac.NewResource(ac.SecretResource, w.ref.Details().ID.String())
	expectedEntry, err := ac.NewEntry(ac.EntryFields{Scope: w.ref.Details().Scope, Actor: w.actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil || !sameProjectAuditEntry(expectedEntry, entry) {
		return projectAuditDenied()
	}
	var scope, project, command, kind, credential, purpose, payload string
	var version int64
	var deleted bool
	err = x.QueryRow(ctx, `SELECT scope,coalesce(project_id::text,''),command_digest,mutation_kind,credential_id::text,purpose,result_version,deleted,digest_payload_id::text FROM agenteam_secret.secret_command_receipts WHERE id=$1`, w.receiptID.String()).Scan(&scope, &project, &command, &kind, &credential, &purpose, &version, &deleted, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectAuditDenied()
	}
	if err != nil {
		return unavailable(err)
	}
	d := w.ref.Details()
	if scope != string(identity.ProjectScope) || project != d.Scope.Details().ProjectID || command != key.Details().CauseRef || kind != string(w.kind) || credential != d.ID.String() || purpose != string(w.purpose) || foundation.Version(version) != w.result.Metadata.Version || deleted != w.result.Deleted || payload != w.receiptPayload.String() {
		return projectAuditDenied()
	}
	if err = checkAuditPayload(ctx, x, w.receiptPayload, d.Scope, receiptOwner, w.receiptID.String()); err != nil {
		return err
	}
	if w.kind == sc.Delete {
		var exists bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE id=$1)`, d.ID.String()).Scan(&exists); err != nil {
			return unavailable(err)
		}
		if exists || w.valuePayload.Validate() == nil {
			return projectAuditDenied()
		}
		return nil
	}
	current, currentPayload, err := loadMetadata(ctx, x, w.ref)
	if err != nil {
		return err
	}
	if current.Purpose != w.purpose || current.Version != w.result.Metadata.Version || currentPayload != w.valuePayload {
		return projectAuditDenied()
	}
	return checkAuditPayload(ctx, x, currentPayload, d.Scope, valueOwner, d.ID.String())
}

func checkResolutionAudit(ctx context.Context, store Store, tx foundation.Tx, x postgres.SQLExecutor, entry ac.Entry, key ac.AppendKey, w projectResolutionAudit) error {
	if _, err := foundation.ParseID[struct{}](w.resolution); err != nil {
		return projectAuditDenied()
	}
	ref, owner := w.lease.ref, w.lease.owner
	if ref.Validate() != nil || owner.Validate() != nil || w.leaseID.Validate() != nil || w.payload.Validate() != nil || w.lease.released || !w.metadata.CredentialRef.Equal(ref) || w.metadata.Version.Validate() != nil || w.metadata.Purpose != w.lease.consumer || w.grant.Consumer != w.lease.consumer || w.grant.Validate(ref.Details().Scope) != nil || !validService(w.caller, ref.Details().Scope) || w.caller.Details().CauseRef != owner.Details().ID || key.Details().CauseRef != w.resolution {
		return projectAuditDenied()
	}
	project, err := foundation.ProjectLock(ref.Details().Scope.Details().ProjectID)
	if err != nil {
		return projectAuditDenied()
	}
	credential, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, ref.Details().ID.String())
	if err = store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: project, Mode: foundation.Shared}, {Key: credential, Mode: foundation.Shared}}); err != nil {
		return unavailable(err)
	}
	requestID := w.grant.RequestID
	if modelLeaseUsage(w.lease.consumer, owner) {
		proof := w.modelRead
		if proof == nil || w.grant.RequestID != "" {
			return projectAuditDenied()
		}
		r := proof.request
		binding, bindErr := sc.UsageBinding(r)
		if bindErr != nil || r.Action != sc.ReadLeaseUsage || r.Purpose != sc.Model ||
			!r.Actor.Equal(w.caller) || !r.Ref.Equal(ref) || !r.LeaseOwner.Equal(owner) || r.LeaseID != w.leaseID ||
			binding != proof.binding || proof.mapping.Validate() != nil || len(proof.locks) == 0 {
			return projectAuditDenied()
		}
		if err = store.RequireHeldLocks(ctx, tx, proof.locks); err != nil {
			return unavailable(err)
		}
		leaseLock, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "secret-lease:"+w.leaseID.String())
		if err = store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: leaseLock, Mode: foundation.Shared}}); err != nil {
			return unavailable(err)
		}
		requestID = r.RequestID
	} else if w.modelRead != nil {
		return projectAuditDenied()
	}
	subject := w.grant.Subject
	if subject.Details().Kind == identity.Service {
		registration, _ := identity.RegisterService(identity.SecretService)
		subject, err = registration.Actor(w.resolution, ref.Details().Scope)
		if err != nil {
			return projectAuditDenied()
		}
	}
	metadata, err := ac.SecretResolveMetadata(w.leaseID.String(), ac.Consumer(w.grant.Consumer), "")
	if err != nil {
		return projectAuditDenied()
	}
	resource, _ := ac.NewResource(ac.SecretResource, ref.Details().ID.String())
	expected, err := ac.NewEntry(ac.EntryFields{Scope: ref.Details().Scope, Actor: subject, Action: ac.SecretResolve, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{RequestID: requestID, OperationID: w.grant.OperationID, ToolID: w.grant.ToolID, RunnerID: w.grant.RunnerID}})
	if err != nil || !sameProjectAuditEntry(expected, entry) {
		return projectAuditDenied()
	}
	lease, err := loadLease(ctx, x, w.leaseID)
	if err != nil {
		return err
	}
	if lease.released || !lease.ref.Equal(ref) || !lease.owner.Equal(owner) || lease.consumer != w.lease.consumer {
		return projectAuditDenied()
	}
	current, payload, err := loadMetadata(ctx, x, ref)
	if err != nil {
		return err
	}
	if current.Purpose != w.metadata.Purpose || current.Version != w.metadata.Version || payload != w.payload {
		return projectAuditDenied()
	}
	return checkAuditPayload(ctx, x, payload, ref.Details().Scope, valueOwner, ref.Details().ID.String())
}

// Only ownership metadata is read. The checker neither reads secret bytes nor
// decrypts them; successful AEAD is witnessed at the private real call site.
func checkAuditPayload(ctx context.Context, x postgres.SQLExecutor, id payloadID, scope identity.Scope, kind ownerKind, owner string) error {
	var storedScope, project, storedOwner string
	var storedKind int16
	err := x.QueryRow(ctx, `SELECT scope,coalesce(project_id::text,''),owner_kind,owner_id::text FROM agenteam_secret.secret_payloads WHERE payload_id=$1`, id.String()).Scan(&storedScope, &project, &storedKind, &storedOwner)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectAuditDenied()
	}
	if err != nil {
		return unavailable(err)
	}
	if storedScope != string(scope.Details().Kind) || project != scope.Details().ProjectID || storedKind != int16(kind) || storedOwner != owner {
		return projectAuditDenied()
	}
	return nil
}

var _ ac.ProjectFactAuthority = (*ProjectAuditAuthority)(nil)
