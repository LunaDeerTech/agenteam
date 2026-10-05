package secret

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type projectAuditUnitStore struct {
	Store
	tx      foundation.Tx
	locks   []foundation.LockRequest
	checks  int
	queries int
	read    func(string, []any) postgres.Row
}

func (s *projectAuditUnitStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if !tx.Valid() || tx != s.tx {
		return nil, errors.New("not this live transaction")
	}
	return s, nil
}
func (s *projectAuditUnitStore) RequireHeldLocks(_ context.Context, tx foundation.Tx, want []foundation.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.checks++
	for _, lock := range want {
		found := false
		for _, held := range s.locks {
			found = found || held.Key.Canonical() == lock.Key.Canonical() && (held.Mode == foundation.Exclusive || held.Mode == lock.Mode)
		}
		if !found {
			return errors.New("missing held lock")
		}
	}
	return nil
}
func (s *projectAuditUnitStore) AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error {
	panic("checker must not acquire")
}
func (s *projectAuditUnitStore) WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult {
	panic("checker must not open a transaction")
}
func (s *projectAuditUnitStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	s.queries++
	if s.read == nil {
		return projectAuditUnitRow{err: errors.New("unexpected SQL")}
	}
	return s.read(sql, args)
}

type projectAuditUnitRow struct {
	values []any
	err    error
}

func (r projectAuditUnitRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("wrong scan shape")
	}
	for i := range dest {
		d := reflect.ValueOf(dest[i]).Elem()
		v := reflect.ValueOf(r.values[i])
		if !v.IsValid() || !v.Type().AssignableTo(d.Type()) {
			return fmt.Errorf("scan type %d", i)
		}
		d.Set(v)
	}
	return nil
}

func projectAuditID[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	id, err := foundation.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func projectAuditCode(t *testing.T, err error, code foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("error %v, want %s", err, code)
	}
}

type projectAuditMutationFixture struct {
	store   *projectAuditUnitStore
	checker *ProjectAuditAuthority
	ctx     context.Context
	p       preparedWrite
	prior   sc.Metadata
	result  sc.MutationResult
	entry   ac.Entry
	key     ac.AppendKey
}

func newProjectAuditMutation(t *testing.T, kind sc.MutationKind, changePurpose bool) *projectAuditMutationFixture {
	t.Helper()
	f := &projectAuditMutationFixture{store: &projectAuditUnitStore{tx: foundation.NewTx()}}
	actor, _ := identity.NewHuman(projectAuditID[identity.User](t), projectAuditID[identity.Session](t))
	scope, _ := identity.InProject(projectAuditID[identity.Project](t))
	ref, _ := sc.NewCredentialRef(projectAuditID[sc.Credential](t), scope)
	command, err := foundation.NewCommandIdentity("secret", []string{scope.Details().ProjectID, actor.Details().UserID}, string(kind), "audit-unit-key")
	if err != nil {
		t.Fatal(err)
	}
	f.p = preparedWrite{actor: actor, scope: scope, command: command, kind: kind, ref: ref, purpose: sc.Model, receiptID: projectAuditID[receiptMarker](t), receipt: envelope{id: projectAuditID[payloadMarker](t)}}
	f.result = sc.MutationResult{Metadata: sc.Metadata{CredentialRef: ref, Purpose: sc.Model, Version: 1}, Deleted: kind == sc.Delete}
	action, changed := ac.SecretCreate, []ac.ChangedField{ac.ValueChanged, ac.PurposeChanged}
	if kind != sc.Create {
		f.p.expected = 7
		f.prior = sc.Metadata{CredentialRef: ref, Purpose: sc.Model, Version: 7}
		f.result.Metadata.Version = 8
		action, changed = ac.SecretUpdate, []ac.ChangedField{ac.ValueChanged}
		if changePurpose {
			f.p.purpose, f.result.Metadata.Purpose = sc.MCP, sc.MCP
			changed = append(changed, ac.PurposeChanged)
		}
		if kind == sc.Delete {
			action, changed = ac.SecretDelete, nil
		}
	}
	if kind != sc.Delete {
		f.p.value.id = projectAuditID[payloadMarker](t)
	}
	metadata, _ := ac.SecretMutationMetadata(action, f.result.Metadata.Version, changed)
	resource, _ := ac.NewResource(ac.SecretResource, ref.Details().ID.String())
	f.entry, err = ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	f.key, _ = audit.CommandAppendKey(ac.SecretProducer, command, 0)
	f.checker, err = NewProjectAuditAuthority(f.store)
	if err != nil {
		t.Fatal(err)
	}
	f.ctx = mutationAuditContext(context.Background(), f.store, f.store.tx, f.p, f.prior, f.result, f.entry, f.key)
	f.store.locks, _ = mutationLocks(command, ref, actor)
	f.store.read = func(sql string, args []any) postgres.Row {
		switch {
		case strings.Contains(sql, "FROM agenteam_secret.secret_command_receipts"):
			if args[0] != f.p.receiptID.String() {
				return projectAuditUnitRow{err: pgx.ErrNoRows}
			}
			return projectAuditUnitRow{values: []any{"project", scope.Details().ProjectID, f.key.Details().CauseRef, string(kind), ref.Details().ID.String(), string(f.p.purpose), int64(f.result.Metadata.Version), f.result.Deleted, f.p.receipt.id.String()}}
		case strings.Contains(sql, "FROM agenteam_secret.secret_payloads"):
			owner, kind := ref.Details().ID.String(), int16(valueOwner)
			if args[0] == f.p.receipt.id.String() {
				owner, kind = f.p.receiptID.String(), int16(receiptOwner)
			}
			return projectAuditUnitRow{values: []any{"project", scope.Details().ProjectID, kind, owner}}
		case strings.Contains(sql, "SELECT EXISTS"):
			return projectAuditUnitRow{values: []any{false}}
		case strings.Contains(sql, "FROM agenteam_secret.secrets"):
			return projectAuditUnitRow{values: []any{string(f.p.purpose), int64(f.result.Metadata.Version), f.p.value.id.String()}}
		default:
			return projectAuditUnitRow{err: errors.New("unexpected checker query")}
		}
	}
	return f
}

