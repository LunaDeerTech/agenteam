package contract

import (
	"fmt"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"io"
	"log/slog"
)

type ContentChange string

const (
	ContentCreated ContentChange = "created"
	TitleChanged   ContentChange = "title"
	SourceChanged  ContentChange = "source"
)

type ContentChangedPayload struct {
	DocumentID     DocumentID      `json:"document_id"`
	ContentVersion f.Version       `json:"content_version"`
	Changes        []ContentChange `json:"changes"`
	ObjectID       oc.ObjectID     `json:"object_id"`
}

func (p ContentChangedPayload) Validate() error {
	if p.DocumentID.Validate() != nil || p.ContentVersion.Validate() != nil || p.ObjectID.Validate() != nil || len(p.Changes) < 1 || len(p.Changes) > 2 {
		return invalid()
	}
	if len(p.Changes) == 2 {
		if p.Changes[0] != TitleChanged || p.Changes[1] != SourceChanged {
			return invalid()
		}
		return nil
	}
	return p.Changes[0].Validate()
}

type DeletedPayload struct {
	DocumentID     DocumentID `json:"document_id"`
	ContentVersion f.Version  `json:"content_version"`
}

func (p DeletedPayload) Validate() error {
	if p.DocumentID.Validate() != nil || p.ContentVersion.Validate() != nil {
		return invalid()
	}
	return nil
}

const (
	KnowledgeProducer   event.StableName = "knowledge"
	KnowledgeAggregate  event.StableName = "knowledge_document"
	ContentChangedEvent event.StableName = "knowledge.content_changed"
	DeletedEvent        event.StableName = "knowledge.deleted"
)

type knowledgeEventData struct {
	content event.EventType[ContentChangedPayload]
	deleted event.EventType[DeletedPayload]
}
type KnowledgeEvents struct{ data func() knowledgeEventData }

// Valid reports whether these event types were registered in a real catalog.
// It does not authorize any event or prove a persistent producer fact.
func (e KnowledgeEvents) Valid() bool { return e.data != nil }

func RegisterKnowledgeEvents(c *event.Catalog) (KnowledgeEvents, error) {
	if !c.Valid() {
		return KnowledgeEvents{}, invalid()
	}
	for _, s := range c.Schemas() {
		if s.EventType == ContentChangedEvent || s.EventType == DeletedEvent {
			return KnowledgeEvents{}, invalid()
		}
	}
	content, e := event.DefineEvent(c, event.Definition[ContentChangedPayload]{Schema: event.Schema{Producer: KnowledgeProducer, EventType: ContentChangedEvent, AggregateType: KnowledgeAggregate, Version: 1}, Codec: event.JSONCodec[ContentChangedPayload]{}, Validate: ContentChangedPayload.Validate})
	if e != nil {
		return KnowledgeEvents{}, e
	}
	deleted, e := event.DefineEvent(c, event.Definition[DeletedPayload]{Schema: event.Schema{Producer: KnowledgeProducer, EventType: DeletedEvent, AggregateType: KnowledgeAggregate, Version: 1}, Codec: event.JSONCodec[DeletedPayload]{}, Validate: DeletedPayload.Validate})
	if e != nil {
		return KnowledgeEvents{}, e
	}
	return KnowledgeEvents{func() knowledgeEventData { return knowledgeEventData{content, deleted} }}, nil
}
func knowledgeHeader(h event.Header, document DocumentID, version f.Version) error {
	if h.Validate() != nil || h.Scope.Kind != event.ProjectScope || h.AggregateID.String() != document.String() || h.AggregateVersion == nil || *h.AggregateVersion != version || h.AggregateSequence != nil {
		return invalid()
	}
	return nil
}
func (e KnowledgeEvents) ContentChanged(h event.Header, p ContentChangedPayload) (event.Event, error) {
	if e.data == nil || p.Validate() != nil || knowledgeHeader(h, p.DocumentID, p.ContentVersion) != nil {
		return event.Event{}, invalid()
	}
	return event.NewEvent(e.data().content, h, p)
}
func (e KnowledgeEvents) Deleted(h event.Header, p DeletedPayload) (event.Event, error) {
	if e.data == nil || p.Validate() != nil || knowledgeHeader(h, p.DocumentID, p.ContentVersion) != nil {
		return event.Event{}, invalid()
	}
	return event.NewEvent(e.data().deleted, h, p)
}

func (v ContentChange) Validate() error              { return oneOf(v, ContentCreated, TitleChanged, SourceChanged) }
func (v ContentChange) MarshalJSON() ([]byte, error) { return enumJSON(v, v.Validate()) }
func (v *ContentChange) UnmarshalJSON(b []byte) error {
	n, e := enumDecode(b, ContentChange.Validate)
	if e == nil {
		*v = n
	}
	return e
}

func (v ContentChangedPayload) MarshalJSON() ([]byte, error) {
	type wire ContentChangedPayload
	return checked(wire(v), v.Validate())
}
func (v *ContentChangedPayload) UnmarshalJSON(b []byte) error {
	type wire ContentChangedPayload
	w, e := decode[wire](b, []string{"document_id", "content_version", "changes", "object_id"}, nil, nil)
	if e != nil {
		return e
	}
	n := ContentChangedPayload(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v DeletedPayload) MarshalJSON() ([]byte, error) {
	type wire DeletedPayload
	return checked(wire(v), v.Validate())
}
func (v *DeletedPayload) UnmarshalJSON(b []byte) error {
	type wire DeletedPayload
	w, e := decode[wire](b, []string{"document_id", "content_version"}, nil, nil)
	if e != nil {
		return e
	}
	n := DeletedPayload(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (KnowledgeEvents) Format(w fmt.State, _ rune)   { io.WriteString(w, "knowledge_event_types") }
func (KnowledgeEvents) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_event_types"`), nil }
func (*KnowledgeEvents) UnmarshalJSON([]byte) error  { return invalid() }
func (KnowledgeEvents) LogValue() slog.Value         { return slog.StringValue("knowledge_event_types") }
