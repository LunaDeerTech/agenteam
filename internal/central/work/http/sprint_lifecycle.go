package workhttp

import (
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func (h *handler) sprintLifecycleCommand(w http.ResponseWriter, r *http.Request, actor id.Actor, project c.ProjectID, route route) ([]byte, error) {
	in, err := decodeSprintStartIntent(w, r, route)
	if err != nil {
		return nil, err
	}
	if h.sprintLifecycle == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	digest, err := c.StartSprintDigest(actor, in.meta, project, in.target)
	if err != nil {
		return nil, err
	}
	ctx := r.Context()
	if in.lookup {
		value, err := h.sprintLifecycle.LookupStartSprint(ctx, actor, c.SprintStartLookupRequest{ProjectID: project, Key: in.meta.IdempotencyKey, Semantic: digest})
		if err != nil {
			return nil, err
		}
		// The domain's State/Result is not a JSON contract. Validate its union
		// before projecting the public status/receipt representation.
		switch value.State {
		case c.LookupCommitted:
			if value.Result == nil || !sprintStartReceiptMatches(*value.Result, actor, project, in) {
				return nil, badProjection()
			}
		case c.LookupInProgress, c.LookupNotObserved:
			if value.Result != nil {
				return nil, badProjection()
			}
		default:
			return nil, badProjection()
		}
		return encodeValue(ctx, sprintStartLookupWire{value.State, value.Result}, bodyLimit)
	}
	value, err := h.sprintLifecycle.StartSprint(ctx, actor, in.meta, project, in.target)
	if err != nil {
		return nil, err
	}
	// A successful service call can already have committed. Bad output cannot
	// become a fictitious NotStarted/NotCommitted Problem or a second write.
	if !sprintStartReceiptMatches(value, actor, project, in) {
		panic(http.ErrAbortHandler)
	}
	body, err := encodeValue(ctx, value, bodyLimit)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return body, nil
}
