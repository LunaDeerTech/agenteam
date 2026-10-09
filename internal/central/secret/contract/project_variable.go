package contract

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ProjectVariableWriteFields contains the original authority request, never
// material, a discovered CredentialRef, or a server-derived version. This is a
// request, not a permission grant; the dedicated provider must authorize it.
type ProjectVariableWriteFields struct {
	Actor           i.Actor
	ProjectID       i.ProjectID
	VariableID      i.ProjectVariableID
	Identity        f.CommandIdentity
	Kind            MutationKind
	ExpectedVersion *f.Version
}

type ProjectVariableWriteRequest struct {
	data func() ProjectVariableWriteFields
}

func cloneProjectVariableWrite(v ProjectVariableWriteFields) ProjectVariableWriteFields {
	if v.ExpectedVersion != nil {
		n := *v.ExpectedVersion
		v.ExpectedVersion = &n
	}
	return v
}

func NewProjectVariableWriteRequest(v ProjectVariableWriteFields) (ProjectVariableWriteRequest, error) {
	if v.Actor.Validate() != nil || v.Actor.Details().Kind != i.Human || v.ProjectID.Validate() != nil || v.VariableID.Validate() != nil || v.Identity.Validate() != nil || v.Identity.Namespace() != "projectvariable" || !slices.Equal(v.Identity.OwnerIDs(), []string{v.ProjectID.String()}) {
		return ProjectVariableWriteRequest{}, bad()
	}
	if v.Kind != Create && v.Kind != Update && v.Kind != Delete || v.Identity.Command() != "project.secret_variable."+string(v.Kind) {
		return ProjectVariableWriteRequest{}, bad()
	}
	if (v.Kind == Create) != (v.ExpectedVersion == nil) || v.ExpectedVersion != nil && v.ExpectedVersion.Validate() != nil {
		return ProjectVariableWriteRequest{}, bad()
	}
	v = cloneProjectVariableWrite(v)
	return ProjectVariableWriteRequest{func() ProjectVariableWriteFields { return cloneProjectVariableWrite(v) }}, nil
}

func (v ProjectVariableWriteRequest) Validate() error {
	if v.data == nil {
		return bad()
	}
	return nil
}
func (v ProjectVariableWriteRequest) Fields() ProjectVariableWriteFields {
	if v.data == nil {
		return ProjectVariableWriteFields{}
	}
	return v.data()
}

type ProjectVariableIntentFields struct {
	Request           ProjectVariableWriteRequest
	Name, Description *string
	Value             *SecretMaterial
}

// Metadata is an explicit backend projection. No value or fingerprint is
// available from it. Pointer fields returned to callers are independent copies.
type ProjectVariableIntentMetadata struct {
	Request           ProjectVariableWriteRequest
	Name, Description *string
	ValuePresent      bool
}

type projectVariableIntentData struct {
	metadata ProjectVariableIntentMetadata
	value    SecretMaterial
}

type ProjectVariableIntent struct {
	data func() projectVariableIntentData
}

func cloneProjectVariableText(v *string) *string {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}
func cloneProjectVariableMetadata(v ProjectVariableIntentMetadata) ProjectVariableIntentMetadata {
	v.Name = cloneProjectVariableText(v.Name)
	v.Description = cloneProjectVariableText(v.Description)
	return v
}

func projectVariableMaterialValid(value []byte) error {
	if len(value) < 1 || len(value) > MaxValueBytes || !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 {
		return bad()
	}
	return nil
}

