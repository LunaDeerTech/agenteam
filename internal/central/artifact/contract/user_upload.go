package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// UserUploadIntent describes the original Human file. It carries no Object
// owner, upload receipt, storage address, caller identity or execution binding.
// The boundary normalizes MediaType before constructing this value; the Object
// preparation still measures the actual stream and verifies an optional hash.
type UserUploadIntent struct {
	Display   Display
	MediaType string
	ByteSize  f.Progress
	SHA256    *f.Digest
}

func (v UserUploadIntent) Validate() error {
	media, err := oc.NormalizeMediaType(v.MediaType)
	if v.Display.Validate() != nil || err != nil || media != v.MediaType || v.ByteSize.Validate() != nil || int64(v.ByteSize) > oc.MaxObjectSize || v.SHA256 != nil && v.SHA256.Validate() != nil {
		return invalid()
	}
	return nil
}

// UserUploadState projects existing Artifact intent/publication facts. Pending
// is an observation, never proof that a previous writer has stopped or that a
// body may be resent. Uploaded has a verified Object but no published Artifact.
type UserUploadState string

const (
	UserUploadNotObserved     UserUploadState = "not_observed"
	UserUploadPending         UserUploadState = "pending"
	UserUploadUploaded        UserUploadState = "uploaded"
	UserUploadCommitted       UserUploadState = "committed"
	UserUploadCancelRequested UserUploadState = "cancel_requested"
	UserUploadCancelled       UserUploadState = "cancelled"
)

// UserUploadResult is an authorized observation, not a capability or a durable
// upload handle. Only Committed may supply a canonical Meeting file reference.
type UserUploadResult struct {
	State    UserUploadState
	Artifact *Metadata
}

func (r UserUploadResult) Validate() error {
	switch r.State {
	case UserUploadCommitted:
		if r.Artifact == nil || r.Artifact.Validate() != nil {
			return invalid()
		}
		d := r.Artifact.Details()
		if d.Kind != UserUpload || d.CreatedByUserID == "" || d.CreatedByAgentID != "" || d.ExecutionID != "" || d.OperationID != "" {
			return invalid()
		}
	case UserUploadNotObserved, UserUploadPending, UserUploadUploaded, UserUploadCancelRequested, UserUploadCancelled:
		if r.Artifact != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
