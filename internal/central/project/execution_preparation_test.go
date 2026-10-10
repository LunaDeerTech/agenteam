package project

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type preparationProjectFixture struct {
	authority   *Authority
	store       *authorityStore
	project     i.ProjectID
	sprint      c.SprintID
	lifecycle   c.Lifecycle
	initialized bool
	missing     bool
	order       []string
}

func newPreparationProjectFixture(t *testing.T) *preparationProjectFixture {
	t.Helper()
	x := &preparationProjectFixture{project: testID[i.Project](t), sprint: testID[c.Sprint](t), lifecycle: c.Active, initialized: true}
	x.store = &authorityStore{tx: f.NewTx(), order: &x.order}
	owner, creation := testID[i.User](t), testID[c.Creation](t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	x.store.row = func(query string, args ...any) postgres.Row {
		if query != "SELECT "+projectColumns+" FROM agenteam_project.projects WHERE id=$1" || len(args) != 1 || args[0] != x.project.String() {
			t.Fatal("unexpected Project query or identity")
		}
		if x.missing {
			return rowFunc(func(...any) error { return pgx.ErrNoRows })
		}
		var archived *time.Time
		if x.lifecycle == c.Archived {
			archived = &now
		}
		sprint := x.sprint.String()
		return valuesRow(x.project.String(), owner.String(), "Demo", "demo", "safe description", string(x.lifecycle), int64(3), &sprint, now, now, archived, creation.String(), x.initialized, nil)
	}
	var err error
	x.authority, err = NewAuthority(x.store, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error {
		t.Fatal("preparation borrowed a Human Session")
		return nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func TestExecutionPreparationProjectCurrentFacts(t *testing.T) {
	x := newPreparationProjectFixture(t)
	ref, err := x.authority.RequirePreparingProjectInTx(context.Background(), x.store.tx, x.project)
	if err != nil || ref.ID != x.project || ref.Version != 3 || ref.CurrentSprintID == nil || *ref.CurrentSprintID != x.sprint {
		t.Fatal("current Project facts not returned", err)
	}
	if !reflect.DeepEqual(x.order, []string{"executor", "locks", "query"}) || len(x.store.locks) != 1 || x.store.locks[0].Key.Canonical() != projectLock(x.project, f.Shared).Key.Canonical() || x.store.locks[0].Mode != f.Shared {
		t.Fatal("original Store/held Project SH did not precede the read")
	}
	// A previous successful gate is never a cache of current lifecycle facts.
	for _, state := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		x.lifecycle = state
		ref, err = x.authority.RequirePreparingProjectInTx(context.Background(), x.store.tx, x.project)
		hasCode(t, err, f.ProjectNotActive)
		if ref.ID.Validate() == nil {
			t.Fatal("inactive Project facts escaped")
		}
	}
	x.lifecycle, x.initialized = c.Active, false
	_, err = x.authority.RequirePreparingProjectInTx(context.Background(), x.store.tx, x.project)
	hasCode(t, err, f.ProjectNotActive)
	x.missing = true
	_, err = x.authority.RequirePreparingProjectInTx(context.Background(), x.store.tx, x.project)
	hasCode(t, err, f.NotFound)
}

func TestExecutionPreparationProjectRequiresOriginalTransactionAndLock(t *testing.T) {
	x := newPreparationProjectFixture(t)
	for _, test := range []struct {
		name string
		a    *Authority
		ctx  context.Context
		tx   f.Tx
		id   i.ProjectID
		code f.Code
	}{
		{"unbound", nil, context.Background(), x.store.tx, x.project, f.DependencyUnbound},
		{"nil-context", x.authority, nil, x.store.tx, x.project, f.InvalidArgument},
		{"invalid-tx", x.authority, context.Background(), f.Tx{}, x.project, f.InvalidArgument},
		{"invalid-project", x.authority, context.Background(), x.store.tx, i.ProjectID{}, f.InvalidArgument},
		{"foreign-tx", x.authority, context.Background(), f.NewTx(), x.project, f.DependencyUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			x.order = nil
			ref, err := test.a.RequirePreparingProjectInTx(test.ctx, test.tx, test.id)
			hasCode(t, err, test.code)
			if ref.ID.Validate() == nil || len(x.order) > 1 || len(x.order) == 1 && x.order[0] != "executor" {
				t.Fatal("rejected transaction reached locks/read or returned facts")
			}
		})
	}
	x.order = nil
	x.store.lockErr = errors.New("controlled missing Project SH")
	_, err := x.authority.RequirePreparingProjectInTx(context.Background(), x.store.tx, x.project)
	hasCode(t, err, f.DependencyUnavailable)
	if !reflect.DeepEqual(x.order, []string{"executor", "locks"}) {
		t.Fatal("missing original Project lock did not prevent the read")
	}
}

func TestExecutionPreparationProjectCancellationJoinsOriginalRead(t *testing.T) {
	x := newPreparationProjectFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	x.store.row = func(string, ...any) postgres.Row {
		return rowFunc(func(...any) error {
			close(entered)
			<-release
			return fmt.Errorf("private-preparation-canary: %w", context.Canceled)
		})
	}
	type result struct {
		ref c.ProjectRef
		err error
	}
	done := make(chan result, 1)
	released, joined := false, false
	defer func() {
		cancel()
		if !released {
			close(release)
		}
		if !joined {
			<-done
		}
	}()
	go func() {
		ref, err := x.authority.RequirePreparingProjectInTx(ctx, x.store.tx, x.project)
		done <- result{ref, err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("original SQL call not entered")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("returned before the original SQL call")
	default:
	}
	close(release)
	released = true
	got := <-done
	joined = true
	if got.err != context.Canceled || !errors.Is(got.err, context.Canceled) || got.ref.ID.Validate() == nil || strings.Contains(fmt.Sprintf("%+v", got.err), "private-preparation-canary") {
		t.Fatal("cancelled read published facts or unsafe error")
	}
	// A driver can report cancellation while the caller context remains live.
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		x.store.row = func(string, ...any) postgres.Row {
			return rowFunc(func(...any) error { return fmt.Errorf("private-preparation-canary: %w", sentinel) })
		}
		ref, err := x.authority.RequirePreparingProjectInTx(context.Background(), x.store.tx, x.project)
		if err != sentinel || ref.ID.Validate() == nil || strings.Contains(fmt.Sprintf("%+v", err), "private-preparation-canary") {
			t.Fatal("driver cancellation was not safely normalized")
		}
	}
}
