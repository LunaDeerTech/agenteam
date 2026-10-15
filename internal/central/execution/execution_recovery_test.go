package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// The Store controls commit outcomes and physical preparation ownership. The
// real recovery, lifecycle codec and producer authority run here; real PG and
// ProcessGuard death remain separate integration checks.
type executionRecoveryStore struct {
	*preparationStoreControl
	history bool
}

func (s *executionRecoveryStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.preparationStoreControl.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *executionRecoveryStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "FROM agenteam_execution.snapshots s JOIN") {
		return launchScan{}
	}
	if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_execution.") {
		return launchScan{values: []any{s.history}}
	}
	return s.preparationStoreControl.QueryRow(ctx, query, args...)
}

func (s *executionRecoveryStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(query, "UPDATE agenteam_execution.executions SET status='cancelled'") {
		return s.preparationStoreControl.Exec(ctx, query, args...)
	}
	if !s.live || ctx.Err() != nil {
		s.t.Fatal("cancellation escaped original transaction")
	}
	row := s.rows[args[0].(string)]
	if row == nil || row[4] != args[1] || row[5].(*time.Time) == nil || row[3] != "created" && row[3] != "preparing" {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	at := args[2].(time.Time)
	row[3], row[4], row[9] = "cancelled", row[4].(int64)+1, &at
	s.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type executionRecoveryEvents struct {
	authority *DirectTextEventAuthority
	issuer    oc.PlanIssuer
	appends   int
	fail      bool
}

func (p *executionRecoveryEvents) PrepareAppend(ctx context.Context, actor i.Actor, e event.Event) (oc.AppendPlan, error) {
	d, err := p.authority.DiscoverAppend(ctx, actor, e.Summary())
	if err != nil {
		return oc.AppendPlan{}, err
	}
	semantic, err := oc.SemanticDigest(actor, e)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	return oc.NewAppendPlan(p.issuer, oc.AppendPlanDetails{Event: e, Semantic: semantic, Producer: d, Project: d, Locks: d.Locks()})
}

func (p *executionRecoveryEvents) AppendEventInTx(ctx context.Context, tx f.Tx, actor i.Actor, e event.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	if err := p.authority.ValidateAppendInTx(ctx, tx, actor, e.Summary(), plan.Details().Producer, oc.NewFact); err != nil {
		return oc.AppendReceipt{}, err
	}
	if p.fail {
		return oc.AppendReceipt{}, fault(f.DependencyUnavailable)
	}
	p.appends++
	return oc.AppendReceipt{EventID: e.Header().EventID, Sequence: 1}, nil
}

func executionRecoveryFixture(t *testing.T) (*associatedExecutorState, *associatedCall, context.Context, *executionRecoveryStore, *executionRecoveryEvents, *preparationProcesses) {
	t.Helper()
	return executionRecoveryForPreparation(t, newPreparationControl(t))
}

func executionRecoveryForPreparation(t *testing.T, p *preparationControl) (*associatedExecutorState, *associatedCall, context.Context, *executionRecoveryStore, *executionRecoveryEvents, *preparationProcesses) {
	t.Helper()
	store := &executionRecoveryStore{preparationStoreControl: p.store}
	p.driver.state.store = store
	p.driver.state.authority.state.store = store
	catalog := event.NewCatalog()
	lifecycle, err := c.RegisterDirectTextEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err = catalog.Seal(); err != nil {
		t.Fatal(err)
	}
	producer, err := NewDirectTextEventAuthority(p.driver.state.authority)
	if err != nil {
		t.Fatal(err)
	}
	events := &executionRecoveryEvents{authority: producer, issuer: oc.NewPlanIssuer()}
	direct := &DirectTextDriver{state: &directTextState{store: store, authority: p.driver.state.authority, processes: p.processes, process: p.processes.process, calls: map[i.ExecutionID]*directTextCall{}, deps: DirectTextDependencies{Lifecycle: lifecycle, Events: events}}}
	s := &associatedExecutorState{preparation: p.driver, direct: direct, jobs: map[i.ExecutionID]*associatedCall{}}
	row, err := loadExecution(context.Background(), store, p.execution)
	if err != nil {
		t.Fatal(err)
	}
	job := &associatedCall{project: row.summary.ProjectID, dispatch: associatedIdentity{AssociatedDispatch: c.AssociatedDispatch{ExecutionID: row.summary.ID, AgentID: row.summary.AgentID, DispatchID: row.launch.Lineage.DispatchID, Key: row.launch.Meta.IdempotencyKey, Digest: row.digest}}, active: true}
	ctx := context.WithValue(context.Background(), associatedContextKey{}, job)
	job.ctx = ctx
	s.jobs[row.summary.ID] = job
	at := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	store.rows[p.execution.String()][5] = &at
	store.rows[p.execution.String()][4] = int64(row.summary.Version) + 1
	return s, job, ctx, store, events, p.processes
}

func TestExecutionRecoveryCreatedCancellationCommitsWithEvent(t *testing.T) {
	s, job, ctx, store, events, _ := executionRecoveryFixture(t)
	before := store.writes
	summary, handled, err := s.recoverCancelledPreparation(ctx, job)
	if err != nil || !handled || summary.Status != c.Cancelled || summary.Version != 3 || summary.CompletedAt == nil || summary.StartedAt != nil || summary.SnapshotID != nil || events.appends != 1 || store.writes != before+1 || job.recovery != nil {
		t.Fatal("uncaptured cancellation did not commit exactly once", summary.Status, handled, err)
	}
	_, handled, err = s.recoverCancelledPreparation(ctx, job)
	if err != nil || handled || events.appends != 1 || store.writes != before+1 {
		t.Fatal("terminal observation replayed cancellation", handled, err)
	}
}

func TestExecutionRecoveryCancellationRetainsUnknownWithoutRepeatingWriter(t *testing.T) {
	for _, observed := range []bool{false, true} {
		t.Run(map[bool]string{false: "not-observed", true: "committed"}[observed], func(t *testing.T) {
			s, job, ctx, store, events, _ := executionRecoveryFixture(t)
			original := append([]any(nil), store.rows[job.dispatch.ExecutionID.String()]...)
			store.unknown = true
			_, handled, err := s.recoverCancelledPreparation(ctx, job)
			if err == nil || !handled || job.recovery == nil || job.recovery.unknown == nil {
				t.Fatal("unknown terminal candidate was forgotten")
			}
			if !observed {
				store.rows[job.dispatch.ExecutionID.String()] = original
			}
			writes, appends := store.writes, events.appends
			summary, handled, err := s.recoverCancelledPreparation(ctx, job)
			if !handled || store.writes != writes || events.appends != appends {
				t.Fatal("unknown observation replayed writer")
			}
			if observed && (err != nil || summary.Status != c.Cancelled || job.recovery != nil) {
				t.Fatal("exact committed terminal did not settle", err)
			}
			if !observed && (err == nil || job.recovery == nil || job.recovery.unknown == nil) {
				t.Fatal("absence was treated as rollback")
			}
		})
	}
}

func TestExecutionRecoveryRejectsUnreturnedPreparationAndHistory(t *testing.T) {
	for _, mode := range []string{"local-active", "foreign-alive", "foreign-dead", "history", "append-failure", "foreign-context"} {
		t.Run(mode, func(t *testing.T) {
			s, job, ctx, store, events, process := executionRecoveryFixture(t)
			if strings.HasPrefix(mode, "foreign-") && mode != "foreign-context" || mode == "local-active" {
				store.rows[job.dispatch.ExecutionID.String()][3] = "preparing"
				owner := process.process
				if mode != "local-active" {
					owner = newTestID[oc.Process](t)
				}
				claim := preparationClaim{execution: job.dispatch.ExecutionID, project: job.project, agent: job.dispatch.AgentID, attempt: newTestID[preparationAttempt](t), process: owner, fence: 1, phase: "running"}
				store.claims[claim.execution.String()], store.attempts[claim.attempt.String()] = claim, claim
				process.stopped = mode == "foreign-dead"
			}
			store.history = mode == "history"
			events.fail = mode == "append-failure"
			if mode == "foreign-context" {
				ctx = context.Background()
			}
			before := store.writes
			summary, handled, err := s.recoverCancelledPreparation(ctx, job)
			if mode == "foreign-dead" {
				if err != nil || !handled || summary.Status != c.Cancelled || process.confirms != 1 {
					t.Fatal("proved-dead uncaptured preparation did not cancel", err)
				}
				for _, claim := range store.attempts {
					if claim.phase != "terminal" {
						t.Fatal("original dead preparation attempt was not retired")
					}
				}
				return
			}
			if err == nil || !handled || store.writes != before || events.appends != 0 || store.rows[job.dispatch.ExecutionID.String()][3] == "cancelled" {
				t.Fatal("unproven cancellation escaped its barrier", err)
			}
		})
	}
}
