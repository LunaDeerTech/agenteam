package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type busyTestStore struct {
	handoffTestStore
	otherPending bool
}

func (s *busyTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.handoffTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *busyTestStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT EXISTS") {
		r := s.staged
		if len(args) != 5 || args[0] != r.project.String() || args[1] != r.guard.SourceSprintID || args[2] != r.guard.SourceState || args[3] != r.guard.SourcePriority || args[4] != r.id.String() || !strings.Contains(query, "id<>$5") {
			return dispatchTestRow{err: errors.New("wrong compensation group exemption")}
		}
		return dispatchTestRow{values: []any{s.otherPending}}
	}
	return s.handoffTestStore.QueryRow(ctx, query, args...)
}

type busyTestPlan struct{ locks []f.LockRequest }

func (p *busyTestPlan) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}

type busyTestApplied struct {
	owner    *busyTestWork
	plan     wc.TaskBusyCompensationPlan
	tx       f.Tx
	restored bool
}

func (a *busyTestApplied) Restored() bool { return a.restored }

// A controlled Work port tests owner/callback/commit ordering, not the actual
// restoration algorithm or PG history. Those belong to Work and the joint
// real fixture. Both restored and preserved results require this issuer.
type busyTestWork struct {
	store                    *busyTestStore
	authority                *PendingAuthority
	discover, apply, checked int
	restored                 bool
	onDiscover               func(context.Context) error
	badApplied               bool
}

