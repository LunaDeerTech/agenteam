package agenthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func invalidInput() error  { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }
func parseDirectoryQuery(r *http.Request, list bool) (f.PageRequest, error) {
	q := f.DefaultPageRequest()
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > 32<<10 || r.URL.ForceQuery && r.URL.RawQuery == "" || !list && r.URL.RawQuery != "" {
		return q, invalidInput()
	}
	values := map[string]string{}
	if r.URL.RawQuery != "" {
		if strings.Contains(r.URL.RawQuery, ";") {
			return q, invalidInput()
		}
		for _, pair := range strings.Split(r.URL.RawQuery, "&") {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return q, invalidInput()
			}
			k, err := url.QueryUnescape(k)
			if err != nil {
				return q, invalidInput()
			}
			v, err = url.QueryUnescape(v)
			if err != nil || v == "" || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
				return q, invalidInput()
			}
			if k != "limit" && k != "cursor" {
				return q, invalidInput()
			}
			if _, ok = values[k]; ok {
				return q, invalidInput()
			}
			values[k] = v
		}
	}
	if v, ok := values["limit"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || strconv.Itoa(n) != v || n < 1 || n > f.MaxPageLimit {
			return q, invalidInput()
		}
		q.Limit = n
	}
	q.Cursor = values["cursor"]
	if len(q.Cursor) > cursor.MaxTokenBytes {
		return q, f.NewFault(f.CursorInvalid, f.NotStarted)
	}
	return q, nil
}
func validateDirectoryPage(page f.Page[c.DirectoryEntry], project i.ProjectID, request f.PageRequest) error {
	if request.Validate() != nil || len(page.Items) > request.Limit || len(page.NextCursor) > cursor.MaxTokenBytes || !utf8.ValidString(page.NextCursor) || strings.ContainsRune(page.NextCursor, 0) || page.NextCursor != "" && len(page.Items) != request.Limit {
		return badProjection()
	}
	seen := map[i.AgentID]bool{}
	for n, v := range page.Items {
		if v.Validate() != nil || v.ProjectID != project || seen[v.ID] {
			return badProjection()
		}
		seen[v.ID] = true
		if n > 0 {
			p := page.Items[n-1]
			if v.CreatedAt.Time().After(p.CreatedAt.Time()) || v.CreatedAt == p.CreatedAt && v.ID.String() >= p.ID.String() {
				return badProjection()
			}
		}
	}
	return nil
}
func encodeDirectory(ctx context.Context, value any, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > limit {
		return nil, badProjection()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
