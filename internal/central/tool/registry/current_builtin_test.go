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
)

// SQL and code Source are controlled here. They prove the boundary algorithm,
// not a production backend registration, execution grant or real PG call.
type currentBuiltinStore struct {
	*metadataStore
	t       *testing.T
	ctx     context.Context
	spec    tc.SpecRef
	checked int
}

func (s *currentBuiltinStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.metadataStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *currentBuiltinStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	registry, _ := f.SystemConfigLock("tool-registry")
	tool, _ := f.AggregateLock(f.ToolSpecAggregate, s.spec.ToolID.String())
	if ctx != s.ctx || tx != s.tx || len(locks) != 2 || locks[0].Key.Canonical() != registry.Canonical() || locks[1].Key.Canonical() != tool.Canonical() || locks[0].Mode != f.Shared || locks[1].Mode != f.Shared {
		s.t.Fatal("original context, Tx or Registry/ToolSpec minimum locks changed")
	}
	s.checked++
	return s.metadataStore.RequireHeldLocks(ctx, tx, locks)
}
func (s *currentBuiltinStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	panic("current binding check must not acquire locks")
}
func (s *currentBuiltinStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	panic("current binding check must not begin a transaction")
}
func (s *currentBuiltinStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	if ctx != s.ctx || s.checked == 0 || !strings.Contains(sql, "agenteam_tool.registrations") || !strings.Contains(sql, "r.spec_revision=i.latest_revision") || len(args) != 1 || args[0] != s.spec.ToolID.String() {
		s.t.Fatal("current registration read escaped original context/target/locks")
	}
	return s.metadataStore.QueryRow(ctx, sql, args...)
}

type currentBuiltinRow struct {
	registration tc.BuiltinRegistration
	spec         tc.SpecRef
	definition   []byte
}

func (r currentBuiltinRow) Scan(dest ...any) error {
	if len(dest) != 8 {
		return errors.New("unexpected current registration projection")
	}
	*dest[0].(*string) = r.registration.Definition.StableKey
	*dest[1].(*int64) = int64(r.spec.SpecRevision)
	*dest[2].(*[]byte) = append([]byte(nil), r.definition...)
	*dest[3].(*string) = r.registration.Binding.HandlerID
	*dest[4].(*int64) = int64(r.registration.Binding.ContractRevision)
	*dest[5].(*tc.ScopeResolverID) = r.registration.ScopeResolverID
	*dest[6].(*tc.RiskClassifierID) = r.registration.RiskClassifierID
	*dest[7].(*tc.ToolClass) = r.registration.Class
	return nil
}

type currentBuiltinSource struct {
	*metadataSource
	t   *testing.T
	ctx context.Context
	tx  f.Tx
}

func (s *currentBuiltinSource) DescribeInTx(ctx context.Context, tx f.Tx) (tc.BuiltinRegistration, error) {
	if ctx != s.ctx || tx != s.tx {
		s.t.Fatal("Source discovery replaced original context/Tx")
	}
	return s.metadataSource.DescribeInTx(ctx, tx)
}
func (s *currentBuiltinSource) CheckBindingInTx(ctx context.Context, tx f.Tx, registration tc.BuiltinRegistration) error {
	if ctx != s.ctx || tx != s.tx || registration.Binding != s.registration.Binding || registration.ScopeResolverID != s.registration.ScopeResolverID || registration.RiskClassifierID != s.registration.RiskClassifierID {
		s.t.Fatal("code binding was checked against another source projection")
	}
	return s.metadataSource.CheckBindingInTx(ctx, tx, registration)
}

func currentBuiltinFixture(t *testing.T) (*Registry, *currentBuiltinStore, *currentBuiltinSource) {
	t.Helper()
	_, store, source := metadataFixture(t)
	tool, err := f.ParseID[id.Tool]("018f0000-0000-7000-8000-000000000007")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), struct{}{}, "original-caller-context")
	s := &currentBuiltinStore{metadataStore: store, t: t, ctx: ctx, spec: tc.SpecRef{ToolID: tool, SpecRevision: 1}}
	key, _ := f.AggregateLock(f.ToolSpecAggregate, tool.String())
	s.held = []f.LockRequest{RegistryLock(f.Shared), {Key: key, Mode: f.Shared}}
	definition, err := tc.CanonicalDefinition(ctx, source.registration.Definition)
	if err != nil {
		t.Fatal(err)
	}
	s.row = currentBuiltinRow{source.registration, s.spec, definition}
	p := &currentBuiltinSource{metadataSource: source, t: t, ctx: ctx, tx: s.tx}
	r, err := New(s, Options{Sources: map[string]BuiltinSource{source.registration.Definition.StableKey: p}})
	if err != nil {
		t.Fatal(err)
	}
	return r, s, p
}

