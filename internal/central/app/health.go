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
	mu        sync.RWMutex
	last      postgres.DatabaseHealth
	received  time.Time
	available bool
	timing    healthTiming
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
func (h *healthMonitor) run(ctx context.Context, db database, logger processLogger) {
	timer := time.NewTicker(h.timing.interval)
	defer timer.Stop()
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
		next, err := db.Check(sample)
		if err == nil {
			err = sample.Err()
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		available := err == nil && healthy(next)
		h.mu.Lock()
		changed := h.available != available
		h.available = available
		if available {
			h.last = next
			h.received = h.timing.now()
		}
		h.mu.Unlock()
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
