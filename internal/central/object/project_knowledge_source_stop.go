package object

import (
	"context"
	"errors"
	"reflect"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Knowledge owns a separate issuer and private Project loan. It cannot consume
// an Artifact request, even when both requests happen to name the same object.
func NewSourceReadsWithKnowledgeProjectStop(service *Service, resolver oc.SourceResolver, authority oc.KnowledgeProjectSourceStopAuthority) (*SourceReads, error) {
	if nilPort(authority) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	reads, err := NewSourceReads(service, resolver)
	if err == nil {
		reads.data().knowledgeAuthority = authority
	}
	return reads, err
}

func (r *SourceReads) RequireKnowledgeProjectStopBinding(service *Service, authority oc.KnowledgeProjectSourceStopAuthority) error {
	if r == nil || r.data == nil || service == nil || r.data().service != service || nilPort(authority) || nilPort(r.data().knowledgeAuthority) || reflect.ValueOf(authority) != reflect.ValueOf(r.data().knowledgeAuthority) || !nilPort(r.data().authority) {
		return failure(f.DependencyUnbound, nil)
	}
	return nil
}

func (s *sourceReadsState) projectOwned() bool {
	return s != nil && (!nilPort(s.authority) || !nilPort(s.knowledgeAuthority))
}

func (r *SourceReads) WithinKnowledgeProjectSourceAcquisition(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if r == nil || r.data == nil || nilPort(r.data().knowledgeAuthority) || !nilPort(r.data().authority) || fn == nil {
		return rejectedAccess(failure(f.DependencyUnbound, nil))
	}
	return r.withinProjectSourceAcquisition(ctx, cause, fn)
}

func (r *SourceReads) BindKnowledgeProjectSourceInTx(ctx context.Context, tx f.Tx, lease oc.SourceLease, target i.ProjectID, command f.CommandIdentity) error {
	if r == nil || r.data == nil || nilPort(r.data().knowledgeAuthority) || !nilPort(r.data().authority) || command.Validate() != nil {
		return invalid()
	}
	owners := command.OwnerIDs()
	if command.Namespace() != "knowledge" || (command.Command() != "create" && command.Command() != "update") || len(owners) != 1 || owners[0] != target.String() {
		return invalid()
	}
	return r.bindProjectSourceInTx(ctx, tx, lease, target, command)
}

func (r *SourceReads) StopKnowledgeProjectSource(ctx context.Context, request oc.KnowledgeProjectSourceStopRequest) (bool, error) {
	if r == nil || r.data == nil || nilPort(r.data().knowledgeAuthority) || !nilPort(r.data().authority) {
		return false, failure(f.DependencyUnbound, nil)
	}
	if ctx == nil || request.Validate() != nil {
		return false, invalid()
	}
	d := request.Details()
	scope := &projectSourceStopScope{ctx: ctx, knowledgeRequest: request, knowledgeAuthority: r.data().knowledgeAuthority}
	return r.stopProjectSource(ctx, oc.ProjectSourceStopDetails{Actor: d.Actor, Cause: d.Cause, Command: d.Command, Lease: d.Lease, SourceProjectID: d.SourceProjectID, TargetProjectID: d.TargetProjectID, ObjectID: d.ObjectID}, scope, false)
}

// Inspect performs only the original read confirmation. It never starts Close,
// release, or join; the next explicit Stop invocation owns those operations.
func (r *SourceReads) InspectKnowledgeProjectSource(ctx context.Context, request oc.KnowledgeProjectSourceStopRequest) (bool, error) {
	if r == nil || r.data == nil || nilPort(r.data().knowledgeAuthority) || !nilPort(r.data().authority) {
		return false, failure(f.DependencyUnbound, nil)
	}
	if ctx == nil || request.Validate() != nil {
		return false, invalid()
	}
	d := request.Details()
	scope := &projectSourceStopScope{ctx: ctx, knowledgeRequest: request, knowledgeAuthority: r.data().knowledgeAuthority}
	return r.stopProjectSource(ctx, oc.ProjectSourceStopDetails{Actor: d.Actor, Cause: d.Cause, Command: d.Command, Lease: d.Lease, SourceProjectID: d.SourceProjectID, TargetProjectID: d.TargetProjectID, ObjectID: d.ObjectID}, scope, true)
}
func (r *SourceReads) WithKnowledgeProjectSourceStop(ctx context.Context, request oc.KnowledgeProjectSourceStopRequest, fn func(context.Context) error) error {
	if r == nil || r.data == nil || nilPort(r.data().knowledgeAuthority) || !nilPort(r.data().authority) || fn == nil {
		return failure(f.DependencyUnbound, nil)
	}
	if request.Validate() != nil {
		return invalid()
	}
	h, err := r.registered(request.Details().Lease)
	if err != nil {
		return err
	}
	if h == nil {
		return failure(f.Forbidden, nil)
	}
	return r.data().knowledgeAuthority.WithKnowledgeProjectSourceStop(ctx, request, fn)
}
func (r *SourceReads) RequireKnowledgeProjectSourceStopInTx(ctx context.Context, tx f.Tx, request oc.KnowledgeProjectSourceStopRequest) error {
	if r == nil || r.data == nil || nilPort(r.data().knowledgeAuthority) || !nilPort(r.data().authority) {
		return failure(f.DependencyUnbound, nil)
	}
	if request.Validate() != nil {
		return invalid()
	}
	h, err := r.registered(request.Details().Lease)
	if err != nil {
		return err
	}
	if h == nil {
		return failure(f.Forbidden, nil)
	}
	return r.data().knowledgeAuthority.RequireKnowledgeProjectSourceStopInTx(ctx, tx, request)
}

func (s *projectSourceStopScope) requiredLocks() ([]f.LockRequest, error) {
	if !nilPort(s.knowledgeAuthority) {
		if !nilPort(s.authority) || s.request.Validate() == nil {
			return nil, failure(f.Forbidden, nil)
		}
		return s.knowledgeRequest.RequiredLocks()
	}
	return s.request.RequiredLocks()
}

func (s *projectSourceStopScope) require(ctx context.Context, tx f.Tx) error {
	if !nilPort(s.knowledgeAuthority) {
		if !nilPort(s.authority) || s.request.Validate() == nil {
			return failure(f.Forbidden, nil)
		}
		return s.knowledgeAuthority.RequireKnowledgeProjectSourceStopInTx(ctx, tx, s.knowledgeRequest)
	}
	if nilPort(s.authority) {
		return failure(f.DependencyUnbound, nil)
	}
	return s.authority.RequireProjectSourceStopInTx(ctx, tx, s.request)
}

func (s *projectSourceStopScope) cause(phase string, lease oc.LeaseID) f.TransactionCause {
	namespace, operation := "artifact-source-", s.request.Details().Cause.Details().OperationID
	if !nilPort(s.knowledgeAuthority) {
		namespace, operation = "knowledge-source-", s.knowledgeRequest.Details().Cause.Details().OperationID
	}
	cause, _ := f.NewRecoveryCause(namespace+phase, lease.String(), operation.String())
	return cause
}

type knowledgeObjectCleanupKey struct{}
type knowledgeObjectCleanupCall struct {
	mu               sync.Mutex
	owner            *sourceReadsState
	cause            oc.ObjectCleanupCause
	object           oc.ObjectID
	active, retained bool
	original         f.CommitResult
	handles          map[*projectWorkHandle]bool
}

func (c *knowledgeObjectCleanupCall) retainedResult() (f.CommitResult, bool) {
	if c == nil {
		return f.CommitResult{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.original, c.retained
}
func (c *knowledgeObjectCleanupCall) capture(result f.CommitResult) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.retained && result.State() == f.Unknown {
		c.retained = true
		c.original = result
	}
}
func (c *knowledgeObjectCleanupCall) captureHandles(handles []*projectWorkHandle) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handles == nil {
		c.handles = map[*projectWorkHandle]bool{}
	}
	for _, h := range handles {
		c.handles[h] = true
	}
}
func (s *Service) knowledgeObjectCleanup(ctx context.Context) *knowledgeObjectCleanupCall {
	c, _ := ctx.Value(knowledgeObjectCleanupKey{}).(*knowledgeObjectCleanupCall)
	if c == nil || c.owner == nil || c.owner.service != s {
		return nil
	}
	c.mu.Lock()
	active := c.active
	c.mu.Unlock()
	if !active {
		return nil
	}
	return c
}
func (c *knowledgeObjectCleanupCall) requiredLocks() ([]f.LockRequest, error) {
	if c == nil || !knowledgeProjectCleanup(c.cause) {
		return nil, failure(f.Forbidden, nil)
	}
	p, err := f.ParseID[i.Project](c.cause.Details().Owner.Details().ProjectID)
	if err != nil {
		return nil, invalid()
	}
	key, err := f.ProjectLock(p.String())
	if err != nil {
		return nil, invalid()
	}
	return []f.LockRequest{{Key: key, Mode: f.Exclusive}}, nil
}
func (s *Service) requireKnowledgeObjectCleanup(ctx context.Context, tx f.Tx, c *knowledgeObjectCleanupCall, object oc.ObjectID) error {
	if c == nil {
		return nil
	}
	if c.owner == nil || c.owner.service != s || c.object != object || !knowledgeProjectCleanup(c.cause) || nilPort(s.state().auth.Cleanup) {
		return failure(f.Forbidden, nil)
	}
	c.mu.Lock()
	active := c.active
	c.mu.Unlock()
	if !active {
		return failure(f.Forbidden, nil)
	}
	if err := s.state().auth.Cleanup.CheckCleanupInTx(ctx, tx, c.cause, c.object); err != nil {
		return portError(err)
	}
	return nil
}

// This is a leaf of the same original Knowledge graph, never a whole-Project
// Object cleanup. Its current Knowledge leaf is rechecked by CleanupAuthority
// in every original transaction, including maintenance and join checkpoints.
func (r *SourceReads) DeleteKnowledgeProjectObject(ctx context.Context, cause oc.ObjectCleanupCause, object oc.ObjectID) (out oc.CleanupResult, err error) {
	if r == nil || r.data == nil || nilPort(r.data().knowledgeAuthority) || !knowledgeProjectCleanup(cause) || object.Validate() != nil {
		return out, failure(f.DependencyUnbound, nil)
	}
	if ctx == nil {
		return out, invalid()
	}
	st := r.data()
	key := cause.Details().OperationID
	st.mu.Lock()
	if st.knowledgeCleanups == nil {
		st.knowledgeCleanups = map[oc.CleanupID]*knowledgeObjectCleanupCall{}
	}
	c := st.knowledgeCleanups[key]
	if c == nil {
		c = &knowledgeObjectCleanupCall{owner: st, cause: cause, object: object}
		st.knowledgeCleanups[key] = c
	}
	c.mu.Lock()
	valid := c.object == object && c.cause.Details().Owner.Equal(cause.Details().Owner) && c.cause.Details().Reason == cause.Details().Reason
	if !valid || c.active {
		c.mu.Unlock()
		st.mu.Unlock()
		return out, failure(f.ResourceBusy, nil)
	}
	c.active = true
	retained, original := c.retained, c.original
	c.mu.Unlock()
	st.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.active = false
		keep := c.retained
		c.mu.Unlock()
		if !keep {
			c.mu.Lock()
			keep = len(c.handles) > 0
			c.mu.Unlock()
		}
		if !keep {
			st.mu.Lock()
			if st.knowledgeCleanups[key] == c {
				delete(st.knowledgeCleanups, key)
			}
			st.mu.Unlock()
		}
	}()
	out = oc.CleanupResult{State: oc.CleanupPending, OperationID: key}
	if retained {
		if err = r.confirmKnowledgeProjectObject(ctx, c, original); err != nil {
			return out, err
		}
		return out, nil
	}
	run := context.WithValue(ctx, knowledgeObjectCleanupKey{}, c)
	// Preserve this same current loan and original budget through finishOperationWork.
	run = context.WithValue(run, boundedCleanupBudgetKey{}, run)
	c.mu.Lock()
	handles := make([]*projectWorkHandle, 0, len(c.handles))
	for h := range c.handles {
		handles = append(handles, h)
	}
	c.mu.Unlock()
	for _, h := range handles {
		if err = st.service.joinProjectWork(run, h); err != nil {
			return out, err
		}
		c.mu.Lock()
		delete(c.handles, h)
		c.mu.Unlock()
	}
	out, err = st.service.DeleteUnreferencedWithinBudget(run, cause, object)
	c.mu.Lock()
	handles = handles[:0]
	for h := range c.handles {
		handles = append(handles, h)
	}
	c.mu.Unlock()
	st.service.state().mu.Lock()
	var retired []*projectWorkHandle
	for _, h := range handles {
		if h.joinRetired {
			retired = append(retired, h)
		}
	}
	st.service.state().mu.Unlock()
	c.mu.Lock()
	for _, h := range retired {
		delete(c.handles, h)
	}
	c.mu.Unlock()
	if result, unknown := c.retainedResult(); unknown {
		out.State = oc.CleanupPending
		return out, projectCleanupUnknown(result)
	}
	c.mu.Lock()
	pending := len(c.handles) != 0
	c.mu.Unlock()
	if pending {
		out.State = oc.CleanupPending
		if err == nil {
			err = failure(f.ResourceBusy, nil)
		}
	}
	return out, err
}
func (r *SourceReads) confirmKnowledgeProjectObject(ctx context.Context, c *knowledgeObjectCleanupCall, original f.CommitResult) error {
	s := r.data().service
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.CleanupObjectAccess, Cleanup: c.cause, ObjectID: c.object})
	if err != nil {
		return invalid()
	}
	plan, err := s.DiscoverAccess(ctx, request)
	if err != nil {
		return errors.Join(projectCleanupUnknown(original), err)
	}
	extra, err := c.requiredLocks()
	if err != nil {
		return err
	}
	valid := false
	result := s.state().store.WithinTx(ctx, original.Cause(), func(ctx context.Context, tx f.Tx) error {
		locked, err := s.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, extra)
		if err != nil {
			return err
		}
		if err = s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
			return err
		}
		if err = s.requireKnowledgeObjectCleanup(ctx, tx, c, c.object); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		obj, found, err := loadObject(ctx, x, c.object)
		if err != nil {
			return err
		}
		if !found || !objectPartition(obj, c.cause.Details().Owner) {
			return failure(f.InvalidState, nil)
		}
		// Reading the original retained graph does not start cleanup or join.
		if _, err = inspect(ctx, x, c.object); err != nil {
			return err
		}
		valid = ctx.Err() == nil
		return ctx.Err()
	})
	if result.State() != f.Committed || !valid || ctx.Err() != nil {
		return errors.Join(projectCleanupUnknown(original), commitError(result), ctx.Err())
	}
	c.mu.Lock()
	c.retained = false
	c.mu.Unlock()
	return nil
}
