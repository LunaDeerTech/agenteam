//go:build integration

package model_test

import (
	"encoding/json"
	"fmt"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func managementSQL(t *testing.T, v *systemHTTPFixture, q string, args ...any) {
	t.Helper()
	if _, err := v.raw.Exec(testContext(t), q, args...); err != nil {
		t.Fatal("owned management fixture SQL", err)
	}
}
func managementImpact(t *testing.T, v *systemHTTPFixture, key string) model.ModelDeletionImpact {
	t.Helper()
	r := v.request(t, v.adminBrowser, "GET", "/api/v1/system/models/"+key+"/deletion-impact", "", nil).want(t, 200)
	if len(r.object(t)) != 6 {
		t.Fatal("impact fields are not closed")
	}
	var out model.ModelDeletionImpact
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatal(err)
	}
	if out.ReferenceGroups == nil {
		t.Fatal("null groups")
	}
	var sum f.Progress
	previous := ""
	for _, g := range out.ReferenceGroups {
		order := g.OwnerKind + "/" + g.Role
		if order <= previous || g.Count <= 0 {
			t.Fatal("noncanonical group")
		}
		previous = order
		sum += g.Count
	}
	if sum != out.ReferenceCount {
		t.Fatal("inexact total")
	}
	return out
}

func managementSelection(t *testing.T, v *systemHTTPFixture) map[string]any {
	t.Helper()
	selection := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-selection", "", nil).want(t, 200).object(t)
	embed := v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel)
	body := httpModelBody(v.provider(t, mc.OpenAIChat), mc.ChatModel)
	body["input"].(map[string]any)["capabilities"].(map[string]any)["structured_output_modes"] = []string{"text", "json_schema"}
	memory := v.request(t, v.adminBrowser, "POST", "/api/v1/system/models", newID[struct{}](t).String(), body).want(t, 200).object(t)["resource_id"].(string)
	change := map[string]any{"id": selection["id"], "expected_version": selection["version"], "embedding": embed, "memory": memory, "reranker": v.model(t, v.provider(t, mc.JinaRerank), mc.RerankerModel), "image": v.model(t, v.provider(t, mc.OpenAIImages), mc.ImageModel)}
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-selection", newID[struct{}](t).String(), change).want(t, 200)
	change["expected_version"] = "2"
	return change
}

