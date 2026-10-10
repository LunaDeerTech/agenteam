package runnerprotocol

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"mime"
	"strings"
	"unicode/utf8"
)

type shape struct{ required, optional string }

var shapes = map[Type]shape{
	HelloType:     {"runner_id runner_version protocol_version os arch headless capabilities feature_flags", ""},
	HelloAckType:  {"accepted negotiated_protocol_version heartbeat_interval_ms heartbeat_timeout_ms enabled_features", ""},
	HeartbeatType: {"sequence runner_time", ""}, HeartbeatAckType: {"sequence", ""},
	RequestType:  {"request_id operation_id execution_id project_id agent_id mount operation_name operation_revision payload", "deadline environment idempotency_key"},
	ResponseType: {"request_id operation_id outcome", "code safe_message payload"},
	CancelType:   {"request_id operation_id reason", ""}, StreamType: {"request_id operation_id stream sequence data timestamp", ""},
	RunnerStatusType:  {"headless capabilities", ""},
	DataOpenType:      {"channel_id direction purpose expires_at one_time_credential", "request_id operation_id size media_type checksum"},
	DataReadyType:     {"channel_id", "request_id operation_id"},
	DataCloseType:     {"channel_id status transferred_size", "request_id operation_id checksum error"},
	ProtocolErrorType: {"code safe_message fatal", "offending_message_id"},
}

