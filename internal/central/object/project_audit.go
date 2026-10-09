package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"mime"
	"reflect"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// ProjectAuditAuthority owns only Object facts. The Project composition must
// authorize its current actor/gate before calling it. Construction performs no
// SQL and does not depend on a Service, backend, or mutable registry.
type ProjectAuditAuthority struct{ data func() Store }

func NewProjectAuditAuthority(store Store) (*ProjectAuditAuthority, error) {
	if nilPort(store) {
		return nil, failure(foundation.DependencyUnbound, nil)
	}
	if !reflect.ValueOf(store).Comparable() {
		return nil, invalid()
	}
	return &ProjectAuditAuthority{data: func() Store { return store }}, nil
}
func projectAuditDenied() error { return failure(foundation.Forbidden, nil) }

func (a *ProjectAuditAuthority) CheckProjectAuditInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	if a == nil || a.data == nil || nilPort(a.data()) {
		return failure(foundation.DependencyUnbound, nil)
	}
	if !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	f, k := entry.Fields(), key.Details()
	actor := f.Actor.Details()
	if f.Scope.Details().Kind != identity.ProjectScope || k.Producer != ac.ObjectProducer || actor.Kind != identity.Service || actor.ServiceName != identity.ObjectService || actor.CauseRef != k.CauseRef || actor.ProjectID != f.Scope.Details().ProjectID {
		return projectAuditDenied()
	}
	w, ok := ctx.Value(projectAuditWitnessKey{}).(projectAuditWitness)
	if !ok || w.stage.tx != tx || !sameProjectAuditStore(a.data(), w.stage.store) || !sameProjectAuditEntry(w.entry, entry) || w.key.Validate() != nil || w.key.Details() != k {
		return projectAuditDenied()
	}
	x, err := a.data().InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	switch f.Action {
	case ac.ObjectUploadComplete, ac.ObjectUploadFailed, ac.ObjectDelete:
		if f.Resource.Details().Kind != ac.ObjectResource || f.Resource.Details().ID != w.meta.ID.String() {
			return projectAuditDenied()
		}
		return checkProjectObjectAudit(ctx, x, w)
	case ac.ObjectTransferIssue, ac.ObjectTransferComplete, ac.ObjectTransferRevoke:
		if f.Resource.Details().Kind != ac.ObjectTransferResource || f.Resource.Details().ID != w.transfer.id.String() {
			return projectAuditDenied()
		}
		return checkProjectTransferAudit(ctx, x, w)
	default:
		return projectAuditDenied()
	}
}

func sameAuditObjectMeta(a, b oc.ObjectMeta) bool {
	return a.Validate() == nil && b.Validate() == nil && a.ID == b.ID && a.Scope.Equal(b.Scope) && a.MediaType == b.MediaType && a.ByteSize == b.ByteSize && a.SHA256 == b.SHA256 && a.State == b.State && a.Version == b.Version && a.CreatedAt == b.CreatedAt
}
func auditExpectedEntry(scope identity.Scope, key ac.AppendKey, action ac.Action, outcome ac.Outcome, kind ac.ResourceKind, resource string, metadata ac.Metadata) (ac.Entry, error) {
	registration, err := identity.RegisterService(identity.ObjectService)
	if err != nil {
		return ac.Entry{}, invalid()
	}
	actor, err := registration.Actor(key.Details().CauseRef, scope)
	if err != nil {
		return ac.Entry{}, invalid()
	}
	ref, err := ac.NewResource(kind, resource)
	if err != nil {
		return ac.Entry{}, invalid()
	}
	return ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: outcome, Resource: ref, Metadata: metadata})
}

