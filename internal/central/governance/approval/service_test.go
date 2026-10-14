package approval

import (
	"bytes"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"strings"
	"testing"
)

func testID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, err := f.NewID[T]()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func testBinding(t *testing.T) tc.ToolCallBinding {
	p, a, e := testID[i.Project](t), testID[i.Agent](t), testID[i.Execution](t)
	actor, _ := i.NewAgentRun(p, a, e)
	tool := testID[i.Tool](t)
	return tc.ToolCallBinding{Actor: actor, ProjectID: p, AgentID: a, ExecutionID: e, RoundID: testID[struct{}](t).String(), SnapshotID: testID[struct{}](t).String(), InputBindingID: testID[struct{}](t).String(), LogicalCallID: testID[mc.Call](t), InvocationID: testID[mc.Invocation](t), CallID: "original-call", ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1}, RunnerBinding: tc.RunCommandRegistration().Binding, Input: tc.ToolInputReference{PayloadID: testID[struct{}](t).String(), SHA256: oc.DigestBytes([]byte("private argv")), Bytes: 12}, CanonicalArguments: oc.DigestBytes([]byte("canonical args"))}
}
func TestApprovalBindingPreservesAllImmutableReferences(t *testing.T) {
	b := testBinding(t)
	raw, err := bindingBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeBinding(raw)
	if err != nil || !sameBinding(got, b) {
		t.Fatal("binding did not round trip", err)
	}
	for _, canary := range []string{"private argv", "canonical args", "tool_call_binding", "tool_input_reference"} {
		if bytes.Contains(raw, []byte(canary)) {
			t.Fatalf("unexpected opaque/protected encoding: %s", canary)
		}
	}
	changed := b
	changed.Input.PayloadID = testID[struct{}](t).String()
	if sameBinding(b, changed) {
		t.Fatal("payload substitution matched")
	}
	changed = b
	changed.CanonicalArguments = oc.DigestBytes([]byte("other"))
	if sameBinding(b, changed) {
		t.Fatal("argument substitution matched")
	}
	changed = b
	changed.RoundID = testID[struct{}](t).String()
	if sameBinding(b, changed) {
		t.Fatal("different operation round matched")
	}
	for name, bad := range map[string][]byte{"unknown": append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"unknown":1}`)...), "duplicate": append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"CallID":"original-call"}`)...), "trailing": append(append([]byte{}, raw...), []byte(`{}`)...), "actor": bytes.Replace(raw, []byte(`"Kind":"agent_run"`), []byte(`"Kind":"human"`), 1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeBinding(bad); err == nil {
				t.Fatal("noncanonical binding accepted")
			}
		})
	}
}
func TestApprovalDecisionDigestPreservesIntentAcrossSessions(t *testing.T) {
	user := testID[i.User](t)
	a, _ := i.NewHuman(user, testID[i.Session](t))
	next, _ := i.NewHuman(user, testID[i.Session](t))
	p, id := testID[i.Project](t), testID[c.RequestIdentity](t)
	input := c.Decision{Decision: "approve", ExpectedVersion: 1}
	first, err := decisionDigest(a, p, id, input)
	if err != nil {
		t.Fatal(err)
	}
	again, err := decisionDigest(next, p, id, input)
	if err != nil || first != again {
		t.Fatal("Session change altered original intent")
	}
	other, _ := i.NewHuman(testID[i.User](t), testID[i.Session](t))
	for name, fn := range map[string]func() (f.Digest, error){"user": func() (f.Digest, error) { return decisionDigest(other, p, id, input) }, "project": func() (f.Digest, error) { return decisionDigest(a, testID[i.Project](t), id, input) }, "request": func() (f.Digest, error) { return decisionDigest(a, p, testID[c.RequestIdentity](t), input) }, "decision": func() (f.Digest, error) {
		return decisionDigest(a, p, id, c.Decision{Decision: "reject", ExpectedVersion: 1})
	}, "version": func() (f.Digest, error) {
		return decisionDigest(a, p, id, c.Decision{Decision: "approve", ExpectedVersion: 2})
	}} {
		t.Run(name, func(t *testing.T) {
			got, err := fn()
			if err != nil || got == first {
				t.Fatal("different intent reused digest", err)
			}
		})
	}
}
