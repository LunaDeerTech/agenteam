//go:build integration

package security_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestSecretProjectAuditRealMutationAndReplay(t *testing.T) {
	f := newSecretProjectAuditFixture(t)
	request := f.request(t, sc.Create, []byte("project-checker-real-material"))
	created, err := f.secret.ExecuteWrite(auditContext(t), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.counts(t); got != (secretProjectAuditCounts{1, 1, 1}) {
		t.Fatal(got)
	}
	checks := f.ports.checks
	replayed, err := f.secret.ExecuteWrite(auditContext(t), request)
	if err != nil || !replayed.Metadata.CredentialRef.Equal(created.Metadata.CredentialRef) || f.ports.checks != checks {
		t.Fatal("command replay appended or lost original receipt", err)
	}
	current := created
	for _, purpose := range []sc.Purpose{sc.Model, sc.MCP} {
		r := f.request(t, sc.Update, []byte("replacement-material"))
		r.Ref = current.Metadata.CredentialRef
		r.ExpectedVersion = current.Metadata.Version
		r.Purpose = purpose
		current, err = f.secret.ExecuteWrite(auditContext(t), r)
		if err != nil {
			t.Fatal(err)
		}
	}
	var first, second string
	if err = f.store.QueryRow(auditContext(t), `SELECT metadata->'changed_fields' FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='secret.update' AND metadata->>'version'='2'`, f.project.String()).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err = f.store.QueryRow(auditContext(t), `SELECT metadata->'changed_fields' FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='secret.update' AND metadata->>'version'='3'`, f.project.String()).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if first != `["value"]` || second != `["purpose", "value"]` {
		t.Fatalf("incorrect actual changed fields %s %s", first, second)
	}
	r := f.request(t, sc.Delete, nil)
	r.Ref = current.Metadata.CredentialRef
	r.ExpectedVersion = current.Metadata.Version
	r.Purpose = current.Metadata.Purpose
	deleted, err := f.secret.ExecuteWrite(auditContext(t), r)
	if err != nil || !deleted.Deleted {
		t.Fatal("delete after canonical row removal", err)
	}
	if got := f.counts(t); got != (secretProjectAuditCounts{0, 4, 4}) {
		t.Fatal(got)
	}
	if _, err = f.secret.ExecuteWrite(auditContext(t), r); err != nil {
		t.Fatal("deleted command replay", err)
	}
	if got := f.counts(t); got != (secretProjectAuditCounts{0, 4, 4}) {
		t.Fatal("replay changed durable counts", got)
	}
}

func TestSecretProjectAuditRealForgeryRollsBack(t *testing.T) {
	for _, tamper := range []string{"changed_fields", "result_version", "receipt_purpose", "receipt_command", "credential", "session", "trace", "key", "no_witness"} {
		t.Run(tamper, func(t *testing.T) {
			f := newSecretProjectAuditFixture(t)
			created := f.create(t, []byte("unchanged-after-rejection"))
			before := f.counts(t)
			f.ports.before = func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (ac.Entry, ac.AppendKey, error) {
				x, err := f.store.InTx(tx)
				if err != nil {
					return e, k, err
				}
				fields := e.Fields()
				switch tamper {
				case "changed_fields":
					fields.Metadata, _ = ac.SecretMutationMetadata(ac.SecretUpdate, 2, []ac.ChangedField{ac.ValueChanged})
				case "result_version":
					fields.Metadata, _ = ac.SecretMutationMetadata(ac.SecretUpdate, 99, []ac.ChangedField{ac.ValueChanged, ac.PurposeChanged})
				case "receipt_purpose":
					_, err = x.Exec(ctx, `UPDATE agenteam_secret.secret_command_receipts SET purpose='runner' WHERE command_digest=$1`, k.Details().CauseRef)
				case "receipt_command":
					changed, _ := cursor.Digest([]byte("different-command"))
					_, err = x.Exec(ctx, `UPDATE agenteam_secret.secret_command_receipts SET command_digest=$2 WHERE command_digest=$1`, k.Details().CauseRef, string(changed))
				case "credential":
					fields.Resource, _ = ac.NewResource(ac.SecretResource, newID[sc.Credential](t).String())
				case "session":
					user, _ := foundation.ParseID[identity.User](fields.Actor.Details().UserID)
					fields.Actor, _ = identity.NewHuman(user, newID[identity.Session](t))
				case "trace":
					fields.Associations.HTTPTraceID = newID[struct{}](t).String()
				case "key":
					k, _ = ac.NewAppendKey(ac.SecretProducer, k.Details().CauseRef, 1)
				case "no_witness":
					return e, k, f.ports.checker.CheckProjectAuditInTx(context.Background(), tx, e, k)
				}
				if err != nil {
					return e, k, err
				}
				e, err = ac.NewEntry(fields)
				return e, k, err
			}
			r := f.request(t, sc.Update, []byte("must-not-persist"))
			r.Ref = created.Metadata.CredentialRef
			r.ExpectedVersion = 1
			r.Purpose = sc.MCP
			if _, err := f.secret.ExecuteWrite(auditContext(t), r); err == nil {
				t.Fatal("forged audit accepted")
			}
			if after := f.counts(t); after != before {
				t.Fatal("partial transaction committed", before, after)
			}
			var purpose string
			var version int64
			if err := f.store.QueryRow(auditContext(t), `SELECT purpose,version FROM agenteam_secret.secrets WHERE id=$1`, created.Metadata.CredentialRef.Details().ID.String()).Scan(&purpose, &version); err != nil || purpose != "model" || version != 1 {
				t.Fatal("canonical write escaped rollback", err)
			}
		})
	}
}

func TestSecretProjectAuditResolveFactsAndRejections(t *testing.T) {
	for _, mode := range []string{"success", "released", "lease_owner", "lease_ref", "binding", "purpose", "payload", "ciphertext", "grant", "grant_subject", "lease_after_decrypt", "metadata_after_decrypt", "payload_after_decrypt", "association", "resolution", "audit_failure", "archived"} {
		t.Run(mode, func(t *testing.T) {
			f := newSecretProjectAuditFixture(t)
			created := f.create(t, []byte("only-after-real-aead-and-audit"))
			owner, actor := f.bind(t, created.Metadata.CredentialRef, sc.Model)
			lease := f.acquire(t, created.Metadata.CredentialRef, owner, actor)
			request := f.modelReadRequest(t, actor, lease.LeaseID)
			switch mode {
			case "released":
				f.exec(t, `UPDATE agenteam_secret.secret_leases SET released=true WHERE id=$1`, lease.LeaseID.String())
			case "lease_owner":
				f.exec(t, `UPDATE agenteam_secret.secret_leases SET owner_id=$2 WHERE id=$1`, lease.LeaseID.String(), newID[struct{}](t).String())
			case "lease_ref":
				other := f.create(t, []byte("different-credential"))
				f.exec(t, `UPDATE agenteam_secret.secret_leases SET credential_id=$2 WHERE id=$1`, lease.LeaseID.String(), other.Metadata.CredentialRef.Details().ID.String())
			case "binding":
				f.exec(t, `UPDATE audit_fixture.secret_bindings SET active=false WHERE owner_id=$1`, owner.Details().ID)
			case "purpose":
				f.exec(t, `UPDATE agenteam_secret.secrets SET purpose='mcp' WHERE id=$1`, created.Metadata.CredentialRef.Details().ID.String())
			case "payload":
				f.exec(t, `UPDATE agenteam_secret.secret_payloads SET owner_id=$2 WHERE payload_id=(SELECT current_payload_id FROM agenteam_secret.secrets WHERE id=$1)`, created.Metadata.CredentialRef.Details().ID.String(), newID[struct{}](t).String())
			case "ciphertext":
				f.exec(t, `UPDATE agenteam_secret.secret_payloads SET ciphertext=set_byte(ciphertext,0,get_byte(ciphertext,0)#1) WHERE payload_id=(SELECT current_payload_id FROM agenteam_secret.secrets WHERE id=$1)`, created.Metadata.CredentialRef.Details().ID.String())
			case "grant":
				f.ports.grant = func(g *sc.UseGrant) { g.Consumer = sc.MCP }
			case "grant_subject":
				f.ports.grant = func(g *sc.UseGrant) {
					g.Subject, _ = identity.NewHuman(newID[identity.User](t), newID[identity.Session](t))
				}
			case "archived":
				f.exec(t, `UPDATE audit_fixture.projects SET state='archived' WHERE id=$1`, f.project.String())
			case "lease_after_decrypt", "metadata_after_decrypt", "payload_after_decrypt":
				f.ports.before = func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (ac.Entry, ac.AppendKey, error) {
					x, err := f.store.InTx(tx)
					if err != nil {
						return e, k, err
					}
					switch mode {
					case "lease_after_decrypt":
						_, err = x.Exec(ctx, `UPDATE agenteam_secret.secret_leases SET released=true WHERE id=$1`, lease.LeaseID.String())
					case "metadata_after_decrypt":
						_, err = x.Exec(ctx, `UPDATE agenteam_secret.secrets SET version=version+1 WHERE id=$1`, created.Metadata.CredentialRef.Details().ID.String())
					case "payload_after_decrypt":
						_, err = x.Exec(ctx, `UPDATE agenteam_secret.secret_payloads SET owner_id=$2 WHERE payload_id=(SELECT current_payload_id FROM agenteam_secret.secrets WHERE id=$1)`, created.Metadata.CredentialRef.Details().ID.String(), newID[struct{}](t).String())
					}
					return e, k, err
				}
			case "association", "resolution", "audit_failure":
				f.ports.before = func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (ac.Entry, ac.AppendKey, error) {
					if mode == "audit_failure" {
						return e, k, deny(foundation.DependencyUnavailable)
					}
					if mode == "resolution" {
						k, _ = ac.NewAppendKey(ac.SecretProducer, newID[struct{}](t).String(), 0)
						return e, k, nil
					}
					fields := e.Fields()
					fields.Associations.RequestID = newID[struct{}](t).String()
					altered, err := ac.NewEntry(fields)
					return altered, k, err
				}
			}
			before := f.counts(t)
			material, err := f.secret.ReadCredentialForUsage(auditContext(t), request)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				defer material.Destroy()
				if err = material.Use(func(value []byte) error {
					if string(value) != "only-after-real-aead-and-audit" {
						return fmt.Errorf("wrong material")
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if f.counts(t).audits != before.audits+1 {
					t.Fatal("resolve omitted real Audit")
				}
			} else {
				projectAuditNoMaterial(t, material, err)
				if f.counts(t).audits != before.audits {
					t.Fatal("rejected resolution committed an Audit")
				}
			}
		})
	}
}

func TestSecretProjectAuditCurrentAuthorityStillRequired(t *testing.T) {
	for _, mode := range []string{"session", "owner", "archived"} {
		t.Run(mode, func(t *testing.T) {
			f := newSecretProjectAuditFixture(t)
			request := f.request(t, sc.Create, []byte("current-authority-required"))
			if _, err := f.secret.ExecuteWrite(auditContext(t), request); err != nil {
				t.Fatal(err)
			}
			before := f.counts(t)
			checks := f.ports.checks
			switch mode {
			case "session":
				f.exec(t, `UPDATE audit_fixture.sessions SET active=false WHERE id=$1`, f.actor.Details().SessionID)
			case "owner":
				f.exec(t, `UPDATE audit_fixture.projects SET owner_id=$2 WHERE id=$1`, f.project.String(), newID[identity.User](t).String())
			case "archived":
				f.exec(t, `UPDATE audit_fixture.projects SET state='archived' WHERE id=$1`, f.project.String())
			}
			if _, err := f.secret.ExecuteWrite(auditContext(t), request); err == nil {
				t.Fatal("historical receipt bypassed current permission")
			}
			if _, err := f.secret.ExecuteWrite(auditContext(t), f.request(t, sc.Create, []byte("must-not-persist"))); err == nil {
				t.Fatal("fresh write bypassed current permission")
			}
			if f.counts(t) != before || f.ports.checks != checks {
				t.Fatal("denied current authority reached checker or changed rows")
			}
		})
	}
}

func TestSecretProjectAuditLiveTxAndDirectAppend(t *testing.T) {
	f := newSecretProjectAuditFixture(t)
	var captured context.Context
	var token foundation.Tx
	var entry ac.Entry
	var key ac.AppendKey
	f.ports.before = func(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (ac.Entry, ac.AppendKey, error) {
		captured, token, entry, key = ctx, tx, e, k
		return e, k, nil
	}
	f.create(t, []byte("witness-belongs-to-one-live-call"))
	if err := f.ports.checker.CheckProjectAuditInTx(captured, token, entry, key); err == nil {
		t.Fatal("completed transaction witness reused")
	}
	other := openAuditStore(t, f.db.Config(t, nil))
	checker, _ := secret.NewProjectAuditAuthority(other)
	if err := checker.CheckProjectAuditInTx(captured, token, entry, key); err == nil {
		t.Fatal("cross Store witness reused")
	}
	r := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		project, _ := foundation.ProjectLock(f.project.String())
		user, _ := foundation.UserLock(f.actor.Details().UserID)
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: user, Mode: foundation.Shared}, {Key: project, Mode: foundation.Shared}}); err != nil {
			return err
		}
		return f.ports.checker.CheckProjectAuditInTx(captured, tx, entry, key)
	})
	if r.State() != foundation.NotCommitted {
		t.Fatal("witness crossed transaction")
	}
	f.ports.before = nil
	a, err := audit.New(f.store, auditKeys(t), audit.Authorizations{Sessions: f.ports, System: f.ports, Projects: &secretProjectAuditAppendPorts{f.ports}})
	if err != nil {
		t.Fatal(err)
	}
	r = f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		project, _ := foundation.ProjectLock(f.project.String())
		user, _ := foundation.UserLock(f.actor.Details().UserID)
		if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: user, Mode: foundation.Shared}, {Key: project, Mode: foundation.Shared}}); err != nil {
			return err
		}
		_, err := a.AppendInTx(ctx, tx, entry, key)
		return err
	})
	if r.State() != foundation.NotCommitted || f.counts(t).audits != 1 {
		t.Fatal("historical receipt fabricated a fresh append authorization")
	}
}

