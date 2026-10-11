package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled SQL/Skill ports exercise the actual ingress and slot decoder.
// They do not stand in for PostgreSQL rollback, constraints or Skill authority.
type skillIngressStore struct {
	*launchStore
	slot, receipt         []any
	slotReads, priorReads int
	readErr, insertErr    error
	onInsert              func()
}

func (s *skillIngressStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *skillIngressStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	switch query {
	case skillAssignmentIngressSelect:
		s.priorReads++
		return launchScan{values: s.receipt, err: s.readErr}
	case agentSlotObservationSQL:
		s.slotReads++
		return launchScan{values: s.slot, err: s.readErr}
	default:
		s.t.Fatalf("unexpected ingress SQL: %s", query)
		return launchScan{}
	}
}
func (s *skillIngressStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if query != skillAssignmentIngressInsert || len(args) != 8 {
		s.t.Fatal("unexpected ingress write")
	}
	if s.insertErr != nil {
		return pgconn.CommandTag{}, s.insertErr
	}
	if s.receipt != nil {
		s.t.Fatal("duplicate ingress write")
	}
	s.writes++
	s.receipt = []any{args[1], args[2], args[3], args[4], bytes.Clone(args[5].([]byte)), args[6], args[7]}
	if s.onInsert != nil {
		s.onInsert()
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type skillIngressAuthority struct {
	store                   *skillIngressStore
	change                  sc.RuntimeAssignmentChange
	requireErr, protectErr  error
	required, protected     int
	protectedExecution      i.ExecutionID
	beforeProtectionReturns func(context.Context)
}

func (a *skillIngressAuthority) RequireAssignmentChangeInTx(ctx context.Context, tx f.Tx, change sc.RuntimeAssignmentChange) error {
	if err := a.store.RequireHeldLocks(ctx, tx, skillAssignmentIngressLocks(a.change)); err != nil {
		return err
	}
	a.required++
	if change != a.change {
		return fault(f.Forbidden)
	}
	return a.requireErr
}
func (a *skillIngressAuthority) ProtectAssignmentChangeInTx(ctx context.Context, tx f.Tx, change sc.RuntimeAssignmentChange, execution i.ExecutionID) error {
	if err := a.store.RequireHeldLocks(ctx, tx, skillAssignmentIngressLocks(a.change)); err != nil {
		return err
	}
	if change != a.change || execution.Validate() != nil {
		return fault(f.Forbidden)
	}
	a.protected++
	a.protectedExecution = execution
	if a.beforeProtectionReturns != nil {
		a.beforeProtectionReturns(ctx)
	}
	return a.protectErr
}

func newSkillIngressControl(t *testing.T) (*SkillAssignmentIngress, *skillIngressStore, *skillIngressAuthority, c.LaunchRequest) {
	t.Helper()
	request := newLaunchControl(t).request.Clone()
	at, _ := f.NewInstant(time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC))
	change := sc.RuntimeAssignmentChange{
		ID: newTestID[sc.AssignmentChange](t), ProjectID: request.ProjectID, AgentID: request.AgentID,
		AssignmentSequence: 2, AgentVersion: 2, RecordedAt: at,
		Binding: sc.SkillBinding{SkillID: newTestID[pc.Skill](t), RevisionID: newTestID[sc.Revision](t), Revision: 1,
			AssignmentID: newTestID[sc.Assignment](t), AssignmentSequence: 2, Name: "workspace guide",
			Description: "A controlled Skill revision.", PackageSHA256: c.TriggerInputDigest([]byte("package")), EntryPath: sc.EntryPath},
	}
	if err := change.Validate(); err != nil {
		t.Fatal("invalid controlled change", err)
	}
	store := &skillIngressStore{launchStore: &launchStore{t: t, tx: f.NewTx(), live: true, locks: skillAssignmentIngressLocks(change)}}
	authority := &skillIngressAuthority{store: store, change: change}
	owner, err := NewSkillAssignmentIngress(store, authority)
	if err != nil {
		t.Fatal(err)
	}
	return owner, store, authority, request
}

func requireNoSkillIngressReceipt(t *testing.T, receipt sc.AssignmentIngressReceipt, err error) {
	t.Helper()
	if err == nil || receipt.ChangeID != (sc.AssignmentChangeID{}) || receipt.Queued || receipt.ExecutionID != nil {
		t.Fatal("rejected ingress returned a receipt", err)
	}
}

func TestExecutionSkillIngressRequiresRealTransactionAndCompleteGates(t *testing.T) {
	for _, name := range []string{"foreign-tx", "closed-tx", "project", "schedule-sh", "agent-sh", "skill", "source", "protection"} {
		t.Run(name, func(t *testing.T) {
			owner, store, authority, request := newSkillIngressControl(t)
			store.slot = occupancyRow(t, request, c.Running, false)
			tx := store.tx
			switch name {
			case "foreign-tx":
				tx = f.NewTx()
			case "closed-tx":
				store.live = false
			case "project":
				store.locks = store.locks[1:]
			case "schedule-sh":
				store.locks[1].Mode = f.Shared
			case "agent-sh":
				store.locks[2].Mode = f.Shared
			case "skill":
				store.locks = store.locks[:3]
			case "source":
				authority.requireErr = fault(f.Forbidden)
			case "protection":
				authority.protectErr = fault(f.Forbidden)
			}
			receipt, err := owner.RecordInTx(context.Background(), tx, authority.change)
			requireNoSkillIngressReceipt(t, receipt, err)
			requireCode(t, err, f.Forbidden)
			if store.writes != 0 || name != "protection" && (store.priorReads != 0 || store.slotReads != 0) {
				t.Fatal("receipt or current read preceded the real gates")
			}
		})
	}
	owner, store, authority, _ := newSkillIngressControl(t)
	if _, err := NewSkillAssignmentIngress(store, (*skillIngressAuthority)(nil)); err == nil {
		t.Fatal("typed nil authority accepted")
	}
	if _, err := NewSkillAssignmentIngress((*skillIngressStore)(nil), authority); err == nil {
		t.Fatal("typed nil Store accepted")
	}
	change := authority.change
	change.AgentVersion++
	receipt, err := owner.RecordInTx(context.Background(), store.tx, change)
	requireNoSkillIngressReceipt(t, receipt, err)
	if store.priorReads != 0 {
		t.Fatal("caller DTO supplied authority")
	}
}

func TestExecutionSkillIngressPinsQueuedAndNoActiveHistory(t *testing.T) {
	for _, state := range []c.Status{c.Created, c.Preparing, c.Running, c.Waiting, ""} {
		t.Run(string(state), func(t *testing.T) {
			owner, store, authority, request := newSkillIngressControl(t)
			if state != "" {
				store.slot = occupancyRow(t, request, state, true)
			}
			receipt, err := owner.RecordInTx(context.Background(), store.tx, authority.change)
			if err != nil || receipt.Validate() != nil || receipt.Queued != (state != "") || store.writes != 1 || store.slotReads != 1 {
				t.Fatal("original slot decision failed", err)
			}
			original := receipt.Clone()
			if receipt.Queued {
				if receipt.ExecutionID.String() != store.slot[0] || authority.protected != 1 {
					t.Fatal("cancel-requested active slot lost its pinned reference")
				}
				*receipt.ExecutionID = newTestID[i.Execution](t)
			}
			// A later unrelated slot must not change either kind of receipt.
			store.slot = occupancyRow(t, request, c.Created, false)
			again, err := owner.RecordInTx(context.Background(), store.tx, authority.change)
			if err != nil || again.ChangeID != original.ChangeID || again.Queued != original.Queued ||
				original.Queued && (*again.ExecutionID != *original.ExecutionID || authority.protectedExecution != *original.ExecutionID) ||
				!original.Queued && (again.ExecutionID != nil || authority.protected != 0) || store.slotReads != 1 || store.writes != 1 {
				t.Fatal("replay redirected, recopied or changed the original decision", err)
			}
		})
	}
}

func TestExecutionSkillIngressRejectsChangedReceiptAndReadFailures(t *testing.T) {
	for _, name := range []string{"project", "agent", "body", "digest", "time", "outcome", "execution", "missing-execution", "read", "insert"} {
		t.Run(name, func(t *testing.T) {
			owner, store, authority, request := newSkillIngressControl(t)
			store.slot = occupancyRow(t, request, c.Created, false)
			if name == "read" || name == "insert" {
				private := errors.New("private-sql-error-canary")
				if name == "read" {
					store.readErr = private
				} else {
					store.insertErr = private
				}
			} else {
				if _, err := owner.RecordInTx(context.Background(), store.tx, authority.change); err != nil {
					t.Fatal(err)
				}
				switch name {
				case "project":
					store.receipt[0] = newTestID[i.Project](t).String()
				case "agent":
					store.receipt[1] = newTestID[i.Agent](t).String()
				case "body":
					store.receipt[4] = []byte(`{}`)
				case "digest":
					store.receipt[5] = string(c.TriggerInputDigest([]byte("other")))
				case "time":
					store.receipt[6] = authority.change.RecordedAt.Time().Add(time.Microsecond)
				case "outcome":
					store.receipt[3] = "applied"
				case "execution":
					invalidID := "not-an-execution"
					store.receipt[2] = &invalidID
				case "missing-execution":
					store.receipt[2] = (*string)(nil)
				}
			}
			before := store.writes
			receipt, err := owner.RecordInTx(context.Background(), store.tx, authority.change)
			requireNoSkillIngressReceipt(t, receipt, err)
			if store.writes != before || strings.Contains(fmt.Sprint(err), "private-sql-error-canary") {
				t.Fatal("failed read/write mutated the receipt or exposed SQL detail")
			}
		})
	}
	// Full legal metadata, including worst-case JSON escaping, must fit the
	// ingress limit without truncating the pinned description.
	owner, store, authority, _ := newSkillIngressControl(t)
	authority.change.Binding.Description = strings.Repeat("<", sc.MaxDescriptionBytes)
	if _, err := owner.RecordInTx(context.Background(), store.tx, authority.change); err != nil {
		t.Fatal("valid maximum description rejected", err)
	}
}

func TestExecutionSkillIngressCancellationWaitsForOriginalReturn(t *testing.T) {
	owner, store, authority, request := newSkillIngressControl(t)
	store.slot = occupancyRow(t, request, c.Running, false)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	authority.beforeProtectionReturns = func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		receipt, err := owner.RecordInTx(ctx, store.tx, authority.change)
		if receipt.ChangeID != (sc.AssignmentChangeID{}) {
			done <- errors.New("cancelled ingress returned a receipt")
			return
		}
		done <- err
	}()
	<-entered
	cancel()
	<-cancelled
	select {
	case <-done:
		t.Fatal("cancellation detached the original protection call")
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) || store.writes != 0 {
		t.Fatal("cancelled protection returned success or wrote ingress", err)
	}
	// Actual SQL return is also required; a cancelled return never reports a
	// successful receipt even if the controlled executor has applied a write.
	owner, store, authority, _ = newSkillIngressControl(t)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	store.onInsert = cancel
	receipt, err := owner.RecordInTx(ctx, store.tx, authority.change)
	requireNoSkillIngressReceipt(t, receipt, err)
	if !errors.Is(err, context.Canceled) || store.writes != 1 {
		t.Fatal("did not wait for the original SQL return")
	}
}
