package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const MaxRequestBytes = 32 << 10
const MaxRecordBytes = 64 << 10
const MaxPageBytes = 2 << 20

func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func textValid(s string, min, max int) bool {
	if len(s) < min || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func tagsValid(v []string) bool {
	if v == nil || len(v) > 32 {
		return false
	}
	for n, s := range v {
		if !textValid(s, 1, 64) || n > 0 && v[n-1] >= s {
			return false
		}
	}
	return true
}

// The management wire is bounded before parsing; its schema never admits a
// dynamic object. Scan string escapes before encoding/json can repair a lone
// surrogate, then reject duplicate decoded names, deep values and trailing data.
func wire(raw []byte, cap int) error {
	if len(raw) == 0 || len(raw) > cap || !utf8.Valid(raw) {
		return invalid()
	}
	depth := 0
	for n := 0; n < len(raw); n++ {
		switch raw[n] {
		case '{', '[':
			depth++
			if depth > 16 {
				return invalid()
			}
		case '}', ']':
			depth--
		case '"':
			for n++; n < len(raw); n++ {
				if raw[n] == '"' {
					break
				}
				if raw[n] != '\\' {
					continue
				}
				n++
				if n >= len(raw) {
					return invalid()
				}
				if raw[n] != 'u' {
					continue
				}
				if n+4 >= len(raw) {
					return invalid()
				}
				u, e := strconv.ParseUint(string(raw[n+1:n+5]), 16, 16)
				if e != nil {
					return invalid()
				}
				n += 4
				if u >= 0xdc00 && u <= 0xdfff {
					return invalid()
				}
				if u >= 0xd800 && u <= 0xdbff {
					if n+6 >= len(raw) || raw[n+1] != '\\' || raw[n+2] != 'u' {
						return invalid()
					}
					low, e := strconv.ParseUint(string(raw[n+3:n+7]), 16, 16)
					if e != nil || low < 0xdc00 || low > 0xdfff {
						return invalid()
					}
					n += 6
				}
			}
		}
	}
	if _, e := cursor.CanonicalJSON(raw); e != nil {
		return invalid()
	}
	return nil
}
func decode[T any](raw []byte, cap int, required, optional, nullable []string) (T, error) {
	var zero T
	if wire(raw, cap) != nil {
		return zero, invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return zero, invalid()
	}
	allowed := map[string]bool{}
	nulls := map[string]bool{}
	for _, k := range nullable {
		nulls[k] = true
	}
	for _, k := range required {
		if _, ok := fields[k]; !ok {
			return zero, invalid()
		}
		allowed[k] = true
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k, v := range fields {
		if !allowed[k] || bytes.Equal(bytes.TrimSpace(v), []byte("null")) && !nulls[k] {
			return zero, invalid()
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var out T
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF {
		return zero, invalid()
	}
	return out, nil
}
func checked(v any, e error) ([]byte, error) {
	if e != nil {
		return nil, e
	}
	return json.Marshal(v)
}
func clone[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func rootValid(s string) bool {
	return len(s) > 0 && len(s) <= 4096 && utf8.ValidString(s) && strings.HasPrefix(s, "/") && !strings.ContainsAny(s, "\x00\\") && path.Clean(s) == s
}
