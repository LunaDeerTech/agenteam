//go:build integration

package model_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestModelProjectAvailableDirectoryHasOnlyEnabledSafeUnion(t *testing.T) {
	v := newProjectConfigurationFixture(t)
	ctx := testContext(t)
	ownProvider := v.projectProvider(t, nil)
	own := v.projectModel(t, ownProvider)
	systemCredential := v.credential(t, sc.Model)
	systemProvider := v.provider(t, mc.OpenAIChat, &systemCredential.CredentialRef)
	system := v.model(t, systemProvider, mc.ChatModel)
	systemDisabledModel := v.model(t, systemProvider, mc.ChatModel)
	disabledModelInput := systemDisabledModel.Input.Clone()
	disabledModelInput.Enabled = false
	if _, err := v.service.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: v.meta(t, "disable-system-model"), ID: systemDisabledModel.ID, ExpectedVersion: 1, Input: disabledModelInput}); err != nil {
		t.Fatal(err)
	}
	disabledProvider := v.provider(t, mc.OpenAIChat, nil)
	v.model(t, disabledProvider, mc.ChatModel)
	disabledProviderInput := disabledProvider.Input.Clone()
	disabledProviderInput.Enabled = false
	if _, err := v.service.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: v.meta(t, "disable-system-provider"), ID: disabledProvider.ID, ExpectedVersion: 1, Input: disabledProviderInput}); err != nil {
		t.Fatal(err)
	}
	ownDisabled := v.projectModel(t, ownProvider)
	ownDisabledInput := ownDisabled.Input.Clone()
	ownDisabledInput.Enabled = false
	if _, err := v.service.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: v.projectMeta(t, "disable-own-model"), ID: ownDisabled.ID, ExpectedVersion: 1, Input: ownDisabledInput}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []struct {
		protocol mc.Protocol
		kind     mc.ModelType
	}{{mc.OpenAIEmbeddings, mc.EmbeddingModel}, {mc.JinaRerank, mc.RerankerModel}, {mc.OpenAIImages, mc.ImageModel}} {
		v.model(t, v.provider(t, k.protocol, nil), k.kind)
	}
	foreign := *v
	foreign.project = v.createProject(t, v.owner)
	foreign.scope, _ = id.InProject(foreign.project.ID)
	foreignModel := foreign.projectModel(t, foreign.projectProvider(t, nil))
	// These legal persisted configuration values deliberately exceed the
	// currently implemented conformance profiles. Directory reads must never
	// load or serialize them; this does not claim Provider execution support.
	canaries := []string{"directory-endpoint-canary", "directory-options-canary", "directory-provider-model-canary", "directory-parameters-canary", "directory-request-canary", "directory-header-canary"}
	v.sql(t, `UPDATE agenteam_model.providers SET base_url='https://directory-endpoint-canary.example/api',provider_options='{"private":"directory-options-canary"}' WHERE id=$1`, systemProvider.ID.String())
	v.sql(t, `UPDATE agenteam_model.models SET provider_model_id='directory-provider-model-canary',parameters='{"private":"directory-parameters-canary"}',request_overwrite='{"private":"directory-request-canary"}',header_overwrite='{"X-Test":"directory-header-canary"}' WHERE id=$1`, system.ID.String())
	page, err := v.service.ListAvailableChatModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 100})
	if err != nil || len(page.Items) != 2 || page.NextCursor != "" {
		t.Fatal("enabled own + System exact union", len(page.Items), err)
	}
	want := map[mc.ModelID]id.Scope{own.ID: v.scope, system.ID: id.SystemScope()}
	for _, item := range page.Items {
		scope, ok := want[item.ID]
		if !ok || !scope.Equal(item.Scope) || item.Version != 1 {
			t.Fatal("foreign/disabled/non-chat or wrong version in selection directory")
		}
		delete(want, item.ID)
	}
	if len(want) != 0 {
		t.Fatal("missing enabled chat")
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	canaries = append(canaries, systemCredential.CredentialRef.Details().ID.String(), foreignModel.ID.String(), foreign.project.ID.String(), "credential_ref", "base_url", "provider_model_id", "parameters", "request_overwrite", "header_overwrite", "options")
	for _, canary := range canaries {
		if strings.Contains(string(raw), canary) {
			t.Fatalf("private directory field leaked: %s", canary)
		}
	}
	if reflect.TypeFor[model.AvailableChatModel]().NumField() != 7 {
		t.Fatal("selection DTO grew configuration fields")
	}
	page.Items[0].Capabilities.StructuredOutputModes[0] = "caller-owned-mutation"
	again, err := v.service.ListAvailableChatModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 100})
	if err != nil || again.Items[0].Capabilities.StructuredOutputModes[0] != "json_schema" {
		t.Fatal("directory capability slice escaped", err)
	}
	models, err := v.service.ListProjectModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 100})
	if err != nil || len(models.Items) != 2 {
		t.Fatal("own configuration list must include disabled models", err)
	}
	providers, err := v.service.ListProjectProviders(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 100})
	if err != nil || len(providers.Items) != 1 || providers.Items[0].ID != ownProvider.ID {
		t.Fatal("own provider list scope", err)
	}
	_, err = v.service.ListModels(ctx, v.owner, systemProvider.ID, model.SystemQuery{Limit: 100})
	requireCode(t, err, f.Forbidden)
	v.gate(t, c.Archived)
	archived, err := v.service.ListAvailableChatModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 100})
	if err != nil || len(archived.Items) != 2 {
		t.Fatal("archived selection directory remains Read", err)
	}
}

