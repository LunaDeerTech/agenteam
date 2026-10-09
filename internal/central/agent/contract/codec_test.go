package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	agent "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAgentCoreEveryKeyPresenceAndExactSpelling(t *testing.T) {
	v := coreFixture(t)
	base := string(mustJSON(t, v))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(base), &fields); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		t.Run(key, func(t *testing.T) {
			missingFields := make(map[string]json.RawMessage, len(fields))
			for k, raw := range fields {
				if k != key {
					missingFields[k] = raw
				}
			}
			cases := []string{
				string(mustJSON(t, missingFields)),
				strings.Replace(base, `"`+key+`":`, `"`+strings.ToUpper(key)+`":`, 1),
				strings.TrimSuffix(base, "}") + `,"` + key + `":` + string(value) + `}`,
				strings.TrimSuffix(base, "}") + `,"` + strings.ToUpper(key) + `":` + string(value) + `}`,
			}
			if key != "display_name" && key != "tag_color" && key != "reasoning_effort" && key != "approval_model_ref" {
				cases = append(cases, strings.Replace(base, `"`+key+`":`+string(value), `"`+key+`":null`, 1))
			}
			for _, replacement := range []string{`{}`, `[]`, `42`} {
				cases = append(cases, strings.Replace(base, `"`+key+`":`+string(value), `"`+key+`":`+replacement, 1))
			}
			for _, bad := range cases {
				out := v.Clone()
				before := out.Clone()
				if out.UnmarshalJSON([]byte(bad)) == nil {
					t.Fatal("bad schema accepted")
				}
				if !reflect.DeepEqual(out, before) {
					t.Fatal("failed decode partially changed receiver")
				}
				zero, err := agent.DecodeAgentCore([]byte(bad))
				if err == nil || !reflect.DeepEqual(zero, agent.AgentCore{}) {
					t.Fatal("Decode returned partial value")
				}
			}
		})
	}
}

func TestAgentStrictRawAndNestedScalarBoundaries(t *testing.T) {
	v := coreFixture(t)
	base := string(mustJSON(t, v))
	badBodies := []string{`"\ud800"`, `"\udfff"`, `"\ud800x"`, `"\ud800\u0041"`, `"\udc00\ud800"`, `"\ud800\ud800"`, `"\uZZZZ"`, `"\u0000"`, `"\x41"`, `"unterminated`, `"` + string([]byte{0xff}) + `"`}
	for _, body := range badBodies {
		bad := replaceField(t, base, "instructions", body)
		if _, err := agent.DecodeAgentCore([]byte(bad)); err == nil {
			t.Fatal("invalid raw Unicode/escape accepted")
		}
	}
	for _, good := range []string{`"\ud83d\ude00"`, `"literal \\ud800"`, `"literal \\\" quote"`, `"\ufffd"`} {
		if _, err := agent.DecodeAgentCore([]byte(replaceField(t, base, "instructions", good))); err != nil {
			t.Fatalf("valid escape rejected: %v", err)
		}
	}
	for _, bad := range []string{
		"null", "[]", "true", "", "{", base + "{}", base + " false", base + " garbage",
		strings.TrimSuffix(base, "}") + `,"unknown":false}`,
		strings.TrimSuffix(base, "}") + `,"\u006eame":"Agent-One"}`,
		strings.TrimSuffix(base, "}") + `,"\ud800":null}`,
		replaceField(t, base, "instructions", `{"body":"text","body":"other"}`),
		replaceField(t, base, "version", `"9223372036854775808"`),
		replaceField(t, base, "version", `1`),
		replaceField(t, base, "created_at", `"2026-10-09T12:00:00.1234567Z"`),
		replaceField(t, base, "updated_at", `"2026-10-09T12:00:00.123455Z"`),
		replaceField(t, base, "id", `"01900000-0000-4000-8000-00000000000a"`),
		replaceField(t, base, "project_id", `"01900000-0000-7000-8000-00000000000B"`),
		replaceField(t, base, "model_ref", `"00000000-0000-0000-0000-000000000000"`),
		replaceField(t, base, "approval_model_ref", `"01900000-0000-7000-8000-00000000000D"`),
	} {
		out := v.Clone()
		if err := out.UnmarshalJSON([]byte(bad)); err == nil || !reflect.DeepEqual(out, v) {
			t.Fatal("bad raw accepted or receiver changed")
		}
	}
	var nilCore *agent.AgentCore
	var nilRef *agent.AgentRef
	if nilCore.UnmarshalJSON([]byte(base)) == nil || nilRef.UnmarshalJSON(mustJSON(t, refFixture(t))) == nil {
		t.Fatal("nil receiver accepted")
	}
}

func replaceField(t *testing.T, base, key, raw string) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(base), &fields); err != nil {
		t.Fatal(err)
	}
	old, ok := fields[key]
	if !ok {
		t.Fatalf("unknown fixture key %s", key)
	}
	return strings.Replace(base, `"`+key+`":`+string(old), `"`+key+`":`+raw, 1)
}

