package usage

import (
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestM3DurableInitiatorRequiresProjectAndOriginalCause(t *testing.T) {
	const value = "01900000-0000-7000-8000-000000000001"
	for _, name := range []id.ServiceName{id.KnowledgeRuntime, id.MemoryRuntime, id.MeetingRuntime} {
		d := id.ActorDetails{Kind: id.Service, ServiceName: name, ProjectID: value, CauseRef: value}
		if !validInitiator(d) {
			t.Fatal("rejected valid M3 runtime")
		}
		for _, mutate := range []func(*id.ActorDetails){func(d *id.ActorDetails) { d.ProjectID = "" }, func(d *id.ActorDetails) {
			d.CauseRef = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		}, func(d *id.ActorDetails) { d.ExecutionID = value }, func(d *id.ActorDetails) { d.ServiceName = "unregistered" }} {
			bad := d
			mutate(&bad)
			if validInitiator(bad) {
				t.Fatal("durable service accepted forged shape")
			}
		}
	}
}
