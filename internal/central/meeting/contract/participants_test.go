package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestParticipantsRequireExplicitDistinctRoster(t *testing.T) {
	r := UpdateParticipantsRequest{ProjectID: fresh[i.Project](t), MeetingID: fresh[Meeting](t), ExpectedVersion: 1, Meta: f.CommandMeta{RequestID: fresh[f.Request](t), IdempotencyKey: "members-1"}}
	if r.Validate() == nil {
		t.Fatal("omitted roster would remove all Agents")
	}
	r.Agents = []i.AgentID{}
	if err := r.Validate(); err != nil {
		t.Fatal("explicit removal", err)
	}
	a, b := fresh[i.Agent](t), fresh[i.Agent](t)
	r.Agents = []i.AgentID{b, a}
	if err := r.Validate(); err != nil {
		t.Fatal("ordered roster", err)
	}
	r.Agents = []i.AgentID{a, a}
	if r.Validate() == nil {
		t.Fatal("duplicate membership")
	}
	r.Agents = []i.AgentID{{}}
	if r.Validate() == nil {
		t.Fatal("invalid Agent identity")
	}
}