func (w *busyTestWork) DiscoverTaskBusyCompensation(ctx context.Context, actor i.Actor, req wc.TaskBusyCompensationRequest) (wc.TaskBusyCompensationPlan, error) {
	w.discover++
	r := w.store.row
	locks, _ := busyLocks(r.project, r.id, r)
	cmd, _ := handoffCommand(r.project, r.id, "controlled_busy_discovery")
	cause, _ := f.NewCommandsCause(cmd)
	result := w.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := w.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		guard, err := w.authority.RequireTaskBusyCompensationDiscoveryInTx(ctx, tx, actor, req)
		if err != nil {
			return err
		}
		if guard.TaskID != req.Claim.TaskID || guard.ClaimedVersion != req.Claim.ExpectedTaskVersion+1 {
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
	return &busyTestPlan{locks}, nil
}
func (w *busyTestWork) ApplyTaskBusyCompensationInTx(ctx context.Context, tx f.Tx, actor i.Actor, req wc.TaskBusyCompensationRequest, plan wc.TaskBusyCompensationPlan) (wc.AppliedTaskBusyCompensation, error) {
	w.apply++
	if _, err := w.authority.RequireTaskBusyCompensationInTx(ctx, tx, actor, req, plan); err != nil {
		return nil, err
	}
	owner := w
	if w.badApplied {
		owner = &busyTestWork{}
	}
	return &busyTestApplied{owner, plan, tx, w.restored}, nil
}
func (w *busyTestWork) CheckTaskBusyCompensationAppliedInTx(ctx context.Context, tx f.Tx, actor i.Actor, req wc.TaskBusyCompensationRequest, plan wc.TaskBusyCompensationPlan, applied wc.AppliedTaskBusyCompensation) error {
	w.checked++
	a, ok := applied.(*busyTestApplied)
	if !ok || a.owner != w || a.plan != plan || a.tx != tx {
		return fault(f.Forbidden)
	}
	_, err := w.authority.RequireTaskBusyCompensationInTx(ctx, tx, actor, req, plan)
	return err
}

func newBusyTest(t *testing.T) (*BusyCompensator, *busyTestStore, *busyTestWork) {
	t.Helper()
	_, old, _ := newHandoffTest(t)
	r := old.row
	r.version, r.attempts, r.busyAttempt, r.outcome = 3, 1, 1, KnownNotCreated
	store := &busyTestStore{handoffTestStore: *old}
	a, _ := NewPendingAuthority(store)
	work := &busyTestWork{store: store, authority: a, restored: true}
	project := claimTestProject{pc.SchedulerProject{Project: pc.ProjectRef{ID: r.project}, Config: pc.ProjectSchedulerConfig{Enabled: true}}}
	s, err := NewBusyCompensator(a, BusyCompensatorDependencies{Projects: project, Work: work})
	if err != nil {
		t.Fatal(err)
	}
	return s, store, work
}

func TestSchedulerBusyCodecPreservesLegacyAndRejectsMalformedMarker(t *testing.T) {
	for _, status := range []Status{Pending, Launched, Failed, Skipped} {
		t.Run("legacy-"+string(status), func(t *testing.T) {
			r := dispatchTestRecord(t, 1, status)
			if status == Pending {
				r.outcome = KnownNotCreated
			}
			got, err := scanDispatch(dispatchTestRow{values: recordValues(t, r)})
			if err != nil || got.status != status || got.busyAttempt != 0 || got.skipReason != "" || got.skippedAt != nil {
				t.Fatal("legacy row inferred Busy or became unreadable", err)
			}
		})
	}
	for _, mode := range []string{"pending", "skipped", "zero-attempt", "other-attempt", "unknown", "failed", "pending-reason", "missing-reason", "missing-time", "other-time", "orphan-reason"} {
		t.Run(mode, func(t *testing.T) {
			_, store, _ := newBusyTest(t)
			r := *store.row
			if mode == "skipped" || mode == "missing-reason" || mode == "missing-time" || mode == "other-time" {
				r.status, r.skipReason = Skipped, "agent_busy"
				at := r.updatedAt
				r.skippedAt = &at
			}
			values := recordValues(t, &r)
			switch mode {
			case "zero-attempt":
				v := int64(0)
				values[21] = &v
			case "other-attempt":
				v := int64(2)
				values[21] = &v
			case "unknown":
				values[10] = string(Unknown)
			case "failed":
				values[9] = string(Failed)
			case "pending-reason":
				v := "agent_busy"
				values[22] = &v
			case "missing-reason":
				values[22] = (*string)(nil)
			case "missing-time":
				values[23] = (*time.Time)(nil)
			case "other-time":
				v := r.updatedAt.Time().Add(time.Microsecond)
				values[23] = &v
			case "orphan-reason":
				values[21] = (*int64)(nil)
				v := "agent_busy"
				values[22] = &v
			}
			got, err := scanDispatch(dispatchTestRow{values: values})
			if mode != "pending" && mode != "skipped" {
				if err == nil || got != nil {
					t.Fatal("malformed marker published partial row")
				}
				return
			}
			if err != nil || got.busyAttempt != 1 || got.status != r.status || got.skipReason != r.skipReason {
				t.Fatal("valid attempt-bound Busy row rejected", err)
			}
			if mode == "skipped" {
				d := snapshot(got)
				first := d.Summary()
				changed, _ := f.NewInstant(r.updatedAt.Time().Add(time.Second))
				*first.SkippedAt = changed
				if !d.Summary().SkippedAt.Time().Equal(got.updatedAt.Time()) {
					t.Fatal("summary exposed mutable skip time")
				}
			}
		})
	}
}

func TestSchedulerBusyRequiresRecordedOutcomeAndEnabledProject(t *testing.T) {
	for _, mode := range []string{"old-known-rejection", "unknown", "mismatched-attempt", "paused", "other-pending"} {
		t.Run(mode, func(t *testing.T) {
			s, store, work := newBusyTest(t)
			switch mode {
			case "old-known-rejection":
				store.row.busyAttempt = 0
			case "unknown":
				store.row.busyAttempt, store.row.outcome = 0, Unknown
			case "mismatched-attempt":
				store.row.busyAttempt = 2
			case "paused":
				s.deps.Projects = claimTestProject{pc.SchedulerProject{Project: pc.ProjectRef{ID: store.row.project}, Config: pc.ProjectSchedulerConfig{Enabled: false}}}
			case "other-pending":
				store.otherPending = true
			}
			out, err := s.CompensateAgentBusy(context.Background(), store.row.project, store.row.id)
			if err == nil || out.data != nil || store.row.status != Pending || work.apply != 0 {
				t.Fatal("invalid Busy/paused/group proof mutated or skipped", err)
			}
		})
	}
}

func TestSchedulerBusySettlementRequiresWorkProofInOriginalTransaction(t *testing.T) {
	for _, mode := range []string{"restored", "preserved", "foreign-applied", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			s, store, work := newBusyTest(t)
			work.restored = mode != "preserved"
			work.badApplied = mode == "foreign-applied"
			if mode == "rollback" {
				store.failName, store.failMode = "compensate_agent_busy", "rollback-after"
			}
			out, err := s.CompensateAgentBusy(context.Background(), store.row.project, store.row.id)
			if mode == "foreign-applied" || mode == "rollback" {
				if err == nil || out.data != nil || store.row.status != Pending || store.row.version != 3 {
					t.Fatal("failed applied/outer commit returned skipped")
				}
				return
			}
			if err != nil || out.Summary().Status != Skipped || out.Summary().SkipReason != "agent_busy" || out.Summary().SkippedAt == nil || work.apply != 1 || work.checked != 1 {
				t.Fatal("known settlement", err)
			}
			if store.row.version != 4 || store.row.busyAttempt != 1 || !store.row.skippedAt.Time().Equal(store.row.updatedAt.Time()) {
				t.Fatal("attempt/version/time changed")
			}
			if _, err = s.CompensateAgentBusy(context.Background(), store.row.project, store.row.id); err != nil || work.apply != 1 {
				t.Fatal("completed replay reapplied Work", err)
			}
		})
	}
}

