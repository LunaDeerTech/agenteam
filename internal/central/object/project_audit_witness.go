package object

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type projectAuditStageKey struct{}
type projectAuditWitnessKey struct{}

type projectAuditStageKind uint8

const (
	auditPublish projectAuditStageKind = iota + 1
	auditWriterFailure
	auditRecoveryFailure
	auditDelete
	auditTransfer
	auditProjectCleanup
	auditProjectStop
)

// A stage is minted only after the existing full Acquire/Validate path. It
// retains safe identities, never a Service, AccessPlan/PreparedPayload, backend
// key, source, signed URL, or a provider's mutable authorization object.
type projectAuditStage struct {
	store          Store
	tx             foundation.Tx
	kind           projectAuditStageKind
	locks          []foundation.LockRequest
	actor          identity.Actor
	owner          oc.ObjectOwner
	object         oc.ObjectID
	upload         oc.UploadID
	attempt        oc.AttemptID
	process        oc.ProcessID
	objects        []oc.ObjectID
	cleanup        oc.ProjectCleanupCause
	stop           oc.ProjectStopCause
	stopIDs        []string
	transfer       projectAuditTransferStage
	boundedCleanup bool
	cleanupWorkers []string
}

type projectAuditTransferStage struct {
	operation                          oc.TransferOperation
	id                                 oc.TransferID
	object                             oc.ObjectID
	upload                             oc.UploadID
	staging, candidate                 oc.AttemptID
	lease                              oc.LeaseID
	runner                             oc.RunnerID
	operationID                        oc.OperationID
	direction                          oc.TransferDirection
	manifest                           oc.TransferManifest
	runnerGeneration, operationVersion foundation.Version
	execution                          identity.ExecutionID
	completed                          *oc.TransferCompletion
}

// The final witness is exact-call evidence, not a durable receipt or an
// authorization. The checker independently rereads canonical Object facts.
type projectAuditWitness struct {
	stage                       projectAuditStage
	entry                       ac.Entry
	key                         ac.AppendKey
	upload                      oc.UploadID
	meta                        oc.ObjectMeta
	reason                      ac.Reason
	transfer                    projectAuditTransferFact
	cleanupID, cleanupOperation string
}

type projectAuditTransferFact struct {
	id                                 oc.TransferID
	actor                              identity.Actor
	owner                              oc.ObjectOwner
	object                             oc.ObjectID
	upload                             oc.UploadID
	staging, candidate                 oc.AttemptID
	lease                              oc.LeaseID
	runner                             oc.RunnerID
	operation                          oc.OperationID
	direction                          oc.TransferDirection
	manifest                           oc.TransferManifest
	runnerGeneration, operationVersion foundation.Version
	execution                          identity.ExecutionID
}

func (projectAuditStage) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_project_audit_stage")
}
func (projectAuditWitness) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_project_audit_witness")
}

func sameProjectAuditStore(a, b Store) bool {
	return !nilPort(a) && !nilPort(b) && reflect.TypeOf(a) == reflect.TypeOf(b) &&
		reflect.ValueOf(a).Comparable() && reflect.ValueOf(b).Comparable() && a == b
}
func sameProjectAuditEntry(a, b ac.Entry) bool {
	if a.Validate() != nil || b.Validate() != nil {
		return false
	}
	x, y := a.Fields(), b.Fields()
	return x.Scope.Equal(y.Scope) && x.Actor.Details() == y.Actor.Details() && x.Action == y.Action &&
		x.Outcome == y.Outcome && x.Resource.Details() == y.Resource.Details() &&
		bytes.Equal(x.Metadata.JSON(), y.Metadata.JSON()) && x.Associations == y.Associations
}

func (s *Service) checkedProjectAuditAccess(tx foundation.Tx, request oc.AccessRequest, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	r := s.state()
	r.accessMu.Lock()
	acquired := r.accessTransactions[tx]
	r.accessMu.Unlock()
	if !acquired || request.Validate() != nil || !locked.Matches(r.accessIssuer, tx, plan, request) {
		return projectAuditDenied()
	}
	if _, err := r.store.InTx(tx); err != nil {
		return unavailable(err)
	}
	return nil
}

// Callers have just completed Acquire, Validate and accessWorkBefore. Publish
// calls this explicitly after its owner/gate/verified checks, including when
// the enclosing transaction belongs to another domain.
func (s *Service) projectAuditAccessContext(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, plan oc.AccessLockPlan, locked oc.LockedAccess) (context.Context, error) {
	d := request.Details()
	var kind projectAuditStageKind
	switch d.Operation {
	case oc.PublishAccess:
		kind = auditPublish
	case oc.FinishWriterAccess:
		kind = auditWriterFailure
	case oc.RecoverAttemptAccess:
		kind = auditRecoveryFailure
	case oc.FinalizeCleanupAccess:
		kind = auditDelete
	case oc.GateProjectAccess:
		kind = auditProjectCleanup
	default:
		return ctx, nil
	}
	if err := s.checkedProjectAuditAccess(tx, request, plan, locked); err != nil {
		return ctx, err
	}
	p := projectAuditStage{store: s.state().store, tx: tx, kind: kind, locks: locked.Locks(), actor: d.Actor,
		owner: d.Owner, object: d.ObjectID, attempt: d.AttemptID, process: d.InstanceID,
		objects: append([]oc.ObjectID(nil), plan.Details().Objects...), cleanup: d.ProjectCleanup}
	if kind == auditPublish {
		a := d.Attempt.Details()
		p.object, p.upload, p.attempt = a.ObjectID, a.UploadID, a.ID
	}
	if kind == auditDelete && s.boundedCleanup(ctx, d.ObjectID) != nil {
		workers, err := s.completedCleanupWorkers(ctx, d.ObjectID)
		if err != nil {
			return ctx, err
		}
		p.boundedCleanup = true
		p.cleanupWorkers = append([]string(nil), workers...)
	}
	return context.WithValue(ctx, projectAuditStageKey{}, p), nil
}

