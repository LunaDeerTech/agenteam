package artifacthttp

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// The staged Human API uses the original Artifact project-call owner:
//
//	POST /api/v1/projects/{project_id}/artifacts/upload-commands/begin
//	POST /api/v1/projects/{project_id}/artifacts/upload-commands/lookup
//	POST /api/v1/projects/{project_id}/artifacts/upload-commands/complete
//	POST /api/v1/projects/{project_id}/artifacts/upload-commands/cancel
//	PUT  /api/v1/projects/{project_id}/artifacts/upload-commands/payload
//
// JSON commands carry the exact original {name, description?, media_type,
// byte_size, sha256?}; byte_size is a decimal Progress string and sha256 uses
// the original "sha256:..." Digest form. Optional keys are omitted, never null.
// Every stage uses one unchanged Idempotency-Key and the current Session/CSRF.
// Payload is the raw File/Blob body with its original Content-Type and length,
// bounded by Object's 1 GiB contract. No receipt, owner or storage URL is input.
//
// Begin/lookup/payload project {state, artifact:null|metadata}; only committed
// carries the original user_upload metadata. Complete returns {artifact:...}.
// Cancel returns {state:not_observed|cancel_requested|cancelled,
// cleanup:null|pending|completed}. Responses never expose prospective refs.
//
// A lost response/CommitUnknown leads to lookup, not automatic payload retry.
// Uploaded may complete; committed may enter the Meeting document. Pending
// retains the original attempt: only original domain facts can permit another
// writer. Cancellation does not delete an already published Artifact.
type artifactUserUploads interface {
	BeginUserUpload(context.Context, ac.Invocation, f.CommandMeta, ac.UserUploadIntent) (ac.UserUploadResult, error)
	LookupUserUpload(context.Context, ac.Invocation, f.CommandMeta, ac.UserUploadIntent) (ac.UserUploadResult, error)
	WriteUserUpload(context.Context, ac.Invocation, f.CommandMeta, string, int64, io.ReadCloser) (ac.UserUploadResult, error)
	CompleteUserUpload(context.Context, ac.Invocation, f.CommandMeta, ac.UserUploadIntent) (ac.Metadata, error)
	CancelUserUpload(context.Context, ac.Invocation, f.CommandMeta, ac.UserUploadIntent) (ac.UserUploadResult, error)
}

// Match the original Object operation upper bound while preserving every
// earlier caller deadline. Small JSON stages retain requestBudget. Actual
// body Close and the original Service return still precede request completion.
const userUploadPayloadBudget = 30 * time.Minute
const maxUserUploadCommandBytes int64 = 32 << 10

type userUploadHTTPBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (b *userUploadHTTPBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

func (h *handler) executeUserUpload(w http.ResponseWriter, r *http.Request, route route, actor id.Actor) ([]byte, error) {
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	meta, err := userUploadHeaders(r)
	if err != nil {
		return nil, err
	}
	if h.uploads == nil {
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
	if route.upload == "payload" {
		media, err := userUploadPayloadInput(w, r)
		if err != nil {
			return nil, err
		}
		out, err := h.uploads.WriteUserUpload(r.Context(), v, meta, media, r.ContentLength, r.Body)
		if err != nil {
			return nil, err
		}
		if out.State != ac.UserUploadUploaded && out.State != ac.UserUploadCommitted {
			return nil, badProjection()
		}
		if out.Artifact != nil {
			d := out.Artifact.Details()
			if d.Object.MediaType != media || int64(d.Object.ByteSize) != r.ContentLength {
				return nil, badProjection()
			}
		}
		return encodeUserUpload(r.Context(), v, nil, out)
	}
	input, err := decodeUserUploadIntent(w, r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	var out ac.UserUploadResult
	switch route.upload {
	case "begin":
		out, err = h.uploads.BeginUserUpload(r.Context(), v, meta, input)
	case "lookup":
		out, err = h.uploads.LookupUserUpload(r.Context(), v, meta, input)
	case "complete":
		var result ac.Metadata
		result, err = h.uploads.CompleteUserUpload(r.Context(), v, meta, input)
		if err != nil {
			return nil, err
		}
		metadata, err := projectUserUploadMetadata(v, &input, result)
		if err != nil {
			return nil, err
		}
		return encodeRepresentation(r.Context(), creationDTO{metadata}, maxCommandRepresentationBytes)
	case "cancel":
		out, err = h.uploads.CancelUserUpload(r.Context(), v, meta, input)
		if err != nil {
			return nil, err
		}
		return encodeUserUploadCancellation(r.Context(), out)
	default:
		return nil, invalidInput()
	}
	if err != nil {
		return nil, err
	}
	return encodeUserUpload(r.Context(), v, &input, out)
}
