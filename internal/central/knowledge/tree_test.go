package knowledge

import (
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
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
