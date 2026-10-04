//go:build integration

package account_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/jackc/pgx/v5/pgconn"
)

type capturedLogout struct {
	event   event.Event
	plan    oc.AppendPlan
	release chan struct{}
}

type logoutPlanBarrier struct {
	oc.Appender
	target  string
	active  atomic.Bool
	calls   atomic.Int64
	arrived chan capturedLogout
}

func (b *logoutPlanBarrier) PrepareAppend(ctx context.Context, actor identity.Actor, evt event.Event) (oc.AppendPlan, error) {
	plan, e := b.Appender.PrepareAppend(ctx, actor, evt)
	if e != nil {
		return plan, e
	}
	b.calls.Add(1)
	if b.active.Load() && actor.Details().SessionID == b.target {
		v := capturedLogout{evt, plan, make(chan struct{})}
		select {
		case b.arrived <- v:
		case <-ctx.Done():
			return oc.AppendPlan{}, ctx.Err()
		}
		select {
		case <-v.release:
		case <-ctx.Done():
			return oc.AppendPlan{}, ctx.Err()
		}
	}
	return plan, nil
}

// Arms only the existing real proxy, after this exact command's real callback.
// Matching the target key avoids intercepting the already-committed competitor.
type logoutSequenceStore struct {
	*postgres.Store
	proxy          *commitProxy
	key, phase     string
	enabled, fired atomic.Bool
}