func (t *TransferService) projectAuditTransferContext(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, plan oc.AccessLockPlan, locked oc.LockedAccess, authorization oc.TransferAuthorization) (context.Context, error) {
	d := request.Details().Transfer.Details()
	if d.Operation != oc.TransferIssue && d.Operation != oc.TransferCapture && d.Operation != oc.TransferPublish && d.Operation != oc.TransferCancel {
		return ctx, nil
	}
	s := t.state().objects
	if err := s.checkedProjectAuditAccess(tx, request, plan, locked); err != nil {
		return ctx, err
	}
	if !authorization.Matches(request.Details().Transfer) {
		return ctx, projectAuditDenied()
	}
	a, spec := authorization.Details(), d.Spec.Details()
	p := projectAuditStage{store: s.state().store, tx: tx, kind: auditTransfer, locks: locked.Locks(), actor: d.Actor, owner: spec.Owner, object: d.ObjectID}
	p.transfer = projectAuditTransferStage{operation: d.Operation, id: d.ID, object: d.ObjectID, upload: d.UploadID, staging: d.StagingID, candidate: d.CandidateID, lease: d.LeaseID,
		runner: spec.RunnerID, operationID: spec.OperationID, direction: spec.Direction, manifest: d.Manifest,
		runnerGeneration: a.RunnerGeneration, operationVersion: a.OperationVersion, execution: a.ExecutionID}
	if a.Completed != nil {
		c := *a.Completed
		p.transfer.completed = &c
	}
	return context.WithValue(ctx, projectAuditStageKey{}, p), nil
}

// Called only by the gated Continue callback after projectStopTransaction has
// acquired the complete plan, revalidated the current cause and reread facts.
func (s *Service) projectAuditStopContext(ctx context.Context, tx foundation.Tx, plan projectStopPlan, authorization oc.ProjectStopAuthorization, facts projectStopFacts) (context.Context, error) {
	if plan.service != s || !authorization.Matches(tx, plan.request, plan.dependencies) || authorization.Mode() != oc.ContinueProjectStop || facts.binding != plan.facts.binding {
		return ctx, projectAuditDenied()
	}
	p := projectAuditStage{store: s.state().store, tx: tx, kind: auditProjectStop, locks: append([]foundation.LockRequest(nil), plan.locks...), actor: plan.request.Details().Actor, stop: plan.request.Details().Cause, stopIDs: append([]string(nil), facts.ids...)}
	for _, raw := range facts.objects {
		id, err := foundation.ParseID[oc.StoredObject](raw)
		if err != nil {
			return ctx, unavailable(err)
		}
		p.objects = append(p.objects, id)
	}
	return context.WithValue(ctx, projectAuditStageKey{}, p), nil
}

func (s *Service) projectAuditWitnessContext(ctx context.Context, tx foundation.Tx, w projectAuditWitness) (context.Context, error) {
	if w.entry.Fields().Scope.Details().Kind != identity.ProjectScope {
		return ctx, nil
	}
	p, ok := ctx.Value(projectAuditStageKey{}).(projectAuditStage)
	if !ok || p.tx != tx || !sameProjectAuditStore(p.store, s.state().store) {
		return ctx, projectAuditDenied()
	}
	w.stage = p
	if w.entry.Fields().Action == ac.ObjectUploadFailed && w.key.Details().Ordinal == 1 {
		x, err := p.store.InTx(tx)
		if err != nil {
			return ctx, unavailable(err)
		}
		// gateAttempt preserves an existing operation on conflict. Capture that
		// actual identity, rather than replacing it with this recovery's UUID.
		if err = x.QueryRow(ctx, `SELECT id::text,operation_id::text FROM agenteam_object.cleanup_operations WHERE attempt_id=$1 AND object_id=$2`, p.attempt.String(), w.meta.ID.String()).Scan(&w.cleanupID, &w.cleanupOperation); err != nil {
			return ctx, unavailable(err)
		}
	}
	return context.WithValue(ctx, projectAuditWitnessKey{}, w), nil
}

func transferAuditFact(r transferRow) projectAuditTransferFact {
	d := r.spec.Details()
	return projectAuditTransferFact{id: r.id, actor: r.actor, owner: d.Owner, object: r.object, upload: r.upload, staging: r.staging, candidate: r.candidate, lease: r.lease,
		runner: d.RunnerID, operation: d.OperationID, direction: d.Direction, manifest: r.manifest,
		runnerGeneration: r.runnerGeneration, operationVersion: r.operationVersion, execution: r.execution}
}

func auditLockCovers(locks []foundation.LockRequest, key foundation.LockKey, mode foundation.LockMode) bool {
	return slices.ContainsFunc(locks, func(v foundation.LockRequest) bool {
		return v.Key.Canonical() == key.Canonical() && (v.Mode == mode || v.Mode == foundation.Exclusive)
	})
}
func auditObjectLocks(p projectAuditStage, project string, object oc.ObjectID) bool {
	pk, err := foundation.ProjectLock(project)
	if err != nil {
		return false
	}
	ok, err := foundation.AggregateLock(foundation.ObjectAggregate, object.String())
	if err != nil {
		return false
	}
	mode := foundation.Shared
	if p.kind == auditProjectCleanup || p.kind == auditProjectStop {
		mode = foundation.Exclusive
	}
	return auditLockCovers(p.locks, pk, mode) && auditLockCovers(p.locks, ok, foundation.Exclusive)
}
