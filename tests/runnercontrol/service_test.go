//go:build integration

package runnercontrol_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestRunnerControlManagement(t *testing.T) {
	v := newRunnerServiceFixture(t)
	intent, key, first := v.create(t, "managed runner")
	target := intent.Target()
	t.Run("original intent and one-time material", func(t *testing.T) {
		replay, e := v.runner.Execute(migrationContext(t), v.actor, key, intent)
		requireServiceOK(t, e, "same command replay")
		if replay.Material != nil || !reflect.DeepEqual(replay.Receipt, first.Receipt) {
			t.Fatal("replay changed historical receipt or repeated material")
		}
		lookup, e := v.runner.Lookup(migrationContext(t), v.actor, key, intent)
		requireServiceOK(t, e, "original lookup")
		if lookup.Receipt == nil || !reflect.DeepEqual(*lookup.Receipt, first.Receipt) {
			t.Fatal("lookup lost committed receipt")
		}
		other, e := rc.NewCreate(rc.CreateRequest{RunnerID: target, Name: "different intent", Description: "original", Tags: []string{}, RootPath: "/srv/runner"})
		requireServiceOK(t, e, "different intent")
		_, e = v.runner.Lookup(migrationContext(t), v.actor, key, other)
		requireServiceCode(t, e, f.IdempotencyKeyReused)
		var stored []byte
		e = v.store.QueryRow(migrationContext(t), `SELECT receipt FROM agenteam_runner.commands WHERE id=$1::uuid`, first.Receipt.CommandID.String()).Scan(&stored)
		requireServiceOK(t, e, "persistent public receipt")
		var receipt rc.Receipt
		if json.Unmarshal(stored, &receipt) != nil || !reflect.DeepEqual(receipt, first.Receipt) {
			t.Fatal("persistent closed receipt differs from returned receipt")
		}
	})
	t.Run("metadata credential and exact facts", func(t *testing.T) {
		description := "updated description"
		update, e := rc.NewUpdate(target, rc.UpdateRequest{ExpectedVersion: 1, Description: &description})
		requireServiceOK(t, e, "update intent")
		result, e := v.runner.Execute(migrationContext(t), v.actor, serviceKey(t), update)
		requireServiceOK(t, e, "real update")
		if !result.Receipt.Changed || result.Receipt.Runner.Version != 2 || result.Receipt.Runner.Description != description {
			t.Fatal("metadata update did not advance exact postimage")
		}
		_, e = v.runner.Execute(migrationContext(t), v.actor, serviceKey(t), update)
		requireServiceCode(t, e, f.VersionConflict)
		noop, e := rc.NewUpdate(target, rc.UpdateRequest{ExpectedVersion: 2, Description: &description})
		requireServiceOK(t, e, "no-op intent")
		unchanged, e := v.runner.Execute(migrationContext(t), v.actor, serviceKey(t), noop)
		requireServiceOK(t, e, "real metadata no-op")
		if unchanged.Receipt.Changed || unchanged.Receipt.Runner.Version != 2 {
			t.Fatal("no-op changed metadata version")
		}
		issue, e := rc.NewEnrollment(target, rc.CredentialRequest{ExpectedVersion: 2})
		requireServiceOK(t, e, "issue intent")
		issued, e := v.runner.Execute(migrationContext(t), v.actor, serviceKey(t), issue)
		requireServiceOK(t, e, "real enrollment reissue")
		if issued.Material == nil || issued.Receipt.Runner.Version != 3 || issued.Receipt.Runner.CredentialGeneration != 2 {
			t.Fatal("reissue did not retire previous generation")
		}
		revoke, e := rc.NewRevoke(target, rc.CredentialRequest{ExpectedVersion: 3})
		requireServiceOK(t, e, "revoke intent")
		revoked, e := v.runner.Execute(migrationContext(t), v.actor, serviceKey(t), revoke)
		requireServiceOK(t, e, "real revocation")
		if revoked.Material != nil || revoked.Receipt.Runner.Version != 4 || revoked.Receipt.Runner.CredentialGeneration != 3 {
			t.Fatal("revocation did not advance generation/version")
		}
		var commands, tokens, liveTokens, events, audits int
		e = v.store.QueryRow(migrationContext(t), `SELECT
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid AND revoked_at IS NULL),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1::uuid)`, target.String()).Scan(&commands, &tokens, &liveTokens, &events, &audits)
		requireServiceOK(t, e, "exact management facts")
		if commands != 5 || tokens != 2 || liveTokens != 0 || events != 3 || audits != 4 {
			t.Fatalf("management facts commands=%d tokens=%d live=%d events=%d audits=%d", commands, tokens, liveTokens, events, audits)
		}
		page, e := v.runner.List(migrationContext(t), v.actor, rc.ListRequest{Limit: 1})
		requireServiceOK(t, e, "real list")
		if len(page.Items) != 1 || page.Items[0].ID != target || page.Items[0].Version != 4 || page.Next != nil {
			t.Fatal("list lost current postimage or fabricated next page")
		}
	})
	t.Run("current Session and same User recovery", func(t *testing.T) {
		original := v.actor
		replacement := v.login(t)
		if replacement.Details().UserID != original.Details().UserID || replacement.Details().SessionID == original.Details().SessionID {
			t.Fatal("second login did not establish a different real Session")
		}
		requireServiceOK(t, v.accounts.Logout(migrationContext(t), account.LogoutRequest{Actor: original, Key: serviceKey(t)}), "real Session revocation")
		_, e := v.runner.Lookup(migrationContext(t), original, key, intent)
		requireServiceCode(t, e, f.SessionRevoked)
		v.actor = replacement
		historical, e := v.runner.Execute(migrationContext(t), replacement, key, intent)
		requireServiceOK(t, e, "same User new Session replay")
		if historical.Material != nil || !reflect.DeepEqual(historical.Receipt, first.Receipt) {
			t.Fatal("new Session repeated secret or replaced historical v1 receipt")
		}
	})
	t.Run("Audit failure rolls back all Runner facts", func(t *testing.T) {
		// A nontransactional sequence proves the real typed append reached this
		// AFTER ROW trigger. No identity or Runner positive fact is SQL seeded.
		_, e := v.store.Exec(migrationContext(t), `CREATE SCHEMA runner_control_fixture;
 CREATE SEQUENCE runner_control_fixture.audit_attempt;
 CREATE FUNCTION runner_control_fixture.reject_runner_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.producer='runner' THEN PERFORM nextval('runner_control_fixture.audit_attempt'); RAISE EXCEPTION 'controlled audit failure' USING ERRCODE='P0001'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER runner_control_fixture_reject AFTER INSERT ON agenteam_audit.audit_records FOR EACH ROW EXECUTE FUNCTION runner_control_fixture.reject_runner_audit()`)
		requireServiceOK(t, e, "install owned atomicity stimulus")
		negative, e := rc.NewCreate(rc.CreateRequest{RunnerID: serviceID[rc.Runner](t), Name: "atomic runner", Tags: []string{}, RootPath: "/srv/runner"})
		requireServiceOK(t, e, "atomic create intent")
		atomicKey := serviceKey(t)
		failed, e := v.runner.Execute(migrationContext(t), v.actor, atomicKey, negative)
		if e == nil || failed.Material != nil || failed.Receipt.CommandID.Validate() == nil {
			t.Fatal("Audit failure leaked success/material")
		}
		var attempted bool
		var sequence int64
		e = v.store.QueryRow(migrationContext(t), `SELECT last_value,is_called FROM runner_control_fixture.audit_attempt`).Scan(&sequence, &attempted)
		requireServiceOK(t, e, "actual Audit injection reached")
		if !attempted || sequence != 1 {
			t.Fatal("negative failed before actual typed Audit append")
		}
		var rows int
		e = v.store.QueryRow(migrationContext(t), `SELECT
 (SELECT count(*) FROM agenteam_runner.runners WHERE id=$1::uuid)+
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1::uuid)+
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid)+
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid)+
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE runner_id=$1::uuid)`, negative.Target().String()).Scan(&rows)
		requireServiceOK(t, e, "atomic zero facts")
		if rows != 0 {
			t.Fatalf("Audit rollback retained %d facts", rows)
		}
		_, e = v.store.Exec(migrationContext(t), `DROP TRIGGER runner_control_fixture_reject ON agenteam_audit.audit_records`)
		requireServiceOK(t, e, "retire exact injection")
		retry, e := v.runner.Execute(migrationContext(t), v.actor, atomicKey, negative)
		requireServiceOK(t, e, "same intent after known rollback")
		if retry.Material == nil || !retry.Receipt.Changed {
			t.Fatal("known rolled-back command did not retry as new")
		}
	})
}

