//go:build integration

package model_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Canonical lifecycle/Owner test input is changed only under the real Project
// EX lock; this does not claim to implement lifecycle or ownership transfer.
func projectCredentialCanonical(t *testing.T, v *projectCredentialFixture, query string, args ...any) {
	t.Helper()
	key, _ := f.ProjectLock(v.project.ID.String())
	r := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if e := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
			return e
		}
		x, e := v.raw.InTx(tx)
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, query, args...)
		return e
	})
	if r.State() != f.Committed {
		t.Fatal("canonical test input failed", r.Fault())
	}
}
func TestModelProjectCredentialHTTPCurrentAuthorityAndFacts(t *testing.T) {
	projectCredentialTop(t)
	v := newProjectCredentialFixture(t)
	created := v.create(t, "authority-create", "authority-private")
	path := v.collection() + "/" + created
	for _, b := range []systemHTTPBrowser{v.adminBrowser, v.otherBrowser} {
		v.request(t, b, "GET", path, "", nil).problem(t, 404, f.NotFound)
		v.request(t, b, "POST", v.lookupPath(), "authority-create", projectCredentialLookupBody(t, sc.Create, "", 0)).problem(t, 404, f.NotFound)
		v.request(t, b, "POST", v.collection(), "outsider-create", projectUpdateJSON(t, map[string]any{"value": "not-authorized"})).problem(t, 404, f.NotFound)
	}
	renewed := v.login(t, v.ownerBrowser.email)
	v.request(t, renewed, "POST", v.lookupPath(), "authority-create", projectCredentialLookupBody(t, sc.Create, "", 0)).want(t, 200)
	if e := v.core.Logout(testContext(t), account.LogoutRequest{Actor: renewed.actor, Key: "credential-logout"}); e != nil {
		t.Fatal(e)
	}
	v.request(t, renewed, "POST", v.lookupPath(), "authority-create", projectCredentialLookupBody(t, sc.Create, "", 0)).problem(t, 401, f.SessionRevoked)
	v.request(t, renewed, "POST", v.collection(), "authority-create", projectUpdateJSON(t, map[string]any{"value": "authority-private"})).problem(t, 401, f.SessionRevoked)
	validSecond := v.login(t, v.ownerBrowser.email)
	for _, mode := range []string{"cause", "changed_fields", "version", "session"} {
		t.Run("real_validator_"+mode, func(t *testing.T) {
			before := v.facts(t)
			calls, rejected := v.tap.calls.Load(), v.tap.rejected.Load()
			v.tap.before = func(_ context.Context, _ f.Tx, e ac.Entry, k ac.AppendKey) (ac.Entry, ac.AppendKey) {
				fields := e.Fields()
				var err error
				switch mode {
				case "cause":
					k, err = ac.NewAppendKey(ac.SecretProducer, newID[struct{}](t).String(), 0)
				case "changed_fields":
					fields.Metadata, err = ac.SecretMutationMetadata(ac.SecretUpdate, 2, []ac.ChangedField{ac.PurposeChanged})
				case "version":
					fields.Metadata, err = ac.SecretMutationMetadata(ac.SecretUpdate, 99, []ac.ChangedField{ac.ValueChanged})
				case "session":
					fields.Actor = validSecond.actor
				}
				if err != nil {
					t.Fatal("malformed corruption fixture")
				}
				e, err = ac.NewEntry(fields)
				if err != nil {
					t.Fatal("tamper rejected before real validator")
				}
				return e, k
			}
			defer func() { v.tap.before = nil }()
			v.request(t, v.ownerBrowser, "PUT", path, "rejected-"+mode, projectUpdateJSON(t, map[string]any{"expected_version": "1", "value": "must-rollback"})).problem(t, 503, f.DependencyUnavailable)
			if v.tap.calls.Load() != calls+1 || v.tap.rejected.Load() != rejected+1 {
				t.Fatal("real Audit validator not reached/rejected")
			}
			v.unchanged(t, before)
			if v.request(t, v.ownerBrowser, "GET", path, "", nil).want(t, 200).object(t)["version"] != "1" {
				t.Fatal("rejected material committed")
			}
		})
	}
	rotate := projectUpdateJSON(t, map[string]any{"expected_version": "1", "value": "authority-rotation"})
	v.request(t, v.ownerBrowser, "PUT", path, "authority-update", rotate).want(t, 200)
	remove := projectUpdateJSON(t, map[string]any{"expected_version": "2"})
	v.request(t, v.ownerBrowser, "DELETE", path, "authority-delete", remove).want(t, 200)
	projectCredentialCanonical(t, v, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.project.ID.String())
	before := v.facts(t)
	for _, tc := range []struct {
		key, method string
		kind        sc.MutationKind
		version     f.Version
		body        []byte
	}{{"authority-create", "POST", sc.Create, 0, projectUpdateJSON(t, map[string]any{"value": "authority-private"})}, {"authority-update", "PUT", sc.Update, 1, rotate}, {"authority-delete", "DELETE", sc.Delete, 2, remove}} {
		response := v.request(t, v.ownerBrowser, "POST", v.lookupPath(), tc.key, projectCredentialLookupBody(t, tc.kind, created, tc.version)).want(t, 200)
		if response.object(t)["observed"] != true {
			t.Fatal("archived Read did not see committed history")
		}
		target := path
		if tc.kind == sc.Create {
			target = v.collection()
		}
		v.request(t, v.ownerBrowser, tc.method, target, tc.key, tc.body).problem(t, 409, f.ProjectNotActive)
	}
	v.unchanged(t, before)
	// The archived Read gate still checks current ownership before old receipts.
	projectCredentialCanonical(t, v, `UPDATE agenteam_project.projects SET owner_user_id=$2,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.project.ID.String(), v.otherBrowser.actor.Details().UserID)
	v.request(t, v.ownerBrowser, "POST", v.lookupPath(), "authority-create", projectCredentialLookupBody(t, sc.Create, "", 0)).problem(t, 404, f.NotFound)
	projectCredentialCanonical(t, v, `UPDATE agenteam_project.projects SET owner_user_id=$2,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, v.project.ID.String(), v.ownerBrowser.actor.Details().UserID)
	t.Log("formal identity chain; current Session/Owner before receipt; real Audit cause/fields/version/session rejection rolled back canonical+receipt+Audit; archived Read and Mutate replay remain different")
}
func projectCredentialLookupCause(c f.TransactionCause) bool {
	return c.Kind() == f.RecoveryCause && c.Details().Owner == "secret-write-lookup"
}
func TestModelProjectCredentialHTTPPassiveLookup(t *testing.T) {
	projectCredentialTop(t)
	v := newProjectCredentialFixture(t)
	target := v.create(t, "lookup-create", "lookup-private")
	present := v.lookup(t, v.ownerBrowser, "lookup-create", sc.Create, "", 0)
	absent := v.lookup(t, v.ownerBrowser, "lookup-absent", sc.Create, "", 0)
	stable := v.facts(t)
	for _, r := range []sc.WriteCommandLookupRequest{present, absent} {
		t.Run("terminal_"+string(r.Identity.Key()), func(t *testing.T) {
			command, _ := f.CommandLock(r.Identity)
			user, _ := f.UserLock(r.Actor.Details().UserID)
			project, _ := f.ProjectLock(v.project.ID.String())
			required := []f.LockRequest{{Key: command, Mode: f.Shared}, {Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}}
			var hits atomic.Int32
			v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
				if projectCredentialLookupCause(c) {
					hits.Add(1)
					return v.raw.RequireHeldLocks(ctx, tx, required)
				}
				return nil
			}, nil)
			observed, e := v.writer.LookupWriteCommand(testContext(t), r)
			if e != nil || hits.Load() != 1 || observed.Observed != (r.Identity.Key() == "lookup-create") {
				t.Fatal("same Tx three locks or safe observation", e, hits.Load())
			}
			v.tracked.hooks(nil, nil)
			// These terminal decorators are publication probes after a real read; the
			// physical ACK-loss cases have their own protocol test and are not faked here.
			for _, mode := range []string{"unknown", "notcommitted", "cancel_after"} {
				ctx, cancel := context.WithCancel(testContext(t))
				var actual atomic.Bool
				v.tracked.hooks(nil, func(c f.TransactionCause, out f.CommitResult) f.CommitResult {
					if !projectCredentialLookupCause(c) {
						return out
					}
					actual.Store(out.State() == f.Committed)
					switch mode {
					case "unknown":
						return f.UnknownResult(newID[f.TransactionAttempt](t), c)
					case "notcommitted":
						return f.NotCommittedResult(f.NewFault(f.ResourceBusy, f.NotCommitted))
					default:
						cancel()
						return out
					}
				})
				got, e := v.writer.LookupWriteCommand(ctx, r)
				cancel()
				v.tracked.hooks(nil, nil)
				if !actual.Load() || e == nil || got.Observed || got.Result != nil {
					t.Fatal("unconfirmed observation escaped")
				}
			}
			release := managementHold(t, &systemHTTPFixture{fixture: v.fixture}, command, f.Exclusive)
			ctx, cancel := context.WithTimeout(testContext(t), 150*time.Millisecond)
			got, e := v.writer.LookupWriteCommand(ctx, r)
			cancel()
			release()
			if e == nil || got.Observed || got.Result != nil {
				t.Fatal("held Command EX became false absence")
			}
		})
	}
	// Safe lookup identity cannot substitute another expected version/target.
	wrong := v.lookup(t, v.ownerBrowser, "lookup-create", sc.Create, "", 0)
	wrong.Kind = sc.Update
	wrong.Ref = credentialRefProject(t, v, target)
	wrong.ExpectedVersion = 1
	if wrong.Validate() == nil {
		t.Fatal("kind and command identity mismatch accepted")
	}
	v.unchanged(t, stable)
	for _, secret := range []string{"lookup-private", v.ownerBrowser.cookie, v.ownerBrowser.csrf} {
		if strings.Contains(v.logs.text(), secret) {
			t.Fatal("lookup leaked protected input")
		}
	}
}
func credentialRefProject(t *testing.T, v *projectCredentialFixture, target string) sc.CredentialRef {
	t.Helper()
	key, e := f.ParseID[sc.Credential](target)
	if e != nil {
		t.Fatal(e)
	}
	ref, e := sc.NewCredentialRef(key, v.scope)
	if e != nil {
		t.Fatal(e)
	}
	return ref
}
