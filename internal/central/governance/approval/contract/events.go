package contract

import (
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const Producer event.StableName = "governance"
const RequestedName event.StableName = "governance.approval_requested"
const ResolvedName event.StableName = "governance.approval_resolved"
const Aggregate event.StableName = "approval"

type EventPayload struct {
	RequestID   RequestID      `json:"request_id"`
	ExecutionID i.ExecutionID  `json:"execution_id"`
	OperationID tc.OperationID `json:"operation_id"`
	Status      string         `json:"status"`
}

func (v EventPayload) Validate() error {
	if v.RequestID.Validate() != nil || v.ExecutionID.Validate() != nil || v.OperationID.Validate() != nil || (v.Status != Pending && v.Status != Approved && v.Status != Rejected) {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}

type Events struct{ requested, resolved event.EventType[EventPayload] }

func RegisterEvents(catalog *event.Catalog) (Events, error) {
	var v Events
	var err error
	define := func(name event.StableName) (event.EventType[EventPayload], error) {
		return event.DefineEvent(catalog, event.Definition[EventPayload]{Schema: event.Schema{Producer: Producer, EventType: name, AggregateType: Aggregate, Version: 1}, Codec: event.JSONCodec[EventPayload]{}, Validate: EventPayload.Validate})
	}
	v.requested, err = define(RequestedName)
	if err != nil {
		return Events{}, err
	}
	v.resolved, err = define(ResolvedName)
	return v, err
}
func (v Events) Valid() bool {
	return v.requested.Schema().EventType == RequestedName && v.resolved.Schema().EventType == ResolvedName
}
func (v Events) New(h event.Header, p EventPayload) (event.Event, error) {
	if !v.Valid() || h.AggregateID.String() != p.RequestID.String() || h.AggregateVersion == nil || (p.Status == Pending && (*h.AggregateVersion != 1 || h.EventType != RequestedName)) || (p.Status != Pending && (*h.AggregateVersion != 2 || h.EventType != ResolvedName)) {
		return event.Event{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	if p.Status == Pending {
		return event.NewEvent(v.requested, h, p)
	}
	return event.NewEvent(v.resolved, h, p)
}
