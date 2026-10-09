package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"testing"

	agent "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	model "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// Hand-authored wire input is independent of the implementation's MarshalJSON
// and the author's fixtures. These pure observations establish no Agent facts.
var independentAgentFields = [][2]string{
	{"updated_at", `"2024-02-29T23:59:59.999999Z"`},
	{"created_at", `"2024-02-29T23:59:59.999999Z"`},
	{"version", `"9223372036854775807"`},
	{"lifecycle", `"deleting"`},
	{"approval_model_ref", `null`},
	{"approval_policy", `"allow"`},
	{"reasoning_effort", `null`},
	{"model_ref", `"018e9ab1-2345-7a67-89ab-012345678903"`},
	{"inject_agents_md", `false`},
	{"instructions", `""`},
	{"description", `""`},
	{"tag_color", `null`},
	{"display_name", `null`},
	{"normalized_name", `"a-7"`},
	{"name", `"A-7"`},
	{"project_id", `"018e9ab1-2345-7a67-89ab-012345678902"`},
	{"id", `"018e9ab1-2345-7a67-89ab-012345678901"`},
}

func independentAgentWire(fields [][2]string) []byte {
	var b strings.Builder
	b.WriteByte('{')
	for n, p := range fields {
		if n > 0 {
			b.WriteString(",\n")
		}
		key, _ := json.Marshal(p[0])
		b.Write(key)
		b.WriteString(" : ")
		b.WriteString(p[1])
	}
	b.WriteByte('}')
	return []byte(b.String())
}
func independentAgentBase(t *testing.T) agent.AgentCore {
	t.Helper()
	v, err := agent.DecodeAgentCore(independentAgentWire(independentAgentFields))
	if err != nil {
		t.Fatal("handwritten valid core rejected")
	}
	return v
}
func independentAgentBad(t *testing.T, raw []byte) {
	t.Helper()
	old := independentAgentBase(t)
	before := old.Clone()
	err := old.UnmarshalJSON(raw)
	if err == nil || !reflect.DeepEqual(old, before) {
		t.Fatal("negative accepted or failure mutated receiver")
	}
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.InvalidArgument || fault.CommitState != f.NotStarted {
		t.Fatal("validation escaped safe Fault boundary")
	}
	zero, err := agent.DecodeAgentCore(raw)
	if err == nil || !reflect.DeepEqual(zero, agent.AgentCore{}) {
		t.Fatal("negative decode returned partial configuration")
	}
}

