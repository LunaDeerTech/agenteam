//go:build integration

package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This same probe runs first on the frozen old implementation and then on the
// repair. It tests local cancellation join; withholding cancellation does not
// establish that the server must already have stopped.
func TestPoolCancellationOwnerJoinsControlBeforeRelease(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newCancellationProxy(t, db, true)
	store, err := postgres.Open(testContext(t), proxy.config(t, db, "2"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = store.ForceClose(ctx)
	})
	t.Cleanup(proxy.releaseHeld)
	var pid int32
	if err := store.QueryRow(testContext(t), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	admin := db.Connect(t)
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := store.Exec(ctx, "SELECT pg_sleep(60)"); done <- err }()
	waitDatabase(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", pid)
	start := time.Now()
	cancel()
	receive(t, proxy.reached)
	if err := receive(t, done); err == nil {
		t.Fatal("cancelled SQL succeeded")
	}
	ownerElapsed := time.Since(start)
	closed := proxy.controlClosed(t)
	t.Logf("exact_backend=%d owner_return_elapsed=%s first_control_closed_at_owner_return=%t", pid, ownerElapsed, closed)
	if !closed {
		t.Error("SQL owner returned while its real cancellation control socket was still live")
	}
	force, stop := context.WithTimeout(context.Background(), time.Second)
	deadline, _ := force.Deadline()
	t.Logf("force_entry_remaining=%s", time.Until(deadline))
	_ = store.ForceClose(force)
	stop()
	proxy.releaseHeld()
	waitDatabase(t, admin, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid)
	proxy.Close()
	t.Log("data/control proxy tasks joined and exact backend exited after fixture release")
}
