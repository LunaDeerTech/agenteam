package object

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type runtimeState struct {
	mu                        sync.Mutex
	service                   *Service
	guard                     *ProcessGuard
	transfer                  *TransferService
	initializing, initialized bool
}
type Runtime struct{ data func() *runtimeState }

// Only runtime startup/recovery creates this marker. Compensating storage I/O
// in that path must consume the outer startup/worker budget, not detach from it.
type runtimeRecoveryBudgetKey struct{}

func (r *Runtime) state() *runtimeState        { return r.data() }
func (r Runtime) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_runtime") }
func (r Runtime) MarshalJSON() ([]byte, error) { return []byte(`"object_runtime"`), nil }
func (*Runtime) UnmarshalJSON([]byte) error    { return invalid() }
func (r Runtime) LogValue() slog.Value         { return slog.StringValue("object_runtime") }
func NewRuntime(s *Service, g *ProcessGuard, t *TransferService) (*Runtime, error) {
	if s == nil || s.data == nil || g == nil || g.data == nil || t == nil || t.data == nil || t.state().objects != s || s.state().auth.Processes != g || s.state().spool != g.state().spool || s.state().process != g.state().process {
		return nil, invalid()
	}
	g.state().mu.Lock()
	defer g.state().mu.Unlock()
	s.state().mu.Lock()
	defer s.state().mu.Unlock()
	if g.state().closed || g.state().service != nil || s.state().stopped || s.state().runtime != nil || s.state().workerDone != nil {
		return nil, failure(foundation.InvalidState, nil)
	}
	state := &runtimeState{service: s, guard: g, transfer: t}
	r := &Runtime{func() *runtimeState { return state }}
	g.state().service = s
	s.state().runtime = r
	return r, nil
}
func (r *Runtime) Initialize(ctx context.Context) error {
	d := r.state()
	d.mu.Lock()
	if d.initializing || d.initialized {
		d.mu.Unlock()
		return failure(foundation.InvalidState, nil)
	}
	d.initializing = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.initializing = false; d.mu.Unlock() }()
	ctx = context.WithValue(ctx, runtimeRecoveryBudgetKey{}, true)
	op, done, err := d.service.admit(ctx, true)
	if err != nil {
		return err
	}
	defer done()
	ctx = op.ctx
	if err = d.service.Initialize(ctx); err != nil {
		return err
	}
	if err = d.guard.bind(ctx, d.service); err != nil {
		return err
	}
	if err = d.service.checkTransferStorage(ctx); err != nil {
		return err
	}
	if err = d.service.recoverProbes(ctx, d.guard); err != nil {
		return err
	}
	if err = d.service.ProbeStorage(ctx, d.guard); err != nil {
		return err
	}
	if err = r.recover(ctx); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return unavailable(err)
	}
	state := d.service.state()
	state.mu.Lock()
	if state.stopped || state.forced {
		state.mu.Unlock()
		return failure(foundation.ShuttingDown, nil)
	}
	state.runtimeReady = true
	state.mu.Unlock()
	d.mu.Lock()
	d.initialized = true
	d.mu.Unlock()
	return nil
}
func (r *Runtime) Check(ctx context.Context) error {
	d := r.state()
	d.mu.Lock()
	ready := d.initialized
	d.mu.Unlock()
	if !ready {
		return unavailable(nil)
	}
	op, done, err := d.service.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	ctx = op.ctx
	if err = d.service.Check(ctx); err != nil {
		return err
	}
	if err = d.service.checkTransferStorage(ctx); err != nil {
		return err
	}
	m := d.service.MaintenanceStatus()
	if !m.Running || !m.Healthy || m.CheckedAt.Time().IsZero() || time.Since(m.CheckedAt.Time()) > 20*time.Second {
		return unavailable(nil)
	}
	return nil
}
func (r *Runtime) recover(ctx context.Context) error {
	s := r.state().service
	if !hasBusinessPlanner(s) {
		var technical bool
		if err := s.state().store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.project_work WHERE joined_at IS NULL) OR EXISTS(SELECT 1 FROM agenteam_object.project_stops WHERE state='stopping')`).Scan(&technical); err != nil {
			return unavailable(err)
		}
		if technical {
			return failure(foundation.DependencyUnbound, nil)
		}
		return s.recoverEmpty(ctx, r.state().guard)
	}
	if err := s.checkRecoveryProtection(ctx); err != nil {
		return err
	}
	var failures recoveryFailures
	failures.remember(s.recoverProgress(ctx, &failures))
	if ctx.Err() != nil {
		return unavailable(ctx.Err())
	}
	if !nilPort(r.state().transfer.state().authority) {
		failures.remember(r.state().transfer.recoverProgress(ctx, &failures))
	} else {
		var n int64
		err := s.state().store.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_transfers`).Scan(&n)
		if err != nil {
			failures.remember(unavailable(err))
		} else if n != 0 {
			failures.remember(failure(foundation.DependencyUnbound, nil))
		}
	}
	if ctx.Err() != nil {
		return unavailable(ctx.Err())
	}
	failures.remember(s.recoverProbeProgress(ctx, r.state().guard, &failures))
	failures.remember(s.checkRecoveryProtection(ctx))
	// Busy means protected work remains pending; it does not authorize release
	// or suppress a storage/authorization error from any later independent item.
	return failures.firstNonBusy
}

