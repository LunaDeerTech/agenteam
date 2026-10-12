package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func successorPartsFixture(t *testing.T) (fixture, SuccessorSemanticParts) {
	t.Helper()
	x := setup(t)
	scope, err := i.InProject(x.c.ProjectID)
	must(t, err)
	ref, err := sc.NewCredentialRef(fresh[sc.Credential](t), scope)
	must(t, err)
	x.snapshot.CredentialRef = &ref
	x.snapshot.HeaderOverwrite = map[string]string{"X-Private": "private-successor-header"}
	x.snapshot.Capabilities.ToolCalls = true
	x.snapshot.SelectionVersion = ptr(f.Version(7))
	model := ResolvedModel{Snapshot: x.snapshot, Consumer: x.c, LeaseOwner: x.owner, CredentialLease: &sc.CredentialLease{LeaseID: x.lease, CredentialRef: ref}}
	messages := []Message{
		{Role: "system", Parts: []MessagePart{{Text: &TextPart{Text: "private-successor-system"}}}},
		{Role: "user", Parts: []MessagePart{{Text: &TextPart{Text: "private-successor-user"}}}},
		{Role: "assistant", Parts: []MessagePart{{ToolCall: &ToolCallPart{ID: "original-call", Name: "original_tool", Arguments: json.RawMessage(`{"value":9007199254740993}`)}}}},
		{Role: "tool", Parts: []MessagePart{{ToolResult: &ToolResultPart{CallID: "original-call", Parts: []ResultPart{{Text: &TextPart{Text: "private-successor-result"}}}}}}},
		{Role: "user", Parts: []MessagePart{{Text: &TextPart{Text: "private-successor-next"}}}},
	}
	return x, SuccessorSemanticParts{Actor: x.actor, Model: model, Messages: messages, Tools: []Tool{{Name: "original_tool", Description: "private-successor-tool", InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}}}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}}, ToolChoice: ToolChoice{Kind: "auto"}, ResponseFormat: ResponseFormat{Kind: "text"}, RetryClass: AgentRetry}
}

func successorRequestFixture(t *testing.T, x fixture, parts SuccessorSemanticParts, semantic SuccessorSemantic) ModelRequest {
	t.Helper()
	input := x.input(t)
	input.SchemaVersion, input.Digest = SuccessorInputSchemaVersion, semantic.Digest()
	return ModelRequest{Actor: parts.Actor, CallID: x.call, Consumer: parts.Model.Consumer.Clone(), Model: parts.Model.Clone(), Input: input, Messages: parts.Clone().Messages, Tools: parts.Clone().Tools, ToolChoice: parts.ToolChoice, ResponseFormat: parts.ResponseFormat.Clone(), RetryClass: parts.RetryClass}
}

func TestSuccessorSemanticBindsCompleteModelAndRequest(t *testing.T) {
	x, parts := successorPartsFixture(t)
	original, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	request := successorRequestFixture(t, x, parts, original)
	validated, err := ValidateSuccessorRequest(context.Background(), request)
	must(t, err)
	if validated.Digest() != original.Digest() || !bytes.Contains(original.CanonicalBytes(), []byte("9007199254740993")) {
		t.Fatal("complete semantic digest or original integer changed")
	}
	for _, name := range []string{"snapshot", "parameters", "request-overwrite", "headers", "capabilities", "selection-version", "credential-reference", "lease", "consumer", "message", "tool-schema", "tool-description"} {
		t.Run(name, func(t *testing.T) {
			changed := parts.Clone()
			switch name {
			case "snapshot":
				changed.Model.Snapshot.ID = fresh[Snapshot](t)
			case "parameters":
				changed.Model.Snapshot.Parameters = json.RawMessage(`{"temperature":1}`)
			case "request-overwrite":
				changed.Model.Snapshot.RequestOverwrite = json.RawMessage(`{"seed":4}`)
			case "headers":
				changed.Model.Snapshot.HeaderOverwrite["X-Private"] = "different-header"
			case "capabilities":
				// Preserve old codec acceptance; this does not enable a response
				// profile or assert that an actual adapter accepts parallel calls.
				changed.Model.Snapshot.Capabilities.ParallelToolCalls = true
			case "selection-version":
				*changed.Model.Snapshot.SelectionVersion++
			case "credential-reference":
				scope, err := i.InProject(x.c.ProjectID)
				must(t, err)
				ref, err := sc.NewCredentialRef(fresh[sc.Credential](t), scope)
				must(t, err)
				changed.Model.Snapshot.CredentialRef = &ref
				changed.Model.CredentialLease.CredentialRef = ref
			case "lease":
				changed.Model.CredentialLease.LeaseID = fresh[sc.Lease](t)
			case "consumer":
				changed.Model.Consumer.MeetingID = fresh[i.Execution](t).String()
			case "message":
				changed.Messages[0].Parts[0].Text.Text = "changed-system"
			case "tool-schema":
				changed.Tools[0].InputSchema = json.RawMessage(`{"type":"object","required":["value"]}`)
			case "tool-description":
				changed.Tools[0].Description = "changed-description"
			}
			v, err := NewSuccessorSemantic(context.Background(), changed)
			must(t, err)
			if v.Digest() == original.Digest() {
				t.Fatal("complete semantic change was omitted from digest")
			}
			stale := successorRequestFixture(t, x, changed, original)
			if _, err := ValidateSuccessorRequest(context.Background(), stale); err == nil {
				t.Fatal("request retained a digest from different semantic bytes")
			}
		})
	}
	otherCall := request.Clone()
	otherCall.CallID = fresh[Call](t)
	v, err := ValidateSuccessorRequest(context.Background(), otherCall)
	must(t, err)
	if v.Digest() != original.Digest() {
		t.Fatal("semantic digest incorrectly includes logical CallID")
	}
}

