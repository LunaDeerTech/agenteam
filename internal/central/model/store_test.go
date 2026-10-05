package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestModelCommitStatesDoNotTreatUnknownAsZeroReceiptSuccess(t *testing.T) {
	ci, e := f.NewCommandIdentity("model.system", []string{mustID[struct{}](t).String()}, "provider.create", "private-command-canary")
	if e != nil {
		t.Fatal(e)
	}
	cause, _ := f.NewCommandsCause(ci)
	attempt := mustID[f.TransactionAttempt](t)
	if e = commitError(f.CommittedResult()); e != nil {
		t.Fatal(e)
	}
	rejected := f.NotCommittedResult(fault(f.VersionConflict))
	requireCode(t, commitError(rejected), f.VersionConflict)
	e = commitError(f.UnknownResult(attempt, cause))
	var unknown *UnknownCommandError
	if !errors.As(e, &unknown) || unknown.AttemptID() != attempt {
		t.Fatal("Unknown lost original attempt")
	}
	requireCode(t, e, f.CommitUnknown)
	raw, err := json.Marshal(unknown)
	if err != nil || !strings.Contains(string(raw), `"unknown"`) || strings.Contains(string(raw), "canary") {
		t.Fatal("unsafe Unknown JSON")
	}
	if strings.Contains(fmt.Sprintf("%+v", unknown), "canary") {
		t.Fatal("unsafe Unknown formatting")
	}
	driver := errors.New("private-driver-canary")
	wrapped := portError(driver)
	if !errors.Is(wrapped, driver) || strings.Contains(fmt.Sprintf("%+v", wrapped), "canary") {
		t.Fatal("unsafe driver projection or lost private cause")
	}
}
