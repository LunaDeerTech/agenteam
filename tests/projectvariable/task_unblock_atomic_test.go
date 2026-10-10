//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The caller owns the actual final transaction. A successful InTx receipt is
// tentative: the fixed error below rolls back the real writes, not a simulated
// CommitResult. The original planned command remains available for recovery.
func TestTaskTechnicalResolutionAtomic(t *testing.T) {
	t.Run("late-transaction-rollback-and-replay", func(t *testing.T) {
		x := newSchedulerFailureFixture(t)
		beforeFailure := x.currentTask(t)
		failed, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("formal final failure", err)
		}
		x.requireSettled(t, failed, beforeFailure)
		before := x.currentTask(t)
		v, actor := x.v, x.v.base.ownerBrowser.actor
		reader, err := work.NewBlockerReader(v.base.tracked, v.authority, v.base.keys)
		if err != nil {
			t.Fatal("real blocker reader", err)
		}
		readBlocker := func() wc.TaskBlocker {
			t.Helper()
			page, err := reader.ListTaskBlockersPage(ctxFor(t), actor, before.ProjectID, before.ID, wc.TaskBlockersAll, f.PageRequest{Limit: 10})
			if err != nil || page.NextCursor != "" || len(page.Items) != 1 || page.Items[0].Type != wc.TaskBlockerTechnical {
				t.Fatal("current real technical blocker", err)
			}
			return page.Items[0]
		}
		blocker := readBlocker()
		if blocker.ResolvedAt != nil || blocker.ResolvedBy != nil || blocker.ResolutionComment != nil {
			t.Fatal("formal failure did not leave an unresolved blocker")
		}
		request := wc.TaskTransfer{TargetState: wc.TaskStateTodo, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{blocker.ID}}
		commandMeta := meta(t, "technical-resolution-atomic", &before.Version)
		digest, err := wc.TaskTransferDigest(actor, commandMeta, before.ProjectID, before.ID, request)
		if err != nil {
			t.Fatal(err)
		}
		lookup := wc.TaskTransitionLookupRequest{ProjectID: before.ProjectID, Command: wc.TaskTransitionTransfer, IdempotencyKey: commandMeta.IdempotencyKey, SemanticDigest: digest}
		prepared, err := v.transitions.PrepareTaskTransition(ctxFor(t), actor, commandMeta, before.ProjectID, before.ID, request)
		if err != nil || prepared.Prepared == nil || prepared.CompletedReceipt != nil {
			t.Fatal("original public transition preparation", err)
		}
		// Prepare has legitimately committed its planned intent. Include that
		// row in the snapshot so rollback cannot silently publish a receipt.
		stable := taskUnblockSnapshot(t, x)
		creation := taskUnblockCreationSnapshot(t, x)
		identity, err := wc.TaskTransitionIdentity(before.ProjectID, commandMeta.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		cause, err := f.NewCommandsCause(identity)
		if err != nil {
			t.Fatal(err)
		}
		marker := errors.New("technical-resolution-late-final-transaction-rollback")
		var sawAll bool
		physical := v.base.tracked.WithinTx(ctxFor(t), cause, func(ctx context.Context, tx f.Tx) error {
			if err := v.base.tracked.AcquireAll(ctx, tx, prepared.Prepared.RequiredLocks()); err != nil {
				return err
			}
			tentative, err := v.transitions.TransferTaskInTx(ctx, tx, actor, prepared.Prepared)
			if err != nil {
				return err
			}
			sql, err := v.base.tracked.InTx(tx)
			if err != nil {
				return err
			}
			stored, err := taskUnblockCommittedFacts(ctx, sql, x, before, blocker.ID, commandMeta.IdempotencyKey)
			if err != nil {
				return err
			}
			gotBytes, gotErr := json.Marshal(tentative)
			wantBytes, wantErr := json.Marshal(stored)
			if gotErr != nil || wantErr != nil || !bytes.Equal(gotBytes, wantBytes) {
				return errors.New("tentative receipt differs from original final transaction")
			}
			sawAll = true
			return marker
		})
		if !sawAll || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) {
			t.Fatal("actual final transaction did not roll back after all facts", physical.Fault())
		}
		if taskUnblockSnapshot(t, x) != stable || !bytes.Equal(jsonBytes(t, readBlocker()), jsonBytes(t, blocker)) || !bytes.Equal(jsonBytes(t, x.currentTask(t)), jsonBytes(t, before)) {
			t.Fatal("rollback leaked resolution, Task, history, order, receipt, Outbox or activity")
		}
		found, err := v.transitions.LookupTaskTransition(ctxFor(t), actor, lookup)
		if err != nil || found.Status != wc.LookupInProgress || found.Receipt != nil {
			t.Fatal("rolled-back command became committed", err)
		}
		committed, err := v.transitions.TransferTask(ctxFor(t), actor, commandMeta, before.ProjectID, before.ID, request)
		if err != nil {
			t.Fatal("original planned command after known rollback", err)
		}
		stored, err := taskUnblockCommittedFacts(ctxFor(t), v.base.raw, x, before, blocker.ID, commandMeta.IdempotencyKey)
		if err != nil || !bytes.Equal(jsonBytes(t, stored), jsonBytes(t, committed)) {
			t.Fatal("committed resolution facts", err)
		}
		current := x.currentTask(t)
		want := before.Clone()
		want.State, want.Version, want.UpdatedAt, want.ManualRank = wc.TaskStateTodo, before.Version+1, current.UpdatedAt, current.ManualRank
		if !bytes.Equal(jsonBytes(t, current), jsonBytes(t, want)) || !bytes.Equal(jsonBytes(t, current), jsonBytes(t, committed.Task)) {
			t.Fatal("resolution rewrote unrelated Task fields or failed to retain the Agent")
		}
		resolved := readBlocker()
		if resolved.ResolvedAt == nil || resolved.ResolvedBy == nil || resolved.ResolutionComment != nil {
			t.Fatal("resolution did not publish the Human resolver")
		}
		preserved := resolved.Clone()
		preserved.ResolvedAt, preserved.ResolvedBy = nil, nil
		if !bytes.Equal(jsonBytes(t, preserved), jsonBytes(t, blocker)) || taskUnblockCreationSnapshot(t, x) != creation {
			t.Fatal("Human resolution rewrote the original Scheduler failure or blocker creation")
		}
		state := wc.TaskStateTodo
		page, err := v.taskReader.ListTasks(ctxFor(t), actor, before.ProjectID, wc.TaskFilter{SprintID: &before.SprintID, State: &state, Priority: &before.Priority}, f.PageRequest{Limit: 10})
		if err != nil || page.NextCursor != "" || len(page.Items) != 1 || page.Items[0].ID != before.ID {
			t.Fatal("current todo group does not contain the restored Task", err)
		}
		stable = taskUnblockSnapshot(t, x)
		replay, err := v.transitions.TransferTask(ctxFor(t), actor, commandMeta, before.ProjectID, before.ID, request)
		if err != nil || !bytes.Equal(jsonBytes(t, replay), jsonBytes(t, committed)) {
			t.Fatal("completed original key did not replay before resolving again", err)
		}
		found, err = v.transitions.LookupTaskTransition(ctxFor(t), actor, lookup)
		if err != nil || found.Status != wc.LookupCommitted || found.Receipt == nil || !bytes.Equal(jsonBytes(t, *found.Receipt), jsonBytes(t, committed)) || stable != taskUnblockSnapshot(t, x) {
			t.Fatal("lookup/replay changed the original resolution facts", err)
		}
		x.requireNoExecution(t)
	})
}

