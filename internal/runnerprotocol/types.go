package runnerprotocol

import "encoding/json"

type Type string

const (
	HelloType         Type = "hello"
	HelloAckType      Type = "hello_ack"
	HeartbeatType     Type = "heartbeat"
	HeartbeatAckType  Type = "heartbeat_ack"
	RequestType       Type = "request"
	ResponseType      Type = "response"
	CancelType        Type = "cancel"
	StreamType        Type = "stream"
	RunnerStatusType  Type = "runner_status"
	DataOpenType      Type = "data_channel_open"
	DataReadyType     Type = "data_channel_ready"
	DataCloseType     Type = "data_channel_close"
	ProtocolErrorType Type = "protocol_error"
)

type Sender uint8

const (
	Central Sender = iota + 1
	Runner
)

// Payload is a closed variant set. JSON/log formatting is deliberately redacted;
// Encode is the only supported operation that serializes a complete wire message.
type Payload interface {
	messageType() Type
	wire() any
}
type Header struct {
	ProtocolVersion Version `json:"protocol_version"`
	MessageID       ID      `json:"message_id"`
	RequestID       ID      `json:"request_id,omitempty"`
	OperationID     ID      `json:"operation_id,omitempty"`
	Timestamp       Instant `json:"timestamp"`
}

type Hello struct {
	RunnerID        ID       `json:"runner_id"`
	RunnerVersion   string   `json:"runner_version"`
	ProtocolVersion Version  `json:"protocol_version"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
	Headless        bool     `json:"headless"`
	Capabilities    []string `json:"capabilities"`
	FeatureFlags    []string `json:"feature_flags"`
}
type HelloAck struct {
	Accepted                  bool     `json:"accepted"`
	NegotiatedProtocolVersion Version  `json:"negotiated_protocol_version"`
	HeartbeatIntervalMS       uint32   `json:"heartbeat_interval_ms"`
	HeartbeatTimeoutMS        uint32   `json:"heartbeat_timeout_ms"`
	EnabledFeatures           []string `json:"enabled_features"`
}
type Heartbeat struct {
	Sequence   Decimal `json:"sequence"`
	RunnerTime Instant `json:"runner_time"`
}
type HeartbeatAck struct {
	Sequence Decimal `json:"sequence"`
}
type Mount struct {
	MountID     ID `json:"mount_id"`
	WorkspaceID ID `json:"workspace_id"`
}
type Request struct {
	RequestID         ID                `json:"request_id"`
	OperationID       ID                `json:"operation_id"`
	ExecutionID       ID                `json:"execution_id"`
	ProjectID         ID                `json:"project_id"`
	AgentID           ID                `json:"agent_id"`
	Mount             Mount             `json:"mount"`
	OperationName     string            `json:"operation_name"`
	OperationRevision Decimal           `json:"operation_revision"`
	Deadline          *Instant          `json:"deadline,omitempty"`
	Environment       map[string]string `json:"environment,omitempty"`
	IdempotencyKey    *string           `json:"idempotency_key,omitempty"`
	Payload           json.RawMessage   `json:"payload"`
}
type Outcome string

const (
	Success   Outcome = "success"
	Failure   Outcome = "failure"
	Cancelled Outcome = "cancelled"
	Unknown   Outcome = "unknown"
)

type Code string

const (
	InvalidRequest        Code = "INVALID_REQUEST"
	UnsupportedOperation  Code = "UNSUPPORTED_OPERATION"
	NotFound              Code = "NOT_FOUND"
	Conflict              Code = "CONFLICT"
	WorkspaceNotFound     Code = "WORKSPACE_NOT_FOUND"
	PathOutsideWorkspace  Code = "PATH_OUTSIDE_WORKSPACE"
	ProcessNotFound       Code = "PROCESS_NOT_FOUND"
	ProcessAlreadyExited  Code = "PROCESS_ALREADY_EXITED"
	Timeout               Code = "TIMEOUT"
	CancelledCode         Code = "CANCELLED"
	TransferFailed        Code = "TRANSFER_FAILED"
	DataChannelFailed     Code = "DATA_CHANNEL_FAILED"
	CapabilityUnavailable Code = "CAPABILITY_UNAVAILABLE"
	InternalError         Code = "INTERNAL_ERROR"
)

func (c Code) SafeMessage() string {
	switch c {
	case InvalidRequest:
		return "Invalid request."
	case UnsupportedOperation:
		return "Operation is unsupported."
	case NotFound:
		return "Resource was not found."
	case Conflict:
		return "Resource conflict."
	case WorkspaceNotFound:
		return "Workspace was not found."
	case PathOutsideWorkspace:
		return "Path is outside the workspace."
	case ProcessNotFound:
		return "Process was not found."
	case ProcessAlreadyExited:
		return "Process has already exited."
	case Timeout:
		return "Operation timed out."
	case CancelledCode:
		return "Operation was cancelled."
	case TransferFailed:
		return "Transfer failed."
	case DataChannelFailed:
		return "Data channel failed."
	case CapabilityUnavailable:
		return "Capability is unavailable."
	case InternalError:
		return "Internal error."
	}
	return ""
}

const UnknownMessage = "Operation outcome is unknown."

type Response struct {
	RequestID   ID              `json:"request_id"`
	OperationID ID              `json:"operation_id"`
	Outcome     Outcome         `json:"outcome"`
	Code        *Code           `json:"code,omitempty"`
	SafeMessage *string         `json:"safe_message,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}
type Cancel struct {
	RequestID   ID     `json:"request_id"`
	OperationID ID     `json:"operation_id"`
	Reason      string `json:"reason"`
}
type Stream struct {
	RequestID   ID      `json:"request_id"`
	OperationID ID      `json:"operation_id"`
	Stream      string  `json:"stream"`
	Sequence    Decimal `json:"sequence"`
	Data        string  `json:"data"`
	Timestamp   Instant `json:"timestamp"`
}
type RunnerStatus struct {
	Headless     bool     `json:"headless"`
	Capabilities []string `json:"capabilities"`
}
type DataOpen struct {
	ChannelID         ID       `json:"channel_id"`
	RequestID         ID       `json:"request_id,omitempty"`
	OperationID       ID       `json:"operation_id,omitempty"`
	Direction         string   `json:"direction"`
	Purpose           string   `json:"purpose"`
	Size              *Decimal `json:"size,omitempty"`
	MediaType         *string  `json:"media_type,omitempty"`
	Checksum          *string  `json:"checksum,omitempty"`
	ExpiresAt         Instant  `json:"expires_at"`
	OneTimeCredential string   `json:"one_time_credential"`
}
type DataReady struct {
	ChannelID   ID `json:"channel_id"`
	RequestID   ID `json:"request_id,omitempty"`
	OperationID ID `json:"operation_id,omitempty"`
}
type DataError struct {
	Code        Code   `json:"code"`
	SafeMessage string `json:"safe_message"`
}
type DataClose struct {
	ChannelID       ID         `json:"channel_id"`
	RequestID       ID         `json:"request_id,omitempty"`
	OperationID     ID         `json:"operation_id,omitempty"`
	Status          string     `json:"status"`
	TransferredSize Decimal    `json:"transferred_size"`
	Checksum        *string    `json:"checksum,omitempty"`
	Error           *DataError `json:"error,omitempty"`
}
type ProtocolCode string

const (
	InvalidEnvelope     ProtocolCode = "INVALID_ENVELOPE"
	IncompatibleVersion ProtocolCode = "INCOMPATIBLE_VERSION"
	HelloOrder          ProtocolCode = "HELLO_ORDER"
	CorrelationInvalid  ProtocolCode = "CORRELATION_INVALID"
	DuplicateMessage    ProtocolCode = "DUPLICATE_MESSAGE"
	UnsupportedMessage  ProtocolCode = "UNSUPPORTED_MESSAGE"
	ResourceExhausted   ProtocolCode = "RESOURCE_EXHAUSTED"
)

func (c ProtocolCode) SafeMessage() string {
	switch c {
	case InvalidEnvelope:
		return "Invalid protocol envelope."
	case IncompatibleVersion:
		return "Protocol version is incompatible."
	case HelloOrder:
		return "Invalid hello order."
	case CorrelationInvalid:
		return "Invalid message correlation."
	case DuplicateMessage:
		return "Duplicate message."
	case UnsupportedMessage:
		return "Message is unsupported."
	case ResourceExhausted:
		return "Protocol resources exhausted."
	}
	return ""
}

type ProtocolError struct {
	Code               ProtocolCode `json:"code"`
	SafeMessage        string       `json:"safe_message"`
	OffendingMessageID ID           `json:"offending_message_id,omitempty"`
	Fatal              bool         `json:"fatal"`
}
