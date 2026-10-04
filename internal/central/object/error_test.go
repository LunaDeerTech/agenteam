package object

import (
	"errors"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestRevokedCommandHistoryDoesNotRewriteUnknownOrOtherFailure(t *testing.T) {
	historical := foundation.NewFault(foundation.ResourceDeleted, foundation.Committed)
	cases := []struct {
		result foundation.CommitResult
		domain error
		code   foundation.Code
		state  foundation.CommitState
	}{
		{foundation.NotCommittedResult(historical), historical, foundation.ResourceDeleted, foundation.Committed},
		{foundation.UnknownResult(foundation.ID[foundation.TransactionAttempt]{}, foundation.TransactionCause{}), historical, foundation.CommitUnknown, foundation.Unknown},
		{foundation.NotCommittedResult(foundation.NewFault(foundation.InternalError, foundation.NotStarted)), historical, foundation.InternalError, foundation.NotCommitted},
		{foundation.NotCommittedResult(foundation.NewFault(foundation.ResourceDeleted, foundation.NotStarted)), deleted(false), foundation.ResourceDeleted, foundation.NotCommitted},
	}
	for _, test := range cases {
		var fault *foundation.Fault
		err := commandCommitError(test.result, test.domain)
		if !errors.As(err, &fault) || fault.Code != test.code || fault.CommitState != test.state {
			t.Fatalf("incorrect command/transaction projection: %v", err)
		}
	}
}