func TestRunnerControlDeviceAndReader(t *testing.T) {
	v := newRunnerServiceFixture(t)
	_, _, material := v.create(t, "device runner")
	target := material.Receipt.Runner.ID
	private, enrollment := v.enroll(t, material)
	_, e := v.runner.Enroll(migrationContext(t), enrollment)
	requireServiceCode(t, e, f.Unauthenticated)
	v.snapshot(t, target, rc.Offline)
	auth := v.authentication(t, target, private)
	// Wrong signature must not consume the actual issued nonce. Reusing that
	// exact nonce with the real device signature below is the positive control.
	bad, e := p.NewAuthentication(auth.RunnerID(), auth.Nonce(), auth.Timestamp(), make([]byte, ed25519.SignatureSize))
	requireServiceOK(t, e, "wrong signature shape")
	_, e = v.runner.Authenticate(migrationContext(t), bad)
	requireServiceCode(t, e, f.Unauthenticated)
	connection, e := v.runner.Authenticate(migrationContext(t), auth)
	requireServiceOK(t, e, "real device authentication")
	_, e = v.runner.Authenticate(migrationContext(t), auth)
	requireServiceCode(t, e, f.Unauthenticated)
	v.snapshot(t, target, rc.Offline)
	hello := p.Hello{RunnerID: p.ID(target.String()), RunnerVersion: "runner-test", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{}, FeatureFlags: []string{}}
	ack, e := v.runner.AcceptHello(migrationContext(t), connection, hello)
	requireServiceOK(t, e, "real hello")
	if !ack.Accepted || ack.HeartbeatIntervalMS != 10000 || ack.HeartbeatTimeoutMS != 30000 {
		t.Fatal("hello did not retain fixed heartbeat policy")
	}
	v.snapshot(t, target, rc.Online)
	foreign := v.newRunner(t)
	seen, e := foreign.Get(migrationContext(t), v.actor, target)
	requireServiceOK(t, e, "foreign owner Reader")
	if seen.Status != rc.Online {
		t.Fatal("local registry absence forged offline for valid foreign lease")
	}
	if e := foreign.CurrentConnection(migrationContext(t), connection); e == nil {
		t.Fatal("foreign service acquired original connection authority")
	}
	beat := p.Heartbeat{Sequence: "1", RunnerTime: p.Instant(time.Now().UTC().Format(time.RFC3339Nano))}
	requireServiceOK(t, v.runner.Heartbeat(migrationContext(t), connection, beat), "real heartbeat")
	if e := v.runner.Heartbeat(migrationContext(t), connection, beat); e == nil {
		t.Fatal("duplicate heartbeat renewed current lease")
	}
	next, e := foreign.Authenticate(migrationContext(t), v.authentication(t, target, private))
	requireServiceOK(t, e, "second owner new generation")
	if e := v.runner.CurrentConnection(migrationContext(t), connection); e == nil {
		t.Fatal("old generation remained current")
	}
	if e := v.runner.RejectIncompatible(migrationContext(t), connection); e == nil {
		t.Fatal("old generation changed successor compatibility")
	}
	v.snapshot(t, target, rc.Offline) // Auth is not hello.
	_, e = foreign.AcceptHello(migrationContext(t), next, hello)
	requireServiceOK(t, e, "successor hello")
	v.snapshot(t, target, rc.Online)
	requireServiceOK(t, foreign.RejectIncompatible(migrationContext(t), next), "current major incompatibility")
	v.snapshot(t, target, rc.Incompatible)
	requireServiceOK(t, foreign.OwnConnection(migrationContext(t), next, func(context.Context) error { return nil }), "known connection retirement")
	// Incompatible is separate from connectivity and only a new accepted hello
	// clears it. A fresh authentication never restores the previous RPC owner.
	final, e := foreign.Authenticate(migrationContext(t), v.authentication(t, target, private))
	requireServiceOK(t, e, "post-retirement authentication")
	_, e = foreign.AcceptHello(migrationContext(t), final, hello)
	requireServiceOK(t, e, "new accepted hello clears incompatible")
	v.snapshot(t, target, rc.Online)
	requireServiceOK(t, foreign.OwnConnection(migrationContext(t), final, func(context.Context) error { return nil }), "ordinary current retirement")
	v.snapshot(t, target, rc.Offline)

	t.Run("lease is bounded without a local owner map", func(t *testing.T) {
		lease, e := foreign.Authenticate(migrationContext(t), v.authentication(t, target, private))
		requireServiceOK(t, e, "lease authentication")
		_, e = foreign.AcceptHello(migrationContext(t), lease, hello)
		requireServiceOK(t, e, "lease hello")
		initial := v.snapshot(t, target, rc.Online)
		var remaining float64
		e = v.store.QueryRow(migrationContext(t), `SELECT extract(epoch FROM lease_expires_at-clock_timestamp())::float8 FROM agenteam_runner.connections WHERE runner_id=$1::uuid`, target.String()).Scan(&remaining)
		requireServiceOK(t, e, "actual remaining lease")
		if remaining <= 25 || remaining > 30 {
			t.Fatal("lease stimulus does not have the original live 30s budget")
		}
		// No heartbeat is sent and no DB clock/row is rewritten. This observes
		// conservative expiry only; it is not an instantaneous crash detector.
		deadline := time.NewTimer(35 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-deadline.C:
				t.Fatal("original lease did not naturally expire")
			case <-tick.C:
				current, e := v.runner.Get(migrationContext(t), v.actor, target)
				requireServiceOK(t, e, "bounded lease Reader")
				if current.Version != initial.Version || current.CredentialGeneration != initial.CredentialGeneration {
					t.Fatal("heartbeat lease changed management identity")
				}
				if current.Status == rc.Offline {
					if e := foreign.CurrentConnection(migrationContext(t), lease); e == nil {
						t.Fatal("expired lease allowed dispatch admission")
					}
					return
				}
				if current.Status != rc.Online {
					t.Fatal("natural disconnect fabricated incompatible")
				}
			}
		}
	})
	t.Run("unreadable database is not offline", func(t *testing.T) {
		store := openRunnerStore(t, v.db)
		// The same database and Account facts are consumed through this pool;
		// closing it produces an actual acquisition failure, not an offline DTO.
		accounts, e := account.NewAuthority(store, v.keys)
		requireServiceOK(t, e, "Reader Account authority")
		requireServiceOK(t, accounts.Initialize(migrationContext(t)), "Reader Account initialization")
		authority, e := service.NewAuthority(store, accounts)
		requireServiceOK(t, e, "closed Store authority")
		reader, e := service.New(authority, v.auditor)
		requireServiceOK(t, e, "closed Store Reader")
		requireServiceOK(t, store.ForceClose(migrationContext(t)), "actual owned pool close")
		result, e := reader.Get(migrationContext(t), v.actor, target)
		requireServiceCode(t, e, f.DependencyUnavailable)
		if result.ID.Validate() == nil || result.Status != "" {
			t.Fatal("unreadable database synthesized a Runner snapshot")
		}
		reader.Stop()
		requireServiceOK(t, reader.Drain(migrationContext(t)), "closed Store Reader join")
	})
}
