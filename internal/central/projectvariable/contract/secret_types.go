package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const (
	SecretVariableType    = "secret"
	MaxSecretValueBytes   = 65536
	MaxSecretVariables    = 4096
	MaxSecretRequestBytes = 1 << 20
	MaxSecretReceiptBytes = 1 << 20
	MaxSecretListBytes    = 5 << 20
	MaxSecretReferences   = 256
)

func secretSafe(w io.Writer) { _, _ = io.WriteString(w, "secret_variable") }
func secretLog() slog.Value  { return slog.StringValue("secret_variable") }

// Shared strict decoding remains unchanged for ordinary variables. Secret
// decoders expose only declared schema paths: an unknown JSON member name may
// itself contain material and must never become a public Fault path.
func secretFields(raw []byte, required, optional, nullable []string, limit int) (map[string]json.RawMessage, error) {
	m, err := fields(raw, required, optional, nullable, limit)
	if err == nil {
		return m, nil
	}
	var fault *f.Fault
	if !errors.As(err, &fault) {
		return nil, invalid("", "INVALID_ENCODING")
	}
	for _, field := range fault.FieldErrors {
		known := field.Path == ""
		for _, name := range required {
			known = known || field.Path == "/"+name
		}
		for _, name := range optional {
			known = known || field.Path == "/"+name
		}
		if !known {
			return nil, invalid("", "INVALID_FIELD")
		}
	}
	return nil, err
}

func decodeSecret[T any](raw []byte, required, optional, nullable []string, limit int) (T, error) {
	var value T
	if _, err := secretFields(raw, required, optional, nullable, limit); err != nil {
		return value, err
	}
	if json.Unmarshal(raw, &value) != nil {
		return value, invalid("", "INVALID_ENCODING")
	}
	return value, nil
}

