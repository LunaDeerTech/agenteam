package artifact

import (
	"context"
	"io"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// These outer calls own every defer in the original implementation, including
// body/reader Close, DiscardPrepared and the last Audit/Store return. Old New
// has no owner and runs the same original implementations without registration.
func (s *Service) CreateFromContent(ctx context.Context, v ac.Invocation, meta f.CommandMeta, display ac.Display, media, content string) (ac.Metadata, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "creation")
	defer done()
	if err != nil {
		return ac.Metadata{}, err
	}
	out, err := s.createFromContent(ctx, v, meta, display, media, content)
	return out, s.projectCallError(ctx, err)
}

// LookupCreation observes an inline Human command through a fresh owned read.
// It never resumes creation or cancellation, and never opens Object payloads.
func (s *Service) LookupCreation(ctx context.Context, v ac.Invocation, meta f.CommandMeta, display ac.Display, media, content string) (ac.CreationLookupResult, error) {
	if err := validateCreationLookupInvocation(v); err != nil {
		return ac.CreationLookupResult{}, err
	}
	ctx, done, err := s.beginProjectCall(ctx, v, "read")
	defer done()
	if err != nil {
		return ac.CreationLookupResult{}, err
	}
	out, err := s.lookupCreation(ctx, v, meta, display, media, content)
	if err = s.projectCallError(ctx, err); err != nil {
		return ac.CreationLookupResult{}, err
	}
	return out, nil
}
func (s *Service) CreateFromSource(ctx context.Context, v ac.Invocation, meta f.CommandMeta, display ac.Display, ref oc.BusinessFileRef) (ac.Metadata, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "creation")
	defer done()
	if err != nil {
		return ac.Metadata{}, err
	}
	out, err := s.createFromSource(ctx, v, meta, display, ref)
	return out, s.projectCallError(ctx, err)
}
func (s *Service) BeginUpload(ctx context.Context, v ac.Invocation, command f.CommandMeta, display ac.Display, media string, length int64, expectedSHA *f.Digest) (ac.UploadTarget, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "upload")
	defer done()
	if err != nil {
		return ac.UploadTarget{}, err
	}
	out, err := s.beginUpload(ctx, v, command, display, media, length, expectedSHA)
	return out, s.projectCallError(ctx, err)
}
func (s *Service) UploadPayload(ctx context.Context, v ac.Invocation, target ac.UploadTarget, body io.ReadCloser) (oc.PutResult, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "upload")
	defer done()
	if err != nil {
		if !nilPort(body) {
			_ = body.Close()
		}
		return oc.PutResult{}, err
	}
	out, err := s.uploadPayload(ctx, v, target, body)
	return out, s.projectCallError(ctx, err)
}
func (s *Service) CreateFromUpload(ctx context.Context, v ac.Invocation, command f.CommandMeta, display ac.Display, receipt oc.UploadReceipt) (ac.Metadata, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "creation")
	defer done()
	if err != nil {
		return ac.Metadata{}, err
	}
	out, err := s.createFromUpload(ctx, v, command, display, receipt)
	return out, s.projectCallError(ctx, err)
}
func (s *Service) ListArtifacts(ctx context.Context, v ac.Invocation, filter ac.ListFilter, page f.PageRequest) (Page, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "read")
	defer done()
	if err != nil {
		return Page{}, err
	}
	out, err := s.listArtifacts(ctx, v, filter, page)
	return out, s.projectCallError(ctx, err)
}
func (s *Service) ReadArtifact(ctx context.Context, v ac.Invocation, ref ac.ArtifactRef, preview ac.PreviewRequest) (ac.ReadResult, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "read")
	defer done()
	if err != nil {
		return ac.ReadResult{}, err
	}
	out, err := s.readArtifact(ctx, v, ref, preview)
	return out, s.projectCallError(ctx, err)
}
func (s *Service) CancelUpload(ctx context.Context, v ac.Invocation, target ac.UploadTarget) (oc.CleanupState, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "converge")
	defer done()
	if err != nil {
		return oc.CleanupPending, err
	}
	out, err := s.cancelUpload(ctx, v, target)
	return out, s.projectCallError(ctx, err)
}
func (s *Service) CancelCreation(ctx context.Context, v ac.Invocation, key f.IdempotencyKey) (oc.CleanupState, error) {
	ctx, done, err := s.beginProjectCall(ctx, v, "converge")
	defer done()
	if err != nil {
		return oc.CleanupPending, err
	}
	out, err := s.cancelCreation(ctx, v, key)
	return out, s.projectCallError(ctx, err)
}
