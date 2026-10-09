//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func (v *variableHTTPFixture) tx(t *testing.T, locks []f.LockRequest, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) {
	t.Helper()
	locks, err := oc.NormalizeLocks(locks)
	if err != nil {
		t.Fatal(err)
	}
	result := v.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
		if e := v.tracked.AcquireAll(ctx, tx, locks); e != nil {
			return e
		}
		x, e := v.tracked.InTx(tx)
		if e != nil {
			return e
		}
		return fn(ctx, tx, x)
	})
	if result.State() != f.Committed {
		t.Fatal("fixture Tx did not commit", result.Fault())
	}
}
func ownerLocks(a identity.Actor, p pc.ProjectID) []f.LockRequest {
	u, _ := f.UserLock(a.Details().UserID)
	project, _ := f.ProjectLock(p.String())
	return []f.LockRequest{{Key: u, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}}
}
func (v *variableHTTPFixture) transferOwner(t *testing.T, p pc.ProjectID, old, next identity.Actor) {
	t.Helper()
	u, _ := f.UserLock(next.Details().UserID)
	locks := append(ownerLocks(old, p), f.LockRequest{Key: u, Mode: f.Exclusive})
	v.tx(t, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if _, e := v.projectAuthority.RequireOwnerInTx(ctx, tx, old, p, identity.Mutate); e != nil {
			return e
		}
		if e := v.accounts.RequireCurrentSession(ctx, tx, next); e != nil {
			return e
		}
		_, e := x.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, p.String(), next.Details().UserID)
		return e
	})
	t.Log("test-owned Owner transfer with actual User/Project locks; no public transfer command claim")
}
func (v *variableHTTPFixture) archiveFixture(t *testing.T, p pc.ProjectID, a identity.Actor) {
	t.Helper()
	v.tx(t, ownerLocks(a, p), func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if _, e := v.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Read); e != nil {
			return e
		}
		_, e := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, p.String())
		return e
	})
	t.Log("archived row is a disclosed lifecycle gate fixture, not participant/cleanup acceptance")
}
func (v *variableHTTPFixture) revoke(t *testing.T, b variableHTTPBrowser) {
	t.Helper()
	if e := v.core.Logout(ctxFor(t), account.LogoutRequest{Actor: b.actor, Key: f.IdempotencyKey(id[struct{}](t).String())}); e != nil {
		t.Fatal("formal logout", e)
	}
}

