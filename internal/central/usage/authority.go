package usage

import (
	"context"
	"encoding/json"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type ProjectAuthority interface {
	AuthorizeProject(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error)
}
type Authorizations struct {
	Sessions    id.SessionAuthority
	Projects    ProjectAuthority
	Invocations uc.InvocationFacts
}
type Authority struct{ data func() *authorityState }
type authorityState struct {
	store Store
	auth  Authorizations
}

func NewAuthority(s Store, a Authorizations) (*Authority, error) {
	if nilPort(s) || nilPort(a.Sessions) || nilPort(a.Projects) || a.Invocations != nil && nilPort(a.Invocations) {
		return nil, fault(f.DependencyUnbound)
	}
	v := &authorityState{s, a}
	return &Authority{func() *authorityState { return v }}, nil
}
func (a *Authority) state() *authorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}
func projectLock(p id.ProjectID) f.LockRequest {
	k, _ := f.ProjectLock(p.String())
	return f.LockRequest{Key: k, Mode: f.Shared}
}
func summaryLock(p id.ProjectID, e id.ExecutionID) f.LockRequest {
	k, _ := f.RecordLock(f.ProjectionRecordLock, "model-usage:"+p.String()+":"+e.String())
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}
func invocationCause(r uc.InvocationRequest) f.TransactionCause {
	c, _ := f.NewCommandIdentity("model.usage", []string{r.Identity.Consumer.ProjectID.String()}, "call-ledger", f.IdempotencyKey(r.Identity.Attempt.CallID.String()))
	out, _ := f.NewCommandsCause(c)
	return out
}
func localLocks(r uc.InvocationRequest) []f.LockRequest {
	c := invocationCause(r)
	k, _ := f.CommandLock(c.Details().Primary)
	i, _ := f.RecordLock(f.CommandRecordLock, "model-invocation:"+r.Identity.Attempt.InvocationID.String())
	ls := []f.LockRequest{{Key: k, Mode: f.Exclusive}, projectLock(r.Identity.Consumer.ProjectID), {Key: i, Mode: f.Exclusive}}
	if r.Identity.Consumer.ExecutionID != nil {
		ls = append(ls, summaryLock(r.Identity.Consumer.ProjectID, *r.Identity.Consumer.ExecutionID))
	}
	return ls
}
func unionLocks(in []f.LockRequest) []f.LockRequest {
	ls := append([]f.LockRequest(nil), in...)
	slices.SortFunc(ls, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	out := ls[:0]
	for _, l := range ls {
		if len(out) > 0 && f.CompareLockKeys(out[len(out)-1].Key, l.Key) == 0 {
			if l.Mode == f.Exclusive {
				out[len(out)-1].Mode = f.Exclusive
			}
		} else {
			out = append(out, l)
		}
	}
	return out
}
func planMapping(m f.Digest, ls []f.LockRequest) f.Digest {
	type lock struct {
		Key  string
		Mode f.LockMode
	}
	v := struct {
		Format  int
		Mapping f.Digest
		Locks   []lock
	}{Format: 1, Mapping: m}
	for _, l := range ls {
		v.Locks = append(v.Locks, lock{l.Key.Canonical(), l.Mode})
	}
	b, _ := json.Marshal(v)
	return hash(b)
}
func (s *Service) writerReady(ctx context.Context, r uc.InvocationRequest) error {
	if e := contextError(ctx); e != nil {
		return e
	}
	if s.state() == nil || nilPort(s.state().authority.state().auth.Invocations) {
		return fault(f.DependencyUnbound)
	}
	if r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	return nil
}
func (s *Service) DiscoverInvocation(ctx context.Context, r uc.InvocationRequest) (uc.InvocationPlan, error) {
	if e := s.writerReady(ctx, r); e != nil {
		return uc.InvocationPlan{}, e
	}
	r = r.Clone()
	b, _ := uc.InvocationBinding(r)
	d, e := s.state().authority.state().auth.Invocations.Discover(ctx, r)
	if e != nil {
		return uc.InvocationPlan{}, portError(e)
	}
	if e = contextError(ctx); e != nil {
		return uc.InvocationPlan{}, e
	}
	if d.Validate() != nil || d.Details().Binding != b {
		return uc.InvocationPlan{}, fault(f.Forbidden)
	}
	ls := unionLocks(append(localLocks(r), d.RequiredLocks()...))
	return uc.NewInvocationPlan(s.state().issuer, uc.InvocationPlanDetails{Binding: b, Mapping: planMapping(d.Details().Mapping, ls), Locks: ls, Facts: d, Cause: invocationCause(r)})
}
func (s *Service) validateInvocation(ctx context.Context, tx f.Tx, r uc.InvocationRequest, p uc.InvocationPlan) (postgres.SQLExecutor, uc.InvocationFact, error) {
	var empty uc.InvocationFact
	if e := s.writerReady(ctx, r); e != nil {
		return nil, empty, e
	}
	x, e := s.state().store.InTx(tx)
	if e != nil {
		return nil, empty, portError(e)
	}
	b, _ := uc.InvocationBinding(r)
	d := p.Details()
	ls := unionLocks(append(localLocks(r), d.Facts.RequiredLocks()...))
	if p.Validate() != nil || !p.Matches(s.state().issuer, b, planMapping(d.Facts.Details().Mapping, ls)) || d.Cause.Details().Primary.Canonical() != invocationCause(r).Details().Primary.Canonical() || !locksEqual(ls, p.RequiredLocks()) {
		return nil, empty, fault(f.Forbidden)
	}
	if e = s.state().store.RequireHeldLocks(ctx, tx, ls); e != nil {
		return nil, empty, portError(e)
	}
	v, e := s.state().authority.state().auth.Invocations.ValidateInTx(ctx, tx, r, d.Facts)
	if e != nil {
		return nil, empty, portError(e)
	}
	if e = contextError(ctx); e != nil {
		return nil, empty, e
	}
	if v.Validate() != nil || !v.Identity.Equal(r.Identity) || v.Sequence != r.Sequence {
		return nil, empty, fault(f.ResourceBusy)
	}
	if r.Access == uc.ConfirmInvocation || r.Action == uc.FinalizeAction || r.Action == uc.ObserveAction && v.Value.Dispatch != uc.Authorized {
		a := r.Actor.Details()
		if a.Kind != id.Service || a.ServiceName != id.ModelRuntime || a.ProjectID != r.Identity.Consumer.ProjectID.String() || a.CauseRef != r.Identity.Attempt.InvocationID.String() {
			return nil, empty, fault(f.Forbidden)
		}
	} else if stableActor(r.Actor) != stableActor(v.Initiator) || r.Actor.Details().ServiceName == id.ModelRuntime {
		return nil, empty, fault(f.Forbidden)
	}
	return x, v.Clone(), nil
}
func locksEqual(a, b []f.LockRequest) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if f.CompareLockKeys(a[i].Key, b[i].Key) != 0 || a[i].Mode != b[i].Mode {
			return false
		}
	}
	return true
}
func (s *Service) authorizeReader(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, extra []f.LockRequest) (postgres.SQLExecutor, error) {
	if e := contextError(ctx); e != nil {
		return nil, e
	}
	if s.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if actor.Validate() != nil || project.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if actor.Details().Kind != id.Human {
		return nil, fault(f.Forbidden)
	}
	x, e := s.state().store.InTx(tx)
	if e != nil {
		return nil, portError(e)
	}
	u, _ := f.UserLock(actor.Details().UserID)
	ls := unionLocks(append([]f.LockRequest{{Key: u, Mode: f.Shared}, projectLock(project)}, extra...))
	if e = s.state().store.AcquireAll(ctx, tx, ls); e != nil {
		return nil, portError(e)
	}
	if e = s.state().authority.state().auth.Sessions.RequireCurrentSession(ctx, tx, actor); e != nil {
		return nil, portError(e)
	}
	g, e := s.state().authority.state().auth.Projects.AuthorizeProject(ctx, tx, actor, project, id.Read)
	if e != nil {
		return nil, portError(e)
	}
	scope, _ := id.InProject(project)
	if !g.Matches(actor, scope, id.Read) {
		return nil, fault(f.Forbidden)
	}
	return x, contextError(ctx)
}
