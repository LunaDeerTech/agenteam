package model

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	media "github.com/LunaDeerTech/agenteam/internal/central/model/media"
)

type managementMediaDigest struct {
	Text        string
	Attachments []managementAttachmentDigest
}
type managementAttachmentDigest struct {
	Name, MediaType string
	SHA256          f.Digest
}

func prepareManagementMedia(ctx context.Context, inputs []*media.Input) (*media.Input, []*media.Prepared, *managementMediaDigest, error) {
	if len(inputs) > 1 {
		return nil, nil, nil, fault(f.InvalidArgument)
	}
	if len(inputs) == 0 || inputs[0] == nil {
		return nil, nil, nil, nil
	}
	input := inputs[0]
	prepared, err := media.PrepareInput(ctx, *input)
	if err != nil {
		return nil, nil, nil, err
	}
	semantic := &managementMediaDigest{Text: input.Text, Attachments: make([]managementAttachmentDigest, 0, len(prepared))}
	// Strict base64 is already bounded and verified above. Hash the original bytes,
	// not a provider encoding, so order, filename and MIME remain part of intent.
	for index, a := range input.Attachments {
		semantic.Attachments = append(semantic.Attachments, managementAttachmentDigest{Name: a.Name, MediaType: a.MediaType, SHA256: prepared[index].Digest()})
	}
	return input, prepared, semantic, nil
}
func decodeManagementMedia(raw json.RawMessage) (*media.Input, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	// RawMessage bypasses the outer HTTP DTO's field-name checks. Decode exact
	// map keys here instead of encoding/json's case-insensitive struct matching.
	// The outer DecodeJSON still rejects duplicate keys at every nesting level.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["text"] == nil || fields["attachments"] == nil {
		return nil, fault(f.InvalidArgument)
	}
	var text *string
	var attachments []map[string]json.RawMessage
	if json.Unmarshal(fields["text"], &text) != nil || text == nil || json.Unmarshal(fields["attachments"], &attachments) != nil || attachments == nil {
		return nil, fault(f.InvalidArgument)
	}
	out := &media.Input{Text: *text, Attachments: make([]media.Attachment, 0, len(attachments))}
	for _, a := range attachments {
		if len(a) != 3 || a["name"] == nil || a["media_type"] == nil || a["data_base64"] == nil {
			return nil, fault(f.InvalidArgument)
		}
		var name, mediaType, data *string
		if json.Unmarshal(a["name"], &name) != nil || name == nil || json.Unmarshal(a["media_type"], &mediaType) != nil || mediaType == nil || json.Unmarshal(a["data_base64"], &data) != nil || data == nil {
			return nil, fault(f.InvalidArgument)
		}
		out.Attachments = append(out.Attachments, media.Attachment{Name: *name, MediaType: *mediaType, DataBase64: *data})
	}
	return out, nil
}
