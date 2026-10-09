package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const maxEnumBytes = 256

func invalid(path, code string) error {
	f := foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	f.FieldErrors = []foundation.FieldError{{Path: path, Code: code}}
	return f
}

func marshalChecked(v any, err error, limit int) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > limit {
		return nil, invalid("", "INVALID_ENCODING")
	}
	return raw, nil
}

// encoding/json otherwise silently replaces malformed UTF-8 and lone UTF-16
// surrogates. Check the actual raw before any loss can occur. The JSON parser
// below still owns syntax, escaping and trailing-value validation.
func validRaw(raw []byte, limit int) bool {
	if len(raw) > limit || !utf8.Valid(raw) {
		return false
	}
	inString := false
	for n := 0; n < len(raw); n++ {
		if raw[n] == '"' {
			inString = !inString
			continue
		}
		if !inString || raw[n] != '\\' {
			continue
		}
		n++
		if n >= len(raw) {
			return false
		}
		if raw[n] != 'u' {
			continue
		}
		if n+4 >= len(raw) {
			return false
		}
		x, err := strconv.ParseUint(string(raw[n+1:n+5]), 16, 16)
		if err != nil {
			return false
		}
		n += 4
		if x >= 0xdc00 && x <= 0xdfff {
			return false
		}
		if x >= 0xd800 && x <= 0xdbff {
			if n+6 >= len(raw) || raw[n+1] != '\\' || raw[n+2] != 'u' {
				return false
			}
			y, err := strconv.ParseUint(string(raw[n+3:n+7]), 16, 16)
			if err != nil || y < 0xdc00 || y > 0xdfff {
				return false
			}
			n += 6
		}
	}
	return true
}

func decodeString(raw []byte, limit int) (string, error) {
	if !validRaw(raw, limit) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '"' {
		return "", invalid("", "INVALID_ENCODING")
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	return s, nil
}

// All current DTO members are scalars. Each key is checked before decoding into
// a struct, avoiding encoding/json's case-insensitive aliases and last-key-wins
// behavior. IDs, timestamps, versions and enums then use their strict codecs.
// No nested arbitrary object/array is accepted as one of those scalar fields.
func decodeObject[T any](raw []byte, limit int, keys, nullable []string) (T, error) {
	var zero T
	if !validRaw(raw, limit) {
		return zero, invalid("", "INVALID_ENCODING")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	opening, err := d.Token()
	if err != nil || opening != json.Delim('{') {
		return zero, invalid("", "INVALID_ENCODING")
	}
	seen := make(map[string]bool, len(keys))
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || !slices.Contains(keys, key) {
			return zero, invalid("", "INVALID_ENCODING")
		}
		seen[key] = true
		var field json.RawMessage
		if d.Decode(&field) != nil {
			return zero, invalid("", "INVALID_ENCODING")
		}
		if bytes.Equal(bytes.TrimSpace(field), []byte("null")) && !slices.Contains(nullable, key) {
			return zero, invalid("/"+key, "NULL_NOT_ALLOWED")
		}
	}
	closing, err := d.Token()
	if err != nil || closing != json.Delim('}') {
		return zero, invalid("", "INVALID_ENCODING")
	}
	var extra json.RawMessage
	if d.Decode(&extra) != io.EOF {
		return zero, invalid("", "INVALID_ENCODING")
	}
	for _, key := range keys {
		if !seen[key] {
			return zero, invalid("/"+key, "REQUIRED")
		}
	}
	var next T
	if json.Unmarshal(raw, &next) != nil {
		return zero, invalid("", "INVALID_ENCODING")
	}
	return next, nil
}
