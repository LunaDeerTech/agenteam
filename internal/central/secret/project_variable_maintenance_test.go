package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type projectVariableMaintenanceStore struct {
	*projectAuditUnitStore
	acquires, transactions, writes int
	unknown                        bool
	exec                           func(string, []any) (pgconn.CommandTag, error)
}

func (s *projectVariableMaintenanceStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.projectAuditUnitStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *projectVariableMaintenanceStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx {
		return errors.New("foreign controlled Tx")
	}
	s.acquires++
	s.locks = append([]f.LockRequest(nil), locks...)
	return nil
}
func (s *projectVariableMaintenanceStore) Acquire(ctx context.Context, tx f.Tx, key f.LockKey, mode f.LockMode) error {
	return s.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: mode}})
}
func (s *projectVariableMaintenanceStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	s.tx = f.NewTx()
	if err := fn(ctx, s.tx); err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			fault = f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
		return f.NotCommittedResult(fault)
	}
	if s.unknown {
		attempt, _ := f.NewID[f.TransactionAttempt]()
		return f.UnknownResult(attempt, cause)
	}
	return f.CommittedResult()
}
func (s *projectVariableMaintenanceStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.writes++
	if s.exec == nil {
		return pgconn.CommandTag{}, errors.New("unexpected controlled write")
	}
	return s.exec(query, args)
}

func TestProjectVariableRotationReverseOwnershipIsReceiptBound(t *testing.T) {
	for _, name := range []string{"found-deleted-credential", "payload-already-gone", "receipt-missing-payload-live", "invalid-credential", "wrong-project-query", "wrong-kind", "sql-error"} {
		t.Run(name, func(t *testing.T) {
			p := projectVariableEnvelope(t)
			credential := projectAuditID[sc.Credential](t).String()
			s := &projectAuditUnitStore{}
			s.read = func(query string, args []any) postgres.Row {
				if strings.Contains(query, "FROM agenteam_secret.project_variable_receipts") {
					if len(args) != 3 || args[0] != p.ownerID || args[1] != p.id.String() || args[2] != p.scope.Details().ProjectID {
						t.Fatal("reverse identity omitted")
					}
					if name == "payload-already-gone" || name == "receipt-missing-payload-live" || name == "wrong-project-query" {
						return projectAuditUnitRow{err: pgx.ErrNoRows}
					}
					if name == "sql-error" {
						return projectAuditUnitRow{err: errors.New("controlled SQL error")}
					}
					if name == "invalid-credential" {
						return projectAuditUnitRow{values: []any{"not-uuid"}}
					}
					return projectAuditUnitRow{values: []any{credential}}
				}
				if strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads") {
					return projectAuditUnitRow{values: []any{name != "payload-already-gone"}}
				}
				t.Fatal("reverse lookup consulted current canonical/legacy receipt")
				return nil
			}
			if name == "wrong-kind" {
				p.ownerKind = receiptOwner
			}
			got, exists, err := projectVariableReceiptCredential(context.Background(), s, p)
			switch name {
			case "found-deleted-credential":
				if err != nil || !exists || got != credential || s.queries != 1 {
					t.Fatal("historical owner not retained")
				}
			case "payload-already-gone":
				if err != nil || exists || got != "" {
					t.Fatal("gone payload not skipped")
				}
			default:
				if err == nil || exists || got != "" {
					t.Fatal("unsafe reverse ownership accepted")
				}
			}
		})
	}
}

