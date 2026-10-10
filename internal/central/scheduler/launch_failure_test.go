package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type failureTestPlan struct{ locks []f.LockRequest }

func (p *failureTestPlan) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}

type failureTestApplied struct {
	owner   *failureTestWork
	plan    wc.TaskLaunchFailurePlan
	tx      f.Tx
	changed bool
}

func (a *failureTestApplied) Changed() bool { return a.changed }

// A controlled Work port tests owner/callback/commit ordering, not the actual
// restoration algorithm or PG history. Those belong to Work and the joint
// real fixture. Both changed and preserved results require this issuer.
type failureTestWork struct {
	store                    *busyTestStore
	authority                *PendingAuthority
	discover, apply, checked int
	changed                  bool
	onDiscover               func(context.Context) error
	badApplied               bool
}

func (w *failureTestWork) DiscoverTaskLaunchFailure(ctx context.Context, actor i.Actor, req wc.TaskLaunchFailureRequest) (wc.TaskLaunchFailurePlan, error) {
	w.discover++
	r := w.store.row
	locks, _ := failureLocks(r.project, r.id, r)
	cmd, _ := handoffCommand(r.project, r.id, "controlled_failure_discovery")
	cause, _ := f.NewCommandsCause(cmd)
	result := w.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := w.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		facts, err := w.authority.RequireTaskLaunchFailureDiscoveryInTx(ctx, tx, actor, req)
		if err != nil {
			return err
		}
		if facts.Guard.TaskID != req.Claim.TaskID || facts.Guard.ClaimedVersion != req.Claim.ExpectedTaskVersion+1 || facts.Reason != wc.TaskLaunchFailureUnsupportedResourceConstraints {
			return errors.New("changed guard")
		}
		return nil
	})
	if err := commitError(result); err != nil {
		return nil, err
	}
	if w.onDiscover != nil {
		if err := w.onDiscover(ctx); err != nil {
			return nil, err
		}
	}
	return &failureTestPlan{locks}, nil
}
func (w *failureTestWork) ApplyTaskLaunchFailureInTx(ctx context.Context, tx f.Tx, actor i.Actor, req wc.TaskLaunchFailureRequest, plan wc.TaskLaunchFailurePlan) (wc.AppliedTaskLaunchFailure, error) {
	w.apply++
	if _, err := w.authority.RequireTaskLaunchFailureInTx(ctx, tx, actor, req, plan); err != nil {
		return nil, err
	}
	owner := w
	if w.badApplied {
		owner = &failureTestWork{}
	}
	return &failureTestApplied{owner, plan, tx, w.changed}, nil
}
func (w *failureTestWork) CheckTaskLaunchFailureAppliedInTx(ctx context.Context, tx f.Tx, actor i.Actor, req wc.TaskLaunchFailureRequest, plan wc.TaskLaunchFailurePlan, applied wc.AppliedTaskLaunchFailure) error {
	w.checked++
	a, ok := applied.(*failureTestApplied)
	if !ok || a.owner != w || a.plan != plan || a.tx != tx {
		return fault(f.Forbidden)
	}
	_, err := w.authority.RequireTaskLaunchFailureInTx(ctx, tx, actor, req, plan)
	return err
}

