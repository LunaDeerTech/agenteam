package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// DecodeJSON may discard its temporary DTO on any failure. Keep only a private
// string while decoding; allocate owned, wipeable bytes/material after the
// entire strict DTO succeeds. Go's parser/runtime string copies are not erasable.
type httpCredentialValue struct {
	value string
}

func (v *httpCredentialValue) UnmarshalJSON(raw []byte) error {
	var value string
	if len(raw) < 2 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil || len(value) < 1 || len(value) > sc.MaxValueBytes {
		return fault(f.InvalidArgument)
	}
	v.value = value
	return nil
}
func (v httpCredentialValue) MarshalJSON() ([]byte, error) {
	return []byte(`"model_credential_input"`), nil
}
func (v httpCredentialValue) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "model_credential_input")
}
func (v httpCredentialValue) LogValue() slog.Value { return slog.StringValue("model_credential_input") }
func (v *httpCredentialValue) material() (sc.SecretMaterial, error) {
	if v.value == "" {
		return sc.SecretMaterial{}, fault(f.InvalidArgument)
	}
	bytes := []byte(v.value)
	v.value = ""
	defer clear(bytes)
	material, err := sc.NewSecretMaterial(bytes)
	if err != nil {
		return sc.SecretMaterial{}, fault(f.InvalidArgument)
	}
	return material, nil
}

type httpCredentialResult struct {
	ID      sc.CredentialID `json:"credential_id"`
	Purpose sc.Purpose      `json:"purpose"`
	Version f.Version       `json:"version"`
	Deleted bool            `json:"deleted"`
}

func httpCredentialDTO(v sc.MutationResult) httpCredentialResult {
	return httpCredentialResult{v.Metadata.CredentialRef.Details().ID, v.Metadata.Purpose, v.Metadata.Version, v.Deleted}
}

func httpCredentialRequest(m mc.CommandMeta, kind sc.MutationKind, ref sc.CredentialRef, expected f.Version) (sc.WriteRequest, error) {
	command, err := f.NewCommandIdentity("secret", []string{m.Actor.Details().UserID}, string(kind), m.Key)
	if err != nil {
		return sc.WriteRequest{}, err
	}
	return sc.WriteRequest{Actor: m.Actor, Scope: id.SystemScope(), Identity: command, Kind: kind, Ref: ref, ExpectedVersion: expected, Purpose: sc.Model}, nil
}

func credentialLookupRequest(r sc.WriteRequest) sc.WriteCommandLookupRequest {
	return sc.WriteCommandLookupRequest{Actor: r.Actor, Scope: r.Scope, Identity: r.Identity, Kind: r.Kind, Ref: r.Ref, ExpectedVersion: r.ExpectedVersion, Purpose: r.Purpose}
}

func (h *systemHTTP) createCredential(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	var dto struct {
		Value httpCredentialValue `json:"value"`
	}
	if err := httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	material, err := dto.Value.material()
	if err != nil {
		return nil, err
	}
	defer material.Destroy()
	request, err := httpCredentialRequest(m, sc.Create, sc.CredentialRef{}, 0)
	if err != nil {
		return nil, err
	}
	request.Value = material
	result, err := h.writes.ExecuteWrite(r.Context(), request)
	if err != nil {
		return nil, err
	}
	return httpCredentialDTO(result), nil
}

func (h *systemHTTP) updateCredential(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	ref, err := httpCredentialRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	var dto struct {
		Expected f.Version           `json:"expected_version"`
		Value    httpCredentialValue `json:"value"`
	}
	if err = httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	material, err := dto.Value.material()
	if err != nil {
		return nil, err
	}
	defer material.Destroy()
	request, err := httpCredentialRequest(m, sc.Update, ref, dto.Expected)
	if err != nil {
		return nil, err
	}
	request.Value = material
	return h.executeCredential(r.Context(), request)
}

func (h *systemHTTP) deleteCredential(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	ref, err := httpCredentialRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	var dto struct {
		Expected f.Version `json:"expected_version"`
	}
	if err = httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	request, err := httpCredentialRequest(m, sc.Delete, ref, dto.Expected)
	if err != nil {
		return nil, err
	}
	return h.executeCredential(r.Context(), request)
}

func httpCredentialRef(raw string) (sc.CredentialRef, error) {
	key, err := f.ParseID[sc.Credential](raw)
	if err != nil {
		return sc.CredentialRef{}, fault(f.InvalidArgument)
	}
	return sc.NewCredentialRef(key, id.SystemScope())
}

// Only a matched historical Model receipt permits skipping current metadata.
// It never skips ExecuteWrite's original material digest comparison.
func (h *systemHTTP) executeCredential(ctx context.Context, r sc.WriteRequest) (any, error) {
	lookup := credentialLookupRequest(r)
	if err := lookup.Validate(); err != nil {
		return nil, err
	}
	observation, err := h.writes.LookupWriteCommand(ctx, lookup)
	if err != nil {
		return nil, err
	}
	if !observation.Observed {
		metadata, beforeErr := h.writes.Metadata(ctx, r.Actor, r.Ref)
		if beforeErr == nil {
			switch {
			case !metadata.CredentialRef.Equal(r.Ref) || metadata.Purpose != sc.Model:
				beforeErr = fault(f.NotFound)
			case metadata.Version != r.ExpectedVersion:
				beforeErr = fault(f.VersionConflict)
			}
		}
		if beforeErr != nil {
			observation, err = h.writes.LookupWriteCommand(ctx, lookup)
			if err != nil {
				return nil, err
			}
			if !observation.Observed {
				return nil, beforeErr
			}
		}
	}
	result, err := h.writes.ExecuteWrite(ctx, r)
	if err != nil {
		return nil, err
	}
	return httpCredentialDTO(result), nil
}

func (h *systemHTTP) lookupCredential(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	var dto struct {
		Kind       sc.MutationKind `json:"kind"`
		Credential json.RawMessage `json:"credential_id,omitempty"`
		Expected   json.RawMessage `json:"expected_version,omitempty"`
	}
	if err := httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	var ref sc.CredentialRef
	var expected f.Version
	if dto.Kind == sc.Create {
		if len(dto.Credential) != 0 || len(dto.Expected) != 0 {
			return nil, fault(f.InvalidArgument)
		}
	} else if dto.Kind == sc.Update || dto.Kind == sc.Delete {
		var key sc.CredentialID
		if json.Unmarshal(dto.Credential, &key) != nil || key.Validate() != nil || json.Unmarshal(dto.Expected, &expected) != nil || expected.Validate() != nil {
			return nil, fault(f.InvalidArgument)
		}
		ref, _ = sc.NewCredentialRef(key, id.SystemScope())
	} else {
		return nil, fault(f.InvalidArgument)
	}
	request, err := httpCredentialRequest(m, dto.Kind, ref, expected)
	if err != nil {
		return nil, err
	}
	lookup := credentialLookupRequest(request)
	if err = lookup.Validate(); err != nil {
		return nil, err
	}
	observation, err := h.writes.LookupWriteCommand(r.Context(), lookup)
	if err != nil {
		return nil, err
	}
	var result *httpCredentialResult
	if observation.Result != nil {
		v := httpCredentialDTO(*observation.Result)
		result = &v
	}
	return struct {
		Observed bool                  `json:"observed"`
		Result   *httpCredentialResult `json:"result"`
	}{observation.Observed, result}, nil
}
