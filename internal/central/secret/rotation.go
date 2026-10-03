package secret

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

const RotationBatchSize = 100

type rewrapCandidate struct {
	previous   envelope
	next       envelope
	credential string
}
type rewrapBatch struct {
	run           string
	target, epoch foundation.Version
	last          string
	items         []rewrapCandidate
	completed     bool
}

// PreparedRewrap contains only sealed envelopes and stable identity. It cannot
// be persisted, logged or transferred to a different process for later use.
type PreparedRewrap struct{ data func() rewrapBatch }

func (p PreparedRewrap) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "prepared_secret_rewrap")
}
func (p PreparedRewrap) MarshalJSON() ([]byte, error) { return []byte(`"prepared_secret_rewrap"`), nil }
func (p *PreparedRewrap) UnmarshalJSON([]byte) error  { return invalid() }
func (p PreparedRewrap) LogValue() slog.Value         { return slog.StringValue("prepared_secret_rewrap") }

type RewrapReport struct {
	Applied foundation.Progress `json:"applied"`
}

func (s *Service) PrepareRewrap(ctx context.Context) (PreparedRewrap, error) {
	if err := s.writable(); err != nil {
		return PreparedRewrap{}, err
	}
	state := s.state()
	target, epoch, err := s.control(ctx, state.store)
	if err != nil {
		return PreparedRewrap{}, err
	}
	if target != state.keys.CurrentVersion() {
		return PreparedRewrap{}, failure(EpochChanged, foundation.InvalidState, nil)
	}
	var run, last, status string
	if err = state.store.QueryRow(ctx, `SELECT run_id::text,coalesce(last_payload_id::text,''),state FROM agenteam_secret.secret_rotation_runs WHERE target_version=$1`, int64(target)).Scan(&run, &last, &status); err != nil {
		return PreparedRewrap{}, unavailable(err)
	}
	if status == "failed" {
		return PreparedRewrap{}, failure(RotationFailed, foundation.DependencyUnavailable, nil)
	}
	batch := rewrapBatch{run: run, target: target, epoch: epoch, last: last, completed: status == "completed"}
	if batch.completed {
		return PreparedRewrap{data: func() rewrapBatch { return batch }}, nil
	}
	candidates, err := scanCandidates(ctx, state.store, target, last)
	if err != nil {
		return PreparedRewrap{}, err
	}
	if len(candidates) == 0 && last != "" {
		// Rows can be inserted, replaced or restored before the saved checkpoint.
		// Reaching the end is only a cue to scan from the head again.
		candidates, err = scanCandidates(ctx, state.store, target, "")
		if err != nil {
			return PreparedRewrap{}, err
		}
	}
	for _, p := range candidates {
		credential := p.ownerID
		if p.ownerKind == receiptOwner {
			err = state.store.QueryRow(ctx, `SELECT credential_id::text FROM agenteam_secret.secret_command_receipts WHERE id=$1 AND digest_payload_id=$2`, p.ownerID, p.id.String()).Scan(&credential)
			if errors.Is(err, pgx.ErrNoRows) {
				var exists bool
				if err = state.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads WHERE payload_id=$1)`, p.id.String()).Scan(&exists); err != nil {
					return PreparedRewrap{}, unavailable(err)
				}
				if !exists {
					batch.last = p.id.String()
					continue
				}
				return PreparedRewrap{}, unavailable(nil)
			}
			if err != nil {
				return PreparedRewrap{}, unavailable(err)
			}
		}
		if _, err = foundation.ParseID[struct{}](credential); err != nil {
			return PreparedRewrap{}, unavailable(err)
		}
		nonce, err := s.nextNonce(ctx, target)
		if err != nil {
			return PreparedRewrap{}, err
		}
		next, err := rewrap(state.keys, p, target, nonce)
		if err != nil {
			return PreparedRewrap{}, err
		}
		batch.items = append(batch.items, rewrapCandidate{p, next, credential})
		batch.last = p.id.String()
	}
	return PreparedRewrap{data: func() rewrapBatch { return batch }}, nil
}
func scanCandidates(ctx context.Context, e postgres.SQLExecutor, target foundation.Version, last string) ([]envelope, error) {
	rows, err := e.Query(ctx, `SELECT `+payloadColumns+` FROM agenteam_secret.secret_payloads WHERE master_version<>$1 AND ($2::uuid IS NULL OR payload_id>$2::uuid) ORDER BY payload_id LIMIT 100`, int64(target), null(last))
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	candidates := make([]envelope, 0, RotationBatchSize)
	for rows.Next() {
		p, err := scanPayload(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, p)
	}
	if err = rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	return candidates, nil
}
func (s *Service) ApplyPreparedRewrapInTx(ctx context.Context, tx foundation.Tx, prepared PreparedRewrap) (RewrapReport, error) {
	if prepared.data == nil {
		return RewrapReport{}, failure(PreparationRequired, foundation.ResourceBusy, nil)
	}
	b := prepared.data()
	state := s.state()
	e, err := state.store.InTx(tx)
	if err != nil {
		return RewrapReport{}, unavailable(err)
	}
	locks := []foundation.LockRequest{{Key: writeLock(), Mode: foundation.Shared}}
	for _, c := range b.items {
		if c.previous.scope.Details().Kind == identity.ProjectScope {
			key, _ := foundation.ProjectLock(c.previous.scope.Details().ProjectID)
			locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
		}
		key, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, c.credential)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Exclusive})
	}
	if err = state.store.AcquireAll(ctx, tx, locks); err != nil {
		return RewrapReport{}, unavailable(err)
	}
	target, epoch, err := s.control(ctx, e)
	if err != nil {
		return RewrapReport{}, err
	}
	if target != b.target || epoch != b.epoch {
		return RewrapReport{}, failure(EpochChanged, foundation.InvalidState, nil)
	}
	var status string
	if err = e.QueryRow(ctx, `SELECT state FROM agenteam_secret.secret_rotation_runs WHERE run_id=$1 AND target_version=$2 FOR UPDATE`, b.run, int64(target)).Scan(&status); err != nil {
		return RewrapReport{}, unavailable(err)
	}
	if status == "completed" {
		return RewrapReport{}, nil
	}
	if status != "running" {
		return RewrapReport{}, failure(RotationFailed, foundation.DependencyUnavailable, nil)
	}
	var applied int64
	for _, c := range b.items {
		p := c.next
		tag, err := e.Exec(ctx, `UPDATE agenteam_secret.secret_payloads SET wrap_nonce=$2,wrapped_dek=$3,master_version=$4,wrap_revision=$5 WHERE payload_id=$1 AND master_version=$6 AND wrap_revision=$7`, p.id.String(), p.wrapNonce, p.wrappedDEK, int64(p.masterVersion), int64(p.wrapRevision), int64(c.previous.masterVersion), int64(c.previous.wrapRevision))
		if err != nil {
			return RewrapReport{}, unavailable(err)
		}
		applied += tag.RowsAffected()
	}
	if _, err = e.Exec(ctx, `UPDATE agenteam_secret.secret_rotation_runs SET last_payload_id=$2,processed=processed+$3,updated_at=clock_timestamp() WHERE run_id=$1`, b.run, null(b.last), applied); err != nil {
		return RewrapReport{}, unavailable(err)
	}
	return RewrapReport{Applied: foundation.Progress(applied)}, nil
}
func (s *Service) finishRotation(ctx context.Context, b rewrapBatch) (bool, error) {
	state := s.state()
	cause, err := recoveryCause("secret-rotation-fence")
	if err != nil {
		return false, err
	}
	complete := false
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := state.store.Acquire(ctx, tx, writeLock(), foundation.Exclusive); err != nil {
			return unavailable(err)
		}
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		target, epoch, err := s.control(ctx, e)
		if err != nil {
			return err
		}
		if target != b.target || epoch != b.epoch {
			return failure(EpochChanged, foundation.InvalidState, nil)
		}
		var status string
		var processed int64
		if err = e.QueryRow(ctx, `SELECT state,processed FROM agenteam_secret.secret_rotation_runs WHERE run_id=$1 AND target_version=$2 FOR UPDATE`, b.run, int64(target)).Scan(&status, &processed); err != nil {
			return unavailable(err)
		}
		if status == "completed" {
			complete = true
			return nil
		}
		if status != "running" {
			return failure(RotationFailed, foundation.DependencyUnavailable, nil)
		}
		var remains bool
		// This deliberately has no checkpoint predicate. Shared writer/rewrap Tx
		// have ended and old epoch candidates cannot become new old-key references.
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads WHERE master_version<>$1)`, int64(target)).Scan(&remains); err != nil {
			return unavailable(err)
		}
		if remains {
			return nil
		}
		if _, err = e.Exec(ctx, `UPDATE agenteam_secret.secret_rotation_runs SET state='completed',safe_error=NULL,updated_at=clock_timestamp() WHERE run_id=$1`, b.run); err != nil {
			return unavailable(err)
		}
		if _, err = e.Exec(ctx, `UPDATE agenteam_secret.secret_master_registry SET retireable=true WHERE version<$1`, int64(target)); err != nil {
			return unavailable(err)
		}
		if err = s.maintenanceAudit(ctx, tx, ac.RotationComplete, target, b.run, 1, foundation.Progress(processed), ""); err != nil {
			return err
		}
		complete = true
		return nil
	})
	if err = commitError(result); err != nil {
		return false, err
	}
	return complete, nil
}

