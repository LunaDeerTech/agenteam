package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"math"
	"sort"
	"strconv"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// Device operations have no Human Actor. All credential changes and challenge
// capacity decisions serialize with management before reading current facts.
func deviceLocks(target c.RunnerID) ([]f.LockRequest, error) {
	if target.Validate() != nil {
		return nil, fault(f.Unauthenticated)
	}
	global, _ := f.SystemConfigLock("runner-management")
	connection, e := f.SystemConfigLock("runner-control-" + target.String())
	if e != nil {
		return nil, fault(f.Unauthenticated)
	}
	held := []f.LockRequest{{Key: global, Mode: f.Exclusive}, {Key: connection, Mode: f.Exclusive}}
	sort.Slice(held, func(i, j int) bool { return f.CompareLockKeys(held[i].Key, held[j].Key) < 0 })
	return held, nil
}

func (s *Service) deviceTransaction(ctx context.Context, target c.RunnerID, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	held, e := deviceLocks(target)
	if e != nil {
		return e
	}
	run, e := f.NewID[struct{}]()
	if e != nil {
		return unavailable(e)
	}
	cause, e := f.NewRecoveryCause("runner-device", run.String(), "")
	if e != nil {
		return unavailable(e)
	}
	store := s.state().authority.state().store
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := store.AcquireAll(ctx, tx, held); e != nil {
			return portError(e)
		}
		x, e := store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		return fn(ctx, tx, x)
	})
	return resultError(result)
}

type deviceRecord struct {
	root                          string
	version, generation, sequence int64
	public                        []byte
	updated                       time.Time
}

func loadDevice(ctx context.Context, x postgres.SQLExecutor, target c.RunnerID) (deviceRecord, error) {
	var v deviceRecord
	e := x.QueryRow(ctx, `SELECT root_path,version,credential_generation,connection_generation,device_public_key,updated_at FROM agenteam_runner.runners WHERE id=$1`, target.String()).Scan(&v.root, &v.version, &v.generation, &v.sequence, &v.public, &v.updated)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, fault(f.Unauthenticated)
	}
	if e != nil {
		return v, unavailable(e)
	}
	if v.version < 1 || v.generation < 1 || v.sequence < 0 || v.public != nil && len(v.public) != 32 {
		return v, unavailable(nil)
	}
	return v, nil
}

// Enroll publishes only a known committed postimage. Lost/Unknown responses
// retain the caller's pending key; repeating the consumed token is never replay.
func (s *Service) Enroll(ctx context.Context, request p.EnrollmentRequest) (p.EnrollmentResponse, error) {
	if _, e := p.EncodeEnrollmentRequest(request); e != nil {
		return p.EnrollmentResponse{}, fault(f.InvalidArgument)
	}
	target, e := f.ParseID[c.Runner](string(request.RunnerID()))
	if e != nil {
		return p.EnrollmentResponse{}, fault(f.InvalidArgument)
	}
	hash, _ := request.Token().Digest()
	var out p.EnrollmentResponse
	e = s.deviceTransaction(ctx, target, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		before, e := loadDevice(ctx, x, target)
		if e != nil {
			return e
		}
		if before.public != nil || before.root != request.RootPath() {
			return fault(f.Unauthenticated)
		}
		now, e := databaseNow(ctx, x)
		if e != nil {
			return e
		}
		var saved []byte
		var issued, expires time.Time
		e = x.QueryRow(ctx, `SELECT token_hash,issued_at,expires_at FROM agenteam_runner.enrollment_tokens WHERE token_hash=$1 AND runner_id=$2 AND credential_generation=$3 AND consumed_at IS NULL AND revoked_at IS NULL`, hash[:], target.String(), before.generation).Scan(&saved, &issued, &expires)
		if errors.Is(e, pgx.ErrNoRows) {
			return fault(f.Unauthenticated)
		}
		if e != nil {
			return unavailable(e)
		}
		if subtle.ConstantTimeCompare(saved, hash[:]) != 1 || now.Time().Before(issued) || !now.Time().Before(expires) {
			return fault(f.Unauthenticated)
		}
		if before.version == math.MaxInt64 {
			return fault(f.ResourceBusy)
		}
		at := now.Time()
		if at.Before(before.updated) {
			at = before.updated
		}
		public := request.PublicKey()
		tag, e := x.Exec(ctx, `UPDATE agenteam_runner.enrollment_tokens SET consumed_at=$2 WHERE token_hash=$1 AND consumed_at IS NULL AND revoked_at IS NULL`, hash[:], now.Time())
		if e = affected(tag, e); e != nil {
			return e
		}
		tag, e = x.Exec(ctx, `UPDATE agenteam_runner.runners SET device_public_key=$2,enrolled_at=$3,version=version+1,updated_at=$3,incompatible=false WHERE id=$1 AND device_public_key IS NULL`, target.String(), public[:], at)
		if e = affected(tag, e); e != nil {
			return e
		}
		event, e := f.NewID[c.IdentityEvent]()
		if e != nil {
			return unavailable(e)
		}
		fp := p.PublicKeyFingerprint(public)
		tag, e = x.Exec(ctx, `INSERT INTO agenteam_runner.identity_events(id,runner_id,kind,credential_generation,version,public_key_fingerprint,occurred_at) VALUES($1,$2,'enrolled',$3,$4,$5,$6)`, event.String(), target.String(), before.generation, before.version+1, fp, at)
		if e = affected(tag, e); e != nil {
			return e
		}
		current, e := loadRunner(ctx, x, target)
		if e != nil {
			return e
		}
		if e = s.appendEnrollment(ctx, tx, event, current, hash, now.Time()); e != nil {
			return e
		}
		out, e = p.NewEnrollmentResponse(request.RunnerID(), p.Decimal(strconv.FormatInt(before.version+1, 10)), p.Decimal(strconv.FormatInt(before.generation, 10)), p.Instant(at.UTC().Format(time.RFC3339Nano)), fp)
		if e != nil {
			return unavailable(e)
		}
		return nil
	})
	if e != nil {
		return p.EnrollmentResponse{}, e
	}
	return out, nil
}

