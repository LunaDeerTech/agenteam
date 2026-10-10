//go:build integration

package projectvariable_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type variableReply struct {
	value vc.VariableMutation
	err   error
}

func asyncVariable(t *testing.T, fn func(context.Context) (vc.VariableMutation, error)) <-chan variableReply {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan variableReply, 1)
	done := make(chan struct{})
	go func() { defer close(done); v, e := fn(ctx); out <- variableReply{v, e} }()
	t.Cleanup(func() { cancel(); await(t, done) })
	return out
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("actual owned call/stage did not join")
	}
}
func reply(t *testing.T, ch <-chan variableReply) variableReply {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("variable call did not return")
	}
	return variableReply{}
}
func awaitStage(t *testing.T, stage <-chan struct{}, result <-chan variableReply) {
	t.Helper()
	select {
	case <-stage:
	case early := <-result:
		t.Fatal("call ended before real observed stage", early.err)
	case <-time.After(10 * time.Second):
		t.Fatal("real stage not reached")
	}
}

type lockAttempt struct {
	pid     int32
	request f.LockRequest
}

func observeLock(s *hookStore, key f.LockKey) <-chan lockAttempt {
	ch := make(chan lockAttempt, 1)
	var once atomic.Bool
	s.mu.Lock()
	s.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
		for _, request := range locks {
			if f.CompareLockKeys(request.Key, key) != 0 || !once.CompareAndSwap(false, true) {
				continue
			}
			x, e := s.InTx(tx)
			if e != nil {
				return e
			}
			var pid int32
			if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				return e
			}
			ch <- lockAttempt{pid, request}
			break
		}
		return nil
	}
	s.mu.Unlock()
	return ch
}
func waitAttempt(t *testing.T, ch <-chan lockAttempt, early <-chan variableReply) lockAttempt {
	t.Helper()
	select {
	case got := <-ch:
		if got.pid <= 0 {
			t.Fatal("missing actual caller PID")
		}
		return got
	case got := <-early:
		t.Fatal("caller returned before lock", got.err)
	case <-time.After(5 * time.Second):
		t.Fatal("caller failed to request exact lock")
	}
	return lockAttempt{}
}
func (v *variableHTTPFixture) waitLock(t *testing.T, a lockAttempt, granted bool, blocker int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mode := "ExclusiveLock"
	if a.request.Mode == f.Shared {
		mode = "ShareLock"
	}
	n := uint64(a.request.Key.AdvisoryKey())
	for {
		var matched bool
		e := v.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=$1 AND l.locktype='advisory' AND l.classid::bigint=$2 AND l.objid::bigint=$3 AND l.objsubid=1 AND l.pid=$4 AND l.mode=$5 AND l.granted=$6 AND $7::int=ANY(pg_blocking_pids(l.pid)))`, v.db.Name, int64(n>>32), int64(n&0xffffffff), a.pid, mode, granted, blocker).Scan(&matched)
		if e != nil {
			t.Fatal("exact lock observation", e)
		}
		if matched {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("exact PID/key/mode/blocker never observed")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
func holdVariableFinal(t *testing.T, v *variableHTTPFixture, p vc.ProjectID, n vc.CommandName, key f.IdempotencyKey) (<-chan struct{}, *atomic.Int32, func()) {
	t.Helper()
	identity, e := vc.VariableCommandIdentity(p, n, key)
	if e != nil {
		t.Fatal(e)
	}
	reached, release := make(chan struct{}), make(chan struct{})
	pid := new(atomic.Int32)
	var armed atomic.Bool
	var once sync.Once
	v.tracked.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
			return nil
		}
		x, e := v.tracked.InTx(tx)
		if e != nil {
			return e
		}
		var completed bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, p.String(), string(n), string(key)).Scan(&completed); e != nil {
			return e
		}
		if completed && armed.CompareAndSwap(false, true) {
			var realPID int32
			if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&realPID); e != nil {
				return e
			}
			pid.Store(realPID)
			close(reached)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unlock(); v.tracked.setAfter(nil) })
	return reached, pid, unlock
}

type preparedAppender struct {
	oc.Appender
	reached, release chan struct{}
	once             sync.Once
}

func (a *preparedAppender) PrepareAppend(ctx context.Context, actor identity.Actor, ev event.Event) (oc.AppendPlan, error) {
	plan, e := a.Appender.PrepareAppend(ctx, actor, ev)
	if e != nil {
		return plan, e
	}
	a.once.Do(func() { close(a.reached) })
	select {
	case <-a.release:
		return plan, nil
	case <-ctx.Done():
		return oc.AppendPlan{}, ctx.Err()
	}
}
func prepareGate(t *testing.T, v *variableHTTPFixture) (*preparedAppender, func()) {
	t.Helper()
	a := &preparedAppender{Appender: v.events, reached: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(a.release) }) }
	t.Cleanup(release)
	return a, release
}

func TestProjectVariableConcurrency(t *testing.T) {
	v := newVariableHTTPFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	for _, kind := range []string{"same-key-same-intent", "same-key-different-intent", "two-keys-same-version", "same-live-name", "rename-collision", "delete-before-update", "update-before-delete", "delete-before-name-reuse"} {
		t.Run(kind, func(t *testing.T) {
			key := f.IdempotencyKey(id[struct{}](t).String())
			request := createInput(t, "Race_"+id[struct{}](t).String()[24:], "first")
			second := createInput(t, request.Fields().Name, "second")
			n := vc.CreateCommand
			var target vc.VariableID
			version := f.Version(1)
			var firstFn, secondFn func(context.Context) (vc.VariableMutation, error)
			firstFn = func(ctx context.Context) (vc.VariableMutation, error) {
				return v.service.CreateVariable(ctx, a, meta(t, string(key), nil), p, request)
			}
			secondKey := f.IdempotencyKey(id[struct{}](t).String())
			expected := f.Code("")
			switch kind {
			case "same-key-same-intent":
				second = request
				secondKey = key
			case "same-key-different-intent":
				secondKey = key
				expected = f.IdempotencyKeyReused
			case "same-live-name":
				expected = f.ResourceBusy
			default:
				seed := v.createVariable(t, request.Fields().Name, "old")
				target = seed.Fields().ID
				version = seed.Fields().Version
				value := "changed"
				patch := updateInput(t, vc.VariableUpdateFields{Value: &value})
				n = vc.UpdateCommand
				firstFn = func(ctx context.Context) (vc.VariableMutation, error) {
					return v.service.UpdateVariable(ctx, a, meta(t, string(key), &version), p, target, patch)
				}
				secondFn = func(ctx context.Context) (vc.VariableMutation, error) {
					return v.service.UpdateVariable(ctx, a, meta(t, string(secondKey), &version), p, target, patch)
				}
				expected = f.VersionConflict
				switch kind {
				case "rename-collision":
					other := v.createVariable(t, "Other_"+id[struct{}](t).String()[24:], "old")
					name := "Claim_" + id[struct{}](t).String()[24:]
					patch = updateInput(t, vc.VariableUpdateFields{Name: &name})
					otherFields := other.Fields()
					secondFn = func(ctx context.Context) (vc.VariableMutation, error) {
						return v.service.UpdateVariable(ctx, a, meta(t, string(secondKey), &otherFields.Version), p, otherFields.ID, patch)
					}
					expected = f.ResourceBusy
				case "delete-before-update", "delete-before-name-reuse":
					n = vc.DeleteCommand
					firstFn = func(ctx context.Context) (vc.VariableMutation, error) {
						return v.service.DeleteVariable(ctx, a, meta(t, string(key), &version), p, target)
					}
					expected = f.NotFound
					if kind == "delete-before-name-reuse" {
						secondFn = func(ctx context.Context) (vc.VariableMutation, error) {
							return v.service.CreateVariable(ctx, a, meta(t, string(secondKey), nil), p, second)
						}
						expected = ""
					}
				case "update-before-delete":
					secondFn = func(ctx context.Context) (vc.VariableMutation, error) {
						return v.service.DeleteVariable(ctx, a, meta(t, string(secondKey), &version), p, target)
					}
				}
			}
			if secondFn == nil {
				secondFn = func(ctx context.Context) (vc.VariableMutation, error) {
					return v.service.CreateVariable(ctx, a, meta(t, string(secondKey), nil), p, second)
				}
			}
			reached, pid, release := holdVariableFinal(t, v, p, n, key)
			leader := asyncVariable(t, firstFn)
			awaitStage(t, reached, leader)
			lock, _ := f.UserLock(a.Details().UserID)
			if secondKey == key {
				identity, _ := vc.VariableCommandIdentity(p, vc.CreateCommand, key)
				lock, _ = f.CommandLock(identity)
			}
			observed := observeLock(v.tracked, lock)
			follower := asyncVariable(t, secondFn)
			attempt := waitAttempt(t, observed, follower)
			v.waitLock(t, attempt, false, pid.Load())
			release()
			first := reply(t, leader)
			if first.err != nil {
				t.Fatal("leader", first.err)
			}
			last := reply(t, follower)
			if expected != "" {
				requireCode(t, last.err, expected)
			} else if last.err != nil {
				t.Fatal("follower", last.err)
			}
			if kind == "same-key-same-intent" {
				sameReceipt(t, first.value, last.value)
			}
			v.tracked.mu.Lock()
			v.tracked.beforeLocks = nil
			v.tracked.mu.Unlock()
		})
	}
	t.Run("different-owners-projects-same-global-id", func(t *testing.T) {
		other, _, _ := v.createProject(t, v.otherBrowser.actor, "global-id-"+id[struct{}](t).String()[24:])
		request := createInput(t, "GlobalID_"+id[struct{}](t).String()[24:], "first")
		firstMeta := meta(t, "global-id-first", nil)
		secondMeta := meta(t, "global-id-second", nil)
		gate, releasePrepare := prepareGate(t, v)
		defer releasePrepare()
		secondService := v.newService(t, gate, v.accounts)
		follower := asyncVariable(t, func(ctx context.Context) (vc.VariableMutation, error) {
			return secondService.CreateVariable(ctx, v.otherBrowser.actor, secondMeta, other.ID, request)
		})
		awaitStage(t, gate.reached, follower)
		reached, pid, release := holdVariableFinal(t, v, p, vc.CreateCommand, firstMeta.IdempotencyKey)
		defer release()
		leader := asyncVariable(t, func(ctx context.Context) (vc.VariableMutation, error) {
			return v.service.CreateVariable(ctx, a, firstMeta, p, request)
		})
		awaitStage(t, reached, leader)
		lock, _ := f.ProjectLock(other.ID.String())
		observed := observeLock(v.tracked, lock)
		releasePrepare()
		attempt := waitAttempt(t, observed, follower)
		// Separate current Owners/Projects have no conflicting advisory locks.
		// Prove the INSERT is waiting on the leader's real PostgreSQL transaction.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			var blocked bool
			e := v.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=$1 AND l.pid=$2 AND l.locktype='transactionid' AND l.mode='ShareLock' AND NOT l.granted AND $3::int=ANY(pg_blocking_pids(l.pid)))`, v.db.Name, attempt.pid, pid.Load()).Scan(&blocked)
			if e != nil {
				t.Fatal("global ID unique-index wait", e)
			}
			if blocked {
				break
			}
			select {
			case early := <-follower:
				t.Fatal("no actual uniqueness wait", early.err)
			case <-ctx.Done():
				t.Fatal("global ID transaction wait not observed")
			case <-time.After(5 * time.Millisecond):
			}
		}
		release()
		if got := reply(t, leader); got.err != nil {
			t.Fatal("global ID winner", got.err)
		}
		requireCode(t, reply(t, follower).err, f.NotFound)
		lookup, e := secondService.LookupVariableCommand(ctxFor(t), v.otherBrowser.actor, queryFor(t, v.otherBrowser.actor, secondMeta, other.ID, request.Fields().ID, vc.CreateCommand, request))
		if e != nil || lookup.Status() != vc.LookupInProgress {
			t.Fatal("loser's original planned state changed", e)
		}
		g, h, aud, event := v.counts(t, other.ID)
		if g != 1 || h != 0 || aud != 0 || event != 0 {
			t.Fatal("losing Project acquired committed variable facts")
		}
		_, e = secondService.CreateVariable(ctxFor(t), v.otherBrowser.actor, secondMeta, other.ID, request)
		requireCode(t, e, f.NotFound)
		v.tracked.mu.Lock()
		v.tracked.beforeLocks = nil
		v.tracked.mu.Unlock()
	})
}

