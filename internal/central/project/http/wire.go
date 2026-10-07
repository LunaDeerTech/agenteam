// Package projecthttp exposes current-Owner Project reads, not Project commands.
package projecthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const (
	maxQueryBytes          = 32768
	maxCursorBytes         = 8192
	maxRepresentationBytes = 5 << 20
)

func invalidQuery() error  { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }
func strictQuery(r *http.Request, list bool) (map[string]string, error) {
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > maxQueryBytes || r.URL.RawQuery == "" && r.URL.ForceQuery {
		return nil, invalidQuery()
	}
	values := map[string]string{}
	if r.URL.RawQuery == "" {
		return values, nil
	}
	if strings.Contains(r.URL.RawQuery, ";") {
		return nil, invalidQuery()
	}
	for _, pair := range strings.Split(r.URL.RawQuery, "&") {
		key, raw, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, invalidQuery()
		}
		key, err := url.QueryUnescape(key)
		if err != nil {
			return nil, invalidQuery()
		}
		value, err := url.QueryUnescape(raw)
		if err != nil || key == "" || value == "" || !utf8.ValidString(key) || !utf8.ValidString(value) || strings.ContainsRune(key, 0) || strings.ContainsRune(value, 0) {
			return nil, invalidQuery()
		}
		if _, ok := values[key]; ok {
			return nil, invalidQuery()
		}
		if !list || (key != "lifecycle" && key != "limit" && key != "cursor") {
			return nil, invalidQuery()
		}
		if key == "cursor" && len(value) > maxCursorBytes {
			return nil, f.NewFault(f.CursorInvalid, f.NotStarted)
		}
		values[key] = value
	}
	return values, nil
}
func projectQuery(r *http.Request, list bool) (pc.ListOwnedProjectsRequest, f.PageRequest, error) {
	request := pc.ListOwnedProjectsRequest{}
	page := f.DefaultPageRequest()
	values, err := strictQuery(r, list)
	if err != nil {
		return request, page, err
	}
	if raw, ok := values["lifecycle"]; ok {
		for _, s := range strings.Split(raw, ",") {
			request.Lifecycle = append(request.Lifecycle, pc.Lifecycle(s))
		}
	}
	if raw, ok := values["limit"]; ok {
		if len(raw) > 3 || raw[0] < '1' || raw[0] > '9' {
			return request, page, invalidQuery()
		}
		for _, c := range raw {
			if c < '0' || c > '9' {
				return request, page, invalidQuery()
			}
		}
		page.Limit, err = strconv.Atoi(raw)
		if err != nil {
			return request, page, invalidQuery()
		}
	}
	page.Cursor = values["cursor"]
	if request.Validate() != nil || pc.ValidateProjectPage(page) != nil {
		return request, page, invalidQuery()
	}
	return request, page, nil
}

type projectDTO struct {
	ID              id.ProjectID `json:"id"`
	OwnerUserID     id.UserID    `json:"owner_user_id"`
	Name            string       `json:"name"`
	NormalizedName  string       `json:"normalized_name"`
	Description     string       `json:"description"`
	Lifecycle       pc.Lifecycle `json:"lifecycle"`
	Version         f.Version    `json:"version"`
	CurrentSprintID *pc.SprintID `json:"current_sprint_id"`
	CreatedAt       f.Instant    `json:"created_at"`
	UpdatedAt       f.Instant    `json:"updated_at"`
	ArchivedAt      *f.Instant   `json:"archived_at"`
}

type projectListDTO struct {
	ID          pc.ProjectID    `json:"id"`
	Name        string          `json:"name"`
	Lifecycle   pc.Lifecycle    `json:"lifecycle"`
	Version     f.Version       `json:"version"`
	Description *string         `json:"description,omitempty"`
	OperationID *pc.OperationID `json:"operation_id,omitempty"`
}
type projectPageDTO struct {
	Items      []projectListDTO `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

func encodeValue(ctx context.Context, value any) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maxRepresentationBytes {
		return nil, badProjection()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
func encodeProject(ctx context.Context, actor id.Actor, target pc.ProjectID, value pc.ProjectRef) ([]byte, error) {
	if actor.Validate() != nil || actor.Details().Kind != id.Human || target.Validate() != nil || value.Validate() != nil || value.ID != target || value.OwnerUserID.String() != actor.Details().UserID || value.Lifecycle == pc.Deleting {
		return nil, badProjection()
	}
	return encodeValue(ctx, projectDTO{ID: value.ID, OwnerUserID: value.OwnerUserID, Name: value.Name, NormalizedName: value.NormalizedName, Description: value.Description, Lifecycle: value.Lifecycle, Version: value.Version, CurrentSprintID: value.CurrentSprintID, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, ArchivedAt: value.ArchivedAt})
}
func encodeList(ctx context.Context, request pc.ListOwnedProjectsRequest, page f.PageRequest, value f.Page[pc.ProjectListItem]) ([]byte, error) {
	filter, err := request.NormalizedFilter()
	if err != nil || pc.ValidateProjectPage(page) != nil || len(value.Items) > page.Limit || len(value.NextCursor) > maxCursorBytes || !utf8.ValidString(value.NextCursor) || strings.ContainsRune(value.NextCursor, 0) || value.NextCursor != "" && len(value.Items) != page.Limit {
		return nil, badProjection()
	}
	out := projectPageDTO{Items: make([]projectListDTO, 0, len(value.Items))}
	seen := map[pc.ProjectID]bool{}
	for _, item := range value.Items {
		if item.Validate() != nil || !slices.Contains(filter, item.Lifecycle) || seen[item.ID] || (item.Lifecycle == pc.Active || item.Lifecycle == pc.Archived) && item.OperationID != nil {
			return nil, badProjection()
		}
		seen[item.ID] = true
		out.Items = append(out.Items, projectListDTO{ID: item.ID, Name: item.Name, Lifecycle: item.Lifecycle, Version: item.Version, Description: item.Description, OperationID: item.OperationID})
	}
	if value.NextCursor != "" {
		next := value.NextCursor
		out.NextCursor = &next
	}
	return encodeValue(ctx, out)
}
