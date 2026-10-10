package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// Reuses the explicitly controlled physical Store/Work/Execution outcomes.
// These tests do not claim actual PG query execution, Task authorization or
// Work restoration. The visitor calls the production drivers and scanner.
type visitTestStore struct {
	*busyTestStore
	rows       []*dispatchRecord
	scans      int
	queryErr   error
	foreignRow bool
	afterScan  func()
}

func (s *visitTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.busyTestStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *visitTestStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if query != pendingVisitSQL {
		return s.busyTestStore.QueryRow(ctx, query, args...)
	}
	s.scans++
	if len(args) != 2 || args[0] != s.row.project.String() {
		return dispatchTestRow{err: errors.New("invalid controlled visit query")}
	}
	if s.afterScan != nil {
		s.afterScan()
	}
	if s.queryErr != nil {
		return dispatchTestRow{err: s.queryErr}
	}
	var selected *dispatchRecord
	for _, candidate := range s.rows {
		if candidate.id == s.row.id {
			candidate = s.staged
		}
		if candidate.status == Pending && candidate.id.String() > args[1].(string) && (selected == nil || candidate.id.String() < selected.id.String()) {
			selected = candidate
		}
	}
	if selected == nil {
		return dispatchTestRow{err: pgx.ErrNoRows}
	}
	values := recordValues(s.t, selected)
	if s.foreignRow {
		values[1] = dispatchTestID[i.Project](s.t, 999).String()
	}
	return dispatchTestRow{values: values}
}

type visitTestProject struct {
	store   *visitTestStore
	enabled bool
	sprint  *pc.SprintID
	reads   int
	onRead  func(int)
}

func (p *visitTestProject) RequireSchedulerProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.SchedulerProject, error) {
	if _, err := p.store.InTx(tx); err != nil {
		return pc.SchedulerProject{}, err
	}
	if err := p.store.RequireHeldLocks(ctx, tx, pendingLocks(project)); err != nil {
		return pc.SchedulerProject{}, err
	}
	p.reads++
	if p.onRead != nil {
		p.onRead(p.reads)
	}
	return pc.SchedulerProject{Project: pc.ProjectRef{ID: project, CurrentSprintID: p.sprint}, Config: pc.ProjectSchedulerConfig{Enabled: p.enabled}}, ctx.Err()
}
func newVisitTest(t *testing.T) (*PendingVisitor, *visitTestStore, *visitTestProject, *handoffTestExecution, *busyTestWork) {
	t.Helper()
	busy, original, work := newBusyTest(t)
	store := &visitTestStore{busyTestStore: original, rows: []*dispatchRecord{original.row}}
	a, _ := NewPendingAuthority(store)
	busy.authority, work.authority = a, a
	sprint, _ := f.ParseID[pc.Sprint](store.row.sprint)
	project := &visitTestProject{store: store, enabled: true, sprint: &sprint}
	busy.deps.Projects = project
	execution := &handoffTestExecution{store: &store.handoffTestStore, authority: a}
	handoff, err := NewLaunchHandoff(a, LaunchHandoffDependencies{Executions: execution, Observations: execution})
	if err != nil {
		t.Fatal(err)
	}
	visitor, err := NewPendingVisitor(a, handoff, busy)
	if err != nil {
		t.Fatal(err)
	}
	return visitor, store, project, execution, work
}
func visitCreated(t *testing.T, r *dispatchRecord) *ec.Summary {
	t.Helper()
	return &ec.Summary{ID: dispatchTestID[i.Execution](t, 900), ProjectID: r.project, AgentID: r.agent, Trigger: r.launch.Trigger, Purpose: r.launch.Purpose, Status: ec.Created, Version: 1, CreatedAt: r.createdAt}
}

