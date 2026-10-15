package artifacthttp

import (
	"context"
	"net/http"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type artifactMetadataReader interface {
	ReadArtifactMetadata(context.Context, ac.Invocation, ac.ArtifactID) (ac.Metadata, error)
}

func (h *handler) executeMetadata(r *http.Request, route route, v ac.Invocation) ([]byte, error) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	artifact, err := f.ParseID[ac.ArtifactEntity](route.artifact)
	if err != nil || artifact.String() != route.artifact {
		return nil, invalidInput()
	}
	if h.metadata == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	out, err := h.metadata.ReadArtifactMetadata(r.Context(), v, artifact)
	if err != nil {
		return nil, err
	}
	metadata, err := projectMetadata(v.Details().ProjectID, out)
	if err != nil {
		return nil, err
	}
	if metadata.Reference.ArtifactID != artifact {
		return nil, badProjection()
	}
	return encodeRepresentation(r.Context(), struct {
		Artifact metadataDTO `json:"artifact"`
	}{metadata}, maxCommandRepresentationBytes)
}
