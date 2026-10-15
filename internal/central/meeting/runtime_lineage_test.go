package meeting

import (
	"encoding/json"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

func lineageIdentity[K any](t *testing.T) f.ID[K] {
	t.Helper()
	id, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// These are representation cases, not evidence that user retry/regenerate
// orchestration is implemented. Existing canonical identities must survive
// the same persisted request-body + separate command-meta round trip.
func TestMeetingAttemptIdentityRoundTrip(t *testing.T) {
	project, agent := lineageIdentity[i.Project](t), lineageIdentity[i.Agent](t)
	meeting, turn := lineageIdentity[c.Meeting](t), lineageIdentity[c.Turn](t)
	contribution, participant := lineageIdentity[c.Contribution](t), lineageIdentity[c.Participant](t)
	for _, tc := range []struct {
		name                  string
		generation, attempt   f.Version
		conceptualIndex       int64
		suffix, publicAttempt string
	}{
		{"first", 1, 1, 0, ":1:1", "1"},
		{"next-attempt", 1, 2, 1, ":1:2", "2"},
		{"new-generation", 2, 1, 0, ":2:1", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := launchRequest(project, meeting, turn, contributionRecord{id: contribution, participant: participant, agent: agent, generation: tc.generation, attempt: tc.attempt})
			request.Meta.RequestID = lineageIdentity[f.Request](t)
			if request.Validate() != nil || request.Meta.IdempotencyKey.String() != "meeting:"+contribution.String()+tc.suffix {
				t.Fatal("canonical positive attempt changed the original launch identity")
			}
			before, err := request.Digest()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var restored ec.LaunchRequest
			if err = json.Unmarshal(raw, &restored); err != nil {
				t.Fatal(err)
			}
			restored.Meta = request.Meta
			after, err := restored.Digest()
			if err != nil || after != before || !sameLaunch(request, restored) || *restored.Lineage.ContributionGeneration != tc.generation || *restored.Lineage.ContributionAttempt != tc.attempt {
				t.Fatal("persisted canonical attempt did not round trip", err)
			}
			if int64(*restored.Lineage.ContributionAttempt)-1 != tc.conceptualIndex || f.Version(tc.conceptualIndex+1) != tc.attempt {
				t.Fatal("conceptual index mapping changed")
			}
			view, err := json.Marshal(c.ContributionView{ID: contribution, TurnID: turn, ParticipantID: participant, Generation: tc.generation, Attempt: tc.attempt})
			if err != nil {
				t.Fatal(err)
			}
			var public map[string]json.RawMessage
			if json.Unmarshal(view, &public) != nil || string(public["attempt"]) != `"`+tc.publicAttempt+`"` {
				t.Fatal("HTTP attempt stopped using the positive decimal string")
			}
			zero := f.Version(0)
			restored.Lineage.ContributionAttempt = &zero
			if restored.Validate() == nil {
				t.Fatal("conceptual zero accepted as canonical Execution lineage")
			}
		})
	}
}
