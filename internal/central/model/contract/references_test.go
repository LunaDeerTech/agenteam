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

func TestMeetingSummaryReferenceRequiredRoleAndOwnerClosure(t *testing.T) {
	x := setup(t)
	r := ReferenceChange{Actor: x.actor, Owner: ReferenceOwner{Kind: "platform_selector", ID: fresh[struct{}](t).String(), Role: "meeting_summary"}, ExpectedOwnerVersion: 1, After: ptr(fresh[Model](t))}
	must(t, r.Validate()) // Shape only: this does not grant the Agent actor authority.
	r.Before = ptr(x.snapshot.Identity.ModelID)
	must(t, r.Validate())
	for _, tc := range []struct {
		name string
		edit func(*ReferenceChange)
	}{
		{"clear", func(r *ReferenceChange) { r.After = nil }},
		{"reasoning", func(r *ReferenceChange) { r.ReasoningEffort = "high" }},
		{"project-on-platform", func(r *ReferenceChange) { r.Owner.ProjectID = ptr(x.c.ProjectID) }},
		{"agent-role", func(r *ReferenceChange) { r.Owner.Kind = "agent"; r.Owner.ProjectID = ptr(x.c.ProjectID) }},
		{"legacy-without-project", func(r *ReferenceChange) { r.Owner.Kind = "project_summary" }},
		{"unknown-role", func(r *ReferenceChange) { r.Owner.Role = "summary" }},
		{"unknown-kind", func(r *ReferenceChange) { r.Owner.Kind = "meeting_summary" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := r.Clone()
			tc.edit(&changed)
			reject(t, changed.Validate())
		})
	}
	for _, role := range []string{"embedding", "memory", "reranker", "image"} {
		t.Run("legacy-"+role, func(t *testing.T) {
			changed := r.Clone()
			changed.Owner.Role = role
			must(t, changed.Validate())
			changed.After = nil
			if role == "reranker" || role == "image" {
				must(t, changed.Validate())
			} else {
				reject(t, changed.Validate())
			}
		})
	}
	r.Owner = ReferenceOwner{Kind: "project_summary", ID: x.c.ProjectID.String(), ProjectID: ptr(x.c.ProjectID), Role: "meeting_summary"}
	must(t, r.Validate())
	r.After = nil
	reject(t, r.Validate())
}

func TestMeetingSummaryReferenceBindingAndPlans(t *testing.T) {
	r := ReferenceChange{Actor: setup(t).actor, Owner: ReferenceOwner{Kind: "platform_selector", ID: fresh[struct{}](t).String(), Role: "meeting_summary"}, ExpectedOwnerVersion: 7, Before: ptr(fresh[Model](t)), After: ptr(fresh[Model](t))}
	b, err := ReferenceBinding(r)
	must(t, err)
	for _, tc := range []struct {
		name string
		edit func(*ReferenceChange)
	}{
		{"owner", func(r *ReferenceChange) { r.Owner.ID = fresh[struct{}](t).String() }},
		{"role", func(r *ReferenceChange) { r.Owner.Role = "memory" }},
		{"version", func(r *ReferenceChange) { r.ExpectedOwnerVersion++ }},
		{"before", func(r *ReferenceChange) { r.Before = ptr(fresh[Model](t)) }},
		{"after", func(r *ReferenceChange) { r.After = ptr(fresh[Model](t)) }},
		{"actor", func(r *ReferenceChange) { r.Actor = setup(t).actor }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := r.Clone()
			tc.edit(&changed)
			next, err := ReferenceBinding(changed)
			must(t, err)
			if next == b {
				t.Fatal("reference binding omitted changed fact")
			}
		})
	}
	i := NewPlanIssuer()
	mapping := digest("b")
	plan, err := NewReferencePlan(i, ReferencePlanDetails{Binding: b, Mapping: mapping, Locks: locks(t), Owner: r.Owner, OwnerVersion: r.ExpectedOwnerVersion})
	must(t, err)
	if !plan.Matches(i, b, mapping) || plan.Matches(NewPlanIssuer(), b, mapping) {
		t.Fatal("plan issuer binding changed")
	}
	replacement, err := NewReplacementPlan(i, ReplacementPlanDetails{Binding: digest("c"), Mapping: mapping, Locks: locks(t), ModelID: *r.Before, ModelVersion: 1, Replacement: r.After, Items: []ReplacementItem{{Change: r, Plan: plan}}})
	must(t, err)
	before := *r.After
	*r.After = fresh[Model](t)
	if got := replacement.Details(); *got.Replacement != before || *got.Items[0].Change.After != before || got.Items[0].Change.Owner.Role != "meeting_summary" {
		t.Fatal("replacement did not freeze summary reference")
	}
	changed := replacement.Details()
	changed.Items[0].Change.After = nil
	changed.Replacement = nil
	_, err = NewReplacementPlan(i, changed)
	reject(t, err)
	changed = replacement.Details()
	changed.Items[0].Change.Owner.Role = "memory"
	_, err = NewReplacementPlan(i, changed)
	reject(t, err)
}
