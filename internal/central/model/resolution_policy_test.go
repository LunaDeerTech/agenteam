package model

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelCurrentResolutionProfileClosedMatrix(t *testing.T) {
	p := providerRecord{Input: providerInput{Protocol: mc.OpenAIChat, Enabled: true, Options: json.RawMessage(`{}`)}}
	m := modelRecord{Input: mc.ModelInput{Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}}
	direct := mc.SelectionRef{Kind: "direct"}
	memory := mc.SelectionRef{Kind: "platform", Selector: mc.MemorySelector}
	rev, e := resolutionProfile(&p, &m, direct)
	if e != nil || rev != adapter.OpenAIChatTextRevision {
		t.Fatal("text profile", e)
	}
	_, e = resolutionProfile(&p, &m, memory)
	requireCode(t, e, f.CapabilityUnsupported)
	m.Input.Capabilities.StructuredOutputModes = []string{"json_schema", "text"}
	rev, e = resolutionProfile(&p, &m, memory)
	if e != nil || rev != adapter.OpenAIChatStructuredRevision {
		t.Fatal("memory profile", e)
	}
	for name, change := range map[string]func(*providerRecord, *modelRecord){"options": func(p *providerRecord, _ *modelRecord) { p.Input.Options = json.RawMessage(`{"unsupported":true}`) }, "reasoning": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.Reasoning = true }, "tool": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.ToolCalls = true }, "image": func(_ *providerRecord, m *modelRecord) {
		m.Input.Capabilities.InputModalities = []string{"text", "image"}
	}, "overwrite": func(_ *providerRecord, m *modelRecord) {
		m.Input.HeaderOverwrite = map[string]string{"X-Custom": "value"}
	}, "protocol": func(p *providerRecord, _ *modelRecord) { p.Input.Protocol = mc.AnthropicMessages }} {
		t.Run(name, func(t *testing.T) {
			cp, cm := p, m
			cm.Input = m.Input.Clone()
			change(&cp, &cm)
			_, e := resolutionProfile(&cp, &cm, memory)
			requireCode(t, e, f.CapabilityUnsupported)
		})
	}
	p.Input.Enabled = false
	_, e = resolutionProfile(&p, &m, direct)
	requireCode(t, e, f.InvalidState)
}
