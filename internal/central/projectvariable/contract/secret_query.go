package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type SecretQueries interface {
	GetSecretVariable(context.Context, i.Actor, ProjectID, VariableID) (SecretVariable, error)
	ListSecretVariables(context.Context, i.Actor, ProjectID, f.PageRequest) (f.Page[SecretVariable], error)
}

// Identity-only lookup does not assert knowledge of the original material.
// A full write replay must separately match the D04 protected original intent.
type SecretVariableCommandLookupFields struct {
	ProjectID       ProjectID         `json:"project_id"`
	Command         SecretCommandName `json:"command"`
	TargetID        VariableID        `json:"target_id"`
	IdempotencyKey  f.IdempotencyKey  `json:"idempotency_key"`
	ExpectedVersion *f.Version        `json:"expected_version,omitempty"`
}
type SecretVariableCommandLookupRequest struct {
	data func() SecretVariableCommandLookupFields
}

func cloneSecretLookup(v SecretVariableCommandLookupFields) SecretVariableCommandLookupFields {
	if v.ExpectedVersion != nil {
		n := *v.ExpectedVersion
		v.ExpectedVersion = &n
	}
	return v
}
func NewSecretVariableCommandLookupRequest(v SecretVariableCommandLookupFields) (SecretVariableCommandLookupRequest, error) {
	if v.ProjectID.Validate() != nil || v.Command.Validate() != nil || v.TargetID.Validate() != nil || v.IdempotencyKey.Validate() != nil || (v.Command == SecretCreateCommand) != (v.ExpectedVersion == nil) || v.ExpectedVersion != nil && v.ExpectedVersion.Validate() != nil {
		return SecretVariableCommandLookupRequest{}, invalid("", "INVALID_LOOKUP")
	}
	v = cloneSecretLookup(v)
	return SecretVariableCommandLookupRequest{func() SecretVariableCommandLookupFields { return cloneSecretLookup(v) }}, nil
}
func (v SecretVariableCommandLookupRequest) Fields() SecretVariableCommandLookupFields {
	if v.data == nil {
		return SecretVariableCommandLookupFields{}
	}
	return v.data()
}
func (v SecretVariableCommandLookupRequest) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_LOOKUP")
	}
	_, err := NewSecretVariableCommandLookupRequest(v.data())
	return err
}
func (v SecretVariableCommandLookupRequest) Clone() SecretVariableCommandLookupRequest { return v }
func (v SecretVariableCommandLookupRequest) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v.data())
}
func (v *SecretVariableCommandLookupRequest) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decode[SecretVariableCommandLookupFields](raw, []string{"project_id", "command", "target_id", "idempotency_key"}, []string{"expected_version"}, nil, MaxSecretRequestBytes)
	if err != nil {
		return err
	}
	next, err := NewSecretVariableCommandLookupRequest(n)
	if err == nil {
		*v = next
	}
	return err
}
func (SecretVariableCommandLookupRequest) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretVariableCommandLookupRequest) LogValue() slog.Value       { return secretLog() }

type SecretLookupStatus string

const (
	SecretLookupCommitted   SecretLookupStatus = "committed"
	SecretLookupNotObserved SecretLookupStatus = "not_observed"
)

type SecretVariableCommandLookup struct {
	status  SecretLookupStatus
	receipt func() SecretVariableMutation
}

func NewSecretVariableCommandLookup(status SecretLookupStatus, receipt *SecretVariableMutation) (SecretVariableCommandLookup, error) {
	v := SecretVariableCommandLookup{status: status}
	if receipt != nil {
		copy := receipt.Clone()
		v.receipt = func() SecretVariableMutation { return copy }
	}
	if err := v.Validate(); err != nil {
		return SecretVariableCommandLookup{}, err
	}
	return v, nil
}
func (v SecretVariableCommandLookup) Status() SecretLookupStatus { return v.status }
func (v SecretVariableCommandLookup) Receipt() *SecretVariableMutation {
	if v.receipt == nil {
		return nil
	}
	n := v.receipt()
	return &n
}
func (v SecretVariableCommandLookup) Validate() error {
	if v.status == SecretLookupCommitted && v.receipt != nil {
		return v.receipt().Validate()
	}
	if v.status == SecretLookupNotObserved && v.receipt == nil {
		return nil
	}
	return invalid("", "INVALID_LOOKUP")
}
func (v SecretVariableCommandLookup) Clone() SecretVariableCommandLookup { return v }
func (v SecretVariableCommandLookup) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Status  SecretLookupStatus      `json:"status"`
		Receipt *SecretVariableMutation `json:"receipt"`
	}{v.status, v.Receipt()})
}
func (v *SecretVariableCommandLookup) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decode[struct {
		Status  SecretLookupStatus      `json:"status"`
		Receipt *SecretVariableMutation `json:"receipt"`
	}](raw, []string{"status", "receipt"}, nil, []string{"receipt"}, MaxSecretReceiptBytes)
	if err != nil {
		return err
	}
	next, err := NewSecretVariableCommandLookup(n.Status, n.Receipt)
	if err == nil {
		*v = next
	}
	return err
}
func (SecretVariableCommandLookup) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretVariableCommandLookup) LogValue() slog.Value       { return secretLog() }
