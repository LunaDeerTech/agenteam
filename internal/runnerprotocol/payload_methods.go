package runnerprotocol

import (
	"fmt"
	"io"
	"log/slog"
)

type wireHello Hello

func (v Hello) messageType() Type            { return HelloType }
func (v Hello) wire() any                    { return wireHello(v) }
func (v Hello) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v Hello) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v Hello) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *Hello) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(HelloType, raw)
	if e == nil {
		*v = p.(Hello)
	}
	return e
}

type wireHelloAck HelloAck

func (v HelloAck) messageType() Type            { return HelloAckType }
func (v HelloAck) wire() any                    { return wireHelloAck(v) }
func (v HelloAck) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v HelloAck) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v HelloAck) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *HelloAck) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(HelloAckType, raw)
	if e == nil {
		*v = p.(HelloAck)
	}
	return e
}

type wireHeartbeat Heartbeat

func (v Heartbeat) messageType() Type            { return HeartbeatType }
func (v Heartbeat) wire() any                    { return wireHeartbeat(v) }
func (v Heartbeat) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v Heartbeat) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v Heartbeat) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *Heartbeat) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(HeartbeatType, raw)
	if e == nil {
		*v = p.(Heartbeat)
	}
	return e
}

type wireHeartbeatAck HeartbeatAck

func (v HeartbeatAck) messageType() Type            { return HeartbeatAckType }
func (v HeartbeatAck) wire() any                    { return wireHeartbeatAck(v) }
func (v HeartbeatAck) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v HeartbeatAck) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v HeartbeatAck) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *HeartbeatAck) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(HeartbeatAckType, raw)
	if e == nil {
		*v = p.(HeartbeatAck)
	}
	return e
}

type wireRequest Request

func (v Request) messageType() Type { return RequestType }
func (v Request) wire() any {
	var environment *map[string]string
	if v.Environment != nil {
		environment = &v.Environment
	}
	return struct {
		wireRequest
		Environment *map[string]string `json:"environment,omitempty"`
	}{wireRequest(v), environment}
}
func (v Request) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v Request) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v Request) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *Request) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(RequestType, raw)
	if e == nil {
		*v = p.(Request)
	}
	return e
}

type wireResponse Response

func (v Response) messageType() Type            { return ResponseType }
func (v Response) wire() any                    { return wireResponse(v) }
func (v Response) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v Response) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v Response) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *Response) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(ResponseType, raw)
	if e == nil {
		*v = p.(Response)
	}
	return e
}

type wireCancel Cancel

func (v Cancel) messageType() Type            { return CancelType }
func (v Cancel) wire() any                    { return wireCancel(v) }
func (v Cancel) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v Cancel) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v Cancel) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *Cancel) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(CancelType, raw)
	if e == nil {
		*v = p.(Cancel)
	}
	return e
}

type wireStream Stream

func (v Stream) messageType() Type            { return StreamType }
func (v Stream) wire() any                    { return wireStream(v) }
func (v Stream) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v Stream) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v Stream) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *Stream) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(StreamType, raw)
	if e == nil {
		*v = p.(Stream)
	}
	return e
}

type wireRunnerStatus RunnerStatus

func (v RunnerStatus) messageType() Type            { return RunnerStatusType }
func (v RunnerStatus) wire() any                    { return wireRunnerStatus(v) }
func (v RunnerStatus) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v RunnerStatus) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v RunnerStatus) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *RunnerStatus) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(RunnerStatusType, raw)
	if e == nil {
		*v = p.(RunnerStatus)
	}
	return e
}

type wireDataOpen DataOpen

func (v DataOpen) messageType() Type            { return DataOpenType }
func (v DataOpen) wire() any                    { return wireDataOpen(v) }
func (v DataOpen) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v DataOpen) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v DataOpen) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *DataOpen) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(DataOpenType, raw)
	if e == nil {
		*v = p.(DataOpen)
	}
	return e
}

type wireDataReady DataReady

func (v DataReady) messageType() Type            { return DataReadyType }
func (v DataReady) wire() any                    { return wireDataReady(v) }
func (v DataReady) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v DataReady) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v DataReady) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *DataReady) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(DataReadyType, raw)
	if e == nil {
		*v = p.(DataReady)
	}
	return e
}

type wireDataClose DataClose

func (v DataClose) messageType() Type            { return DataCloseType }
func (v DataClose) wire() any                    { return wireDataClose(v) }
func (v DataClose) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v DataClose) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v DataClose) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *DataClose) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(DataCloseType, raw)
	if e == nil {
		*v = p.(DataClose)
	}
	return e
}

type wireProtocolError ProtocolError

func (v ProtocolError) messageType() Type            { return ProtocolErrorType }
func (v ProtocolError) wire() any                    { return wireProtocolError(v) }
func (v ProtocolError) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "runner_payload") }
func (v ProtocolError) MarshalJSON() ([]byte, error) { return []byte(`"runner_payload"`), nil }
func (v ProtocolError) LogValue() slog.Value         { return slog.StringValue("runner_payload") }
func (v *ProtocolError) UnmarshalJSON(raw []byte) error {
	p, e := decodePayload(ProtocolErrorType, raw)
	if e == nil {
		*v = p.(ProtocolError)
	}
	return e
}