func (s *logoutSequenceStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if !s.enabled.Load() || s.fired.Load() {
			return nil
		}
		x, e := s.InTx(tx)
		if e != nil {
			return e
		}
		var match bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_name='logout' AND command_key=$1 AND phase=$2 AND (event_header->>'aggregate_sequence')::bigint=3)`, s.key, s.phase).Scan(&match); e != nil {
			return e
		}
		if match && s.fired.CompareAndSwap(false, true) {
			s.proxy.armed.Store(true)
		}
		return nil
	})
}

func newLogoutSequenceFixture(t *testing.T, commit *bool) (*fixture, *logoutPlanBarrier, *logoutSequenceStore, *commitProxy) {
	t.Helper()
	db, raw, authority := database(t)
	var store accountTestStore = raw
	var wrapper *logoutSequenceStore
	var proxy *commitProxy
	if commit != nil {
		proxy = newCommitProxy(t, net.JoinHostPort("127.0.0.1", db.Fixture.Port), *commit)
		u, e := url.Parse(db.Fixture.URL(db.Name))
		if e != nil {
			t.Fatal(e)
		}
		u.Host = proxy.listener.Addr().String()
		raw = openStore(t, db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
		wrapper = &logoutSequenceStore{Store: raw, proxy: proxy}
		store = wrapper
		keyring, _, _ := keys(t)
		authority, e = account.NewAuthority(store, keyring)
		if e != nil {
			t.Fatal(e)
		}
	}
	_, cursor, master := keys(t)
	aud, e := audit.New(store, cursor, audit.Authorizations{Sessions: authority, System: authority, Accounts: authority})
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.New(store, master, aud, secret.Authorizations{Sessions: authority, System: authority, Usage: authority, AccountWrites: authority})
	if e != nil {
		t.Fatal(e)
	}
	if e = secrets.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	log := filepath.Join(dir, "recovery.jsonl")
	sink, e := recoverylog.Open(log)
	if e != nil {
		t.Fatal(e)
	}
	catalog := event.NewCatalog()
	revoked, e := c.DefineSessionsRevoked(catalog)
	if e != nil {
		t.Fatal(e)
	}
	events, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.AccountProducer: authority}, Sessions: authority, System: authority, Audit: aud, Cursors: cursor})
	if e != nil {
		t.Fatal(e)
	}
	barrier := &logoutPlanBarrier{Appender: events, arrived: make(chan capturedLogout, 1)}
	service, e := account.New(account.Dependencies{Authority: authority, Audit: aud, Secrets: secrets, Events: barrier, SessionsRevoked: revoked, Processes: liveProcess{id[c.Process](t)}, RecoveryLog: sink})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := service.Force(ctx); e != nil && !service.Joined() {
			t.Error(e)
		}
	})
	return &fixture{db: db, service: service, store: raw, authority: authority, secrets: secrets, log: log}, barrier, wrapper, proxy
}

func capturedLogoutPlan(t *testing.T, b *logoutPlanBarrier) capturedLogout {
	t.Helper()
	select {
	case got := <-b.arrived:
		return got
	case <-time.After(4 * time.Second):
		t.Fatal("real PrepareAppend barrier timeout")
		return capturedLogout{}
	}
}

func logoutResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case e := <-done:
		return e
	case <-time.After(4 * time.Second):
		t.Fatal("logout operation did not join")
		return nil
	}
}

type logoutSnapshot struct {
	id, eventID, digest, identity, phase string
	payload, mac, headerBytes            []byte
	header                               event.Header
}

func snapshotLogout(t *testing.T, f *fixture, r account.LogoutRequest) logoutSnapshot {
	t.Helper()
	var s logoutSnapshot
	e := f.store.QueryRow(ctxFor(t), `SELECT id::text,event_id::text,event_digest,identity_digest,phase,event_payload,semantic_mac,event_header FROM agenteam_account.commands WHERE command_name='logout' AND command_key=$1`, string(r.Key)).Scan(&s.id, &s.eventID, &s.digest, &s.identity, &s.phase, &s.payload, &s.mac, &s.headerBytes)
	if e != nil {
		t.Fatal(e)
	}
	s.header, e = event.DecodeHeader(s.headerBytes)
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func sameLogoutIdentity(t *testing.T, before, after logoutSnapshot) {
	t.Helper()
	if before.id != after.id || before.eventID != after.eventID || before.digest != after.digest || before.identity != after.identity || !bytes.Equal(before.payload, after.payload) || !bytes.Equal(before.mac, after.mac) || before.header.OccurredAt != after.header.OccurredAt {
		t.Fatal("replanning changed durable command/event identity, payload or occurrence time")
	}
}

func logoutSequenceFacts(t *testing.T, f *fixture, want int64) {
	t.Helper()
	var count, distinct, minimum, maximum, audits, revoked, sequence int64
	e := f.store.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_outbox.events),
 (SELECT count(DISTINCT aggregate_sequence) FROM agenteam_outbox.events),
 (SELECT min(aggregate_sequence) FROM agenteam_outbox.events),
 (SELECT max(aggregate_sequence) FROM agenteam_outbox.events),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.logout'),
 (SELECT count(*) FROM agenteam_account.sessions WHERE revoked_at IS NOT NULL),
 (SELECT auth_sequence FROM agenteam_account.users)`).Scan(&count, &distinct, &minimum, &maximum, &audits, &revoked, &sequence)
	if e != nil || count != want || distinct != want || minimum != 2 || maximum != want+1 || audits != want || revoked != want || sequence != want+1 {
		t.Fatal("logout effects duplicated, split or regressed", count, distinct, minimum, maximum, audits, revoked, sequence, e)
	}
}