func newFailureTest(t *testing.T) (*LaunchFailureFinalizer, *busyTestStore, *failureTestWork) {
	t.Helper()
	_, old, _ := newHandoffTest(t)
	r := old.row
	r.version, r.attempts, r.finalAttempt, r.outcome = 3, 1, 1, KnownNotCreated
	r.launch.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{}`)}
	r.digest, _ = r.launch.Digest()
	r.failureReason, r.failureCode = wc.TaskLaunchFailureUnsupportedResourceConstraints, f.DependencyUnbound
	r.failureOccurredAt = cloneInstant(&r.updatedAt)
	store := &busyTestStore{handoffTestStore: *old}
	a, _ := NewPendingAuthority(store)
	work := &failureTestWork{store: store, authority: a, changed: true}
	project := claimTestProject{pc.SchedulerProject{Project: pc.ProjectRef{ID: r.project}, Config: pc.ProjectSchedulerConfig{Enabled: true}}}
	s, err := NewLaunchFailureFinalizer(a, LaunchFailureFinalizerDependencies{Projects: project, Work: work})
	if err != nil {
		t.Fatal(err)
	}
	return s, store, work
}

func TestSchedulerLaunchFailureCodecKeepsLegacyAndStrictAttempt(t *testing.T) {
	for _, mode := range []string{"pending", "failed", "legacy", "partial", "other-attempt", "unknown", "busy", "no-constraints", "bad-time"} {
		t.Run(mode, func(t *testing.T) {
			_, store, _ := newFailureTest(t)
			r := store.row
			if mode == "failed" {
				r.status = Failed
				r.failedAt = cloneInstant(&r.updatedAt)
			}
			if mode == "legacy" {
				r.finalAttempt, r.failureReason, r.failureCode, r.failureOccurredAt = 0, "", "", nil
			}
			if mode == "no-constraints" {
				r.launch.Policy.AllowedResourceConstraints = []json.RawMessage{}
				r.digest, _ = r.launch.Digest()
			}
			values := recordValues(t, r)
			switch mode {
			case "partial":
				values[25] = (*string)(nil)
			case "other-attempt":
				n := int64(2)
				values[24] = &n
			case "unknown":
				values[10] = string(Unknown)
			case "busy":
				n := int64(1)
				values[21] = &n
			case "bad-time":
				at := r.updatedAt.Time().Add(time.Second)
				values[27] = &at
			}
			got, err := scanDispatch(dispatchTestRow{values: values})
			if mode != "pending" && mode != "failed" && mode != "legacy" {
				if err == nil || got != nil {
					t.Fatal("malformed durable rejection accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "legacy" {
				if pendingFinalFailure(got) || completedFinalFailure(got) {
					t.Fatal("legacy outcome became final proof")
				}
				return
			}
			view := snapshot(got)
			first := view.Summary()
			*first.FailureOccurredAt = f.Instant{}
			if mode == "failed" {
				*first.FailedAt = f.Instant{}
			}
			if view.Summary().FailureOccurredAt.Validate() != nil || mode == "failed" && view.Summary().FailedAt.Validate() != nil {
				t.Fatal("summary aliases evidence")
			}
		})
	}
}

func TestSchedulerLaunchFailureSettlementChecksCurrentProofAndCommit(t *testing.T) {
	for _, mode := range []string{"changed", "preserved", "paused", "pause-after-discovery", "foreign-applied", "rollback", "other-pending", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			s, store, work := newFailureTest(t)
			work.changed, work.badApplied = mode != "preserved", mode == "foreign-applied"
			pause := func() {
				s.deps.Projects = claimTestProject{pc.SchedulerProject{Project: pc.ProjectRef{ID: store.row.project}, Config: pc.ProjectSchedulerConfig{Enabled: false}}}
			}
			switch mode {
			case "paused":
				pause()
			case "pause-after-discovery":
				work.onDiscover = func(context.Context) error { pause(); return nil }
			case "rollback":
				store.failName, store.failMode = "finalize_launch_failure", "rollback-after"
			case "other-pending":
				store.otherPending = true
			case "legacy":
				store.row.finalAttempt, store.row.failureReason, store.row.failureCode, store.row.failureOccurredAt = 0, "", "", nil
			}
			out, err := s.FinalizeLaunchFailure(context.Background(), store.row.project, store.row.id)
			if mode != "changed" && mode != "preserved" {
				if err == nil || out.data != nil || store.row.status != Pending || store.row.version != 3 {
					t.Fatal("invalid proof or failed commit settled", err)
				}
				return
			}
			if err != nil || out.Summary().Status != Failed || out.Summary().FailedAt == nil || work.apply != 1 || work.checked != 1 || store.row.attempts != 1 || store.row.version != 4 {
				t.Fatal("same-Tx settlement failed", err)
			}
			if _, err = s.FinalizeLaunchFailure(context.Background(), store.row.project, store.row.id); err != nil || work.apply != 1 {
				t.Fatal("replay invoked Work", err)
			}
		})
	}
}

func TestSchedulerLaunchFailurePrivateProofCannotBeReplayed(t *testing.T) {
	s, store, _ := newFailureTest(t)
	r := store.row
	ctx, call, err := s.begin(context.Background(), r.project, r.id, false)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := failureRequest(r)
	call.record, call.request, call.stage, call.live = r, req, failureDiscovery, true
	locks, _ := failureLocks(r.project, r.id, r)
	plan := &failureTestPlan{locks}
	cause, _ := f.NewCommandsCause(failureCommand(r.project, r.id))
	for _, mode := range []string{"no-private", "foreign-tx", "foreign-plan", "changed-attempt", "missing-held", "retired"} {
		t.Run(mode, func(t *testing.T) {
			result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				if err := store.AcquireAll(ctx, tx, locks); err != nil {
					return err
				}
				call.live, call.stage, call.tx, call.plan, call.locks = true, failureApplying, tx, plan, locks
				request, checkCtx := req, ctx
				var checkPlan wc.TaskLaunchFailurePlan = plan
				switch mode {
				case "no-private":
					checkCtx = context.Background()
				case "foreign-tx":
					tx = f.NewTx()
				case "foreign-plan":
					checkPlan = &failureTestPlan{locks}
				case "changed-attempt":
					request.LaunchAttempt++
				case "missing-held":
					store.locks = nil
				case "retired":
					call.live = false
				}
				if _, err := s.authority.RequireTaskLaunchFailureInTx(checkCtx, tx, call.actor, request, checkPlan); err == nil {
					return errors.New("foreign final-failure proof accepted")
				}
				return nil
			})
			if result.State() != f.Committed {
				t.Fatal(result.Fault())
			}
		})
	}
	s.finish(call, false, nil)
}

func TestSchedulerLaunchFailureUnknownOnlyObservesOriginalSettlement(t *testing.T) {
	for _, mode := range []string{"unknown-before", "unknown-after"} {
		t.Run(mode, func(t *testing.T) {
			s, store, work := newFailureTest(t)
			store.failName, store.failMode = "finalize_launch_failure", mode
			out, err := s.FinalizeLaunchFailure(context.Background(), store.row.project, store.row.id)
			original, ok := UnknownAttempt(err)
			if !ok || original.AttemptID() != store.attempt || out.data != nil {
				t.Fatal("lost original physical Unknown", err)
			}
			count := work.apply
			if _, duplicate := s.FinalizeLaunchFailure(context.Background(), store.row.project, store.row.id); duplicate != err || work.apply != count {
				t.Fatal("Unknown reapplied Work")
			}
			s.Stop()
			out, observed := s.Lookup(context.Background(), store.row.project, store.row.id)
			if work.apply != count {
				t.Fatal("Lookup invoked Work")
			}
			if mode == "unknown-before" {
				if observed != err || out.data != nil || s.Joined() {
					t.Fatal("absence became rollback proof")
				}
			} else if observed != nil || out.Summary().Status != Failed || !s.Joined() {
				t.Fatal("original outcome not retired", observed)
			}
		})
	}
}

func TestSchedulerLaunchFailureStopWaitsOriginalWorkReturn(t *testing.T) {
	s, store, work := newFailureTest(t)
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	work.onDiscover = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return ctx.Err()
	}
	done := make(chan error, 1)
	p, id := store.row.project, store.row.id
	go func() { _, err := s.FinalizeLaunchFailure(context.Background(), p, id); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("original Work call not reached")
	}
	s.Stop()
	<-canceled
	if s.Joined() {
		t.Fatal("cancel replaced actual Work return")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Drain(ctx) == nil {
		t.Fatal("Drain skipped original call")
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) || !s.Joined() || work.apply != 0 || store.row.status != Pending {
		t.Fatal("original return or zero write lost", err)
	}
}

func TestSchedulerLaunchFailureClassifiesOnlyOriginalBoundRejection(t *testing.T) {
	for _, mode := range []string{"confirmed", "plain-code", "wrong-digest", "wrong-request", "unknown", "canceled", "checkpoint-rollback", "checkpoint-unknown"} {
		t.Run(mode, func(t *testing.T) {
			h, store, execution := newHandoffTest(t)
			store.row.launch.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{}`)}
			store.row.digest, _ = store.row.launch.Digest()
			execution.onLaunch = func(context.Context) error {
				r := execution.lastRequest.Clone()
				if mode == "wrong-digest" {
					r.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`true`)}
				}
				if mode == "wrong-request" {
					r.Meta.RequestID = dispatchTestID[f.Request](t, 999)
				}
				cause := wc.RejectTaskResourceConstraints(r)
				switch mode {
				case "plain-code":
					return f.NewFault(f.DependencyUnbound, f.NotStarted)
				case "unknown":
					return f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
				case "canceled":
					return errors.Join(context.Canceled, cause)
				}
				return cause
			}
			if mode == "checkpoint-rollback" {
				store.failName, store.failMode = "reject_launch", "rollback-after"
			}
			if mode == "checkpoint-unknown" {
				store.failName, store.failMode = "reject_launch", "unknown-after"
			}
			out, err := h.LaunchOnce(context.Background(), store.row.project, store.row.id)
			if err == nil || execution.launches != 1 || execution.created != nil || store.row.attempts != 1 {
				t.Fatal("original actual rejection not used")
			}
			wantMarker := mode == "confirmed" || mode == "checkpoint-unknown"
			if pendingFinalFailure(store.row) != wantMarker {
				t.Fatal("classification widened or lost original attempt")
			}
			if mode == "checkpoint-rollback" || mode == "checkpoint-unknown" || mode == "unknown" {
				if out.data != nil || len(h.calls) != 1 {
					t.Fatal("uncertain outcome published/retired")
				}
			} else if out.Summary().LaunchOutcome != KnownNotCreated || len(h.calls) != 0 {
				t.Fatal("known rejection not observed", err)
			}
			if mode == "checkpoint-unknown" {
				h.Stop()
				observed, e := h.Lookup(context.Background(), store.row.project, store.row.id)
				if e != nil || observed.Summary().FailureReason != wc.TaskLaunchFailureUnsupportedResourceConstraints || !h.Joined() || execution.launches != 1 || execution.lookups != 0 {
					t.Fatal("checkpoint recovery resent or lost marker", e)
				}
			}
		})
	}
}

