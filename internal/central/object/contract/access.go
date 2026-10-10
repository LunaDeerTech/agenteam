package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type AccessKind string

const (
	OwnerAccess          AccessKind = "owner"
	ObjectReadAccess     AccessKind = "object_read"
	LeaseAccess          AccessKind = "lease"
	ObjectCleanupAccess  AccessKind = "object_cleanup"
	ProjectCleanupAccess AccessKind = "project_cleanup"
	SourceAccess         AccessKind = "source"
	MaintenanceAccess    AccessKind = "maintenance"
	TransferAccess       AccessKind = "transfer"
	CleanupReleaseAccess AccessKind = "cleanup_release"
)

type AccessOperation string

const (
	PrepareAccess           AccessOperation = "prepare"
	PrepareReadAccess       AccessOperation = "prepare_read"
	ReserveAccess           AccessOperation = "reserve"
	SendAccess              AccessOperation = "send"
	PublishAccess           AccessOperation = "publish"
	LookupAccess            AccessOperation = "lookup"
	CancelAccess            AccessOperation = "cancel"
	AttachAccess            AccessOperation = "attach"
	ConsumeAccess           AccessOperation = "consume"
	ReleaseAccess           AccessOperation = "release"
	StatAccess              AccessOperation = "stat"
	ReadAccess              AccessOperation = "read"
	OpenSourceAccess        AccessOperation = "open_source"
	AcquireUseAccess        AccessOperation = "acquire_use"
	ReleaseUseAccess        AccessOperation = "release_use"
	CleanupObjectAccess     AccessOperation = "cleanup_object"
	GateProjectAccess       AccessOperation = "gate_project"
	FinishProjectAccess     AccessOperation = "finish_project"
	ValidateSourceAccess    AccessOperation = "validate_source"
	AcquireSourceAccess     AccessOperation = "acquire_source"
	FinishWriterAccess      AccessOperation = "finish_writer"
	RecoverAttemptAccess    AccessOperation = "recover_attempt"
	JoinAttemptAccess       AccessOperation = "join_attempt"
	ReleaseProcessAccess    AccessOperation = "release_process"
	ReleaseReaderAccess     AccessOperation = "release_reader"
	InspectAccess           AccessOperation = "inspect"
	ClaimCleanupAccess      AccessOperation = "claim_cleanup"
	CheckpointCleanupAccess AccessOperation = "checkpoint_cleanup"
	FinalizeCleanupAccess   AccessOperation = "finalize_cleanup"
	ReleaseForCleanupAccess AccessOperation = "release_for_cleanup"
)

// AccessRequest is a non-authorizing, immutable description of one complete
// operation. Its constructors reject fields belonging to another variant.
type AccessRequest struct {
	data        func() AccessRequestDetails
	fingerprint func() string
}
type AccessRequestDetails struct {
	Kind                  AccessKind
	Operation             AccessOperation
	Actor                 identity.Actor
	Owner                 ObjectOwner
	Intent                identity.AccessIntent
	ObjectID              ObjectID
	Command               *foundation.CommandMeta
	Key                   foundation.IdempotencyKey
	Prepared              PreparedPayload
	Attempt               UploadAttempt
	Receipt               UploadReceipt
	ExpectedSemantic      foundation.Digest
	Range                 *ByteRange
	LeaseOwner            LeaseOwner
	Cleanup               ObjectCleanupCause
	ProjectCleanup        ProjectCleanupCause
	Objects               []ObjectID
	Source                ResolvedSource
	InstanceID, ProcessID ProcessID
	AttemptID             AttemptID
	UploadID              UploadID
	LeaseID               LeaseID
	CleanupID             CleanupID
	WorkerID              CleanupID
	Fence                 foundation.Version
	Transfer              TransferAccessRequest
}

