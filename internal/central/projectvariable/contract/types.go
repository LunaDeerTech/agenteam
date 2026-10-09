// Package contract defines ordinary Project Variables; it grants no Secret or Agent capability.
package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type VariableID = i.ProjectVariableID
type ProjectID = i.ProjectID
type Operation struct{}
type OperationID = f.ID[Operation]

const (
	MaxNameBytes        = 128
	MaxDescriptionBytes = 4096
	MaxValueBytes       = 32768
	MaxRequestBytes     = 512 << 10
	MaxReceiptBytes     = 512 << 10
	MaxVariables        = 4096
	MaxPageLimit        = 100
	VariableType        = "variable"
)

func invalid(path, code string) error {
	e := f.NewFault(f.InvalidArgument, f.NotStarted)
	if code != "" {
		e.FieldErrors = []f.FieldError{{Path: path, Code: code}}
	}
	return e
}
func safe(w fmt.State)    { _, _ = io.WriteString(w, "project_variable") }
func safeLog() slog.Value { return slog.StringValue("project_variable") }
func ValidateName(v string) error {
	if len(v) < 1 || len(v) > MaxNameBytes {
		return invalid("/name", "INVALID_NAME")
	}
	for n, c := range []byte(v) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || n > 0 && c >= '0' && c <= '9') {
			return invalid("/name", "INVALID_NAME")
		}
	}
	upper := strings.ToUpper(v)
	if upper == "AGENTEAM" || strings.HasPrefix(upper, "AGENTEAM_") {
		return invalid("/name", "RESERVED_NAME")
	}
	return nil
}
func ValidateDescription(v string) error {
	if len(v) > MaxDescriptionBytes || !utf8.ValidString(v) {
		return invalid("/description", "INVALID_DESCRIPTION")
	}
	for _, c := range v {
		if unicode.IsControl(c) && c != '\t' && c != '\n' && c != '\r' {
			return invalid("/description", "INVALID_DESCRIPTION")
		}
	}
	return nil
}
func ValidateValue(v string) error {
	if len(v) > MaxValueBytes || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
		return invalid("/value", "INVALID_VALUE")
	}
	return nil
}

// Fields are explicit wire/storage projections. Do not log them.
type VariableFields struct {
	ID          VariableID `json:"id"`
	ProjectID   ProjectID  `json:"project_id"`
	Type        string     `json:"type"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Value       string     `json:"value"`
	Version     f.Version  `json:"version"`
	CreatedAt   f.Instant  `json:"created_at"`
	UpdatedAt   f.Instant  `json:"updated_at"`
}
type Variable struct{ data func() VariableFields }

func NewVariable(v VariableFields) (Variable, error) {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.Type != VariableType || v.Version.Validate() != nil || v.CreatedAt.Validate() != nil || v.UpdatedAt.Validate() != nil || v.CreatedAt.Time().IsZero() || v.UpdatedAt.Time().Before(v.CreatedAt.Time()) {
		return Variable{}, invalid("", "INVALID_VARIABLE")
	}
	for _, err := range []error{ValidateName(v.Name), ValidateDescription(v.Description), ValidateValue(v.Value)} {
		if err != nil {
			return Variable{}, err
		}
	}
	return Variable{func() VariableFields { return v }}, nil
}
func (v Variable) Fields() VariableFields {
	if v.data == nil {
		return VariableFields{}
	}
	return v.data()
}
func (v Variable) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_VARIABLE")
	}
	_, err := NewVariable(v.data())
	return err
}
func (v Variable) Clone() Variable { return v }
func (v Variable) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v.data())
}
func (v *Variable) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decode[VariableFields](raw, []string{"id", "project_id", "type", "name", "description", "value", "version", "created_at", "updated_at"}, nil, nil, MaxReceiptBytes)
	if err != nil {
		return err
	}
	next, err := NewVariable(n)
	if err == nil {
		*v = next
	}
	return err
}
func (Variable) Format(w fmt.State, _ rune) { safe(w) }
func (Variable) LogValue() slog.Value       { return safeLog() }

type VariableSummaryFields struct {
	ID          VariableID `json:"id"`
	ProjectID   ProjectID  `json:"project_id"`
	Type        string     `json:"type"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Version     f.Version  `json:"version"`
	CreatedAt   f.Instant  `json:"created_at"`
	UpdatedAt   f.Instant  `json:"updated_at"`
}
type VariableSummary struct{ data func() VariableSummaryFields }

