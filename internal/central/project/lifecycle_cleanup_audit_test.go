package project

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func cleanupAuditTestReport(t *testing.T, round lifecycleCleanupRound, state audit.CleanupState) c.CleanupReport {
	t.Helper()
	operation, err := f.ParseID[audit.LifecycleOperation](round.cause.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	last := testID[audit.Record](t)
	report, err := auditCleanupReport(round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, audit.CleanupReport{State: state, Checkpoint: audit.CleanupCheckpoint{ProjectID: round.claim.project, OperationID: operation, LastID: &last}})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestLifecycleCleanupAuditConstructionAndStages(t *testing.T) {
	store := &stopRoundGateStore{}
	legacy, _ := cleanupDriverFixture(t, store, nil)
	port := &cleanupDriverPort{name: c.AuditParticipant}
	driver, err := NewLifecycleCleanupDriverWithAudit(store, legacy.state.authority, legacy.state.processes, legacy.state.participant, port)
	if err != nil || driver.state.audit != port || driver.state.participant != legacy.state.participant {
		t.Fatal("fixed ports lost", err)
	}
	for _, bad := range []c.ProjectLifecycleParticipant{nil, (*cleanupDriverPort)(nil), &cleanupDriverPort{name: c.OutboxParticipant}} {
		if _, err := NewLifecycleCleanupDriverWithAudit(store, legacy.state.authority, legacy.state.processes, legacy.state.participant, bad); err == nil {
			t.Fatal("invalid Audit port bound")
		}
	}
	if _, err := NewLifecycleCleanupDriverWithAudit(store, legacy.state.authority, legacy.state.processes, port, legacy.state.participant); err == nil {
		t.Fatal("swapped ports bound")
	}
	// These are controlled complete progress records, not real COMMIT facts.
	// The PG test executes both original participants through Run sequentially.
	for _, tc := range []struct {
		name           string
		legacy         bool
		stage          string
		auditState     string
		want           c.ParticipantName
		replay, reject bool
	}{
		{"outbox", false, "outbox", "required", c.OutboxParticipant, false, false},
		{"audit-required", false, "audit", "required", c.AuditParticipant, false, false},
		{"audit-pending", false, "audit", "pending", c.AuditParticipant, false, false},
		{"final-read-only", false, "final", "completed", c.AuditParticipant, true, false},
		{"premature-final", false, "final", "pending", "", false, true},
		{"legacy-audit-confirmation", true, "audit", "required", c.OutboxParticipant, true, false},
		{"legacy-never-finalizes", true, "final", "completed", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := outboxCleanupRecord(t)
			r.cleanupStage = tc.stage
			if tc.stage != "outbox" {
				skillsCleanupSet(r, c.OutboxParticipant, "stopped", "completed")
			}
			skillsCleanupSet(r, c.AuditParticipant, "stopped", tc.auditState)
			st := driver.state
			if tc.legacy {
				st = legacy.state
			}
			name, replay, err := st.cleanupProgress(r)
			if tc.reject {
				if err == nil {
					t.Fatal("invalid stage accepted")
				}
				return
			}
			if err != nil || name != tc.want || replay != tc.replay {
				t.Fatal("incorrect single-stage selection", name, replay, err)
			}
		})
	}
	if store.transactions != 0 {
		t.Fatal("construction or selection performed work")
	}
}

func TestLifecycleCleanupAuditOriginalCheckpointAndReturn(t *testing.T) {
	for _, mode := range []string{"pending", "cancelled", "completed-with-error"} {
		t.Run(mode, func(t *testing.T) {
			store := &retirementPrepStore{tx: f.NewTx(), live: true, held: map[string]f.LockMode{}}
			legacy, round := cleanupDriverFixture(t, store, nil)
			round.participant = c.AuditParticipant
			prior := cleanupAuditTestReport(t, round, audit.CleanupPending)
			completed := cleanupAuditTestReport(t, round, audit.CleanupCompleted)
			round.checkpoint = prior.Details().Checkpoint
			store.claim = &round.claim
			store.required = []f.LockRequest{projectLock(round.claim.project, f.Shared)}
			store.held[store.required[0].Key.Canonical()] = f.Exclusive
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			var saved context.Context
			failure := errors.New("controlled audit cleanup failed after original batch")
			port := &cleanupDriverPort{name: c.AuditParticipant}
			driver, err := NewLifecycleCleanupDriverWithAudit(store, legacy.state.authority, legacy.state.processes, legacy.state.participant, port)
			if err != nil {
				t.Fatal(err)
			}
			port.call = func(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, cp *c.CleanupCheckpoint) (c.CleanupReport, error) {
				saved = ctx
				if cp == nil || !bytes.Equal(cp.Bytes(), round.checkpoint.Bytes()) || cause != round.cause || scope.ProjectID != round.claim.project || actor.Details().CauseRef != cause.OperationID.String() {
					t.Error("original checkpoint/cause lost")
				}
				if err := driver.state.authority.requireOutboxCleanupClaimInTx(ctx, store.tx, cause, scope.ProjectID); err == nil {
					t.Error("Audit callback borrowed Outbox permission")
				}
				if err := driver.state.authority.requireAuditCleanupClaimInTx(ctx, store.tx, cause, scope.ProjectID); err != nil {
					t.Error("controlled exact Audit claim rejected", err)
				}
				close(entered)
				<-release
				if mode == "completed-with-error" {
					return completed, failure
				}
				return prior, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			type outcome struct {
				report c.CleanupReport
				err    error
			}
			returned := make(chan outcome, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					select {
					case <-returned:
					case <-time.After(5 * time.Second):
						t.Error("original Audit call not returned")
					}
				}
			})
			go func() { report, err := driver.state.invoke(ctx, round); returned <- outcome{report, err} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original Audit call not entered")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-returned:
				joined = true
				t.Fatal("original return fabricated")
			default:
			}
			free()
			var got outcome
			select {
			case got = <-returned:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("original Audit did not return")
			}
			if mode == "pending" {
				if got.err != nil || got.report.Details().State != c.CleanupPending || !bytes.Equal(got.report.Details().Checkpoint.Bytes(), round.checkpoint.Bytes()) {
					t.Fatal("pending checkpoint changed", got.err)
				}
			} else {
				want := failure
				if mode == "cancelled" {
					want = context.Canceled
				}
				if !errors.Is(got.err, want) || got.report.Details().State != "" {
					t.Fatal("failed/cancelled callback published progress", got.err)
				}
			}
			hasCode(t, driver.state.authority.requireAuditCleanupClaimInTx(context.WithoutCancel(saved), store.tx, round.cause, round.claim.project), f.Forbidden)
		})
	}
}

