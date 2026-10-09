package projectvariablehttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func parseQuery(r *http.Request, kind resource) (f.PageRequest, error) {
	q := f.DefaultPageRequest()
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > maxQueryBytes || r.URL.RawQuery == "" && r.URL.ForceQuery {
		return q, invalidInput()
	}
	if kind != variables && r.URL.RawQuery != "" {
		return q, invalidInput()
	}
	values := map[string]string{}
	if r.URL.RawQuery != "" {
		if strings.Contains(r.URL.RawQuery, ";") {
			return q, invalidInput()
		}
		for _, pair := range strings.Split(r.URL.RawQuery, "&") {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return q, invalidInput()
			}
			key, e := url.QueryUnescape(key)
			if e != nil {
				return q, invalidInput()
			}
			value, e = url.QueryUnescape(value)
			if e != nil || key == "" || value == "" || !utf8.ValidString(key) || !utf8.ValidString(value) || strings.ContainsRune(key, 0) || strings.ContainsRune(value, 0) {
				return q, invalidInput()
			}
			if _, exists := values[key]; exists {
				return q, invalidInput()
			}
			if key != "limit" && key != "cursor" {
				return q, invalidInput()
			}
			values[key] = value
		}
	}
	if v, ok := values["limit"]; ok {
		n, e := strconv.Atoi(v)
		if e != nil || strconv.Itoa(n) != v || n < 1 || n > c.MaxPageLimit {
			return q, invalidInput()
		}
		q.Limit = n
	}
	q.Cursor = values["cursor"]
	if len(q.Cursor) > maxCursorBytes {
		return q, f.NewFault(f.CursorInvalid, f.NotStarted)
	}
	return q, nil
}
func (h *handler) read(r *http.Request, a id.Actor, p c.ProjectID, route route) ([]byte, error) {
	q, e := parseQuery(r, route.kind)
	if e != nil {
		return nil, e
	}
	if e = emptyBody(r); e != nil {
		return nil, e
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	switch route.kind {
	case variables:
		page, e := h.variables.ListVariables(r.Context(), a, p, q)
		if e != nil {
			return nil, e
		}
		if !validPage(q, len(page.Items), page.NextCursor) {
			return nil, badProjection()
		}
		seen := map[c.VariableID]bool{}
		for n, item := range page.Items {
			v := item.Fields()
			if item.Validate() != nil || v.ProjectID != p || seen[v.ID] {
				return nil, badProjection()
			}
			seen[v.ID] = true
			if n > 0 {
				last := page.Items[n-1].Fields()
				if v.Name < last.Name || v.Name == last.Name && v.ID.String() <= last.ID.String() {
					return nil, badProjection()
				}
			}
		}
		return encodeValue(r.Context(), page, listLimit)
	case variable:
		target, e := f.ParseID[id.ProjectVariable](route.target)
		if e != nil {
			return nil, invalidInput()
		}
		out, e := h.variables.GetVariable(r.Context(), a, p, target)
		if e != nil {
			return nil, e
		}
		fields := out.Fields()
		if out.Validate() != nil || fields.ProjectID != p || fields.ID != target {
			return nil, badProjection()
		}
		return encodeValue(r.Context(), out, bodyLimit)
	}
	return nil, invalidInput()
}
