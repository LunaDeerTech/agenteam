package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ExecutionMetadataRequest identifies a caller-owned preparation transaction.
// Its IDs are input, not authority. RunnerIDs is a non-nil, strictly increasing
// set of at most 128 IDs; an empty set still requires current authorization.
type ExecutionMetadataRequest struct {
	ProjectID   i.ProjectID
	AgentID     i.AgentID
	ExecutionID i.ExecutionID
	RunnerIDs   []RunnerID
}

func (r ExecutionMetadataRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.RunnerIDs == nil || len(r.RunnerIDs) > 128 {
		return invalid()
	}
	for n, id := range r.RunnerIDs {
		if id.Validate() != nil || n > 0 && r.RunnerIDs[n-1].String() >= id.String() {
			return invalid()
		}
	}
	return nil
}

// ExecutionMetadata is the complete safe Runner projection for durable
// execution capture. Availability and protocol compatibility do not determine
// whether this metadata can be captured.
type ExecutionMetadata struct {
	ID          RunnerID  `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     f.Version `json:"version"`
}

func (v ExecutionMetadata) Validate() error {
	if v.ID.Validate() != nil || !textValid(v.Name, 1, 128) || !textValid(v.Description, 0, 4096) || v.Version.Validate() != nil {
		return invalid()
	}
	return nil
}

// ExecutionMetadataAuthority is implemented by the Execution owner. It must
// require the current private preparation witness for this exact request and
// original transaction; public request IDs alone cannot authorize a read.
type ExecutionMetadataAuthority interface {
	RequireExecutionMetadataInTx(context.Context, f.Tx, ExecutionMetadataRequest) error
}

// ExecutionMetadataProvider reads only safe Runner facts after same-Store,
// held-lock and Execution-owner checks. The caller collects the complete lock
// union before acquiring any locks. InTx neither opens a transaction nor adds
// locks, and returns all requested metadata in request order or no result.
type ExecutionMetadataProvider interface {
	ExecutionMetadataLocks([]RunnerID) ([]f.LockRequest, error)
	ReadExecutionMetadataInTx(context.Context, f.Tx, ExecutionMetadataRequest) ([]ExecutionMetadata, error)
}
