package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"unicode/utf8"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// The envelope owner may impose a smaller limit; this keeps the existing
// successor semantic budget rather than shrinking already valid text inputs.
const MaxCanonicalMessagesBytes = MaxSuccessorSemanticBytes

// These codecs are explicit persistence projections, not authorization or a
// provider capability. Ordinary Message JSON keeps BusinessFileRef redacted.
// Text/tool bytes retain the original json.Marshal field order. The owning
// digest codec remains responsible for canonical key ordering of its envelope.
func EncodeCanonicalMessage(message Message) ([]byte, error) {
	wire, err := messageWire(message)
	if err != nil {
		return nil, err
	}
	return encodeMessageWire(wire)
}

func EncodeCanonicalMessages(messages []Message) ([]byte, error) {
	if len(messages) == 0 || len(messages) > 256 {
		return nil, bad()
	}
	wire := make([]canonicalMessageWire, len(messages))
	parts := 0
	for n, message := range messages {
		parts += len(message.Parts)
		if parts > 256 {
			return nil, bad()
		}
		var err error
		if wire[n], err = messageWire(message); err != nil {
			return nil, err
		}
	}
	return encodeMessageWire(wire)
}

func DecodeCanonicalMessage(raw []byte) (Message, error) {
	var wire canonicalMessageWire
	if err := decodeMessageWire(raw, &wire); err != nil {
		return Message{}, err
	}
	message, err := wire.message()
	if err != nil {
		return Message{}, err
	}
	encoded, err := EncodeCanonicalMessage(message)
	if err != nil || !sameMessageJSON(raw, encoded) {
		return Message{}, bad()
	}
	return message, nil
}

func DecodeCanonicalMessages(raw []byte) ([]Message, error) {
	var wire []canonicalMessageWire
	if err := decodeMessageWire(raw, &wire); err != nil || len(wire) == 0 || len(wire) > 256 {
		return nil, bad()
	}
	messages := make([]Message, len(wire))
	for n := range wire {
		var err error
		if messages[n], err = wire[n].message(); err != nil {
			return nil, err
		}
	}
	encoded, err := EncodeCanonicalMessages(messages)
	if err != nil || !sameMessageJSON(raw, encoded) {
		return nil, bad()
	}
	return messages, nil
}

type canonicalMessageWire struct {
	Role  string                     `json:"role"`
	Parts []canonicalMessagePartWire `json:"parts"`
}
type canonicalMessagePartWire struct {
	Text       *TextPart                `json:"text,omitempty"`
	Image      *canonicalMediaWire      `json:"image,omitempty"`
	File       *canonicalMediaWire      `json:"file,omitempty"`
	ToolCall   *ToolCallPart            `json:"tool_call,omitempty"`
	ToolResult *canonicalToolResultWire `json:"tool_result,omitempty"`
	Metadata   *ProviderMetadataPart    `json:"metadata,omitempty"`
}
type canonicalResultPartWire struct {
	Text  *TextPart           `json:"text,omitempty"`
	Image *canonicalMediaWire `json:"image,omitempty"`
	File  *canonicalMediaWire `json:"file,omitempty"`
}
type canonicalToolResultWire struct {
	CallID  string                    `json:"call_id"`
	Parts   []canonicalResultPartWire `json:"parts"`
	IsError bool                      `json:"is_error"`
}
type canonicalMediaWire struct {
	Ref       canonicalMCPFileWire `json:"ref"`
	MediaType string               `json:"media_type"`
}
type canonicalMCPFileWire struct {
	Kind       oc.SourceKind `json:"kind"`
	ProjectID  id.ProjectID  `json:"project_id"`
	FileID     string        `json:"file_id"`
	ArtifactID string        `json:"artifact_id,omitempty"`
}