func TestSchedulerPendingVisitBoundedCursorIncludesAllPending(t *testing.T) {
	s, store, project, execution, work := newVisitTest(t)
	project.enabled, project.sprint = false, nil
	for n := 2; n <= 4; n++ {
		r := dispatchTestRecord(t, n, Pending)
		r.sprint = dispatchTestID[pc.Sprint](t, 500+n).String()
		store.rows = append(store.rows, r)
	}
	store.rows = append(store.rows, dispatchTestRecord(t, 5, Launched))
	var after *DispatchID
	for n := 1; n <= 4; n++ {
		out, err := s.VisitNext(context.Background(), store.row.project, after)
		if err != nil || !out.Found || out.After == nil || out.After.String() != dispatchTestID[DispatchIdentity](t, n).String() || out.Action != PendingVisitDeferred || store.scans != n {
			t.Fatal("bounded paused traversal omitted or repeated an identity", err)
		}
		after = out.After
	}
	end, err := s.VisitNext(context.Background(), store.row.project, after)
	if err != nil || end.Found || end.After != nil || store.scans != 5 || execution.lookups != 0 || execution.launches != 0 || work.discover != 0 {
		t.Fatal("paused/end visit performed a downstream action", err)
	}
	// A caller-paced new pass can inspect unresolved early rows again.
	first, err := s.VisitNext(context.Background(), store.row.project, nil)
	if err != nil || first.After == nil || *first.After != store.row.id || store.scans != 6 {
		t.Fatal("explicit next pass lost original identity", err)
	}
}

func TestSchedulerPendingVisitSelectsOnlySupportedOriginalPath(t *testing.T) {
	for _, mode := range []string{"not-sent", "no-current-sprint", "old-sprint", "unknown-found", "unknown-not-observed", "confirmed-busy", "other-rejection"} {
		t.Run(mode, func(t *testing.T) {
			s, store, project, execution, work := newVisitTest(t)
			want := PendingVisitDeferred
			switch mode {
			case "not-sent", "no-current-sprint", "old-sprint":
				store.row.outcome, store.row.attempts, store.row.busyAttempt = NotSent, 0, 0
				if mode == "not-sent" {
					want = PendingVisitLaunch
				} else if mode == "no-current-sprint" {
					project.sprint = nil
				} else {
					other := dispatchTestID[pc.Sprint](t, 999)
					project.sprint = &other
				}
			case "unknown-found", "unknown-not-observed":
				store.row.outcome, store.row.busyAttempt = Unknown, 0
				project.sprint = nil // history cannot require Current Sprint
				want = PendingVisitLookup
				if mode == "unknown-found" {
					execution.created = visitCreated(t, store.row)
				}
			case "confirmed-busy":
				want = PendingVisitBusy
			case "other-rejection":
				store.row.busyAttempt = 0
			}
			out, err := s.VisitNext(context.Background(), store.row.project, nil)
			if !out.Found || out.After == nil || *out.After != store.row.id || out.Action != want {
				t.Fatal("wrong original branch", err)
			}
			if mode == "unknown-not-observed" {
				if err == nil || out.Dispatch.data != nil || store.row.status != Pending || execution.lookups != 1 || execution.launches != 0 {
					t.Fatal("absence became negative creation proof")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch want {
			case PendingVisitLaunch:
				if execution.launches != 1 || store.row.status != Launched {
					t.Fatal("initial launch was not exactly once")
				}
			case PendingVisitLookup:
				if execution.launches != 0 || execution.lookups != 1 || store.row.status != Launched {
					t.Fatal("unknown dispatched or failed to associate")
				}
			case PendingVisitBusy:
				if work.apply != 1 || work.checked != 1 || store.row.status != Skipped || execution.launches != 0 {
					t.Fatal("Busy bypassed original Work settlement")
				}
			case PendingVisitDeferred:
				if execution.launches != 0 || execution.lookups != 0 || work.apply != 0 || store.row.status != Pending {
					t.Fatal("unsupported/current mismatch triggered action")
				}
			}
		})
	}
}

func TestSchedulerPendingVisitRechecksPauseInOriginalWriteTransaction(t *testing.T) {
	for _, mode := range []string{"pause-before-mark", "sprint-before-mark", "pause-before-associate"} {
		t.Run(mode, func(t *testing.T) {
			s, store, project, execution, _ := newVisitTest(t)
			store.row.busyAttempt = 0
			if mode == "pause-before-associate" {
				store.row.outcome = Unknown
				execution.created = visitCreated(t, store.row)
				execution.onLookup = func(context.Context) error { project.enabled = false; return nil }
			} else {
				store.row.outcome, store.row.attempts = NotSent, 0
				project.onRead = func(n int) {
					if n == 2 {
						if mode == "pause-before-mark" {
							project.enabled = false
						} else {
							project.sprint = nil
						}
					}
				}
			}
			version, attempts := store.row.version, store.row.attempts
			out, err := s.VisitNext(context.Background(), store.row.project, nil)
			if err == nil || !out.Found || store.row.version != version || store.row.attempts != attempts || store.row.status != Pending || execution.launches != 0 {
				t.Fatal("scan snapshot authorized a later write", err)
			}
			if mode == "pause-before-associate" {
				project.enabled, execution.onLookup = true, nil
				out, err = s.VisitNext(context.Background(), store.row.project, nil)
				if err != nil || out.Dispatch.Summary().Status != Launched || execution.launches != 0 || execution.lookups != 2 {
					t.Fatal("resume did not use retained original lookup", err)
				}
			}
		})
	}
}

func TestSchedulerPendingVisitScanFailurePublishesNoCursor(t *testing.T) {
	for _, mode := range []string{"query", "foreign-row", "unknown-commit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, store, _, execution, work := newVisitTest(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "query":
				store.queryErr = errors.New("private query canary")
			case "foreign-row":
				store.foreignRow = true
			case "unknown-commit":
				store.failName, store.failMode = "visit_pending", "unknown-after"
			case "cancel":
				store.afterScan = cancel
			}
			out, err := s.VisitNext(ctx, store.row.project, nil)
			if err == nil || out.Found || out.After != nil || out.Dispatch.data != nil || execution.launches != 0 || execution.lookups != 0 || work.discover != 0 {
				t.Fatal("scan error published a cursor or started work")
			}
		})
	}
}

