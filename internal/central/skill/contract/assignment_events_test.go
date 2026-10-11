package contract

import (
	"encoding/json"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

func assignmentEventID[T any](t *testing.T, s string) f.ID[T] {
	t.Helper()
	v, e := f.ParseID[T](s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestSkillAssignmentTypedEventsPreserveClosedConfigurationFacts(t *testing.T) {
	catalog := ec.NewCatalog()
	events, e := RegisterAssignmentEvents(catalog)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = RegisterAssignmentEvents(catalog); e == nil {
		t.Fatal("duplicate assignment schemas accepted")
	}
	revision := sampleRevision(t)
	agent := assignmentEventID[i.Agent](t, "01900000-0000-7000-8000-000000000031")
	assignment := assignmentEventID[Assignment](t, "01900000-0000-7000-8000-000000000032")
	change := assignmentEventID[AssignmentChange](t, "01900000-0000-7000-8000-000000000033")
	p := AssignmentEventPayload{AgentID: agent, AssignmentID: assignment, AssignmentSequence: 2, SkillID: revision.SkillID, ChangeID: change}
	version := f.Version(2)
	eventID := assignmentEventID[ec.EventIdentity](t, change.String())
	scope := assignmentEventID[ec.Project](t, revision.ProjectID.String())
	aggregate := assignmentEventID[ec.Aggregate](t, agent.String())
	h := ec.Header{EventID: eventID, EventType: AssignmentAddedEvent, SchemaVersion: 1, OccurredAt: revision.PublishedAt, Scope: ec.Scope{Kind: ec.ProjectScope, ProjectID: scope}, AggregateType: AssignmentAggregate, AggregateID: aggregate, AggregateVersion: &version}
	for _, name := range []string{"enabled", "disabled-config", "removed"} {
		t.Run(name, func(t *testing.T) {
			v := p
			header := h
			if name == "enabled" {
				r := revision.Revision
				v.FixedRevision = &r
			}
			if name == "removed" {
				header.EventType = AssignmentRemovedEvent
			}
			raw, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			var round AssignmentEventPayload
			if json.Unmarshal(raw, &round) != nil || round.Validate() != nil {
				t.Fatal("nullable fixed revision did not round trip")
			}
			event, e := events.New(header, v)
			if e != nil || event.Validate() != nil {
				t.Fatal("typed assignment fact rejected", e)
			}
			bad := strings.Replace(string(raw), `"change_id":`, `"extra":true,"change_id":`, 1)
			if json.Unmarshal([]byte(bad), &round) == nil {
				t.Fatal("unknown field accepted")
			}
			bad = strings.Replace(string(raw), `"agent_id":`, `"agent_id":"`+agent.String()+`","agent_id":`, 1)
			if json.Unmarshal([]byte(bad), &round) == nil {
				t.Fatal("duplicate field accepted")
			}
		})
	}
	r := revision.Revision
	p.FixedRevision = &r
	h.EventType = AssignmentRemovedEvent
	if _, e = events.New(h, p); e == nil {
		t.Fatal("remove claimed a newly fixed runtime revision")
	}
}
