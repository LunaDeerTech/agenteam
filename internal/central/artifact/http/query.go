package artifacthttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const maxQueryBytes = 32768
const maxCursorBytes = 8192

// Bound original input, then decode each key and value exactly once.
func strictQuery(r *http.Request, read bool) (map[string]string, error) {
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > maxQueryBytes || r.URL.RawQuery == "" && r.URL.ForceQuery || strings.Contains(r.URL.RawQuery, ";") {
		return nil, invalidInput()
	}
	values := make(map[string]string)
	if r.URL.RawQuery == "" {
		return values, nil
	}
	for _, pair := range strings.Split(r.URL.RawQuery, "&") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, invalidInput()
		}
		key, err := url.QueryUnescape(key)
		if err != nil {
			return nil, invalidInput()
		}
		value, err = url.QueryUnescape(value)
		if err != nil || value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return nil, invalidInput()
		}
		if _, duplicate := values[key]; duplicate {
			return nil, invalidInput()
		}
		allowed := key == "limit"
		if read {
			allowed = allowed || key == "offset"
		} else {
			allowed = allowed || key == "execution_id" || key == "kind" || key == "media_type" || key == "name_query" || key == "cursor"
		}
		if !allowed {
			return nil, invalidInput()
		}
		if key == "cursor" && len(value) > maxCursorBytes {
			return nil, f.NewFault(f.CursorInvalid, f.NotStarted)
		}
		values[key] = value
	}
	return values, nil
}

func decimal(value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != value {
		return 0, invalidInput()
	}
	return n, nil
}

func listQuery(r *http.Request) (ac.ListFilter, f.PageRequest, error) {
	values, err := strictQuery(r, false)
	filter, page := ac.ListFilter{}, f.DefaultPageRequest()
	if err != nil {
		return filter, page, err
	}
	filter = ac.ListFilter{ExecutionID: values["execution_id"], Kind: ac.Kind(values["kind"]), MediaType: values["media_type"], NameQuery: values["name_query"]}
	page.Cursor = values["cursor"]
	if value, ok := values["limit"]; ok {
		n, err := decimal(value)
		if err != nil || n < 1 || n > f.MaxPageLimit {
			return filter, page, invalidInput()
		}
		page.Limit = int(n)
	}
	if filter.ExecutionID != "" {
		v, err := f.ParseID[id.Execution](filter.ExecutionID)
		if err != nil || v.String() != filter.ExecutionID {
			return filter, page, invalidInput()
		}
	}
	if filter.Validate() != nil || page.Validate() != nil {
		return filter, page, invalidInput()
	}
	return filter, page, nil
}

func readQuery(r *http.Request) (ac.PreviewRequest, error) {
	values, err := strictQuery(r, true)
	out := ac.PreviewRequest{Limit: ac.DefaultPreviewBytes}
	if err != nil {
		return out, err
	}
	for key, value := range values {
		n, err := decimal(value)
		if err != nil {
			return out, err
		}
		if key == "offset" {
			out.Offset = n
		} else {
			if n < 1 || n > ac.MaxPreviewBytes {
				return out, invalidInput()
			}
			out.Limit = int(n)
		}
	}
	if _, err := out.Normalize(); err != nil {
		return out, invalidInput()
	}
	return out, nil
}
