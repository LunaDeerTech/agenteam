package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func fresh[K any](t *testing.T) f.ID[K] {
	t.Helper()
	id, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestStartRejectsDuplicateAgentsAndForgedContent(t *testing.T) {
	agent := fresh[i.Agent](t)
	r := StartRequest{ProjectID: fresh[i.Project](t), Agents: []i.AgentID{agent}, Mode: "sequential", Content: []InlineNode{{Type: "text", Text: "Discuss this change"}}, Meta: f.CommandMeta{RequestID: fresh[f.Request](t), IdempotencyKey: "start-1"}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Agents = append(r.Agents, agent)
	if r.Validate() == nil {
		t.Fatal("duplicate Agent accepted")
	}
	r.Agents = r.Agents[:1]
	r.Content = []InlineNode{{Type: "text", Text: "hello", ParticipantID: fresh[Participant](t).String()}}
	if r.Validate() == nil {
		t.Fatal("text node smuggled author identity")
	}
	r.Content = []InlineNode{{Type: "mention", AgentID: agent.String()}}
	if r.Validate() != nil {
		t.Fatal("draft mention rejected")
	}
	if ValidateContent(r.Content, false) == nil {
		t.Fatal("draft identity accepted in existing session")
	}
}
func TestMeaningfulContentAndTerminalContributions(t *testing.T) {
	for _, nodes := range [][]InlineNode{nil, {{Type: "text", Text: "  \n"}}, {{Type: "unknown", Text: "hello"}}, {{Type: "text", Text: "secret\x00"}}} {
		if ValidateContent(nodes, false) == nil {
			t.Fatal("invalid content accepted")
		}
	}
	for _, state := range []string{"completed", "failed", "cancelled", "skipped"} {
		if !TerminalContribution(state) {
			t.Fatal(state)
		}
	}
	for _, state := range []string{"pending", "waiting_for_agent", "running", "waiting"} {
		if TerminalContribution(state) {
			t.Fatal(state)
		}
	}
}
