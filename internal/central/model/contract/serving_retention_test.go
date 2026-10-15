package contract

import (
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestServingRetentionBindsOwnerSnapshotVersionAndAction(t *testing.T) {
	x := setup(t)
	r := ServingSnapshotRetentionRequest{Actor: x.actor, Owner: ServingSnapshotOwner{Kind: MemoryConsumer, ProjectID: x.c.ProjectID, AgentID: x.c.AgentID, GenerationID: fresh[struct{}](t).String()}, SnapshotID: fresh[Snapshot](t), OwnerVersion: 1, Action: RetainServingSnapshot}
	must(t, r.Validate())
	binding, err := ServingSnapshotRetentionBinding(r)
	must(t, err)
	for name, change := range map[string]func(*ServingSnapshotRetentionRequest){
		"snapshot":   func(v *ServingSnapshotRetentionRequest) { v.SnapshotID = fresh[Snapshot](t) },
		"generation": func(v *ServingSnapshotRetentionRequest) { v.Owner.GenerationID = fresh[struct{}](t).String() },
		"version":    func(v *ServingSnapshotRetentionRequest) { v.OwnerVersion++ },
		"action":     func(v *ServingSnapshotRetentionRequest) { v.Action = ReleaseServingSnapshot },
	} {
		t.Run(name, func(t *testing.T) {
			other := r.Clone()
			change(&other)
			got, err := ServingSnapshotRetentionBinding(other)
			must(t, err)
			if got == binding {
				t.Fatal("changed ownership retained old binding")
			}
		})
	}
	for name, change := range map[string]func(*ServingSnapshotRetentionRequest){
		"non embedding owner":  func(v *ServingSnapshotRetentionRequest) { v.Owner.Kind = MeetingConsumer },
		"memory without agent": func(v *ServingSnapshotRetentionRequest) { v.Owner.AgentID = nil },
		"knowledge with agent": func(v *ServingSnapshotRetentionRequest) { v.Owner.Kind = KnowledgeConsumer },
		"wrong agent":          func(v *ServingSnapshotRetentionRequest) { a := fresh[id.Agent](t); v.Owner.AgentID = &a },
		"wrong project":        func(v *ServingSnapshotRetentionRequest) { v.Owner.ProjectID = fresh[id.Project](t) },
		"zero version":         func(v *ServingSnapshotRetentionRequest) { v.OwnerVersion = 0 },
	} {
		t.Run(name, func(t *testing.T) { other := r.Clone(); change(&other); reject(t, other.Validate()) })
	}
	other := r.Clone()
	*other.Owner.AgentID = fresh[id.Agent](t)
	if *r.Owner.AgentID != *x.c.AgentID {
		t.Fatal("owner clone aliases")
	}
}
