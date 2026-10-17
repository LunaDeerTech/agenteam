package meeting

import (
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type FrozenAttachment struct {
	NodeIndex int                           `json:"node_index"`
	Source    ec.CapturedArtifactAttachment `json:"source"`
}

func meetingAttachmentIDs(nodes []c.InlineNode) ([]ac.ArtifactID, error) {
	ids := []ac.ArtifactID{}
	seen := map[ac.ArtifactID]bool{}
	fileNodes := 0
	for _, node := range nodes {
		if node.Type != "file" || node.Reference == nil {
			continue
		}
		fileNodes++
		if fileNodes > ec.MaxInputAttachments {
			return nil, fault(f.PayloadTooLarge)
		}
		id, err := f.ParseID[ac.ArtifactEntity](node.Reference.ID)
		if err != nil || node.Reference.Kind != "file" {
			return nil, fault(f.InvalidArgument)
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	if len(ids) > ec.MaxInputAttachments {
		return nil, fault(f.PayloadTooLarge)
	}
	return ids, nil
}

func (s *Service) captureMeetingAttachments(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actor i.Actor, project i.ProjectID, meeting c.MeetingID, nodes []c.InlineNode) ([]FrozenAttachment, error) {
	ids, err := meetingAttachmentIDs(nodes)
	if err != nil {
		return nil, err
	}
	out := []FrozenAttachment{}
	if len(ids) == 0 {
		return out, nil
	}
	if nilPort(s.deps.Attachments) {
		return nil, fault(f.DependencyUnbound)
	}
	captured, err := s.deps.Attachments.CaptureAttachmentsInTx(ctx, tx, actor, project, ids)
	if err != nil {
		return nil, portError(err)
	}
	if len(captured) != len(ids) {
		return nil, fault(f.DependencyUnavailable)
	}
	byID := map[string]ec.CapturedArtifactAttachment{}
	for n, source := range captured {
		if source.Validate() != nil || source.ProjectID != project || source.ArtifactID != ids[n] {
			return nil, fault(f.DependencyUnavailable)
		}
		byID[source.ArtifactID.String()] = source
	}
	// A Meeting captures all current messages, so the distinct attachment cap
	// applies to that complete input, not independently to each message.
	rows, err := x.Query(ctx, `SELECT m.attachment_data FROM agenteam_meeting.messages m JOIN agenteam_meeting.contributions c ON c.current_message_id=m.id WHERE c.meeting_id=$1`, meeting.String())
	if err != nil {
		return nil, portError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var previous []FrozenAttachment
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &previous) != nil {
			return nil, fault(f.InternalError)
		}
		for _, attachment := range previous {
			if attachment.Source.Validate() != nil || attachment.Source.ProjectID != project {
				return nil, fault(f.InternalError)
			}
			key := attachment.Source.ArtifactID.String()
			if current, exists := byID[key]; exists && current != attachment.Source {
				return nil, fault(f.ConfirmationStale)
			}
			byID[key] = attachment.Source
		}
	}
	if err = rows.Err(); err != nil {
		return nil, portError(err)
	}
	if len(byID) > ec.MaxInputAttachments {
		return nil, fault(f.PayloadTooLarge)
	}
	for n, node := range nodes {
		if node.Type == "file" && node.Reference != nil {
			out = append(out, FrozenAttachment{NodeIndex: n, Source: byID[node.Reference.ID]})
		}
	}
	return out, nil
}

func validateFrozenAttachments(project i.ProjectID, message FrozenMessage) error {
	index := 0
	for n, node := range message.Message.Content {
		if node.Type != "file" || node.Reference == nil {
			continue
		}
		if index >= len(message.Attachments) {
			return fault(f.InvalidArgument)
		}
		attachment := message.Attachments[index]
		if attachment.NodeIndex != n || attachment.Source.Validate() != nil || attachment.Source.ProjectID != project || attachment.Source.ArtifactID.String() != node.Reference.ID || node.Reference.Kind != "file" {
			return fault(f.InvalidArgument)
		}
		index++
	}
	if index != len(message.Attachments) {
		return fault(f.InvalidArgument)
	}
	return nil
}

func frozenAttachments(project i.ProjectID, messages []FrozenMessage) ([]ec.CapturedArtifactAttachment, error) {
	out := []ec.CapturedArtifactAttachment{}
	seen := map[ac.ArtifactID]ec.CapturedArtifactAttachment{}
	for _, message := range messages {
		if err := validateFrozenAttachments(project, message); err != nil {
			return nil, err
		}
		for _, attachment := range message.Attachments {
			source := attachment.Source
			if previous, ok := seen[source.ArtifactID]; ok {
				if previous != source {
					return nil, fault(f.ConfirmationStale)
				}
				continue
			}
			seen[source.ArtifactID] = source
			out = append(out, source)
		}
	}
	if len(out) > ec.MaxInputAttachments {
		return nil, fault(f.PayloadTooLarge)
	}
	return out, nil
}

func (v FrozenContext) Attachments() ([]ec.CapturedArtifactAttachment, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return frozenAttachments(v.ProjectID, v.Messages)
}
