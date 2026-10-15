package mcp

import (
	"context"
	"encoding/json"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type ContentRecoveryObjects interface {
	oc.ReferenceCleanup
	oc.MCPUploadRecovery
	DeleteUnreferencedWithinBudget(context.Context, oc.ObjectCleanupCause, oc.ObjectID) (oc.CleanupResult, error)
}

func NewContentAuthorityWithRecovery(binding *Authority, invocations tc.MCPInvocationAuthority, reads ContentReadAuthority, maintenance ContentMaintenanceOwnerResolver, uploads oc.MCPUploadResolver, resources ...ResourceInvocationAuthority) (*ContentAuthority, error) {
	if nilPort(uploads) {
		return nil, fail(f.DependencyUnbound)
	}
	a, err := NewContentAuthorityWithMaintenance(binding, invocations, reads, maintenance, resources...)
	if err != nil {
		return nil, err
	}
	a.recovery = uploads
	return a, nil
}

func NewContentStoreWithRecovery(authority *ContentAuthority, objects oc.Objects, cleanup ContentRecoveryObjects) (*ContentStore, error) {
	if authority == nil || nilPort(authority.recovery) || nilPort(cleanup) {
		return nil, fail(f.DependencyUnbound)
	}
	s, err := NewContentStore(authority, objects)
	if err != nil {
		return nil, err
	}
	s.recovery = cleanup
	return s, nil
}

func (a *ContentAuthority) materializationProcessInTx(ctx context.Context, tx f.Tx) (oc.ProcessID, error) {
	if a == nil || a.service() == nil || a.service().owner == nil {
		return oc.ProcessID{}, fail(f.DependencyUnbound)
	}
	o := a.service().owner
	if err := o.guard.RequireCurrentProcessInTx(ctx, tx, o.process); err != nil {
		return oc.ProcessID{}, safeError(err)
	}
	return o.process, nil
}

type contentRecoveryKey struct{}
type contentRecoveryUse struct {
	authority                 *ContentAuthority
	call                      *processCall
	fact                      contentFact
	process                   oc.ProcessID
	recovery, previousProcess string
	owner                     oc.ObjectOwner
	key                       f.IdempotencyKey
	locks                     []f.LockRequest
	mapping                   oc.MCPUploadMapping
	found, stopped            bool
	active                    atomic.Bool
}

func (a *ContentAuthority) recoveryUse(ctx context.Context) (*contentRecoveryUse, error) {
	if a == nil || a.service() == nil || a.service().owner == nil || nilPort(a.recovery) {
		return nil, fail(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, fail(f.InvalidArgument)
	}
	w, ok := ctx.Value(contentRecoveryKey{}).(*contentRecoveryUse)
	if !ok || w == nil || w.authority != a || !w.active.Load() || !w.stopped || w.call == nil || w.call.owner != a.service().owner {
		return nil, fail(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return nil, safeError(err)
	}
	return w, nil
}

func sameRecoveryMapping(a, b oc.MCPUploadMapping) bool {
	// State/disposition advance only in Object's original cleanup transaction.
	return a.Owner.Equal(b.Owner) && a.Key == b.Key && a.UploadID == b.UploadID && a.ObjectID == b.ObjectID && a.AttemptID == b.AttemptID && a.ProcessID == b.ProcessID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID && a.CreationCause == b.CreationCause
}

func (a *ContentAuthority) requireContentRecovery(ctx context.Context, tx f.Tx, w *contentRecoveryUse) (string, error) {
	return a.requireContentRecoveryClaim(ctx, tx, w, true)
}

func (a *ContentAuthority) requireContentRecoveryClaim(ctx context.Context, tx f.Tx, w *contentRecoveryUse, mappingRequired bool) (string, error) {
	s := a.service()
	var x postgres.SQLExecutor = s.store
	if tx.Valid() {
		if err := s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
			return "", safeError(err)
		}
		if err := s.owner.requireInTx(ctx, tx, w.call); err != nil {
			return "", err
		}
		var err error
		x, err = s.store.InTx(tx)
		if err != nil {
			return "", safeError(err)
		}
	} else {
		current, err := s.owner.guard.CurrentProcess()
		if err != nil || current != s.owner.process {
			return "", fail(f.Forbidden)
		}
	}
	actual, err := loadContentFact(ctx, x, w.fact.id)
	if err != nil {
		return "", err
	}
	if actual != w.fact || actual.state != "pending" {
		return "", fail(f.Forbidden)
	}
	var process, recoveryProcess, originalProcess, upload, object, phase, claimID string
	err = x.QueryRow(ctx, `SELECT m.process_id::text,r.id,r.original_process_id::text,r.recovery_process_id::text,COALESCE(r.upload_id::text,''),COALESCE(r.object_id::text,''),r.phase FROM agenteam_mcp.materializations m JOIN agenteam_mcp.content_recoveries r ON r.materialization_id=m.id WHERE m.id=$1 AND r.project_id=$2`, w.fact.id, w.fact.project).Scan(&process, &claimID, &originalProcess, &recoveryProcess, &upload, &object, &phase)
	if err != nil {
		return "", safeError(err)
	}
	if process != w.process.String() || originalProcess != process || claimID != w.recovery || recoveryProcess != s.owner.process.String() || phase == "complete" {
		return "", fail(f.Forbidden)
	}
	if mappingRequired && w.found && (upload != w.mapping.UploadID.String() || object != w.mapping.ObjectID.String()) {
		return "", fail(f.Forbidden)
	}
	return phase, nil
}

func (a *ContentAuthority) contentRecoveryDependencies(ctx context.Context, tx f.Tx, request oc.AccessRequest) (oc.AccessDependencies, error) {
	w, err := a.recoveryUse(ctx)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if request.Validate() != nil || !w.found {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	d := request.Details()
	if d.ObjectID != w.mapping.ObjectID {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	switch d.Kind {
	case oc.CleanupReleaseAccess:
		if d.Operation != oc.ReleaseForCleanupAccess || d.UploadID != w.mapping.UploadID || !recoveryCauseMatches(w, d.Cleanup) {
			return oc.AccessDependencies{}, fail(f.Forbidden)
		}
	case oc.ObjectCleanupAccess:
		if d.Operation != oc.CleanupObjectAccess || !recoveryCauseMatches(w, d.Cleanup) {
			return oc.AccessDependencies{}, fail(f.Forbidden)
		}
	case oc.MaintenanceAccess:
		if d.InstanceID != a.service().owner.process {
			return oc.AccessDependencies{}, fail(f.Forbidden)
		}
		switch d.Operation {
		case oc.JoinAttemptAccess:
			if d.AttemptID != w.mapping.AttemptID || d.ProcessID != w.process {
				return oc.AccessDependencies{}, fail(f.Forbidden)
			}
		case oc.ClaimCleanupAccess, oc.CheckpointCleanupAccess:
			if d.AttemptID != w.mapping.AttemptID {
				return oc.AccessDependencies{}, fail(f.Forbidden)
			}
		case oc.InspectAccess, oc.FinalizeCleanupAccess:
		default:
			return oc.AccessDependencies{}, fail(f.Forbidden)
		}
	default:
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	phase, err := a.requireContentRecovery(ctx, tx, w)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if phase != "reference" && phase != "object" {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	var mapping oc.MCPUploadMapping
	var found bool
	if tx.Valid() {
		mapping, found, err = a.recovery.ResolveInTx(ctx, tx, w.owner, w.key)
	} else {
		mapping, found, err = a.recovery.Discover(ctx, w.owner, w.key)
	}
	if err != nil {
		return oc.AccessDependencies{}, safeError(err)
	}
	if !found || !sameRecoveryMapping(mapping, w.mapping) || phase == "object" && mapping.Disposition != "revoked" {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	raw, err := json.Marshal([]string{"mcp.content.recovery.v1", w.fact.project, w.fact.id, w.fact.execution, w.fact.operation, w.fact.attempt, w.fact.request, w.process.String(), w.recovery, a.service().owner.process.String(), mapping.UploadID.String(), mapping.ObjectID.String(), mapping.AttemptID.String(), string(d.Kind), string(d.Operation)})
	if err != nil {
		return oc.AccessDependencies{}, fail(f.InvalidState)
	}
	return oc.NewAccessDependencies(f.Digest(digest(raw)), w.locks)
}

func recoveryCauseMatches(w *contentRecoveryUse, cause oc.ObjectCleanupCause) bool {
	return cause.Validate() == nil && cause.Details().Reason == oc.AbandonedAttempt && cause.Details().Owner.Equal(w.owner) && cause.Details().OperationID.String() == w.mapping.UploadID.String()
}

func (a *ContentAuthority) CheckCleanupInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	w, err := a.recoveryUse(ctx)
	if err != nil {
		return err
	}
	if !tx.Valid() || !w.found || object != w.mapping.ObjectID || !recoveryCauseMatches(w, cause) {
		return fail(f.Forbidden)
	}
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.CleanupObjectAccess, Cleanup: cause, ObjectID: object})
	if err != nil {
		return fail(f.InvalidArgument)
	}
	_, err = a.contentRecoveryDependencies(ctx, tx, request)
	return err
}

func contentRecoveryLocks(fact contentFact) ([]f.LockRequest, error) {
	project, err := f.ParseID[id.Project](fact.project)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	p, err := f.ProjectLock(project.String())
	if err != nil {
		return nil, err
	}
	c, err := connectionLock(fact.connection, f.Exclusive)
	if err != nil {
		return nil, err
	}
	return sortedLocks([]f.LockRequest{{Key: p, Mode: f.Shared}, c}), nil
}
