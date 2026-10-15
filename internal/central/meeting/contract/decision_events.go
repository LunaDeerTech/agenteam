package contract

import (
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	DecisionAggregate     event.StableName = "meeting_decision"
	DecisionRequestedName event.StableName = "meeting.decision.requested"
	DecisionAnsweredName  event.StableName = "meeting.decision.answered"
	DecisionSkippedName   event.StableName = "meeting.decision.skipped"
	DecisionCancelledName event.StableName = "meeting.decision.cancelled"
)

// DecisionPayload identifies the original canonical source. It deliberately
// contains no question, options, answer or approval facts. Receivers reread the
// current authorized Decision before projecting an actionable item.
type DecisionPayload struct {
	DecisionID     DecisionID     `json:"decision_id"`
	MeetingID      MeetingID      `json:"meeting_id"`
	TurnID         TurnID         `json:"turn_id"`
	ContributionID ContributionID `json:"contribution_id"`
	ParticipantID  ParticipantID  `json:"participant_id"`
	ExecutionID    i.ExecutionID  `json:"execution_id"`
	Status         string         `json:"status"`
}

func (p DecisionPayload) Validate() error {
	if p.DecisionID.Validate() != nil || p.MeetingID.Validate() != nil || p.TurnID.Validate() != nil || p.ContributionID.Validate() != nil || p.ParticipantID.Validate() != nil || p.ExecutionID.Validate() != nil {
		return invalid()
	}
	switch p.Status {
	case "pending", "answered", "skipped", "cancelled":
		return nil
	default:
		return invalid()
	}
}

type DecisionEvents struct {
	types map[event.StableName]event.EventType[DecisionPayload]
}

// RegisterDecisionEvents shares the original Catalog and Meeting producer,
// while retaining a separate typed aggregate and canonical state vocabulary.
func RegisterDecisionEvents(catalog *event.Catalog) (DecisionEvents, error) {
	out := DecisionEvents{types: make(map[event.StableName]event.EventType[DecisionPayload])}
	for _, name := range []event.StableName{DecisionRequestedName, DecisionAnsweredName, DecisionSkippedName, DecisionCancelledName} {
		typed, err := event.DefineEvent(catalog, event.Definition[DecisionPayload]{Schema: event.Schema{Producer: MeetingProducer, EventType: name, AggregateType: DecisionAggregate, Version: 1}, Codec: event.JSONCodec[DecisionPayload]{}, Validate: DecisionPayload.Validate})
		if err != nil {
			return DecisionEvents{}, err
		}
		out.types[name] = typed
	}
	return out, nil
}

func (e DecisionEvents) Valid() bool {
	for _, name := range []event.StableName{DecisionRequestedName, DecisionAnsweredName, DecisionSkippedName, DecisionCancelledName} {
		if e.types[name].Schema().EventType != name {
			return false
		}
	}
	return true
}

func decisionEventMatches(h event.Header, p DecisionPayload) bool {
	if p.Validate() != nil || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.Validate() != nil || h.AggregateType != DecisionAggregate || h.AggregateID.String() != p.DecisionID.String() || h.AggregateVersion == nil || h.AggregateVersion.Validate() != nil {
		return false
	}
	switch h.EventType {
	case DecisionRequestedName:
		return p.Status == "pending"
	case DecisionAnsweredName:
		return p.Status == "answered"
	case DecisionSkippedName:
		return p.Status == "skipped"
	case DecisionCancelledName:
		return p.Status == "cancelled"
	default:
		return false
	}
}

func (e DecisionEvents) New(h event.Header, p DecisionPayload) (event.Event, error) {
	if !e.Valid() || !decisionEventMatches(h, p) {
		return event.Event{}, invalid()
	}
	return event.NewEvent(e.types[h.EventType], h, p)
}

func (e DecisionEvents) Decode(v event.Event) (DecisionPayload, error) {
	typed, found := e.types[v.Header().EventType]
	if !found {
		return DecisionPayload{}, invalid()
	}
	p, err := event.DecodeEvent(typed, v)
	if err != nil {
		return DecisionPayload{}, err
	}
	if !decisionEventMatches(v.Header(), p) {
		return DecisionPayload{}, invalid()
	}
	return p, nil
}