func requireCurrentBuiltin(r *Registry, s *currentBuiltinStore, p *currentBuiltinSource) error {
	v := p.registration
	return r.RequireCurrentBuiltinInTx(s.ctx, s.tx, s.spec, v.Binding, v.ScopeResolverID, v.RiskClassifierID)
}

func TestRegistryCurrentBuiltinRequiresExactCurrentTuple(t *testing.T) {
	r, s, p := currentBuiltinFixture(t)
	if err := requireCurrentBuiltin(r, s, p); err != nil || s.queries != 1 || p.described != 1 || p.checked != 1 || len(s.writes) != 0 {
		t.Fatal("current row and actual Source were not both checked without writes", err)
	}
	for _, field := range []string{"revision", "handler", "contract", "scope", "risk"} {
		spec, binding, scope, risk := s.spec, p.registration.Binding, p.registration.ScopeResolverID, p.registration.RiskClassifierID
		switch field {
		case "revision":
			spec.SpecRevision++
		case "handler":
			binding.HandlerID = "another.handler"
		case "contract":
			binding.ContractRevision++
		case "scope":
			scope = "another.scope"
		case "risk":
			risk = "another.risk"
		}
		if err := r.RequireCurrentBuiltinInTx(s.ctx, s.tx, spec, binding, scope, risk); err == nil {
			t.Fatal("stale or different expected tuple accepted", field)
		}
	}
	if len(s.writes) != 0 || p.checked != 6 {
		t.Fatal("expected tuple comparison skipped real binding or wrote metadata")
	}
}

func TestRegistryCurrentBuiltinRejectsMissingTransactionLocksOrSource(t *testing.T) {
	r, s, p := currentBuiltinFixture(t)
	v := p.registration
	if err := r.RequireCurrentBuiltinInTx(s.ctx, f.NewTx(), s.spec, v.Binding, v.ScopeResolverID, v.RiskClassifierID); err == nil || s.queries != 0 || p.described != 0 {
		t.Fatal("foreign transaction reached rows/source")
	}
	all := s.held
	for _, held := range [][]f.LockRequest{all[:1], all[1:], nil} {
		s.held = held
		if err := requireCurrentBuiltin(r, s, p); err == nil || s.queries != 0 || p.described != 0 {
			t.Fatal("incomplete locks reached rows/source")
		}
	}
	s.held = all
	missing, err := New(s, Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = requireCurrentBuiltin(missing, s, p)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || p.described != 0 || p.checked != 0 {
		t.Fatal("persisted registration without actual Source was accepted", err)
	}
	s.row = metadataRow{err: pgx.ErrNoRows}
	if err = requireCurrentBuiltin(r, s, p); err == nil || p.described != 0 {
		t.Fatal("removed current registration accepted")
	}
	if err = r.RequireCurrentBuiltinInTx(nil, s.tx, s.spec, v.Binding, v.ScopeResolverID, v.RiskClassifierID); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestRegistryCurrentBuiltinRejectsCodeAndDefinitionDrift(t *testing.T) {
	for _, field := range []string{"inactive", "definition", "handler", "scope", "risk", "core", "backend-error"} {
		r, s, p := currentBuiltinFixture(t)
		original := p.registration
		private := errors.New("private-binding-canary")
		switch field {
		case "inactive":
			p.registration.Active = false
		case "definition":
			p.registration.Definition.Description = "new definition"
		case "handler":
			p.registration.Binding.HandlerID = "new.handler"
		case "scope":
			p.registration.ScopeResolverID = "new.scope"
		case "risk":
			p.registration.RiskClassifierID = "new.risk"
		case "core":
			p.registration.Class = tc.CoreTool
			row := s.row.(currentBuiltinRow)
			row.registration.Class = tc.CoreTool
			s.row = row
		case "backend-error":
			p.checkErr = private
		}
		err := r.RequireCurrentBuiltinInTx(s.ctx, s.tx, s.spec, original.Binding, original.ScopeResolverID, original.RiskClassifierID)
		if err == nil || len(s.writes) != 0 {
			t.Fatal("unavailable or changed code accepted", field)
		}
		if field == "backend-error" && (!errors.Is(err, private) || strings.Contains(fmt.Sprint(err), "private-binding-canary")) {
			t.Fatal("backend refusal lost error identity or exposed raw material")
		}
	}
}
