//go:build integration

package outbox_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type lifecycleResolver struct {
	*lifecycleAuthority
	blocked identity.ProjectID
	mode    atomic.Int32
}

func (a *lifecycleResolver) ResolveLifecycleActor(ctx context.Context, c oc.LifecycleCause) (identity.Actor, error) {
	if c.Validate() != nil {
		return identity.Actor{}, foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	v := c.Details()
	if v.ProjectID == a.blocked {
		switch a.mode.Load() {
		case 1:
			return identity.Actor{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		case 2:
			return identity.NewHuman(idFromProject(v.ProjectID), newRecoverySession())
		case 4:
			<-ctx.Done()
			return identity.Actor{}, ctx.Err()
		}
	}
	var cause string
	if err := a.base.store.QueryRow(ctx, `SELECT actor_cause FROM outbox_fixture.lifecycle WHERE project_id=$1 AND operation_id=$2 AND action=$3 AND version=$4`, v.ProjectID.String(), v.OperationID.String(), string(v.Action), int64(v.ProjectVersion)).Scan(&cause); err != nil {
		return identity.Actor{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	role, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(v.ProjectID)
	actor, err := role.Actor(cause, scope)
	if v.ProjectID == a.blocked && a.mode.CompareAndSwap(3, 1) {
		_, err = a.base.store.Exec(ctx, `UPDATE outbox_fixture.lifecycle SET version=version+1 WHERE project_id=$1`, v.ProjectID.String())
	}
	return actor, err
}
func idFromProject(p identity.ProjectID) identity.UserID {
	v, _ := foundation.ParseID[identity.User](p.String())
	return v
}
func newRecoverySession() identity.SessionID { v, _ := foundation.NewID[identity.Session](); return v }

func recoveryRuntime(t *testing.T, f *fixture, a oc.ProjectAuthority, h *handler) *outbox.Runtime {
	t.Helper()
	s, err := outbox.New(f.store, f.cat, outbox.Authorizations{Projects: a, Processes: f.auth})
	if err != nil {
		t.Fatal(err)
	}
	r, err := outbox.NewRuntime(s, []oc.HandlerDefinition{h.definition(1)}, outbox.Options{PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := r.Force(ctx); err != nil {
			t.Error(err)
		}
	})
	return r
}
func stoppedProject(t *testing.T, f *fixture, s *outbox.Service) oc.LifecycleCause {
	t.Helper()
	_, result := f.append(t, s, f.store, f.actor, f.event(t, true, 1, "private recovery payload"))
	state(t, result, foundation.Committed)
	actor, cause := lifecycleCause(t, f, oc.DeleteProject, 2)
	if report, err := s.RequestStop(ctxFor(t), actor, cause); err != nil || !report.Stopped {
		t.Fatal("stop preparation", err)
	}
	f.sql(t, `UPDATE outbox_fixture.lifecycle SET others=true WHERE project_id=$1`, f.project.String())
	return cause
}
func TestOutboxLifecycleRebuiltRuntimeResolvesAndCompletesCleanup(t *testing.T) {
	f := newFixture(t)
	s, a := lifecycleService(t, f, f.auth)
	h := f.handler("recovery.lifecycle")
	if _, err := s.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
		t.Fatal(err)
	}
	c := stoppedProject(t, f, s)
	resolver := &lifecycleResolver{lifecycleAuthority: a}
	r := recoveryRuntime(t, f, resolver, h)
	if err := r.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	waitOutbox(t, func() bool {
		return f.count(t, `SELECT count(*) FROM agenteam_outbox.project_lifecycle WHERE phase='completed'`) == 1
	})
	r.StopClaims()
	if err := r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.events`)+f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries`) != 0 {
		t.Fatal("worker reported completed without erasure")
	}
	// Only completed receipts remain: a fresh runtime needs no resolver, even
	// though the old Project actor/cause mapping has now physically disappeared.
	f.sql(t, `DELETE FROM outbox_fixture.lifecycle WHERE project_id=$1`, c.Details().ProjectID.String())
	next := recoveryRuntime(t, f, a, h)
	if err := next.Initialize(ctxFor(t)); err != nil {
		t.Fatal("completed receipt required an actor", err)
	}
	var raw string
	if err := f.store.QueryRow(ctxFor(t), `SELECT row_to_json(l)::text FROM agenteam_outbox.project_lifecycle l`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "actor") || strings.Contains(raw, "cause_ref") || strings.Contains(raw, "private recovery payload") {
		t.Fatal("cleanup receipt retained actor/body")
	}
}
func TestOutboxLifecycleRecoveryRefusalDoesNotBlockIndependentWork(t *testing.T) {
	for _, mode := range []int32{0, 1, 2, 3} {
		name := map[int32]string{0: "unbound", 1: "refused", 2: "wrong-actor", 3: "changed-after-resolution"}[mode]
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			s, a := lifecycleService(t, f, f.auth)
			h := f.handler("recovery.independent")
			if _, err := s.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
				t.Fatal(err)
			}
			first := stoppedProject(t, f, s)
			other := *f
			other.project = id[event.Project](t)
			f.sql(t, `INSERT INTO outbox_fixture.authority(id,parent_id) VALUES($1,$2)`, other.project.String(), id[event.Aggregate](t).String())
			second := stoppedProject(t, &other, s)
			// A separate known marker can always be recognized without a domain
			// recovery actor; missing/invalid cleanup capability cannot hide it.
			e := f.event(t, false, 1, "system independent marker")
			_, result := f.append(t, s, f.store, f.actor, e)
			state(t, result, foundation.Committed)
			d := f.delivery(t, e, string(h.name))
			f.claim(t, d)
			plan, err := s.PrepareDelivery(ctxFor(t), d)
			if err != nil {
				t.Fatal(err)
			}
			result, _ = s.ApplyDelivery(ctxFor(t), plan)
			state(t, result, foundation.Committed)
			f.sql(t, `UPDATE agenteam_outbox.deliveries SET phase='processing' WHERE id=$1`, d.String())
			var projects oc.ProjectAuthority = a
			if mode != 0 {
				resolver := &lifecycleResolver{lifecycleAuthority: a, blocked: first.Details().ProjectID}
				resolver.mode.Store(mode)
				projects = resolver
			}
			r := recoveryRuntime(t, f, projects, h)
			if err = r.Initialize(ctxFor(t)); err == nil {
				t.Fatal("untrusted lifecycle recovery accepted")
			}
			for i := 0; i < 3; i++ {
				if err = r.Recover(ctxFor(t)); err == nil {
					t.Fatal("refused checkpoint was hidden")
				}
			}
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND phase='succeeded'`, d.String()) != 1 {
				t.Fatal("refusal blocked independent marker")
			}
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1`, first.Details().ProjectID.String()) != 1 {
				t.Fatal("invalid actor/cause erased payload")
			}
			if mode != 0 && f.count(t, `SELECT count(*) FROM agenteam_outbox.project_lifecycle WHERE project_id=$1 AND phase='completed'`, second.Details().ProjectID.String()) != 1 {
				t.Fatal("refusal starved second project")
			}
		})
	}
}
func TestOutboxLifecycleResolverSharesRecoveryDeadline(t *testing.T) {
	f := newFixture(t)
	s, a := lifecycleService(t, f, f.auth)
	h := f.handler("recovery.deadline")
	if _, err := s.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
		t.Fatal(err)
	}
	c := stoppedProject(t, f, s)
	resolver := &lifecycleResolver{lifecycleAuthority: a, blocked: c.Details().ProjectID}
	resolver.mode.Store(4)
	r := recoveryRuntime(t, f, resolver, h)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := r.Recover(ctx)
	if err == nil || time.Since(start) > 300*time.Millisecond || f.count(t, `SELECT count(*) FROM agenteam_outbox.events`) != 1 {
		t.Fatal("resolver renewed recovery budget or erased unproven state", err)
	}
}
