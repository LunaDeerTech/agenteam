//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestSprintCompleteHTTP(t *testing.T) {
	t.Run("empty-cancelled-members-and-original-recovery", func(t *testing.T) {
		v := assembleTaskHumanHTTPFixture(t, true)
		base, owner := v.domain.base, v.domain.base.ownerBrowser
		original := sprintCompleteGet(t, v, v.domain.task.SprintID)
		empty := sprintCompleteCreate(t, v, original, "empty")
		current := sprintCompleteStart(t, v, empty, "start-empty")
		body, lookup := sprintCompleteIntent(t, current)
		path := variableHTTPPath(base.project.ID, "/sprints/"+current.ID.String()+"/complete")
		before := sprintCompleteSnapshot(t, v)
		wrong := owner
		wrong.csrf = "invalid-completion-csrf"
		v.requireProblem(t, v.request(t, wrong, "POST", path, body, "complete-empty"), 403, f.CSRFFailed)
		v.requireProblem(t, v.request(t, base.otherBrowser, "POST", path, body, "complete-empty"), 404, f.NotFound)
		stale := current.Clone()
		stale.Version--
		staleBody, _ := sprintCompleteIntent(t, stale)
		v.requireProblem(t, v.request(t, owner, "POST", path, staleBody, "complete-stale"), 409, f.VersionConflict)
		if sprintCompleteSnapshot(t, v) != before {
			t.Fatal("denied completion changed original business facts")
		}
		project, err := base.projects.GetSchedulerConfig(ctxFor(t), owner.actor, base.project.ID)
		agentAppRequire(t, err)
		reply := v.request(t, owner, "POST", path, body, "complete-empty")
		completed := sprintCompleteReceipt(t, v, current, project, "complete-empty", reply)
		after := sprintCompleteSnapshot(t, v)
		sprintCompleteLookup(t, v, lookup, "complete-empty", completed)
		replay := v.request(t, owner, "POST", path, body, "complete-empty")
		if replay.status != 200 || !bytes.Equal(replay.body, reply.body) || sprintCompleteSnapshot(t, v) != after {
			t.Fatal("completion replay or Lookup rewrote the committed result")
		}
		if got := sprintCompleteGet(t, v, original.ID); !reflect.DeepEqual(got, original) {
			t.Fatal("completion automatically changed another planned Sprint")
		}

		current = sprintCompleteStart(t, v, original, "start-members")
		body, _ = sprintCompleteIntent(t, current)
		path = variableHTTPPath(base.project.ID, "/sprints/"+current.ID.String()+"/complete")
		before = sprintCompleteSnapshot(t, v)
		rejected := v.request(t, owner, "POST", path, body, "unfinished-complete")
		v.requireProblem(t, rejected, 409, f.InvalidState)
		if !bytes.Contains(rejected.body, []byte("UNFINISHED_TASKS")) || sprintCompleteSnapshot(t, v) != before {
			t.Fatal("unfinished Sprint was not refused without mutation")
		}
		second, err := v.domain.tasks.CreateTask(ctxFor(t), owner.actor, meta(t, "second-member", nil), base.project.ID, wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: current.ID, Title: "Second original member", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium})
		agentAppRequire(t, err)
		for index, task := range []wc.Task{v.domain.task, second.Task} {
			key := f.IdempotencyKey("cancel-first-member")
			if index == 1 {
				key = "cancel-second-member"
			}
			result, err := v.domain.transitions.TransferTask(ctxFor(t), owner.actor, meta(t, string(key), &task.Version), base.project.ID, task.ID, wc.TaskTransfer{TargetState: wc.TaskStateCancelled, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
			agentAppRequire(t, err)
			if result.Task.State != wc.TaskStateCancelled || result.Task.SprintID != current.ID {
				t.Fatal("formal member cancellation lost original Sprint")
			}
		}
		tasks := sprintCompleteTaskRows(t, base)
		project, err = base.projects.GetSchedulerConfig(ctxFor(t), owner.actor, base.project.ID)
		agentAppRequire(t, err)
		body, lookup = sprintCompleteIntent(t, current)
		masked := sprintCompleteUnknown(t, base, "complete-members")
		unknown := v.request(t, owner, "POST", path, body, "complete-members")
		masked.stop()
		var problem struct {
			CommitState string `json:"commit_state"`
		}
		if unknown.status != 503 || json.Unmarshal(unknown.body, &problem) != nil || problem.CommitState != "unknown" || masked.count.Load() != 2 {
			t.Fatal("original COMMIT and confirmation ACK did not preserve Unknown")
		}
		// The explicit Lookup consumes the same original key, version and body;
		// no second Complete is used to discover the original outcome.
		found := v.request(t, owner, "POST", variableHTTPPath(base.project.ID, "/sprint-lifecycle-commands/lookup"), lookup, "complete-members")
		var result struct {
			Status  wc.LookupState             `json:"status"`
			Receipt *wc.SprintCompleteMutation `json:"receipt"`
		}
		if found.status != 200 || json.Unmarshal(found.body, &result) != nil || result.Status != wc.LookupCommitted || result.Receipt == nil {
			t.Fatal("explicit original completion Lookup was not committed")
		}
		sprintCompleteCheck(t, base, current, project, "complete-members", *result.Receipt)
		if sprintCompleteTaskRows(t, base) != tasks {
			t.Fatal("completion changed member bytes, versions or Sprint ownership")
		}
		after = sprintCompleteSnapshot(t, v)
		sprintCompleteLookup(t, v, lookup, "complete-members", *result.Receipt)
		if sprintCompleteSnapshot(t, v) != after {
			t.Fatal("repeated original Lookup wrote business or activity facts")
		}
		next := sprintCompleteCreate(t, v, current, "next-explicit")
		config, err := base.projects.GetSchedulerConfig(ctxFor(t), owner.actor, base.project.ID)
		agentAppRequire(t, err)
		if config.Project.CurrentSprintID != nil || next.State != wc.Planned {
			t.Fatal("completion automatically started the next Sprint")
		}
		_ = sprintCompleteStart(t, v, next, "explicit-next-start")
	})
	t.Run("final-completion-lock-excludes-new-member", func(t *testing.T) {
		v := assembleTaskHumanHTTPFixture(t, true)
		base := v.domain.base
		empty := sprintCompleteCreate(t, v, sprintCompleteGet(t, v, v.domain.task.SprintID), "locked-empty")
		current := sprintCompleteStart(t, v, empty, "locked-start")
		project, err := base.projects.GetSchedulerConfig(ctxFor(t), base.ownerBrowser.actor, base.project.ID)
		agentAppRequire(t, err)
		identity, err := wc.SprintCompleteIdentity(base.project.ID, "locked-complete")
		agentAppRequire(t, err)
		reached, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unlock := func() { once.Do(func() { close(release) }) }
		var pid atomic.Int32
		base.tracked.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || pid.Load() != 0 {
				return nil
			}
			x, err := base.tracked.InTx(tx)
			if err != nil {
				return err
			}
			var completed bool
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.sprint_complete_commands WHERE project_id=$1 AND idempotency_key=$2 AND state='completed')`, base.project.ID.String(), "locked-complete").Scan(&completed); err != nil {
				return err
			}
			if !completed {
				return nil
			}
			var p int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&p); err != nil {
				return err
			}
			pid.Store(p)
			close(reached)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		t.Cleanup(func() {
			unlock()
			base.tracked.setAfter(nil)
			base.tracked.mu.Lock()
			base.tracked.beforeLocks = nil
			base.tracked.mu.Unlock()
		})
		body, _ := sprintCompleteIntent(t, current)
		completed := make(chan variableHTTPResponse, 1)
		completionDone := make(chan struct{})
		go func() {
			defer close(completionDone)
			completed <- v.request(t, base.ownerBrowser, "POST", variableHTTPPath(base.project.ID, "/sprints/"+current.ID.String()+"/complete"), body, "locked-complete")
		}()
		t.Cleanup(func() { unlock(); await(t, completionDone) })
		select {
		case <-reached:
		case <-completionDone:
			t.Fatal("completion returned before final held transaction")
		case <-time.After(5 * time.Second):
			t.Fatal("completion did not reach its original final transaction")
		}
		// Both formal mutations take the same Owner's User EX gate before
		// Project/Schedule/Sprint. Observe that first real blocking lock;
		// waiting for a later Project lock would never be reached here.
		key, err := f.UserLock(base.ownerBrowser.actor.Details().UserID)
		agentAppRequire(t, err)
		attempt := observeLock(base.tracked, key)
		createDone := make(chan struct{})
		createResult := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		newID := id[wc.Task](t)
		commandMeta := meta(t, "competing-member", nil)
		go func() {
			defer close(createDone)
			_, e := v.domain.tasks.CreateTask(ctx, base.ownerBrowser.actor, commandMeta, base.project.ID, wc.TaskCreate{TaskID: newID, SprintID: current.ID, Title: "Must not enter completed Sprint", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium})
			createResult <- e
		}()
		t.Cleanup(func() { unlock(); cancel(); await(t, createDone) })
		var blocked lockAttempt
		select {
		case blocked = <-attempt:
		case <-createDone:
			t.Fatal("competing member did not request original Owner lock")
		case <-time.After(5 * time.Second):
			t.Fatal("competing member lock not observed")
		}
		base.waitLock(t, blocked, false, pid.Load())
		unlock()
		await(t, completionDone)
		await(t, createDone)
		select {
		case reply := <-completed:
			sprintCompleteReceipt(t, v, current, project, "locked-complete", reply)
		default:
			t.Fatal("completion returned without response")
		}
		var fault *f.Fault
		if err := <-createResult; !errors.As(err, &fault) || fault.Code != f.TaskSprintInvalid {
			t.Fatal("TaskCreate did not reject the actual completed Sprint")
		}
		var count int
		agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.tasks WHERE id=$1`, newID.String()).Scan(&count))
		if count != 0 {
			t.Fatal("blocked TaskCreate published a member after completion")
		}
	})
}