func TestSecretProjectAuditMutationFacts(t *testing.T) {
	for _, tc := range []struct {
		kind   sc.MutationKind
		change bool
	}{{sc.Create, false}, {sc.Update, false}, {sc.Update, true}, {sc.Delete, false}} {
		t.Run(fmt.Sprint(tc.kind, tc.change), func(t *testing.T) {
			f := newProjectAuditMutation(t, tc.kind, tc.change)
			if err := f.checker.CheckProjectAuditInTx(f.ctx, f.store.tx, f.entry, f.key); err != nil {
				t.Fatal(err)
			}
			if f.store.checks != 1 || f.store.queries < 3 {
				t.Fatal("facts or held locks were not checked")
			}
			// The same exact call may reach Audit's idempotent append check.
			if err := f.checker.CheckProjectAuditInTx(f.ctx, f.store.tx, f.entry, f.key); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSecretProjectAuditMutationRejectsChangedRows(t *testing.T) {
	for _, tc := range []struct {
		name, table string
		column      int
		replacement any
	}{
		{"receipt-scope", "secret_command_receipts", 0, "system"},
		{"receipt-project", "secret_command_receipts", 1, "other"},
		{"receipt-command", "secret_command_receipts", 2, "other"},
		{"receipt-kind", "secret_command_receipts", 3, "delete"},
		{"receipt-credential", "secret_command_receipts", 4, "other"},
		{"receipt-purpose", "secret_command_receipts", 5, "mcp"},
		{"receipt-version", "secret_command_receipts", 6, int64(99)},
		{"receipt-deleted", "secret_command_receipts", 7, true},
		{"receipt-payload", "secret_command_receipts", 8, "other"},
		{"payload-owner", "secret_payloads", 3, "other"},
		{"canonical-purpose", "FROM agenteam_secret.secrets", 0, "mcp"},
		{"canonical-version", "FROM agenteam_secret.secrets", 1, int64(99)},
		{"canonical-payload", "FROM agenteam_secret.secrets", 2, projectAuditID[payloadMarker](t).String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newProjectAuditMutation(t, sc.Create, false)
			read := f.store.read
			f.store.read = func(sql string, args []any) postgres.Row {
				r := read(sql, args).(projectAuditUnitRow)
				if strings.Contains(sql, tc.table) {
					r.values[tc.column] = tc.replacement
				}
				return r
			}
			if err := f.checker.CheckProjectAuditInTx(f.ctx, f.store.tx, f.entry, f.key); err == nil {
				t.Fatal("changed durable fact accepted")
			}
		})
	}
	t.Run("missing-receipt", func(t *testing.T) {
		f := newProjectAuditMutation(t, sc.Create, false)
		f.store.read = func(string, []any) postgres.Row { return projectAuditUnitRow{err: pgx.ErrNoRows} }
		projectAuditCode(t, f.checker.CheckProjectAuditInTx(f.ctx, f.store.tx, f.entry, f.key), foundation.Forbidden)
	})
	t.Run("delete-still-live", func(t *testing.T) {
		f := newProjectAuditMutation(t, sc.Delete, false)
		read := f.store.read
		f.store.read = func(sql string, args []any) postgres.Row {
			if strings.Contains(sql, "SELECT EXISTS") {
				return projectAuditUnitRow{values: []any{true}}
			}
			return read(sql, args)
		}
		projectAuditCode(t, f.checker.CheckProjectAuditInTx(f.ctx, f.store.tx, f.entry, f.key), foundation.Forbidden)
	})
}
