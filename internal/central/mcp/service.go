// Package mcp owns MCP configuration, Project connections and capability
// synchronization. Tool operations and authorization remain with their owners.
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type ConfigIdentity struct{}
type ConnectionIdentity struct{}
type ConfigID = f.ID[ConfigIdentity]
type ConnectionID = f.ID[ConnectionIdentity]

type Config struct {
	ID              ConfigID                `json:"id"`
	ScopeKind       string                  `json:"scope"`
	ProjectID       string                  `json:"project_id,omitempty"`
	Name            string                  `json:"name"`
	Description     string                  `json:"description"`
	Endpoint        string                  `json:"endpoint"`
	Authentication  protocol.Authentication `json:"authentication"`
	Version         f.Version               `json:"version"`
	RuntimeRevision f.Version               `json:"runtime_revision"`
}
type Connection struct {
	ID                ConnectionID `json:"id"`
	ProjectID         id.ProjectID `json:"project_id"`
	ConfigID          ConfigID     `json:"config_id"`
	Version           f.Version    `json:"version"`
	ConfigRevision    f.Version    `json:"config_revision"`
	State             string       `json:"state"`
	Enabled           bool         `json:"enabled"`
	CredentialID      string       `json:"credential_id,omitempty"`
	RuntimeBindingID  string       `json:"runtime_binding_id,omitempty"`
	ProtocolRevision  string       `json:"protocol_revision,omitempty"`
	ToolsState        string       `json:"tools_state"`
	ResourcesState    string       `json:"resources_state"`
	ToolsRevision     f.Progress   `json:"tools_revision"`
	ResourcesRevision f.Progress   `json:"resources_revision"`
	LastErrorCode     string       `json:"last_error_code,omitempty"`
}
type Command struct {
	Actor           id.Actor
	Scope           id.Scope
	Identity        f.CommandIdentity
	ExpectedVersion f.Version
}
type ConfigInput struct {
	Name, Description, Endpoint string
	Authentication              protocol.Authentication
}
type CommandResult struct {
	Config     *Config             `json:"config,omitempty"`
	Connection *Connection         `json:"connection,omitempty"`
	Deleted    bool                `json:"deleted,omitempty"`
	OAuth      *OAuthAuthorization `json:"oauth,omitempty"`
}

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}
type ProjectAuthority interface {
	AuthorizeProject(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error)
}
type AuditEvent struct {
	Actor                                  id.Actor
	Scope                                  id.Scope
	Action, ConfigID, ConnectionID         string
	Version                                f.Version
	ToolCount, ResourceCount, InvalidCount int
	ErrorCode, CauseID                     string
}
type AuditAppender interface {
	AppendMCPInTx(context.Context, f.Tx, AuditEvent) error
}
type Registry interface {
	ReconcileMCPInTx(context.Context, f.Tx, tc.MCPDiscovery) ([]tc.SpecRef, error)
	RemoveMCPConnectionInTx(context.Context, f.Tx, id.ProjectID, string) error
}

// CredentialRequest identifies the original domain-owned use. A provider must
// consume the Service's current authorization proof; this DTO alone is no grant.
type CredentialRequest struct {
	Actor            id.Actor
	ProjectID        id.ProjectID
	ConnectionID     ConnectionID
	RuntimeBindingID string
	ConfigRevision   f.Version
	CredentialRef    sc.CredentialRef
	RequestID        string
	ExecutionID      id.ExecutionID
	OperationID      tc.OperationID
	AttemptID        tc.AttemptID
}
type CredentialProvider interface {
	ResolveMCP(context.Context, CredentialRequest) (sc.SecretMaterial, error)
}
type CaptureAuthority interface {
	RequireMCPCaptureInTx(context.Context, f.Tx, id.ExecutionID, tc.MCPBinding) error
	RequireMCPRetentionDiscoveryInTx(context.Context, f.Tx, tc.ExecutionToolCaptureRequest, tc.MCPBinding) error
}
type Options struct {
	Process      *ProcessOptions
	Authority    *Authority
	Sessions     id.SessionAuthority
	System       id.SystemAuthority
	Projects     ProjectAuthority
	Audit        AuditAppender
	Registry     Registry
	Protocol     *protocol.Runtime
	Credentials  CredentialProvider
	Capture      CaptureAuthority
	Secret       sc.UsageOperations
	SecretReader sc.MCPCredentialUsageReader
	OAuth        *OAuthOptions
}
type Service struct {
	owner        *processOwner
	store        Store
	options      Options
	secretIssuer sc.PlanIssuer
}

