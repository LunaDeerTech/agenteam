package outbox

import (
	"context"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type execution struct {
	id     oc.AttemptID
	record record
	cancel context.CancelFunc
	done   chan struct{}
	// Written by the task before done closes, read only after that close.
	claimed   bool
	confirmed bool
	returned  *time.Time
	err       error
}
type duePosition struct {
	at       time.Time
	sequence int64
	id       string
}
type recoveryPosition struct {
	pass int64
	id   string
}

func (r *runtimeState) run(ctx context.Context) {
	defer func() { r.mu.Lock(); close(r.workerDone); r.signalLocked(); r.mu.Unlock() }()
	timer := time.NewTicker(r.options.PollInterval)
	defer timer.Stop()
	for {
		r.reap(ctx)
		r.mu.Lock()
		stopped := r.stopped
		allDone := true
		for _, a := range r.active {
			select {
			case <-a.done:
			default:
				allDone = false
			}
		}
		r.mu.Unlock()
		if stopped && allDone {
			return
		}
		if !stopped && ctx.Err() == nil {
			cycle, cancel := context.WithTimeout(ctx, r.options.QueryTimeout)
			// A protected recovery prefix cannot consume the whole round before
			// lifecycle or due work gets its turn. All stages remain inside the
			// same overall budget and never refresh a shorter caller/force limit.
			var first error
			for _, stage := range []func(context.Context) error{r.recoverBatch, r.recoverLifecycleBatch, func(c context.Context) error { return r.dispatch(c, ctx) }} {
				if cycle.Err() != nil {
					break
				}
				part, finish := context.WithTimeout(cycle, r.options.QueryTimeout/3)
				err := stage(part)
				err = recoveryBudgetError(part, ctx, err)
				finish()
				if err != nil && !protectedError(err) && first == nil {
					first = err
				}
			}
			cancel()
			r.recordError(first)
		}
		// A cancelled worker still owns its callback joins. It waits without new
		// I/O until those tasks actually return; Force's API wait is bounded.
		select {
		case <-timer.C:
		case <-r.wake:
		}
	}
}

func (r *runtimeState) discoverDue(ctx context.Context) ([]record, error) {
	p := r.dueAfter
	var at any
	if !p.at.IsZero() {
		at = p.at
	}
	rows, err := r.svc.state().store.Query(ctx, `SELECT `+recordColumns+recordFrom+` WHERE d.phase IN ('pending','retry_wait') AND d.next_attempt_at<=clock_timestamp() AND ($1::timestamptz IS NULL OR (d.next_attempt_at,e.sequence,d.id)>($1,$2,$3::uuid)) ORDER BY d.next_attempt_at,e.sequence,d.id LIMIT $4`, at, p.sequence, nullableUUID(p.id), r.options.Batch)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	var out []record
	for rows.Next() {
		d, e := scanRecord(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	if err = rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	if len(out) == 0 {
		r.dueAfter = duePosition{}
	}
	return out, nil
}
func nullableUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func fairCandidates(input []record) []record {
	order := []event.StableName{}
	groups := map[event.StableName][]record{}
	for _, d := range input {
		if _, ok := groups[d.handler]; !ok {
			order = append(order, d.handler)
		}
		groups[d.handler] = append(groups[d.handler], d)
	}
	out := make([]record, 0, len(input))
	for len(out) < len(input) {
		for _, h := range order {
			g := groups[h]
			if len(g) > 0 {
				out = append(out, g[0])
				groups[h] = g[1:]
			}
		}
	}
	return out
}
func (r *runtimeState) dispatch(ctx, serving context.Context) error {
	candidates, err := r.discoverDue(ctx)
	if err != nil {
		return err
	}
	// The keyset advances over the entire bounded discovery page, including a
	// locked/protected prefix. Wrapping happens only after the tail is reached.
	if len(candidates) > 0 {
		last := candidates[len(candidates)-1]
		r.dueAfter = duePosition{last.due, last.sequence, last.id.String()}
	}
	for _, d := range fairCandidates(candidates) {
		if ctx.Err() != nil {
			return portError(ctx.Err())
		}
		r.mu.Lock()
		if r.stopped {
			r.mu.Unlock()
			return nil
		}
		per := 0
		duplicate := false
		for _, a := range r.active {
			if a.record.handler == d.handler {
				per++
			}
			if a.record.id == d.id {
				duplicate = true
			}
		}
		if len(r.active) >= r.options.Concurrent || per >= r.options.PerHandler || duplicate {
			r.mu.Unlock()
			continue
		}
		id, e := foundation.NewID[oc.Attempt]()
		if e != nil {
			r.mu.Unlock()
			return unavailable(e)
		}
		task, cancel := context.WithTimeout(serving, r.options.AttemptTimeout)
		a := &execution{id: id, record: d, cancel: cancel, done: make(chan struct{})}
		r.active[id] = a
		r.mu.Unlock()
		go r.execute(task, serving, a)
	}
	return nil
}

func (r *runtimeState) execute(ctx, parent context.Context, a *execution) {
	defer func() {
		a.cancel()
		close(a.done)
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}()
	claimCtx, cancel := context.WithTimeout(ctx, r.options.ItemTimeout)
	d, claimed, err := r.claim(claimCtx, a)
	cancel()
	if d.attempt == a.id {
		r.mu.Lock()
		a.record = d
		r.mu.Unlock()
		a.claimed = true
	}
	if err != nil || !claimed {
		a.err = err
		return
	}
	a.confirmed = true
	var result oc.Result
	plan, err := r.svc.PrepareDelivery(ctx, d.id)
	if err == nil {
		commit, outcome, returned := r.svc.applyDeliveryObserved(ctx, plan)
		a.returned = returned
		result = outcome
		switch commit.State() {
		case foundation.Committed:
			a.err = nil
			return
		case foundation.Unknown:
			err = foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
		default:
			err = commitError(commit)
		}
	}
	reason := reasonFor(err, ctx)
	if result.Valid() && result.Kind() != oc.Acknowledged {
		reason = result.Reason()
	} else if reason == oc.UnsupportedSchema || reason == oc.InvalidEvent || reason == oc.SourceTerminal || reason == oc.AuthorizationChanged || reason == oc.ProjectStopped {
		result = oc.Reject(reason)
	} else {
		result = oc.Retry(reason)
	}
	cleanup, finish := r.checkpointContext(parent)
	defer finish()
	a.err = r.checkpoint(cleanup, d, result, reason, a.returned)
}

func reasonFor(err error, ctx context.Context) oc.SafeReason {
	if ctx.Err() == context.DeadlineExceeded {
		return oc.Deadline
	}
	if ctx.Err() != nil {
		return oc.Shutdown
	}
	switch safeFaultCode(err) {
	case foundation.SchemaUnsupported:
		return oc.UnsupportedSchema
	case foundation.InvalidArgument, foundation.InvalidState:
		return oc.InvalidEvent
	case foundation.NotFound, foundation.ResourceDeleted:
		return oc.SourceTerminal
	case foundation.Forbidden, foundation.Unauthenticated, foundation.SessionRevoked:
		return oc.AuthorizationChanged
	case foundation.ProjectNotActive:
		return oc.ProjectStopped
	case foundation.DependencyUnbound:
		return oc.UnboundHandler
	case foundation.CommitUnknown:
		return oc.CommitUnknown
	case foundation.InternalError:
		return oc.HandlerPanic
	default:
		return oc.Unavailable
	}
}
func protectedError(err error) bool {
	if err == nil {
		return false
	}
	switch safeFaultCode(err) {
	case foundation.ResourceBusy, foundation.CommitUnknown, foundation.NotFound, foundation.ResourceDeleted, foundation.ProjectNotActive, foundation.ShuttingDown:
		return true
	}
	return false
}

func (r *runtimeState) reap(ctx context.Context) {
	r.mu.Lock()
	var done []*execution
	for _, a := range r.active {
		select {
		case <-a.done:
			done = append(done, a)
		default:
		}
	}
	stopped := r.stopped
	r.mu.Unlock()
	for _, a := range done {
		var err error
		if a.claimed {
			checkpoint, cancel := r.checkpointContext(ctx)
			if checkpoint.Err() == nil {
				if !a.confirmed {
					var exists bool
					exists, err = r.confirmRetirement(checkpoint, a.record)
					if err == nil && !exists {
						a.claimed = false
					}
				}
				if err == nil && a.claimed && a.err != nil {
					err = r.checkpoint(checkpoint, a.record, oc.Retry(oc.CommitUnknown), oc.CommitUnknown, a.returned)
				}
				if err == nil && a.claimed {
					err = r.markJoined(checkpoint, a.record, a.returned)
				}
			} else {
				err = checkpoint.Err()
			}
			cancel()
		}
		if err != nil && !stopped {
			r.recordError(portError(err))
			continue
		}
		r.mu.Lock()
		delete(r.active, a.id)
		r.signalLocked()
		r.mu.Unlock()
	}
}
