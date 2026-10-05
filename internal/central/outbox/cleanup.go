package outbox

import (
	"context"
	"errors"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type lifecyclePlan struct {
	request      oc.ProjectRequest
	dependencies oc.Dependencies
	cause        oc.LifecycleDetails
}
type lifecycleRow struct {
	operation string
	action    oc.LifecycleAction
	version   int64
	phase     string
	after     int64
}

func (s *Service) Name() string { return "outbox" }
func (s *Service) planLifecycle(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause, step oc.LifecycleStep) (lifecyclePlan, error) {
	var p lifecyclePlan
	if cause.Validate() != nil {
		return p, invalid()
	}
	request, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.LifecycleProject, ProjectID: cause.Details().ProjectID, Actor: actor, Lifecycle: cause, LifecycleStep: step})
	if err != nil {
		return p, err
	}
	if nilPort(s.state().auth.Projects) || nilPort(s.state().auth.Processes) {
		return p, failure(foundation.DependencyUnbound, nil)
	}
	dependencies, err := s.state().auth.Projects.Discover(ctx, request)
	if err != nil {
		return p, portError(err)
	}
	if dependencies.Validate() != nil {
		return p, unavailable(nil)
	}
	return lifecyclePlan{request, dependencies, cause.Details()}, nil
}
func (p lifecyclePlan) locks(mode foundation.LockMode) []foundation.LockRequest {
	key, _ := foundation.ProjectLock(p.cause.ProjectID.String())
	locks := p.dependencies.Locks()
	return append(locks, foundation.LockRequest{Key: key, Mode: mode})
}
func (s *Service) validateLifecycle(ctx context.Context, tx foundation.Tx, p lifecyclePlan) error {
	if err := s.state().auth.Projects.ValidateInTx(ctx, tx, p.request, p.dependencies); err != nil {
		return portError(err)
	}
	return nil
}
func readLifecycle(ctx context.Context, x postgres.SQLExecutor, project identity.ProjectID) (lifecycleRow, bool, error) {
	var row lifecycleRow
	var action string
	err := x.QueryRow(ctx, `SELECT operation_id::text,action,project_version,phase,scan_sequence FROM agenteam_outbox.project_lifecycle WHERE project_id=$1`, project.String()).Scan(&row.operation, &action, &row.version, &row.phase, &row.after)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, unavailable(err)
	}
	row.action = oc.LifecycleAction(action)
	return row, true, nil
}
func lifecycleMatches(row lifecycleRow, cause oc.LifecycleDetails) bool {
	return row.operation == cause.OperationID.String() && row.action == cause.Action && row.version == int64(cause.ProjectVersion)
}
func checkLifecycleTransition(row lifecycleRow, exists bool, cause oc.LifecycleDetails) error {
	if !exists || lifecycleMatches(row, cause) {
		return nil
	}
	if row.action == oc.DeleteProject {
		return failure(foundation.ResourceDeleted, nil)
	}
	if int64(cause.ProjectVersion) <= row.version {
		return failure(foundation.ResourceBusy, nil)
	}
	return nil
}

type capturedCallback struct {
	runtime *runtimeState
	handle  *execution
	record  record
}

