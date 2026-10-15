package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type pages struct {
	seen                map[string]bool
	count, items, bytes int
}

func (p *pages) add(cursor string, v any, n int) error {
	p.count++
	p.items += n
	raw, err := json.Marshal(v)
	if err != nil {
		return failure(InvalidResponse, false)
	}
	p.bytes += len(raw)
	if p.count > MaxPages || p.items > MaxCatalogItems || p.bytes > MaxCatalogBytes {
		return failure(TooLarge, false)
	}
	if cursor != "" {
		if !textValid(cursor, 2048) {
			return failure(InvalidResponse, false)
		}
		if p.seen == nil {
			p.seen = map[string]bool{}
		}
		if p.seen[cursor] {
			return failure(InvalidResponse, false)
		}
		p.seen[cursor] = true
	}
	return nil
}

func (s *Session) ListTools(ctx context.Context) ([]Tool, error) {
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	defer s.active.Done()
	if !s.capabilities.Tools {
		return []Tool{}, nil
	}
	var out []Tool
	var cursor string
	var bounds pages
	seen := map[string]bool{}
	for {
		result, err := s.sdk.ListTools(ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, classify(err, false)
		}
		if result == nil {
			return nil, failure(InvalidResponse, false)
		}
		if err = s.safe(result); err != nil {
			return nil, err
		}
		if err = bounds.add(result.NextCursor, result, len(result.Tools)); err != nil {
			return nil, err
		}
		for _, v := range result.Tools {
			if v == nil || !ValidRemoteName(v.Name) || seen[v.Name] {
				return nil, failure(InvalidResponse, false)
			}
			seen[v.Name] = true
			input, err := json.Marshal(v.InputSchema)
			if err != nil {
				return nil, failure(InvalidResponse, false)
			}
			var output json.RawMessage
			if v.OutputSchema != nil {
				output, err = json.Marshal(v.OutputSchema)
				if err != nil {
					return nil, failure(InvalidResponse, false)
				}
			}
			t := Tool{Name: v.Name, Description: v.Description, InputSchema: input, OutputSchema: output}
			if v.Annotations != nil {
				t.ReadOnly = v.Annotations.ReadOnlyHint
				t.Idempotent = v.Annotations.IdempotentHint
				t.Destructive = v.Annotations.DestructiveHint
			}
			out = append(out, t)
		}
		if result.NextCursor == "" {
			return out, nil
		}
		cursor = result.NextCursor
	}
}

func (s *Session) ListResources(ctx context.Context) ([]Resource, error) {
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	defer s.active.Done()
	if !s.capabilities.Resources {
		return []Resource{}, nil
	}
	var out []Resource
	var cursor string
	var bounds pages
	seen := map[string]bool{}
	for {
		result, err := s.sdk.ListResources(ctx, &sdk.ListResourcesParams{Cursor: cursor})
		if err != nil {
			return nil, classify(err, false)
		}
		if result == nil {
			return nil, failure(InvalidResponse, false)
		}
		if err = s.safe(result); err != nil {
			return nil, err
		}
		if err = bounds.add(result.NextCursor, result, len(result.Resources)); err != nil {
			return nil, err
		}
		for _, v := range result.Resources {
			if v == nil || !ValidResourceURI(v.URI) || seen[v.URI] || !textValid(v.Name, 256) || !textValid(v.Title, 512) || !textValid(v.Description, 8192) || !textValid(v.MIMEType, 256) || v.Size < 0 {
				return nil, failure(InvalidResponse, false)
			}
			seen[v.URI] = true
			out = append(out, Resource{v.URI, v.Name, v.Title, v.Description, v.MIMEType, &v.Size})
		}
		if result.NextCursor == "" {
			return out, nil
		}
		cursor = result.NextCursor
	}
}
func (s *Session) ListResourceTemplates(ctx context.Context) ([]Template, error) {
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	defer s.active.Done()
	if !s.capabilities.Resources {
		return []Template{}, nil
	}
	var out []Template
	var cursor string
	var bounds pages
	seen := map[string]bool{}
	for {
		result, err := s.sdk.ListResourceTemplates(ctx, &sdk.ListResourceTemplatesParams{Cursor: cursor})
		if err != nil {
			return nil, classify(err, false)
		}
		if result == nil {
			return nil, failure(InvalidResponse, false)
		}
		if err = s.safe(result); err != nil {
			return nil, err
		}
		if err = bounds.add(result.NextCursor, result, len(result.ResourceTemplates)); err != nil {
			return nil, err
		}
		for _, v := range result.ResourceTemplates {
			if v == nil || !ValidResourceURI(v.URITemplate) || seen[v.URITemplate] || !textValid(v.Name, 256) || !textValid(v.Title, 512) || !textValid(v.Description, 8192) || !textValid(v.MIMEType, 256) {
				return nil, failure(InvalidResponse, false)
			}
			seen[v.URITemplate] = true
			out = append(out, Template{v.URITemplate, v.Name, v.Title, v.Description, v.MIMEType})
		}
		if result.NextCursor == "" {
			return out, nil
		}
		cursor = result.NextCursor
	}
}

