package meeting

import (
	"context"
	"errors"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func loadDeletionReceipt(ctx context.Context, x postgres.SQLExecutor, actor i.Actor, r c.DeleteRequest, d f.Digest) (c.DeleteReceipt, error) {
	var out c.DeleteReceipt
	var raw string
	err := x.QueryRow(ctx, `SELECT id,accepted_version,phase,error_code,created_at,deleted_at FROM agenteam_meeting.deletion_requests WHERE project_id=$1 AND meeting_id=$2 AND actor_id=$3 AND idempotency_key=$4 AND semantic_digest=$5 AND expected_version=$6`, r.ProjectID.String(), r.MeetingID.String(), actor.Details().UserID, r.Meta.IdempotencyKey.String(), d.String(), int64(r.ExpectedVersion)).Scan(&raw, &out.AcceptedVersion, &out.Phase, &out.ErrorCode, &out.RequestedAt, &out.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fault(f.NotFound)
	}
	if err != nil {
		return out, portError(err)
	}
	out.DeletionID, err = f.ParseID[c.Deletion](raw)
	out.MeetingID = r.MeetingID
	if err != nil || out.Validate() != nil {
		return c.DeleteReceipt{}, fault(f.InternalError)
	}
	return out, nil
}

func (s *Service) LookupDelete(ctx context.Context, actor i.Actor, r c.DeleteRequest) (c.DeleteReceipt, bool, error) {
	if s == nil || ctx == nil {
		return c.DeleteReceipt{}, false, fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return c.DeleteReceipt{}, false, err
	}
	if err := r.Validate(); err != nil {
		return c.DeleteReceipt{}, false, err
	}
	d, err := commandDigest(actor, "delete", r)
	if err != nil {
		return c.DeleteReceipt{}, false, err
	}
	command, err := commandIdentity(r.ProjectID, r.Meta.IdempotencyKey)
	if err != nil {
		return c.DeleteReceipt{}, false, err
	}
	locks, err := normalize([]f.LockRequest{userLock(actor), projectLock(r.ProjectID, f.Shared), meetingLock(r.MeetingID, f.Shared), commandLock(command)})
	if err != nil {
		return c.DeleteReceipt{}, false, err
	}
	cause, err := readCause("meeting.delete.lookup")
	if err != nil {
		return c.DeleteReceipt{}, false, err
	}
	var out c.DeleteReceipt
	var found bool
	err = commitError(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, actor, r.ProjectID, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if _, found, err = lookupCommand(ctx, x, r.ProjectID, actor, r.Meta.IdempotencyKey, d); err != nil || !found {
			return err
		}
		out, err = loadDeletionReceipt(ctx, x, actor, r, d)
		out.Replayed = true
		return err
	}))
	if err == nil && found {
		s.invalidateMeeting(r.ProjectID, r.MeetingID)
	}
	return out, found && err == nil, err
}

