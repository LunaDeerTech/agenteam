package workhttp

import (
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type sprintCompleteLookupWire struct {
	Status  c.LookupState             `json:"status"`
	Receipt *c.SprintCompleteMutation `json:"receipt"`
}

func (h *handler) sprintCompletionCommand(r *http.Request, actor id.Actor, project c.ProjectID, in sprintStartIntent) ([]byte, error) {
	if h.sprintCompletion == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	digest, err := c.CompleteSprintDigest(actor, in.meta, project, in.target)
	if err != nil {
		return nil, err
	}
	ctx := r.Context()
	if in.lookup {
		value, err := h.sprintCompletion.LookupCompleteSprint(ctx, actor, c.SprintCompleteLookupRequest{ProjectID: project, Key: in.meta.IdempotencyKey, Semantic: digest})
		if err != nil {
			return nil, err
		}
		// The domain's State/Result is not a JSON contract. Validate its union
		// before projecting the public status/receipt representation.
		switch value.State {
		case c.LookupCommitted:
			if value.Result == nil || !sprintCompleteReceiptMatches(*value.Result, actor, project, in) {
				return nil, badProjection()
			}
		case c.LookupInProgress, c.LookupNotObserved:
			if value.Result != nil {
				return nil, badProjection()
			}
		default:
			return nil, badProjection()
		}
		return encodeValue(ctx, sprintCompleteLookupWire{value.State, value.Result}, bodyLimit)
	}
	value, err := h.sprintCompletion.CompleteSprint(ctx, actor, in.meta, project, in.target)
	if err != nil {
		return nil, err
	}
	// A successful service call can already have committed. Bad output cannot
	// become a fictitious NotStarted/NotCommitted Problem or a second write.
	if !sprintCompleteReceiptMatches(value, actor, project, in) {
		panic(http.ErrAbortHandler)
	}
	body, err := encodeValue(ctx, value, bodyLimit)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return body, nil
}

func sprintCompleteReceiptMatches(v c.SprintCompleteMutation, actor id.Actor, project c.ProjectID, in sprintStartIntent) bool {
	return v.Validate() == nil && v.Sprint.ProjectID == project && v.Sprint.ID == in.target &&
		validVersion(v.Sprint.Version, in.meta.ExpectedVersion, true) &&
		v.Project.OwnerUserID.String() == actor.Details().UserID &&
		v.Sprint.CompletedBy != nil && v.Sprint.CompletedBy.Kind == id.Human && v.Sprint.CompletedBy.UserID == actor.Details().UserID
}