func messageWire(message Message) (canonicalMessageWire, error) {
	if message.Validate() != nil {
		return canonicalMessageWire{}, bad()
	}
	wire := canonicalMessageWire{Role: message.Role, Parts: make([]canonicalMessagePartWire, len(message.Parts))}
	for n, part := range message.Parts {
		p := canonicalMessagePartWire{Text: part.Text, ToolCall: part.ToolCall, Metadata: part.Metadata}
		var err error
		if p.Image, err = mediaWire(part.Image); err != nil {
			return canonicalMessageWire{}, err
		}
		if p.File, err = mediaWire(part.File); err != nil {
			return canonicalMessageWire{}, err
		}
		if part.ToolResult != nil {
			r := part.ToolResult
			p.ToolResult = &canonicalToolResultWire{CallID: r.CallID, IsError: r.IsError}
			if r.Parts != nil {
				p.ToolResult.Parts = make([]canonicalResultPartWire, len(r.Parts))
			}
			for i, result := range r.Parts {
				q := canonicalResultPartWire{Text: result.Text}
				if q.Image, err = mediaWire(result.Image); err != nil {
					return canonicalMessageWire{}, err
				}
				if q.File, err = mediaWire(result.File); err != nil {
					return canonicalMessageWire{}, err
				}
				p.ToolResult.Parts[i] = q
			}
		}
		wire.Parts[n] = p
	}
	return wire, nil
}

func mediaWire(media *MediaPart) (*canonicalMediaWire, error) {
	if media == nil {
		return nil, nil
	}
	if media.Validate() != nil {
		return nil, bad()
	}
	d := media.Ref.Details()
	if d.Kind != oc.MCPFile && d.Kind != oc.ArtifactFile {
		return nil, bad()
	}
	return &canonicalMediaWire{canonicalMCPFileWire{Kind: d.Kind, ProjectID: d.ProjectID, FileID: d.FileID, ArtifactID: d.ArtifactID}, media.MediaType}, nil
}

func (wire canonicalMessageWire) message() (Message, error) {
	m := Message{Role: wire.Role, Parts: make([]MessagePart, len(wire.Parts))}
	for n, p := range wire.Parts {
		part := MessagePart{Text: p.Text, ToolCall: p.ToolCall, Metadata: p.Metadata}
		var err error
		if part.Image, err = p.Image.media(); err != nil {
			return Message{}, err
		}
		if part.File, err = p.File.media(); err != nil {
			return Message{}, err
		}
		if p.ToolResult != nil {
			r := p.ToolResult
			part.ToolResult = &ToolResultPart{CallID: r.CallID, IsError: r.IsError}
			if r.Parts != nil {
				part.ToolResult.Parts = make([]ResultPart, len(r.Parts))
			}
			for i, q := range r.Parts {
				result := ResultPart{Text: q.Text}
				if result.Image, err = q.Image.media(); err != nil {
					return Message{}, err
				}
				if result.File, err = q.File.media(); err != nil {
					return Message{}, err
				}
				part.ToolResult.Parts[i] = result
			}
		}
		m.Parts[n] = part
	}
	if m.Validate() != nil {
		return Message{}, bad()
	}
	return m, nil
}
func (wire *canonicalMediaWire) media() (*MediaPart, error) {
	if wire == nil {
		return nil, nil
	}
	if wire.Ref.Kind != oc.MCPFile && wire.Ref.Kind != oc.ArtifactFile {
		return nil, bad()
	}
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: wire.Ref.Kind, ProjectID: wire.Ref.ProjectID, FileID: wire.Ref.FileID, ArtifactID: wire.Ref.ArtifactID})
	if err != nil {
		return nil, bad()
	}
	return &MediaPart{Ref: ref, MediaType: wire.MediaType}, nil
}

func encodeMessageWire(wire any) ([]byte, error) {
	remaining := MaxCanonicalMessagesBytes
	if err := successorSemanticJSONSize(context.Background(), reflect.ValueOf(wire), &remaining); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(wire)
	if err != nil || len(raw) > MaxCanonicalMessagesBytes {
		return nil, bad()
	}
	return raw, nil
}
func decodeMessageWire(raw []byte, wire any) error {
	if len(raw) == 0 || len(raw) > MaxCanonicalMessagesBytes || !utf8.Valid(raw) {
		return bad()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	first, err := d.Token()
	if err != nil || jsonBody(d, first, 1) != nil {
		return bad()
	}
	if _, err = d.Token(); err != io.EOF {
		return bad()
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(wire) != nil {
		return bad()
	}
	return nil
}

// JSONB may reorder keys. Comparing exact-key typed projections after decoding
// also rejects case aliases, omitted required fields and explicit union nulls.
// UseNumber preserves original integer and decimal argument values.
func sameMessageJSON(a, b []byte) bool {
	var left, right any
	decode := func(raw []byte, out *any) bool {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		return d.Decode(out) == nil
	}
	return decode(a, &left) && decode(b, &right) && reflect.DeepEqual(left, right)
}