func TestProjectVariableAuthority(t *testing.T) {
	v := newVariableHTTPFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	input := createInput(t, "Authority", "private")
	m := meta(t, "authority-original", nil)
	original, e := v.service.CreateVariable(ctxFor(t), a, m, p, input)
	if e != nil {
		t.Fatal(e)
	}
	target := original.Fields().Variable.Fields()
	q := queryFor(t, a, m, p, target.ID, vc.CreateCommand, input)
	t.Run("current-owner-and-scope", func(t *testing.T) {
		before := v.snapshot(t)
		for _, other := range []identity.Actor{v.otherBrowser.actor, v.adminBrowser.actor} {
			_, e := v.service.GetVariable(ctxFor(t), other, p, target.ID)
			requireCode(t, e, f.NotFound)
			_, e = v.service.ListVariables(ctxFor(t), other, p, f.PageRequest{Limit: 1})
			requireCode(t, e, f.NotFound)
			_, e = v.service.LookupVariableCommand(ctxFor(t), other, q)
			requireCode(t, e, f.NotFound)
			_, e = v.service.CreateVariable(ctxFor(t), other, m, p, input)
			requireCode(t, e, f.NotFound)
		}
		_, e := v.service.GetVariable(ctxFor(t), identity.Actor{}, p, target.ID)
		requireCode(t, e, f.Unauthenticated)
		foreign, _, _ := v.createProject(t, a, "foreign-variable")
		_, e = v.service.GetVariable(ctxFor(t), a, foreign.ID, target.ID)
		requireCode(t, e, f.NotFound)
		_, e = v.service.DeleteVariable(ctxFor(t), a, meta(t, "wrong-parent", &target.Version), foreign.ID, target.ID)
		requireCode(t, e, f.NotFound)
		// A caller-supplied globally occupied ID still follows the target Project boundary.
		before = v.snapshot(t)
		_, e = v.service.CreateVariable(ctxFor(t), a, meta(t, "same-project-occupied-id", nil), p, input)
		requireCode(t, e, f.ResourceBusy)
		foreignMeta := meta(t, "foreign-project-occupied-id", nil)
		_, e = v.service.CreateVariable(ctxFor(t), a, foreignMeta, foreign.ID, input)
		requireCode(t, e, f.NotFound)
		absent, e := v.service.LookupVariableCommand(ctxFor(t), a, queryFor(t, a, foreignMeta, foreign.ID, target.ID, vc.CreateCommand, input))
		if e != nil || absent.Status() != vc.LookupNotObserved || before != v.snapshot(t) {
			t.Fatal("foreign Create disclosed or persisted facts", e)
		}
		// Project setup touches activity, so compare only after it, before the denied calls below.
		_ = before
		before = v.snapshot(t)
		agent, e := identity.NewAgentRun(p, id[identity.Agent](t), id[identity.Execution](t))
		if e != nil {
			t.Fatal(e)
		}
		_, e = v.service.ListVariables(ctxFor(t), agent, p, f.PageRequest{Limit: 1})
		requireCode(t, e, f.DependencyUnbound)
		registration, e := identity.RegisterService(identity.ProjectLifecycle)
		if e != nil {
			t.Fatal(e)
		}
		scope, _ := identity.InProject(p)
		service, e := registration.Actor(id[struct{}](t).String(), scope)
		if e != nil {
			t.Fatal(e)
		}
		_, e = v.service.ListVariables(ctxFor(t), service, p, f.PageRequest{Limit: 1})
		requireCode(t, e, f.Forbidden)
		if before != v.snapshot(t) {
			t.Fatal("denial changed variable facts")
		}
	})
	t.Run("new-session-and-original-version-presence", func(t *testing.T) {
		newer := v.login(t, v.ownerBrowser.email)
		got, e := v.service.CreateVariable(ctxFor(t), newer.actor, m, p, input)
		if e != nil {
			t.Fatal(e)
		}
		sameReceipt(t, original, got)
		changed := input.Fields()
		changed.Value = "different"
		request, e := vc.NewVariableCreate(changed)
		if e != nil {
			t.Fatal(e)
		}
		_, e = v.service.CreateVariable(ctxFor(t), a, m, p, request)
		requireCode(t, e, f.IdempotencyKeyReused)
		value := "new"
		patch := updateInput(t, vc.VariableUpdateFields{Value: &value})
		um := meta(t, "authority-version", &target.Version)
		if _, e = v.service.UpdateVariable(ctxFor(t), a, um, p, target.ID, patch); e != nil {
			t.Fatal(e)
		}
		nextVersion := target.Version + 1
		_, e = v.service.UpdateVariable(ctxFor(t), a, meta(t, string(um.IdempotencyKey), &nextVersion), p, target.ID, patch)
		requireCode(t, e, f.IdempotencyKeyReused)
		_, e = v.service.UpdateVariable(ctxFor(t), a, meta(t, "missing-version", nil), p, target.ID, patch)
		requireCode(t, e, f.InvalidArgument)
	})
	t.Run("current-owner-loss-hides-history", func(t *testing.T) {
		v.transferOwner(t, p, a, v.otherBrowser.actor)
		defer v.transferOwner(t, p, v.otherBrowser.actor, a)
		_, e := v.service.LookupVariableCommand(ctxFor(t), a, q)
		requireCode(t, e, f.NotFound)
		_, e = v.service.CreateVariable(ctxFor(t), a, m, p, input)
		requireCode(t, e, f.NotFound)
	})
	t.Run("archiving-and-archived-read-original-replay", func(t *testing.T) {
		project, _, _ := v.createProject(t, a, "archive-variable")
		r := createInput(t, "Archive", "old")
		cm := meta(t, "archive-original", nil)
		first, e := v.service.CreateVariable(ctxFor(t), a, cm, project.ID, r)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = v.projects.BeginArchive(ctxFor(t), a, meta(t, "archive-project", &project.Version), project.ID); e != nil {
			t.Fatal("real BeginArchive", e)
		}
		for _, state := range []string{"archiving", "archived"} {
			if state == "archived" {
				v.archiveFixture(t, project.ID, a)
			}
			before := v.snapshot(t)
			got, e := v.service.GetVariable(ctxFor(t), a, project.ID, r.Fields().ID)
			if e != nil || got.Fields().Value != "old" {
				t.Fatal("archive read", e)
			}
			if _, e = v.service.ListVariables(ctxFor(t), a, project.ID, f.PageRequest{Limit: 50}); e != nil {
				t.Fatal(e)
			}
			lookup, e := v.service.LookupVariableCommand(ctxFor(t), a, queryFor(t, a, cm, project.ID, r.Fields().ID, vc.CreateCommand, r))
			if e != nil || lookup.Status() != vc.LookupCommitted {
				t.Fatal(e)
			}
			again, e := v.service.CreateVariable(ctxFor(t), a, cm, project.ID, r)
			if e != nil {
				t.Fatal(e)
			}
			sameReceipt(t, first, again)
			_, e = v.service.CreateVariable(ctxFor(t), a, meta(t, "new-"+state, nil), project.ID, createInput(t, "NewVariable", ""))
			requireCode(t, e, f.ProjectNotActive)
			version := got.Fields().Version
			_, e = v.service.DeleteVariable(ctxFor(t), a, meta(t, "delete-"+state, &version), project.ID, got.Fields().ID)
			requireCode(t, e, f.ProjectNotActive)
			if before != v.snapshot(t) {
				t.Fatal("read-only lifecycle changed variable facts")
			}
		}
	})
	t.Run("pending-and-deleting-have-no-history-bypass", func(t *testing.T) {
		v.skills.setMode("pending")
		pending := id[identity.Project](t)
		result, e := v.projects.CreateProject(ctxFor(t), a, meta(t, "pending-project", nil), pc.CreateProjectRequest{ProjectID: pending, Name: "pending-variable"})
		v.skills.setMode("")
		if e != nil || result.State == pc.CreationReady {
			t.Fatal("expected actual pending initialization", e)
		}
		_, e = v.service.ListVariables(ctxFor(t), a, pending, f.PageRequest{Limit: 1})
		requireCode(t, e, f.ProjectNotActive)
		_, e = v.service.CreateVariable(ctxFor(t), a, meta(t, "pending-variable", nil), pending, createInput(t, "Pending", ""))
		requireCode(t, e, f.ProjectNotActive)
		project, _, _ := v.createProject(t, a, "deleting-variable")
		r := createInput(t, "DeleteGate", "")
		cm := meta(t, "delete-gate-original", nil)
		if _, e = v.service.CreateVariable(ctxFor(t), a, cm, project.ID, r); e != nil {
			t.Fatal(e)
		}
		var path string
		v.tx(t, ownerLocks(a, project.ID), func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			route, err := v.accounts.CurrentUserRouteInTx(ctx, tx, a)
			if err != nil {
				return err
			}
			path, err = pc.NormalizeProjectPath(route.Username, project.Name)
			return err
		})
		if _, e = v.projects.BeginDeleteProject(ctxFor(t), a, meta(t, "delete-project", &project.Version), project.ID, pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true}); e != nil {
			t.Fatal("real BeginDeleteProject", e)
		}
		before := v.snapshot(t)
		_, e = v.service.GetVariable(ctxFor(t), a, project.ID, r.Fields().ID)
		requireCode(t, e, f.ProjectNotActive)
		_, e = v.service.LookupVariableCommand(ctxFor(t), a, queryFor(t, a, cm, project.ID, r.Fields().ID, vc.CreateCommand, r))
		requireCode(t, e, f.ProjectNotActive)
		_, e = v.service.CreateVariable(ctxFor(t), a, cm, project.ID, r)
		requireCode(t, e, f.ProjectNotActive)
		if before != v.snapshot(t) {
			t.Fatal("deleting bypassed current gate")
		}
	})
	t.Run("revocation-and-expiry", func(t *testing.T) {
		revoked := v.login(t, v.ownerBrowser.email)
		v.revoke(t, revoked)
		_, e := v.service.LookupVariableCommand(ctxFor(t), revoked.actor, q)
		requireCode(t, e, f.SessionRevoked)
		expired := v.login(t, v.ownerBrowser.email)
		// Explicit expired real Session is a negative input, never a forged positive login.
		u, _ := f.UserLock(expired.actor.Details().UserID)
		v.tx(t, []f.LockRequest{{Key: u, Mode: f.Exclusive}}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			_, e := x.Exec(ctx, `UPDATE agenteam_account.sessions SET absolute_expires_at=clock_timestamp()-interval '1 second',issued_at=clock_timestamp()-interval '1 day' WHERE id=$1`, expired.actor.Details().SessionID)
			return e
		})
		_, e = v.service.GetVariable(ctxFor(t), expired.actor, p, target.ID)
		// Account distinguishes an expired current-session check from explicit revocation.
		requireCode(t, e, f.Unauthenticated)
	})
	t.Run("private-record-corruption-fails-closed", func(t *testing.T) {
		var plan []byte
		if e := v.raw.QueryRow(ctxFor(t), `SELECT plan FROM agenteam_projectvariable.commands WHERE project_id=$1 AND idempotency_key=$2`, p.String(), string(m.IdempotencyKey)).Scan(&plan); e != nil {
			t.Fatal(e)
		}
		if _, e := v.raw.Exec(ctxFor(t), `UPDATE agenteam_projectvariable.commands SET plan=plan||'{"Unknown":true}'::jsonb WHERE project_id=$1 AND idempotency_key=$2`, p.String(), string(m.IdempotencyKey)); e != nil {
			t.Fatal(e)
		}
		_, e := v.service.LookupVariableCommand(ctxFor(t), a, q)
		requireCode(t, e, f.InternalError)
		if !json.Valid(plan) {
			t.Fatal("saved private plan invalid")
		}
		if _, e = v.raw.Exec(ctxFor(t), `UPDATE agenteam_projectvariable.commands SET plan=$3 WHERE project_id=$1 AND idempotency_key=$2`, p.String(), string(m.IdempotencyKey), plan); e != nil {
			t.Fatal(e)
		}
	})
}
