package meeting

import (
	"context"
	"encoding/json"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

// DeletionAuditFacts can be composed before Project and Meeting services. It
// accepts only the deletion owner's live, borrowed transaction, not public
// metadata or a technical Actor's service name as evidence.
type DeletionAuditFacts struct{ store Store }

func NewDeletionAuditFacts(store Store) (au.ProjectFactAuthority, error) {
	if nilPort(store) || !sameStore(store, store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &DeletionAuditFacts{store: store}, nil
}

type deletionAuditKey struct{}
type deletionAuditWitness struct {
	authority *DeletionAuthority
	tx        f.Tx
	original  deletionRecord
	entry     au.Entry
	key       au.AppendKey
	rows      int64
	purged    bool
	active    bool
}

func deletionAuditEntry(actor i.Actor, original deletionRecord, rows int64) (au.Entry, au.AppendKey, error) {
	action, ordinal := au.MeetingDeleteAccepted, int64(0)
	if original.phase == c.DeletionCompleted {
		action, ordinal = au.MeetingDeleteCompleted, 1
	}
	meta, err := au.MeetingDeletionMetadata(action, au.MeetingDeletionMetadataFields{
		MeetingID: original.meeting.String(), DeletionID: original.id.String(),
		InitiatorID: original.actor.String(), Version: original.accepted, DeletedRows: rows,
	})
	if err != nil {
		return au.Entry{}, au.AppendKey{}, portError(err)
	}
	scope, err := i.InProject(original.project)
	if err != nil {
		return au.Entry{}, au.AppendKey{}, portError(err)
	}
	resource, err := au.NewResource(au.MeetingResource, original.meeting.String())
	if err != nil {
		return au.Entry{}, au.AppendKey{}, portError(err)
	}
	entry, err := au.NewEntry(au.EntryFields{Actor: actor, Scope: scope, Action: action, Outcome: au.Success,
		Resource: resource, Metadata: meta, Associations: au.Associations{OperationID: original.id.String()}})
	if err != nil {
		return au.Entry{}, au.AppendKey{}, portError(err)
	}
	key, err := au.NewAppendKey(au.MeetingProducer, original.id.String(), ordinal)
	return entry, key, portError(err)
}

func appendDeletionAudit(ctx context.Context, tx f.Tx, appender au.Appender, authority *DeletionAuthority, actor i.Actor, original deletionRecord, rows int64, purged bool) error {
	if nilPort(appender) || authority == nil {
		return fault(f.DependencyUnbound)
	}
	entry, key, err := deletionAuditEntry(actor, original, rows)
	if err != nil {
		return err
	}
	w := &deletionAuditWitness{authority: authority, tx: tx, original: original, entry: entry, key: key, rows: rows, purged: purged, active: true}
	defer func() { w.active = false }()
	_, err = appender.AppendInTx(context.WithValue(ctx, deletionAuditKey{}, w), tx, entry, key)
	return portError(err)
}

func (a *DeletionAuditFacts) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) error {
	if a == nil || nilPort(a.store) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || entry.Validate() != nil || key.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	w, _ := ctx.Value(deletionAuditKey{}).(*deletionAuditWitness)
	if w == nil || !w.active || w.tx != tx || w.authority == nil || w.authority.original == nil || !sameStore(a.store, w.authority.original.store) || key.Details() != w.key.Details() {
		return fault(f.Forbidden)
	}
	e, expected := entry.Fields(), w.entry.Fields()
	actualMeta, err := e.Metadata.MeetingDeletionFields()
	if err != nil {
		return fault(f.Forbidden)
	}
	wantMeta, err := expected.Metadata.MeetingDeletionFields()
	if err != nil || !e.Actor.Equal(expected.Actor) || !e.Scope.Equal(expected.Scope) || e.Action != expected.Action || e.Outcome != expected.Outcome || e.Resource.Details() != expected.Resource.Details() || e.Associations != expected.Associations || actualMeta != wantMeta {
		return fault(f.Forbidden)
	}
	if err := w.authority.requireCurrentInTx(ctx, tx, a.store); err != nil {
		return err
	}
	command, err := commandIdentity(w.original.project, w.original.key)
	if err != nil {
		return err
	}
	locks := []f.LockRequest{projectLock(w.original.project, f.Shared), meetingLock(w.original.meeting, f.Exclusive), commandLock(command)}
	switch e.Action {
	case au.MeetingDeleteAccepted:
		if w.original.phase != c.DeletionCancelling || w.purged || w.rows != 0 || human(e.Actor) != nil || e.Actor.Details().UserID != w.original.actor.String() {
			return fault(f.Forbidden)
		}
		locks = append(locks, userLock(e.Actor))
	case au.MeetingDeleteCompleted:
		actor := e.Actor.Details()
		if w.original.phase != c.DeletionCompleted || !w.purged || w.rows < 1 || actor.Kind != i.Service || actor.ServiceName != i.MeetingRuntime || actor.ProjectID != w.original.project.String() || actor.CauseRef != w.original.id.String() {
			return fault(f.Forbidden)
		}
		locks[0] = projectLock(w.original.project, f.Exclusive)
	default:
		return fault(f.Forbidden)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	original, err := loadDeletion(ctx, x, w.original.project, w.original.meeting)
	if err != nil {
		return err
	}
	if original != w.original {
		return fault(f.ConfirmationStale)
	}
	var raw []byte
	var exact bool
	err = x.QueryRow(ctx, `SELECT receipt,actor_id=$3 AND semantic_digest=$4 AND command='delete' AND meeting_id=$5 FROM agenteam_meeting.commands WHERE project_id=$1 AND idempotency_key=$2`, original.project.String(), original.key.String(), original.actor.String(), original.digest.String(), original.meeting.String()).Scan(&raw, &exact)
	if err != nil {
		return portError(err)
	}
	var receipt c.Receipt
	if !exact || json.Unmarshal(raw, &receipt) != nil || receipt.MeetingID != original.meeting || receipt.Version != original.accepted || receipt.TurnID != nil {
		return fault(f.Forbidden)
	}
	if original.phase == c.DeletionCancelling {
		session, err := loadSession(ctx, x, original.project, original.meeting)
		if err != nil {
			return err
		}
		if !session.Deleting || session.Version != original.accepted {
			return fault(f.ConfirmationStale)
		}
	} else {
		if err := requireDeletionPostimage(ctx, x, original, w.rows); err != nil {
			return err
		}
	}
	return ctx.Err()
}

var _ au.ProjectFactAuthority = (*DeletionAuditFacts)(nil)
