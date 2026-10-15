package meeting

import (
	"context"
	"errors"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// DeletionAuthority is separate from result-reading authority. A normal
// contribution result witness never authorizes permanent Meeting removal.
type DeletionAuthority struct {
	original *Authority
	guard    *object.ProcessGuard
	process  oc.ProcessID
}

func NewDeletionAuthority(original *Authority, guard *object.ProcessGuard, process oc.ProcessID) (*DeletionAuthority, error) {
	if original == nil || nilPort(original.store) || guard == nil || process.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &DeletionAuthority{original: original, guard: guard, process: process}, nil
}

type deletionRecord struct {
	id       c.DeletionID
	project  i.ProjectID
	meeting  c.MeetingID
	actor    i.UserID
	request  f.ID[f.Request]
	key      f.IdempotencyKey
	digest   f.Digest
	expected f.Version
	accepted f.Version
	phase    c.DeletionPhase
}

func loadDeletion(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, m c.MeetingID) (deletionRecord, error) {
	var v deletionRecord
	var deletion, actor, request string
	err := x.QueryRow(ctx, `SELECT id,actor_id,request_id,idempotency_key,semantic_digest,expected_version,accepted_version,phase FROM agenteam_meeting.deletion_requests WHERE project_id=$1 AND meeting_id=$2`, p.String(), m.String()).Scan(&deletion, &actor, &request, &v.key, &v.digest, &v.expected, &v.accepted, &v.phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, fault(f.NotFound)
	}
	if err != nil {
		return v, portError(err)
	}
	v.project, v.meeting = p, m
	if v.id, err = f.ParseID[c.Deletion](deletion); err != nil {
		return v, fault(f.InternalError)
	}
	if v.actor, err = f.ParseID[i.User](actor); err != nil {
		return v, fault(f.InternalError)
	}
	if v.request, err = f.ParseID[f.Request](request); err != nil || v.key.Validate() != nil || v.digest.Validate() != nil || v.expected.Validate() != nil || v.accepted <= v.expected || v.accepted-v.expected != 1 || (v.phase != c.DeletionCancelling && v.phase != c.DeletionCompleted) {
		return v, fault(f.InternalError)
	}
	return v, nil
}

type deletionWitnessKey struct{}
type deletionWitness struct {
	authority *DeletionAuthority
	tx        f.Tx
	original  deletionRecord
	intent    intentRecord
	active    bool
}

func (a *DeletionAuthority) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil || a.original == nil || a.guard == nil || ctx == nil || !tx.Valid() || !sameStore(a.original.store, store) {
		return fault(f.Forbidden)
	}
	w, _ := ctx.Value(deletionWitnessKey{}).(*deletionWitness)
	if w == nil || !w.active || w.authority != a || w.tx != tx {
		return fault(f.Forbidden)
	}
	return a.requireCurrentInTx(ctx, tx, store)
}

func (a *DeletionAuthority) requireCurrentInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil || a.original == nil || a.guard == nil || ctx == nil || !tx.Valid() || !sameStore(a.original.store, store) {
		return fault(f.Forbidden)
	}
	if _, err := a.original.store.InTx(tx); err != nil {
		return portError(err)
	}
	return portError(a.guard.RequireCurrentProcessInTx(ctx, tx, a.process))
}

func (a *DeletionAuthority) RequireMeetingDeletionInTx(ctx context.Context, tx f.Tx, p i.ProjectID, m ec.AssociatedMeeting) error {
	if a == nil || a.original == nil {
		return fault(f.DependencyUnbound)
	}
	if err := a.RequireBindingInTx(ctx, tx, a.original.store); err != nil {
		return err
	}
	w := ctx.Value(deletionWitnessKey{}).(*deletionWitness)
	if p != w.original.project || m != w.intent.association() || m.Trigger.MeetingID != w.original.meeting.String() {
		return fault(f.Forbidden)
	}
	command, err := commandIdentity(p, w.original.key)
	if err != nil {
		return err
	}
	if err := a.original.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p, f.Exclusive), meetingLock(w.original.meeting, f.Exclusive), commandLock(command)}); err != nil {
		return portError(err)
	}
	x, err := a.original.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	current, err := loadDeletion(ctx, x, p, w.original.meeting)
	if err != nil {
		return err
	}
	if current != w.original || current.phase != c.DeletionCancelling {
		return fault(f.ConfirmationStale)
	}
	session, err := loadSession(ctx, x, p, current.meeting)
	if err != nil {
		return err
	}
	if !session.Deleting || session.Version < current.accepted {
		return fault(f.ConfirmationStale)
	}
	// Every historical generation/attempt is checked against its own immutable
	// launch. It must not be replaced by the current Contribution pointer.
	intent, err := loadIntentForRequest(ctx, x, w.intent.request)
	if err != nil {
		return err
	}
	if intent.id != w.intent.id || intent.execution == nil || intent.association() != m {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

var _ ec.MeetingExecutionRetirementAuthority = (*DeletionAuthority)(nil)
