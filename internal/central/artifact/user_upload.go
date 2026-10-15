package artifact

import (
	"context"
	"io"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// These outer calls retain the installed project-call owner until all original
// Store/Object/body Close tails return. No second service or upload ledger is
// introduced, and Unknown is never converted into a mutation retry.
func (s *Service) BeginUserUpload(ctx context.Context, v ac.Invocation, meta f.CommandMeta, input ac.UserUploadIntent) (ac.UserUploadResult, error) {
	meta, input = copyCommand(meta), copyUserUploadIntent(input)
	if err := validateUserUpload(v, meta, &input); err != nil {
		return ac.UserUploadResult{}, err
	}
	ctx, done, err := s.beginProjectCall(ctx, v, "upload")
	defer done()
	if err != nil {
		return ac.UserUploadResult{}, err
	}
	_, out, err := s.readUserUpload(ctx, v, meta, &input)
	if err == nil && out.State == ac.UserUploadNotObserved {
		_, err = s.beginUpload(ctx, v, meta, input.Display, input.MediaType, int64(input.ByteSize), input.SHA256)
		if err == nil {
			_, out, err = s.readUserUpload(ctx, v, meta, &input)
		}
	}
	if err = s.projectCallError(ctx, err); err != nil {
		return ac.UserUploadResult{}, err
	}
	return out, nil
}

func (s *Service) LookupUserUpload(ctx context.Context, v ac.Invocation, meta f.CommandMeta, input ac.UserUploadIntent) (ac.UserUploadResult, error) {
	meta, input = copyCommand(meta), copyUserUploadIntent(input)
	if err := validateUserUpload(v, meta, &input); err != nil {
		return ac.UserUploadResult{}, err
	}
	ctx, done, err := s.beginProjectCall(ctx, v, "read")
	defer done()
	if err != nil {
		return ac.UserUploadResult{}, err
	}
	_, out, err := s.readUserUpload(ctx, v, meta, &input)
	if err = s.projectCallError(ctx, err); err != nil {
		return ac.UserUploadResult{}, err
	}
	return out, nil
}

type userUploadBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func (b *userUploadBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

func (s *Service) WriteUserUpload(ctx context.Context, v ac.Invocation, meta f.CommandMeta, media string, length int64, body io.ReadCloser) (out ac.UserUploadResult, err error) {
	if nilPort(body) {
		return out, invalid()
	}
	// Admission itself may fail. Body ownership still ends before this method
	// returns, and the actual Close remains inside the original project call.
	owned := &userUploadBody{ReadCloser: body}
	defer owned.Close()
	meta = copyCommand(meta)
	normal, mediaErr := oc.NormalizeMediaType(media)
	if err = validateUserUpload(v, meta, nil); err != nil {
		return out, err
	}
	if mediaErr != nil || normal != media || length < 0 || length > oc.MaxObjectSize {
		return out, invalid()
	}
	ctx, done, err := s.beginProjectCall(ctx, v, "upload")
	defer done()
	// This defer runs before done, including failures before original uploadPayload.
	defer func() {
		if closeErr := owned.Close(); closeErr != nil && err == nil {
			err = unavailable(closeErr)
			out = ac.UserUploadResult{}
		}
	}()
	if err != nil {
		return out, err
	}
	u, observed, err := s.readUserUpload(ctx, v, meta, nil)
	if err == nil {
		switch observed.State {
		case ac.UserUploadNotObserved:
			err = failure(f.NotFound, nil)
		case ac.UserUploadPending:
			// A persisted Object attempt can still be uncertain or live. This
			// Human entry never resumes it from a mere pending observation.
			if u.object != (oc.ObjectID{}) || u.upload != (oc.UploadID{}) || u.attempt != (oc.AttemptID{}) {
				err = failure(f.ResourceBusy, nil)
			}
		case ac.UserUploadUploaded:
			// Original UploadPayload verifies identical bytes and LookupPut;
			// it emits no new physical upload for this known publication.
		default:
			err = failure(f.InvalidState, nil)
		}
	}
	if err == nil && (media != u.media || length != u.size) {
		err = failure(f.IdempotencyKeyReused, nil)
	}
	if err == nil {
		target := ac.UploadTarget{Reference: u.ref, Owner: artifactOwner(u.ref), Command: meta}
		_, err = s.uploadPayloadAttempt(ctx, v, target, owned, observed.State == ac.UserUploadPending)
		if err == nil {
			_, out, err = s.readUserUpload(ctx, v, meta, nil)
		}
	}
	if err = s.projectCallError(ctx, err); err != nil {
		return ac.UserUploadResult{}, err
	}
	return out, nil
}

func (s *Service) CompleteUserUpload(ctx context.Context, v ac.Invocation, meta f.CommandMeta, input ac.UserUploadIntent) (ac.Metadata, error) {
	meta, input = copyCommand(meta), copyUserUploadIntent(input)
	if err := validateUserUpload(v, meta, &input); err != nil {
		return ac.Metadata{}, err
	}
	ctx, done, err := s.beginProjectCall(ctx, v, "creation")
	defer done()
	if err != nil {
		return ac.Metadata{}, err
	}
	u, observed, err := s.readUserUpload(ctx, v, meta, &input)
	var out ac.Metadata
	if err == nil {
		switch observed.State {
		case ac.UserUploadCommitted:
			out = *observed.Artifact
		case ac.UserUploadUploaded:
			var receipt oc.UploadReceipt
			receipt, err = u.receiptValue()
			if err == nil {
				// Original consumption rechecks current authority, immutable
				// intent and the exact receipt under its complete lock set.
				out, err = s.createFromUpload(ctx, v, meta, input.Display, receipt)
			}
		case ac.UserUploadPending:
			err = failure(f.ResourceBusy, nil)
		default:
			err = failure(f.InvalidState, nil)
		}
	}
	if err = s.projectCallError(ctx, err); err != nil {
		return ac.Metadata{}, err
	}
	return out, nil
}

func (s *Service) CancelUserUpload(ctx context.Context, v ac.Invocation, meta f.CommandMeta, input ac.UserUploadIntent) (ac.UserUploadResult, error) {
	meta, input = copyCommand(meta), copyUserUploadIntent(input)
	if err := validateUserUpload(v, meta, &input); err != nil {
		return ac.UserUploadResult{}, err
	}
	ctx, done, err := s.beginProjectCall(ctx, v, "converge")
	defer done()
	if err != nil {
		return ac.UserUploadResult{}, err
	}
	u, out, err := s.readUserUpload(ctx, v, meta, &input)
	if err == nil && out.State != ac.UserUploadNotObserved {
		if out.State == ac.UserUploadCommitted {
			err = failure(f.InvalidState, nil)
		} else {
			var cleanup oc.CleanupState
			cleanup, err = s.cancelUpload(ctx, v, ac.UploadTarget{Reference: u.ref, Owner: artifactOwner(u.ref), Command: meta})
			if err == nil {
				switch cleanup {
				case oc.CleanupCompleted:
					out.State = ac.UserUploadCancelled
				case oc.CleanupPending:
					out.State = ac.UserUploadCancelRequested
				default:
					err = unavailable(nil)
				}
			}
		}
	}
	if err = s.projectCallError(ctx, err); err != nil {
		return ac.UserUploadResult{}, err
	}
	return out, nil
}
