package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// A nil default option means omission and expands to true. Nullable metadata
// remains nil. Fields() always returns the expanded defaults and owned copies.
type AgentCreateFields struct {
	AgentID                  i.AgentID             `json:"agent_id"`
	Name                     string                `json:"name"`
	DisplayName              *string               `json:"display_name"`
	TagColor                 *string               `json:"tag_color"`
	Description              string                `json:"description"`
	Instructions             string                `json:"instructions"`
	InjectAgentsMD           bool                  `json:"inject_agents_md"`
	ModelRef                 mc.ModelID            `json:"model_ref"`
	ReasoningEffort          *string               `json:"reasoning_effort"`
	ApprovalPolicy           ApprovalPolicy        `json:"approval_policy"`
	ApprovalModelRef         *mc.ModelID           `json:"approval_model_ref"`
	AllowedToolIDs           []i.ToolID            `json:"allowed_tool_ids"`
	AllowedMountIDs          []i.MountID           `json:"allowed_mount_ids"`
	AllowedSecretVariableIDs []i.ProjectVariableID `json:"allowed_secret_variable_ids"`
	AddSkillsEnabled         *bool                 `json:"add_skills_enabled"`
	InstallSkillEnabled      *bool                 `json:"install_skill_enabled"`
}
type AgentCreate struct{ data func() AgentCreateFields }

func cloneCreate(v AgentCreateFields) AgentCreateFields {
	v.DisplayName, v.TagColor, v.ReasoningEffort = clonePtr(v.DisplayName), clonePtr(v.TagColor), clonePtr(v.ReasoningEffort)
	v.ApprovalModelRef = clonePtr(v.ApprovalModelRef)
	v.AddSkillsEnabled, v.InstallSkillEnabled = clonePtr(v.AddSkillsEnabled), clonePtr(v.InstallSkillEnabled)
	v.AllowedToolIDs, v.AllowedMountIDs, v.AllowedSecretVariableIDs = slices.Clone(v.AllowedToolIDs), slices.Clone(v.AllowedMountIDs), slices.Clone(v.AllowedSecretVariableIDs)
	return v
}
func NewAgentCreate(v AgentCreateFields) (AgentCreate, error) {
	if v.AgentID.Validate() != nil || !validName(v.Name) || v.ModelRef.Validate() != nil || v.ApprovalPolicy.Validate() != nil ||
		v.DisplayName != nil && !validDisplayName(*v.DisplayName) || v.TagColor != nil && !validTagColor(*v.TagColor) ||
		!validBody(v.Description, MaxAgentDescriptionBytes) || !validBody(v.Instructions, MaxAgentInstructionsBytes) ||
		v.ReasoningEffort != nil && !validEffort(*v.ReasoningEffort) ||
		(v.ApprovalPolicy == ApprovalAuto) != (v.ApprovalModelRef != nil) || v.ApprovalModelRef != nil && v.ApprovalModelRef.Validate() != nil {
		return AgentCreate{}, invalid("", "INVALID_AGENT_CREATE")
	}
	if err := validateReferences(v.AllowedToolIDs, v.AllowedMountIDs, v.AllowedSecretVariableIDs); err != nil {
		return AgentCreate{}, err
	}
	v = cloneCreate(v)
	if v.AddSkillsEnabled == nil {
		enabled := true
		v.AddSkillsEnabled = &enabled
	}
	if v.InstallSkillEnabled == nil {
		enabled := true
		v.InstallSkillEnabled = &enabled
	}
	return AgentCreate{func() AgentCreateFields { return cloneCreate(v) }}, nil
}
func (v AgentCreate) Fields() AgentCreateFields {
	if v.data == nil {
		return AgentCreateFields{}
	}
	return v.data()
}
func (v AgentCreate) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_AGENT_CREATE")
	}
	return nil
}
func (v AgentCreate) Clone() AgentCreate { return v }
func (v AgentCreate) MarshalJSON() ([]byte, error) {
	return marshalChecked(v.Fields(), v.Validate(), MaxAgentCoreBytes)
}
func (v *AgentCreate) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	_, err := decodeFields(raw, MaxAgentCoreBytes,
		[]string{"agent_id", "name", "model_ref", "inject_agents_md", "approval_policy", "allowed_tool_ids", "allowed_mount_ids", "allowed_secret_variable_ids"},
		[]string{"display_name", "tag_color", "description", "instructions", "reasoning_effort", "approval_model_ref", "add_skills_enabled", "install_skill_enabled"},
		[]string{"display_name", "tag_color", "reasoning_effort", "approval_model_ref"})
	if err != nil {
		return err
	}
	var fields AgentCreateFields
	if json.Unmarshal(raw, &fields) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	next, err := NewAgentCreate(fields)
	if err == nil {
		*v = next
	}
	return err
}
func DecodeAgentCreate(raw []byte) (AgentCreate, error) {
	var v AgentCreate
	err := v.UnmarshalJSON(raw)
	return v, err
}
func (AgentCreate) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_create") }
func (AgentCreate) LogValue() slog.Value       { return slog.StringValue("agent_create") }
