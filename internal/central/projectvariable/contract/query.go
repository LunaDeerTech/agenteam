package contract

import (
	"context"
	"encoding/json"
	"fmt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"log/slog"
)

type Queries interface {
	GetVariable(context.Context, i.Actor, ProjectID, VariableID) (Variable, error)
	ListVariables(context.Context, i.Actor, ProjectID, f.PageRequest) (f.Page[VariableSummary], error)
}
type VariableCommandLookupFields struct {
	ProjectID      ProjectID        `json:"project_id"`
	Command        CommandName      `json:"command"`
	IdempotencyKey f.IdempotencyKey `json:"idempotency_key"`
	SemanticDigest f.Digest         `json:"semantic_digest"`
}
type VariableCommandLookupRequest struct {
	data func() VariableCommandLookupFields
}

func NewVariableCommandLookupRequest(v VariableCommandLookupFields) (VariableCommandLookupRequest, error) {
	if v.ProjectID.Validate() != nil || v.Command.Validate() != nil || v.IdempotencyKey.Validate() != nil || v.SemanticDigest.Validate() != nil {
		return VariableCommandLookupRequest{}, invalid("", "INVALID_LOOKUP")
	}
	return VariableCommandLookupRequest{func() VariableCommandLookupFields { return v }}, nil
}
func (v VariableCommandLookupRequest) Fields() VariableCommandLookupFields {
	if v.data == nil {
		return VariableCommandLookupFields{}
	}
	return v.data()
}
func (v VariableCommandLookupRequest) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_LOOKUP")
	}
	_, err := NewVariableCommandLookupRequest(v.data())
	return err
}
func (v VariableCommandLookupRequest) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(v.data())
}
func (v *VariableCommandLookupRequest) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decode[VariableCommandLookupFields](raw, []string{"project_id", "command", "idempotency_key", "semantic_digest"}, nil, nil, 2048)
	if err != nil {
		return err
	}
	next, err := NewVariableCommandLookupRequest(n)
	if err == nil {
		*v = next
	}
	return err
}
func (VariableCommandLookupRequest) Format(w fmt.State, _ rune) { safe(w) }
func (VariableCommandLookupRequest) LogValue() slog.Value       { return safeLog() }

type LookupStatus string

const (
	LookupCommitted   LookupStatus = "committed"
	LookupInProgress  LookupStatus = "in_progress"
	LookupNotObserved LookupStatus = "not_observed"
)

type VariableCommandLookup struct {
	status  LookupStatus
	receipt func() VariableMutation
}

func NewVariableCommandLookup(status LookupStatus, receipt *VariableMutation) (VariableCommandLookup, error) {
	v := VariableCommandLookup{status: status}
	if receipt != nil {
		copy := receipt.Clone()
		v.receipt = func() VariableMutation { return copy }
	}
	if err := v.Validate(); err != nil {
		return VariableCommandLookup{}, err
	}
	return v, nil
}
func (v VariableCommandLookup) Status() LookupStatus { return v.status }
func (v VariableCommandLookup) Receipt() *VariableMutation {
	if v.receipt == nil {
		return nil
	}
	n := v.receipt()
	return &n
}
func (v VariableCommandLookup) Validate() error {
	switch v.status {
	case LookupCommitted:
		if v.receipt != nil {
			return v.receipt().Validate()
		}
	case LookupInProgress, LookupNotObserved:
		if v.receipt == nil {
			return nil
		}
	}
	return invalid("", "INVALID_LOOKUP")
}
func (v VariableCommandLookup) Clone() VariableCommandLookup { return v }
func (v VariableCommandLookup) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Status  LookupStatus      `json:"status"`
		Receipt *VariableMutation `json:"receipt"`
	}{v.status, v.Receipt()})
}
func (v *VariableCommandLookup) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	n, err := decode[struct {
		Status  LookupStatus      `json:"status"`
		Receipt *VariableMutation `json:"receipt"`
	}](raw, []string{"status", "receipt"}, nil, []string{"receipt"}, MaxReceiptBytes+1024)
	if err != nil {
		return err
	}
	next, err := NewVariableCommandLookup(n.Status, n.Receipt)
	if err == nil {
		*v = next
	}
	return err
}
func (VariableCommandLookup) Format(w fmt.State, _ rune) { safe(w) }
func (VariableCommandLookup) LogValue() slog.Value       { return safeLog() }