func TestIndependentAgentC1WireLanguage(t *testing.T) {
	base := independentAgentWire(independentAgentFields)
	v := independentAgentBase(t)
	if v.Name != "A-7" || v.NormalizedName != "a-7" || v.InjectAgentsMD || v.Version.String() != "9223372036854775807" || v.CreatedAt.String() != "2024-02-29T23:59:59.999999Z" {
		t.Fatal("handwritten values changed")
	}
	if v.DisplayName != nil || v.TagColor != nil || v.ReasoningEffort != nil || v.ApprovalModelRef != nil {
		t.Fatal("null optional metadata not preserved")
	}
	rows := 0
	for n, field := range independentAgentFields {
		// Eight-byte escaped member spelling is still the same JSON key.
		escaped := fmt.Sprintf(`"\u%04x%s"`, field[0][0], field[0][1:])
		duplicate := append(append([]byte{}, base[:len(base)-1]...), []byte(","+escaped+":"+field[1]+"}")...)
		independentAgentBad(t, duplicate)
		rows++
		legal := bytes.Replace(base, []byte(`"`+field[0]+`"`), []byte(escaped), 1)
		if _, err := agent.DecodeAgentCore(legal); err != nil {
			t.Fatal("legal escaped exact member rejected")
		}
		for _, replacement := range []string{`{"value":null}`, `[null]`, `-0`, `1e0`, `false`, `null`} {
			if field[0] == "inject_agents_md" && replacement == "false" {
				continue
			}
			if replacement == "null" && (field[0] == "display_name" || field[0] == "tag_color" || field[0] == "reasoning_effort" || field[0] == "approval_model_ref") {
				continue
			}
			changed := append([][2]string(nil), independentAgentFields...)
			changed[n][1] = replacement
			independentAgentBad(t, independentAgentWire(changed))
			rows++
		}
	}
	for _, key := range []string{"allowed_tool_ids", "allowed_mount_ids", "allowed_secret_variable_ids", "skills", "config_version", "system_prompt", "ID", "Name", "NAME"} {
		fields := append(append([][2]string{}, independentAgentFields...), [2]string{key, `[]`})
		independentAgentBad(t, independentAgentWire(fields))
		rows++
	}
	for _, suffix := range []string{"null", "[]", "0", `""`, "{}", "\x00", "/*comment*/"} {
		independentAgentBad(t, append(append([]byte{}, base...), suffix...))
		rows++
	}
	// Escape parity, valid pairs at both surrogate limits, and malformed UTF-8.
	for _, literal := range []string{`"\ud800\udc00"`, `"\udbff\udfff"`, `"\\ud800"`, `"\\\\\ud83d\ude80"`, `"\"\\\""`, `"\ufffd"`} {
		fields := append([][2]string(nil), independentAgentFields...)
		fields[9][1] = literal
		if _, err := agent.DecodeAgentCore(independentAgentWire(fields)); err != nil {
			t.Fatal("legal Unicode/escape parity rejected")
		}
	}
	for _, literal := range []string{`"\ud800"`, `"\udfff"`, `"\ud800\\udc00"`, `"\\\udc00"`, `"\ud800\u0042"`, `"\ud800\udc00\udfff"`, `"\udbff\ud800"`, `"\ud800\udc00\ud800"`, "\"\xc0\xaf\"", "\"\xed\xa0\x80\"", "\"\xf4\x90\x80\x80\""} {
		fields := append([][2]string(nil), independentAgentFields...)
		fields[9][1] = literal
		independentAgentBad(t, independentAgentWire(fields))
		rows++
	}
	for _, literal := range []string{`"+1"`, `"0001"`, `"-1"`, `"1.0"`, `" 1"`, `"18446744073709551615"`} {
		fields := append([][2]string(nil), independentAgentFields...)
		fields[2][1] = literal
		independentAgentBad(t, independentAgentWire(fields))
		rows++
	}
	t.Logf("handwritten schema/Unicode/precision negative rows=%d", rows)
}

