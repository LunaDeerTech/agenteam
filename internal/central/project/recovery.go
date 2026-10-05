package project

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type workClaim struct {
	creation      c.CreationID
	project       c.ProjectID
	process       oc.ProcessID
	attempt       string
	fence         int64
	phase         string
	terminalKnown bool
}

func (r workClaim) cause() foundation.TransactionCause {
	v, _ := foundation.NewJobCause("project-initialization", r.creation.String(), r.attempt)
	return v
}
func loadClaim(ctx context.Context, x postgres.SQLExecutor, id c.CreationID) (*workClaim, error) {
	var r workClaim
	var process, project string
	e := x.QueryRow(ctx, `SELECT project_id::text,process_id::text,attempt_id::text,fence,phase FROM agenteam_project.work_claims WHERE work_kind='creation' AND work_id=$1`, id.String()).Scan(&project, &process, &r.attempt, &r.fence, &r.phase)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	r.creation = id
	r.project, e = parseID[identity.Project](project)
	if e != nil {
		return nil, e
	}
	r.process, e = parseID[oc.Process](process)
	if e != nil {
		return nil, e
	}
	if _, e = parseID[struct{}](r.attempt); e != nil {
		return nil, e
	}
	if r.fence < 1 || r.phase != "running" && r.phase != "terminal" {
		return nil, unavailable(nil)
	}
	return &r, nil
}
func checkClaim(ctx context.Context, x postgres.SQLExecutor, claim workClaim) error {
	current, e := loadClaim(ctx, x, claim.creation)
	if e != nil {
		return e
	}
	if current == nil || *current != claim || current.phase != "running" {
		return fault(foundation.ResourceBusy)
	}
	return nil
}
func terminalClaim(ctx context.Context, x postgres.SQLExecutor, claim workClaim) error {
	tag, e := x.Exec(ctx, `UPDATE agenteam_project.work_claims SET phase='terminal' WHERE work_kind='creation' AND work_id=$1 AND process_id=$2 AND attempt_id=$3 AND fence=$4 AND phase='running'`, claim.creation.String(), claim.process.String(), claim.attempt, claim.fence)
	return affected(tag, e)
}
func (s *Service) joined(attempt string) {
	st := s.state()
	st.mu.Lock()
	st.joined[attempt] = true
	st.mu.Unlock()
}
func (s *Service) claimCanJoin(ctx context.Context, claim *workClaim) error {
	if claim == nil || claim.phase == "terminal" {
		return nil
	}
	if claim.process == s.state().deps.Processes.CurrentProcess() {
		s.state().mu.Lock()
		joined := s.state().joined[claim.attempt]
		s.state().mu.Unlock()
		if !joined {
			return fault(foundation.ResourceBusy)
		}
		return nil
	}
	// No timestamp, lease expiry, missing connection, or caller cancellation
	// establishes death. ConfirmStopped is the root's exact ProcessGuard port.
	return portError(s.state().deps.Processes.ConfirmStopped(ctx, claim.process))
}
func (s *Service) claimCreation(ctx context.Context, id c.CreationID) (*creationRecord, *workClaim, error) {
	candidate, e := loadCreation(ctx, s.state().store, id)
	if e != nil {
		return nil, nil, e
	}
	if candidate == nil {
		return nil, nil, fault(foundation.NotFound)
	}
	previous, e := loadClaim(ctx, s.state().store, id)
	if e != nil {
		return nil, nil, e
	}
	if e = s.claimCanJoin(ctx, previous); e != nil {
		return nil, nil, e
	}
	attempt, e := foundation.NewID[struct{}]()
	if e != nil {
		return nil, nil, unavailable(e)
	}
	claim := &workClaim{creation: id, project: candidate.operation.ProjectID, process: s.state().deps.Processes.CurrentProcess(), attempt: attempt.String(), fence: 1, phase: "running"}
	if previous != nil {
		if previous.fence == math.MaxInt64 {
			return nil, nil, fault(foundation.InvalidState)
		}
		claim.fence = previous.fence + 1
	}
	var output *creationRecord
	completed := false
	result := s.state().store.WithinTx(ctx, claim.cause(), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(candidate.identity()), projectLock(candidate.operation.ProjectID, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCreation(ctx, x, id)
		if e != nil {
			return e
		}
		if current == nil || current.operation.ProjectID != candidate.operation.ProjectID {
			return fault(foundation.NotFound)
		}
		p, e := loadProject(ctx, x, current.operation.ProjectID)
		if e != nil {
			return e
		}
		if p == nil || p.creation != id || p.ref.OwnerUserID != current.owner {
			return fault(foundation.NotFound)
		}
		if current.operation.State == c.CreationCompleted {
			output = current
			completed = true
			return nil
		}
		if p.initialized || p.ref.Lifecycle != c.Active {
			return fault(foundation.InvalidState)
		}
		// Recheck exact claim after obtaining the original writer lock. The
		// earlier joined/death proof cannot be transferred to a new attempt.
		actual, e := loadClaim(ctx, x, id)
		if e != nil {
			return e
		}
		if (actual == nil) != (previous == nil) || actual != nil && *actual != *previous {
			return fault(foundation.ResourceBusy)
		}
		if actual == nil {
			_, e = x.Exec(ctx, `INSERT INTO agenteam_project.work_claims(work_kind,work_id,project_id,process_id,attempt_id,fence,phase) VALUES('creation',$1,$2,$3,$4,1,'running')`, id.String(), claim.project.String(), claim.process.String(), claim.attempt)
		} else {
			var tag interface{ RowsAffected() int64 }
			tag, e = x.Exec(ctx, `UPDATE agenteam_project.work_claims SET process_id=$2,attempt_id=$3,fence=$4,phase='running' WHERE work_kind='creation' AND work_id=$1 AND fence=$5 AND attempt_id=$6`, id.String(), claim.process.String(), claim.attempt, claim.fence, previous.fence, previous.attempt)
			if e == nil && tag.RowsAffected() != 1 {
				return fault(foundation.ResourceBusy)
			}
		}
		if e != nil {
			return unavailable(e)
		}
		now, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		version, e := nextVersion(current.operation.Version)
		if e != nil {
			return e
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_project.creations SET state='initializing',version=$2,updated_at=$3,safe_reason=NULL WHERE id=$1 AND state<>'completed'`, id.String(), int64(version), now.Time())
		if e = affected(tag, e); e != nil {
			return e
		}
		current.operation.State = c.CreationInitializing
		current.operation.Version = version
		current.operation.UpdatedAt = now
		current.operation.SafeReason = ""
		output = current
		return nil
	})
	if result.State() == foundation.Unknown {
		// The callback has returned and no provider call has started. This
		// local attempt is actually joined; a later claim still serializes its
		// database writer before taking over, even if this COMMIT remains held.
		s.joined(claim.attempt)
	}
	if e = commitError(result); e != nil {
		return nil, nil, e
	}
	if completed {
		return output, nil, nil
	}
	// Any prior locally joined attempt has been fenced out under its writer
	// lock. Its in-memory proof can be released only after known commit.
	if previous != nil && previous.process == claim.process {
		s.state().mu.Lock()
		delete(s.state().joined, previous.attempt)
		s.state().mu.Unlock()
	}
	return output, claim, nil
}
func (s *Service) finishCreationRound(ctx context.Context, r *creationRecord, claim *workClaim, reason c.SafeReason, providerErr error) (c.CreationResult, error) {
	state := c.CreationFailed
	if reason == c.ReasonWorkPending || reason == c.ReasonOutcomeUnknown || reason.Validate() != nil {
		state = c.CreationInitializing
	}
	return s.checkpointCreationRound(ctx, r, claim, state, reason, providerErr)
}
func (s *Service) checkpointCreationRound(ctx context.Context, r *creationRecord, claim *workClaim, state c.CreationState, reason c.SafeReason, providerErr error) (c.CreationResult, error) {
	if reason.Validate() != nil {
		reason = c.ReasonWorkPending
	}
	// External call has returned. A checkpoint uses a bounded independent
	// context so a disconnected caller does not silently erase its outcome.
	checkpoint, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	var output c.CreationResult
	result := s.state().store.WithinTx(checkpoint, claim.cause(), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(r.identity()), projectLock(r.operation.ProjectID, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = checkClaim(ctx, x, *claim); e != nil {
			return e
		}
		current, e := loadCreation(ctx, x, r.operation.ID)
		if e != nil {
			return e
		}
		if current == nil {
			return fault(foundation.NotFound)
		}
		p, e := loadProject(ctx, x, r.operation.ProjectID)
		if e != nil {
			return e
		}
		if p == nil {
			return fault(foundation.NotFound)
		}
		if current.operation.State == c.CreationCompleted {
			output, e = creationResult(current, p)
			return e
		}
		now, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		version, e := nextVersion(current.operation.Version)
		if e != nil {
			return e
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_project.creations SET state=$2,safe_reason=$3,version=$4,updated_at=$5 WHERE id=$1 AND state<>'completed'`, r.operation.ID.String(), string(state), string(reason), int64(version), now.Time())
		if e = affected(tag, e); e != nil {
			return e
		}
		current.operation.State = state
		current.operation.SafeReason = reason
		current.operation.Version = version
		current.operation.UpdatedAt = now
		if e = terminalClaim(ctx, x, *claim); e != nil {
			return e
		}
		output, e = creationResult(current, p)
		return e
	})
	if result.State() == foundation.Unknown {
		confirmed, e := s.confirmCreationUnknown(checkpoint, r, claim, result)
		if e != nil {
			return c.CreationResult{}, e
		}
		return creationRoundResult(confirmed, providerErr)
	}
	if e := commitError(result); e != nil {
		return c.CreationResult{}, e
	}
	claim.terminalKnown = true
	return creationRoundResult(output, providerErr)
}
func creationRoundResult(output c.CreationResult, providerErr error) (c.CreationResult, error) {
	if output.Operation == nil || output.Operation.State != c.CreationFailed {
		return output, nil
	}
	// Failure is real, but both acceptance and the failed checkpoint committed.
	// The stable creation ID permits authorized status lookup; raw provider
	// diagnostics remain private and never become safe output or Audit fields.
	code := foundation.DependencyUnavailable
	if output.Operation.SafeReason == c.ReasonDependencyUnbound {
		code = foundation.DependencyUnbound
	}
	f := foundation.NewFault(code, foundation.Committed)
	f.CauseID = output.Operation.ID.String()
	f.RetryHint = "retry_same_key"
	return c.CreationResult{}, f.WithCause(providerErr)
}
func (s *Service) confirmCreationUnknown(ctx context.Context, r *creationRecord, claim *workClaim, original foundation.CommitResult) (c.CreationResult, error) {
	confirm, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	var output c.CreationResult
	result := s.state().store.WithinTx(confirm, claim.cause(), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(r.identity()), projectLock(r.operation.ProjectID, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCreation(ctx, x, r.operation.ID)
		if e != nil {
			return e
		}
		if current == nil {
			return fault(foundation.NotFound)
		}
		p, e := loadProject(ctx, x, r.operation.ProjectID)
		if e != nil {
			return e
		}
		output, e = creationResult(current, p)
		if e != nil {
			return e
		}
		actual, e := loadClaim(ctx, x, claim.creation)
		if e != nil {
			return e
		}
		claim.terminalKnown = actual != nil && actual.phase == "terminal" && actual.attempt == claim.attempt && actual.fence == claim.fence
		return nil
	})
	if result.State() != foundation.Committed {
		claim.terminalKnown = false
		return c.CreationResult{}, commitError(original)
	}
	return creationRoundResult(output, commitFailure{original})
}