// MaintenanceStep attempts at most one batch or final fence. An unknown commit
// is resolved by fresh persistent state, never by replaying an in-memory count.
func (s *Service) MaintenanceStep(ctx context.Context) (bool, error) {
	select {
	case <-s.state().stop:
		return false, context.Canceled
	default:
	}
	prepared, err := s.PrepareRewrap(ctx)
	if err != nil {
		return false, s.rotationFailure(ctx, err)
	}
	b := prepared.data()
	if b.completed {
		return true, s.refreshRotation(ctx)
	}
	var completed bool
	if len(b.items) == 0 {
		completed, err = s.finishRotation(ctx, b)
	} else {
		cause, causeErr := recoveryCause("secret-rotation-batch")
		if causeErr != nil {
			return false, s.rotationFailure(ctx, causeErr)
		}
		result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			_, err := s.ApplyPreparedRewrapInTx(ctx, tx, prepared)
			return err
		})
		err = commitError(result)
	}
	if err != nil {
		var fault *foundation.Fault
		if errors.As(err, &fault) && fault.CommitState == foundation.Unknown {
			if refreshErr := s.refreshRotation(ctx); refreshErr == nil {
				return s.Status().Rotation == "completed", nil
			}
		}
		return false, s.rotationFailure(ctx, err)
	}
	if err = s.refreshRotation(ctx); err != nil {
		return false, s.rotationFailure(ctx, err)
	}
	return completed, nil
}
func (s *Service) refreshRotation(ctx context.Context) error {
	state := s.state()
	var version, epoch, remaining int64
	var status string
	err := state.store.QueryRow(ctx, `SELECT c.current_write_version,c.write_epoch,r.state,(SELECT count(*) FROM agenteam_secret.secret_payloads WHERE master_version<>c.current_write_version) FROM agenteam_secret.secret_control c JOIN agenteam_secret.secret_rotation_runs r ON r.target_version=c.current_write_version WHERE singleton`).Scan(&version, &epoch, &status, &remaining)
	if err != nil {
		return unavailable(err)
	}
	if foundation.Version(version) != state.keys.CurrentVersion() {
		return failure(EpochChanged, foundation.InvalidState, nil)
	}
	state.mu.Lock()
	state.writeVersion = foundation.Version(version)
	state.epoch = foundation.Version(epoch)
	state.rotationState = status
	state.remaining = foundation.Progress(remaining)
	state.mu.Unlock()
	return nil
}
func (s *Service) rotationFailure(ctx context.Context, original error) error {
	if ctx.Err() != nil {
		return original
	}
	state := s.state()
	state.mu.Lock()
	state.unavailable = true
	state.rotationState = "failed"
	state.mu.Unlock()
	code := RotationFailed
	reason := ac.RotationError
	var se *Error
	if errors.As(original, &se) {
		switch se.Code() {
		case DecryptFailed:
			code = DecryptFailed
			reason = ac.DecryptFailed
		case KeyUnavailable:
			code = KeyUnavailable
			reason = ac.KeyUnavailable
		case EpochChanged:
			code = EpochChanged
		case Unavailable, NonceReservationUnknown, CommitUnknown:
			code = Unavailable
		}
	}
	cause, err := recoveryCause("secret-rotation-failure")
	if err != nil {
		return original
	}
	// Persist a fixed code and a monotonically allocated failure ordinal. A
	// later resumed attempt can fail at a different count without reusing an
	// Audit append key for different semantics.
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := state.store.Acquire(ctx, tx, writeLock(), foundation.Shared); err != nil {
			return unavailable(err)
		}
		e, err := state.store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		var run string
		var count, failures int64
		err = e.QueryRow(ctx, `UPDATE agenteam_secret.secret_rotation_runs SET state='failed',safe_error=$2,failure_count=failure_count+1,updated_at=clock_timestamp() WHERE target_version=$1 AND state='running' RETURNING run_id::text,processed,failure_count`, int64(state.keys.CurrentVersion()), string(code)).Scan(&run, &count, &failures)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return unavailable(err)
		}
		return s.maintenanceAudit(ctx, tx, ac.RotationFailed, state.keys.CurrentVersion(), run, failures+1, foundation.Progress(count), reason)
	})
	if err = commitError(result); err != nil {
		// No success or retirement is inferred when even the failure journal cannot
		// be confirmed. Runtime status is already unavailable; restart rechecks DB.
		return original
	}
	return original
}

// StopMaintenance stops admission of the next batch. The current short batch
// retains its context and may drain alongside in-flight HTTP requests.
func (s *Service) StopMaintenance() { s.state().stopOnce.Do(func() { close(s.state().stop) }) }
func (s *Service) RunMaintenance(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.state().stop:
			return nil
		default:
		}
		done, err := s.MaintenanceStep(ctx)
		if err != nil {
			select {
			case <-s.state().stop:
				return nil
			default:
			}
			return err
		}
		if done {
			return nil
		}
	}
}
