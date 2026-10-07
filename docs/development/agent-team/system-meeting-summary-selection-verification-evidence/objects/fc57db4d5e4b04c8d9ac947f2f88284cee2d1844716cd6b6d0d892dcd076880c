package model

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func meetingSummarySemantic(r commandRequest) (f.Digest, error) {
	if r.MeetingSummary == nil || r.MeetingSummary.Validate() != nil || r.Meta.Validate() != nil || r.Meta.Scope.Details().Kind != id.System || r.Kind != "model.selection.update" || r.Provider != "" || r.ProviderInput != nil || r.ModelInput != nil || r.Selection != nil || r.Replacement != "" {
		return "", fault(f.InvalidArgument)
	}
	v := mc.UpdateMeetingSummarySelectionRequest{CommandMeta: r.Meta, SelectionID: r.Resource, ExpectedVersion: r.Expected, Model: *r.MeetingSummary}
	if v.Validate() != nil {
		return "", fault(f.InvalidArgument)
	}
	raw, e := json.Marshal(struct {
		Format          string `json:"format"`
		User            string `json:"user"`
		Kind            string `json:"kind"`
		Resource        string `json:"resource"`
		ExpectedVersion string `json:"expected_version"`
		Model           string `json:"model"`
	}{"model-meeting-summary-command-v1", r.Meta.Actor.Details().UserID, r.Kind, r.Resource, r.Expected.String(), r.MeetingSummary.String()})
	if e != nil {
		return "", unavailable(e)
	}
	return cursor.Digest(raw)
}
func (s *Service) prepareMeetingSummary(ctx context.Context, p *preparedCommand, r commandRequest) (f.Version, error) {
	old, e := loadMeetingSummaryState(ctx, s.state().store)
	if e != nil {
		return 0, e
	}
	if old == nil {
		return 0, fault(f.DependencyUnbound)
	}
	if old.ID != r.Resource {
		return 0, fault(f.NotFound)
	}
	if old.Version != r.Expected {
		return 0, fault(f.VersionConflict)
	}
	version, e := nextVersion(old.Version)
	if e != nil {
		return 0, e
	}
	m, e := loadModel(ctx, s.state().store, r.MeetingSummary.String())
	if e != nil {
		return 0, e
	}
	if m == nil {
		return 0, fault(f.NotFound)
	}
	if e = selectionCompatible("meeting_summary", m.Input, ""); e != nil {
		return 0, e
	}
	provider, e := loadProvider(ctx, s.state().store, m.ProviderID)
	if e != nil {
		return 0, e
	}
	if provider == nil || !provider.Input.Enabled {
		return 0, fault(f.InvalidState)
	}
	if e = modelPolicy(m.Input, provider.Input.Protocol); e != nil {
		return 0, e
	}
	p.plan.BeforeMeetingSummary = old
	p.plan.AfterMeetingSummary = &meetingSummaryRecord{ID: old.ID, Version: version, Model: m.ID, UpdatedAt: p.plan.At}
	p.plan.References = meetingSummaryReferences(old)
	p.plan.Models = append(p.plan.Models, *m)
	p.plan.Providers = append(p.plan.Providers, *provider)
	p.plan.Changed = []string{"selection"}
	return version, nil
}

// Historical plans omit both fields. An explicit malformed/null or mixed new
// branch must not be interpreted as an old plan merely because JSON is valid.
func decodeMeetingSummaryPlan(raw []byte, p *mutationPlan) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return unavailable(nil)
	}
	present := 0
	for k, v := range fields {
		if strings.EqualFold(k, "BeforeMeetingSummary") || strings.EqualFold(k, "AfterMeetingSummary") {
			if k != "BeforeMeetingSummary" && k != "AfterMeetingSummary" {
				return unavailable(nil)
			}
			present++
			if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return unavailable(nil)
			}
			var record meetingSummaryRecord
			d := json.NewDecoder(bytes.NewReader(v))
			d.DisallowUnknownFields()
			if d.Decode(&record) != nil {
				return unavailable(nil)
			}
		}
	}
	if present == 0 {
		return nil
	}
	if present != 2 {
		return unavailable(nil)
	}
	return validateMeetingSummaryPlan(p)
}
func validateMeetingSummaryPlan(p *mutationPlan) error {
	b, a := p.BeforeMeetingSummary, p.AfterMeetingSummary
	if b == nil && a == nil {
		return nil
	}
	if b == nil || a == nil || p.Project != "" || p.BeforeProvider != nil || p.AfterProvider != nil {
		return unavailable(nil)
	}
	if _, e := b.view(); e != nil {
		return unavailable(e)
	}
	if _, e := a.view(); e != nil {
		return unavailable(e)
	}
	version, e := nextVersion(b.Version)
	if e != nil || a.ID != b.ID || a.Version != version || a.Model == "" || a.UpdatedAt != p.At {
		return unavailable(e)
	}
	if p.BeforeSelection != nil && p.BeforeSelection.ID == a.ID || p.AfterSelection != nil && p.AfterSelection.ID == a.ID {
		return unavailable(nil)
	}
	switch p.Kind {
	case "model.selection.update":
		if len(p.Models) != 1 || len(p.Providers) != 1 || p.Receipt.Kind != p.Kind || p.Receipt.AffectedReferences != 0 || p.Metadata.Version != a.Version || p.Metadata.ProviderID != "" || p.Metadata.ModelID != "" || p.Metadata.ReplacementID != "" || p.Metadata.AffectedCount != nil || !sameValue(p.Changed, []string{"selection"}) || !sameValue(p.Metadata.ChangedFields, []string{"selection"}) || p.BeforeModel != nil || p.AfterModel != nil || p.BeforeSelection != nil || p.AfterSelection != nil || p.Resource != a.ID || !sameValue(p.References, meetingSummaryReferences(b)) || p.Receipt.ResourceID != a.ID || p.Receipt.Version != a.Version || p.Metadata.SelectionID != a.ID || p.Metadata.SelectorKind != "platform" {
			return unavailable(nil)
		}
	case "model.delete":
		if e := validateMeetingSummaryDeletion(p); e != nil {
			return e
		}
	default:
		return unavailable(nil)
	}
	matched := false
	for _, m := range p.Models {
		if _, e := m.view(); e != nil {
			return unavailable(e)
		}
		if m.ID == a.Model && m.Project == "" && selectionCompatible("meeting_summary", m.Input, "") == nil {
			for _, provider := range p.Providers {
				if provider.ID == m.ProviderID && provider.Project == "" && provider.Input.Enabled && modelPolicy(m.Input, provider.Input.Protocol) == nil {
					matched = true
				}
			}
		}
	}
	for _, provider := range p.Providers {
		if _, e := provider.view(); e != nil {
			return unavailable(e)
		}
	}
	if !matched {
		return unavailable(nil)
	}
	return nil
}