func taskUnblockSnapshot(t *testing.T, x *schedulerFailureFixture) string {
	t.Helper()
	var commands string
	err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb)::text
 FROM agenteam_work.task_transition_commands c WHERE project_id=$1::text::uuid`, x.request.ProjectID.String()).Scan(&commands)
	if err != nil {
		t.Fatal("original transition commands snapshot", err)
	}
	return x.snapshot(t) + "\n" + commands
}

func taskUnblockCreationSnapshot(t *testing.T, x *schedulerFailureFixture) string {
	t.Helper()
	var value string
	err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'failure',to_jsonb(c),'dispatch',to_jsonb(d),
 'creation',to_jsonb(b)-ARRAY['resolved_at','resolved_by','resolution_comment','resolved_operation_id','resolved_transition_operation_id'])::text
 FROM agenteam_work.task_launch_failures c JOIN agenteam_scheduler.dispatches d ON d.project_id=c.project_id::text AND d.id=c.id::text
 JOIN agenteam_work.task_blockers b ON b.project_id=c.project_id AND b.id=c.blocker_id
 WHERE c.project_id=$1::text::uuid AND c.id=$2::text::uuid`, x.request.ProjectID.String(), x.dispatch.String()).Scan(&value)
	if err != nil {
		t.Fatal("original immutable failure creation facts", err)
	}
	return value
}

