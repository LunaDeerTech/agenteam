package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type NullableChange[T any] struct {
	Present bool
	Value   *T
}
type AgentUpdateFields struct {
	Name                     *string
	DisplayName              NullableChange[string]
	TagColor                 NullableChange[string]
	Description              *string
	Instructions             *string
	InjectAgentsMD           *bool
	ModelRef                 *mc.ModelID
	ReasoningEffort          NullableChange[string]
	ApprovalPolicy           *ApprovalPolicy
	ApprovalModelRef         NullableChange[mc.ModelID]
	AllowedToolIDs           *[]i.ToolID
	AllowedMountIDs          *[]i.MountID
	AllowedSecretVariableIDs *[]i.ProjectVariableID
}
type AgentUpdate struct{ data func() AgentUpdateFields }

func cloneSlicePtr[T any](v *[]T) *[]T {
	if v == nil {
		return nil
	}
	copy := slices.Clone(*v)
	return &copy
}
func cloneNullable[T any](v NullableChange[T]) NullableChange[T] {
	v.Value = clonePtr(v.Value)
	return v
}
func cloneUpdate(v AgentUpdateFields) AgentUpdateFields {
	v.Name, v.Description, v.Instructions = clonePtr(v.Name), clonePtr(v.Description), clonePtr(v.Instructions)
	v.InjectAgentsMD, v.ModelRef, v.ApprovalPolicy = clonePtr(v.InjectAgentsMD), clonePtr(v.ModelRef), clonePtr(v.ApprovalPolicy)
	v.DisplayName, v.TagColor, v.ReasoningEffort = cloneNullable(v.DisplayName), cloneNullable(v.TagColor), cloneNullable(v.ReasoningEffort)
	v.ApprovalModelRef = cloneNullable(v.ApprovalModelRef)
	v.AllowedToolIDs, v.AllowedMountIDs, v.AllowedSecretVariableIDs = cloneSlicePtr(v.AllowedToolIDs), cloneSlicePtr(v.AllowedMountIDs), cloneSlicePtr(v.AllowedSecretVariableIDs)
	return v
}
func validNullable[T any](v NullableChange[T], valid func(T) bool) bool {
	return v.Present && (v.Value == nil || valid(*v.Value)) || !v.Present && v.Value == nil
}
func validID[T interface{ Validate() error }](v T) bool { return v.Validate() == nil }
func NewAgentUpdate(v AgentUpdateFields) (AgentUpdate, error) {
	if len(updateWire(v)) == 0 || v.Name != nil && !validName(*v.Name) || v.Description != nil && !validBody(*v.Description, MaxAgentDescriptionBytes) ||
		v.Instructions != nil && !validBody(*v.Instructions, MaxAgentInstructionsBytes) || v.ModelRef != nil && v.ModelRef.Validate() != nil || v.ApprovalPolicy != nil && v.ApprovalPolicy.Validate() != nil ||
		!validNullable(v.DisplayName, validDisplayName) || !validNullable(v.TagColor, validTagColor) || !validNullable(v.ReasoningEffort, validEffort) || !validNullable(v.ApprovalModelRef, validID[mc.ModelID]) ||
		v.AllowedToolIDs != nil && !validReferences(*v.AllowedToolIDs) || v.AllowedMountIDs != nil && !validReferences(*v.AllowedMountIDs) || v.AllowedSecretVariableIDs != nil && !validReferences(*v.AllowedSecretVariableIDs) {
		return AgentUpdate{}, invalid("", "INVALID_AGENT_UPDATE")
	}
	count := 0
	if v.AllowedToolIDs != nil {
		count += len(*v.AllowedToolIDs)
	}
	if v.AllowedMountIDs != nil {
		count += len(*v.AllowedMountIDs)
	}
	if v.AllowedSecretVariableIDs != nil {
		count += len(*v.AllowedSecretVariableIDs)
	}
	if count > MaxTotalCapabilityReferences {
		return AgentUpdate{}, invalid("", "INVALID_CAPABILITY_REFERENCES")
	}
	v = cloneUpdate(v)
	return AgentUpdate{func() AgentUpdateFields { return cloneUpdate(v) }}, nil
}
func putValue[T any](m map[string]any, key string, v *T) {
	if v != nil {
		m[key] = *v
	}
}
func putNullable[T any](m map[string]any, key string, v NullableChange[T]) {
	if v.Present {
		m[key] = v.Value
	}
}
func updateWire(v AgentUpdateFields) map[string]any {
	m := map[string]any{}
	putValue(m, "name", v.Name)
	putNullable(m, "display_name", v.DisplayName)
	putNullable(m, "tag_color", v.TagColor)
	putValue(m, "description", v.Description)
	putValue(m, "instructions", v.Instructions)
	putValue(m, "inject_agents_md", v.InjectAgentsMD)
	putValue(m, "model_ref", v.ModelRef)
	putNullable(m, "reasoning_effort", v.ReasoningEffort)
	putValue(m, "approval_policy", v.ApprovalPolicy)
	putNullable(m, "approval_model_ref", v.ApprovalModelRef)
	putValue(m, "allowed_tool_ids", v.AllowedToolIDs)
	putValue(m, "allowed_mount_ids", v.AllowedMountIDs)
	putValue(m, "allowed_secret_variable_ids", v.AllowedSecretVariableIDs)
	return m
}
func (v AgentUpdate) Fields() AgentUpdateFields {
	if v.data == nil {
		return AgentUpdateFields{}
	}
	return v.data()
}
func (v AgentUpdate) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_AGENT_UPDATE")
	}
	return nil
}
func (v AgentUpdate) Clone() AgentUpdate { return v }
func (v AgentUpdate) MarshalJSON() ([]byte, error) {
	return marshalChecked(updateWire(v.Fields()), v.Validate(), MaxAgentCoreBytes)
}
func decodeOptional[T any](fields map[string]json.RawMessage, key string, out **T) error {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	var value T
	if json.Unmarshal(raw, &value) != nil {
		return invalid("/"+key, "INVALID_ENCODING")
	}
	*out = &value
	return nil
}
func decodeNullable[T any](fields map[string]json.RawMessage, key string, out *NullableChange[T]) error {
	raw, ok := fields[key]
	if !ok {
		return nil
	}
	out.Present = true
	return json.Unmarshal(raw, &out.Value)
}
func (v *AgentUpdate) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := decodeFields(raw, MaxAgentCoreBytes, nil, []string{"name", "display_name", "tag_color", "description", "instructions", "inject_agents_md", "model_ref", "reasoning_effort", "approval_policy", "approval_model_ref", "allowed_tool_ids", "allowed_mount_ids", "allowed_secret_variable_ids"}, coreNullable)
	if err != nil {
		return err
	}
	var d AgentUpdateFields
	checks := []error{decodeOptional(fields, "name", &d.Name), decodeNullable(fields, "display_name", &d.DisplayName), decodeNullable(fields, "tag_color", &d.TagColor),
		decodeOptional(fields, "description", &d.Description), decodeOptional(fields, "instructions", &d.Instructions), decodeOptional(fields, "inject_agents_md", &d.InjectAgentsMD), decodeOptional(fields, "model_ref", &d.ModelRef),
		decodeNullable(fields, "reasoning_effort", &d.ReasoningEffort), decodeOptional(fields, "approval_policy", &d.ApprovalPolicy), decodeNullable(fields, "approval_model_ref", &d.ApprovalModelRef),
		decodeOptional(fields, "allowed_tool_ids", &d.AllowedToolIDs), decodeOptional(fields, "allowed_mount_ids", &d.AllowedMountIDs), decodeOptional(fields, "allowed_secret_variable_ids", &d.AllowedSecretVariableIDs)}
	for _, err := range checks {
		if err != nil {
			return invalid("", "INVALID_ENCODING")
		}
	}
	next, err := NewAgentUpdate(d)
	if err == nil {
		*v = next
	}
	return err
}
func DecodeAgentUpdate(raw []byte) (AgentUpdate, error) {
	var v AgentUpdate
	err := v.UnmarshalJSON(raw)
	return v, err
}

