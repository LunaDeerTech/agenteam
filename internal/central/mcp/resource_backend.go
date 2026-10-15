package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// These aliases preserve the domain API while the shared contract remains the
// sole owner of runtime/capture connection and invocation shapes.
type RuntimeConnectionBinding = tc.MCPResourceConnection
type ResourceInvocation = tc.MCPResourceInvocation
type ResourceInvocationPlan = tc.MCPResourceInvocationPlan
type ResourceInvocationAuthority = tc.MCPResourceInvocationAuthority

func resourceBinding(b tc.MCPBinding) RuntimeConnectionBinding {
	return RuntimeConnectionBinding{ProjectID: b.ProjectID, ConfigID: b.ConfigID, ConnectionID: b.ConnectionID, RuntimeBindingID: b.RuntimeBindingID, ConfigRevision: b.ConfigRevision}
}

type ResourceMaterializer interface {
	MaterializeMCPResource(context.Context, ResourceInvocation, ResourceInvocationPlan, RuntimeConnectionBinding, int, protocol.ResourceContent) (mc.ResultPart, error)
}
type ResourceBackend struct {
	direct    *Service
	binding   *Authority
	authority ResourceInvocationAuthority
	media     ResourceMaterializer
	mu        sync.Mutex
	claimed   map[string]struct{}
}

func NewResourceBackendWithAuthority(binding *Authority, authority ResourceInvocationAuthority, media ResourceMaterializer) (*ResourceBackend, error) {
	if binding == nil || nilPort(binding.store) || nilPort(authority) || nilPort(media) {
		return nil, fail(f.DependencyUnbound)
	}
	return &ResourceBackend{binding: binding, authority: authority, media: media, claimed: map[string]struct{}{}}, nil
}
func (b *ResourceBackend) service() *Service {
	if b == nil {
		return nil
	}
	if b.binding != nil {
		s, _ := b.binding.service()
		return s
	}
	return b.direct
}

func NewResourceBackend(service *Service, authority ResourceInvocationAuthority, media ResourceMaterializer) (*ResourceBackend, error) {
	if service == nil || nilPort(authority) || nilPort(media) {
		return nil, fail(f.DependencyUnbound)
	}
	return &ResourceBackend{direct: service, authority: authority, media: media, claimed: map[string]struct{}{}}, nil
}
func (b *ResourceBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.service() == nil {
		return fail(f.DependencyUnbound)
	}
	return b.service().RequireBindingInTx(ctx, tx, store)
}

type resourceArguments struct {
	ConnectionID string `json:"connection_id,omitempty"`
	URI          string `json:"uri,omitempty"`
}

