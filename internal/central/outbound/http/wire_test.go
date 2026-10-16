package outboundhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

func testActor(t *testing.T) identity.Actor {
	t.Helper()
	u, err := foundation.NewID[identity.User]()
	if err != nil {
		t.Fatal(err)
	}
	s, err := foundation.NewID[identity.Session]()
	if err != nil {
		t.Fatal(err)
	}
	a, err := identity.NewHuman(u, s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func updateRequest(raw string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "https://example.test"+policyPath, strings.NewReader(raw))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	r.Header.Set("Idempotency-Key", "policy-update")
	return r
}

func requireCode(t *testing.T, err error, code foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != code {
		t.Fatalf("expected safe fault %s", code)
	}
}

func TestOutboundWireCanonicalIdentityAndBounds(t *testing.T) {
	actor := testActor(t)
	raw := `{"rule_mode":"denylist","expected_version":"9223372036854775807","rules":[{"ports":[8443,443],"target":"fd00::/8"},{"ports":"all","target":"10.2.0.0/16"}]}`
	var first outbound.CommandMeta
	var firstRules outbound.Rules
	httpapi.WithRequestID(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta, mode, rules, err := decodeUpdate(w, r, actor)
		if err != nil {
			t.Fatal("valid wire failed", err)
		}
		if mode != outbound.Denylist || meta.ExpectedVersion != foundation.Version(math.MaxInt64) || meta.Identity.Namespace() != "outbound-policy-v2" || meta.Identity.Command() != "update" || !reflect.DeepEqual(meta.Identity.OwnerIDs(), []string{actor.Details().UserID}) || meta.HTTPTraceID != httpapi.RequestID(r.Context()).String() {
			t.Fatal("server identity or version changed")
		}
		first, firstRules = meta, rules
	})).ServeHTTP(httptest.NewRecorder(), updateRequest(raw))
	want := `[{"target":"10.2.0.0/16","ports":"all"},{"target":"fd00::/8","ports":[443,8443]}]`
	if string(firstRules.JSON()) != want {
		t.Fatal("canonical rules changed")
	}
	canonical := `{"rule_mode":"denylist","expected_version":"9223372036854775807","rules":` + want + `}`
	meta, mode, rules, err := decodeUpdate(httptest.NewRecorder(), updateRequest(canonical), actor)
	if err != nil || mode != outbound.Denylist || meta.Identity.Canonical() != first.Identity.Canonical() || !bytes.Equal(rules.JSON(), firstRules.JSON()) || meta.HTTPTraceID != "" {
		t.Fatal("reordering changed intent or injected trace")
	}
	// Session is transport authority, not the original command identity.
	uid, err := foundation.ParseID[identity.User](actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := foundation.NewID[identity.Session]()
	if err != nil {
		t.Fatal(err)
	}
	next, err := identity.NewHuman(uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	meta, _, _, err = decodeUpdate(httptest.NewRecorder(), updateRequest(canonical), next)
	if err != nil || meta.Identity.Canonical() != first.Identity.Canonical() {
		t.Fatal("new Session changed same user's command")
	}
}

func TestOutboundWireRejectsAmbiguousInput(t *testing.T) {
	actor := testActor(t)
	for _, raw := range []string{
		`{"expected_version":"1","rules":[]}`, `{"expected_version":"1","rule_mode":"bogus","rules":[]}`, `{"expected_version":"1","rule_mode":null,"rules":[]}`, `{"expected_version":"1","rule_mode":"denylist","rules":[{"cidr":"10.0.0.0/8","ports":"all"}]}`,
		`null`, `[]`, `{}`, `{"rule_mode":"denylist","expected_version":"1"}`, `{"rules":[]}`, `{"rule_mode":"denylist","expected_version":null,"rules":[]}`,
		`{"rule_mode":"denylist","expected_version":1,"rules":[]}`, `{"rule_mode":"denylist","expected_version":"01","rules":[]}`, `{"rule_mode":"denylist","expected_version":"0","rules":[]}`, `{"rule_mode":"denylist","expected_version":"9223372036854775808","rules":[]}`,
		`{"rule_mode":"denylist","expected_version":"1","rules":null}`, `{"rule_mode":"denylist","expected_version":"1","rules":[],"scope":"system"}`, `{"rule_mode":"denylist","expected_version":"1","rule_mode":"denylist","expected_version":"1","rules":[]}`,
		`{"rule_mode":"denylist","expected_version":"1","rules":[],"ru\u006ces":[]}`, `{"rule_mode":"denylist","expected_version":"1","rules":[]} {}`, string([]byte{'{', 0xff, '}'}),
	} {
		_, _, _, err := decodeUpdate(httptest.NewRecorder(), updateRequest(raw), actor)
		requireCode(t, err, foundation.InvalidArgument)
	}
	for _, rule := range []string{
		`{}`, `null`, `{"target":"10.0.0.0/8"}`, `{"ports":"all"}`,
		`{"target":"10.0.0.0/8","ports":"all","allow_http":null}`, `{"target":"10.0.0.0/8","ports":"all","extra":false}`,
		`{"target":"10.0.0.0/8","ports":"all","ports":"all"}`, `{"target":"10.0.0.0/8","ports":null}`,
		`{"target":"10.0.0.0/8","ports":[]}`, `{"target":"10.0.0.0/8","ports":[443,443]}`,
		`{"target":"10.0.0.0/8","ports":["443"]}`, `{"target":"10.0.0.0/8","ports":[443.0]}`, `{"target":"10.0.0.0/8","ports":[4.43e2]}`,
		`{"target":"10.0.0.0/8","ports":[0]}`, `{"target":"10.0.0.0/8","ports":[-1]}`, `{"target":"10.0.0.0/8","ports":[65536]}`,
		`{"target":"10.0.0.1/8","ports":"all"}`, `{"target":"https://example.test","ports":"all"}`, `{"target":"*.127.0.0.1","ports":"all"}`,
		`{"target":"::ffff:10.0.0.0/104","ports":"all"}`, `{"target":"FD00::/8","ports":"all"}`, `{"target":"fd00:0::/8","ports":"all"}`,
	} {
		_, _, _, err := decodeUpdate(httptest.NewRecorder(), updateRequest(`{"rule_mode":"denylist","expected_version":"1","rules":[`+rule+`]}`), actor)
		requireCode(t, err, foundation.InvalidArgument)
	}
	for _, key := range []string{"", "two words", " leading", "trailing ", "a,b", "雪", strings.Repeat("x", 129)} {
		r := updateRequest(`{"rule_mode":"denylist","expected_version":"1","rules":[]}`)
		r.Header.Set("Idempotency-Key", key)
		_, _, _, err := decodeUpdate(httptest.NewRecorder(), r, actor)
		requireCode(t, err, foundation.InvalidArgument)
	}
	for _, key := range []string{"a", "aA09._:/-", strings.Repeat("x", 128)} {
		r := updateRequest(`{"rule_mode":"denylist","expected_version":"1","rules":[]}`)
		r.Header.Set("Idempotency-Key", key)
		if _, _, _, err := decodeUpdate(httptest.NewRecorder(), r, actor); err != nil {
			t.Fatal("valid key rejected")
		}
	}
	r := updateRequest(`{"rule_mode":"denylist","expected_version":"1","rules":[]}`)
	r.Header.Add("Idempotency-Key", "second")
	_, _, _, err := decodeUpdate(httptest.NewRecorder(), r, actor)
	requireCode(t, err, foundation.InvalidArgument)
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		func(r *http.Request) { r.Header.Add("Content-Type", "application/json") },
		func(r *http.Request) { r.Header.Set("Content-Type", "application/json; charset=ascii") },
		func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") },
	} {
		r := updateRequest(`{"rule_mode":"denylist","expected_version":"1","rules":[]}`)
		mutate(r)
		_, _, _, err := decodeUpdate(httptest.NewRecorder(), r, actor)
		requireCode(t, err, foundation.UnsupportedMediaType)
	}
}

