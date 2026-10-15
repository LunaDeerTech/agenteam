package runtime

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"testing"
)

func TestMCPUnknownKeepsExistingOperationStateAndSeparateAttempt(t *testing.T) {
	call := mcpCodecBinding(t)
	input, raw, err := builtinInput(context.Background(), call, []byte(`{}`))
	// The fixture input digest is deliberately unrelated. Only canonical input is admitted.
	if err == nil {
		t.Fatal("MCP input substituted")
	}
	input = bindingProjection(call)
	raw, err = encode(input)
	if err != nil {
		t.Fatal(err)
	}
	r := builtinRecord{ID: newOperationID[tc.Operation](t), Input: input, InputDigest: digest(raw), Fingerprint: digest(raw), Phase: "failed", Version: 3, Attempt: newOperationID[tc.Attempt](t), Request: p.ID(newOperationID[f.Request](t).String())}
	body := []byte(`{"outcome_known":false}`)
	term := builtinTerminal{Format: 1, Operation: r.ID, Attempt: r.Attempt, Request: r.Request, Outcome: "unknown", Code: string(f.DependencyUnavailable), ResultDigest: digest(body), ResultBytes: f.Progress(len(body))}
	if r.validate() != nil || term.validate(r, body) != nil {
		t.Fatal("original failed/unknown tuple rejected")
	}
	phase, outcome := term.phases()
	if phase != "failed" || outcome != "unknown" || term.attemptPhase() != "unknown" {
		t.Fatal("unknown effect changed original operation state machine")
	}
	r.Phase = "unknown"
	if r.validate() == nil {
		t.Fatal("new operation phase accepted")
	}
	r.Phase = "failed"
	r.Version = 2
	r.Attempt = tc.AttemptID{}
	r.Request = ""
	if r.validate() != nil {
		t.Fatal("unattempted MCP denial rejected")
	}
	r.Input.MCPBinding = nil
	r.Input.Binding = tc.BuiltinBinding{HandlerID: "task.read", ContractRevision: 1}
	raw, _ = encode(r.Input)
	r.InputDigest = digest(raw)
	r.Fingerprint = r.InputDigest
	if r.validate() == nil {
		t.Fatal("ordinary builtin acquired MCP denial exception")
	}
}
