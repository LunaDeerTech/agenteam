package contract

import (
	"context"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// WriteCommandLookupRequest observes a Human's historical Model credential
// receipt. It neither carries material nor authorizes replay of new material.
type WriteCommandLookupRequest struct {
	Actor           id.Actor
	Scope           id.Scope
	Identity        f.CommandIdentity
	Kind            MutationKind
	Ref             CredentialRef
	ExpectedVersion f.Version
	Purpose         Purpose
}

func (r WriteCommandLookupRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != id.Human || r.Scope.Validate() != nil || r.Purpose != Model || r.Identity.Validate() != nil || r.Identity.Namespace() != "secret" || r.Identity.Command() != string(r.Kind) {
		return bad()
	}
	owners := r.Identity.OwnerIDs()
	switch r.Scope.Details().Kind {
	case id.System:
		if len(owners) != 1 || owners[0] != r.Actor.Details().UserID {
			return bad()
		}
	case id.ProjectScope:
		if len(owners) != 2 || owners[0] != r.Scope.Details().ProjectID || owners[1] != r.Actor.Details().UserID {
			return bad()
		}
	default:
		return bad()
	}
	switch r.Kind {
	case Create:
		if r.Ref.Validate() == nil || r.ExpectedVersion != 0 {
			return bad()
		}
	case Update, Delete:
		if r.Ref.Validate() != nil || !r.Ref.Details().Scope.Equal(r.Scope) || r.ExpectedVersion.Validate() != nil || r.ExpectedVersion == f.Version(math.MaxInt64) {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}

type WriteCommandObservation struct {
	Observed bool            `json:"observed"`
	Result   *MutationResult `json:"result"`
}

type HumanWriteCommands interface {
	ExecuteWrite(context.Context, WriteRequest) (MutationResult, error)
	Metadata(context.Context, id.Actor, CredentialRef) (Metadata, error)
	LookupWriteCommand(context.Context, WriteCommandLookupRequest) (WriteCommandObservation, error)
}
