//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// All five command services seed through the same explicit WithReaders pair.
// The three returned readers then execute eight real, nonempty public reads.
// This is not default App binding, borrowed-transaction admission or cleanup.
func TestProjectWorkReaderStop(t *testing.T) {
	t.Run("eight-nonempty-reads-and-original-store-return", testWorkReaderStopReturn)
	t.Run("reader-technical-unknown-readonly-confirmation", testWorkReaderStopUnknown)
	t.Run("archive-preserves-original-and-new-reads", testWorkReaderStopArchive)
}

type workReaderStopPG struct {
	*workStopPG
	sprint   wc.SprintID
	task     wc.Task
	retained bool
}

func newWorkReaderStopPG(t *testing.T) *workReaderStopPG {
	t.Helper()
	v := &workReaderStopPG{workStopPG: newWorkStopPG(t, false, true)}
	if v.pair.Reader() == nil || v.pair.TaskReader() == nil || v.pair.BlockerReader() == nil {
		t.Fatal("explicit owned reader graph is incomplete")
	}
	page, err := v.pair.TaskReader().ListTasks(ctxFor(t), v.base.ownerBrowser.actor, v.target.ID, wc.TaskFilter{}, f.PageRequest{Limit: 10})
	workStopRequire(t, "reader-formal-task-identities", err)
	if len(page.Items) != 2 || page.NextCursor != "" {
		t.Fatal("formal five-service seed did not contain its two Tasks")
	}
	for _, task := range page.Items {
		if task.Validate() != nil || task.ProjectID != v.target.ID || task.MilestoneID != v.milestone.MilestoneID {
			t.Fatal("formal Task identity differs")
		}
		if task.Title == "Owned task 1" {
			v.task = task
			v.sprint = task.SprintID
		}
	}
	if v.task.ID.Validate() != nil {
		t.Fatal("formally blocked Task not found")
	}
	// This cleanup is registered after the inherited one, and therefore runs
	// first: release actual returns, settle exact Unknown, then let it Drain.
	t.Cleanup(func() { v.closeReaders(t) })
	return v
}