func TestProjectVariableFinalAuthorityCompetition(t *testing.T) {
	v := newVariableHTTPFixture(t)
	for _, boundary := range []string{"session", "owner", "archive"} {
		for _, variableFirst := range []bool{false, true} {
			name := boundary + map[bool]string{false: "-gate-first", true: "-variable-first"}[variableFirst]
			t.Run(name, func(t *testing.T) {
				browser := v.login(t, v.ownerBrowser.email)
				a := browser.actor
				project, _, _ := v.createProject(t, a, "auth-"+id[struct{}](t).String()[24:])
				p := project.ID
				request := createInput(t, "Competition", "original")
				m := meta(t, "competition", nil)
				gate, releasePrepare := prepareGate(t, v)
				service := v.newService(t, gate, v.accounts)
				writer := func(ctx context.Context) (vc.VariableMutation, error) {
					return service.CreateVariable(ctx, a, m, p, request)
				}
				change := func(ctx context.Context) error {
					switch boundary {
					case "session":
						return v.core.Logout(ctx, account.LogoutRequest{Actor: a, Key: f.IdempotencyKey(id[struct{}](t).String())})
					case "archive":
						_, e := v.projects.BeginArchive(ctx, a, meta(t, "competition-archive", &project.Version), p)
						return e
					default:
						u, _ := f.UserLock(v.otherBrowser.actor.Details().UserID)
						locks, e := oc.NormalizeLocks(append(ownerLocks(a, p), f.LockRequest{Key: u, Mode: f.Exclusive}))
						if e != nil {
							return e
						}
						result := v.tracked.WithinTx(ctx, cause(t), func(ctx context.Context, tx f.Tx) error {
							if e := v.tracked.AcquireAll(ctx, tx, locks); e != nil {
								return e
							}
							if _, e := v.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate); e != nil {
								return e
							}
							if e := v.accounts.RequireCurrentSession(ctx, tx, v.otherBrowser.actor); e != nil {
								return e
							}
							x, e := v.tracked.InTx(tx)
							if e != nil {
								return e
							}
							_, e = x.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, p.String(), v.otherBrowser.actor.Details().UserID)
							return e
						})
						if result.State() != f.Committed {
							return result.Fault()
						}
						return nil
					}
				}
				var final <-chan struct{}
				var pid *atomic.Int32
				var releaseFinal func()
				if variableFirst {
					final, pid, releaseFinal = holdVariableFinal(t, v, p, vc.CreateCommand, m.IdempotencyKey)
				}
				result := asyncVariable(t, writer)
				awaitStage(t, gate.reached, result)
				if !variableFirst {
					if e := change(ctxFor(t)); e != nil {
						t.Fatal("authority winner", e)
					}
					before := v.snapshot(t)
					releasePrepare()
					got := reply(t, result)
					want := map[string]f.Code{"session": f.SessionRevoked, "owner": f.NotFound, "archive": f.ProjectNotActive}[boundary]
					requireCode(t, got.err, want)
					if before != v.snapshot(t) {
						t.Fatal("final used cached authority")
					}
				} else {
					releasePrepare()
					awaitStage(t, final, result)
					lock, _ := f.UserLock(a.Details().UserID)
					observed := observeLock(v.tracked, lock)
					changed := asyncVariable(t, func(ctx context.Context) (vc.VariableMutation, error) { return vc.VariableMutation{}, change(ctx) })
					attempt := waitAttempt(t, observed, changed)
					v.waitLock(t, attempt, false, pid.Load())
					releaseFinal()
					got := reply(t, result)
					if got.err != nil {
						t.Fatal("variable winner", got.err)
					}
					if got := reply(t, changed); got.err != nil {
						t.Fatal("later authority command", got.err)
					}
					var facts int
					if e := v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1 AND variable_id=$2`, p.String(), request.Fields().ID.String()).Scan(&facts); e != nil || facts != 1 {
						t.Fatal("winner fact count", e)
					}
				}
				v.tracked.mu.Lock()
				v.tracked.beforeLocks = nil
				v.tracked.mu.Unlock()
			})
		}
	}
}
