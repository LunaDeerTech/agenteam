package artifacthttp

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type userUploadDTO struct {
	State    ac.UserUploadState `json:"state"`
	Artifact *metadataDTO       `json:"artifact"`
}

type userUploadCancellationDTO struct {
	State   ac.UserUploadState `json:"state"`
	Cleanup *oc.CleanupState   `json:"cleanup"`
}

func projectUserUploadMetadata(v ac.Invocation, input *ac.UserUploadIntent, result ac.Metadata) (metadataDTO, error) {
	if (ac.UserUploadResult{State: ac.UserUploadCommitted, Artifact: &result}).Validate() != nil {
		return metadataDTO{}, badProjection()
	}
	out, err := projectMetadata(v.Details().ProjectID, result)
	if err != nil {
		return metadataDTO{}, err
	}
	d := result.Details()
	if d.CreatedByUserID != v.Details().Actor.Details().UserID {
		return metadataDTO{}, badProjection()
	}
	if input != nil && (input.Validate() != nil || d.Name != input.Display.Name || d.Description != input.Display.Description || d.Object.MediaType != input.MediaType || d.Object.ByteSize != input.ByteSize || input.SHA256 != nil && d.Object.SHA256 != *input.SHA256) {
		return metadataDTO{}, badProjection()
	}
	return out, nil
}

func encodeUserUpload(ctx context.Context, v ac.Invocation, input *ac.UserUploadIntent, result ac.UserUploadResult) ([]byte, error) {
	if result.Validate() != nil {
		return nil, badProjection()
	}
	out := userUploadDTO{State: result.State}
	if result.Artifact != nil {
		metadata, err := projectUserUploadMetadata(v, input, *result.Artifact)
		if err != nil {
			return nil, err
		}
		out.Artifact = &metadata
	}
	return encodeRepresentation(ctx, out, maxCommandRepresentationBytes)
}

func encodeUserUploadCancellation(ctx context.Context, result ac.UserUploadResult) ([]byte, error) {
	if result.Validate() != nil {
		return nil, badProjection()
	}
	out := userUploadCancellationDTO{State: result.State}
	var cleanup oc.CleanupState
	switch result.State {
	case ac.UserUploadNotObserved:
	case ac.UserUploadCancelRequested:
		cleanup = oc.CleanupPending
		out.Cleanup = &cleanup
	case ac.UserUploadCancelled:
		cleanup = oc.CleanupCompleted
		out.Cleanup = &cleanup
	default:
		return nil, badProjection()
	}
	return encodeRepresentation(ctx, out, maxCommandRepresentationBytes)
}