func conflictLogout(t *testing.T, f *fixture, b *logoutPlanBarrier) (account.LogoutRequest, []identity.Actor, capturedLogout, logoutSnapshot) {
	t.Helper()
	password := f.bootstrap(t)
	actors := make([]identity.Actor, 3)
	for i := range actors {
		r, _ := loginRequest(t, f, password, "admin@mail.com")
		response, e := f.service.Login(ctxFor(t), r)
		if e != nil {
			t.Fatal(e)
		}
		cookie := useCookie(t, response)
		if e = response.Close(ctxFor(t)); e != nil {
			t.Fatal(e)
		}
		actors[i], e = f.service.Authenticate(ctxFor(t), cookie)
		if e != nil {
			t.Fatal(e)
		}
	}
	for range 2 {
		if _, e := f.service.Recover(ctxFor(t)); e != nil {
			t.Fatal(e)
		}
	}
	first := account.LogoutRequest{Actor: actors[0], Key: foundation.IdempotencyKey(id[struct{}](t).String())}
	b.target = actors[0].Details().SessionID
	b.active.Store(true)
	done := make(chan error, 1)
	go func() { done <- f.service.Logout(ctxFor(t), first) }()
	old := capturedLogoutPlan(t, b)
	before := snapshotLogout(t, f, first)
	if *before.header.AggregateSequence != 2 || before.phase != "planned" {
		t.Fatal("first real plan differs")
	}
	second := account.LogoutRequest{Actor: actors[1], Key: foundation.IdempotencyKey(id[struct{}](t).String())}
	if e := f.service.Logout(ctxFor(t), second); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	close(old.release)
	if e := logoutResult(t, done); !hasCode(e, foundation.ResourceBusy) {
		t.Fatal("first conflict must be bounded, with no automatic retry", e, safeFailure(e))
	}
	if e := f.authority.RequireCurrentSession(ctxFor(t), foundation.Tx{}, actors[0]); e != nil {
		t.Fatal(e)
	}
	logoutSequenceFacts(t, f, 1)
	return first, actors, old, before
}

func TestAccountLogoutReplansOnlyHeaderAndRejectsOldPrepared(t *testing.T) {
	f, b, _, _ := newLogoutSequenceFixture(t, nil)
	r, actors, old, before := conflictLogout(t, f, b)
	done := make(chan error, 1)
	go func() { done <- f.service.Logout(ctxFor(t), r) }()
	current := capturedLogoutPlan(t, b)
	after := snapshotLogout(t, f, r)
	sameLogoutIdentity(t, before, after)
	if *after.header.AggregateSequence != 3 || int64(*after.header.AggregateVersion) != 3 || after.phase != "planned" {
		t.Fatal("retry did not actually replan the header")
	}
	command, e := foundation.NewCommandIdentity("account.logout", []string{r.Actor.Details().UserID}, "logout", r.Key)
	if e != nil {
		t.Fatal(e)
	}
	cause, e := foundation.NewCommandsCause(command)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		event event.Event
		code  foundation.Code
	}{{old.event, foundation.Forbidden}, {current.event, foundation.InvalidArgument}} {
		result := f.store.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := f.store.AcquireAll(ctx, tx, old.plan.Locks()); e != nil {
				return e
			}
			_, e := b.Appender.AppendEventInTx(ctx, tx, r.Actor, tc.event, old.plan)
			return e
		})
		if result.State() != foundation.NotCommitted || !hasCode(result.Fault(), tc.code) {
			t.Fatal("old prepared binding was accepted", result.State(), result.Fault())
		}
	}
	// A different still-current Session cannot use this command key to change
	// its planned payload/header, even though both Sessions have the same User.
	wrong := account.LogoutRequest{Actor: actors[2], Key: r.Key}
	if e = f.service.Logout(ctxFor(t), wrong); !hasCode(e, foundation.IdempotencyKeyReused) {
		t.Fatal("current but different original semantics accepted", e)
	}
	unchanged := snapshotLogout(t, f, r)
	if !bytes.Equal(after.headerBytes, unchanged.headerBytes) {
		t.Fatal("rejected request rewrote the planned header")
	}
	logoutSequenceFacts(t, f, 1)
	close(current.release)
	if e = logoutResult(t, done); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	final := snapshotLogout(t, f, r)
	sameLogoutIdentity(t, before, final)
	if final.phase != "committed" || !bytes.Equal(after.headerBytes, final.headerBytes) {
		t.Fatal("publication changed the newly planned event")
	}
	logoutSequenceFacts(t, f, 2)
	if e = f.service.Logout(ctxFor(t), r); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("committed logout replay bypassed current revocation", e)
	}
	if !bytes.Equal(final.headerBytes, snapshotLogout(t, f, r).headerBytes) {
		t.Fatal("committed header was replanned")
	}
	logoutSequenceFacts(t, f, 2)
}

