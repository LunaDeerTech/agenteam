package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const (
	MaxDefinitionBytes  = 128 << 10
	MaxSchemaBytes      = 48 << 10
	MaxDescriptionBytes = 8192
)

type Idempotency string

const (
	Inherent Idempotency = "inherent"
	Keyed    Idempotency = "keyed"
	None     Idempotency = "none"
	Unknown  Idempotency = "unknown"
)

type Annotations struct {
	ReadOnly    bool        `json:"read_only"`
	Destructive bool        `json:"destructive"`
	Idempotency Idempotency `json:"idempotency"`
}

// Definition is the definition of a logical capability. It contains no backend,
// credentials, availability or execution permission. Registry assigns its ID and
// revision. Schema JSON is retained in full; this is not a schema validator or a
// Provider schema projection.
type Definition struct {
	StableKey    string          `json:"stable_key"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Annotations  *Annotations    `json:"annotations,omitempty"`
}

func invalidRegistry() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func stablePart(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
func ValidBuiltinKey(s string) bool {
	return strings.HasPrefix(s, "builtin:") && stablePart(strings.TrimPrefix(s, "builtin:"))
}

// CanonicalDefinition bounds and checks strict JSON (including duplicate keys)
// but never removes unsupported schema keywords. Numbers retain their lexical
// representation, so a changed numeric spelling may conservatively revise a
// definition. It does not claim semantic JSON-Schema equivalence.
func CanonicalDefinition(ctx context.Context, d Definition) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ValidBuiltinKey(d.StableKey) || len(d.Name) == 0 || len(d.Name) > 128 || !utf8.ValidString(d.Name) || strings.ContainsRune(d.Name, 0) || len(d.Description) > MaxDescriptionBytes || !utf8.ValidString(d.Description) || strings.ContainsRune(d.Description, 0) {
		return nil, invalidRegistry()
	}
	if d.Annotations != nil {
		switch d.Annotations.Idempotency {
		case Inherent, Keyed, None, Unknown:
		default:
			return nil, invalidRegistry()
		}
	}
	i, err := canonicalSchema(ctx, d.InputSchema)
	if err != nil {
		return nil, err
	}
	d.InputSchema = i
	if d.OutputSchema != nil {
		d.OutputSchema, err = canonicalSchema(ctx, d.OutputSchema)
		if err != nil {
			return nil, err
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return nil, invalidRegistry()
	}
	if len(b) > MaxDefinitionBytes {
		return nil, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return b, nil
}
func canonicalSchema(ctx context.Context, b []byte) ([]byte, error) {
	if len(b) > MaxSchemaBytes {
		return nil, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if len(b) == 0 || !utf8.Valid(b) {
		return nil, invalidRegistry()
	}
	if err := checkUnicodeEscapes(ctx, b); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, err := schemaValue(ctx, d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, invalidRegistry()
	}
	switch v.(type) {
	case map[string]any, bool:
	default:
		return nil, invalidRegistry()
	}
	return json.Marshal(v)
}

// encoding/json replaces lone surrogate escapes with U+FFFD. Reject them before
// decoding instead; escaped backslashes are consumed as pairs, so a literal
// "\\ud800" string is not mistaken for a surrogate. The decoder below remains
// responsible for the JSON grammar and all non-Unicode escape validity.
func checkUnicodeEscapes(ctx context.Context, b []byte) error {
	nextCheck := 0
	for n := 0; n < len(b); n++ {
		if n >= nextCheck {
			if err := ctx.Err(); err != nil {
				return err
			}
			nextCheck = n + 4096
		}
		if b[n] != '\\' {
			continue
		}
		n++
		if n == len(b) {
			return invalidRegistry()
		}
		if b[n] != 'u' {
			continue
		}
		value, ok := unicodeUnit(b[n+1:])
		if !ok {
			return invalidRegistry()
		}
		n += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return invalidRegistry()
		}
		if value < 0xd800 || value > 0xdbff {
			continue
		}
		if len(b)-n < 7 || b[n+1] != '\\' || b[n+2] != 'u' {
			return invalidRegistry()
		}
		low, ok := unicodeUnit(b[n+3:])
		if !ok || low < 0xdc00 || low > 0xdfff {
			return invalidRegistry()
		}
		n += 6
	}
	return ctx.Err()
}
func unicodeUnit(b []byte) (uint16, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var value uint16
	for _, c := range b[:4] {
		value <<= 4
		switch {
		case c >= '0' && c <= '9':
			value |= uint16(c - '0')
		case c >= 'a' && c <= 'f':
			value |= uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			value |= uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return value, true
}
func schemaValue(ctx context.Context, d *json.Decoder, depth int) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > 32 {
		return nil, invalidRegistry()
	}
	t, err := d.Token()
	if err != nil {
		return nil, invalidRegistry()
	}
	if delimiter, ok := t.(json.Delim); ok {
		switch delimiter {
		case '{':
			v := make(map[string]any)
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok {
					return nil, invalidRegistry()
				}
				if _, exists := v[name]; exists {
					return nil, invalidRegistry()
				}
				value, err := schemaValue(ctx, d, depth+1)
				if err != nil {
					return nil, err
				}
				v[name] = value
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, invalidRegistry()
			}
			return v, nil
		case '[':
			v := make([]any, 0)
			for d.More() {
				item, err := schemaValue(ctx, d, depth+1)
				if err != nil {
					return nil, err
				}
				v = append(v, item)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, invalidRegistry()
			}
			return v, nil
		default:
			return nil, invalidRegistry()
		}
	}
	switch t.(type) {
	case nil, string, bool, json.Number:
		return t, nil
	}
	return nil, invalidRegistry()
}

type BuiltinBinding struct {
	HandlerID        string    `json:"handler_id"`
	ContractRevision f.Version `json:"contract_revision"`
}
type ScopeResolverID string
type RiskClassifierID string
type ToolClass string

const (
	OrdinaryTool ToolClass = "ordinary"
	CoreTool     ToolClass = "core"
)

// BuiltinRegistration is supplied only by a trusted source fixed at composition.
// Active=false removes current registration while preserving all history/refs.
// Resolver/classifier IDs are code-bound identities, not arbitrary executable
// names. The source must verify that the exact three bindings actually exist.
type BuiltinRegistration struct {
	Definition       Definition
	Binding          BuiltinBinding
	ScopeResolverID  ScopeResolverID
	RiskClassifierID RiskClassifierID
	Class            ToolClass
	Active           bool
}

func (v BuiltinRegistration) Validate(ctx context.Context) error {
	if _, err := CanonicalDefinition(ctx, v.Definition); err != nil {
		return err
	}
	if !stablePart(v.Binding.HandlerID) || v.Binding.ContractRevision.Validate() != nil || !stablePart(string(v.ScopeResolverID)) || !stablePart(string(v.RiskClassifierID)) || v.Class != OrdinaryTool && v.Class != CoreTool {
		return invalidRegistry()
	}
	return ctx.Err()
}
func (Definition) Format(s fmt.State, _ rune)          { _, _ = io.WriteString(s, "tool_definition") }
func (Definition) LogValue() slog.Value                { return slog.StringValue("tool_definition") }
func (BuiltinRegistration) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "tool_registration") }
func (BuiltinRegistration) LogValue() slog.Value       { return slog.StringValue("tool_registration") }
