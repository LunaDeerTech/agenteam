package model

import (
	"encoding/json"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelAuditPlanHasExactStableCauseAndCurrentActor(t *testing.T) {
	actor := testActor(t)
	meta := mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "private-key"}
	ci, _ := commandIdentity(meta, "model.delete")
	resource, provider := mustID[mc.Model](t).String(), mustID[mc.Provider](t).String()
	zero := f.Progress(0)
	p := mutationPlan{Identity: ci.Canonical(), Kind: "model.delete", Resource: resource, BeforeModel: &modelRecord{ID: resource, ProviderID: provider}, Metadata: ac.ModelMetadataFields{ModelID: resource, ProviderID: provider, Version: 3, ChangedFields: []string{"deleted"}, AffectedCount: &zero}}
	entry, key, e := auditEntry(actor, &p)
	if e != nil {
		t.Fatal(e)
	}
	if entry.Fields().Action != ac.ModelDelete || entry.Fields().Actor.Details() != actor.Details() || entry.Fields().Resource.Details() != (ac.ResourceDetails{Kind: ac.ModelConfigResource, ID: resource}) || key.Details().Producer != ac.ModelProducer || key.Details().Ordinal != 0 {
		t.Fatal("Audit lost exact command fact")
	}
	uid, _ := f.ParseID[id.User](actor.Details().UserID)
	next, _ := id.NewHuman(uid, mustID[id.Session](t))
	again, againKey, e := auditEntry(next, &p)
	if e != nil || againKey.Details() != key.Details() || again.Fields().Actor.Details() != next.Details() {
		t.Fatal("stable command cause mixed with stale Session")
	}
}

func TestModelMeetingSummaryUsesOriginalPlatformAuditShape(t *testing.T) {
	actor := testActor(t)
	meta := mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "meeting-summary-audit-key"}
	identity, e := commandIdentity(meta, "model.selection.update")
	if e != nil {
		t.Fatal(e)
	}
	plan := summaryPlanFixture(t)
	plan.Identity = identity.Canonical()
	entry, _, e := auditEntry(actor, &plan)
	if e != nil {
		t.Fatal(e)
	}
	fields := entry.Fields()
	if fields.Resource.Details().ID != plan.Resource {
		t.Fatal("audit used old selector identity")
	}
	var values map[string]any
	if json.Unmarshal(fields.Metadata.JSON(), &values) != nil || len(values) != 4 || values["selector_kind"] != "platform" || values["selection_id"] != plan.Resource || values["version"] != "2" {
		t.Fatal("expanded existing Audit closed shape")
	}
}
