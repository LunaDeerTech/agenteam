package model

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func resolutionVariant(r mc.ResolveRequest) error {
	if r.Source != mc.CurrentSelectionSource || r.Selection == nil || r.Selection.Kind == "project_summary" {
		return fault(f.DependencyUnbound)
	}
	if r.Selection.Kind != "direct" && (r.Selection.Kind != "platform" || r.Selection.Selector != mc.MemorySelector && r.Selection.Selector != mc.MeetingSummarySelector && r.Selection.Selector != mc.EmbeddingSelector) {
		return fault(f.CapabilityUnsupported)
	}
	if r.ReasoningEffort != "" {
		return fault(f.CapabilityUnsupported)
	}
	return nil
}
func resolutionProfile(p *providerRecord, m *modelRecord, selection mc.SelectionRef) (string, error) {
	if !p.Input.Enabled || !m.Input.Enabled {
		return "", fault(f.InvalidState)
	}
	c := m.Input.Capabilities
	if selection.Kind == "platform" && selection.Selector == mc.EmbeddingSelector {
		if p.Input.Protocol != mc.OpenAIEmbeddings || m.Input.Type != mc.EmbeddingModel || !emptyObject(p.Input.Options) || !emptyObject(m.Input.Parameters) || !emptyObject(m.Input.RequestOverwrite) || len(m.Input.HeaderOverwrite) != 0 || c.Streaming || c.ToolCalls || c.ParallelToolCalls || c.Reasoning || len(c.ReasoningEfforts) != 0 || len(c.StructuredOutputModes) != 0 || c.MaxOutput != nil || !slices.Equal(c.InputModalities, []string{"text"}) || !slices.Equal(c.OutputModalities, []string{"vector"}) {
			return "", fault(f.CapabilityUnsupported)
		}
		return adapter.OpenAIEmbeddingsFloatRevision, nil
	}
	if p.Input.Protocol != mc.OpenAIChat || m.Input.Type != mc.ChatModel || !emptyObject(p.Input.Options) || !emptyObject(m.Input.Parameters) || !emptyObject(m.Input.RequestOverwrite) || len(m.Input.HeaderOverwrite) != 0 || c.ToolCalls || c.ParallelToolCalls || c.Reasoning || len(c.ReasoningEfforts) != 0 || !slices.Equal(c.InputModalities, []string{"text"}) || !slices.Equal(c.OutputModalities, []string{"text"}) {
		return "", fault(f.CapabilityUnsupported)
	}
	if slices.Contains(c.StructuredOutputModes, "json_schema") {
		return adapter.OpenAIChatStructuredRevision, nil
	}
	if selection.Kind == "platform" && selection.Selector != mc.MeetingSummarySelector {
		return "", fault(f.CapabilityUnsupported)
	}
	if len(c.StructuredOutputModes) != 0 && !slices.Equal(c.StructuredOutputModes, []string{"text"}) {
		return "", fault(f.CapabilityUnsupported)
	}
	return adapter.OpenAIChatTextRevision, nil
}

// These reads collect candidates only. Callers authorize before exposing any
// failure and re-read under the complete returned writer/reader union.
func currentResolutionDraft(ctx context.Context, x postgres.SQLExecutor, r mc.ResolveRequest, snapshot mc.SnapshotID) (resolutionDraft, error) {
	d := resolutionDraft{Format: 1, Snapshot: resolutionSnapshotDTO{Format: 1, ID: snapshot, ProjectID: r.Consumer.ProjectID}}
	if e := resolutionVariant(r); e != nil {
		return d, e
	}
	key := ""
	if r.Selection.Kind == "direct" {
		key = r.ModelRef.String()
	} else if r.Selection.Selector == mc.MeetingSummarySelector {
		selection, e := loadMeetingSummaryState(ctx, x)
		if e != nil {
			return d, e
		}
		if selection == nil {
			return d, fault(f.DependencyUnbound)
		}
		v := selection.Version
		d.Snapshot.SelectionVersion = &v
		if selection.Model == "" {
			return d, fault(f.InvalidState)
		}
		if r.Selection.Version != nil && *r.Selection.Version != v {
			return d, fault(f.ResourceBusy)
		}
		key = selection.Model
	} else {
		selection, e := loadSelection(ctx, x)
		if e != nil {
			return d, e
		}
		v := selection.Version
		d.Snapshot.SelectionVersion = &v
		key = selection.Memory
		if r.Selection.Selector == mc.EmbeddingSelector {
			key = selection.Embedding
		}
		if !selection.Configured || key == "" {
			return d, fault(f.InvalidState)
		}
		if r.Selection.Version != nil && *r.Selection.Version != v {
			return d, fault(f.ResourceBusy)
		}
	}
	m, e := loadModel(ctx, x, key)
	if e != nil {
		return d, e
	}
	if m == nil && r.Selection.Kind == "direct" {
		scope, _ := id.InProject(r.Consumer.ProjectID)
		m, e = loadModelScope(ctx, x, key, scope)
		if e != nil {
			return d, e
		}
	}
	if m == nil {
		return d, fault(f.NotFound)
	}
	p, e := loadProviderScope(ctx, x, m.ProviderID, configurationScope(m.Project))
	if e != nil {
		return d, e
	}
	if p == nil {
		return d, unavailable(nil)
	}
	mid, _ := f.ParseID[mc.Model](m.ID)
	pid, _ := f.ParseID[mc.Provider](p.ID)
	d.ProviderVersion = p.Version
	d.ModelVersion = m.Version
	d.Snapshot.Identity = mc.ModelIdentity{ProviderID: pid, ModelID: mid, ProviderName: p.Input.Name, ModelName: m.Input.Name, ProviderModelID: m.Input.ProviderModelID, Protocol: p.Input.Protocol, Profile: p.Input.Protocol.Profile(), ModelType: m.Input.Type}
	d.Snapshot.Endpoint = p.Input.BaseURL
	d.Snapshot.Parameters = normalizedObject(m.Input.Parameters)
	d.Snapshot.RequestOverwrite = normalizedObject(m.Input.RequestOverwrite)
	d.Snapshot.HeaderOverwrite = m.Input.Clone().HeaderOverwrite
	d.Snapshot.Capabilities = m.Input.Capabilities.Clone()
	d.Snapshot.CredentialID = p.Input.CredentialID
	if p.Input.CredentialID != "" {
		d.Snapshot.CredentialProject = p.Project
	}
	revision, e := resolutionProfile(p, m, *r.Selection)
	if e != nil {
		return d, e
	}
	d.Snapshot.Identity.AdapterRevision = revision
	return d, nil
}
