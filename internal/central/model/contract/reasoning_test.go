package contract

import (
	"context"
	"strings"
	"testing"
)

func TestReasoningCapabilitiesDeclaredValuesAndNames(t *testing.T) {
	valid := Capabilities{Reasoning: true, InputModalities: []string{"text", "image", "file"}, OutputModalities: []string{"text"}, ReasoningEfforts: []ReasoningEffort{{Name: strings.Repeat("深", 64), Value: "vendor:deep-v2_1.0"}}}
	if valid.Validate() != nil || !valid.SupportsTextInput() || !valid.SupportsReasoningEffort("vendor:deep-v2_1.0") || valid.SupportsReasoningEffort(valid.ReasoningEfforts[0].Name) || valid.SupportsReasoningEffort("") {
		t.Fatal("declared value or Unicode name contract")
	}
	for name, change := range map[string]func(*Capabilities){
		"trim":         func(c *Capabilities) { c.ReasoningEfforts[0].Name = " deep" },
		"control":      func(c *Capabilities) { c.ReasoningEfforts[0].Name = "deep\u0085value" },
		"name-length":  func(c *Capabilities) { c.ReasoningEfforts[0].Name = strings.Repeat("深", 65) },
		"value-length": func(c *Capabilities) { c.ReasoningEfforts[0].Value = strings.Repeat("a", 33) },
		"value-escape": func(c *Capabilities) { c.ReasoningEfforts[0].Value = "deep/value" },
		"duplicate-name": func(c *Capabilities) {
			c.ReasoningEfforts = append(c.ReasoningEfforts, ReasoningEffort{Name: c.ReasoningEfforts[0].Name, Value: "other"})
		},
		"duplicate-value": func(c *Capabilities) {
			c.ReasoningEfforts = append(c.ReasoningEfforts, ReasoningEffort{Name: "Other", Value: c.ReasoningEfforts[0].Value})
		},
		"unsupported": func(c *Capabilities) { c.Reasoning = false },
		"too-many":    func(c *Capabilities) { c.ReasoningEfforts = make([]ReasoningEffort, 33) },
	} {
		t.Run(name, func(t *testing.T) {
			c := valid.Clone()
			change(&c)
			if c.Validate() == nil {
				t.Fatal("invalid declaration accepted")
			}
		})
	}
	for _, supported := range []bool{false, true} {
		c := valid.Clone()
		c.Reasoning, c.ReasoningEfforts = supported, nil
		if c.Validate() != nil || c.SupportsReasoningEffort("vendor:deep-v2_1.0") {
			t.Fatal("unadjustable capability")
		}
	}
	for _, input := range [][]string{{"image"}, {"text", "vector"}, {"text", "text"}, nil} {
		c := valid.Clone()
		c.InputModalities = input
		if c.SupportsTextInput() {
			t.Fatal("unsupported text input declaration", input)
		}
	}
	copy := valid.Clone()
	copy.ReasoningEfforts[0].Value = "other"
	if valid.ReasoningEfforts[0].Value != "vendor:deep-v2_1.0" {
		t.Fatal("clone aliases declaration")
	}
}

func TestReasoningChoiceSurvivesSuccessorAndBindsDigest(t *testing.T) {
	_, parts := successorPartsFixture(t)
	parts.Model.Snapshot.Capabilities.Reasoning = true
	parts.Model.Snapshot.Capabilities.ReasoningEfforts = []ReasoningEffort{{Name: "Deliberate", Value: "custom:deep"}}
	baseline, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	parts.Model.Snapshot.ReasoningEffort = "custom:deep"
	selected, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	decoded, err := DecodeSuccessorSemantic(context.Background(), selected.CanonicalBytes())
	must(t, err)
	if decoded.data.parts.Model.Snapshot.ReasoningEffort != "custom:deep" || selected.Digest() == baseline.Digest() {
		t.Fatal("choice lost from successor or digest")
	}
	parts.Model.Snapshot.ReasoningEffort = "Deliberate"
	if parts.Model.Snapshot.Validate() == nil {
		t.Fatal("display name accepted as wire value")
	}
}
