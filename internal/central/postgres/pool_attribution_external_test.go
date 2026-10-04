//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestPoolSQLCancellationIndependentClosedDriver(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	postgres.CheckIndependentPoolCloseForTest(t, db.Config(t, nil), func(pid int32) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Terminate(ctx, pid); err != nil {
			t.Fatal(err)
		}
		conn := db.Connect(t)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			var exists bool
			if err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)", pid).Scan(&exists); err != nil {
				t.Fatal("owned backend absence query failed")
			}
			if !exists {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("independent termination did not remove owned backend")
			case <-tick.C:
			}
		}
	})
}
