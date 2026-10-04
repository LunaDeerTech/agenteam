package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const MaxDescriptionBytes = 8192

func invalid(path, code string) error {
	f := foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	if code != "" {
		f.FieldErrors = []foundation.FieldError{{Path: path, Code: code}}
	}
	return f
}

func fault(code foundation.Code) error { return foundation.NewFault(code, foundation.NotStarted) }

// NormalizeName preserves the display spelling at the caller and returns only
// the route key. No trimming, Unicode folding or URL decoding is performed.
func NormalizeName(name string) (string, error) {
	if len(name) < 1 || len(name) > 64 || name == "." || name == ".." {
		return "", invalid("/name", "INVALID_NAME")
	}
	for _, c := range []byte(name) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return "", invalid("/name", "INVALID_NAME")
		}
	}
	return strings.ToLower(name), nil
}

func ValidateDescription(description string) error {
	if len(description) > MaxDescriptionBytes || !utf8.ValidString(description) {
		return invalid("/description", "INVALID_DESCRIPTION")
	}
	for _, c := range []byte(description) {
		if c < 32 && c != '\n' && c != '\t' || c == 127 {
			return invalid("/description", "INVALID_DESCRIPTION")
		}
	}
	return nil
}

// NormalizeUserRoute checks an existing username's syntax, including admin.
// It deliberately does not apply account creation's reserved-name rules.
func NormalizeUserRoute(username string) (string, error) {
	if len(username) < 3 || len(username) > 32 {
		return "", invalid("/username", "INVALID_ROUTE")
	}
	for i, c := range []byte(username) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' && i > 0 && i < len(username)-1) {
			return "", invalid("/username", "INVALID_ROUTE")
		}
	}
	return strings.ToLower(username), nil
}

// NormalizeProjectPath accepts two already once-decoded segments.
func NormalizeProjectPath(username, name string) (string, error) {
	user, err := NormalizeUserRoute(username)
	if err != nil {
		return "", err
	}
	project, err := NormalizeName(name)
	if err != nil {
		return "", err
	}
	return user + "/" + project, nil
}

func NormalizeConfirmationPath(path string) (string, error) {
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return "", invalid("/normalized_current_path", "INVALID_ROUTE")
	}
	canonical, err := NormalizeProjectPath(parts[0], parts[1])
	if err != nil {
		return "", invalid("/normalized_current_path", "INVALID_ROUTE")
	}
	return canonical, nil
}

func oneOf[S ~string](value S, allowed ...S) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return invalid("", "UNKNOWN_VARIANT")
}

func decodeEnum[S ~string](raw []byte, validate func(S) error) (S, error) {
	var wire string
	if len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '"' || json.Unmarshal(raw, &wire) != nil || validate(S(wire)) != nil {
		return "", invalid("", "UNKNOWN_VARIANT")
	}
	return S(wire), nil
}

func enumJSON[S ~string](value S, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(value))
}

// decodeFields uses the existing event codec's UTF-8, duplicate-key, depth and
// trailing-data checks, then enforces exact field names and explicit presence.
// RawMessage is private to this boundary, never an exported command or payload.
func decodeFields[T any](raw []byte, required, optional, nullable []string) (T, error) {
	var zero T
	value, err := (event.JSONCodec[T]{}).Decode(raw)
	if err != nil {
		return zero, invalid("", "INVALID_ENCODING")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return zero, invalid("", "INVALID_ENCODING")
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return zero, invalid("/"+key, "REQUIRED")
		}
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, rawValue := range fields {
		if !allowed[key] {
			// Unknown attacker-controlled keys must not become safe error paths.
			return zero, invalid("", "UNKNOWN_FIELD")
		}
		if bytes.Equal(bytes.TrimSpace(rawValue), []byte("null")) {
			canBeNull := false
			for _, name := range nullable {
				canBeNull = canBeNull || key == name
			}
			if !canBeNull {
				return zero, invalid("/"+key, "NULL_NOT_ALLOWED")
			}
		}
	}
	return value, nil
}

func checkedJSON(value any, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func validTimes(created, updated foundation.Instant) bool {
	return created.Validate() == nil && updated.Validate() == nil && !updated.Time().Before(created.Time())
}
