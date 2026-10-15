package mcp

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
)

// resourceLinkSession is implemented by the already admitted official SDK
// session. It has no endpoint or HTTP-download entry point: each link remains
// inside the exact Connection that produced the original tools/call response.
type resourceLinkSession interface {
	ReadResource(context.Context, string) ([]protocol.ResourceContent, error)
}

// expandResourceLinks belongs to Resource adaptation. It completes bounded
// reads before canonical materialization, with current original-call authority
// checked before each read. A failure is postprocessing of a known Tool result;
// it never permits replaying tools/call.
func expandResourceLinks(ctx context.Context, session resourceLinkSession, require func(context.Context) error, contents []protocol.Content, maximum int) ([]protocol.Content, error) {
	if ctx == nil || nilPort(session) || require == nil || maximum < 0 || maximum > 256 {
		return nil, &protocol.Error{Code: protocol.InvalidRequest}
	}
	out := make([]protocol.Content, 0, len(contents))
	bytes := 0
	appendPart := func(part protocol.Content) error {
		if len(out) >= maximum {
			return &protocol.Error{Code: protocol.TooLarge}
		}
		bytes += len(part.Text) + len(part.Data) + len(part.URI) + len(part.MIMEType) + len(part.Name)
		if part.Resource != nil {
			bytes += len(part.Resource.URI) + len(part.Resource.MIMEType) + len(part.Resource.Blob)
			if part.Resource.Text != nil {
				bytes += len(*part.Resource.Text)
			}
		}
		if bytes > protocol.MaxContentBytes {
			return &protocol.Error{Code: protocol.TooLarge}
		}
		out = append(out, part)
		return nil
	}
	for _, content := range contents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if content.Kind != "resource_link" {
			if err := appendPart(content); err != nil {
				return nil, err
			}
			continue
		}
		if !protocol.ValidResourceURI(content.URI) {
			return nil, &protocol.Error{Code: protocol.InvalidResponse}
		}
		if err := require(ctx); err != nil {
			return nil, err
		}
		resources, err := session.ReadResource(ctx, content.URI)
		if err != nil {
			return nil, err
		}
		if len(resources) == 0 {
			return nil, &protocol.Error{Code: protocol.InvalidResponse}
		}
		for _, resource := range resources {
			// Resource contents have already passed protocol validation and
			// credential-reflection checks in this same SDK session.
			if err := appendPart(protocol.Content{Kind: "resource", Resource: &resource}); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
