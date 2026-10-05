//go:build integration

package account_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func TestAccountProfileCurrentMutationReplayAndVersion(t *testing.T) {
	f := newAvatarFixture(t)
	current, e := f.profiles.GetProfile(ctxFor(t), f.actor)
	if e != nil || current.User.Username != "admin" || current.Avatar != nil {
		t.Fatal("initial", current, e)
	}
	username, display := "changed-admin", "First display"
	request := c.ProfileChange{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "profile-change", ExpectedVersion: current.User.Version}, Username: &username, DisplayName: &display}
	got, e := f.profiles.UpdateProfile(ctxFor(t), request)
	if e != nil {
		t.Fatalf("update %v [%s]", e, safeFailure(e))
	}
	if got.User.Username != username || got.User.DisplayName != display || got.User.Version != current.User.Version+1 || got.User.Email != current.User.Email {
		t.Fatal("profile projection", got)
	}
	again, e := f.profiles.UpdateProfile(ctxFor(t), request)
	if e != nil || again.User.Version != got.User.Version {
		t.Fatal("replay", again, e)
	}
	display = "different"
	_, e = f.profiles.UpdateProfile(ctxFor(t), request)
	requireAvatarFault(t, e, foundation.IdempotencyKeyReused)
	request.Key = "version-conflict"
	_, e = f.profiles.UpdateProfile(ctxFor(t), request)
	requireAvatarFault(t, e, foundation.VersionConflict)
	theme, e := f.profiles.SetTheme(ctxFor(t), c.ThemeChange{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "theme", ExpectedVersion: got.User.Version}, Theme: c.DarkTheme})
	if e != nil || theme.User.Theme != c.DarkTheme {
		t.Fatal("theme", e)
	}
	var count int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.profile.update'`).Scan(&count); e != nil || count != 2 {
		t.Fatal("audit", count, e)
	}
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, f.actor.Details().SessionID); e != nil {
		t.Fatal(e)
	}
	_, e = f.profiles.UpdateProfile(ctxFor(t), c.ProfileChange{ProfileMutation: request.ProfileMutation, Username: &username})
	requireAvatarFault(t, e, foundation.SessionRevoked)
}

func TestAccountCurrentUserRouteTransactionAndCurrentMapping(t *testing.T) {
	f := newAvatarFixture(t)
	if _, e := f.authority.CurrentUserRouteInTx(ctxFor(t), foundation.Tx{}, f.actor); e == nil {
		t.Fatal("zero Tx opened a read")
	}
	key, _ := foundation.UserLock(f.actor.Details().UserID)
	var ended foundation.Tx
	for _, mode := range []foundation.LockMode{foundation.Shared, foundation.Exclusive} {
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			ended = tx
			if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: mode}}); e != nil {
				return e
			}
			route, e := f.authority.CurrentUserRouteInTx(ctx, tx, f.actor)
			if e == nil && (route.Username != "admin" || route.UserID.String() != f.actor.Details().UserID || route.Version != 1) {
				t.Fatal("canonical bootstrap route", route)
			}
			return e
		})
		if result.State() != foundation.Committed {
			t.Fatal("held route", result.Fault())
		}
	}
	if _, e := f.authority.CurrentUserRouteInTx(ctxFor(t), ended, f.actor); e == nil {
		t.Fatal("ended Tx accepted")
	}
	other := openStore(t, f.db.Config(t, nil))
	r := other.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := other.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); e != nil {
			return e
		}
		_, e := f.authority.CurrentUserRouteInTx(ctx, tx, f.actor)
		if e == nil {
			t.Fatal("foreign Store handle accepted")
		}
		return e
	})
	if r.State() == foundation.Committed {
		t.Fatal("foreign transaction committed")
	}
	r = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		_, e := f.authority.CurrentUserRouteInTx(ctx, tx, f.actor)
		if e == nil {
			t.Fatal("missing User lock accepted")
		}
		return nil
	})
	if r.State() == foundation.Committed {
		t.Fatal("ignored missing-lock failure did not poison")
	}
	name := "current-route"
	view, e := f.profiles.UpdateProfile(ctxFor(t), c.ProfileChange{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "rename-route", ExpectedVersion: 1}, Username: &name})
	if e != nil {
		t.Fatal(e)
	}
	r = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); e != nil {
			return e
		}
		route, e := f.authority.CurrentUserRouteInTx(ctx, tx, f.actor)
		if e == nil && (route.Username != name || route.Version != view.User.Version) {
			t.Fatal("route retained stale name")
		}
		return e
	})
	if r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	wrongUser := id[identity.User](t)
	session, _ := foundation.ParseID[identity.Session](f.actor.Details().SessionID)
	wrong, _ := identity.NewHuman(wrongUser, session)
	if _, e = f.profiles.GetProfile(ctxFor(t), wrong); e == nil {
		t.Fatal("Session authorized another User")
	}
}