func TestIndependentAgentC1ScalarLanguages(t *testing.T) {
	base := independentAgentBase(t)
	nameGrammar := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{1,30}[A-Za-z0-9]$`)
	effortGrammar := regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,32}$`)
	colorGrammar := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	rows := 0
	for n := 0; n <= 255; n++ {
		middle := string([]byte{byte(n)})
		for kind := 0; kind < 3; kind++ {
			v, want := base.Clone(), false
			switch kind {
			case 0:
				v.Name = "A" + middle + "9"
				v.NormalizedName = strings.ToLower(v.Name)
				want = nameGrammar.MatchString(v.Name)
			case 1:
				value := "M" + middle + "7"
				v.ReasoningEffort = &value
				want = effortGrammar.MatchString(value)
			case 2:
				value := "#aa" + middle + "123"
				v.TagColor = &value
				want = colorGrammar.MatchString(value)
			}
			_, err := v.MarshalJSON()
			if (v.Validate() == nil) != want || (err == nil) != want {
				t.Fatalf("scalar grammar mismatch kind=%d byte=%d", kind, n)
			}
			rows++
		}
	}
	for n := 0; n <= 159; n++ {
		text := "A" + string(rune(n)) + "Z"
		control := n < 32 || n >= 127
		for kind := 0; kind < 3; kind++ {
			v, want := base.Clone(), !control
			switch kind {
			case 0:
				v.DisplayName = &text
			case 1:
				v.Description = text
				want = want || n == 9 || n == 10 || n == 13
			case 2:
				v.Instructions = text
				want = want || n == 9 || n == 10 || n == 13
			}
			if (v.Validate() == nil) != want {
				t.Fatalf("control class mismatch field=%d scalar=%d", kind, n)
			}
			rows++
		}
	}
	reserved := strings.Fields("api assets auth login logout invite reset settings system personal diagnostics livez readyz debug support root admin")
	for _, name := range reserved {
		v := base.Clone()
		chars := []byte(name)
		for n := range chars {
			if n%2 == 0 {
				chars[n] -= 32
			}
		}
		v.Name = string(chars)
		v.NormalizedName = name
		if v.Validate() == nil {
			t.Fatal("mixed-case reserved name admitted")
		}
		v.Name += "-7"
		v.NormalizedName = strings.ToLower(v.Name)
		if v.Validate() != nil {
			t.Fatal("name namespace expanded beyond exact reserved set")
		}
		rows += 2
	}
	for _, name := range []string{"9-0", "AbC", strings.Repeat("Q", 32)} {
		v := base.Clone()
		v.Name = name
		v.NormalizedName = strings.ToLower(name)
		if v.Validate() != nil {
			t.Fatal("legal name boundary rejected")
		}
	}
	for _, name := range []string{"a", "a7", strings.Repeat("q", 33), "-aa", "aa-"} {
		v := base.Clone()
		v.Name = name
		v.NormalizedName = strings.ToLower(name)
		if v.Validate() == nil {
			t.Fatal("illegal name boundary admitted")
		}
	}
	for _, policy := range []agent.ApprovalPolicy{agent.ApprovalDefault, agent.ApprovalAllow, agent.ApprovalAuto} {
		for _, present := range []bool{false, true} {
			v := base.Clone()
			v.ApprovalPolicy = policy
			if present {
				value := v.ModelRef
				v.ApprovalModelRef = &value
			}
			if (v.Validate() == nil) != (present == (policy == agent.ApprovalAuto)) {
				t.Fatal("policy/model null pairing differs")
			}
		}
	}
	t.Logf("exhaustive scalar-class and reserved-neighbor rows=%d", rows)
}

func TestIndependentAgentC1MaximaAndCaps(t *testing.T) {
	v := independentAgentBase(t)
	display := strings.Repeat("\U0010ffff", 256)
	v.DisplayName = &display
	desc := strings.Repeat("<>&", 2731)[:8192]
	v.Description = desc
	instructions := strings.Repeat("&><", 10923)[:32768]
	v.Instructions = instructions
	effort := strings.Repeat(".:_-", 8)
	v.ReasoningEffort = &effort
	v.ApprovalPolicy = agent.ApprovalAuto
	approval := v.ModelRef
	v.ApprovalModelRef = &approval
	raw, err := v.MarshalJSON()
	if err != nil || len(raw) < 6*(8192+32768) || len(raw) >= agent.MaxAgentCoreBytes {
		t.Fatal("simultaneous maxima failed escaping/cap")
	}
	decoded, err := agent.DecodeAgentCore(raw)
	if err != nil || !reflect.DeepEqual(decoded, v) {
		t.Fatal("simultaneous maxima changed body")
	}
	for kind := 0; kind < 4; kind++ {
		bad := v.Clone()
		switch kind {
		case 0:
			bad.Description += "x"
		case 1:
			bad.Instructions += "x"
		case 2:
			*bad.DisplayName += "x"
		case 3:
			*bad.ReasoningEffort += "x"
		}
		if bad.Validate() == nil {
			t.Fatal("one-over metadata limit admitted")
		}
	}
	ref := []byte(`{"config_version":"9223372036854775807","agent_id":"018e9ab1-2345-7a67-89ab-012345678901","project_id":"018e9ab1-2345-7a67-89ab-012345678902"}`)
	for _, tc := range []struct {
		raw      []byte
		limit    int
		decode   func([]byte) error
		standard func([]byte) error
	}{
		{raw, agent.MaxAgentCoreBytes, func(b []byte) error { _, e := agent.DecodeAgentCore(b); return e }, func(b []byte) error { var v agent.AgentCore; return json.Unmarshal(b, &v) }},
		{ref, agent.MaxAgentRefBytes, func(b []byte) error { _, e := agent.DecodeAgentRef(b); return e }, func(b []byte) error { var v agent.AgentRef; return json.Unmarshal(b, &v) }},
	} {
		exact := append(append([]byte{}, tc.raw...), bytes.Repeat([]byte("\n"), tc.limit-len(tc.raw))...)
		if tc.decode(exact) != nil || tc.decode(append(exact, '\t')) == nil {
			t.Fatal("complete raw exact/over cap differs")
		}
		if tc.standard(append(exact, '\t')) != nil {
			t.Fatal("standard library outer-whitespace boundary changed")
		}
		inside := append([]byte{'{'}, bytes.Repeat([]byte(" "), tc.limit-len(tc.raw)+1)...)
		inside = append(inside, tc.raw[1:]...)
		if tc.standard(inside) == nil {
			t.Fatal("nested actual custom-method raw bypassed cap")
		}
	}
	// A container adds no authority. Its inner Core still runs its exact codec.
	var box struct {
		Core agent.AgentCore `json:"core"`
	}
	if json.Unmarshal(append(append([]byte(`{"core":`), raw...), '}'), &box) != nil || !reflect.DeepEqual(box.Core, v) {
		t.Fatal("legal nested Core changed")
	}
	t.Logf("maximal combined Core encoded bytes=%d; direct caps=524288/16384; standard outer-whitespace limitation observed", len(raw))
}