// Called while the successful authorization still holds its Project gate.
// A capture is a precise local run, never a later Project-wide cancellation.
func (s *Service) captureProjectCallbacks(p lifecyclePlan) []capturedCallback {
	s.state().mu.RLock()
	runtime := s.state().runtime
	s.state().mu.RUnlock()
	if runtime == nil {
		return nil
	}
	r := runtime.data()
	r.mu.Lock()
	defer r.mu.Unlock()
	var targets []capturedCallback
	for _, a := range r.active {
		if a.record.attempt == a.id && a.record.scope.Kind == event.ProjectScope && a.record.scope.ProjectID.String() == p.cause.ProjectID.String() && (p.cause.Action == oc.DeleteProject || a.record.effect == oc.DomainIngress) {
			targets = append(targets, capturedCallback{r, a, a.record})
		}
	}
	return targets
}
func cancelCapturedCallbacks(targets []capturedCallback) {
	for _, t := range targets {
		r := t.runtime
		r.mu.Lock()
		if r.active[t.handle.id] == t.handle && sameAttempt(t.handle.record, t.record) {
			select {
			case <-t.handle.done:
			default:
				t.handle.cancel()
			}
		}
		r.mu.Unlock()
	}
}
func (s *Service) startLifecycle(ctx context.Context, p lifecyclePlan, write bool) (lifecycleRow, error) {
	// A read-only current authorization precedes cancellation; Discover alone is
	// not authority to affect a callback. The later durable transition takes EX.
	var targets []capturedCallback
	var row lifecycleRow
	terminal := false
	preflight := s.state().store.WithinTx(ctx, recoveryCause("outbox.stop-authorize"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, p.locks(foundation.Shared)); err != nil {
			return unavailable(err)
		}
		if err := s.validateLifecycle(ctx, tx, p); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		old, found, err := readLifecycle(ctx, x, p.cause.ProjectID)
		if err != nil {
			return err
		}
		if !write && (!found || !lifecycleMatches(old, p.cause)) {
			return failure(foundation.InvalidState, nil)
		}
		// Inspect observes an exact terminal receipt under the same SH union as
		// the current authorization and callback capture. A separate preflight
		// would permit another instance to finish between the two transactions.
		if p.request.Details().LifecycleStep == oc.LifecycleInspect && (old.phase == "stopped" || old.phase == "completed") {
			row, terminal = old, true
			return nil
		}
		if err = checkLifecycleTransition(old, found, p.cause); err != nil {
			return err
		}
		targets = s.captureProjectCallbacks(p)
		return nil
	})
	if err := commitError(preflight); err != nil {
		return lifecycleRow{}, err
	}
	if terminal {
		return row, nil
	}
	cancelCapturedCallbacks(targets)
	commit := s.state().store.WithinTx(ctx, recoveryCause("outbox.stop-gate"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, p.locks(foundation.Exclusive)); err != nil {
			return unavailable(err)
		}
		if err := s.validateLifecycle(ctx, tx, p); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		old, found, err := readLifecycle(ctx, x, p.cause.ProjectID)
		if err != nil {
			return err
		}
		if err = checkLifecycleTransition(old, found, p.cause); err != nil {
			return err
		}
		if found && lifecycleMatches(old, p.cause) {
			row = old
			return nil
		}
		if !write {
			return failure(foundation.InvalidState, nil)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.project_lifecycle(project_id,operation_id,action,project_version,phase) VALUES($1,$2,$3,$4,'stopping') ON CONFLICT(project_id) DO UPDATE SET operation_id=excluded.operation_id,action=excluded.action,project_version=excluded.project_version,phase='stopping',recovery_pass=0,scan_sequence=0,started_at=clock_timestamp(),completed_at=NULL`, p.cause.ProjectID.String(), p.cause.OperationID.String(), string(p.cause.Action), int64(p.cause.ProjectVersion))
		if err != nil {
			return unavailable(err)
		}
		row = lifecycleRow{operation: p.cause.OperationID.String(), action: p.cause.Action, version: int64(p.cause.ProjectVersion), phase: "stopping"}
		return nil
	})
	return row, commitError(commit)
}

type lifecycleEvent struct {
	id         event.EventID
	sequence   int64
	deliveries []record
}

func (s *Service) lifecycleBatch(ctx context.Context, p lifecyclePlan, after int64) ([]lifecycleEvent, error) {
	rows, err := s.state().store.Query(ctx, `SELECT id::text,sequence FROM agenteam_outbox.events WHERE project_id=$1 AND sequence>$2 ORDER BY sequence LIMIT 100`, p.cause.ProjectID.String(), after)
	if err != nil {
		return nil, unavailable(err)
	}
	var batch []lifecycleEvent
	for rows.Next() {
		var id string
		var sequence int64
		if err = rows.Scan(&id, &sequence); err != nil {
			rows.Close()
			return nil, unavailable(err)
		}
		eid, e := foundation.ParseID[event.EventIdentity](id)
		if e != nil {
			rows.Close()
			return nil, unavailable(e)
		}
		batch = append(batch, lifecycleEvent{id: eid, sequence: sequence})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, unavailable(err)
	}
	// Keep the complete lock union bounded even at the 128-handler fanout limit.
	// A batch may contain fewer than 100 Events, never a partial Event deletion.
	lockCount := 0
	for i := range batch {
		rows, err = s.state().store.Query(ctx, `SELECT `+recordColumns+recordFrom+` WHERE d.event_id=$1 ORDER BY d.id`, batch[i].id.String())
		if err != nil {
			return nil, unavailable(err)
		}
		for rows.Next() {
			d, e := scanRecord(rows)
			if e != nil {
				rows.Close()
				return nil, e
			}
			batch[i].deliveries = append(batch[i].deliveries, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, unavailable(err)
		}
		next := 1 + len(batch[i].deliveries)
		if i > 0 && lockCount+next > 256 {
			return batch[:i], nil
		}
		lockCount += next
	}
	return batch, nil
}
func (s *Service) terminalProof(ctx context.Context, d record) (bool, error) {
	if ctx.Err() != nil {
		return false, portError(ctx.Err())
	}
	if d.attempt.Validate() != nil || d.joined != nil || d.retired {
		return true, nil
	}
	if d.process == s.state().auth.Processes.CurrentProcess() {
		s.state().mu.RLock()
		runtime := s.state().runtime
		s.state().mu.RUnlock()
		if runtime != nil && runtime.data().localReturned(d) {
			return true, nil
		}
		return false, nil
	}
	item, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	err := s.state().auth.Processes.ConfirmStopped(item, d.process)
	// Only an explicit item-deadline result is a protected inspection. An
	// independent hard error that races that deadline must remain observable.
	err = recoveryBudgetError(item, ctx, err)
	if ctx.Err() != nil {
		return false, portError(ctx.Err())
	}
	if err != nil {
		// A non-local or unprovable claim stays protected. Its refusal must not
		// prevent a separately authorized event from being cleaned in this batch.
		switch safeFaultCode(err) {
		case foundation.ResourceBusy, foundation.NotFound, foundation.DependencyUnavailable:
			return false, nil
		default:
			return false, portError(err)
		}
	}
	return true, nil
}
func sameAttempt(a, b record) bool {
	return a.id == b.id && a.eventID == b.eventID && a.handler == b.handler && a.effect == b.effect && a.scope == b.scope && a.attempt == b.attempt && a.fence == b.fence && a.process == b.process
}
func (s *Service) stopProject(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause, step oc.LifecycleStep) (oc.StopReport, error) {
	var report oc.StopReport
	ctx, done, err := s.beginAdministration(ctx)
	if err != nil {
		return report, err
	}
	defer done()
	p, err := s.planLifecycle(ctx, actor, cause, step)
	if err != nil {
		return report, err
	}
	row, err := s.startLifecycle(ctx, p, step == oc.LifecycleStop)
	if err != nil {
		return report, err
	}
	if row.phase == "completed" || step == oc.LifecycleInspect && row.phase == "stopped" {
		return oc.StopReport{Stopped: true}, nil
	}
	batch, err := s.lifecycleBatch(ctx, p, row.after)
	if err != nil {
		return report, err
	}
	proof := map[oc.DeliveryID]bool{}
	var proofError error
	locks := p.locks(foundation.Exclusive)
	for _, e := range batch {
		locks = append(locks, foundation.LockRequest{Key: eventLock(e.id), Mode: foundation.Exclusive})
		for _, d := range e.deliveries {
			if cause.Details().Action == oc.ArchiveProject && d.effect != oc.DomainIngress {
				continue
			}
			proved, err := s.terminalProof(ctx, d)
			if ctx.Err() != nil {
				return report, portError(ctx.Err())
			}
			if err != nil && proofError == nil {
				proofError = err
			}
			proof[d.id] = proved
			locks = append(locks, foundation.LockRequest{Key: deliveryLock(d.id), Mode: foundation.Exclusive})
		}
	}
	terminal, err := s.progressLifecycleStop(ctx, p, batch, proof, locks)
	if err != nil {
		return report, err
	}
	if terminal {
		return oc.StopReport{Stopped: true}, proofError
	}
	// The current authorization/report transaction has priority over an earlier
	// proof error. A successful independent checkpoint must not erase that error.
	report, err = s.stopReport(ctx, p)
	if err != nil {
		return report, err
	}
	return report, proofError
}

func (s *Service) progressLifecycleStop(ctx context.Context, p lifecyclePlan, batch []lifecycleEvent, proof map[oc.DeliveryID]bool, locks []foundation.LockRequest) (bool, error) {
	terminal := false
	commit := s.state().store.WithinTx(ctx, recoveryCause("outbox.stop-progress"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		if err := s.validateLifecycle(ctx, tx, p); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, found, err := readLifecycle(ctx, x, p.cause.ProjectID)
		if err != nil {
			return err
		}
		if !found || !lifecycleMatches(current, p.cause) {
			return failure(foundation.ResourceBusy, nil)
		}
		// A terminal transition can win while the bounded scan or attempt proof
		// runs outside this transaction. Do not renew any progress after it.
		if p.request.Details().LifecycleStep == oc.LifecycleInspect && (current.phase == "stopped" || current.phase == "completed") {
			terminal = true
			return nil
		}
		for _, e := range batch {
			for _, d := range e.deliveries {
				if !proof[d.id] {
					continue
				}
				now, err := s.deliveryRecord(ctx, x, d.id)
				if safeFaultCode(err) == foundation.NotFound {
					continue
				}
				if err != nil {
					return err
				}
				if !sameAttempt(now, d) {
					continue
				}
				// The proof belongs to this exact attempt, and acquiring its lock
				// has now joined the original database writer. Persist retirement
				// without inventing a callback-return timestamp for a dead process.
				if now.attempt.Validate() == nil {
					if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.attempts SET joined_at=CASE WHEN handler_returned_at IS NOT NULL THEN coalesce(joined_at,clock_timestamp()) ELSE joined_at END,finished_at=coalesce(finished_at,clock_timestamp()),checkpoint=CASE WHEN handler_returned_at IS NULL THEN 'stopped' ELSE checkpoint END WHERE id=$1`, now.attempt.String()); err != nil {
						return unavailable(err)
					}
				}
				if now.phase != oc.Pending && now.phase != oc.Processing && now.phase != oc.RetryWait {
					continue
				}
				var processed bool
				if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_outbox.processed WHERE delivery_id=$1)`, d.id.String()).Scan(&processed); err != nil {
					return unavailable(err)
				}
				if processed {
					if err = finishDeliveryInTx(ctx, x, now.identity()); err != nil {
						return err
					}
					continue
				}
				if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.deliveries SET phase='dead_letter',safe_reason='project_stopped',version=version+1,last_at=clock_timestamp() WHERE id=$1`, d.id.String()); err != nil {
					return unavailable(err)
				}
				if d.attempt.Validate() == nil {
					if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.attempts SET finished_at=coalesce(finished_at,clock_timestamp()),checkpoint='stopped',safe_reason='project_stopped' WHERE id=$1`, d.attempt.String()); err != nil {
						return unavailable(err)
					}
				}
			}
		}
		after := int64(0)
		if len(batch) > 0 {
			after = batch[len(batch)-1].sequence
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.project_lifecycle SET scan_sequence=$2::bigint,recovery_pass=CASE WHEN $2::bigint=0 THEN recovery_pass+1 ELSE recovery_pass END WHERE project_id=$1`, p.cause.ProjectID.String(), after); err != nil {
			return unavailable(err)
		}
		return nil
	})
	return terminal, commitError(commit)
}
func (s *Service) RequestStop(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
	return s.stopProject(ctx, actor, cause, oc.LifecycleStop)
}
func (s *Service) InspectStop(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.StopReport, error) {
	return s.stopProject(ctx, actor, cause, oc.LifecycleInspect)
}
func (s *Service) stopReport(ctx context.Context, p lifecyclePlan) (oc.StopReport, error) {
	var report oc.StopReport
	commit := s.state().store.WithinTx(ctx, recoveryCause("outbox.stop-inspect"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, p.locks(foundation.Exclusive)); err != nil {
			return unavailable(err)
		}
		if err := s.validateLifecycle(ctx, tx, p); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		row, found, err := readLifecycle(ctx, x, p.cause.ProjectID)
		if err != nil {
			return err
		}
		if !found || !lifecycleMatches(row, p.cause) {
			return failure(foundation.ResourceBusy, nil)
		}
		// Count under the same EX gate that serializes every claim. The bounded
		// progress batch above persists exact retirement proofs; an unvisited or
		// still-live attempt remains pending. No out-of-transaction snapshot can
		// accidentally certify a newly claimed/returned but unjoined callback.
		var pending int64
		if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.handlers h ON h.id=d.handler_id LEFT JOIN agenteam_outbox.attempts a ON a.id=d.current_attempt_id WHERE d.project_id=$1 AND ($2='delete' OR h.effect='domain_ingress') AND (d.phase IN ('pending','processing','retry_wait') OR (d.current_attempt_id IS NOT NULL AND a.joined_at IS NULL AND NOT (a.checkpoint IN ('not_committed','stopped') AND a.finished_at IS NOT NULL)))`, p.cause.ProjectID.String(), string(p.cause.Action)).Scan(&pending); err != nil {
			return unavailable(err)
		}
		report.Pending = foundation.Progress(pending)
		report.Stopped = pending == 0
		if report.Stopped && row.phase == "stopping" {
			_, err = x.Exec(ctx, `UPDATE agenteam_outbox.project_lifecycle SET phase='stopped' WHERE project_id=$1`, p.cause.ProjectID.String())
			return portResult(err)
		}
		return nil
	})
	if err := commitError(commit); err != nil {
		return oc.StopReport{}, err
	}
	return report, nil
}

func (s *Service) Cleanup(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.CleanupReport, error) {
	var report oc.CleanupReport
	if cause.Validate() != nil || cause.Details().Action != oc.DeleteProject {
		return report, invalid()
	}
	ctx, done, err := s.beginAdministration(ctx)
	if err != nil {
		return report, err
	}
	defer done()
	p, err := s.planLifecycle(ctx, actor, cause, oc.LifecycleCleanup)
	if err != nil {
		return report, err
	}
	// Current formal cleanup authorization is required even for completed replay.
	row, err := s.startLifecycle(ctx, p, false)
	if err != nil {
		return report, err
	}
	if row.phase == "completed" {
		return oc.CleanupReport{Completed: true}, nil
	}
	gate := s.state().store.WithinTx(ctx, recoveryCause("outbox.cleanup-gate"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, p.locks(foundation.Exclusive)); err != nil {
			return unavailable(err)
		}
		if err := s.validateLifecycle(ctx, tx, p); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, found, err := readLifecycle(ctx, x, p.cause.ProjectID)
		if err != nil {
			return err
		}
		if !found || !lifecycleMatches(current, p.cause) {
			return failure(foundation.ResourceBusy, nil)
		}
		if current.phase == "completed" {
			return nil
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_outbox.project_lifecycle SET phase='cleaning' WHERE project_id=$1`, p.cause.ProjectID.String())
		return portResult(err)
	})
	if err = commitError(gate); err != nil {
		return report, err
	}
	batch, err := s.lifecycleBatch(ctx, p, row.after)
	if err != nil {
		return report, err
	}
	proof := map[oc.DeliveryID]bool{}
	var proofError error
	locks := p.locks(foundation.Exclusive)
	for _, e := range batch {
		locks = append(locks, foundation.LockRequest{Key: eventLock(e.id), Mode: foundation.Exclusive})
		for _, d := range e.deliveries {
			proved, err := s.terminalProof(ctx, d)
			if ctx.Err() != nil {
				return report, portError(ctx.Err())
			}
			if err != nil && proofError == nil {
				proofError = err
			}
			proof[d.id] = proved
			locks = append(locks, foundation.LockRequest{Key: deliveryLock(d.id), Mode: foundation.Exclusive})
		}
	}
	commit := s.state().store.WithinTx(ctx, recoveryCause("outbox.cleanup-batch"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		if err := s.validateLifecycle(ctx, tx, p); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, found, err := readLifecycle(ctx, x, p.cause.ProjectID)
		if err != nil {
			return err
		}
		if !found || !lifecycleMatches(current, p.cause) || current.phase != "cleaning" {
			return failure(foundation.ResourceBusy, nil)
		}
		for _, e := range batch {
			safe := true
			for _, d := range e.deliveries {
				if !proof[d.id] {
					safe = false
					break
				}
				now, err := s.deliveryRecord(ctx, x, d.id)
				if safeFaultCode(err) == foundation.NotFound {
					continue
				}
				if err != nil {
					return err
				}
				if !sameAttempt(now, d) {
					safe = false
					break
				}
			}
			if !safe {
				continue
			}
			for _, query := range []string{
				`DELETE FROM agenteam_outbox.requeue_commands WHERE delivery_id IN(SELECT id FROM agenteam_outbox.deliveries WHERE event_id=$1)`,
				`DELETE FROM agenteam_outbox.processed WHERE event_id=$1`,
				`UPDATE agenteam_outbox.deliveries SET current_attempt_id=NULL,fence=0,phase='pending' WHERE event_id=$1`,
				`DELETE FROM agenteam_outbox.attempts WHERE delivery_id IN(SELECT id FROM agenteam_outbox.deliveries WHERE event_id=$1)`,
				`DELETE FROM agenteam_outbox.deliveries WHERE event_id=$1`,
			} {
				if _, err = x.Exec(ctx, query, e.id.String()); err != nil {
					return unavailable(err)
				}
			}
			tag, err := x.Exec(ctx, `DELETE FROM agenteam_outbox.events WHERE id=$1 AND project_id=$2`, e.id.String(), p.cause.ProjectID.String())
			if err != nil {
				return unavailable(err)
			}
			report.Removed += foundation.Progress(tag.RowsAffected())
		}
		after := int64(0)
		if len(batch) > 0 {
			after = batch[len(batch)-1].sequence
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.project_lifecycle SET scan_sequence=$2::bigint,recovery_pass=CASE WHEN $2::bigint=0 THEN recovery_pass+1 ELSE recovery_pass END WHERE project_id=$1`, p.cause.ProjectID.String(), after); err != nil {
			return unavailable(err)
		}
		var remaining, relations int64
		if err = x.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1),
    (SELECT count(*) FROM agenteam_outbox.deliveries WHERE project_id=$1)+
    (SELECT count(*) FROM agenteam_outbox.attempts a JOIN agenteam_outbox.deliveries d ON d.id=a.delivery_id WHERE d.project_id=$1)+
    (SELECT count(*) FROM agenteam_outbox.processed p JOIN agenteam_outbox.deliveries d ON d.id=p.delivery_id WHERE d.project_id=$1)+
    (SELECT count(*) FROM agenteam_outbox.requeue_commands c JOIN agenteam_outbox.deliveries d ON d.id=c.delivery_id WHERE d.project_id=$1)`, p.cause.ProjectID.String()).Scan(&remaining, &relations); err != nil {
			return unavailable(err)
		}
		report.Remaining = foundation.Progress(remaining)
		report.Completed = remaining == 0 && relations == 0
		if report.Completed {
			_, err = x.Exec(ctx, `UPDATE agenteam_outbox.project_lifecycle SET phase='completed',completed_at=clock_timestamp(),scan_sequence=0,recovery_pass=0 WHERE project_id=$1`, p.cause.ProjectID.String())
			return portResult(err)
		}
		return nil
	})
	if err = commitError(commit); err != nil {
		return oc.CleanupReport{}, err
	}
	return report, proofError
}

var _ oc.ProjectLifecycleParticipant = (*Service)(nil)
