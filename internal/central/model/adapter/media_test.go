package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/media"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func preparedMediaFixture(t *testing.T, project id.ProjectID, name, mime string, raw []byte) PreparedMedia {
	t.Helper()
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: project, FileID: fresh[struct{}](t).String()})
	if err != nil {
		t.Fatal(err)
	}
	p, err := media.Prepare(context.Background(), name, mime, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Clear)
	return PreparedMedia{Part: mc.MediaPart{Ref: ref, MediaType: mime}, Value: p}
}

func mediaPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func mediaPDF(text string) []byte {
	stream := "BT /F1 12 Tf 20 100 Td (" + text + ") Tj ET\n"
	objects := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Count 1/Kids[3 0 R]>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>",
		fmt.Sprintf("<</Length %d>>\nstream\n%sendstream", len(stream), stream),
		"<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

type mediaWireMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
}

func mediaRequestMessages(t *testing.T, r Request, o CallOptions) []mediaWireMessage {
	t.Helper()
	req, _, err := prepare(r, o)
	if err != nil {
		t.Fatal(err)
	}
	defer req.Body.Close()
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Messages []mediaWireMessage }
	if json.Unmarshal(raw, &body) != nil {
		t.Fatal("native request is not JSON")
	}
	return body.Messages
}

func TestChatMediaPreparedEncoding(t *testing.T) {
	for _, profile := range []struct {
		name  string
		input func(*testing.T) (Request, CallOptions)
	}{{"text", unitInput}, {"structured", structuredInput}, {"tools", jsonToolsInput}} {
		t.Run(profile.name, func(t *testing.T) {
			r, o := profile.input(t)
			r.Snapshot.Capabilities.InputModalities = []string{"text", "image", "file"}
			img := preparedMediaFixture(t, o.ProjectID, "pixel.png", "image/png", mediaPNG(t))
			pdf := preparedMediaFixture(t, o.ProjectID, "sample.pdf", "application/pdf", mediaPDF("owned"))
			txt := preparedMediaFixture(t, o.ProjectID, "readme.txt", "text/plain", []byte("attachment-text-canary"))
			r.Media = []PreparedMedia{img, pdf, txt}
			r.Messages = []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "explain"}}, {Image: &img.Part}, {Text: &mc.TextPart{}}, {File: &pdf.Part}, {File: &txt.Part}}}}
			messages := mediaRequestMessages(t, r, o)
			var parts []media.ContentPart
			if len(messages) != 1 || json.Unmarshal(messages[0].Content, &parts) != nil || len(parts) != 4 || parts[0].Text != "explain" || parts[1].ImageURL == nil || !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") || parts[2].File == nil || parts[2].File.Filename != "sample.pdf" || !strings.HasPrefix(parts[2].File.FileData, "data:application/pdf;base64,") || !strings.Contains(parts[3].Text, "attachment-text-canary") {
				t.Fatal("native media content shape or order changed")
			}
			if r.Messages[0].Parts[1].Image.Ref.Details().FileID != img.Part.Ref.Details().FileID || r.Messages[0].Parts[1].Text != nil {
				t.Fatal("encoding changed the canonical reference")
			}
		})
	}
}

func TestChatMediaSupportedImageContent(t *testing.T) {
	var jpg, gifBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if jpeg.Encode(&jpg, img, nil) != nil || gif.Encode(&gifBytes, img, nil) != nil {
		t.Fatal("owned image fixture")
	}
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mime string
		raw  []byte
	}{{"image/png", mediaPNG(t)}, {"image/jpeg", jpg.Bytes()}, {"image/gif", gifBytes.Bytes()}, {"image/webp", webp}} {
		t.Run(tc.mime, func(t *testing.T) {
			p := preparedMediaFixture(t, fresh[id.Project](t), "owned", tc.mime, tc.raw)
			content, err := EncodeUserContent(context.Background(), "image", []*media.Prepared{p.Value}, mc.Capabilities{InputModalities: []string{"text", "image"}})
			if err != nil {
				t.Fatal(err)
			}
			parts := content.([]media.ContentPart)
			if parts[1].ImageURL.URL != "data:"+tc.mime+";base64,"+base64.StdEncoding.EncodeToString(tc.raw) {
				t.Fatal("native image changed bytes or MIME")
			}
		})
	}
}