// NewProjectVariableIntent owns a fresh material copy. Text validation here is
// structural and bounded; D10's authority still owns name syntax, reserved names
// and the shared name space. This constructor confers no write authority.
func NewProjectVariableIntent(v ProjectVariableIntentFields) (ProjectVariableIntent, error) {
	if v.Request.Validate() != nil {
		return ProjectVariableIntent{}, bad()
	}
	if v.Name != nil && (len(*v.Name) < 1 || len(*v.Name) > 128 || !utf8.ValidString(*v.Name) || strings.ContainsRune(*v.Name, 0)) || v.Description != nil && (len(*v.Description) > 4096 || !utf8.ValidString(*v.Description) || strings.ContainsRune(*v.Description, 0)) {
		return ProjectVariableIntent{}, bad()
	}
	switch v.Request.Fields().Kind {
	case Create:
		if v.Name == nil || v.Description == nil || v.Value == nil {
			return ProjectVariableIntent{}, bad()
		}
	case Update:
		if v.Name == nil && v.Description == nil && v.Value == nil {
			return ProjectVariableIntent{}, bad()
		}
	case Delete:
		if v.Name != nil || v.Description != nil || v.Value != nil {
			return ProjectVariableIntent{}, bad()
		}
	default:
		return ProjectVariableIntent{}, bad()
	}
	d := projectVariableIntentData{metadata: cloneProjectVariableMetadata(ProjectVariableIntentMetadata{v.Request, v.Name, v.Description, v.Value != nil})}
	if v.Value != nil {
		err := v.Value.Use(func(value []byte) error {
			if err := projectVariableMaterialValid(value); err != nil {
				return err
			}
			var err error
			d.value, err = NewSecretMaterial(value)
			return err
		})
		if err != nil {
			return ProjectVariableIntent{}, bad()
		}
	}
	return ProjectVariableIntent{func() projectVariableIntentData {
		n := d
		n.metadata = cloneProjectVariableMetadata(d.metadata)
		return n
	}}, nil
}

func (v ProjectVariableIntent) Validate() error {
	if v.data == nil {
		return bad()
	}
	d := v.data()
	if d.metadata.ValuePresent {
		if err := d.value.Use(projectVariableMaterialValid); err != nil {
			return bad()
		}
	}
	return nil
}
func (v ProjectVariableIntent) Fields() ProjectVariableIntentMetadata {
	if v.data == nil {
		return ProjectVariableIntentMetadata{}
	}
	return v.data().metadata
}
func (v ProjectVariableIntent) UseValue(fn func([]byte) error) error {
	if v.data == nil || !v.data().metadata.ValuePresent {
		return bad()
	}
	return v.data().value.Use(fn)
}
func (v ProjectVariableIntent) Clone() (ProjectVariableIntent, error) {
	if err := v.Validate(); err != nil {
		return ProjectVariableIntent{}, err
	}
	d := v.data()
	f := ProjectVariableIntentFields{Request: d.metadata.Request, Name: d.metadata.Name, Description: d.metadata.Description}
	if d.metadata.ValuePresent {
		f.Value = &d.value
	}
	return NewProjectVariableIntent(f)
}
func (v ProjectVariableIntent) Destroy() {
	if v.data != nil {
		v.data().value.Destroy()
	}
}

func projectVariableSafe(w io.Writer)      { _, _ = io.WriteString(w, "project_variable_secret_write") }
func projectVariableLog() slog.Value       { return slog.StringValue("project_variable_secret_write") }
func projectVariableJSON() ([]byte, error) { return []byte(`"project_variable_secret_write"`), nil }

func (ProjectVariableWriteRequest) Format(w fmt.State, _ rune)     { projectVariableSafe(w) }
func (ProjectVariableWriteRequest) LogValue() slog.Value           { return projectVariableLog() }
func (ProjectVariableWriteRequest) MarshalJSON() ([]byte, error)   { return projectVariableJSON() }
func (*ProjectVariableWriteRequest) UnmarshalJSON([]byte) error    { return bad() }
func (ProjectVariableWriteFields) Format(w fmt.State, _ rune)      { projectVariableSafe(w) }
func (ProjectVariableWriteFields) LogValue() slog.Value            { return projectVariableLog() }
func (ProjectVariableWriteFields) MarshalJSON() ([]byte, error)    { return projectVariableJSON() }
func (ProjectVariableIntent) Format(w fmt.State, _ rune)           { projectVariableSafe(w) }
func (ProjectVariableIntent) LogValue() slog.Value                 { return projectVariableLog() }
func (ProjectVariableIntent) MarshalJSON() ([]byte, error)         { return projectVariableJSON() }
func (*ProjectVariableIntent) UnmarshalJSON([]byte) error          { return bad() }
func (ProjectVariableIntentFields) Format(w fmt.State, _ rune)     { projectVariableSafe(w) }
func (ProjectVariableIntentFields) LogValue() slog.Value           { return projectVariableLog() }
func (ProjectVariableIntentFields) MarshalJSON() ([]byte, error)   { return projectVariableJSON() }
func (ProjectVariableIntentMetadata) Format(w fmt.State, _ rune)   { projectVariableSafe(w) }
func (ProjectVariableIntentMetadata) LogValue() slog.Value         { return projectVariableLog() }
func (ProjectVariableIntentMetadata) MarshalJSON() ([]byte, error) { return projectVariableJSON() }
