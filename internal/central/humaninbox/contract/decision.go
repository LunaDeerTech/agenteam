package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SourceContext carries source-owned navigation identity, never authority.
// Proposal sources leave it empty; a Decision source identifies its Meeting.
type SourceContext struct {
	MeetingID string `json:"meeting_id,omitempty"`
}

// DecisionSourceReadAuthority requires the Inbox's independent synchronous
// Decision projection or Owner rebuild witness, bound to the original Store,
// transaction, Project, Meeting and Decision. A scan has empty source IDs and
// requires the Owner's Project-exclusive lock; a single read holds P/M/D.
type DecisionSourceReadAuthority interface {
	RequireDecisionSourceReadInTx(ctx context.Context, tx f.Tx, store any, project i.ProjectID, meetingID, decisionID string, scan bool) error
}

// DecisionSourceProvider supplies canonical Meeting-owned facts. An open item
// is a prompt to inspect the source; the original response operation retains
// its full Execution and current-wait authorization checks.
type DecisionSourceProvider interface {
	ReadDecisionSourceInTx(ctx context.Context, tx f.Tx, project i.ProjectID, meetingID, decisionID string) (Source, error)
	ScanDecisionSourcesInTx(ctx context.Context, tx f.Tx, project i.ProjectID) ([]Source, error)
}
