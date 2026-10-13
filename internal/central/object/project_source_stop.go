package object

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/jackc/pgx/v5"
)

// The optional composition preserves the original SourceReads issuer and
// handle owner. Neither a request DTO nor an unrelated cleanup can issue loans.
func NewSourceReadsWithProjectStop(service *Service, resolver oc.SourceResolver, authority oc.ProjectSourceStopAuthority) (*SourceReads, error) {
	if nilPort(authority) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	reads, err := NewSourceReads(service, resolver)
	if err == nil {
		reads.data().authority = authority
	}
	return reads, err
}
func (r *SourceReads) RequireProjectStopBinding(service *Service, authority oc.ProjectSourceStopAuthority) error {
	if r == nil || r.data == nil || service == nil || r.data().service != service || nilPort(authority) || nilPort(r.data().authority) || reflect.ValueOf(authority) != reflect.ValueOf(r.data().authority) {
		return failure(f.DependencyUnbound, nil)
	}
	return nil
}

// WithinProjectSourceAcquisition executes the original domain callback on the
// fixed same Store. Only its actual return publishes the acquisition outcome;
// expiry of the callback Tx cannot substitute for that return.
func (r *SourceReads) WithinProjectSourceAcquisition(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if r == nil || r.data == nil || nilPort(r.data().authority) || fn == nil {
		return rejectedAccess(failure(f.DependencyUnbound, nil))
	}
	return r.withinProjectSourceAcquisition(ctx, cause, fn)
}
func (r *SourceReads) withinProjectSourceAcquisition(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	var original f.Tx
	result := r.data().service.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error { original = tx; return fn(ctx, tx) })
	state := r.data()
	state.mu.Lock()
	var owned []*sourceLeaseState
	for _, h := range state.leases {
		if original.Valid() && h.origin == original {
			owned = append(owned, h)
		}
	}
	state.mu.Unlock()
	for _, h := range owned {
		_ = sourceCommitError(h, result)
		h.mu.Lock()
		h.acquisition = result
		h.acquireReturned = true
		h.mu.Unlock()
	}
	return result
}

// Bind records the original caller relationship while that exact acquisition
// transaction is live. It is not a Project grant and cannot rebind a lease.
func (r *SourceReads) BindProjectSourceInTx(ctx context.Context, tx f.Tx, lease oc.SourceLease, target i.ProjectID, command f.CommandIdentity) error {
	if r == nil || r.data == nil || nilPort(r.data().authority) || command.Validate() != nil {
		return invalid()
	}
	owners := command.OwnerIDs()
	if command.Namespace() != "artifact" || command.Command() != "create" || len(owners) != 1 || owners[0] != target.String() {
		return invalid()
	}
	return r.bindProjectSourceInTx(ctx, tx, lease, target, command)
}
func (r *SourceReads) bindProjectSourceInTx(ctx context.Context, tx f.Tx, lease oc.SourceLease, target i.ProjectID, command f.CommandIdentity) error {
	h, err := r.registered(lease)
	if err != nil {
		return err
	}
	if h == nil || ctx == nil || h.origin != tx || target.Validate() != nil || command.Validate() != nil {
		return invalid()
	}
	s := r.data().service
	if _, err = executor(s, tx); err != nil {
		return err
	}
	source := workProject(h.source.Details().Owner)
	if source.Validate() != nil {
		return failure(f.Forbidden, nil)
	}
	var locks []f.LockRequest
	for _, p := range []i.ProjectID{source, target} {
		k, _ := f.ProjectLock(p.String())
		locks = append(locks, f.LockRequest{Key: k, Mode: f.Shared})
	}
	held, ok := s.state().store.(interface {
		RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
	})
	if !ok || nilPort(held) {
		return failure(f.DependencyUnbound, nil)
	}
	if err = held.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.command.Validate() == nil && (h.command.Canonical() != command.Canonical() || h.target != target) {
		return failure(f.InvalidState, nil)
	}
	h.command, h.target = command, target
	return ctx.Err()
}

type sourceResultContextKey struct{}
type sourceResultContext struct {
	service *Service
	handle  *sourceLeaseState
	stop    *projectSourceStopScope
}
type projectSourceStopScope struct {
	active             atomic.Bool
	ctx                context.Context
	request            oc.ProjectSourceStopRequest
	authority          oc.ProjectSourceStopAuthority
	knowledgeRequest   oc.KnowledgeProjectSourceStopRequest
	knowledgeAuthority oc.KnowledgeProjectSourceStopAuthority
}

