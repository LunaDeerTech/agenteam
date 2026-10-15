package contract

import (
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
	"time"
)

func TestDecisionEventsKeepCanonicalSourceAndState(t *testing.T) {
	catalog := event.NewCatalog()
	proposals, err := RegisterEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := RegisterDecisionEvents(catalog)
	if err != nil || !decisions.Valid() {
		t.Fatal("Decision catalog did not register alongside proposals", err)
	}
	p := DecisionPayload{DecisionID: fresh[Decision](t), MeetingID: fresh[Meeting](t), TurnID: fresh[Turn](t), ContributionID: fresh[Contribution](t), ParticipantID: fresh[Participant](t), ExecutionID: fresh[i.Execution](t)}
	aggregate, _ := f.ParseID[event.Aggregate](p.DecisionID.String())
	at, _ := f.NewInstant(time.Now())
	version := f.Version(2)
	for name, status := range map[event.StableName]string{DecisionRequestedName: "pending", DecisionAnsweredName: "answered", DecisionSkippedName: "skipped", DecisionCancelledName: "cancelled"} {
		t.Run(string(name), func(t *testing.T) {
			p.Status = status
			h := event.Header{EventID: fresh[event.EventIdentity](t), EventType: name, SchemaVersion: 1, OccurredAt: at, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: fresh[event.Project](t)}, AggregateType: DecisionAggregate, AggregateID: aggregate, AggregateVersion: &version}
			actual, err := decisions.New(h, p)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decisions.Decode(actual)
			if err != nil || decoded != p {
				t.Fatal("Decision event changed its original full source", err)
			}
			if _, err := proposals.Decode(actual); err == nil {
				t.Fatal("Decision was accepted as a proposal lifecycle event")
			}
			foreign := h
			foreign.AggregateID = fresh[event.Aggregate](t)
			if _, err := decisions.New(foreign, p); err == nil {
				t.Fatal("foreign Decision aggregate accepted")
			}
			foreign = h
			foreign.AggregateVersion = nil
			if _, err := decisions.New(foreign, p); err == nil {
				t.Fatal("unversioned Decision event accepted")
			}
			for _, invalidStatus := range []string{"approved", "rejected", "invalidated", "expired", "active"} {
				changed := p
				changed.Status = invalidStatus
				if _, err := decisions.New(h, changed); err == nil {
					t.Fatal("invented Decision state accepted", invalidStatus)
				}
			}
			changed := p
			changed.Status = "pending"
			if status == "pending" {
				changed.Status = "cancelled"
			}
			if _, err := decisions.New(h, changed); err == nil {
				t.Fatal("event name and canonical Decision status diverged")
			}
		})
	}
}