func (v *workReaderStopPG) readCounts(t *testing.T) [4]int64 {
	t.Helper()
	var n [4]int64
	workStopRequire(t, "reader-returned-ledger-counts", v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FILTER(WHERE lane='structure'),count(*) FILTER(WHERE lane='task'),count(*) FILTER(WHERE lane='blocker'),count(*) FILTER(WHERE phase<>'business_closed') FROM agenteam_work.project_calls WHERE project_id=$1::uuid AND class='read'`, v.target.ID.String()).Scan(&n[0], &n[1], &n[2], &n[3]))
	return n
}

func (v *workReaderStopPG) eightReads(t *testing.T, outstanding ...int64) []byte {
	t.Helper()
	a, p, ctx := v.base.ownerBrowser.actor, v.target.ID, ctxFor(t)
	before := v.readCounts(t)
	var running int64
	if len(outstanding) == 1 {
		running = outstanding[0]
	}
	if before[3] != running {
		t.Fatal("eight-read control began with an unrelated active reader")
	}
	milestone, err := v.pair.Reader().GetMilestone(ctx, a, p, v.milestone.MilestoneID)
	workStopRequire(t, "reader-get-milestone", err)
	if milestone.Validate() != nil || milestone.ID != v.milestone.MilestoneID || milestone.ProjectID != p {
		t.Fatal("GetMilestone lost its formal identity")
	}
	sprint, err := v.pair.Reader().GetSprint(ctx, a, p, v.sprint)
	workStopRequire(t, "reader-get-sprint", err)
	if sprint.Validate() != nil || sprint.ID != v.sprint || sprint.MilestoneID != milestone.ID || sprint.StartedAt == nil {
		t.Fatal("GetSprint lost the formal StartSprint receipt")
	}
	milestones, err := v.pair.Reader().ListMilestones(ctx, a, p, f.PageRequest{Limit: 10})
	workStopRequire(t, "reader-list-milestones", err)
	if len(milestones.Items) != 1 || milestones.NextCursor != "" || !bytes.Equal(jsonBytes(t, milestones.Items[0]), jsonBytes(t, milestone)) {
		t.Fatal("ListMilestones differs from the original nonempty object")
	}
	sprints, err := v.pair.Reader().ListSprints(ctx, a, p, milestone.ID, f.PageRequest{Limit: 10})
	workStopRequire(t, "reader-list-sprints", err)
	if len(sprints.Items) != 1 || sprints.NextCursor != "" || !bytes.Equal(jsonBytes(t, sprints.Items[0]), jsonBytes(t, sprint)) {
		t.Fatal("ListSprints differs from the original nonempty object")
	}
	task, err := v.pair.TaskReader().GetTask(ctx, a, p, v.task.ID)
	workStopRequire(t, "reader-get-task", err)
	if task.Validate() != nil || !bytes.Equal(jsonBytes(t, task), jsonBytes(t, v.task)) {
		t.Fatal("GetTask changed the formal seed receipt")
	}
	tasks, err := v.pair.TaskReader().ListTasks(ctx, a, p, wc.TaskFilter{SprintID: &v.sprint}, f.PageRequest{Limit: 10})
	workStopRequire(t, "reader-list-tasks", err)
	if len(tasks.Items) != 2 || tasks.NextCursor != "" {
		t.Fatal("ListTasks lost the two original Tasks")
	}
	found := false
	for _, value := range tasks.Items {
		if value.Validate() != nil || value.ProjectID != p {
			t.Fatal("ListTasks returned an invalid Task")
		}
		if value.ID == task.ID {
			found = bytes.Equal(jsonBytes(t, value), jsonBytes(t, task))
		}
	}
	if !found {
		t.Fatal("ListTasks omitted the original blocked Task")
	}
	events, err := v.pair.TaskReader().ListTaskEvents(ctx, a, p, task.ID, wc.TaskTimelineFilter{}, f.PageRequest{Limit: 10})
	workStopRequire(t, "reader-list-task-events", err)
	if len(events.Items) < 2 || events.NextCursor != "" {
		t.Fatal("real create/blocker events were not returned")
	}
	blockerEvent := false
	for _, event := range events.Items {
		header, e := event.Header()
		workStopRequire(t, "reader-original-event-header", e)
		if event.Validate() != nil || header.ProjectID != p || header.TaskID != task.ID {
			t.Fatal("original Task event identity differs")
		}
		blockerEvent = blockerEvent || event.Blocker != nil
	}
	if !blockerEvent {
		t.Fatal("formal blocker event missing from Task timeline")
	}
	blockers, err := v.pair.BlockerReader().ListTaskBlockersPage(ctx, a, p, task.ID, wc.TaskBlockersAll, f.PageRequest{Limit: 10})
	workStopRequire(t, "reader-list-task-blockers", err)
	if len(blockers.Items) != 1 || blockers.NextCursor != "" || blockers.Items[0].Validate() != nil || blockers.Items[0].TaskID != task.ID || blockers.Items[0].Type != wc.TaskBlockerWaitingForHuman || blockers.Items[0].ResolvedAt != nil {
		t.Fatal("real unresolved Human blocker not returned")
	}
	after := v.readCounts(t)
	if after[0]-before[0] != 4 || after[1]-before[1] != 3 || after[2]-before[2] != 1 || after[3] != running {
		t.Fatal("eight public readers did not preserve their three returned read lanes", before, after)
	}
	if v.snapshot(t, false) != v.business {
		t.Fatal("public readers changed Work business history")
	}
	return jsonBytes(t, []any{milestone, sprint, milestones, sprints, task, tasks, events, blockers})
}

func (v *workReaderStopPG) otherReader(t *testing.T) {
	t.Helper()
	page, err := v.pair.Reader().ListMilestones(ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID, f.PageRequest{Limit: 10})
	workStopRequire(t, "reader-other-project", err)
	if len(page.Items) != 1 || page.Items[0].Validate() != nil || page.Items[0].ProjectID != v.base.project.ID {
		t.Fatal("B original nonempty reader was blocked or changed")
	}
}

func (v *workReaderStopPG) holdReader(t *testing.T) (*skillsCleanupPGCall, *workStopHeld) {
	t.Helper()
	v.clear()
	h := &workStopHeld{barrier: v.barrier()}
	var selected f.TransactionCause
	s := v.base.tracked
	s.mu.Lock()
	s.enter = func(ctx context.Context, c f.TransactionCause) {
		if ctx.Value(workStopPGTag{}) == "owned-task-reader" && c.Kind() == f.RecoveryCause && c.Details().Owner == "work.task.read" {
			h.mu.Lock()
			selected = c
			h.ctx = ctx
			h.mu.Unlock()
			h.selected.Store(true)
		}
	}
	s.afterResult = func(c f.TransactionCause, r f.CommitResult) {
		h.mu.Lock()
		exact := h.selected.Load() && c.Kind() == f.RecoveryCause && c.Details().Owner == selected.Details().Owner && c.Details().RecoveryRunID == selected.Details().RecoveryRunID
		h.mu.Unlock()
		if exact && r.State() == f.Committed && h.held.CompareAndSwap(false, true) {
			h.barrier.hold(r)
		}
	}
	s.mu.Unlock()
	call := v.start(func(ctx context.Context) error {
		got, err := v.pair.TaskReader().GetTask(context.WithValue(ctx, workStopPGTag{}, "owned-task-reader"), v.base.ownerBrowser.actor, v.target.ID, v.task.ID)
		if err == nil && !bytes.Equal(jsonBytes(t, got), jsonBytes(t, v.task)) {
			return errors.New("held reader lost its original Task")
		}
		return err
	})
	v.await(t, call, h.barrier)
	h.mu.Lock()
	live := h.ctx != nil && h.ctx.Err() == nil
	h.mu.Unlock()
	if !live {
		t.Fatal("actual owned Reader context was not live before Stop")
	}
	return call, h
}

func (v *workReaderStopPG) closeReaders(t *testing.T) {
	for _, barrier := range v.barriers {
		barrier.open()
	}
	for _, call := range v.calls {
		outboxCleanupWait(t, call.done, "original Work Reader call before cleanup")
	}
	v.clear()
	if v.retained {
		if v.cause.Validate() != nil {
			v.accept(t, pc.Delete)
		}
		// Two exact retained technical owners: one confirmation each, a
		// separate close for the registration, then the stopped observation.
		for n := 0; n < 5; n++ {
			call := v.stop(t, v.pair, false)
			outboxCleanupWait(t, call.done, "original Work Reader technical cleanup")
			if call.err != nil {
				t.Error("Reader technical cleanup failed", call.err)
				break
			}
			if v.last.Details().State == pc.Stopped {
				v.retained = false
				break
			}
		}
	}
}

func testWorkReaderStopReturn(t *testing.T) {
	v := newWorkReaderStopPG(t)
	v.eightReads(t)
	v.otherReader(t)
	call, held := v.holdReader(t)
	v.accept(t, pc.Delete)
	v.observed(t, v.pair, false, pc.StopPending)
	v.notReturned(t, call)
	held.mu.Lock()
	canceled := errors.Is(held.ctx.Err(), context.Canceled)
	held.mu.Unlock()
	if !canceled {
		t.Fatal("Delete did not cancel its actual Reader context")
	}
	v.otherReader(t)
	if v.snapshot(t, false) != v.business {
		t.Fatal("Delete Stop changed Work business history")
	}
	held.barrier.open()
	outboxCleanupWait(t, call.done, "original public Reader return")
	workStopRequire(t, "reader-original-return", call.err)
	v.clear()
	v.observed(t, v.pair, false, pc.Stopped)
	if v.readCounts(t)[3] != 0 {
		t.Fatal("Reader ownership remained running after the actual method returned")
	}
	stable := v.snapshot(t, true)
	v.observed(t, v.newPair(t, true), false, pc.StopPending)
	if v.snapshot(t, true) != stable {
		t.Fatal("different Reader instance rewrote original ownership")
	}
	v.otherReader(t)
	t.Log("PROJECT_WORK_READER_STOP_RETURN eight_nonempty_public_reads=true same_original_five_service_seed=true three_read_lanes=true original_store_and_method_return=true delete_pending_then_stopped=true other_project_readable=true different_instance_pending=true history_retained=true no_default_app=true no_work_cleanup=true")
}

func (v *workReaderStopPG) maskReaderTechnical(t *testing.T, owner string) workStopPGUnknown {
	t.Helper()
	v.clear()
	s := v.base.tracked
	var mu sync.Mutex
	selected := ""
	var original f.CommitResult
	var once atomic.Bool
	s.mu.Lock()
	s.enter = func(ctx context.Context, c f.TransactionCause) {
		d := c.Details()
		if ctx.Value(workStopPGTag{}) == owner && c.Kind() == f.RecoveryCause && d.Owner == owner && d.CheckpointRef == v.target.ID.String() {
			mu.Lock()
			selected = d.RecoveryRunID
			mu.Unlock()
		}
	}
	s.transformResult = func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		mu.Lock()
		exact := selected != "" && c.Kind() == f.RecoveryCause && c.Details().Owner == owner && c.Details().RecoveryRunID == selected
		mu.Unlock()
		if exact && r.State() == f.Committed && once.CompareAndSwap(false, true) {
			original = f.UnknownResult(id[f.TransactionAttempt](t), c)
			return original
		}
		return r
	}
	s.mu.Unlock()
	call := v.start(func(ctx context.Context) error {
		got, err := v.pair.TaskReader().GetTask(context.WithValue(ctx, workStopPGTag{}, owner), v.base.ownerBrowser.actor, v.target.ID, v.task.ID)
		if err == nil && !bytes.Equal(jsonBytes(t, got), jsonBytes(t, v.task)) {
			return errors.New("technical Unknown changed the original Task read")
		}
		return err
	})
	outboxCleanupWait(t, call.done, "original Reader technical acknowledgement")
	v.clear()
	v.retained = v.retained || once.Load()
	if !once.Load() || original.State() != f.Unknown {
		t.Fatal("Reader technical COMMIT was not selected")
	}
	if owner == "work-call-register" {
		var problem *f.Fault
		if !errors.As(call.err, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.CauseID != original.AttemptID().String() {
			agentRetirementDiagnostic(t, "reader-registration-unknown", call.err)
			t.Fatal("Reader registration lost its first Unknown identity")
		}
	} else {
		workStopRequire(t, "reader-return-unknown-keeps-business-value", call.err)
	}
	return workStopPGUnknown{original: original, id: selected, owner: owner}
}

func testWorkReaderStopUnknown(t *testing.T) {
	v := newWorkReaderStopPG(t)
	registration := v.maskReaderTechnical(t, "work-call-register")
	returned := v.maskReaderTechnical(t, "work-call-return")
	if registration.id == returned.id || v.phase(t, registration.id) != "running" || v.phase(t, returned.id) != "business_closed" {
		t.Fatal("real Reader technical outcomes differ")
	}
	v.accept(t, pc.Delete)
	// The existing confirmation helper selects the exact original cause and
	// compares counters before/after in the same f.Tx, then holds that return.
	if returned.id < registration.id {
		v.confirmTechnical(t, returned)
	}
	v.confirmTechnical(t, registration)
	if v.phase(t, registration.id) != "running" {
		t.Fatal("readonly confirmation closed a Reader in the same step")
	}
	v.observed(t, v.pair, false, pc.StopPending)
	if v.phase(t, registration.id) != "business_closed" {
		t.Fatal("explicit post-confirmation Reader close did not occur")
	}
	if registration.id < returned.id {
		v.confirmTechnical(t, returned)
	}
	v.observed(t, v.pair, false, pc.Stopped)
	v.retained = false
	v.otherReader(t)
	if v.snapshot(t, false) != v.business {
		t.Fatal("Reader confirmation changed original Work history")
	}
	t.Log("PROJECT_WORK_READER_STOP_UNKNOWN real_register_and_return_commit=true local_ack_masks=true original_cause_attempt=true same_tx_zero_writes=true original_confirmation_return=true explicit_following_close=true returned_read_value_preserved=true other_project_readable=true no_network_lost_ack=true no_work_cleanup=true")
}

func testWorkReaderStopArchive(t *testing.T) {
	v := newWorkReaderStopPG(t)
	before := v.eightReads(t)
	call, held := v.holdReader(t)
	v.accept(t, pc.Archive)
	v.observed(t, v.pair, false, pc.Stopped)
	v.notReturned(t, call)
	held.mu.Lock()
	live := held.ctx != nil && held.ctx.Err() == nil
	held.mu.Unlock()
	if !live {
		t.Fatal("Archive canceled its permitted original Reader")
	}
	v.otherReader(t)
	// The held task reader remains registered. All eight additional public
	// reads must still succeed while this same original read is outstanding.
	if !bytes.Equal(before, v.eightReads(t, 1)) {
		t.Fatal("Archive changed the eight original nonempty read projections")
	}
	v.notReturned(t, call)
	held.barrier.open()
	outboxCleanupWait(t, call.done, "original Archive-permitted Reader return")
	workStopRequire(t, "reader-archive-original-return", call.err)
	v.clear()
	v.observed(t, v.pair, false, pc.Stopped)
	v.otherReader(t)
	t.Log("PROJECT_WORK_READER_STOP_ARCHIVE original_read_uncanceled=true stopped_excludes_reads=true actual_read_return=true eight_new_reads_succeed=true other_project_readable=true business_history_unchanged=true no_default_app=true no_work_cleanup=true")
}
