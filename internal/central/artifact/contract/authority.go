package contract

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// AccessSubject is trusted provenance, separate from display/model arguments.
// Discovery is not authorization and must never return business data to users.
// The owning Project/Execution adapter supplies actual parent/gate locks; it
// must not guess an Execution identity from an Operation or Artifact ID.
type AccessSubject struct {
	Actor     identity.Actor
	ProjectID identity.ProjectID
	// Maintenance is discovery only. It has no Actor or invocation provenance
	// and cannot be passed to AuthorizeInTx to obtain a business grant.
	Maintenance bool
	ExecutionID string
	OperationID string
	ToolID      string
	ToolCallID  string
}

func (s AccessSubject) Validate() error {
	if s.ProjectID.Validate() != nil {
		return invalid()
	}
	if s.Maintenance {
		if s.Actor.Validate() == nil || s.ExecutionID != "" || s.OperationID != "" || s.ToolID != "" || s.ToolCallID != "" {
			return invalid()
		}
		return nil
	}
	if s.Actor.Validate() != nil {
		return invalid()
	}
	a := s.Actor.Details()
	if a.Kind == identity.AgentRun && (a.ProjectID != s.ProjectID.String() || a.ExecutionID != s.ExecutionID) {
		return invalid()
	}
	if a.Kind != identity.Human && a.Kind != identity.AgentRun && a.Kind != identity.Service {
		return invalid()
	}
	for _, v := range []string{s.ExecutionID, s.OperationID, s.ToolID, s.ToolCallID} {
		if v != "" {
			if _, e := foundation.ParseID[struct{}](v); e != nil {
				return invalid()
			}
		}
	}
	if s.OperationID != "" && s.ExecutionID == "" || s.ToolCallID != "" && s.ToolID == "" {
		return invalid()
	}
	return nil
}

// Authority is implemented by current identity/Project/Execution adapters.
// DiscoverInTx rereads discovery mappings under the already-held complete union;
// the consumer compares the entire mapping and locks before authorization, and
// any change aborts the Tx. AuthorizeInTx then checks current Session or actual
// Agent/Execution, Project Owner/gate and provenance binding. Neither method may
// open a nested Tx, acquire missing locks, or substitute a cached AccessGrant.
// Archived permits Read. A Human's Converge may cancel its own prospective
// upload. ObjectMaintenance Converge only follows the Artifact provider's exact
// persisted cancellation/cause check and must check the real current Project
// gate (active/archived, or its authorized deleting lifecycle). It grants no
// ordinary read/mutation permission. Project Lifecycle requires its exact
// durable operation and version; unbound adapters reject all of these calls.
type Authority interface {
	Discover(context.Context, AccessSubject) (oc.AccessDependencies, error)
	DiscoverInTx(context.Context, foundation.Tx, AccessSubject) (oc.AccessDependencies, error)
	AuthorizeInTx(context.Context, foundation.Tx, AccessSubject, identity.AccessIntent) (identity.AccessGrant, error)
	CheckProjectCleanupInTx(context.Context, foundation.Tx, identity.Actor, oc.ProjectCleanupCause) error
}

// UploadTarget only reserves this domain's prospective identity. The Artifact
// upload composition persists the returned object attempt before any I/O, and
// creation consumes its verified receipt. It grants no permission by itself.
type UploadTarget struct {
	Reference ArtifactRef
	Owner     oc.ObjectOwner
	Command   foundation.CommandMeta
}