func NewOwnerAccess(d AccessRequestDetails) (AccessRequest, error) {
	return newAccessRequest(OwnerAccess, d)
}
func NewObjectReadAccess(d AccessRequestDetails) (AccessRequest, error) {
	return newAccessRequest(ObjectReadAccess, d)
}
func NewLeaseAccess(d AccessRequestDetails) (AccessRequest, error) {
	return newAccessRequest(LeaseAccess, d)
}
func NewObjectCleanupAccess(d AccessRequestDetails) (AccessRequest, error) {
	return newAccessRequest(ObjectCleanupAccess, d)
}
func NewCleanupReleaseAccess(d AccessRequestDetails) (AccessRequest, error) {
	return newAccessRequest(CleanupReleaseAccess, d)
}
func NewProjectCleanupAccess(d AccessRequestDetails) (AccessRequest, error) {
	return newAccessRequest(ProjectCleanupAccess, d)
}
func NewSourceAccess(d AccessRequestDetails) (AccessRequest, error) {
	return newAccessRequest(SourceAccess, d)
}
func NewMaintenanceAccess(d AccessRequestDetails) (AccessRequest, error) {
	return newAccessRequest(MaintenanceAccess, d)
}
func NewTransferAccess(r TransferAccessRequest) (AccessRequest, error) {
	if r.Validate() != nil {
		return AccessRequest{}, bad()
	}
	return newAccessRequest(TransferAccess, AccessRequestDetails{Operation: AccessOperation("transfer_" + string(r.Details().Operation)), Transfer: r})
}
func newAccessRequest(kind AccessKind, d AccessRequestDetails) (AccessRequest, error) {
	if d.Kind != "" && d.Kind != kind {
		return AccessRequest{}, bad()
	}
	d.Kind = kind
	allowed := map[string]bool{}
	allow := func(names ...string) {
		for _, n := range names {
			allowed[n] = true
		}
	}
	require := func(ok bool) error {
		if !ok {
			return bad()
		}
		return nil
	}
	var err error
	switch kind {
	case TransferAccess:
		allow("transfer")
		err = require(d.Transfer.Validate() == nil && d.Operation == AccessOperation("transfer_"+string(d.Transfer.Details().Operation)))
	case OwnerAccess:
		allow("actor", "owner", "intent")
		if d.Actor.Validate() != nil || d.Owner.Validate() != nil {
			return AccessRequest{}, bad()
		}
		intent := identity.Mutate
		switch d.Operation {
		case PrepareAccess:
		case PrepareReadAccess:
			intent = identity.Read
		case ReserveAccess:
			allow("command", "prepared")
			err = require(d.Command != nil && d.Command.Validate() == nil && d.Prepared.Validate() == nil)
		case SendAccess:
			allow("prepared", "attempt")
			err = require(d.Prepared.Validate() == nil && d.Attempt.Validate() == nil)
		case PublishAccess:
			allow("attempt")
			err = require(d.Attempt.Validate() == nil)
		case LookupAccess:
			allow("key", "semantic")
			err = d.Key.Validate()
			intent = identity.Read
		case CancelAccess:
			allow("key")
			err = d.Key.Validate()
			intent = identity.Converge
		case AttachAccess, ReleaseAccess:
			allow("object")
			err = d.ObjectID.Validate()
			if d.Operation == ReleaseAccess {
				intent = identity.Converge
			}
		case ConsumeAccess:
			allow("receipt")
			err = require(d.Receipt.Validate() == nil && d.Receipt.Details().Owner.Equal(d.Owner))
		default:
			return AccessRequest{}, bad()
		}
		if d.Intent != intent {
			return AccessRequest{}, bad()
		}
	case ObjectReadAccess:
		allow("actor", "owner", "intent", "object")
		if d.Actor.Validate() != nil || d.Owner.Validate() != nil || d.Intent != identity.Read || d.ObjectID.Validate() != nil {
			return AccessRequest{}, bad()
		}
		switch d.Operation {
		case StatAccess:
		case ReadAccess:
			allow("range")
		case OpenSourceAccess:
			allow("receipt")
			err = require(d.Receipt.Validate() == nil && d.Receipt.Details().Owner.Equal(d.Owner) && d.Receipt.Details().ObjectID == d.ObjectID)
		default:
			return AccessRequest{}, bad()
		}
	case LeaseAccess:
		allow("actor", "object", "lease_owner")
		err = require(d.Actor.Validate() == nil && d.ObjectID.Validate() == nil && d.LeaseOwner.Validate() == nil && (d.Operation == AcquireUseAccess || d.Operation == ReleaseUseAccess))
		if d.LeaseOwner.Validate() == nil {
			switch d.LeaseOwner.Details().Kind {
			case ExecutionOwner, HistoryOwner, TransferOwner:
			default:
				return AccessRequest{}, bad()
			}
		}
	case ObjectCleanupAccess:
		allow("object", "cleanup")
		allowedOperation := d.Operation == CleanupObjectAccess
		if d.Operation == PurgeDeletedObjectMetadataAccess {
			cause := d.Cleanup.Details()
			owner := cause.Owner.Details()
			allowedOperation = owner.Kind == SkillRevision && validID(owner.ProjectID) && cause.Reason == ProjectDeleted
		}
		err = require(allowedOperation && d.ObjectID.Validate() == nil && d.Cleanup.Validate() == nil)
	case CleanupReleaseAccess:
		allow("object", "cleanup", "upload")
		cause := d.Cleanup.Details()
		owner := cause.Owner.Details()
		allowedCause := owner.Kind == Avatar && (cause.Reason == ReplacedObject || cause.Reason == CancelledUpload) ||
			owner.Kind == Knowledge && validID(owner.ProjectID) && (cause.Reason == ReplacedObject || cause.Reason == CancelledUpload || cause.Reason == OwnerDeleted) ||
			owner.Kind == SkillRevision && validID(owner.ProjectID) && cause.Reason == ProjectDeleted
		err = require(d.Operation == ReleaseForCleanupAccess && d.ObjectID.Validate() == nil && d.UploadID.Validate() == nil && d.Cleanup.Validate() == nil && allowedCause)
	case ProjectCleanupAccess:
		allow("actor", "project_cleanup")
		err = require(d.Actor.Validate() == nil && d.ProjectCleanup.Validate() == nil)
		allow("objects")
		if d.Operation != FinishProjectAccess && d.Operation != GateProjectAccess {
			return AccessRequest{}, bad()
		}
	case SourceAccess:
		allow("actor", "source")
		err = require((d.Operation == ValidateSourceAccess || d.Operation == AcquireSourceAccess) && d.Actor.Validate() == nil && d.Source.Validate() == nil)
	case MaintenanceAccess:
		allow("instance", "object")
		err = require(d.InstanceID.Validate() == nil && d.ObjectID.Validate() == nil)
		switch d.Operation {
		case InspectAccess, FinalizeCleanupAccess:
		case FinishWriterAccess, RecoverAttemptAccess, ClaimCleanupAccess:
			allow("attempt_id")
			if d.AttemptID.Validate() != nil {
				return AccessRequest{}, bad()
			}
		case JoinAttemptAccess:
			allow("attempt_id", "process")
			if d.AttemptID.Validate() != nil || d.ProcessID.Validate() != nil {
				return AccessRequest{}, bad()
			}
		case ReleaseProcessAccess:
			allow("process")
			if d.ProcessID.Validate() != nil {
				return AccessRequest{}, bad()
			}
		case ReleaseReaderAccess:
			allow("lease_id")
			if d.LeaseID.Validate() != nil {
				return AccessRequest{}, bad()
			}
		case CheckpointCleanupAccess:
			allow("attempt_id", "cleanup_id", "worker_id", "fence")
			if d.AttemptID.Validate() != nil || d.CleanupID.Validate() != nil || d.WorkerID.Validate() != nil || d.Fence.Validate() != nil {
				return AccessRequest{}, bad()
			}
		default:
			return AccessRequest{}, bad()
		}
	default:
		return AccessRequest{}, bad()
	}
	if err != nil {
		return AccessRequest{}, bad()
	}
	present := map[string]bool{
		"actor": d.Actor.Validate() == nil, "owner": d.Owner.Validate() == nil, "intent": d.Intent != "", "object": d.ObjectID != (ObjectID{}),
		"command": d.Command != nil, "key": d.Key != "", "prepared": d.Prepared.Validate() == nil, "attempt": d.Attempt.Validate() == nil, "receipt": d.Receipt.Validate() == nil,
		"semantic": d.ExpectedSemantic != "", "range": d.Range != nil, "lease_owner": d.LeaseOwner.Validate() == nil, "cleanup": d.Cleanup.Validate() == nil,
		"project_cleanup": d.ProjectCleanup.Validate() == nil, "objects": len(d.Objects) > 0, "source": d.Source.Validate() == nil,
		"instance": d.InstanceID != (ProcessID{}), "process": d.ProcessID != (ProcessID{}), "attempt_id": d.AttemptID != (AttemptID{}), "lease_id": d.LeaseID != (LeaseID{}),
		"cleanup_id": d.CleanupID != (CleanupID{}), "worker_id": d.WorkerID != (CleanupID{}), "fence": d.Fence != 0,
		"transfer": d.Transfer.Validate() == nil,
		"upload":   d.UploadID != (UploadID{}),
	}
	for name, p := range present {
		if p && !allowed[name] {
			return AccessRequest{}, bad()
		}
	}
	if d.ExpectedSemantic != "" && d.ExpectedSemantic.Validate() != nil {
		return AccessRequest{}, bad()
	}
	if r := d.Range; r != nil && (r.Offset < 0 || r.Length <= 0 || r.Offset > math.MaxInt64-r.Length) {
		return AccessRequest{}, bad()
	}
	if len(d.Objects) > 100 {
		return AccessRequest{}, bad()
	}
	seen := map[ObjectID]bool{}
	for _, id := range d.Objects {
		if id.Validate() != nil || seen[id] {
			return AccessRequest{}, bad()
		}
		seen[id] = true
	}
	d = copyAccessRequest(d)
	fingerprint := requestFingerprint(d)
	return AccessRequest{data: func() AccessRequestDetails { return copyAccessRequest(d) }, fingerprint: func() string { return fingerprint }}, nil
}
func copyAccessRequest(d AccessRequestDetails) AccessRequestDetails {
	if d.Command != nil {
		v := *d.Command
		if v.ExpectedVersion != nil {
			version := *v.ExpectedVersion
			v.ExpectedVersion = &version
		}
		d.Command = &v
	}
	if d.Range != nil {
		v := *d.Range
		d.Range = &v
	}
	d.Objects = append([]ObjectID(nil), d.Objects...)
	return d
}
func receiptProjection(r UploadReceipt) any {
	d := r.Details()
	return []any{d.ID.String(), d.UploadID.String(), d.ObjectID.String(), d.Owner.Details(), d.CreationCause}
}
func sourceProjection(s ResolvedSource) any {
	d := s.Details()
	r := d.Reference.Details()
	m := d.Meta
	return []any{r.Kind, r.ProjectID.String(), r.ArtifactID, r.FileID, r.DocumentID, r.ExecutionID, r.PayloadID, int64(r.Revision), receiptProjection(r.Receipt), d.Owner.Details(), m.ID.String(), m.Scope.Details(), m.MediaType, int64(m.ByteSize), m.SHA256.String(), m.State, int64(m.Version), m.CreatedAt.String(), int64(d.Revision)}
}
func requestFingerprint(d AccessRequestDetails) string {
	cleanup := d.Cleanup.Details()
	project := d.ProjectCleanup.Details()
	prepared := d.Prepared.Details()
	attempt := d.Attempt.Details()
	var command any
	if d.Command != nil {
		expected := ""
		if d.Command.ExpectedVersion != nil {
			expected = d.Command.ExpectedVersion.String()
		}
		command = []any{d.Command.RequestID.String(), string(d.Command.IdempotencyKey), expected}
	}
	objects := make([]string, len(d.Objects))
	for i, id := range d.Objects {
		objects[i] = id.String()
	}
	// Only primitive projections enter JSON: optional zero typed scalars must not
	// make Marshal fail and collapse distinct requests into the same fingerprint.
	b, err := json.Marshal([]any{d.Kind, d.Operation, d.Actor.Details(), d.Owner.Details(), d.Intent, d.ObjectID.String(), command, string(d.Key), prepared.ID.String(), prepared.MediaType, prepared.Length, prepared.SHA256.String(), attempt.ID.String(), attempt.UploadID.String(), attempt.ObjectID.String(), receiptProjection(d.Receipt), d.ExpectedSemantic.String(), d.Range, d.LeaseOwner.Details(), cleanup.OperationID.String(), cleanup.Owner.Details(), cleanup.Reason, project.ProjectID.String(), project.OperationID.String(), int64(project.Version), objects, sourceProjection(d.Source), d.InstanceID.String(), d.ProcessID.String(), d.AttemptID.String(), d.LeaseID.String(), d.CleanupID.String(), d.WorkerID.String(), int64(d.Fence), d.Transfer.Fingerprint()})
	if err != nil {
		panic("primitive object access projection")
	}
	if d.Kind == CleanupReleaseAccess {
		b = append(b, []byte("\x00"+d.UploadID.String())...)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (r AccessRequest) Validate() error {
	if r.data == nil || r.fingerprint == nil {
		return bad()
	}
	return nil
}
func (r AccessRequest) Details() AccessRequestDetails {
	if r.data == nil {
		return AccessRequestDetails{}
	}
	return r.data()
}
func (r AccessRequest) Equal(other AccessRequest) bool {
	return r.Validate() == nil && other.Validate() == nil && r.fingerprint() == other.fingerprint()
}

// AccessDependencies is supplied by the trusted owning domains, before a Tx.
// Mapping describes parent identities, not cached authorization or existence.
type AccessDependencies struct{ data func() dependencyDetails }
type dependencyDetails struct {
	mapping foundation.Digest
	locks   []foundation.LockRequest
}

func NewAccessDependencies(mapping foundation.Digest, locks []foundation.LockRequest) (AccessDependencies, error) {
	normalized, err := NormalizeAccessLocks(locks)
	if mapping.Validate() != nil || err != nil || len(normalized) == 0 {
		return AccessDependencies{}, bad()
	}
	return AccessDependencies{func() dependencyDetails {
		return dependencyDetails{mapping, append([]foundation.LockRequest(nil), normalized...)}
	}}, nil
}
func (d AccessDependencies) Validate() error {
	if d.data == nil {
		return bad()
	}
	return nil
}
func (d AccessDependencies) Mapping() foundation.Digest {
	if d.data == nil {
		return ""
	}
	return d.data().mapping
}
func (d AccessDependencies) Locks() []foundation.LockRequest {
	if d.data == nil {
		return nil
	}
	return d.data().locks
}
func (d AccessDependencies) Equal(other AccessDependencies) bool {
	return d.Validate() == nil && other.Validate() == nil && d.Mapping() == other.Mapping() && sameAccessLocks(d.Locks(), other.Locks())
}

type AccessPlanner interface {
	Discover(context.Context, AccessRequest) (AccessDependencies, error)
	ValidateInTx(context.Context, foundation.Tx, AccessRequest, AccessDependencies) error
}

// AccessIssuer is private service identity. No projection reveals it; callers
// can create a different issuer, which cannot mint plans/tokens for a service.
type AccessIssuer struct{ data func() foundation.Tx }

func NewAccessIssuer() AccessIssuer {
	id := foundation.NewTx()
	return AccessIssuer{func() foundation.Tx { return id }}
}
func (i AccessIssuer) equal(j AccessIssuer) bool {
	return i.data != nil && j.data != nil && i.data() == j.data()
}

type AccessLockPlan struct{ data func() accessPlan }
type AccessPlanDetails struct {
	Request           AccessRequest
	DependencyRequest AccessRequest
	Dependencies      AccessDependencies
	DomainBinding     foundation.Digest
	Objects           []ObjectID
	Locks             []foundation.LockRequest
}
type accessPlan struct {
	issuer  AccessIssuer
	id      foundation.Tx
	details AccessPlanDetails
}

func NewAccessLockPlan(issuer AccessIssuer, d AccessPlanDetails) (AccessLockPlan, error) {
	if issuer.data == nil || d.Request.Validate() != nil || d.DependencyRequest.Validate() != nil || d.Dependencies.Validate() != nil || d.DomainBinding.Validate() != nil || len(d.Objects) > 100 {
		return AccessLockPlan{}, bad()
	}
	for _, id := range d.Objects {
		if id.Validate() != nil {
			return AccessLockPlan{}, bad()
		}
	}
	locks, err := NormalizeAccessLocks(d.Locks)
	if err != nil || len(locks) == 0 {
		return AccessLockPlan{}, bad()
	}
	d.Locks = locks
	// The service supplies its own locks together with all planner dependencies.
	for _, need := range d.Dependencies.Locks() {
		if !hasAccessLock(d.Locks, need) {
			return AccessLockPlan{}, bad()
		}
	}
	d = copyAccessPlan(d)
	id := foundation.NewTx()
	return AccessLockPlan{func() accessPlan { return accessPlan{issuer, id, copyAccessPlan(d)} }}, nil
}
func copyAccessPlan(d AccessPlanDetails) AccessPlanDetails {
	d.Objects = append([]ObjectID(nil), d.Objects...)
	d.Locks = append([]foundation.LockRequest(nil), d.Locks...)
	return d
}
func (p AccessLockPlan) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p AccessLockPlan) Details() AccessPlanDetails {
	if p.data == nil {
		return AccessPlanDetails{}
	}
	return p.data().details
}
func (p AccessLockPlan) IssuedBy(i AccessIssuer) bool {
	return p.data != nil && p.data().issuer.equal(i)
}

// LockedAccess records the exact plan identities and their original modes,
// plus the complete normalized union and outer extra locks. It is not a grant.
type LockedAccess struct{ data func() lockedAccess }
type lockedAccess struct {
	issuer       AccessIssuer
	tx           foundation.Tx
	plans        []accessPlan
	union, extra []foundation.LockRequest
}

func NewLockedAccess(i AccessIssuer, tx foundation.Tx, plans []AccessLockPlan, extra []foundation.LockRequest) (LockedAccess, error) {
	if i.data == nil || !tx.Valid() || len(plans) == 0 {
		return LockedAccess{}, bad()
	}
	normalized, err := NormalizeAccessLocks(extra)
	if err != nil {
		return LockedAccess{}, bad()
	}
	captured := make([]accessPlan, 0, len(plans))
	union := append([]foundation.LockRequest(nil), normalized...)
	seen := map[foundation.Tx]bool{}
	for _, p := range plans {
		if !p.IssuedBy(i) {
			return LockedAccess{}, bad()
		}
		v := p.data()
		if seen[v.id] {
			return LockedAccess{}, bad()
		}
		seen[v.id] = true
		captured = append(captured, v)
		union = append(union, v.details.Locks...)
	}
	union, err = NormalizeAccessLocks(union)
	if err != nil {
		return LockedAccess{}, bad()
	}
	v := lockedAccess{i, tx, captured, union, normalized}
	return LockedAccess{func() lockedAccess { return v }}, nil
}
func (l LockedAccess) Matches(i AccessIssuer, tx foundation.Tx, p AccessLockPlan, request AccessRequest) bool {
	if l.data == nil || !p.IssuedBy(i) || !p.Details().Request.Equal(request) {
		return false
	}
	v := l.data()
	if !v.issuer.equal(i) || v.tx != tx {
		return false
	}
	for _, candidate := range v.plans {
		if candidate.id == p.data().id {
			return candidate.details.Request.Equal(request) && sameAccessLocks(candidate.details.Locks, p.Details().Locks)
		}
	}
	return false
}
func (l LockedAccess) Locks() []foundation.LockRequest {
	if l.data == nil {
		return nil
	}
	return append([]foundation.LockRequest(nil), l.data().union...)
}
func NormalizeAccessLocks(in []foundation.LockRequest) ([]foundation.LockRequest, error) {
	byKey := map[string]foundation.LockRequest{}
	for _, r := range in {
		if r.Key.Validate() != nil || !r.Mode.Valid() {
			return nil, bad()
		}
		key := r.Key.Canonical()
		if byKey[key].Mode != foundation.Exclusive {
			byKey[key] = r
		}
	}
	out := make([]foundation.LockRequest, 0, len(byKey))
	for _, r := range byKey {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b foundation.LockRequest) int { return foundation.CompareLockKeys(a.Key, b.Key) })
	return out, nil
}
func sameAccessLocks(a, b []foundation.LockRequest) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key.Canonical() != b[i].Key.Canonical() || a[i].Mode != b[i].Mode {
			return false
		}
	}
	return true
}
func hasAccessLock(held []foundation.LockRequest, need foundation.LockRequest) bool {
	for _, h := range held {
		if h.Key.Canonical() == need.Key.Canonical() && (h.Mode == foundation.Exclusive || h.Mode == need.Mode) {
			return true
		}
	}
	return false
}

