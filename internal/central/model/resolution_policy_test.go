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

func TestModelMeetingSummaryResolutionProfileMatrix(t *testing.T) {
	p := providerRecord{Input: providerInput{Protocol: mc.OpenAIChat, Enabled: true, Options: json.RawMessage(`{}`)}}
	m := modelRecord{Input: mc.ModelInput{Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, Streaming: true}}}
	s := mc.SelectionRef{Kind: "platform", Selector: mc.MeetingSummarySelector}
	for _, modes := range [][]string{nil, {"text"}, {"json_schema"}, {"text", "json_schema"}} {
		m.Input.Capabilities.StructuredOutputModes = modes
		before, _ := json.Marshal(m.Input)
		revision, e := resolutionProfile(&p, &m, s)
		want := adapter.OpenAIChatTextRevision
		if len(modes) > 0 && modes[len(modes)-1] == "json_schema" {
			want = adapter.OpenAIChatStructuredRevision
		}
		if e != nil || revision != want {
			t.Fatal("Summary profile", modes, revision, e)
		}
		after, _ := json.Marshal(m.Input)
		if string(before) != string(after) {
			t.Fatal("profile stripped real capabilities")
		}
	}
	for name, change := range map[string]func(*providerRecord, *modelRecord){
		"parameters": func(_ *providerRecord, m *modelRecord) { m.Input.Parameters = json.RawMessage(`{"temperature":0.3}`) },
		"options":    func(p *providerRecord, _ *modelRecord) { p.Input.Options = json.RawMessage(`{"custom":true}`) },
		"overwrite": func(_ *providerRecord, m *modelRecord) {
			m.Input.RequestOverwrite = json.RawMessage(`{"temperature":0.3}`)
		},
		"headers":   func(_ *providerRecord, m *modelRecord) { m.Input.HeaderOverwrite = map[string]string{"X-Foo": "x"} },
		"tools":     func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.ToolCalls = true },
		"parallel":  func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.ParallelToolCalls = true },
		"reasoning": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.Reasoning = true },
		"efforts":   func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.ReasoningEfforts = []string{"high"} },
		"image":     func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.OutputModalities = []string{"image"} },
		"protocol":  func(p *providerRecord, _ *modelRecord) { p.Input.Protocol = mc.AnthropicMessages },
	} {
		t.Run(name, func(t *testing.T) {
			cp, cm := p, m
			cm.Input = m.Input.Clone()
			change(&cp, &cm)
			_, e := resolutionProfile(&cp, &cm, s)
			requireCode(t, e, f.CapabilityUnsupported)
		})
	}
	for _, provider := range []bool{false, true} {
		cp, cm := p, m
		if provider {
			cp.Input.Enabled = false
		} else {
			cm.Input.Enabled = false
		}
		_, e := resolutionProfile(&cp, &cm, s)
		requireCode(t, e, f.InvalidState)
	}
}
