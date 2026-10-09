package knowledge

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestMovePlanMappingRejectsRelevantTreeDrift(t *testing.T) {
	project := newID[id.Project](t)
	parent := newID[kc.Document](t)
	target := newID[kc.Document](t)
	facts := kc.MoveFacts{Current: kc.ScopeNode{ID: target, ProjectID: project, ContentVersion: 1, Status: kc.Active}, TargetAncestors: []kc.ScopeNode{{ID: parent, ProjectID: project, ContentVersion: 1, Status: kc.Active}}}
	original, err := moveMapping(facts)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*kc.MoveFacts){
		"target content version": func(f *kc.MoveFacts) { f.Current.ContentVersion++ },
		"current parent":         func(f *kc.MoveFacts) { f.Current.ParentID = &parent },
		"ancestor version":       func(f *kc.MoveFacts) { f.TargetAncestors[0].ContentVersion++ },
		"ancestor project":       func(f *kc.MoveFacts) { f.TargetAncestors[0].ProjectID = newID[id.Project](t) },
	} {
		t.Run(name, func(t *testing.T) {
			altered := facts
			altered.TargetAncestors = append([]kc.ScopeNode(nil), facts.TargetAncestors...)
			change(&altered)
			got, err := moveMapping(altered)
			if err != nil || got == original {
				t.Fatal("stale mapping accepted", err)
			}
		})
	}
}

func TestDeletePlanClosesExactScopeAndPersistentObjectIdentities(t *testing.T) {
	project := newID[id.Project](t)
	root, child := newID[kc.Document](t), newID[kc.Document](t)
	now, _ := f.NewInstant(time.Now())
	rows := []documentRow{
		{head: kc.DocumentHead{Active: &kc.DocumentRef{ID: root, ProjectID: project, ContentVersion: 1, Status: kc.Active, ObjectID: newID[oc.StoredObject](t)}}, upload: newID[oc.Upload](t)},
		{head: kc.DocumentHead{Active: &kc.DocumentRef{ID: child, ProjectID: project, ParentDocumentID: &root, ContentVersion: 3, Status: kc.Active, ObjectID: newID[oc.StoredObject](t)}}, upload: newID[oc.Upload](t)},
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].head.Active.ID.String() < rows[b].head.Active.ID.String() })
	plan, err := newDeletePlan(project, root, rows, now)
	if err != nil {
		t.Fatal(err)
	}
	if !deleteRowsMatch(plan, rows) {
		t.Fatal("exact snapshot differs")
	}
	for name, alter := range map[string]func([]documentRow){
		"version": func(v []documentRow) { v[0].head.Active.ContentVersion++ },
		"object":  func(v []documentRow) { v[0].head.Active.ObjectID = newID[oc.StoredObject](t) },
		"upload":  func(v []documentRow) { v[0].upload = newID[oc.Upload](t) },
		"parent":  func(v []documentRow) { x := newID[kc.Document](t); v[0].head.Active.ParentDocumentID = &x },
		"project": func(v []documentRow) { v[0].head.Active.ProjectID = newID[id.Project](t) },
	} {
		t.Run(name, func(t *testing.T) {
			v := append([]documentRow(nil), rows...)
			d := *v[0].head.Active
			v[0].head.Active = &d
			alter(v)
			if deleteRowsMatch(plan, v) {
				t.Fatal("changed scope accepted")
			}
		})
	}
	if deleteRowsMatch(plan, rows[:1]) {
		t.Fatal("partial tree accepted")
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	store := &authorityStore{row: sourceRow{values: []any{raw}}}
	record := &commandRecord{id: newID[command](t), project: project, document: root}
	restored, err := loadDeletePlan(context.Background(), store, record)
	if err != nil || !deleteRowsMatch(restored, rows) {
		t.Fatal("persisted plan lost exact identities", err)
	}
	original, _ := plan.mapping()
	again, _ := restored.mapping()
	if original != again {
		t.Fatal("persisted mapping changed")
	}
	for _, bad := range [][]byte{
		[]byte(strings.TrimSuffix(string(raw), "}") + `,"format":1}`),
		[]byte(strings.TrimSuffix(string(raw), "}") + `,"body":"forbidden"}`),
		[]byte(strings.Replace(string(raw), `"format":1`, `"format":2`, 1)),
	} {
		store.row = sourceRow{values: []any{bad}}
		if _, err = loadDeletePlan(context.Background(), store, record); err == nil {
			t.Fatal("malformed durable plan accepted")
		}
	}
}
