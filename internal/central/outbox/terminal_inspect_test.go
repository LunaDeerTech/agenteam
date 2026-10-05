package outbox

import (
	"context"
	"errors"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This executor checks the service's actual call ordering and side effects.
// It is not a database/lock test; those remain in the owned fixture suite.
type terminalInspectStore struct {
	Store
	row                                       lifecycleRow
	found, unknown                            bool
	tx                                        foundation.Tx
	locks                                     []foundation.LockRequest
	transactions, acquisitions, writes, scans int
	beforeTx                                  func(foundation.TransactionCause)
}

func (s *terminalInspectStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.transactions++
	if s.beforeTx != nil {
		s.beforeTx(cause)
	}
	s.tx = foundation.NewTx()
	s.locks = nil
	err := fn(ctx, s.tx)
	s.tx = foundation.Tx{}
	if err != nil {
		var f *foundation.Fault
		if !errors.As(err, &f) {
			f = foundation.NewFault(foundation.DependencyUnavailable, foundation.NotCommitted).WithCause(err)
		}
		return foundation.NotCommittedResult(f)
	}
	if s.unknown {
		id, _ := foundation.NewID[foundation.TransactionAttempt]()
		return foundation.UnknownResult(id, cause)
	}
	return foundation.CommittedResult()
}
func (s *terminalInspectStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if !tx.Valid() || tx != s.tx {
		return nil, errors.New("not this live transaction")
	}
	return s, nil
}
func (s *terminalInspectStore) AcquireAll(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	if tx != s.tx || s.locks != nil {
		return errors.New("not one union")
	}
	s.acquisitions++
	s.locks = append([]foundation.LockRequest{}, locks...)
	return nil
}
func (s *terminalInspectStore) RequireHeldLocks(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	if tx != s.tx || len(s.locks) == 0 {
		return errors.New("locks absent")
	}
	return nil
}
func (s *terminalInspectStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	if !s.tx.Valid() || !strings.Contains(sql, "FROM agenteam_outbox.project_lifecycle") {
		return terminalInspectRow{err: errors.New("unexpected query")}
	}
	if !s.found {
		return terminalInspectRow{err: pgx.ErrNoRows}
	}
	return terminalInspectRow{row: s.row}
}
func (s *terminalInspectStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.scans++
	return nil, errors.New("nonterminal scan reached")
}
func (s *terminalInspectStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.writes++
	return pgconn.CommandTag{}, errors.New("write not allowed by this test")
}

type terminalInspectRow struct {
	row lifecycleRow
	err error
}

func (r terminalInspectRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.row.operation
	*dest[1].(*string) = string(r.row.action)
	*dest[2].(*int64) = r.row.version
	*dest[3].(*string) = r.row.phase
	*dest[4].(*int64) = r.row.after
	return nil
}

type terminalInspectAuthority struct {
	store  *terminalInspectStore
	deny   bool
	checks int
}

func (a *terminalInspectAuthority) Discover(_ context.Context, r oc.ProjectRequest) (oc.Dependencies, error) {
	d, e := oc.LifecycleBinding(r)
	if e != nil {
		return oc.Dependencies{}, e
	}
	key, _ := foundation.ProjectLock(r.Details().ProjectID.String())
	return oc.NewDependencies(oc.NewPlanIssuer(), d, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}, nil)
}
func (a *terminalInspectAuthority) ValidateInTx(_ context.Context, tx foundation.Tx, _ oc.ProjectRequest, _ oc.Dependencies) error {
	a.checks++
	if tx != a.store.tx || len(a.store.locks) == 0 {
		return errors.New("authorization not in locked transaction")
	}
	if a.deny {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return nil
}

type terminalInspectProcesses struct{}

func (terminalInspectProcesses) CurrentProcess() oc.ProcessID {
	id, _ := foundation.ParseID[oc.Process]("01900000-0000-7000-8000-000000000081")
	return id
}
func (terminalInspectProcesses) ConfirmStopped(context.Context, oc.ProcessID) error { return nil }
func terminalInspectFixture(t *testing.T, action oc.LifecycleAction, phase string) (*Service, *terminalInspectStore, *terminalInspectAuthority, identity.Actor, oc.LifecycleCause, *int) {
	t.Helper()
	project, _ := foundation.NewID[identity.Project]()
	op, _ := foundation.NewID[oc.LifecycleOperation]()
	cause, e := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: project, OperationID: op, Action: action, ProjectVersion: 4})
	if e != nil {
		t.Fatal(e)
	}
	scope, _ := identity.InProject(project)
	role, _ := identity.RegisterService(identity.ProjectLifecycle)
	actor, e := role.Actor(op.String(), scope)
	if e != nil {
		t.Fatal(e)
	}
	store := &terminalInspectStore{found: true, row: lifecycleRow{operation: op.String(), action: action, version: 4, phase: phase}}
	authority := &terminalInspectAuthority{store: store}
	state := &serviceState{store: store, auth: Authorizations{Projects: authority, Processes: terminalInspectProcesses{}}}
	s := &Service{func() *serviceState { return state }}
	runtime := localRuntime()
	runtime.svc = s
	state.runtime = &Runtime{func() *runtimeState { return runtime }}
	attempt, _ := foundation.NewID[oc.Attempt]()
	ep, _ := foundation.ParseID[event.Project](project.String())
	cancelled := new(int)
	record := record{attempt: attempt, scope: event.Scope{Kind: event.ProjectScope, ProjectID: ep}, effect: oc.DomainIngress}
	runtime.active[attempt] = &execution{id: attempt, record: record, cancel: func() { *cancelled++ }, done: make(chan struct{})}
	return s, store, authority, actor, cause, cancelled
}
func TestTerminalInspectExactReceiptDoesNotCancelOrWrite(t *testing.T) {
	for _, tc := range []struct {
		action oc.LifecycleAction
		phase  string
	}{{oc.ArchiveProject, "stopped"}, {oc.DeleteProject, "stopped"}, {oc.DeleteProject, "completed"}} {
		t.Run(string(tc.action)+"_"+tc.phase, func(t *testing.T) {
			s, store, auth, actor, cause, cancelled := terminalInspectFixture(t, tc.action, tc.phase)
			report, e := s.InspectStop(context.Background(), actor, cause)
			if e != nil || !report.Stopped || report.Pending != 0 {
				t.Errorf("exact terminal receipt not returned: %+v %v", report, e)
			}
			if *cancelled != 0 || store.writes != 0 || store.scans != 0 {
				t.Errorf("terminal inspection performed cancellation/scan/write: %d/%d/%d", *cancelled, store.scans, store.writes)
			}
			if store.transactions != 1 || store.acquisitions != 1 || auth.checks != 1 {
				t.Errorf("not one authorized physical transaction: %d/%d/%d", store.transactions, store.acquisitions, auth.checks)
			}
			for _, lock := range store.locks {
				if lock.Mode != foundation.Shared {
					t.Error("terminal inspection took exclusive lock")
				}
			}
		})
	}
}
func TestTerminalInspectRejectsStaleAuthorityAndUnknownWithoutSideEffects(t *testing.T) {
	for _, kind := range []string{"authority denied", "missing", "wrong operation", "wrong version", "wrong action", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			s, store, auth, actor, cause, cancelled := terminalInspectFixture(t, oc.ArchiveProject, "stopped")
			switch kind {
			case "authority denied":
				auth.deny = true
			case "missing":
				store.found = false
			case "wrong operation":
				id, _ := foundation.NewID[oc.LifecycleOperation]()
				store.row.operation = id.String()
			case "wrong version":
				store.row.version++
			case "wrong action":
				store.row.action = oc.DeleteProject
			case "unknown":
				store.unknown = true
			}
			report, e := s.InspectStop(context.Background(), actor, cause)
			if e == nil || report.Stopped {
				t.Errorf("invalid terminal observation accepted: %+v %v", report, e)
			}
			if *cancelled != 0 || store.writes != 0 || store.scans != 0 || store.transactions != 1 {
				t.Errorf("rejected observation escaped read transaction: cancel=%d write=%d scan=%d tx=%d", *cancelled, store.writes, store.scans, store.transactions)
			}
		})
	}
}
func TestTerminalInspectNonterminalRetainsOriginalConvergence(t *testing.T) {
	s, store, _, actor, cause, cancelled := terminalInspectFixture(t, oc.DeleteProject, "stopping")
	report, e := s.InspectStop(context.Background(), actor, cause)
	if e == nil || report.Stopped || *cancelled != 1 || store.scans != 1 {
		t.Fatalf("nonterminal did not follow original cancellation/scan path: %+v %v cancel=%d scan=%d", report, e, *cancelled, store.scans)
	}
}