func TestSecretProjectAuditMutationCommitUnknown(t *testing.T) {
	for _, mode := range []string{"committed", "rollback", "pending"} {
		t.Run(mode, func(t *testing.T) {
			f := newSecretProjectAuditFixture(t)
			upstream := net.JoinHostPort("127.0.0.1", f.db.Fixture.Port)
			var reached, release <-chan struct{}
			var unlock func()
			var armed func()
			var address net.Addr
			var completed <-chan struct{}
			if mode == "pending" {
				p := newOutboundLateCommitProxy(t, upstream)
				address = p.listener.Addr()
				reached, release, completed = p.reached, p.release, p.completed
				unlock = func() { close(p.release) }
				armed = func() { p.armed.Store(true) }
			} else {
				p := newCommitProxy(t, upstream, mode == "committed")
				p.armed.Store(false)
				address = p.listener.Addr()
				reached, release = p.reached, p.release
				unlock = func() { close(p.release) }
				armed = func() { p.armed.Store(true) }
			}
			_ = release
			store := projectAuditProxyStore(t, f, address)
			service, _ := projectAuditServiceOnStore(t, store)
			request := f.request(t, sc.Create, []byte("original-command-unknown"))
			prepared, err := service.PrepareWrite(auditContext(t), request)
			if err != nil {
				t.Fatal(err)
			}
			// Arm only after nonce/preparation transactions. This is the actual
			// ApplyPreparedWrite + receipt + checker + Audit final transaction.
			armed()
			done := make(chan foundation.CommitResult, 1)
			ctx := auditContext(t)
			cause, _ := foundation.NewCommandsCause(request.Identity)
			go func() {
				done <- store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
					if err := store.AcquireAll(ctx, tx, prepared.RequiredLocks()); err != nil {
						return err
					}
					_, err := service.ApplyPreparedWriteInTx(ctx, tx, prepared)
					return err
				})
			}()
			select {
			case <-reached:
			case <-ctx.Done():
				t.Fatal("actual mutation COMMIT was not intercepted")
			}
			if mode == "pending" {
				select {
				case r := <-done:
					if r.State() != foundation.Unknown {
						t.Fatal("pending writer was declared final")
					}
				case <-ctx.Done():
					t.Fatal("unknown did not return")
				}
				probe, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				r := f.store.WithinTx(probe, txCause(t), func(ctx context.Context, tx foundation.Tx) error {
					return f.store.AcquireAll(ctx, tx, prepared.RequiredLocks())
				})
				cancel()
				if r.State() == foundation.Committed {
					t.Fatal("uncommitted original writer was not actually holding its locks")
				}
			}
			observed := f.counts(t)
			if mode == "committed" && observed != (secretProjectAuditCounts{1, 1, 1}) || mode != "committed" && observed != (secretProjectAuditCounts{}) {
				t.Fatal("proxy intercepted the wrong transaction", observed)
			}
			unlock()
			if mode == "pending" {
				select {
				case <-completed:
				case <-ctx.Done():
					t.Fatal("original held COMMIT did not finish")
				}
			} else {
				select {
				case r := <-done:
					if r.State() != foundation.Unknown {
						t.Fatal("ACK loss did not remain unknown")
					}
				case <-ctx.Done():
					t.Fatal("unknown did not return")
				}
			}
			projectAuditJoinWriter(t, f.store, prepared.RequiredLocks())
			lookup, err := service.LookupWrite(ctx, prepared)
			if err != nil || lookup.Observed != (mode != "rollback") {
				t.Fatal("confirmed original command lookup", err)
			}
			if _, err = service.ExecuteWrite(ctx, request); err != nil {
				t.Fatal("original command failed to recover", err)
			}
			if got := f.counts(t); got != (secretProjectAuditCounts{1, 1, 1}) {
				t.Fatal("mutation recovery duplicated or lost facts", got)
			}
		})
	}
}