func sourceCommitError(h *sourceLeaseState, result f.CommitResult) error {
	if h.owner == nil || !h.owner.projectOwned() {
		return commitError(result)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.retained && result.State() == f.Unknown {
		h.retained = true
		h.original = result
		h.originalError = projectCleanupUnknown(result)
	}
	if h.retained {
		return h.originalError
	}
	return commitError(result)
}
func (s *Service) sourceContext(ctx context.Context) *sourceResultContext {
	c, _ := ctx.Value(sourceResultContextKey{}).(*sourceResultContext)
	if c == nil || c.service != s || c.handle == nil {
		return nil
	}
	return c
}
func (s *Service) requireSourceStop(ctx context.Context, tx f.Tx, c *sourceResultContext) error {
	if c == nil || c.stop == nil {
		return nil
	}
	if !c.stop.active.Load() || c.stop.ctx.Err() != nil {
		return failure(f.Forbidden, nil)
	}
	return c.stop.require(ctx, tx)
}
func (r *SourceReads) sourceCleanupContext(h *sourceLeaseState) (context.Context, func()) {
	s := r.data().service
	cleanup, done := s.cleanupContext()
	h.mu.Lock()
	scope := h.stopScope
	h.mu.Unlock()
	if scope == nil {
		return cleanup, done
	}
	// Keep the original loan's values/deadline, plus the existing cleanup/force
	// registration. Ending either original budget forbids any subsequent SQL.
	run, cancel := context.WithCancel(scope.ctx)
	stop := context.AfterFunc(cleanup, cancel)
	if cleanup.Err() != nil {
		cancel()
	}
	return run, func() { stop(); cancel(); done() }
}
func (r *SourceReads) releaseSource(ctx context.Context, h *sourceLeaseState) error {
	h.mu.Lock()
	if h.retained {
		err := h.originalError
		h.mu.Unlock()
		return err
	}
	if h.releaseRunning {
		h.mu.Unlock()
		return failure(f.ResourceBusy, nil)
	}
	h.releaseRunning = true
	scope := h.stopScope
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.releaseRunning = false; h.mu.Unlock() }()
	s := r.data().service
	run := context.WithValue(ctx, sourceResultContextKey{}, &sourceResultContext{service: s, handle: h, stop: scope})
	cause := recoveryCause()
	if scope != nil {
		cause = scope.cause("release", h.id)
	}
	result := s.releaseInternalResult(run, cause, h.id, h.source.Details().Meta.ID)
	if err := sourceCommitError(h, result); err != nil {
		return err
	}
	s.state().mu.Lock()
	delete(s.state().closedLeases, h.id)
	if h.work != nil {
		h.work.ended = true
		s.state().notifyWorkLocked()
	}
	s.state().mu.Unlock()
	if err := s.joinProjectWork(run, h.work); err != nil {
		return err
	}
	return nil
}

// StopProjectSource is called only inside the fixed authority's original loan.
// A prior native/Store call still in flight yields Pending. A loan which starts
// the original Close or release waits for that complete original return.
func (r *SourceReads) StopProjectSource(ctx context.Context, request oc.ProjectSourceStopRequest) (bool, error) {
	if r == nil || r.data == nil || nilPort(r.data().authority) {
		return false, failure(f.DependencyUnbound, nil)
	}
	if ctx == nil || request.Validate() != nil {
		return false, invalid()
	}
	scope := &projectSourceStopScope{ctx: ctx, request: request, authority: r.data().authority}
	return r.stopProjectSource(ctx, request.Details(), scope, false)
}
func (r *SourceReads) stopProjectSource(ctx context.Context, d oc.ProjectSourceStopDetails, scope *projectSourceStopScope, observe bool) (bool, error) {
	h, err := r.registered(d.Lease)
	if err != nil {
		return false, err
	}
	if h == nil {
		return false, failure(f.InvalidState, nil)
	}
	h.mu.Lock()
	valid := h.command.Validate() == nil && h.command.Canonical() == d.Command.Canonical() && h.target == d.TargetProjectID && workProject(h.source.Details().Owner) == d.SourceProjectID && h.source.Details().Meta.ID == d.ObjectID && h.process == r.data().service.state().process
	if !valid {
		h.mu.Unlock()
		return false, failure(f.Forbidden, nil)
	}
	if !h.acquireReturned || h.stopActive || h.releaseRunning {
		h.mu.Unlock()
		return false, nil
	}
	h.stopActive = true
	original := h.originalError
	retained := h.retained
	h.mu.Unlock()
	scope.active.Store(true)
	defer func() { scope.active.Store(false); h.mu.Lock(); h.stopActive = false; h.stopScope = nil; h.mu.Unlock() }()
	if _, err = r.data().service.state().store.InTx(h.origin); err == nil {
		return false, nil
	}
	run := context.WithValue(ctx, sourceResultContextKey{}, &sourceResultContext{service: r.data().service, handle: h, stop: scope})
	// This first original transaction checks the exact lease/work and current
	// private Project grant before any cancellation. Confirmation has no writes.
	validated := false
	result := r.inspectProjectSource(run, h, scope, &validated)
	if retained {
		if result.State() != f.Committed || !validated || ctx.Err() != nil {
			return false, errors.Join(original, commitError(result))
		}
		h.mu.Lock()
		h.retained = false
		h.original = f.CommitResult{}
		h.originalError = nil
		h.mu.Unlock()
		return false, nil
	}
	if err = sourceCommitError(h, result); err != nil {
		return false, err
	}
	if !validated || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	h.mu.Lock()
	if observe {
		ready := h.released && !h.busy && !h.releaseRunning
		if h.closeDone != nil {
			select {
			case <-h.closeDone:
			default:
				ready = false
			}
		}
		h.mu.Unlock()
		return ready, nil
	}
	if h.releaseRunning || h.busy && h.reader == nil {
		h.mu.Unlock()
		return false, nil
	}
	h.stopScope = scope
	h.cancelled = true
	if h.cancel != nil {
		h.cancel()
	}
	reader := h.reader
	released := h.released
	h.mu.Unlock()
	if reader != nil {
		// No select on caller cancellation: the borrowed scope covers the original
		// reader.Close return, including its monitor and backend Close tails.
		<-r.closeProjectSourceReader(h, reader)
		h.mu.Lock()
		released = h.released
		retained = h.retained
		original = h.originalError
		h.mu.Unlock()
		if retained {
			return false, original
		}
		// Close caches the first release result. Its actual return has now
		// joined the reader; an already confirmed old error is not a new
		// attempt. Unreleased state proceeds through this explicit loan.
	}
	if !released {
		cleanup, done := r.sourceCleanupContext(h)
		err = r.releaseSource(cleanup, h)
		done()
		h.mu.Lock()
		h.result = err
		h.released = err == nil
		h.joined = true
		h.mu.Unlock()
		if err != nil {
			return false, err
		}
	}
	if err = ctx.Err(); err != nil {
		return false, unavailable(err)
	}
	return true, nil
}
func (r *SourceReads) inspectProjectSource(ctx context.Context, h *sourceLeaseState, scope *projectSourceStopScope, validated *bool) f.CommitResult {
	s := r.data().service
	cause := scope.cause("confirm", h.id)
	if !nilPort(scope.knowledgeAuthority) {
		h.mu.Lock()
		if h.retained && h.original.AttemptID().Validate() == nil && h.original.Cause().Validate() == nil {
			cause = h.original.Cause()
		}
		h.mu.Unlock()
	}
	req := s.maintenanceRequest(oc.ReleaseReaderAccess, h.source.Details().Meta.ID, oc.AccessRequestDetails{LeaseID: h.id})
	return s.withinAccess(ctx, cause, req, func(ctx context.Context, tx f.Tx, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		var object, owner, process, state string
		err = e.QueryRow(ctx, `SELECT object_id::text,owner_id::text,process_id::text,state FROM agenteam_object.object_leases WHERE id=$1 AND owner_kind='source'`, h.id.String()).Scan(&object, &owner, &process, &state)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return unavailable(err)
		}
		if err == nil && (object != h.source.Details().Meta.ID.String() || owner != h.id.String() || process != h.process.String() || (state != "active" && state != "released")) {
			return failure(f.InvalidState, nil)
		}
		if h.work != nil {
			current, err := loadProjectWork(ctx, e, h.work.work.id)
			if err != nil {
				return err
			}
			if current != nil && !workIdentityEqual(h.work.work, *current) {
				return failure(f.InvalidState, nil)
			}
		}
		*validated = ctx.Err() == nil
		return ctx.Err()
	})
}

