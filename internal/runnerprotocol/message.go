package runnerprotocol

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"
)

// Message owns an immutable, validated payload. Accessors return independent copies.
type Message struct {
	header  Header
	payload Payload
}

func (m Message) Header() Header { return m.header }
func (m Message) Type() Type {
	if m.payload == nil {
		return ""
	}
	return m.payload.messageType()
}
func (m Message) Payload() Payload {
	if m.payload == nil {
		return nil
	}
	b, e := json.Marshal(m.payload.wire())
	if e != nil {
		return nil
	}
	p, e := decodePayload(m.Type(), b)
	if e != nil {
		return nil
	}
	return p
}
func (m Message) Clone() Message {
	b, e := Encode(m)
	if e != nil {
		return Message{}
	}
	v, e := Decode(b)
	if e != nil {
		return Message{}
	}
	return v
}
func (m Message) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_message") }
func (m Message) MarshalJSON() ([]byte, error) { return []byte(`"runner_message"`), nil }
func (m Message) LogValue() slog.Value         { return slog.StringValue("runner_message") }
func (m *Message) UnmarshalJSON(raw []byte) error {
	v, e := Decode(raw)
	if e == nil {
		*m = v
	}
	return e
}
func NewMessage(h Header, p Payload) (Message, error) {
	if p == nil || reflect.ValueOf(p).Kind() == reflect.Pointer && reflect.ValueOf(p).IsNil() {
		return Message{}, ErrInvalid
	}
	if reflect.ValueOf(p).Kind() == reflect.Pointer {
		p = reflect.ValueOf(p).Elem().Interface().(Payload)
	}
	switch v := p.(type) {
	case Request:
		if len(v.Payload) > MaxMessageBytes {
			return Message{}, ErrTooLarge
		}
	case Response:
		if len(v.Payload) > MaxMessageBytes {
			return Message{}, ErrTooLarge
		}
	}
	if !validPayload(p) {
		return Message{}, ErrInvalid
	}
	b, e := marshalWire(h, p)
	if e != nil {
		return Message{}, ErrInvalid
	}
	return Decode(b)
}
func marshalWire(h Header, p Payload) ([]byte, error) {
	return json.Marshal(struct {
		Header
		Type    Type `json:"type"`
		Payload any  `json:"payload"`
	}{h, p.messageType(), p.wire()})
}
func Encode(m Message) ([]byte, error) {
	if m.payload == nil {
		return nil, ErrInvalid
	}
	b, e := marshalWire(m.header, m.payload)
	if e != nil {
		return nil, ErrInvalid
	}
	if _, e = Decode(b); e != nil {
		return nil, e
	}
	return b, nil
}
func Decode(raw []byte) (Message, error) {
	root, e := parseJSON(raw)
	if e != nil {
		return Message{}, e
	}
	m, e := fields(root, words("protocol_version type message_id timestamp payload"), words("request_id operation_id"))
	if e != nil {
		return Message{}, e
	}
	if _, e = fields(m["protocol_version"], words("major minor"), nil); e != nil {
		return Message{}, e
	}
	for _, k := range []string{"message_id", "request_id", "operation_id"} {
		if v, ok := m[k]; ok {
			s, ok := v.(string)
			if !ok || !ID(s).Valid() {
				return Message{}, ErrInvalid
			}
		}
	}
	b, e := json.Marshal(m)
	if e != nil {
		return Message{}, ErrInvalid
	}
	var wire struct {
		Header
		Type    Type            `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(b, &wire) != nil || !wire.Header.Timestamp.Valid() {
		return Message{}, ErrInvalid
	}
	if !wire.Header.ProtocolVersion.Compatible() {
		return Message{}, ErrIncompatibleVersion
	}
	p, e := decodePayload(wire.Type, wire.Payload)
	if e != nil {
		return Message{}, e
	}
	if e = correlation(wire.Header, p); e != nil {
		return Message{}, e
	}
	return Message{wire.Header, p}, nil
}
func (m Message) AllowedFrom(sender Sender) error {
	if m.payload == nil || sender != Central && sender != Runner {
		return ErrDirection
	}
	t := m.Type()
	if t == DataCloseType || t == ProtocolErrorType {
		return nil
	}
	central := t == HelloAckType || t == HeartbeatAckType || t == RequestType || t == CancelType || t == DataOpenType
	if central != (sender == Central) {
		return ErrDirection
	}
	return nil
}
func correlation(h Header, p Payload) error {
	var request, operation ID
	correlated := false
	optional := false
	switch v := p.(type) {
	case Request:
		request, operation, correlated = v.RequestID, v.OperationID, true
	case Response:
		request, operation, correlated = v.RequestID, v.OperationID, true
	case Cancel:
		request, operation, correlated = v.RequestID, v.OperationID, true
	case Stream:
		request, operation, correlated = v.RequestID, v.OperationID, true
	case DataOpen:
		request, operation, optional = v.RequestID, v.OperationID, true
	case DataReady:
		request, operation, optional = v.RequestID, v.OperationID, true
	case DataClose:
		request, operation, optional = v.RequestID, v.OperationID, true
	case Hello:
		if v.ProtocolVersion != h.ProtocolVersion {
			return ErrCorrelation
		}
	}
	if correlated && (!h.RequestID.Valid() || !h.OperationID.Valid()) {
		return ErrCorrelation
	}
	if optional {
		if (h.RequestID == "") != (h.OperationID == "") {
			return ErrCorrelation
		}
	}
	if h.RequestID != request || h.OperationID != operation {
		return ErrCorrelation
	}
	return nil
}
