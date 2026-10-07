package projecthttp

import (
	"context"
	"encoding/json"
	"math"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const updateBodyLimit = 64 << 10
const updateLookupBodyLimit = 1 << 10

// A value field preserves absence while its decoder rejects explicit null.
// DecodeJSON still checks duplicate, unknown and case-alias field names.
type updateString struct {
	value   string
	present bool
}

func (v *updateString) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '"' {
		return invalidQuery()
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	v.value, v.present = value, true
	return nil
}

type updateInput struct {
	Expected    f.Version    `json:"expected_version"`
	Name        updateString `json:"name"`
	Description updateString `json:"description"`
}

func updateKey(r *http.Request) (f.IdempotencyKey, error) {
	v := r.Header.Values("Idempotency-Key")
	if len(v) != 1 || f.IdempotencyKey(v[0]).Validate() != nil {
		return "", invalidQuery()
	}
	return f.IdempotencyKey(v[0]), nil
}
func decodeUpdate(w http.ResponseWriter, r *http.Request, key f.IdempotencyKey) (f.CommandMeta, pc.UpdateProjectRequest, error) {
	var input updateInput
	var request pc.UpdateProjectRequest
	if err := httpapi.DecodeJSON(w, r, &input, updateBodyLimit); err != nil {
		return f.CommandMeta{}, request, err
	}
	if input.Name.present {
		request.Name = &input.Name.value
	}
	if input.Description.present {
		request.Description = &input.Description.value
	}
	meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key, ExpectedVersion: &input.Expected}
	if pc.ValidateCommandMeta(pc.UpdateCommand, meta) != nil || request.Validate() != nil {
		return f.CommandMeta{}, pc.UpdateProjectRequest{}, invalidQuery()
	}
	return meta, request, nil
}
func decodeUpdateLookup(w http.ResponseWriter, r *http.Request, target pc.ProjectID, key f.IdempotencyKey) (pc.CommandLookupRequest, error) {
	var input struct {
		Command string `json:"command"`
	}
	if err := httpapi.DecodeJSON(w, r, &input, updateLookupBodyLimit); err != nil {
		return pc.CommandLookupRequest{}, err
	}
	if input.Command != string(pc.UpdateCommand) {
		return pc.CommandLookupRequest{}, invalidQuery()
	}
	return pc.CommandLookupRequest{ProjectID: target, Command: pc.UpdateCommand, Key: key}, nil
}
func encodeUpdate(ctx context.Context, actor id.Actor, target pc.ProjectID, meta f.CommandMeta, request pc.UpdateProjectRequest, value pc.ProjectRef) ([]byte, error) {
	if pc.ValidateCommandMeta(pc.UpdateCommand, meta) != nil || request.Validate() != nil || value.Lifecycle != pc.Active || request.Name != nil && value.Name != *request.Name || request.Description != nil && value.Description != *request.Description {
		return nil, badProjection()
	}
	expected := *meta.ExpectedVersion
	if value.Version != expected && (expected == f.Version(math.MaxInt64) || value.Version != expected+1) {
		return nil, badProjection()
	}
	raw, err := encodeProject(ctx, actor, target, value)
	if err == nil && len(raw) > updateBodyLimit {
		return nil, badProjection()
	}
	return raw, err
}
func encodeUpdateLookup(ctx context.Context, actor id.Actor, target pc.ProjectID, value pc.CommandLookupResult) ([]byte, error) {
	if value.Validate() != nil {
		return nil, badProjection()
	}
	type receipt struct {
		Command pc.CommandName  `json:"command"`
		Project json.RawMessage `json:"project"`
	}
	out := struct {
		State  pc.LookupState `json:"state"`
		Result *receipt       `json:"result,omitempty"`
	}{State: value.State}
	if value.Result != nil {
		if value.Result.Command != pc.UpdateCommand || value.Result.Project == nil || value.Result.Project.Lifecycle != pc.Active {
			return nil, badProjection()
		}
		raw, err := encodeProject(ctx, actor, target, *value.Result.Project)
		if err != nil {
			return nil, err
		}
		out.Result = &receipt{pc.UpdateCommand, raw}
	}
	raw, err := encodeValue(ctx, out)
	if err == nil && len(raw) > updateBodyLimit {
		return nil, badProjection()
	}
	return raw, err
}
