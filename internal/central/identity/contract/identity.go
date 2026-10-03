// Package contract defines trusted identities and current-authorization ports.
// It supplies no Session, Owner, administrator, or lifecycle implementation.
package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type User struct{}
type Session struct{}
type Project struct{}
type Agent struct{}
type Execution struct{}
type UserID = foundation.ID[User]
type SessionID = foundation.ID[Session]
type ProjectID = foundation.ID[Project]
type AgentID = foundation.ID[Agent]
type ExecutionID = foundation.ID[Execution]

type ActorKind string

const (
	Human    ActorKind = "human"
	AgentRun ActorKind = "agent_run"
	Service  ActorKind = "service"
)

type ScopeKind string

const (
	System       ScopeKind = "system"
	ProjectScope ScopeKind = "project"
	AgentMemory  ScopeKind = "agent_memory"
)

type AccessIntent string

const (
	Read      AccessIntent = "read"
	Mutate    AccessIntent = "mutate"
	Launch    AccessIntent = "launch"
	Resume    AccessIntent = "resume"
	Lifecycle AccessIntent = "lifecycle"
	Converge  AccessIntent = "converge"
)

type ServiceName string

const (
	SecretService     ServiceName = "secret"
	SecretMaintenance ServiceName = "secret-maintenance"
	OutboundService   ServiceName = "outbound"
	ObjectService     ServiceName = "object"
	ObjectMaintenance ServiceName = "object-maintenance"
	ProjectLifecycle  ServiceName = "project-lifecycle"
)

func invalid() error { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
func Unbound() error { return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted) }

