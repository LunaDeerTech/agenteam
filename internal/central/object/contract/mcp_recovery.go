package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MCPUploadMapping is original Object metadata, never a receipt or grant. The
// caller must re-read it in the authorized cleanup transaction. Missing metadata
// is not evidence that its process stopped.
type MCPUploadMapping struct {
	Owner         ObjectOwner
	Key           f.IdempotencyKey
	UploadID      UploadID
	ObjectID      ObjectID
	AttemptID     AttemptID
	ProcessID     ProcessID
	AgentID       id.AgentID
	ExecutionID   id.ExecutionID
	CreationCause string
	State         string
	Disposition   string
}

func (m MCPUploadMapping) Validate() error {
	if m.Owner.Validate() != nil || m.Owner.Details().Kind != MCPContent || !validID(m.Owner.Details().ProjectID) ||
		m.Key.Validate() != nil || string(m.Key) != m.Owner.Details().ID || m.UploadID.Validate() != nil || m.ObjectID.Validate() != nil ||
		m.AgentID.Validate() != nil || m.ExecutionID.Validate() != nil || !validID(m.CreationCause) {
		return bad()
	}
	if (m.AttemptID == (AttemptID{})) != (m.ProcessID == (ProcessID{})) ||
		m.AttemptID != (AttemptID{}) && (m.AttemptID.Validate() != nil || m.ProcessID.Validate() != nil) {
		return bad()
	}
	switch m.State {
	case "pending", "unknown", "failed", "committed":
	default:
		return bad()
	}
	switch m.Disposition {
	case "reserved", "attached", "revoked":
	default:
		return bad()
	}
	return nil
}

// MCPUploadResolver does not acquire locks, create an upload, return credentials,
// or issue authorization. ResolveInTx must use the original Store and live Tx.
type MCPUploadResolver interface {
	Discover(context.Context, ObjectOwner, f.IdempotencyKey) (MCPUploadMapping, bool, error)
	ResolveInTx(context.Context, f.Tx, ObjectOwner, f.IdempotencyKey) (MCPUploadMapping, bool, error)
}

// MCPUploadRecovery joins only the original stopped process's exact writer.
// Reference cleanup must already have revoked the upload under its private
// domain proof; physical deletion remains the normal Object cleanup operation.
type MCPUploadRecovery interface {
	JoinMCPUploadWithinBudget(context.Context, ObjectCleanupCause, f.IdempotencyKey, ProcessID) error
}
