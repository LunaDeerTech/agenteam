package contenthttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

const maxQueryBytes = 256

func parseQuery(r *http.Request) (kc.ReadRequest, error) {
	out := kc.DefaultReadRequest()
	if r == nil || r.URL == nil || len(r.URL.RawQuery) > maxQueryBytes || r.URL.RawQuery == "" && r.URL.ForceQuery {
		return out, invalidInput()
	}
	if r.URL.RawQuery == "" {
		return out, nil
	}
	seen := make(map[string]bool)
	for _, pair := range strings.Split(r.URL.RawQuery, "&") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return out, invalidInput()
		}
		key, err := url.QueryUnescape(key)
		if err != nil || key != "byte_offset" && key != "max_bytes" || seen[key] {
			return out, invalidInput()
		}
		seen[key] = true
		value, err = url.QueryUnescape(value)
		if err != nil {
			return out, invalidInput()
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 || strconv.FormatInt(n, 10) != value {
			return out, invalidInput()
		}
		if key == "byte_offset" {
			out.ByteOffset = f.Progress(n)
		} else {
			if n < 1 || n > kc.MaxReadBytes {
				return out, invalidInput()
			}
			out.MaxBytes = int(n)
		}
	}
	if out.Validate() != nil {
		return out, invalidInput()
	}
	return out, nil
}
