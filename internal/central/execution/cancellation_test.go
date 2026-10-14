package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// This uses the real cancellation writer and Owner gate with controlled SQL.
// Real Work protection, commit uncertainty and constraints are separate PG gates.
type cancellationStore struct{ *launchStore }

func (s *cancellationStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *cancellationStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "clock_timestamp()") {
		return launchScan{values: []any{time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}}
	}
	return s.launchStore.QueryRow(ctx, q, args...)
}
func (s *cancellationStore) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(q, "UPDATE agenteam_execution.executions SET cancel_requested_at=") {
		return s.launchStore.Exec(ctx, q, args...)
	}
	if !s.live || ctx.Err() != nil {
		s.t.Fatal("cancellation escaped original transaction")
	}
	row := s.rows[args[0].(string)]
	if row == nil || row[4] != args[1] || row[5].(*time.Time) != nil {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	at := args[2].(time.Time)
	row[5] = &at
	row[4] = row[4].(int64) + 1
	s.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type cancellationTasks struct {
	c.TaskCancellationProvider
	denied bool
	calls  int
}

func (p *cancellationTasks) RequireTaskCancellationInTx(context.Context, f.Tx, i.Actor, c.CancellationScope) error {
	p.calls++
	if p.denied {
		return fault(f.Forbidden)
	}
	return nil
}

func cancellationFixture(t *testing.T) (*CancellationService, *launchControl, *cancellationStore, c.CancellationScope) {
	t.Helper()
	control := newLaunchControl(t)
	created, err := control.service.Launch(context.Background(), control.actor, control.request)
	if err != nil {
		t.Fatal(err)
	}
	store := &cancellationStore{control.store}
	state := &projectCallState{store: store, service: control.service}
	service := &CancellationService{owner: &ProjectCallOwnership{state}, tasks: &cancellationTasks{}}
	scope := c.CancellationScope{ProjectID: created.Execution.ProjectID, TaskID: created.Execution.Trigger.TaskID, AgentID: created.Execution.AgentID, ExecutionID: created.Execution.ID}
	return service, control, store, scope
}

func TestHumanCancellationIsDurableRequestAndNeverEarlyTerminal(t *testing.T) {
	s, control, store, scope := cancellationFixture(t)
	before := store.writes
	locks, err := s.CancelLocks(control.actor, scope)
	if err != nil {
		t.Fatal(err)
	}
	cause, _ := f.NewRecoveryCause("test-cancel", scope.ExecutionID.String(), "once")
	var accepted c.Summary
	result := store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		version := f.Version(1)
		accepted, err = s.RequestCancelInTx(ctx, tx, control.actor, scope, &version)
		return err
	})
	if result.State() != f.Committed || accepted.Status != c.Created || accepted.CompletedAt != nil || accepted.CancelRequestedAt == nil || accepted.Version != 2 || store.writes != before+1 {
		t.Fatal("request fabricated stopping completion", result.State(), accepted)
	}
	result = store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		current, err := s.RequestCancelInTx(ctx, tx, control.actor, scope, nil)
		if err == nil && (current.Version != 2 || *current.CancelRequestedAt != *accepted.CancelRequestedAt) {
			t.Fatal("duplicate request changed canonical request")
		}
		return err
	})
	if result.State() != f.Committed || store.writes != before+1 {
		t.Fatal("duplicate request wrote again")
	}
}

func TestCancellationRejectsStaleForeignAndUnprovenCalls(t *testing.T) {
	for _, mode := range []string{"stale", "foreign-task", "foreign-agent", "revoked", "missing-lock", "agent-without-proof", "human-without-proof", "terminal"} {
		t.Run(mode, func(t *testing.T) {
			s, control, store, scope := cancellationFixture(t)
			actor := control.actor
			expected := f.Version(1)
			wanted := f.VersionConflict
			switch mode {
			case "stale":
				expected = 2
			case "foreign-task":
				scope.TaskID = newTestID[struct{}](t).String()
				wanted = f.NotFound
			case "foreign-agent":
				scope.AgentID = newTestID[i.Agent](t)
				wanted = f.Forbidden
			case "revoked":
				control.revoked = true
				wanted = f.Forbidden
			case "missing-lock":
				wanted = f.Forbidden
			case "human-without-proof":
				s.tasks.(*cancellationTasks).denied = true
				wanted = f.Forbidden
			case "agent-without-proof":
				s.tasks.(*cancellationTasks).denied = true
				actor, _ = i.NewAgentRun(scope.ProjectID, scope.AgentID, scope.ExecutionID)
				wanted = f.Forbidden
			case "terminal":
				row := store.rows[scope.ExecutionID.String()]
				row[3] = "cancelled"
				at := row[7].(time.Time)
				row[9] = &at
			}
			locks, _ := s.CancelLocks(actor, scope)
			if mode == "missing-lock" {
				locks = locks[:len(locks)-1]
			}
			before := store.writes
			cause, _ := f.NewRecoveryCause("test-cancel-negative", scope.ExecutionID.String(), mode)
			var actual error
			result := store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := store.AcquireAll(ctx, tx, locks); err != nil {
					return err
				}
				_, actual = s.RequestCancelInTx(ctx, tx, actor, scope, &expected)
				return actual
			})
			if mode == "terminal" {
				if result.State() != f.Committed || actual != nil {
					t.Fatal("terminal repeat did not remain terminal", actual)
				}
			} else {
				requireCode(t, actual, wanted)
				if result.State() != f.NotCommitted {
					t.Fatal("refusal committed")
				}
			}
			if store.writes != before {
				t.Fatal("rejected/terminal cancellation wrote state")
			}
		})
	}
}