func taskUnblockCommittedFacts(ctx context.Context, sql postgres.SQLExecutor, x *schedulerFailureFixture, before wc.Task, blocker wc.TaskBlockerID, key f.IdempotencyKey) (wc.TaskTransitionMutation, error) {
	var command string
	var raw, payload []byte
	err := sql.QueryRow(ctx, `SELECT c.id::text,c.receipt,e.payload FROM agenteam_work.task_transition_commands c
 JOIN agenteam_outbox.events e ON e.id=c.event_id WHERE c.project_id=$1::text::uuid AND c.task_id=$2::text::uuid
 AND c.idempotency_key=$3 AND c.state='completed'`, before.ProjectID.String(), before.ID.String(), string(key)).Scan(&command, &raw, &payload)
	if err != nil {
		return wc.TaskTransitionMutation{}, err
	}
	receipt, err := wc.DecodeTaskTransitionMutation(raw)
	if err != nil || receipt.Task.ID != before.ID || receipt.Task.Version != before.Version+1 || receipt.Task.State != wc.TaskStateTodo || len(receipt.TaskEventIDs) != 2 || len(receipt.EventIDs) != 1 {
		return wc.TaskTransitionMutation{}, errors.New("invalid committed technical resolution receipt")
	}
	var event wc.TaskTransitioned
	if json.Unmarshal(payload, &event) != nil || event.Validate() != nil || event.CommandID.String() != command || event.FromState != wc.TaskStateBlocked || event.ToState != wc.TaskStateTodo || event.AssigneeChange != nil || !reflect.DeepEqual(event.TaskEventIDs, receipt.TaskEventIDs) || event.TargetPosition.NextID != nil {
		return wc.TaskTransitionMutation{}, errors.New("invalid original typed resolution event")
	}
	var facts [8]int64
	err = sql.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid AND id=$2::text::uuid AND state='todo' AND version=$5 AND assignee_agent_id=$7::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_blockers b JOIN agenteam_work.tasks t ON t.project_id=b.project_id AND t.id=b.task_id
  WHERE b.project_id=$1::text::uuid AND b.task_id=$2::text::uuid AND b.id=$4::text::uuid AND b.type='technical'
  AND b.resolved_transition_operation_id=$3::text::uuid AND b.resolved_operation_id IS NULL
  AND b.resolved_at=t.updated_at AND b.resolved_by=jsonb_build_object('type','human','user_id',$6::text,'source','task_domain')
  AND b.resolution_comment IS NULL AND b.failure_operation_id=$8::text::uuid AND b.created_operation_id IS NULL),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND transition_operation_id=$3::text::uuid),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND transition_operation_id=$3::text::uuid
  AND correlation_id=$3::text::uuid AND task_version=$5 AND type='state_changed'
  AND actor=jsonb_build_object('type','human','user_id',$6::text,'source','task_domain')
  AND payload=jsonb_build_object('from_state','blocked','to_state','todo','reason_code',NULL)),
 (SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND transition_operation_id=$3::text::uuid
  AND correlation_id=$3::text::uuid AND task_version=$5 AND type='blocker_resolved'
  AND actor=jsonb_build_object('type','human','user_id',$6::text,'source','task_domain')
  AND payload=jsonb_build_object('blocker_id',$4::text,'blocker_type','technical','resolution_comment',NULL)),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND aggregate_version=$5
  AND id=$9::text::uuid AND producer='work' AND event_type='work.task_transitioned' AND schema_version=1),
 (SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1::text::uuid AND task_id=$2::text::uuid AND resolved_at IS NULL),
 (SELECT count(*) FROM agenteam_work.task_transition_commands WHERE project_id=$1::text::uuid AND id=$3::text::uuid AND state='completed'
  AND committed_at=(SELECT updated_at FROM agenteam_work.tasks WHERE project_id=$1::text::uuid AND id=$2::text::uuid))`,
		before.ProjectID.String(), before.ID.String(), command, blocker.String(), int64(before.Version+1), x.v.base.ownerBrowser.actor.Details().UserID, x.request.AgentID.String(), x.dispatch.String(), receipt.EventIDs[0].String()).Scan(
		&facts[0], &facts[1], &facts[2], &facts[3], &facts[4], &facts[5], &facts[6], &facts[7])
	if err != nil {
		return wc.TaskTransitionMutation{}, err
	}
	if facts != ([8]int64{1, 1, 2, 1, 1, 1, 0, 1}) {
		return wc.TaskTransitionMutation{}, fmt.Errorf("atomic technical resolution fact counts %v", facts)
	}
	return receipt, nil
}
