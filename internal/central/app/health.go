package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

type healthTiming struct {
	interval, timeout, stale time.Duration
	now                      func() time.Time
}

func (t healthTiming) defaults() healthTiming {
	if t.interval <= 0 {
		t.interval = 10 * time.Second
	}
	if t.timeout <= 0 {
		t.timeout = 2 * time.Second
	}
	if t.stale <= 0 {
		t.stale = 20 * time.Second
	}
	if t.now == nil {
		t.now = time.Now
	}
	return t
}

type healthMonitor struct {
	mu               sync.RWMutex
	last             postgres.DatabaseHealth
	received         time.Time
	available        bool
	timing           healthTiming
	objectAvailable  bool
	objectReceived   time.Time
	outboxAvailable  bool
	outboxReceived   time.Time
	accountAvailable bool
	accountReceived  time.Time
	accountBound     bool
}

func healthy(h postgres.DatabaseHealth) bool {
	return h.PostgreSQL && h.PGVector && h.Migrations && h.ReadWrite && h.CheckedAt.Validate() == nil
}
func newHealthMonitor(initial postgres.DatabaseHealth, timing healthTiming) *healthMonitor {
	timing = timing.defaults()
	return &healthMonitor{last: initial, received: timing.now(), available: healthy(initial), timing: timing}
}
func (h *healthMonitor) snapshot() (postgres.DatabaseHealth, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	available := h.available && h.timing.now().Sub(h.received) <= h.timing.stale
	if !available {
		return postgres.DatabaseHealth{}, false
	}
	return h.last, true
}
func (h *healthMonitor) objectSnapshot() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.objectAvailable && h.timing.now().Sub(h.objectReceived) <= h.timing.stale
}

type healthComponent interface{ Check(context.Context) error }

// A timed-out sample remains owned until Check actually returns. A later
// round can discard its result and start fresh, but cannot overlap it or
// publish a late success under the later round's deadline.
type healthCheck[T any] struct{ pending chan T }

func (c *healthCheck[T]) start(ctx context.Context, work func(context.Context) T) chan T {
	if c.pending != nil {
		select {
		case <-c.pending:
			c.pending = nil
		default:
			return nil
		}
	}
	done := make(chan T, 1)
	c.pending = done
	go func() {
		done <- work(ctx)
		close(done)
	}()
	return done
}

func (h *healthMonitor) accountSnapshot() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.accountAvailable && h.timing.now().Sub(h.accountReceived) <= h.timing.stale
}

func (h *healthMonitor) outboxSnapshot() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.outboxAvailable && h.timing.now().Sub(h.outboxReceived) <= h.timing.stale
}
func (h *healthMonitor) run(ctx context.Context, db database, logger processLogger, objects ...healthComponent) {
	timer := time.NewTicker(h.timing.interval)
	defer timer.Stop()
	type databaseSample struct {
		health postgres.DatabaseHealth
		err    error
	}
	var databaseCheck healthCheck[databaseSample]
	var objectCheck, outboxCheck, accountCheck healthCheck[error]
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		sample, cancel := context.WithTimeout(ctx, h.timing.timeout)
		databaseDone := databaseCheck.start(sample, func(ctx context.Context) databaseSample {
			next, err := db.Check(ctx)
			return databaseSample{next, err}
		})
		var objectService healthComponent
		if len(objects) > 0 {
			objectService = objects[0]
		}
		objectDone := objectCheck.start(sample, func(ctx context.Context) error {
			if objectService != nil {
				return objectService.Check(ctx)
			}
			return errors.New("OBJECT_UNBOUND")
		})
		var outboxService healthComponent
		if len(objects) > 1 {
			outboxService = objects[1]
		}
		outboxDone := outboxCheck.start(sample, func(ctx context.Context) error {
			if outboxService != nil {
				return outboxService.Check(ctx)
			}
			return errors.New("OUTBOX_UNBOUND")
		})
		var accountService healthComponent
		if len(objects) > 2 {
			accountService = objects[2]
		}
		accountDone := accountCheck.start(sample, func(ctx context.Context) error {
			if accountService != nil {
				return accountService.Check(ctx)
			}
			return errors.New("ACCOUNT_UNBOUND")
		})
		var next postgres.DatabaseHealth
		var err, objectErr, outboxErr, accountErr error
		// A previous round's unjoined call is unavailable, even if its result
		// arrives while independent dependencies complete this fresh round.
		if databaseDone == nil {
			err = context.DeadlineExceeded
		}
		if objectDone == nil {
			objectErr = context.DeadlineExceeded
		}
		if outboxDone == nil {
			outboxErr = context.DeadlineExceeded
		}
		if accountDone == nil {
			accountErr = context.DeadlineExceeded
		}
		for databaseDone != nil || objectDone != nil || outboxDone != nil || accountDone != nil {
			select {
			case result := <-databaseDone:
				databaseCheck.pending = nil
				next = result.health
				err = result.err
				databaseDone = nil
			case result := <-objectDone:
				objectCheck.pending = nil
				objectErr = result
				objectDone = nil
			case result := <-outboxDone:
				outboxCheck.pending = nil
				outboxErr = result
				outboxDone = nil
			case result := <-accountDone:
				accountCheck.pending = nil
				accountErr = result
				accountDone = nil
			case <-sample.Done():
				if accountDone != nil {
					accountErr = sample.Err()
					accountDone = nil
				}
				if outboxDone != nil {
					outboxErr = sample.Err()
					outboxDone = nil
				}
				if databaseDone != nil {
					err = sample.Err()
					databaseDone = nil
				}
				if objectDone != nil {
					objectErr = sample.Err()
					objectDone = nil
				}
			}
		}
		if err == nil {
			err = sample.Err()
		}
		if objectErr == nil {
			objectErr = sample.Err()
		}
		if outboxErr == nil {
			outboxErr = sample.Err()
		}
		if accountErr == nil {
			accountErr = sample.Err()
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		available := err == nil && healthy(next)
		h.mu.Lock()
		changed := h.available != available
		h.available = available
		objectChanged := h.objectAvailable != (objectErr == nil)
		h.objectAvailable = objectErr == nil
		if objectErr == nil {
			h.objectReceived = h.timing.now()
		}
		outboxChanged := h.outboxAvailable != (outboxErr == nil)
		h.outboxAvailable = outboxErr == nil
		if outboxErr == nil {
			h.outboxReceived = h.timing.now()
		}
		h.accountAvailable = accountErr == nil
		if accountErr == nil {
			h.accountReceived = h.timing.now()
		}
		if available {
			h.last = next
			h.received = h.timing.now()
		}
		h.mu.Unlock()
		if outboxChanged {
			if outboxErr == nil {
				logger.Security(logging.OutboxAvailable)
			} else {
				logger.Security(logging.OutboxUnavailable)
			}
		}
		if objectChanged {
			if objectErr == nil {
				logger.Security(logging.ObjectAvailable)
			} else {
				logger.Security(logging.ObjectUnavailable)
			}
		}
		if changed {
			if available {
				logger.Database(logging.DatabaseHealthy, "", "", 0)
			} else {
				databaseFailure(logger, err)
			}
		}
	}
}
func asDatabaseError(err error) *postgres.Error {
	var safe *postgres.Error
	if errors.As(err, &safe) {
		return safe
	}
	return nil
}
func databaseFailure(logger processLogger, err error) {
	code, sqlState := string(postgres.HealthFailed), ""
	if safe := asDatabaseError(err); safe != nil {
		code = string(safe.Code())
		sqlState = safe.SQLState()
	}
	logger.Database(logging.DatabaseUnavailable, code, sqlState, 0)
}