func NewVariableSummary(v VariableSummaryFields) (VariableSummary, error) {
	_, err := NewVariable(VariableFields{v.ID, v.ProjectID, v.Type, v.Name, v.Description, "", v.Version, v.CreatedAt, v.UpdatedAt})
	if err != nil {
		return VariableSummary{}, err
	}
	return VariableSummary{func() VariableSummaryFields { return v }}, nil
}
func (v Variable) Summary() VariableSummary {
	x := v.Fields()
	n, _ := NewVariableSummary(VariableSummaryFields{x.ID, x.ProjectID, x.Type, x.Name, x.Description, x.Version, x.CreatedAt, x.UpdatedAt})
	return n
}
func (v VariableSummary) Fields() VariableSummaryFields {
	if v.data == nil {
		return VariableSummaryFields{}
	}
	return v.data()
}
func (v VariableSummary) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_VARIABLE")
	}
	_, err := NewVariableSummary(v.data())
	return err
}
func (v VariableSummary) Clone() VariableSummary { return v }
func (v VariableSummary) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v.data())
}
func (v *VariableSummary) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decode[VariableSummaryFields](raw, []string{"id", "project_id", "type", "name", "description", "version", "created_at", "updated_at"}, nil, nil, 32768)
	if err != nil {
		return err
	}
	next, err := NewVariableSummary(n)
	if err == nil {
		*v = next
	}
	return err
}
func (VariableSummary) Format(w fmt.State, _ rune) { safe(w) }
func (VariableSummary) LogValue() slog.Value       { return safeLog() }

// Raw validation precedes encoding/json's replacement of isolated UTF-16 surrogates.
func strict(raw []byte, limit int) error {
	if len(raw) > limit || !utf8.Valid(raw) {
		return invalid("", "INVALID_ENCODING")
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
			return invalid("", "INVALID_ENCODING")
		}
		if raw[n] != 'u' {
			continue
		}
		if n+4 >= len(raw) {
			return invalid("", "INVALID_ENCODING")
		}
		x, err := strconv.ParseUint(string(raw[n+1:n+5]), 16, 16)
		if err != nil {
			return invalid("", "INVALID_ENCODING")
		}
		n += 4
		if x >= 0xdc00 && x <= 0xdfff {
			return invalid("", "INVALID_ENCODING")
		}
		if x >= 0xd800 && x <= 0xdbff {
			if n+6 >= len(raw) || raw[n+1] != '\\' || raw[n+2] != 'u' {
				return invalid("", "INVALID_ENCODING")
			}
			y, err := strconv.ParseUint(string(raw[n+3:n+7]), 16, 16)
			if err != nil || y < 0xdc00 || y > 0xdfff {
				return invalid("", "INVALID_ENCODING")
			}
			n += 6
		}
	}
	if _, err := cursor.CanonicalJSON(raw); err != nil {
		return invalid("", "INVALID_ENCODING")
	}
	return nil
}
func fields(raw []byte, required, optional, nullable []string, limit int) (map[string]json.RawMessage, error) {
	if err := strict(raw, limit); err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, invalid("", "INVALID_ENCODING")
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			return nil, invalid("/"+k, "REQUIRED")
		}
	}
	for k, v := range m {
		if !slices.Contains(required, k) && !slices.Contains(optional, k) || bytes.Equal(bytes.TrimSpace(v), []byte("null")) && !slices.Contains(nullable, k) {
			return nil, invalid("/"+k, "INVALID_FIELD")
		}
	}
	return m, nil
}
func decode[T any](raw []byte, required, optional, nullable []string, limit int) (T, error) {
	var n T
	if _, err := fields(raw, required, optional, nullable, limit); err != nil {
		return n, err
	}
	if json.Unmarshal(raw, &n) != nil {
		return n, invalid("", "INVALID_ENCODING")
	}
	return n, nil
}
