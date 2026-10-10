package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Resource contracts contain no Agent implementation and issue no material or
// execution grant. Providers must bind an issuer to one Store and check the
// caller's live Tx, the complete held lock union and current authority in Tx.
type SecretDirectoryRequest struct {
	Actor     i.Actor
	ProjectID ProjectID
	Command   f.CommandIdentity
	IDs       []VariableID
}

func validateSecretIDs(ids []VariableID) error {
	if len(ids) > MaxSecretReferences {
		return invalid("", "INVALID_SECRET_IDS")
	}
	for n, id := range ids {
		if id.Validate() != nil || n > 0 && ids[n-1].String() >= id.String() {
			return invalid("", "INVALID_SECRET_IDS")
		}
	}
	return nil
}
func validateSecretAgentCommand(project ProjectID, command f.CommandIdentity) error {
	if project.Validate() != nil || command.Validate() != nil || command.Namespace() != "project" || !slices.Equal(command.OwnerIDs(), []string{project.String()}) || (command.Command() != "agent.create" && command.Command() != "agent.update") {
		return invalid("", "INVALID_AGENT_COMMAND")
	}
	return nil
}
func (r SecretDirectoryRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != i.Human || validateSecretAgentCommand(r.ProjectID, r.Command) != nil {
		return invalid("", "INVALID_SECRET_DIRECTORY")
	}
	return validateSecretIDs(r.IDs)
}
func (r SecretDirectoryRequest) Clone() SecretDirectoryRequest { r.IDs = slices.Clone(r.IDs); return r }
func SecretDirectoryBinding(r SecretDirectoryRequest) (f.Digest, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return secretBinding(struct {
		Format  string
		Actor   i.ActorDetails
		Project ProjectID
		Command string
		IDs     []VariableID
	}{"secret-directory-v1", r.Actor.Details(), r.ProjectID, r.Command.Canonical(), append([]VariableID{}, r.IDs...)})
}
func secretBinding(v any) (f.Digest, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", invalid("", "INVALID_SECRET_BINDING")
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

type SecretDirectoryStatus string

const (
	SecretDirectoryValid      SecretDirectoryStatus = "valid"
	SecretDirectoryRemoved    SecretDirectoryStatus = "removed"
	SecretDirectoryNotInScope SecretDirectoryStatus = "not_in_scope"
)

type SecretDirectoryEntry struct {
	ID       VariableID
	Status   SecretDirectoryStatus
	Variable *SecretVariable
}

func cloneSecretEntry(v SecretDirectoryEntry) SecretDirectoryEntry {
	if v.Variable != nil {
		copy := v.Variable.Clone()
		v.Variable = &copy
	}
	return v
}

type SecretDirectoryFacts struct{ data func() []SecretDirectoryEntry }

// Missing and foreign-project IDs must both be NotInScope. Removed is only for
// a tombstone proved in this Project. No caller may treat these facts as a grant.
func NewSecretDirectoryFacts(request SecretDirectoryRequest, entries []SecretDirectoryEntry) (SecretDirectoryFacts, error) {
	if request.Validate() != nil || len(entries) != len(request.IDs) {
		return SecretDirectoryFacts{}, invalid("", "INVALID_SECRET_DIRECTORY")
	}
	copy := make([]SecretDirectoryEntry, len(entries))
	for n, e := range entries {
		if e.ID != request.IDs[n] {
			return SecretDirectoryFacts{}, invalid("", "INVALID_SECRET_DIRECTORY")
		}
		switch e.Status {
		case SecretDirectoryValid:
			if e.Variable == nil || e.Variable.Validate() != nil || e.Variable.Fields().ID != e.ID || e.Variable.Fields().ProjectID != request.ProjectID {
				return SecretDirectoryFacts{}, invalid("", "INVALID_SECRET_DIRECTORY")
			}
		case SecretDirectoryRemoved, SecretDirectoryNotInScope:
			if e.Variable != nil {
				return SecretDirectoryFacts{}, invalid("", "INVALID_SECRET_DIRECTORY")
			}
		default:
			return SecretDirectoryFacts{}, invalid("", "INVALID_SECRET_DIRECTORY")
		}
		copy[n] = cloneSecretEntry(e)
	}
	return SecretDirectoryFacts{func() []SecretDirectoryEntry {
		out := make([]SecretDirectoryEntry, len(copy))
		for n, e := range copy {
			out[n] = cloneSecretEntry(e)
		}
		return out
	}}, nil
}
func (v SecretDirectoryFacts) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_SECRET_DIRECTORY")
	}
	return nil
}
func (v SecretDirectoryFacts) Entries() []SecretDirectoryEntry {
	if v.data == nil {
		return nil
	}
	return v.data()
}

