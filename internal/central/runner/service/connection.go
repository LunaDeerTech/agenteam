package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const connectionBudget = 5 * time.Second

// The gate holds only the Runner's connection lock, never global management
// or a User lock. A replacement/revocation waits until the real write returns.
// Canceling a context cannot make the callback or its transaction look joined.
func (s *Service) withConnection(ctx context.Context, connection Connection, write, requireHello, live bool, work func(context.Context, postgres.SQLExecutor, connectionReservation) error) error {
	r, e := s.reservation(connection)
	if e != nil {
		return e
	}
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	bounded, cancel := context.WithTimeout(ctx, connectionBudget)
	defer cancel()
	key, e := f.SystemConfigLock("runner-control-" + r.runner.String())
	if e != nil {
		return fault(f.InvalidArgument)
	}
	mode := f.Shared
	if write {
		mode = f.Exclusive
	}
	run, e := f.NewID[struct{}]()
	if e != nil {
		return unavailable(e)
	}
	cause, e := f.NewRecoveryCause("runner-connection", run.String(), "")
	if e != nil {
		return unavailable(e)
	}
	store := s.state().authority.state().store
	result := store.WithinTx(bounded, cause, func(ctx context.Context, tx f.Tx) error {
		if e := store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: mode}}); e != nil {
			return portError(e)
		}
		x, e := store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		var current bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_runner.connections c JOIN agenteam_runner.runners r ON r.id=c.runner_id WHERE c.runner_id=$1 AND c.id=$2 AND c.owner_id=$3 AND c.credential_generation=$4 AND c.generation=$5 AND r.credential_generation=c.credential_generation AND r.connection_generation=c.generation AND r.device_public_key IS NOT NULL AND (NOT $6::boolean OR c.hello_at IS NOT NULL) AND (NOT $7::boolean OR c.lease_expires_at>clock_timestamp()))`, r.runner.String(), r.id.String(), s.state().owner.String(), r.credential, r.generation, requireHello, live).Scan(&current)
		if e != nil {
			return unavailable(e)
		}
		if !current {
			return fault(f.Unauthenticated)
		}
		return work(ctx, x, r)
	})
	return resultError(result)
}

// WithCurrentConnection surrounds one actual bounded write or inbound
// publication. It does not authorize a business operation, and must not enclose
// an operation runtime. The server supplies the original socket deadline too.
func (s *Service) WithCurrentConnection(ctx context.Context, connection Connection, work func(context.Context) error) error {
	if work == nil {
		return fault(f.InvalidArgument)
	}
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	return s.withConnection(ctx, connection, false, false, true, func(ctx context.Context, _ postgres.SQLExecutor, _ connectionReservation) error {
		return portError(work(ctx))
	})
}

func (s *Service) CurrentConnection(ctx context.Context, connection Connection) error {
	return s.WithCurrentConnection(ctx, connection, func(context.Context) error { return nil })
}

// OwnConnection retains ownership through the original socket/runtime callback
// and bounded conditional retirement. Stop cancels that callback; Drain only
// succeeds once it actually returns. Cleanup cannot erase a successor's row.
func (s *Service) OwnConnection(ctx context.Context, connection Connection, work func(context.Context) error) error {
	if work == nil {
		return fault(f.InvalidArgument)
	}
	if _, e := s.reservation(connection); e != nil {
		return e
	}
	owned, done, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	if e = s.withConnection(owned, connection, false, false, true, func(context.Context, postgres.SQLExecutor, connectionReservation) error { return nil }); e != nil {
		return e
	}
	e = portError(work(owned))
	// Keep the original parent's cancellation/deadline. No WithoutCancel cleanup
	// or restarted shutdown budget is created after the owned callback returns.
	retired := s.withConnection(ctx, connection, true, false, false, func(ctx context.Context, x postgres.SQLExecutor, r connectionReservation) error {
		tag, e := x.Exec(ctx, `DELETE FROM agenteam_runner.connections WHERE runner_id=$1 AND id=$2 AND owner_id=$3 AND credential_generation=$4 AND generation=$5`, r.runner.String(), r.id.String(), s.state().owner.String(), r.credential, r.generation)
		return affected(tag, e)
	})
	var ff *f.Fault
	if errors.As(retired, &ff) && ff.Code == f.Unauthenticated {
		retired = nil // Already revoked/replaced; no row belonging to this owner.
	}
	return errors.Join(e, retired)
}

func (s *Service) AcceptHello(ctx context.Context, connection Connection, hello p.Hello) (p.HelloAck, error) {
	if c.HelloSnapshot(hello).Validate() != nil || hello.RunnerID != p.ID(connection.RunnerID().String()) {
		return p.HelloAck{}, fault(f.InvalidArgument)
	}
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return p.HelloAck{}, e
	}
	defer done()
	ack := p.HelloAck{Accepted: hello.ProtocolVersion.Compatible(), NegotiatedProtocolVersion: p.CurrentVersion(), HeartbeatIntervalMS: 10000, HeartbeatTimeoutMS: 30000, EnabledFeatures: []string{}}
	raw, e := json.Marshal(c.HelloSnapshot(hello))
	if e != nil {
		return p.HelloAck{}, fault(f.InvalidArgument)
	}
	e = s.withConnection(ctx, connection, true, false, true, func(ctx context.Context, x postgres.SQLExecutor, r connectionReservation) error {
		var fresh bool
		if e := x.QueryRow(ctx, `SELECT hello_at IS NULL FROM agenteam_runner.connections WHERE runner_id=$1 AND id=$2`, r.runner.String(), r.id.String()).Scan(&fresh); e != nil {
			return unavailable(e)
		}
		if !fresh {
			return fault(f.InvalidState)
		}
		if !ack.Accepted {
			tag, e := x.Exec(ctx, `UPDATE agenteam_runner.runners SET incompatible=true WHERE id=$1`, r.runner.String())
			return affected(tag, e)
		}
		var seen time.Time
		e := x.QueryRow(ctx, `UPDATE agenteam_runner.connections c SET hello_at=greatest(clock_timestamp(),c.last_seen_at,r.created_at),last_seen_at=greatest(clock_timestamp(),c.last_seen_at,r.created_at),lease_expires_at=greatest(clock_timestamp(),c.last_seen_at,r.created_at)+interval '30 seconds' FROM agenteam_runner.runners r WHERE c.runner_id=r.id AND c.runner_id=$1 AND c.id=$2 RETURNING c.last_seen_at`, r.runner.String(), r.id.String()).Scan(&seen)
		if e != nil {
			return unavailable(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_runner.runners SET last_seen_at=$2,last_hello=$3::jsonb,incompatible=false WHERE id=$1`, r.runner.String(), seen, string(raw))
		return affected(tag, e)
	})
	if e != nil {
		return p.HelloAck{}, e
	}
	return ack, nil
}

