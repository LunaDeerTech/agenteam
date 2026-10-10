package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAgentCommitOutcomeKeepsPhysicalProvenance(t *testing.T) {
	command, err := f.NewCommandIdentity("project", []string{"01900000-0000-7000-8000-000000000001"}, "agent.create", "private-command-key")
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := f.ParseID[f.TransactionAttempt]("01900000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	original := f.UnknownResult(attempt, cause)
	wrapped := commitError(original)
	var problem *f.Fault
	if !errors.As(wrapped, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.RetryHint != "lookup" {
		t.Fatal("unknown downgraded")
	}
	retained, ok := UnknownAttempt(wrapped)
	if !ok || retained.AttemptID() != attempt || retained.Cause().Details().Primary.Canonical() != command.Canonical() {
		t.Fatal("original attempt/cause lost")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", wrapped, wrapped), "private-command-key") {
		t.Fatal("commit error leaked command material")
	}
	begin := f.NewFault(f.DependencyUnavailable, f.NotCommitted)
	begin.FieldErrors = []f.FieldError{{Path: "", Code: "BEGIN_UNAVAILABLE"}}
	if err := commitError(f.NotCommittedResult(begin)); !errors.As(err, &problem) || problem.Code != begin.Code || problem.CommitState != f.NotCommitted || len(problem.FieldErrors) != 1 {
		t.Fatal("pre-callback failure lost")
	}
	canceled := commitError(f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(context.Canceled)))
	if !errors.Is(canceled, context.Canceled) || !errors.As(canceled, &problem) || problem.CommitState != f.NotCommitted {
		t.Fatal("known cancellation provenance lost")
	}
	if commitError(f.CommittedResult()) != nil {
		t.Fatal("committed downgraded")
	}
}