func TestSystemModelManagementDeletionImpact(t *testing.T) {
	v := newSystemHTTPFixture(t)
	unused := v.model(t, v.provider(t, mc.OpenAIChat), mc.ChatModel)
	t.Run("empty-and-head", func(t *testing.T) {
		out := managementImpact(t, v, unused)
		if out.ReferenceCount != 0 || len(out.ReferenceGroups) != 0 || out.ReplacementRequirement != "none" || out.DeleteBlocker != nil {
			t.Fatal("empty impact")
		}
		r := v.request(t, v.adminBrowser, "HEAD", "/api/v1/system/models/"+unused+"/deletion-impact", "", nil).want(t, 200)
		if len(r.body) != 0 {
			t.Fatal("HEAD body")
		}
	})
	selection := managementSelection(t, v)
	t.Run("required-optional-and-seven-groups", func(t *testing.T) {
		seen := map[string]bool{}
		for _, role := range []string{"embedding", "memory", "reranker", "image"} {
			out := managementImpact(t, v, selection[role].(string))
			requirement := model.ModelReplacementRequirement("optional")
			if role == "embedding" || role == "memory" {
				requirement = "required"
			}
			if out.ReferenceCount != 1 || out.ReplacementRequirement != requirement || out.DeleteBlocker != nil || len(out.ReferenceGroups) != 1 || out.ReferenceGroups[0].Role != role {
				t.Fatal("platform impact")
			}
			seen[out.ReferenceGroups[0].OwnerKind+"/"+role] = true
		}
		owner, project := newID[struct{}](t).String(), newID[id.Project](t).String()
		memory := selection["memory"].(string)
		// Only the Model-owned index is a fixture; no foreign adapter is bound.
		managementSQL(t, v, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) VALUES('agent',$1,'agent_model',$2,$3,1),('agent',$1,'approval_model',$2,$3,1),('project_summary',$2,'meeting_summary',$2,$3,1)`, owner, project, memory)
		out := managementImpact(t, v, memory)
		if out.ReferenceCount != 4 || len(out.ReferenceGroups) != 4 || out.ReplacementRequirement != "required" || out.DeleteBlocker == nil || *out.DeleteBlocker != "reference_adapter_unbound" {
			t.Fatal("foreign edge aggregation")
		}
		for _, g := range out.ReferenceGroups {
			seen[g.OwnerKind+"/"+g.Role] = true
		}
		if len(seen) != 7 {
			t.Fatal("seven kind/role combinations not covered")
		}
		v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+memory, newID[struct{}](t).String(), map[string]any{"expected_version": "1", "replacement": unused}).problem(t, 503, f.DependencyUnbound)
	})
	t.Run("large-version-is-string", func(t *testing.T) {
		managementSQL(t, v, `UPDATE agenteam_model.models SET version=9007199254740993 WHERE id=$1`, unused)
		out := managementImpact(t, v, unused)
		if out.Version != 9007199254740993 {
			t.Fatal("large integer rounded")
		}
	})
	t.Run("canonical-corruption", func(t *testing.T) {
		for _, kind := range []string{"missing-index", "owner", "version", "extra-owner", "missing-singleton"} {
			t.Run(kind, func(t *testing.T) {
				owner := selection["id"].(string)
				other := newID[struct{}](t).String()
				embed := selection["embedding"].(string)
				switch kind {
				case "missing-index":
					managementSQL(t, v, `DELETE FROM agenteam_model.references WHERE owner_kind='platform_selector' AND role='embedding'`)
					defer managementSQL(t, v, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,model_id,owner_version) VALUES('platform_selector',$1,'embedding',$2,2)`, owner, embed)
				case "owner":
					managementSQL(t, v, `UPDATE agenteam_model.references SET owner_id=$1 WHERE owner_kind='platform_selector' AND role='embedding'`, other)
					defer managementSQL(t, v, `UPDATE agenteam_model.references SET owner_id=$1 WHERE owner_kind='platform_selector' AND role='embedding'`, owner)
				case "version":
					managementSQL(t, v, `UPDATE agenteam_model.references SET owner_version=3 WHERE owner_kind='platform_selector' AND role='embedding'`)
					defer managementSQL(t, v, `UPDATE agenteam_model.references SET owner_version=2 WHERE owner_kind='platform_selector' AND role='embedding'`)
				case "extra-owner":
					managementSQL(t, v, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,model_id,owner_version) VALUES('platform_selector',$1,'embedding',$2,2)`, other, embed)
					defer managementSQL(t, v, `DELETE FROM agenteam_model.references WHERE owner_kind='platform_selector' AND owner_id=$1`, other)
				case "missing-singleton":
					managementSQL(t, v, `DELETE FROM agenteam_model.platform_selection`)
					defer managementSQL(t, v, `INSERT INTO agenteam_model.platform_selection(id,singleton,version,configured,embedding_id,memory_id,reranker_id,image_id,updated_at) VALUES($1,true,2,true,$2,$3,$4,$5,clock_timestamp())`, owner, embed, selection["memory"], selection["reranker"], selection["image"])
				}
				v.request(t, v.adminBrowser, "GET", "/api/v1/system/models/"+unused+"/deletion-impact", "", nil).problem(t, 503, f.DependencyUnavailable)
			})
		}
	})
	t.Run("bounded-exact-count-and-sentinel", func(t *testing.T) {
		project := newID[id.Project](t).String()
		managementSQL(t, v, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) SELECT 'agent',('018f0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'agent_model',$1,$2,1 FROM generate_series(1,10000) n`, project, unused)
		before := v.facts(t)
		out := managementImpact(t, v, unused)
		if out.ReferenceCount != 10000 || len(out.ReferenceGroups) != 1 || out.ReferenceGroups[0].Count != 10000 {
			t.Fatal("10000 truncated")
		}
		httpSameFacts(t, before, v.facts(t))
		managementSQL(t, v, `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) VALUES('agent',$1,'agent_model',$2,$3,1)`, fmt.Sprintf("018f0000-0000-7000-8000-%012x", 10001), project, unused)
		r := v.request(t, v.adminBrowser, "GET", "/api/v1/system/models/"+unused+"/deletion-impact", "", nil)
		r.problem(t, 409, f.ResourceBusy)
		for _, field := range []string{"reference_count", "reference_groups", "model_id"} {
			if _, ok := r.object(t)[field]; ok {
				t.Fatal("sentinel leaked partial impact")
			}
		}
	})
	t.Run("protected-existence", func(t *testing.T) {
		regular := v.addBrowser(t, "user")
		path := "/api/v1/system/models/" + newID[mc.Model](t).String() + "/deletion-impact"
		v.request(t, regular, "GET", path, "", nil).problem(t, 403, f.Forbidden)
		v.request(t, systemHTTPBrowser{}, "GET", path, "", nil).problem(t, 401, f.Unauthenticated)
		v.request(t, v.adminBrowser, "GET", path, "", nil).problem(t, 404, f.NotFound)
	})
}
