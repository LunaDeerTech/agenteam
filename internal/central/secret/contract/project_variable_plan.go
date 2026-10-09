package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ProjectVariableReceipt struct{}
type ProjectVariableReceiptID = f.ID[ProjectVariableReceipt]
type ProjectVariableWriteStage string

const (
	ProjectVariableReceiptRead ProjectVariableWriteStage = "receipt_read"
	ProjectVariableNewWrite    ProjectVariableWriteStage = "new_write"
)

func (s ProjectVariableWriteStage) Valid() bool {
	return s == ProjectVariableReceiptRead || s == ProjectVariableNewWrite
}

type ProjectVariableEffect string

const (
	ProjectVariableCreated   ProjectVariableEffect = "create"
	ProjectVariableReplaced  ProjectVariableEffect = "replace"
	ProjectVariableDeleted   ProjectVariableEffect = "delete"
	ProjectVariableUnchanged ProjectVariableEffect = "none"
)

// Results retain the original stable writer and external expected version.
// They contain no Session, material, semantic fingerprint or sealed data.
type ProjectVariableWriteResultFields struct {
	ReceiptID       ProjectVariableReceiptID
	ProjectID       i.ProjectID
	VariableID      i.ProjectVariableID
	UserID          i.UserID
	Identity        f.CommandIdentity
	Kind            MutationKind
	ExpectedVersion *f.Version
	Ref             CredentialRef
	Effect          ProjectVariableEffect
	Version         f.Version
	Deleted         bool
}

