//go:build integration

package runnercontrol_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

// The only external authority stimulus is a demotion of the exact real
// bootstrap User under its original User EX lock. Account has no public role
// mutation API; this does not claim acceptance of such a production command.
// All positive identities, commands, receipts and device credentials use the
// real Account/Runner services. Triggers below only hold an original operation.
type runnerSafetyCall[T any] struct {
	ctx   context.Context
	done  chan struct{}
	value T
	err   error
}

func startRunnerSafetyCall[T any](t *testing.T, fn func(context.Context) (T, error)) *runnerSafetyCall[T] {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	call := &runnerSafetyCall[T]{ctx: ctx, done: make(chan struct{})}
	go func() { defer close(call.done); call.value, call.err = fn(ctx) }()
	t.Cleanup(func() {
		cancel()
		tail, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		select {
		case <-call.done:
		case <-tail.Done():
			t.Error("original safety call did not actually return")
		}
	})
	return call
}

func (c *runnerSafetyCall[T]) await(t *testing.T) (T, error) {
	t.Helper()
	select {
	case <-c.done:
		return c.value, c.err
	case <-c.ctx.Done():
		t.Fatal("original safety call did not reach its terminal result")
		var zero T
		return zero, c.ctx.Err()
	}
}

type runnerSafetyBarrier struct {
	tx       pgx.Tx
	ctx      context.Context
	key      f.LockKey
	pid      int
	released bool
}

