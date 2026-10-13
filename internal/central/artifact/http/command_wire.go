package artifacthttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const maxCommandRepresentationBytes = 64 << 10

type creationDTO struct {
	Artifact metadataDTO `json:"artifact"`
}
type creationLookupDTO struct {
	State    ac.CreationState `json:"state"`
	Artifact *metadataDTO     `json:"artifact"`
}
type cancellationDTO struct {
	State   ac.CreationState `json:"state"`
	Cleanup *oc.CleanupState `json:"cleanup"`
}

func projectCreationMetadata(v ac.Invocation, intent contentIntent, result ac.Metadata) (metadataDTO, error) {
	metadata, err := projectMetadata(v.Details().ProjectID, result)
	if err != nil {
		return metadataDTO{}, err
	}
	d := result.Details()
	sum := sha256.Sum256([]byte(intent.content))
	expected, err := f.ParseDigest("sha256:" + hex.EncodeToString(sum[:]))
	if err != nil || d.Kind != ac.Generated || d.Name != intent.display.Name || d.Description != intent.display.Description || d.Object.MediaType != intent.media || d.Object.ByteSize != f.Progress(len(intent.content)) || d.Object.SHA256 != expected || d.CreatedByUserID != v.Details().Actor.Details().UserID || d.CreatedByAgentID != "" || d.ExecutionID != "" || d.OperationID != "" {
		return metadataDTO{}, badProjection()
	}
	return metadata, nil
}

func projectCreationLookup(v ac.Invocation, intent contentIntent, result ac.CreationLookupResult) (creationLookupDTO, error) {
	if result.Validate() != nil {
		return creationLookupDTO{}, badProjection()
	}
	out := creationLookupDTO{State: result.State}
	if result.State == ac.CreationCommitted {
		metadata, err := projectCreationMetadata(v, intent, *result.Artifact)
		if err != nil {
			return creationLookupDTO{}, err
		}
		out.Artifact = &metadata
	}
	return out, nil
}

func encodeCancellation(ctx context.Context, cleanup oc.CleanupState) ([]byte, error) {
	out := cancellationDTO{Cleanup: &cleanup}
	switch cleanup {
	case oc.CleanupPending:
		out.State = ac.CreationCancelRequested
	case oc.CleanupCompleted:
		out.State = ac.CreationCancelled
	default:
		return nil, badProjection()
	}
	return encodeRepresentation(ctx, out, maxCommandRepresentationBytes)
}