func cloneProjectVariableResult(v ProjectVariableWriteResultFields) ProjectVariableWriteResultFields {
	if v.ExpectedVersion != nil {
		n := *v.ExpectedVersion
		v.ExpectedVersion = &n
	}
	return v
}
func projectVariableRefInProject(ref CredentialRef, project i.ProjectID) bool {
	return ref.Validate() == nil && project.Validate() == nil && ref.Details().Scope.Details().Kind == i.ProjectScope && ref.Details().Scope.Details().ProjectID == project.String()
}
func validateProjectVariableResult(v ProjectVariableWriteResultFields) error {
	if v.ReceiptID.Validate() != nil || v.ProjectID.Validate() != nil || v.VariableID.Validate() != nil || v.UserID.Validate() != nil || v.Identity.Validate() != nil || v.Identity.Namespace() != "projectvariable" || !slices.Equal(v.Identity.OwnerIDs(), []string{v.ProjectID.String()}) || v.Identity.Command() != "project.secret_variable."+string(v.Kind) || !projectVariableRefInProject(v.Ref, v.ProjectID) || v.Version.Validate() != nil {
		return bad()
	}
	if (v.Kind == Create) != (v.ExpectedVersion == nil) || v.ExpectedVersion != nil && v.ExpectedVersion.Validate() != nil {
		return bad()
	}
	switch v.Kind {
	case Create:
		if v.Effect != ProjectVariableCreated || v.Version != 1 || v.Deleted {
			return bad()
		}
	case Update:
		if v.Deleted || v.Effect != ProjectVariableReplaced && v.Effect != ProjectVariableUnchanged || v.Effect == ProjectVariableReplaced && v.Version < 2 {
			return bad()
		}
	case Delete:
		if v.Effect != ProjectVariableDeleted || !v.Deleted || v.Version < 2 {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}

type projectVariableObservation struct {
	observed bool
	result   ProjectVariableWriteResultFields
}
type ProjectVariableWriteObservation struct {
	data func() projectVariableObservation
}

func ProjectVariableWriteNotObserved() ProjectVariableWriteObservation {
	return ProjectVariableWriteObservation{func() projectVariableObservation { return projectVariableObservation{} }}
}
func NewProjectVariableWriteObservation(v ProjectVariableWriteResultFields) (ProjectVariableWriteObservation, error) {
	if err := validateProjectVariableResult(v); err != nil {
		return ProjectVariableWriteObservation{}, err
	}
	v = cloneProjectVariableResult(v)
	return ProjectVariableWriteObservation{func() projectVariableObservation {
		return projectVariableObservation{true, cloneProjectVariableResult(v)}
	}}, nil
}
func (v ProjectVariableWriteObservation) Validate() error {
	if v.data == nil {
		return bad()
	}
	return nil
}
func (v ProjectVariableWriteObservation) Observed() bool { return v.data != nil && v.data().observed }
func (v ProjectVariableWriteObservation) Result() (ProjectVariableWriteResultFields, error) {
	if !v.Observed() {
		return ProjectVariableWriteResultFields{}, bad()
	}
	return v.data().result, nil
}

// A historical basis binds the original completed result, not a canonical row
// that may have changed or been deleted. New-write bases bind a current preimage.
type ProjectVariableWriteBasisFields struct {
	Request           ProjectVariableWriteRequest
	Ref               CredentialRef
	VariableVersion   f.Version
	CredentialVersion f.Version
	Receipt           ProjectVariableWriteObservation
}
type projectVariableWritePlanData struct {
	issuer PlanIssuer
	basis  ProjectVariableWriteBasisFields
	locks  []f.LockRequest
}
type ProjectVariableWritePlan struct {
	data func() projectVariableWritePlanData
}

func sameProjectVariableExpected(a, b *f.Version) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func sameProjectVariableRequest(a, b ProjectVariableWriteRequest) bool {
	if a.Validate() != nil || b.Validate() != nil {
		return false
	}
	x, y := a.Fields(), b.Fields()
	return x.Actor.Equal(y.Actor) && x.ProjectID == y.ProjectID && x.VariableID == y.VariableID && x.Identity.Canonical() == y.Identity.Canonical() && x.Kind == y.Kind && sameProjectVariableExpected(x.ExpectedVersion, y.ExpectedVersion)
}
func projectVariableResultMatches(v ProjectVariableWriteResultFields, r ProjectVariableWriteRequest) bool {
	if r.Validate() != nil {
		return false
	}
	q := r.Fields()
	return v.ProjectID == q.ProjectID && v.VariableID == q.VariableID && v.UserID.String() == q.Actor.Details().UserID && v.Identity.Canonical() == q.Identity.Canonical() && v.Kind == q.Kind && sameProjectVariableExpected(v.ExpectedVersion, q.ExpectedVersion)
}

// Minimum locks are a structural requirement, not evidence of actual ownership.
// The bound provider must require these and its complete additional union in Tx.
func ProjectVariableWriteLocks(request ProjectVariableWriteRequest, ref CredentialRef) ([]f.LockRequest, error) {
	if request.Validate() != nil || !projectVariableRefInProject(ref, request.Fields().ProjectID) {
		return nil, bad()
	}
	r := request.Fields()
	c, _ := f.CommandLock(r.Identity)
	u, _ := f.UserLock(r.Actor.Details().UserID)
	p, _ := f.ProjectLock(r.ProjectID.String())
	w, _ := f.SystemConfigLock("secret-write-key")
	v, _ := f.AggregateLock(f.CredentialRefAggregate, ref.Details().ID.String())
	return normalizePlanLocks([]f.LockRequest{{Key: c, Mode: f.Exclusive}, {Key: u, Mode: f.Exclusive}, {Key: p, Mode: f.Exclusive}, {Key: w, Mode: f.Shared}, {Key: v, Mode: f.Exclusive}})
}
func projectVariableLocksCover(all, required []f.LockRequest) bool {
	for _, r := range required {
		found := false
		for _, l := range all {
			if f.CompareLockKeys(l.Key, r.Key) == 0 && (l.Mode == f.Exclusive || l.Mode == r.Mode) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func NewProjectVariableWritePlan(issuer PlanIssuer, basis ProjectVariableWriteBasisFields, locks []f.LockRequest) (ProjectVariableWritePlan, error) {
	if !issuer.Valid() || basis.Request.Validate() != nil || !projectVariableRefInProject(basis.Ref, basis.Request.Fields().ProjectID) || basis.Receipt.Validate() != nil {
		return ProjectVariableWritePlan{}, bad()
	}
	if basis.Receipt.Observed() {
		r, _ := basis.Receipt.Result()
		if !projectVariableResultMatches(r, basis.Request) || !r.Ref.Equal(basis.Ref) || basis.VariableVersion != 0 || basis.CredentialVersion != r.Version {
			return ProjectVariableWritePlan{}, bad()
		}
	} else if basis.Request.Fields().Kind == Create {
		if basis.VariableVersion != 0 || basis.CredentialVersion != 0 {
			return ProjectVariableWritePlan{}, bad()
		}
	} else if basis.VariableVersion.Validate() != nil || basis.CredentialVersion.Validate() != nil {
		return ProjectVariableWritePlan{}, bad()
	}
	locks, err := normalizePlanLocks(locks)
	if err != nil {
		return ProjectVariableWritePlan{}, err
	}
	required, err := ProjectVariableWriteLocks(basis.Request, basis.Ref)
	if err != nil || !projectVariableLocksCover(locks, required) {
		return ProjectVariableWritePlan{}, bad()
	}
	d := projectVariableWritePlanData{issuer, basis, locks}
	return ProjectVariableWritePlan{func() projectVariableWritePlanData { n := d; n.locks = slices.Clone(d.locks); return n }}, nil
}
func (v ProjectVariableWritePlan) Validate() error {
	if v.data == nil {
		return bad()
	}
	return nil
}
func (v ProjectVariableWritePlan) Basis() (ProjectVariableWriteBasisFields, error) {
	if v.data == nil {
		return ProjectVariableWriteBasisFields{}, bad()
	}
	return v.data().basis, nil
}
func (v ProjectVariableWritePlan) RequiredLocks() ([]f.LockRequest, error) {
	if v.data == nil {
		return nil, bad()
	}
	return v.data().locks, nil
}

// Matches proves only private issuer + immutable original request binding. It
// cannot establish current rights, database freshness, or an actual held lock.
func (v ProjectVariableWritePlan) Matches(issuer PlanIssuer, request ProjectVariableWriteRequest) bool {
	return issuer.Valid() && v.data != nil && v.data().issuer == issuer && sameProjectVariableRequest(v.data().basis.Request, request)
}

type ProjectVariablePreparationFields struct {
	Request   ProjectVariableWriteRequest
	ReceiptID ProjectVariableReceiptID
	Ref       CredentialRef
}
type ProjectVariablePreparation struct {
	data func() ProjectVariablePreparationFields
}

// This is a safe projection only; the producer must reject forged interface
// implementations before calling any method or trusting this projection.
func NewProjectVariablePreparation(v ProjectVariablePreparationFields) (ProjectVariablePreparation, error) {
	if v.Request.Validate() != nil || v.ReceiptID.Validate() != nil || !projectVariableRefInProject(v.Ref, v.Request.Fields().ProjectID) {
		return ProjectVariablePreparation{}, bad()
	}
	return ProjectVariablePreparation{func() ProjectVariablePreparationFields { return v }}, nil
}
func (v ProjectVariablePreparation) Fields() (ProjectVariablePreparationFields, error) {
	if v.data == nil {
		return ProjectVariablePreparationFields{}, bad()
	}
	return v.data(), nil
}

type PreparedProjectVariableWrite interface {
	Preparation() (ProjectVariablePreparation, error)
	RequiredLocks() ([]f.LockRequest, error)
	Destroy()
	fmt.Formatter
	slog.LogValuer
	json.Marshaler
}
type ProjectVariableWriteAuthority interface {
	Discover(context.Context, ProjectVariableWriteRequest) (ProjectVariableWritePlan, error)
	CheckPlan(ProjectVariableWriteRequest, ProjectVariableWritePlan) error
	CheckInTx(context.Context, f.Tx, ProjectVariableWriteRequest, ProjectVariableWritePlan, ProjectVariableWriteStage) error
}
type ProjectVariableWrites interface {
	PrepareProjectVariableWrite(context.Context, ProjectVariableIntent, ProjectVariableWritePlan) (PreparedProjectVariableWrite, error)
	MatchProjectVariableIntentInTx(context.Context, f.Tx, PreparedProjectVariableWrite) (ProjectVariableWriteObservation, error)
	ApplyProjectVariableWriteInTx(context.Context, f.Tx, PreparedProjectVariableWrite) (ProjectVariableWriteObservation, error)
	LookupProjectVariableWriteInTx(context.Context, f.Tx, ProjectVariableWriteRequest, ProjectVariableWritePlan) (ProjectVariableWriteObservation, error)
}

func (ProjectVariableWritePlan) Format(w fmt.State, _ rune)           { projectVariableSafe(w) }
func (ProjectVariableWritePlan) LogValue() slog.Value                 { return projectVariableLog() }
func (ProjectVariableWritePlan) MarshalJSON() ([]byte, error)         { return projectVariableJSON() }
func (*ProjectVariableWritePlan) UnmarshalJSON([]byte) error          { return bad() }
func (ProjectVariableWriteObservation) Format(w fmt.State, _ rune)    { projectVariableSafe(w) }
func (ProjectVariableWriteObservation) LogValue() slog.Value          { return projectVariableLog() }
func (ProjectVariableWriteObservation) MarshalJSON() ([]byte, error)  { return projectVariableJSON() }
func (*ProjectVariableWriteObservation) UnmarshalJSON([]byte) error   { return bad() }
func (ProjectVariableWriteResultFields) Format(w fmt.State, _ rune)   { projectVariableSafe(w) }
func (ProjectVariableWriteResultFields) LogValue() slog.Value         { return projectVariableLog() }
func (ProjectVariableWriteResultFields) MarshalJSON() ([]byte, error) { return projectVariableJSON() }
func (ProjectVariablePreparation) Format(w fmt.State, _ rune)         { projectVariableSafe(w) }
func (ProjectVariablePreparation) LogValue() slog.Value               { return projectVariableLog() }
func (ProjectVariablePreparation) MarshalJSON() ([]byte, error)       { return projectVariableJSON() }
func (*ProjectVariablePreparation) UnmarshalJSON([]byte) error        { return bad() }
func (ProjectVariablePreparationFields) Format(w fmt.State, _ rune)   { projectVariableSafe(w) }
func (ProjectVariablePreparationFields) LogValue() slog.Value         { return projectVariableLog() }
func (ProjectVariablePreparationFields) MarshalJSON() ([]byte, error) { return projectVariableJSON() }
func (ProjectVariableWriteBasisFields) Format(w fmt.State, _ rune)    { projectVariableSafe(w) }
func (ProjectVariableWriteBasisFields) LogValue() slog.Value          { return projectVariableLog() }
func (ProjectVariableWriteBasisFields) MarshalJSON() ([]byte, error)  { return projectVariableJSON() }