type cleanupCheckpointSQL struct {
	postgres.SQLExecutor
	row         postgres.Row
	operation   c.OperationID
	participant c.ParticipantName
}

func (x *cleanupCheckpointSQL) QueryRow(_ context.Context, _ string, args ...any) postgres.Row {
	if len(args) != 2 || args[0] != x.operation.String() || args[1] != string(x.participant) {
		return rowFunc(func(...any) error { return errors.New("wrong checkpoint row") })
	}
	return x.row
}

func TestLifecycleCleanupAuditStoredCheckpoint(t *testing.T) {
	_, round := cleanupDriverFixture(t, &stopRoundGateStore{}, nil)
	round.participant = c.AuditParticipant
	cp := cleanupAuditTestReport(t, round, audit.CleanupPending).Details().Checkpoint
	one, two := int64(1), int64(2)
	for _, tc := range []struct {
		name        string
		participant c.ParticipantName
		schema      *int64
		raw         []byte
		reject      bool
	}{
		{"initial", c.AuditParticipant, nil, nil, false},
		{"original-audit", c.AuditParticipant, &one, cp.Bytes(), false},
		{"schema-without-bytes", c.AuditParticipant, &one, nil, true},
		{"bytes-without-schema", c.AuditParticipant, nil, cp.Bytes(), true},
		{"wrong-schema", c.AuditParticipant, &two, cp.Bytes(), true},
		{"outbox-rejects-audit-checkpoint", c.OutboxParticipant, &one, cp.Bytes(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := round
			r.participant = tc.participant
			x := &cleanupCheckpointSQL{operation: r.claim.operation, participant: r.participant, row: rowFunc(func(dest ...any) error {
				*dest[0].(*int64), *dest[1].(*int64) = 7, 2
				*dest[2].(**int64), *dest[3].(*[]byte) = tc.schema, tc.raw
				return nil
			})}
			v, attempts, got, err := cleanupParticipantRow(context.Background(), x, r)
			if tc.reject {
				if err == nil {
					t.Fatal("invalid persisted checkpoint accepted")
				}
				return
			}
			if err != nil || v != 7 || attempts != 2 || (got == nil) != (tc.raw == nil) {
				t.Fatal("lost original persisted row", err)
			}
			if got != nil && (!bytes.Equal(got.Bytes(), tc.raw) || !got.Matches(c.AuditParticipant, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project})) {
				t.Fatal("persisted checkpoint rebound")
			}
		})
	}
}