// Only plans carrying the new Summary branch enter this check. Historical
// plans retain their original loader and never consult today's canonical state.
func validateMeetingSummaryDeletion(p *mutationPlan) error {
	b, a := p.BeforeMeetingSummary, p.AfterMeetingSummary
	if p.BeforeModel == nil || p.AfterModel != nil || p.BeforeModel.Project != "" || p.BeforeModel.ID != p.Resource || b.Model != p.Resource || a.Model == p.Resource || a.Model != p.Metadata.ReplacementID || len(p.Models) != 1 || len(p.Providers) != 2 {
		return unavailable(nil)
	}
	if _, e := p.BeforeModel.view(); e != nil {
		return unavailable(e)
	}
	version, e := nextVersion(p.BeforeModel.Version)
	if e != nil || p.Receipt != (mc.CommandReceipt{Kind: p.Kind, ResourceID: p.Resource, Version: version, AffectedReferences: mc.TokenCount(len(p.References))}) || p.Metadata.ModelID != p.Resource || p.Metadata.ProviderID != p.BeforeModel.ProviderID || p.Metadata.Version != version || p.Metadata.AffectedCount == nil || *p.Metadata.AffectedCount != f.Progress(len(p.References)) || p.Metadata.SelectionID != "" || p.Metadata.SelectorKind != "" || !sameValue(p.Changed, []string{"deleted", "replacement"}) || !sameValue(p.Metadata.ChangedFields, p.Changed) {
		return unavailable(e)
	}
	// The provider snapshots are collected in deletion-target/replacement order.
	if p.Providers[0].ID != p.BeforeModel.ProviderID || p.Providers[0].Project != "" || p.Providers[1].ID != p.Models[0].ProviderID || p.Models[0].ID != a.Model {
		return unavailable(nil)
	}
	expected := meetingSummaryReferences(b)
	old, next := p.BeforeSelection, p.AfterSelection
	if (old == nil) != (next == nil) {
		return unavailable(nil)
	}
	if old != nil {
		if _, e := f.ParseID[struct{}](old.ID); e != nil || !old.Configured || old.ID == b.ID || old.Version.Validate() != nil {
			return unavailable(e)
		}
		if _, e := f.ParseID[mc.Model](old.Embedding); e != nil {
			return unavailable(e)
		}
		if _, e := f.ParseID[mc.Model](old.Memory); e != nil {
			return unavailable(e)
		}
		updated := *old
		updated.Version, e = nextVersion(old.Version)
		if e != nil {
			return unavailable(e)
		}
		changed := false
		for _, ref := range selectionReferences(old) {
			if _, e := f.ParseID[mc.Model](ref.Model); e != nil {
				return unavailable(e)
			}
			if ref.Model != p.Resource {
				continue
			}
			if e := selectionCompatible(ref.Role, p.Models[0].Input, ref.Effort); e != nil {
				return unavailable(e)
			}
			expected = append(expected, ref)
			changed = true
			switch ref.Role {
			case "embedding":
				updated.Embedding = a.Model
			case "memory":
				updated.Memory = a.Model
			case "reranker":
				updated.Reranker = a.Model
			case "image":
				updated.Image = a.Model
			}
		}
		if !changed || updated != *next {
			return unavailable(nil)
		}
	}
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].Owner != expected[j].Owner {
			return expected[i].Owner < expected[j].Owner
		}
		return expected[i].Role < expected[j].Role
	})
	if !sameValue(p.References, expected) {
		return unavailable(nil)
	}
	return nil
}
