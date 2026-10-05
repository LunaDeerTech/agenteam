package model

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type referenceRecord struct {
	Kind, Owner, Role, Project, Model string
	Version                           f.Version
	Effort                            string
}

func selectionReferences(s *selectionRecord) []referenceRecord {
	out := []referenceRecord{}
	if s == nil || !s.Configured {
		return out
	}
	for _, v := range []struct{ role, id string }{{"embedding", s.Embedding}, {"image", s.Image}, {"memory", s.Memory}, {"reranker", s.Reranker}} {
		if v.id != "" {
			out = append(out, referenceRecord{Kind: "platform_selector", Owner: s.ID, Role: v.role, Model: v.id, Version: s.Version})
		}
	}
	return out
}
func loadReferences(ctx context.Context, x postgres.SQLExecutor, where string, value string) ([]referenceRecord, error) {
	rows, e := x.Query(ctx, `SELECT owner_kind,owner_id::text,role,project_id::text,model_id::text,owner_version,reasoning_effort FROM agenteam_model.references WHERE `+where+` ORDER BY owner_kind,owner_id,role`, value)
	if e != nil {
		return nil, unavailable(e)
	}
	defer rows.Close()
	out := []referenceRecord{}
	for rows.Next() {
		var r referenceRecord
		var project *string
		if e = rows.Scan(&r.Kind, &r.Owner, &r.Role, &project, &r.Model, &r.Version, &r.Effort); e != nil {
			return nil, unavailable(e)
		}
		if project != nil {
			r.Project = *project
		}
		out = append(out, r)
	}
	if e = rows.Err(); e != nil {
		return nil, unavailable(e)
	}
	return out, nil
}
func loadModelReferences(ctx context.Context, x postgres.SQLExecutor, model string) ([]referenceRecord, error) {
	return loadReferences(ctx, x, "model_id=$1", model)
}
func loadOwnerReferences(ctx context.Context, x postgres.SQLExecutor, owner string) ([]referenceRecord, error) {
	return loadReferences(ctx, x, "owner_kind='platform_selector' AND owner_id=$1", owner)
}
func (s *Service) prepareReplacement(ctx context.Context, p *preparedCommand, replacement string) error {
	x := s.state().store
	plan := &p.plan
	refs, e := loadModelReferences(ctx, x, plan.Resource)
	if e != nil {
		return e
	}
	plan.References = refs
	for _, ref := range refs {
		if ref.Kind != "platform_selector" {
			return fault(f.DependencyUnbound)
		}
	}
	var next *modelRecord
	if replacement != "" {
		next, e = loadModel(ctx, x, replacement)
		if e != nil {
			return e
		}
		if next == nil {
			return fault(f.NotFound)
		}
		if next.ID == plan.Resource || next.Input.Type != plan.BeforeModel.Input.Type || !next.Input.Enabled {
			return fault(f.InvalidArgument)
		}
		provider, e := loadProvider(ctx, x, next.ProviderID)
		if e != nil {
			return e
		}
		if provider == nil || !provider.Input.Enabled {
			return fault(f.InvalidState)
		}
		plan.Models = append(plan.Models, *next)
		plan.Providers = append(plan.Providers, *provider)
	}
	if len(refs) == 0 {
		return nil
	}
	old, e := loadSelection(ctx, x)
	if e != nil {
		return e
	}
	all, e := loadOwnerReferences(ctx, x, old.ID)
	if e != nil {
		return e
	}
	if !sameValue(all, selectionReferences(old)) {
		return unavailable(nil)
	}
	current := *old
	current.Version, e = nextVersion(old.Version)
	if e != nil {
		return e
	}
	for _, ref := range refs {
		if ref.Owner != old.ID || ref.Version != old.Version || ref.Project != "" {
			return fault(f.ResourceBusy)
		}
		if next == nil && (ref.Role == "embedding" || ref.Role == "memory") {
			return fault(f.InvalidState)
		}
		if next != nil {
			if e = selectionCompatible(ref.Role, next.Input, ref.Effort); e != nil {
				return e
			}
		}
		switch ref.Role {
		case "embedding":
			if current.Embedding != plan.Resource {
				return fault(f.ResourceBusy)
			}
			current.Embedding = replacement
		case "memory":
			if current.Memory != plan.Resource {
				return fault(f.ResourceBusy)
			}
			current.Memory = replacement
		case "reranker":
			if current.Reranker != plan.Resource {
				return fault(f.ResourceBusy)
			}
			current.Reranker = replacement
		case "image":
			if current.Image != plan.Resource {
				return fault(f.ResourceBusy)
			}
			current.Image = replacement
		default:
			return fault(f.DependencyUnbound)
		}
	}
	plan.BeforeSelection = old
	plan.AfterSelection = &current
	return nil
}
func selectionCompatible(role string, m mc.ModelInput, effort string) error {
	want := map[string]mc.ModelType{"embedding": mc.EmbeddingModel, "memory": mc.ChatModel, "reranker": mc.RerankerModel, "image": mc.ImageModel}[role]
	if want == "" || m.Type != want || !m.Enabled {
		return fault(f.InvalidArgument)
	}
	if role == "memory" && !slices.Contains(m.Capabilities.StructuredOutputModes, "json_schema") {
		return fault(f.CapabilityUnsupported)
	}
	if effort != "" && (!m.Capabilities.Reasoning || !slices.Contains(m.Capabilities.ReasoningEfforts, effort)) {
		return fault(f.CapabilityUnsupported)
	}
	return nil
}
func (s *Service) prepareSelectionModels(ctx context.Context, p *preparedCommand, selection *selectionRecord) error {
	for _, ref := range selectionReferences(selection) {
		m, e := loadModel(ctx, s.state().store, ref.Model)
		if e != nil {
			return e
		}
		if m == nil {
			return fault(f.NotFound)
		}
		if e = selectionCompatible(ref.Role, m.Input, ref.Effort); e != nil {
			return e
		}
		provider, e := loadProvider(ctx, s.state().store, m.ProviderID)
		if e != nil {
			return e
		}
		if provider == nil || !provider.Input.Enabled {
			return fault(f.InvalidState)
		}
		p.plan.Models = append(p.plan.Models, *m)
		p.plan.Providers = append(p.plan.Providers, *provider)
	}
	return nil
}
