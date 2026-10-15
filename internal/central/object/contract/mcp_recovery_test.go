package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMCPRecoveryMappingClosedTuple(t *testing.T) {
	owner, _ := NewObjectOwner(MCPContent, testID, secondID)
	upload, _ := f.ParseID[Upload](testID)
	object, _ := f.ParseID[StoredObject](secondID)
	attempt, _ := f.ParseID[Attempt](testID)
	process, _ := f.ParseID[Process](secondID)
	agent, _ := f.ParseID[id.Agent](testID)
	execution, _ := f.ParseID[id.Execution](secondID)
	m := MCPUploadMapping{Owner: owner, Key: f.IdempotencyKey(testID), UploadID: upload, ObjectID: object,
		AttemptID: attempt, ProcessID: process, AgentID: agent, ExecutionID: execution, CreationCause: testID, State: "unknown", Disposition: "revoked"}
	if m.Validate() != nil {
		t.Fatal("valid original mapping rejected")
	}
	for name, change := range map[string]func(*MCPUploadMapping){
		"other-owner":         func(v *MCPUploadMapping) { v.Owner, _ = NewObjectOwner(Knowledge, testID, secondID) },
		"other-key":           func(v *MCPUploadMapping) { v.Key = f.IdempotencyKey(secondID) },
		"missing-upload":      func(v *MCPUploadMapping) { v.UploadID = UploadID{} },
		"missing-object":      func(v *MCPUploadMapping) { v.ObjectID = ObjectID{} },
		"missing-attempt":     func(v *MCPUploadMapping) { v.AttemptID = AttemptID{} },
		"missing-process":     func(v *MCPUploadMapping) { v.ProcessID = ProcessID{} },
		"missing-agent":       func(v *MCPUploadMapping) { v.AgentID = id.AgentID{} },
		"missing-execution":   func(v *MCPUploadMapping) { v.ExecutionID = id.ExecutionID{} },
		"invalid-cause":       func(v *MCPUploadMapping) { v.CreationCause = "not-original" },
		"unknown-state":       func(v *MCPUploadMapping) { v.State = "absent" },
		"unknown-disposition": func(v *MCPUploadMapping) { v.Disposition = "consumed" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := m
			change(&changed)
			if changed.Validate() == nil {
				t.Fatal("invalid mapping accepted")
			}
		})
	}
	m.AttemptID, m.ProcessID = AttemptID{}, ProcessID{}
	if m.Validate() != nil {
		t.Fatal("original upload before attempt must remain observable")
	}
}

func TestMCPRecoveryReleaseRequiresExactOwnerCauseUpload(t *testing.T) {
	owner, _ := NewObjectOwner(MCPContent, testID, secondID)
	upload, _ := f.ParseID[Upload](testID)
	object, _ := f.ParseID[StoredObject](secondID)
	operation, _ := f.ParseID[CleanupOperation](testID)
	cause, _ := NewObjectCleanupCause(CleanupDetails{Owner: owner, Reason: AbandonedAttempt, OperationID: operation})
	d := AccessRequestDetails{Operation: ReleaseForCleanupAccess, Cleanup: cause, ObjectID: object, UploadID: upload}
	original, err := NewCleanupReleaseAccess(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []CleanupReason{ProjectDeleted, CancelledUpload, ReplacedObject, OwnerDeleted} {
		other := d
		other.Cleanup, _ = NewObjectCleanupCause(CleanupDetails{Owner: owner, Reason: reason, OperationID: operation})
		if _, err := NewCleanupReleaseAccess(other); err == nil {
			t.Fatal("other MCP cleanup reason admitted", reason)
		}
	}
	d.UploadID, _ = f.ParseID[Upload](secondID)
	other, err := NewCleanupReleaseAccess(d)
	if err != nil || original.Equal(other) {
		t.Fatal("upload identity collapsed", err)
	}
	d.UploadID = UploadID{}
	if _, err := NewCleanupReleaseAccess(d); err == nil {
		t.Fatal("missing upload admitted")
	}
}