func TestTerminalInspectCrossTransactionTerminalWinsBeforeCancel(t *testing.T) {
	for _, stage := range []string{"outbox.stop-authorize", "outbox.stop-gate"} {
		for _, phase := range []string{"stopped", "completed"} {
			t.Run(stage+"/"+phase, func(t *testing.T) {
				s, store, _, actor, cause, cancelled := terminalInspectFixture(t, oc.DeleteProject, "stopping")
				cancelledAfterTerminal := 0
				for _, handle := range s.state().runtime.data().active {
					handle.cancel = func() {
						*cancelled++
						if store.row.phase == "stopped" || store.row.phase == "completed" {
							cancelledAfterTerminal++
						}
					}
				}
				transitions := 0
				store.beforeTx = func(cause foundation.TransactionCause) {
					if cause.Details().Owner == stage {
						store.row.phase = phase
						transitions++
					}
				}
				report, e := s.InspectStop(context.Background(), actor, cause)
				if transitions != 1 || e != nil || !report.Stopped || cancelledAfterTerminal != 0 || store.scans != 0 || store.writes != 0 {
					t.Fatalf("terminal transition lost: %+v %v transitions=%d late cancel=%d scan=%d write=%d", report, e, transitions, cancelledAfterTerminal, store.scans, store.writes)
				}
			})
		}
	}
}
func TestTerminalInspectProgressRechecksExactTerminalInsideWriter(t *testing.T) {
	for _, phase := range []string{"stopped", "completed"} {
		t.Run(phase, func(t *testing.T) {
			s, store, _, actor, cause, _ := terminalInspectFixture(t, oc.DeleteProject, "stopping")
			plan, e := s.planLifecycle(context.Background(), actor, cause, oc.LifecycleInspect)
			if e != nil {
				t.Fatal(e)
			}
			// The external read/attempt-join work observed stopping; the serialized
			// progress writer now sees another instance's exact terminal receipt.
			transitions := 0
			store.beforeTx = func(cause foundation.TransactionCause) {
				if cause.Details().Owner == "outbox.stop-progress" {
					store.row.phase = phase
					transitions++
				}
			}
			terminal, e := s.progressLifecycleStop(context.Background(), plan, nil, nil, plan.locks(foundation.Exclusive))
			if transitions != 1 || e != nil || !terminal || store.writes != 0 {
				t.Fatalf("terminal checkpoint wrote after recheck: terminal=%v err=%v transitions=%d writes=%d", terminal, e, transitions, store.writes)
			}
		})
	}
}
