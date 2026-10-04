package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"strconv"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type AccountOwnerKind string

const (
	InvitationMaterial    AccountOwnerKind = "invitation"
	PasswordResetMaterial AccountOwnerKind = "password_reset"
	LoginResponseMaterial AccountOwnerKind = "login_response"
	SMTPSettingsMaterial  AccountOwnerKind = "smtp_settings"
)

type ServiceWriteFields struct {
	Actor           identity.Actor
	Scope           identity.Scope
	Identity        foundation.CommandIdentity
	Kind            MutationKind
	Ref             CredentialRef
	ExpectedVersion foundation.Version
	Purpose         Purpose
	OwnerKind       AccountOwnerKind
	OwnerID         string
	Value           SecretMaterial
}
type serviceWriteData struct {
	fields      ServiceWriteFields
	valueDigest string
}
type ServiceWriteRequest struct{ data func() serviceWriteData }

func NewServiceWriteRequest(f ServiceWriteFields) (ServiceWriteRequest, error) {
	a := f.Actor.Details()
	if f.Actor.Validate() != nil || a.Kind != identity.Service || f.Scope.Validate() != nil || f.Scope.Details().Kind != identity.System || a.ProjectID != "" || !validID(f.OwnerID) || f.Identity.Validate() != nil || f.Identity.Namespace() != "secret.account" || f.Identity.Command() != string(f.Kind) || !slices.Equal(f.Identity.OwnerIDs(), []string{f.OwnerID}) {
		return ServiceWriteRequest{}, bad()
	}
	if f.OwnerKind != InvitationMaterial && f.OwnerKind != PasswordResetMaterial && f.OwnerKind != LoginResponseMaterial && f.OwnerKind != SMTPSettingsMaterial {
		return ServiceWriteRequest{}, bad()
	}
	if f.Purpose != System && f.Purpose != SMTP || f.OwnerKind == SMTPSettingsMaterial && f.Purpose != SMTP || f.OwnerKind != SMTPSettingsMaterial && f.Purpose != System {
		return ServiceWriteRequest{}, bad()
	}
	switch a.ServiceName {
	case identity.AccountAuth:
		if f.Kind != Create || f.OwnerKind == SMTPSettingsMaterial {
			return ServiceWriteRequest{}, bad()
		}
	case identity.AccountMaintenance:
		if f.Kind != Delete {
			return ServiceWriteRequest{}, bad()
		}
	default:
		return ServiceWriteRequest{}, bad()
	}
	if f.Kind == Create {
		if f.Ref.Validate() == nil || f.ExpectedVersion != 0 {
			return ServiceWriteRequest{}, bad()
		}
	} else if f.Ref.Validate() != nil || !f.Ref.Details().Scope.Equal(f.Scope) || f.ExpectedVersion.Validate() != nil {
		return ServiceWriteRequest{}, bad()
	}
	digest := ""
	if f.Kind == Create {
		if f.Value.Use(func(b []byte) error { h := sha256.Sum256(b); digest = hex.EncodeToString(h[:]); return nil }) != nil {
			return ServiceWriteRequest{}, bad()
		}
	}
	d := serviceWriteData{f, digest}
	return ServiceWriteRequest{func() serviceWriteData { return d }}, nil
}
func (r ServiceWriteRequest) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r ServiceWriteRequest) Fields() ServiceWriteFields {
	if r.data == nil {
		return ServiceWriteFields{}
	}
	return r.data().fields
}

