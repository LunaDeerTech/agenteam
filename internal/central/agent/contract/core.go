// Package contract defines pure Agent configuration and current-fact ports.
// Validation establishes shape, never existence, authorization, initialization,
// free execution capacity or a usable Model. No service implementation is supplied.
package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	model "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const (
	MaxAgentCoreBytes         = 512 << 10
	MaxAgentDescriptionBytes  = 8192
	MaxAgentInstructionsBytes = 32768
)

type ApprovalPolicy string

const (
	ApprovalDefault ApprovalPolicy = "default"
	ApprovalAuto    ApprovalPolicy = "auto"
	ApprovalAllow   ApprovalPolicy = "allow"
)

func (v ApprovalPolicy) Validate() error {
	switch v {
	case ApprovalDefault, ApprovalAuto, ApprovalAllow:
		return nil
	}
	return invalid("/approval_policy", "INVALID_APPROVAL_POLICY")
}
func (v ApprovalPolicy) MarshalJSON() ([]byte, error) {
	return marshalChecked(string(v), v.Validate(), maxEnumBytes)
}
func (v *ApprovalPolicy) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, err := decodeString(raw, maxEnumBytes)
	if err != nil {
		return err
	}
	next := ApprovalPolicy(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func (ApprovalPolicy) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_approval_policy") }
func (ApprovalPolicy) LogValue() slog.Value       { return slog.StringValue("agent_approval_policy") }

// AgentLifecycle is a configuration deletion gate, not execution state.
type AgentLifecycle string

const (
	AgentActive   AgentLifecycle = "active"
	AgentDeleting AgentLifecycle = "deleting"
)

func (v AgentLifecycle) Validate() error {
	switch v {
	case AgentActive, AgentDeleting:
		return nil
	}
	return invalid("/lifecycle", "INVALID_AGENT_LIFECYCLE")
}
func (v AgentLifecycle) MarshalJSON() ([]byte, error) {
	return marshalChecked(string(v), v.Validate(), maxEnumBytes)
}
func (v *AgentLifecycle) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, err := decodeString(raw, maxEnumBytes)
	if err != nil {
		return err
	}
	next := AgentLifecycle(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func (AgentLifecycle) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_lifecycle") }
func (AgentLifecycle) LogValue() slog.Value       { return slog.StringValue("agent_lifecycle") }

// AgentCore is the seventeen-field configuration core. It intentionally omits
// Tool/Mount/SecretVariable references and is not a complete AgentConfig or a
// creation request. Version is the sole config_version authority.
type AgentCore struct {
	ID               identity.AgentID   `json:"id"`
	ProjectID        identity.ProjectID `json:"project_id"`
	Name             string             `json:"name"`
	NormalizedName   string             `json:"normalized_name"`
	DisplayName      *string            `json:"display_name"`
	TagColor         *string            `json:"tag_color"`
	Description      string             `json:"description"`
	Instructions     string             `json:"instructions"`
	InjectAgentsMD   bool               `json:"inject_agents_md"`
	ModelRef         model.ModelID      `json:"model_ref"`
	ReasoningEffort  *string            `json:"reasoning_effort"`
	ApprovalPolicy   ApprovalPolicy     `json:"approval_policy"`
	ApprovalModelRef *model.ModelID     `json:"approval_model_ref"`
	Lifecycle        AgentLifecycle     `json:"lifecycle"`
	Version          foundation.Version `json:"version"`
	CreatedAt        foundation.Instant `json:"created_at"`
	UpdatedAt        foundation.Instant `json:"updated_at"`
}

func (v AgentCore) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.ModelRef.Validate() != nil || v.Version.Validate() != nil || v.CreatedAt.Validate() != nil || v.UpdatedAt.Validate() != nil || v.UpdatedAt.Time().Before(v.CreatedAt.Time()) {
		return invalid("", "INVALID_AGENT_CORE")
	}
	if !validName(v.Name) || v.NormalizedName != strings.ToLower(v.Name) {
		return invalid("/name", "INVALID_AGENT_NAME")
	}
	if v.DisplayName != nil && !validDisplayName(*v.DisplayName) {
		return invalid("/display_name", "INVALID_DISPLAY_NAME")
	}
	if v.TagColor != nil && !validTagColor(*v.TagColor) {
		return invalid("/tag_color", "INVALID_TAG_COLOR")
	}
	if !validBody(v.Description, MaxAgentDescriptionBytes) {
		return invalid("/description", "INVALID_DESCRIPTION")
	}
	if !validBody(v.Instructions, MaxAgentInstructionsBytes) {
		return invalid("/instructions", "INVALID_INSTRUCTIONS")
	}
	if v.ReasoningEffort != nil && !validEffort(*v.ReasoningEffort) {
		return invalid("/reasoning_effort", "INVALID_REASONING_EFFORT")
	}
	if err := v.ApprovalPolicy.Validate(); err != nil {
		return err
	}
	if (v.ApprovalPolicy == ApprovalAuto) != (v.ApprovalModelRef != nil) || v.ApprovalModelRef != nil && v.ApprovalModelRef.Validate() != nil {
		return invalid("/approval_model_ref", "INVALID_APPROVAL_MODEL")
	}
	return v.Lifecycle.Validate()
}

func validName(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' && i > 0 && i < len(s)-1 {
			continue
		}
		return false
	}
	switch strings.ToLower(s) {
	case "api", "assets", "auth", "login", "logout", "invite", "reset", "settings", "system", "personal", "diagnostics", "livez", "readyz", "debug", "support", "root", "admin":
		return false
	}
	return true
}
func validDisplayName(s string) bool {
	if s == "" || len(s) > 1024 || !utf8.ValidString(s) || utf8.RuneCountInString(s) > 256 {
		return false
	}
	nonspace := false
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) {
			return false
		}
		nonspace = nonspace || !unicode.IsSpace(r)
	}
	return nonspace
}
func validTagColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}
func validBody(s string, limit int) bool {
	if len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}
