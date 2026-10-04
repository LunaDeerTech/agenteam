package object

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// DownloadResult describes bytes accepted by the HTTP writer, never proof that
// a remote client received them. AuditPending preserves a real terminal result
// whose Audit transaction has not yet been confirmed. It never invites replay.
type DownloadResult struct {
	AttemptID    oc.DownloadAttemptID `json:"attempt_id"`
	Phase        oc.DownloadPhase     `json:"phase"`
	Length       foundation.Progress  `json:"length"`
	SentBytes    foundation.Progress  `json:"sent_bytes"`
	Failure      oc.DownloadFailure   `json:"failure,omitempty"`
	AuditPending bool                 `json:"audit_pending"`
}
type downloadAttemptRow struct {
	grant          oc.DownloadGrantID
	offset, length int64
	phase          oc.DownloadPhase
	pending        oc.DownloadPhase
	bytes          *int64
	failure        oc.DownloadFailure
}

func loadDownloadAttempt(ctx context.Context, e postgres.SQLExecutor, id oc.DownloadAttemptID) (downloadAttemptRow, error) {
	var row downloadAttemptRow
	var grant string
	var pending, failure *string
	err := e.QueryRow(ctx, `SELECT grant_id::text,offset_bytes,length_bytes,phase,pending_phase,pending_bytes,pending_failure FROM agenteam_download.attempts WHERE id=$1`, id.String()).Scan(&grant, &row.offset, &row.length, &row.phase, &pending, &row.bytes, &failure)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, failureErrorDownload()
	}
	if err != nil {
		return row, unavailable(err)
	}
	row.grant, err = foundation.ParseID[oc.DownloadGrant](grant)
	if err != nil {
		return row, unavailable(err)
	}
	if pending != nil {
		row.pending = oc.DownloadPhase(*pending)
	}
	if failure != nil {
		row.failure = oc.DownloadFailure(*failure)
	}
	return row, nil
}
func failureErrorDownload() error { return failure(foundation.Forbidden, nil) }
func (d *Downloads) start(ctx context.Context, actor identity.Actor, c downloadClaims, id oc.DownloadAttemptID, offset, length int64) error {
	result := d.within(ctx, actor, c, func(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if err := checkGrant(ctx, e, c, true); err != nil {
			return err
		}
		_, err := e.Exec(ctx, `INSERT INTO agenteam_download.attempts(id,grant_id,session_id,offset_bytes,length_bytes,phase) VALUES($1,$2,$3,$4,$5,'started')`, id.String(), c.grant.String(), actor.Details().SessionID, offset, length)
		if err != nil {
			return unavailable(err)
		}
		return d.state().provider.AppendDownloadInTx(ctx, tx, actor, c.target, oc.DownloadEvent{GrantID: c.grant, AttemptID: id, Phase: oc.DownloadStarted, Length: foundation.Progress(length)}, plan, locked)
	})
	if result.State() == foundation.Unknown {
		result = d.within(ctx, actor, c, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
			if err := checkGrant(ctx, e, c, true); err != nil {
				return err
			}
			row, err := loadDownloadAttempt(ctx, e, id)
			if err != nil {
				return err
			}
			if row.grant != c.grant || row.offset != offset || row.length != length || row.phase != oc.DownloadStarted {
				return failure(foundation.Forbidden, nil)
			}
			return nil
		})
		if result.State() != foundation.Committed {
			return failure(foundation.CommitUnknown, nil)
		}
	}
	return commitError(result)
}
func sameDownloadOutcome(row downloadAttemptRow, event oc.DownloadEvent) bool {
	return row.grant == event.GrantID && row.length == int64(event.Length) && row.pending == event.Phase && row.bytes != nil && *row.bytes == int64(event.SentBytes) && row.failure == event.Failure
}
func (d *Downloads) checkpoint(ctx context.Context, actor identity.Actor, c downloadClaims, event oc.DownloadEvent) error {
	// This technical result is retained independently of the terminal Audit Tx.
	// Authorization loss may prevent the eventual business Audit, but must never
	// erase the actual accepted byte count. It is only writable through the
	// private stream instance, after its body has closed and joined.
	r := d.state()
	store := r.objects.state().store
	result := store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		locks := []foundation.LockRequest{downloadGrantLock(c.grant)}
		if project := c.target.Details().Source.Details().Meta.Scope.Details().ProjectID; project != "" {
			key, err := foundation.ProjectLock(project)
			if err != nil {
				return invalid()
			}
			locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
		}
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		e, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = checkGrant(ctx, e, c, false); err != nil {
			return err
		}
		if c.user.String() != actor.Details().UserID {
			return failure(foundation.Forbidden, nil)
		}
		row, err := loadDownloadAttempt(ctx, e, event.AttemptID)
		if err != nil {
			return err
		}
		if row.grant != c.grant || row.length != int64(event.Length) {
			return failure(foundation.Forbidden, nil)
		}
		if row.pending != "" {
			if sameDownloadOutcome(row, event) {
				return nil
			}
			return failure(foundation.InvalidState, nil)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_download.attempts SET pending_phase=$2,pending_bytes=$3,pending_failure=$4 WHERE id=$1 AND phase='started'`, event.AttemptID.String(), string(event.Phase), int64(event.SentBytes), nullDownload(string(event.Failure)))
		return unavailableIf(err)
	})
	return commitError(result)
}
func (d *Downloads) complete(ctx context.Context, actor identity.Actor, c downloadClaims, event oc.DownloadEvent) error {
	if event.Validate() != nil || event.Phase != oc.DownloadSent && event.Phase != oc.DownloadFailed {
		return invalid()
	}
	result := d.within(ctx, actor, c, func(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if err := checkGrant(ctx, e, c, false); err != nil {
			return err
		}
		row, err := loadDownloadAttempt(ctx, e, event.AttemptID)
		if err != nil {
			return err
		}
		if !sameDownloadOutcome(row, event) {
			return failure(foundation.InvalidState, nil)
		}
		if row.phase == event.Phase {
			return nil
		}
		if row.phase != oc.DownloadStarted {
			return failure(foundation.InvalidState, nil)
		}
		if err = d.state().provider.AppendDownloadInTx(ctx, tx, actor, c.target, event, plan, locked); err != nil {
			return portError(err)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_download.attempts SET phase=$2,sent_bytes=$3,failure=$4,finished_at=clock_timestamp() WHERE id=$1`, event.AttemptID.String(), string(event.Phase), int64(event.SentBytes), nullDownload(string(event.Failure)))
		return unavailableIf(err)
	})
	if result.State() == foundation.Unknown {
		result = d.within(ctx, actor, c, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
			if err := checkGrant(ctx, e, c, false); err != nil {
				return err
			}
			row, err := loadDownloadAttempt(ctx, e, event.AttemptID)
			if err != nil {
				return err
			}
			if !sameDownloadOutcome(row, event) || row.phase != event.Phase {
				return failure(foundation.CommitUnknown, nil)
			}
			return nil
		})
		if result.State() != foundation.Committed {
			return failure(foundation.CommitUnknown, nil)
		}
	}
	return commitError(result)
}

