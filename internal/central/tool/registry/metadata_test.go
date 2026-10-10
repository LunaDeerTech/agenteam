package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controlled ports exercise algorithm and admission failures only. They
// are not a real backend, PG, a production registration or an execution grant.
type metadataStore struct {
	tx      f.Tx
	held    []f.LockRequest
	row     postgres.Row
	writes  []string
	args    [][]any
	queries int
}

func (s *metadataStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !tx.Valid() {
		return nil, errors.New("foreign transaction")
	}
	return s, nil
}
func (s *metadataStore) AcquireAll(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
	s.held = locks
	return nil
}
func (s *metadataStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx {
		return errors.New("foreign transaction")
	}
	for _, need := range locks {
		found := false
		for _, have := range s.held {
			if f.CompareLockKeys(have.Key, need.Key) == 0 && (have.Mode == f.Exclusive || have.Mode == need.Mode) {
				found = true
			}
		}
		if !found {
			return errors.New("missing lock")
		}
	}
	return nil
}
func (s *metadataStore) WithinTx(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if err := fn(ctx, s.tx); err != nil {
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	return f.CommittedResult()
}
func (s *metadataStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.writes = append(s.writes, sql)
	s.args = append(s.args, args)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (*metadataStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	panic("unexpected Query: this pure fixture does not emulate rows")
}
func (s *metadataStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.queries++
	if s.row == nil {
		return metadataRow{err: pgx.ErrNoRows}
	}
	return s.row
}

type metadataRow struct {
	tool, key  string
	revision   f.Version
	definition []byte
	err        error
}

func (r metadataRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 4 {
		return errors.New("unexpected pure row shape")
	}
	*dest[0].(*string) = r.tool
	*dest[1].(*string) = r.key
	*dest[2].(*f.Version) = r.revision
	*dest[3].(*[]byte) = append([]byte(nil), r.definition...)
	return nil
}

type metadataSource struct {
	registration tc.BuiltinRegistration
	checked      int
	checkErr     error
	described    int
}

func (s *metadataSource) DescribeInTx(context.Context, f.Tx) (tc.BuiltinRegistration, error) {
	s.described++
	return s.registration, nil
}
func (s *metadataSource) CheckBindingInTx(context.Context, f.Tx, tc.BuiltinRegistration) error {
	s.checked++
	return s.checkErr
}
func metadataFixture(t *testing.T) (*Registry, *metadataStore, *metadataSource) {
	t.Helper()
	s := &metadataStore{tx: f.NewTx(), held: []f.LockRequest{RegistryLock(f.Exclusive)}}
	provider := &metadataSource{registration: tc.BuiltinRegistration{Definition: tc.Definition{StableKey: "builtin:fixture", Name: "Fixture", InputSchema: []byte(`{"type":"object"}`)}, Binding: tc.BuiltinBinding{HandlerID: "fixture", ContractRevision: 1}, ScopeResolverID: "fixture.scope", RiskClassifierID: "fixture.risk", Class: tc.OrdinaryTool, Active: true}}
	r, err := New(s, Options{Sources: map[string]BuiltinSource{"builtin:fixture": provider}})
	if err != nil {
		t.Fatal(err)
	}
	return r, s, provider
}
func TestRegistryIdentityAndRevisionArePersistentMetadata(t *testing.T) {
	r, s, p := metadataFixture(t)
	first, err := r.ReconcileBuiltinInTx(context.Background(), s.tx, "builtin:fixture")
	if err != nil {
		t.Fatal(err)
	}
	if first.ToolID.Validate() != nil || first.SpecRevision != 1 || len(s.writes) != 3 || p.checked != 1 {
		t.Fatal("initial metadata was not persisted behind binding check")
	}
	canonical, err := tc.CanonicalDefinition(context.Background(), p.registration.Definition)
	if err != nil {
		t.Fatal(err)
	}
	s.row = metadataRow{tool: first.ToolID.String(), key: "builtin:fixture", revision: 1, definition: canonical}
	s.writes = nil
	s.args = nil
	again, err := r.ReconcileBuiltinInTx(context.Background(), s.tx, "builtin:fixture")
	if err != nil {
		t.Fatal(err)
	}
	if again != first || len(s.writes) != 1 || strings.Contains(s.writes[0], "spec_revisions") {
		t.Fatal("unchanged definition allocated an identity or revision")
	}
	p.registration.Definition.Description = "new formal definition"
	s.writes = nil
	s.args = nil
	next, err := r.ReconcileBuiltinInTx(context.Background(), s.tx, "builtin:fixture")
	if err != nil {
		t.Fatal(err)
	}
	if next.ToolID != first.ToolID || next.SpecRevision != 2 || len(s.writes) != 3 || strings.HasPrefix(s.writes[0], "UPDATE") {
		t.Fatal("changed definition did not append immutable history")
	}
	for _, sql := range s.writes {
		if strings.Contains(sql, "UPDATE agenteam_tool.spec_revisions") || strings.Contains(sql, "DELETE FROM agenteam_tool.spec_revisions") {
			t.Fatal("history was changed")
		}
	}
}
func TestRegistryRegistrationRequiresLiveTransactionLockAndActualPort(t *testing.T) {
	r, s, p := metadataFixture(t)
	if _, err := r.ReconcileBuiltinInTx(context.Background(), f.NewTx(), "builtin:fixture"); err == nil || p.described != 0 || s.queries != 0 {
		t.Fatal("foreign Tx reached source/SQL")
	}
	s.held = nil
	if _, err := r.ReconcileBuiltinInTx(context.Background(), s.tx, "builtin:fixture"); err == nil || p.described != 0 || s.queries != 0 {
		t.Fatal("missing complete locks reached source/SQL")
	}
	s.held = []f.LockRequest{RegistryLock(f.Exclusive)}
	if _, err := r.ReconcileBuiltinInTx(context.Background(), s.tx, "builtin:unknown"); err == nil || len(s.writes) != 0 {
		t.Fatal("unregistered source was accepted")
	}
	private := errors.New("private-backend-canary")
	p.checkErr = private
	if got, err := r.ReconcileBuiltinInTx(context.Background(), s.tx, "builtin:fixture"); err == nil || got.ToolID.Validate() == nil || !errors.Is(err, private) || strings.Contains(fmt.Sprint(err), "private-backend-canary") || len(s.writes) != 0 {
		t.Fatal("unbound actual backend wrote metadata or leaked cause")
	}
}
func TestRegistryRemovalRetainsStableIdentityAndHistory(t *testing.T) {
	r, s, p := metadataFixture(t)
	tool, err := f.ParseID[id.Tool]("018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := tc.CanonicalDefinition(context.Background(), p.registration.Definition)
	if err != nil {
		t.Fatal(err)
	}
	s.row = metadataRow{tool: tool.String(), key: "builtin:fixture", revision: 4, definition: canonical}
	p.registration.Active = false
	ref, err := r.ReconcileBuiltinInTx(context.Background(), s.tx, "builtin:fixture")
	if err != nil {
		t.Fatal(err)
	}
	if ref.ToolID != tool || ref.SpecRevision != 4 || len(s.writes) != 1 || s.writes[0] != `DELETE FROM agenteam_tool.registrations WHERE tool_id=$1` || p.checked != 0 {
		t.Fatal("removal touched retained identity/history/references")
	}
}