// A busy callback alone is not proof of safety. Check the durable object and
// transfer protection relationships in a single database snapshot before and
// after each recovery round. These are technical facts, never permission to
// read, publish or retire a lease.
func (s *Service) checkRecoveryProtection(ctx context.Context) error {
	var invalidRelationship bool
	err := s.state().store.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_object.objects o LEFT JOIN agenteam_object.upload_attempts a ON a.candidate_key=o.candidate_key
  WHERE o.state='available' AND NOT o.cleaning AND (a.id IS NULL OR a.kind<>'private_candidate' OR a.object_id<>o.id OR a.phase<>'published' OR a.byte_size<>o.byte_size OR a.sha256<>o.sha256))
 OR EXISTS(SELECT 1 FROM agenteam_object.object_transfers t
  LEFT JOIN agenteam_object.object_leases l ON l.id=t.lease_id
  LEFT JOIN agenteam_object.upload_attempts a ON a.id=t.staging_id
  LEFT JOIN agenteam_object.upload_attempts c ON c.id=t.candidate_id
  LEFT JOIN agenteam_object.object_leases r ON r.id=t.source_lease_id
  WHERE l.id IS NULL OR l.object_id<>t.object_id OR l.owner_kind<>'transfer' OR l.owner_id<>t.id OR l.process_id IS NOT NULL
   OR (l.state='released' AND t.retirement_evidence IS NULL)
   OR (t.direction='put' AND (a.id IS NULL OR a.kind<>'runner_staging' OR a.transfer_id<>t.id OR a.object_id<>t.object_id OR a.upload_id<>t.upload_id OR a.byte_size<>t.byte_size OR a.sha256<>t.sha256))
   OR (t.candidate_id IS NOT NULL AND (c.id IS NULL OR c.kind<>'private_candidate' OR c.object_id<>t.object_id OR c.upload_id<>t.upload_id))
   OR (t.source_lease_id IS NOT NULL AND (r.id IS NULL OR r.object_id<>t.object_id OR r.owner_kind<>'source' OR r.process_id IS DISTINCT FROM t.source_process_id)))`).Scan(&invalidRelationship)
	if err != nil {
		return unavailable(err)
	}
	if invalidRelationship {
		return failure(foundation.InvalidState, nil)
	}
	return nil
}
func hasBusinessPlanner(s *Service) bool {
	p := s.state().auth.Planner
	if wrapped, ok := p.(*transferPlanner); ok {
		return !nilPort(wrapped.base)
	}
	return !nilPort(p)
}
func (r *Runtime) StartMaintenance(ctx context.Context) error {
	d := r.state()
	d.mu.Lock()
	ready := d.initialized
	d.mu.Unlock()
	if !ready {
		return unavailable(nil)
	}
	s := d.service.state()
	s.mu.Lock()
	if s.stopped || s.forced || !s.runtimeReady {
		s.mu.Unlock()
		return unavailable(nil)
	}
	if s.workerDone != nil {
		s.mu.Unlock()
		return failure(foundation.InvalidState, nil)
	}
	s.workerDone = make(chan struct{})
	s.maintenance.Running = true
	s.maintenance.Healthy = true
	s.maintenance.CheckedAt, _ = foundation.NewInstant(time.Now())
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			s.maintenance.Running = false
			close(s.workerDone)
			close(s.changed)
			s.changed = make(chan struct{})
			s.mu.Unlock()
		}()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.workerStop:
				return
			case <-ticker.C:
			}
			attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
			attempt = context.WithValue(attempt, runtimeRecoveryBudgetKey{}, true)
			op, done, err := d.service.begin(attempt)
			if err == nil {
				err = r.recover(op.ctx)
				done()
			}
			cancel()
			at, _ := foundation.NewInstant(time.Now())
			s.mu.Lock()
			s.maintenance.CheckedAt = at
			s.maintenance.Healthy = err == nil
			s.maintenance.Code = ""
			if err != nil {
				s.maintenance.Code = foundation.DependencyUnavailable
			}
			s.mu.Unlock()
		}
	}()
	return nil
}
func (r *Runtime) StopAdmission() { r.state().service.StopAdmission() }
func (r *Runtime) Drain(ctx context.Context) error {
	if err := r.state().service.Drain(ctx); err != nil {
		return err
	}
	return r.state().guard.finish(ctx)
}
func (r *Runtime) Force(ctx context.Context) error {
	if err := r.state().service.Force(ctx); err != nil {
		return err
	}
	return r.state().guard.finish(ctx)
}
func (r *Runtime) MaintenanceStatus() MaintenanceStatus { return r.state().service.MaintenanceStatus() }