func TestAccountLogoutReplanKeepsCurrentRevocationGate(t *testing.T) {
	f, b, _, _ := newLogoutSequenceFixture(t, nil)
	r, _, _, before := conflictLogout(t, f, b)
	done := make(chan error, 1)
	go func() { done <- f.service.Logout(ctxFor(t), r) }()
	prepared := capturedLogoutPlan(t, b)
	planned := snapshotLogout(t, f, r)
	b.active.Store(false)
	other := account.LogoutRequest{Actor: r.Actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}
	if e := f.service.Logout(ctxFor(t), other); e != nil {
		t.Fatal(e)
	}
	close(prepared.release)
	if e := logoutResult(t, done); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("late publication ignored current revocation", e)
	}
	if e := f.service.Logout(ctxFor(t), r); !hasCode(e, foundation.SessionRevoked) {
		t.Fatal("revoked Session was revived by replanning", e)
	}
	after := snapshotLogout(t, f, r)
	sameLogoutIdentity(t, before, after)
	if after.phase != "planned" || !bytes.Equal(planned.headerBytes, after.headerBytes) {
		t.Fatal("rejected late request changed its planned checkpoint")
	}
	logoutSequenceFacts(t, f, 2)
}

func TestAccountLogoutReplanUnknownSerializesBeforeRetry(t *testing.T) {
	for _, phase := range []string{"planned", "committed"} {
		for _, commit := range []bool{true, false} {
			name := phase + "/late_commit"
			if !commit {
				name = phase + "/rollback"
			}
			t.Run(name, func(t *testing.T) {
				f, b, wrapper, proxy := newLogoutSequenceFixture(t, &commit)
				r, _, _, before := conflictLogout(t, f, b)
				b.active.Store(false)
				wrapper.key, wrapper.phase = string(r.Key), phase
				wrapper.enabled.Store(true)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- f.service.Logout(ctx, r) }()
				await(t, proxy.reached)
				e := logoutResult(t, done)
				var fault *foundation.Fault
				if !errors.As(e, &fault) || fault == nil || fault.Code != foundation.CommitUnknown || fault.CommitState != foundation.Unknown || ctx.Err() != nil || !wrapper.fired.Load() {
					t.Fatal("original unknown classification lost", e, safeFailure(e))
				}
				if phase == "committed" {
					var pg *pgconn.PgError
					if !errors.As(e, &pg) || pg == nil || pg.Code != "55P03" {
						t.Fatal("final confirmation did not observe real writer lock", e, safeFailure(e))
					}
				}
				held := snapshotLogout(t, f, r)
				calls := b.calls.Load()
				e = f.service.Logout(ctxFor(t), r)
				var pg *pgconn.PgError
				if e == nil || !errors.As(e, &pg) || pg == nil || pg.Code != "55P03" || calls != b.calls.Load() {
					t.Fatal("retry passed an unresolved original writer", e, safeFailure(e))
				}
				if !bytes.Equal(held.headerBytes, snapshotLogout(t, f, r).headerBytes) {
					t.Fatal("unknown writer checkpoint was rewritten")
				}
				logoutSequenceFacts(t, f, 1)
				close(proxy.release)
				await(t, proxy.completed)
				e = f.service.Logout(ctxFor(t), r)
				if phase == "committed" && commit {
					if !hasCode(e, foundation.SessionRevoked) {
						t.Fatal("committed unknown replay ignored revocation", e)
					}
				} else if e != nil {
					t.Fatal("resolved checkpoint did not retry", e, safeFailure(e))
				}
				final := snapshotLogout(t, f, r)
				sameLogoutIdentity(t, before, final)
				if final.phase != "committed" || *final.header.AggregateSequence != 3 || int64(*final.header.AggregateVersion) != 3 {
					t.Fatal("retry changed or lost final sequence")
				}
				logoutSequenceFacts(t, f, 2)
				if e = f.service.Logout(ctxFor(t), r); !hasCode(e, foundation.SessionRevoked) {
					t.Fatal("final receipt was replayed without current authority", e)
				}
				logoutSequenceFacts(t, f, 2)
			})
		}
	}
}
