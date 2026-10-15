package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestCanonicalMessagesPreserveTextAndToolBytes(t *testing.T) {
	_, parts := successorPartsFixture(t)
	parts.Messages = append(parts.Messages,
		Message{Role: "assistant", Parts: []MessagePart{{Metadata: &ProviderMetadataPart{AdapterID: "test", Version: "1", OpaqueRef: "bound"}}}},
		Message{Role: "tool", Parts: []MessagePart{{ToolResult: &ToolResultPart{CallID: "empty", IsError: true}}}},
	)
	legacy, err := json.Marshal(parts.Messages)
	must(t, err)
	got, err := EncodeCanonicalMessages(parts.Messages)
	must(t, err)
	if !bytes.Equal(got, legacy) {
		t.Fatal("legacy text/tool bytes changed")
	}
	decoded, err := DecodeCanonicalMessages(got)
	must(t, err)
	roundtrip, err := json.Marshal(decoded)
	must(t, err)
	if !bytes.Equal(roundtrip, legacy) {
		t.Fatal("legacy text/tool roundtrip changed")
	}
	for _, message := range parts.Messages {
		legacy, err = json.Marshal(message)
		must(t, err)
		got, err = EncodeCanonicalMessage(message)
		must(t, err)
		if !bytes.Equal(got, legacy) {
			t.Fatal("single legacy message bytes changed")
		}
	}
}

func TestCanonicalMCPMessageRoundTripAndClosedShape(t *testing.T) {
	project := fresh[id.Project](t)
	file := fresh[struct{}](t).String()
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: project, FileID: file})
	must(t, err)
	media := &MediaPart{Ref: ref, MediaType: "image/png"}
	message := Message{Role: "tool", Parts: []MessagePart{{ToolResult: &ToolResultPart{CallID: "original", Parts: []ResultPart{{Text: &TextPart{Text: "caption"}}, {Image: media}, {File: media}}}}}}
	raw, err := EncodeCanonicalMessage(message)
	must(t, err)
	if !bytes.Contains(raw, []byte(file)) || bytes.Contains(raw, []byte("business_file_ref")) {
		t.Fatal("persistence codec lost source identity")
	}
	canonical, err := cursor.CanonicalJSON(raw)
	must(t, err)
	got, err := DecodeCanonicalMessage(canonical)
	must(t, err)
	for _, r := range got.Parts[0].ToolResult.Parts[1:] {
		m := r.Image
		if m == nil {
			m = r.File
		}
		if m.Ref.Details().Kind != oc.MCPFile || m.Ref.Details().ProjectID != project || m.Ref.Details().FileID != file || m.MediaType != media.MediaType {
			t.Fatal("original media identity changed")
		}
	}
	redacted, err := json.Marshal(message)
	must(t, err)
	if bytes.Contains(redacted, []byte(file)) {
		t.Fatal("ordinary message JSON leaked the opaque reference")
	}
	for name, badRaw := range map[string][]byte{
		"duplicate":       bytes.Replace(raw, []byte(`"kind":"mcp_file"`), []byte(`"kind":"mcp_file","kind":"mcp_file"`), 1),
		"alias":           bytes.Replace(raw, []byte(`"project_id"`), []byte(`"PROJECT_ID"`), 1),
		"other-source":    bytes.Replace(raw, []byte(`"mcp_file"`), []byte(`"artifact_file"`), 1),
		"bare-object":     bytes.Replace(raw, []byte(`"file_id"`), []byte(`"object_id"`), 1),
		"revision":        bytes.Replace(raw, []byte(`"kind":"mcp_file"`), []byte(`"kind":"mcp_file","revision":"1"`), 1),
		"missing-project": bytes.Replace(raw, []byte(`"project_id":"`+project.String()+`",`), nil, 1),
		"union-null":      bytes.Replace(raw, []byte(`"image":`), []byte(`"text":null,"image":`), 1),
		"trailing":        append(bytes.Clone(raw), []byte(`{}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCanonicalMessage(badRaw); err == nil {
				t.Fatal("invalid persisted message accepted")
			}
		})
	}
	message = Message{Role: "user", Parts: []MessagePart{{File: media}}}
	raw, err = EncodeCanonicalMessage(message)
	must(t, err)
	if _, err = DecodeCanonicalMessage(raw); err != nil {
		t.Fatal("direct media message did not roundtrip", err)
	}
	if _, err = DecodeCanonicalMessage([]byte(strings.Repeat(" ", MaxCanonicalMessagesBytes+1))); err == nil {
		t.Fatal("unbounded input accepted")
	}
}

func TestSuccessorSemanticMCPIdentityAndLegacyDigest(t *testing.T) {
	x, parts := successorPartsFixture(t)
	legacy := struct {
		SchemaVersion  any            `json:"schema_version"`
		Actor          any            `json:"actor"`
		Model          any            `json:"model"`
		Messages       []Message      `json:"messages"`
		Tools          []Tool         `json:"tools"`
		ToolChoice     ToolChoice     `json:"tool_choice"`
		ResponseFormat ResponseFormat `json:"response_format"`
		RetryClass     RetryClass     `json:"retry_class"`
	}{SuccessorInputSchemaVersion, successorActorWire{id.AgentRun, x.c.ProjectID, *x.c.AgentID, *x.c.ExecutionID}, successorModelWire(parts.Model), parts.Messages, parts.Tools, parts.ToolChoice, parts.ResponseFormat, parts.RetryClass}
	raw, err := json.Marshal(legacy)
	must(t, err)
	raw, err = cursor.CanonicalJSON(raw)
	must(t, err)
	before, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	if !bytes.Equal(raw, before.CanonicalBytes()) || successorSemanticDigest(raw) != before.Digest() {
		t.Fatal("legacy text canonical bytes or digest changed")
	}
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: x.c.ProjectID, FileID: fresh[struct{}](t).String()})
	must(t, err)
	parts.Messages[3].Parts[0].ToolResult.Parts = []ResultPart{{File: &MediaPart{Ref: ref, MediaType: "application/pdf"}}}
	media, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	decoded, err := DecodeSuccessorSemantic(context.Background(), media.CanonicalBytes())
	must(t, err)
	if decoded.Parts().Messages[3].Parts[0].ToolResult.Parts[0].File.Ref.Details().FileID != ref.Details().FileID {
		t.Fatal("successor lost MCP identity")
	}
	other, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: x.c.ProjectID, FileID: fresh[struct{}](t).String()})
	must(t, err)
	parts.Messages[3].Parts[0].ToolResult.Parts[0].File.Ref = other
	changed, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	if changed.Digest() == media.Digest() {
		t.Fatal("MCP identity omitted from original semantic digest")
	}
}
