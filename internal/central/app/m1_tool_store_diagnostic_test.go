//go:build integration && linux

package app

import (
	"errors"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Observe the original returned commit result without changing SQL, return
// provenance or retry behavior. Only bounded technical identifiers are logged;
// PostgreSQL Message/Detail, SQL text, bind arguments and cause IDs are omitted.
func m1CommitObserver(t *testing.T) func(f.TransactionCause, f.CommitResult) {
	var count atomic.Int32
	identifier := regexp.MustCompile(`^[a-zA-Z0-9_.-]{0,128}$`)
	safe := func(value string) string {
		if identifier.MatchString(value) {
			return value
		}
		return "invalid_identifier"
	}
	return func(cause f.TransactionCause, result f.CommitResult) {
		details := cause.Details()
		transfer := details.Kind == f.CommandsCause && details.Primary.Command() == "work.task.transfer"
		if result.State() != f.NotCommitted || !strings.HasPrefix(details.Owner, "execution") && !strings.HasPrefix(details.JobType, "execution") && !transfer || count.Add(1) > 32 {
			return
		}
		fault := result.Fault()
		if fault == nil {
			return
		}
		t.Logf("M1 original transaction rejected owner=%s job=%s command=%s code=%s outcome=%s", safe(details.Owner), safe(details.JobType), safe(details.Primary.Command()), fault.Code.Safe(), fault.CommitState.Safe())
		var db *postgres.Error
		if errors.As(fault, &db) {
			t.Logf("M1 database error code=%s sqlstate=%s", db.Code(), safe(db.SQLState()))
		}
		var pg *pgconn.PgError
		if errors.As(fault, &pg) {
			t.Logf("M1 database constraint sqlstate=%s table=%s constraint=%s", safe(pg.Code), safe(pg.TableName), safe(pg.ConstraintName))
		}
	}
}
