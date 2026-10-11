package contract

import (
	"bytes"
	"encoding/json"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"io"
)

const (
	AssignmentProducer     ec.StableName = "skill"
	AssignmentAggregate    ec.StableName = "skill.assignment"
	AssignmentAddedEvent   ec.StableName = "skill.assignment_added"
	AssignmentRemovedEvent ec.StableName = "skill.assignment_removed"
)

type AssignmentEventPayload struct {
	AgentID            i.AgentID          `json:"agent_id"`
	AssignmentID       AssignmentID       `json:"assignment_id"`
	AssignmentSequence f.Version          `json:"assignment_sequence"`
	SkillID            SkillID            `json:"skill_id"`
	FixedRevision      *f.Revision        `json:"fixed_revision"`
	ChangeID           AssignmentChangeID `json:"change_id"`
}

func (p AssignmentEventPayload) Validate() error {
	if p.AgentID.Validate() != nil || p.AssignmentID.Validate() != nil || p.AssignmentSequence < 2 || p.SkillID.Validate() != nil || p.ChangeID.Validate() != nil || p.FixedRevision != nil && p.FixedRevision.Validate() != nil {
		return invalid()
	}
	return nil
}
func (p AssignmentEventPayload) MarshalJSON() ([]byte, error) {
	if p.Validate() != nil {
		return nil, invalid()
	}
	type wire AssignmentEventPayload
	return json.Marshal(wire(p))
}
func (p *AssignmentEventPayload) UnmarshalJSON(raw []byte) error {
	if p == nil || len(raw) > 4096 {
		return invalid()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return invalid()
	}
	seen := map[string]bool{}
	for d.More() {
		token, e := d.Token()
		name, ok := token.(string)
		if e != nil || !ok || seen[name] {
			return invalid()
		}
		switch name {
		case "agent_id", "assignment_id", "assignment_sequence", "skill_id", "fixed_revision", "change_id":
		default:
			return invalid()
		}
		seen[name] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || name != "fixed_revision" && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return invalid()
		}
	}
	last, e := d.Token()
	if e != nil || last != json.Delim('}') || len(seen) != 6 || d.Decode(new(any)) != io.EOF {
		return invalid()
	}
	type wire AssignmentEventPayload
	var v wire
	if json.Unmarshal(raw, &v) != nil {
		return invalid()
	}
	next := AssignmentEventPayload(v)
	if next.Validate() != nil {
		return invalid()
	}
	*p = next
	return nil
}

type AssignmentEvents struct {
	catalog        *ec.Catalog
	added, removed ec.EventType[AssignmentEventPayload]
}

func RegisterAssignmentEvents(c *ec.Catalog) (AssignmentEvents, error) {
	if !c.Valid() {
		return AssignmentEvents{}, invalid()
	}
	var out AssignmentEvents
	out.catalog = c
	for _, name := range []ec.StableName{AssignmentAddedEvent, AssignmentRemovedEvent} {
		for _, s := range c.Schemas() {
			if s.EventType == name && s.Version == 1 {
				return AssignmentEvents{}, invalid()
			}
		}
	}
	makeType := func(name ec.StableName) (ec.EventType[AssignmentEventPayload], error) {
		return ec.DefineEvent(c, ec.Definition[AssignmentEventPayload]{Schema: ec.Schema{Producer: AssignmentProducer, EventType: name, AggregateType: AssignmentAggregate, Version: 1}, Codec: ec.JSONCodec[AssignmentEventPayload]{}, Validate: AssignmentEventPayload.Validate})
	}
	var err error
	out.added, err = makeType(AssignmentAddedEvent)
	if err != nil {
		return AssignmentEvents{}, err
	}
	out.removed, err = makeType(AssignmentRemovedEvent)
	return out, err
}
func (v AssignmentEvents) Valid() bool { return v.catalog != nil && v.catalog.Valid() }
func ValidateAssignmentEventHeader(h ec.Header) error {
	if h.Validate() != nil || h.SchemaVersion != 1 || h.EventType != AssignmentAddedEvent && h.EventType != AssignmentRemovedEvent || h.AggregateType != AssignmentAggregate || h.Scope.Kind != ec.ProjectScope || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil {
		return invalid()
	}
	return nil
}
func (v AssignmentEvents) New(h ec.Header, p AssignmentEventPayload) (ec.Event, error) {
	if !v.Valid() || ValidateAssignmentEventHeader(h) != nil || p.Validate() != nil || h.AggregateID.String() != p.AgentID.String() {
		return ec.Event{}, invalid()
	}
	if h.EventType == AssignmentAddedEvent {
		return ec.NewEvent(v.added, h, p)
	}
	if p.FixedRevision != nil {
		return ec.Event{}, invalid()
	}
	return ec.NewEvent(v.removed, h, p)
}
