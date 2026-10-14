package contract

import (
	"encoding/json"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

func TestTaskBlockerExecutionCancellationProjection(t *testing.T) {
	b := blockerRecordFixture(t, false)
	reason := strings.Repeat("\t", 1023) + "x"
	b.Type = TaskBlockerUserCancelledExecution
	b.Metadata = TaskBlockerMetadata{}
	b.ExecutionCancellation = &TaskBlockerExecutionCancellationMetadata{ExecutionID: testID[i.Execution](t, 91), CancelledBy: b.CreatedBy.UserID, Reason: &reason}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var got TaskBlocker
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ExecutionCancellation == nil || *got.ExecutionCancellation.Reason != reason || got.CreatedBy != b.CreatedBy {
		t.Fatal("lost user cancellation provenance")
	}
	copy := got.Clone()
	*copy.ExecutionCancellation.Reason = "changed"
	if *got.ExecutionCancellation.Reason != reason {
		t.Fatal("aliased reason")
	}
	if (TaskBlockerCreate{BlockerID: b.ID, Type: b.Type, Description: b.Description}).Validate() == nil {
		t.Fatal("public add accepted trusted cancellation cause")
	}
	got.ExecutionCancellation.CancelledBy = testID[i.User](t, 92)
	if got.Validate() == nil {
		t.Fatal("different cancelling user accepted")
	}
}