func TestSchedulerPendingVisitFinalFailureKeepsOriginalOwners(t *testing.T) {
	for _, mode := range []string{"settle", "paused", "no-finalizer", "retained-unknown"} {
		t.Run(mode, func(t *testing.T) {
			old, store, project, execution, busyWork := newVisitTest(t)
			_, classified, _ := newFailureTest(t)
			store.row, store.rows = classified.row, []*dispatchRecord{classified.row}
			work := &failureTestWork{store: store.busyTestStore, authority: old.authority, changed: true}
			finalizer, err := NewLaunchFailureFinalizer(old.authority, LaunchFailureFinalizerDependencies{Projects: project, Work: work})
			if err != nil {
				t.Fatal(err)
			}
			visitor, err := NewPendingVisitorWithFailure(old.authority, old.handoff, old.busy, finalizer)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "no-finalizer" {
				visitor = old
			}
			if mode == "paused" {
				project.enabled = false
			}
			var prior error
			if mode == "retained-unknown" {
				store.failName, store.failMode = "finalize_launch_failure", "unknown-before"
				_, prior = finalizer.FinalizeLaunchFailure(context.Background(), store.row.project, store.row.id)
				if _, ok := UnknownAttempt(prior); !ok {
					t.Fatal("control did not retain actual Unknown")
				}
			}
			out, err := visitor.VisitNext(context.Background(), store.row.project, nil)
			if !out.Found || execution.launches != 0 || execution.lookups != 0 || busyWork.apply != 0 {
				t.Fatal("final settlement launched or entered Busy")
			}
			switch mode {
			case "settle":
				if err != nil || out.Action != PendingVisitFailure || out.Dispatch.Summary().Status != Failed || work.apply != 1 {
					t.Fatal("visitor did not call actual finalizer", err)
				}
			case "retained-unknown":
				if err != prior || work.apply != 0 || store.row.status != Pending {
					t.Fatal("visitor replaced original Unknown owner", err)
				}
			default:
				if err != nil || out.Action != PendingVisitDeferred || store.row.status != Pending || work.discover != 0 {
					t.Fatal("paused/unbound visit settled")
				}
			}
		})
	}
}
