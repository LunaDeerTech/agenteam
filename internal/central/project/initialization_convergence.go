package project

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ValidateInitializationConvergenceInTx authorizes only observation of the
// original active initialization's Project facts. It neither acquires locks nor
// owns the caller's transaction, and never authorizes new initialization writes.
func (a *Authority) ValidateInitializationConvergenceInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, request c.InitializationRequest) error {
	state := a.state()
	if state == nil {
		return fault(foundation.DependencyUnbound)
	}
	if !tx.Valid() || actor.Validate() != nil || request.Validate() != nil {
		return invalid()
	}
	details := actor.Details()
	if details.Kind != identity.Service || details.ServiceName != identity.ProjectInitialization || details.ProjectID != request.ProjectID.String() || details.CauseRef != request.CreationID.String() {
		return fault(foundation.Forbidden)
	}
	if err := state.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{projectLock(request.ProjectID, foundation.Exclusive)}); err != nil {
		return unavailable(err)
	}
	x, err := state.store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	creation, err := loadCreation(ctx, x, request.CreationID)
	if err != nil {
		return err
	}
	if creation == nil {
		return fault(foundation.Forbidden)
	}
	if creation.operation.ID != request.CreationID {
		return unavailable(nil)
	}
	// Selecting a different legitimate Project/key is a request mismatch, not
	// permission to follow the row's pointers to another target.
	if creation.operation.ProjectID != request.ProjectID || creation.initializationKey != request.InitializationKey {
		return fault(foundation.Forbidden)
	}
	project, err := loadProject(ctx, x, request.ProjectID)
	if err != nil {
		return err
	}
	if project == nil {
		return fault(foundation.Forbidden)
	}
	// Once the forward mapping matches, a contradictory reverse mapping or
	// owner is damaged canonical data, never a successful or absent grant.
	if project.ref.ID != request.ProjectID || project.creation != request.CreationID || project.ref.OwnerUserID != creation.owner {
		return unavailable(nil)
	}
	if project.ref.Lifecycle != c.Active {
		return fault(foundation.Forbidden)
	}
	return initializationConvergenceFacts(creation, project)
}

func initializationConvergenceFacts(creation *creationRecord, project *projectRecord) error {
	if creation.operation.State == c.CreationCompleted {
		result := creation.result
		if !project.initialized || creation.requestName != nil || creation.requestDescription != nil || creation.operation.SafeReason != "" || creation.protectedSkill == nil || creation.protectedSkill.Validate() != nil || creation.revision == nil || creation.revision.Validate() != nil || result == nil || result.Validate() != nil {
			return unavailable(nil)
		}
		// The initial result is historical. A later valid active Project name,
		// description or version must not replace it or invalidate observation.
		if result.ID != project.ref.ID || result.OwnerUserID != creation.owner || result.Lifecycle != c.Active || result.Version != 1 || result.CurrentSprintID != nil || result.ArchivedAt != nil {
			return unavailable(nil)
		}
		return nil
	}
	// In particular, require both protected fields absent: checking only that
	// they are not both present would accept a single damaged leftover field.
	if project.initialized || project.ref.Version != 1 || project.ref.CurrentSprintID != nil || project.operation != nil || creation.protectedSkill != nil || creation.revision != nil || creation.result != nil || creation.requestName == nil || creation.requestDescription == nil {
		return unavailable(nil)
	}
	if _, err := c.NormalizeName(*creation.requestName); err != nil {
		return unavailable(err)
	}
	if c.ValidateDescription(*creation.requestDescription) != nil || *creation.requestName != project.ref.Name || *creation.requestDescription != project.ref.Description {
		return unavailable(nil)
	}
	switch creation.operation.State {
	case c.CreationAccepted:
		if creation.operation.SafeReason != "" {
			return unavailable(nil)
		}
	case c.CreationInitializing:
		if creation.operation.SafeReason != "" && creation.operation.SafeReason.Validate() != nil {
			return unavailable(nil)
		}
	case c.CreationFailed:
		if creation.operation.SafeReason.Validate() != nil {
			return unavailable(nil)
		}
	default:
		return unavailable(nil)
	}
	return nil
}

var _ c.InitializationConvergenceAuthority = (*Authority)(nil)
