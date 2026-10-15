package artifact

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// ReadArtifactMetadata resolves a stable Artifact identity for a current Human
// Owner. It reads published metadata only; no Object payload or lease is opened.
func (s *Service) ReadArtifactMetadata(ctx context.Context, v ac.Invocation, artifact ac.ArtifactID) (ac.Metadata, error) {
	if ctx == nil || v.Validate() != nil || artifact.Validate() != nil {
		return ac.Metadata{}, invalid()
	}
	d := v.Details()
	if d.Actor.Details().Kind != i.Human || d.ExecutionID != "" || d.OperationID != "" || d.ToolID != "" || d.ToolCallID != "" {
		return ac.Metadata{}, failure(f.Forbidden, nil)
	}
	ctx, done, err := s.beginProjectCall(ctx, v, "read")
	defer done()
	if err != nil {
		return ac.Metadata{}, err
	}
	cause, err := txCause()
	if err != nil {
		return ac.Metadata{}, err
	}
	var out ac.Metadata
	result := s.within(ctx, invocationSubject(v), i.Read, cause, nil, []f.LockRequest{artifactLock(artifact.String())}, func(ctx context.Context, tx f.Tx, _ oc.LockedAccess) error {
		x, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, found, err := loadArtifact(ctx, x, artifact.String())
		if err != nil {
			return err
		}
		m := row.meta.Details()
		if !found || m.Reference.ProjectID != d.ProjectID || m.Reference.ArtifactID != artifact {
			return failure(f.NotFound, nil)
		}
		if _, err = s.state().owners.AuthorizeOwnerInTx(ctx, tx, d.Actor, artifactOwner(m.Reference), i.Read); err != nil {
			return err
		}
		// ArtifactRead records the authorized metadata read. Zero sent bytes
		// deliberately makes no assertion that Object content was accessed.
		fields, err := au.ArtifactMetadata(au.ArtifactRead, au.ArtifactMetadataFields{ArtifactID: artifact.String(), ObjectID: m.Object.ID.String(), MediaType: auditMedia(m.Object.MediaType), ByteSize: m.Object.ByteSize, SentBytes: 0, Phase: au.ReadPhase})
		if err != nil {
			return err
		}
		resource, err := au.NewResource(au.ArtifactResource, artifact.String())
		if err != nil {
			return err
		}
		key, err := jsonDigest([]string{"metadata", d.AttemptID.String(), stableActor(d.Actor), d.ProjectID.String(), artifact.String()})
		if err != nil {
			return err
		}
		if err = s.appendReadAudit(ctx, tx, v, au.ArtifactRead, resource, fields, key, row.meta); err != nil {
			return err
		}
		out = row.meta
		return nil
	})
	if err = s.projectCallError(ctx, commitError(result)); err != nil {
		return ac.Metadata{}, err
	}
	if out.Validate() != nil {
		return ac.Metadata{}, unavailable(nil)
	}
	return out, nil
}