type Scope struct{ data func() ScopeDetails }
type ScopeDetails struct {
	Kind      ScopeKind `json:"kind"`
	ProjectID string    `json:"project_id,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
}

func SystemScope() Scope {
	return Scope{data: func() ScopeDetails { return ScopeDetails{Kind: System} }}
}
func InProject(id ProjectID) (Scope, error) {
	if id.Validate() != nil {
		return Scope{}, invalid()
	}
	d := ScopeDetails{Kind: ProjectScope, ProjectID: id.String()}
	return Scope{data: func() ScopeDetails { return d }}, nil
}
func InAgentMemory(project ProjectID, agent AgentID) (Scope, error) {
	if project.Validate() != nil || agent.Validate() != nil {
		return Scope{}, invalid()
	}
	d := ScopeDetails{Kind: AgentMemory, ProjectID: project.String(), AgentID: agent.String()}
	return Scope{data: func() ScopeDetails { return d }}, nil
}
func (s Scope) Validate() error {
	if s.data == nil {
		return invalid()
	}
	return nil
}
func (s Scope) Details() ScopeDetails {
	if s.data == nil {
		return ScopeDetails{}
	}
	return s.data()
}
func (s Scope) Equal(other Scope) bool {
	return s.Validate() == nil && other.Validate() == nil && s.Details() == other.Details()
}
func (s Scope) MarshalJSON() ([]byte, error) {
	if s.Validate() != nil {
		return nil, invalid()
	}
	return json.Marshal(s.Details())
}
func (s *Scope) UnmarshalJSON([]byte) error { return invalid() }
func (s Scope) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "identity_scope") }
func (s Scope) LogValue() slog.Value        { return slog.StringValue("identity_scope") }

// Details is an explicit trusted adapter projection, never a logging object.
type ActorDetails struct {
	Kind                                               ActorKind
	UserID, SessionID, ProjectID, AgentID, ExecutionID string
	ServiceName                                        ServiceName
	CauseRef                                           string
}
type Actor struct{ data func() ActorDetails }

func NewHuman(user UserID, session SessionID) (Actor, error) {
	if user.Validate() != nil || session.Validate() != nil {
		return Actor{}, invalid()
	}
	d := ActorDetails{Kind: Human, UserID: user.String(), SessionID: session.String()}
	return actor(d), nil
}
func NewAgentRun(project ProjectID, agentID AgentID, execution ExecutionID) (Actor, error) {
	if project.Validate() != nil || agentID.Validate() != nil || execution.Validate() != nil {
		return Actor{}, invalid()
	}
	return actor(ActorDetails{Kind: AgentRun, ProjectID: project.String(), AgentID: agentID.String(), ExecutionID: execution.String()}), nil
}
func actor(d ActorDetails) Actor { return Actor{data: func() ActorDetails { return d }} }
func (a Actor) Validate() error {
	if a.data == nil {
		return invalid()
	}
	return nil
}
func (a Actor) Details() ActorDetails {
	if a.data == nil {
		return ActorDetails{}
	}
	return a.data()
}
func (a Actor) Equal(b Actor) bool {
	return a.Validate() == nil && b.Validate() == nil && a.Details() == b.Details()
}
func (a Actor) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "trusted_actor") }
func (a Actor) MarshalJSON() ([]byte, error) { return []byte(`"trusted_actor"`), nil }
func (a *Actor) UnmarshalJSON([]byte) error  { return invalid() }
func (a Actor) LogValue() slog.Value         { return slog.StringValue("trusted_actor") }

// RegisterService is only for the trusted composition root. Roles are a closed
// set; registration creates no permission to browse Audit or impersonate Owner.
type ServiceRegistration struct{ data func() ServiceName }

func RegisterService(name ServiceName) (ServiceRegistration, error) {
	switch name {
	case SecretService, SecretMaintenance, OutboundService, ObjectService, ObjectMaintenance, ProjectLifecycle:
	default:
		return ServiceRegistration{}, invalid()
	}
	return ServiceRegistration{data: func() ServiceName { return name }}, nil
}
func (r ServiceRegistration) Actor(causeRef string, scope Scope) (Actor, error) {
	if r.data == nil || scope.Validate() != nil || scope.Details().Kind == AgentMemory || !ValidCauseRef(causeRef) {
		return Actor{}, invalid()
	}
	return actor(ActorDetails{Kind: Service, ServiceName: r.data(), CauseRef: causeRef, ProjectID: scope.Details().ProjectID}), nil
}
func ValidCauseRef(ref string) bool {
	if _, err := foundation.ParseID[struct{}](ref); err == nil {
		return true
	}
	return foundation.Digest(ref).Validate() == nil
}

type AccessGrant struct{ data func() grantDetails }
type grantDetails struct {
	Actor     Actor
	Scope     Scope
	Intent    AccessIntent
	CheckedAt foundation.Instant
	Revision  foundation.Version
}

func NewAccessGrant(actor Actor, scope Scope, intent AccessIntent, checkedAt foundation.Instant, revision foundation.Version) (AccessGrant, error) {
	if actor.Validate() != nil || scope.Validate() != nil || checkedAt.Validate() != nil || revision.Validate() != nil {
		return AccessGrant{}, invalid()
	}
	switch intent {
	case Read, Mutate, Launch, Resume, Lifecycle, Converge:
	default:
		return AccessGrant{}, invalid()
	}
	d := grantDetails{actor, scope, intent, checkedAt, revision}
	return AccessGrant{data: func() grantDetails { return d }}, nil
}
func (g AccessGrant) Matches(actor Actor, scope Scope, intent AccessIntent) bool {
	return g.data != nil && g.data().Actor.Equal(actor) && g.data().Scope.Equal(scope) && g.data().Intent == intent
}
func (g AccessGrant) CheckedAt() foundation.Instant {
	if g.data == nil {
		return foundation.Instant{}
	}
	return g.data().CheckedAt
}
func (g AccessGrant) AuthorizationRevision() foundation.Version {
	if g.data == nil {
		return 0
	}
	return g.data().Revision
}
func (g AccessGrant) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "access_grant") }
func (g AccessGrant) MarshalJSON() ([]byte, error) { return []byte(`"access_grant"`), nil }
func (g *AccessGrant) UnmarshalJSON([]byte) error  { return invalid() }
func (g AccessGrant) LogValue() slog.Value         { return slog.StringValue("access_grant") }

type Authorizer interface {
	Authorize(context.Context, Actor, Scope, AccessIntent) (AccessGrant, error)
}

// A zero Tx denotes a read outside a caller-owned transaction. Implementations
// must consult current durable Session/system permissions on every invocation.
type SessionAuthority interface {
	RequireCurrentSession(context.Context, foundation.Tx, Actor) error
}
type SystemAuthority interface {
	AuthorizeSystem(context.Context, foundation.Tx, Actor, AccessIntent) (AccessGrant, error)
}
