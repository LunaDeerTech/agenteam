//go:build integration

package project_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestProjectSecretAuditBindingConstruction(t *testing.T) {
	f := newBindingFixture(t)
	t.Run("opaque-checker-store-is-validated-at-use", func(t *testing.T) {
		other := openStore(t, f.db.Config(t, nil))
		// The opaque interface cannot be compared in Project's constructor.
		// Real Secret's exact Service/checker provenance must still reject it.
		b := f.bindStore(t, f.raw, other)
		_, err := b.secret.ExecuteWrite(ctxFor(t), f.request(t, f.owner, sc.Create, "wrong-checker-store"))
		requireCode(t, b.tap.err, foundation.Forbidden)
		requireCode(t, err, foundation.DependencyUnavailable)
		f.unchanged(t, bindingCounts{})
	})
	t.Run("missing-provider-keeps-owner-but-rejects-secret", func(t *testing.T) {
		a, err := project.NewAuthority(f.raw, project.AuthorityDependencies{Sessions: f.accounts})
		if err != nil {
			t.Fatal(err)
		}
		delegate, err := project.NewSecretAuthority(a)
		if err != nil {
			t.Fatal(err)
		}
		grant, err := delegate.AuthorizeProject(ctxFor(t), foundation.Tx{}, f.owner, f.project.ID, identity.Mutate)
		if err != nil || !grant.Matches(f.owner, f.scope, identity.Mutate) {
			t.Fatal("legacy Owner changed", err)
		}
		req := f.request(t, f.owner, sc.Create, "missing-provider")
		resource, _ := ac.NewResource(ac.SecretResource, id[sc.Credential](t).String())
		metadata, _ := ac.SecretMutationMetadata(ac.SecretCreate, 1, []ac.ChangedField{ac.ValueChanged, ac.PurposeChanged})
		entry, _ := ac.NewEntry(ac.EntryFields{Scope: f.scope, Actor: f.owner, Action: ac.SecretCreate, Outcome: ac.Success, Resource: resource, Metadata: metadata})
		key, _ := audit.CommandAppendKey(ac.SecretProducer, req.Identity, 0)
		r := f.raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.raw.AcquireAll(ctx, tx, bindingLocks(f.owner, f.project.ID)); err != nil {
				return err
			}
			return a.CheckAppendInTx(ctx, tx, entry, key)
		})
		bindingDenied(t, r, foundation.DependencyUnbound)
		f.unchanged(t, bindingCounts{})
	})
	t.Run("same-store-official-chain-and-retired-tx", func(t *testing.T) {
		observed := &bindingObservedStore{Store: f.raw}
		a, err := project.NewAuthority(observed, project.AuthorityDependencies{Sessions: f.accounts, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.SecretProducer: f.bundle.checker}})
		if err != nil {
			t.Fatal(err)
		}
		delegate, err := project.NewSecretAuthority(a)
		if err != nil {
			t.Fatal(err)
		}
		f.bundle.tap.before = func(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey) {
			refID, err := foundation.ParseID[sc.Credential](entry.Fields().Resource.Details().ID)
			if err != nil {
				t.Fatal(err)
			}
			ref, _ := sc.NewCredentialRef(refID, f.scope)
			if err = delegate.CheckMutationInTx(ctx, tx, f.owner, ref); err != nil {
				t.Fatal(err)
			}
			if err = a.CheckAppendInTx(ctx, tx, entry, key); err != nil {
				t.Fatal(err)
			}
			if observed.acquires != 0 || observed.transactions != 0 {
				t.Fatal("authority acquired or nested a transaction")
			}
			return ctx, entry, key
		}
		f.credential(t)
		capture := f.bundle.tap
		requireCode(t, f.bundle.projects.CheckAppendInTx(ctxFor(t), capture.tx, capture.entry, capture.key), foundation.DependencyUnavailable)
		f.unchanged(t, bindingCounts{1, 1, 1})
	})
}

