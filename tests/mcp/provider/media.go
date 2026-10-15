package main

import (
	"context"
	"encoding/base64"
	"encoding/json"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerMedia extends the owned official SDK service with a real binary
// Resource. These fixed bytes are fixture content, never credential material.
// Reading it uses resources/read on the caller's already admitted Connection.
func registerMedia(server *sdk.Server) {
	server.AddResource(&sdk.Resource{URI: "repo://owned/binary", Name: "owned-binary", MIMEType: "application/octet-stream"}, func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: "repo://owned/binary", MIMEType: "application/octet-stream", Blob: []byte{0, 1, 2, 3, 128, 129, 254, 255}}}}, nil
	})
	// Small genuine media payloads exercise the official SDK's ImageContent and
	// AudioContent wire variants. They still require canonical Object storage.
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=")
	if err != nil {
		panic("invalid owned image fixture")
	}
	server.AddResource(&sdk.Resource{URI: "repo://owned/image", Name: "owned-image", MIMEType: "image/png"}, func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: "repo://owned/image", MIMEType: "image/png", Blob: image}}}, nil
	})
	audio := []byte{'R', 'I', 'F', 'F', 38, 0, 0, 0, 'W', 'A', 'V', 'E', 'f', 'm', 't', ' ', 16, 0, 0, 0, 1, 0, 1, 0, 0x40, 0x1f, 0, 0, 0x80, 0x3e, 0, 0, 2, 0, 16, 0, 'd', 'a', 't', 'a', 2, 0, 0, 0, 0, 0}
	for _, media := range []struct {
		name    string
		content sdk.Content
	}{{"owned_image", &sdk.ImageContent{Data: image, MIMEType: "image/png"}}, {"owned_audio", &sdk.AudioContent{Data: audio, MIMEType: "audio/wav"}}} {
		server.AddTool(&sdk.Tool{Name: media.name, Description: "Return owned canonical media fixture bytes", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`), Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{media.content}}, nil
		})
	}
}