func sprintCompleteGet(t *testing.T, v *taskHumanHTTPFixture, s wc.SprintID) wc.Sprint {
	t.Helper()
	reply := v.request(t, v.domain.base.ownerBrowser, "GET", variableHTTPPath(v.domain.base.project.ID, "/sprints/"+s.String()), "", "")
	var result wc.Sprint
	if reply.status != 200 || json.Unmarshal(reply.body, &result) != nil || result.ID != s {
		t.Fatal("real Sprint GET failed")
	}
	return result
}
func sprintCompleteCreate(t *testing.T, v *taskHumanHTTPFixture, parent wc.Sprint, name string) wc.Sprint {
	t.Helper()
	id := id[pc.Sprint](t)
	_, err := v.domain.structure.CreateSprint(ctxFor(t), v.domain.base.ownerBrowser.actor, meta(t, "create-"+name, nil), v.domain.base.project.ID, wc.CreateSprintRequest{SprintID: id, MilestoneID: parent.MilestoneID, Title: name})
	agentAppRequire(t, err)
	return sprintCompleteGet(t, v, id)
}
func sprintCompleteStart(t *testing.T, v *taskHumanHTTPFixture, s wc.Sprint, key f.IdempotencyKey) wc.Sprint {
	t.Helper()
	body, _ := sprintCompleteIntent(t, s)
	reply := v.request(t, v.domain.base.ownerBrowser, "POST", variableHTTPPath(v.domain.base.project.ID, "/sprints/"+s.ID.String()+"/start"), body, key)
	var result wc.SprintStartMutation
	if reply.status != 200 || json.Unmarshal(reply.body, &result) != nil || result.Validate() != nil || result.Sprint.ID != s.ID || result.Sprint.Version != s.Version+1 {
		t.Fatal("explicit original Sprint Start failed")
	}
	return result.Sprint
}
func sprintCompleteIntent(t *testing.T, s wc.Sprint) (string, string) {
	t.Helper()
	return string(jsonBytes(t, map[string]any{"expected_version": s.Version, "request": map[string]any{}})), string(jsonBytes(t, map[string]any{"command": wc.SprintCompleteCommandName, "target_id": s.ID, "expected_version": s.Version, "request": map[string]any{}}))
}
func sprintCompleteReceipt(t *testing.T, v *taskHumanHTTPFixture, before wc.Sprint, project pc.SchedulerProject, key f.IdempotencyKey, reply variableHTTPResponse) wc.SprintCompleteMutation {
	t.Helper()
	var result wc.SprintCompleteMutation
	if reply.status != 200 || json.Unmarshal(reply.body, &result) != nil {
		t.Fatal("original HTTPS completion receipt invalid")
	}
	sprintCompleteCheck(t, v.domain.base, before, project, key, result)
	return result
}
func sprintCompleteLookup(t *testing.T, v *taskHumanHTTPFixture, body string, key f.IdempotencyKey, want wc.SprintCompleteMutation) {
	t.Helper()
	reply := v.request(t, v.domain.base.ownerBrowser, "POST", variableHTTPPath(v.domain.base.project.ID, "/sprint-lifecycle-commands/lookup"), body, key)
	var result struct {
		Status  wc.LookupState             `json:"status"`
		Receipt *wc.SprintCompleteMutation `json:"receipt"`
	}
	if reply.status != 200 || json.Unmarshal(reply.body, &result) != nil || result.Status != wc.LookupCommitted || result.Receipt == nil || !reflect.DeepEqual(*result.Receipt, want) {
		t.Fatal("Lookup lost the full original completion receipt")
	}
}
func sprintCompleteCheck(t *testing.T, base *variableHTTPFixture, before wc.Sprint, project pc.SchedulerProject, key f.IdempotencyKey, result wc.SprintCompleteMutation) {
	t.Helper()
	if err := sprintCompleteCommitted(ctxFor(t), base, before, project, key, result); err != nil {
		t.Fatal("original completion durable facts differ", err)
	}
}
func sprintCompleteCommitted(ctx context.Context, base *variableHTTPFixture, before wc.Sprint, project pc.SchedulerProject, key f.IdempotencyKey, result wc.SprintCompleteMutation) error {
	if result.Validate() != nil {
		return errors.New("invalid original completion receipt")
	}
	expected := before.Clone()
	expected.State = wc.Completed
	expected.Version++
	expected.UpdatedAt = result.Sprint.UpdatedAt
	expected.CompletedAt = &expected.UpdatedAt
	expected.CompletedBy = &wc.ActorHistory{Kind: base.ownerBrowser.actor.Details().Kind, UserID: base.ownerBrowser.actor.Details().UserID}
	p := project.Project.Clone()
	p.Version++
	p.UpdatedAt = expected.UpdatedAt
	p.CurrentSprintID = nil
	if !reflect.DeepEqual(result.Sprint, expected) || !reflect.DeepEqual(result.Project, p) {
		return errors.New("completion changed fields beyond its two postimages")
	}
	meta := f.CommandMeta{RequestID: idFromSprint(before.ID), IdempotencyKey: key, ExpectedVersion: &before.Version}
	semantic, err := wc.CompleteSprintDigest(base.ownerBrowser.actor, meta, base.project.ID, before.ID)
	if err != nil {
		return err
	}
	var raw []byte
	var joined bool
	err = base.raw.QueryRow(ctx, `SELECT c.receipt,
 c.actor_user_id=$4::text::uuid AND c.expected_version=$5 AND c.semantic_digest=$6 AND c.state='completed' AND c.committed_at IS NOT NULL
 AND c.receipt->'sprint'=c.plan->'after'->'sprint' AND c.receipt->'project'=c.plan->'after'->'project'
 AND c.plan->'before'=$7::jsonb AND c.plan->'project'=$8::jsonb
 AND p.current_sprint_id IS NULL AND p.version=$9 AND p.updated_at=$10 AND p.scheduler_enabled=$11 AND p.scheduler_max_concurrency IS NOT DISTINCT FROM $12::bigint
 AND s.version=$13 AND s.completed_at=$10 AND s.updated_at=$10 AND s.completed_by->>'user_id'=$4::text
 AND e.id=$14::uuid AND e.producer='work' AND e.event_type='work.sprint_completed' AND e.schema_version=1 AND e.aggregate_id=s.id AND e.aggregate_version=s.version
 AND convert_from(e.payload,'UTF8')::jsonb->>'command_id'=c.id::text AND convert_from(e.payload,'UTF8')::jsonb->>'actor_user_id'=$4::text
 AND convert_from(e.payload,'UTF8')::jsonb->>'milestone_id'=s.milestone_id::text AND convert_from(e.payload,'UTF8')::jsonb->>'project_version'=p.version::text
 AND c.xmin=p.xmin AND c.xmin=s.xmin AND c.xmin=e.xmin
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text)
 FROM agenteam_work.sprint_complete_commands c JOIN agenteam_project.projects p ON p.id=c.project_id JOIN agenteam_work.sprints s ON s.id=c.sprint_id JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.sprint_id=$2::uuid AND c.idempotency_key=$3`, base.project.ID.String(), before.ID.String(), string(key), base.ownerBrowser.actor.Details().UserID, int64(before.Version), semantic.String(), mustSprintJSON(before), mustSprintJSON(project.Project), int64(result.Project.Version), result.Sprint.UpdatedAt.Time(), project.Config.Enabled, project.Config.MaxConcurrency, int64(result.Sprint.Version), result.EventID.String()).Scan(&raw, &joined)
	if err != nil {
		return err
	}
	var stored wc.SprintCompleteMutation
	if !joined || json.Unmarshal(raw, &stored) != nil || !reflect.DeepEqual(stored, result) {
		return errors.New("completion command/event/Project/Sprint are not exact original same-transaction facts")
	}
	return nil
}
func idFromSprint(s wc.SprintID) f.ID[f.Request] { v, _ := f.ParseID[f.Request](s.String()); return v }
func mustSprintJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
func sprintCompleteTaskRows(t *testing.T, base *variableHTTPFixture) string {
	t.Helper()
	var raw string
	agentAppRequire(t, base.raw.QueryRow(ctxFor(t), `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb)::text FROM agenteam_work.tasks t WHERE project_id=$1`, base.project.ID.String()).Scan(&raw))
	return raw
}
func sprintCompleteSnapshot(t *testing.T, v *taskHumanHTTPFixture) string {
	t.Helper()
	raw, err := sprintCompleteSnapshotContext(ctxFor(t), v.domain.base)
	agentAppRequire(t, err)
	return raw
}
func sprintCompleteSnapshotContext(ctx context.Context, base *variableHTTPFixture) (string, error) {
	var value string
	err := base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'sprints',(SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY id),'[]'::jsonb) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.sprint_complete_commands c WHERE project_id=$1::text::uuid),
 'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid),
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2::uuid))::text`, base.project.ID.String(), base.ownerBrowser.actor.Details().SessionID).Scan(&value)
	return value, err
}

type sprintCompleteMask struct {
	count atomic.Int32
	stop  func()
}

func sprintCompleteUnknown(t *testing.T, base *variableHTTPFixture, key f.IdempotencyKey) *sprintCompleteMask {
	t.Helper()
	identity, err := wc.SprintCompleteIdentity(base.project.ID, key)
	agentAppRequire(t, err)
	var completed atomic.Bool
	mask := &sprintCompleteMask{}
	base.tracked.mu.Lock()
	base.tracked.after = func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() {
			return nil
		}
		x, err := base.tracked.InTx(tx)
		if err != nil {
			return err
		}
		var done bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.sprint_complete_commands WHERE project_id=$1 AND idempotency_key=$2 AND state='completed')`, base.project.ID.String(), string(key)).Scan(&done); err != nil {
			return err
		}
		completed.Store(done)
		return nil
	}
	base.tracked.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == identity.Canonical() && completed.Load() && result.State() == f.Committed && mask.count.Load() < 2 {
			mask.count.Add(1)
			return f.UnknownResult(result.AttemptID(), result.Cause())
		}
		return result
	}
	base.tracked.mu.Unlock()
	mask.stop = func() {
		base.tracked.mu.Lock()
		base.tracked.after = nil
		base.tracked.transformResult = nil
		base.tracked.mu.Unlock()
	}
	t.Cleanup(mask.stop)
	return mask
}
