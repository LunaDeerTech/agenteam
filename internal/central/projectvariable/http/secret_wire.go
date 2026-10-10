package projectvariablehttp

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Secret bodies have one owned allocation. No growing reader, decoder buffer,
// generic decoded value map or immutable plaintext string holds the material.
// The caller clears successful reads; all failing reads clear before return.
func readSecretBody(r *http.Request) ([]byte, error) {
	var types []string
	for name, values := range r.Header {
		if strings.EqualFold(name, "Content-Encoding") {
			return nil, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
		if strings.EqualFold(name, "Content-Type") {
			types = append(types, values...)
		}
	}
	if len(types) != 1 {
		return nil, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
	}
	media, params, err := mime.ParseMediaType(types[0])
	if err != nil || media != "application/json" {
		return nil, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
	}
	for key, value := range params {
		if key != "charset" || !strings.EqualFold(value, "utf-8") {
			return nil, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	if r.Body == nil {
		return nil, invalidInput()
	}
	raw := make([]byte, bodyLimit+1)
	owned := true
	defer func() {
		if owned {
			clear(raw)
		}
	}()
	n, err := io.ReadFull(r.Body, raw)
	if n > bodyLimit {
		clear(raw)
		return nil, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if err != io.EOF && err != io.ErrUnexpectedEOF || !utf8.Valid(raw[:n]) {
		clear(raw)
		return nil, invalidInput()
	}
	owned = false
	return raw[:n], nil
}

func secretSpace(raw []byte, at int) int {
	for at < len(raw) && (raw[at] == ' ' || raw[at] == '\t' || raw[at] == '\r' || raw[at] == '\n') {
		at++
	}
	return at
}

func secretHex(raw []byte) (rune, bool) {
	if len(raw) < 4 {
		return 0, false
	}
	var n rune
	for _, b := range raw[:4] {
		n <<= 4
		switch {
		case b >= '0' && b <= '9':
			n += rune(b - '0')
		case b >= 'a' && b <= 'f':
			n += rune(b-'a') + 10
		case b >= 'A' && b <= 'F':
			n += rune(b-'A') + 10
		default:
			return 0, false
		}
	}
	return n, true
}

// at points at the backslash. Return the decoded scalar and the next byte.
func secretEscape(raw []byte, at int) (rune, int, bool) {
	if at+1 >= len(raw) {
		return 0, at, false
	}
	switch b := raw[at+1]; b {
	case '"', '\\', '/':
		return rune(b), at + 2, true
	case 'b':
		return '\b', at + 2, true
	case 'f':
		return '\f', at + 2, true
	case 'n':
		return '\n', at + 2, true
	case 'r':
		return '\r', at + 2, true
	case 't':
		return '\t', at + 2, true
	case 'u':
		x, ok := secretHex(raw[at+2:])
		if !ok || x >= 0xdc00 && x <= 0xdfff {
			return 0, at, false
		}
		if x < 0xd800 || x > 0xdbff {
			return x, at + 6, true
		}
		if at+12 > len(raw) || raw[at+6] != '\\' || raw[at+7] != 'u' {
			return 0, at, false
		}
		y, ok := secretHex(raw[at+8:])
		if !ok || y < 0xdc00 || y > 0xdfff {
			return 0, at, false
		}
		return utf16.DecodeRune(x, y), at + 12, true
	}
	return 0, at, false
}

func secretStringEnd(raw []byte, at int) (int, bool) {
	if at >= len(raw) || raw[at] != '"' {
		return at, false
	}
	for at++; at < len(raw); {
		switch {
		case raw[at] == '"':
			return at + 1, true
		case raw[at] < 0x20:
			return at, false
		case raw[at] == '\\':
			_, next, ok := secretEscape(raw, at)
			if !ok {
				return at, false
			}
			at = next
		default:
			at++
		}
	}
	return at, false
}

// After encoding/json validates JSON syntax, recognize only this wire's strings
// and one nested request object. This is not a general JSON decoder.
func secretValueEnd(raw []byte, at, depth int) (int, bool) {
	if at >= len(raw) {
		return at, false
	}
	if raw[at] == '"' {
		return secretStringEnd(raw, at)
	}
	if raw[at] != '{' || depth > 1 {
		return at, false
	}
	at = secretSpace(raw, at+1)
	if at < len(raw) && raw[at] == '}' {
		return at + 1, true
	}
	for at < len(raw) {
		end, ok := secretStringEnd(raw, at)
		if !ok {
			return at, false
		}
		at = secretSpace(raw, end)
		if at >= len(raw) || raw[at] != ':' {
			return at, false
		}
		end, ok = secretValueEnd(raw, secretSpace(raw, at+1), depth+1)
		if !ok {
			return at, false
		}
		at = secretSpace(raw, end)
		if at < len(raw) && raw[at] == '}' {
			return at + 1, true
		}
		if at >= len(raw) || raw[at] != ',' {
			return at, false
		}
		at = secretSpace(raw, at+1)
	}
	return at, false
}

// Values are slices of the caller's owned body, never additional raw copies.
// Only known schema keys become map keys; unknown names never enter a Fault.
func secretObject(raw []byte, allowed ...string) (map[string][]byte, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, invalidInput()
	}
	at := secretSpace(raw, 0)
	end, ok := secretValueEnd(raw, at, 0)
	if !ok || raw[at] != '{' || secretSpace(raw, end) != len(raw) {
		return nil, invalidInput()
	}
	out := make(map[string][]byte, len(allowed))
	at = secretSpace(raw, at+1)
	for raw[at] != '}' {
		keyEnd, _ := secretStringEnd(raw, at)
		// All admitted field names are short ASCII; allow their escaped form.
		if keyEnd-at > 6*32+2 {
			return nil, invalidInput()
		}
		var key string
		if json.Unmarshal(raw[at:keyEnd], &key) != nil || !slices.Contains(allowed, key) {
			return nil, invalidInput()
		}
		if _, exists := out[key]; exists {
			return nil, invalidInput()
		}
		at = secretSpace(raw, keyEnd)
		at = secretSpace(raw, at+1) // already validated colon
		valueEnd, _ := secretValueEnd(raw, at, 1)
		out[key] = raw[at:valueEnd]
		at = secretSpace(raw, valueEnd)
		if raw[at] == ',' {
			at = secretSpace(raw, at+1)
		}
	}
	return out, nil
}

func secretStringBytes(raw []byte) ([]byte, error) {
	end, ok := secretStringEnd(raw, 0)
	if !ok || end != len(raw) || !utf8.Valid(raw) {
		return nil, invalidInput()
	}
	value := make([]byte, 0, len(raw)-2)
	for at := 1; at < len(raw)-1; {
		if raw[at] != '\\' {
			value = append(value, raw[at])
			at++
			continue
		}
		r, next, _ := secretEscape(raw, at)
		value = utf8.AppendRune(value, r)
		at = next
	}
	return value, nil
}

// Account's projector trusts SafeMessage and valid FieldError strings. Keep
// only this adapter's closed schema diagnostics; retain code/state/cause for
// Unknown and Session failures, never an arbitrary provider message or path.
func secretProblem(err error) error {
	var fault *f.Fault
	if !errors.As(err, &fault) || fault == nil {
		return f.NewFault(f.InternalError, f.Unknown)
	}
	next := *fault
	next.SafeMessage, next.RetryHint = "", ""
	next.FieldErrors = nil
	for _, field := range fault.FieldErrors {
		if !slices.Contains([]string{"", "/request", "/request/variable_id", "/request/name", "/request/description", "/request/value", "/command", "/target_id", "/expected_version", "/limit", "/cursor"}, field.Path) {
			continue
		}
		if !slices.Contains([]string{"REQUIRED", "INVALID_FIELD", "INVALID_ENCODING", "INVALID_ID", "INVALID_NAME", "RESERVED_NAME", "INVALID_DESCRIPTION", "INVALID_SECRET_VALUE", "EMPTY_UPDATE", "INVALID_COMMAND", "INVALID_COMMAND_META", "INVALID_LOOKUP"}, field.Code) {
			continue
		}
		next.FieldErrors = append(next.FieldErrors, field)
	}
	return &next
}
