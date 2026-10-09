package runnerprotocol

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// parseJSON rejects lossy JSON forms before encoding/json can replace them.
// Counters apply to the whole message, including operation-owned JSON payloads.
func parseJSON(raw []byte) (any, error) {
	if len(raw) > MaxMessageBytes {
		return nil, ErrTooLarge
	}
	if len(raw) == 0 || !utf8.Valid(raw) || !validSurrogates(raw) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	members := 0
	v, e := jsonValue(d, 0, &members)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	return v, nil
}
func jsonValue(d *json.Decoder, depth int, members *int) (any, error) {
	t, e := d.Token()
	if e != nil {
		return nil, ErrInvalid
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	if depth >= MaxJSONDepth {
		return nil, ErrTooLarge
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return nil, ErrInvalid
			}
			key, ok := k.(string)
			if !ok {
				return nil, ErrInvalid
			}
			if _, exists := m[key]; exists {
				return nil, ErrInvalid
			}
			*members++
			if *members > MaxJSONMembers {
				return nil, ErrTooLarge
			}
			v, e := jsonValue(d, depth+1, members)
			if e != nil {
				return nil, e
			}
			m[key] = v
		}
		if end, e := d.Token(); e != nil || end != json.Delim('}') {
			return nil, ErrInvalid
		}
		return m, nil
	case '[':
		a := []any{}
		for d.More() {
			*members++
			if *members > MaxJSONMembers {
				return nil, ErrTooLarge
			}
			v, e := jsonValue(d, depth+1, members)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		if end, e := d.Token(); e != nil || end != json.Delim(']') {
			return nil, ErrInvalid
		}
		return a, nil
	}
	return nil, ErrInvalid
}
func hex4(b []byte) (uint16, bool) {
	if len(b) != 4 {
		return 0, false
	}
	var n uint16
	for _, c := range b {
		n <<= 4
		switch {
		case c >= '0' && c <= '9':
			n += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			n += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			n += uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return n, true
}
func validSurrogates(raw []byte) bool {
	inString := false
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		if b == '"' {
			inString = !inString
			continue
		}
		if !inString || b != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, ok := hex4(raw[i+1 : i+5])
		if !ok {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			next, ok := hex4(raw[i+3 : i+7])
			if !ok || next < 0xdc00 || next > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return !inString
}
func fields(v any, required, optional []string) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, ErrInvalid
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, k := range required {
		allowed[k] = true
		if x, ok := m[k]; !ok || x == nil {
			return nil, ErrInvalid
		}
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k, v := range m {
		if !allowed[k] || v == nil {
			return nil, ErrInvalid
		}
	}
	return m, nil
}
func words(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}
