//go:build integration

package model_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestModelProjectConfigurationHTTPProjection(t *testing.T) {
	projectConfigurationHTTPTop(t)
	v := newProjectConfigurationHTTP(t)
	ctx := testContext(t)
	base := projectConfigurationHTTPPath(v.project.ID, "")
	empty := v.request(t, v.ownerBrowser, "GET", base+"models").want(t, 200)
	if string(empty.body) != `{"items":[],"next_cursor":null}` {
		t.Fatal("empty Model page")
	}
	projectConfigurationHTTPSchema(t, "ModelPage", empty.body)
	projectConfigurationHTTPExport(t, "empty-models", "GET", base+"models", empty)
	credential := v.projectCredential(t, sc.Model)
	provider := v.projectProvider(t, &credential.CredentialRef)
	own := v.projectModel(t, provider)
	for range 2 {
		v.projectModel(t, v.projectProvider(t, nil))
	}
	disabled := v.projectModel(t, provider)
	disabledInput := disabled.Input.Clone()
	disabledInput.Enabled = false
	if _, e := v.service.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: v.projectMeta(t, "http-disable-model"), ID: disabled.ID, ExpectedVersion: 1, Input: disabledInput}); e != nil {
		t.Fatal(e)
	}
	disabledProvider := v.projectProvider(t, nil)
	hidden := v.projectModel(t, disabledProvider)
	di := disabledProvider.Input.Clone()
	di.Enabled = false
	if _, e := v.service.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: v.projectMeta(t, "http-disable-provider"), ID: disabledProvider.ID, ExpectedVersion: 1, Input: di}); e != nil {
		t.Fatal(e)
	}
	systemSecret := v.credential(t, sc.Model)
	systemProvider := v.provider(t, mc.OpenAIChat, &systemSecret.CredentialRef)
	systemModel := v.model(t, systemProvider, mc.ChatModel)
	v.model(t, v.provider(t, mc.OpenAIEmbeddings, nil), mc.EmbeddingModel)
	foreign := *v.projectConfigurationFixture
	foreign.project = v.createProject(t, v.otherBrowser.actor)
	foreign.owner = v.otherBrowser.actor
	foreign.scope, _ = id.InProject(foreign.project.ID)
	foreignProvider := foreign.projectProvider(t, nil)
	foreignModel := foreign.projectModel(t, foreignProvider)
	canaries := []string{"directory-endpoint-http-canary", "directory-options-http-canary", "directory-model-http-canary", "directory-parameters-http-canary", "directory-request-http-canary", "directory-header-http-canary"}
	v.sql(t, `UPDATE agenteam_model.providers SET base_url='https://directory-endpoint-http-canary.example/api',provider_options='{"private":"directory-options-http-canary"}' WHERE id=$1`, systemProvider.ID.String())
	v.sql(t, `UPDATE agenteam_model.models SET provider_model_id='directory-model-http-canary',parameters='{"private":"directory-parameters-http-canary"}',request_overwrite='{"private":"directory-request-http-canary"}',header_overwrite='{"X-Test":"directory-header-http-canary"}' WHERE id=$1`, systemModel.ID.String())
	before := v.modelCounts(t)
	for _, tc := range []struct{ suffix, schema, name string }{{"model-providers", "ProviderPage", "providers"}, {"model-providers/" + provider.ID.String(), "Provider", "provider"}, {"models", "ModelPage", "models"}, {"models/" + own.ID.String(), "Model", "model"}, {"available-chat-models", "AvailableChatModelPage", "available"}} {
		path := base + tc.suffix
		r := v.request(t, v.ownerBrowser, "GET", path).want(t, 200)
		head := v.request(t, v.ownerBrowser, "HEAD", path).want(t, 200)
		if len(head.body) != 0 || head.headers.Get("Content-Length") != r.headers.Get("Content-Length") {
			t.Fatal("HEAD full representation framing")
		}
		projectConfigurationHTTPSchema(t, tc.schema, r.body)
		projectConfigurationHTTPExport(t, tc.name, "GET", path, r)
	}
	models, _ := projectConfigurationHTTPPage(t, v.request(t, v.ownerBrowser, "GET", base+"models"))
	foundDisabled := false
	for _, row := range models {
		if projectConfigurationHTTPID(t, row) == disabled.ID.String() {
			foundDisabled = true
			if !bytes.Contains(row["input"], []byte(`"enabled":false`)) {
				t.Fatal("disabled Model changed")
			}
		}
	}
	if !foundDisabled {
		t.Fatal("configuration omitted disabled")
	}
	available := v.request(t, v.ownerBrowser, "GET", base+"available-chat-models").want(t, 200)
	items, _ := projectConfigurationHTTPPage(t, available)
	if len(items) != 4 {
		t.Fatal("safe enabled own/System union", len(items))
	}
	for _, row := range items {
		if len(row) != 7 {
			t.Fatal("directory item field count")
		}
		key := projectConfigurationHTTPID(t, row)
		if key == disabled.ID.String() || key == hidden.ID.String() || key == foreignModel.ID.String() {
			t.Fatal("unsafe directory membership")
		}
	}
	canaries = append(canaries, systemSecret.CredentialRef.Details().ID.String(), foreignModel.ID.String(), foreign.project.ID.String(), "credential_ref", "base_url", "provider_model_id", "parameters", "request_overwrite", "header_overwrite", "created_at")
	for _, private := range canaries {
		if bytes.Contains(available.body, []byte(private)) || strings.Contains(v.logs.text(), private) {
			t.Fatal("directory/log configuration leak")
		}
	}
	for _, suffix := range []string{"model-providers/" + systemProvider.ID.String(), "models/" + systemModel.ID.String(), "model-providers/" + foreignProvider.ID.String(), "models/" + foreignModel.ID.String()} {
		v.request(t, v.ownerBrowser, "GET", base+suffix).problem(t, 404, f.NotFound)
	}
	for _, suffix := range []string{"models?limit=01", "models?limit=1&%6cimit=2", "models?provider_id=" + provider.ID.String(), "models?scope=system", "models?", "models/" + own.ID.String() + "?limit=1", "models/not-a-uuid"} {
		v.request(t, v.ownerBrowser, "GET", base+suffix).problem(t, 400, f.InvalidArgument)
	}
	v.request(t, v.ownerBrowser, "POST", base+"models").problem(t, 405, f.MethodNotAllowed)
	v.unchanged(t, before)
	// All three existing query bindings preserve watermark and stable User across
	// a fresh formal Login and a changed legal page size. Later inserts stay out.
	for _, suffix := range []string{"model-providers", "models", "available-chat-models"} {
		t.Run("pagination/"+suffix, func(t *testing.T) {
			all, _ := projectConfigurationHTTPPage(t, v.request(t, v.ownerBrowser, "GET", base+suffix+"?limit=100"))
			first, cursor := projectConfigurationHTTPPage(t, v.request(t, v.ownerBrowser, "GET", base+suffix+"?limit=1"))
			if len(first) != 1 || cursor == "" {
				t.Fatal("no first-page cursor")
			}
			seen := map[string]bool{projectConfigurationHTTPID(t, first[0]): true}
			original := cursor
			laterProvider := v.projectProvider(t, nil)
			v.projectModel(t, laterProvider)
			fresh := v.login(t, v.ownerBrowser.email)
			for cursor != "" {
				rows, next := projectConfigurationHTTPPage(t, v.request(t, fresh, "GET", base+suffix+"?limit=2&cursor="+url.QueryEscape(cursor)))
				for _, row := range rows {
					key := projectConfigurationHTTPID(t, row)
					if seen[key] {
						t.Fatal("duplicate keyset row")
					}
					seen[key] = true
				}
				cursor = next
			}
			if len(seen) != len(all) {
				t.Fatal("watermark/continuation lost scope", len(seen), len(all))
			}
			for _, row := range all {
				if !seen[projectConfigurationHTTPID(t, row)] {
					t.Fatal("keyset omitted initial row")
				}
			}
			for _, other := range []string{"model-providers", "models", "available-chat-models"} {
				if other != suffix {
					v.request(t, v.ownerBrowser, "GET", base+other+"?cursor="+url.QueryEscape(original)).problem(t, 400, f.CursorInvalid)
				}
			}
			v.request(t, v.ownerBrowser, "GET", base+suffix+"?cursor="+url.QueryEscape(original+"tampered")).problem(t, 400, f.CursorInvalid)
			v.request(t, v.otherBrowser, "GET", base+suffix+"?cursor="+url.QueryEscape(original)).problem(t, 404, f.NotFound)
		})
	}
	t.Log("formal Bootstrap/Invitation/Redeem/Login; controlled HTTP writer over real Account/Model/Project Tx; five exact bodies and safe catalog; no Provider execution")
}
func TestModelProjectConfigurationHTTPBoundedRepresentation(t *testing.T) {
	projectConfigurationHTTPTop(t)
	v := newProjectConfigurationHTTP(t)
	provider := v.projectProvider(t, nil)
	m := v.projectModel(t, provider)
	path := projectConfigurationHTTPPath(v.project.ID, "models/"+m.ID.String())
	// This is accepted-domain storage preparation, not a new HTTP mutation or a
	// claim that the preexisting DB/library allocations fit the HTTP DTO budget.
	caps := m.Input.Capabilities.Clone()
	caps.Reasoning = true
	caps.ReasoningEfforts = make([]string, 1<<18)
	for i := range caps.ReasoningEfforts {
		caps.ReasoningEfforts[i] = fmt.Sprintf("r%031x", i)
	}
	if e := caps.Validate(); e != nil {
		t.Fatal("large legal counterexample", e)
	}
	raw, e := json.Marshal(caps)
	if e != nil || len(raw) <= 8<<20 {
		t.Fatal("legal capabilities not beyond cap", e)
	}
	v.sql(t, `UPDATE agenteam_model.models SET capabilities=$2::jsonb WHERE id=$1`, m.ID.String(), raw)
	before := v.modelCounts(t)
	for _, target := range []struct{ name, path string }{
		{"model", path},
		{"models", projectConfigurationHTTPPath(v.project.ID, "models")},
		{"available", projectConfigurationHTTPPath(v.project.ID, "available-chat-models")},
	} {
		t.Run(target.name, func(t *testing.T) {
			for _, method := range []string{"GET", "HEAD"} {
				r := v.request(t, v.ownerBrowser, method, target.path).want(t, 503)
				if method == "GET" {
					r.problem(t, 503, f.DependencyUnavailable)
					if !bytes.Contains(r.body, []byte(`"commit_state":"not_started"`)) {
						t.Fatal("cap reinterpreted completed read")
					}
					projectConfigurationHTTPExport(t, "oversize-"+target.name, method, target.path, r)
				} else if len(r.body) != 0 {
					t.Fatal("oversize HEAD entity")
				}
				if bytes.Contains(r.body, []byte("r00000000")) || bytes.Contains(r.body, []byte(`"input"`)) || bytes.Contains(r.body, []byte(`"items"`)) {
					t.Fatal("oversize success prefix")
				}
			}
		})
	}
	// The exact same persisted large candidate must not replace an actual Read
	// Unknown with the later representation cap. Natural 2s still applies; a
	// pre-HTTP library timeout/abort is a failed cap probe, never a cap PASS.
	t.Run("oversize-original-Unknown", func(t *testing.T) { projectConfigurationHTTPUnknown(t, v, path, 503) })
	v.unchanged(t, before)
}
