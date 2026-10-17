package model

import (
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// This revision identifies configuration capture only. No network adapter or
// default Runtime accepts it; the stopped tools HTTP profile is not enabled.
const toolFactsCaptureRevision = "execution-tool-call-facts-capture-v1"

type resolutionCaptureProfile uint8

const (
	defaultResolutionCapture resolutionCaptureProfile = iota
	toolFactsResolutionCapture
	toolCallingResolutionCapture
)

// NewToolCallFactsCapture is an explicit construction seam for the bounded
// ToolCall facts consumer. It uses the same real owner, resolution transaction,
// immutable snapshot and Secret lease as ExecutionCapture. It grants neither
// Model invocation nor Tool/Runner dispatch authority.
func NewToolCallFactsCapture(models *Service, owner mc.ExecutionModelCaptureAuthority) (*ExecutionCapture, error) {
	p, err := NewExecutionCapture(models, owner)
	if err != nil {
		return nil, err
	}
	p.profile = toolFactsResolutionCapture
	return p, nil
}

func (p resolutionCaptureProfile) requireRequest(r mc.ResolveRequest) error {
	switch p {
	case defaultResolutionCapture:
		return nil
	case toolFactsResolutionCapture, toolCallingResolutionCapture:
		if r.Actor.Details().Kind != id.AgentRun || r.Consumer.Kind != mc.AgentConsumer || r.Purpose != mc.AgentGeneration || r.Consumer.ExecutionID == nil || r.Consumer.OperationID != "" || r.Source != mc.CurrentSelectionSource || r.Selection == nil || r.Selection.Kind != "direct" {
			return fault(f.CapabilityUnsupported)
		}
		return nil
	default:
		return fault(f.InvalidArgument)
	}
}

func toolFactsResolutionProfile(p *providerRecord, m *modelRecord, selection mc.SelectionRef) (string, error) {
	return toolResolutionProfile(p, m, selection, false)
}

func toolResolutionProfile(p *providerRecord, m *modelRecord, selection mc.SelectionRef, executable bool) (string, error) {
	if !p.Input.Enabled || !m.Input.Enabled {
		return "", fault(f.InvalidState)
	}
	c := m.Input.Capabilities
	// An additional schema capability does not request structured output. This
	// profile still requires text for each actual tools request.
	if selection.Kind != "direct" || p.Input.Protocol.EffectiveProtocol(m.Input.Type) != mc.OpenAIChat || m.Input.Type != mc.ChatModel || !emptyObject(p.Input.Options) || !emptyObject(m.Input.Parameters) || !emptyObject(m.Input.RequestOverwrite) || len(m.Input.HeaderOverwrite) != 0 || !c.ToolCalls || c.ParallelToolCalls || !executable && c.Streaming || !c.SupportsTextInput() || !slices.Equal(c.OutputModalities, []string{"text"}) || len(c.StructuredOutputModes) != 0 && !slices.Contains(c.StructuredOutputModes, "text") {
		return "", fault(f.CapabilityUnsupported)
	}
	if executable {
		return wire.OpenAIChatToolsRevision, nil
	}
	return toolFactsCaptureRevision, nil
}

// Check even prepared or committed historical drafts before any reuse. The
// identity/semantic remain the original resolution unit: switching profile
// cannot replace its draft or reinterpret an existing credential lease.
func (p resolutionCaptureProfile) requireDraft(r mc.ResolveRequest, d resolutionDraft) error {
	if err := p.requireRequest(r); err != nil {
		return err
	}
	s := d.Snapshot
	if !resolutionReasoningMatches(resolutionRequest(r), s) {
		return fault(f.InvalidArgument)
	}
	if p == defaultResolutionCapture {
		if s.Identity.AdapterRevision == toolFactsCaptureRevision || s.Identity.AdapterRevision == wire.OpenAIChatToolsRevision || s.Capabilities.ToolCalls || s.Capabilities.ParallelToolCalls {
			return fault(f.CapabilityUnsupported)
		}
		return nil
	}
	expected := toolFactsCaptureRevision
	if p == toolCallingResolutionCapture {
		expected = wire.OpenAIChatToolsRevision
	}
	if s.Identity.AdapterRevision != expected {
		return fault(f.CapabilityUnsupported)
	}
	provider := providerRecord{Input: providerInput{Enabled: true, Protocol: s.Identity.Protocol, Options: []byte(`{}`)}}
	model := modelRecord{Input: mc.ModelInput{Enabled: true, Type: s.Identity.ModelType, Parameters: s.Parameters, RequestOverwrite: s.RequestOverwrite, HeaderOverwrite: s.HeaderOverwrite, Capabilities: s.Capabilities}}
	_, err := toolResolutionProfile(&provider, &model, *r.Selection, p == toolCallingResolutionCapture)
	return err
}

// NewToolCallingExecutionCapture freezes a runnable tools revision only for a
// newly resolved Execution. Existing facts-only snapshots cannot be upgraded.
func NewToolCallingExecutionCapture(models *Service, owner mc.ExecutionModelCaptureAuthority) (*ExecutionCapture, error) {
	p, err := NewExecutionCapture(models, owner)
	if err != nil {
		return nil, err
	}
	p.profile = toolCallingResolutionCapture
	return p, nil
}
