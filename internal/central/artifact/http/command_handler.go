package artifacthttp

import (
	"context"
	"net/http"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type artifactCommands interface {
	CreateFromContent(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.Metadata, error)
	LookupCreation(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.CreationLookupResult, error)
	CancelCreation(context.Context, ac.Invocation, f.IdempotencyKey) (oc.CleanupState, error)
}

func (h *handler) executeCommand(w http.ResponseWriter, r *http.Request, route route, actor id.Actor) ([]byte, error) {
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	intent, err := decodeContentIntent(w, r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if h.commands == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	attempt, err := f.NewID[ac.CallAttempt]()
	if err != nil {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(err)
	}
	v, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: project, AttemptID: attempt})
	if err != nil {
		return nil, err
	}
	if route.command == "create-content" {
		result, err := h.commands.CreateFromContent(r.Context(), v, intent.meta, intent.display, intent.media, intent.content)
		if err != nil {
			return nil, err
		}
		metadata, err := projectCreationMetadata(v, intent, result)
		if err != nil {
			return nil, err
		}
		return encodeRepresentation(r.Context(), creationDTO{metadata}, maxCommandRepresentationBytes)
	}
	result, err := h.commands.LookupCreation(r.Context(), v, intent.meta, intent.display, intent.media, intent.content)
	if err != nil {
		return nil, err
	}
	lookup, err := projectCreationLookup(v, intent, result)
	if err != nil {
		return nil, err
	}
	if route.command == "lookup" {
		return encodeRepresentation(r.Context(), lookup, maxCommandRepresentationBytes)
	}
	if route.command != "cancel" {
		return nil, invalidInput()
	}
	// A missing row is not a reservation of this key. A concurrent command with
	// different semantics may appear immediately after Lookup: never cancel it.
	switch result.State {
	case ac.CreationNotObserved:
		return encodeRepresentation(r.Context(), cancellationDTO{State: ac.CreationNotObserved}, maxCommandRepresentationBytes)
	case ac.CreationCommitted:
		return nil, f.NewFault(f.InvalidState, f.NotStarted)
	case ac.CreationPending, ac.CreationCancelRequested, ac.CreationCancelled:
		// The immutable original digest/path/ref/Actor was matched by this actual
		// Lookup. Cancel still rechecks current Converge authority and command
		// state under its own original locks; it cannot delete a published item.
	default:
		return nil, badProjection()
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	cleanup, err := h.commands.CancelCreation(r.Context(), v, intent.meta.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	return encodeCancellation(r.Context(), cleanup)
}
