package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sort"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type Schema struct {
	Producer, EventType, AggregateType StableName
	Version                            uint32
}
type schemaKey struct {
	name    StableName
	version uint32
}
type definition struct {
	schema   Schema
	validate func([]byte) ([]byte, error)
}
type catalogState struct {
	mu      sync.RWMutex
	sealed  bool
	entries map[schemaKey]*definition
}
type Catalog struct{ data func() *catalogState }

func NewCatalog() *Catalog {
	s := &catalogState{entries: make(map[schemaKey]*definition)}
	return &Catalog{func() *catalogState { return s }}
}
func (c *Catalog) Valid() bool { return c != nil && c.data != nil }
func (c *Catalog) Seal() error {
	if !c.Valid() {
		return invalid()
	}
	s := c.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sealed = true
	return nil
}
func (c *Catalog) Schemas() []Schema {
	if !c.Valid() {
		return nil
	}
	s := c.data()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Schema, 0, len(s.entries))
	for _, d := range s.entries {
		out = append(out, d.schema)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].EventType != out[j].EventType {
			return out[i].EventType < out[j].EventType
		}
		return out[i].Version < out[j].Version
	})
	return out
}
func (c *Catalog) Owns(e Event) bool {
	return c.Valid() && e.data != nil && e.data().issuer == c.data()
}
func (c Catalog) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "event_catalog") }
func (c Catalog) MarshalJSON() ([]byte, error) { return []byte(`"event_catalog"`), nil }
func (*Catalog) UnmarshalJSON([]byte) error    { return invalid() }
func (c Catalog) LogValue() slog.Value         { return slog.StringValue("event_catalog") }

type Definition[T any] struct {
	Schema   Schema
	Codec    Codec[T]
	Validate func(T) error
}
type eventTypeData[T any] struct {
	catalog    *catalogState
	definition *definition
	codec      Codec[T]
}
type EventType[T any] struct{ data func() eventTypeData[T] }

func DefineEvent[T any](c *Catalog, d Definition[T]) (EventType[T], error) {
	if !c.Valid() || d.Schema.Producer.Validate() != nil || d.Schema.EventType.Validate() != nil || d.Schema.AggregateType.Validate() != nil || d.Schema.Version == 0 || d.Validate == nil || d.Codec == nil || !typedPayload(reflect.TypeFor[T](), map[reflect.Type]bool{}) {
		return EventType[T]{}, invalid()
	}
	v := reflect.ValueOf(d.Codec)
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Func || v.Kind() == reflect.Map || v.Kind() == reflect.Slice || v.Kind() == reflect.Interface) && v.IsNil() {
		return EventType[T]{}, invalid()
	}
	def := &definition{schema: d.Schema}
	def.validate = func(raw []byte) (canonical []byte, err error) {
		defer func() {
			if recover() != nil {
				canonical = nil
				err = invalid()
			}
		}()
		canonical, err = canonicalJSON(raw)
		if err != nil {
			return nil, err
		}
		decoded, err := d.Codec.Decode(canonical)
		if err != nil || d.Validate(decoded) != nil {
			return nil, invalid()
		}
		reencoded, err := d.Codec.Encode(decoded)
		if err != nil {
			return nil, invalid()
		}
		next, err := canonicalJSON(reencoded)
		if err != nil || !bytes.Equal(next, canonical) {
			return nil, invalid()
		}
		return canonical, nil
	}
	s := c.data()
	s.mu.Lock()
	defer s.mu.Unlock()
	k := schemaKey{d.Schema.EventType, d.Schema.Version}
	if s.sealed || s.entries[k] != nil {
		return EventType[T]{}, invalid()
	}
	for key, known := range s.entries {
		if key.name == k.name && (known.schema.Producer != d.Schema.Producer || known.schema.AggregateType != d.Schema.AggregateType) {
			return EventType[T]{}, invalid()
		}
	}
	s.entries[k] = def
	return EventType[T]{func() eventTypeData[T] { return eventTypeData[T]{s, def, d.Codec} }}, nil
}
func (t EventType[T]) Schema() Schema {
	if t.data == nil {
		return Schema{}
	}
	return t.data().definition.schema
}
func (t EventType[T]) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "event_type") }
func (t EventType[T]) MarshalJSON() ([]byte, error) { return []byte(`"event_type"`), nil }
func (*EventType[T]) UnmarshalJSON([]byte) error    { return invalid() }
func (t EventType[T]) LogValue() slog.Value         { return slog.StringValue("event_type") }

func NewEvent[T any](t EventType[T], h Header, value T) (event Event, err error) {
	defer func() {
		if recover() != nil {
			event = Event{}
			err = invalid()
		}
	}()
	if t.data == nil {
		return Event{}, invalid()
	}
	d := t.data()
	raw, err := d.codec.Encode(value)
	if err != nil {
		return Event{}, invalid()
	}
	return makeEvent(d.catalog, d.definition, h, raw)
}
func makeEvent(c *catalogState, d *definition, h Header, raw []byte) (Event, error) {
	if h.Validate() != nil || h.EventType != d.schema.EventType || h.SchemaVersion != d.schema.Version || h.AggregateType != d.schema.AggregateType {
		return Event{}, invalid()
	}
	if len(raw) > MaxPayloadBytes {
		return Event{}, foundation.NewFault(foundation.PayloadTooLarge, foundation.NotStarted)
	}
	payload, err := d.validate(raw)
	if err != nil {
		return Event{}, err
	}
	sum := sha256.Sum256(payload)
	value := eventData{issuer: c, header: cloneHeader(h), producer: d.schema.Producer, payload: payload, digest: foundation.Digest("sha256:" + hex.EncodeToString(sum[:]))}
	return Event{func() eventData { return value }}, nil
}
func (c *Catalog) Restore(producer StableName, h Header, payload []byte) (Event, error) {
	if !c.Valid() {
		return Event{}, invalid()
	}
	s := c.data()
	s.mu.RLock()
	d := s.entries[schemaKey{h.EventType, h.SchemaVersion}]
	s.mu.RUnlock()
	if d == nil {
		return Event{}, unsupported()
	}
	if d.schema.Producer != producer {
		return Event{}, invalid()
	}
	return makeEvent(s, d, h, payload)
}
func DecodeEvent[T any](t EventType[T], e Event) (value T, err error) {
	defer func() {
		if recover() != nil {
			var zero T
			value = zero
			err = invalid()
		}
	}()
	if t.data == nil || e.data == nil {
		return value, invalid()
	}
	d, v := t.data(), e.data()
	if d.catalog != v.issuer || v.header.EventType != d.definition.schema.EventType || v.header.SchemaVersion != d.definition.schema.Version {
		return value, unsupported()
	}
	value, err = d.codec.Decode(append([]byte(nil), v.payload...))
	if err != nil {
		var zero T
		return zero, invalid()
	}
	return value, nil
}
