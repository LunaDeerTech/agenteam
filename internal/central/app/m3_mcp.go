package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	mcphttp "github.com/LunaDeerTech/agenteam/internal/central/mcp/http"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

func createMCPHTTP(service *mcp.Service, secrets *secret.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	remote, err := mcphttp.NewHTTPHandler(service, boundary)
	if err != nil {
		return nil, err
	}
	credentials, err := mcphttp.NewCredentialHTTPHandler(secrets, boundary)
	if err != nil {
		return nil, err
	}
	oauth, err := mcphttp.NewOAuthHTTPHandler(service, boundary)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mcphttp.HandlesOAuthPath(r.URL.Path) {
			oauth.ServeHTTP(w, r)
			return
		}
		if mcphttp.HandlesCredentialPath(r.URL.Path) {
			credentials.ServeHTTP(w, r)
			return
		}
		remote.ServeHTTP(w, r)
	}), nil
}

func mcpRoutes(previous, remote http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mcphttp.HandlesPath(r.URL.Path) || mcphttp.HandlesCredentialPath(r.URL.Path) || mcphttp.HandlesOAuthPath(r.URL.Path) {
			remote.ServeHTTP(w, r)
			return
		}
		previous.ServeHTTP(w, r)
	})
}

// This construction record never grants access. The domain Authority binds its
// same-Store service once and every invocation still needs its original owner.
type mcpAssembly struct {
	authority        *mcp.Authority
	invocations      *tr.MCPAuthority
	contentAuthority *mcp.ContentAuthority
	content          *mcp.ContentStore
	resources        *mcp.ResourceBackend
	backend          *mcp.Backend
}

