package project

import (
	"context"
	"errors"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// LifecycleStopBatch describes unfinished operations visited in one bounded
// scan. A returned local round does not complete an operation or participant.
// Next is the last visited ID when this scan has more work. After a nil Next,
// the caller must begin a later cycle at nil to revisit earlier pending work.
// An error before any visit does not consume the supplied cursor.
type LifecycleStopBatch struct {
	Visited, Pending int
	Next             *c.OperationID
}

// LifecycleStopRecovery owns one fixed driver and at most one active batch.
// It starts no background worker and does not own or close the process guard.
// Its caller must Stop and actually Drain before releasing that shared guard.
type LifecycleStopRecovery struct {
	state *lifecycleStopRecoveryState
}

type lifecycleStopRecoveryState struct {
	driver  *LifecycleStopDriver
	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	changed chan struct{}
}

func NewLifecycleStopRecovery(store Store, authority *LifecycleAuthority, processes oc.ProcessAuthority, step LifecycleLocalStopStep) (*LifecycleStopRecovery, error) {
	driver, err := NewLifecycleStopDriver(store, authority, processes, step)
	if err != nil {
		return nil, err
	}
	return &LifecycleStopRecovery{state: &lifecycleStopRecoveryState{driver: driver, changed: make(chan struct{})}}, nil
}

// RunBatch discovers canonical current operations and delegates each round to
// the original driver. Discovery is not authority: Run revalidates the full
// operation, manifest, claim and Project gate under the original Project EX.
// Busy entries do not starve later IDs. Other errors remain errors (including
// physical Unknown), even if later entries can be visited within this batch.
func (r *LifecycleStopRecovery) RunBatch(ctx context.Context, after *c.OperationID, limit int) (LifecycleStopBatch, error) {
	if ctx == nil || limit < 1 || limit > 4 {
		return LifecycleStopBatch{}, invalid()
	}
	var cursor *c.OperationID
	var afterValue any
	if after != nil {
		value := *after
		if value.Validate() != nil {
			return LifecycleStopBatch{}, invalid()
		}
		cursor, afterValue = &value, value.String()
	}
	if r == nil || r.state == nil {
		return LifecycleStopBatch{}, fault(f.DependencyUnbound)
	}
	st := r.state
	if err := ctx.Err(); err != nil {
		return LifecycleStopBatch{}, portError(err)
	}
	st.mu.Lock()
	if st.stopped {
		st.mu.Unlock()
		return LifecycleStopBatch{}, fault(f.ShuttingDown)
	}
	if st.cancel != nil {
		st.mu.Unlock()
		return LifecycleStopBatch{}, fault(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	st.cancel = cancel
	st.mu.Unlock()
	defer func() {
		cancel()
		st.mu.Lock()
		st.cancel = nil
		close(st.changed)
		st.changed = make(chan struct{})
		st.mu.Unlock()
	}()

	rows, err := st.driver.state.store.Query(run, `SELECT o.id::text,o.project_id::text FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.projects p ON p.id=o.project_id AND p.current_lifecycle_operation_id=o.id WHERE o.state IN ('accepted','stopping') AND ($1::uuid IS NULL OR o.id>$1::uuid) ORDER BY o.id LIMIT $2`, afterValue, limit+1)
	if err != nil {
		return LifecycleStopBatch{}, unavailable(err)
	}
	if rows == nil {
		return LifecycleStopBatch{}, unavailable(nil)
	}
	work, more, err := readLifecycleStopBatch(rows, cursor, limit)
	if err != nil {
		return LifecycleStopBatch{}, err
	}
	// The actual Rows.Close has returned before provider calls start. Both
	// discovery and every original Run/checkpoint remain owned by this batch.
	return st.visit(run, work, more)
}

type lifecycleStopCandidate struct {
	operation c.OperationID
	project   c.ProjectID
}

type lifecycleStopRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

func readLifecycleStopBatch(rows lifecycleStopRows, after *c.OperationID, limit int) ([]lifecycleStopCandidate, bool, error) {
	defer rows.Close()
	work := make([]lifecycleStopCandidate, 0, limit+1)
	previous := ""
	if after != nil {
		previous = after.String()
	}
	projects := map[c.ProjectID]bool{}
	for rows.Next() {
		if len(work) == limit+1 {
			return nil, false, unavailable(nil)
		}
		var operation, project string
		if err := rows.Scan(&operation, &project); err != nil {
			return nil, false, unavailable(err)
		}
		op, err := parseID[c.Operation](operation)
		if err != nil {
			return nil, false, err
		}
		pid, err := parseID[identity.Project](project)
		if err != nil {
			return nil, false, err
		}
		if operation <= previous || projects[pid] {
			return nil, false, unavailable(nil)
		}
		previous, projects[pid] = operation, true
		work = append(work, lifecycleStopCandidate{operation: op, project: pid})
	}
	if err := rows.Err(); err != nil {
		return nil, false, unavailable(err)
	}
	more := len(work) > limit
	if more {
		work = work[:limit]
	}
	return work, more, nil
}

func (st *lifecycleStopRecoveryState) visit(ctx context.Context, work []lifecycleStopCandidate, more bool) (LifecycleStopBatch, error) {
	page := LifecycleStopBatch{}
	var failed error
	for _, candidate := range work {
		if ctx.Err() != nil {
			break
		}
		page.Visited++
		// Even a successful local round leaves the whole operation stopping.
		page.Pending++
		err := st.driver.Run(ctx, candidate.project, candidate.operation)
		var code *f.Fault
		_, unknown := UnknownAttempt(err)
		if err != nil && (unknown || !errors.As(err, &code) || code.Code != f.ResourceBusy) {
			failed = lifecycleStopBatchError(failed, err)
		}
	}
	if err := ctx.Err(); err != nil {
		failed = lifecycleStopBatchError(failed, portError(err))
	}
	if page.Visited > 0 && (more || page.Visited < len(work)) {
		last := work[page.Visited-1].operation
		page.Next = &last
	}
	return page, failed
}

func lifecycleStopBatchError(previous, current error) error {
	// Preserve a physical Unknown before unrelated per-item errors so callers
	// retain the original attempt instead of classifying this as a new failure.
	if _, ok := UnknownAttempt(current); ok {
		if _, existing := UnknownAttempt(previous); !existing {
			return errors.Join(current, previous)
		}
	}
	return errors.Join(previous, current)
}

func (r *LifecycleStopRecovery) Stop() {
	if r == nil || r.state == nil {
		return
	}
	st := r.state
	st.mu.Lock()
	st.stopped = true
	if st.cancel != nil {
		st.cancel()
	}
	st.mu.Unlock()
	// No callback into the batch and no Drain under either admission mutex.
	st.driver.Stop()
}

func (r *LifecycleStopRecovery) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if r == nil || r.state == nil {
		return nil
	}
	st := r.state
	for {
		st.mu.Lock()
		if st.cancel == nil {
			st.mu.Unlock()
			return st.driver.Drain(ctx)
		}
		changed := st.changed
		st.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
