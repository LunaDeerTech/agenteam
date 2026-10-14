package artifact

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func validateCreationLookupInvocation(v ac.Invocation) error {
	if v.Validate() != nil {
		return invalid()
	}
	d := v.Details()
	if d.Actor.Details().Kind == i.AgentRun {
		if d.ExecutionID == "" || d.OperationID == "" || d.ToolID == "" || d.ToolCallID == "" {
			return invalid()
		}
		return nil
	}
	if d.Actor.Details().Kind != i.Human || d.ExecutionID != "" || d.OperationID != "" || d.ToolID != "" || d.ToolCallID != "" {
		return failure(f.DependencyUnbound, nil)
	}
	return nil
}

func (s *Service) lookupCreation(ctx context.Context, v ac.Invocation, meta f.CommandMeta, display ac.Display, media, content string) (ac.CreationLookupResult, error) {
	req, err := makeContentRequest(v, meta, display, media, content)
	if err != nil {
		return ac.CreationLookupResult{}, err
	}
	r := s.state()
	// This preliminary row supplies only the lock set. No state is returned
	// until the original current authority and the locked command are checked.
	before, found, err := loadCommand(ctx, r.store, req.identity)
	if err != nil {
		return ac.CreationLookupResult{}, err
	}
	var artifact string
	if found {
		artifact = before.ref.ArtifactID.String()
	}
	cause, _ := f.NewCommandsCause(req.identity)
	var out ac.CreationLookupResult
	result := s.within(ctx, invocationSubject(v), i.Read, cause, nil, createLocks(req.identity, artifact), func(ctx context.Context, tx f.Tx, _ oc.LockedAccess) error {
		e, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, exists, err := loadCommand(ctx, e, req.identity)
		if err != nil {
			return err
		}
		if !exists {
			out.State = ac.CreationNotObserved
			return nil
		}
		if !found || current.ref != before.ref {
			return failure(f.ResourceBusy, nil)
		}
		if err = matchesCommand(current, req); err != nil {
			return err
		}
		if current.path != "content" || current.kind != ac.Generated {
			return failure(f.IdempotencyKeyReused, nil)
		}
		switch current.state {
		case "pending":
			out.State = ac.CreationPending
		case "cancel_requested":
			out.State = ac.CreationCancelRequested
		case "cancelled":
			out.State = ac.CreationCancelled
		case "deleted":
			return f.NewFault(f.ResourceDeleted, f.Committed)
		case "completed":
			// Cancellation history must never go through (or weaken) Owner.Read.
			// A committed result, however, still requires the original read grant.
			if _, err = r.owners.AuthorizeOwnerInTx(ctx, tx, v.Details().Actor, artifactOwner(current.ref), i.Read); err != nil {
				return err
			}
			row, exists, err := loadArtifact(ctx, e, current.ref.ArtifactID.String())
			if err != nil {
				return err
			}
			if !exists {
				return f.NewFault(f.ResourceDeleted, f.Committed)
			}
			if row.meta.Details().Reference != current.ref || row.meta.Details().Object.ID != current.object || row.cause != current.cause {
				return unavailable(nil)
			}
			m := row.meta
			out = ac.CreationLookupResult{State: ac.CreationCommitted, Artifact: &m}
		default:
			return unavailable(nil)
		}
		return out.Validate()
	})
	if err = commitError(result); err != nil {
		return ac.CreationLookupResult{}, err
	}
	if out.Validate() != nil {
		return ac.CreationLookupResult{}, unavailable(nil)
	}
	return out, nil
}