func TestSchedulerPendingVisitRestrictionNeverMintsHandoff(t *testing.T) {
	s, store, _, _, _ := newVisitTest(t)
	foreign, _ := NewPendingAuthority(&pendingTestStore{})
	if _, err := NewPendingVisitor(foreign, s.handoff, s.busy); err == nil {
		t.Fatal("foreign owner accepted")
	}
	ctx, call, err := s.begin(context.Background(), store.row.project)
	if err != nil {
		t.Fatal(err)
	}
	call.id = store.row.id
	// A real visit context alone does not issue LaunchHandoff's private grant.
	if _, err = s.authority.RequireSchedulerIntentInTx(ctx, f.NewTx(), i.Actor{}, store.row.project, store.row.agent, i.Launch); err == nil {
		t.Fatal("visit context minted original authority")
	}
	s.finish(call)
	if err = requirePendingVisitInTx(ctx, f.NewTx(), s.handoff, store.row, false); err == nil {
		t.Fatal("retired visit context survived")
	}
}

func TestSchedulerPendingVisitStopWaitsBorrowedCallAndPreservesUnknownOwner(t *testing.T) {
	s, store, _, execution, _ := newVisitTest(t)
	store.row.outcome, store.row.busyAttempt = Unknown, 0
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	execution.onLookup = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return ctx.Err()
	}
	done := make(chan error, 1)
	p := store.row.project
	go func() { _, err := s.VisitNext(context.Background(), p, nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("original lookup did not start")
	}
	s.Stop()
	<-cancelled
	if s.Joined() {
		t.Fatal("cancel replaced original lookup return")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Drain(ctx) == nil {
		t.Fatal("Drain ignored original call")
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) || !s.Joined() || execution.launches != 0 {
		t.Fatal("original call did not retire", err)
	}
	s.handoff.Stop()
	if s.handoff.Joined() {
		t.Fatal("visitor retired the handoff's retained Unknown")
	}
	execution.onLookup, execution.created = nil, visitCreated(t, store.row)
	if _, err := s.handoff.Lookup(context.Background(), p, store.row.id); err != nil || !s.handoff.Joined() {
		t.Fatal("original owner could not resolve its retained result", err)
	}
}