type SecretReferenceOperation string

const (
	SecretReferenceCreate SecretReferenceOperation = "create"
	SecretReferenceUpdate SecretReferenceOperation = "update"
)

type SecretReferenceChange struct {
	Actor                i.Actor
	ProjectID            ProjectID
	AgentID              i.AgentID
	Command              f.CommandIdentity
	Operation            SecretReferenceOperation
	ExpectedOwnerVersion *f.Version
	ResultOwnerVersion   f.Version
	Before, After        []VariableID
}

func (r SecretReferenceChange) Validate() error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != i.Human || r.AgentID.Validate() != nil || validateSecretAgentCommand(r.ProjectID, r.Command) != nil || r.ResultOwnerVersion.Validate() != nil || validateSecretIDs(r.Before) != nil || validateSecretIDs(r.After) != nil {
		return invalid("", "INVALID_SECRET_REFERENCE")
	}
	switch r.Operation {
	case SecretReferenceCreate:
		if r.Command.Command() != "agent.create" || r.ExpectedOwnerVersion != nil || r.ResultOwnerVersion != 1 || len(r.Before) != 0 {
			return invalid("", "INVALID_SECRET_REFERENCE")
		}
	case SecretReferenceUpdate:
		if r.Command.Command() != "agent.update" || r.ExpectedOwnerVersion == nil || r.ExpectedOwnerVersion.Validate() != nil || r.ResultOwnerVersion < *r.ExpectedOwnerVersion || r.ResultOwnerVersion-*r.ExpectedOwnerVersion > 1 || !slices.Equal(r.Before, r.After) && r.ResultOwnerVersion == *r.ExpectedOwnerVersion {
			return invalid("", "INVALID_SECRET_REFERENCE")
		}
	default:
		return invalid("", "INVALID_SECRET_REFERENCE")
	}
	return nil
}
func (r SecretReferenceChange) Clone() SecretReferenceChange {
	r.Before = slices.Clone(r.Before)
	r.After = slices.Clone(r.After)
	if r.ExpectedOwnerVersion != nil {
		v := *r.ExpectedOwnerVersion
		r.ExpectedOwnerVersion = &v
	}
	return r
}
func SecretReferenceBinding(r SecretReferenceChange) (f.Digest, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return secretBinding(struct {
		Format        string
		Actor         i.ActorDetails
		Project       ProjectID
		Agent         i.AgentID
		Command       string
		Operation     SecretReferenceOperation
		Expected      *f.Version
		Result        f.Version
		Before, After []VariableID
	}{"secret-reference-v1", r.Actor.Details(), r.ProjectID, r.AgentID, r.Command.Canonical(), r.Operation, r.ExpectedOwnerVersion, r.ResultOwnerVersion, append([]VariableID{}, r.Before...), append([]VariableID{}, r.After...)})
}

type SecretPlanIssuer struct{ token *byte }

func NewSecretPlanIssuer() SecretPlanIssuer { return SecretPlanIssuer{new(byte)} }
func (v SecretPlanIssuer) Valid() bool      { return v.token != nil }

type SecretPlanDetails struct {
	Binding, Mapping f.Digest
	Locks            []f.LockRequest
}

func (v SecretPlanDetails) clone() SecretPlanDetails { v.Locks = slices.Clone(v.Locks); return v }

type secretPlanData struct {
	issuer  SecretPlanIssuer
	details SecretPlanDetails
}

