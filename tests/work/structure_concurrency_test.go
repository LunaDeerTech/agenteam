//go:build integration

package work_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type workReply struct {
	result wc.StructureMutation
	err    error
}

func joinReply(t *testing.T, ch <-chan workReply) workReply {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("owned Work call did not actually return")
	}
	return workReply{}
}
func callAsync(t *testing.T, fn func(context.Context) (wc.StructureMutation, error)) <-chan workReply {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan workReply, 1)
	done := make(chan struct{})
	go func() { defer close(done); r, err := fn(ctx); out <- workReply{r, err} }()
	t.Cleanup(func() { cancel(); await(t, done) })
	return out
}
func holdLocks(t *testing.T, f *fixture, locks []foundation.LockRequest) (func(), int32) {
	t.Helper()
	reached, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	var result foundation.CommitResult
	var backendPID int32
	var once sync.Once
	holderCause := cause(t)
	go func() {
		defer close(done)
		result = f.store.WithinTx(ctx, holderCause, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, locks); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backendPID); err != nil {
				return err
			}
			close(reached)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	free := func() {
		once.Do(func() { close(release) })
		await(t, done)
		if result.State() != foundation.Committed {
			t.Error("holder did not commit", result.State(), result.Fault())
		}
	}
	t.Cleanup(func() { once.Do(func() { close(release) }); cancel(); await(t, done) })
	await(t, reached)
	return free, backendPID
}
func TestWorkStructureConcurrencyAndRank(t *testing.T) {
	f := newFixture(t)
	a := f.human(t, "concurrency-owner", "user")
	p, _, _ := f.create(t, a, "rank")
	secondService := f.newService(t, f.events, f.accounts)
	t.Run("same-key-final-writer-and-bound-replay-waiter", func(t *testing.T) {
		m := meta(t, "same-key", nil)
		r := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "one intent"}
		command, err := wc.Identity(p.ID, wc.MilestoneCreate, m.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		key, err := foundation.CommandLock(command)
		if err != nil {
			t.Fatal(err)
		}
		writer, writerStore := observedFixture(t, f)
		replayer, replayStore := observedFixture(t, f)
		held, release := make(chan lockAttempt, 1), make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unblock()
		writerStore.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
			if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
				return nil
			}
			x, err := writerStore.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT state='completed' FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.MilestoneCreate), string(m.IdempotencyKey)).Scan(&completed); err != nil {
				return err
			}
			if !completed {
				return nil
			}
			request := foundation.LockRequest{Key: key, Mode: foundation.Exclusive}
			if err = writerStore.RequireHeldLocks(ctx, tx, []foundation.LockRequest{request}); err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			held <- lockAttempt{BackendPID: pid, Request: request}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		replayAttempt := observeLock(replayStore, key, nil)
		before := f.eventCount(t)
		one := callAsync(t, func(ctx context.Context) (wc.StructureMutation, error) {
			return writer.service.CreateMilestone(ctx, a, m, p.ID, r)
		})
		actualWriter := awaitLockAttempt(t, held)
		waitExactLock(t, f.db, actualWriter, true, 0)
		two := callAsync(t, func(ctx context.Context) (wc.StructureMutation, error) {
			return replayer.service.CreateMilestone(ctx, a, m, p.ID, r)
		})
		actualReplayer := awaitLockAttempt(t, replayAttempt)
		if actualWriter.BackendPID == actualReplayer.BackendPID {
			t.Fatal("two calls did not use independent transactions")
		}
		waitExactLock(t, f.db, actualReplayer, false, actualWriter.BackendPID)
		unblock()
		x, y := joinReply(t, one), joinReply(t, two)
		if x.err != nil || y.err != nil {
			t.Fatal(x.err, y.err)
		}
		equalResult(t, x.result, y.result)
		if f.eventCount(t) != before+1 {
			t.Fatal("duplicate same-key event")
		}
	})
	t.Run("different-key-one-version-winner", func(t *testing.T) {
		target := f.milestone(t, a, p.ID, "version")
		user, _ := foundation.UserLock(a.Details().UserID)
		left, leftStore := observedFixture(t, f)
		right, rightStore := observedFixture(t, f)
		leftObserved, rightObserved := observeLock(leftStore, user, nil), observeLock(rightStore, user, nil)
		release, holderPID := holdLocks(t, f, []foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}})
		text1, text2 := "one", "two"
		meta1, meta2 := meta(t, "winner-one", &target.Version), meta(t, "winner-two", &target.Version)
		one := callAsync(t, func(ctx context.Context) (wc.StructureMutation, error) {
			return left.service.UpdateMilestone(ctx, a, meta1, p.ID, target.ID, wc.UpdateFields{Title: &text1})
		})
		two := callAsync(t, func(ctx context.Context) (wc.StructureMutation, error) {
			return right.service.UpdateMilestone(ctx, a, meta2, p.ID, target.ID, wc.UpdateFields{Title: &text2})
		})
		leftAttempt, rightAttempt := awaitLockAttempt(t, leftObserved), awaitLockAttempt(t, rightObserved)
		if leftAttempt.BackendPID == rightAttempt.BackendPID {
			t.Fatal("version race callers share one Tx")
		}
		waitExactLock(t, f.db, leftAttempt, false, holderPID)
		waitExactLock(t, f.db, rightAttempt, false, holderPID)
		release()
		x, y := joinReply(t, one), joinReply(t, two)
		if (x.err == nil) == (y.err == nil) {
			t.Fatal("not exactly one version winner", x.err, y.err)
		}
		if x.err != nil {
			requireCode(t, x.err, foundation.VersionConflict)
		} else {
			requireCode(t, y.err, foundation.VersionConflict)
		}
	})
	t.Run("same-and-different-parent-tail", func(t *testing.T) {
		left := f.milestone(t, a, p.ID, "left")
		right := f.milestone(t, a, p.ID, "right")
		results := make([]<-chan workReply, 0, 6)
		for n := 0; n < 6; n++ {
			parent := left.ID
			if n%2 != 0 {
				parent = right.ID
			}
			request := wc.CreateSprintRequest{SprintID: id[c.Sprint](t), MilestoneID: parent, Title: "parallel"}
			m := meta(t, fmt.Sprintf("tail-%d", n), nil)
			results = append(results, callAsync(t, func(ctx context.Context) (wc.StructureMutation, error) {
				return secondService.CreateSprint(ctx, a, m, p.ID, request)
			}))
		}
		for _, result := range results {
			if got := joinReply(t, result); got.err != nil {
				t.Fatal(got.err)
			}
		}
		for _, parent := range []wc.MilestoneID{left.ID, right.ID} {
			page, err := f.reader.ListSprints(ctxFor(t), a, p.ID, parent, foundation.DefaultPageRequest())
			if err != nil || len(page.Items) != 3 {
				t.Fatal("parent tail lost rows", err)
			}
			for n := 1; n < len(page.Items); n++ {
				if page.Items[n-1].ManualRank >= page.Items[n].ManualRank {
					t.Fatal("duplicate tail ranks")
				}
			}
		}
	})
	t.Run("rebalance-preserves-completed-spectators", func(t *testing.T) {
		parent := f.milestone(t, a, p.ID, "dense")
		one := f.sprint(t, a, p.ID, parent.ID, "one")
		two := f.sprint(t, a, p.ID, parent.ID, "two")
		three := f.sprint(t, a, p.ID, parent.ID, "three")
		f.lifecycle(t, a, p.ID, one, true)
		ids := []c.SprintID{one.ID, two.ID, three.ID}
		seedDense(t, f, a, p.ID, parent.ID, ids)
		readFacts := func() []wc.Sprint {
			facts := make([]wc.Sprint, 0, len(ids))
			for _, id := range ids {
				value, err := f.reader.GetSprint(ctxFor(t), a, p.ID, id)
				if err != nil {
					t.Fatal(err)
				}
				facts = append(facts, value)
			}
			return facts
		}
		generation := func() int64 {
			var value int64
			if err := f.raw.QueryRow(ctxFor(t), `SELECT order_generation FROM agenteam_work.sprint_order_groups WHERE project_id=$1 AND milestone_id=$2`, p.ID.String(), parent.ID.String()).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		activity := func() time.Time {
			var value time.Time
			if err := f.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := readFacts()
		beforeGeneration := generation()
		beforeEvents := f.eventCount(t)
		if before[0].State != wc.Completed || before[1].State != wc.Planned || before[2].State != wc.Planned {
			t.Fatal("spectator lifecycle premise")
		}
		page, err := f.reader.ListSprints(ctxFor(t), a, p.ID, parent.ID, foundation.PageRequest{Limit: 1})
		if err != nil || page.NextCursor == "" {
			t.Fatal(err)
		}
		moved, err := f.service.ReorderSprint(ctxFor(t), a, meta(t, "dense-reorder", &before[2].Version), p.ID, three.ID, wc.ReorderSprintRequest{MilestoneID: parent.ID, BeforeID: &two.ID})
		if err != nil || !moved.Changed {
			t.Fatal("dense reorder", err)
		}
		after := readFacts()
		for i := 0; i < 2; i++ {
			if after[i].Validate() != nil || after[i].ManualRank == before[i].ManualRank {
				t.Fatal("dense maintenance did not produce a valid new spectator rank", i)
			}
			business := after[i].Clone()
			business.ManualRank = before[i].ManualRank
			if string(jsonBytes(t, business)) != string(jsonBytes(t, before[i])) {
				t.Fatal("spectator identity/parent/lifecycle/version/time/content changed", i)
			}
		}
		if after[2].Version != before[2].Version+1 || !after[2].UpdatedAt.Time().After(before[2].UpdatedAt.Time()) || after[2].ManualRank == before[2].ManualRank {
			t.Fatal("target reorder version/time/rank increment absent")
		}
		targetBusiness := after[2].Clone()
		targetBusiness.ManualRank = before[2].ManualRank
		targetBusiness.Version = before[2].Version
		targetBusiness.UpdatedAt = before[2].UpdatedAt
		if string(jsonBytes(t, targetBusiness)) != string(jsonBytes(t, before[2])) {
			t.Fatal("target reorder changed identity/parent/lifecycle/content")
		}
		if string(jsonBytes(t, *moved.Sprint)) != string(jsonBytes(t, after[2])) || generation() != beforeGeneration+1 || f.eventCount(t) != beforeEvents+1 {
			t.Fatal("rebalance plus reorder was not exactly one generation/event")
		}
		_, err = f.reader.ListSprints(ctxFor(t), a, p.ID, parent.ID, foundation.PageRequest{Cursor: page.NextCursor, Limit: 1})
		requireCode(t, err, foundation.CursorStale)
		// A new no-op command writes its own receipt and Activity, but neither
		// physical maintenance nor canonical/group/event changes are permitted.
		f.ageSession(t, a)
		beforeTouch := activity()
		stableFacts := string(jsonBytes(t, after))
		stableGeneration := generation()
		stableEvents := f.eventCount(t)
		noOpMeta := meta(t, "logical-no-op", &after[2].Version)
		noOpRequest := wc.ReorderSprintRequest{MilestoneID: parent.ID, BeforeID: &two.ID}
		unchanged, err := f.service.ReorderSprint(ctxFor(t), a, noOpMeta, p.ID, three.ID, noOpRequest)
		if err != nil || unchanged.Changed {
			t.Fatal("logical no-op", err)
		}
		if string(jsonBytes(t, readFacts())) != stableFacts || generation() != stableGeneration || f.eventCount(t) != stableEvents {
			t.Fatal("no-op changed canonical/rank/generation/event")
		}
		if !activity().After(beforeTouch) {
			t.Fatal("first no-op omitted its allowed Activity touch")
		}
		var receiptBytes []byte
		var state string
		if err = f.raw.QueryRow(ctxFor(t), `SELECT state,receipt FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.SprintReorder), string(noOpMeta.IdempotencyKey)).Scan(&state, &receiptBytes); err != nil || state != "completed" {
			t.Fatal("first no-op receipt missing", err)
		}
		var receipt wc.StructureMutation
		if err = json.Unmarshal(receiptBytes, &receipt); err != nil {
			t.Fatal(err)
		}
		equalResult(t, unchanged, receipt)
		f.ageSession(t, a)
		historicalBefore := f.snapshot(t, a)
		again, err := f.service.ReorderSprint(ctxFor(t), a, noOpMeta, p.ID, three.ID, noOpRequest)
		if err != nil {
			t.Fatal(err)
		}
		equalResult(t, unchanged, again)
		if f.snapshot(t, a) != historicalBefore {
			t.Fatal("historical no-op replay touched Activity or repeated durable facts")
		}
	})
	t.Run("content-update-replans-after-physical-rank", func(t *testing.T) {
		parent := f.milestone(t, a, p.ID, "content-race")
		one := f.sprint(t, a, p.ID, parent.ID, "one")
		two := f.sprint(t, a, p.ID, parent.ID, "two")
		three := f.sprint(t, a, p.ID, parent.ID, "three")
		seedDense(t, f, a, p.ID, parent.ID, []c.SprintID{one.ID, two.ID, three.ID})
		var fired atomic.Bool
		capture := &capturingAppender{Appender: f.events}
		capture.after = func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
			if fired.CompareAndSwap(false, true) {
				_, err := secondService.ReorderSprint(ctx, a, meta(t, "concurrent-rebalance", &three.Version), p.ID, three.ID, wc.ReorderSprintRequest{MilestoneID: parent.ID, BeforeID: &two.ID})
				return err
			}
			return nil
		}
		writer := f.newService(t, capture, f.accounts)
		text := "new content"
		out, err := writer.UpdateSprint(ctxFor(t), a, meta(t, "content", &one.Version), p.ID, one.ID, wc.UpdateFields{Title: &text})
		if err != nil {
			t.Fatal(err)
		}
		actual, err := f.reader.GetSprint(ctxFor(t), a, p.ID, one.ID)
		if err != nil || actual.ManualRank == "00000000000000000000000000000001" || actual.ManualRank != out.Sprint.ManualRank || actual.Title != text || actual.Version != one.Version+1 {
			t.Fatal("update wrote back stale rank", err)
		}
		capture.mu.Lock()
		attempts := len(capture.captured)
		capture.mu.Unlock()
		if attempts != 2 {
			t.Fatal("expected one bounded non-target replan", attempts)
		}
	})
	t.Run("three-round-cap-and-stale-plan", func(t *testing.T) {
		parent := f.milestone(t, a, p.ID, "cap-target")
		anchor := f.milestone(t, a, p.ID, "cap-anchor")
		var rounds atomic.Int32
		capture := &capturingAppender{Appender: f.events}
		capture.after = func(ctx context.Context, _ identity.Actor, _ event.Event, _ oc.AppendPlan) error {
			n := rounds.Add(1)
			_, err := secondService.CreateMilestone(ctx, a, meta(t, fmt.Sprintf("invalidate-%d", n), nil), p.ID, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "changes generation"})
			return err
		}
		writer := f.newService(t, capture, f.accounts)
		_, err := writer.ReorderMilestone(ctxFor(t), a, meta(t, "bounded-replan", &anchor.Version), p.ID, anchor.ID, wc.ReorderMilestoneRequest{BeforeID: &parent.ID})
		if err == nil {
			t.Fatal("test premise must be a real reorder")
		}
		requireCode(t, err, foundation.ResourceBusy)
		if rounds.Load() != 3 {
			t.Fatal("replan did not stop at three", rounds.Load())
		}
		capture.mu.Lock()
		first := capture.captured[0]
		capture.mu.Unlock()
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, first.Plan.Locks()); err != nil {
				return err
			}
			return f.authority.ValidateAppendInTx(ctx, tx, a, first.Event.Summary(), first.Plan.Details().Producer, oc.CurrentAccess)
		})
		if result.State() != foundation.NotCommitted {
			t.Fatal("replaced opaque plan survived")
		}
	})
}
func seedDense(t *testing.T, f *fixture, a identity.Actor, project c.ProjectID, parent wc.MilestoneID, ids []c.SprintID) {
	t.Helper()
	rankKey, _ := foundation.RankGroupLock("work.sprint:" + project.String() + ":" + parent.String())
	f.tx(t, fixtureLocks(a, project, foundation.LockRequest{Key: rankKey, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, project, identity.Mutate); err != nil {
			return err
		}
		ranks := []string{"00000000000000000000000000000001", "00000000000000000000000000000002", "fffffffffffffffffffffffffffffffe"}
		for n, id := range ids {
			if _, err := x.Exec(ctx, `UPDATE agenteam_work.sprints SET manual_rank=$3 WHERE project_id=$1 AND id=$2`, project.String(), id.String(), ranks[n]); err != nil {
				return err
			}
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_work.sprint_order_groups SET order_generation=order_generation+1 WHERE project_id=$1 AND milestone_id=$2`, project.String(), parent.String())
		return err
	})
	t.Log("test-only valid dense ranks; business versions/times unchanged")
}

func TestWorkStructureCommitUnknown(t *testing.T) {
	base := newFixture(t)
	a := base.human(t, "unknown-owner", "user")
	p, _, _ := base.create(t, a, "unknown")
	target := base.milestone(t, a, p.ID, "before")
	for _, phase := range []string{"planned", "completed"} {
		for _, commit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/commit=%t", phase, commit), func(t *testing.T) {
				f, store, proxy := proxyFixture(t, base, commit)
				current, err := base.reader.GetMilestone(ctxFor(t), a, p.ID, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				text := fmt.Sprintf("%s-%t", phase, commit)
				m := meta(t, id[struct{}](t).String(), &current.Version)
				r := wc.UpdateFields{Title: &text}
				semantic, err := wc.UpdateMilestoneDigest(a, m, p.ID, target.ID, r)
				if err != nil {
					t.Fatal(err)
				}
				lookup := wc.CommandLookupRequest{ProjectID: p.ID, Command: wc.MilestoneUpdate, Key: m.IdempotencyKey, Semantic: semantic}
				command, err := wc.Identity(p.ID, wc.MilestoneUpdate, m.IdempotencyKey)
				if err != nil {
					t.Fatal(err)
				}
				key, err := foundation.CommandLock(command)
				if err != nil {
					t.Fatal(err)
				}
				confirmationAttempt := observeLock(store, key, func() bool {
					select {
					case <-proxy.reached:
						return true
					default:
						return false
					}
				})
				lookupFixture, lookupStore := observedFixture(t, base)
				lookupAttempt := observeLock(lookupStore, key, nil)
				var armed atomic.Bool
				var backend atomic.Int32
				store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
					if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() || armed.Load() {
						return nil
					}
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					var state string
					if err = x.QueryRow(ctx, `SELECT state FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.MilestoneUpdate), string(m.IdempotencyKey)).Scan(&state); err != nil {
						return err
					}
					if state == phase && armed.CompareAndSwap(false, true) {
						var pid int32
						if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
							return err
						}
						backend.Store(pid)
						proxy.targetPID.Store(pid)
					}
					return nil
				})
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(proxy.release) }) }
				defer release()
				beforeEvents := base.eventCount(t)
				original := callAsync(t, func(ctx context.Context) (wc.StructureMutation, error) {
					return f.service.UpdateMilestone(ctx, a, m, p.ID, target.ID, r)
				})
				await(t, proxy.reached)
				if proxy.backendPID.Load() != backend.Load() || backend.Load() <= 0 {
					t.Fatal("COMMIT not bound to exact backend")
				}
				actualConfirmation := awaitLockAttempt(t, confirmationAttempt)
				waitExactLock(t, base.db, actualConfirmation, false, backend.Load())
				type lookupReply struct {
					result wc.CommandLookup
					err    error
				}
				lookupDone := make(chan lookupReply, 1)
				lookupJoined := make(chan struct{})
				lookupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				go func() {
					defer close(lookupJoined)
					value, err := lookupFixture.service.LookupCommand(lookupCtx, a, lookup)
					lookupDone <- lookupReply{value, err}
				}()
				t.Cleanup(func() { cancel(); await(t, lookupJoined) })
				actualLookup := awaitLockAttempt(t, lookupAttempt)
				if actualLookup.BackendPID == actualConfirmation.BackendPID || actualLookup.BackendPID == backend.Load() {
					t.Fatal("lookup did not use an independent caller Tx")
				}
				waitExactLock(t, base.db, actualLookup, false, backend.Load())
				cancel() // Only after this caller is proved waiting on the exact writer.
				await(t, lookupJoined)
				looked := <-lookupDone
				if !errors.Is(looked.err, context.Canceled) || looked.result.State != "" || looked.result.Result != nil {
					t.Fatal("cancelled exact Lookup waiter published a result or lost cancellation", looked.result, looked.err)
				}
				got := joinReply(t, original)
				requireCode(t, got.err, foundation.CommitUnknown)
				var unknown *foundation.Fault
				if !errors.As(got.err, &unknown) || unknown.CommitState != foundation.Unknown || unknown.RetryHint != "lookup" {
					t.Fatal("unknown proof rewritten")
				}
				if base.eventCount(t) != beforeEvents {
					t.Fatal("held writer result was prematurely visible")
				}
				release()
				await(t, proxy.completed)
				observed, err := base.service.LookupCommand(ctxFor(t), a, lookup)
				if err != nil {
					t.Fatal(err)
				}
				want := wc.LookupInProgress
				if phase == "planned" && !commit {
					want = wc.LookupNotObserved
				}
				if phase == "completed" && commit {
					want = wc.LookupCommitted
				}
				if observed.State != want {
					t.Fatal("wrong serialized late outcome", observed.State, want)
				}
				fresh := base.renew(t, a)
				resolved, err := base.service.UpdateMilestone(ctxFor(t), fresh, m, p.ID, target.ID, r)
				if err != nil {
					t.Fatal("explicit original recovery", err)
				}
				if resolved.Milestone.Title != text || resolved.Milestone.Version != current.Version+1 || base.eventCount(t) != beforeEvents+1 {
					t.Fatal("late recovery duplicated or changed intent")
				}
				if observed.Result != nil {
					equalResult(t, *observed.Result, resolved)
				}
				stable := base.snapshot(t, fresh)
				again, err := base.service.UpdateMilestone(ctxFor(t), fresh, m, p.ID, target.ID, r)
				if err != nil {
					t.Fatal(err)
				}
				equalResult(t, resolved, again)
				if base.eventCount(t) != beforeEvents+1 || base.snapshot(t, fresh) != stable {
					t.Fatal("receipt recovery duplicated event or touched stored facts")
				}
				var commands int
				if err = base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.MilestoneUpdate), string(m.IdempotencyKey)).Scan(&commands); err != nil || commands != 1 {
					t.Fatal("original identity was replaced", err)
				}
				t.Log("actual COMMIT frame held; serialized lookup and real late outcome", phase, commit, "backend", backend.Load())
			})
		}
	}
	for _, failure := range []string{"confirmation-sql-error", "confirmation-revoked-session"} {
		t.Run(failure, func(t *testing.T) {
			f, store, proxy := proxyFixture(t, base, true)
			actor := base.renew(t, a)
			recoveryActor := base.renew(t, a)
			current, err := base.reader.GetMilestone(ctxFor(t), a, p.ID, target.ID)
			if err != nil {
				t.Fatal(err)
			}
			text := failure
			metadata := meta(t, failure, &current.Version)
			identity, err := wc.Identity(p.ID, wc.MilestoneUpdate, metadata.IdempotencyKey)
			if err != nil {
				t.Fatal(err)
			}
			var armed atomic.Bool
			var original foundation.CommitResult
			var observed atomic.Bool
			var once sync.Once
			release := func() { once.Do(func() { close(proxy.release) }) }
			defer release()
			store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
				if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
					return nil
				}
				x, err := store.InTx(tx)
				if err != nil {
					return err
				}
				var completed bool
				if err = x.QueryRow(ctx, `SELECT state='completed' FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.MilestoneUpdate), string(metadata.IdempotencyKey)).Scan(&completed); err != nil {
					return err
				}
				if completed && armed.CompareAndSwap(false, true) {
					var pid int32
					if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
						return err
					}
					proxy.targetPID.Store(pid)
				}
				return nil
			})
			store.mu.Lock()
			store.afterResult = func(cause foundation.TransactionCause, result foundation.CommitResult) {
				if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || result.State() != foundation.Unknown || !observed.CompareAndSwap(false, true) {
					return
				}
				original = result
				release()
				await(t, proxy.completed)
				if failure == "confirmation-revoked-session" {
					key, _ := foundation.UserLock(actor.Details().UserID)
					base.tx(t, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
						if err := base.accounts.RequireCurrentSession(ctx, tx, actor); err != nil {
							return err
						}
						_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, actor.Details().SessionID)
						return err
					})
				}
			}
			store.beforeLocks = func(ctx context.Context, tx foundation.Tx, _ []foundation.LockRequest) error {
				if failure != "confirmation-sql-error" || !observed.Load() {
					return nil
				}
				x, err := store.InTx(tx)
				if err != nil {
					return err
				}
				_, err = x.Exec(ctx, `SELECT 1/0`)
				return err
			}
			store.mu.Unlock()
			before := base.eventCount(t)
			result, err := f.service.UpdateMilestone(ctxFor(t), actor, metadata, p.ID, target.ID, wc.UpdateFields{Title: &text})
			requireCode(t, err, foundation.CommitUnknown)
			var fault *foundation.Fault
			if !observed.Load() || original.State() != foundation.Unknown || original.AttemptID().Validate() != nil || !errors.As(err, &fault) || fault.CauseID != original.AttemptID().String() || fault.CommitState != foundation.Unknown || fault.RetryHint != "lookup" || result.Changed {
				t.Fatal("failed confirmation replaced original physical Unknown", err)
			}
			if base.eventCount(t) != before+1 {
				t.Fatal("actual late commit not observed once")
			}
			resolved, err := base.service.UpdateMilestone(ctxFor(t), recoveryActor, metadata, p.ID, target.ID, wc.UpdateFields{Title: &text})
			if err != nil || resolved.Milestone.Title != text || base.eventCount(t) != before+1 {
				t.Fatal("explicit recovery after failed confirmation", err)
			}
		})
	}
	t.Run("stop-joins-confirmation-not-proxy-writer", func(t *testing.T) {
		f, store, proxy := proxyFixture(t, base, true)
		current, err := base.reader.GetMilestone(ctxFor(t), a, p.ID, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		text := "stop"
		m := meta(t, "stop-confirmation", &current.Version)
		identity, err := wc.Identity(p.ID, wc.MilestoneUpdate, m.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		key, err := foundation.CommandLock(identity)
		if err != nil {
			t.Fatal(err)
		}
		confirmationAttempt := observeLock(store, key, func() bool {
			select {
			case <-proxy.reached:
				return true
			default:
				return false
			}
		})
		var armed atomic.Bool
		store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
			if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT state='completed' FROM agenteam_work.structure_commands WHERE project_id=$1 AND idempotency_key=$2`, p.ID.String(), string(m.IdempotencyKey)).Scan(&completed); err != nil {
				return err
			}
			if completed && armed.CompareAndSwap(false, true) {
				var pid int32
				if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					return err
				}
				proxy.targetPID.Store(pid)
			}
			return nil
		})
		var once sync.Once
		release := func() { once.Do(func() { close(proxy.release) }) }
		defer release()
		reply := callAsync(t, func(ctx context.Context) (wc.StructureMutation, error) {
			return f.service.UpdateMilestone(ctx, a, m, p.ID, target.ID, wc.UpdateFields{Title: &text})
		})
		await(t, proxy.reached)
		actualConfirmation := awaitLockAttempt(t, confirmationAttempt)
		waitExactLock(t, base.db, actualConfirmation, false, proxy.backendPID.Load())
		f.service.Stop()
		got := joinReply(t, reply)
		requireCode(t, got.err, foundation.CommitUnknown)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err = f.service.Drain(ctx); err != nil {
			t.Fatal("confirmation was not joined", err)
		}
		release()
		await(t, proxy.completed)
	})
	t.Run("cancel-before-final-and-after-known-commit", func(t *testing.T) {
		for _, afterCommit := range []bool{false, true} {
			t.Run(fmt.Sprint(afterCommit), func(t *testing.T) {
				raw := openStore(t, base.db.Config(t, nil))
				store := &hookStore{fixtureStore: raw}
				f := assemble(t, base.db, raw, store, false)
				current, err := base.reader.GetMilestone(ctxFor(t), a, p.ID, target.ID)
				if err != nil {
					t.Fatal(err)
				}
				text := fmt.Sprintf("cancel-%t", afterCommit)
				m := meta(t, id[struct{}](t).String(), &current.Version)
				command, err := wc.Identity(p.ID, wc.MilestoneUpdate, m.IdempotencyKey)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var completed atomic.Bool
				store.setAfter(func(ctx context.Context, tx foundation.Tx, cause foundation.TransactionCause) error {
					if cause.Kind() != foundation.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
						return nil
					}
					x, err := store.InTx(tx)
					if err != nil {
						return err
					}
					var state string
					if err = x.QueryRow(ctx, `SELECT state FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.ID.String(), string(wc.MilestoneUpdate), string(m.IdempotencyKey)).Scan(&state); err != nil {
						return err
					}
					if state == "completed" {
						completed.Store(true)
						if !afterCommit {
							cancel()
						}
					}
					return nil
				})
				store.mu.Lock()
				store.afterResult = func(cause foundation.TransactionCause, result foundation.CommitResult) {
					if afterCommit && completed.Load() && result.State() == foundation.Committed && cause.Kind() == foundation.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
						cancel()
					}
				}
				store.mu.Unlock()
				before := base.eventCount(t)
				result, err := f.service.UpdateMilestone(ctx, a, m, p.ID, target.ID, wc.UpdateFields{Title: &text})
				if !completed.Load() {
					t.Fatal("final callback not reached")
				}
				if afterCommit {
					if err != nil || !result.Changed || base.eventCount(t) != before+1 {
						t.Fatal("known commit rewritten after delivery cancellation", err)
					}
				} else {
					if err == nil || !errors.Is(err, context.Canceled) || base.eventCount(t) != before {
						t.Fatal("pre-COMMIT cancellation did not preserve rollback/cause", err)
					}
				}
			})
		}
	})
}
