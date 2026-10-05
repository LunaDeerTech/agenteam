package contract

import (
	"encoding/json"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestReferenceBindingAndReplacementClosure(t *testing.T) {
	x := setup(t)
	replacement := fresh[Model](t)
	c := ReferenceChange{Actor: x.actor, Owner: ReferenceOwner{Kind: "agent", ID: x.c.AgentID.String(), ProjectID: &x.c.ProjectID, Role: "agent_model"}, ExpectedOwnerVersion: 1, Before: &x.snapshot.Identity.ModelID, After: &replacement}
	b, e := ReferenceBinding(c)
	must(t, e)
	changed := c.Clone()
	changed.ExpectedOwnerVersion++
	b2, e := ReferenceBinding(changed)
	must(t, e)
	if b == b2 {
		t.Fatal("owner version absent from binding")
	}
	plan, e := NewReferencePlan(NewPlanIssuer(), ReferencePlanDetails{Binding: b, Mapping: digest("b"), Locks: locks(t), Owner: c.Owner, OwnerVersion: 1})
	must(t, e)
	d := ReplacementPlanDetails{Binding: digest("c"), Mapping: digest("d"), Locks: locks(t), ModelID: *c.Before, ModelVersion: 1, Replacement: c.After, Items: []ReplacementItem{{Change: c, Plan: plan}}}
	p, e := NewReplacementPlan(NewPlanIssuer(), d)
	must(t, e)
	*d.Items[0].Change.After = fresh[Model](t)
	if *p.Details().Items[0].Change.After == *d.Items[0].Change.After {
		t.Fatal("replacement aliased mutable input")
	}
	d = p.Details()
	d.Items = append(d.Items, d.Items[0])
	_, e = NewReplacementPlan(NewPlanIssuer(), d)
	reject(t, e)
	d = p.Details()
	d.Items[0].Change.ExpectedOwnerVersion++
	_, e = NewReplacementPlan(NewPlanIssuer(), d)
	reject(t, e)
	d = p.Details()
	d.Items[0].Change.Before = ptr(fresh[Model](t))
	_, e = NewReplacementPlan(NewPlanIssuer(), d)
	reject(t, e)
	var forged ReferencePlan
	reject(t, json.Unmarshal([]byte(`{}`), &forged))
	reject(t, forged.Validate())
	q := p.Details()
	q.Items[0].Change.Owner.ProjectID = ptr(fresh[id.Project](t))
	if *p.Details().Items[0].Change.Owner.ProjectID != x.c.ProjectID {
		t.Fatal("replacement nested alias")
	}
}
func TestRequiredReferencesCannotBeSilentlyCleared(t *testing.T) {
	x := setup(t)
	o := ReferenceOwner{Kind: "project_summary", ID: x.c.ProjectID.String(), ProjectID: &x.c.ProjectID, Role: "meeting_summary"}
	r := ReferenceChange{Actor: x.actor, Owner: o, ExpectedOwnerVersion: 1, Before: &x.snapshot.Identity.ModelID}
	reject(t, r.Validate())
	r.After = ptr(fresh[Model](t))
	must(t, r.Validate())
	r.Owner.ID = fresh[id.Project](t).String()
	reject(t, r.Validate())
	r.Owner = ReferenceOwner{Kind: "platform_selector", ID: fresh[struct{}](t).String(), Role: "reranker"}
	r.After = nil
	must(t, r.Validate())
	r.ReasoningEffort = "high"
	reject(t, r.Validate())
	r.ReasoningEffort = ""
	r.Owner.Role = "embedding"
	reject(t, r.Validate())
}