func checkProjectObjectAudit(ctx context.Context, x postgres.SQLExecutor, w projectAuditWitness) error {
	p, f, k := w.stage, w.entry.Fields(), w.key.Details()
	u, found, err := loadUpload(ctx, x, w.upload)
	if err != nil {
		return err
	}
	if !found || u.object != w.meta.ID || !u.owner.Scope().Equal(f.Scope) {
		return projectAuditDenied()
	}
	o, found, err := loadObject(ctx, x, u.object)
	if err != nil {
		return err
	}
	if !found || !objectPartition(o, u.owner) || !sameAuditObjectMeta(o.meta, w.meta) || !auditObjectLocks(p, f.Scope.Details().ProjectID, u.object) {
		return projectAuditDenied()
	}
	phase, outcome := ac.PublishedPhase, ac.Success
	switch f.Action {
	case ac.ObjectUploadComplete:
		if p.kind != auditPublish || p.object != u.object || p.upload != u.id || !p.owner.Equal(u.owner) || checkOriginal(u, p.actor, p.owner) != nil || k.CauseRef != u.id.String() || k.Ordinal != 0 || u.disposition == "revoked" || u.state == "committed" || o.cleaning || o.meta.State == oc.Deleted || u.attempt != p.attempt || w.reason != "" {
			return projectAuditDenied()
		}
		a, ok, err := loadAttempt(ctx, x, p.attempt)
		if err != nil {
			return err
		}
		if !ok || a.kind != "private_candidate" || a.upload != u.id || a.object != u.object || a.phase != "verified" || a.cleaning || !a.closed || a.size != int64(o.meta.ByteSize) || a.digest != o.meta.SHA256 {
			return projectAuditDenied()
		}
		if err = requireNativeWork(ctx, x, "preparation", a.id.String()); err != nil {
			return err
		}
	case ac.ObjectUploadFailed:
		phase, outcome = ac.FailedPhase, ac.Unknown
		if p.object != u.object || p.attempt.Validate() != nil || k.CauseRef != p.attempt.String() || w.reason != ac.PayloadMissing && w.reason != ac.IntegrityMismatch && w.reason != ac.StorageUnavailable {
			return projectAuditDenied()
		}
		a, ok, err := loadAttempt(ctx, x, p.attempt)
		if err != nil {
			return err
		}
		if !ok || a.kind != "private_candidate" || a.upload != u.id || a.object != u.object || !a.closed {
			return projectAuditDenied()
		}
		if k.Ordinal == 0 && p.kind == auditWriterFailure {
			if a.process != p.process || a.phase != "unknown" || a.cleaning || u.state != "unknown" && u.state != "committed" {
				return projectAuditDenied()
			}
			var released, active bool
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$1 AND attempt_id=$2 AND owner_kind='writer' AND process_id=$3 AND state='released'),EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE attempt_id=$2 AND owner_kind='writer' AND process_id=$3 AND state='active')`, u.object.String(), a.id.String(), p.process.String()).Scan(&released, &active)
			if err != nil {
				return unavailable(err)
			}
			if !released || active {
				return projectAuditDenied()
			}
		} else if k.Ordinal == 1 && p.kind == auditRecoveryFailure {
			if !a.cleaning || a.phase != "abandoned" {
				return projectAuditDenied()
			}
			var exists bool
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND attempt_id=$2 AND operation_id=$3 AND id=$4)`, u.object.String(), a.id.String(), w.cleanupOperation, w.cleanupID).Scan(&exists)
			if err != nil {
				return unavailable(err)
			}
			if !exists {
				return projectAuditDenied()
			}
		} else {
			return projectAuditDenied()
		}
	case ac.ObjectDelete:
		phase = ac.DeletedPhase
		if p.kind != auditDelete || p.object != u.object || k.Ordinal != 1 || !o.cleaning || o.meta.State == oc.Deleted || w.reason != "" {
			return projectAuditDenied()
		}
		var blocked bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$1) OR EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active') OR EXISTS(SELECT 1 FROM agenteam_object.upload_attempts WHERE object_id=$1 AND phase<>'cleaned')`, u.object.String()).Scan(&blocked)
		if err != nil {
			return unavailable(err)
		}
		if blocked {
			return projectAuditDenied()
		}
		if p.boundedCleanup {
			if u.owner.Details().Kind != oc.SkillRevision || u.disposition != "revoked" || len(p.cleanupWorkers) > oc.ObjectMetadataPurgeBatchLimit {
				return projectAuditDenied()
			}
			blocked, err = boundedCleanupPending(ctx, x, u.object, p.cleanupWorkers)
			if err != nil {
				return err
			}
			if blocked {
				return projectAuditDenied()
			}
		}
		var cause string
		if err = x.QueryRow(ctx, `SELECT operation_id::text FROM agenteam_object.cleanup_operations WHERE object_id=$1 ORDER BY created_at,id LIMIT 1`, u.object.String()).Scan(&cause); err != nil {
			return unavailable(err)
		}
		digest := sha256.Sum256([]byte("object.delete.v1\x00" + cause + "\x00" + u.object.String()))
		if k.CauseRef != newDigest(digest[:]).String() {
			return projectAuditDenied()
		}
	default:
		return projectAuditDenied()
	}
	media, _, err := mime.ParseMediaType(o.meta.MediaType)
	if err != nil {
		return unavailable(err)
	}
	metadata, err := ac.ObjectMetadata(f.Action, ac.ObjectMetadataFields{ObjectID: u.object.String(), InitiatorKind: u.initiator, InitiatorID: u.initiatorID, InitiatorExecutionID: u.executionID, MediaType: media, ByteSize: o.meta.ByteSize, Phase: phase, Reason: w.reason})
	if err != nil {
		return projectAuditDenied()
	}
	expected, err := auditExpectedEntry(f.Scope, w.key, f.Action, outcome, ac.ObjectResource, u.object.String(), metadata)
	if err != nil || !sameProjectAuditEntry(expected, w.entry) {
		return projectAuditDenied()
	}
	return nil
}