// SecretVariable has deliberately no material, credential reference or value
// fingerprint. Detail, list and historical receipts use this same safe shape.
type SecretVariableFields struct {
	ID          VariableID `json:"id"`
	ProjectID   ProjectID  `json:"project_id"`
	Type        string     `json:"type"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Version     f.Version  `json:"version"`
	CreatedAt   f.Instant  `json:"created_at"`
	UpdatedAt   f.Instant  `json:"updated_at"`
}
type SecretVariable struct{ data func() SecretVariableFields }

func NewSecretVariable(v SecretVariableFields) (SecretVariable, error) {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.Type != SecretVariableType || v.Version.Validate() != nil || v.CreatedAt.Validate() != nil || v.UpdatedAt.Validate() != nil || v.CreatedAt.Time().IsZero() || v.UpdatedAt.Time().Before(v.CreatedAt.Time()) {
		return SecretVariable{}, invalid("", "INVALID_SECRET_VARIABLE")
	}
	if err := ValidateName(v.Name); err != nil {
		return SecretVariable{}, err
	}
	if err := ValidateDescription(v.Description); err != nil {
		return SecretVariable{}, err
	}
	return SecretVariable{func() SecretVariableFields { return v }}, nil
}
func (v SecretVariable) Fields() SecretVariableFields {
	if v.data == nil {
		return SecretVariableFields{}
	}
	return v.data()
}
func (v SecretVariable) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_SECRET_VARIABLE")
	}
	_, err := NewSecretVariable(v.data())
	return err
}
func (v SecretVariable) Clone() SecretVariable { return v }
func (v SecretVariable) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v.data())
}
func (v *SecretVariable) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decodeSecret[SecretVariableFields](raw, []string{"id", "project_id", "type", "name", "description", "version", "created_at", "updated_at"}, nil, nil, MaxSecretReceiptBytes)
	if err != nil {
		return err
	}
	next, err := NewSecretVariable(n)
	if err == nil {
		*v = next
	}
	return err
}
func (SecretVariable) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretVariable) LogValue() slog.Value       { return secretLog() }

// Values are bytes, not normalizable text. This validates the UTF-8 wire rule
// without producing a printable value or digest.
func ValidateSecretValue(value []byte) error {
	if len(value) < 1 || len(value) > MaxSecretValueBytes || !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 {
		return invalid("/value", "INVALID_SECRET_VALUE")
	}
	return nil
}
func cloneSecretMaterial(m sc.SecretMaterial) (sc.SecretMaterial, error) {
	var copy sc.SecretMaterial
	err := m.Use(func(value []byte) error {
		if err := ValidateSecretValue(value); err != nil {
			return err
		}
		var err error
		copy, err = sc.NewSecretMaterial(value)
		return err
	})
	if err != nil {
		return sc.SecretMaterial{}, invalid("/value", "INVALID_SECRET_VALUE")
	}
	return copy, nil
}
func validateSecretMaterial(m sc.SecretMaterial) error {
	if err := m.Use(ValidateSecretValue); err != nil {
		return invalid("/value", "INVALID_SECRET_VALUE")
	}
	return nil
}

type SecretVariableCreateFields struct {
	ID                VariableID
	Name, Description string
	Value             sc.SecretMaterial
}
type SecretVariableCreateMetadata struct {
	ID                VariableID
	Name, Description string
}
type secretCreateData struct {
	metadata SecretVariableCreateMetadata
	value    sc.SecretMaterial
}
type SecretVariableCreate struct{ data func() secretCreateData }

func NewSecretVariableCreate(v SecretVariableCreateFields) (SecretVariableCreate, error) {
	if v.ID.Validate() != nil {
		return SecretVariableCreate{}, invalid("/variable_id", "INVALID_ID")
	}
	if err := ValidateName(v.Name); err != nil {
		return SecretVariableCreate{}, err
	}
	if err := ValidateDescription(v.Description); err != nil {
		return SecretVariableCreate{}, err
	}
	m, err := cloneSecretMaterial(v.Value)
	if err != nil {
		return SecretVariableCreate{}, err
	}
	d := secretCreateData{SecretVariableCreateMetadata{v.ID, v.Name, v.Description}, m}
	return SecretVariableCreate{func() secretCreateData { return d }}, nil
}
func (v SecretVariableCreate) Fields() SecretVariableCreateMetadata {
	if v.data == nil {
		return SecretVariableCreateMetadata{}
	}
	return v.data().metadata
}
func (v SecretVariableCreate) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_REQUEST")
	}
	return validateSecretMaterial(v.data().value)
}
func (v SecretVariableCreate) UseValue(fn func([]byte) error) error {
	if err := v.Validate(); err != nil {
		return err
	}
	return v.data().value.Use(fn)
}
func (v SecretVariableCreate) Clone() (SecretVariableCreate, error) {
	if err := v.Validate(); err != nil {
		return SecretVariableCreate{}, err
	}
	d := v.data()
	return NewSecretVariableCreate(SecretVariableCreateFields{d.metadata.ID, d.metadata.Name, d.metadata.Description, d.value})
}
func (v SecretVariableCreate) Destroy() {
	if v.data != nil {
		v.data().value.Destroy()
	}
}

// JSON encoding is intentionally a safe marker. Only the strict request decoder
// accepts plaintext; callers must use UseValue for synchronous backend work.
func (SecretVariableCreate) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_variable_request"`), nil
}
func (v *SecretVariableCreate) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	m, err := secretFields(raw, []string{"variable_id", "name", "description", "value"}, nil, nil, MaxSecretRequestBytes)
	if err != nil {
		return err
	}
	var d SecretVariableCreateFields
	if json.Unmarshal(m["variable_id"], &d.ID) != nil || json.Unmarshal(m["name"], &d.Name) != nil || json.Unmarshal(m["description"], &d.Description) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	material, err := decodeSecretMaterial(m["value"])
	if err != nil {
		return err
	}
	defer material.Destroy()
	d.Value = material
	next, err := NewSecretVariableCreate(d)
	if err == nil {
		v.Destroy()
		*v = next
	}
	return err
}
func (SecretVariableCreate) Format(w fmt.State, _ rune)       { secretSafe(w) }
func (SecretVariableCreate) LogValue() slog.Value             { return secretLog() }
func (SecretVariableCreateFields) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretVariableCreateFields) LogValue() slog.Value       { return secretLog() }

type SecretVariableUpdateFields struct {
	Name, Description *string
	Value             *sc.SecretMaterial
}
type SecretVariableUpdateMetadata struct {
	Name, Description *string
	ValuePresent      bool
}
type secretUpdateData struct {
	metadata SecretVariableUpdateMetadata
	value    sc.SecretMaterial
}
type SecretVariableUpdate struct{ data func() secretUpdateData }