func TestSecretProjectAuditResolveCommitUnknown(t *testing.T) {
	for _, mode := range []string{"committed", "rollback", "pending"} {
		t.Run(mode, func(t *testing.T) {
			f := newSecretProjectAuditFixture(t)
			created := f.create(t, []byte("unknown-must-not-escape"))
			owner, actor := f.bind(t, created.Metadata.CredentialRef, sc.Model)
			lease := f.acquire(t, created.Metadata.CredentialRef, owner, actor)
			upstream := net.JoinHostPort("127.0.0.1", f.db.Fixture.Port)
			var reached, completed <-chan struct{}
			var unlock, armed func()
			var address net.Addr
			if mode == "pending" {
				p := newOutboundLateCommitProxy(t, upstream)
				address, reached, completed = p.listener.Addr(), p.reached, p.completed
				unlock = func() { close(p.release) }
				armed = func() { p.armed.Store(true) }
			} else {
				p := newCommitProxy(t, upstream, mode == "committed")
				p.armed.Store(false)
				address, reached = p.listener.Addr(), p.reached
				unlock = func() { close(p.release) }
				armed = func() { p.armed.Store(true) }
			}
			store := projectAuditProxyStore(t, f, address)
			service, _ := projectAuditServiceOnStore(t, store)
			armed()
			ctx := auditContext(t)
			type readResult struct {
				material sc.SecretMaterial
				err      error
			}
			done := make(chan readResult, 1)
			go func() {
				m, err := service.ReadCredentialForUsage(ctx, f.modelReadRequest(t, actor, lease.LeaseID))
				done <- readResult{m, err}
			}()
			select {
			case <-reached:
			case <-ctx.Done():
				t.Fatal("actual resolve COMMIT was not intercepted")
			}
			if mode == "pending" {
				select {
				case r := <-done:
					requireCode(t, r.err, foundation.CommitUnknown)
					projectAuditNoMaterial(t, r.material, r.err)
				case <-ctx.Done():
					t.Fatal("pending read did not return unknown")
				}
			}
			want := 1
			if mode == "committed" {
				want = 2
			}
			if f.counts(t).audits != want {
				t.Fatal("wrong resolve transaction intercepted")
			}
			unlock()
			if mode == "pending" {
				select {
				case <-completed:
				case <-ctx.Done():
					t.Fatal("held read COMMIT did not finish")
				}
			} else {
				select {
				case r := <-done:
					requireCode(t, r.err, foundation.CommitUnknown)
					projectAuditNoMaterial(t, r.material, r.err)
				case <-ctx.Done():
					t.Fatal("read COMMIT failure hung")
				}
			}
			project, _ := foundation.ProjectLock(f.project.String())
			credential, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, created.Metadata.CredentialRef.Details().ID.String())
			projectAuditJoinWriter(t, f.store, []foundation.LockRequest{{Key: project, Mode: foundation.Shared}, {Key: credential, Mode: foundation.Exclusive}})
			if string(readMaterial(t, f.secretFixture, service, actor, lease.LeaseID)) != "unknown-must-not-escape" {
				t.Fatal("confirmed fresh read failed")
			}
			want = 3
			if mode == "rollback" {
				want = 2
			}
			if f.counts(t).audits != want {
				t.Fatal("new resolution reused or lost an audit")
			}
		})
	}
}