// A private BEFORE trigger waits on an independently held advisory lock. The
// original caller must appear in pg_locks before its dependent contender starts.
func newRunnerSafetyBarrier(t *testing.T, v *runnerServiceFixture, kind, target string) *runnerSafetyBarrier {
	t.Helper()
	table, column, event, extra := "", "", "", ""
	switch kind {
	case "role":
		table, column, event = "agenteam_account.users", "id", "UPDATE"
	case "audit":
		table, column, event, extra = "agenteam_audit.audit_records", "runner_id", "INSERT", " AND NEW.producer='runner'"
	case "connection":
		table, column, event = "agenteam_runner.connections", "runner_id", "INSERT"
	default:
		t.Fatal("unsupported private safety barrier")
	}
	key, e := f.SystemConfigLock("runner-safety-" + serviceID[struct{}](t).String())
	requireServiceOK(t, e, "private barrier identity")
	query := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS runner_safety_fixture;
 CREATE FUNCTION runner_safety_fixture.hold_original() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.%s='%s'::uuid%s THEN PERFORM pg_advisory_xact_lock(%d::bigint); END IF; RETURN NEW; END $$;
 CREATE TRIGGER runner_safety_hold BEFORE %s ON %s FOR EACH ROW EXECUTE FUNCTION runner_safety_fixture.hold_original()`, column, target, extra, key.AdvisoryKey(), event, table)
	_, e = v.store.Exec(migrationContext(t), query)
	requireServiceOK(t, e, "install owned original-call barrier")
	conn := v.db.Connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	tx, e := conn.Begin(ctx)
	requireServiceOK(t, e, "begin original barrier holder")
	barrier := &runnerSafetyBarrier{tx: tx, ctx: ctx, key: key}
	t.Cleanup(func() {
		tail, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if !barrier.released {
			if e := tx.Rollback(tail); e != nil && e != pgx.ErrTxClosed {
				t.Error("original barrier holder did not roll back")
			}
		}
		cancel()
		if _, e := v.store.Exec(tail, "DROP TRIGGER runner_safety_hold ON "+table+"; DROP FUNCTION runner_safety_fixture.hold_original()"); e != nil {
			t.Error("owned original-call barrier did not retire")
		}
	})
	_, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1::bigint)", key.AdvisoryKey())
	requireServiceOK(t, e, "hold private ordering barrier")
	requireServiceOK(t, tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&barrier.pid), "original holder backend")
	return barrier
}

func (b *runnerSafetyBarrier) release(t *testing.T) {
	t.Helper()
	requireServiceOK(t, b.tx.Commit(b.ctx), "actual ordering holder commit")
	b.released = true
}

func waitRunnerSafetyLock(t *testing.T, v *runnerServiceFixture, blocker int, key f.LockKey, mode f.LockMode) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	word := uint64(key.AdvisoryKey())
	lockMode := "ExclusiveLock"
	if mode == f.Shared {
		lockMode = "ShareLock"
	}
	for {
		var count, pid int
		e := v.store.QueryRow(ctx, `SELECT count(*),coalesce(min(a.pid),0) FROM pg_stat_activity a
 WHERE a.datname=current_database() AND $1::integer=ANY(pg_blocking_pids(a.pid)) AND EXISTS (
 SELECT 1 FROM pg_locks l WHERE l.pid=a.pid AND l.locktype='advisory' AND NOT l.granted
 AND l.mode=$2 AND l.classid::bigint=$3 AND l.objid::bigint=$4 AND l.objsubid=1)`, blocker, lockMode, int64(word>>32), int64(word&0xffffffff)).Scan(&count, &pid)
		requireServiceOK(t, e, "observe exact original lock waiter")
		if count == 1 {
			return pid
		}
		if count > 1 {
			t.Fatal("original ordering waiter is not unique")
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("original operation never reached the exact lock wait")
		}
	}
}

func runnerSafetyFacts(t *testing.T, v *runnerServiceFixture, target rc.RunnerID) [7][32]byte {
	t.Helper()
	var out [7][32]byte
	for index, table := range []string{"runners", "commands", "enrollment_tokens", "challenges", "identity_events", "connections", "audit_records"} {
		schema, column, filter := "agenteam_runner", "runner_id", ""
		if table == "runners" {
			column = "id"
		}
		if table == "audit_records" {
			schema, filter = "agenteam_audit", " AND producer='runner'"
		}
		var raw []byte
		e := v.store.QueryRow(migrationContext(t), "SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]'::jsonb) FROM "+schema+"."+table+" x WHERE "+column+"=$1::uuid"+filter, target.String()).Scan(&raw)
		requireServiceOK(t, e, "read exact owned safety facts")
		out[index] = sha256.Sum256(raw)
		clear(raw)
	}
	return out
}

func demoteRunnerSafetyUser(t *testing.T, v *runnerServiceFixture) *runnerSafetyCall[f.CommitResult] {
	t.Helper()
	user := v.actor.Details().UserID
	key, e := f.UserLock(user)
	requireServiceOK(t, e, "original User gate")
	cause, e := f.NewRecoveryCause("runner-authority-negative", serviceID[struct{}](t).String(), "")
	requireServiceOK(t, e, "external negative authority cause")
	return startRunnerSafetyCall(t, func(ctx context.Context) (f.CommitResult, error) {
		result := v.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if e := v.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
				return e
			}
			x, e := v.store.InTx(tx)
			if e != nil {
				return e
			}
			tag, e := x.Exec(ctx, `UPDATE agenteam_account.users SET role='user',version=version+1 WHERE id=$1 AND role='admin'`, user)
			if e == nil && tag.RowsAffected() != 1 {
				return f.NewFault(f.InvalidState, f.NotStarted)
			}
			return e
		})
		return result, nil
	})
}

func requireRunnerSafetyDemoted(t *testing.T, v *runnerServiceFixture, call *runnerSafetyCall[f.CommitResult]) {
	t.Helper()
	result, e := call.await(t)
	requireServiceOK(t, e, "external authority operation returned")
	if result.State() != f.Committed {
		t.Fatalf("external authority fact not known committed: state=%s", result.State())
	}
	var role string
	requireServiceOK(t, v.store.QueryRow(migrationContext(t), "SELECT role FROM agenteam_account.users WHERE id=$1", v.actor.Details().UserID).Scan(&role), "current exact User role")
	if role != "user" {
		t.Fatal("external authority fact is not current")
	}
}

func requireRunnerSafetyDenied(t *testing.T, v *runnerServiceFixture, actor id.Actor, original rc.Intent, originalKey f.IdempotencyKey, update rc.Intent) {
	t.Helper()
	before := runnerSafetyFacts(t, v, original.Target())
	for _, command := range []struct {
		intent rc.Intent
		key    f.IdempotencyKey
	}{{original, originalKey}, {update, serviceKey(t)}} {
		value, e := v.runner.Execute(migrationContext(t), actor, command.key, command.intent)
		requireServiceCode(t, e, f.Forbidden)
		if value.Material != nil || value.Receipt.CommandID.Validate() == nil {
			t.Fatal("current non-admin received a historical or new mutation")
		}
	}
	lookup, e := v.runner.Lookup(migrationContext(t), actor, originalKey, original)
	requireServiceCode(t, e, f.Forbidden)
	if lookup.Receipt != nil {
		t.Fatal("current non-admin recovered a historical receipt")
	}
	value, e := v.runner.Get(migrationContext(t), actor, original.Target())
	requireServiceCode(t, e, f.Forbidden)
	if value.ID.Validate() == nil {
		t.Fatal("current non-admin received a Reader snapshot")
	}
	page, e := v.runner.List(migrationContext(t), actor, rc.ListRequest{Limit: 10})
	requireServiceCode(t, e, f.Forbidden)
	if len(page.Items) != 0 || page.Next != nil {
		t.Fatal("current non-admin received a Reader page")
	}
	if runnerSafetyFacts(t, v, original.Target()) != before {
		t.Fatal("denied current authority changed exact Runner facts")
	}
}

func TestRunnerControlCurrentAuthorityAndCredentialInvalidation(t *testing.T) {
	for _, roleFirst := range []bool{true, false} {
		name := "command commits before external demotion"
		if roleFirst {
			name = "external demotion commits before command and lookup"
		}
		t.Run(name, func(t *testing.T) {
			v := newRunnerServiceFixture(t)
			original, originalKey, created := v.create(t, "current authority")
			target := original.Target()
			description := "authorized update"
			update, e := rc.NewUpdate(target, rc.UpdateRequest{ExpectedVersion: created.Receipt.Runner.Version, Description: &description})
			requireServiceOK(t, e, "original update intent")
			updateKey := serviceKey(t)
			before := runnerSafetyFacts(t, v, target)
			user, e := f.UserLock(v.actor.Details().UserID)
			requireServiceOK(t, e, "current User lock identity")
			connection, e := f.SystemConfigLock("runner-control-" + target.String())
			requireServiceOK(t, e, "current connection lock identity")
			var command *runnerSafetyCall[rc.Mutation]
			var demotion *runnerSafetyCall[f.CommitResult]
			var lookup *runnerSafetyCall[rc.Lookup]
			var barrier *runnerSafetyBarrier
			if roleFirst {
				barrier = newRunnerSafetyBarrier(t, v, "role", v.actor.Details().UserID)
				demotion = demoteRunnerSafetyUser(t, v)
				writer := waitRunnerSafetyLock(t, v, barrier.pid, barrier.key, f.Exclusive)
				command = startRunnerSafetyCall(t, func(ctx context.Context) (rc.Mutation, error) {
					return v.runner.Execute(ctx, v.actor, updateKey, update)
				})
				contender := waitRunnerSafetyLock(t, v, writer, user, f.Shared)
				lookup = startRunnerSafetyCall(t, func(ctx context.Context) (rc.Lookup, error) {
					return v.runner.Lookup(ctx, v.actor, originalKey, original)
				})
				waitRunnerSafetyLock(t, v, contender, connection, f.Shared)
			} else {
				barrier = newRunnerSafetyBarrier(t, v, "audit", target.String())
				command = startRunnerSafetyCall(t, func(ctx context.Context) (rc.Mutation, error) {
					return v.runner.Execute(ctx, v.actor, updateKey, update)
				})
				writer := waitRunnerSafetyLock(t, v, barrier.pid, barrier.key, f.Exclusive)
				demotion = demoteRunnerSafetyUser(t, v)
				waitRunnerSafetyLock(t, v, writer, user, f.Exclusive)
			}
			barrier.release(t)
			value, e := command.await(t)
			if roleFirst {
				requireServiceCode(t, e, f.Forbidden)
				if value.Material != nil || value.Receipt.CommandID.Validate() == nil {
					t.Fatal("write after demotion published success")
				}
				found, e := lookup.await(t)
				requireServiceCode(t, e, f.Forbidden)
				if found.Receipt != nil || runnerSafetyFacts(t, v, target) != before {
					t.Fatal("post-demotion command/lookup published or changed facts")
				}
			} else {
				requireServiceOK(t, e, "command before demotion")
				if value.Material != nil || value.Receipt.Runner.Version != 2 || value.Receipt.Runner.Description != description {
					t.Fatal("authorized pre-demotion command lost original receipt")
				}
				var version, commands, audits int
				e = v.store.QueryRow(migrationContext(t), `SELECT version,(SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE runner_id=$1 AND producer='runner') FROM agenteam_runner.runners WHERE id=$1`, target.String()).Scan(&version, &commands, &audits)
				requireServiceOK(t, e, "pre-demotion exact command facts")
				if version != 2 || commands != 2 || audits != 2 {
					t.Fatal("pre-demotion command did not commit exactly once")
				}
			}
			requireRunnerSafetyDemoted(t, v, demotion)
			requireRunnerSafetyDenied(t, v, v.actor, original, originalKey, update)
			fresh := v.login(t)
			if fresh.Details().UserID != v.actor.Details().UserID || fresh.Details().SessionID == v.actor.Details().SessionID {
				t.Fatal("ordinary User login did not retain exact User and fresh Session")
			}
			requireRunnerSafetyDenied(t, v, fresh, original, originalKey, update)
		})
	}
	t.Run("invalid enrollment never consumes current token", runnerSafetyEnrollment)
	t.Run("credential rotation and authentication original orders", runnerSafetyRotation)
}

func runnerSafetyEnrollRequest(t *testing.T, target rc.RunnerID, token p.EnrollmentToken, root string, public ed25519.PublicKey) p.EnrollmentRequest {
	t.Helper()
	request, e := p.NewEnrollmentRequest(p.ID(target.String()), token, [32]byte(public), root, "linux", "amd64")
	requireServiceOK(t, e, "owned enrollment input")
	return request
}

func runnerSafetyRejectedEnrollment(t *testing.T, v *runnerServiceFixture, target rc.RunnerID, request p.EnrollmentRequest) {
	t.Helper()
	before := runnerSafetyFacts(t, v, target)
	value, e := v.runner.Enroll(migrationContext(t), request)
	requireServiceCode(t, e, f.Unauthenticated)
	if _, e := p.EncodeEnrollmentResponse(value); e == nil || runnerSafetyFacts(t, v, target) != before {
		t.Fatal("invalid enrollment published a response or changed original facts")
	}
}

func runnerSafetyKnownEnrollment(t *testing.T, v *runnerServiceFixture, request p.EnrollmentRequest, version, generation p.Decimal) {
	t.Helper()
	response, e := v.runner.Enroll(migrationContext(t), request)
	requireServiceOK(t, e, "known current enrollment")
	if response.RunnerID() != request.RunnerID() || response.Version() != version || response.CredentialGeneration() != generation || response.PublicKeyFingerprint() != p.PublicKeyFingerprint(request.PublicKey()) {
		t.Fatal("current enrollment changed original key or generation")
	}
}

func runnerSafetyEnrollment(t *testing.T) {
	v := newRunnerServiceFixture(t)
	_, _, created := v.create(t, "invalid enrollment")
	target := created.Receipt.Runner.ID
	public, private, e := ed25519.GenerateKey(rand.Reader)
	requireServiceOK(t, e, "owned current enrollment key")
	t.Cleanup(func() { clear(private) })
	badToken, e := p.NewEnrollmentToken()
	requireServiceOK(t, e, "unissued enrollment material")
	runnerSafetyRejectedEnrollment(t, v, target, runnerSafetyEnrollRequest(t, target, created.Material.Token, "/srv/wrong", public))
	runnerSafetyRejectedEnrollment(t, v, target, runnerSafetyEnrollRequest(t, target, badToken, "/srv/runner", public))
	// The exact token denied for a wrong root is still usable at its original
	// root. Invalid attempts must not consume it or create identity/Audit facts.
	runnerSafetyKnownEnrollment(t, v, runnerSafetyEnrollRequest(t, target, created.Material.Token, "/srv/runner", public), "2", "1")
	v.snapshot(t, target, rc.Offline)
	// Keep a different original token unconsumed, so old-generation rejection
	// cannot pass merely because the earlier positive enrollment consumed it.
	_, _, unbound := v.create(t, "unconsumed replacement token")
	target = unbound.Receipt.Runner.ID
	issue, e := rc.NewEnrollment(target, rc.CredentialRequest{ExpectedVersion: 1})
	requireServiceOK(t, e, "explicit replacement enrollment intent")
	issued, e := v.runner.Execute(migrationContext(t), v.actor, serviceKey(t), issue)
	requireServiceOK(t, e, "explicit replacement enrollment")
	if issued.Material == nil || issued.Receipt.Runner.Version != 2 || issued.Receipt.Runner.CredentialGeneration != 2 {
		t.Fatal("replacement enrollment did not publish new current material")
	}
	nextPublic, nextPrivate, e := ed25519.GenerateKey(rand.Reader)
	requireServiceOK(t, e, "replacement enrollment key")
	t.Cleanup(func() { clear(nextPrivate) })
	runnerSafetyRejectedEnrollment(t, v, target, runnerSafetyEnrollRequest(t, target, unbound.Material.Token, "/srv/runner", nextPublic))
	oldHash, e := unbound.Material.Token.Digest()
	requireServiceOK(t, e, "original token digest")
	var retiredUnconsumed bool
	e = v.store.QueryRow(migrationContext(t), `SELECT consumed_at IS NULL AND revoked_at IS NOT NULL FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1 AND token_hash=$2`, target.String(), oldHash[:]).Scan(&retiredUnconsumed)
	requireServiceOK(t, e, "exact old unconsumed token retirement")
	if !retiredUnconsumed {
		t.Fatal("old token rejection did not preserve its original unconsumed/retired fact")
	}
	runnerSafetyKnownEnrollment(t, v, runnerSafetyEnrollRequest(t, target, issued.Material.Token, "/srv/runner", nextPublic), "3", "2")
	v.snapshot(t, target, rc.Offline)
}

func runnerSafetySign(t *testing.T, original p.Authentication, key ed25519.PrivateKey) p.Authentication {
	t.Helper()
	raw, e := p.SigningBytes(original.RunnerID(), original.Nonce(), original.Timestamp())
	requireServiceOK(t, e, "same issued nonce signing bytes")
	signed, e := p.NewAuthentication(original.RunnerID(), original.Nonce(), original.Timestamp(), ed25519.Sign(key, raw))
	requireServiceOK(t, e, "same issued nonce signature")
	return signed
}

func runnerSafetyRejectAuthentication(t *testing.T, v *runnerServiceFixture, target rc.RunnerID, auth p.Authentication) {
	t.Helper()
	before := runnerSafetyFacts(t, v, target)
	value, e := v.runner.Authenticate(migrationContext(t), auth)
	requireServiceCode(t, e, f.Unauthenticated)
	if value.ID() != "" || runnerSafetyFacts(t, v, target) != before {
		t.Fatal("invalidated authentication published authority or changed original facts")
	}
}

func runnerSafetyRotation(t *testing.T) {
	v := newRunnerServiceFixture(t)
	for _, issue := range []bool{false, true} {
		for _, authFirst := range []bool{false, true} {
			name := "revoke"
			if issue {
				name = "reissue"
			}
			if authFirst {
				name += " after original authentication"
			} else {
				name += " before original authentication"
			}
			t.Run(name, func(t *testing.T) {
				_, _, created := v.create(t, name)
				target := created.Receipt.Runner.ID
				private, _ := v.enroll(t, created)
				auth := v.authentication(t, target, private)
				intent, e := rc.NewRevoke(target, rc.CredentialRequest{ExpectedVersion: 2})
				if issue {
					intent, e = rc.NewEnrollment(target, rc.CredentialRequest{ExpectedVersion: 2})
				}
				requireServiceOK(t, e, "original credential rotation intent")
				key := serviceKey(t)
				connectionKey, e := f.SystemConfigLock("runner-control-" + target.String())
				requireServiceOK(t, e, "original credential serialization key")
				kind := "audit"
				if authFirst {
					kind = "connection"
				}
				barrier := newRunnerSafetyBarrier(t, v, kind, target.String())
				var authentication *runnerSafetyCall[service.Connection]
				var rotation *runnerSafetyCall[rc.Mutation]
				if authFirst {
					authentication = startRunnerSafetyCall(t, func(ctx context.Context) (service.Connection, error) { return v.runner.Authenticate(ctx, auth) })
					writer := waitRunnerSafetyLock(t, v, barrier.pid, barrier.key, f.Exclusive)
					rotation = startRunnerSafetyCall(t, func(ctx context.Context) (rc.Mutation, error) { return v.runner.Execute(ctx, v.actor, key, intent) })
					waitRunnerSafetyLock(t, v, writer, connectionKey, f.Exclusive)
				} else {
					rotation = startRunnerSafetyCall(t, func(ctx context.Context) (rc.Mutation, error) { return v.runner.Execute(ctx, v.actor, key, intent) })
					writer := waitRunnerSafetyLock(t, v, barrier.pid, barrier.key, f.Exclusive)
					authentication = startRunnerSafetyCall(t, func(ctx context.Context) (service.Connection, error) { return v.runner.Authenticate(ctx, auth) })
					waitRunnerSafetyLock(t, v, writer, connectionKey, f.Exclusive)
				}
				barrier.release(t)
				connection, authErr := authentication.await(t)
				rotated, e := rotation.await(t)
				requireServiceOK(t, e, "known original credential rotation")
				if rotated.Receipt.Runner.Version != 3 || rotated.Receipt.Runner.CredentialGeneration != 2 || rotated.Receipt.Runner.PublicKeyFingerprint != nil || (rotated.Material != nil) != issue {
					t.Fatal("credential rotation did not retire original key and generation")
				}
				if authFirst {
					requireServiceOK(t, authErr, "known authentication before rotation")
					if connection.ID() == "" {
						t.Fatal("known authentication omitted original reservation")
					}
					called := false
					e := v.runner.WithCurrentConnection(migrationContext(t), connection, func(context.Context) error { called = true; return nil })
					requireServiceCode(t, e, f.Unauthenticated)
					if called {
						t.Fatal("old authenticated generation invoked current writer")
					}
				} else {
					requireServiceCode(t, authErr, f.Unauthenticated)
					if connection.ID() != "" {
						t.Fatal("authentication after rotation published a reservation")
					}
				}
				var version, generation, sequence, current, commands, events, audits, tokens, retiredTokens, consumed, revoked int
				e = v.store.QueryRow(migrationContext(t), `SELECT version,credential_generation,connection_generation,
 (SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1 AND revoked_at IS NULL AND consumed_at IS NULL),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1 AND revoked_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1 AND consumed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1 AND revoked_at IS NOT NULL)
 FROM agenteam_runner.runners WHERE id=$1`, target.String()).Scan(&version, &generation, &sequence, &current, &commands, &events, &audits, &tokens, &retiredTokens, &consumed, &revoked)
				requireServiceOK(t, e, "exact current rotation facts")
				wantSequence, wantEvents, wantTokens := 0, 3, 0
				if authFirst {
					wantSequence = 1
				}
				if issue {
					wantEvents, wantTokens = 4, 1
				}
				if version != 3 || generation != 2 || sequence != wantSequence || current != 0 || commands != 2 || events != wantEvents || audits != wantEvents || tokens != wantTokens || retiredTokens != 1 || consumed != wantSequence || revoked != 1 {
					t.Fatal("credential/authentication order lost exact durable facts")
				}
				runnerSafetyRejectAuthentication(t, v, target, auth)
				v.snapshot(t, target, rc.Offline)
				if !issue {
					request, e := p.NewChallengeRequest(p.ID(target.String()))
					requireServiceOK(t, e, "revoked challenge input")
					value, e := v.runner.Challenge(migrationContext(t), request)
					requireServiceCode(t, e, f.Unauthenticated)
					if value.Nonce().Valid() {
						t.Fatal("revoked key obtained a current nonce")
					}
					return
				}
				nextPublic, nextPrivate, e := ed25519.GenerateKey(rand.Reader)
				requireServiceOK(t, e, "new current key")
				t.Cleanup(func() { clear(nextPrivate) })
				runnerSafetyRejectedEnrollment(t, v, target, runnerSafetyEnrollRequest(t, target, created.Material.Token, "/srv/runner", nextPublic))
				runnerSafetyKnownEnrollment(t, v, runnerSafetyEnrollRequest(t, target, rotated.Material.Token, "/srv/runner", nextPublic), "4", "2")
				currentAuth := v.authentication(t, target, nextPrivate)
				runnerSafetyRejectAuthentication(t, v, target, runnerSafetySign(t, currentAuth, private))
				currentConnection, e := v.runner.Authenticate(migrationContext(t), currentAuth)
				requireServiceOK(t, e, "same original nonce with new current key")
				requireServiceOK(t, v.runner.OwnConnection(migrationContext(t), currentConnection, func(context.Context) error { return nil }), "new current owner actual retirement")
				var left int
				requireServiceOK(t, v.store.QueryRow(migrationContext(t), "SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1", target.String()).Scan(&left), "new current connection retirement facts")
				if left != 0 {
					t.Fatal("new current owner retained a connection")
				}
			})
		}
	}
}
