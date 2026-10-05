//go:build integration

package project_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type commitStore struct {
	*postgres.Store
	proxy          *commitProxy
	enabled, fired atomic.Bool
	target         c.ProjectID
	phase          string
}

func (w *commitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if !w.enabled.Load() || w.fired.Load() {
			return nil
		}
		x, e := w.InTx(tx)
		if e != nil {
			return e
		}
		var exists bool
		if w.phase == "creation-accepted" {
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.creations WHERE project_id=$1 AND state='accepted')`, w.target.String()).Scan(&exists)
		} else if w.phase == "creation-claim" {
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.creations c JOIN agenteam_project.work_claims w ON w.work_kind='creation' AND w.work_id=c.id WHERE c.project_id=$1 AND c.state='initializing' AND c.event_header IS NULL AND w.phase='running')`, w.target.String()).Scan(&exists)
		} else if w.phase == "creation-plan" {
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.creations WHERE project_id=$1 AND state='initializing' AND event_header IS NOT NULL)`, w.target.String()).Scan(&exists)
		} else if w.phase == "creation-completed" || w.phase == "creation-checkpoint" {
			state := "completed"
			if w.phase == "creation-checkpoint" {
				state = "failed"
			}
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.creations WHERE project_id=$1 AND state=$2)`, w.target.String(), state).Scan(&exists)
		} else {
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND state=$2)`, w.target.String(), w.phase).Scan(&exists)
		}
		if e != nil {
			return e
		}
		if exists && w.fired.CompareAndSwap(false, true) {
			w.proxy.armed.Store(true)
		}
		return nil
	})
}
func newProxyFixture(t *testing.T, commit bool) (*b02Fixture, *commitStore, *commitProxy) {
	t.Helper()
	db := newDatabase(t)
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), commit)
	u, e := url.Parse(db.Fixture.URL(db.Name))
	if e != nil {
		t.Fatal(e)
	}
	u.Host = proxy.listener.Addr().String()
	raw := openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	store := &commitStore{Store: raw, proxy: proxy}
	return assemble(t, db, raw, store), store, proxy
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("owned asynchronous work did not reach checkpoint")
	}
}
func unknownCommand(t *testing.T, f *b02Fixture, owner identity.Actor, phase string) (func(context.Context) error, c.CommandLookupRequest) {
	t.Helper()
	var invoke func(context.Context) error
	var request c.CommandLookupRequest
	if strings.HasPrefix(phase, "creation-") {
		if phase == "creation-checkpoint" {
			f.skills.setMode("failed")
		}
		target := id[identity.Project](t)
		m := meta(t, "unknown-create", nil)
		r := c.CreateProjectRequest{ProjectID: target, Name: "UnknownCreate"}
		invoke = func(ctx context.Context) error { _, e := f.service.CreateProject(ctx, owner, m, r); return e }
		request = c.CommandLookupRequest{ProjectID: target, Command: c.CreateCommand, Key: m.IdempotencyKey}
	} else {
		p, _, _ := f.create(t, owner, "UnknownUpdate")
		version := p.Version
		description := "after"
		m := meta(t, "unknown-update", &version)
		invoke = func(ctx context.Context) error {
			_, e := f.service.UpdateProject(ctx, owner, m, p.ID, c.UpdateProjectRequest{Description: &description})
			return e
		}
		request = c.CommandLookupRequest{ProjectID: p.ID, Command: c.UpdateCommand, Key: m.IdempotencyKey}
	}
	return invoke, request
}
func TestProjectB02UnknownWriterHeldThenCommitOrRollback(t *testing.T) {
	for _, phase := range []string{"creation-accepted", "creation-claim", "creation-plan", "creation-completed", "creation-checkpoint", "planned", "completed"} {
		for _, commit := range []bool{true, false} {
			name := phase + "/rollback"
			if commit {
				name = phase + "/late-commit"
			}
			t.Run(name, func(t *testing.T) {
				f, w, proxy := newProxyFixture(t, commit)
				owner := f.human(t, "owner-alpha", "user")
				invoke, request := unknownCommand(t, f, owner, phase)
				w.target = request.ProjectID
				w.phase = phase
				w.enabled.Store(true)
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- invoke(ctx) }()
				await(t, proxy.reached)
				select {
				case e := <-done:
					var fault *foundation.Fault
					if !errors.As(e, &fault) || fault.CommitState != foundation.Unknown {
						t.Fatal("held original writer was declared terminal", e)
					}
					saved, ok := project.UnknownAttempt(e)
					if !ok || saved.AttemptID().Validate() != nil || saved.Cause().Validate() != nil {
						t.Fatal("original unknown provenance missing")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("request did not join within original observation bound")
				}
				check, stop := context.WithTimeout(context.Background(), 150*time.Millisecond)
				_, e := f.service.LookupCommand(check, owner, request)
				stop()
				if e == nil {
					t.Fatal("pre-serialization absence returned success")
				}
				close(proxy.release)
				await(t, proxy.completed)
				lookup, e := f.service.LookupCommand(ctxFor(t), owner, request)
				if e != nil {
					t.Fatal("resolved lookup", e)
				}
				expected := c.LookupNotObserved
				if strings.HasPrefix(phase, "creation-") && phase != "creation-accepted" {
					expected = c.LookupCommitted // the original acceptance already committed
				} else if commit {
					if phase == "planned" {
						expected = c.LookupInProgress
					} else {
						expected = c.LookupCommitted
					}
				} else if phase == "completed" {
					expected = c.LookupInProgress
				}
				if lookup.State != expected {
					t.Fatalf("lookup=%s want=%s", lookup.State, expected)
				}
				if phase == "creation-completed" {
					if lookup.Result == nil || lookup.Result.Creation == nil {
						t.Fatal("missing original creation checkpoint")
					}
					want := c.CreationPending
					if commit {
						want = c.CreationReady
					}
					if lookup.Result.Creation.State != want {
						t.Fatal("completion Unknown resolved to wrong checkpoint", lookup.Result.Creation.State, want)
					}
				}
				if phase == "creation-checkpoint" {
					if lookup.Result == nil || lookup.Result.Creation == nil || lookup.Result.Creation.Operation == nil {
						t.Fatal("missing failed-round checkpoint")
					}
					want := c.CreationInitializing
					if commit {
						want = c.CreationFailed
					}
					if lookup.Result.Creation.Operation.State != want {
						t.Fatal("failed-round Unknown resolved incorrectly", lookup.Result.Creation.Operation.State, want)
					}
					f.skills.setMode("")
				}
				if e = invoke(ctxFor(t)); e != nil {
					t.Fatal("same original intent recovery", e)
				}
				lookup, e = f.service.LookupCommand(ctxFor(t), owner, request)
				if e != nil || lookup.State != c.LookupCommitted {
					t.Fatal("recovered receipt", e)
				}
				var count int
				if e = f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_project.projects WHERE id=$1`, request.ProjectID.String()).Scan(&count); e != nil || count != 1 {
					t.Fatal("target duplicated/replaced", count, e)
				}
				if strings.HasPrefix(phase, "creation-") {
					var skills, accepted, completed, events int
					var creationEvent, eventID string
					if e = f.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM project_fixture.skills WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.create.accepted'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.create.completed'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.created'),(SELECT event_id::text FROM agenteam_project.creations WHERE project_id=$1),(SELECT id::text FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.created')`, request.ProjectID.String()).Scan(&skills, &accepted, &completed, &events, &creationEvent, &eventID); e != nil || skills != 1 || accepted != 1 || completed != 1 || events != 1 || creationEvent != eventID {
						t.Fatal("Unknown recovery duplicated or replaced canonical facts", skills, accepted, completed, events, e)
					}
				}
			})
		}
	}
}

func TestProjectB02UnknownOriginalCallResolvesAfterWriterSerialization(t *testing.T) {
	for _, phase := range []string{"creation-accepted", "planned", "completed"} {
		for _, commit := range []bool{true, false} {
			name := phase + "/rollback"
			if commit {
				name = phase + "/commit"
			}
			t.Run(name, func(t *testing.T) {
				f, w, proxy := newProxyFixture(t, commit)
				owner := f.human(t, "owner-alpha", "user")
				invoke, request := unknownCommand(t, f, owner, phase)
				w.target, w.phase = request.ProjectID, phase
				w.enabled.Store(true)
				done := make(chan error, 1)
				go func() { done <- invoke(ctxFor(t)) }()
				await(t, proxy.reached)
				// Release the real original backend before the bounded receipt
				// confirmation ends; the store still first observes a lost ACK.
				close(proxy.release)
				await(t, proxy.completed)
				var err error
				select {
				case err = <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("serialized confirmation failed to join")
				}
				if !commit || phase == "planned" {
					want := foundation.NotCommitted
					if commit {
						want = foundation.Unknown // A committed plan is not an Update result.
					}
					var fault *foundation.Fault
					if !errors.As(err, &fault) || fault.CommitState != want {
						t.Fatal("wrong serialized original-call outcome", err, want)
					}
					attempt, ok := project.UnknownAttempt(err)
					if !ok || attempt.AttemptID().Validate() != nil || attempt.Cause().Validate() != nil || fault.CauseID != attempt.AttemptID().String() {
						t.Fatal("original physical attempt/cause was replaced")
					}
				} else if err != nil {
					t.Fatal("confirmed business receipt was not returned", err)
				}
				lookup, err := f.service.LookupCommand(ctxFor(t), owner, request)
				want := c.LookupNotObserved
				if phase == "planned" && commit || phase == "completed" && !commit {
					want = c.LookupInProgress
				} else if commit {
					want = c.LookupCommitted
				}
				if err != nil || lookup.State != want {
					t.Fatal("serialized durable result", lookup.State, want, err)
				}
				if err = invoke(ctxFor(t)); err != nil {
					t.Fatal("same-identity recovery", err)
				}
				lookup, err = f.service.LookupCommand(ctxFor(t), owner, request)
				if err != nil || lookup.State != c.LookupCommitted {
					t.Fatal("recovery did not preserve original identity", err)
				}
			})
		}
	}
}
