package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type CommandName string

const (
	CreateCommand CommandName = "project.variable.create"
	UpdateCommand CommandName = "project.variable.update"
	DeleteCommand CommandName = "project.variable.delete"
)

func (n CommandName) Validate() error {
	if n != CreateCommand && n != UpdateCommand && n != DeleteCommand {
		return invalid("/command", "INVALID_COMMAND")
	}
	return nil
}
func (n CommandName) MarshalJSON() ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(string(n))
}
func (n *CommandName) UnmarshalJSON(raw []byte) error {
	if n == nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if strict(raw, 128) != nil || json.Unmarshal(raw, &s) != nil || CommandName(s).Validate() != nil {
		return invalid("/command", "INVALID_COMMAND")
	}
	*n = CommandName(s)
	return nil
}

// Fields are explicit request projections, not logging values.
type VariableCreateFields struct {
	ID          VariableID `json:"variable_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Value       string     `json:"value"`
}
type VariableCreate struct{ data func() VariableCreateFields }

func NewVariableCreate(v VariableCreateFields) (VariableCreate, error) {
	if v.ID.Validate() != nil {
		return VariableCreate{}, invalid("/variable_id", "INVALID_ID")
	}
	for _, err := range []error{ValidateName(v.Name), ValidateDescription(v.Description), ValidateValue(v.Value)} {
		if err != nil {
			return VariableCreate{}, err
		}
	}
	return VariableCreate{func() VariableCreateFields { return v }}, nil
}
func (v VariableCreate) Fields() VariableCreateFields {
	if v.data == nil {
		return VariableCreateFields{}
	}
	return v.data()
}
func (v VariableCreate) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_REQUEST")
	}
	_, err := NewVariableCreate(v.data())
	return err
}
func (v VariableCreate) Clone() VariableCreate { return v }
func (v VariableCreate) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v.data())
}
func (v *VariableCreate) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decode[VariableCreateFields](raw, []string{"variable_id", "name", "description", "value"}, nil, nil, MaxRequestBytes)
	if err != nil {
		return err
	}
	next, err := NewVariableCreate(n)
	if err == nil {
		*v = next
	}
	return err
}
func (VariableCreate) Format(w fmt.State, _ rune) { safe(w) }
func (VariableCreate) LogValue() slog.Value       { return safeLog() }

type VariableUpdateFields struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Value       *string `json:"value,omitempty"`
}

func cloneString(v *string) *string {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}
func cloneUpdate(v VariableUpdateFields) VariableUpdateFields {
	return VariableUpdateFields{cloneString(v.Name), cloneString(v.Description), cloneString(v.Value)}
}

type VariableUpdate struct{ data func() VariableUpdateFields }

func NewVariableUpdate(v VariableUpdateFields) (VariableUpdate, error) {
	if v.Name == nil && v.Description == nil && v.Value == nil {
		return VariableUpdate{}, invalid("", "EMPTY_UPDATE")
	}
	if v.Name != nil {
		if err := ValidateName(*v.Name); err != nil {
			return VariableUpdate{}, err
		}
	}
	if v.Description != nil {
		if err := ValidateDescription(*v.Description); err != nil {
			return VariableUpdate{}, err
		}
	}
	if v.Value != nil {
		if err := ValidateValue(*v.Value); err != nil {
			return VariableUpdate{}, err
		}
	}
	v = cloneUpdate(v)
	return VariableUpdate{func() VariableUpdateFields { return cloneUpdate(v) }}, nil
}
func (v VariableUpdate) Fields() VariableUpdateFields {
	if v.data == nil {
		return VariableUpdateFields{}
	}
	return v.data()
}
func (v VariableUpdate) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_REQUEST")
	}
	_, err := NewVariableUpdate(v.data())
	return err
}
func (v VariableUpdate) Clone() VariableUpdate { return v }
func (v VariableUpdate) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v.data())
}
func (v *VariableUpdate) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decode[VariableUpdateFields](raw, nil, []string{"name", "description", "value"}, nil, MaxRequestBytes)
	if err != nil {
		return err
	}
	next, err := NewVariableUpdate(n)
	if err == nil {
		*v = next
	}
	return err
}
func (VariableUpdate) Format(w fmt.State, _ rune) { safe(w) }
func (VariableUpdate) LogValue() slog.Value       { return safeLog() }

func ValidateCommandMeta(n CommandName, m f.CommandMeta) error {
	if n.Validate() != nil || m.Validate() != nil || (n == CreateCommand) != (m.ExpectedVersion == nil) {
		return invalid("/expected_version", "INVALID_COMMAND_META")
	}
	return nil
}
func VariableCommandIdentity(p ProjectID, n CommandName, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if p.Validate() != nil || n.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return f.NewCommandIdentity("projectvariable", []string{p.String()}, string(n), key)
}
func VariableCommandDigest(a i.Actor, m f.CommandMeta, p ProjectID, target VariableID, n CommandName, request any) (f.Digest, error) {
	if a.Validate() != nil || a.Details().Kind != i.Human || ValidateCommandMeta(n, m) != nil || p.Validate() != nil || target.Validate() != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	switch n {
	case CreateCommand:
		r, ok := request.(VariableCreate)
		if !ok || r.Validate() != nil || r.Fields().ID != target {
			return "", invalid("", "INVALID_REQUEST")
		}
	case UpdateCommand:
		r, ok := request.(VariableUpdate)
		if !ok || r.Validate() != nil {
			return "", invalid("", "INVALID_REQUEST")
		}
	case DeleteCommand:
		if request != nil {
			return "", invalid("", "INVALID_REQUEST")
		}
	}
	raw, err := json.Marshal(struct {
		Format   string      `json:"format"`
		Command  CommandName `json:"command"`
		Project  ProjectID   `json:"project_id"`
		Target   VariableID  `json:"target_id"`
		User     string      `json:"actor_user_id"`
		Expected *f.Version  `json:"expected_version,omitempty"`
		Request  any         `json:"request,omitempty"`
	}{"project-variable-v1", n, p, target, a.Details().UserID, m.ExpectedVersion, request})
	if err != nil {
		return "", err
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

type VariableDeleted struct {
	ID        VariableID `json:"id"`
	ProjectID ProjectID  `json:"project_id"`
	Type      string     `json:"type"`
	Version   f.Version  `json:"version"`
	DeletedAt f.Instant  `json:"deleted_at"`
}

func (v VariableDeleted) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.Type != VariableType || v.Version < 2 || v.Version.Validate() != nil || v.DeletedAt.Validate() != nil || v.DeletedAt.Time().IsZero() {
		return invalid("", "INVALID_DELETED")
	}
	return nil
}
func (v VariableDeleted) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	type wire VariableDeleted
	return json.Marshal(wire(v))
}
func (v *VariableDeleted) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire VariableDeleted
	n, err := decode[wire](raw, []string{"id", "project_id", "type", "version", "deleted_at"}, nil, nil, 2048)
	if err != nil {
		return err
	}
	next := VariableDeleted(n)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

type VariableMutationFields struct {
	Command  CommandName
	Changed  bool
	Variable Variable
	Deleted  *VariableDeleted
	EventID  *event.EventID
	AuditID  *audit.ID
}
type VariableMutation struct{ data func() VariableMutationFields }

func cloneMutation(v VariableMutationFields) VariableMutationFields {
	if v.Deleted != nil {
		n := *v.Deleted
		v.Deleted = &n
	}
	if v.EventID != nil {
		n := *v.EventID
		v.EventID = &n
	}
	if v.AuditID != nil {
		n := *v.AuditID
		v.AuditID = &n
	}
	return v
}
func NewVariableMutation(v VariableMutationFields) (VariableMutation, error) {
	bad := func() (VariableMutation, error) { return VariableMutation{}, invalid("", "INVALID_RECEIPT") }
	if v.Command.Validate() != nil || v.Changed != (v.EventID != nil) || v.Changed != (v.AuditID != nil) || v.EventID != nil && v.EventID.Validate() != nil || v.AuditID != nil && v.AuditID.Validate() != nil {
		return bad()
	}
	if v.Command == DeleteCommand {
		if !v.Changed || v.Deleted == nil || v.Deleted.Validate() != nil || v.Variable.data != nil {
			return bad()
		}
	} else {
		if v.Deleted != nil || v.Variable.Validate() != nil {
			return bad()
		}
		version := v.Variable.Fields().Version
		if v.Command == CreateCommand && (!v.Changed || version != 1) || v.Command == UpdateCommand && v.Changed && version < 2 {
			return bad()
		}
	}
	v = cloneMutation(v)
	return VariableMutation{func() VariableMutationFields { return cloneMutation(v) }}, nil
}
func (v VariableMutation) Fields() VariableMutationFields {
	if v.data == nil {
		return VariableMutationFields{}
	}
	return v.data()
}
func (v VariableMutation) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_RECEIPT")
	}
	_, err := NewVariableMutation(v.data())
	return err
}
func (v VariableMutation) Clone() VariableMutation { return v }
func (v VariableMutation) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	d := v.data()
	if d.Command == DeleteCommand {
		return json.Marshal(struct {
			Command CommandName      `json:"command"`
			Changed bool             `json:"changed"`
			Deleted *VariableDeleted `json:"deleted"`
			EventID *event.EventID   `json:"event_id"`
			AuditID *audit.ID        `json:"audit_id"`
		}{d.Command, d.Changed, d.Deleted, d.EventID, d.AuditID})
	}
	return json.Marshal(struct {
		Command  CommandName    `json:"command"`
		Changed  bool           `json:"changed"`
		Variable Variable       `json:"variable"`
		EventID  *event.EventID `json:"event_id"`
		AuditID  *audit.ID      `json:"audit_id"`
	}{d.Command, d.Changed, d.Variable, d.EventID, d.AuditID})
}
func (v *VariableMutation) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	m, err := fields(raw, []string{"command", "changed", "event_id", "audit_id"}, []string{"variable", "deleted"}, []string{"event_id", "audit_id"}, MaxReceiptBytes)
	if err != nil {
		return err
	}
	var d VariableMutationFields
	if json.Unmarshal(m["command"], &d.Command) != nil || json.Unmarshal(m["changed"], &d.Changed) != nil || json.Unmarshal(m["event_id"], &d.EventID) != nil || json.Unmarshal(m["audit_id"], &d.AuditID) != nil {
		return invalid("", "INVALID_RECEIPT")
	}
	if d.Command == DeleteCommand {
		if len(m["variable"]) != 0 || len(m["deleted"]) == 0 || json.Unmarshal(m["deleted"], &d.Deleted) != nil {
			return invalid("", "INVALID_RECEIPT")
		}
	} else {
		if len(m["deleted"]) != 0 || json.Unmarshal(m["variable"], &d.Variable) != nil {
			return invalid("", "INVALID_RECEIPT")
		}
	}
	next, err := NewVariableMutation(d)
	if err == nil {
		*v = next
	}
	return err
}
func (VariableMutation) Format(w fmt.State, _ rune) { safe(w) }
func (VariableMutation) LogValue() slog.Value       { return safeLog() }

type Commands interface {
	CreateVariable(context.Context, i.Actor, f.CommandMeta, ProjectID, VariableCreate) (VariableMutation, error)
	UpdateVariable(context.Context, i.Actor, f.CommandMeta, ProjectID, VariableID, VariableUpdate) (VariableMutation, error)
	DeleteVariable(context.Context, i.Actor, f.CommandMeta, ProjectID, VariableID) (VariableMutation, error)
	LookupVariableCommand(context.Context, i.Actor, VariableCommandLookupRequest) (VariableCommandLookup, error)
}
