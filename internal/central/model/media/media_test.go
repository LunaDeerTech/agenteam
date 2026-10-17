package media

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestChatMediaNativeAndExtractedContent(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=")
	var docx bytes.Buffer
	z := zip.NewWriter(&docx)
	w, _ := z.Create("word/document.xml")
	_, _ = w.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>private-docx-marker</w:t></w:r></w:p></w:body></w:document>`))
	_ = z.Close()
	for _, tc := range []struct {
		name, mime, want string
		raw              []byte
	}{
		{"x.png", "image/png", "image_url", png},
		{"x.pdf", "application/pdf", "file", []byte("%PDF-1.4\nowned")},
		{"x.md", "text/markdown", "text", []byte("private-markdown-marker")},
		{"x.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "text", docx.Bytes()},
	} {
		p, err := Prepare(context.Background(), tc.name, tc.mime, tc.raw)
		if err != nil {
			t.Fatal(tc.mime, err)
		}
		part, err := p.Content(context.Background())
		if err != nil || part.Type != tc.want {
			t.Fatal(tc.mime, "content shape")
		}
		b, err := json.Marshal(p)
		if err != nil || bytes.Contains(b, tc.raw) {
			t.Fatal("prepared media leaked through implicit JSON")
		}
		if tc.want == "text" && !strings.HasPrefix(part.Text, "Untrusted file attachment ") {
			t.Fatal("extracted text lacks source boundary")
		}
		p.Clear()
	}
}
func TestChatMediaBoundsAndMimeDoNotSilentlyDowngrade(t *testing.T) {
	for _, name := range []string{"../x.pdf", "x/y.pdf", " x.pdf", "x.pdf\n", ".", strings.Repeat("a", 256)} {
		if ValidFilename(name) {
			t.Fatal("invalid filename")
		}
	}
	for _, tc := range []struct {
		mime string
		raw  []byte
	}{{"image/png", []byte("not an image")}, {"application/pdf", []byte("not PDF")}, {"text/plain", []byte{0xff}}, {"text/plain", []byte("bad\x00text")}, {"application/zip", []byte("archive")}, {"text/plain", bytes.Repeat([]byte("x"), MaxTextFileBytes+1)}} {
		if p, err := Prepare(context.Background(), "owned", tc.mime, tc.raw); err == nil {
			p.Clear()
			t.Fatal("invalid media accepted", tc.mime)
		}
	}
	if _, err := PrepareInput(context.Background(), Input{}); err == nil {
		t.Fatal("empty explicit input accepted")
	}
	if _, err := PrepareInput(nil, Input{Text: "x"}); err == nil {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PrepareInput(ctx, Input{Text: "x"}); err == nil {
		t.Fatal("cancelled input accepted")
	}
	if _, err := PrepareInput(context.Background(), Input{Attachments: []Attachment{{Name: "x.txt", MediaType: "text/plain", DataBase64: "eA==\n"}}}); err == nil {
		t.Fatal("noncanonical base64 accepted")
	}
}