func parseResourceArguments(v ResourceInvocation, raw []byte) (resourceArguments, error) {
	var args resourceArguments
	if v.Validate() != nil || len(raw) > 1<<20 || f.Digest(digest(raw)) != v.Call.Input.SHA256 || f.Progress(len(raw)) != v.Call.Input.Bytes {
		return args, fail(f.InvalidArgument)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || len(canonical) == 0 || canonical[0] != '{' || f.Digest(digest(canonical)) != v.Call.CanonicalArguments {
		return args, fail(f.InvalidArgument)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&args) != nil {
		return args, fail(f.InvalidArgument)
	}
	if args.ConnectionID != "" {
		if _, err = f.ParseID[struct{}](args.ConnectionID); err != nil {
			return args, fail(f.InvalidArgument)
		}
	}
	if v.Call.Binding.HandlerID == "mcp.list-resources" {
		if args.URI != "" {
			return args, fail(f.InvalidArgument)
		}
	} else if args.ConnectionID == "" || len(args.URI) == 0 || len(args.URI) > 8192 {
		return args, fail(f.InvalidArgument)
	}
	return args, nil
}
func resourceFailure(v ResourceInvocation, err error) (tc.MCPBackendResult, error) {
	code := safeProtocolCode(err)
	return tc.MCPBackendResult{OutcomeKnown: true, Result: mc.ToolResultPart{CallID: v.Call.CallID, IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: code}}}}, FailureCode: code}, nil
}
func (b *ResourceBackend) Execute(ctx context.Context, v ResourceInvocation, raw []byte) (tc.MCPBackendResult, error) {
	if b == nil || b.service() == nil || ctx == nil {
		return tc.MCPBackendResult{}, fail(f.InvalidArgument)
	}
	args, err := parseResourceArguments(v, raw)
	if err != nil {
		return tc.MCPBackendResult{}, err
	}
	plan, err := b.authority.DiscoverMCPResourceInvocation(ctx, v)
	if err != nil {
		return tc.MCPBackendResult{}, safeError(err)
	}
	if nilPort(plan) {
		return tc.MCPBackendResult{}, fail(f.DependencyUnbound)
	}
	connections := slices.Clone(plan.RuntimeConnections())
	if len(connections) > 64 {
		return tc.MCPBackendResult{}, fail(f.PayloadTooLarge)
	}
	seen := map[string]bool{}
	for _, c := range connections {
		if c.Validate() != nil || c.ProjectID != v.Call.ProjectID || seen[c.ConnectionID] {
			return tc.MCPBackendResult{}, fail(f.Forbidden)
		}
		seen[c.ConnectionID] = true
	}
	if args.ConnectionID != "" && !seen[args.ConnectionID] {
		return tc.MCPBackendResult{}, fail(f.Forbidden)
	}
	claim := v.AttemptID.String() + ":" + v.RequestID
	b.mu.Lock()
	if _, ok := b.claimed[claim]; ok || len(b.claimed) >= 8192 {
		b.mu.Unlock()
		return tc.MCPBackendResult{}, fail(f.ResourceBusy)
	}
	b.claimed[claim] = struct{}{}
	b.mu.Unlock()
	context.AfterFunc(ctx, func() { b.mu.Lock(); delete(b.claimed, claim); b.mu.Unlock() })
	result := tc.MCPBackendResult{OutcomeKnown: true, Result: mc.ToolResultPart{CallID: v.Call.CallID}}
	var catalog []struct {
		ConnectionID string              `json:"connection_id"`
		Resources    []protocol.Resource `json:"resources"`
		Templates    []protocol.Template `json:"templates"`
	}
	for _, connection := range connections {
		if args.ConnectionID != "" && connection.ConnectionID != args.ConnectionID {
			continue
		}
		snapshot, err := b.snapshot(ctx, v, plan, connection)
		if err != nil {
			return resourceFailure(v, err)
		}
		key, err := ac.NewAppendKey(ac.AccessProducer, v.RequestID, 0)
		if err != nil {
			return resourceFailure(v, err)
		}
		scope, _ := id.InProject(v.Call.ProjectID)
		call, err := outbound.NewCallContext(v.Call.Actor, scope, key, ac.Associations{ExecutionID: v.Call.ExecutionID.String(), OperationID: v.OperationID.String(), ToolID: v.Call.Spec.ToolID.String(), RequestID: v.RequestID})
		if err != nil {
			return resourceFailure(v, err)
		}
		access := protocol.Access{Endpoint: snapshot.endpoint, Authentication: snapshot.auth, Context: call}
		if snapshot.auth.Scheme != "none" {
			access.Material, err = b.service().readResourceCredential(ctx, b, v, plan, snapshot)
			if err != nil {
				return resourceFailure(v, err)
			}
		}
		session, err := b.service().options.Protocol.Open(ctx, access)
		if err != nil {
			return resourceFailure(v, err)
		}
		defer session.Close()
		if session.Revision() != snapshot.revision {
			_ = session.Close()
			return resourceFailure(v, &protocol.Error{Code: protocol.ProtocolUnsupported})
		}
		if v.Call.Binding.HandlerID == "mcp.list-resources" {
			resources, err := session.ListResources(ctx)
			var templates []protocol.Template
			if err == nil {
				templates, err = session.ListResourceTemplates(ctx)
			}
			closeErr := session.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return resourceFailure(v, err)
			}
			catalog = append(catalog, struct {
				ConnectionID string              `json:"connection_id"`
				Resources    []protocol.Resource `json:"resources"`
				Templates    []protocol.Template `json:"templates"`
			}{connection.ConnectionID, resources, templates})
		} else {
			contents, err := session.ReadResource(ctx, args.URI)
			closeErr := session.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return resourceFailure(v, err)
			}
			for ordinal, content := range contents {
				part, err := b.media.MaterializeMCPResource(ctx, v, plan, connection, ordinal, content)
				if err != nil {
					return resourceFailure(v, err)
				}
				if part.Validate() != nil {
					return resourceFailure(v, &protocol.Error{Code: protocol.InvalidResponse})
				}
				result.Result.Parts = append(result.Result.Parts, part)
			}
		}
	}
	if v.Call.Binding.HandlerID == "mcp.list-resources" {
		raw, err := json.Marshal(struct {
			Connections any `json:"connections"`
		}{catalog})
		if err != nil || len(raw) > 1<<20 {
			return resourceFailure(v, fail(f.PayloadTooLarge))
		}
		result.Result.Parts = []mc.ResultPart{{Text: &mc.TextPart{Text: string(raw)}}}
	}
	if result.Result.Validate() != nil {
		return resourceFailure(v, &protocol.Error{Code: protocol.InvalidResponse})
	}
	return result, nil
}
func (b *ResourceBackend) snapshot(ctx context.Context, v ResourceInvocation, plan ResourceInvocationPlan, connection RuntimeConnectionBinding) (runtimeSnapshot, error) {
	locks, err := mergeLocks(plan.RequiredLocks(), []f.LockRequest{registryLock(f.Shared)})
	if err != nil {
		return runtimeSnapshot{}, err
	}
	cause, err := f.NewRecoveryCause("mcp.resource", v.RequestID, "")
	if err != nil {
		return runtimeSnapshot{}, err
	}
	var snapshot runtimeSnapshot
	err = commit(b.service().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := b.service().store.AcquireAll(ctx, tx, locks); err != nil {
			return safeError(err)
		}
		if err := b.authority.RequireMCPResourceInvocationInTx(ctx, tx, v, plan); err != nil {
			return safeError(err)
		}
		x, err := b.service().store.InTx(tx)
		if err != nil {
			return safeError(err)
		}
		snapshot, err = loadConnectionSnapshot(ctx, x, v.Call.ExecutionID, connection)
		return err
	}))
	return snapshot, err
}