// closeProjectSourceReader owns a single original Close call, even when its
// first caller stops waiting. The channel follows both Close and its original
// cleanup registration; a later loan waits for this same tail.
func (r *SourceReads) closeProjectSourceReader(h *sourceLeaseState, reader *oc.ObjectReader) <-chan struct{} {
	h.mu.Lock()
	if h.closeDone != nil {
		done := h.closeDone
		h.mu.Unlock()
		return done
	}
	done := make(chan struct{})
	h.closeDone = done
	h.mu.Unlock()
	_, finished := r.data().service.cleanupContext()
	go func() { _ = reader.Close(); finished(); close(done) }()
	return done
}

// Forget releases completed local bookkeeping after the original Store return.
// A known NotCommitted acquisition never issued durable lease/work ownership;
// an Unknown or a still-running acquisition cannot take that branch.
func (r *SourceReads) ForgetProjectSource(lease oc.SourceLease) error {
	h, err := r.registered(lease)
	if err != nil || h == nil {
		return err
	}
	h.mu.Lock()
	rolledBack := h.acquireReturned && h.acquisition.State() == f.NotCommitted && !h.started && h.reader == nil
	ready := (h.released || rolledBack) && !h.busy && !h.releaseRunning && !h.retained && !h.stopActive
	if h.closeDone != nil {
		select {
		case <-h.closeDone:
		default:
			ready = false
		}
	}
	if !ready {
		h.mu.Unlock()
		return failure(f.ResourceBusy, nil)
	}
	h.stopActive = true
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.stopActive = false; h.mu.Unlock() }()
	if rolledBack && h.work != nil {
		state := r.data().service.state()
		state.mu.Lock()
		if h.work.origin != h.origin || h.work.joining || h.work.joinPending {
			state.mu.Unlock()
			return failure(f.ResourceBusy, nil)
		}
		for writer := range state.projectWriters {
			if writer.handle == h.work {
				state.mu.Unlock()
				return failure(f.ResourceBusy, nil)
			}
		}
		if state.projectWork[h.work.work.id] == h.work {
			delete(state.projectWork, h.work.work.id)
		}
		h.work.ended, h.work.joinRetired = true, true
		state.notifyWorkLocked()
		state.mu.Unlock()
	}
	r.forget(h)
	return nil
}
