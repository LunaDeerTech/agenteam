package executionhttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func invalidInput() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func readQuery(r *http.Request, detail bool) (f.PageRequest, error) {
	page := f.DefaultPageRequest()
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > 32768 || r.URL.ForceQuery && r.URL.RawQuery == "" || strings.Contains(r.URL.RawQuery, ";") {
		return page, invalidInput()
	}
	if r.URL.RawQuery == "" {
		return page, nil
	}
	if detail {
		return page, invalidInput()
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(r.URL.RawQuery, "&") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return page, invalidInput()
		}
		k, err := url.QueryUnescape(k)
		if err != nil {
			return page, invalidInput()
		}
		v, err = url.QueryUnescape(v)
		if err != nil || v == "" || !utf8.ValidString(v) || strings.ContainsRune(v, 0) || seen[k] {
			return page, invalidInput()
		}
		seen[k] = true
		switch k {
		case "limit":
			n, err := strconv.Atoi(v)
			if err != nil || strconv.Itoa(n) != v || n < 1 || n > execution.MaxTaskExecutionPage {
				return page, invalidInput()
			}
			page.Limit = n
		case "cursor":
			if len(v) > cursor.MaxTokenBytes {
				return page, f.NewFault(f.CursorInvalid, f.NotStarted)
			}
			page.Cursor = v
		default:
			return page, invalidInput()
		}
	}
	return page, nil
}