type AccessPlanning interface {
	DiscoverAccess(context.Context, AccessRequest) (AccessLockPlan, error)
	AcquireAccessPlansInTx(context.Context, foundation.Tx, []AccessLockPlan, []foundation.LockRequest) (LockedAccess, error)
	ValidateAccessPlanInTx(context.Context, foundation.Tx, AccessRequest, AccessLockPlan, LockedAccess) error
}

func (AccessRequest) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_access_request") }
func (AccessRequest) MarshalJSON() ([]byte, error) { return []byte(`"object_access_request"`), nil }
func (*AccessRequest) UnmarshalJSON([]byte) error  { return bad() }
func (AccessRequest) LogValue() slog.Value         { return slog.StringValue("object_access_request") }

func (AccessDependencies) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_access_dependencies")
}
func (AccessDependencies) MarshalJSON() ([]byte, error) {
	return []byte(`"object_access_dependencies"`), nil
}
func (*AccessDependencies) UnmarshalJSON([]byte) error { return bad() }
func (AccessDependencies) LogValue() slog.Value {
	return slog.StringValue("object_access_dependencies")
}

func (AccessIssuer) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_access_issuer") }
func (AccessIssuer) MarshalJSON() ([]byte, error) { return []byte(`"object_access_issuer"`), nil }
func (*AccessIssuer) UnmarshalJSON([]byte) error  { return bad() }
func (AccessIssuer) LogValue() slog.Value         { return slog.StringValue("object_access_issuer") }

func (AccessLockPlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_access_plan") }
func (AccessLockPlan) MarshalJSON() ([]byte, error) { return []byte(`"object_access_plan"`), nil }
func (*AccessLockPlan) UnmarshalJSON([]byte) error  { return bad() }
func (AccessLockPlan) LogValue() slog.Value         { return slog.StringValue("object_access_plan") }

func (LockedAccess) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_locked_access") }
func (LockedAccess) MarshalJSON() ([]byte, error) { return []byte(`"object_locked_access"`), nil }
func (*LockedAccess) UnmarshalJSON([]byte) error  { return bad() }
func (LockedAccess) LogValue() slog.Value         { return slog.StringValue("object_locked_access") }