func constructMCPContent(db database, binding *mcp.Authority, tools *tr.BuiltinAuthority, reads mcp.ContentReadAuthority) (*mcpAssembly, error) {
	if binding == nil || tools == nil || runtimeInformationNil(reads) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	a := &mcpAssembly{authority: binding}
	var err error
	a.invocations, err = tr.NewMCPAuthority(tools)
	if err != nil {
		return nil, err
	}
	// Detached reads borrow the original Execution Model call and exact input.
	// The same authority owns its reader-release tail after cancellation.
	store, ok := db.(object.Store)
	if !ok {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	maintenance, err := object.NewMaintenanceOwnerResolver(store)
	if err != nil {
		return nil, err
	}
	uploads, err := object.NewMCPUploadResolver(store)
	if err != nil {
		return nil, err
	}
	a.contentAuthority, err = mcp.NewContentAuthorityWithRecovery(binding, a.invocations, reads, maintenance, uploads, tools)
	return a, err
}

func (a *mcpAssembly) resourceBackends(objects oc.Objects, cleanup mcp.ContentRecoveryObjects, tools *tr.BuiltinAuthority) ([]tr.BuiltinBackend, error) {
	if a == nil || runtimeInformationNil(objects) || runtimeInformationNil(cleanup) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	var err error
	a.content, err = mcp.NewContentStoreWithRecovery(a.contentAuthority, objects, cleanup)
	if err != nil {
		return nil, err
	}
	a.resources, err = mcp.NewResourceBackendWithAuthority(a.authority, tools, a.content)
	if err != nil {
		return nil, err
	}
	var out []tr.BuiltinBackend
	for _, r := range mcpResourceRegistrations() {
		backend, e := tr.NewMCPResourceBuiltin(r, a.resources)
		if e != nil {
			return nil, e
		}
		out = append(out, backend)
	}
	return out, nil
}

func mcpResourceRegistrations() []tc.BuiltinRegistration {
	list := tc.BuiltinRegistration{Definition: tc.Definition{StableKey: "builtin:list-mcp-resources", Name: "list-mcp-resources", Description: "List resources available through captured MCP connections.", InputSchema: json.RawMessage(`{"type":"object","properties":{"connection_id":{"type":"string","format":"uuid"}},"additionalProperties":false}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &tc.Annotations{ReadOnly: true, Idempotency: tc.Inherent}}, Binding: tc.BuiltinBinding{HandlerID: "mcp.list-resources", ContractRevision: 1}, ScopeResolverID: "mcp.resources.project.v1", RiskClassifierID: "mcp.resources.read.v1", Class: tc.CoreTool, Active: true}
	read := list
	read.Definition = tc.Definition{StableKey: "builtin:read-mcp-resource", Name: "read-mcp-resource", Description: "Read a resource URI from its captured MCP connection.", InputSchema: json.RawMessage(`{"type":"object","properties":{"connection_id":{"type":"string","format":"uuid"},"uri":{"type":"string","minLength":1,"maxLength":8192}},"required":["connection_id","uri"],"additionalProperties":false}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &tc.Annotations{ReadOnly: true, Idempotency: tc.Inherent}}
	read.Binding.HandlerID = "mcp.read-resource"
	return []tc.BuiltinRegistration{list, read}
}

func mcpOAuthRedirect(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.String() != origin {
		return "", f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	u.Path = "/mcp/oauth/callback"
	return u.String(), nil
}

func constructMCPControl(db database, binding *mcp.Authority, accounts *account.Authority, projects *project.Authority, registered *registry.Registry, capture mcp.CaptureAuthority, secrets *secret.Service, auditor *audit.Service, outbound *outboundRuntime, publicOrigin string, process *mcp.ProcessOptions) (*mcp.Service, error) {
	store, ok := db.(mcp.Store)
	if !ok || binding == nil || registered == nil || outbound == nil || process == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	protocol, err := protocol.New(outbound.client)
	if err != nil {
		return nil, err
	}
	audit, err := mcp.NewAuditAdapter(auditor)
	if err != nil {
		return nil, err
	}
	redirect, err := mcpOAuthRedirect(publicOrigin)
	if err != nil {
		return nil, err
	}
	oauth, err := secret.NewMCPOAuthWrites(secrets, binding)
	if err != nil {
		return nil, err
	}
	// This fixed writer/redirect does not register OAuth HTTP entry points. The
	// domain's complete callback and original request lifecycle own that route.
	return mcp.New(store, mcp.Options{Authority: binding, Sessions: accounts, System: accounts, Projects: projects, Audit: audit, Registry: registered, Protocol: protocol, Capture: capture, Secret: secrets, SecretReader: secrets, Process: process, OAuth: &mcp.OAuthOptions{Writes: oauth, RedirectURI: redirect}})
}

func constructMCP(db database, b *executionRuntimeAssembly, a *mcpAssembly, accounts *account.Authority, projects *project.Authority, execution *executionRuntimeAuthorities, registered *registry.Registry, secrets *secret.Service, auditor *audit.Service, outbound *outboundRuntime, publicOrigin string, process *mcp.ProcessOptions) error {
	if b == nil || a == nil || execution == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	capture, err := registry.NewExecutionCapture(registered, execution.execution)
	if err != nil {
		return err
	}
	b.mcp, err = constructMCPControl(db, a.authority, accounts, projects, registered, capture, secrets, auditor, outbound, publicOrigin, process)
	if err != nil {
		return err
	}
	a.backend, err = mcp.NewBackend(b.mcp, a.invocations, a.content)
	if err != nil {
		return err
	}
	b.mcpRetirement, err = mcp.NewExecutionRetirement(b.mcp, execution.execution)
	if err != nil {
		return err
	}
	b.mcpTools = a
	b.mcpCapture = capture
	return nil
}

// Purpose selects planned uses. Lease grants additionally require the exact
// active private MCP witness; errors never fall back to another authority.
type m3SecretUsage struct {
	model *model.SecretUsageRouter
	mcp   *mcp.Authority
}

func (r m3SecretUsage) CheckReferenceInTx(ctx context.Context, tx f.Tx, a id.Actor, ref sc.CredentialRef, p sc.Purpose, owner string, retain bool) error {
	if p == sc.MCP {
		return r.mcp.CheckReferenceInTx(ctx, tx, a, ref, p, owner, retain)
	}
	return r.model.CheckReferenceInTx(ctx, tx, a, ref, p, owner, retain)
}
func (r m3SecretUsage) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, a id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if r.mcp.OwnsUsageContext(ctx) {
		return r.mcp.AuthorizeLeaseInTx(ctx, tx, a, ref, owner, action)
	}
	return r.model.AuthorizeLeaseInTx(ctx, tx, a, ref, owner, action)
}
func (r m3SecretUsage) DiscoverUsage(ctx context.Context, q sc.UsageRequest) (sc.UsageDependencies, error) {
	if q.Purpose == sc.MCP {
		return r.mcp.DiscoverUsage(ctx, q)
	}
	return r.model.DiscoverUsage(ctx, q)
}
func (r m3SecretUsage) ValidateUsageInTx(ctx context.Context, tx f.Tx, q sc.UsageRequest, p sc.UsageDependencies) error {
	if q.Purpose == sc.MCP {
		return r.mcp.ValidateUsageInTx(ctx, tx, q, p)
	}
	return r.model.ValidateUsageInTx(ctx, tx, q, p)
}

// These finite recovery workers outlive initialize's security/startup context.
// Each actual MCP entry point admits its work through the same processOwner;
// accountAssembly StopAdmission/Drain still cancels and joins those original
// calls before their providers are released, including partial startup failure.
func startMCPRecovery(ctx context.Context, content, reconcile func(context.Context) error) error {
	if ctx == nil || reconcile == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owned := context.WithoutCancel(ctx)
	if content != nil {
		if err := content(owned); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := reconcile(owned); err != nil {
		return err
	}
	return ctx.Err()
}
