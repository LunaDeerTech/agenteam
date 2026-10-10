package workhttp

import (
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func (h *handler) transitionCommand(w http.ResponseWriter, r *http.Request, actor id.Actor, project c.ProjectID, route route) ([]byte, error) {
	in, err := decodeTransitionIntent(w, r, route)
	if err != nil {
		return nil, err
	}
	if h.transitions == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	ctx := r.Context()
	digest, err := c.TaskTransferDigest(actor, in.meta, project, in.target, in.request)
	if err != nil {
		return nil, err
	}
	if in.lookup {
		v, err := h.transitions.LookupTaskTransition(ctx, actor, c.TaskTransitionLookupRequest{
			ProjectID: project, Command: c.TaskTransitionTransfer,
			IdempotencyKey: in.meta.IdempotencyKey, SemanticDigest: digest,
		})
		if err != nil {
			return nil, err
		}
		if v.Validate() != nil || v.Receipt != nil && !transitionReceiptMatches(*v.Receipt, project, in) {
			return nil, badProjection()
		}
		return encodeValue(ctx, v, bodyLimit)
	}
	v, err := h.transitions.TransferTask(ctx, actor, in.meta, project, in.target, in.request.Clone())
	if err != nil {
		return nil, err
	}
	// The service may already have committed. A broken postcondition must abort
	// this response, never fabricate a NotStarted/NotCommitted Problem or retry.
	if !transitionReceiptMatches(v, project, in) {
		panic(http.ErrAbortHandler)
	}
	body, err := encodeValue(ctx, v, bodyLimit)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return body, nil
}
