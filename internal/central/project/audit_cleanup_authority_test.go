package project

import (
	"context"
	"errors"
	"testing"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func auditCleanupRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	r := outboxCleanupRecord(t)
	r.cleanupStage = "audit"
	skillsCleanupSet(r, c.OutboxParticipant, "stopped", "completed")
	return r
}

// These are dependency-predicate controls, not real claims or completed
// domains. The PG test supplies the original driver and Audit service.
func TestAuditCleanupProgressBoundaries(t *testing.T) {
	for _, state := range []string{"required", "pending"} {
		t.Run(state, func(t *testing.T) {
			r := auditCleanupRecord(t)
			skillsCleanupSet(r, c.AuditParticipant, "stopped", state)
			if err := auditCleanupProgress(r); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*lifecycleRecord)
		code f.Code
	}{
		{"archive", func(r *lifecycleRecord) { r.operation.Action = c.Archive }, f.InvalidState},
		{"stopping", func(r *lifecycleRecord) { r.operation.State = c.OperationStopping }, f.InvalidState},
		{"failed-cleanup", func(r *lifecycleRecord) { r.operation.State, r.resume = c.OperationFailed, c.CleanupPhase }, f.InvalidState},
		{"outbox", func(r *lifecycleRecord) { r.cleanupStage = "outbox" }, f.InvalidState},
		{"final", func(r *lifecycleRecord) { r.cleanupStage = "final" }, f.InvalidState},
		{"not-stopped", func(r *lifecycleRecord) { skillsCleanupSet(r, c.AuditParticipant, "pending", "required") }, f.DependencyUnavailable},
		{"outbox-pending", func(r *lifecycleRecord) { skillsCleanupSet(r, c.OutboxParticipant, "stopped", "pending") }, f.InvalidState},
		{"object-pending", func(r *lifecycleRecord) { skillsCleanupSet(r, c.ArtifactObjectParticipant, "stopped", "pending") }, f.InvalidState},
		{"audit-completed", func(r *lifecycleRecord) { skillsCleanupSet(r, c.AuditParticipant, "stopped", "completed") }, f.InvalidState},
		{"audit-failed", func(r *lifecycleRecord) { skillsCleanupSet(r, c.AuditParticipant, "stopped", "failed") }, f.InvalidState},
		{"missing", func(r *lifecycleRecord) { r.participants = r.participants[1:] }, f.DependencyUnavailable},
		{"foreign", func(r *lifecycleRecord) { r.participants[0].name = "foreign" }, f.DependencyUnavailable},
		{"duplicate", func(r *lifecycleRecord) { r.participants = append(r.participants, r.participants[0]) }, f.DependencyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := auditCleanupRecord(t)
			tc.edit(r)
			hasCode(t, auditCleanupProgress(r), tc.code)
		})
	}
	hasCode(t, auditCleanupProgress(nil), f.DependencyUnavailable)
}

func TestAuditCleanupAuthorityUsesOriginalTransaction(t *testing.T) {
	actor, target, current := lifecycleTestActor(t)
	current.Action = c.Delete
	op, err := f.ParseID[audit.LifecycleOperation](current.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	cause, err := audit.NewLifecycleCause(op, audit.Delete, current.ProjectVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"foreign-tx", "missing-exclusive", "original-sql-error"} {
		t.Run(mode, func(t *testing.T) {
			tx := f.NewTx()
			ctx := context.WithValue(context.Background(), struct{ auditCleanup bool }{}, true)
			queries, checks := 0, 0
			sentinel := errors.New("original controlled database error")
			order := []string{}
			x := &authorityStore{order: &order, row: func(_ string, args ...any) postgres.Row {
				queries++
				if checks != 2 || len(args) != 1 || args[0] != target.String() {
					t.Fatal("SQL ran before exact held lock checks or lost target")
				}
				return rowFunc(func(...any) error { return sentinel })
			}}
			store := &barrierControlStore{
				inTx: func(got f.Tx) (postgres.SQLExecutor, error) {
					if mode == "foreign-tx" || got != tx {
						return nil, errors.New("foreign transaction")
					}
					return x, nil
				},
				held: func(got context.Context, actual f.Tx, locks []f.LockRequest) error {
					checks++
					want := f.Shared
					if checks == 2 {
						want = f.Exclusive
					}
					if got != ctx || actual != tx || len(locks) != 1 || locks[0].Mode != want || locks[0].Key.Canonical() != projectLock(target, want).Key.Canonical() {
						t.Fatal("cleanup replaced original context/Tx/held Project lock")
					}
					if mode == "missing-exclusive" && want == f.Exclusive {
						return errors.New("only Shared held")
					}
					return nil
				},
			}
			facts, err := NewLifecycleAuthority(store, registryTestManifest(t, registryTestEntries()), nil)
			if err != nil {
				t.Fatal(err)
			}
			sessions := sessionFunc(func(context.Context, f.Tx, identity.Actor) error {
				t.Fatal("cleanup borrowed Human authority")
				return nil
			})
			a, err := NewAuthority(store, AuthorityDependencies{Sessions: sessions, Lifecycle: facts})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = NewAuthority(&barrierControlStore{}, AuthorityDependencies{Sessions: sessions, Lifecycle: facts}); err == nil {
				t.Fatal("foreign Store bound the original lifecycle authority")
			}
			hasCode(t, a.CheckCleanupInTx(ctx, tx, testActor(t), cause, target), f.Forbidden)
			hasCode(t, a.CheckCleanupInTx(ctx, tx, actor, cause, testID[identity.Project](t)), f.Forbidden)
			archive, _ := audit.NewLifecycleCause(op, audit.Archive, current.ProjectVersion)
			hasCode(t, a.CheckCleanupInTx(ctx, tx, actor, archive, target), f.Forbidden)
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if err = a.CheckCleanupInTx(cancelled, tx, actor, cause, target); !errors.Is(err, context.Canceled) || checks != 0 || queries != 0 {
				t.Fatal("invalid/cancelled issuer reached Store", err)
			}
			err = a.CheckCleanupInTx(ctx, tx, actor, cause, target)
			hasCode(t, err, f.DependencyUnavailable)
			if mode == "original-sql-error" {
				if queries != 1 || !errors.Is(err, sentinel) {
					t.Fatal("original SQL failure lost", err)
				}
			} else if queries != 0 {
				t.Fatal("rejected original Tx reached canonical SQL")
			}
		})
	}
}
