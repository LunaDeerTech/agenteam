// Package tools binds Knowledge reads to the original Core Tool invocation.
package tools

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

type backend struct {
	service       *knowledge.IndexService
	name, handler string
}

func NewBackends(service *knowledge.IndexService) ([]tr.BuiltinBackend, error) {
	if service == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return []tr.BuiltinBackend{&backend{service, "query-doc", "knowledge.query"}, &backend{service, "read-doc", "knowledge.read"}}, nil
}
func (b *backend) Registration() tc.BuiltinRegistration {
	if b == nil || b.service == nil {
		return tc.BuiltinRegistration{}
	}
	definition := tc.Definition{StableKey: "builtin:" + b.name, Name: b.name, Annotations: &tc.Annotations{ReadOnly: true, Idempotency: tc.Inherent}}
	switch b.name {
	case "query-doc":
		definition.Description = "Search the current project's knowledge documents. Results include document_id, exact document_version and a source locator; use read-doc with that version to inspect the source."
		definition.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1,"maxLength":16384},"limit":{"type":"integer","minimum":1,"maximum":100},"document_id":{"type":"string","format":"uuid"},"media_type":{"type":"string"},"source_kind":{"type":"string","enum":["text","file"]}}}`)
		definition.OutputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["hits","degraded","truncated"],"properties":{"hits":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["document_id","document_version","title","media_type","snippet","locator"],"properties":{"document_id":{"type":"string","format":"uuid"},"document_version":{"type":"string","pattern":"^[1-9][0-9]*$"},"title":{"type":"string"},"media_type":{"type":"string"},"snippet":{"type":"string"},"locator":{"type":"object"}}}},"degraded":{"type":"boolean"},"truncated":{"type":"boolean"}}}`)
	case "read-doc":
		definition.Description = "Read the exact current knowledge document version cited by query-doc. byte_offset and max_bytes continue a truncated text source. A changed or deleted source must be searched again."
		definition.InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["document_id","document_version"],"properties":{"document_id":{"type":"string","format":"uuid"},"document_version":{"type":"string","pattern":"^[1-9][0-9]*$"},"byte_offset":{"type":"string","pattern":"^[0-9]+$"},"max_bytes":{"type":"integer","minimum":1,"maximum":1048576}}}`)
		definition.OutputSchema = json.RawMessage(`{"type":"object","required":["document"],"properties":{"document":{"type":"object"},"text":{"type":"object","required":["text","next_byte_offset","truncated"],"properties":{"text":{"type":"string"},"next_byte_offset":{"type":"string"},"truncated":{"type":"boolean"}}},"file":{"type":"object"},"unavailable":{"type":"string"}}}`)
	}
	return tc.BuiltinRegistration{Definition: definition, Binding: tc.BuiltinBinding{HandlerID: b.handler, ContractRevision: 1}, ScopeResolverID: "knowledge.project.v1", RiskClassifierID: "knowledge.read.v1", Class: tc.CoreTool, Active: true}
}
func (b *backend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.service == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return b.service.RequireBuiltinBindingInTx(ctx, tx, store)
}
func (b *backend) invocation(ctx context.Context, v tr.BuiltinInvocation) (kc.Invocation, error) {
	if ctx == nil || b == nil || b.service == nil {
		return kc.Invocation{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if err := ctx.Err(); err != nil {
		return kc.Invocation{}, err
	}
	if v.Validate() != nil || v.Call.Binding != b.Registration().Binding {
		return kc.Invocation{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	attempt, err := f.ParseID[kc.CallAttempt](v.AttemptID.String())
	if err != nil {
		return kc.Invocation{}, err
	}
	return kc.NewInvocation(kc.InvocationDetails{Actor: v.Call.Actor, ProjectID: v.Call.ProjectID, ExecutionID: v.Call.ExecutionID.String(), OperationID: v.OperationID.String(), ToolID: v.Call.Spec.ToolID.String(), ToolCallID: v.Call.Input.PayloadID, AttemptID: attempt})
}
func (b *backend) Execute(ctx context.Context, v tr.BuiltinInvocation, raw []byte) ([]byte, error) {
	invocation, err := b.invocation(ctx, v)
	if err != nil {
		return nil, err
	}
	return b.service.ExecuteTool(ctx, invocation, b.name, raw)
}
func (b *backend) Lookup(ctx context.Context, v tr.BuiltinInvocation, _ []byte) ([]byte, bool, error) {
	_, err := b.invocation(ctx, v)
	// The Tool owner reads its own durable result. Recovery never reissues reads.
	return nil, false, err
}

var _ tr.BuiltinBackend = (*backend)(nil)
