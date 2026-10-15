package mcp

import (
	"bytes"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
)

func TestMCPContentMediaUsesResourceMIMEForImageReference(t *testing.T) {
	body := []byte{0, 1, 128, 255}
	text := "text remains text"
	for _, item := range []struct {
		name, media string
		text        *string
		image       bool
	}{
		{"image", "image/png", nil, true},
		{"normalized image", "IMAGE/PNG", nil, true},
		{"audio", "audio/wav", nil, false},
		{"binary", "application/octet-stream", nil, false},
		{"untyped binary", "", nil, false},
		{"text is not image bytes", "image/png", &text, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			resource := &protocol.ResourceContent{URI: "repo://owned/media", MIMEType: item.media, Text: item.text}
			want := []byte(text)
			if item.text == nil {
				resource.Blob, want = body, body
			}
			actual, _, uri, image, err := contentBytes(protocol.Content{Kind: "resource", Resource: resource})
			if err != nil || image != item.image || uri != resource.URI || !bytes.Equal(actual, want) {
				t.Fatal("Resource bytes or image/file projection changed", err)
			}
			actual[0] ^= 1
			if item.text == nil && body[0] != 0 {
				t.Fatal("canonical candidate aliased provider bytes")
			}
		})
	}
}
