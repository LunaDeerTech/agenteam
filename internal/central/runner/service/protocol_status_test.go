package service

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

type protocolStatusExecutor struct {
	*connectionExecutor
	marked int
}

func (x *protocolStatusExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if query != `UPDATE agenteam_runner.runners SET incompatible=true WHERE id=$1` || len(args) != 1 || args[0] != x.connection.RunnerID().String() {
		return pgconn.CommandTag{}, errors.New("unexpected compatibility write")
	}
	x.marked++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type protocolStatusStore struct {
	*connectionStore
	x *protocolStatusExecutor
}

func (s *protocolStatusStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("different transaction")
	}
	return s.x, nil
}

func TestRunnerProtocolStatusPureCurrentOwnerOnly(t *testing.T) {
	_, base, _, _ := deviceFixture(t)
	store := &protocolStatusStore{connectionStore: &connectionStore{deviceStore: base}}
	authority, _ := NewAuthority(store, &readerAuthority{})
	s, e := New(authority, readerAudit{})
	if e != nil {
		t.Fatal(e)
	}
	value := connectionReservation{s.state(), base.x.target, sid[connectionID](t), 2, 3, "/srv"}
	connection := Connection{data: func() connectionReservation { return value }}
	store.x = &protocolStatusExecutor{connectionExecutor: &connectionExecutor{deviceExecutor: base.x, service: s, connection: connection, current: true}}
	if e := s.RejectIncompatible(context.Background(), connection); e != nil || store.x.marked != 1 || len(store.held) != 1 || store.held[0].Mode != f.Exclusive {
		t.Fatal("compatibility not written inside exclusive current gate", e)
	}
	store.x.current = false
	if e := s.RejectIncompatible(context.Background(), connection); e == nil || store.x.marked != 1 {
		t.Fatal("old generation marked successor incompatible")
	}
	store.x.current = true
	store.unknown = true
	if e := s.RejectIncompatible(context.Background(), connection); e == nil {
		t.Fatal("unknown compatibility write reported committed")
	} else {
		var commit commitFailure
		if !errors.As(e, &commit) || commit.result.AttemptID() != store.attempt || commit.result.Cause().Details().Owner != "runner-connection" {
			t.Fatal("original unknown attempt/cause lost")
		}
	}
}
