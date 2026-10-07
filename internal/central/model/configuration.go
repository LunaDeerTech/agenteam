package model

import (
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) prepare(ctx context.Context, r commandRequest, identity f.CommandIdentity, semantic f.Digest) (*preparedCommand, error) {
	x := s.state().store
	id, e := newID()
	if e != nil {
		return nil, e
	}
	at, e := dbNow(ctx, x)
	if e != nil {
		return nil, e
	}
	p := &preparedCommand{authority: s.state().authority, actor: r.Meta.Actor, identity: identity, plan: mutationPlan{Project: r.Meta.Scope.Details().ProjectID, CommandID: id, Identity: identity.Canonical(), User: r.Meta.Actor.Details().UserID, Kind: r.Kind, Resource: r.Resource, Semantic: semantic, At: at}}
	plan := &p.plan
	mode := f.Shared
	if r.Kind == "model.delete" {
		mode = f.Exclusive
	}
	p.locks = []f.LockRequest{commandLock(identity), userLock(plan.User), systemLock("model-references", mode), systemLock("outbox-registration", f.Shared), recordLock(f.CommandRecordLock, "model:"+id)}
	if plan.Project != "" {
		p.locks = append(p.locks, projectLock(plan.Project))
	}
	if plan.Project != "" && r.ModelInput != nil && r.ModelInput.Type != mc.ChatModel {
		return nil, fault(f.InvalidArgument)
	}
	version := f.Version(1)
	switch r.Kind {
	case "provider.create":
		if e = providerPolicyScope(*r.ProviderInput, r.Meta.Scope); e != nil {
			return nil, e
		}
		plan.Resource, e = newID()
		if e != nil {
			return nil, e
		}
		plan.AfterProvider = &providerRecord{Project: plan.Project, ID: plan.Resource, Input: providerFromInput(*r.ProviderInput), Version: 1, CreatedAt: at, UpdatedAt: at}
		plan.Changed = []string{"created"}
	case "provider.update", "provider.delete":
		old, e := loadProviderScope(ctx, x, r.Resource, r.Meta.Scope)
		if e != nil {
			return nil, e
		}
		if old == nil {
			return nil, fault(f.NotFound)
		}
		if old.Version != r.Expected {
			return nil, fault(f.VersionConflict)
		}
		plan.BeforeProvider = old
		version, e = nextVersion(old.Version)
		if e != nil {
			return nil, e
		}
		if r.Kind == "provider.update" {
			if e = providerPolicyScope(*r.ProviderInput, r.Meta.Scope); e != nil {
				return nil, e
			}
			if r.ProviderInput.Protocol != old.Input.Protocol {
				return nil, fault(f.InvalidArgument)
			}
			v := *old
			v.Input = providerFromInput(*r.ProviderInput)
			v.Version = version
			v.UpdatedAt = at
			plan.AfterProvider = &v
			plan.Changed = providerChanges(old.Input, v.Input)
		} else {
			var exists bool
			if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.models WHERE provider_id=$1)`, old.ID).Scan(&exists); e != nil {
				return nil, unavailable(e)
			}
			if exists {
				return nil, fault(f.InvalidState)
			}
			plan.Changed = []string{"deleted"}
		}
	case "model.create":
		provider, e := loadProviderScope(ctx, x, r.Provider, r.Meta.Scope)
		if e != nil {
			return nil, e
		}
		if provider == nil {
			return nil, fault(f.NotFound)
		}
		if e = modelPolicy(*r.ModelInput, provider.Input.Protocol); e != nil {
			return nil, e
		}
		plan.Providers = append(plan.Providers, *provider)
		plan.Resource, e = newID()
		if e != nil {
			return nil, e
		}
		v := normalizedModel(*r.ModelInput)
		plan.AfterModel = &modelRecord{Project: plan.Project, ID: plan.Resource, ProviderID: r.Provider, Input: v, Version: 1, CreatedAt: at, UpdatedAt: at}
		plan.Changed = []string{"created"}
	case "model.update", "model.delete":
		old, e := loadModelScope(ctx, x, r.Resource, r.Meta.Scope)
		if e != nil {
			return nil, e
		}
		if old == nil {
			return nil, fault(f.NotFound)
		}
		if old.Version != r.Expected {
			return nil, fault(f.VersionConflict)
		}
		plan.BeforeModel = old
		provider, e := loadProviderScope(ctx, x, old.ProviderID, r.Meta.Scope)
		if e != nil {
			return nil, e
		}
		if provider == nil {
			return nil, unavailable(nil)
		}
		plan.Providers = append(plan.Providers, *provider)
		version, e = nextVersion(old.Version)
		if e != nil {
			return nil, e
		}
		if r.Kind == "model.update" {
			if r.ModelInput.Type != old.Input.Type {
				return nil, fault(f.InvalidArgument)
			}
			if e = modelPolicy(*r.ModelInput, provider.Input.Protocol); e != nil {
				return nil, e
			}
			v := *old
			v.Input = normalizedModel(*r.ModelInput)
			v.Version = version
			v.UpdatedAt = at
			plan.AfterModel = &v
			plan.Changed = modelChanges(old.Input, v.Input)
		} else {
			plan.Changed = []string{"deleted"}
			if e = s.prepareReplacement(ctx, p, r.Replacement); e != nil {
				return nil, e
			}
			if r.Replacement != "" {
				plan.Changed = append(plan.Changed, "replacement")
			}
		}
	case "model.selection.update":
		if r.MeetingSummary != nil {
			version, e = s.prepareMeetingSummary(ctx, p, r)
			if e != nil {
				return nil, e
			}
			break
		}
		old, e := loadSelection(ctx, x)
		if e != nil {
			return nil, e
		}
		if old.ID != r.Resource {
			return nil, fault(f.NotFound)
		}
		if old.Version != r.Expected {
			return nil, fault(f.VersionConflict)
		}
		plan.BeforeSelection = old
		version, e = nextVersion(old.Version)
		if e != nil {
			return nil, e
		}
		v := selectionRecord{ID: old.ID, Version: version, Configured: true, Embedding: r.Selection.Embedding.String(), Memory: r.Selection.Memory.String()}
		if r.Selection.Reranker != nil {
			v.Reranker = r.Selection.Reranker.String()
		}
		if r.Selection.Image != nil {
			v.Image = r.Selection.Image.String()
		}
		plan.AfterSelection = &v
		plan.Changed = []string{"selection"}
		plan.References, e = loadOwnerReferences(ctx, x, old.ID)
		if e != nil {
			return nil, e
		}
		if !sameValue(plan.References, selectionReferences(old)) {
			return nil, unavailable(nil)
		}
		if e = s.prepareSelectionModels(ctx, p, &v); e != nil {
			return nil, e
		}
	default:
		return nil, fault(f.InvalidArgument)
	}
	if len(plan.Changed) == 0 {
		return nil, fault(f.InvalidArgument)
	}
	plan.Receipt = mc.CommandReceipt{Kind: r.Kind, ResourceID: plan.Resource, Version: version, AffectedReferences: mc.TokenCount(len(plan.References))}
	if r.Kind != "model.delete" {
		plan.Receipt.AffectedReferences = 0
	}
	plan.Metadata = ac.ModelMetadataFields{Version: version, ChangedFields: append([]string(nil), plan.Changed...)}
	switch r.Kind {
	case "provider.create", "provider.update", "provider.delete":
		plan.Metadata.ProviderID = plan.Resource
		p.locks = append(p.locks, aggregateLock(f.ProviderAggregate, plan.Resource, f.Exclusive))
	case "model.create", "model.update", "model.delete":
		model := plan.AfterModel
		if model == nil {
			model = plan.BeforeModel
		}
		plan.Metadata.ProviderID = model.ProviderID
		plan.Metadata.ModelID = model.ID
		p.locks = append(p.locks, aggregateLock(f.ModelConfigAggregate, plan.Resource, f.Exclusive))
		if r.Kind == "model.delete" {
			v := f.Progress(len(plan.References))
			plan.Metadata.AffectedCount = &v
			plan.Metadata.ReplacementID = r.Replacement
		}
	case "model.selection.update":
		plan.Metadata.SelectionID = plan.Resource
		plan.Metadata.SelectorKind = "platform"
	}
	for _, provider := range plan.Providers {
		p.locks = append(p.locks, aggregateLock(f.ProviderAggregate, provider.ID, f.Shared))
	}
	for _, model := range plan.Models {
		p.locks = append(p.locks, aggregateLock(f.ModelConfigAggregate, model.ID, f.Shared))
	}
	if plan.BeforeSelection != nil {
		p.locks = append(p.locks, systemLock("model-platform-selection", f.Exclusive))
		for _, ref := range append(selectionReferences(plan.BeforeSelection), selectionReferences(plan.AfterSelection)...) {
			p.locks = append(p.locks, recordLock(f.ReferenceRecordLock, "model:platform:"+ref.Owner+":"+ref.Role))
		}
	}
	if plan.BeforeMeetingSummary != nil {
		p.locks = append(p.locks, systemLock("model-meeting-summary-selection", f.Exclusive))
		for _, ref := range append(meetingSummaryReferences(plan.BeforeMeetingSummary), meetingSummaryReferences(plan.AfterMeetingSummary)...) {
			p.locks = append(p.locks, recordLock(f.ReferenceRecordLock, "model:platform:"+ref.Owner+":"+ref.Role))
		}
	}
	if e = validateMeetingSummaryPlan(plan); e != nil {
		return nil, e
	}
	if e = s.prepareEvents(p); e != nil {
		return nil, e
	}
	return p, nil
}
func normalizedModel(v mc.ModelInput) mc.ModelInput {
	v = v.Clone()
	v.Parameters = normalizedObject(v.Parameters)
	v.RequestOverwrite = normalizedObject(v.RequestOverwrite)
	if v.HeaderOverwrite == nil {
		v.HeaderOverwrite = map[string]string{}
	}
	return v
}

func (s *Service) validateMapping(ctx context.Context, x postgres.SQLExecutor, p *preparedCommand) error {
	v := &p.plan
	if e := validateMeetingSummaryPlan(v); e != nil {
		return e
	}
	if v.BeforeMeetingSummary != nil {
		current, e := loadMeetingSummaryState(ctx, x)
		if e != nil {
			return e
		}
		if !sameValue(current, v.BeforeMeetingSummary) {
			return fault(f.ResourceBusy)
		}
	}
	if v.BeforeProvider != nil {
		current, e := loadProviderScope(ctx, x, v.BeforeProvider.ID, configurationScope(v.BeforeProvider.Project))
		if e != nil {
			return e
		}
		if !sameValue(current, v.BeforeProvider) {
			return fault(f.ResourceBusy)
		}
	}
	if v.BeforeModel != nil {
		current, e := loadModelScope(ctx, x, v.BeforeModel.ID, configurationScope(v.BeforeModel.Project))
		if e != nil {
			return e
		}
		if !sameValue(current, v.BeforeModel) {
			return fault(f.ResourceBusy)
		}
	}
	for _, old := range v.Providers {
		current, e := loadProviderScope(ctx, x, old.ID, configurationScope(old.Project))
		if e != nil {
			return e
		}
		if !sameValue(current, &old) {
			return fault(f.ResourceBusy)
		}
	}
	for _, old := range v.Models {
		current, e := loadModelScope(ctx, x, old.ID, configurationScope(old.Project))
		if e != nil {
			return e
		}
		if !sameValue(current, &old) {
			return fault(f.ResourceBusy)
		}
	}
	if v.BeforeSelection != nil {
		current, e := loadSelection(ctx, x)
		if e != nil {
			return e
		}
		if !sameValue(current, v.BeforeSelection) {
			return fault(f.ResourceBusy)
		}
	}
	if v.Kind == "model.delete" {
		refs, e := loadModelReferences(ctx, x, v.Resource)
		if e != nil {
			return e
		}
		if !sameValue(refs, v.References) {
			return fault(f.ResourceBusy)
		}
	}
	if v.BeforeSelection != nil {
		refs, e := loadOwnerReferences(ctx, x, v.BeforeSelection.ID)
		if e != nil {
			return e
		}
		if !sameValue(refs, selectionReferences(v.BeforeSelection)) {
			return fault(f.ResourceBusy)
		}
	}
	if v.Kind == "provider.delete" {
		var exists bool
		if e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.models WHERE provider_id=$1)`, v.Resource).Scan(&exists); e != nil {
			return unavailable(e)
		}
		if exists {
			return fault(f.InvalidState)
		}
	}
	return nil
}
func (s *Service) applyConfiguration(ctx context.Context, x postgres.SQLExecutor, p *preparedCommand) error {
	v := &p.plan
	if r := v.AfterProvider; r != nil {
		if v.BeforeProvider == nil {
			_, e := x.Exec(ctx, `INSERT INTO agenteam_model.providers(id,scope,project_id,name,protocol,base_url,enabled,credential_id,provider_options,version,created_at,updated_at) VALUES($1,$11,$12,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, r.ID, r.Input.Name, string(r.Input.Protocol), r.Input.BaseURL, r.Input.Enabled, null(r.Input.CredentialID), []byte(r.Input.Options), int64(r.Version), r.CreatedAt.Time(), r.UpdatedAt.Time(), string(configurationScope(r.Project).Details().Kind), null(r.Project))
			if e != nil {
				return unavailable(e)
			}
		} else {
			_, e := x.Exec(ctx, `UPDATE agenteam_model.providers SET name=$2,base_url=$3,enabled=$4,credential_id=$5,provider_options=$6,version=$7,updated_at=$8 WHERE id=$1`, r.ID, r.Input.Name, r.Input.BaseURL, r.Input.Enabled, null(r.Input.CredentialID), []byte(r.Input.Options), int64(r.Version), r.UpdatedAt.Time())
			if e != nil {
				return unavailable(e)
			}
		}
	}
	if r := v.AfterModel; r != nil {
		headers, _ := json.Marshal(r.Input.HeaderOverwrite)
		caps, _ := json.Marshal(r.Input.Capabilities)
		if v.BeforeModel == nil {
			_, e := x.Exec(ctx, `INSERT INTO agenteam_model.models(id,provider_id,type,name,provider_model_id,enabled,parameters,request_overwrite,header_overwrite,capabilities,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, r.ID, r.ProviderID, string(r.Input.Type), r.Input.Name, r.Input.ProviderModelID, r.Input.Enabled, []byte(r.Input.Parameters), []byte(r.Input.RequestOverwrite), headers, caps, int64(r.Version), r.CreatedAt.Time(), r.UpdatedAt.Time())
			if e != nil {
				return unavailable(e)
			}
		} else {
			_, e := x.Exec(ctx, `UPDATE agenteam_model.models SET name=$2,provider_model_id=$3,enabled=$4,parameters=$5,request_overwrite=$6,header_overwrite=$7,capabilities=$8,version=$9,updated_at=$10 WHERE id=$1`, r.ID, r.Input.Name, r.Input.ProviderModelID, r.Input.Enabled, []byte(r.Input.Parameters), []byte(r.Input.RequestOverwrite), headers, caps, int64(r.Version), r.UpdatedAt.Time())
			if e != nil {
				return unavailable(e)
			}
		}
	}
	if r := v.AfterSelection; r != nil {
		_, e := x.Exec(ctx, `UPDATE agenteam_model.platform_selection SET version=$2,configured=$3,embedding_id=$4,memory_id=$5,reranker_id=$6,image_id=$7,updated_at=$8 WHERE id=$1`, r.ID, int64(r.Version), r.Configured, null(r.Embedding), null(r.Memory), null(r.Reranker), null(r.Image), v.At.Time())
		if e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `DELETE FROM agenteam_model.references WHERE owner_kind='platform_selector' AND owner_id=$1`, r.ID); e != nil {
			return unavailable(e)
		}
		for _, ref := range selectionReferences(r) {
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,model_id,owner_version) VALUES('platform_selector',$1,$2,$3,$4)`, ref.Owner, ref.Role, ref.Model, int64(ref.Version)); e != nil {
				return unavailable(e)
			}
		}
	}
	if v.AfterMeetingSummary != nil {
		if e := applyMeetingSummary(ctx, x, v.AfterMeetingSummary); e != nil {
			return e
		}
	}
	if v.Kind == "model.delete" {
		if _, e := x.Exec(ctx, `DELETE FROM agenteam_model.models WHERE id=$1`, v.Resource); e != nil {
			return unavailable(e)
		}
	}
	if v.Kind == "provider.delete" {
		if _, e := x.Exec(ctx, `DELETE FROM agenteam_model.providers WHERE id=$1`, v.Resource); e != nil {
			return unavailable(e)
		}
	}
	return nil
}