func TestProjectSecretAuditBindingCRUD(t *testing.T) {
	f := newBindingFixture(t)
	request := f.request(t, f.owner, sc.Create, "initial-secret")
	created, err := f.bundle.secret.ExecuteWrite(ctxFor(t), request)
	if err != nil || created.Metadata.Version != 1 {
		t.Fatal("create", err)
	}
	f.unchanged(t, bindingCounts{1, 1, 1})
	key, _ := audit.CommandAppendKey(ac.SecretProducer, request.Identity, 0)
	var matches bool
	err = f.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets s JOIN agenteam_secret.secret_command_receipts r ON r.credential_id=s.id JOIN agenteam_audit.audit_records a ON a.resource_id=s.id AND a.cause_ref=r.command_digest WHERE s.id=$1 AND s.project_id=$2 AND s.version=1 AND r.result_version=1 AND r.purpose=s.purpose AND a.producer='secret' AND a.action='secret.create' AND a.user_id=$3 AND a.session_id=$4 AND a.cause_ref=$5 AND a.ordinal=0 AND a.metadata->>'version'='1')`, created.Metadata.CredentialRef.Details().ID.String(), f.project.ID.String(), f.owner.Details().UserID, f.owner.Details().SessionID, key.Details().CauseRef).Scan(&matches)
	if err != nil || !matches {
		t.Fatal("canonical/receipt/Audit identity mismatch", err)
	}
	calls := f.bundle.tap.calls
	request.Actor = f.renewedSession(t)
	replay, err := f.bundle.secret.ExecuteWrite(ctxFor(t), request)
	if err != nil || !replay.Metadata.CredentialRef.Equal(created.Metadata.CredentialRef) || f.bundle.tap.calls != calls {
		t.Fatal("current new Session replay changed original result or appended", err)
	}
	current := created
	for _, purpose := range []sc.Purpose{sc.Model, sc.MCP} {
		r := f.request(t, f.owner, sc.Update, "updated-secret")
		r.Ref, r.ExpectedVersion, r.Purpose = current.Metadata.CredentialRef, current.Metadata.Version, purpose
		current, err = f.bundle.secret.ExecuteWrite(ctxFor(t), r)
		if err != nil {
			t.Fatal("update", err)
		}
		calls = f.bundle.tap.calls
		got, err := f.bundle.secret.ExecuteWrite(ctxFor(t), r)
		if err != nil || got.Metadata.Version != current.Metadata.Version || f.bundle.tap.calls != calls {
			t.Fatal("update replay appended", err)
		}
	}
	var unchanged, changed string
	if err = f.raw.QueryRow(ctxFor(t), `SELECT metadata->'changed_fields' FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='secret.update' AND metadata->>'version'='2'`, f.project.ID.String()).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if err = f.raw.QueryRow(ctxFor(t), `SELECT metadata->'changed_fields' FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='secret.update' AND metadata->>'version'='3'`, f.project.ID.String()).Scan(&changed); err != nil {
		t.Fatal(err)
	}
	if unchanged != `["value"]` || changed != `["purpose", "value"]` {
		t.Fatal("changed_fields did not reflect actual prior purpose", unchanged, changed)
	}
	r := f.request(t, f.owner, sc.Delete, "")
	r.Ref, r.ExpectedVersion, r.Purpose = current.Metadata.CredentialRef, current.Metadata.Version, current.Metadata.Purpose
	deleted, err := f.bundle.secret.ExecuteWrite(ctxFor(t), r)
	if err != nil || !deleted.Deleted || deleted.Metadata.Version != 4 {
		t.Fatal("delete post-state", err)
	}
	if _, err = f.bundle.secret.ExecuteWrite(ctxFor(t), r); err != nil {
		t.Fatal("deleted receipt replay", err)
	}
	f.unchanged(t, bindingCounts{0, 4, 4})
	admin := f.human(t, "binding-system-admin", "admin")
	system := f.request(t, admin, sc.Create, "system-compatible")
	system.Scope = identity.SystemScope()
	system.Identity, err = foundation.NewCommandIdentity("secret", []string{admin.Details().UserID}, "create", foundation.IdempotencyKey(id[struct{}](t).String()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.bundle.secret.ExecuteWrite(ctxFor(t), system); err != nil {
		t.Fatal("System/Account authority compatibility", err)
	}
	f.unchanged(t, bindingCounts{0, 4, 4})
}

func TestProjectSecretAuditBindingCurrentAuthority(t *testing.T) {
	for _, mode := range []string{"revoked-session", "foreign-owner", "admin", "owner-changed", "archiving-after-prepare", "archived-after-prepare", "deleting-after-prepare", "uninitialized", "missing-lock", "wrong-project-lock", "foreign-tx"} {
		t.Run(mode, func(t *testing.T) {
			f := newBindingFixture(t)
			actor := f.owner
			if mode == "foreign-owner" || mode == "admin" {
				role := "user"
				if mode == "admin" {
					role = "admin"
				}
				actor = f.human(t, "binding-outsider", role)
			}
			if mode == "uninitialized" {
				f.skills.setMode("pending")
				request := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "PendingSecret"}
				result, err := f.service.CreateProject(ctxFor(t), f.owner, meta(t, id[struct{}](t).String(), nil), request)
				if err != nil || result.Operation == nil {
					t.Fatal("real pending creation", err)
				}
				f.project.ID = request.ProjectID
				f.scope, _ = identity.InProject(request.ProjectID)
			}
			request := f.request(t, actor, sc.Create, "must-not-persist")
			prepared, err := f.bundle.secret.PrepareWrite(ctxFor(t), request)
			if err != nil {
				t.Fatal(err)
			}
			code, final := foundation.ProjectNotActive, foundation.ProjectNotActive
			switch mode {
			case "revoked-session":
				f.sql(t, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, f.owner.Details().SessionID)
				code, final = foundation.SessionRevoked, foundation.SessionRevoked
			case "owner-changed":
				other := f.human(t, "binding-new-owner", "user")
				f.sql(t, `UPDATE agenteam_project.projects SET owner_user_id=$2 WHERE id=$1`, f.project.ID.String(), other.Details().UserID)
				code, final = foundation.NotFound, foundation.NotFound
			case "foreign-owner", "admin":
				code, final = foundation.NotFound, foundation.NotFound
			case "archiving-after-prepare":
				f.gate(t, c.Archiving)
			case "archived-after-prepare":
				f.gate(t, c.Archived)
			case "deleting-after-prepare":
				f.gate(t, c.Deleting)
			case "missing-lock", "wrong-project-lock", "foreign-tx":
				code, final = foundation.DependencyUnavailable, foundation.InternalError
			}
			store := f.raw
			if mode == "foreign-tx" {
				store = openStore(t, f.db.Config(t, nil))
			}
			locks := prepared.RequiredLocks()
			if mode == "missing-lock" {
				locks = nil
			}
			if mode == "wrong-project-lock" {
				old := r3Lock(f.project.ID, foundation.Shared)
				for i := range locks {
					if locks[i].Key.Canonical() == old.Key.Canonical() {
						locks[i] = r3Lock(id[identity.Project](t), foundation.Shared)
					}
				}
			}
			r := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := store.AcquireAll(ctx, tx, locks); err != nil {
					return err
				}
				_, err := f.bundle.secret.ApplyPreparedWriteInTx(ctx, tx, prepared)
				requireCode(t, err, code)
				return err
			})
			bindingDenied(t, r, final)
			f.unchanged(t, bindingCounts{})
		})
	}
	t.Run("replay-rechecks-current-gate", func(t *testing.T) {
		f := newBindingFixture(t)
		r := f.request(t, f.owner, sc.Create, "replay-before-archive")
		if _, err := f.bundle.secret.ExecuteWrite(ctxFor(t), r); err != nil {
			t.Fatal(err)
		}
		prepared, err := f.bundle.secret.PrepareWrite(ctxFor(t), r)
		if err != nil {
			t.Fatal(err)
		}
		f.gate(t, c.Archived)
		lookup, err := f.bundle.secret.LookupWrite(ctxFor(t), prepared)
		if err != nil || !lookup.Observed {
			t.Fatal("archived Owner receipt Read was denied", err)
		}
		_, err = f.bundle.secret.ExecuteWrite(ctxFor(t), r)
		requireCode(t, err, foundation.ProjectNotActive)
		f.unchanged(t, bindingCounts{1, 1, 1})
	})
}

func TestProjectSecretAuditBindingFactRejection(t *testing.T) {
	f := newBindingFixture(t)
	created := f.credential(t)
	originalCtx, originalTx, originalEntry, originalKey := f.bundle.tap.ctx, f.bundle.tap.tx, f.bundle.tap.entry, f.bundle.tap.key
	renewed := f.renewedSession(t)
	other, _, _ := f.create(t, f.owner, "SecretOther")
	otherScope, _ := identity.InProject(other.ID)
	for _, mode := range []string{"no-witness", "resource", "session", "metadata", "action", "cause", "ordinal", "producer", "association", "scope", "outcome"} {
		t.Run(mode, func(t *testing.T) {
			code, final := foundation.Forbidden, foundation.DependencyUnavailable
			if mode == "producer" {
				code = foundation.InvalidArgument
			}
			if mode == "scope" {
				code, final = foundation.DependencyUnavailable, foundation.InternalError
			}
			f.bundle.tap.before = func(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey) {
				fields := entry.Fields()
				switch mode {
				case "no-witness":
					ctx = context.Background()
				case "resource":
					fields.Resource, _ = ac.NewResource(ac.SecretResource, id[struct{}](t).String())
				case "session":
					fields.Actor = renewed
				case "metadata":
					fields.Metadata, _ = ac.SecretMutationMetadata(ac.SecretUpdate, 99, []ac.ChangedField{ac.ValueChanged})
				case "action":
					fields.Action = ac.SecretCreate
					fields.Metadata, _ = ac.SecretMutationMetadata(ac.SecretCreate, 2, []ac.ChangedField{ac.ValueChanged, ac.PurposeChanged})
				case "cause":
					key, _ = ac.NewAppendKey(ac.SecretProducer, id[struct{}](t).String(), 0)
				case "ordinal":
					key, _ = ac.NewAppendKey(ac.SecretProducer, key.Details().CauseRef, 1)
				case "producer":
					key, _ = ac.NewAppendKey(ac.ObjectProducer, key.Details().CauseRef, 0)
				case "association":
					fields.Associations.HTTPTraceID = id[struct{}](t).String()
				case "scope":
					fields.Scope = otherScope
				case "outcome":
					fields.Outcome = ac.Denied
				}
				changed, err := ac.NewEntry(fields)
				if err != nil {
					t.Fatal("invalid tamper fixture", err)
				}
				return ctx, changed, key
			}
			r := f.request(t, f.owner, sc.Update, "must-roll-back")
			r.Ref, r.ExpectedVersion = created.Metadata.CredentialRef, 1
			_, err := f.bundle.secret.ExecuteWrite(ctxFor(t), r)
			requireCode(t, f.bundle.tap.err, code)
			requireCode(t, err, final)
			f.unchanged(t, bindingCounts{1, 1, 1})
			metadata, err := f.bundle.secret.Metadata(ctxFor(t), f.owner, created.Metadata.CredentialRef)
			if err != nil || metadata.Version != 1 || metadata.Purpose != sc.Model {
				t.Fatal("rejected canonical write escaped", err)
			}
		})
	}
	f.bundle.tap.before = nil
	for _, mode := range []string{"direct-append", "captured-new-tx", "captured-other-store"} {
		t.Run(mode, func(t *testing.T) {
			store, authority := f.raw, f.bundle.projects
			if mode == "captured-other-store" {
				store = openStore(t, f.db.Config(t, nil))
				authority = f.bindStore(t, store, store).projects
			}
			r := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if tx == originalTx {
					t.Fatal("fresh Tx reused retired capability")
				}
				if err := store.AcquireAll(ctx, tx, bindingLocks(f.owner, f.project.ID)); err != nil {
					return err
				}
				if mode == "direct-append" {
					_, err := f.bundle.audit.AppendInTx(ctx, tx, originalEntry, originalKey)
					return err
				}
				// Store cancels its callback context when the old operation is
				// released. Keep the captured values but remove that cancellation
				// to isolate witness identity from the expired-context rejection.
				err := authority.CheckAppendInTx(context.WithoutCancel(originalCtx), tx, originalEntry, originalKey)
				requireCode(t, err, foundation.Forbidden)
				return err
			})
			bindingDenied(t, r, foundation.Forbidden)
			f.unchanged(t, bindingCounts{1, 1, 1})
		})
	}
}

func TestProjectSecretAuditBindingUnknown(t *testing.T) {
	for _, mode := range []string{"committed", "rollback", "pending"} {
		t.Run(mode, func(t *testing.T) {
			f := newBindingFixture(t)
			trace := newBindingPGTrace(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port))
			var proxy *commitProxy
			address, reached := trace.listener.Addr(), (<-chan struct{})(trace.ackDropped)
			if mode != "committed" {
				proxy = newCommitProxy(t, trace.listener.Addr().String(), mode != "rollback")
				address, reached = proxy.listener.Addr(), proxy.reached
			}
			store := f.proxyStore(t, address)
			b := f.bindStore(t, store, store)
			request := f.request(t, f.owner, sc.Create, "unknown-final-command")
			prepared, err := b.secret.PrepareWrite(ctxFor(t), request)
			if err != nil {
				t.Fatal(err)
			}
			key, _ := audit.CommandAppendKey(ac.SecretProducer, request.Identity, 0)
			commandCause, _ := foundation.NewCommandsCause(request.Identity)
			done := make(chan foundation.CommitResult, 1)
			ctx := ctxFor(t)
			go func() {
				done <- store.WithinTx(ctx, commandCause, func(ctx context.Context, tx foundation.Tx) error {
					if err := store.AcquireAll(ctx, tx, prepared.RequiredLocks()); err != nil {
						return err
					}
					if _, err := b.secret.ApplyPreparedWriteInTx(ctx, tx, prepared); err != nil {
						return err
					}
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					var exact bool
					err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_command_receipts r JOIN agenteam_secret.secrets s ON s.id=r.credential_id JOIN agenteam_audit.audit_records a ON a.cause_ref=r.command_digest AND a.resource_id=s.id WHERE r.project_id=$1 AND r.command_digest=$2 AND a.producer='secret' AND a.action='secret.create' AND a.ordinal=0 AND a.project_id=r.project_id)`, f.project.ID.String(), key.Details().CauseRef).Scan(&exact)
					if err != nil {
						return err
					}
					if !exact {
						return foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
					}
					// Arm only the final original command COMMIT after the real
					// canonical, receipt, checker and Audit ran in this very Tx.
					if proxy == nil {
						trace.dropCommitACK.Store(true)
					} else {
						proxy.armed.Store(true)
					}
					return nil
				})
			}()
			await(t, reached)
			r := <-done
			if r.State() != foundation.Unknown {
				t.Fatal("lost final command ACK was not Unknown", r.State(), r.Fault())
			}
			if mode == "committed" {
				f.unchanged(t, bindingCounts{1, 1, 1})
			} else {
				f.unchanged(t, bindingCounts{})
			}
			if mode == "pending" {
				probe, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				locked := f.raw.WithinTx(probe, cause(t), func(ctx context.Context, tx foundation.Tx) error {
					return f.raw.AcquireAll(ctx, tx, prepared.RequiredLocks())
				})
				cancel()
				if locked.State() == foundation.Committed {
					t.Fatal("Unknown original writer no longer held its actual locks")
				}
				// A missing row while this writer is live is not a rollback proof.
				lookup, err := b.secret.LookupWrite(ctx, prepared)
				if err != nil || lookup.Observed {
					t.Fatal("pending receipt observation", err)
				}
			}
			if proxy != nil {
				close(proxy.release)
				select {
				case <-proxy.completed:
				case <-time.After(5 * time.Second):
					t.Fatalf("original COMMIT completion missing: protocol=%v durable=%v", trace.snapshot(), f.counts(t))
				}
			}
			t.Logf("final command protocol: %v", trace.snapshot())
			joined := f.raw.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
				return f.raw.AcquireAll(ctx, tx, prepared.RequiredLocks())
			})
			if joined.State() != foundation.Committed {
				t.Fatal("original writer did not actually terminate", joined.Fault())
			}
			lookup, err := b.secret.LookupWrite(ctx, prepared)
			if err != nil || lookup.Observed != (mode != "rollback") {
				t.Fatal("terminal command lookup", err)
			}
			result, err := b.secret.ExecuteWrite(ctx, request)
			if err != nil {
				t.Fatal("original command recovery", err)
			}
			if lookup.Observed && !lookup.Result.Metadata.CredentialRef.Equal(result.Metadata.CredentialRef) {
				t.Fatal("recovery replaced original credential identity")
			}
			f.unchanged(t, bindingCounts{1, 1, 1})
		})
	}
}