func TestModelProjectQueryCursorsAndEveryPageCurrentAuthority(t *testing.T) {
	v := newProjectConfigurationFixture(t)
	ctx := testContext(t)
	for range 4 {
		v.projectModel(t, v.projectProvider(t, nil))
	}
	first, err := v.service.ListAvailableChatModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatal(err)
	}
	providers, err := v.service.ListProjectProviders(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 1})
	if err != nil || len(providers.Items) != 1 || providers.NextCursor == "" {
		t.Fatal(err)
	}
	models, err := v.service.ListProjectModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 1})
	if err != nil || len(models.Items) != 1 || models.NextCursor == "" {
		t.Fatal(err)
	}
	late := v.projectModel(t, v.projectProvider(t, nil))
	uid, _ := f.ParseID[id.User](v.owner.Details().UserID)
	newSession := v.session(t, uid)
	next, err := v.service.ListAvailableChatModels(ctx, newSession, v.project.ID, model.ProjectQuery{Cursor: first.NextCursor, Limit: 100})
	if err != nil || len(next.Items) != 3 || next.NextCursor != "" {
		t.Fatal("watermark, changed limit and stable User", len(next.Items), err)
	}
	seen := map[mc.ModelID]bool{first.Items[0].ID: true}
	for _, item := range next.Items {
		if seen[item.ID] || item.ID == late.ID {
			t.Fatal("duplicate/new-watermark row")
		}
		seen[item.ID] = true
	}
	for _, cursor := range []string{providers.NextCursor, models.NextCursor, first.NextCursor + "x"} {
		_, err = v.service.ListAvailableChatModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 1, Cursor: cursor})
		requireCode(t, err, f.CursorInvalid)
	}
	_, err = v.service.ListProjectProviders(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 1, Cursor: models.NextCursor})
	requireCode(t, err, f.CursorInvalid)
	_, err = v.service.ListProjectModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 1, Cursor: first.NextCursor})
	requireCode(t, err, f.CursorInvalid)
	other := v.createProject(t, v.owner)
	_, err = v.service.ListAvailableChatModels(ctx, v.owner, other.ID, model.ProjectQuery{Limit: 1, Cursor: first.NextCursor})
	requireCode(t, err, f.CursorInvalid)
	otherUser := v.human(t, "user")
	otherOwned := v.createProject(t, otherUser)
	_, err = v.service.ListAvailableChatModels(ctx, otherUser, otherOwned.ID, model.ProjectQuery{Limit: 1, Cursor: first.NextCursor})
	requireCode(t, err, f.CursorInvalid)
	_, err = v.service.ListAvailableChatModels(ctx, v.admin, v.project.ID, model.ProjectQuery{Limit: 1, Cursor: first.NextCursor})
	requireCode(t, err, f.NotFound)
	v.revokeSession(t, newSession)
	_, err = v.service.ListAvailableChatModels(ctx, newSession, v.project.ID, model.ProjectQuery{Limit: 1, Cursor: first.NextCursor})
	requireCode(t, err, f.SessionRevoked)
	// Revocation applies independently to configuration pages as well.
	v.revokeSession(t, v.owner)
	_, err = v.service.ListProjectProviders(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 1, Cursor: providers.NextCursor})
	requireCode(t, err, f.SessionRevoked)
	_, err = v.service.ListProjectModels(ctx, v.owner, v.project.ID, model.ProjectQuery{Limit: 1, Cursor: models.NextCursor})
	requireCode(t, err, f.SessionRevoked)
}
