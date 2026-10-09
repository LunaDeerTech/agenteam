// Independent offline probe of the actual public authority and strict loaders.
// The Store/rows are disclosed controlled facts, never a PG authorization claim.
package project

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type independentCleanupRows struct {
	pgx.Rows
	data    [][]any
	at      int
	closed  bool
	endErr  error
	onClose func()
}

func (r *independentCleanupRows) Next() bool { r.at++; return r.at < len(r.data) }
func (r *independentCleanupRows) Scan(dest ...any) error {
	return valuesRow(r.data[r.at]...).Scan(dest...)
}
func (r *independentCleanupRows) Err() error { return r.endErr }
func (r *independentCleanupRows) Close() {
	if !r.closed {
		r.closed = true
		r.onClose()
	}
}

type independentCleanupStore struct {
	Store
	t                                                     *testing.T
	ctx                                                   context.Context
	tx                                                    f.Tx
	project                                               c.ProjectID
	operation                                             c.OperationID
	actor                                                 i.Actor
	cause                                                 c.LifecycleCause
	manifest                                              c.RequiredManifest
	projectRow, operationRow                              []any
	participants                                          [][]any
	held, rowsOpened, rowsClosed, operationReads, queries int
	lockErr, secondErr, rowsErr                           error
}

func (s *independentCleanupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign controlled Tx")
	}
	return s, nil
}
func (s *independentCleanupStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if ctx != s.ctx || tx != s.tx || len(locks) != 1 || locks[0].Mode != f.Shared || f.CompareLockKeys(locks[0].Key, projectLock(s.project, f.Shared).Key) != 0 {
		s.t.Fatal("authority changed original context/Tx/Project SH")
	}
	s.held++
	if s.lockErr != nil {
		return s.lockErr
	}
	return ctx.Err()
}
func (s *independentCleanupStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if ctx != s.ctx || s.held != 1 {
		s.t.Fatal("query before original held-lock gate")
	}
	s.queries++
	if strings.Contains(query, "FROM agenteam_project.projects WHERE") {
		if len(args) != 1 || args[0] != s.project.String() {
			s.t.Fatal("Project scope drift")
		}
		return valuesRow(s.projectRow...)
	}
	if strings.Contains(query, "FROM agenteam_project.deletion_receipts WHERE") {
		return rowFunc(func(...any) error { return pgx.ErrNoRows })
	}
	if strings.Contains(query, "FROM agenteam_project.lifecycle_operations WHERE") {
		if len(args) != 2 || args[0] != s.project.String() || args[1] != s.operation.String() {
			s.t.Fatal("operation scope drift")
		}
		s.operationReads++
		if s.operationReads == 2 {
			if s.rowsClosed != 1 {
				s.t.Fatal("first actual Rows not closed before second read")
			}
			if s.secondErr != nil {
				return rowFunc(func(...any) error { return s.secondErr })
			}
		}
		return valuesRow(s.operationRow...)
	}
	s.t.Fatal("unexpected authority query")
	return nil
}
func (s *independentCleanupStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	if ctx != s.ctx || !strings.Contains(query, "FROM agenteam_project.lifecycle_participants WHERE operation_id=$1") || len(args) != 1 || args[0] != s.operation.String() {
		s.t.Fatal("participant scope/context drift")
	}
	s.rowsOpened++
	return postgres.ReviewB02Rows(&independentCleanupRows{data: s.participants, at: -1, endErr: s.rowsErr, onClose: func() { s.rowsClosed++ }}), nil
}

func independentCleanupFixture(t *testing.T) (*LifecycleAuthority, *independentCleanupStore) {
	t.Helper()
	project, operation, owner, creation := testID[i.Project](t), testID[c.Operation](t), testID[i.User](t), testID[c.Creation](t)
	entries := registryTestEntries()
	for k := range entries {
		if entries[k].Name == c.OutboxParticipant {
			entries[k].CleanupAfter = append(entries[k].CleanupAfter, c.SkillsParticipant)
		}
	}
	entries = append(entries, c.ParticipantRegistration{Name: c.SkillsParticipant, ContractVersion: 1, OwnerModule: "skills", CleanupAfter: []c.ParticipantName{c.SecretParticipant}})
	manifest := registryTestManifest(t, entries)
	raw, err := json.Marshal(manifest.Entries())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	op := operation.String()
	s := &independentCleanupStore{t: t, ctx: context.WithValue(context.Background(), struct{}{}, "original"), tx: f.NewTx(), project: project, operation: operation, manifest: manifest, cause: c.LifecycleCause{OperationID: operation, Action: c.Delete, ProjectVersion: 2}}
	s.projectRow = []any{project.String(), owner.String(), "Project", "project", "", string(c.Deleting), int64(2), nil, now, now, nil, creation.String(), true, &op}
	s.operationRow = []any{op, project.String(), owner.String(), string(c.Delete), int64(2), nil, string(c.OperationCleaning), "", "domains", int64(2), raw, digest, "", now, now, nil}
	for _, entry := range manifest.Entries() {
		cleanup := "required"
		if entry.Name == c.SecretParticipant {
			cleanup = "completed"
		}
		s.participants = append(s.participants, []any{string(entry.Name), int64(1), "stopped", cleanup, []byte("[]"), "", int64(1)})
	}
	reg, _ := i.RegisterService(i.ProjectLifecycle)
	scope, _ := i.InProject(project)
	s.actor, err = reg.Actor(op, scope)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewLifecycleAuthority(s, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a, s
}

func TestIndependentSkillsCleanupOriginalTxAndRows(t *testing.T) {
	for _, mode := range []string{"current", "missing_lock", "second_read_error", "participant_tail_error"} {
		t.Run(mode, func(t *testing.T) {
			a, s := independentCleanupFixture(t)
			sentinel := errors.New("controlled original read failure")
			switch mode {
			case "missing_lock":
				s.lockErr = sentinel
			case "second_read_error":
				s.secondErr = sentinel
			case "participant_tail_error":
				s.rowsErr = sentinel
			}
			err := a.ValidateLifecycleInTx(s.ctx, s.tx, s.actor, s.cause, c.SkillsParticipant, c.CleanupPhase)
			if mode == "current" {
				if err != nil || s.operationReads != 2 || s.rowsOpened != 2 || s.rowsClosed != 2 || s.held != 1 {
					t.Fatal("original current facts not fully read/closed", err)
				}
			} else {
				hasCode(t, err, f.DependencyUnavailable)
				if !errors.Is(err, sentinel) {
					t.Fatal("original dependency error cause lost")
				}
				if s.rowsOpened != s.rowsClosed {
					t.Fatal("failed actual Rows left unclosed")
				}
				if mode == "missing_lock" && s.queries != 0 {
					t.Fatal("lock failure read private facts")
				}
				if mode == "participant_tail_error" && s.operationReads != 1 {
					t.Fatal("failed first stream promoted cached authorization")
				}
			}
		})
	}
}
