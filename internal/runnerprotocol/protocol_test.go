package runnerprotocol_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const first p.ID = "01900000-0000-7000-8000-000000000001"
const second p.ID = "01900000-0000-7000-8000-000000000002"
const third p.ID = "01900000-0000-7000-8000-000000000003"
const at p.Instant = "2026-10-09T01:02:03.123456789Z"

func ptr[T any](v T) *T { return &v }
func header(request bool) p.Header {
	h := p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: third, Timestamp: at}
	if request {
		h.RequestID = first
		h.OperationID = second
	}
	return h
}
func request() p.Request {
	return p.Request{RequestID: first, OperationID: second, ExecutionID: third, ProjectID: first, AgentID: second, Mount: p.Mount{MountID: first, WorkspaceID: second}, OperationName: "run-command", OperationRevision: "1", Payload: json.RawMessage(`{"argv":["printf","ok"]}`)}
}
func encode(t *testing.T, v p.Payload, correlated bool) []byte {
	t.Helper()
	m, e := p.NewMessage(header(correlated), v)
	if e != nil {
		t.Fatalf("message construction failed: %v", e)
	}
	b, e := p.Encode(m)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestAllVariantsRoundTripAndDirection(t *testing.T) {
	examples := []struct {
		payload          p.Payload
		from             p.Sender
		correlated, both bool
	}{
		{p.Hello{RunnerID: first, RunnerVersion: "v1.0", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Capabilities: []string{}, FeatureFlags: []string{}}, p.Runner, false, false},
		{p.HelloAck{Accepted: true, NegotiatedProtocolVersion: p.CurrentVersion(), HeartbeatIntervalMS: 10000, HeartbeatTimeoutMS: 30000, EnabledFeatures: []string{}}, p.Central, false, false},
		{p.Heartbeat{Sequence: "1", RunnerTime: at}, p.Runner, false, false},
		{p.HeartbeatAck{Sequence: "1"}, p.Central, false, false},
		{request(), p.Central, true, false},
		{p.Response{RequestID: first, OperationID: second, Outcome: p.Success, Payload: json.RawMessage(`{"ok":true}`)}, p.Runner, true, false},
		{p.Cancel{RequestID: first, OperationID: second, Reason: "caller_cancelled"}, p.Central, true, false},
		{p.Stream{RequestID: first, OperationID: second, Stream: "stdout", Sequence: "1", Data: "hello\n🧭", Timestamp: at}, p.Runner, true, false},
		{p.RunnerStatus{Headless: true, Capabilities: []string{}}, p.Runner, false, false},
		{p.DataOpen{ChannelID: third, Direction: "runner_to_central", Purpose: "file-upload", ExpiresAt: at, OneTimeCredential: strings.Repeat("A", 43)}, p.Central, false, false},
		{p.DataReady{ChannelID: third}, p.Runner, false, false},
		{p.DataClose{ChannelID: third, Status: "completed", TransferredSize: "0"}, p.Runner, false, true},
		{p.ProtocolError{Code: p.UnsupportedMessage, SafeMessage: p.UnsupportedMessage.SafeMessage()}, p.Central, false, true},
	}
	for _, x := range examples {
		m, e := p.Decode(encode(t, x.payload, x.correlated))
		if e != nil {
			t.Fatal(e)
		}
		t.Run(string(m.Type()), func(t *testing.T) {
			if m.AllowedFrom(x.from) != nil {
				t.Fatal("valid direction rejected")
			}
			other := p.Central
			if x.from == p.Central {
				other = p.Runner
			}
			if (m.AllowedFrom(other) == nil) != x.both {
				t.Fatal("wrong direction result")
			}
			b, e := p.Encode(m.Clone())
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(b, encode(t, x.payload, x.correlated)) {
				t.Fatal("clone changed wire")
			}
		})
	}
}
func TestExactAcknowledgmentAndStrictJSON(t *testing.T) {
	golden := `{"protocol_version":{"major":1,"minor":0},"message_id":"01900000-0000-7000-8000-000000000003","timestamp":"2026-10-09T01:02:03.123456789Z","type":"heartbeat_ack","payload":{"sequence":"1"}}`
	if got := string(encode(t, p.HeartbeatAck{Sequence: "1"}, false)); got != golden {
		t.Fatalf("wire differs: %s", got)
	}
	mutations := []string{
		strings.Replace(golden, `"sequence":"1"`, `"sequence":"1","server_time":"2026-10-09T00:00:00Z"`, 1),
		strings.Replace(golden, `"sequence":"1"`, `"sequence":"1","se\u0071uence":"2"`, 1),
		strings.Replace(golden, `"sequence":"1"`, `"Sequence":"1"`, 1),
		strings.Replace(golden, `"sequence":"1"`, `"sequence":null`, 1),
		strings.Replace(golden, `"sequence":"1"`, `"sequence":"01"`, 1),
		strings.Replace(golden, `"sequence":"1"`, `"sequence":"0"`, 1),
		strings.Replace(golden, `"sequence":"1"`, `"sequence":1`, 1),
		strings.Replace(golden, `"sequence":"1"`, `"sequence":"\ud800"`, 1),
		golden + `{}`,
		strings.Replace(golden, `"minor":0`, `"minor":0,"extra":0`, 1),
		strings.Replace(golden, `"minor":0`, `"minor":1e0`, 1),
		strings.Replace(golden, `"type":"heartbeat_ack"`, `"request_id":"","type":"heartbeat_ack"`, 1),
	}
	for i, raw := range mutations {
		if _, e := p.Decode([]byte(raw)); e == nil {
			t.Fatalf("invalid JSON %d accepted", i)
		}
	}
	if _, e := p.Decode([]byte(strings.Replace(golden, `"major":1`, `"major":2`, 1))); !errors.Is(e, p.ErrIncompatibleVersion) {
		t.Fatalf("major mismatch not distinguished: %v", e)
	}
}
func TestResponseOutcomePresence(t *testing.T) {
	valid := []p.Response{
		{RequestID: first, OperationID: second, Outcome: p.Success},
		{RequestID: first, OperationID: second, Outcome: p.Failure, Code: ptr(p.Timeout), SafeMessage: ptr("Operation timed out.")},
		{RequestID: first, OperationID: second, Outcome: p.Cancelled, Code: ptr(p.CancelledCode), SafeMessage: ptr("Operation was cancelled.")},
		{RequestID: first, OperationID: second, Outcome: p.Unknown, SafeMessage: ptr("Operation outcome is unknown.")},
	}
	for _, v := range valid {
		encode(t, v, true)
	}
	bad := []p.Response{
		{RequestID: first, OperationID: second, Outcome: p.Success, Code: ptr(p.InternalError)},
		{RequestID: first, OperationID: second, Outcome: p.Failure, Code: ptr(p.CancelledCode), SafeMessage: ptr(p.CancelledCode.SafeMessage())},
		{RequestID: first, OperationID: second, Outcome: p.Failure, Code: ptr(p.InternalError), SafeMessage: ptr("backend-secret")},
		{RequestID: first, OperationID: second, Outcome: p.Cancelled, Code: ptr(p.InternalError), SafeMessage: ptr(p.InternalError.SafeMessage())},
		{RequestID: first, OperationID: second, Outcome: p.Unknown, Code: ptr(p.InternalError), SafeMessage: ptr(p.UnknownMessage)},
		{RequestID: first, OperationID: second, Outcome: p.Unknown, SafeMessage: ptr(p.UnknownMessage), Payload: json.RawMessage(`{}`)},
	}
	for i, v := range bad {
		if _, e := p.NewMessage(header(true), v); e == nil {
			t.Fatalf("invalid outcome %d accepted", i)
		}
	}
}
func TestAtomicDecodeOwnershipAndSafeFormatting(t *testing.T) {
	const canary = "runner-secret-canary"
	req := request()
	req.Environment = map[string]string{"TOKEN": canary}
	req.Deadline = ptr(at)
	m, e := p.NewMessage(header(true), req)
	if e != nil {
		t.Fatal(e)
	}
	original, e := p.Encode(m)
	if e != nil {
		t.Fatal(e)
	}
	req.Environment["TOKEN"] = "caller mutation"
	req.Payload[0] = 'x'
	*req.Deadline = "bad"
	decoded := m.Payload().(p.Request)
	decoded.Environment["TOKEN"] = "accessor mutation"
	decoded.Payload[0] = 'x'
	*decoded.Deadline = "bad"
	after, _ := p.Encode(m)
	if !bytes.Equal(original, after) {
		t.Fatal("payload aliases escaped")
	}
	if e := json.Unmarshal([]byte(`{"protocol_version":{"major":1,"minor":0}}`), &m); e == nil {
		t.Fatal("partial decode accepted")
	}
	after, _ = p.Encode(m)
	if !bytes.Equal(original, after) {
		t.Fatal("failed decode modified message")
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("probe", "message", m, "payload", m.Payload())
	j, _ := json.Marshal(m)
	q, _ := json.Marshal(m.Payload())
	outputs := []string{fmt.Sprintf("%+v %#v", m, m.Payload()), string(j), string(q), logs.String()}
	for _, out := range outputs {
		if strings.Contains(out, canary) {
			t.Fatal("secret leaked through default projection")
		}
	}
	if !bytes.Contains(original, []byte(canary)) {
		t.Fatal("explicit wire lost environment")
	}
	empty := request()
	empty.Environment = map[string]string{}
	m, e = p.NewMessage(header(true), empty)
	if e != nil {
		t.Fatal(e)
	}
	if m.Payload().(p.Request).Environment == nil {
		t.Fatal("explicit empty environment presence lost")
	}
}
func TestNestedLimitsAndCorrelation(t *testing.T) {
	req := request()
	req.Environment = map[string]string{"TOKEN": strings.Repeat("x", 8192)}
	encode(t, req, true)
	req.Environment["TOKEN"] += "x"
	if _, e := p.NewMessage(header(true), req); e == nil {
		t.Fatal("oversize environment accepted")
	}
	req = request()
	req.Payload = json.RawMessage(`{"x":` + strings.Repeat(`[`, 17) + `0` + strings.Repeat(`]`, 17) + `}`)
	if _, e := p.NewMessage(header(true), req); !errors.Is(e, p.ErrTooLarge) {
		t.Fatalf("nested cap missing: %v", e)
	}
	req = request()
	req.Payload = json.RawMessage(`{"x":"\ud800"}`)
	if _, e := p.NewMessage(header(true), req); e == nil {
		t.Fatal("lossy surrogate accepted")
	}
	raw := encode(t, request(), true)
	bad := bytes.Replace(raw, []byte(`"mount_id":"`+string(first)+`"`), []byte(`"mount_id":"`+string(first)+`","mount_id":"`+string(second)+`"`), 1)
	if _, e := p.Decode(bad); e == nil {
		t.Fatal("nested duplicate accepted")
	}
	wrong := header(true)
	wrong.OperationID = third
	if _, e := p.NewMessage(wrong, request()); !errors.Is(e, p.ErrCorrelation) {
		t.Fatal("wrong operation accepted")
	}
	if _, e := p.Decode(bytes.Repeat([]byte(" "), p.MaxMessageBytes+1)); !errors.Is(e, p.ErrTooLarge) {
		t.Fatal("raw cap missing")
	}
	var v p.Request
	v = request()
	before := v.OperationName
	if e := json.Unmarshal([]byte(`{"request_id":"`+string(first)+`"}`), &v); e == nil || v.OperationName != before {
		t.Fatal("partial payload decode mutated receiver")
	}
}
func FuzzDecodeAtomic(f *testing.F) {
	f.Add([]byte(`{"type":"hello"}`))
	f.Add([]byte(`{"x":"\ud800"}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		m, e := p.Decode(raw)
		if e != nil {
			return
		}
		b, e := p.Encode(m)
		if e != nil {
			t.Fatal("valid decode cannot encode")
		}
		again, e := p.Decode(b)
		if e != nil || again.Type() != m.Type() {
			t.Fatal("roundtrip invariant")
		}
	})
}
