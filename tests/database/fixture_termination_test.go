//go:build integration

package database_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestFixtureTerminationOwnedAbsentIsIdempotent(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	target, observer := db.Connect(t), db.Connect(t)
	pid := int32(target.PgConn().PID())
	waitDatabase(t, observer, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=$2)`, pid, db.Name)
	if err := target.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
	waitDatabase(t, observer, `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)`, pid)
	for range 2 {
		if err := db.Terminate(testContext(t), pid); err != nil {
			t.Fatal("previously owned exact PID already absent", err)
		}
	}
}

func TestFixtureTerminationOwnedBackendSignalsThenConverges(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	target, observer := db.Connect(t), db.Connect(t)
	pid := int32(target.PgConn().PID())
	waitDatabase(t, observer, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=$2)`, pid, db.Name)
	if err := db.Terminate(testContext(t), pid); err != nil {
		t.Fatal(err)
	}
	// Terminate's true result is signal acceptance, not itself an exit proof.
	waitDatabase(t, observer, `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1)`, pid)
	if err := target.Ping(testContext(t)); err == nil {
		t.Fatal("terminated backend is still usable")
	}
	if err := db.Terminate(testContext(t), pid); err != nil {
		t.Fatal("termination replay after confirmed exit", err)
	}
}

func TestFixtureTerminationRejectsForeignDatabaseWithoutSignal(t *testing.T) {
	db, other := pgfixture.NewDatabase(t), pgfixture.NewDatabase(t)
	target, observer := other.Connect(t), db.Connect(t)
	pid := int32(target.PgConn().PID())
	waitDatabase(t, observer, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=$2)`, pid, other.Name)
	err := db.Terminate(testContext(t), pid)
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.Code != foundation.Forbidden {
		t.Fatalf("foreign live PID must be refused, got %v", err)
	}
	if err := target.Ping(testContext(t)); err != nil || int32(target.PgConn().PID()) != pid {
		t.Fatal("foreign database backend was signalled or replaced")
	}
	waitDatabase(t, observer, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=$2)`, pid, other.Name)
}

func TestFixtureTerminationConnectionAndCancellationErrorsRemainVisible(t *testing.T) {
	for _, mode := range []string{"cancelled", "invalid-credentials"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			target, observer := db.Connect(t), db.Connect(t)
			pid := int32(target.PgConn().PID())
			waitDatabase(t, observer, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=$2)`, pid, db.Name)
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			request := *db
			const rejectedPassword = "fixture-rejected-password-never-log"
			if mode == "cancelled" {
				cancel()
			} else {
				// Same verified task-owned endpoint/nonce; only the new control
				// connection's temporary password is deliberately invalid.
				descriptor := *db.Fixture
				descriptor.Password = rejectedPassword
				request.Fixture = &descriptor
			}
			err := request.Terminate(ctx, pid)
			var fault *foundation.Fault
			if !errors.As(err, &fault) {
				t.Fatalf("cleanup failure falsely reported success: %v", err)
			}
			if mode == "cancelled" {
				if fault.Code != foundation.DependencyUnavailable || !errors.Is(err, context.Canceled) {
					t.Fatal("parent cancellation identity lost")
				}
			} else if fault.Code != foundation.DependencyUnavailable || errors.Unwrap(err) == nil || !strings.Contains(errors.Unwrap(err).Error(), "connection failed") {
				t.Fatal("connection failure phase lost")
			}
			encoded, marshalErr := json.Marshal(err)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			for _, projected := range []string{fmt.Sprint(err), fmt.Sprintf("%+v", struct{ err error }{err}), string(encoded)} {
				if strings.Contains(projected, rejectedPassword) || strings.Contains(projected, db.Fixture.Password) {
					t.Fatal("cleanup error projection leaked fixture credentials")
				}
			}
			if err := target.Ping(testContext(t)); err != nil {
				t.Fatal("failed cleanup affected the target backend")
			}
		})
	}
}
