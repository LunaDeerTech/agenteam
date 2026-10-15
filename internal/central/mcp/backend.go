package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

// MediaMaterializer retains canonical MCP copies through Object. It never
// downloads a resource URI. The backend supplies its original admitted call.
type MediaMaterializer interface {
	MaterializeMCPContent(context.Context, tc.MCPInvocation, int, protocol.Content) (mc.ResultPart, error)
}
type Backend struct {
	service   *Service
	authority tc.MCPInvocationAuthority
	media     MediaMaterializer
	mu        sync.Mutex
	claimed   map[string]struct{}
}

func NewBackend(service *Service, authority tc.MCPInvocationAuthority, media MediaMaterializer) (*Backend, error) {
	if service == nil || nilPort(authority) {
		return nil, fail(f.DependencyUnbound)
	}
	return &Backend{service: service, authority: authority, media: media, claimed: map[string]struct{}{}}, nil
}
func (b *Backend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if b == nil || b.service == nil {
		return fail(f.DependencyUnbound)
	}
	return b.service.RequireBindingInTx(ctx, tx, store)
}

type runtimeSnapshot struct {
	binding                     RuntimeConnectionBinding
	endpoint                    string
	auth                        protocol.Authentication
	credential, lease, revision string
}

func loadRuntimeSnapshot(ctx context.Context, x postgres.SQLExecutor, v tc.MCPInvocation) (runtimeSnapshot, error) {
	return loadConnectionSnapshot(ctx, x, v.Call.ExecutionID, resourceBinding(v.Call.MCPBinding))
}
func loadConnectionSnapshot(ctx context.Context, x postgres.SQLExecutor, execution id.ExecutionID, b RuntimeConnectionBinding) (runtimeSnapshot, error) {
	s := runtimeSnapshot{binding: b}
	err := x.QueryRow(ctx, `SELECT r.endpoint,r.auth_scheme,r.header_name,COALESCE(r.credential_id,''),COALESCE(l.credential_lease_id,''),r.protocol_revision FROM agenteam_mcp.runtime_bindings r JOIN agenteam_mcp.runtime_leases l ON l.runtime_binding_id=r.id AND l.project_id=r.project_id AND l.connection_id=r.connection_id WHERE r.id=$1 AND r.project_id=$2 AND r.config_id=$3 AND r.connection_id=$4 AND r.config_revision=$5 AND l.execution_id=$6 AND NOT l.retired`, b.RuntimeBindingID, b.ProjectID.String(), b.ConfigID, b.ConnectionID, int64(b.ConfigRevision), execution.String()).Scan(&s.endpoint, &s.auth.Scheme, &s.auth.HeaderName, &s.credential, &s.lease, &s.revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, fail(f.Forbidden)
	}
	if err != nil {
		return s, safeError(err)
	}
	if s.auth.Validate() != nil || s.auth.Scheme == "none" && (s.credential != "" || s.lease != "") || s.auth.Scheme != "none" && (s.credential == "" || s.lease == "") {
		return s, fail(f.InvalidState)
	}
	return s, nil
}
func mergeLocks(groups ...[]f.LockRequest) ([]f.LockRequest, error) {
	byKey := map[string]f.LockRequest{}
	for _, group := range groups {
		for _, r := range group {
			if r.Key.Validate() != nil || !r.Mode.Valid() {
				return nil, fail(f.InvalidArgument)
			}
			key := r.Key.Canonical()
			if byKey[key].Mode != f.Exclusive {
				byKey[key] = r
			}
		}
	}
	out := make([]f.LockRequest, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, r)
	}
	return sortedLocks(out), nil
}
func (b *Backend) claim(ctx context.Context, v tc.MCPInvocation) error {
	key := v.AttemptID.String() + ":" + v.RequestID
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.claimed[key]; exists {
		return fail(f.InvalidState)
	}
	if len(b.claimed) >= 8192 {
		return fail(f.ResourceBusy)
	}
	b.claimed[key] = struct{}{}
	// The original dispatch context owns this marker. A completed request cannot
	// be replayed while that same original authority witness is still live.
	context.AfterFunc(ctx, func() { b.mu.Lock(); delete(b.claimed, key); b.mu.Unlock() })
	return nil
}
func (b *Backend) authorize(ctx context.Context, v tc.MCPInvocation, plan tc.MCPInvocationPlan) (runtimeSnapshot, error) {
	key, err := f.AggregateLock(f.ExecutionAggregate, v.Call.ExecutionID.String())
	if err != nil {
		return runtimeSnapshot{}, err
	}
	locks, err := mergeLocks(plan.RequiredLocks(), []f.LockRequest{registryLock(f.Shared), {Key: key, Mode: f.Shared}})
	if err != nil {
		return runtimeSnapshot{}, err
	}
	cause, err := f.NewRecoveryCause("mcp.invoke", v.RequestID, "")
	if err != nil {
		return runtimeSnapshot{}, err
	}
	var snapshot runtimeSnapshot
	err = commit(b.service.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := b.service.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := b.authority.RequireMCPInvocationInTx(ctx, tx, v, plan); err != nil {
			return safeError(err)
		}
		x, err := b.service.store.InTx(tx)
		if err != nil {
			return err
		}
		snapshot, err = loadRuntimeSnapshot(ctx, x, v)
		return err
	}))
	return snapshot, err
}
func backendError(v tc.MCPInvocation, err error, mayHaveExecuted bool) (tc.MCPBackendResult, error) {
	code := safeProtocolCode(err)
	var p *protocol.Error
	if errors.As(err, &p) {
		mayHaveExecuted = mayHaveExecuted || p.MayHaveExecuted
	}
	out := tc.MCPBackendResult{Result: mc.ToolResultPart{CallID: v.Call.CallID, IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: code}}}}, OutcomeKnown: !mayHaveExecuted, MayHaveExecuted: mayHaveExecuted, FailureCode: code}
	return out, nil
}
func (b *Backend) Execute(ctx context.Context, v tc.MCPInvocation, arguments []byte) (tc.MCPBackendResult, error) {
	if b == nil || b.service == nil || ctx == nil || v.Validate() != nil || len(arguments) > 1<<20 {
		return tc.MCPBackendResult{}, fail(f.InvalidArgument)
	}
	canonical, err := cursor.CanonicalJSON(arguments)
	if err != nil || f.Digest(digest(canonical)) != v.Call.CanonicalArguments {
		return tc.MCPBackendResult{}, fail(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return backendError(v, err, false)
	}
	plan, err := b.authority.DiscoverMCPInvocation(ctx, v)
	if err != nil {
		return tc.MCPBackendResult{}, safeError(err)
	}
	if nilPort(plan) {
		return tc.MCPBackendResult{}, fail(f.DependencyUnbound)
	}
	if err = b.claim(ctx, v); err != nil {
		return tc.MCPBackendResult{}, err
	}
	snapshot, err := b.authorize(ctx, v, plan)
	if err != nil {
		return tc.MCPBackendResult{}, err
	}
	key, err := ac.NewAppendKey(ac.AccessProducer, v.RequestID, 0)
	if err != nil {
		return tc.MCPBackendResult{}, err
	}
	scope, _ := id.InProject(v.Call.ProjectID)
	call, err := outbound.NewCallContext(v.Call.Actor, scope, key, ac.Associations{ExecutionID: v.Call.ExecutionID.String(), OperationID: v.OperationID.String(), ToolID: v.Call.Spec.ToolID.String(), RequestID: v.RequestID})
	if err != nil {
		return tc.MCPBackendResult{}, err
	}
	access := protocol.Access{Endpoint: snapshot.endpoint, Authentication: snapshot.auth, Context: call}
	if snapshot.auth.Scheme != "none" {
		access.Material, err = b.readRuntimeCredential(ctx, v, plan, snapshot)
		if err != nil {
			return backendError(v, err, false)
		}
	}
	session, err := b.service.options.Protocol.Open(ctx, access)
	if err != nil {
		return backendError(v, err, false)
	}
	defer session.Close()
	if session.Revision() != snapshot.revision {
		_ = session.Close()
		return backendError(v, &protocol.Error{Code: protocol.ProtocolUnsupported}, false)
	}
	result, callErr := session.CallTool(ctx, v.Call.MCPBinding.RemoteToolName, arguments)
	if callErr != nil {
		return backendError(v, callErr, false)
	}
	// A remote output schema describes structuredContent, never the complete
	// protocol envelope. Validation still requires the live original dispatch.
	if !result.IsError {
		validator, ok := b.authority.(interface {
			ValidateMCPOutput(context.Context, tc.MCPInvocation, json.RawMessage) error
		})
		if !ok {
			return backendKnownFailure(v, fail(f.DependencyUnbound))
		}
		if err = validator.ValidateMCPOutput(ctx, v, result.StructuredContent); err != nil {
			return backendKnownFailure(v, &protocol.Error{Code: protocol.InvalidResponse})
		}
	}
	maximum := 256
	if len(result.StructuredContent) > 0 {
		maximum--
	}
	contents, expansionErr := expandResourceLinks(ctx, session, func(ctx context.Context) error {
		_, err := b.authorize(ctx, v, plan)
		return err
	}, result.Content, maximum)
	// The same original SDK session owns any ResourceLink reads and its actual
	// Close. A received Tool result remains known even if local adaptation or
	// teardown fails; neither failure authorizes another tools/call.
	_ = session.Close()
	if expansionErr != nil {
		return backendKnownFailure(v, expansionErr)
	}
	output := tc.MCPBackendResult{Result: mc.ToolResultPart{CallID: v.Call.CallID, IsError: result.IsError}, OutcomeKnown: true, MayHaveExecuted: true}
	for index, c := range contents {
		var part mc.ResultPart
		switch c.Kind {
		case "text":
			part.Text = &mc.TextPart{Text: c.Text}
		default:
			if nilPort(b.media) {
				return backendKnownFailure(v, fail(f.DependencyUnbound))
			}
			part, err = b.media.MaterializeMCPContent(ctx, v, index, c)
			if err != nil {
				return backendKnownFailure(v, err)
			}
		}
		if part.Validate() != nil {
			return backendKnownFailure(v, &protocol.Error{Code: protocol.InvalidResponse})
		}
		output.Result.Parts = append(output.Result.Parts, part)
	}
	if len(result.StructuredContent) > 0 {
		output.Result.Parts = append(output.Result.Parts, mc.ResultPart{Text: &mc.TextPart{Text: string(result.StructuredContent)}})
	}
	if output.Result.Validate() != nil {
		return backendKnownFailure(v, &protocol.Error{Code: protocol.InvalidResponse})
	}
	return output, nil
}

// A received result establishes the remote outcome even if local validation
// or content retention fails. Such a result is an error, never a retry permit.
func backendKnownFailure(v tc.MCPInvocation, err error) (tc.MCPBackendResult, error) {
	out, _ := backendError(v, err, true)
	out.OutcomeKnown = true
	return out, nil
}

// Authenticated calls use the retained Execution lease and the same original
// invocation authority again in Secret's read transaction.
func (b *Backend) readRuntimeCredential(ctx context.Context, v tc.MCPInvocation, plan tc.MCPInvocationPlan, snapshot runtimeSnapshot) (sc.SecretMaterial, error) {
	return b.service.readExecutionCredential(ctx, b, v, plan, snapshot)
}

var _ tc.MCPBackend = (*Backend)(nil)