func New(store Store, options Options) (*Service, error) {
	if nilPort(store) || nilPort(options.Sessions) || nilPort(options.System) || nilPort(options.Projects) || nilPort(options.Audit) || options.Protocol == nil {
		return nil, fail(f.DependencyUnbound)
	}
	s := &Service{store: store, options: options, secretIssuer: sc.NewPlanIssuer()}
	if options.Process != nil {
		owner, err := newProcessOwner(store, *options.Process)
		if err != nil {
			return nil, err
		}
		s.owner = owner
	}
	if options.Authority != nil {
		if err := options.Authority.bind(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}
func fail(code f.Code) error { return f.NewFault(code, f.NotStarted) }
func safeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var fault *f.Fault
	if errors.As(err, &fault) {
		return f.NewFault(fault.Code.Safe(), fault.CommitState.Safe())
	}
	return fail(f.DependencyUnavailable)
}
func digest(b []byte) string { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func catalogLock(mode f.LockMode) f.LockRequest {
	k, _ := f.SystemConfigLock("mcp-catalog")
	return f.LockRequest{Key: k, Mode: mode}
}
func registryLock(mode f.LockMode) f.LockRequest {
	k, _ := f.SystemConfigLock("tool-registry")
	return f.LockRequest{Key: k, Mode: mode}
}
func connectionLock(connection string, mode f.LockMode) (f.LockRequest, error) {
	k, e := f.AggregateLock(f.MCPConnectionAggregate, connection)
	return f.LockRequest{Key: k, Mode: mode}, e
}
func text(v string, n int) bool {
	return len(v) <= n && utf8.ValidString(v) && !strings.ContainsRune(v, 0)
}
func (v ConfigInput) validate() error {
	if v.Name == "" || !text(v.Name, 256) || !text(v.Description, 8192) || v.Authentication.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	if _, err := outbound.ParseTarget(v.Endpoint); err != nil {
		return fail(f.InvalidArgument)
	}
	return nil
}
func project(scope id.Scope) id.ProjectID {
	p, _ := f.ParseID[id.Project](scope.Details().ProjectID)
	return p
}
func scopeValid(scope id.Scope) bool {
	return scope.Validate() == nil && (scope.Details().Kind == id.System || scope.Details().Kind == id.ProjectScope)
}
func CommandIdentity(actor id.Actor, scope id.Scope, name string, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if actor.Validate() != nil || actor.Details().Kind != id.Human || !scopeValid(scope) {
		return f.CommandIdentity{}, fail(f.Forbidden)
	}
	owners := []string{actor.Details().UserID}
	if scope.Details().Kind == id.ProjectScope {
		owners = append(owners, scope.Details().ProjectID)
	}
	return f.NewCommandIdentity("mcp", owners, name, key)
}
func (s *Service) authorize(ctx context.Context, tx f.Tx, actor id.Actor, scope id.Scope, intent id.AccessIntent) error {
	if s == nil || actor.Validate() != nil || actor.Details().Kind != id.Human || !scopeValid(scope) {
		return fail(f.Forbidden)
	}
	if err := s.options.Sessions.RequireCurrentSession(ctx, tx, actor); err != nil {
		return safeError(err)
	}
	var grant id.AccessGrant
	var err error
	if scope.Details().Kind == id.System {
		grant, err = s.options.System.AuthorizeSystem(ctx, tx, actor, intent)
	} else {
		grant, err = s.options.Projects.AuthorizeProject(ctx, tx, actor, project(scope), intent)
	}
	if err != nil {
		return safeError(err)
	}
	if !grant.Matches(actor, scope, intent) {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}
func (s *Service) locks(actor id.Actor, scope id.Scope, command *f.CommandIdentity, write bool) ([]f.LockRequest, error) {
	if actor.Validate() != nil || actor.Details().Kind != id.Human || !scopeValid(scope) {
		return nil, fail(f.Forbidden)
	}
	mode := f.Shared
	if write {
		mode = f.Exclusive
	}
	locks := []f.LockRequest{catalogLock(mode), registryLock(mode)}
	u, _ := f.UserLock(actor.Details().UserID)
	locks = append(locks, f.LockRequest{Key: u, Mode: f.Shared})
	if scope.Details().Kind == id.ProjectScope {
		p, _ := f.ProjectLock(scope.Details().ProjectID)
		locks = append(locks, f.LockRequest{Key: p, Mode: f.Shared})
	}
	if command != nil {
		k, err := f.CommandLock(*command)
		if err != nil {
			return nil, err
		}
		locks = append(locks, f.LockRequest{Key: k, Mode: f.Exclusive})
	}
	return locks, nil
}

type UnknownError struct{ result f.CommitResult }

func (e *UnknownError) Error() string                         { return string(f.CommitUnknown) }
func (e *UnknownError) AttemptID() f.ID[f.TransactionAttempt] { return e.result.AttemptID() }
func (e *UnknownError) Cause() f.TransactionCause             { return e.result.Cause() }
func (e UnknownError) Format(w fmt.State, _ rune)             { _, _ = io.WriteString(w, string(f.CommitUnknown)) }
func (e UnknownError) LogValue() slog.Value                   { return slog.StringValue(string(f.CommitUnknown)) }
func (e UnknownError) MarshalJSON() ([]byte, error)           { return []byte(`{"code":"COMMIT_UNKNOWN"}`), nil }
func commit(result f.CommitResult) error {
	switch result.State() {
	case f.Committed:
		return nil
	case f.Unknown:
		return &UnknownError{result}
	default:
		if result.Fault() == nil {
			return fail(f.InvalidState)
		}
		return safeError(result.Fault())
	}
}

func (s *Service) read(ctx context.Context, actor id.Actor, scope id.Scope, consume func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	locks, err := s.locks(actor, scope, nil, false)
	if err != nil {
		return err
	}
	run, err := f.NewID[struct{}]()
	if err != nil {
		return fail(f.DependencyUnavailable)
	}
	cause, _ := f.NewRecoveryCause("mcp.read", run.String(), "")
	return commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, actor, scope, id.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		return consume(ctx, tx, x)
	}))
}
func validateCommand(c Command, name string) error {
	want, err := CommandIdentity(c.Actor, c.Scope, name, c.Identity.Key())
	if err != nil || c.Identity.Validate() != nil || want.Canonical() != c.Identity.Canonical() {
		return fail(f.InvalidArgument)
	}
	return nil
}
func null(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// credentialLeaseValue preserves SQL NULL for unauthenticated connections while
// rejecting a missing or unexpected lease for an authenticated connection.
func credentialLeaseValue(credential string, lease sc.LeaseID) (string, error) {
	if credential == "" {
		if lease != (sc.LeaseID{}) {
			return "", fail(f.InvalidState)
		}
		return "", nil
	}
	if lease.Validate() != nil {
		return "", fail(f.InvalidState)
	}
	return lease.String(), nil
}

func sameStore(a, b any) bool {
	if nilPort(a) || nilPort(b) {
		return false
	}
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	return av.Type() == bv.Type() && av.Comparable() && av.Interface() == bv.Interface()
}
func sortedLocks(locks []f.LockRequest) []f.LockRequest {
	slices.SortFunc(locks, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	return locks
}
func (Config) Format(w fmt.State, _ rune)      { _, _ = io.WriteString(w, "mcp_config") }
func (ConfigInput) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "mcp_config_input") }
func (CredentialRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "mcp_credential_request")
}
func (CredentialRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"mcp_credential_request"`), nil
}

func encode(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil || len(b) > 128<<10 {
		return nil, fail(f.InvalidArgument)
	}
	return b, nil
}
