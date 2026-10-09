//go:build integration

package work_test

// These independent probes use real fixture assembly and frame/lock observation
// ports. Expected facts below are derived from D11 sections 3–6 and 8.3, not
// from the implementation's planner or the author's expected-result helpers.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	fnd "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type independentRuntimeResult[T any] struct {
	Value T
	Err   error
}

func independentRuntimeAsync[T any](t *testing.T, fn func(context.Context) (T, error)) (<-chan independentRuntimeResult[T], context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result, joined := make(chan independentRuntimeResult[T], 1), make(chan struct{})
	go func() { defer close(joined); value, err := fn(ctx); result <- independentRuntimeResult[T]{value, err} }()
	t.Cleanup(func() { cancel(); await(t, joined) })
	return result, cancel
}
func independentRuntimeJoin[T any](t *testing.T, ch <-chan independentRuntimeResult[T]) independentRuntimeResult[T] {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(8 * time.Second):
		t.Fatal("independent call failed to finish")
	}
	return independentRuntimeResult[T]{}
}
func independentRuntimeCode(t *testing.T, err error, want fnd.Code) {
	t.Helper()
	var fault *fnd.Fault
	if !errors.As(err, &fault) || fault.Code != want {
		t.Fatalf("independent safe code mismatch: want %s", want)
	}
}
func independentRuntimeWaiter(t *testing.T, base *taskFixture, observations <-chan lockAttempt, key fnd.LockKey, mode fnd.LockMode, blocker int32) {
	t.Helper()
	var attempt lockAttempt
	select {
	case attempt = <-observations:
	case <-time.After(4 * time.Second):
		t.Fatal("independent caller did not reach AcquireAll")
	}
	if fnd.CompareLockKeys(attempt.Request.Key, key) != 0 || attempt.Request.Mode != mode || attempt.BackendPID <= 0 || attempt.BackendPID == blocker || blocker <= 0 {
		t.Fatal("independent exact waiter identity mismatch")
	}
	lockMode := "ShareLock"
	if mode == fnd.Exclusive {
		lockMode = "ExclusiveLock"
	}
	conn := base.db.Connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	hash := uint64(key.AdvisoryKey())
	for {
		var found bool
		err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=$1 AND l.locktype='advisory' AND l.classid::bigint=$2 AND l.objid::bigint=$3 AND l.objsubid=1 AND l.pid=$4 AND l.mode=$5 AND NOT l.granted AND $6=ANY(pg_blocking_pids(l.pid)))`, base.db.Name, int64(hash>>32), int64(hash&0xffffffff), attempt.BackendPID, lockMode, blocker).Scan(&found)
		if err != nil {
			t.Fatal("independent waiter observation query failed")
		}
		if found {
			t.Log("independent exact caller waiter", attempt.BackendPID, "mode", lockMode, "blocker", blocker)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("independent exact blocked lock was not observed")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

type independentRuntimeHold struct {
	release chan struct{}
	joined  chan struct{}
	ready   chan int32
	once    sync.Once
	result  fnd.CommitResult
}

func (h *independentRuntimeHold) unlock() { h.once.Do(func() { close(h.release) }) }
func independentRuntimeHoldTx(t *testing.T, base *taskFixture, locks []fnd.LockRequest, before, after func(context.Context, fnd.Tx, postgres.SQLExecutor) error) *independentRuntimeHold {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	transactionCause := cause(t)
	h := &independentRuntimeHold{release: make(chan struct{}), joined: make(chan struct{}), ready: make(chan int32, 1)}
	go func() {
		defer close(h.joined)
		h.result = base.store.WithinTx(ctx, transactionCause, func(ctx context.Context, tx fnd.Tx) error {
			if err := base.store.AcquireAll(ctx, tx, locks); err != nil {
				return err
			}
			x, err := base.store.InTx(tx)
			if err != nil {
				return err
			}
			if before != nil {
				if err = before(ctx, tx, x); err != nil {
					return err
				}
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			h.ready <- pid
			select {
			case <-h.release:
			case <-ctx.Done():
				return ctx.Err()
			}
			if after != nil {
				return after(ctx, tx, x)
			}
			return nil
		})
	}()
	t.Cleanup(func() { h.unlock(); cancel(); await(t, h.joined) })
	return h
}
func (h *independentRuntimeHold) pid(t *testing.T) int32 {
	t.Helper()
	select {
	case pid := <-h.ready:
		return pid
	case <-h.joined:
		t.Fatal("independent gate transaction failed before hold")
	case <-time.After(4 * time.Second):
		t.Fatal("independent gate transaction did not reach hold")
	}
	return 0
}
func (h *independentRuntimeHold) commit(t *testing.T) {
	t.Helper()
	h.unlock()
	await(t, h.joined)
	if h.result.State() != fnd.Committed {
		t.Fatal("independent gate transaction did not commit")
	}
}

func independentRuntimeMembershipLocks(a identity.Actor, p pc.ProjectID, s wc.SprintID) []fnd.LockRequest {
	user, _ := fnd.UserLock(a.Details().UserID)
	project, _ := fnd.ProjectLock(p.String())
	schedule, _ := fnd.ProjectScheduleLock(p.String())
	sprint, _ := fnd.AggregateLock(fnd.SprintAggregate, s.String())
	return []fnd.LockRequest{{Key: user, Mode: fnd.Shared}, {Key: project, Mode: fnd.Shared}, {Key: schedule, Mode: fnd.Exclusive}, {Key: sprint, Mode: fnd.Shared}}
}

func TestTaskPlanningIndependentAuthorityMembership(t *testing.T) {
	base := newTaskFixture(t)
	owner := base.human(t, "independent-owner-a", "user")
	other := base.human(t, "independent-owner-b", "user")
	p, _, _ := base.create(t, owner, "independent-authority")
	milestone := base.milestone(t, owner, p.ID, "independent m")
	sprint := base.sprint(t, owner, p.ID, milestone.ID, "independent s")
	create := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.ID, Title: "independent original", Type: wc.TaskTypeSpike, Priority: wc.TaskPriorityCritical}
	metaCreate := meta(t, "independent-shared-key", nil)
	original, err := base.tasks.CreateTask(ctxFor(t), owner, metaCreate, p.ID, create)
	if err != nil {
		t.Fatal("independent baseline create failed")
	}
	semantic, err := wc.TaskCreateDigest(owner, metaCreate, p.ID, create)
	if err != nil {
		t.Fatal("independent lookup input failed")
	}
	lookup := wc.TaskCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskCommandCreate, IdempotencyKey: metaCreate.IdempotencyKey, SemanticDigest: semantic}
	fresh := base.renew(t, owner)
	userKey, _ := fnd.UserLock(owner.Details().UserID)
	// Historical Execute and Lookup must use distinct completed Command
	// identities: both acquire Command EX before User, so the same identity
	// would serialize them before either can prove this shared User gate.
	revokedReplayRequest := create
	revokedReplayRequest.TaskID = id[wc.Task](t)
	revokedReplayMeta := meta(t, "independent-revoked-history", nil)
	if _, err := base.tasks.CreateTask(ctxFor(t), owner, revokedReplayMeta, p.ID, revokedReplayRequest); err != nil {
		t.Fatal("independent second historical identity setup failed")
	}

	t.Run("revocation-gate-three-actual-callers", func(t *testing.T) {
		writer, ws := observedTaskFixture(t, base)
		reader, rs := observedTaskFixture(t, base)
		observer, os := observedTaskFixture(t, base)
		hold := independentRuntimeHoldTx(t, base, []fnd.LockRequest{{Key: userKey, Mode: fnd.Exclusive}}, func(ctx context.Context, _ fnd.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID)
			return err
		}, nil)
		blocker := hold.pid(t)
		wo, ro, oo := observeLock(ws, userKey, nil), observeLock(rs, userKey, nil), observeLock(os, userKey, nil)
		write, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			return writer.tasks.CreateTask(ctx, owner, revokedReplayMeta, p.ID, revokedReplayRequest)
		})
		read, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.Task, error) {
			return reader.taskReader.GetTask(ctx, owner, p.ID, create.TaskID)
		})
		look, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskCommandLookup, error) {
			return observer.tasks.LookupTaskCommand(ctx, owner, lookup)
		})
		independentRuntimeWaiter(t, base, wo, userKey, fnd.Exclusive, blocker)
		independentRuntimeWaiter(t, base, ro, userKey, fnd.Shared, blocker)
		independentRuntimeWaiter(t, base, oo, userKey, fnd.Shared, blocker)
		hold.commit(t)
		w, r, l := independentRuntimeJoin(t, write), independentRuntimeJoin(t, read), independentRuntimeJoin(t, look)
		independentRuntimeCode(t, w.Err, fnd.SessionRevoked)
		independentRuntimeCode(t, r.Err, fnd.SessionRevoked)
		independentRuntimeCode(t, l.Err, fnd.SessionRevoked)
		if w.Value.Task.ID.Validate() == nil || r.Value.ID.Validate() == nil || l.Value.Status != "" || l.Value.Receipt != nil {
			t.Fatal("revocation published a historical or current result")
		}
	})
	owner = fresh
	replayed, err := base.tasks.CreateTask(ctxFor(t), owner, metaCreate, p.ID, create)
	if err != nil || replayed.Task.ID != create.TaskID || replayed.Task.Version != 1 || replayed.Task.Title != create.Title || replayed.TaskEventID == nil || *replayed.TaskEventID != *original.TaskEventID || len(replayed.EventIDs) != 1 || replayed.EventIDs[0] != original.EventIDs[0] {
		t.Fatal("renewed Session did not recover exactly the historical result")
	}

	// Distinct real Owner/Project receives an independent identity for the same key.
	otherProject, _, _ := base.create(t, other, "independent-other")
	otherMilestone := base.milestone(t, other, otherProject.ID, "m")
	otherSprint := base.sprint(t, other, otherProject.ID, otherMilestone.ID, "s")
	otherRequest := create
	otherRequest.TaskID = id[wc.Task](t)
	otherRequest.SprintID = otherSprint.ID
	separate, err := base.tasks.CreateTask(ctxFor(t), other, metaCreate, otherProject.ID, otherRequest)
	if err != nil || separate.Task.ProjectID != otherProject.ID || separate.Task.ID == original.Task.ID {
		t.Fatal("same key mixed independent Project identities")
	}
	_, err = base.tasks.LookupTaskCommand(ctxFor(t), other, lookup)
	independentRuntimeCode(t, err, fnd.NotFound)

	plannedRequest := create
	plannedRequest.TaskID = id[wc.Task](t)
	plannedRequest.Title = "independent planned before archive"
	plannedMeta := meta(t, "independent-planned", nil)
	intercept := &capturingAppender{Appender: base.events, before: func(context.Context, identity.Actor, event.Event) error {
		return fnd.NewFault(fnd.DependencyUnavailable, fnd.NotStarted)
	}}
	planner := base.newTaskService(t, intercept, base.accounts)
	_, err = planner.CreateTask(ctxFor(t), owner, plannedMeta, p.ID, plannedRequest)
	independentRuntimeCode(t, err, fnd.DependencyUnavailable)
	plannedDigest, err := wc.TaskCreateDigest(owner, plannedMeta, p.ID, plannedRequest)
	if err != nil {
		t.Fatal("planned lookup digest failed")
	}
	plannedLookup := wc.TaskCommandLookupRequest{ProjectID: p.ID, Command: wc.TaskCommandCreate, IdempotencyKey: plannedMeta.IdempotencyKey, SemanticDigest: plannedDigest}
	planned, err := base.tasks.LookupTaskCommand(ctxFor(t), owner, plannedLookup)
	if err != nil || planned.Status != wc.LookupInProgress || planned.Receipt != nil {
		t.Fatal("pre-archive plan was not actually persisted")
	}

	t.Run("archive-gate-historical-versus-unfinished", func(t *testing.T) {
		projectKey, _ := fnd.ProjectLock(p.ID.String())
		reader, rs := observedTaskFixture(t, base)
		observer, os := observedTaskFixture(t, base)
		writer, ws := observedTaskFixture(t, base)
		hold := independentRuntimeHoldTx(t, base, []fnd.LockRequest{{Key: userKey, Mode: fnd.Shared}, {Key: projectKey, Mode: fnd.Exclusive}}, func(ctx context.Context, tx fnd.Tx, x postgres.SQLExecutor) error {
			if _, err := base.projectAuthority.RequireOwnerInTx(ctx, tx, owner, p.ID, identity.Mutate); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, p.ID.String())
			return err
		}, nil)
		blocker := hold.pid(t)
		ro, oo, wo := observeLock(rs, projectKey, nil), observeLock(os, projectKey, nil), observeLock(ws, userKey, nil)
		read, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.Task, error) {
			return reader.taskReader.GetTask(ctx, owner, p.ID, create.TaskID)
		})
		look, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskCommandLookup, error) {
			return observer.tasks.LookupTaskCommand(ctx, owner, lookup)
		})
		// Establish both Project waiters before queuing the exclusive User
		// waiter; this avoids relying on PostgreSQL's wait-queue scheduling.
		independentRuntimeWaiter(t, base, ro, projectKey, fnd.Shared, blocker)
		independentRuntimeWaiter(t, base, oo, projectKey, fnd.Shared, blocker)
		write, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
			return writer.tasks.CreateTask(ctx, owner, plannedMeta, p.ID, plannedRequest)
		})
		independentRuntimeWaiter(t, base, wo, userKey, fnd.Exclusive, blocker)
		hold.commit(t)
		r, l, w := independentRuntimeJoin(t, read), independentRuntimeJoin(t, look), independentRuntimeJoin(t, write)
		if r.Err != nil || r.Value.ID != create.TaskID || l.Err != nil || l.Value.Status != wc.LookupCommitted || l.Value.Receipt == nil || l.Value.Receipt.Task.ID != create.TaskID {
			t.Fatal("archived current or historical read lost its authorized result")
		}
		independentRuntimeCode(t, w.Err, fnd.ProjectNotActive)
		if w.Value.Task.ID.Validate() == nil {
			t.Fatal("unfinished archived plan published success")
		}
	})
	stillPlanned, err := base.tasks.LookupTaskCommand(ctxFor(t), owner, plannedLookup)
	if err != nil || stillPlanned.Status != wc.LookupInProgress {
		t.Fatal("archive silently completed or erased planned intent")
	}
	replayed, err = base.tasks.CreateTask(ctxFor(t), owner, metaCreate, p.ID, create)
	if err != nil || replayed.Task.ID != original.Task.ID || replayed.Task.Version != 1 {
		t.Fatal("archived completed original did not replay")
	}
	var absent bool
	if err = base.raw.QueryRow(ctxFor(t), `SELECT NOT EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE id=$1)`, plannedRequest.TaskID.String()).Scan(&absent); err != nil || !absent {
		t.Fatal("archived planned target was written")
	}

	// Use the second owner's active Project for the membership directions.
	membershipOwner := other
	membershipProject := otherProject.ID
	for _, createFirst := range []bool{false, true} {
		name := "membership-first"
		if createFirst {
			name = "create-first"
		}
		t.Run(name, func(t *testing.T) {
			s := base.sprint(t, membershipOwner, membershipProject, otherMilestone.ID, name)
			locks := independentRuntimeMembershipLocks(membershipOwner, membershipProject, s.ID)
			memberUser, _ := fnd.UserLock(membershipOwner.Details().UserID)
			request := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: s.ID, Title: name, Type: wc.TaskTypeChore, Priority: wc.TaskPriorityLow}
			cm := meta(t, "independent-"+name, nil)
			if !createFirst {
				checkEmpty := func(ctx context.Context, tx fnd.Tx, _ postgres.SQLExecutor) error {
					yes, err := base.taskReader.HasTasksInSprintInTx(ctx, tx, membershipOwner, membershipProject, s.ID)
					if err != nil {
						return err
					}
					if yes {
						return errors.New("independent empty membership changed under held locks")
					}
					return nil
				}
				writer, store := observedTaskFixture(t, base)
				observed := observeLock(store, memberUser, nil)
				hold := independentRuntimeHoldTx(t, base, locks, checkEmpty, checkEmpty)
				blocker := hold.pid(t)
				write, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
					return writer.tasks.CreateTask(ctx, membershipOwner, cm, membershipProject, request)
				})
				independentRuntimeWaiter(t, base, observed, memberUser, fnd.Exclusive, blocker)
				hold.commit(t)
				got := independentRuntimeJoin(t, write)
				if got.Err != nil || got.Value.Task.ID != request.TaskID {
					t.Fatal("create after held membership failed")
				}
			} else {
				writer, store := observedTaskFixture(t, base)
				reader, readerStore := observedTaskFixture(t, base)
				command, _ := wc.TaskIdentity(membershipProject, wc.TaskCommandCreate, cm.IdempotencyKey)
				reached, release := make(chan int32, 1), make(chan struct{})
				var once sync.Once
				unlock := func() { once.Do(func() { close(release) }) }
				defer unlock()
				store.setAfter(func(ctx context.Context, tx fnd.Tx, cause fnd.TransactionCause) error {
					if cause.Kind() != fnd.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
						return nil
					}
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					var completed bool
					if err = x.QueryRow(ctx, `SELECT state='completed' FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, membershipProject.String(), string(wc.TaskCommandCreate), string(cm.IdempotencyKey)).Scan(&completed); err != nil {
						return err
					}
					if !completed {
						return nil
					}
					var pid int32
					if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
						return err
					}
					reached <- pid
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
				write, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
					return writer.tasks.CreateTask(ctx, membershipOwner, cm, membershipProject, request)
				})
				var blocker int32
				select {
				case blocker = <-reached:
				case <-time.After(4 * time.Second):
					t.Fatal("independent winning create never reached final boundary")
				}
				observed := observeLock(readerStore, memberUser, nil)
				readCause := cause(t)
				read, _ := independentRuntimeAsync(t, func(ctx context.Context) (bool, error) {
					var yes bool
					result := reader.store.WithinTx(ctx, readCause, func(ctx context.Context, tx fnd.Tx) error {
						if err := reader.store.AcquireAll(ctx, tx, locks); err != nil {
							return err
						}
						var err error
						yes, err = reader.taskReader.HasTasksInSprintInTx(ctx, tx, membershipOwner, membershipProject, s.ID)
						return err
					})
					if result.State() != fnd.Committed {
						return false, result.Fault()
					}
					return yes, nil
				})
				independentRuntimeWaiter(t, base, observed, memberUser, fnd.Shared, blocker)
				unlock()
				w, r := independentRuntimeJoin(t, write), independentRuntimeJoin(t, read)
				if w.Err != nil || r.Err != nil || !r.Value {
					t.Fatal("membership did not observe the committed winning create")
				}
			}
			base.tx(t, locks, func(ctx context.Context, tx fnd.Tx, _ postgres.SQLExecutor) error {
				yes, err := base.taskReader.HasTasksInSprintInTx(ctx, tx, membershipOwner, membershipProject, s.ID)
				if err == nil && !yes {
					return errors.New("independent completed create absent from membership")
				}
				return err
			})
		})
	}
	otherDigest, err := wc.TaskCreateDigest(other, metaCreate, otherProject.ID, otherRequest)
	if err != nil {
		t.Fatal("original second-owner digest failed")
	}
	base.transferOwner(t, other, owner, otherProject.ID)
	_, err = base.tasks.LookupTaskCommand(ctxFor(t), owner, wc.TaskCommandLookupRequest{ProjectID: otherProject.ID, Command: wc.TaskCommandCreate, IdempotencyKey: metaCreate.IdempotencyKey, SemanticDigest: otherDigest})
	independentRuntimeCode(t, err, fnd.NotFound)
	t.Log("independent A validates real canonical gates; lifecycle/session/owner fixture changes are not product lifecycle, Login or transfer commands")
}

var _ oc.Appender = (*capturingAppender)(nil)
