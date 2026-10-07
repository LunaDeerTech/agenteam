package model

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const credentialMaterialBodyBytes = 400 * 1024
const credentialSmallBodyBytes = 1024
const credentialOutputBytes = 1024

type credentialObservation struct {
	Observed bool                  `json:"observed"`
	Result   *httpCredentialResult `json:"result"`
}

func credentialEmptyBody(r *http.Request) error {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return fault(f.InvalidArgument)
	}
	if r.Body == nil {
		return nil
	}
	var one [1]byte
	n, err := r.Body.Read(one[:])
	if n != 0 || err != io.EOF {
		return fault(f.InvalidArgument)
	}
	return nil
}
func credentialIdentity(actor id.Actor, scope id.Scope, key f.IdempotencyKey, kind sc.MutationKind) (f.CommandIdentity, error) {
	return f.NewCommandIdentity("secret", []string{scope.Details().ProjectID, actor.Details().UserID}, string(kind), key)
}
func credentialExpected(raw json.RawMessage) (f.Version, error) {
	var version f.Version
	if len(raw) == 0 || json.Unmarshal(raw, &version) != nil || version.Validate() != nil || version == f.Version(math.MaxInt64) {
		return 0, fault(f.InvalidArgument)
	}
	return version, nil
}
func credentialDecodeMutation(w http.ResponseWriter, r *http.Request, actor id.Actor, scope id.Scope, key f.IdempotencyKey, kind sc.MutationKind, ref sc.CredentialRef) (sc.WriteRequest, error) {
	var expected f.Version
	var value httpCredentialValue
	if kind == sc.Create {
		var dto struct {
			Value httpCredentialValue `json:"value"`
		}
		if err := httpapi.DecodeJSON(w, r, &dto, credentialMaterialBodyBytes); err != nil {
			return sc.WriteRequest{}, err
		}
		value = dto.Value
	} else if kind == sc.Update {
		var dto struct {
			Expected json.RawMessage     `json:"expected_version"`
			Value    httpCredentialValue `json:"value"`
		}
		if err := httpapi.DecodeJSON(w, r, &dto, credentialMaterialBodyBytes); err != nil {
			return sc.WriteRequest{}, err
		}
		var err error
		expected, err = credentialExpected(dto.Expected)
		if err != nil {
			return sc.WriteRequest{}, err
		}
		value = dto.Value
	} else {
		var dto struct {
			Expected json.RawMessage `json:"expected_version"`
		}
		if err := httpapi.DecodeJSON(w, r, &dto, credentialSmallBodyBytes); err != nil {
			return sc.WriteRequest{}, err
		}
		var err error
		expected, err = credentialExpected(dto.Expected)
		if err != nil {
			return sc.WriteRequest{}, err
		}
	}
	command, err := credentialIdentity(actor, scope, key, kind)
	if err != nil {
		return sc.WriteRequest{}, err
	}
	request := sc.WriteRequest{Actor: actor, Scope: scope, Identity: command, Kind: kind, Ref: ref, ExpectedVersion: expected, Purpose: sc.Model}
	if kind != sc.Delete {
		material, err := value.material()
		if err != nil {
			return sc.WriteRequest{}, err
		}
		request.Value = material
	}
	return request, nil
}
func credentialDecodeLookup(w http.ResponseWriter, r *http.Request, actor id.Actor, scope id.Scope, key f.IdempotencyKey) (sc.WriteCommandLookupRequest, error) {
	var dto struct {
		Kind       sc.MutationKind `json:"kind"`
		Credential json.RawMessage `json:"credential_id"`
		Expected   json.RawMessage `json:"expected_version"`
	}
	if err := httpapi.DecodeJSON(w, r, &dto, credentialSmallBodyBytes); err != nil {
		return sc.WriteCommandLookupRequest{}, err
	}
	command, err := credentialIdentity(actor, scope, key, dto.Kind)
	if err != nil {
		return sc.WriteCommandLookupRequest{}, fault(f.InvalidArgument)
	}
	request := sc.WriteCommandLookupRequest{Actor: actor, Scope: scope, Identity: command, Kind: dto.Kind, Purpose: sc.Model}
	switch dto.Kind {
	case sc.Create:
		if len(dto.Credential) != 0 || len(dto.Expected) != 0 {
			return sc.WriteCommandLookupRequest{}, fault(f.InvalidArgument)
		}
	case sc.Update, sc.Delete:
		var key sc.CredentialID
		if json.Unmarshal(dto.Credential, &key) != nil || key.Validate() != nil {
			return sc.WriteCommandLookupRequest{}, fault(f.InvalidArgument)
		}
		request.Ref, _ = sc.NewCredentialRef(key, scope)
		request.ExpectedVersion, err = credentialExpected(dto.Expected)
		if err != nil {
			return sc.WriteCommandLookupRequest{}, err
		}
	default:
		return sc.WriteCommandLookupRequest{}, fault(f.InvalidArgument)
	}
	if request.Validate() != nil {
		return sc.WriteCommandLookupRequest{}, fault(f.InvalidArgument)
	}
	return request, nil
}
func credentialMutationDTO(r sc.WriteCommandLookupRequest, result sc.MutationResult) (httpCredentialResult, error) {
	ref := result.Metadata.CredentialRef
	if r.Validate() != nil || ref.Validate() != nil || !ref.Details().Scope.Equal(r.Scope) || result.Metadata.Purpose != sc.Model || result.Metadata.Version.Validate() != nil || result.Deleted != (r.Kind == sc.Delete) {
		return httpCredentialResult{}, unavailable(nil)
	}
	if r.Kind == sc.Create {
		if result.Metadata.Version != 1 {
			return httpCredentialResult{}, unavailable(nil)
		}
	} else if !ref.Equal(r.Ref) || result.Metadata.Version != r.ExpectedVersion+1 {
		return httpCredentialResult{}, unavailable(nil)
	}
	return httpCredentialDTO(result), nil
}
func credentialObservationDTO(r sc.WriteCommandLookupRequest, o sc.WriteCommandObservation) (credentialObservation, error) {
	if r.Validate() != nil || o.Observed != (o.Result != nil) {
		return credentialObservation{}, unavailable(nil)
	}
	if !o.Observed {
		return credentialObservation{}, nil
	}
	v, err := credentialMutationDTO(r, *o.Result)
	if err != nil {
		return credentialObservation{}, err
	}
	return credentialObservation{true, &v}, nil
}
func credentialEncode(ctx context.Context, value any) ([]byte, error) {
	credentialCheckBudget(ctx)
	data, err := json.Marshal(value)
	if err != nil || len(data) > credentialOutputBytes {
		return nil, unavailable(nil)
	}
	credentialCheckBudget(ctx)
	return data, nil
}
func credentialEncodeMutation(ctx context.Context, r sc.WriteCommandLookupRequest, v sc.MutationResult) ([]byte, error) {
	dto, err := credentialMutationDTO(r, v)
	if err != nil {
		return nil, err
	}
	return credentialEncode(ctx, dto)
}
func credentialEncodeObservation(ctx context.Context, r sc.WriteCommandLookupRequest, v sc.WriteCommandObservation) ([]byte, error) {
	dto, err := credentialObservationDTO(r, v)
	if err != nil {
		return nil, err
	}
	return credentialEncode(ctx, dto)
}
func credentialEncodeMetadata(ctx context.Context, ref sc.CredentialRef, v sc.Metadata) ([]byte, error) {
	if v.CredentialRef.Validate() != nil || !v.Purpose.Valid() || v.Version.Validate() != nil || !v.CredentialRef.Equal(ref) {
		return nil, unavailable(nil)
	}
	if v.Purpose != sc.Model {
		return nil, fault(f.NotFound)
	}
	return credentialEncode(ctx, httpCredentialMetadata{v.CredentialRef.Details().ID, v.Purpose, v.Version})
}
