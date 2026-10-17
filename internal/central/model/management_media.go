package model

import (
	"bytes"
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	media "github.com/LunaDeerTech/agenteam/internal/central/model/media"
	"io"
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
	if bytes.Equal(raw, []byte("null")) {
		return nil, fault(f.InvalidArgument)
	}
	var dto struct {
		Text        *string `json:"text"`
		Attachments *[]struct {
			Name      *string `json:"name"`
			MediaType *string `json:"media_type"`
			Data      *string `json:"data_base64"`
		} `json:"attachments"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&dto) != nil || decoder.Decode(new(any)) != io.EOF || dto.Text == nil || dto.Attachments == nil {
		return nil, fault(f.InvalidArgument)
	}
	out := &media.Input{Text: *dto.Text, Attachments: make([]media.Attachment, 0, len(*dto.Attachments))}
	for _, a := range *dto.Attachments {
		if a.Name == nil || a.MediaType == nil || a.Data == nil {
			return nil, fault(f.InvalidArgument)
		}
		out.Attachments = append(out.Attachments, media.Attachment{Name: *a.Name, MediaType: *a.MediaType, DataBase64: *a.Data})
	}
	return out, nil
}
