package artifact

import (
	"context"
	"strings"
	"sync/atomic"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type cancelledUploadWitnessKey struct{}
type cancelledUploadFact struct {
	identity, actor, cause, state string
	ref                           ac.ArtifactRef
	digest                        f.Digest
	object                        oc.ObjectID
	upload                        oc.UploadID
	attempt                       oc.AttemptID
}
type cancelledUploadWitness struct {
	active  atomic.Bool
	store   Store
	tx      f.Tx
	subject ac.AccessSubject
	owner   oc.ObjectOwner
	fact    cancelledUploadFact
}

func requireCancelledUploadLocks(ctx context.Context, tx f.Tx, store Store, owner oc.ObjectOwner) error {
	locks, ok := store.(interface {
		RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
	})
	if !ok || nilPort(locks) {
		return failure(f.DependencyUnbound, nil)
	}
	return portOrNil(locks.RequireHeldLocks(ctx, tx, []f.LockRequest{artifactLock(owner.Details().ID)}))
}

func (p *OwnerProvider) authorizeOwnerScope(ctx context.Context, tx f.Tx, actor i.Actor, owner oc.ObjectOwner, intent i.AccessIntent) error {
	project, err := f.ParseID[i.Project](owner.Details().ProjectID)
	if err != nil {
		return invalid()
	}
	if actor.Details().Kind != i.Service {
		return p.authorize(ctx, tx, subject(actor, project), intent)
	}
	x, err := p.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return failure(f.DependencyUnbound, nil)
	}
	row, found, err := ownerByID(ctx, x, owner.Details().ID)
	if err != nil {
		return err
	}
	if !found || row.project != project.String() {
		return failure(f.Forbidden, nil)
	}
	if row.upload != "" && row.upload == actor.Details().CauseRef {
		return p.authorizeCancelledUpload(ctx, tx, actor, owner, intent)
	}
	// The separate Project-delete family still requires its original Project
	// scope as well as the durable Artifact cleanup operation.
	if err = p.authorize(ctx, tx, subject(actor, project), intent); err != nil {
		return err
	}
	return p.checkTechnicalCause(ctx, x, actor, project)
}

// Only the original Artifact owner callback creates this short-lived proof.
// A Service label or a Project-wide matching cancellation is not a proof of
// the particular Object/upload being converged.
func (p *OwnerProvider) authorizeCancelledUpload(ctx context.Context, tx f.Tx, actor i.Actor, owner oc.ObjectOwner, intent i.AccessIntent) error {
	if intent != i.Converge {
		return failure(f.Forbidden, nil)
	}
	if err := requireCancelledUploadLocks(ctx, tx, p.state().store, owner); err != nil {
		return err
	}
	x, err := p.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return failure(f.DependencyUnbound, nil)
	}
	fact, err := readCancelledUploadFact(ctx, x, actor, owner)
	if err != nil {
		return err
	}
	s := subject(actor, fact.ref.ProjectID)
	w := &cancelledUploadWitness{store: p.state().store, tx: tx, subject: s, owner: owner, fact: fact}
	w.active.Store(true)
	defer w.active.Store(false)
	return p.authorize(context.WithValue(ctx, cancelledUploadWitnessKey{}, w), tx, s, intent)
}

// CheckCancelledUploadConvergenceInTx is a verifier, never a proof issuer. The
// Project adapter separately requires its current initialized active/archived
// gate. It cannot use this proof for reads, mutations or Project deletion.
func CheckCancelledUploadConvergenceInTx(ctx context.Context, tx f.Tx, store any, subject ac.AccessSubject) error {
	if ctx == nil || !tx.Valid() || subject.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	w, _ := ctx.Value(cancelledUploadWitnessKey{}).(*cancelledUploadWitness)
	original, ok := store.(Store)
	if !ok || w == nil || !w.active.Load() || w.tx != tx || !sameProjectAuditStore(original, w.store) ||
		!subject.Actor.Equal(w.subject.Actor) || subject.ProjectID != w.subject.ProjectID || subject.Maintenance ||
		subject.ExecutionID != "" || subject.OperationID != "" || subject.ToolID != "" || subject.ToolCallID != "" {
		return failure(f.Forbidden, nil)
	}
	if err := requireCancelledUploadLocks(ctx, tx, original, w.owner); err != nil {
		return err
	}
	x, err := original.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return failure(f.DependencyUnbound, nil)
	}
	current, err := readCancelledUploadFact(ctx, x, subject.Actor, w.owner)
	if err != nil {
		return err
	}
	if current != w.fact || !w.active.Load() {
		return failure(f.Forbidden, nil)
	}
	return portOrNil(ctx.Err())
}

func readCancelledUploadFact(ctx context.Context, x postgres.SQLExecutor, actor i.Actor, owner oc.ObjectOwner) (cancelledUploadFact, error) {
	var out cancelledUploadFact
	if actor.Validate() != nil || owner.Validate() != nil || owner.Details().Kind != oc.Artifact {
		return out, invalid()
	}
	d, o := actor.Details(), owner.Details()
	if d.Kind != i.Service || d.ServiceName != i.ObjectMaintenance || d.ProjectID != o.ProjectID {
		return out, failure(f.Forbidden, nil)
	}
	row, found, err := ownerByID(ctx, x, o.ID)
	if err != nil {
		return out, err
	}
	if !found || row.exists || row.project != o.ProjectID || row.upload != d.CauseRef ||
		(row.state != "cancel_requested" && row.state != "cancelled") {
		return out, failure(f.Forbidden, nil)
	}
	project, err := f.ParseID[i.Project](row.project)
	if err != nil {
		return out, unavailable(err)
	}
	id, err := commandIdentity(project, f.IdempotencyKey(row.commandKey), "create")
	if err != nil {
		return out, unavailable(err)
	}
	command, found, err := loadCommand(ctx, x, id)
	if err != nil {
		return out, err
	}
	if found && command.ref.ArtifactID.String() == o.ID {
		if command.creatorKind != i.Human || command.originalActor != "human:"+command.creator ||
			command.execution != "" || command.operation != "" || command.tool != "" || command.toolCall != "" {
			return out, failure(f.Forbidden, nil)
		}
		out = cancelledUploadFact{id.Canonical(), command.originalActor, command.cause, command.state, command.ref, command.digest, command.object, command.upload, command.attempt}
	} else {
		intent, exists, err := loadIntentID(ctx, x, o.ID)
		if err != nil {
			return out, err
		}
		if !exists || string(intent.identity.Key()) != row.commandKey || intent.execution != "" || intent.operation != "" || intent.tool != "" || intent.toolCall != "" {
			return out, failure(f.Forbidden, nil)
		}
		out = cancelledUploadFact{intent.identity.Canonical(), intent.originalActor, intent.cause, intent.state, intent.ref, intent.digest, intent.object, intent.upload, intent.attempt}
	}
	_, actorErr := f.ParseID[i.User](strings.TrimPrefix(out.actor, "human:"))
	_, causeErr := f.ParseID[struct{}](out.cause)
	if !strings.HasPrefix(out.actor, "human:") || actorErr != nil || causeErr != nil || out.ref.Validate() != nil || out.ref.ProjectID != project ||
		out.ref.ArtifactID.String() != o.ID || out.digest.Validate() != nil || out.object.Validate() != nil || out.upload.Validate() != nil || out.attempt.Validate() != nil ||
		out.object.String() != row.object || out.upload.String() != row.upload || out.actor != row.originalActor || out.cause != row.cause || out.state != row.state {
		return cancelledUploadFact{}, failure(f.Forbidden, nil)
	}
	return out, nil
}
