package model

import (
	"context"
	"io"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	media "github.com/LunaDeerTech/agenteam/internal/central/model/media"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

func runtimeMediaParts(messages []mc.Message) []mc.MediaPart {
	var out []mc.MediaPart
	add := func(p *mc.MediaPart) {
		if p != nil {
			out = append(out, *p)
		}
	}
	for _, m := range messages {
		for _, p := range m.Parts {
			add(p.Image)
			add(p.File)
			if p.ToolResult != nil {
				for _, r := range p.ToolResult.Parts {
					add(r.Image)
					add(r.File)
				}
			}
		}
	}
	return out
}
func runtimeMediaShape(ctx context.Context, messages []mc.Message) error {
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	for _, m := range messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			return fault(f.CapabilityUnsupported)
		}
		for _, p := range m.Parts {
			if p.ToolCall != nil || p.ToolResult != nil || p.Metadata != nil {
				return fault(f.CapabilityUnsupported)
			}
			if (p.Image != nil || p.File != nil) && m.Role != "user" {
				return fault(f.CapabilityUnsupported)
			}
		}
	}
	raw, err := mc.EncodeCanonicalMessages(messages)
	if err != nil {
		return runtimePortError(err)
	}
	defer clear(raw)
	if len(raw) > runtimeTextLimit {
		return fault(f.PayloadTooLarge)
	}
	return runtimeContextError(ctx)
}
func runtimeMediaCapabilities(messages []mc.Message, caps mc.Capabilities) error {
	has := func(v string) bool {
		for _, m := range caps.InputModalities {
			if m == v {
				return true
			}
		}
		return false
	}
	for _, part := range runtimeMediaParts(messages) {
		if media.Limit(part.MediaType) == 0 || media.Image(part.MediaType) && !has("image") || !media.Image(part.MediaType) && !has("file") {
			return fault(f.CapabilityUnsupported)
		}
	}
	return nil
}
func (c *runtimeCall) prepareMedia() error {
	parts := runtimeMediaParts(c.request.Messages)
	if len(parts) == 0 {
		return nil
	}
	if nilPort(c.runtime.deps.Media) {
		return fault(f.DependencyUnbound)
	}
	session, err := c.runtime.deps.Media.OpenModelMedia(c.ctx, c.request)
	if err != nil {
		return runtimePortError(err)
	}
	if nilPort(session) {
		return fault(f.DependencyUnavailable)
	}
	c.mediaSession = session
	total := 0
	for _, part := range parts {
		duplicate := false
		for _, prior := range c.preparedMedia {
			if reflect.DeepEqual(prior.Part.Ref.Details(), part.Ref.Details()) {
				if prior.Part.MediaType != part.MediaType {
					return fault(f.InvalidArgument)
				}
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if len(c.preparedMedia) >= media.MaxAttachments {
			return fault(f.PayloadTooLarge)
		}
		reader, err := session.Read(c.ctx, part)
		if err != nil {
			return runtimePortError(err)
		}
		if nilPort(reader) {
			return fault(f.DependencyUnavailable)
		}
		limit := media.Limit(part.MediaType)
		if limit == 0 || reader.Meta().MediaType != part.MediaType || int64(reader.Meta().ByteSize) > int64(limit) {
			_ = reader.Close()
			return fault(f.CapabilityUnsupported)
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			clear(raw)
			if readErr != nil {
				return runtimePortError(readErr)
			}
			return runtimePortError(closeErr)
		}
		total += len(raw)
		if len(raw) > limit || total > media.MaxTotalBytes {
			clear(raw)
			return fault(f.PayloadTooLarge)
		}
		value, err := media.Prepare(c.ctx, reader.Filename(), part.MediaType, raw)
		clear(raw)
		if err != nil {
			return runtimePortError(err)
		}
		c.preparedMedia = append(c.preparedMedia, wire.PreparedMedia{Part: part, Value: value})
	}
	return nil
}
func (c *runtimeCall) mediaWireContext(ctx context.Context) context.Context {
	if c.mediaSession == nil {
		return ctx
	}
	return outbound.WithModelSendGuard(ctx, c.mediaSession.WithSend)
}
func (c *runtimeCall) closeMedia(ctx context.Context) error {
	if c.mediaSession != nil {
		if err := c.mediaSession.Close(ctx); err != nil {
			return runtimePortError(err)
		}
		c.mediaSession = nil
	}
	for _, part := range c.preparedMedia {
		part.Value.Clear()
	}
	c.preparedMedia = nil
	return nil
}
