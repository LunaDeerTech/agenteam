package model

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// StructuredInputDigest binds messages and the canonical output schema. JSONB
// object-key ordering cannot change this identity; schema content still does.
func StructuredInputDigest(messages []mc.Message, format mc.ResponseFormat) (f.Digest, error) {
	return runtimeStructuredInputDigest(context.Background(), messages, format)
}

func runtimeStructuredInputDigest(ctx context.Context, messages []mc.Message, format mc.ResponseFormat) (f.Digest, error) {
	if format.Validate() != nil {
		return "", fault(f.InvalidArgument)
	}
	if err := runtimeTextShape(ctx, messages); err != nil {
		return "", err
	}
	if format.Kind == "json_schema" {
		canonical, err := cursor.CanonicalJSON(format.Schema)
		if err != nil {
			return "", fault(f.InvalidArgument)
		}
		defer clear(canonical)
		format.Schema = canonical
	}
	// Use an explicit durable projection rather than any request log encoding.
	input := struct {
		Format         int               `json:"format"`
		Messages       []mc.Message      `json:"messages"`
		ToolChoice     mc.ToolChoice     `json:"tool_choice"`
		ResponseFormat mc.ResponseFormat `json:"response_format"`
	}{1, messages, mc.ToolChoice{Kind: "none"}, format}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", unavailable(err)
	}
	defer clear(raw)
	if len(raw) > runtimeTextLimit {
		return "", fault(f.PayloadTooLarge)
	}
	if err := runtimeContextError(ctx); err != nil {
		return "", err
	}
	return hash(raw), nil
}

// EmbeddingInputDigest binds item order, text bytes and the expected vector
// dimensions. It never grants access to a snapshot or a consumer operation.
func EmbeddingInputDigest(texts []string, dimensions int) (f.Digest, error) {
	return runtimeEmbeddingDigest(context.Background(), texts, dimensions)
}

func runtimeEmbeddingDigest(ctx context.Context, texts []string, dimensions int) (f.Digest, error) {
	if err := runtimeContextError(ctx); err != nil {
		return "", err
	}
	if dimensions < 1 || dimensions > 4096 || len(texts) < 1 || len(texts) > 128 || len(texts) > 262144/dimensions {
		return "", fault(f.InvalidArgument)
	}
	total := 0
	encodedSize := len(`{"format":1,"texts":[],"dimensions":4096}`)
	for _, text := range texts {
		if len(text) == 0 || len(text) > 1<<20 || len(text) > (4<<20)-total || !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
			return "", fault(f.InvalidArgument)
		}
		total += len(text)
		encodedSize += 3
		for _, r := range text {
			switch {
			case r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '\t' || r == '\b' || r == '\f':
				encodedSize += 2
			case r < 32 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
				encodedSize += 6
			default:
				encodedSize += utf8.RuneLen(r)
			}
			if encodedSize > 8<<20 {
				return "", fault(f.PayloadTooLarge)
			}
		}
		if err := runtimeContextError(ctx); err != nil {
			return "", err
		}
	}
	raw, err := json.Marshal(struct {
		Format     int      `json:"format"`
		Texts      []string `json:"texts"`
		Dimensions int      `json:"dimensions"`
	}{1, texts, dimensions})
	if err != nil {
		return "", unavailable(err)
	}
	defer clear(raw)
	if len(raw) > 8<<20 {
		return "", fault(f.PayloadTooLarge)
	}
	if err := runtimeContextError(ctx); err != nil {
		return "", err
	}
	return hash(raw), nil
}
