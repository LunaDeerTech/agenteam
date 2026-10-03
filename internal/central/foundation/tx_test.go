package foundation

import (
	"errors"
	"reflect"
	"testing"
)

func TestOpaqueTxAndCommitEvidence(t *testing.T) {
	a, b := NewTx(), NewTx()
	if !a.Valid() || a == b || (Tx{}).Valid() {
		t.Fatal("invalid token identity")
	}
	for _, method := range []string{"Commit", "Rollback", "Conn"} {
		if _, exposed := reflect.TypeOf(a).MethodByName(method); exposed {
			t.Fatal("unexpected public capability")
		}
	}
	attempt, err := NewID[TransactionAttempt]()
	if err != nil {
		t.Fatal(err)
	}
	cause, err := NewRecoveryCause("test", attempt.String(), "checkpoint/1")
	if err != nil {
		t.Fatal(err)
	}
	unknown := UnknownResult(attempt, cause)
	if unknown.State() != Unknown || unknown.AttemptID() != attempt || unknown.Cause().Details().CheckpointRef != "checkpoint/1" {
		t.Fatal("lost unknown evidence")
	}
	raw := errors.New("private_callback_failure")
	fault := NewFault(InvalidArgument, Unknown).WithCause(raw)
	fault.FieldErrors = []FieldError{{Path: "/original", Code: "ORIGINAL"}}
	result := NotCommittedResult(fault)
	fault.FieldErrors[0].Path = "/changed"
	if result.Fault().FieldErrors[0].Path != "/original" {
		t.Fatal("result aliases input field errors")
	}
	if result.State() != NotCommitted || result.Fault().CommitState != NotCommitted || !errors.Is(result.Fault(), raw) || fault.CommitState != Unknown {
		t.Fatal("fault projection mutated or lost cause")
	}
	if CommittedResult().State() != Committed || (CommitResult{}).State() != Unknown {
		t.Fatal("invalid commit states")
	}
}
