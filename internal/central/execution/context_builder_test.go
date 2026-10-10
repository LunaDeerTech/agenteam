package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type contextTriggerFunc func(context.Context, ec.PreparationRequest, ec.CapturedTriggerInput) (ec.TriggerContext, error)

func (fn contextTriggerFunc) BuildTriggerContext(ctx context.Context, r ec.PreparationRequest, s ec.CapturedTriggerInput) (ec.TriggerContext, error) {
	return fn(ctx, r, s)
}

func contextBuilderInput(t *testing.T) ec.PreparationInput {
	t.Helper()
	// Reuse the bounded controlled capture fixture for a complete typed input;
	// this is not real Work source authority or PostgreSQL commit evidence.
	r := newCompleteCaptureControl(t)
	if err := r.p.driver.Run(context.Background(), r.p.execution); err != nil {
		t.Fatal(err)
	}
	stored, err := scanPreparationInput(launchScan{values: r.p.store.inputs[r.p.execution.String()]})
	if err != nil || stored == nil {
		t.Fatal("controlled input", err)
	}
	return stored.input
}
func contextBuilderProvider(t *testing.T) contextTriggerFunc {
	t.Helper()
	component, err := ec.NewPromptComponent("controlled/scene/v1", "scene-private-canary")
	if err != nil {
		t.Fatal(err)
	}
	return func(_ context.Context, r ec.PreparationRequest, s ec.CapturedTriggerInput) (ec.TriggerContext, error) {
		return ec.NewTriggerContext(r, s, component)
	}
}
func TestExecutionContextBuildKeepsCapturedComponents(t *testing.T) {
	input := contextBuilderInput(t)
	b, err := NewContextBuilder(contextBuilderProvider(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.BuildContext(context.Background(), input)
	if err != nil || out.Validate() != nil || out.InputDigest() != input.Digest() {
		t.Fatal("input binding lost", err)
	}
	if !out.Trigger().Matches(input.Fields().Request, input.Fields().Trigger) || out.PlatformPrompt().Content() != input.Fields().PlatformPrompt.Content() || out.AgentInstructions() != input.Fields().Agent.Fields().Core.Instructions || out.Trigger().Instructions().Content() != "scene-private-canary" {
		t.Fatal("prompt components drifted")
	}
	raw := out.CanonicalBytes()
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || !bytes.Equal(raw, canonical) || out.Digest() != ec.TriggerInputDigest(raw) {
		t.Fatal("nondeterministic encoding", err)
	}
	round, err := ec.DecodeExecutionContext(raw)
	if err != nil || round.Digest() != out.Digest() || !bytes.Equal(round.Input().CanonicalBytes(), input.CanonicalBytes()) {
		t.Fatal("closed context round trip", err)
	}
	for n := 0; n < 2; n++ {
		again, err := b.BuildContext(context.Background(), input)
		if err != nil || again.Digest() != out.Digest() || !bytes.Equal(again.CanonicalBytes(), raw) {
			t.Fatal("Build added mutable time/source", err)
		}
	}
	// Mutating returned material or constructing a different current config
	// cannot replace this context's captured Agent or source.
	fields := out.Input().Fields()
	a := fields.Agent.Fields()
	a.Core.Instructions = "later-agent-private-canary"
	fields.Agent, err = ac.NewAgentConfig(a)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ec.NewPreparationInput(fields)
	if err != nil {
		t.Fatal(err)
	}
	other, err := b.BuildContext(context.Background(), changed)
	if err != nil || other.Digest() == out.Digest() || out.InputDigest() != input.Digest() || out.AgentInstructions() == a.Core.Instructions {
		t.Fatal("new configuration rewrote fixed input", err)
	}
	raw[0] = '!'
	source := out.Trigger().Source().Data()
	source[0] = '!'
	if out.Digest() != ec.TriggerInputDigest(out.CanonicalBytes()) || out.Trigger().Source().Validate() != nil {
		t.Fatal("mutable bytes escaped")
	}
	implicit, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, safe := range []string{string(implicit), fmt.Sprintf("%+v %#v", out, out), out.LogValue().String(), fmt.Sprintf("%+v", out.Trigger()), fmt.Sprintf("%+v", out.Trigger().Instructions())} {
		if strings.Contains(safe, "canary") || strings.Contains(safe, input.Fields().Model.Snapshot.Endpoint) {
			t.Fatal("implicit context output leaked source material")
		}
	}
	if string(implicit) != `"execution_context"` {
		t.Fatal("implicit JSON exposed internal snapshot")
	}
	// Canonical decoding keeps an exact, closed envelope and source binding.
	for _, mode := range []string{"unknown", "schema", "input-digest", "trigger-source", "prompt-digest"} {
		var fields map[string]json.RawMessage
		if json.Unmarshal(out.CanonicalBytes(), &fields) != nil {
			t.Fatal("fixture JSON")
		}
		switch mode {
		case "unknown":
			fields["unexpected"] = json.RawMessage(`true`)
		case "schema":
			fields["schema_version"] = json.RawMessage(`2`)
		case "input-digest":
			fields["input_digest"], _ = json.Marshal(ec.TriggerInputDigest([]byte("other")))
		case "trigger-source", "prompt-digest":
			var trigger map[string]json.RawMessage
			_ = json.Unmarshal(fields["trigger_context"], &trigger)
			if mode == "trigger-source" {
				trigger["execution_id"], _ = json.Marshal(newTestID[i.Execution](t))
			} else {
				var prompt map[string]json.RawMessage
				_ = json.Unmarshal(trigger["instructions"], &prompt)
				prompt["digest"], _ = json.Marshal(ec.TriggerInputDigest([]byte("other")))
				trigger["instructions"], _ = json.Marshal(prompt)
			}
			fields["trigger_context"], _ = json.Marshal(trigger)
		}
		bad, _ := json.Marshal(fields)
		bad, _ = cursor.CanonicalJSON(bad)
		if got, err := ec.DecodeExecutionContext(bad); err == nil || got.Validate() == nil {
			t.Fatal("bad context accepted", mode)
		}
	}
}
func TestExecutionContextBuildRejectsMissingMismatchedAndCanceledProvider(t *testing.T) {
	input := contextBuilderInput(t)
	var absent contextTriggerFunc
	for _, provider := range []ec.TriggerContextBuilder{nil, absent} {
		if _, err := NewContextBuilder(provider); err == nil {
			t.Fatal("missing provider became empty context")
		}
	}
	for _, mode := range []string{"zero", "execution", "request-key", "source", "callback-error", "callback-cancel", "already-canceled"} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		base := contextBuilderProvider(t)
		provider := contextTriggerFunc(func(ctx context.Context, r ec.PreparationRequest, s ec.CapturedTriggerInput) (ec.TriggerContext, error) {
			calls++
			switch mode {
			case "zero":
				return ec.TriggerContext{}, nil
			case "execution":
				r.ExecutionID = newTestID[i.Execution](t)
			case "request-key":
				r.Launch.Meta.IdempotencyKey = "different-build-request"
			case "source":
				ref := s.Ref()
				ref.InputID = newTestID[ec.TriggerInput](t)
				var err error
				s, err = ec.NewCapturedTriggerInput(ref, s.Data())
				if err != nil {
					t.Fatal(err)
				}
			case "callback-error":
				return ec.TriggerContext{}, errors.New("private-source-canary")
			case "callback-cancel":
				cancel()
			}
			return base(ctx, r, s)
		})
		b, err := NewContextBuilder(provider)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "already-canceled" {
			cancel()
		}
		out, err := b.BuildContext(ctx, input)
		cancel()
		if err == nil || out.Validate() == nil || strings.Contains(err.Error(), "canary") {
			t.Fatal("invalid provider result published", mode, err)
		}
		if mode == "already-canceled" && calls != 0 {
			t.Fatal("canceled Build called provider")
		}
		if mode == "callback-cancel" && !errors.Is(err, context.Canceled) {
			t.Fatal("callback cancellation lost", err)
		}
	}
	b, _ := NewContextBuilder(contextBuilderProvider(t))
	_, err := b.BuildContext(nil, input)
	requireCode(t, err, f.InvalidArgument)
}
