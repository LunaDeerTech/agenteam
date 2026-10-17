package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// ReasoningChoice distinguishes provider defaults from an explicit disable.
// The model declares the only permitted mapping; callers cannot supply keys.
type ReasoningChoice struct {
	Mode  string `json:"mode"`
	Value string `json:"value,omitempty"`
}

func (r ReasoningChoice) Validate() error {
	if !one(r.Mode, "default", "on", "off", "effort") || r.Mode == "effort" && !safeToken(r.Value, 32) || r.Mode != "effort" && r.Value != "" {
		return bad()
	}
	return nil
}
func (r *ReasoningChoice) UnmarshalJSON(raw []byte) error {
	var v struct {
		Mode  string  `json:"mode"`
		Value *string `json:"value"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF || v.Mode == "effort" && v.Value == nil || v.Mode != "effort" && v.Value != nil {
		return bad()
	}
	// Explicit null is not a value in any branch.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return bad()
	}
	if _, ok := fields["value"]; ok && v.Value == nil {
		return bad()
	}
	out := ReasoningChoice{Mode: v.Mode}
	if v.Value != nil {
		out.Value = *v.Value
	}
	if out.Validate() != nil {
		return bad()
	}
	*r = out
	return nil
}
func (r ReasoningChoice) ValidateFor(m ModelInput) error {
	if r.Validate() != nil {
		return bad()
	}
	if r.Mode == "default" {
		return nil
	}
	if m.Type != ChatModel || !m.Capabilities.Reasoning {
		return bad()
	}
	if r.Mode == "effort" {
		if !m.Capabilities.SupportsReasoningEffort(r.Value) {
			return bad()
		}
		return nil
	}
	if len(m.Capabilities.ReasoningEfforts) != 0 || m.Capabilities.ReasoningToggleMode == nil {
		return bad()
	}
	return nil
}
func (r ReasoningChoice) Enabled() *bool {
	if r.Mode != "on" && r.Mode != "off" {
		return nil
	}
	v := r.Mode == "on"
	return &v
}

type ModelChoice struct {
	ModelID   ModelID         `json:"model_id"`
	Reasoning ReasoningChoice `json:"reasoning"`
}

func (c ModelChoice) Validate() error {
	if c.ModelID.Validate() != nil || c.Reasoning.Validate() != nil {
		return bad()
	}
	return nil
}
func DefaultModelChoice(m ModelID) ModelChoice {
	return ModelChoice{ModelID: m, Reasoning: ReasoningChoice{Mode: "default"}}
}
func validNotes(s string) bool {
	return len(s) <= 8000 && utf8.ValidString(s) && utf8.RuneCountInString(s) <= 2000 && !strings.ContainsRune(s, 0)
}
