package object

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestProjectWorkChildInheritsOnlyLiveInitialization(t *testing.T) {
	newService := func() (*Service, *serviceState) {
		r := &serviceState{initialized: true, runtime: &Runtime{}, operations: map[*operation]bool{}, changed: make(chan struct{})}
		s := &Service{func() *serviceState { return r }}
		return s, r
	}
	t.Run("initializing_parent_and_independent_cancel", func(t *testing.T) {
		s, r := newService()
		parent, finish, err := s.admit(context.Background(), true)
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		child, done, err := s.childOperation(parent.ctx)
		if err != nil || child == parent || !child.initializing || len(r.operations) != 2 {
			t.Fatal("live initialization admission was not inherited", err)
		}
		child.cancel()
		if parent.ctx.Err() != nil {
			t.Fatal("child cancelled its parent recovery round")
		}
		done()
		if len(r.operations) != 1 || !r.operations[parent] {
			t.Fatal("child did not release exactly its own lifetime")
		}
	})
	t.Run("ordinary_parent_still_needs_ready", func(t *testing.T) {
		s, r := newService()
		r.runtimeReady = true
		parent, finish, err := s.admit(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		defer finish()
		r.runtimeReady = false
		if _, _, err = s.childOperation(parent.ctx); !hasCode(err, foundation.DependencyUnavailable) {
			t.Fatal("ordinary parent bypassed runtimeReady", err)
		}
	})
	for _, mode := range []string{"no_parent", "unregistered", "foreign_service", "done", "cancelled", "caller_cancelled", "stopped", "forced", "quota"} {
		t.Run(mode, func(t *testing.T) {
			s, r := newService()
			parent, finish, err := s.admit(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			defer finish()
			ctx := parent.ctx
			switch mode {
			case "no_parent":
				ctx = context.Background()
			case "unregistered":
				fake := &operation{service: s, ctx: context.Background(), initializing: true}
				ctx = context.WithValue(context.Background(), operationKey{}, fake)
			case "foreign_service":
				other, _ := newService()
				foreign, end, e := other.admit(context.Background(), true)
				if e != nil {
					t.Fatal(e)
				}
				defer end()
				ctx = foreign.ctx
			case "done":
				finish()
			case "cancelled":
				parent.cancel()
			case "caller_cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "stopped":
				r.stopped = true
			case "forced":
				r.forced = true
			case "quota":
				for range 63 {
					r.operations[&operation{}] = true
				}
			}
			before := len(r.operations)
			if _, done, err := s.childOperation(ctx); err == nil {
				done()
				t.Fatal("invalid child admission succeeded", mode)
			}
			if len(r.operations) != before {
				t.Fatal("rejected child leaked a lifetime")
			}
		})
	}
}

func TestProjectWorkImmutableCleanupIdentity(t *testing.T) {
	project, _ := foundation.NewID[identity.Project]()
	process, _ := foundation.NewID[oc.Process]()
	object, _ := foundation.NewID[oc.StoredObject]()
	id, _ := newWorkIdentity()
	resource, _ := newWorkIdentity()
	w := projectWork{id: id, project: project, process: process, kind: "cleanup", resource: resource, object: object, fence: 2}
	changes := []func(*projectWork){func(v *projectWork) { v.resource = id }, func(v *projectWork) { v.fence++ }, func(v *projectWork) { v.process, _ = foundation.NewID[oc.Process]() }, func(v *projectWork) { v.project, _ = foundation.NewID[identity.Project]() }, func(v *projectWork) { v.object = oc.ObjectID{} }, func(v *projectWork) { v.epoch = 2; v.epochOperation = id }, func(v *projectWork) { v.kind = "verification" }}
	for _, change := range changes {
		other := w
		change(&other)
		if workIdentityEqual(w, other) {
			t.Fatal("cleanup work was interchangeable with a different lifetime")
		}
	}
	terminal := w
	terminal.joined = true
	terminal.revoked = id
	if !workIdentityEqual(w, terminal) {
		t.Fatal("terminal facts changed immutable identity")
	}
}

func TestProjectWorkIdentityLivesInPrivatePlan(t *testing.T) {
	user, _ := foundation.NewID[identity.User]()
	session, _ := foundation.NewID[identity.Session]()
	actor, _ := identity.NewHuman(user, session)
	owner, _ := oc.NewObjectOwner(oc.Avatar, user.String(), "")
	request := ownerRequest(actor, owner, oc.PrepareAccess, oc.AccessRequestDetails{})
	issuer := oc.NewAccessIssuer()
	state := &serviceState{accessIssuer: issuer}
	service := &Service{func() *serviceState { return state }}
	key, _ := foundation.UserLock(user.String())
	dependencyLocks := []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}
	deps, _ := oc.NewAccessDependencies(newDigest(make([]byte, 32)), dependencyLocks)
	id, _ := newWorkIdentity()
	makePlan := func(i oc.AccessIssuer, locks []foundation.LockRequest) oc.AccessLockPlan {
		p, err := oc.NewAccessLockPlan(i, oc.AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: deps, DomainBinding: newDigest(make([]byte, 32)), Locks: append(locks, dependencyLocks...)})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	plan := makePlan(issuer, []foundation.LockRequest{workLock(id)})
	if got, err := service.plannedWorkID(plan); err != nil || got != id {
		t.Fatal("preallocated identity did not survive immutable plan", err)
	}
	if _, err := service.plannedWorkID(makePlan(oc.NewAccessIssuer(), []foundation.LockRequest{workLock(id)})); err == nil {
		t.Fatal("foreign issuer accepted")
	}
	other, _ := newWorkIdentity()
	if _, err := service.plannedWorkID(makePlan(issuer, []foundation.LockRequest{workLock(id), workLock(other)})); err == nil {
		t.Fatal("ambiguous identity accepted")
	}
}