func (s *Service) Challenge(ctx context.Context, request p.ChallengeRequest) (p.ChallengeResponse, error) {
	if _, e := p.EncodeChallengeRequest(request); e != nil {
		return p.ChallengeResponse{}, fault(f.InvalidArgument)
	}
	target, e := f.ParseID[c.Runner](string(request.RunnerID()))
	if e != nil {
		return p.ChallengeResponse{}, fault(f.InvalidArgument)
	}
	var out p.ChallengeResponse
	e = s.deviceTransaction(ctx, target, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		record, e := loadDevice(ctx, x, target)
		if e != nil {
			return e
		}
		if len(record.public) != 32 {
			return fault(f.Unauthenticated)
		}
		now, e := databaseNow(ctx, x)
		if e != nil {
			return e
		}
		// Cleanup is part of this admitted bounded call, not a detached worker.
		// The live capacity query excludes retired rows even if the batch fills.
		_, e = x.Exec(ctx, `DELETE FROM agenteam_runner.challenges WHERE nonce_hash IN (SELECT nonce_hash FROM agenteam_runner.challenges WHERE expires_at<=$1 OR consumed_at IS NOT NULL OR revoked_at IS NOT NULL ORDER BY expires_at,nonce_hash LIMIT 128)`, now.Time())
		if e != nil {
			return unavailable(e)
		}
		var total, own int64
		e = x.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE runner_id=$2) FROM agenteam_runner.challenges WHERE expires_at>$1 AND consumed_at IS NULL AND revoked_at IS NULL`, now.Time(), target.String()).Scan(&total, &own)
		if e != nil {
			return unavailable(e)
		}
		if total >= 16384 || own >= 4 {
			return fault(f.ResourceBusy)
		}
		nonce, e := p.NewNonce()
		if e != nil {
			return unavailable(e)
		}
		hash, _ := nonce.Digest()
		expires := now.Time().Add(30 * time.Second)
		tag, e := x.Exec(ctx, `INSERT INTO agenteam_runner.challenges(nonce_hash,runner_id,credential_generation,issued_at,expires_at) VALUES($1,$2,$3,$4,$5)`, hash[:], target.String(), record.generation, now.Time(), expires)
		if e = affected(tag, e); e != nil {
			return e
		}
		out, e = p.NewChallengeResponse(nonce, p.Instant(expires.UTC().Format(time.RFC3339Nano)))
		if e != nil {
			return unavailable(e)
		}
		return nil
	})
	if e != nil {
		return p.ChallengeResponse{}, e
	}
	return out, nil
}
