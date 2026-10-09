package knowledgehttp

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxQueryBytes = 32 << 10
const maxCursorBytes = 8192

type query struct {
	filter kc.ListFilter
	page   f.PageRequest
	parent *kc.DocumentID
}

func safeQueryText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func parseQuery(r *http.Request, kind resource) (query, error) {
	q := query{page: f.DefaultPageRequest()}
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > maxQueryBytes || r.URL.RawQuery == "" && r.URL.ForceQuery {
		return q, invalidInput()
	}
	if (kind == document || kind == ancestors) && r.URL.RawQuery != "" {
		return q, invalidInput()
	}
	values := map[string]string{}
	if raw := r.URL.RawQuery; raw != "" {
		if strings.Contains(raw, ";") {
			return q, invalidInput()
		}
		for _, pair := range strings.Split(raw, "&") {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return q, invalidInput()
			}
			key, err := url.QueryUnescape(key)
			if err != nil {
				return q, invalidInput()
			}
			value, err = url.QueryUnescape(value)
			if err != nil || key == "" || value == "" || !safeQueryText(key) || !safeQueryText(value) {
				return q, invalidInput()
			}
			if _, exists := values[key]; exists {
				return q, invalidInput()
			}
			allowed := key == "limit" || key == "cursor" || key == "title_query"
			if kind == documents || kind == children {
				allowed = allowed || key == "source_kind" || key == "media_type" || key == "indexing_status"
			}
			if kind == children {
				allowed = allowed || key == "parent_document_id"
			}
			if !allowed {
				return q, invalidInput()
			}
			values[key] = value
		}
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || strconv.Itoa(n) != raw {
			return q, invalidInput()
		}
		q.page.Limit = n
	}
	q.page.Cursor = values["cursor"]
	if len(q.page.Cursor) > maxCursorBytes {
		return q, f.NewFault(f.CursorInvalid, f.NotStarted)
	}
	q.filter.TitleQuery = values["title_query"]
	if v, ok := values["source_kind"]; ok {
		x := kc.SourceKind(v)
		q.filter.SourceKind = &x
	}
	if v, ok := values["media_type"]; ok {
		q.filter.MediaType = &v
	}
	if v, ok := values["indexing_status"]; ok {
		x := kc.IndexingStatus(v)
		q.filter.IndexingStatus = &x
	}
	if kind == children {
		raw, ok := values["parent_document_id"]
		if !ok {
			return q, invalidInput()
		}
		if raw != "null" {
			x, err := f.ParseID[kc.Document](raw)
			if err != nil || x.String() != raw {
				return q, invalidInput()
			}
			q.parent = &x
		}
	}
	if q.filter.Validate() != nil || q.page.Validate() != nil {
		return q, invalidInput()
	}
	return q, nil
}
