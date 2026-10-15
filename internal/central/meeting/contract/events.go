package contract

import (
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const MeetingProducer event.StableName = "meeting"
const MeetingAggregate event.StableName = "meeting"
const ProposedName event.StableName = "meeting.proposed"
const ActivatedName event.StableName = "meeting.activated"
const RestoredName event.StableName = "meeting.restored"
const ArchivedName event.StableName = "meeting.archived"
const DeletedName event.StableName = "meeting.deleted"

type LifecyclePayload struct {
	MeetingID MeetingID `json:"meeting_id"`
	Status    string    `json:"status"`
}

func (v LifecyclePayload) Validate() error {
	if v.MeetingID.Validate() != nil || v.Status != "proposed" && v.Status != "active" && v.Status != "archive" && v.Status != "deleted" {
		return invalid()
	}
	return nil
}

type Events struct {
	types map[event.StableName]event.EventType[LifecyclePayload]
}

func RegisterEvents(catalog *event.Catalog) (Events, error) {
	v := Events{types: map[event.StableName]event.EventType[LifecyclePayload]{}}
	for _, name := range []event.StableName{ProposedName, ActivatedName, RestoredName, ArchivedName, DeletedName} {
		t, err := event.DefineEvent(catalog, event.Definition[LifecyclePayload]{Schema: event.Schema{Producer: MeetingProducer, EventType: name, AggregateType: MeetingAggregate, Version: 1}, Codec: event.JSONCodec[LifecyclePayload]{}, Validate: LifecyclePayload.Validate})
		if err != nil {
			return Events{}, err
		}
		v.types[name] = t
	}
	return v, nil
}
func (v Events) Valid() bool {
	for _, n := range []event.StableName{ProposedName, ActivatedName, RestoredName, ArchivedName, DeletedName} {
		if v.types[n].Schema().EventType != n {
			return false
		}
	}
	return true
}
func (v Events) New(h event.Header, p LifecyclePayload) (event.Event, error) {
	if !v.Valid() || h.AggregateID.String() != p.MeetingID.String() || h.AggregateVersion == nil || h.AggregateVersion.Validate() != nil || !lifecycleStatus(h.EventType, p.Status) {
		return event.Event{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return event.NewEvent(v.types[h.EventType], h, p)
}
func (v Events) Decode(e event.Event) (LifecyclePayload, error) {
	t, ok := v.types[e.Header().EventType]
	if !ok {
		return LifecyclePayload{}, invalid()
	}
	p, err := event.DecodeEvent(t, e)
	if err != nil {
		return LifecyclePayload{}, err
	}
	if !lifecycleStatus(e.Header().EventType, p.Status) || e.Header().AggregateID.String() != p.MeetingID.String() || e.Header().AggregateVersion == nil {
		return LifecyclePayload{}, invalid()
	}
	return p, nil
}
func lifecycleStatus(n event.StableName, s string) bool {
	switch n {
	case ProposedName:
		return s == "proposed"
	case ActivatedName, RestoredName:
		return s == "active"
	case ArchivedName:
		return s == "archive"
	case DeletedName:
		return s == "deleted"
	}
	return false
}
