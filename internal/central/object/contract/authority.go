package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type OwnerExistence string

const (
	ExistingOwner    OwnerExistence = "existing"
	ProspectiveOwner OwnerExistence = "prospective"
)

// OwnerAuthorization is issued only by the trusted owning domain after checking
// real persisted entities or creation causes. No caller boolean selects this
// branch. The object service matches Actor/Owner/Intent again on every use.
type OwnerAuthorization struct {
	data func() OwnerAuthorizationDetails
}
type OwnerAuthorizationDetails struct {
	Actor         identity.Actor
	Owner         ObjectOwner
	Intent        identity.AccessIntent
	Existence     OwnerExistence
	CreationCause string
	Version       foundation.Version
	// ReadObjectID is mandatory on grants from ObjectReadAuthority. A general
	// owner grant cannot select a historical object on the caller's behalf.
	ReadObjectID   ObjectID
	ProtectedLease *ObjectLease
}

func NewOwnerAuthorization(d OwnerAuthorizationDetails) (OwnerAuthorization, error) {
	if d.Actor.Validate() != nil || d.Owner.Validate() != nil || d.Version.Validate() != nil || d.Existence != ExistingOwner && d.Existence != ProspectiveOwner || d.CreationCause != "" && !identity.ValidCauseRef(d.CreationCause) || d.Existence == ProspectiveOwner && d.CreationCause == "" {
		return OwnerAuthorization{}, bad()
	}
	switch d.Intent {
	case identity.Read, identity.Mutate, identity.Converge, identity.Lifecycle:
	default:
		return OwnerAuthorization{}, bad()
	}
	a := d.Actor.Details()
	if d.Owner.Details().Kind == Avatar && (a.Kind != identity.Human || a.UserID != d.Owner.Details().ID) {
		return OwnerAuthorization{}, bad()
	}
	if a.Kind == identity.Service && d.Intent != identity.Converge && d.Intent != identity.Lifecycle {
		return OwnerAuthorization{}, bad()
	}
	if a.Kind == identity.AgentRun && (d.Owner.Details().ProjectID == "" || a.ProjectID != d.Owner.Details().ProjectID) || a.Kind == identity.Service && a.ProjectID != d.Owner.Details().ProjectID {
		return OwnerAuthorization{}, bad()
	}
	if d.ReadObjectID != (ObjectID{}) || d.ProtectedLease != nil {
		if d.ReadObjectID.Validate() != nil || d.Intent != identity.Read || d.Existence != ExistingOwner {
			return OwnerAuthorization{}, bad()
		}
	}
	if lease := d.ProtectedLease; lease != nil {
		if lease.Validate() != nil || lease.ObjectID != d.ReadObjectID {
			return OwnerAuthorization{}, bad()
		}
		switch lease.Owner.Details().Kind {
		case ExecutionOwner, HistoryOwner, TransferOwner:
		default:
			return OwnerAuthorization{}, bad()
		}
	}
	d = copyAuthorization(d)
	return OwnerAuthorization{func() OwnerAuthorizationDetails { return copyAuthorization(d) }}, nil
}
func copyAuthorization(d OwnerAuthorizationDetails) OwnerAuthorizationDetails {
	if d.ProtectedLease != nil {
		v := *d.ProtectedLease
		d.ProtectedLease = &v
	}
	return d
}
func (g OwnerAuthorization) Validate() error {
	if g.data == nil {
		return bad()
	}
	return nil
}
func (g OwnerAuthorization) Details() OwnerAuthorizationDetails {
	if g.data == nil {
		return OwnerAuthorizationDetails{}
	}
	return g.data()
}
func (g OwnerAuthorization) Matches(actor identity.Actor, owner ObjectOwner, intent identity.AccessIntent) bool {
	return g.data != nil && g.data().Actor.Equal(actor) && g.data().Owner.Equal(owner) && g.data().Intent == intent
}
func (g OwnerAuthorization) MatchesObjectRead(actor identity.Actor, owner ObjectOwner, object ObjectID) bool {
	return object.Validate() == nil && g.Matches(actor, owner, identity.Read) && g.data().Existence == ExistingOwner && g.data().ReadObjectID == object
}
func (g OwnerAuthorization) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_owner_authorization")
}
func (g OwnerAuthorization) MarshalJSON() ([]byte, error) {
	return []byte(`"object_owner_authorization"`), nil
}
func (*OwnerAuthorization) UnmarshalJSON([]byte) error { return bad() }
func (g OwnerAuthorization) LogValue() slog.Value {
	return slog.StringValue("object_owner_authorization")
}

// Both calls must check the current Session/Owner or actual Agent/Execution.
// System scope for Avatar means the exact owning user, not an administrator.
// InTx consumes the caller's transaction and may not open or commit another.
type ResourceAuthority interface {
	AuthorizeOwner(context.Context, identity.Actor, ObjectOwner, identity.AccessIntent) (OwnerAuthorization, error)
	AuthorizeOwnerInTx(context.Context, foundation.Tx, identity.Actor, ObjectOwner, identity.AccessIntent) (OwnerAuthorization, error)
}