func (s *Session) CallTool(ctx context.Context, name string, arguments json.RawMessage) (ToolResult, error) {
	if err := s.check(ctx); err != nil {
		return ToolResult{}, err
	}
	defer s.active.Done()
	if !s.capabilities.Tools {
		return ToolResult{}, failure(CapabilityUnsupported, false)
	}
	if !ValidRemoteName(name) || len(arguments) > 1<<20 || !json.Valid(arguments) || !utf8.Valid(arguments) || !bytes.HasPrefix(bytes.TrimSpace(arguments), []byte("{")) {
		return ToolResult{}, failure(InvalidRequest, false)
	}
	v, err := s.sdk.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return ToolResult{}, classify(err, true)
	}
	if v == nil {
		return ToolResult{}, failure(InvalidResponse, true)
	}
	if len(v.InputRequests) > 0 || v.NeedsInput() {
		return ToolResult{}, failure(CapabilityUnsupported, true)
	}
	if err = s.safe(v); err != nil {
		e := err.(*Error)
		e.MayHaveExecuted = true
		return ToolResult{}, e
	}
	if len(v.Content) > 256 {
		return ToolResult{}, failure(TooLarge, true)
	}
	out := ToolResult{IsError: v.IsError}
	if v.StructuredContent != nil {
		out.StructuredContent, err = json.Marshal(v.StructuredContent)
		if err != nil {
			return ToolResult{}, failure(InvalidResponse, true)
		}
	}
	for _, raw := range v.Content {
		c, err := mapContent(raw)
		if err != nil {
			return ToolResult{}, failure(InvalidResponse, true)
		}
		if s.sensitive(c.Data) || c.Resource != nil && s.sensitive(c.Resource.Blob) {
			clear(c.Data)
			if c.Resource != nil {
				clear(c.Resource.Blob)
			}
			return ToolResult{}, failure(SensitiveResponse, true)
		}
		out.Content = append(out.Content, c)
	}
	return out, nil
}

func mapResource(v *sdk.ResourceContents) (ResourceContent, error) {
	if v == nil || !ValidResourceURI(v.URI) || !textValid(v.MIMEType, 256) || len(v.Blob) > MaxContentBytes || len(v.Text) > MaxContentBytes || !utf8.ValidString(v.Text) || v.Blob != nil && v.Text != "" {
		return ResourceContent{}, failure(InvalidResponse, false)
	}
	out := ResourceContent{URI: v.URI, MIMEType: v.MIMEType}
	if v.Blob != nil {
		out.Blob = bytes.Clone(v.Blob)
	} else {
		text := v.Text
		out.Text = &text
	}
	return out, nil
}
func mapContent(v sdk.Content) (Content, error) {
	switch c := v.(type) {
	case *sdk.TextContent:
		if c == nil || len(c.Text) > MaxContentBytes || !utf8.ValidString(c.Text) {
			break
		}
		return Content{Kind: "text", Text: c.Text}, nil
	case *sdk.ImageContent:
		if c == nil || !strings.HasPrefix(c.MIMEType, "image/") {
			break
		}
		return Content{Kind: "image", MIMEType: c.MIMEType, Data: bytes.Clone(c.Data)}, nil
	case *sdk.AudioContent:
		if c == nil {
			break
		}
		return Content{Kind: "audio", MIMEType: c.MIMEType, Data: bytes.Clone(c.Data)}, nil
	case *sdk.ResourceLink:
		if c == nil || !ValidResourceURI(c.URI) {
			break
		}
		return Content{Kind: "resource_link", URI: c.URI, Name: c.Name, MIMEType: c.MIMEType}, nil
	case *sdk.EmbeddedResource:
		if c == nil {
			break
		}
		r, err := mapResource(c.Resource)
		if err != nil {
			return Content{}, err
		}
		return Content{Kind: "resource", Resource: &r}, nil
	}
	return Content{}, failure(InvalidResponse, false)
}
func (s *Session) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	if err := s.check(ctx); err != nil {
		return nil, err
	}
	defer s.active.Done()
	if !s.capabilities.Resources {
		return nil, failure(CapabilityUnsupported, false)
	}
	if !ValidResourceURI(uri) {
		return nil, failure(InvalidRequest, false)
	}
	v, err := s.sdk.ReadResource(ctx, &sdk.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, classify(err, false)
	}
	if v == nil || len(v.Contents) > 256 {
		return nil, failure(InvalidResponse, false)
	}
	if v.NeedsInput() || len(v.InputRequests) > 0 {
		return nil, failure(CapabilityUnsupported, false)
	}
	if err = s.safe(v); err != nil {
		return nil, err
	}
	out := make([]ResourceContent, 0, len(v.Contents))
	for _, c := range v.Contents {
		r, err := mapResource(c)
		if err != nil {
			return nil, err
		}
		if s.sensitive(r.Blob) {
			clear(r.Blob)
			return nil, failure(SensitiveResponse, false)
		}
		out = append(out, r)
	}
	return out, nil
}
