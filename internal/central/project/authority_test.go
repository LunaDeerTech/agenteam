package project

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func testID[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	id, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func testActor(t *testing.T) identity.Actor {
	t.Helper()
	a, e := identity.NewHuman(testID[identity.User](t), testID[identity.Session](t))
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func hasCode(t *testing.T, err error, want foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != want {
		t.Fatalf("code = %v, want %s", err, want)
	}
}

type rowFunc func(...any) error

func (f rowFunc) Scan(v ...any) error { return f(v...) }
func valuesRow(values ...any) postgres.Row {
	return rowFunc(func(dest ...any) error {
		if len(dest) != len(values) {
			return errors.New("scan arity")
		}
		for i, v := range values {
			d := reflect.ValueOf(dest[i]).Elem()
			if v == nil {
				d.SetZero()
				continue
			}
			x := reflect.ValueOf(v)
			if !x.Type().ConvertibleTo(d.Type()) {
				return errors.New("scan type")
			}
			d.Set(x.Convert(d.Type()))
		}
		return nil
	})
}

type authorityStore struct {
	Store
	tx      foundation.Tx
	order   *[]string
	locks   []foundation.LockRequest
	lockErr error
	row     func(string, ...any) postgres.Row
}

func (s *authorityStore) RequireHeldLocks(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	*s.order = append(*s.order, "locks")
	s.locks = append([]foundation.LockRequest(nil), locks...)
	if s.lockErr != nil {
		return s.lockErr
	}
	if tx != s.tx {
		return errors.New("foreign tx")
	}
	return nil
}
func (s *authorityStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	*s.order = append(*s.order, "executor")
	if tx != s.tx {
		return nil, errors.New("foreign tx")
	}
	return s, nil
}
func (s *authorityStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	*s.order = append(*s.order, "query")
	return s.row(q, args...)
}

type sessionFunc func(context.Context, foundation.Tx, identity.Actor) error

func (f sessionFunc) RequireCurrentSession(ctx context.Context, tx foundation.Tx, a identity.Actor) error {
	return f(ctx, tx, a)
}
func TestOwnerAuthorityOrdersLockSessionAndFacts(t *testing.T) {
	ctx := context.Background()
	actor := testActor(t)
	project := testID[identity.Project](t)
	creation := testID[c.Creation](t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	order := []string{}
	st := &authorityStore{tx: foundation.NewTx(), order: &order}
	sessionErr := error(nil)
	a, e := NewAuthority(st, AuthorityDependencies{Sessions: sessionFunc(func(_ context.Context, tx foundation.Tx, got identity.Actor) error {
		order = append(order, "session")
		if tx != st.tx || !got.Equal(actor) {
			t.Fatal("wrong current identity")
		}
		return sessionErr
	})})
	if e != nil {
		t.Fatal(e)
	}
	st.row = func(q string, args ...any) postgres.Row {
		if q == "SELECT clock_timestamp()" {
			return valuesRow(now)
		}
		return valuesRow(project.String(), actor.Details().UserID, "Demo", "demo", "body", string(c.Active), int64(1), nil, now, now, nil, creation.String(), true, nil)
	}
	access, e := a.RequireOwnerInTx(ctx, st.tx, actor, project, identity.Read)
	if e != nil || !access.Matches(actor, project) {
		t.Fatal("current owner denied", e)
	}
	if !reflect.DeepEqual(order[:3], []string{"locks", "session", "executor"}) {
		t.Fatal("authorization order", order)
	}
	order = nil
	sessionErr = fault(foundation.SessionRevoked)
	_, e = a.RequireOwnerInTx(ctx, st.tx, actor, project, identity.Read)
	hasCode(t, e, foundation.SessionRevoked)
	if !reflect.DeepEqual(order, []string{"locks", "session"}) {
		t.Fatal("revoked session reached Project data")
	}
	order = nil
	sessionErr = nil
	st.lockErr = errors.New("weak lock")
	_, e = a.RequireOwnerInTx(ctx, st.tx, actor, project, identity.Read)
	hasCode(t, e, foundation.DependencyUnavailable)
	if !reflect.DeepEqual(order, []string{"locks"}) {
		t.Fatal("weak lock was ignored")
	}
	st.lockErr = nil
	order = nil
	_, e = a.RequireOwnerInTx(ctx, foundation.Tx{}, actor, project, identity.Read)
	hasCode(t, e, foundation.InvalidArgument)
	if len(order) != 0 {
		t.Fatal("zero tx touched store")
	}
}
func TestForeignOwnerAndUninitializedRowsAreNotGrants(t *testing.T) {
	actor := testActor(t)
	project := testID[identity.Project](t)
	creation := testID[c.Creation](t)
	now := time.Now().UTC()
	order := []string{}
	st := &authorityStore{tx: foundation.NewTx(), order: &order}
	a, e := NewAuthority(st, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, foundation.Tx, identity.Actor) error { return nil })})
	if e != nil {
		t.Fatal(e)
	}
	owner := testID[identity.User](t).String()
	initialized := true
	st.row = func(string, ...any) postgres.Row {
		return valuesRow(project.String(), owner, "Demo", "demo", "body", string(c.Active), int64(1), nil, now, now, nil, creation.String(), initialized, nil)
	}
	_, e = a.RequireOwnerInTx(context.Background(), st.tx, actor, project, identity.Read)
	hasCode(t, e, foundation.NotFound)
	owner = actor.Details().UserID
	initialized = false
	_, e = a.RequireOwnerInTx(context.Background(), st.tx, actor, project, identity.Read)
	hasCode(t, e, foundation.ProjectNotActive)
	run, _ := identity.NewAgentRun(project, testID[identity.Agent](t), testID[identity.Execution](t))
	_, e = a.RequireOwnerInTx(context.Background(), st.tx, run, project, identity.Read)
	hasCode(t, e, foundation.DependencyUnbound)
	reg, _ := identity.RegisterService(identity.ProjectInitialization)
	scope, _ := identity.InProject(project)
	service, _ := reg.Actor(creation.String(), scope)
	_, e = a.RequireOwnerInTx(context.Background(), st.tx, service, project, identity.Read)
	hasCode(t, e, foundation.Forbidden)
}
func TestMissingOrdinaryProjectDoesNotExposeTombstone(t *testing.T) {
	actor := testActor(t)
	id := testID[identity.Project](t)
	order := []string{}
	st := &authorityStore{order: &order}
	owner := actor.Details().UserID
	st.row = func(q string, args ...any) postgres.Row {
		if q == "SELECT original_owner_user_id::text FROM agenteam_project.deletion_receipts WHERE deleted_project_id=$1" {
			return valuesRow(owner)
		}
		return rowFunc(func(...any) error { return pgx.ErrNoRows })
	}
	_, e := ownerProject(context.Background(), st, actor, id)
	hasCode(t, e, foundation.NotFound)
	owner = testID[identity.User](t).String()
	_, e = ownerProject(context.Background(), st, actor, id)
	hasCode(t, e, foundation.NotFound)
}