func TestOutboundWireMaximumRulesAndIndependentByteLimits(t *testing.T) {
	actor := testActor(t)
	ports := make([]uint16, 256)
	for i := range ports {
		ports[i] = uint16(65535 - i)
	}
	selected, err := outbound.SelectedPorts(ports...)
	if err != nil {
		t.Fatal(err)
	}
	rules := make([]outbound.Rule, 512)
	for i := range rules {
		rules[i], err = outbound.NewRule(fmt.Sprintf("%d.%s.%s.%s.%s", i, strings.Repeat("a", 63), strings.Repeat("b", 63), strings.Repeat("c", 63), strings.Repeat("d", 56)), selected)
		if err != nil {
			t.Fatal(err)
		}
	}
	maximum, err := outbound.NewRules(rules...)
	if err != nil {
		t.Fatal(err)
	}
	input := `{"rule_mode":"denylist","expected_version":"1","rules":` + string(maximum.JSON()) + `}`
	_, _, got, err := decodeUpdate(httptest.NewRecorder(), updateRequest(input), actor)
	if err != nil || got.Count() != 512 || !bytes.Equal(got.JSON(), maximum.JSON()) {
		t.Fatal("maximum legal rules rejected")
	}
	out, err := encodePolicy(samplePolicy(t, maximum))
	if err != nil || len(out) > maxJSONBytes || !bytes.Contains(out, maximum.JSON()) {
		t.Fatal("maximum response truncated or rejected")
	}
	for _, raw := range []string{
		`[{"target":"10.0.0.0/8","ports":"all"},{"target":"10.0.0.0/8","ports":"all"}]`,
		"[" + strings.Repeat(`{"target":"10.0.0.0/8","ports":"all"},`, 512) + `{"target":"10.0.0.0/8","ports":"all"}]`,
		`[{"target":"10.0.0.0/8","ports":[` + strings.Repeat("1,", 256) + `2]}]`,
	} {
		_, _, _, err := decodeUpdate(httptest.NewRecorder(), updateRequest(`{"rule_mode":"denylist","expected_version":"1","rules":`+raw+`}`), actor)
		requireCode(t, err, foundation.InvalidArgument)
	}
	// The complete request, including field and outer whitespace, has one
	// 1 MiB budget. A legal 512 x 256-port payload fits under that bound.
	empty := `{"rule_mode":"denylist","expected_version":"1","rules":[]}`
	for _, delta := range []int{0, 1} {
		body := empty + strings.Repeat(" ", maxJSONBytes-len(empty)+delta)
		_, _, _, err = decodeUpdate(httptest.NewRecorder(), updateRequest(body), actor)
		if delta == 0 && err != nil {
			t.Fatal("exact body limit rejected")
		}
		if delta == 1 {
			requireCode(t, err, foundation.PayloadTooLarge)
		}
	}
}

