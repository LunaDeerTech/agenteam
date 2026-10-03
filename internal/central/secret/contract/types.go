package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type Credential struct{}
type Lease struct{}
type LifecycleOperation struct{}
type CredentialID = foundation.ID[Credential]
type LeaseID = foundation.ID[Lease]
type CredentialRef struct{ data func() RefDetails }
type RefDetails struct {
	ID    CredentialID   `json:"id"`
	Scope identity.Scope `json:"scope"`
}

func NewCredentialRef(id CredentialID, scope identity.Scope) (CredentialRef, error) {
	if id.Validate() != nil || !validScope(scope) {
		return CredentialRef{}, bad()
	}
	d := RefDetails{id, scope}
	return CredentialRef{data: func() RefDetails { return d }}, nil
}
func (r CredentialRef) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r CredentialRef) Details() RefDetails {
	if r.data == nil {
		return RefDetails{}
	}
	return r.data()
}
func (r CredentialRef) Equal(other CredentialRef) bool {
	return r.Validate() == nil && other.Validate() == nil && r.Details().ID == other.Details().ID && r.Details().Scope.Equal(other.Details().Scope)
}
func (r CredentialRef) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "credential_ref") }
func (r CredentialRef) MarshalJSON() ([]byte, error) {
	if r.Validate() != nil {
		return nil, bad()
	}
	return json.Marshal(r.Details())
}
func (r *CredentialRef) UnmarshalJSON([]byte) error { return bad() }
func (r CredentialRef) LogValue() slog.Value        { return slog.StringValue("credential_ref") }

type Purpose string

const (
	Model         Purpose = "model"
	MCP           Purpose = "mcp"
	Runner        Purpose = "runner"
	SMTP          Purpose = "smtp"
	ObjectStorage Purpose = "object_storage"
	System        Purpose = "system"
)

func (p Purpose) Valid() bool {
	switch p {
	case Model, MCP, Runner, SMTP, ObjectStorage, System:
		return true
	}
	return false
}

type LeaseOwnerKind string

const (
	ExecutionOwner LeaseOwnerKind = "execution"
	ModelCallOwner LeaseOwnerKind = "model_call"
)

type CredentialLeaseOwner struct{ data func() OwnerDetails }
type OwnerDetails struct {
	Kind LeaseOwnerKind `json:"kind"`
	ID   string         `json:"id"`
}

func NewCredentialLeaseOwner(kind LeaseOwnerKind, id string) (CredentialLeaseOwner, error) {
	if kind != ExecutionOwner && kind != ModelCallOwner || !validID(id) {
		return CredentialLeaseOwner{}, bad()
	}
	d := OwnerDetails{kind, id}
	return CredentialLeaseOwner{data: func() OwnerDetails { return d }}, nil
}
func (o CredentialLeaseOwner) Validate() error {
	if o.data == nil {
		return bad()
	}
	return nil
}
func (o CredentialLeaseOwner) Details() OwnerDetails {
	if o.data == nil {
		return OwnerDetails{}
	}
	return o.data()
}
func (o CredentialLeaseOwner) Equal(other CredentialLeaseOwner) bool {
	return o.Validate() == nil && other.Validate() == nil && o.Details() == other.Details()
}
func (o CredentialLeaseOwner) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "credential_lease_owner")
}
func (o CredentialLeaseOwner) MarshalJSON() ([]byte, error) {
	if o.Validate() != nil {
		return nil, bad()
	}
	return json.Marshal(o.Details())
}
func (o *CredentialLeaseOwner) UnmarshalJSON([]byte) error { return bad() }
func (o CredentialLeaseOwner) LogValue() slog.Value {
	return slog.StringValue("credential_lease_owner")
}

type CredentialLease struct {
	LeaseID       LeaseID       `json:"lease_id"`
	CredentialRef CredentialRef `json:"credential_ref"`
}
type Metadata struct {
	CredentialRef CredentialRef      `json:"credential_ref"`
	Purpose       Purpose            `json:"purpose"`
	Version       foundation.Version `json:"version"`
}
type MutationResult struct {
	Metadata Metadata `json:"metadata"`
	Deleted  bool     `json:"deleted"`
}
type MutationKind string

const (
	Create MutationKind = "create"
	Update MutationKind = "update"
	Delete MutationKind = "delete"
)

type WriteRequest struct {
	Actor           identity.Actor
	Scope           identity.Scope
	Identity        foundation.CommandIdentity
	Kind            MutationKind
	Ref             CredentialRef      // omitted for create; its ID is allocated during preparation
	ExpectedVersion foundation.Version // omitted for create
	Purpose         Purpose
	Value           SecretMaterial // omitted for delete
}