func TestProjectVariableRotationApplyRechecksOwnerUnderOriginalLocks(t *testing.T) {
	for _, name := range []string{"kind3", "changed-owner", "payload-gone", "wrong-payload-kind", "kind1", "kind2"} {
		t.Run(name, func(t *testing.T) {
			old := projectVariableEnvelope(t)
			if name == "kind1" {
				old.ownerKind = valueOwner
			}
			if name == "kind2" {
				old.ownerKind = receiptOwner
			}
			// The kind1/2 cases verify unchanged dispatch/locks only. The kind3
			// case uses actual AEAD rewrap; Prepare's Rows/PG scan is not simulated.
			next := old
			if old.ownerKind == projectVariableReceiptOwner {
				nonce, _ := masterNonce(91)
				var err error
				next, err = rewrap(testKeys(t), old, 2, nonce)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				next.masterVersion, next.wrapRevision = 2, 2
			}
			credential := projectAuditID[sc.Credential](t).String()
			store := &projectVariableMaintenanceStore{projectAuditUnitStore: &projectAuditUnitStore{tx: f.NewTx()}}
			reverse, updates := 0, 0
			store.read = func(query string, args []any) postgres.Row {
				switch {
				case strings.Contains(query, "FROM agenteam_secret.secret_control"):
					return projectAuditUnitRow{values: []any{int64(2), int64(1)}}
				case strings.Contains(query, "FROM agenteam_secret.secret_rotation_runs"):
					return projectAuditUnitRow{values: []any{"running"}}
				case strings.Contains(query, "FROM agenteam_secret.project_variable_receipts"):
					reverse++
					if store.acquires != 1 {
						t.Fatal("reverse validation before full lock acquisition")
					}
					if name == "payload-gone" {
						return projectAuditUnitRow{err: pgx.ErrNoRows}
					}
					owner := credential
					if name == "changed-owner" {
						owner = projectAuditID[sc.Credential](t).String()
					}
					return projectAuditUnitRow{values: []any{owner}}
				case strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads"):
					return projectAuditUnitRow{values: []any{false}}
				case strings.HasPrefix(query, "SELECT scope,"):
					kind := int16(3)
					if name == "wrong-payload-kind" {
						kind = 2
					}
					return projectAuditUnitRow{values: []any{"project", old.scope.Details().ProjectID, kind, old.ownerID}}
				}
				return projectAuditUnitRow{err: errors.New("unexpected controlled rotation SQL")}
			}
			store.exec = func(query string, args []any) (pgconn.CommandTag, error) {
				if strings.HasPrefix(query, "UPDATE agenteam_secret.secret_payloads") {
					updates++
					if args[0] != old.id.String() || args[5] != int64(old.masterVersion) || args[6] != int64(old.wrapRevision) {
						t.Fatal("rewrap CAS changed")
					}
				} else if !strings.HasPrefix(query, "UPDATE agenteam_secret.secret_rotation_runs") {
					t.Fatal("unexpected rotation mutation")
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}
			state := &serviceState{store: store}
			service := &Service{data: func() *serviceState { return state }}
			batch := rewrapBatch{run: projectAuditID[struct{}](t).String(), target: 2, epoch: 1, items: []rewrapCandidate{{previous: old, next: next, credential: credential}}}
			got, err := service.ApplyPreparedRewrapInTx(context.Background(), store.tx, PreparedRewrap{data: func() rewrapBatch { return batch }})
			if name == "changed-owner" || name == "wrong-payload-kind" {
				if err == nil || got.Applied != 0 || updates != 0 {
					t.Fatal("changed ownership rewrapped")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if name == "payload-gone" {
					want = 0
				}
				if updates != want || got.Applied != f.Progress(want) {
					t.Fatal("rewrap counts")
				}
			}
			if store.acquires != 1 || store.transactions != 0 {
				t.Fatal("extra lock acquisition/transaction")
			}
			projectLock, _ := f.ProjectLock(old.scope.Details().ProjectID)
			credentialLock, _ := f.AggregateLock(f.CredentialRefAggregate, credential)
			if err := store.RequireHeldLocks(context.Background(), store.tx, []f.LockRequest{{Key: writeLock(), Mode: f.Shared}, {Key: projectLock, Mode: f.Shared}, {Key: credentialLock, Mode: f.Exclusive}}); err != nil {
				t.Fatal("deleted credential lock lost")
			}
			if (name == "kind1" || name == "kind2") && reverse != 0 {
				t.Fatal("legacy reverse path changed")
			}
		})
	}
}

type projectVariableCleanupAuthority struct {
	sc.ProjectAuthority
	store  *projectVariableMaintenanceStore
	checks int
	deny   bool
}

func (a *projectVariableCleanupAuthority) CheckCleanupInTx(_ context.Context, tx f.Tx, _ i.Actor, _ sc.LifecycleCause, _ i.ProjectID) error {
	a.checks++
	if a.store.acquires != 1 || tx != a.store.tx {
		return errors.New("lifecycle check before owned Project lock")
	}
	if a.deny {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

func TestProjectVariableCleanupBoundedHeadCurrentGateAndUnknown(t *testing.T) {
	for _, name := range []string{"101", "100", "denied", "retained", "malformed", "unknown", "sql-error"} {
		t.Run(name, func(t *testing.T) {
			project := projectAuditID[i.Project](t)
			scope, _ := i.InProject(project)
			operation := projectAuditID[sc.LifecycleOperation](t)
			cause, _ := sc.NewLifecycleCause(operation, 7, true)
			registration, _ := i.RegisterService(i.ProjectLifecycle)
			actor, _ := registration.Actor(operation.String(), scope)
			store := &projectVariableMaintenanceStore{projectAuditUnitStore: &projectAuditUnitStore{}, unknown: name == "unknown"}
			a := &projectVariableCleanupAuthority{store: store, deny: name == "denied"}
			remaining := 100
			if name == "101" {
				remaining = 101
			}
			newDeletes := 0
			store.read = func(query string, args []any) postgres.Row {
				if len(args) != 1 || args[0] != project.String() {
					t.Fatal("cleanup Project binding")
				}
				if a.checks != 1 {
					t.Fatal("SQL before current lifecycle gate")
				}
				switch {
				case strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_references"):
					return projectAuditUnitRow{values: []any{name == "retained"}}
				case strings.Contains(query, "LEFT JOIN agenteam_secret.secret_payloads"):
					for _, part := range []string{"ORDER BY id LIMIT 100", "p.project_id IS DISTINCT FROM r.project_id", "p.owner_kind<>3", "p.owner_id<>r.id", "p.payload_id IS NULL"} {
						if !strings.Contains(query, part) {
							t.Fatal("unbounded/unsafe cleanup selection")
						}
					}
					return projectAuditUnitRow{values: []any{name == "malformed"}}
				case strings.HasPrefix(query, "SELECT NOT EXISTS"):
					if !strings.Contains(query, "NOT EXISTS(SELECT 1 FROM agenteam_secret.project_variable_receipts WHERE project_id=$1)") {
						t.Fatal("receipt missing from completion")
					}
					return projectAuditUnitRow{values: []any{remaining == 0}}
				}
				return projectAuditUnitRow{err: errors.New("unexpected controlled cleanup query")}
			}
			store.exec = func(query string, args []any) (pgconn.CommandTag, error) {
				if strings.Contains(query, "DELETE FROM agenteam_secret.project_variable_receipts") {
					if !strings.Contains(query, "ORDER BY id LIMIT 100") || !strings.Contains(query, "p.owner_kind=3 AND p.owner_id=r.id") || !strings.Contains(query, "p.project_id=r.project_id") {
						t.Fatal("unsafe cleanup delete")
					}
					newDeletes++
					if name == "sql-error" {
						return pgconn.CommandTag{}, errors.New("controlled failure")
					}
					n := remaining
					if n > 100 {
						n = 100
					}
					remaining -= n
				}
				return pgconn.NewCommandTag("DELETE 100"), nil
			}
			state := &serviceState{store: store, auth: Authorizations{Projects: a}}
			service := &Service{data: func() *serviceState { return state }}
			got, err := service.CleanupProject(context.Background(), actor, cause, project, nil)
			if name == "denied" || name == "malformed" || name == "unknown" || name == "sql-error" {
				if err == nil || got.Completed || got.Checkpoint.ProjectID.Validate() == nil {
					t.Fatal("failed/unknown cleanup published result")
				}
				if name == "unknown" {
					var fault *f.Fault
					if !errors.As(err, &fault) || fault.CommitState != f.Unknown {
						t.Fatal("unknown lost")
					}
				}
			} else if err != nil || got.Completed != (name == "100") {
				t.Fatalf("bounded completion: %v", err)
			}
			if store.transactions != 1 || store.acquires != 1 || a.checks != 1 {
				t.Fatal("cleanup retry/extra Tx/gate")
			}
			if (name == "denied" || name == "retained" || name == "malformed") && newDeletes != 0 {
				t.Fatal("receipt deleted before gate/ownership")
			}
			if name == "denied" && store.queries != 0 || name == "retained" && store.writes != 0 {
				t.Fatal("early gate bypassed")
			}
		})
	}
}