// WithoutMaterial preserves the immutable full request binding while dropping
// the handle before it is retained by a prepared sealed write.
func (r ServiceWriteRequest) WithoutMaterial() ServiceWriteRequest {
	if r.data == nil {
		return ServiceWriteRequest{}
	}
	d := r.data()
	d.fields.Value = SecretMaterial{}
	return ServiceWriteRequest{func() serviceWriteData { return d }}
}
func ServiceWriteBinding(r ServiceWriteRequest) (foundation.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	d := r.data()
	f := d.fields
	a := f.Actor.Details()
	ref := ""
	if f.Ref.Validate() == nil {
		ref = f.Ref.Details().ID.String()
	}
	b, e := json.Marshal(struct {
		ActorKind     identity.ActorKind
		Service       identity.ServiceName
		Cause         string
		Command       string
		OwnerKind     AccountOwnerKind
		OwnerID       string
		Kind          MutationKind
		Ref, Expected string
		Purpose       Purpose
		ValueDigest   string
	}{a.Kind, a.ServiceName, a.CauseRef, f.Identity.Canonical(), f.OwnerKind, f.OwnerID, f.Kind, ref, strconv.FormatInt(int64(f.ExpectedVersion), 10), f.Purpose, d.valueDigest})
	if e != nil {
		return "", bad()
	}
	defer clear(b)
	h := sha256.Sum256(b)
	return foundation.Digest("sha256:" + hex.EncodeToString(h[:])), nil
}
func (r ServiceWriteRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "account_secret_write")
}
func (r ServiceWriteRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"account_secret_write"`), nil
}
func (*ServiceWriteRequest) UnmarshalJSON([]byte) error { return bad() }
func (r ServiceWriteRequest) LogValue() slog.Value      { return slog.StringValue("account_secret_write") }

type planIdentity struct{ nonzero byte }
type PlanIssuer struct{ identity *planIdentity }

func NewPlanIssuer() PlanIssuer  { return PlanIssuer{&planIdentity{1}} }
func (i PlanIssuer) Valid() bool { return i.identity != nil }

type writeDependencies struct {
	issuer           PlanIssuer
	binding, mapping foundation.Digest
	locks            []foundation.LockRequest
}
type WriteDependencies struct{ data func() writeDependencies }

func normalizePlanLocks(in []foundation.LockRequest) ([]foundation.LockRequest, error) {
	if len(in) > 512 {
		return nil, bad()
	}
	m := map[string]foundation.LockRequest{}
	for _, l := range in {
		if l.Key.Validate() != nil || !l.Mode.Valid() {
			return nil, bad()
		}
		if m[l.Key.Canonical()].Mode != foundation.Exclusive {
			m[l.Key.Canonical()] = l
		}
	}
	out := make([]foundation.LockRequest, 0, len(m))
	for _, l := range m {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return foundation.CompareLockKeys(out[i].Key, out[j].Key) < 0 })
	return out, nil
}
func NewWriteDependencies(issuer PlanIssuer, binding, mapping foundation.Digest, locks []foundation.LockRequest) (WriteDependencies, error) {
	if !issuer.Valid() || binding.Validate() != nil || mapping.Validate() != nil {
		return WriteDependencies{}, bad()
	}
	locks, e := normalizePlanLocks(locks)
	if e != nil {
		return WriteDependencies{}, e
	}
	d := writeDependencies{issuer, binding, mapping, locks}
	return WriteDependencies{func() writeDependencies { return d }}, nil
}
func (d WriteDependencies) Validate() error {
	if d.data == nil {
		return bad()
	}
	return nil
}
func (d WriteDependencies) Matches(i PlanIssuer, b, m foundation.Digest) bool {
	return d.data != nil && d.data().issuer == i && d.data().binding == b && d.data().mapping == m
}
func (d WriteDependencies) Binding() foundation.Digest {
	if d.data == nil {
		return ""
	}
	return d.data().binding
}
func (d WriteDependencies) Mapping() foundation.Digest {
	if d.data == nil {
		return ""
	}
	return d.data().mapping
}
func (d WriteDependencies) Locks() []foundation.LockRequest {
	if d.data == nil {
		return nil
	}
	return append([]foundation.LockRequest(nil), d.data().locks...)
}
func (d WriteDependencies) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "secret_write_dependencies")
}
func (d WriteDependencies) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_write_dependencies"`), nil
}
func (*WriteDependencies) UnmarshalJSON([]byte) error { return bad() }
func (d WriteDependencies) LogValue() slog.Value {
	return slog.StringValue("secret_write_dependencies")
}

type AccountWriteAuthority interface {
	DiscoverServiceWrite(context.Context, ServiceWriteRequest) (WriteDependencies, error)
	ValidateServiceWriteInTx(context.Context, foundation.Tx, ServiceWriteRequest, WriteDependencies) error
}

type UsageAction string

const (
	RetainReferenceUsage  UsageAction = "retain_reference"
	ReleaseReferenceUsage UsageAction = "release_reference"
	AcquireLeaseUsage     UsageAction = "acquire_lease"
	ReleaseLeaseUsage     UsageAction = "release_lease"
	ReadLeaseUsage        UsageAction = "read_lease"
)

type UsageRequest struct {
	Actor          identity.Actor
	Ref            CredentialRef
	Purpose        Purpose
	ReferenceOwner string
	LeaseOwner     CredentialLeaseOwner
	LeaseID        LeaseID
	Action         UsageAction
	Retain         bool
}

