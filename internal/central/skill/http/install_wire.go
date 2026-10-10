package skillhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const installBodyLimit = 1 << 20
const catalogRepresentationLimit = 100*maxRepresentationBytes + cursor.MaxTokenBytes + 1024

type installFileDTO struct {
	Path string  `json:"path"`
	Text *string `json:"utf8_text"`
}
type installSourceDTO struct {
	Kind  string           `json:"kind"`
	Files []installFileDTO `json:"files"`
}
type installRequestDTO struct {
	Skill  sc.SkillID       `json:"skill_id"`
	Mode   string           `json:"mode"`
	Source installSourceDTO `json:"source"`
}

// Both Install and Lookup consume the same complete intent. Lookup never
// submits another upload and a new key cannot recover an uncertain command.
func decodeInstallation(w http.ResponseWriter, r *http.Request) (f.CommandMeta, skill.InstallRequest, error) {
	var empty skill.InstallRequest
	if r.URL == nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return f.CommandMeta{}, empty, invalidInput()
	}
	var keys, contentTypes []string
	for name, values := range r.Header {
		switch {
		case strings.EqualFold(name, "Idempotency-Key"):
			keys = append(keys, values...)
		case strings.EqualFold(name, "Content-Type"):
			contentTypes = append(contentTypes, values...)
		case strings.EqualFold(name, "Content-Encoding"):
			return f.CommandMeta{}, empty, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	if len(keys) != 1 || f.IdempotencyKey(keys[0]).Validate() != nil {
		return f.CommandMeta{}, empty, invalidInput()
	}
	if len(contentTypes) != 1 {
		return f.CommandMeta{}, empty, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
	}
	meta := f.CommandMeta{IdempotencyKey: f.IdempotencyKey(keys[0]), RequestID: httpapi.RequestID(r.Context())}
	if meta.Validate() != nil {
		return f.CommandMeta{}, empty, invalidInput()
	}
	var wire struct {
		Request *installRequestDTO `json:"request"`
	}
	if err := httpapi.DecodeJSON(w, r, &wire, installBodyLimit); err != nil {
		return f.CommandMeta{}, empty, err
	}
	if wire.Request == nil || wire.Request.Skill.Validate() != nil || wire.Request.Mode != "create" || wire.Request.Source.Kind != "text_files" || len(wire.Request.Source.Files) == 0 || len(wire.Request.Source.Files) > sc.MaxFiles {
		return f.CommandMeta{}, empty, invalidInput()
	}
	files := make([]sc.TextFile, 0, len(wire.Request.Source.Files))
	for _, file := range wire.Request.Source.Files {
		if file.Text == nil {
			return f.CommandMeta{}, empty, invalidInput()
		}
		files = append(files, sc.TextFile{Path: file.Path, UTF8Text: *file.Text})
	}
	text, err := sc.NewTextFiles(files)
	if err != nil {
		return f.CommandMeta{}, empty, err
	}
	pkg, err := skill.BuildPackage(r.Context(), text)
	if err != nil {
		return f.CommandMeta{}, empty, err
	}
	request, err := skill.NewInstallRequest(r.Context(), wire.Request.Skill, pkg)
	return meta, request, err
}

func catalogQuery(r *http.Request) (skill.OwnerCatalogQuery, error) {
	if r.URL == nil || r.URL.ForceQuery || len(r.URL.RawQuery) > 3*cursor.MaxTokenBytes+64 {
		return skill.OwnerCatalogQuery{}, invalidInput()
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return skill.OwnerCatalogQuery{}, invalidInput()
	}
	out := skill.OwnerCatalogQuery{Limit: 25}
	for key, values := range query {
		if len(values) != 1 || values[0] == "" {
			return skill.OwnerCatalogQuery{}, invalidInput()
		}
		switch key {
		case "limit":
			limit, err := strconv.Atoi(values[0])
			if err != nil || limit < 1 || limit > 100 || strconv.Itoa(limit) != values[0] {
				return skill.OwnerCatalogQuery{}, invalidInput()
			}
			out.Limit = limit
		case "cursor":
			if len(values[0]) > cursor.MaxTokenBytes {
				return skill.OwnerCatalogQuery{}, invalidInput()
			}
			out.Cursor = values[0]
		default:
			return skill.OwnerCatalogQuery{}, invalidInput()
		}
	}
	return out, nil
}

func encodeCatalog(ctx context.Context, project id.ProjectID, query skill.OwnerCatalogQuery, page skill.OwnerCatalogPage) ([]byte, error) {
	if query.Limit < 1 || query.Limit > 100 || page.Items == nil || len(page.Items) > query.Limit || len(page.NextCursor) > cursor.MaxTokenBytes || page.NextCursor != "" && len(page.Items) != query.Limit {
		return nil, badProjection()
	}
	items := make([]metadataDTO, 0, len(page.Items))
	previous := ""
	for _, item := range page.Items {
		dto, err := projectMetadata(project, item)
		if err != nil || dto.ID.String() <= previous {
			return nil, badProjection()
		}
		previous = dto.ID.String()
		items = append(items, dto)
	}
	return encodeManagement(ctx, struct {
		Items      []metadataDTO `json:"items"`
		NextCursor string        `json:"next_cursor,omitempty"`
	}{items, page.NextCursor}, catalogRepresentationLimit)
}

func encodeInstallation(ctx context.Context, project id.ProjectID, request skill.InstallRequest, receipt skill.InstallReceipt) ([]byte, error) {
	if receipt.Validate() != nil || receipt.ProjectID != project || receipt.SkillID != request.SkillID() {
		return nil, f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	return encodeManagement(ctx, struct {
		Skill    sc.SkillID `json:"skill_id"`
		Revision f.Revision `json:"revision"`
		Version  f.Version  `json:"version"`
	}{receipt.SkillID, receipt.Revision, receipt.Version}, 1024)
}

func encodeManagement(ctx context.Context, value any, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > limit {
		return nil, badProjection()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
