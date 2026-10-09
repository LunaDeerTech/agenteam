package contract

import (
	"context"
	"encoding/json"
	"fmt"
	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"log/slog"
)

type SecretCommandName string

const (
	SecretCreateCommand SecretCommandName = "project.secret_variable.create"
	SecretUpdateCommand SecretCommandName = "project.secret_variable.update"
	SecretDeleteCommand SecretCommandName = "project.secret_variable.delete"
)

func (n SecretCommandName) Validate() error {
	if n != SecretCreateCommand && n != SecretUpdateCommand && n != SecretDeleteCommand {
		return invalid("/command", "INVALID_COMMAND")
	}
	return nil
}
func (n SecretCommandName) MarshalJSON() ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(string(n))
}
func (n *SecretCommandName) UnmarshalJSON(raw []byte) error {
	if n == nil {
		return invalid("", "INVALID_ENCODING")
	}
	var s string
	if strict(raw, 128) != nil || json.Unmarshal(raw, &s) != nil || SecretCommandName(s).Validate() != nil {
		return invalid("/command", "INVALID_COMMAND")
	}
	*n = SecretCommandName(s)
	return nil
}

func ValidateSecretCommandMeta(n SecretCommandName, m f.CommandMeta) error {
	if n.Validate() != nil || m.Validate() != nil || (n == SecretCreateCommand) != (m.ExpectedVersion == nil) {
		return invalid("/expected_version", "INVALID_COMMAND_META")
	}
	return nil
}
func SecretVariableCommandIdentity(p ProjectID, n SecretCommandName, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if p.Validate() != nil || n.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return f.NewCommandIdentity("projectvariable", []string{p.String()}, string(n), key)
}

type SecretVariableDeleted struct {
	ID        VariableID `json:"id"`
	ProjectID ProjectID  `json:"project_id"`
	Type      string     `json:"type"`
	Version   f.Version  `json:"version"`
	DeletedAt f.Instant  `json:"deleted_at"`
}

func (v SecretVariableDeleted) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.Type != SecretVariableType || v.Version < 2 || v.Version.Validate() != nil || v.DeletedAt.Validate() != nil || v.DeletedAt.Time().IsZero() {
		return invalid("", "INVALID_DELETED")
	}
	return nil
}
func (v SecretVariableDeleted) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	type wire SecretVariableDeleted
	return json.Marshal(wire(v))
}
func (v *SecretVariableDeleted) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire SecretVariableDeleted
	n, err := decodeSecret[wire](raw, []string{"id", "project_id", "type", "version", "deleted_at"}, nil, nil, 2048)
	if err != nil {
		return err
	}
	next := SecretVariableDeleted(n)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

type SecretVariableMutationFields struct {
	Command  SecretCommandName
	Changed  bool
	Variable SecretVariable
	Deleted  *SecretVariableDeleted
	EventID  *event.EventID
	AuditID  *audit.ID
}
type SecretVariableMutation struct {
	data func() SecretVariableMutationFields
}

func cloneSecretMutation(v SecretVariableMutationFields) SecretVariableMutationFields {
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
func NewSecretVariableMutation(v SecretVariableMutationFields) (SecretVariableMutation, error) {
	bad := func() (SecretVariableMutation, error) {
		return SecretVariableMutation{}, invalid("", "INVALID_RECEIPT")
	}
	if v.Command.Validate() != nil || v.Changed != (v.EventID != nil) || v.Changed != (v.AuditID != nil) || v.EventID != nil && v.EventID.Validate() != nil || v.AuditID != nil && v.AuditID.Validate() != nil {
		return bad()
	}
	if v.Command == SecretDeleteCommand {
		if !v.Changed || v.Deleted == nil || v.Deleted.Validate() != nil || v.Variable.data != nil {
			return bad()
		}
	} else {
		if v.Deleted != nil || v.Variable.Validate() != nil {
			return bad()
		}
		version := v.Variable.Fields().Version
		if v.Command == SecretCreateCommand && (!v.Changed || version != 1) || v.Command == SecretUpdateCommand && v.Changed && version < 2 {
			return bad()
		}
	}
	v = cloneSecretMutation(v)
	return SecretVariableMutation{func() SecretVariableMutationFields { return cloneSecretMutation(v) }}, nil
}
func (v SecretVariableMutation) Fields() SecretVariableMutationFields {
	if v.data == nil {
		return SecretVariableMutationFields{}
	}
	return v.data()
}
func (v SecretVariableMutation) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_RECEIPT")
	}
	_, err := NewSecretVariableMutation(v.data())
	return err
}
func (v SecretVariableMutation) Clone() SecretVariableMutation { return v }
func (v SecretVariableMutation) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	d := v.data()
	if d.Command == SecretDeleteCommand {
		return json.Marshal(struct {
			Command SecretCommandName      `json:"command"`
			Changed bool                   `json:"changed"`
			Deleted *SecretVariableDeleted `json:"deleted"`
			EventID *event.EventID         `json:"event_id"`
			AuditID *audit.ID              `json:"audit_id"`
		}{d.Command, d.Changed, d.Deleted, d.EventID, d.AuditID})
	}
	return json.Marshal(struct {
		Command  SecretCommandName `json:"command"`
		Changed  bool              `json:"changed"`
		Variable SecretVariable    `json:"variable"`
		EventID  *event.EventID    `json:"event_id"`
		AuditID  *audit.ID         `json:"audit_id"`
	}{d.Command, d.Changed, d.Variable, d.EventID, d.AuditID})
}
func (v *SecretVariableMutation) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	m, err := secretFields(raw, []string{"command", "changed", "event_id", "audit_id"}, []string{"variable", "deleted"}, []string{"event_id", "audit_id"}, MaxSecretReceiptBytes)
	if err != nil {
		return err
	}
	var d SecretVariableMutationFields
	if json.Unmarshal(m["command"], &d.Command) != nil || json.Unmarshal(m["changed"], &d.Changed) != nil || json.Unmarshal(m["event_id"], &d.EventID) != nil || json.Unmarshal(m["audit_id"], &d.AuditID) != nil {
		return invalid("", "INVALID_RECEIPT")
	}
	if d.Command == SecretDeleteCommand {
		if len(m["variable"]) != 0 || len(m["deleted"]) == 0 || json.Unmarshal(m["deleted"], &d.Deleted) != nil {
			return invalid("", "INVALID_RECEIPT")
		}
	} else {
		if len(m["deleted"]) != 0 || json.Unmarshal(m["variable"], &d.Variable) != nil {
			return invalid("", "INVALID_RECEIPT")
		}
	}
	next, err := NewSecretVariableMutation(d)
	if err == nil {
		*v = next
	}
	return err
}
func (SecretVariableMutation) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretVariableMutation) LogValue() slog.Value       { return secretLog() }

type SecretCommands interface {
	CreateSecretVariable(context.Context, i.Actor, f.CommandMeta, ProjectID, SecretVariableCreate) (SecretVariableMutation, error)
	UpdateSecretVariable(context.Context, i.Actor, f.CommandMeta, ProjectID, VariableID, SecretVariableUpdate) (SecretVariableMutation, error)
	DeleteSecretVariable(context.Context, i.Actor, f.CommandMeta, ProjectID, VariableID) (SecretVariableMutation, error)
	LookupSecretVariableCommand(context.Context, i.Actor, SecretVariableCommandLookupRequest) (SecretVariableCommandLookup, error)
}
