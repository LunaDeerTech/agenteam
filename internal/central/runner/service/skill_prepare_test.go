package service

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
)

func TestSkillPrepareOriginalConnectionSameTransaction(t *testing.T) {
	service, prior, connection := connectionFixture(t)
	store := &metadataStore{tx: f.NewTx(), executor: prior.connectionSQL}
	service.state().authority.state().store = store
	locks, err := service.SkillPrepareConnectionLocks(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.RequireSkillPrepareConnectionInTx(context.Background(), store.tx, connection); err == nil {
		t.Fatal("missing original lock accepted")
	}
	store.held = locks
	got, err := service.RequireSkillPrepareConnectionInTx(context.Background(), store.tx, connection)
	if err != nil || got != f.Version(connection.data().generation) {
		t.Fatal("original current generation", err)
	}
	if _, err = service.RequireSkillPrepareConnectionInTx(context.Background(), f.NewTx(), connection); err == nil {
		t.Fatal("foreign transaction accepted")
	}
	other, _, _ := connectionFixture(t)
	if _, err = other.RequireSkillPrepareConnectionInTx(context.Background(), store.tx, connection); err == nil {
		t.Fatal("foreign issuer accepted")
	}
	prior.connectionSQL.current = false
	if _, err = service.RequireSkillPrepareConnectionInTx(context.Background(), store.tx, connection); err == nil {
		t.Fatal("replaced/revoked connection accepted")
	}
	prior.connectionSQL.current = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = service.RequireSkillPrepareConnectionInTx(ctx, store.tx, connection); err == nil {
		t.Fatal("cancelled owner accepted")
	}
	service.Stop()
	if _, err = service.RequireSkillPrepareConnectionInTx(context.Background(), store.tx, connection); err == nil {
		t.Fatal("stopped Service accepted")
	}
	if err = service.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}
