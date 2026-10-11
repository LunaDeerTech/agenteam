package scheduler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Only persisted rows and physical outcomes are controlled. The existing
// Coordinator, Runner, Handoff and codecs execute unchanged. These controls
// neither create a real Work origin nor prove a PostgreSQL/Model invocation.
type reviewDispatchStore struct {
	*relaunchTestStore
	reviewHistory bool
}

func (s *reviewDispatchStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.relaunchTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *reviewDispatchStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if query == relaunchReviewHistorySQL {
		if len(args) != 2 || args[0] != s.row.project.String() || args[1] != s.row.task {
			return dispatchTestRow{err: errors.New("changed review history scope")}
		}
		// Absence of review history is independent of an existing work pending.
		return dispatchTestRow{values: []any{s.reviewHistory}}
	}
	return s.relaunchTestStore.QueryRow(ctx, query, args...)
}

func newReviewDispatchTest(t *testing.T, terminalReview bool) (*relaunchTestFixture, *reviewDispatchStore) {
	t.Helper()
	v := newRelaunchTest(t, 2)
	v.request.Purpose = "task/review"
	v.current.facts.State = wc.TaskStateInReview
	s := &reviewDispatchStore{relaunchTestStore: v.store}
	a, err := NewPendingAuthority(s)
	if err != nil {
		t.Fatal(err)
	}
	v.coordinator, err = NewCoordinator(a, v.coordinator.deps)
	if err != nil {
		t.Fatal(err)
	}
	v.owner, err = NewRelaunchCoordinator(v.coordinator, v.work, v.current, v.occupancy, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.owner.Stop)
	if terminalReview {
		reviewDispatchOrigin(t, s.row)
		v.coordinator.deps.Executions.(*relaunchTestObserver).value.Purpose = "task/review"
		// The work pair remains distinct and need not be read to visit review.
		s.runtime = relaunchRuntimeValues([]any{dispatchTestID[DispatchIdentity](t, 1100).String(), dispatchTestID[i.Execution](t, 1101).String(), nil, nil, int64(0), int64(1), s.row.updatedAt.Time(), s.row.id.String(), s.row.execution.String()})
	} else {
		// A completed work phase still has two skips. First review must neither
		// observe that Execution as a review predecessor nor consume its skips.
		s.runtime = relaunchRuntimeValues([]any{s.row.id.String(), s.row.execution.String(), s.row.execution.String(), "task/work", int64(2), int64(1), s.row.updatedAt.Time(), nil, nil})
	}
	return v, s
}

func reviewDispatchOrigin(t *testing.T, r *dispatchRecord) {
	t.Helper()
	request := relaunchRequest(t, r, 1)
	request.DispatchID, request.RequestID, request.Purpose = r.id.String(), r.launch.Meta.RequestID, "task/review"
	r.guard = nil
	r.relaunch = &wc.TaskRelaunchSource{Request: request, MilestoneID: dispatchTestID[wc.Milestone](t, 701), ReferenceDigest: f.Digest("sha256:" + strings.Repeat("1", 64))}
	r.launch.Purpose = request.Purpose
	var err error
	r.digest, err = r.launch.Digest()
	if err != nil || r.relaunch.Validate() != nil {
		t.Fatal("invalid controlled review origin", err)
	}
}