func testReceipt(t *testing.T) outbound.UpdateResult {
	t.Helper()
	id, err := foundation.NewID[ac.Record]()
	if err != nil {
		t.Fatal(err)
	}
	at, err := foundation.NewInstant(time.Date(2026, 10, 6, 7, 8, 9, 123456000, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return outbound.UpdateResult{Version: 2, RuleCount: 0, AuditID: id, CreatedAt: at}
}

func TestOutboundWireOutputAndRedaction(t *testing.T) {
	rules, err := outbound.DecodeRules([]byte(`[{"target":"10.2.0.0/16","ports":"all"}]`))
	if err != nil {
		t.Fatal(err)
	}
	policy := samplePolicy(t, rules)
	for _, value := range []any{rules, policy} {
		b, err := json.Marshal(value)
		if err != nil || strings.Contains(fmt.Sprint(value), "10.2.") || bytes.Contains(b, []byte("10.2.")) {
			t.Fatal("ordinary projection leaked rules")
		}
	}
	for _, output := range []struct {
		value  any
		fields []string
	}{
		{policy, []string{"rule_mode", "rules", "version"}},
		{testReceipt(t), []string{"audit_id", "created_at", "rule_count", "version"}},
	} {
		var b []byte
		switch v := output.value.(type) {
		case outbound.Policy:
			b, err = encodePolicy(v)
		case outbound.UpdateResult:
			b, err = encodeUpdate(v)
		}
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(b, &fields) != nil || len(fields) != len(output.fields) {
			t.Fatal("wrong output shape")
		}
		for _, f := range output.fields {
			if fields[f] == nil {
				t.Fatal("missing output field")
			}
		}
	}
	if b, err := encodePolicy(outbound.Policy{}); err == nil || b != nil {
		t.Fatal("zero policy leaked candidate")
	}
	for _, mutate := range []func(*outbound.UpdateResult){
		func(v *outbound.UpdateResult) { v.Version = 0 }, func(v *outbound.UpdateResult) { v.RuleCount = -1 }, func(v *outbound.UpdateResult) { v.RuleCount = 513 }, func(v *outbound.UpdateResult) { v.AuditID = ac.ID{} },
	} {
		v := testReceipt(t)
		mutate(&v)
		b, err := encodeUpdate(v)
		if b != nil || err == nil {
			t.Fatal("invalid receipt leaked candidate")
		}
		var f *foundation.Fault
		if !errors.As(err, &f) || f.CommitState != foundation.Unknown {
			t.Fatal("invalid accepted result claimed not_started")
		}
	}
}

func TestOutboundSchemaMatchesFourOperationsAndClosedWire(t *testing.T) {
	b, err := os.ReadFile("../../../../api/openapi/outbound-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		OpenAPI string `json:"openapi"`
		Paths   map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Type       string                     `json:"type"`
				Additional bool                       `json:"additionalProperties"`
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if json.Unmarshal(b, &spec) != nil || spec.OpenAPI != "3.1.0" || len(spec.Paths) != 2 {
		t.Fatal("invalid OpenAPI")
	}
	methods := spec.Paths[policyPath]
	if len(methods) != 2 || methods["get"].OperationID != "getSystemOutboundPolicy" || methods["put"].OperationID != "updateSystemOutboundPolicy" {
		t.Fatal("route/OpenAPI mismatch")
	}
	proxy := spec.Paths[proxyPath]
	if len(proxy) != 2 || proxy["get"].OperationID != "getSystemOutboundProxy" || proxy["put"].OperationID != "updateSystemOutboundProxy" {
		t.Fatal("proxy route/OpenAPI mismatch")
	}
	want := map[string][]string{"OutboundRuleInput": {"target", "ports"}, "OutboundRule": {"target", "ports"}, "OutboundPolicy": {"version", "rule_mode", "rules"}, "OutboundPolicyUpdateInput": {"expected_version", "rule_mode", "rules"}, "OutboundPolicyUpdateResult": {"version", "rule_count", "audit_id", "created_at"}}
	for name, required := range want {
		s := spec.Components.Schemas[name]
		if s.Type != "object" || s.Additional || !reflect.DeepEqual(s.Required, required) {
			t.Fatalf("schema closure mismatch %s", name)
		}
		for _, field := range required {
			if s.Properties[field] == nil {
				t.Fatal("required field not declared")
			}
		}
	}
	if !bytes.Contains(b, []byte("./common.json#/components/schemas/Version")) || !bytes.Contains(b, []byte("./common.json#/components/schemas/Instant")) || !bytes.Contains(b, []byte("./common.json#/components/schemas/IdempotencyKey")) {
		t.Fatal("scalars diverged")
	}
}
