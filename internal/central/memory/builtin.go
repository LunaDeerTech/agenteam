package memory

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

type MemoryTools interface {
	Retain(context.Context, i.Actor, c.Namespace, tc.OperationID, c.RetainInput) (c.RetainResult, error)
	Recall(context.Context, i.Actor, c.Namespace, tc.OperationID, c.RecallInput) (c.RecallResult, error)
	Reflect(context.Context, i.Actor, c.Namespace, tc.OperationID, c.ReflectInput) (c.ReflectResult, error)
	LookupRetain(context.Context, i.Actor, c.Namespace, tc.OperationID, c.RetainInput) (c.RetainResult, bool, error)
	RequireBuiltinBindingInTx(context.Context, f.Tx, any) error
	JoinOperation(context.Context, i.Actor, c.Namespace, tc.OperationID) error
}
type builtinBackend struct {
	runtime MemoryTools
	name    string
}

func NewBuiltinBackends(runtime MemoryTools) ([]tr.BuiltinBackend, error) {
	if nilPort(runtime) {
		return nil, fault(f.DependencyUnbound)
	}
	out := make([]tr.BuiltinBackend, 0, 3)
	for _, name := range []string{"retain", "recall", "reflect"} {
		out = append(out, &builtinBackend{runtime, name})
	}
	return out, nil
}
func (b *builtinBackend) Registration() tc.BuiltinRegistration {
	if b == nil || nilPort(b.runtime) {
		return tc.BuiltinRegistration{}
	}
	definition := memoryDefinition(b.name)
	return tc.BuiltinRegistration{Definition: definition, Binding: tc.BuiltinBinding{HandlerID: "memory." + b.name, ContractRevision: 1}, ScopeResolverID: "memory.namespace.v1", RiskClassifierID: tc.RiskClassifierID("memory." + b.name + ".v1"), Class: tc.CoreTool, Active: true}
}
func memoryDefinition(name string) tc.Definition {
	definition := tc.Definition{StableKey: "builtin:" + name, Name: name, OutputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &tc.Annotations{ReadOnly: true, Idempotency: tc.Inherent}}
	switch name {
	case "retain":
		definition.Description = "Explicitly retain durable lessons and reusable procedures for this Agent across Executions. Do not submit credentials, temporary progress or ordinary canonical Project facts. The Memory service may consolidate the sanitized lesson with this Agent's existing memories."
		definition.Annotations = &tc.Annotations{Idempotency: tc.Keyed}
		definition.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["content"],"properties":{"content":{"type":"string","minLength":1,"maxLength":32768},"source_context":{"type":"string","minLength":1,"maxLength":4096},"intent":{"type":"string","minLength":1,"maxLength":1024}}}`)
	case "recall":
		definition.Description = "Recall this Agent's active long-term memories before tackling a recurring problem. The namespace is bound to the current Execution; other Agents' memories are unavailable."
		definition.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1,"maxLength":4096},"limit":{"type":"integer","minimum":1,"maximum":32},"filters":{"type":"object","additionalProperties":false,"properties":{"types":{"type":"array","maxItems":4,"items":{"type":"string","enum":["fact","experience","procedure","observation"]}},"tags":{"type":"array","maxItems":16,"items":{"type":"string","minLength":1,"maxLength":64}}}}}}`)
	case "reflect":
		definition.Description = "Produce a read-only synthesis from this Agent's active memories. Reflection never adds, updates or deletes memories; call retain explicitly if a new lesson should be saved."
		definition.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1,"maxLength":4096}}}`)
	default:
		return tc.Definition{}
	}
	return definition
}
func (b *builtinBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || nilPort(b.runtime) {
		return fault(f.DependencyUnbound)
	}
	return b.runtime.RequireBuiltinBindingInTx(ctx, tx, store)
}

// JoinBuiltinCall is lent only the original still-active executor witness.
// It can settle retained work; it cannot discover or resend a model request.
func (b *builtinBackend) JoinBuiltinCall(ctx context.Context, inv tr.BuiltinInvocation) error {
	if b == nil || nilPort(b.runtime) {
		return fault(f.DependencyUnbound)
	}
	if inv.Validate() != nil || inv.Call.Binding.HandlerID != "memory."+b.name {
		return fault(f.Forbidden)
	}
	return b.runtime.JoinOperation(ctx, inv.Call.Actor, c.Namespace{ProjectID: inv.Call.ProjectID, AgentID: inv.Call.AgentID}, inv.OperationID)
}
func (b *builtinBackend) Execute(ctx context.Context, inv tr.BuiltinInvocation, raw []byte) ([]byte, error) {
	if b == nil || nilPort(b.runtime) {
		return nil, fault(f.DependencyUnbound)
	}
	if inv.Validate() != nil || inv.Call.Binding.HandlerID != "memory."+b.name {
		return nil, fault(f.Forbidden)
	}
	n := c.Namespace{ProjectID: inv.Call.ProjectID, AgentID: inv.Call.AgentID}
	var out any
	var err error
	switch b.name {
	case "retain":
		var input c.RetainInput
		if c.Decode(raw, &input, 48<<10) != nil || input.Validate() != nil {
			return nil, fault(f.InvalidArgument)
		}
		out, err = b.runtime.Retain(ctx, inv.Call.Actor, n, inv.OperationID, input)
	case "recall":
		var input c.RecallInput
		if c.Decode(raw, &input, 16<<10) != nil || input.Validate() != nil {
			return nil, fault(f.InvalidArgument)
		}
		out, err = b.runtime.Recall(ctx, inv.Call.Actor, n, inv.OperationID, input)
	case "reflect":
		var input c.ReflectInput
		if c.Decode(raw, &input, 16<<10) != nil || input.Validate() != nil {
			return nil, fault(f.InvalidArgument)
		}
		out, err = b.runtime.Reflect(ctx, inv.Call.Actor, n, inv.OperationID, input)
	default:
		return nil, fault(f.InvalidArgument)
	}
	if err != nil {
		return nil, err
	}
	return encode(out)
}
func (b *builtinBackend) Lookup(ctx context.Context, inv tr.BuiltinInvocation, raw []byte) ([]byte, bool, error) {
	if b == nil || nilPort(b.runtime) {
		return nil, false, fault(f.DependencyUnbound)
	}
	if inv.Validate() != nil || inv.Call.Binding.HandlerID != "memory."+b.name {
		return nil, false, fault(f.Forbidden)
	}
	if b.name != "retain" {
		return nil, false, nil
	}
	var input c.RetainInput
	if c.Decode(raw, &input, 48<<10) != nil || input.Validate() != nil {
		return nil, false, fault(f.InvalidArgument)
	}
	out, found, err := b.runtime.LookupRetain(ctx, inv.Call.Actor, c.Namespace{ProjectID: inv.Call.ProjectID, AgentID: inv.Call.AgentID}, inv.OperationID, input)
	if err != nil || !found {
		return nil, false, err
	}
	encoded, err := encode(out)
	return encoded, err == nil, err
}

func (b *builtinBackend) LookupCanonical(ctx context.Context, inv tr.BuiltinInvocation, raw []byte) (tr.BuiltinCanonicalOutcome, error) {
	body, found, err := b.Lookup(ctx, inv, raw)
	if err != nil || found {
		return tr.BuiltinCanonicalOutcome{Found: found, Body: body}, err
	}
	if b.name != "retain" {
		return tr.BuiltinCanonicalOutcome{}, fault(f.Forbidden)
	}
	negative, ok := b.runtime.(interface {
		LookupRetainFailure(context.Context, i.Actor, c.Namespace, tc.OperationID, c.RetainInput) (f.Code, bool, error)
	})
	if !ok {
		return tr.BuiltinCanonicalOutcome{}, nil
	}
	var input c.RetainInput
	if c.Decode(raw, &input, 48<<10) != nil || input.Validate() != nil {
		return tr.BuiltinCanonicalOutcome{}, fault(f.InvalidArgument)
	}
	code, found, err := negative.LookupRetainFailure(ctx, inv.Call.Actor, c.Namespace{ProjectID: inv.Call.ProjectID, AgentID: inv.Call.AgentID}, inv.OperationID, input)
	return tr.BuiltinCanonicalOutcome{Found: found, Failure: code}, err
}