func TestAccountProfileConcurrentVersionAndCanonicalReplay(t *testing.T) {
	f := newAvatarFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var work sync.WaitGroup
	for _, key := range []string{"first", "second"} {
		work.Add(1)
		go func(key string) {
			defer work.Done()
			<-start
			_, e := f.profiles.SetTheme(ctxFor(t), c.ThemeChange{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: foundation.IdempotencyKey(key), ExpectedVersion: 1}, Theme: c.DarkTheme})
			results <- e
		}(key)
	}
	close(start)
	work.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if hasCode(e, foundation.VersionConflict) {
			conflict++
		} else {
			t.Fatal("concurrent profile", e, safeFailure(e))
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("version race", success, conflict)
	}
	var commands, audits int
	if e := f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.commands WHERE command_name='profile-update'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.profile.update')`).Scan(&commands, &audits); e != nil || commands != 1 || audits != 1 {
		t.Fatal("loser left facts", commands, audits, e)
	}
	upper := "New-Route"
	request := c.ProfileChange{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "canonical-username", ExpectedVersion: 2}, Username: &upper}
	v, e := f.profiles.UpdateProfile(ctxFor(t), request)
	if e != nil || v.User.Username != "new-route" {
		t.Fatal("canonical update", e)
	}
	lower := "new-route"
	request.Username = &lower
	replay, e := f.profiles.UpdateProfile(ctxFor(t), request)
	if e != nil || replay.User.Version != v.User.Version {
		t.Fatal("canonical same-command replay", e)
	}
}

func TestAccountProfileUsernameUniquenessPreservesBothUsers(t *testing.T) {
	f := newAvatarFixture(t)
	other, _ := invitedMailUser(t, f.b02Fixture, f.actor, "profile-other@example.test")
	before, e := f.profiles.GetProfile(ctxFor(t), f.actor)
	if e != nil {
		t.Fatal(e)
	}
	otherBefore, e := f.profiles.GetProfile(ctxFor(t), other)
	if e != nil {
		t.Fatal(e)
	}
	username := "MAIL-TARGET"
	_, e = f.profiles.UpdateProfile(ctxFor(t), c.ProfileChange{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "existing-username", ExpectedVersion: before.User.Version}, Username: &username})
	var failure *foundation.Fault
	if !errors.As(e, &failure) || failure.Code != foundation.InvalidArgument || failure.CommitState != foundation.NotCommitted || len(failure.FieldErrors) != 1 || failure.FieldErrors[0].Path != "/username" || failure.FieldErrors[0].Code != "ALREADY_EXISTS" {
		t.Fatal("case-normalized uniqueness", e, safeFailure(e))
	}
	after, e := f.profiles.GetProfile(ctxFor(t), f.actor)
	if e != nil || after.User != before.User {
		t.Fatal("failed rename changed current User", e)
	}
	otherAfter, e := f.profiles.GetProfile(ctxFor(t), other)
	if e != nil || otherAfter.User != otherBefore.User {
		t.Fatal("failed rename changed existing User", e)
	}
	var commands, audits int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.commands WHERE command_name='profile-update'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.profile.update')`).Scan(&commands, &audits); e != nil || commands != 0 || audits != 0 {
		t.Fatal("failed uniqueness left command or Audit", commands, audits, e)
	}
	t.Run("different_current_users_same_canonical_name", func(t *testing.T) {
		upper, lower := "Fresh-Route", "fresh-route"
		requests := []c.ProfileChange{
			{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "competing-name-admin", ExpectedVersion: before.User.Version}, Username: &upper},
			{ProfileMutation: c.ProfileMutation{Actor: other, Key: "competing-name-other", ExpectedVersion: otherBefore.User.Version}, Username: &lower},
		}
		prior := []c.ProfileView{before, otherBefore}
		type result struct {
			n    int
			view c.ProfileView
			err  error
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		ctx := ctxFor(t)
		for n, request := range requests {
			go func() {
				<-start
				view, e := f.profiles.UpdateProfile(ctx, request)
				results <- result{n, view, e}
			}()
		}
		close(start)
		winner, loser := -1, -1
		for range 2 {
			r := <-results
			if r.err == nil {
				if winner != -1 || r.view.User.Username != lower || r.view.User.Version != prior[r.n].User.Version+1 {
					t.Fatal("two users acquired the same canonical username")
				}
				winner = r.n
				continue
			}
			var failure *foundation.Fault
			if loser != -1 || !errors.As(r.err, &failure) || failure.Code != foundation.InvalidArgument || failure.CommitState != foundation.NotCommitted || len(failure.FieldErrors) != 1 || failure.FieldErrors[0].Path != "/username" || failure.FieldErrors[0].Code != "ALREADY_EXISTS" {
				t.Fatal("competing unique-name result", r.err, safeFailure(r.err))
			}
			loser = r.n
		}
		if winner == -1 || loser == -1 {
			t.Fatal("canonical competition did not have exactly one winner")
		}
		current, e := f.profiles.GetProfile(ctxFor(t), requests[loser].Actor)
		if e != nil || current.User != prior[loser].User {
			t.Fatal("losing user's original identity changed", e)
		}
		var held, loserCommands, loserAudits int
		if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.users WHERE username=$1),(SELECT count(*) FROM agenteam_account.commands WHERE command_name='profile-update' AND owner_id=$2),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.profile.update' AND resource_id=$2)`, lower, requests[loser].Actor.Details().UserID).Scan(&held, &loserCommands, &loserAudits); e != nil || held != 1 || loserCommands != 0 || loserAudits != 0 {
			t.Fatal("losing username command left receipt/Audit", held, loserCommands, loserAudits, e)
		}
		if _, e = f.profiles.UpdateProfile(ctxFor(t), requests[winner]); e != nil {
			t.Fatal("committed username receipt did not replay", e)
		}
		if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.commands WHERE command_name='profile-update'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='account.profile.update')`).Scan(&commands, &audits); e != nil || commands != 1 || audits != 1 {
			t.Fatal("username replay duplicated receipt/Audit", commands, audits, e)
		}
	})
}

type profileCommitBarrier struct {
	*postgres.Store
	armed, fired atomic.Bool
	held         chan int32
	release      chan struct{}
}

func (w *profileCommitBarrier) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return w.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if !w.armed.Load() || w.fired.Load() {
			return nil
		}
		x, e := w.InTx(tx)
		if e != nil {
			return e
		}
		var ready bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands WHERE command_key='held-profile-rename' AND command_name='profile-update' AND phase='committed')`).Scan(&ready); e != nil {
			return e
		}
		if ready && w.fired.CompareAndSwap(false, true) {
			var pid int32
			if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				return e
			}
			w.held <- pid
			select {
			case <-w.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
}