func decodePayload(kind Type, raw []byte) (Payload, error) {
	schema, ok := shapes[kind]
	if !ok {
		return nil, ErrInvalid
	}
	tree, e := parseJSON(raw)
	if e != nil {
		return nil, e
	}
	m, e := fields(tree, words(schema.required), words(schema.optional))
	if e != nil {
		return nil, e
	}
	for _, key := range []string{"request_id", "operation_id", "runner_id", "execution_id", "project_id", "agent_id", "channel_id", "offending_message_id"} {
		if value, found := m[key]; found {
			s, ok := value.(string)
			if !ok || !ID(s).Valid() {
				return nil, ErrInvalid
			}
		}
	}
	for _, key := range []string{"protocol_version", "negotiated_protocol_version"} {
		if value, found := m[key]; found {
			if _, e = fields(value, words("major minor"), nil); e != nil {
				return nil, e
			}
		}
	}
	if kind == RequestType {
		if _, e = fields(m["mount"], words("mount_id workspace_id"), nil); e != nil {
			return nil, e
		}
		if _, ok = m["payload"].(map[string]any); !ok {
			return nil, ErrInvalid
		}
	}
	if kind == ResponseType {
		if value, found := m["payload"]; found {
			if _, ok = value.(map[string]any); !ok {
				return nil, ErrInvalid
			}
		}
	}
	if kind == DataCloseType {
		if value, found := m["error"]; found {
			if _, e = fields(value, words("code safe_message"), nil); e != nil {
				return nil, e
			}
		}
	}
	// Decode aliases: their lack of custom JSON methods avoids recursion while the
	// parsed tree above is the only authority for field presence and object shape.
	var p Payload
	switch kind {
	case HelloType:
		var v wireHello
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = Hello(v)
	case HelloAckType:
		var v wireHelloAck
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = HelloAck(v)
	case HeartbeatType:
		var v wireHeartbeat
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = Heartbeat(v)
	case HeartbeatAckType:
		var v wireHeartbeatAck
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = HeartbeatAck(v)
	case RequestType:
		var v wireRequest
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = Request(v)
	case ResponseType:
		var v wireResponse
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = Response(v)
	case CancelType:
		var v wireCancel
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = Cancel(v)
	case StreamType:
		var v wireStream
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = Stream(v)
	case RunnerStatusType:
		var v wireRunnerStatus
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = RunnerStatus(v)
	case DataOpenType:
		var v wireDataOpen
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = DataOpen(v)
	case DataReadyType:
		var v wireDataReady
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = DataReady(v)
	case DataCloseType:
		var v wireDataClose
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = DataClose(v)
	case ProtocolErrorType:
		var v wireProtocolError
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrInvalid
		}
		p = ProtocolError(v)
	}
	if !validPayload(p) {
		return nil, ErrInvalid
	}
	return p, nil
}
func names(v []string) bool {
	if v == nil || len(v) > 32 {
		return false
	}
	seen := map[string]bool{}
	for _, s := range v {
		if !stable(s, 64) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func pair(a, b ID) bool { return a == "" && b == "" || a.Valid() && b.Valid() }
func digest(s string) bool {
	if len(s) != 71 || !strings.HasPrefix(s, "sha256:") {
		return false
	}
	for i := 7; i < len(s); i++ {
		if !lowerHex(s[i]) {
			return false
		}
	}
	return true
}
func credential(s string) bool {
	if len(s) != 43 {
		return false
	}
	b, e := base64.RawURLEncoding.DecodeString(s)
	return e == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == s
}
func environment(v map[string]string) bool {
	if len(v) > 128 {
		return false
	}
	size := 0
	for key, value := range v {
		if len(key) == 0 || len(key) > 128 || len(value) > 8192 || strings.IndexByte(value, 0) >= 0 || !utf8.ValidString(value) {
			return false
		}
		for i := range len(key) {
			c := key[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || i > 0 && c >= '0' && c <= '9') {
				return false
			}
		}
		size += len(key) + len(value)
		if size > 64<<10 {
			return false
		}
	}
	return true
}
func keyValid(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for i := range len(s) {
		b := s[i]
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.' || b == ':' || b == '/') {
			return false
		}
	}
	return true
}
func validPayload(payload Payload) bool {
	switch v := payload.(type) {
	case Hello:
		return v.RunnerID.Valid() && text(v.RunnerVersion, 64, false) && (v.OS == "linux" || v.OS == "darwin") && (v.Arch == "amd64" || v.Arch == "arm64") && names(v.Capabilities) && names(v.FeatureFlags)
	case HelloAck:
		return v.NegotiatedProtocolVersion == CurrentVersion() && v.HeartbeatIntervalMS >= 1000 && v.HeartbeatIntervalMS <= 30000 && v.HeartbeatTimeoutMS >= 3*v.HeartbeatIntervalMS && v.HeartbeatTimeoutMS <= 120000 && names(v.EnabledFeatures)
	case Heartbeat:
		return decimal(v.Sequence, math.MaxUint64, true) && v.RunnerTime.Valid()
	case HeartbeatAck:
		return decimal(v.Sequence, math.MaxUint64, true)
	case Request:
		return v.RequestID.Valid() && v.OperationID.Valid() && v.ExecutionID.Valid() && v.ProjectID.Valid() && v.AgentID.Valid() && v.Mount.MountID.Valid() && v.Mount.WorkspaceID.Valid() && stable(v.OperationName, 128) && decimal(v.OperationRevision, math.MaxInt64, true) && (v.Deadline == nil || v.Deadline.Valid()) && environment(v.Environment) && (v.IdempotencyKey == nil || keyValid(*v.IdempotencyKey))
	case Response:
		if !v.RequestID.Valid() || !v.OperationID.Valid() {
			return false
		}
		switch v.Outcome {
		case Success:
			return v.Code == nil && v.SafeMessage == nil
		case Failure:
			return v.Code != nil && *v.Code != CancelledCode && v.Code.SafeMessage() != "" && v.SafeMessage != nil && *v.SafeMessage == v.Code.SafeMessage() && v.Payload == nil
		case Cancelled:
			return v.Code != nil && (*v.Code == CancelledCode || *v.Code == Timeout) && v.SafeMessage != nil && *v.SafeMessage == v.Code.SafeMessage() && v.Payload == nil
		case Unknown:
			return v.Code == nil && v.SafeMessage != nil && *v.SafeMessage == UnknownMessage && v.Payload == nil
		}
		return false
	case Cancel:
		return v.RequestID.Valid() && v.OperationID.Valid() && (v.Reason == "caller_cancelled" || v.Reason == "deadline_exceeded" || v.Reason == "connection_closed" || v.Reason == "runner_stopping")
	case Stream:
		return v.RequestID.Valid() && v.OperationID.Valid() && (v.Stream == "stdout" || v.Stream == "stderr" || v.Stream == "progress") && decimal(v.Sequence, math.MaxUint64, true) && len(v.Data) <= MaxStreamBytes && v.Timestamp.Valid()
	case RunnerStatus:
		return names(v.Capabilities)
	case DataOpen:
		if !v.ChannelID.Valid() || !pair(v.RequestID, v.OperationID) || !stable(v.Purpose, 64) || !v.ExpiresAt.Valid() || !credential(v.OneTimeCredential) {
			return false
		}
		if v.Direction != "runner_to_central" && v.Direction != "central_to_runner" && v.Direction != "bidirectional_stream" {
			return false
		}
		if v.Size != nil && !decimal(*v.Size, math.MaxInt64, false) || v.Checksum != nil && !digest(*v.Checksum) {
			return false
		}
		if v.MediaType != nil {
			if len(*v.MediaType) > 255 || !text(*v.MediaType, 255, false) {
				return false
			}
			for _, b := range []byte(*v.MediaType) {
				if b > 127 {
					return false
				}
			}
			kind, params, e := mime.ParseMediaType(*v.MediaType)
			if e != nil || len(params) != 0 || kind != *v.MediaType || strings.Count(kind, "/") != 1 {
				return false
			}
		}
		return true
	case DataReady:
		return v.ChannelID.Valid() && pair(v.RequestID, v.OperationID)
	case DataClose:
		if !v.ChannelID.Valid() || !pair(v.RequestID, v.OperationID) || !decimal(v.TransferredSize, math.MaxInt64, false) {
			return false
		}
		if v.Status == "completed" {
			return v.Error == nil && (v.Checksum == nil || digest(*v.Checksum))
		}
		if v.Checksum != nil || v.Error == nil || v.Error.Code.SafeMessage() == "" || v.Error.SafeMessage != v.Error.Code.SafeMessage() {
			return false
		}
		return v.Status == "failed" && (v.Error.Code == TransferFailed || v.Error.Code == DataChannelFailed) || v.Status == "cancelled" && v.Error.Code == CancelledCode || v.Status == "expired" && v.Error.Code == Timeout
	case ProtocolError:
		return v.Code.SafeMessage() != "" && v.SafeMessage == v.Code.SafeMessage() && (v.OffendingMessageID == "" || v.OffendingMessageID.Valid())
	}
	return false
}
