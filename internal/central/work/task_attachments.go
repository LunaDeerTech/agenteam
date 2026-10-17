package work

import (
	"context"
	"encoding/json"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func taskAttachmentIDs(in taskInput) ([]ac.ArtifactID, bool) {
	if in.Create != nil {
		return slices.Clone(in.Create.Attachments), true
	}
	if in.Update != nil && in.Update.Attachments != nil {
		return slices.Clone(*in.Update.Attachments), true
	}
	return nil, false
}

func (s *TaskService) taskAttachmentLocks(ctx context.Context, actor i.Actor, in taskInput) ([]f.LockRequest, error) {
	ids, changed := taskAttachmentIDs(in)
	if !changed || len(ids) == 0 {
		return nil, nil
	}
	if nilPort(s.state().deps.Attachments) {
		return nil, fault(f.DependencyUnbound)
	}
	return s.state().deps.Attachments.AttachmentLocks(ctx, actor, in.Project, ids)
}

func (s *TaskService) captureTaskAttachments(ctx context.Context, tx f.Tx, actor i.Actor, in taskInput) ([]ec.CapturedArtifactAttachment, error) {
	ids, changed := taskAttachmentIDs(in)
	if !changed {
		return nil, nil
	}
	if len(ids) == 0 {
		return []ec.CapturedArtifactAttachment{}, nil
	}
	if nilPort(s.state().deps.Attachments) {
		return nil, fault(f.DependencyUnbound)
	}
	attachments, err := s.state().deps.Attachments.CaptureAttachmentsInTx(ctx, tx, actor, in.Project, ids)
	if err != nil {
		return nil, portError(err)
	}
	if err = validateTaskAttachmentBinding(in.Project, ids, attachments); err != nil {
		return nil, err
	}
	return slices.Clone(attachments), nil
}

func validateTaskAttachmentBinding(project i.ProjectID, ids []ac.ArtifactID, attachments []ec.CapturedArtifactAttachment) error {
	if c.ValidateTaskAttachments(ids) != nil || len(ids) != len(attachments) {
		return internal(nil)
	}
	for n, source := range attachments {
		if source.Validate() != nil || source.ProjectID != project || source.ArtifactID != ids[n] {
			return internal(nil)
		}
	}
	return nil
}

func decodeTaskAttachments(raw []byte, project i.ProjectID) ([]ec.CapturedArtifactAttachment, error) {
	var attachments []ec.CapturedArtifactAttachment
	if json.Unmarshal(raw, &attachments) != nil || attachments == nil || len(attachments) > ec.MaxInputAttachments {
		return nil, internal(nil)
	}
	ids := make([]ac.ArtifactID, len(attachments))
	for n, source := range attachments {
		ids[n] = source.ArtifactID
	}
	if err := validateTaskAttachmentBinding(project, ids, attachments); err != nil {
		return nil, err
	}
	return attachments, nil
}

func loadTaskAttachments(ctx context.Context, x postgres.SQLExecutor, task c.Task) ([]ec.CapturedArtifactAttachment, error) {
	var raw []byte
	if err := x.QueryRow(ctx, `SELECT attachment_data FROM agenteam_work.tasks WHERE project_id=$1 AND id=$2 AND version=$3`, task.ProjectID.String(), task.ID.String(), int64(task.Version)).Scan(&raw); err != nil {
		return nil, taskSQL(err)
	}
	attachments, err := decodeTaskAttachments(raw, task.ProjectID)
	if err != nil {
		return nil, err
	}
	if err = validateTaskAttachmentBinding(task.ProjectID, task.Attachments, attachments); err != nil {
		return nil, err
	}
	return attachments, nil
}
