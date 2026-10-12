package project

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type auditBarrierFixture struct {
	gate     *auditGateFixture
	stage    auditLifecycleStage
	entry    ac.Entry
	key      ac.AppendKey
	queries  int
	provider int
}

func newAuditBarrierFixture(t *testing.T, action ac.Action) *auditBarrierFixture {
	t.Helper()
	x := &auditBarrierFixture{gate: newAuditGateFixture(t, nil)}
	x.gate.lifecycle = c.Deleting
	x.stage = auditLifecycleStage{project: x.gate.project.String(), owner: x.gate.actor.Details().UserID, action: c.Delete, version: 1, state: c.OperationCleaning, stage: "outbox"}
	base := x.gate.store.row
	x.gate.store.row = func(q string, args ...any) postgres.Row {
		if !strings.Contains(q, "FROM agenteam_project.lifecycle_operations") {
			return base(q, args...)
		}
		x.queries++
		if len(args) != 2 || args[1] != x.gate.project.String() {
			t.Fatal("phase read lost original project binding")
		}
		return valuesRow(x.stage.project, x.stage.owner, string(x.stage.action), int64(x.stage.version), string(x.stage.state), string(x.stage.resume), x.stage.stage)
	}
	x.gate.a = withObjectFacts(t, x.gate, auditFactFunc(func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error {
		x.provider++
		return nil
	}))
	ordinal := int64(0)
	if action == ac.ObjectDelete {
		ordinal = 1
	}
	x.entry, x.key = objectAuditEntry(t, x.gate, action, ordinal)
	return x
}

func TestAuditLifecycleBarrierObjectConvergence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		state  c.OperationState
		resume c.OperationPhase
		stage  string
		deny   bool
	}{
		{"stopping", c.OperationStopping, "", "", false},
		{"domains", c.OperationCleaning, "", "domains", false},
		{"outbox", c.OperationCleaning, "", "outbox", false},
		{"audit", c.OperationCleaning, "", "audit", true},
		{"final", c.OperationCleaning, "", "final", true},
		{"failed-stop", c.OperationFailed, c.StopPhase, "", false},
		{"failed-outbox", c.OperationFailed, c.CleanupPhase, "outbox", false},
		{"failed-audit", c.OperationFailed, c.CleanupPhase, "audit", true},
		{"failed-final", c.OperationFailed, c.CleanupPhase, "final", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []ac.Action{ac.ObjectUploadFailed, ac.ObjectDelete} {
				x := newAuditBarrierFixture(t, action)
				x.stage.state, x.stage.resume, x.stage.stage = tc.state, tc.resume, tc.stage
				err := x.gate.a.CheckAppendInTx(context.Background(), x.gate.store.tx, x.entry, x.key)
				if tc.deny {
					hasCode(t, err, f.InvalidState)
				} else if err != nil {
					t.Fatal("pre-barrier exact convergence refused", err)
				}
				if x.provider != 1 || x.queries != 1 {
					t.Fatal("missing original fact check or current phase read", x.provider, x.queries)
				}
				locks := x.gate.store.locks
				if len(locks) != 1 || locks[0].Mode != f.Shared || locks[0].Key.Canonical() != projectLock(x.gate.project, f.Shared).Key.Canonical() {
					t.Fatal("barrier lost target Project lock")
				}
			}
		})
	}
}

func TestAuditLifecycleBarrierRejectsInvalidCurrentFacts(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*auditBarrierFixture)
	}{
		{"wrong-project", func(x *auditBarrierFixture) { x.stage.project = testID[identity.Project](t).String() }},
		{"wrong-owner", func(x *auditBarrierFixture) { x.stage.owner = testID[struct{}](t).String() }},
		{"wrong-version", func(x *auditBarrierFixture) { x.stage.version++ }},
		{"wrong-action", func(x *auditBarrierFixture) { x.stage.action = c.Archive }},
		{"completed-delete", func(x *auditBarrierFixture) { x.stage.state = c.OperationCompleted }},
		{"unknown-stage", func(x *auditBarrierFixture) { x.stage.stage = "other" }},
		{"stopping-stage", func(x *auditBarrierFixture) { x.stage.state = c.OperationStopping }},
		{"cleaning-resume", func(x *auditBarrierFixture) { x.stage.resume = c.CleanupPhase }},
		{"failed-no-resume", func(x *auditBarrierFixture) { x.stage.state = c.OperationFailed }},
		{"failed-stop-with-stage", func(x *auditBarrierFixture) { x.stage.state, x.stage.resume = c.OperationFailed, c.StopPhase }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newAuditBarrierFixture(t, ac.ObjectDelete)
			tc.edit(x)
			hasCode(t, x.gate.a.CheckAppendInTx(context.Background(), x.gate.store.tx, x.entry, x.key), f.DependencyUnavailable)
		})
	}
}