func TestProjectSecretAuditBindingResolveGate(t *testing.T) {
	t.Run("strict-usage-service-resolve", func(t *testing.T) {
		f := newBindingFixture(t)
		created := f.credential(t)
		lease, actor, owner := f.lease(t, created.Metadata.CredentialRef)
		request := f.modelReadRequest(t, actor, lease, owner)
		material, err := f.bundle.secret.ReadCredentialForUsage(ctxFor(t), request)
		if err != nil {
			t.Fatal("strict Usage + real Project/Secret/Audit resolve", err)
		}
		defer material.Destroy()
		if err = material.Use(func(raw []byte) error {
			if string(raw) != "binding-secret" {
				t.Fatal("wrong authenticated material")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		f.unchanged(t, bindingCounts{1, 1, 2})
	})
	for _, mode := range []string{"usage-revoked", "lease-released", "archiving", "archived", "deleting", "wrong-role", "wrong-cause", "human-no-user-plan", "agent-run", "agent-subject", "direct-service"} {
		t.Run(mode, func(t *testing.T) {
			f := newBindingFixture(t)
			created := f.credential(t)
			lease, actor, owner := f.lease(t, created.Metadata.CredentialRef)
			request := f.modelReadRequest(t, actor, lease, owner)
			code := foundation.Forbidden
			var auditCode foundation.Code
			switch mode {
			case "usage-revoked":
				f.sql(t, `UPDATE project_fixture.secret_usage SET active=false WHERE owner_id=$1`, owner.Details().ID)
			case "lease-released":
				_, r := f.applyModelUsage(t, sc.UsageRequest{Actor: actor, Ref: lease.CredentialRef, Purpose: sc.Model, LeaseOwner: owner, LeaseID: lease.LeaseID, Action: sc.ReleaseLeaseUsage})
				if r.State() != foundation.Committed {
					t.Fatal("lease release", r.Fault())
				}
			case "archiving", "archived", "deleting":
				f.gate(t, c.Lifecycle(mode))
				code, auditCode = foundation.DependencyUnavailable, foundation.ProjectNotActive
			case "wrong-role":
				reg, _ := identity.RegisterService(identity.ObjectService)
				actor, _ = reg.Actor(owner.Details().ID, f.scope)
			case "wrong-cause":
				reg, _ := identity.RegisterService(identity.SecretService)
				actor, _ = reg.Actor(id[struct{}](t).String(), f.scope)
			case "human-no-user-plan":
				f.sql(t, `UPDATE project_fixture.secret_usage SET subject_user=$2,subject_session=$3 WHERE owner_id=$1`, owner.Details().ID, f.owner.Details().UserID, f.owner.Details().SessionID)
				code = foundation.InternalError
			case "agent-run":
				actor, _ = identity.NewAgentRun(f.project.ID, id[identity.Agent](t), id[identity.Execution](t))
			case "agent-subject":
				f.bundle.usage.subject, _ = identity.NewAgentRun(f.project.ID, id[identity.Agent](t), id[identity.Execution](t))
				code, auditCode = foundation.DependencyUnavailable, foundation.DependencyUnbound
			case "direct-service":
				metadata, _ := ac.SecretResolveMetadata(lease.LeaseID.String(), ac.Model, "")
				resource, _ := ac.NewResource(ac.SecretResource, created.Metadata.CredentialRef.Details().ID.String())
				entry, _ := ac.NewEntry(ac.EntryFields{Scope: f.scope, Actor: actor, Action: ac.SecretResolve, Outcome: ac.Success, Resource: resource, Metadata: metadata})
				key, _ := ac.NewAppendKey(ac.SecretProducer, actor.Details().CauseRef, 0)
				r := f.raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
					if err := f.raw.AcquireAll(ctx, tx, bindingLocks(actor, f.project.ID)); err != nil {
						return err
					}
					_, err := f.bundle.audit.AppendInTx(ctx, tx, entry, key)
					return err
				})
				bindingDenied(t, r, foundation.Forbidden)
				f.unchanged(t, bindingCounts{1, 1, 1})
				return
			}
			request.Actor = actor
			material, err := f.bundle.secret.ReadCredentialForUsage(ctxFor(t), request)
			bindingNoMaterial(t, material, err, code)
			if auditCode != "" {
				requireCode(t, f.bundle.tap.err, auditCode)
			}
			if mode == "human-no-user-plan" {
				requireCode(t, f.bundle.tap.err, foundation.DependencyUnavailable)
			}
			f.unchanged(t, bindingCounts{1, 1, 1})
		})
	}
	for _, mode := range []string{"committed", "rollback", "pending"} {
		t.Run("resolve-unknown-"+mode, func(t *testing.T) {
			f := newBindingFixture(t)
			created := f.credential(t)
			lease, actor, owner := f.lease(t, created.Metadata.CredentialRef)
			request := f.modelReadRequest(t, actor, lease, owner)
			proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), mode != "rollback")
			store := f.proxyStore(t, proxy.listener.Addr())
			b := f.bindStore(t, store, store)
			b.tap.after = func(entry ac.Entry, err error) {
				if entry.Fields().Action == ac.SecretResolve && err == nil {
					proxy.armed.Store(true)
				}
			}
			type readResult struct {
				material sc.SecretMaterial
				err      error
			}
			done := make(chan readResult, 1)
			ctx := ctxFor(t)
			go func() {
				material, err := b.secret.ReadCredentialForUsage(ctx, request)
				done <- readResult{material, err}
			}()
			await(t, proxy.reached)
			result := <-done
			bindingNoMaterial(t, result.material, result.err, foundation.CommitUnknown)
			f.unchanged(t, bindingCounts{1, 1, 1})
			if mode == "pending" {
				lock, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, created.Metadata.CredentialRef.Details().ID.String())
				probe, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				r := f.raw.WithinTx(probe, cause(t), func(ctx context.Context, tx foundation.Tx) error {
					return f.raw.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}})
				})
				cancel()
				if r.State() == foundation.Committed {
					t.Fatal("pending resolve did not hold actual credential lock")
				}
			}
			close(proxy.release)
			await(t, proxy.completed)
			lock, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, created.Metadata.CredentialRef.Details().ID.String())
			joined := f.raw.WithinTx(ctx, cause(t), func(ctx context.Context, tx foundation.Tx) error {
				return f.raw.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: lock, Mode: foundation.Exclusive}})
			})
			if joined.State() != foundation.Committed {
				t.Fatal("resolve writer did not terminate", joined.Fault())
			}
			want := 2
			if mode == "rollback" {
				want = 1
			}
			f.unchanged(t, bindingCounts{1, 1, want})
		})
	}
}
