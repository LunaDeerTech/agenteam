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
	mu              sync.RWMutex
	last            postgres.DatabaseHealth
	received        time.Time
	available       bool
	timing          healthTiming
	objectAvailable bool
	objectReceived  time.Time
	outboxAvailable bool
	outboxReceived  time.Time
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

func (h *healthMonitor) outboxSnapshot() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.outboxAvailable && h.timing.now().Sub(h.outboxReceived) <= h.timing.stale
}
func (h *healthMonitor) run(ctx context.Context, db database, logger processLogger, objects ...healthComponent) {
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
		type databaseSample struct {
			health postgres.DatabaseHealth
			err    error
		}
		databaseDone := make(chan databaseSample, 1)
		go func(done chan databaseSample) {
			next, err := db.Check(sample)
			done <- databaseSample{next, err}
		}(databaseDone)
		objectDone := make(chan error, 1)
		var objectService healthComponent
		if len(objects) > 0 {
			objectService = objects[0]
		}
		go func(done chan error) {
			if objectService != nil {
				done <- objectService.Check(sample)
			} else {
				done <- errors.New("OBJECT_UNBOUND")
			}
		}(objectDone)
		outboxDone := make(chan error, 1)
		var outboxService healthComponent
		if len(objects) > 1 {
			outboxService = objects[1]
		}
		go func(done chan error) {
			if outboxService != nil {
				done <- outboxService.Check(sample)
			} else {
				done <- errors.New("OUTBOX_UNBOUND")
			}
		}(outboxDone)
		var next postgres.DatabaseHealth
		var err, objectErr, outboxErr error
		for databaseDone != nil || objectDone != nil || outboxDone != nil {
			select {
			case result := <-databaseDone:
				next = result.health
				err = result.err
				databaseDone = nil
			case result := <-objectDone:
				objectErr = result
				objectDone = nil
			case result := <-outboxDone:
				outboxErr = result
				outboxDone = nil
			case <-sample.Done():
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