func TestAgentCodecFullRawCaps(t *testing.T) {
	core := mustJSON(t, coreFixture(t))
	ref := mustJSON(t, refFixture(t))
	tests := []struct {
		name     string
		raw      []byte
		cap      int
		decode   func([]byte) error
		standard func([]byte) error
	}{
		{"core", core, agent.MaxAgentCoreBytes, func(raw []byte) error { _, err := agent.DecodeAgentCore(raw); return err }, func(raw []byte) error { var v agent.AgentCore; return json.Unmarshal(raw, &v) }},
		{"ref", ref, agent.MaxAgentRefBytes, func(raw []byte) error { _, err := agent.DecodeAgentRef(raw); return err }, func(raw []byte) error { var v agent.AgentRef; return json.Unmarshal(raw, &v) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exact := append(bytes.Repeat([]byte(" "), tc.cap-len(tc.raw)), tc.raw...)
			if err := tc.decode(exact); err != nil {
				t.Fatal("exact cap rejected")
			}
			over := append([]byte(" "), exact...)
			if tc.decode(over) == nil {
				t.Fatal("full raw over cap accepted")
			}
			// The standard library strips outer whitespace before calling a custom
			// method. A transport must bound the body or use DecodeAgentCore/Ref.
			if err := tc.standard(over); err != nil {
				t.Fatal("unexpected standard-library boundary")
			}
			inside := append([]byte{'{'}, bytes.Repeat([]byte(" "), tc.cap-len(tc.raw)+1)...)
			inside = append(inside, tc.raw[1:]...)
			if tc.decode(inside) == nil || tc.standard(inside) == nil {
				t.Fatal("inner whitespace bypassed cap")
			}
		})
	}
}

func TestAgentEnumsStrictAndAtomic(t *testing.T) {
	for _, value := range []agent.ApprovalPolicy{agent.ApprovalDefault, agent.ApprovalAuto, agent.ApprovalAllow} {
		var out agent.ApprovalPolicy
		if err := json.Unmarshal(mustJSON(t, value), &out); err != nil || out != value {
			t.Fatal("policy round trip")
		}
	}
	for _, value := range []agent.AgentLifecycle{agent.AgentActive, agent.AgentDeleting} {
		var out agent.AgentLifecycle
		if err := json.Unmarshal(mustJSON(t, value), &out); err != nil || out != value {
			t.Fatal("lifecycle round trip")
		}
	}
	for _, raw := range []string{`null`, `true`, `1`, `[]`, `{}`, `""`, `"AUTO"`, `"idle"`, `"\ud800"`, `"auto""auto"`, strings.Repeat(" ", 257) + `"auto"`} {
		p := agent.ApprovalDefault
		l := agent.AgentActive
		if p.UnmarshalJSON([]byte(raw)) == nil || p != agent.ApprovalDefault {
			t.Fatal("policy failure not atomic")
		}
		if l.UnmarshalJSON([]byte(raw)) == nil || l != agent.AgentActive {
			t.Fatal("lifecycle failure not atomic")
		}
	}
	var p *agent.ApprovalPolicy
	var l *agent.AgentLifecycle
	if p.UnmarshalJSON([]byte(`"auto"`)) == nil || l.UnmarshalJSON([]byte(`"active"`)) == nil {
		t.Fatal("nil enum receiver accepted")
	}
	if _, err := json.Marshal(agent.ApprovalPolicy("bad")); err == nil {
		t.Fatal("invalid policy serialized")
	}
	if _, err := json.Marshal(agent.AgentLifecycle("busy")); err == nil {
		t.Fatal("invalid lifecycle serialized")
	}
}

func TestAgentDirectSafeOutputAndFaults(t *testing.T) {
	const secret = "private-config-sentinel"
	v := coreFixture(t)
	v.Instructions, v.Description = secret, secret
	for _, value := range []any{v, &v, refFixture(t), agent.ApprovalPolicy(secret), agent.AgentLifecycle(secret)} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			if strings.Contains(fmt.Sprintf(format, value), secret) {
				t.Fatal("direct fmt leaked body")
			}
		}
		for _, jsonLog := range []bool{false, true} {
			var b bytes.Buffer
			var handler slog.Handler = slog.NewTextHandler(&b, nil)
			if jsonLog {
				handler = slog.NewJSONHandler(&b, nil)
			}
			slog.New(handler).Info("safe", "agent", value)
			if strings.Contains(b.String(), secret) {
				t.Fatal("direct slog leaked body")
			}
		}
	}
	if !bytes.Contains(mustJSON(t, v), []byte(secret)) {
		t.Fatal("business JSON must preserve authorized body")
	}
	bad := strings.TrimSuffix(string(mustJSON(t, v)), "}") + `,"` + secret + `":null}`
	_, err := agent.DecodeAgentCore([]byte(bad))
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.Code != foundation.InvalidArgument || fault.CommitState != foundation.NotStarted {
		t.Fatal("wrong safe validation error")
	}
	if strings.Contains(string(mustJSON(t, fault)), secret) || strings.Contains(fmt.Sprintf("%+v", err), secret) {
		t.Fatal("Fault exposed rejected value/key")
	}
	// Enclosing JSON fallbacks can bypass direct LogValue; document, rather than
	// falsely claim, universal automatic body redaction for arbitrary containers.
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("authorized-test", "wrapper", struct{ Core agent.AgentCore }{v})
	if !strings.Contains(log.String(), secret) {
		t.Fatal("expected wrapper JSON limitation was not exercised")
	}
}