func TestIndependentAgentC1ReferenceAndCopies(t *testing.T) {
	v := independentAgentBase(t)
	text, color, effort := "e\u0301\u2028", "#00ff00", "MODE:-.v1"
	v.DisplayName = &text
	v.TagColor = &color
	v.ReasoningEffort = &effort
	v.ApprovalPolicy = agent.ApprovalAuto
	m := v.ModelRef
	v.ApprovalModelRef = &m
	a, b := v.Clone(), v.Clone()
	*a.DisplayName = "changed"
	*a.TagColor = "#ffffff"
	*a.ReasoningEffort = "other"
	*a.ApprovalModelRef = model.ModelID{}
	a.ModelRef = model.ModelID{}
	if !reflect.DeepEqual(v, b) || v.DisplayName == b.DisplayName || v.TagColor == b.TagColor || v.ReasoningEffort == b.ReasoningEffort || v.ApprovalModelRef == b.ApprovalModelRef {
		t.Fatal("copy has shared metadata/model storage")
	}
	if reflect.TypeOf(v.ID) != reflect.TypeOf(id.AgentID{}) || reflect.TypeOf(v.ProjectID) != reflect.TypeOf(id.ProjectID{}) || reflect.TypeOf(v.ModelRef) != reflect.TypeOf(model.ModelID{}) || reflect.TypeOf(v.ID) == reflect.TypeOf(v.ProjectID) {
		t.Fatal("canonical typed ID ownership changed")
	}
	ref := agent.AgentRef{ProjectID: v.ProjectID, AgentID: v.ID, ConfigVersion: v.Version}
	raw, err := ref.MarshalJSON()
	if err != nil {
		t.Fatal("valid reference encoding failed")
	}
	var members map[string]json.RawMessage
	_ = json.Unmarshal(raw, &members)
	if len(members) != 3 || members["project_id"] == nil || members["agent_id"] == nil || members["config_version"] == nil {
		t.Fatal("Ref closed shape differs")
	}
	for _, tail := range []string{`,"initialized":true}`, `,"Agent_Id":"018e9ab1-2345-7a67-89ab-012345678901"}`, `,"\u0061gent_id":"018e9ab1-2345-7a67-89ab-012345678901"}`} {
		bad := append(append([]byte{}, raw[:len(raw)-1]...), tail...)
		out := ref
		if out.UnmarshalJSON(bad) == nil || out != ref {
			t.Fatal("reference negative accepted or non-atomic")
		}
		zero, e := agent.DecodeAgentRef(bad)
		if e == nil || zero != (agent.AgentRef{}) {
			t.Fatal("failed reference returned partial current fact")
		}
	}
	for _, field := range []string{"project_id", "agent_id", "config_version"} {
		parts := map[string]json.RawMessage{}
		_ = json.Unmarshal(raw, &parts)
		delete(parts, field)
		bad, _ := json.Marshal(parts)
		if _, e := agent.DecodeAgentRef(bad); e == nil {
			t.Fatal("missing reference member admitted")
		}
	}
	var nilCore *agent.AgentCore
	var nilRef *agent.AgentRef
	var nilPolicy *agent.ApprovalPolicy
	var nilLife *agent.AgentLifecycle
	if nilCore.UnmarshalJSON(raw) == nil || nilRef.UnmarshalJSON(raw) == nil || nilPolicy.UnmarshalJSON([]byte(`"allow"`)) == nil || nilLife.UnmarshalJSON([]byte(`"active"`)) == nil {
		t.Fatal("nil receiver accepted")
	}
}

