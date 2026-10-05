package model

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type projectGrantFunc func(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error)

type projectGrantChannel chan struct{}

func (projectGrantChannel) AuthorizeProject(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error) {
	panic("constructor must not invoke the Project provider")
}

func (fn projectGrantFunc) AuthorizeProject(ctx context.Context, tx f.Tx, a id.Actor, p id.ProjectID, i id.AccessIntent) (id.AccessGrant, error) {
	return fn(ctx, tx, a, p, i)
}

type projectScopeStore struct {
	Store
	tx                         f.Tx
	locks                      []f.LockRequest
	acquisitions, transactions int
	row                        func(string, ...any) postgres.Row
}

func (s *projectScopeStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !tx.Valid() {
		return nil, fault(f.InvalidArgument)
	}
	return s, nil
}
func (s *projectScopeStore) RequireHeldLocks(_ context.Context, tx f.Tx, want []f.LockRequest) error {
	if tx != s.tx {
		return fault(f.InvalidArgument)
	}
	for _, w := range want {
		found := false
		for _, have := range s.locks {
			if have.Key.Canonical() == w.Key.Canonical() && (have.Mode == f.Exclusive || have.Mode == w.Mode) {
				found = true
			}
		}
		if !found {
			return fault(f.InvalidState)
		}
	}
	return nil
}
func (s *projectScopeStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if tx != s.tx {
		return fault(f.InvalidArgument)
	}
	s.acquisitions++
	s.locks = append([]f.LockRequest(nil), locks...)
	return nil
}
func (s *projectScopeStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	s.tx = f.NewTx()
	defer func() { s.tx = f.Tx{}; s.locks = nil }()
	if err := fn(ctx, s.tx); err != nil {
		var ff *f.Fault
		if !errors.As(err, &ff) {
			panic(err)
		}
		return f.NotCommittedResult(ff)
	}
	return f.CommittedResult()
}
func (s *projectScopeStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	return s.row(q, args...)
}
func projectGrant(actor id.Actor, project id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
	scope, _ := id.InProject(project)
	at, _ := f.NewInstant(time.Now())
	return id.NewAccessGrant(actor, scope, intent, at, 1)
}
func projectPureAuthority(t *testing.T, store Store, provider ProjectAuthority) *Authority {
	t.Helper()
	a, err := NewAuthority(store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error { return nil }), System: systemFunc(func(_ context.Context, _ f.Tx, a id.Actor, i id.AccessIntent) (id.AccessGrant, error) {
		return validGrant(a, i)
	}), Projects: provider})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestModelProjectAuthorityOptionalTypedNilExactGrantAndNoLateLocks(t *testing.T) {
	store := &projectScopeStore{tx: f.NewTx()}
	actor := testActor(t)
	project := mustID[id.Project](t)
	scope, _ := id.InProject(project)
	var typedNil projectGrantFunc
	d := Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error { return nil }), System: systemFunc(func(_ context.Context, _ f.Tx, a id.Actor, i id.AccessIntent) (id.AccessGrant, error) {
		return validGrant(a, i)
	}), Projects: typedNil}
	_, err := NewAuthority(store, d)
	requireCode(t, err, f.DependencyUnbound)
	var nilChannel projectGrantChannel
	d.Projects = nilChannel
	_, err = NewAuthority(store, d)
	requireCode(t, err, f.DependencyUnbound)
	absent := projectPureAuthority(t, store, nil)
	requireCode(t, absent.currentScope(context.Background(), store.tx, actor, scope, id.Read), f.DependencyUnbound)
	calls := 0
	provider := projectGrantFunc(func(_ context.Context, tx f.Tx, a id.Actor, p id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
		calls++
		if tx != store.tx || !a.Equal(actor) || p != project {
			t.Fatal("changed identity/transaction")
		}
		return projectGrant(a, p, intent)
	})
	d.Projects = provider
	a, err := NewAuthority(store, d)
	if err != nil {
		t.Fatal(err)
	}
	d.Projects = projectGrantFunc(func(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error) {
		t.Fatal("mutable selection")
		return id.AccessGrant{}, nil
	})
	requireCode(t, a.currentScope(context.Background(), store.tx, actor, scope, id.Mutate), f.InvalidState)
	store.locks = scopeLocks(actor, scope)
	if err = a.currentScope(context.Background(), store.tx, actor, scope, id.Mutate); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || store.acquisitions != 0 || store.transactions != 0 {
		t.Fatal("authority acquired late locks or created transaction")
	}
	requireCode(t, a.currentScope(context.Background(), f.NewTx(), actor, scope, id.Read), f.InvalidArgument)
	for _, mode := range []string{"zero", "actor", "scope", "intent"} {
		t.Run(mode, func(t *testing.T) {
			bad := projectPureAuthority(t, store, projectGrantFunc(func(_ context.Context, _ f.Tx, a id.Actor, p id.ProjectID, i id.AccessIntent) (id.AccessGrant, error) {
				switch mode {
				case "zero":
					return id.AccessGrant{}, nil
				case "actor":
					a = testActor(t)
				case "scope":
					p = mustID[id.Project](t)
				case "intent":
					i = id.Mutate
				}
				return projectGrant(a, p, i)
			}))
			requireCode(t, bad.currentScope(context.Background(), store.tx, actor, scope, id.Read), f.Forbidden)
		})
	}
}