func TestSuccessorSemanticRejectsIncompleteCanonicalAndIdentity(t *testing.T) {
	x, parts := successorPartsFixture(t)
	good, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	for _, name := range []string{"actor", "consumer", "lease-owner", "choice", "retry", "response", "schema", "unknown", "duplicate", "noncanonical"} {
		t.Run(name, func(t *testing.T) {
			raw := good.CanonicalBytes()
			var value map[string]any
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			must(t, d.Decode(&value))
			switch name {
			case "actor":
				value["actor"].(map[string]any)["agent_id"] = fresh[i.Agent](t).String()
			case "consumer":
				value["model"].(map[string]any)["consumer"].(map[string]any)["execution_id"] = fresh[i.Execution](t).String()
			case "lease-owner":
				value["model"].(map[string]any)["lease_owner"].(map[string]any)["id"] = fresh[i.Execution](t).String()
			case "choice":
				value["tool_choice"].(map[string]any)["kind"] = "required"
			case "retry":
				value["retry_class"] = "bounded_consumer"
			case "response":
				value["response_format"].(map[string]any)["name"] = "unexpected"
			case "schema":
				value["schema_version"] = "2"
			case "unknown":
				value["committed"] = true
			}
			if name == "duplicate" {
				raw = append([]byte(`{"schema_version":"3",`), raw[1:]...)
			} else if name == "noncanonical" {
				raw = append([]byte(" "), raw...)
			} else {
				raw, err = json.Marshal(value)
				must(t, err)
				raw, err = cursor.CanonicalJSON(raw)
				must(t, err)
			}
			v, err := DecodeSuccessorSemantic(context.Background(), raw)
			if err == nil || v.Validate() == nil {
				t.Fatal("incomplete or changed semantic identity was accepted")
			}
		})
	}
	r := successorRequestFixture(t, x, parts, good)
	r.Input.SchemaVersion = 1
	if _, err := ValidateSuccessorRequest(context.Background(), r); err == nil {
		t.Fatal("schema-1 identity was accepted as schema 3")
	}
}

func TestSuccessorSemanticOwnsCopiesAndSafeDefaults(t *testing.T) {
	_, parts := successorPartsFixture(t)
	good, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	expected := good.CanonicalBytes()
	parts.Messages[0].Parts[0].Text.Text = "caller-mutated"
	parts.Tools[0].InputSchema[0] = '!'
	parts.Model.Snapshot.Parameters[0] = '!'
	parts.Model.Snapshot.HeaderOverwrite["X-Private"] = "caller-mutated"
	*parts.Model.Consumer.AgentID = fresh[i.Agent](t)
	parts.Model.CredentialLease.LeaseID = fresh[sc.Lease](t)
	loan := good.Parts()
	loan.Messages[0].Parts[0].Text.Text = "reader-mutated"
	loan.Tools[0].InputSchema[0] = '!'
	loan.Model.Snapshot.HeaderOverwrite["X-Private"] = "reader-mutated"
	*loan.Model.Consumer.ExecutionID = fresh[i.Execution](t)
	loan.Model.CredentialLease.LeaseID = fresh[sc.Lease](t)
	clear(good.CanonicalBytes())
	fromOwned, err := NewSuccessorSemantic(context.Background(), good.Parts())
	must(t, err)
	decoded, err := DecodeSuccessorSemantic(context.Background(), expected)
	must(t, err)
	if !bytes.Equal(expected, fromOwned.CanonicalBytes()) || !bytes.Equal(expected, good.CanonicalBytes()) || !bytes.Equal(expected, decoded.CanonicalBytes()) || decoded.Digest() != good.Digest() {
		t.Fatal("caller or getter mutation reached owned semantic state")
	}
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("value", "semantic", good, "parts", good.Parts())
	raw, err := json.Marshal([]any{good, good.Parts()})
	must(t, err)
	printed := fmt.Sprintf("%v %#v %+v", good, good.Parts(), good)
	if strings.Contains(printed+log.String()+string(raw), "private-successor") {
		t.Fatal("default representation exposed protected content")
	}
	var implicit SuccessorSemantic
	if json.Unmarshal(expected, &implicit) == nil || implicit.Validate() == nil {
		t.Fatal("default JSON decoded a protected semantic value")
	}
}

func TestSuccessorSemanticEscapedBudgetAndContext(t *testing.T) {
	_, parts := successorPartsFixture(t)
	good, err := NewSuccessorSemantic(context.Background(), parts)
	must(t, err)
	t.Run("escaping", func(t *testing.T) {
		large := parts.Clone()
		large.Messages[0].Parts[0].Text.Text = strings.Repeat("<", MaxSuccessorSemanticBytes/6+1)
		v, err := NewSuccessorSemantic(context.Background(), large)
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.PayloadTooLarge || v.Validate() == nil {
			t.Fatal("JSON escaping crossed the original stored-size preflight")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		v, err := DecodeSuccessorSemantic(ctx, good.CanonicalBytes())
		if !errors.Is(err, context.Canceled) || v.Validate() == nil {
			t.Fatal("cancelled decode published a semantic value")
		}
	})
	t.Run("nil-context", func(t *testing.T) {
		if _, err := NewSuccessorSemantic(nil, parts); err == nil {
			t.Fatal("nil context was accepted")
		}
	})
}