func TestAccountCurrentUserRouteWaitsActualRenameWriter(t *testing.T) {
	db, _, _ := database(t)
	raw := openStore(t, db.Config(t, nil))
	w := &profileCommitBarrier{Store: raw, held: make(chan int32, 1), release: make(chan struct{})}
	k, _, _ := keys(t)
	authority, e := account.NewAuthority(w, k)
	if e != nil {
		t.Fatal(e)
	}
	f := assembleAvatarFixture(t, assembleB02(t, db, raw, w, authority, nil))
	var once sync.Once
	release := func() { once.Do(func() { close(w.release) }) }
	defer release()
	w.armed.Store(true)
	rename := make(chan error, 1)
	go func() {
		name := "renamed-under-lock"
		_, e := f.profiles.UpdateProfile(ctxFor(t), c.ProfileChange{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: "held-profile-rename", ExpectedVersion: 1}, Username: &name})
		rename <- e
	}()
	var holder int32
	select {
	case holder = <-w.held:
	case <-time.After(3 * time.Second):
		t.Fatal("actual rename transaction did not reach commit barrier")
	}
	key, _ := foundation.UserLock(f.actor.Details().UserID)
	read := make(chan foundation.CommitResult, 1)
	var route c.UserRoute
	go func() {
		read <- f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if e := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); e != nil {
				return e
			}
			var e error
			route, e = f.authority.CurrentUserRouteInTx(ctx, tx, f.actor)
			return e
		})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	hash := uint64(key.AdvisoryKey())
	for {
		var blocked bool
		e = raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid WHERE w.locktype='advisory' AND w.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND NOT w.granted AND w.mode='ShareLock' AND h.granted AND h.mode='ExclusiveLock' AND h.pid=$1 AND h.pid=ANY(pg_blocking_pids(w.pid)) AND w.classid::bigint=$2 AND w.objid::bigint=$3)`, holder, int64(uint32(hash>>32)), int64(uint32(hash))).Scan(&blocked)
		if e != nil {
			t.Fatal("exact User holder/waiter observation", e)
		}
		if blocked {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("route did not wait on actual rename's exact User EX")
		}
	}
	release()
	if e = <-rename; e != nil {
		t.Fatal("rename commit", e, safeFailure(e))
	}
	if result := <-read; result.State() != foundation.Committed || route.Username != "renamed-under-lock" || route.Version != 2 {
		t.Fatal("route read stale mapping after writer", result.State(), result.Fault(), route)
	}
}