// Delete durably closes this Meeting's admission gate. Runtime must still
// cancel its original operations and prove their retirement before purging.
// Replays and lookup remain valid after the session row is gone.
func (s *Service) Delete(ctx context.Context, actor i.Actor, r c.DeleteRequest) (c.DeleteReceipt, error) {
	if s == nil || ctx == nil {
		return c.DeleteReceipt{}, fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return c.DeleteReceipt{}, err
	}
	if err := r.Validate(); err != nil {
		return c.DeleteReceipt{}, err
	}
	if nilPort(s.deps.Audit) || s.deps.DeletionAuthority == nil || s.deps.DeletionAuthority.original == nil || !sameStore(s.store, s.deps.DeletionAuthority.original.store) {
		return c.DeleteReceipt{}, fault(f.DependencyUnbound)
	}
	d, err := commandDigest(actor, "delete", r)
	if err != nil {
		return c.DeleteReceipt{}, err
	}
	command, err := commandIdentity(r.ProjectID, r.Meta.IdempotencyKey)
	if err != nil {
		return c.DeleteReceipt{}, err
	}
	// The accepted audit is appended in this transaction. Its original Project
	// authority requires EX, so include that mode before discovering Decision
	// cancellation locks; never upgrade the lock after the transaction starts.
	locks, err := normalize([]f.LockRequest{userLock(actor), projectLock(r.ProjectID, f.Exclusive), meetingLock(r.MeetingID, f.Exclusive), commandLock(command)})
	if err != nil {
		return c.DeleteReceipt{}, err
	}
	if old, found, e := s.LookupDelete(ctx, actor, r); e != nil || found {
		return old, e
	}
	if _, err = s.GetSession(ctx, actor, r.ProjectID, r.MeetingID); err != nil {
		return c.DeleteReceipt{}, err
	}
	decisions, err := prepareDecisionCancellation(ctx, s.store, s.deps.DecisionEvents, actor, decisionEventScope{project: r.ProjectID, meeting: r.MeetingID}, locks)
	if err != nil {
		return c.DeleteReceipt{}, err
	}
	locks = decisions.locks
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		return c.DeleteReceipt{}, err
	}
	var out c.DeleteReceipt
	err = commitError(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, actor, r.ProjectID, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if _, found, err := lookupCommand(ctx, x, r.ProjectID, actor, r.Meta.IdempotencyKey, d); err != nil || found {
			if err == nil {
				out, err = loadDeletionReceipt(ctx, x, actor, r, d)
				out.Replayed = true
			}
			return err
		}
		if err = s.owner(ctx, tx, actor, r.ProjectID, i.Mutate); err != nil {
			return err
		}
		session, err := loadSession(ctx, x, r.ProjectID, r.MeetingID)
		if err != nil {
			return err
		}
		if session.Version != r.ExpectedVersion {
			return fault(f.VersionConflict)
		}
		if session.Deleting {
			return fault(f.ResourceBusy)
		}
		if session.Version == f.Version(math.MaxInt64) {
			return fault(f.InvalidState)
		}
		if err = s.deps.DeletionAuthority.requireCurrentInTx(ctx, tx, s.store); err != nil {
			return err
		}
		deletion, err := f.NewID[c.Deletion]()
		if err != nil {
			return portError(err)
		}
		at := now()
		out = c.DeleteReceipt{DeletionID: deletion, MeetingID: r.MeetingID, AcceptedVersion: session.Version + 1, Phase: c.DeletionCancelling, RequestedAt: at}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET deleting=true,version=version+1,updated_at=$2 WHERE id=$1 AND NOT deleting`, r.MeetingID.String(), at); err != nil {
			return portError(err)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.turns SET cancel_requested=true,version=version+1 WHERE meeting_id=$1 AND status IN('queued','running','finalizing') AND NOT cancel_requested`, r.MeetingID.String()); err != nil {
			return portError(err)
		}
		// The contribution flag belongs to an existing Execution. The session
		// gate also stops unlaunched and unknown launches; cleanup observes
		// their original intent before deciding whether an owner needs cancel.
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.contributions SET cancel_requested=true WHERE meeting_id=$1 AND kind='agent' AND current_execution_id IS NOT NULL AND status IN('pending','waiting_for_agent','running') AND NOT cancel_requested`, r.MeetingID.String()); err != nil {
			return portError(err)
		}
		if err = decisions.applyInTx(ctx, tx, s.store, func(ctx context.Context, tx f.Tx) error { return s.owner(ctx, tx, actor, r.ProjectID, i.Mutate) }); err != nil {
			return err
		}
		if err = saveCommand(ctx, x, r.ProjectID, actor, r.Meta.IdempotencyKey, "delete", d, c.Receipt{MeetingID: r.MeetingID, Version: out.AcceptedVersion}); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.deletion_requests(id,project_id,meeting_id,actor_id,request_id,idempotency_key,semantic_digest,expected_version,accepted_version,phase,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'cancelling',$10)`, deletion.String(), r.ProjectID.String(), r.MeetingID.String(), actor.Details().UserID, r.Meta.RequestID.String(), r.Meta.IdempotencyKey.String(), d.String(), int64(r.ExpectedVersion), int64(out.AcceptedVersion), at)
		if err != nil {
			return portError(err)
		}
		original, err := loadDeletion(ctx, x, r.ProjectID, r.MeetingID)
		if err != nil {
			return err
		}
		return appendDeletionAudit(ctx, tx, s.deps.Audit, s.deps.DeletionAuthority, actor, original, 0, false)
	}))
	if err == nil {
		s.invalidateMeeting(r.ProjectID, r.MeetingID)
	}
	return out, err
}
