package artifact

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Keep the existing prepare_upload digest byte-for-byte stable. Both the
// original BeginUpload and the Human recovery view use this one definition.
func uploadIntentDigest(v ac.Invocation, display ac.Display, media string, length int64, sha *f.Digest, version *f.Version) (f.Digest, error) {
	d := v.Details()
	return jsonDigest(struct {
		Actor, Project, Execution, Operation, Tool, ToolCall, Name, Description, Media string
		Size                                                                           int64
		SHA                                                                            *f.Digest
		Version                                                                        *f.Version
	}{stableActor(d.Actor), d.ProjectID.String(), d.ExecutionID, d.OperationID, d.ToolID, d.ToolCallID, display.Name, display.Description, media, length, sha, version})
}

func validateUserUpload(v ac.Invocation, meta f.CommandMeta, intent *ac.UserUploadIntent) error {
	if v.Validate() != nil || meta.Validate() != nil || intent != nil && intent.Validate() != nil {
		return invalid()
	}
	d := v.Details()
	if d.Actor.Details().Kind != i.Human || d.ExecutionID != "" || d.OperationID != "" || d.ToolID != "" || d.ToolCallID != "" {
		return failure(f.Forbidden, nil)
	}
	return nil
}

func copyUserUploadIntent(v ac.UserUploadIntent) ac.UserUploadIntent {
	if v.SHA256 != nil {
		d := *v.SHA256
		v.SHA256 = &d
	}
	return v
}

// The preliminary row supplies only the complete lock set. All returned facts
// come from the original current authority and a second read under both command
// locks plus the Artifact lock. No Object network or writer recovery runs here.
func (s *Service) readUserUpload(ctx context.Context, v ac.Invocation, meta f.CommandMeta, intent *ac.UserUploadIntent) (uploadIntent, ac.UserUploadResult, error) {
	var out ac.UserUploadResult
	var current uploadIntent
	if err := validateUserUpload(v, meta, intent); err != nil {
		return current, out, err
	}
	id, _ := commandIdentity(v.Details().ProjectID, meta.IdempotencyKey, "prepare_upload")
	createID, _ := commandIdentity(v.Details().ProjectID, meta.IdempotencyKey, "create")
	before, found, err := loadIntent(ctx, s.state().store, id)
	if err != nil {
		return current, out, err
	}
	var entity string
	if found {
		entity = before.ref.ArtifactID.String()
	}
	locks := append(createLocks(id, entity), createLocks(createID, "")...)
	cause, _ := f.NewCommandsCause(id, createID)
	r := s.state()
	result := s.within(ctx, invocationSubject(v), i.Read, cause, nil, locks, func(ctx context.Context, tx f.Tx, _ oc.LockedAccess) error {
		e, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		var exists bool
		current, exists, err = loadIntent(ctx, e, id)
		if err != nil {
			return err
		}
		if !exists {
			if found {
				return failure(f.ResourceBusy, nil)
			}
			out.State = ac.UserUploadNotObserved
			return nil
		}
		if !found || current.ref != before.ref {
			return failure(f.ResourceBusy, nil)
		}
		if current.originalActor != stableActor(v.Details().Actor) || current.ref.ProjectID != v.Details().ProjectID {
			return failure(f.Forbidden, nil)
		}
		if current.execution != "" || current.operation != "" || current.tool != "" || current.toolCall != "" || (current.expected == nil) != (meta.ExpectedVersion == nil) || current.expected != nil && *current.expected != *meta.ExpectedVersion {
			return failure(f.IdempotencyKeyReused, nil)
		}
		if intent != nil {
			digest, err := uploadIntentDigest(v, intent.Display, intent.MediaType, int64(intent.ByteSize), intent.SHA256, meta.ExpectedVersion)
			if err != nil {
				return err
			}
			if digest != current.digest {
				return failure(f.IdempotencyKeyReused, nil)
			}
		}
		switch current.state {
		case "pending":
			out.State = ac.UserUploadPending
		case "uploaded":
			if _, err := current.receiptValue(); err != nil || current.meta.Validate() != nil {
				return unavailable(err)
			}
			out.State = ac.UserUploadUploaded
		case "cancel_requested":
			out.State = ac.UserUploadCancelRequested
		case "cancelled":
			out.State = ac.UserUploadCancelled
		case "deleted":
			return f.NewFault(f.ResourceDeleted, f.Committed)
		case "consumed":
			receipt, err := current.receiptValue()
			if err != nil {
				return unavailable(err)
			}
			req, err := makeRequest(v, meta, current.display, "upload", "", 0, "", oc.BusinessFileRef{}, receipt)
			if err != nil {
				return err
			}
			command, exists, err := loadCommand(ctx, e, createID)
			if err != nil {
				return err
			}
			if !exists || command.state != "completed" || command.path != "upload" || command.kind != ac.UserUpload || command.ref != current.ref || command.cause != current.cause || command.object != current.object || command.upload != current.upload || command.sha != current.sha {
				return unavailable(nil)
			}
			if err = matchesCommand(command, req); err != nil {
				return err
			}
			if _, err = r.owners.AuthorizeOwnerInTx(ctx, tx, v.Details().Actor, artifactOwner(current.ref), i.Read); err != nil {
				return err
			}
			row, exists, err := loadArtifact(ctx, e, current.ref.ArtifactID.String())
			if err != nil {
				return err
			}
			if !exists {
				return f.NewFault(f.ResourceDeleted, f.Committed)
			}
			d := row.meta.Details()
			if row.cause != current.cause || d.Reference != current.ref || !sameUserUploadObject(d.Object, current.meta) || d.Name != current.display.Name || d.Description != current.display.Description || d.CreatedByUserID != v.Details().Actor.Details().UserID {
				return unavailable(nil)
			}
			m := row.meta
			out = ac.UserUploadResult{State: ac.UserUploadCommitted, Artifact: &m}
		default:
			return unavailable(nil)
		}
		return out.Validate()
	})
	if err = commitError(result); err != nil {
		return uploadIntent{}, ac.UserUploadResult{}, err
	}
	if out.Validate() != nil {
		return uploadIntent{}, ac.UserUploadResult{}, unavailable(nil)
	}
	return current, out, nil
}

func sameUserUploadObject(a, b oc.ObjectMeta) bool {
	return a.ID == b.ID && a.Scope.Equal(b.Scope) && a.MediaType == b.MediaType && a.ByteSize == b.ByteSize && a.SHA256 == b.SHA256 && a.State == b.State && a.Version == b.Version && a.CreatedAt.String() == b.CreatedAt.String()
}
