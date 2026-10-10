package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

type connectionExecutor struct {
	*deviceExecutor
	service        *Service
	connection     Connection
	current        bool
	reads, retired int
	retireEntered  chan context.Context
	retireRelease  chan struct{}
}

func (x *connectionExecutor) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	x.reads++
	if strings.HasPrefix(query, "SELECT EXISTS") {
		r := x.connection.data()
		matches := args[0] == r.runner.String() && args[1] == r.id.String() && args[2] == x.service.state().owner.String() && args[3] == r.credential && args[4] == r.generation
		return deviceRow{values: []any{x.current && matches}}
	}
	return deviceRow{err: errors.New("unexpected gate query")}
}
func (x *connectionExecutor) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(query, "DELETE FROM agenteam_runner.connections") {
		return pgconn.CommandTag{}, errors.New("unexpected gate mutation")
	}
	r := x.connection.data()
	if args[0] != r.runner.String() || args[1] != r.id.String() || args[2] != x.service.state().owner.String() || args[3] != r.credential || args[4] != r.generation {
		return pgconn.CommandTag{}, errors.New("retirement not fully generation bound")
	}
	if x.retireEntered != nil {
		x.retireEntered <- ctx
		<-x.retireRelease
	}
	x.retired++
	return pgconn.NewCommandTag("DELETE 1"), nil
}

type connectionStore struct {
	*deviceStore
	connectionSQL *connectionExecutor
}

func (s *connectionStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign gate transaction")
	}
	return s.connectionSQL, nil
}

func connectionFixture(t *testing.T) (*Service, *connectionStore, Connection) {
	t.Helper()
	_, base, _, _ := deviceFixture(t)
	store := &connectionStore{deviceStore: base}
	authority, _ := NewAuthority(store, &readerAuthority{})
	s, e := New(authority, readerAudit{})
	if e != nil {
		t.Fatal(e)
	}
	value := connectionReservation{s.state(), base.x.target, sid[connectionID](t), 2, 3, "/srv"}
	connection := Connection{data: func() connectionReservation { return value }}
	store.connectionSQL = &connectionExecutor{deviceExecutor: base.x, service: s, connection: connection, current: true}
	return s, store, connection
}

func TestRunnerConnectionPureGateDeadlineAndCurrentGeneration(t *testing.T) {
	s, store, connection := connectionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	called := 0
	e := s.WithCurrentConnection(ctx, connection, func(gate context.Context) error {
		called++
		actual, ok := gate.Deadline()
		if !ok || actual != deadline || len(store.held) != 1 || store.held[0].Mode != f.Shared {
			t.Fatal("gate refreshed deadline or held global management lock")
		}
		want, _ := f.SystemConfigLock("runner-control-" + connection.RunnerID().String())
		if f.CompareLockKeys(store.held[0].Key, want) != 0 {
			t.Fatal("gate took wrong resource lock")
		}
		return nil
	})
	if e != nil || called != 1 {
		t.Fatal("current write", e)
	}
	store.connectionSQL.current = false
	if e = s.WithCurrentConnection(ctx, connection, func(context.Context) error { called++; return nil }); e == nil || called != 1 {
		t.Fatal("replaced generation reached writer")
	}
	other, _, _ := connectionFixture(t)
	if e = other.CurrentConnection(ctx, connection); e == nil {
		t.Fatal("foreign issuer accepted")
	}
}

func TestRunnerConnectionPureCanceledWriteIsNotJoined(t *testing.T) {
	s, store, connection := connectionFixture(t)
	entered, release, returned := make(chan context.Context, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		returned <- s.WithCurrentConnection(context.Background(), connection, func(ctx context.Context) error {
			entered <- ctx
			<-release
			return ctx.Err()
		})
	}()
	ctx := <-entered
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > connectionBudget {
		t.Fatal("missing bounded gate")
	}
	s.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("writer not canceled")
	}
	force, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := s.Force(force); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("cancel incorrectly counted as actual writer join", e)
	}
	select {
	case <-returned:
		t.Fatal("original writer returned before release")
	default:
	}
	before := store.connectionSQL.reads
	if s.CurrentConnection(context.Background(), connection) == nil || store.connectionSQL.reads != before {
		t.Fatal("Stop admitted another generation read")
	}
	close(release)
	<-returned
	drain, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if e := s.Drain(drain); e != nil {
		t.Fatal(e)
	}
}

func TestRunnerConnectionPureRetirementOwnsActualTail(t *testing.T) {
	s, store, connection := connectionFixture(t)
	store.connectionSQL.retireEntered = make(chan context.Context, 1)
	store.connectionSQL.retireRelease = make(chan struct{})
	entered, returned := make(chan struct{}), make(chan error, 1)
	parent, parentCancel := context.WithTimeout(context.Background(), time.Second)
	defer parentCancel()
	deadline, _ := parent.Deadline()
	go func() {
		returned <- s.OwnConnection(parent, connection, func(ctx context.Context) error { close(entered); <-ctx.Done(); return nil })
	}()
	<-entered
	s.Stop()
	tail := <-store.connectionSQL.retireEntered
	if actual, _ := tail.Deadline(); actual != deadline || tail.Err() != nil {
		t.Fatal("cleanup replaced parent deadline or reused canceled child")
	}
	force, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e := s.Force(force); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("cleanup SQL falsely joined", e)
	}
	close(store.connectionSQL.retireRelease)
	if e := <-returned; e != nil || store.connectionSQL.retired != 1 {
		t.Fatal("actual retirement", e)
	}
	if e := s.Drain(parent); e != nil {
		t.Fatal(e)
	}
}

func TestRunnerConnectionPureOldCleanupCannotDeleteSuccessor(t *testing.T) {
	s, store, connection := connectionFixture(t)
	e := s.OwnConnection(context.Background(), connection, func(context.Context) error { store.connectionSQL.current = false; return nil })
	if e != nil || store.connectionSQL.retired != 0 {
		t.Fatal("old owner cleanup touched replacement", e)
	}
}