func cloneSecretUpdateMetadata(v SecretVariableUpdateMetadata) SecretVariableUpdateMetadata {
	return SecretVariableUpdateMetadata{cloneString(v.Name), cloneString(v.Description), v.ValuePresent}
}
func NewSecretVariableUpdate(v SecretVariableUpdateFields) (SecretVariableUpdate, error) {
	if v.Name == nil && v.Description == nil && v.Value == nil {
		return SecretVariableUpdate{}, invalid("", "EMPTY_UPDATE")
	}
	if v.Name != nil {
		if err := ValidateName(*v.Name); err != nil {
			return SecretVariableUpdate{}, err
		}
	}
	if v.Description != nil {
		if err := ValidateDescription(*v.Description); err != nil {
			return SecretVariableUpdate{}, err
		}
	}
	d := secretUpdateData{metadata: cloneSecretUpdateMetadata(SecretVariableUpdateMetadata{v.Name, v.Description, v.Value != nil})}
	if v.Value != nil {
		var err error
		d.value, err = cloneSecretMaterial(*v.Value)
		if err != nil {
			return SecretVariableUpdate{}, err
		}
	}
	return SecretVariableUpdate{func() secretUpdateData { n := d; n.metadata = cloneSecretUpdateMetadata(d.metadata); return n }}, nil
}
func (v SecretVariableUpdate) Fields() SecretVariableUpdateMetadata {
	if v.data == nil {
		return SecretVariableUpdateMetadata{}
	}
	return v.data().metadata
}
func (v SecretVariableUpdate) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_REQUEST")
	}
	d := v.data()
	if d.metadata.ValuePresent {
		return validateSecretMaterial(d.value)
	}
	return nil
}
func (v SecretVariableUpdate) UseValue(fn func([]byte) error) error {
	if err := v.Validate(); err != nil {
		return err
	}
	d := v.data()
	if !d.metadata.ValuePresent {
		return invalid("/value", "VALUE_NOT_PRESENT")
	}
	return d.value.Use(fn)
}
func (v SecretVariableUpdate) Clone() (SecretVariableUpdate, error) {
	if err := v.Validate(); err != nil {
		return SecretVariableUpdate{}, err
	}
	d := v.data()
	fields := SecretVariableUpdateFields{Name: d.metadata.Name, Description: d.metadata.Description}
	if d.metadata.ValuePresent {
		fields.Value = &d.value
	}
	return NewSecretVariableUpdate(fields)
}
func (v SecretVariableUpdate) Destroy() {
	if v.data != nil {
		v.data().value.Destroy()
	}
}
func (SecretVariableUpdate) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_variable_request"`), nil
}
func (v *SecretVariableUpdate) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	m, err := secretFields(raw, nil, []string{"name", "description", "value"}, nil, MaxSecretRequestBytes)
	if err != nil {
		return err
	}
	var d SecretVariableUpdateFields
	if raw, ok := m["name"]; ok {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return invalid("/name", "INVALID_ENCODING")
		}
		d.Name = &s
	}
	if raw, ok := m["description"]; ok {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return invalid("/description", "INVALID_ENCODING")
		}
		d.Description = &s
	}
	if raw, ok := m["value"]; ok {
		material, err := decodeSecretMaterial(raw)
		if err != nil {
			return err
		}
		defer material.Destroy()
		d.Value = &material
	}
	next, err := NewSecretVariableUpdate(d)
	if err == nil {
		v.Destroy()
		*v = next
	}
	return err
}
func (SecretVariableUpdate) Format(w fmt.State, _ rune)       { secretSafe(w) }
func (SecretVariableUpdate) LogValue() slog.Value             { return secretLog() }
func (SecretVariableUpdateFields) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretVariableUpdateFields) LogValue() slog.Value       { return secretLog() }

func decodeSecretMaterial(raw []byte) (sc.SecretMaterial, error) {
	// encoding/json temporarily allocates an immutable string. Keep it local;
	// owned byte copies are cleared, but Go/GC copies cannot be promised erased.
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return sc.SecretMaterial{}, invalid("/value", "INVALID_SECRET_VALUE")
	}
	value := []byte(text)
	defer clear(value)
	if err := ValidateSecretValue(value); err != nil {
		return sc.SecretMaterial{}, err
	}
	return sc.NewSecretMaterial(value)
}