func newSecretPlan(issuer SecretPlanIssuer, binding, mapping f.Digest, locks []f.LockRequest) (secretPlanData, error) {
	if !issuer.Valid() || binding.Validate() != nil || mapping.Validate() != nil || len(locks) == 0 {
		return secretPlanData{}, invalid("", "INVALID_SECRET_PLAN")
	}
	copy := slices.Clone(locks)
	for _, l := range copy {
		if l.Key.Validate() != nil || !l.Mode.Valid() {
			return secretPlanData{}, invalid("", "INVALID_SECRET_PLAN")
		}
	}
	slices.SortFunc(copy, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	merged := copy[:0]
	for _, l := range copy {
		if len(merged) > 0 && f.CompareLockKeys(merged[len(merged)-1].Key, l.Key) == 0 {
			if l.Mode == f.Exclusive {
				merged[len(merged)-1].Mode = f.Exclusive
			}
		} else {
			merged = append(merged, l)
		}
	}
	return secretPlanData{issuer, SecretPlanDetails{binding, mapping, merged}}, nil
}
func secretLocksCover(all, required []f.LockRequest) bool {
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
func secretBaseLocks(actor i.Actor, project ProjectID, command f.CommandIdentity, agent *i.AgentID) []f.LockRequest {
	u, _ := f.UserLock(actor.Details().UserID)
	p, _ := f.ProjectLock(project.String())
	c, _ := f.CommandLock(command)
	locks := []f.LockRequest{{Key: c, Mode: f.Exclusive}, {Key: u, Mode: f.Exclusive}, {Key: p, Mode: f.Shared}}
	if agent != nil {
		a, _ := f.AgentLock(agent.String())
		locks = append(locks, f.LockRequest{Key: a, Mode: f.Exclusive})
	}
	return locks
}

type SecretDirectoryPlan struct{ data func() secretPlanData }

func NewSecretDirectoryPlan(issuer SecretPlanIssuer, r SecretDirectoryRequest, mapping f.Digest, locks []f.LockRequest) (SecretDirectoryPlan, error) {
	b, err := SecretDirectoryBinding(r)
	if err != nil {
		return SecretDirectoryPlan{}, err
	}
	d, err := newSecretPlan(issuer, b, mapping, locks)
	if err != nil || !secretLocksCover(d.details.Locks, secretBaseLocks(r.Actor, r.ProjectID, r.Command, nil)) {
		return SecretDirectoryPlan{}, invalid("", "INVALID_SECRET_PLAN")
	}
	return SecretDirectoryPlan{func() secretPlanData { return d }}, nil
}
func (v SecretDirectoryPlan) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_SECRET_PLAN")
	}
	return nil
}
func (v SecretDirectoryPlan) Details() SecretPlanDetails {
	if v.data == nil {
		return SecretPlanDetails{}
	}
	return v.data().details.clone()
}
func (v SecretDirectoryPlan) Matches(issuer SecretPlanIssuer, binding, mapping f.Digest) bool {
	return issuer.Valid() && v.data != nil && v.data().issuer == issuer && v.data().details.Binding == binding && v.data().details.Mapping == mapping
}
func (v SecretDirectoryPlan) RequiredLocks() []f.LockRequest { return v.Details().Locks }

// OwnerPlan can be validated only by its original F1 provider. Shape validation
// is not proof of the old canonical, postimage, initialization or transaction.
type SecretReferenceOwnerPlan struct{ data func() secretPlanData }

func NewSecretReferenceOwnerPlan(issuer SecretPlanIssuer, r SecretReferenceChange, mapping f.Digest, locks []f.LockRequest) (SecretReferenceOwnerPlan, error) {
	b, err := SecretReferenceBinding(r)
	if err != nil {
		return SecretReferenceOwnerPlan{}, err
	}
	d, err := newSecretPlan(issuer, b, mapping, locks)
	if err != nil || !secretLocksCover(d.details.Locks, secretBaseLocks(r.Actor, r.ProjectID, r.Command, &r.AgentID)) {
		return SecretReferenceOwnerPlan{}, invalid("", "INVALID_SECRET_PLAN")
	}
	return SecretReferenceOwnerPlan{func() secretPlanData { return d }}, nil
}
func (v SecretReferenceOwnerPlan) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_SECRET_PLAN")
	}
	return nil
}
func (v SecretReferenceOwnerPlan) Details() SecretPlanDetails {
	if v.data == nil {
		return SecretPlanDetails{}
	}
	return v.data().details.clone()
}
func (v SecretReferenceOwnerPlan) Matches(issuer SecretPlanIssuer, binding, mapping f.Digest) bool {
	return issuer.Valid() && v.data != nil && v.data().issuer == issuer && v.data().details.Binding == binding && v.data().details.Mapping == mapping
}
func (v SecretReferenceOwnerPlan) RequiredLocks() []f.LockRequest { return v.Details().Locks }

type secretReferencePlanData struct {
	plan  secretPlanData
	owner SecretReferenceOwnerPlan
}
type SecretReferencePlan struct {
	data func() secretReferencePlanData
}