func sameTransferAuditFact(a, b projectAuditTransferFact) bool {
	return a.id == b.id && a.actor.Details() == b.actor.Details() && a.owner.Equal(b.owner) && a.object == b.object && a.upload == b.upload && a.staging == b.staging && a.candidate == b.candidate && a.lease == b.lease && a.runner == b.runner && a.operation == b.operation && a.direction == b.direction && a.manifest.Equal(b.manifest) && a.runnerGeneration == b.runnerGeneration && a.operationVersion == b.operationVersion && a.execution == b.execution
}
func checkProjectTransferAudit(ctx context.Context, x postgres.SQLExecutor, w projectAuditWitness) error {
	p, f, k := w.stage, w.entry.Fields(), w.key.Details()
	r, found, err := loadTransfer(ctx, x, w.transfer.id)
	if err != nil {
		return err
	}
	if !found || !r.spec.Details().Owner.Scope().Equal(f.Scope) || !sameTransferAuditFact(transferAuditFact(r), w.transfer) || k.CauseRef != r.id.String() || !auditObjectLocks(p, f.Scope.Details().ProjectID, r.object) {
		return projectAuditDenied()
	}
	if p.kind == auditTransfer {
		v, d := p.transfer, r.spec.Details()
		if v.id != r.id || v.object != r.object || !p.owner.Equal(d.Owner) || stableActor(p.actor) != r.stable || v.runner != d.RunnerID || v.operationID != d.OperationID || v.direction != d.Direction || !v.manifest.Equal(r.manifest) || v.runnerGeneration != r.runnerGeneration || v.operationVersion != r.operationVersion || v.execution != r.execution {
			return projectAuditDenied()
		}
		// GET Issue assigns the actual lease in its already-validated extra
		// AcquireUseAccess plan. Other phases preserve the persisted lease.
		if (v.operation != oc.TransferIssue || d.Direction == oc.TransferPUT) && v.lease != r.lease {
			return projectAuditDenied()
		}
		if d.Direction == oc.TransferPUT && (v.upload != r.upload || v.staging != r.staging) {
			return projectAuditDenied()
		}
	}
	phase, ordinal := ac.IssuedPhase, int64(0)
	var sent foundation.Progress
	switch f.Action {
	case ac.ObjectTransferIssue:
		if p.kind != auditTransfer || p.transfer.operation != oc.TransferIssue || r.phase != "issued" || r.revoked || r.completed.Validate() == nil {
			return projectAuditDenied()
		}
		if err = checkTransferAuditLease(ctx, x, r, true); err != nil {
			return err
		}
		if err = checkTransferAuditContent(ctx, x, r, false); err != nil {
			return err
		}
	case ac.ObjectTransferComplete:
		phase, ordinal, sent = ac.SentPhase, 1, foundation.Progress(r.manifest.Details().Length)
		if p.kind != auditTransfer || r.phase != "complete" || r.revoked {
			return projectAuditDenied()
		}
		v, proof := p.transfer, p.transfer.completed
		if proof == nil || r.completed != proof.Evidence.ID || proof.Evidence.Kind != oc.TransferCompletedEvidence || !bytes.Equal(r.completedDigest, digestBytes(proof.Digest)) || proof.Length != r.manifest.Details().Length || proof.SHA256 != r.manifest.Details().SHA256 {
			return projectAuditDenied()
		}
		if r.spec.Details().Direction == oc.TransferGET && v.operation != oc.TransferCapture || r.spec.Details().Direction == oc.TransferPUT && (v.operation != oc.TransferPublish || v.candidate != r.candidate) {
			return projectAuditDenied()
		}
		// A GET completion reports historical, already-authorized I/O. Its
		// independent retirement may have released the original lease first.
		if err = checkTransferAuditLease(ctx, x, r, r.spec.Details().Direction != oc.TransferGET); err != nil {
			return err
		}
		if err = checkTransferAuditContent(ctx, x, r, true); err != nil {
			return err
		}
	case ac.ObjectTransferRevoke:
		phase, ordinal = ac.RevokedPhase, 2
		if !r.revoked {
			return projectAuditDenied()
		}
		switch p.kind {
		case auditTransfer:
			if p.transfer.operation != oc.TransferCancel {
				return projectAuditDenied()
			}
		case auditProjectCleanup:
			if p.cleanup.Validate() != nil || p.cleanup.Details().ProjectID.String() != f.Scope.Details().ProjectID || !slices.Contains(p.objects, r.object) {
				return projectAuditDenied()
			}
		case auditProjectStop:
			if p.stop.Validate() != nil || p.stop.Details().ProjectID.String() != f.Scope.Details().ProjectID || !slices.Contains(p.objects, r.object) && !slices.Contains(p.stopIDs, r.id.String()) || p.stop.Details().Action == oc.ProjectStopArchive && r.spec.Details().Direction != oc.TransferPUT {
				return projectAuditDenied()
			}
			current, err := readProjectStop(ctx, x, p.stop)
			if err != nil {
				return err
			}
			if current == nil || current.state == "stopped" {
				return projectAuditDenied()
			}
		default:
			return projectAuditDenied()
		}
	default:
		return projectAuditDenied()
	}
	if k.Ordinal != ordinal {
		return projectAuditDenied()
	}
	media, _, err := mime.ParseMediaType(r.manifest.Details().MediaType)
	if err != nil {
		return unavailable(err)
	}
	a := r.actor.Details()
	initiator := a.UserID
	if a.Kind == identity.AgentRun {
		initiator = a.AgentID
	}
	metadata, err := ac.ObjectMetadata(f.Action, ac.ObjectMetadataFields{ObjectID: r.object.String(), TransferID: r.id.String(), InitiatorKind: a.Kind, InitiatorID: initiator, InitiatorExecutionID: a.ExecutionID, MediaType: media, ByteSize: foundation.Progress(r.manifest.Details().Length), SentBytes: sent, Phase: phase})
	if err != nil {
		return projectAuditDenied()
	}
	expected, err := auditExpectedEntry(f.Scope, w.key, f.Action, ac.Success, ac.ObjectTransferResource, r.id.String(), metadata)
	if err != nil || !sameProjectAuditEntry(expected, w.entry) {
		return projectAuditDenied()
	}
	return nil
}
func checkTransferAuditLease(ctx context.Context, x postgres.SQLExecutor, r transferRow, active bool) error {
	var object, owner, kind, state, attempt string
	var released bool
	err := x.QueryRow(ctx, `SELECT object_id::text,owner_kind,owner_id::text,state,coalesce(attempt_id::text,''),released_at IS NOT NULL FROM agenteam_object.object_leases WHERE id=$1`, r.lease.String()).Scan(&object, &kind, &owner, &state, &attempt, &released)
	if err != nil {
		return unavailable(err)
	}
	if object != r.object.String() || kind != "transfer" || owner != r.id.String() || active && state != "active" || attempt != "" {
		return projectAuditDenied()
	}
	if state != "active" && (state != "released" || !released || r.retirement.Validate() != nil || len(r.retirementDigest) != 32 || r.retirementKind != oc.TransferStoppedEvidence && r.retirementKind != oc.TransferCompletedEvidence) {
		return projectAuditDenied()
	}
	return nil
}
func checkTransferAuditContent(ctx context.Context, x postgres.SQLExecutor, r transferRow, completed bool) error {
	o, found, err := loadObject(ctx, x, r.object)
	if err != nil {
		return err
	}
	m := r.manifest.Details()
	if !found || !objectPartition(o, r.spec.Details().Owner) || o.meta.MediaType != m.MediaType || int64(o.meta.ByteSize) != m.Length || o.meta.SHA256 != m.SHA256 {
		return projectAuditDenied()
	}
	if r.spec.Details().Direction == oc.TransferGET {
		if !completed && (o.meta.State != oc.Available || o.cleaning) {
			return projectAuditDenied()
		}
		return nil
	}
	if o.cleaning || o.meta.State == oc.Deleted {
		return projectAuditDenied()
	}
	u, found, err := loadUpload(ctx, x, r.upload)
	if err != nil {
		return err
	}
	if !found || u.object != r.object || !u.owner.Equal(r.spec.Details().Owner) || u.actor != r.stable || u.disposition == "revoked" {
		return projectAuditDenied()
	}
	a, found, err := loadAttempt(ctx, x, r.staging)
	if err != nil {
		return err
	}
	if !found || a.kind != "runner_staging" || a.transfer != r.id || a.upload != r.upload || a.object != r.object || a.size != m.Length || a.digest != m.SHA256 {
		return projectAuditDenied()
	}
	if !completed {
		if a.cleaning || a.phase != "reserved" || u.attempt != a.id {
			return projectAuditDenied()
		}
		return nil
	}
	if u.state != "committed" || o.meta.State != oc.Available || u.attempt != r.candidate {
		return projectAuditDenied()
	}
	a, found, err = loadAttempt(ctx, x, r.candidate)
	if err != nil {
		return err
	}
	if !found || a.kind != "private_candidate" || a.upload != r.upload || a.object != r.object || a.phase != "published" || a.cleaning || !a.closed || a.size != m.Length || a.digest != m.SHA256 {
		return projectAuditDenied()
	}
	return nil
}

var _ ac.ProjectFactAuthority = (*ProjectAuditAuthority)(nil)