// ConfirmDownloadOutcome rechecks one durable terminal checkpoint under the
// current Human/provider permissions. It never opens storage or sends payload.
// Started-only attempts remain unresolved; time is not completion evidence.
func (d *Downloads) ConfirmDownloadOutcome(ctx context.Context, actor identity.Actor, id oc.DownloadAttemptID) (DownloadResult, error) {
	if downloadHuman(actor) != nil || id.Validate() != nil {
		return DownloadResult{}, failure(foundation.Forbidden, nil)
	}
	r := d.state()
	op, finish, err := r.objects.begin(ctx)
	if err != nil {
		return DownloadResult{}, err
	}
	defer finish()
	ctx = op.ctx
	before, err := loadDownloadAttempt(ctx, r.objects.state().store, id)
	if err != nil {
		return DownloadResult{}, err
	}
	c, err := readGrant(ctx, r.objects.state().store, before.grant)
	if err != nil {
		return DownloadResult{}, err
	}
	var event oc.DownloadEvent
	result := d.within(ctx, actor, c, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
		if err := checkGrant(ctx, e, c, false); err != nil {
			return err
		}
		row, err := loadDownloadAttempt(ctx, e, id)
		if err != nil {
			return err
		}
		if row.grant != c.grant {
			return failure(foundation.Forbidden, nil)
		}
		if row.pending == "" || row.bytes == nil {
			return failure(foundation.ResourceBusy, nil)
		}
		event = oc.DownloadEvent{GrantID: c.grant, AttemptID: id, Phase: row.pending, Length: foundation.Progress(row.length), SentBytes: foundation.Progress(*row.bytes), Failure: row.failure}
		return event.Validate()
	})
	if err = commitError(result); err != nil {
		return DownloadResult{}, err
	}
	out := resultFromDownload(event)
	out.AuditPending = true
	err = d.complete(ctx, actor, c, event)
	out.AuditPending = err != nil
	return out, err
}
func resultFromDownload(e oc.DownloadEvent) DownloadResult {
	return DownloadResult{AttemptID: e.AttemptID, Phase: e.Phase, Length: e.Length, SentBytes: e.SentBytes, Failure: e.Failure}
}
