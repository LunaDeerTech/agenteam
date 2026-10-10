package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// The digest preserves caller presence and expanded create defaults. Session,
// request ID, command key and server-generated plan fields are not semantics.
func AgentCommandDigest(actor i.Actor, meta f.CommandMeta, project i.ProjectID, target i.AgentID, name CommandName, request any) (f.Digest, error) {
	if actor.Validate() != nil || actor.Details().Kind != i.Human || ValidateAgentCommandMeta(name, meta) != nil || project.Validate() != nil || target.Validate() != nil {
		return "", invalid("", "INVALID_AGENT_COMMAND")
	}
	switch name {
	case CreateAgentCommand:
		r, ok := request.(AgentCreate)
		if !ok || r.Validate() != nil || r.Fields().AgentID != target {
			return "", invalid("", "INVALID_AGENT_CREATE")
		}
	case UpdateAgentCommand:
		r, ok := request.(AgentUpdate)
		if !ok || r.Validate() != nil {
			return "", invalid("", "INVALID_AGENT_UPDATE")
		}
	default:
		return "", invalid("", "INVALID_AGENT_COMMAND")
	}
	raw, err := json.Marshal(struct {
		Format   string      `json:"format"`
		Command  CommandName `json:"command"`
		Project  i.ProjectID `json:"project_id"`
		Target   i.AgentID   `json:"target_id"`
		User     string      `json:"actor_user_id"`
		Expected *f.Version  `json:"expected_version,omitempty"`
		Request  any         `json:"request"`
	}{"agent-configuration-v1", name, project, target, actor.Details().UserID, meta.ExpectedVersion, request})
	if err != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

type AgentMutationFields struct {
	Agent    AgentConfig  `json:"agent"`
	Changed  bool         `json:"changed"`
	EventIDs []ec.EventID `json:"event_ids"`
}
type AgentMutation struct{ data func() AgentMutationFields }

func cloneMutation(v AgentMutationFields) AgentMutationFields {
	v.Agent = v.Agent.Clone()
	v.EventIDs = slices.Clone(v.EventIDs)
	return v
}
func NewAgentMutation(v AgentMutationFields) (AgentMutation, error) {
	if v.Agent.Validate() != nil || v.EventIDs == nil || v.Changed && len(v.EventIDs) != 1 || !v.Changed && len(v.EventIDs) != 0 {
		return AgentMutation{}, invalid("", "INVALID_AGENT_MUTATION")
	}
	for _, id := range v.EventIDs {
		if id.Validate() != nil {
			return AgentMutation{}, invalid("", "INVALID_AGENT_MUTATION")
		}
	}
	v = cloneMutation(v)
	return AgentMutation{func() AgentMutationFields { return cloneMutation(v) }}, nil
}
func (v AgentMutation) Fields() AgentMutationFields {
	if v.data == nil {
		return AgentMutationFields{}
	}
	return v.data()
}
func (v AgentMutation) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_AGENT_MUTATION")
	}
	return nil
}
func (v AgentMutation) Clone() AgentMutation { return v }
func (v AgentMutation) MarshalJSON() ([]byte, error) {
	return marshalChecked(v.Fields(), v.Validate(), MaxAgentCoreBytes)
}
func (v *AgentMutation) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	_, err := decodeFields(raw, MaxAgentCoreBytes, []string{"agent", "changed", "event_ids"}, nil, nil)
	if err != nil {
		return err
	}
	var fields AgentMutationFields
	if json.Unmarshal(raw, &fields) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next, err := NewAgentMutation(fields)
	if err == nil {
		*v = next
	}
	return err
}
func DecodeAgentMutation(raw []byte) (AgentMutation, error) {
	var v AgentMutation
	err := v.UnmarshalJSON(raw)
	return v, err
}
func (AgentMutation) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_mutation") }
func (AgentMutation) LogValue() slog.Value       { return slog.StringValue("agent_mutation") }

type AgentLookupStatus string

const (
	AgentLookupCommitted   AgentLookupStatus = "committed"
	AgentLookupInProgress  AgentLookupStatus = "in_progress"
	AgentLookupNotObserved AgentLookupStatus = "not_observed"
)

type AgentCommandLookup struct {
	status  AgentLookupStatus
	receipt *AgentMutation
}

func NewAgentCommandLookup(status AgentLookupStatus, receipt *AgentMutation) (AgentCommandLookup, error) {
	if status != AgentLookupCommitted && status != AgentLookupInProgress && status != AgentLookupNotObserved || (status == AgentLookupCommitted) != (receipt != nil) || receipt != nil && receipt.Validate() != nil {
		return AgentCommandLookup{}, invalid("", "INVALID_AGENT_LOOKUP")
	}
	if receipt != nil {
		copy := receipt.Clone()
		receipt = &copy
	}
	return AgentCommandLookup{status, receipt}, nil
}
func (v AgentCommandLookup) Status() AgentLookupStatus { return v.status }
func (v AgentCommandLookup) Receipt() *AgentMutation {
	if v.receipt == nil {
		return nil
	}
	copy := v.receipt.Clone()
	return &copy
}
func (v AgentCommandLookup) Validate() error {
	_, err := NewAgentCommandLookup(v.status, v.receipt)
	return err
}
func (v AgentCommandLookup) Clone() AgentCommandLookup {
	copy, _ := NewAgentCommandLookup(v.status, v.receipt)
	return copy
}
func (v AgentCommandLookup) MarshalJSON() ([]byte, error) {
	return marshalChecked(struct {
		Status  AgentLookupStatus `json:"status"`
		Receipt *AgentMutation    `json:"receipt"`
	}{v.status, v.receipt}, v.Validate(), MaxAgentCoreBytes)
}
func (v *AgentCommandLookup) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	_, err := decodeFields(raw, MaxAgentCoreBytes, []string{"status", "receipt"}, nil, []string{"receipt"})
	if err != nil {
		return err
	}
	var d struct {
		Status  AgentLookupStatus `json:"status"`
		Receipt *AgentMutation    `json:"receipt"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next, err := NewAgentCommandLookup(d.Status, d.Receipt)
	if err == nil {
		*v = next
	}
	return err
}
func DecodeAgentCommandLookup(raw []byte) (AgentCommandLookup, error) {
	var v AgentCommandLookup
	err := v.UnmarshalJSON(raw)
	return v, err
}
func (AgentCommandLookup) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_command_lookup")
}
func (AgentCommandLookup) LogValue() slog.Value { return slog.StringValue("agent_command_lookup") }