func TestChatMediaToolGroupOrderAndRepeatedReference(t *testing.T) {
	r, o := jsonToolsInput(t)
	r.Snapshot.Capabilities.InputModalities = []string{"text", "image"}
	img := preparedMediaFixture(t, o.ProjectID, "pixel.png", "image/png", mediaPNG(t))
	r.Media = []PreparedMedia{img}
	r.Messages = append(r.Messages,
		mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "first", Name: "read_file", Arguments: json.RawMessage(`{}`)}}, {ToolCall: &mc.ToolCallPart{ID: "second", Name: "read_file", Arguments: json.RawMessage(`{}`)}}}},
		mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "first", Parts: []mc.ResultPart{{Image: &img.Part}}}}}},
		mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "second", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "second result"}}, {Image: &img.Part}}}}}},
		mc.Message{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "continue"}}}})
	messages := mediaRequestMessages(t, r, o)
	if len(messages) != 6 || messages[2].Role != "tool" || messages[2].ToolCallID != "first" || messages[3].Role != "tool" || messages[3].ToolCallID != "second" || messages[4].Role != "user" || messages[5].Role != "user" || string(messages[5].Content) != `"continue"` {
		t.Fatal("attachment interrupted the pending tool group")
	}
	var attachments []media.ContentPart
	if json.Unmarshal(messages[4].Content, &attachments) != nil || len(attachments) != 4 || !strings.Contains(attachments[0].Text, "untrusted tool attachment (tool_call_id: first)") || !strings.Contains(attachments[2].Text, "tool_call_id: second") || attachments[1].ImageURL.URL != attachments[3].ImageURL.URL {
		t.Fatal("tool attachment provenance or duplicate-reference reuse lost")
	}
}

func TestChatMediaRejectsBeforeAdmission(t *testing.T) {
	for _, tc := range []string{"missing", "foreign-ref", "mime", "capability", "category", "assistant", "system", "extra", "duplicate", "cleared"} {
		t.Run(tc, func(t *testing.T) {
			r, o := unitInput(t)
			r.Snapshot.Capabilities.InputModalities = []string{"text", "image"}
			img := preparedMediaFixture(t, o.ProjectID, "pixel.png", "image/png", mediaPNG(t))
			r.Media = []PreparedMedia{img}
			r.Messages[0].Parts = []mc.MessagePart{{Image: &img.Part}}
			switch tc {
			case "missing":
				r.Media = nil
			case "foreign-ref":
				x := preparedMediaFixture(t, o.ProjectID, "other.png", "image/png", mediaPNG(t))
				r.Media[0].Part = x.Part
			case "mime":
				r.Media[0].Part.MediaType = "image/jpeg"
			case "capability":
				r.Snapshot.Capabilities.InputModalities = []string{"text"}
			case "category":
				r.Messages[0].Parts = []mc.MessagePart{{File: &img.Part}}
				r.Snapshot.Capabilities.InputModalities = []string{"text", "file"}
			case "assistant", "system":
				r.Messages[0].Role = tc
			case "extra":
				r.Messages[0].Parts = []mc.MessagePart{{Text: &mc.TextPart{Text: "plain"}}}
			case "duplicate":
				r.Media = append(r.Media, img)
			case "cleared":
				img.Value.Clear()
			}
			budget := NewBudget()
			budget.StopAdmission()
			x, err := (&OpenAIChat{budget: budget}).Start(context.Background(), r, o)
			if x != nil || err == nil || !budget.Joined() {
				t.Fatal("invalid media admitted an exchange")
			}
			if tc == "capability" || tc == "category" || tc == "assistant" || tc == "system" {
				var modelErr *mc.ModelError
				if !errors.As(err, &modelErr) || modelErr.Category != "unsupported_feature" {
					t.Fatal("unsupported media reached admission", err)
				}
			} else {
				requireFault(t, err, f.InvalidArgument)
			}
		})
	}
}

func TestChatMediaBudgetAndExtractedFileGate(t *testing.T) {
	r, o := unitInput(t)
	r.Snapshot.Capabilities.InputModalities = []string{"text", "file"}
	pdf := preparedMediaFixture(t, o.ProjectID, "large.pdf", "application/pdf", mediaPDF(strings.Repeat("x", 3<<20)))
	r.Media = []PreparedMedia{pdf}
	r.Messages = nil
	for range 5 {
		r.Messages = append(r.Messages, mc.Message{Role: "user", Parts: []mc.MessagePart{{File: &pdf.Part}}})
	}
	if req, _, err := prepare(r, o); err == nil || req != nil {
		t.Fatal("repeated media exceeded the complete request budget")
	}
	txt := preparedMediaFixture(t, o.ProjectID, "readme.txt", "text/plain", []byte("private text"))
	if _, err := EncodeUserContent(context.Background(), "read", []*media.Prepared{txt.Value}, mc.Capabilities{InputModalities: []string{"text"}}); err == nil {
		t.Fatal("text extraction bypassed the file capability")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EncodeUserContent(ctx, "read", nil, mc.Capabilities{InputModalities: []string{"text"}}); err == nil {
		t.Fatal("cancelled encoding succeeded")
	}
}
