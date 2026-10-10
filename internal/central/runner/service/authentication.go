package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

type connectionOwner struct{}
type connectionID struct{}

// Connection is a reservation from a known committed authentication. It is not
// an online status or a permission that survives a later DB generation change.
// An external caller cannot mint it from public IDs or another Service's DTO.
type Connection struct{ data func() connectionReservation }
type connectionReservation struct {
	issuer                 *serviceState
	runner                 c.RunnerID
	id                     f.ID[connectionID]
	credential, generation int64
	root                   string
}

func (v Connection) RunnerID() c.RunnerID {
	if v.data == nil {
		return c.RunnerID{}
	}
	return v.data().runner
}
func (v Connection) ID() p.ID {
	if v.data == nil {
		return ""
	}
	return p.ID(v.data().id.String())
}
func (v Connection) RootPath() string {
	if v.data == nil {
		return ""
	}
	return v.data().root
}
func (Connection) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_connection") }
func (Connection) MarshalJSON() ([]byte, error) { return []byte(`"runner_connection"`), nil }
func (Connection) LogValue() slog.Value         { return slog.StringValue("runner_connection") }
func (s *Service) reservation(v Connection) (connectionReservation, error) {
	if s.state() == nil {
		return connectionReservation{}, fault(f.DependencyUnbound)
	}
	if v.data == nil {
		return connectionReservation{}, fault(f.Unauthenticated)
	}
	r := v.data()
	if r.issuer != s.state() || r.runner.Validate() != nil || r.id.Validate() != nil || r.credential < 1 || r.generation < 1 {
		return connectionReservation{}, fault(f.Unauthenticated)
	}
	return r, nil
}

func (s *Service) Authenticate(ctx context.Context, request p.Authentication) (Connection, error) {
	if !request.Valid() {
		return Connection{}, fault(f.Unauthenticated)
	}
	target, e := f.ParseID[c.Runner](string(request.RunnerID()))
	if e != nil {
		return Connection{}, fault(f.Unauthenticated)
	}
	hash, _ := request.Nonce().Digest()
	var out Connection
	e = s.deviceTransaction(ctx, target, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		before, e := loadDevice(ctx, x, target)
		if e != nil {
			return e
		}
		if len(before.public) != 32 {
			return fault(f.Unauthenticated)
		}
		now, e := databaseNow(ctx, x)
		if e != nil {
			return e
		}
		seconds, e := request.Timestamp().Seconds()
		if e != nil || seconds < now.Time().Unix()-30 || seconds > now.Time().Unix()+30 {
			return fault(f.Unauthenticated)
		}
		var saved []byte
		var issued, expires time.Time
		e = x.QueryRow(ctx, `SELECT nonce_hash,issued_at,expires_at FROM agenteam_runner.challenges WHERE nonce_hash=$1 AND runner_id=$2 AND credential_generation=$3 AND consumed_at IS NULL AND revoked_at IS NULL`, hash[:], target.String(), before.generation).Scan(&saved, &issued, &expires)
		if errors.Is(e, pgx.ErrNoRows) {
			return fault(f.Unauthenticated)
		}
		if e != nil {
			return unavailable(e)
		}
		if subtle.ConstantTimeCompare(saved, hash[:]) != 1 || now.Time().Before(issued) || !now.Time().Before(expires) || !request.Verify([32]byte(before.public)) {
			return fault(f.Unauthenticated)
		}
		if before.sequence == math.MaxInt64 {
			return fault(f.ResourceBusy)
		}
		connection, e := f.NewID[connectionID]()
		if e != nil {
			return unavailable(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_runner.challenges SET consumed_at=$2 WHERE nonce_hash=$1 AND consumed_at IS NULL AND revoked_at IS NULL`, hash[:], now.Time())
		if e = affected(tag, e); e != nil {
			return e
		}
		tag, e = x.Exec(ctx, `UPDATE agenteam_runner.runners SET connection_generation=connection_generation+1 WHERE id=$1 AND credential_generation=$2 AND connection_generation=$3`, target.String(), before.generation, before.sequence)
		if e = affected(tag, e); e != nil {
			return e
		}
		tag, e = x.Exec(ctx, `INSERT INTO agenteam_runner.connections(runner_id,id,credential_generation,generation,owner_id,authenticated_at,last_seen_at,lease_expires_at) VALUES($1,$2,$3,$4,$5,$6,$6,$7)
 ON CONFLICT(runner_id) DO UPDATE SET id=EXCLUDED.id,credential_generation=EXCLUDED.credential_generation,generation=EXCLUDED.generation,owner_id=EXCLUDED.owner_id,authenticated_at=EXCLUDED.authenticated_at,hello_at=NULL,last_seen_at=EXCLUDED.last_seen_at,lease_expires_at=EXCLUDED.lease_expires_at,heartbeat_sequence=0`, target.String(), connection.String(), before.generation, before.sequence+1, s.state().owner.String(), now.Time(), now.Time().Add(30*time.Second))
		if e = affected(tag, e); e != nil {
			return e
		}
		value := connectionReservation{s.state(), target, connection, before.generation, before.sequence + 1, before.root}
		out = Connection{data: func() connectionReservation { return value }}
		return nil
	})
	if e != nil {
		return Connection{}, e
	}
	return out, nil
}
