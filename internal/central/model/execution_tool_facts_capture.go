package model

import (
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// This revision identifies configuration capture only. No network adapter or
// default Runtime accepts it; the stopped tools HTTP profile is not enabled.
const toolFactsCaptureRevision = "execution-tool-call-facts-capture-v1"

type resolutionCaptureProfile uint8

const (
	defaultResolutionCapture resolutionCaptureProfile = iota
	toolFactsResolutionCapture
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
	case toolFactsResolutionCapture:
		if r.Actor.Details().Kind != id.AgentRun || r.Consumer.Kind != mc.AgentConsumer || r.Purpose != mc.AgentGeneration || r.Consumer.ExecutionID == nil || r.Consumer.OperationID != "" || r.Source != mc.CurrentSelectionSource || r.Selection == nil || r.Selection.Kind != "direct" || r.ReasoningEffort != "" {
			return fault(f.CapabilityUnsupported)
		}
		return nil
	default:
		return fault(f.InvalidArgument)
	}
}

func toolFactsResolutionProfile(p *providerRecord, m *modelRecord, selection mc.SelectionRef) (string, error) {
	if !p.Input.Enabled || !m.Input.Enabled {
		return "", fault(f.InvalidState)
	}
	c := m.Input.Capabilities
	if selection.Kind != "direct" || p.Input.Protocol != mc.OpenAIChat || m.Input.Type != mc.ChatModel || !emptyObject(p.Input.Options) || !emptyObject(m.Input.Parameters) || !emptyObject(m.Input.RequestOverwrite) || len(m.Input.HeaderOverwrite) != 0 || !c.ToolCalls || c.ParallelToolCalls || c.Streaming || c.Reasoning || len(c.ReasoningEfforts) != 0 || !slices.Equal(c.InputModalities, []string{"text"}) || !slices.Equal(c.OutputModalities, []string{"text"}) || len(c.StructuredOutputModes) != 0 && !slices.Equal(c.StructuredOutputModes, []string{"text"}) {
		return "", fault(f.CapabilityUnsupported)
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
	if p == defaultResolutionCapture {
		if s.Identity.AdapterRevision == toolFactsCaptureRevision || s.Capabilities.ToolCalls || s.Capabilities.ParallelToolCalls {
			return fault(f.CapabilityUnsupported)
		}
		return nil
	}
	if s.Identity.AdapterRevision != toolFactsCaptureRevision {
		return fault(f.CapabilityUnsupported)
	}
	provider := providerRecord{Input: providerInput{Enabled: true, Protocol: s.Identity.Protocol, Options: []byte(`{}`)}}
	model := modelRecord{Input: mc.ModelInput{Enabled: true, Type: s.Identity.ModelType, Parameters: s.Parameters, RequestOverwrite: s.RequestOverwrite, HeaderOverwrite: s.HeaderOverwrite, Capabilities: s.Capabilities}}
	_, err := toolFactsResolutionProfile(&provider, &model, *r.Selection)
	return err
}