func TestAuditLifecycleBarrierOriginalTransactionAndErrors(t *testing.T) {
	x := newAuditBarrierFixture(t, ac.ObjectDelete)
	for _, name := range []string{"foreign", "ended", "missing-lock"} {
		t.Run(name, func(t *testing.T) {
			tx := x.gate.store.tx
			if name == "foreign" {
				tx = f.NewTx()
			} else if name == "ended" {
				x.gate.store.tx = f.NewTx()
			} else {
				x.gate.store.lockErr = errors.New("missing Project lock")
			}
			hasCode(t, x.gate.a.checkAuditLifecycleBarrierInTx(context.Background(), tx, x.gate.project), f.DependencyUnavailable)
			x.gate.store.lockErr = nil
		})
	}
	if x.queries != 0 {
		t.Fatal("invalid transaction reached phase SQL")
	}
	for _, want := range []error{pgx.ErrNoRows, errors.New("private scan failure")} {
		base := x.gate.store.row
		x.gate.store.row = func(q string, args ...any) postgres.Row {
			if strings.Contains(q, "FROM agenteam_project.lifecycle_operations") {
				return rowFunc(func(...any) error { return want })
			}
			return base(q, args...)
		}
		err := x.gate.a.CheckAppendInTx(context.Background(), x.gate.store.tx, x.entry, x.key)
		hasCode(t, err, f.DependencyUnavailable)
		if !errors.Is(err, want) {
			t.Fatal("original SQL error discarded")
		}
		x.gate.store.row = base
	}
	// A second ordinary Project gets no global denial from the first stage.
	other := newAuditBarrierFixture(t, ac.ObjectDelete)
	other.gate.lifecycle = c.Active
	other.stage.stage = "audit"
	if err := other.gate.a.CheckAppendInTx(context.Background(), other.gate.store.tx, other.entry, other.key); err != nil || other.queries != 0 {
		t.Fatal("ordinary Project used another lifecycle barrier", err)
	}
	// Existing provider faults take precedence; this read fence cannot replace
	// an Unknown or grant Audit permission after a failed original fact check.
	want := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("original producer"))
	x.gate.a = withObjectFacts(t, x.gate, auditFactFunc(func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error { return want }))
	x.gate.order = nil
	if got := x.gate.a.CheckAppendInTx(context.Background(), x.gate.store.tx, x.entry, x.key); got != want || !reflect.DeepEqual(x.gate.order, []string{"locks", "executor", "query"}) {
		t.Fatal("original error order changed", got, x.gate.order)
	}
}

func TestAuditLifecycleBarrierWaitsForOriginalScan(t *testing.T) {
	x := newAuditBarrierFixture(t, ac.ObjectDelete)
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	base := x.gate.store.row
	x.gate.store.row = func(q string, args ...any) postgres.Row {
		row := base(q, args...)
		if !strings.Contains(q, "FROM agenteam_project.lifecycle_operations") {
			return row
		}
		return rowFunc(func(dest ...any) error {
			close(entered)
			<-release
			return row.Scan(dest...)
		})
	}
	joined := false
	defer func() {
		cancel()
		select {
		case <-release:
		default:
			close(release)
		}
		if !joined {
			<-returned
		}
	}()
	go func() { returned <- x.gate.a.CheckAppendInTx(ctx, x.gate.store.tx, x.entry, x.key) }()
	select {
	case <-entered:
	case err := <-returned:
		joined = true
		t.Fatal("original call returned before Scan", err)
	case <-time.After(time.Second):
		t.Fatal("original Scan was not reached")
	}
	cancel()
	select {
	case err := <-returned:
		joined = true
		t.Fatal("cancellation substituted for Scan return", err)
	default:
	}
	close(release)
	err := <-returned
	joined = true
	hasCode(t, err, f.DependencyUnavailable)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("original cancellation lost after actual Scan return", err)
	}
}