func TestIndependentAgentC1SafeOutputBoundary(t *testing.T) {
	const sentinel = "INDEPENDENT_PRIVATE_AGENT_BODY_83"
	v := independentAgentBase(t)
	v.Description = sentinel
	v.Instructions = sentinel
	values := []any{v, &v, agent.AgentRef{ProjectID: v.ProjectID, AgentID: v.ID, ConfigVersion: v.Version}, agent.ApprovalPolicy(sentinel), agent.AgentLifecycle(sentinel)}
	for _, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X"} {
			if strings.Contains(fmt.Sprintf(format, value), sentinel) {
				t.Fatal("direct Formatter leaked")
			}
		}
		for _, asJSON := range []bool{false, true} {
			var b bytes.Buffer
			var h slog.Handler = slog.NewTextHandler(&b, nil)
			if asJSON {
				h = slog.NewJSONHandler(&b, nil)
			}
			slog.New(h).Info("independent", "value", value)
			if bytes.Contains(b.Bytes(), []byte(sentinel)) {
				t.Fatal("direct LogValuer leaked")
			}
		}
	}
	raw, e := v.MarshalJSON()
	if e != nil || !bytes.Contains(raw, []byte(sentinel)) {
		t.Fatal("business encoding lost legal body")
	}
	for _, wrapper := range []any{struct{ Core agent.AgentCore }{v}, []agent.AgentCore{v}, map[string]agent.AgentCore{"core": v}} {
		var b bytes.Buffer
		slog.New(slog.NewJSONHandler(&b, nil)).Info("boundary-only", "value", wrapper)
		if !bytes.Contains(b.Bytes(), []byte(sentinel)) {
			t.Fatal("expected nested JSON limitation not demonstrated")
		}
	}
	if !strings.Contains(fmt.Sprintf("%+v", struct{ hidden agent.AgentCore }{v}), sentinel) {
		t.Fatal("expected unexported reflection limitation not demonstrated")
	}
	bad := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"`+sentinel+`":false}`)...)
	_, err := agent.DecodeAgentCore(bad)
	var fault *f.Fault
	if !errors.As(err, &fault) {
		t.Fatal("missing safe Fault")
	}
	encoded, _ := json.Marshal(fault)
	if bytes.Contains(encoded, []byte(sentinel)) || strings.Contains(fmt.Sprintf("%+v", err), sentinel) {
		t.Fatal("validation fault exposed rejected member/body")
	}
	t.Log("direct safety verified; enclosing reflection/JSON leaks deliberately observed as prohibited logging inputs, not automatic redaction")
}

// Exact compile-time interface compatibility, with no successful authority fake.
var _ interface {
	RequireCurrentInTx(context.Context, f.Tx, id.Actor, id.ProjectID, id.AgentID) (agent.AgentRef, error)
} = (agent.WorkReferences)(nil)