// RecoveryPage is operational progress over stable creation IDs. Every scan
// visits past busy prefixes; the composition root schedules another bounded
// round. This function starts no goroutine and claims no periodic ownership.
type RecoveryPage struct {
	Next                        *c.CreationID
	Visited, Completed, Pending int
}

func (s *Service) RecoverCreations(ctx context.Context, after *c.CreationID, limit int) (RecoveryPage, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return RecoveryPage{}, e
	}
	defer done()
	if limit < 1 || limit > 100 || after != nil && after.Validate() != nil {
		return RecoveryPage{}, invalid()
	}
	if nilPort(s.state().deps.Initializer) {
		return RecoveryPage{}, fault(foundation.DependencyUnbound)
	}
	var afterValue any
	if after != nil {
		afterValue = after.String()
	}
	rows, e := s.state().store.Query(ctx, `SELECT id::text FROM agenteam_project.creations WHERE state<>'completed' AND ($1::uuid IS NULL OR id>$1::uuid) ORDER BY id LIMIT $2`, afterValue, limit+1)
	if e != nil {
		return RecoveryPage{}, unavailable(e)
	}
	ids := []c.CreationID{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return RecoveryPage{}, unavailable(e)
		}
		id, e := parseID[c.Creation](raw)
		if e != nil {
			rows.Close()
			return RecoveryPage{}, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return RecoveryPage{}, unavailable(e)
	}
	page := RecoveryPage{}
	more := len(ids) > limit
	if more {
		ids = ids[:limit]
	}
	for _, id := range ids {
		if e = ctx.Err(); e != nil {
			return page, unavailable(e)
		}
		page.Visited++
		if !s.slot() {
			page.Pending++
			continue
		}
		result, e := s.advanceCreation(ctx, id)
		s.releaseSlot()
		if e == nil && result.State == c.CreationReady {
			page.Completed++
		} else {
			page.Pending++
		}
		// A failed item stays durable; move to the next ID instead of starving
		// later work behind one unjoined process or unavailable provider.
	}
	if more && len(ids) > 0 {
		last := ids[len(ids)-1]
		page.Next = &last
	}
	return page, nil
}