// ObjectReadAuthority proves a current fixed-version use of this exact object.
// The caller holds the precollected User/Project, owner and ObjectAggregate
// locks. The implementation must not open a transaction or acquire an omitted
// earlier lock. The object service rechecks the exact protected active lease
// and partition in the same transaction; the returned grant alone is no lease.
type ObjectReadAuthority interface {
	AuthorizeObjectReadInTx(context.Context, foundation.Tx, identity.Actor, ObjectOwner, ObjectID) (OwnerAuthorization, error)
}

// CheckInTx uses already-held User/Project locks and the current durable gate.
// Read on archived is distinct from mutation; deleting allows only explicitly
// validated lifecycle/convergence operations. There is no default allow port.
type ProjectGate interface {
	CheckInTx(context.Context, foundation.Tx, identity.Actor, ObjectOwner, identity.AccessIntent) error
}
type CleanupAuthority interface {
	CheckCleanupInTx(context.Context, foundation.Tx, ObjectCleanupCause, ObjectID) error
	CheckProjectCleanupInTx(context.Context, foundation.Tx, identity.Actor, ProjectCleanupCause) error
}
type LeaseAction string

const (
	AcquireLease LeaseAction = "acquire"
	ReleaseLease LeaseAction = "release"
)

// Stable execution/history/transfer leases need the owning domain's explicit
// release evidence. Their age or another process's missing connection is not
// terminal evidence. Reader/source/writer leases are owned by this service.
type LeaseAuthority interface {
	AuthorizeLeaseInTx(context.Context, foundation.Tx, identity.Actor, ObjectID, LeaseOwner, LeaseAction) error
}
type ProcessAuthority interface {
	// ConfirmStopped must prove death of this exact former instance. It must
	// not infer death from heartbeat age, TTL or a failed network request.
	ConfirmStopped(context.Context, ProcessID) error
}
type ProjectCleanupCause struct{ data func() ProjectCleanupDetails }
type ProjectCleanupDetails struct {
	ProjectID   identity.ProjectID
	OperationID CleanupID
	Version     foundation.Version
}

func NewProjectCleanupCause(d ProjectCleanupDetails) (ProjectCleanupCause, error) {
	if d.ProjectID.Validate() != nil || d.OperationID.Validate() != nil || d.Version.Validate() != nil {
		return ProjectCleanupCause{}, bad()
	}
	return ProjectCleanupCause{func() ProjectCleanupDetails { return d }}, nil
}
func (c ProjectCleanupCause) Validate() error {
	if c.data == nil {
		return bad()
	}
	return nil
}
func (c ProjectCleanupCause) Details() ProjectCleanupDetails {
	if c.data == nil {
		return ProjectCleanupDetails{}
	}
	return c.data()
}
func (c ProjectCleanupCause) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_project_cleanup")
}
func (c ProjectCleanupCause) MarshalJSON() ([]byte, error) {
	return []byte(`"object_project_cleanup"`), nil
}
func (*ProjectCleanupCause) UnmarshalJSON([]byte) error { return bad() }
func (c ProjectCleanupCause) LogValue() slog.Value      { return slog.StringValue("object_project_cleanup") }

type ProjectCleanupResult struct {
	State       CleanupState        `json:"state"`
	ProjectID   identity.ProjectID  `json:"project_id"`
	OperationID CleanupID           `json:"operation_id"`
	Remaining   foundation.Progress `json:"remaining"`
}

// These interfaces are consumer ports, not implementations of future business
// authorities. Unbound authorities are rejected by the object implementation.
type Objects interface {
	PutObject(context.Context, identity.Actor, ObjectOwner, foundation.CommandMeta, string, int64, *foundation.Digest, io.ReadCloser) (PutResult, error)
	LookupPut(context.Context, identity.Actor, ObjectOwner, foundation.IdempotencyKey) (LookupResult, error)
	CancelUpload(context.Context, identity.Actor, ObjectOwner, foundation.IdempotencyKey) (LookupResult, error)
	AttachObjectInTx(context.Context, foundation.Tx, identity.Actor, ObjectOwner, ObjectID) (ObjectReference, error)
	ConsumeUploadInTx(context.Context, foundation.Tx, identity.Actor, ObjectOwner, UploadReceipt) (ObjectReference, error)
	ReleaseObjectInTx(context.Context, foundation.Tx, identity.Actor, ObjectOwner, ObjectID) error
	StatObject(context.Context, identity.Actor, ObjectOwner, ObjectID) (ObjectMeta, error)
	ReadObject(context.Context, identity.Actor, ObjectOwner, ObjectID, *ByteRange) (*ObjectReader, error)
	OpenUploadSource(context.Context, identity.Actor, ObjectOwner, UploadReceipt) (*ObjectReader, error)
}
type Leases interface {
	AcquireLeaseInTx(context.Context, foundation.Tx, identity.Actor, ObjectID, LeaseOwner) (ObjectLease, error)
	ReleaseLeaseInTx(context.Context, foundation.Tx, identity.Actor, ObjectID, LeaseOwner) error
}
type Cleaner interface {
	InspectReferences(context.Context, ObjectID) (ReferenceInspection, error)
	DeleteUnreferenced(context.Context, ObjectCleanupCause, ObjectID) (CleanupResult, error)
	CleanupProject(context.Context, identity.Actor, ProjectCleanupCause) (ProjectCleanupResult, error)
}