type CredentialLeases interface {
	AcquireCredentialLeaseInTx(context.Context, foundation.Tx, identity.Actor, CredentialRef, CredentialLeaseOwner) (CredentialLease, error)
	ReadCredentialForRequest(context.Context, identity.Actor, LeaseID) (SecretMaterial, error)
	ReleaseCredentialLeaseInTx(context.Context, foundation.Tx, identity.Actor, LeaseID) error
}
type References interface {
	RetainReferenceInTx(context.Context, foundation.Tx, identity.Actor, CredentialRef, Purpose, string) error
	ReleaseReferenceInTx(context.Context, foundation.Tx, identity.Actor, CredentialRef, Purpose, string) error
}
type LeaseAction string

const (
	AcquireLease LeaseAction = "acquire"
	ReleaseLease LeaseAction = "release"
	ReadLease    LeaseAction = "read"
)

// UseGrant is a trusted adapter response. D09/D20/D22 must check persisted
// binding, actual execution identity, cancellation and current Project gate;
// Model and MCP binding validity remain distinct responsibilities there.
type UseGrant struct {
	Subject                                  identity.Actor
	Consumer                                 Purpose
	RequestID, OperationID, ToolID, RunnerID string
}

func (g UseGrant) Validate(scope identity.Scope) error {
	if !validScope(scope) || g.Subject.Validate() != nil || !g.Consumer.Valid() {
		return bad()
	}
	a := g.Subject.Details()
	if a.Kind == identity.AgentRun && (scope.Details().Kind != identity.ProjectScope || a.ProjectID != scope.Details().ProjectID) {
		return bad()
	}
	if a.Kind == identity.Service && (a.ServiceName != identity.SecretService || a.ProjectID != scope.Details().ProjectID) {
		return bad()
	}
	for _, id := range []string{g.RequestID, g.OperationID, g.ToolID, g.RunnerID} {
		if id != "" && !validID(id) {
			return bad()
		}
	}
	return nil
}

type UsageAuthority interface {
	CheckReferenceInTx(context.Context, foundation.Tx, identity.Actor, CredentialRef, Purpose, string, bool) error
	AuthorizeLeaseInTx(context.Context, foundation.Tx, identity.Actor, CredentialRef, CredentialLeaseOwner, LeaseAction) (UseGrant, error)
}
type ProjectAuthority interface {
	AuthorizeProject(context.Context, foundation.Tx, identity.Actor, identity.ProjectID, identity.AccessIntent) (identity.AccessGrant, error)
	CheckMutationInTx(context.Context, foundation.Tx, identity.Actor, CredentialRef) error
	CheckCleanupInTx(context.Context, foundation.Tx, identity.Actor, LifecycleCause, identity.ProjectID) error
}
type LifecycleCause struct{ data func() LifecycleDetails }
type LifecycleDetails struct {
	OperationID    foundation.ID[LifecycleOperation]
	ProjectVersion foundation.Version
	Deleting       bool
}

// The boolean labels the requested lifecycle intent only. The Project adapter
// must independently verify persisted deleting + stopped in the supplied Tx.
func NewLifecycleCause(id foundation.ID[LifecycleOperation], version foundation.Version, deleting bool) (LifecycleCause, error) {
	if id.Validate() != nil || version.Validate() != nil {
		return LifecycleCause{}, bad()
	}
	d := LifecycleDetails{id, version, deleting}
	return LifecycleCause{data: func() LifecycleDetails { return d }}, nil
}
func (c LifecycleCause) Validate() error {
	if c.data == nil {
		return bad()
	}
	return nil
}
func (c LifecycleCause) Details() LifecycleDetails {
	if c.data == nil {
		return LifecycleDetails{}
	}
	return c.data()
}
func (c LifecycleCause) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "secret_lifecycle_cause")
}
func (c LifecycleCause) MarshalJSON() ([]byte, error) { return []byte(`"secret_lifecycle_cause"`), nil }
func (c *LifecycleCause) UnmarshalJSON([]byte) error  { return bad() }
func (c LifecycleCause) LogValue() slog.Value         { return slog.StringValue("secret_lifecycle_cause") }

type CleanupCheckpoint struct {
	ProjectID   identity.ProjectID                `json:"project_id"`
	OperationID foundation.ID[LifecycleOperation] `json:"operation_id"`
}
type CleanupReport struct {
	Completed  bool              `json:"completed"`
	Checkpoint CleanupCheckpoint `json:"checkpoint"`
}

func validID(id string) bool { _, err := foundation.ParseID[struct{}](id); return err == nil }
func validScope(scope identity.Scope) bool {
	return scope.Validate() == nil && (scope.Details().Kind == identity.System || scope.Details().Kind == identity.ProjectScope)
}
func bad() error { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
