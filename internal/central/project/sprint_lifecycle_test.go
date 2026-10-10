package project

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

type sprintStartFactsFunc func(context.Context, f.Tx, i.Actor, c.SprintStartChange) error

func (fn sprintStartFactsFunc) CheckSprintStartAppliedInTx(ctx context.Context, tx f.Tx, a i.Actor, c c.SprintStartChange) error {
	return fn(ctx, tx, a, c)
}

type sprintPointerStore struct {
	*authorityStore
	exec func(context.Context, string, ...any) (pgconn.CommandTag, error)
	held [][]f.LockRequest
}

func (s *sprintPointerStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.authorityStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *sprintPointerStore) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return s.exec(ctx, q, args...)
}
func (s *sprintPointerStore) RequireHeldLocks(ctx context.Context, tx f.Tx, l []f.LockRequest) error {
	s.held = append(s.held, append([]f.LockRequest{}, l...))
	return s.authorityStore.RequireHeldLocks(ctx, tx, l)
}

type sprintPointerFixture struct {
	store      *sprintPointerStore
	service    *SprintLifecycle
	actor      i.Actor
	change     c.SprintStartChange
	current    c.ProjectRef
	sessionErr error
	factErr    error
	writes     int
	facts      int
	order      []string
	factFn     sprintStartFactsFunc
}

func newSprintPointerFixture(t *testing.T) *sprintPointerFixture {
	t.Helper()
	v := &sprintPointerFixture{actor: testActor(t)}
	v.store = &sprintPointerStore{authorityStore: &authorityStore{tx: f.NewTx(), order: &v.order}}
	at, err := f.NewInstant(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := f.ParseID[i.User](v.actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	before := c.ProjectRef{ID: testID[i.Project](t), OwnerUserID: owner, Name: "Demo", NormalizedName: "demo", Description: "body", Lifecycle: c.Active, Version: 3, CreatedAt: at, UpdatedAt: at}
	after := before.Clone()
	target := testID[c.Sprint](t)
	after.CurrentSprintID = &target
	after.Version++
	command, err := f.NewCommandIdentity("project", []string{before.ID.String()}, "work.sprint.start", "start-key")
	if err != nil {
		t.Fatal(err)
	}
	v.change = c.SprintStartChange{Command: command, CommandID: testID[struct{}](t).String(), PlanRevision: 1, SprintID: target, SprintVersion: 2, StartedAt: at, Before: before, After: after}
	v.current = before.Clone()
	creation := testID[c.Creation](t)
	v.store.row = func(q string, args ...any) postgres.Row {
		if q == "SELECT clock_timestamp()" {
			return valuesRow(at.Time())
		}
		if !strings.HasPrefix(q, "SELECT "+projectColumns+" FROM agenteam_project.projects") {
			t.Fatal("unexpected query")
		}
		p := v.current
		var pointer *string
		if p.CurrentSprintID != nil {
			x := p.CurrentSprintID.String()
			pointer = &x
		}
		var archived *time.Time
		if p.ArchivedAt != nil {
			x := p.ArchivedAt.Time()
			archived = &x
		}
		return valuesRow(p.ID.String(), p.OwnerUserID.String(), p.Name, p.NormalizedName, p.Description, string(p.Lifecycle), int64(p.Version), pointer, p.CreatedAt.Time(), p.UpdatedAt.Time(), archived, creation.String(), true, nil)
	}
	authority, err := NewAuthority(v.store, AuthorityDependencies{Sessions: sessionFunc(func(ctx context.Context, tx f.Tx, actor i.Actor) error {
		v.order = append(v.order, "session")
		if tx != v.store.tx || !actor.Equal(v.actor) {
			t.Fatal("wrong current Session")
		}
		return v.sessionErr
	})})
	if err != nil {
		t.Fatal(err)
	}
	facts := sprintStartFactsFunc(func(ctx context.Context, tx f.Tx, a i.Actor, change c.SprintStartChange) error {
		v.facts++
		v.order = append(v.order, "work-postimage")
		if tx != v.store.tx || !a.Equal(v.actor) || change.Command.Canonical() != v.change.Command.Canonical() {
			t.Fatal("original Work call changed")
		}
		if v.factFn != nil {
			return v.factFn(ctx, tx, a, change)
		}
		return v.factErr
	})
	v.service, err = NewSprintLifecycle(authority, facts)
	if err != nil {
		t.Fatal(err)
	}
	v.store.exec = func(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
		v.writes++
		v.order = append(v.order, "pointer-write")
		if !strings.HasPrefix(q, "UPDATE agenteam_project.projects SET current_sprint_id=") || len(args) != 5 || args[0] != before.ID.String() || args[1] != target.String() || args[2] != int64(4) || args[4] != int64(3) || v.facts != 1 {
			t.Fatal("pointer write before original facts or wrong preimage")
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return v
}
func TestSprintLifecycleProjectOriginalOwnerAndPostimage(t *testing.T) {
	v := newSprintPointerFixture(t)
	out, err := v.service.ApplySprintStartInTx(context.Background(), v.store.tx, v.actor, v.change)
	if err != nil || out.Version != 4 || out.CurrentSprintID == nil || *out.CurrentSprintID != v.change.SprintID || v.writes != 1 {
		t.Fatal("Project pointer not applied", err)
	}
	required, err := v.change.RequiredLocks(v.change.Before.OwnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.store.held) < 1 || len(v.store.held[0]) != 5 {
		t.Fatal("full original union not checked")
	}
	for n, want := range required {
		got := v.store.held[0][n]
		if got.Key.Canonical() != want.Key.Canonical() || got.Mode != want.Mode {
			t.Fatal("missing original lock")
		}
	}
	for _, scenario := range []string{"foreign-tx", "lock", "session", "owner", "stale-project", "already-current", "work-fact"} {
		t.Run(scenario, func(t *testing.T) {
			x := newSprintPointerFixture(t)
			tx := x.store.tx
			switch scenario {
			case "foreign-tx":
				tx = f.NewTx()
			case "lock":
				x.store.lockErr = errors.New("missing-lock")
			case "session":
				x.sessionErr = f.NewFault(f.Unauthenticated, f.NotStarted)
			case "owner":
				x.current.OwnerUserID = testID[i.User](t)
			case "stale-project":
				x.current.Version++
			case "already-current":
				target := x.change.SprintID
				x.current.CurrentSprintID = &target
			case "work-fact":
				x.factErr = f.NewFault(f.Forbidden, f.NotStarted)
			}
			out, err := x.service.ApplySprintStartInTx(context.Background(), tx, x.actor, x.change)
			if err == nil || out.ID.Validate() == nil || x.writes != 0 {
				t.Fatal("rejected original gate wrote or returned partial fact")
			}
			if scenario != "work-fact" && x.facts != 0 {
				t.Fatal("failed Project gate called Work")
			}
		})
	}
}
func TestSprintLifecycleProjectCancellationJoinsFacts(t *testing.T) {
	v := newSprintPointerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	v.factFn = func(context.Context, f.Tx, i.Actor, c.SprintStartChange) error { close(entered); <-release; return nil }
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
		ref, err := v.service.ApplySprintStartInTx(ctx, v.store.tx, v.actor, v.change)
		done <- result{ref, err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("original Work callback not entered")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("returned before original callback ended")
	default:
	}
	close(release)
	released = true
	r := <-done
	joined = true
	if !errors.Is(r.err, context.Canceled) || r.ref.ID.Validate() == nil || v.writes != 0 {
		t.Fatal("canceled callback published Project pointer")
	}
}
