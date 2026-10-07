package model

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func TestModelMeetingSummarySemanticAndLegacyBytes(t *testing.T) {
	user, _ := f.ParseID[id.User]("018f0000-0000-7000-8000-000000000001")
	session, _ := f.ParseID[id.Session]("018f0000-0000-7000-8000-000000000002")
	actor, _ := id.NewHuman(user, session)
	model, _ := f.ParseID[mc.Model]("018f0000-0000-7000-8000-000000000003")
	resource := "018f0000-0000-7000-8000-000000000004"
	r := commandRequest{Meta: mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "shared-key"}, Kind: "model.selection.update", Resource: resource, Expected: 9007199254740993, MeetingSummary: &model}
	want, e := cursor.Digest([]byte(`{"format":"model-meeting-summary-command-v1","user":"018f0000-0000-7000-8000-000000000001","kind":"model.selection.update","resource":"018f0000-0000-7000-8000-000000000004","expected_version":"9007199254740993","model":"018f0000-0000-7000-8000-000000000003"}`))
	if e != nil {
		t.Fatal(e)
	}
	got, e := commandSemantic(r)
	if e != nil || got != want {
		t.Fatal("new semantic wire changed", e)
	}
	same := r
	same.Meta.Actor, _ = id.NewHuman(user, mustID[id.Session](t))
	again, e := commandSemantic(same)
	if e != nil || again != got {
		t.Fatal("session entered stable semantic")
	}
	legacy := r
	legacy.MeetingSummary = nil
	legacy.Selection = &mc.PlatformSelection{ID: resource, Version: r.Expected, Embedding: model, Memory: model}
	old, e := commandSemantic(legacy)
	if e != nil {
		t.Fatal(e)
	}
	oldWant, e := cursor.Digest([]byte(`{"Format":"model-command-v1","User":"018f0000-0000-7000-8000-000000000001","Kind":"model.selection.update","Resource":"018f0000-0000-7000-8000-000000000004","Provider":"","Expected":"9007199254740993","ProviderInput":null,"ModelInput":null,"Selection":{"id":"018f0000-0000-7000-8000-000000000004","version":"9007199254740993","embedding":"018f0000-0000-7000-8000-000000000003","memory":"018f0000-0000-7000-8000-000000000003","reranker":null,"image":null},"Replacement":""}`))
	if e != nil || old != oldWant || old == got {
		t.Fatal("legacy command-v1 changed")
	}
	ci, _ := commandIdentity(r.Meta, r.Kind)
	oldCI, _ := commandIdentity(legacy.Meta, legacy.Kind)
	if ci.Canonical() != oldCI.Canonical() {
		t.Fatal("new selector invented an independent key namespace")
	}
	for _, edit := range []func(*commandRequest){func(r *commandRequest) { r.Kind = "model.update" }, func(r *commandRequest) { r.Selection = legacy.Selection }, func(r *commandRequest) { r.Provider = resource }, func(r *commandRequest) { r.Replacement = model.String() }, func(r *commandRequest) { r.Expected = 0 }} {
		bad := r
		edit(&bad)
		_, e = commandSemantic(bad)
		requireCode(t, e, f.InvalidArgument)
	}
	legacyPlan := summaryPlanFixture(t)
	legacyPlan.BeforeMeetingSummary, legacyPlan.AfterMeetingSummary = nil, nil
	raw, e := json.Marshal(legacyPlan)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "MeetingSummary") {
		t.Fatal("old mutation plan gained serialized fields")
	}
	var p mutationPlan
	if e = decodeMeetingSummaryPlan(raw, &p); e != nil {
		t.Fatal("old plan rejected", e)
	}
}
func summaryPlanFixture(t *testing.T) mutationPlan {
	at, _ := f.NewInstant(time.Now())
	key := mustID[struct{}](t).String()
	modelID, providerID := mustID[mc.Model](t).String(), mustID[mc.Provider](t).String()
	m := modelRecord{ID: modelID, ProviderID: providerID, Version: 1, CreatedAt: at, UpdatedAt: at, Input: mc.ModelInput{Name: "plain text", ProviderModelID: "plain", Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), HeaderOverwrite: map[string]string{}}}
	provider := providerRecord{ID: providerID, Input: providerInput{Name: "provider", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/v1", Enabled: true, Options: json.RawMessage(`{}`)}, Version: 1, CreatedAt: at, UpdatedAt: at}
	actor := testActor(t)
	ci, e := commandIdentity(mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "summary-plan"}, "model.selection.update")
	if e != nil {
		t.Fatal(e)
	}
	return mutationPlan{CommandID: mustID[struct{}](t).String(), User: actor.Details().UserID, Identity: ci.Canonical(), Semantic: hash([]byte("summary-plan")), Kind: "model.selection.update", Resource: key, BeforeMeetingSummary: &meetingSummaryRecord{ID: key, Version: 1, UpdatedAt: at}, AfterMeetingSummary: &meetingSummaryRecord{ID: key, Version: 2, Model: modelID, UpdatedAt: at}, At: at, Models: []modelRecord{m}, Providers: []providerRecord{provider}, References: []referenceRecord{}, Receipt: mc.CommandReceipt{Kind: "model.selection.update", ResourceID: key, Version: 2}, Metadata: ac.ModelMetadataFields{SelectionID: key, SelectorKind: "platform", Version: 2, ChangedFields: []string{"selection"}}, Changed: []string{"selection"}}
}
func TestModelMeetingSummaryPlanClosedBranch(t *testing.T) {
	p := summaryPlanFixture(t)
	if e := validateMeetingSummaryPlan(&p); e != nil {
		t.Fatal(e)
	}
	for name, edit := range map[string]func(*mutationPlan){
		"missing-before": func(p *mutationPlan) { p.BeforeMeetingSummary = nil }, "missing-after": func(p *mutationPlan) { p.AfterMeetingSummary = nil },
		"clear": func(p *mutationPlan) { p.AfterMeetingSummary.Model = "" }, "wrong-version": func(p *mutationPlan) { p.AfterMeetingSummary.Version++ },
		"wrong-id": func(p *mutationPlan) { p.AfterMeetingSummary.ID = mustID[struct{}](t).String() }, "old-selection-mixed": func(p *mutationPlan) { p.BeforeSelection = &selectionRecord{} },
		"provider-mixed": func(p *mutationPlan) { p.BeforeProvider = &p.Providers[0] }, "project": func(p *mutationPlan) { p.Project = mustID[id.Project](t).String() },
		"disabled": func(p *mutationPlan) { p.Models[0].Input.Enabled = false }, "wrong-provider": func(p *mutationPlan) { p.Providers[0].Input.Enabled = false },
		"wrong-receipt": func(p *mutationPlan) { p.Receipt.Version++ }, "wrong-kind": func(p *mutationPlan) { p.Kind = "model.update" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := summaryPlanFixture(t)
			edit(&candidate)
			requireCode(t, validateMeetingSummaryPlan(&candidate), f.DependencyUnavailable)
		})
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip mutationPlan
	if json.Unmarshal(raw, &roundtrip) != nil {
		t.Fatal("plan JSON")
	}
	if e := decodeMeetingSummaryPlan(raw, &roundtrip); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"BeforeMeetingSummary":null}`, `{"BeforeMeetingSummary":{},"AfterMeetingSummary":null}`, `{"beforemeetingsummary":{}}`} {
		var v mutationPlan
		_ = json.Unmarshal([]byte(raw), &v)
		requireCode(t, decodeMeetingSummaryPlan([]byte(raw), &v), f.DependencyUnavailable)
	}
}

func summaryDeletionPlanFixture(t *testing.T, both bool) mutationPlan {
	t.Helper()
	p := summaryPlanFixture(t)
	p.Kind = "model.delete"
	target := p.Models[0]
	target.ID = mustID[mc.Model](t).String()
	p.BeforeModel, p.Resource = &target, target.ID
	p.BeforeMeetingSummary.Model = target.ID
	p.Providers = append(p.Providers, p.Providers[0])
	p.References = meetingSummaryReferences(p.BeforeMeetingSummary)
	if both {
		p.Models[0].Input.Capabilities.StructuredOutputModes = []string{"json_schema"}
		p.BeforeSelection = &selectionRecord{ID: mustID[struct{}](t).String(), Version: 4, Configured: true, Embedding: mustID[mc.Model](t).String(), Memory: target.ID}
		after := *p.BeforeSelection
		after.Version++
		after.Memory = p.AfterMeetingSummary.Model
		p.AfterSelection = &after
		for _, ref := range selectionReferences(p.BeforeSelection) {
			if ref.Model == target.ID {
				p.References = append(p.References, ref)
			}
		}
		sort.Slice(p.References, func(i, j int) bool {
			if p.References[i].Owner != p.References[j].Owner {
				return p.References[i].Owner < p.References[j].Owner
			}
			return p.References[i].Role < p.References[j].Role
		})
	}
	count := f.Progress(len(p.References))
	p.Receipt = mc.CommandReceipt{Kind: p.Kind, ResourceID: target.ID, Version: target.Version + 1, AffectedReferences: mc.TokenCount(count)}
	p.Changed = []string{"deleted", "replacement"}
	p.Metadata = ac.ModelMetadataFields{ProviderID: target.ProviderID, ModelID: target.ID, Version: p.Receipt.Version, ChangedFields: append([]string(nil), p.Changed...), ReplacementID: p.AfterMeetingSummary.Model, AffectedCount: &count}
	user, _ := f.ParseID[id.User](p.User)
	actor, _ := id.NewHuman(user, mustID[id.Session](t))
	ci, _ := commandIdentity(mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "summary-plan"}, p.Kind)
	p.Identity = ci.Canonical()
	return p
}

type summaryPlanStore struct {
	Store
	plan         mutationPlan
	raw, receipt []byte
}

func (s *summaryPlanStore) QueryRow(context.Context, string, ...any) postgres.Row {
	return scanFunc(func(out ...any) error {
		p := s.plan
		*out[0].(*string), *out[1].(*string), *out[2].(*string) = p.CommandID, p.Identity, p.User
		*out[3].(*string), *out[4].(*string), *out[5].(*string) = p.Kind, p.Resource, "committed"
		*out[6].(*f.Digest), *out[7].(*[]byte), *out[8].(*[]byte) = p.Semantic, s.raw, s.receipt
		return nil
	})
}
func loadSummaryPlanFixture(t *testing.T, p mutationPlan, receipt mc.CommandReceipt) (*commandRecord, error) {
	t.Helper()
	raw, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	r, e := json.Marshal(receipt)
	if e != nil {
		t.Fatal(e)
	}
	user, _ := f.ParseID[id.User](p.User)
	actor, _ := id.NewHuman(user, mustID[id.Session](t))
	ci, _ := commandIdentity(mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "summary-plan"}, p.Kind)
	return loadCommand(context.Background(), &summaryPlanStore{plan: p, raw: raw, receipt: r}, ci)
}
func TestModelMeetingSummaryPersistedDeletionPlanAndReceipt(t *testing.T) {
	for _, both := range []bool{false, true} {
		p := summaryDeletionPlanFixture(t, both)
		r, e := loadSummaryPlanFixture(t, p, p.Receipt)
		if e != nil || r == nil || r.Receipt == nil || *r.Receipt != p.Receipt {
			t.Fatal("valid historical new-branch plan", e)
		}
	}
	for name, edit := range map[string]func(*mutationPlan){
		"missing-old-after":      func(p *mutationPlan) { p.AfterSelection = nil },
		"missing-old-before":     func(p *mutationPlan) { p.BeforeSelection = nil },
		"old-version":            func(p *mutationPlan) { p.AfterSelection.Version++ },
		"old-id":                 func(p *mutationPlan) { p.AfterSelection.ID = mustID[struct{}](t).String() },
		"old-other-role-mutated": func(p *mutationPlan) { p.AfterSelection.Embedding = p.AfterMeetingSummary.Model },
		"missing-old-ref":        func(p *mutationPlan) { p.References = meetingSummaryReferences(p.BeforeMeetingSummary) },
		"wrong-role":             func(p *mutationPlan) { p.References[0].Role = "image" },
		"third-owner":            func(p *mutationPlan) { p.References[0].Owner = mustID[struct{}](t).String() },
		"extra-ref":              func(p *mutationPlan) { p.References = append(p.References, p.References[0]) },
		"receipt-kind":           func(p *mutationPlan) { p.Receipt.Kind = "model.update" },
		"receipt-resource":       func(p *mutationPlan) { p.Receipt.ResourceID = p.AfterMeetingSummary.ID },
		"receipt-version":        func(p *mutationPlan) { p.Receipt.Version++ },
		"receipt-count":          func(p *mutationPlan) { p.Receipt.AffectedReferences++ },
		"metadata-model":         func(p *mutationPlan) { p.Metadata.ModelID = p.AfterMeetingSummary.Model },
		"metadata-provider":      func(p *mutationPlan) { p.Metadata.ProviderID = mustID[mc.Provider](t).String() },
		"metadata-version":       func(p *mutationPlan) { p.Metadata.Version++ },
		"metadata-count":         func(p *mutationPlan) { *p.Metadata.AffectedCount++ },
		"changed":                func(p *mutationPlan) { p.Changed = []string{"deleted"} },
		"metadata-changed":       func(p *mutationPlan) { p.Metadata.ChangedFields = []string{"deleted"} },
	} {
		t.Run(name, func(t *testing.T) {
			p := summaryDeletionPlanFixture(t, true)
			edit(&p)
			_, e := loadSummaryPlanFixture(t, p, p.Receipt)
			requireCode(t, e, f.DependencyUnavailable)
		})
	}
	for _, selection := range []bool{false, true} {
		p := summaryDeletionPlanFixture(t, true)
		if selection {
			p = summaryPlanFixture(t)
		}
		for _, edit := range []func(*mc.CommandReceipt){func(r *mc.CommandReceipt) { r.Kind = "model.update" }, func(r *mc.CommandReceipt) { r.ResourceID = mustID[mc.Model](t).String() }, func(r *mc.CommandReceipt) { r.Version++ }, func(r *mc.CommandReceipt) { r.AffectedReferences++ }} {
			r := p.Receipt
			edit(&r)
			if r.Validate() != nil {
				t.Fatal("alternate receipt must be independently valid")
			}
			_, e := loadSummaryPlanFixture(t, p, r)
			requireCode(t, e, f.DependencyUnavailable)
		}
	}
}
