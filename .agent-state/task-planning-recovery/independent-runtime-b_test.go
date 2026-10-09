//go:build integration

package work_test

// The overlay builder joins these declarations after independent-runtime-ab.
// Their imports intentionally come from that single virtual Go source.

func independentRuntimeText(unit string, size int) string {
	raw := make([]byte, size)
	for n := range raw {
		raw[n] = unit[n%len(unit)]
	}
	return string(raw)
}

type independentRuntimeCounters struct {
	Source, Target, Query int64
	History, Outbox       int
}

func independentRuntimeReadCounters(t *testing.T, base *taskFixture, p pc.ProjectID, s wc.SprintID) independentRuntimeCounters {
	t.Helper()
	var out independentRuntimeCounters
	err := base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1 AND sprint_id=$2 AND state='backlog' AND priority='medium'),
 (SELECT order_generation FROM agenteam_work.task_order_groups WHERE project_id=$1 AND sprint_id=$2 AND state='backlog' AND priority='high'),
 (SELECT query_generation FROM agenteam_work.task_query_generations WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='work.task_changed')`, p.String(), s.String()).Scan(&out.Source, &out.Target, &out.Query, &out.History, &out.Outbox)
	if err != nil {
		t.Fatal("independent durable counter read failed")
	}
	return out
}
func independentRuntimeActivity(t *testing.T, base *taskFixture, a identity.Actor) time.Time {
	t.Helper()
	var at time.Time
	if err := base.raw.QueryRow(ctxFor(t), `SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1`, a.Details().SessionID).Scan(&at); err != nil {
		t.Fatal("independent Activity read failed")
	}
	return at
}
func independentRuntimeSameBusiness(a, b wc.Task) bool {
	return a.ID == b.ID && a.ProjectID == b.ProjectID && a.MilestoneID == b.MilestoneID && a.SprintID == b.SprintID && a.Title == b.Title && a.Description == b.Description && a.Type == b.Type && a.Priority == b.Priority && a.State == b.State && a.AssigneeAgentID == nil && b.AssigneeAgentID == nil && a.Plan == b.Plan && a.Version == b.Version && a.CreatedAt.Time().Equal(b.CreatedAt.Time()) && a.UpdatedAt.Time().Equal(b.UpdatedAt.Time())
}

func TestTaskPlanningIndependentCommitRank(t *testing.T) {
	base := newTaskFixture(t)
	owner := base.human(t, "independent-rank-owner", "user")
	for _, commit := range []bool{false, true} {
		name := "rollback"
		if commit {
			name = "commit"
		}
		t.Run(name, func(t *testing.T) {
			project, _, _ := base.create(t, owner, "independent-geometry-"+name)
			milestone := base.milestone(t, owner, project.ID, "independent rank milestone")
			sprint := base.sprint(t, owner, project.ID, milestone.ID, "independent rank sprint")
			var before [4]wc.Task
			for n, title := range []string{"left spectator", "competing reorder", "large original", "target spectator"} {
				priority := wc.TaskPriorityMedium
				if n == 3 {
					priority = wc.TaskPriorityHigh
				}
				created, err := base.tasks.CreateTask(ctxFor(t), owner, meta(t, id[struct{}](t).String(), nil), project.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.ID, Title: title, Type: wc.TaskTypeFeature, Priority: priority})
				if err != nil {
					t.Fatal("independent four-object setup failed")
				}
				before[n] = created.Task
			}
			// A/B/C occupy the smallest three legal source ranks; D is one below
			// the upper bound in the destination group. Both geometries force
			// exact, independently predictable rebalance when their gap is used.
			ranks := [4]string{"00000000000000000000000000000001", "00000000000000000000000000000002", "00000000000000000000000000000003", "fffffffffffffffffffffffffffffffe"}
			schedule, _ := fnd.ProjectScheduleLock(project.ID.String())
			sourceRank, _ := fnd.RankGroupLock("work.task:" + project.ID.String() + ":" + sprint.ID.String() + ":backlog:medium")
			targetRank, _ := fnd.RankGroupLock("work.task:" + project.ID.String() + ":" + sprint.ID.String() + ":backlog:high")
			base.tx(t, fixtureLocks(owner, project.ID, fnd.LockRequest{Key: schedule, Mode: fnd.Exclusive}, fnd.LockRequest{Key: sourceRank, Mode: fnd.Exclusive}, fnd.LockRequest{Key: targetRank, Mode: fnd.Exclusive}), func(ctx context.Context, tx fnd.Tx, x postgres.SQLExecutor) error {
				if _, err := base.projectAuthority.RequireOwnerInTx(ctx, tx, owner, project.ID, identity.Mutate); err != nil {
					return err
				}
				for n, task := range before {
					tag, err := x.Exec(ctx, `UPDATE agenteam_work.tasks SET manual_rank=$2 WHERE project_id=$3 AND id=$1`, task.ID.String(), ranks[n], project.ID.String())
					if err != nil {
						return err
					}
					if tag.RowsAffected() != 1 {
						return errors.New("independent dense seed missed a Task")
					}
				}
				if _, err := x.Exec(ctx, `UPDATE agenteam_work.task_order_groups SET order_generation=order_generation+1 WHERE project_id=$1 AND sprint_id=$2 AND state='backlog' AND priority IN ('medium','high')`, project.ID.String(), sprint.ID.String()); err != nil {
					return err
				}
				_, err := x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=query_generation+1 WHERE project_id=$1`, project.ID.String())
				return err
			})
			for n := range before {
				before[n].ManualRank = ranks[n]
			}
			counts := independentRuntimeReadCounters(t, base, project.ID, sprint.ID)
			if counts.History != 4 || counts.Outbox != 4 {
				t.Fatal("independent setup did not have exactly four committed creations")
			}

			writer, store, proxy := proxyTaskFixture(t, base, commit)
			observer, observerStore := observedTaskFixture(t, base)
			competitor, competitorStore := observedTaskFixture(t, base)
			description := independentRuntimeText("<>&", 32768)
			plan := independentRuntimeText("\"\\\t\n\r", 32768)
			priority := wc.TaskPriorityHigh
			request := wc.TaskFieldsUpdate{Description: &description, Plan: &plan, Priority: &priority}
			cm := meta(t, "independent-original-"+name, &before[2].Version)
			semantic, err := wc.TaskUpdateDigest(owner, cm, project.ID, before[2].ID, request)
			if err != nil {
				t.Fatal("independent original digest failed")
			}
			lookup := wc.TaskCommandLookupRequest{ProjectID: project.ID, Command: wc.TaskCommandUpdate, IdempotencyKey: cm.IdempotencyKey, SemanticDigest: semantic}
			command, err := wc.TaskIdentity(project.ID, wc.TaskCommandUpdate, cm.IdempotencyKey)
			if err != nil {
				t.Fatal("independent original identity failed")
			}
			commandKey, _ := fnd.CommandLock(command)
			userKey, _ := fnd.UserLock(owner.Details().UserID)
			confirmation := observeLock(store, commandKey, func() bool {
				select {
				case <-proxy.reached:
					return true
				default:
					return false
				}
			})
			lookupObserved := observeLock(observerStore, commandKey, nil)
			competingObserved := observeLock(competitorStore, userKey, nil)
			var arm sync.Once
			store.setAfter(func(ctx context.Context, tx fnd.Tx, cause fnd.TransactionCause) error {
				if cause.Kind() != fnd.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
					return nil
				}
				x, err := store.InTx(tx)
				if err != nil {
					return err
				}
				var completed bool
				if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed')`, project.ID.String(), string(wc.TaskCommandUpdate), string(cm.IdempotencyKey)).Scan(&completed); err != nil {
					return err
				}
				if completed {
					var pid int32
					if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
						return err
					}
					arm.Do(func() { proxy.targetPID.Store(pid) })
				}
				return nil
			})
			actualUnknown := make(chan fnd.CommitResult, 1)
			var firstUnknown sync.Once
			store.mu.Lock()
			store.afterResult = func(cause fnd.TransactionCause, result fnd.CommitResult) {
				if cause.Kind() == fnd.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() && result.State() == fnd.Unknown {
					firstUnknown.Do(func() { actualUnknown <- result })
				}
			}
			store.mu.Unlock()
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(proxy.release) }) }
			defer release()
			original, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
				return writer.tasks.UpdateTask(ctx, owner, cm, project.ID, before[2].ID, request)
			})
			await(t, proxy.reached)
			writerPID := proxy.backendPID.Load()
			if writerPID <= 0 {
				t.Fatal("independent COMMIT frame lacks exact backend")
			}
			independentRuntimeWaiter(t, base, confirmation, commandKey, fnd.Exclusive, writerPID)
			look, cancelLookup := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskCommandLookup, error) {
				return observer.tasks.LookupTaskCommand(ctx, owner, lookup)
			})
			independentRuntimeWaiter(t, base, lookupObserved, commandKey, fnd.Exclusive, writerPID)
			cancelLookup()
			cancelled := independentRuntimeJoin(t, look)
			if !errors.Is(cancelled.Err, context.Canceled) || cancelled.Value.Status != "" || cancelled.Value.Receipt != nil {
				t.Fatal("independent cancelled original Lookup did not fail with zero result")
			}
			competingMeta := meta(t, "independent-competing-"+name, &before[1].Version)
			competing, _ := independentRuntimeAsync(t, func(ctx context.Context) (wc.TaskMutation, error) {
				return competitor.tasks.ReorderTask(ctx, owner, competingMeta, project.ID, before[1].ID, wc.TaskReorder{BeforeID: &before[0].ID})
			})
			independentRuntimeWaiter(t, base, competingObserved, userKey, fnd.Shared, writerPID)
			unknown := independentRuntimeJoin(t, original)
			independentRuntimeCode(t, unknown.Err, fnd.CommitUnknown)
			var physical fnd.CommitResult
			select {
			case physical = <-actualUnknown:
			default:
				t.Fatal("independent original writer result was not captured")
			}
			var public *fnd.Fault
			if !errors.As(unknown.Err, &public) || public.CommitState != fnd.Unknown || public.CauseID != physical.AttemptID().String() || physical.Cause().Details().Primary.Canonical() != command.Canonical() {
				t.Fatal("independent Unknown lost original writer provenance")
			}
			if during := independentRuntimeReadCounters(t, base, project.ID, sprint.ID); during != counts {
				t.Fatal("held original COMMIT became visible before release")
			}
			release()
			await(t, proxy.completed)
			reordered := independentRuntimeJoin(t, competing)
			if reordered.Err != nil || !reordered.Value.Changed || reordered.Value.Task.ID != before[1].ID || reordered.Value.Task.Version != 2 {
				t.Fatal("independent competing reorder failed after writer retirement")
			}
			observed, err := base.tasks.LookupTaskCommand(ctxFor(t), owner, lookup)
			if err != nil {
				t.Fatal("independent late Lookup failed")
			}
			want := wc.LookupInProgress
			if commit {
				want = wc.LookupCommitted
			}
			if observed.Status != want {
				t.Fatal("independent late commit/rollback result was misclassified")
			}
			resolved, err := base.tasks.UpdateTask(ctxFor(t), owner, cm, project.ID, before[2].ID, request)
			if err != nil || resolved.Task.Version != 2 || resolved.Task.Description != description || resolved.Task.Plan != plan || resolved.Task.Priority != wc.TaskPriorityHigh || !resolved.Changed {
				t.Fatal("independent explicit original recovery failed")
			}
			if commit && (observed.Receipt == nil || observed.Receipt.TaskEventID == nil || resolved.TaskEventID == nil || *observed.Receipt.TaskEventID != *resolved.TaskEventID || observed.Receipt.EventIDs[0] != resolved.EventIDs[0]) {
				t.Fatal("committed original replay changed its receipt identities")
			}

			// The accepted rank algorithm spreads the ORIGINAL group first.
			// Destination: D=floor(M/2), then C=floor((D+M)/2).
			// Commit source has A/B: A=M/3, then B=floor(A/2).
			// Rollback source still has A/B/C: A=floor(M/4), then
			// B=floor(A/2); recovering C later preserves those ranks.
			wantRanks := [4]string{"55555555555555555555555555555555", "2aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bfffffffffffffffffffffffffffffff", "7fffffffffffffffffffffffffffffff"}
			if !commit {
				wantRanks[0] = "3fffffffffffffffffffffffffffffff"
				wantRanks[1] = "1fffffffffffffffffffffffffffffff"
			}
			var final [4]wc.Task
			for n, previous := range before {
				value, err := base.taskReader.GetTask(ctxFor(t), owner, project.ID, previous.ID)
				if err != nil {
					t.Fatal("independent final canonical read failed")
				}
				final[n] = value
				if value.ManualRank != wantRanks[n] {
					t.Fatal("independent hand-derived rebalance rank differs")
				}
				if (n == 0 || n == 3) && !independentRuntimeSameBusiness(previous, value) {
					t.Fatal("pure rebalance changed spectator business fields")
				}
			}
			if final[1].Version != 2 || final[2].Version != 2 {
				t.Fatal("independent logical mutations did not increment exactly once")
			}
			after := independentRuntimeReadCounters(t, base, project.ID, sprint.ID)
			if after.Source != counts.Source+2 || after.Target != counts.Target+1 || after.Query != counts.Query+2 || after.History != counts.History+2 || after.Outbox != counts.Outbox+2 {
				t.Fatal("independent generations or durable events changed more than the two real mutations")
			}
			var historyOK bool
			if err = base.raw.QueryRow(ctxFor(t), `SELECT count(*)=1 AND bool_and(payload->'changed_fields'='["description","plan","priority"]'::jsonb AND payload->'priority_change'='{"from":"medium","to":"high"}'::jsonb AND payload->'position'->>'previous_id'=$3 AND payload->'position'->'next_id'='null'::jsonb) FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2 AND task_version=2`, project.ID.String(), before[2].ID.String(), before[3].ID.String()).Scan(&historyOK); err != nil || !historyOK {
				t.Fatal("independent original history does not describe the final safe position")
			}
			var identityCount int
			if err = base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed'`, project.ID.String(), string(wc.TaskCommandUpdate), string(cm.IdempotencyKey)).Scan(&identityCount); err != nil || identityCount != 1 {
				t.Fatal("independent original command did not complete exactly once")
			}

			base.ageSession(t, owner)
			aged := independentRuntimeActivity(t, base, owner)
			noop, err := base.tasks.UpdateTask(ctxFor(t), owner, meta(t, "independent-noop-"+name, &final[2].Version), project.ID, final[2].ID, request)
			if err != nil || noop.Changed || noop.TaskEventID != nil || len(noop.EventIDs) != 0 || noop.Task.Version != 2 {
				t.Fatal("independent authorized no-op changed task/event facts")
			}
			if !independentRuntimeActivity(t, base, owner).After(aged) {
				t.Fatal("independent no-op omitted Activity")
			}
			base.ageSession(t, owner)
			aged = independentRuntimeActivity(t, base, owner)
			replay, err := base.tasks.UpdateTask(ctxFor(t), owner, cm, project.ID, before[2].ID, request)
			if err != nil || replay.Task.Version != 2 || !independentRuntimeActivity(t, base, owner).Equal(aged) {
				t.Fatal("independent historical replay touched Activity or changed receipt")
			}
			if unchanged := independentRuntimeReadCounters(t, base, project.ID, sprint.ID); unchanged != after {
				t.Fatal("independent no-op or replay changed generations/history/outbox")
			}
		})
	}
	t.Log("independent B uses four-object low/high dense geometry, real final COMMIT frame outcomes and full-size original text")
}
