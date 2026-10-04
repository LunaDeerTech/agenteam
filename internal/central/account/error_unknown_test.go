package account

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestConfirmationErrorPreservesUnknownAndCauseWithoutTypedNil(t *testing.T) {
	original := foundation.UnknownResult(foundation.ID[foundation.TransactionAttempt]{}, foundation.TransactionCause{})
	raw := errors.New("private confirmation diagnostic")
	confirmation := foundation.NewFault(foundation.InternalError, foundation.NotCommitted).WithCause(raw)
	for _, cause := range []error{nil, confirmation, resultError(original)} {
		err := confirmationError(original, cause)
		var f *foundation.Fault
		if !errors.As(err, &f) || f == nil || f.Code != foundation.CommitUnknown || f.CommitState != foundation.Unknown {
			t.Fatal("unknown state lost", err)
		}
		// Traversal must terminate safely when UnknownResult has no Fault;
		// storing its typed nil as an error would violate that boundary.
		if errors.Is(err, raw) != (cause == confirmation) {
			t.Fatal("confirmation cause not preserved")
		}
		wire, e := json.Marshal(err)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(fmt.Sprintf("%+v %#v", err, struct{ err error }{err})+string(wire), raw.Error()) {
			t.Fatal("private confirmation cause leaked")
		}
	}
}

func TestConfirmationErrorDoesNotReclassifyKnownBusinessFailure(t *testing.T) {
	for _, original := range []foundation.CommitResult{foundation.CommittedResult(), foundation.NotCommittedResult(foundation.NewFault(foundation.Forbidden, foundation.NotCommitted))} {
		for _, code := range []foundation.Code{foundation.Unauthenticated, foundation.SessionRevoked, foundation.Forbidden, foundation.IdempotencyKeyReused} {
			err := fault(code, nil)
			if got := confirmationError(original, err); got != err {
				t.Fatal("known business failure was rewritten", code)
			}
		}
		if confirmationError(original, nil) != nil {
			t.Fatal("known confirmation success became unknown")
		}
	}
	if resultError(foundation.CommittedResult()) != nil {
		t.Fatal("committed result became an error")
	}
}