func validEffort(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == ':' || c == '-') {
			return false
		}
	}
	return true
}
func clonePtr[T any](v *T) *T {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
func (v AgentCore) Clone() AgentCore {
	v.DisplayName = clonePtr(v.DisplayName)
	v.TagColor = clonePtr(v.TagColor)
	v.ReasoningEffort = clonePtr(v.ReasoningEffort)
	v.ApprovalModelRef = clonePtr(v.ApprovalModelRef)
	return v
}
func (v AgentCore) MarshalJSON() ([]byte, error) {
	type wire AgentCore
	return marshalChecked(wire(v), v.Validate(), MaxAgentCoreBytes)
}
func (v *AgentCore) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire AgentCore
	w, err := decodeObject[wire](raw, MaxAgentCoreBytes,
		[]string{"id", "project_id", "name", "normalized_name", "display_name", "tag_color", "description", "instructions", "inject_agents_md", "model_ref", "reasoning_effort", "approval_policy", "approval_model_ref", "lifecycle", "version", "created_at", "updated_at"},
		[]string{"display_name", "tag_color", "reasoning_effort", "approval_model_ref"})
	if err != nil {
		return err
	}
	next := AgentCore(w)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

// DecodeAgentCore bounds the complete supplied raw, including outer whitespace.
func DecodeAgentCore(raw []byte) (AgentCore, error) {
	var v AgentCore
	err := v.UnmarshalJSON(raw)
	return v, err
}

// Direct formatting is safe. Arbitrary enclosing structs or JSON fallbacks may
// bypass this method; authorized business JSON must not be used as a log value.
func (AgentCore) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_core") }
func (AgentCore) LogValue() slog.Value       { return slog.StringValue("agent_core") }

var _ json.Marshaler = AgentCore{}
var _ json.Unmarshaler = (*AgentCore)(nil)