func (r UsageRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Ref.Validate() != nil || !r.Purpose.Valid() {
		return bad()
	}
	switch r.Action {
	case RetainReferenceUsage, ReleaseReferenceUsage:
		if !validID(r.ReferenceOwner) || r.LeaseOwner.Validate() == nil || r.LeaseID != (LeaseID{}) || r.Retain != (r.Action == RetainReferenceUsage) {
			return bad()
		}
	case AcquireLeaseUsage, ReleaseLeaseUsage, ReadLeaseUsage:
		if r.ReferenceOwner != "" || r.Retain || r.LeaseOwner.Validate() != nil || r.LeaseID.Validate() != nil {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func UsageBinding(r UsageRequest) (foundation.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	id := ""
	if r.LeaseID.Validate() == nil {
		id = r.LeaseID.String()
	}
	a := r.Actor.Details()
	scope := r.Ref.Details().Scope.Details()
	owner := r.LeaseOwner.Details()
	b, e := json.Marshal(struct {
		Actor          identity.ActorDetails
		Scope          identity.ScopeDetails
		Ref            string
		Purpose        Purpose
		ReferenceOwner string
		LeaseOwner     OwnerDetails
		LeaseID        string
		Action         UsageAction
		Retain         bool
	}{a, scope, r.Ref.Details().ID.String(), r.Purpose, r.ReferenceOwner, owner, id, r.Action, r.Retain})
	if e != nil {
		return "", bad()
	}
	h := sha256.Sum256(b)
	return foundation.Digest("sha256:" + hex.EncodeToString(h[:])), nil
}

type usageDependencyData struct {
	issuer           PlanIssuer
	binding, mapping foundation.Digest
	locks            []foundation.LockRequest
	provider         *UsageDependencies
}
type UsageDependencies struct{ data func() usageDependencyData }

func NewUsageDependencies(issuer PlanIssuer, binding, mapping foundation.Digest, locks []foundation.LockRequest) (UsageDependencies, error) {
	if !issuer.Valid() || binding.Validate() != nil || mapping.Validate() != nil {
		return UsageDependencies{}, bad()
	}
	locks, e := normalizePlanLocks(locks)
	if e != nil {
		return UsageDependencies{}, e
	}
	d := usageDependencyData{issuer: issuer, binding: binding, mapping: mapping, locks: locks}
	return UsageDependencies{func() usageDependencyData { return d }}, nil
}

// WrapUsageDependencies is a constructor for the trusted Service. It does not
// grant callers the Service's private issuer or manufacture its authorization.
func WrapUsageDependencies(issuer PlanIssuer, provider UsageDependencies, locks []foundation.LockRequest) (UsageDependencies, error) {
	if provider.Validate() != nil {
		return UsageDependencies{}, bad()
	}
	d, e := NewUsageDependencies(issuer, provider.Binding(), provider.Mapping(), locks)
	if e != nil {
		return UsageDependencies{}, e
	}
	v := d.data()
	copy := provider
	v.provider = &copy
	return UsageDependencies{func() usageDependencyData { return v }}, nil
}
func (d UsageDependencies) Validate() error {
	if d.data == nil {
		return bad()
	}
	return nil
}
func (d UsageDependencies) Matches(i PlanIssuer, b, m foundation.Digest) bool {
	return d.data != nil && d.data().issuer == i && d.data().binding == b && d.data().mapping == m
}
func (d UsageDependencies) Binding() foundation.Digest {
	if d.data == nil {
		return ""
	}
	return d.data().binding
}
func (d UsageDependencies) Mapping() foundation.Digest {
	if d.data == nil {
		return ""
	}
	return d.data().mapping
}
func (d UsageDependencies) RequiredLocks() []foundation.LockRequest {
	if d.data == nil {
		return nil
	}
	return append([]foundation.LockRequest(nil), d.data().locks...)
}
func (d UsageDependencies) ProviderPlan() UsageDependencies {
	if d.data == nil || d.data().provider == nil {
		return UsageDependencies{}
	}
	return *d.data().provider
}
func (d UsageDependencies) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "secret_usage_dependencies")
}
func (d UsageDependencies) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_usage_dependencies"`), nil
}
func (*UsageDependencies) UnmarshalJSON([]byte) error { return bad() }
func (d UsageDependencies) LogValue() slog.Value {
	return slog.StringValue("secret_usage_dependencies")
}

type UsageResult struct {
	data func() (UsageAction, CredentialLease)
}

func NewUsageResult(action UsageAction, lease CredentialLease) (UsageResult, error) {
	if action != RetainReferenceUsage && action != ReleaseReferenceUsage && action != AcquireLeaseUsage && action != ReleaseLeaseUsage {
		return UsageResult{}, bad()
	}
	if action == AcquireLeaseUsage {
		if lease.LeaseID.Validate() != nil || lease.CredentialRef.Validate() != nil {
			return UsageResult{}, bad()
		}
	} else if lease.LeaseID != (LeaseID{}) || lease.CredentialRef.Validate() == nil {
		return UsageResult{}, bad()
	}
	return UsageResult{func() (UsageAction, CredentialLease) { return action, lease }}, nil
}
func (r UsageResult) Action() UsageAction {
	if r.data == nil {
		return ""
	}
	a, _ := r.data()
	return a
}
func (r UsageResult) Lease() (CredentialLease, bool) {
	if r.data == nil {
		return CredentialLease{}, false
	}
	a, l := r.data()
	return l, a == AcquireLeaseUsage
}
func (r UsageResult) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "secret_usage_result") }
func (r UsageResult) MarshalJSON() ([]byte, error) { return []byte(`"secret_usage_result"`), nil }
func (*UsageResult) UnmarshalJSON([]byte) error    { return bad() }
func (r UsageResult) LogValue() slog.Value         { return slog.StringValue("secret_usage_result") }

type UsageOperations interface {
	DiscoverUsage(context.Context, UsageRequest) (UsageDependencies, error)
	ApplyUsageInTx(context.Context, foundation.Tx, UsageRequest, UsageDependencies) (UsageResult, error)
}
type UsagePlanner interface {
	DiscoverUsage(context.Context, UsageRequest) (UsageDependencies, error)
	ValidateUsageInTx(context.Context, foundation.Tx, UsageRequest, UsageDependencies) error
}
