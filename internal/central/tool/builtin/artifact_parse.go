package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const MaxArtifactArguments = ac.MaxInlineBytes + (16 << 10)

type artifactArguments struct {
	display        ac.Display
	media, content string
	ref            ac.ArtifactRef
	preview        ac.PreviewRequest
	filter         ac.ListFilter
	page           f.PageRequest
}

func artifactInvalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }

// ValidateArtifactArguments validates the closed schema before operation
// admission. The same decoder is used immediately before the domain call.
func ValidateArtifactArguments(ctx context.Context, name string, raw []byte) error {
	_, err := parseArtifactArguments(ctx, name, raw)
	return err
}

func artifactObject(raw []byte, required []string, fields map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return artifactInvalid()
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		target, allowed := fields[name]
		if err != nil || !ok || !allowed || seen[name] {
			return artifactInvalid()
		}
		seen[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, target) != nil {
			return artifactInvalid()
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return artifactInvalid()
	}
	if _, err = decoder.Token(); err != io.EOF {
		return artifactInvalid()
	}
	for _, key := range required {
		if !seen[key] {
			return artifactInvalid()
		}
	}
	return nil
}

func parseArtifactArguments(ctx context.Context, name string, raw []byte) (artifactArguments, error) {
	var out artifactArguments
	if ctx == nil {
		return out, artifactInvalid()
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if len(raw) > MaxArtifactArguments {
		return out, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if len(raw) == 0 || !utf8.Valid(raw) || skillInstallUnicode(ctx, raw) != nil {
		return out, artifactInvalid()
	}
	var err error
	switch name {
	case ArtifactCreateName:
		err = artifactObject(raw, []string{"name", "media_type", "content"}, map[string]any{"name": &out.display.Name, "description": &out.display.Description, "media_type": &out.media, "content": &out.content})
		if err == nil {
			err = out.display.Validate()
		}
		if err == nil {
			err = ac.ValidateInline(out.content)
		}
		if err == nil {
			out.media, err = oc.NormalizeMediaType(out.media)
		}
	case ArtifactReadName:
		out.preview.Limit = ac.DefaultPreviewBytes
		var reference json.RawMessage
		err = artifactObject(raw, []string{"artifact_ref"}, map[string]any{"artifact_ref": &reference, "offset": &out.preview.Offset, "limit": &out.preview.Limit})
		if err == nil {
			err = artifactObject(reference, []string{"project_id", "artifact_id", "file_id"}, map[string]any{"project_id": &out.ref.ProjectID, "artifact_id": &out.ref.ArtifactID, "file_id": &out.ref.FileID})
		}
		if err == nil {
			err = out.ref.Validate()
		}
		if err == nil && out.preview.Limit < 1 {
			err = artifactInvalid()
		}
		if err == nil {
			out.preview, err = out.preview.Normalize()
		}
	case ArtifactListName:
		out.page = f.DefaultPageRequest()
		err = artifactObject(raw, nil, map[string]any{"execution_id": &out.filter.ExecutionID, "kind": &out.filter.Kind, "media_type": &out.filter.MediaType, "name_query": &out.filter.NameQuery, "limit": &out.page.Limit, "cursor": &out.page.Cursor})
		if err == nil {
			err = out.filter.Validate()
		}
		if err == nil && (out.page.Validate() != nil || len(out.page.Cursor) > 8192) {
			err = artifactInvalid()
		}
	default:
		err = artifactInvalid()
	}
	if err != nil {
		return artifactArguments{}, err
	}
	return out, ctx.Err()
}