func TestSchedulerBusyProofRejectsForeignOrRetiredCalls(t *testing.T) {
	s, store, _ := newBusyTest(t)
	r := store.row
	ctx, call, err := s.begin(context.Background(), r.project, r.id, false)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := busyRequest(r)
	call.record, call.request, call.stage, call.live = r, req, busyDiscovery, true
	locks, _ := busyLocks(r.project, r.id, r)
	plan := &busyTestPlan{locks}
	cause, _ := f.NewCommandsCause(busyCommand(r.project, r.id))
	for _, mode := range []string{"no-private", "foreign-tx", "foreign-plan", "changed-request", "missing-held", "retired"} {
		t.Run(mode, func(t *testing.T) {
			result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				if err := store.AcquireAll(ctx, tx, locks); err != nil {
					return err
				}
				call.stage, call.tx, call.plan, call.locks = busyApplying, tx, plan, locks
				request, checkCtx := req, ctx
				var checkPlan wc.TaskBusyCompensationPlan = plan
				switch mode {
				case "no-private":
					checkCtx = context.Background()
				case "foreign-tx":
					tx = f.NewTx()
				case "foreign-plan":
					checkPlan = &busyTestPlan{locks}
				case "changed-request":
					request.LaunchAttempt++
				case "missing-held":
					store.locks = nil
				case "retired":
					call.live = false
				}
				if _, err := s.authority.RequireTaskBusyCompensationInTx(checkCtx, tx, call.actor, request, checkPlan); err == nil {
					return errors.New("invalid compensation proof accepted")
				}
				return nil
			})
			if result.State() != f.Committed {
				t.Fatal("proof rejection control", result.Fault())
			}
		})
	}
	s.finish(call, false, nil)
}

func TestSchedulerBusyUnknownUsesObservationWithoutReapply(t *testing.T) {
	for _, mode := range []string{"unknown-before", "unknown-after"} {
		t.Run(mode, func(t *testing.T) {
			s, store, work := newBusyTest(t)
			store.failName, store.failMode = "compensate_agent_busy", mode
			out, err := s.CompensateAgentBusy(context.Background(), store.row.project, store.row.id)
			original, ok := UnknownAttempt(err)
			if !ok || original.AttemptID() != store.attempt || out.data != nil {
				t.Fatal("lost physical Unknown", err)
			}
			count := work.apply
			if _, duplicate := s.CompensateAgentBusy(context.Background(), store.row.project, store.row.id); duplicate != err || work.apply != count {
				t.Fatal("Unknown reapplied")
			}
			s.Stop()
			out, observed := s.Lookup(context.Background(), store.row.project, store.row.id)
			if work.apply != count {
				t.Fatal("lookup invoked Work")
			}
			if mode == "unknown-before" {
				if observed != err || out.data != nil || s.Joined() {
					t.Fatal("absence treated as rollback")
				}
			} else if observed != nil || out.Summary().Status != Skipped || !s.Joined() {
				t.Fatal("observed original outcome not retired", observed)
			}
		})
	}
}

func TestSchedulerBusyStopWaitsOriginalWorkReturn(t *testing.T) {
	s, store, work := newBusyTest(t)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	work.onDiscover = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	}
	done := make(chan error, 1)
	p, id := store.row.project, store.row.id
	go func() { _, err := s.CompensateAgentBusy(context.Background(), p, id); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Work discovery not reached")
	}
	s.Stop()
	<-cancelled
	if s.Joined() {
		t.Fatal("cancel substituted for original Work return")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Drain(ctx) == nil {
		t.Fatal("Drain did not wait for Work")
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) || !s.Joined() || work.apply != 0 || store.row.status != Pending {
		t.Fatal("actual return/zero write", err)
	}
}