func (s *Service) Heartbeat(ctx context.Context, connection Connection, heartbeat p.Heartbeat) error {
	sequence, e := strconv.ParseUint(string(heartbeat.Sequence), 10, 64)
	if e != nil || sequence == 0 || strconv.FormatUint(sequence, 10) != string(heartbeat.Sequence) || !heartbeat.RunnerTime.Valid() {
		return fault(f.InvalidArgument)
	}
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	return s.withConnection(ctx, connection, true, true, true, func(ctx context.Context, x postgres.SQLExecutor, r connectionReservation) error {
		var seen time.Time
		e := x.QueryRow(ctx, `UPDATE agenteam_runner.connections SET heartbeat_sequence=$3::numeric,last_seen_at=greatest(clock_timestamp(),last_seen_at),lease_expires_at=greatest(clock_timestamp(),last_seen_at)+interval '30 seconds' WHERE runner_id=$1 AND id=$2 AND heartbeat_sequence+1=$3::numeric RETURNING last_seen_at`, r.runner.String(), r.id.String(), string(heartbeat.Sequence)).Scan(&seen)
		if e != nil {
			return unavailable(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_runner.runners SET last_seen_at=$2 WHERE id=$1`, r.runner.String(), seen)
		return affected(tag, e)
	})
}

func (s *Service) UpdateStatus(ctx context.Context, connection Connection, status p.RunnerStatus) error {
	messageID, e := p.NewID()
	if e != nil {
		return unavailable(e)
	}
	at, _ := p.NewInstant(time.Now())
	message, e := p.NewMessage(p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: messageID, Timestamp: at}, status)
	if e != nil {
		return fault(f.InvalidArgument)
	}
	status = message.Payload().(p.RunnerStatus)
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	return s.withConnection(ctx, connection, true, true, true, func(ctx context.Context, x postgres.SQLExecutor, r connectionReservation) error {
		current, e := loadRunner(ctx, x, r.runner)
		if e != nil {
			return e
		}
		if current.LastHello == nil {
			return unavailable(nil)
		}
		hello := *current.LastHello
		hello.Headless, hello.Capabilities = status.Headless, status.Capabilities
		raw, e := json.Marshal(hello)
		if e != nil {
			return unavailable(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_runner.runners SET last_hello=$2::jsonb WHERE id=$1`, r.runner.String(), string(raw))
		return affected(tag, e)
	})
}
