//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func (v *secretOwnerFixture) archiveSecretFixture(t *testing.T, p pc.ProjectID, a i.Actor) {
	t.Helper()
	v.tx(t, ownerLocks(a, p), func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if _, err := v.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, i.Read); err != nil {
			return err
		}
		// Both fields use the same statement-stable DB timestamp. Independent
		// clock_timestamp calls need not follow SQL's written assignment order.
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=statement_timestamp(),updated_at=statement_timestamp() WHERE id=$1`, p.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errors.New("Secret archived fixture lost original Project")
		}
		access, err := v.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, i.Read)
		if err != nil {
			return err
		}
		ref := access.Project()
		if ref.Validate() != nil || ref.Lifecycle != pc.Archived || ref.ArchivedAt == nil || !ref.ArchivedAt.Time().Equal(ref.UpdatedAt.Time()) {
			return errors.New("Secret archived fixture violated actual Project contract")
		}
		return nil
	})
	t.Log("archived row is a disclosed lifecycle gate fixture, with actual same-Tx Project validation; not participant/cleanup acceptance")
}

type secretAfterPrepare struct {
	oc.Appender
	after func() error
	calls int
}

func (a *secretAfterPrepare) PrepareAppend(ctx context.Context, actor i.Actor, e event.Event) (oc.AppendPlan, error) {
	plan, err := a.Appender.PrepareAppend(ctx, actor, e)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	a.calls++
	if err = a.after(); err != nil {
		return oc.AppendPlan{}, err
	}
	return plan, nil
}

func TestSecretVariableOwnerCurrentAuthority(t *testing.T) {
	v := newSecretOwnerFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	input := secretCreateInput(t, "AUTHORITY_SECRET", []byte("authority-private-canary"))
	m := meta(t, "authority-create", nil)
	original, err := v.owner.CreateSecretVariable(ctxFor(t), a, m, p, input)
	if err != nil {
		t.Fatal(err)
	}
	target := input.Fields().ID
	q := secretLookup(t, p, target, vc.SecretCreateCommand, m)
	t.Run("current-owner-and-cross-project", func(t *testing.T) {
		before := v.secretSnapshot(t)
		for _, other := range []i.Actor{v.otherBrowser.actor, v.adminBrowser.actor} {
			_, err := v.owner.GetSecretVariable(ctxFor(t), other, p, target)
			requireCode(t, err, f.NotFound)
			_, err = v.owner.ListSecretVariables(ctxFor(t), other, p, f.PageRequest{Limit: 1})
			requireCode(t, err, f.NotFound)
			_, err = v.owner.LookupSecretVariableCommand(ctxFor(t), other, q)
			requireCode(t, err, f.NotFound)
			_, err = v.owner.CreateSecretVariable(ctxFor(t), other, m, p, input)
			requireCode(t, err, f.NotFound)
		}
		if before != v.secretSnapshot(t) {
			t.Fatal("denied actor changed facts")
		}
		other, _, _ := v.createProject(t, a, "other-secret-project")
		before = v.secretSnapshot(t)
		_, err := v.owner.GetSecretVariable(ctxFor(t), a, other.ID, target)
		requireCode(t, err, f.NotFound)
		_, err = v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "foreign-id", nil), other.ID, input)
		requireCode(t, err, f.NotFound)
		_, err = v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "same-id", nil), p, input)
		requireCode(t, err, f.ResourceBusy)
		if before != v.secretSnapshot(t) {
			t.Fatal("occupied ID changed facts")
		}
		agent, err := i.NewAgentRun(p, id[i.Agent](t), id[i.Execution](t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = v.owner.ListSecretVariables(ctxFor(t), agent, p, f.PageRequest{Limit: 1})
		requireCode(t, err, f.DependencyUnbound)
		_, err = v.owner.GetSecretVariable(ctxFor(t), i.Actor{}, p, target)
		requireCode(t, err, f.Unauthenticated)
	})
	t.Run("current-owner-loss-hides-original-history", func(t *testing.T) {
		v.transferOwner(t, p, a, v.otherBrowser.actor)
		defer v.transferOwner(t, p, v.otherBrowser.actor, a)
		before := v.secretSnapshot(t)
		_, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, q)
		requireCode(t, err, f.NotFound)
		_, err = v.owner.CreateSecretVariable(ctxFor(t), a, m, p, input)
		requireCode(t, err, f.NotFound)
		if before != v.secretSnapshot(t) {
			t.Fatal("old Owner history bypass")
		}
	})
	t.Run("real-archive-after-prepare-rechecks-final-gate", func(t *testing.T) {
		project, _, _ := v.createProject(t, a, "secret-prepare-archive")
		in := secretCreateInput(t, "ARCHIVE_SECRET", []byte("archive-private-canary"))
		cm := meta(t, "archive-before", nil)
		first, err := v.owner.CreateSecretVariable(ctxFor(t), a, cm, project.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		wrapped := &secretAfterPrepare{Appender: v.deps.Events, after: func() error {
			_, err := v.projects.BeginArchive(ctxFor(t), a, meta(t, "archive-project", &project.Version), project.ID)
			return err
		}}
		deps := v.deps
		v.deps.Events = wrapped
		pendingOwner := v.newOwner(t)
		v.deps = deps
		pending := secretCreateInput(t, "MUST_NOT_PUBLISH", []byte("pending-private-canary"))
		pendingMeta := meta(t, "prepared-then-archive", nil)
		_, err = pendingOwner.CreateSecretVariable(ctxFor(t), a, pendingMeta, project.ID, pending)
		requireCode(t, err, f.ProjectNotActive)
		if wrapped.calls != 1 {
			t.Fatal("archive did not occur after actual Outbox preparation")
		}
		absent, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, secretLookup(t, project.ID, pending.Fields().ID, vc.SecretCreateCommand, pendingMeta))
		if err != nil || absent.Status() != vc.SecretLookupNotObserved {
			t.Fatal("rejected prepared write published", err)
		}
		for _, state := range []string{"archiving", "archived"} {
			if state == "archived" {
				v.archiveSecretFixture(t, project.ID, a)
			}
			before := v.secretSnapshot(t)
			replay, err := v.owner.CreateSecretVariable(ctxFor(t), a, cm, project.ID, in)
			if err != nil {
				t.Fatal("historical original material on read-only Project", state, err)
			}
			sameSecretReceipt(t, first, replay)
			_, err = v.owner.GetSecretVariable(ctxFor(t), a, project.ID, in.Fields().ID)
			if err != nil {
				t.Fatal(err)
			}
			_, err = v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "new-"+state, nil), project.ID, secretCreateInput(t, "NEW_SECRET", []byte("new-private")))
			requireCode(t, err, f.ProjectNotActive)
			if before != v.secretSnapshot(t) {
				t.Fatal("read-only Project changed Secret facts")
			}
		}
	})
	t.Run("revoked-current-session-before-safe-history", func(t *testing.T) {
		fresh := v.login(t, v.ownerBrowser.email)
		v.revoke(t, fresh)
		before := v.secretSnapshot(t)
		_, err := v.owner.LookupSecretVariableCommand(ctxFor(t), fresh.actor, q)
		requireCode(t, err, f.SessionRevoked)
		_, err = v.owner.CreateSecretVariable(ctxFor(t), fresh.actor, m, p, input)
		requireCode(t, err, f.SessionRevoked)
		if before != v.secretSnapshot(t) {
			t.Fatal("revoked Session changed facts")
		}
		got, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, q)
		if err != nil || got.Receipt() == nil {
			t.Fatal("unrevoked Session lost history", err)
		}
		sameSecretReceipt(t, original, *got.Receipt())
	})
}

// Snapshot intentionally excludes the global nonce reservation counter: legal
// preparation may consume a range before a rejected write. It includes actual
// protected payloads and both receipt owners, so partial effects cannot hide.
func (v *secretOwnerFixture) secretSnapshot(t *testing.T) string {
	t.Helper()
	var raw string
	err := v.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'variables',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_projectvariable.variables x),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_projectvariable.secret_commands x),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_projectvariable.secret_history x),
 'generations',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY project_id),'[]'::jsonb) FROM agenteam_projectvariable.secret_project_generations x),
 'credentials',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_secret.secrets x WHERE scope='project'),
 'payloads',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY payload_id),'[]'::jsonb) FROM agenteam_secret.secret_payloads x WHERE scope='project'),
 'protected_receipts',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_secret.project_variable_receipts x),
 'audit',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_audit.audit_records x WHERE producer IN ('projectvariable','secret') AND scope='project'),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events x WHERE event_type='project.secret_variable_changed'))::text`).Scan(&raw)
	if err != nil {
		t.Fatal("Secret atomic snapshot", err)
	}
	return raw
}