// Proposed applies presence only; it does not increment version, authorize a
// directory, initialize defaults, or publish this candidate as current.
func (v AgentUpdate) Proposed(base AgentConfig) (AgentConfig, error) {
	if err := v.Validate(); err != nil {
		return AgentConfig{}, err
	}
	if err := base.Validate(); err != nil {
		return AgentConfig{}, err
	}
	r, d := v.Fields(), base.Fields()
	core := &d.Core
	if r.Name != nil {
		core.Name = *r.Name
		core.NormalizedName = strings.ToLower(*r.Name)
	}
	if r.DisplayName.Present {
		core.DisplayName = clonePtr(r.DisplayName.Value)
	}
	if r.TagColor.Present {
		core.TagColor = clonePtr(r.TagColor.Value)
	}
	if r.Description != nil {
		core.Description = *r.Description
	}
	if r.Instructions != nil {
		core.Instructions = *r.Instructions
	}
	if r.InjectAgentsMD != nil {
		core.InjectAgentsMD = *r.InjectAgentsMD
	}
	if r.ModelRef != nil {
		core.ModelRef = *r.ModelRef
	}
	if r.ReasoningEffort.Present {
		core.ReasoningEffort = clonePtr(r.ReasoningEffort.Value)
	}
	if r.ApprovalPolicy != nil {
		core.ApprovalPolicy = *r.ApprovalPolicy
	}
	if r.ApprovalModelRef.Present {
		core.ApprovalModelRef = clonePtr(r.ApprovalModelRef.Value)
	}
	if r.AllowedToolIDs != nil {
		d.AllowedToolIDs = slices.Clone(*r.AllowedToolIDs)
	}
	if r.AllowedMountIDs != nil {
		d.AllowedMountIDs = slices.Clone(*r.AllowedMountIDs)
	}
	if r.AllowedSecretVariableIDs != nil {
		d.AllowedSecretVariableIDs = slices.Clone(*r.AllowedSecretVariableIDs)
	}
	return NewAgentConfig(d)
}
func (AgentUpdate) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_update") }
func (AgentUpdate) LogValue() slog.Value       { return slog.StringValue("agent_update") }
