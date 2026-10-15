package object

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestMCPUploadRecoveryDoesNotAcceptDTOOrUnboundGuard(t *testing.T) {
	s, owner, key := mappingFixture(t)
	op, err := f.ParseID[oc.CleanupOperation](s.outside.upload.id.String())
	if err != nil {
		t.Fatal(err)
	}
	cause, _ := oc.NewObjectCleanupCause(oc.CleanupDetails{Owner: owner, OperationID: op, Reason: oc.AbandonedAttempt})
	process := s.outside.attempt.process
	for _, service := range []*Service{nil, {}, {data: func() *serviceState { return &serviceState{store: s} }}} {
		if err := service.JoinMCPUploadWithinBudget(context.Background(), cause, key, process); !hasCode(err, f.DependencyUnbound) {
			t.Fatal("bare DTO or unbound guard accepted", err)
		}
	}
	if s.outside.calls != 0 || s.inTx != 0 {
		t.Fatal("unbound recovery accessed Store")
	}
	for _, reason := range []oc.CleanupReason{oc.OwnerDeleted, oc.ProjectDeleted, oc.CancelledUpload, oc.ReplacedObject} {
		wrong, _ := oc.NewObjectCleanupCause(oc.CleanupDetails{Owner: owner, OperationID: op, Reason: reason})
		if err := mcpUploadRecoveryRequest(context.Background(), wrong, key, process); err == nil {
			t.Fatal("wrong recovery reason accepted")
		}
	}
	if mcpUploadRecoveryRequest(context.Background(), cause, "foreign", process) == nil || mcpUploadRecoveryRequest(context.Background(), cause, key, oc.ProcessID{}) == nil {
		t.Fatal("missing original request/process accepted")
	}
}