func NewSecretReferencePlan(issuer SecretPlanIssuer, r SecretReferenceChange, mapping f.Digest, locks []f.LockRequest, owner SecretReferenceOwnerPlan) (SecretReferencePlan, error) {
	b, err := SecretReferenceBinding(r)
	if err != nil {
		return SecretReferencePlan{}, err
	}
	d, err := newSecretPlan(issuer, b, mapping, locks)
	if err != nil || owner.Validate() != nil || owner.Details().Binding != b || !secretLocksCover(d.details.Locks, owner.RequiredLocks()) {
		return SecretReferencePlan{}, invalid("", "INVALID_SECRET_PLAN")
	}
	return SecretReferencePlan{func() secretReferencePlanData { return secretReferencePlanData{d, owner} }}, nil
}
func (v SecretReferencePlan) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_SECRET_PLAN")
	}
	return nil
}
func (v SecretReferencePlan) Details() SecretPlanDetails {
	if v.data == nil {
		return SecretPlanDetails{}
	}
	return v.data().plan.details.clone()
}
func (v SecretReferencePlan) Matches(issuer SecretPlanIssuer, binding, mapping f.Digest) bool {
	return issuer.Valid() && v.data != nil && v.data().plan.issuer == issuer && v.data().plan.details.Binding == binding && v.data().plan.details.Mapping == mapping
}
func (v SecretReferencePlan) RequiredLocks() []f.LockRequest { return v.Details().Locks }
func (v SecretReferencePlan) OwnerPlan() SecretReferenceOwnerPlan {
	if v.data == nil {
		return SecretReferenceOwnerPlan{}
	}
	return v.data().owner
}

type SecretDirectory interface {
	DiscoverSecretVariables(context.Context, SecretDirectoryRequest) (SecretDirectoryPlan, error)
	RequireSecretVariablesInTx(context.Context, f.Tx, SecretDirectoryRequest, SecretDirectoryPlan) (SecretDirectoryFacts, error)
}
type SecretReferences interface {
	DiscoverSecretReferences(context.Context, SecretReferenceChange) (SecretReferencePlan, error)
	ApplySecretReferencesInTx(context.Context, f.Tx, SecretReferenceChange, SecretReferencePlan) error
}

// F1 is the only real provider. CheckAppliedInTx verifies exact canonical
// postimage and the same-Tx private preimage/create witness; no provider here.
type SecretReferenceOwnerAuthority interface {
	Discover(context.Context, SecretReferenceChange) (SecretReferenceOwnerPlan, error)
	CheckAppliedInTx(context.Context, f.Tx, SecretReferenceChange, SecretReferenceOwnerPlan) error
}

func (SecretDirectoryRequest) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretDirectoryRequest) LogValue() slog.Value       { return secretLog() }
func (SecretReferenceChange) Format(w fmt.State, _ rune)  { secretSafe(w) }
func (SecretReferenceChange) LogValue() slog.Value        { return secretLog() }
func (SecretPlanDetails) Format(w fmt.State, _ rune)      { secretSafe(w) }
func (SecretPlanDetails) LogValue() slog.Value            { return secretLog() }
func (SecretDirectoryFacts) Format(w fmt.State, _ rune)   { secretSafe(w) }
func (SecretDirectoryFacts) LogValue() slog.Value         { return secretLog() }
func (SecretDirectoryFacts) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_directory_facts"`), nil
}
func (*SecretDirectoryFacts) UnmarshalJSON([]byte) error {
	return invalid("", "INVALID_SECRET_DIRECTORY")
}
func (SecretPlanIssuer) Format(w fmt.State, _ rune)    { secretSafe(w) }
func (SecretPlanIssuer) LogValue() slog.Value          { return secretLog() }
func (SecretPlanIssuer) MarshalJSON() ([]byte, error)  { return []byte(`"secret_plan_issuer"`), nil }
func (*SecretPlanIssuer) UnmarshalJSON([]byte) error   { return invalid("", "INVALID_SECRET_PLAN") }
func (SecretDirectoryPlan) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretDirectoryPlan) LogValue() slog.Value       { return secretLog() }
func (SecretDirectoryPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_directory_plan"`), nil
}
func (*SecretDirectoryPlan) UnmarshalJSON([]byte) error     { return invalid("", "INVALID_SECRET_PLAN") }
func (SecretReferenceOwnerPlan) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretReferenceOwnerPlan) LogValue() slog.Value       { return secretLog() }
func (SecretReferenceOwnerPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_reference_owner_plan"`), nil
}
func (*SecretReferenceOwnerPlan) UnmarshalJSON([]byte) error {
	return invalid("", "INVALID_SECRET_PLAN")
}
func (SecretReferencePlan) Format(w fmt.State, _ rune) { secretSafe(w) }
func (SecretReferencePlan) LogValue() slog.Value       { return secretLog() }
func (SecretReferencePlan) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_reference_plan"`), nil
}
func (*SecretReferencePlan) UnmarshalJSON([]byte) error { return invalid("", "INVALID_SECRET_PLAN") }