func TestSchedulerReviewEligibilityAndRunnerSelection(t *testing.T) {
	v, s := newReviewDispatchTest(t, false)
	before := append([]any(nil), s.runtime...)
	out, err := v.owner.VisitRelaunch(context.Background(), v.request, emptyClaimPolicy())
	if !errors.Is(err, relaunchAdmissionReached) || out.Dispatch.data != nil || out.CooldownSkipped || s.capacityReads != 1 || s.runtimeWrites != 0 || s.receiptWrites != 0 || v.work.records != 0 || !reflect.DeepEqual(before, s.runtime) {
		t.Fatal("first review consumed work cooldown or failed before current capacity", err)
	}
	for _, denied := range []string{"work-state", "different-reviewer", "paused", "work-pending", "work-active", "unresolved", "missing-review-head"} {
		v, s := newReviewDispatchTest(t, false)
		want := f.ConfirmationStale
		switch denied {
		case "work-state":
			v.current.facts.State = wc.TaskStateInProgress
		case "different-reviewer":
			other := dispatchTestID[i.Agent](t, 1190)
			v.current.facts.AssigneeAgentID = &other
		case "paused":
			v.project.project.Config.Enabled = false
			want = f.InvalidState
		case "work-pending":
			s.pending, want = true, f.ResourceBusy
		case "work-active":
			v.occupancy.active, want = true, f.ResourceBusy
		case "unresolved":
			v.current.facts.HasUnresolvedBlockers = true
		case "missing-review-head":
			s.reviewHistory, want = true, f.DependencyUnbound
		}
		out, err := v.owner.VisitRelaunch(context.Background(), v.request, emptyClaimPolicy())
		var issue *f.Fault
		if !errors.As(err, &issue) || issue.Code != want || out.Dispatch.data != nil || out.CooldownSkipped || s.runtimeWrites != 0 || s.receiptWrites != 0 || s.capacityReads != 0 || v.work.records != 0 {
			t.Fatalf("%s did not reject before any new phase mutation: %v", denied, err)
		}
	}

	// The real Runner selects review and the current reviewer. Stop the first
	// relaunch transaction before its callback; no Work grant is manufactured.
	runner := newRunnerTest(t, time.Millisecond)
	task := runner.addTask(t, 1200, wc.TaskStateInReview, wc.TaskPriorityHigh)
	reviewer := dispatchTestID[i.Agent](t, 1201)
	facts := runner.reader.facts[task]
	facts.AssigneeAgentID = &reviewer
	runner.reader.facts[task] = facts
	work := &relaunchTestWork{}
	owner, err := NewRelaunchCoordinator(runner.coordinator, work, runner.reader, &relaunchTestOccupancy{store: &runner.store.handoffTestStore, task: task.String()}, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Stop()
	runner.runner.options.Relaunch = owner
	runner.store.failName, runner.store.failMode = "relaunch", "rollback-before"
	visits, err := runner.runner.RunTraversal(context.Background())
	if err != nil || len(visits.Visits) != 1 {
		t.Fatal("review traversal failed outside the controlled relaunch", err)
	}
	item := visits.Visits[0]
	var issue *f.Fault
	if item.Action != ProjectVisitRelaunch || item.ExpectedState != wc.TaskStateInReview || item.RelaunchRequest == nil || item.RelaunchRequest.Purpose != "task/review" || item.RelaunchRequest.AgentID != reviewer || item.RelaunchRequest.TaskID != task || item.RelaunchRequest.ExpectedTaskVersion != facts.Version || item.DispatchID == nil || item.DispatchID.String() != item.RelaunchRequest.DispatchID || !errors.As(item.Err, &issue) || issue.Code != f.DependencyUnavailable || len(runner.claims.requests) != 0 || runner.execution.launches != 0 || work.discovered != 0 {
		t.Fatal("Runner used todo/work or dispatched after the original rejection", item.Err)
	}
}

func TestSchedulerReviewCooldownAndUnknownKeepOriginalPhase(t *testing.T) {
	v, s := newReviewDispatchTest(t, true)
	workPair := append([]any(nil), s.runtime[:2]...)
	first, err := v.owner.VisitRelaunch(context.Background(), v.request, emptyClaimPolicy())
	if err != nil || !first.CooldownSkipped || first.Remaining != 1 || s.runtimeWrites != 1 || s.receiptWrites != 1 || !reflect.DeepEqual(workPair, s.runtime[:2]) || s.runtime[3] == nil || *s.runtime[3].(*string) != "task/review" {
		t.Fatal("review terminal did not begin its own two-visit cooldown", err)
	}
	replayed, err := v.owner.VisitRelaunch(context.Background(), v.request, emptyClaimPolicy())
	if err != nil || !replayed.CooldownSkipped || replayed.Remaining != 1 || s.runtimeWrites != 1 || s.receiptWrites != 1 {
		t.Fatal("review replay deducted cooldown twice", err)
	}
	v.owner.Stop()
	next, err := NewRelaunchCoordinator(v.coordinator, v.work, v.current, v.occupancy, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Stop()
	request := v.request
	request.DispatchID, request.RequestID = dispatchTestID[DispatchIdentity](t, 1202).String(), dispatchTestID[f.Request](t, 1203)
	second, err := next.VisitRelaunch(context.Background(), request, emptyClaimPolicy())
	if err != nil || !second.CooldownSkipped || second.Remaining != 0 || s.runtimeWrites != 2 || s.receiptWrites != 2 || v.work.records != 0 || !reflect.DeepEqual(workPair, s.runtime[:2]) {
		t.Fatal("new owner lost persisted review count or dispatched at decrement-to-zero", err)
	}
	request.DispatchID, request.RequestID = dispatchTestID[DispatchIdentity](t, 1204).String(), dispatchTestID[f.Request](t, 1205)
	if _, err = next.VisitRelaunch(context.Background(), request, emptyClaimPolicy()); !errors.Is(err, relaunchAdmissionReached) || s.capacityReads != 1 || s.runtimeWrites != 2 || s.receiptWrites != 2 {
		t.Fatal("zero review count did not reach current admission exactly once", err)
	}

	u, us := newReviewDispatchTest(t, true)
	us.failName, us.failMode, us.failSkip = "relaunch", "unknown-after", 1
	_, original := u.owner.VisitRelaunch(context.Background(), u.request, emptyClaimPolicy())
	attempt, ok := UnknownAttempt(original)
	if !ok || attempt.AttemptID() != us.attempt || us.runtimeWrites != 1 || us.receiptWrites != 1 {
		t.Fatal("review lost the original physical Unknown", original)
	}
	if _, err := u.owner.VisitRelaunch(context.Background(), u.request, emptyClaimPolicy()); err != original {
		t.Fatal("Unknown review was sent as a new visit", err)
	}
	other := u.request
	other.DispatchID = dispatchTestID[DispatchIdentity](t, 1206).String()
	var busy *f.Fault
	if _, err := u.owner.VisitRelaunch(context.Background(), other, emptyClaimPolicy()); !errors.As(err, &busy) || busy.Code != f.ResourceBusy {
		t.Fatal("fresh review bypassed the retained original visit", err)
	}
	u.project.project.Config.Enabled, u.occupancy.active = false, true
	resolved, err := u.owner.ResolveRelaunch(context.Background(), u.request)
	if err != nil || !resolved.CooldownSkipped || resolved.Remaining != 1 || us.runtimeWrites != 1 || us.receiptWrites != 1 || us.capacityReads != 0 || u.work.records != 0 {
		t.Fatal("original review receipt recovery retried deduction or admission", err)
	}
	u.owner.Stop()
	if !u.owner.Joined() {
		t.Fatal("read-only review recovery did not retire its original owner")
	}

	// Existing controlled Execution acknowledges one call. Production Handoff
	// verifies its original tuple before updating only the review association.
	handoff, hs, execution := newHandoffTest(t)
	defer handoff.Stop()
	reviewDispatchOrigin(t, hs.row)
	workD, workE := dispatchTestID[DispatchIdentity](t, 1210), dispatchTestID[i.Execution](t, 1211)
	hs.associationRuntime = relaunchRuntimeValues([]any{workD.String(), workE.String(), workE.String(), "task/work", int64(1), int64(3), hs.row.updatedAt.Time(), nil, nil})
	associated, err := handoff.LaunchOnce(context.Background(), hs.row.project, hs.row.id)
	if err != nil || associated.Summary().Status != Launched || execution.launches != 1 || len(hs.associationRuntime) != 9 || *hs.associationRuntime[0].(*string) != workD.String() || *hs.associationRuntime[1].(*string) != workE.String() || *hs.associationRuntime[7].(*string) != hs.row.id.String() || *hs.associationRuntime[8].(*string) != associated.Summary().ExecutionID.String() || hs.associationRuntime[2] != nil || hs.associationRuntime[3] != nil || hs.associationRuntime[4] != int64(0) {
		t.Fatal("reliable review association overwrote work or retained its cooldown", err)
	}
	stored := append([]any(nil), hs.associationRuntime...)
	if _, err := handoff.LaunchOnce(context.Background(), hs.row.project, hs.row.id); err != nil || execution.launches != 1 || !reflect.DeepEqual(stored, hs.associationRuntime) {
		t.Fatal("historical association replay moved a phase head or resent", err)
	}
}

func TestSchedulerReviewBusyAndFinalFailureKeepOrigin(t *testing.T) {
	owner, store, work := newBusyTest(t)
	defer owner.Stop()
	reviewDispatchOrigin(t, store.row)
	before := store.row.relaunch.Clone()
	out, err := owner.CompensateAgentBusy(context.Background(), store.row.project, store.row.id)
	if err != nil || out.Summary().Status != Skipped || out.Summary().SkipReason != "agent_busy" || work.discover != 0 || work.apply != 0 || work.checked != 0 || store.associationRuntime != nil || store.row.relaunch == nil || *store.row.relaunch != before || store.row.guard != nil {
		t.Fatal("review Busy restored Work or changed phase/cooldown", err)
	}
	replay, err := owner.CompensateAgentBusy(context.Background(), store.row.project, store.row.id)
	if err != nil || replay.Summary().Version != out.Summary().Version || work.discover != 0 || work.apply != 0 || work.checked != 0 || store.associationRuntime != nil {
		t.Fatal("review Busy replay repeated compensation", err)
	}

	finalizer, fs, fw := newFailureTest(t)
	defer finalizer.Stop()
	claim, err := failureRequest(fs.row)
	if err != nil || claim.Relaunch != nil || claim.Claim.Purpose != "task/work" {
		t.Fatal("legacy todo claim failure changed", err)
	}
	oldGuard := *fs.row.guard
	reviewDispatchOrigin(t, fs.row)
	decoded, err := scanDispatch(dispatchTestRow{values: recordValues(t, fs.row)})
	if err != nil {
		t.Fatal("valid review final-failure row did not decode", err)
	}
	request, err := failureRequest(decoded)
	if err != nil || request.Relaunch == nil || *request.Relaunch != fs.row.relaunch.Request || request.Claim != (wc.TaskClaimRequest{}) || request.LaunchAttempt != fs.row.finalAttempt || request.DispatchVersion != fs.row.version || fw.discover != 0 || fw.apply != 0 {
		t.Fatal("review final-failure request lost its exact relaunch origin", err)
	}
	for _, invalid := range []string{"review-claim", "both-origins", "changed-purpose", "changed-reviewer"} {
		bad := *fs.row
		source := fs.row.relaunch.Clone()
		bad.relaunch = &source
		switch invalid {
		case "review-claim":
			bad.guard, bad.relaunch = &oldGuard, nil
		case "both-origins":
			bad.guard = &oldGuard
		case "changed-purpose":
			bad.relaunch.Request.Purpose = "task/work"
		case "changed-reviewer":
			bad.relaunch.Request.AgentID = dispatchTestID[i.Agent](t, 1220)
		}
		if _, err := failureRequest(&bad); err == nil {
			t.Fatalf("%s manufactured a final-failure origin", invalid)
		}
	}
}
