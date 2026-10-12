package projecthttp

import (
	"context"
	"encoding/json"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const creationBodyLimit = 16 << 10

func decodeCreation(w http.ResponseWriter, r *http.Request, key f.IdempotencyKey) (f.CommandMeta, pc.CreateProjectRequest, error) {
	var input pc.CreateProjectRequest
	if err := httpapi.DecodeJSON(w, r, &input, creationBodyLimit); err != nil {
		return f.CommandMeta{}, input, err
	}
	meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key}
	if err := pc.ValidateCommandMeta(pc.CreateCommand, meta); err != nil {
		return f.CommandMeta{}, pc.CreateProjectRequest{}, err
	}
	if err := input.Validate(); err != nil {
		return f.CommandMeta{}, pc.CreateProjectRequest{}, err
	}
	return meta, input, nil
}

func encodeCreation(ctx context.Context, actor id.Actor, target pc.ProjectID, creation pc.CreationID, value pc.CreationResult) ([]byte, string, error) {
	if actor.Validate() != nil || actor.Details().Kind != id.Human || value.Validate() != nil {
		return nil, "", badProjection()
	}
	out := struct {
		State     pc.CreationResultState `json:"state"`
		Project   json.RawMessage        `json:"project,omitempty"`
		Operation *pc.CreationOperation  `json:"operation,omitempty"`
		StatusURL string                 `json:"status_url,omitempty"`
	}{State: value.State}
	if value.State == pc.CreationReady {
		p := *value.Project
		if target == (pc.ProjectID{}) {
			target = p.ID
		}
		var err error
		out.Project, err = encodeProject(ctx, actor, target, p)
		if err != nil {
			return nil, "", err
		}
	} else {
		op := *value.Operation
		if target != (pc.ProjectID{}) && op.ProjectID != target || creation != (pc.CreationID{}) && op.ID != creation {
			return nil, "", badProjection()
		}
		out.Operation = &op
		out.StatusURL = creationPrefix + op.ID.String()
	}
	raw, err := encodeValue(ctx, out)
	if err == nil && len(raw) > maxRepresentationBytes {
		return nil, "", badProjection()
	}
	return raw, out.StatusURL, err
}
