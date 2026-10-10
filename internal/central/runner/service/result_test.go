package service

import (
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func TestRunnerCommitResultPureFailureBoundaries(t *testing.T) {
	// LoadConfig provides a real safe D03 error without touching a pool/socket.
	// D03's NotCommitted adapter wraps non-domain failures as InternalError;
	// this test verifies the consuming boundary, not a physical transaction.
	_, databaseFailure := postgres.LoadConfig(func(string) (string, bool) { return "", false }, nil)
	if databaseFailure == nil {
		t.Fatal("missing database configuration unexpectedly valid")
	}
	for _, code := range []postgres.Code{postgres.AdmissionStopped, postgres.ConnectionFailed} {
		if !databaseAdmissionUnavailable(code) {
			t.Fatal("known pre-callback admission failure not classified unavailable")
		}
	}
	for _, code := range []postgres.Code{postgres.SQLFailed, postgres.TransactionBeginFailed, postgres.TransactionCommitFailed, postgres.InvalidConfiguration, postgres.InvalidTransaction, postgres.TransactionExpired, postgres.TransactionNested, postgres.TransactionRowsOpen, postgres.LockFailed, postgres.LockOrderViolation, postgres.LockUpgradeForbidden, postgres.MigrationFailed, postgres.VersionUnsupported, "future_code", ""} {
		if databaseAdmissionUnavailable(code) {
			t.Fatal("non-admission failure classified as infrastructure outage")
		}
	}
	for _, test := range []struct {
		name  string
		input *f.Fault
		code  f.Code
	}{
		{"non-admission database failure", f.NewFault(f.InternalError, f.NotCommitted).WithCause(databaseFailure), f.InternalError},
		{"ordinary internal failure", f.NewFault(f.InternalError, f.NotCommitted).WithCause(errors.New("private")), f.InternalError},
		{"business rejection", f.NewFault(f.VersionConflict, f.NotCommitted), f.VersionConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := resultError(f.NotCommittedResult(test.input))
			var got *f.Fault
			if !errors.As(result, &got) || got == nil || got.Code != test.code || got.CommitState != f.NotCommitted {
				t.Fatal("known failure changed meaning or commit state")
			}
			if test.input.Code != f.InternalError && test.input.Code != got.Code || test.input.CommitState != f.NotCommitted {
				t.Fatal("original fault mutated")
			}
		})
	}
	if resultError(f.CommittedResult()) != nil {
		t.Fatal("known commit became an error")
	}
	attempt, _ := f.NewID[f.TransactionAttempt]()
	run, _ := f.NewID[struct{}]()
	cause, e := f.NewRecoveryCause("runner-boundary-test", run.String(), "")
	if e != nil {
		t.Fatal("invalid original cause stimulus")
	}
	result := resultError(f.UnknownResult(attempt, cause))
	var unknown commitFailure
	var fault *f.Fault
	if !errors.As(result, &unknown) || unknown.result.AttemptID() != attempt || unknown.result.Cause().Details().Owner != "runner-boundary-test" || !errors.As(result, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown {
		t.Fatal("unknown provenance changed by dependency mapping")
	}
}
