package contract

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type ConsumerAction string

const (
	ResolveConsumer        ConsumerAction = "resolve"
	InvokeConsumer         ConsumerAction = "invoke"
	ReadCredentialConsumer ConsumerAction = "credential_read"
	RetireConsumer         ConsumerAction = "retire"
	FinalizeConsumer       ConsumerAction = "finalize"
)

type InputIdentity struct {
	ExecutionID   *id.ExecutionID `json:"execution_id,omitempty"`
	RoundID       string          `json:"round_id,omitempty"`
	OperationID   string          `json:"operation_id,omitempty"`
	Digest        f.Digest        `json:"digest"`
	SchemaVersion f.Version       `json:"schema_version"`
}

func (i InputIdentity) Validate() error {
	if i.Digest.Validate() != nil || i.SchemaVersion.Validate() != nil {
		return bad()
	}
	if i.ExecutionID != nil {
		if i.ExecutionID.Validate() != nil || !uuid(i.RoundID) || i.OperationID != "" {
			return bad()
		}
	} else if i.RoundID != "" || !uuid(i.OperationID) {
		return bad()
	}
	return nil
}
func (i InputIdentity) ValidateFor(c Consumer) error {
	if i.Validate() != nil || c.Validate() != nil {
		return bad()
	}
	if c.ExecutionID != nil {
		if i.ExecutionID == nil || *i.ExecutionID != *c.ExecutionID {
			return bad()
		}
	} else if i.ExecutionID != nil || i.OperationID != c.OperationID {
		return bad()
	}
	return nil
}
func (i InputIdentity) Clone() InputIdentity { i.ExecutionID = copyPtr(i.ExecutionID); return i }

type AttemptIdentity struct {
	CallID       CallID       `json:"call_id"`
	InvocationID InvocationID `json:"invocation_id"`
	AttemptIndex f.Sequence   `json:"attempt_index"`
	ProcessID    oc.ProcessID `json:"process_id"`
	Fence        f.Sequence   `json:"fence"`
}

func (a AttemptIdentity) Validate() error {
	if a.CallID.Validate() != nil || a.InvocationID.Validate() != nil || a.AttemptIndex.Validate() != nil || a.ProcessID.Validate() != nil || a.Fence.Validate() != nil {
		return bad()
	}
	return nil
}

type ConsumerRequest struct {
	Action          ConsumerAction          `json:"action"`
	Actor           id.Actor                `json:"-"`
	Consumer        Consumer                `json:"consumer"`
	Resolve         *ResolveRequest         `json:"resolve,omitempty"`
	SnapshotID      SnapshotID              `json:"snapshot_id"`
	LeaseOwner      sc.CredentialLeaseOwner `json:"lease_owner"`
	LeaseID         *sc.LeaseID             `json:"lease_id,omitempty"`
	CallID          *CallID                 `json:"call_id,omitempty"`
	Attempt         *AttemptIdentity        `json:"attempt,omitempty"`
	Input           *InputIdentity          `json:"input,omitempty"`
	TerminalVersion *f.Version              `json:"terminal_version,omitempty"`
}

func (r ConsumerRequest) Validate() error {
	if r.Consumer.Validate() != nil || !actorMatches(r.Actor, r.Consumer) || r.SnapshotID.Validate() != nil || !ownerMatches(r.LeaseOwner, r.Consumer, r.CallID) || r.LeaseID != nil && r.LeaseID.Validate() != nil || r.CallID != nil && r.CallID.Validate() != nil || r.Attempt != nil && r.Attempt.Validate() != nil || r.Input != nil && r.Input.ValidateFor(r.Consumer) != nil || r.TerminalVersion != nil && r.TerminalVersion.Validate() != nil {
		return bad()
	}
	switch r.Action {
	case ResolveConsumer:
		if r.Resolve == nil || r.Resolve.Validate() != nil || !r.Actor.Equal(r.Resolve.Actor) || !r.Consumer.Equal(r.Resolve.Consumer) || !r.LeaseOwner.Equal(r.Resolve.LeaseOwner) || r.Attempt != nil || r.Input != nil || r.TerminalVersion != nil {
			return bad()
		}
		if r.Resolve.ServingSnapshotID != nil && *r.Resolve.ServingSnapshotID != r.SnapshotID {
			return bad()
		}
		if (r.LeaseOwner.Details().Kind == sc.ModelCallOwner) != (r.CallID != nil) {
			return bad()
		}
	case InvokeConsumer:
		if r.Resolve != nil || r.CallID == nil || r.Input == nil || r.Attempt != nil || r.TerminalVersion != nil {
			return bad()
		}
	case ReadCredentialConsumer, FinalizeConsumer:
		if r.Resolve != nil || r.CallID == nil || r.Attempt == nil || r.Input == nil || r.Attempt.CallID != *r.CallID {
			return bad()
		}
		if r.Action == ReadCredentialConsumer && (r.LeaseID == nil || r.TerminalVersion != nil) {
			return bad()
		}
	case RetireConsumer:
		if r.Resolve != nil || r.LeaseID == nil || r.Attempt != nil || r.Input != nil {
			return bad()
		}
		if (r.LeaseOwner.Details().Kind == sc.ModelCallOwner) != (r.CallID != nil) {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func (r ConsumerRequest) Clone() ConsumerRequest {
	r.Consumer = r.Consumer.Clone()
	r.Resolve = copyPtr(r.Resolve)
	if r.Resolve != nil {
		x := r.Resolve.Clone()
		r.Resolve = &x
	}
	r.LeaseID = copyPtr(r.LeaseID)
	r.CallID = copyPtr(r.CallID)
	r.Attempt = copyPtr(r.Attempt)
	r.Input = copyPtr(r.Input)
	if r.Input != nil {
		x := r.Input.Clone()
		r.Input = &x
	}
	r.TerminalVersion = copyPtr(r.TerminalVersion)
	return r
}
func ConsumerBinding(r ConsumerRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	return binding(struct {
		Actor   id.ActorDetails
		Request ConsumerRequest
	}{r.Actor.Details(), r})
}

type RetryPolicy struct {
	Class       RetryClass      `json:"class"`
	Deadline    *f.Instant      `json:"deadline,omitempty"`
	MaxAttempts *f.Sequence     `json:"max_attempts,omitempty"`
	Categories  []ErrorCategory `json:"categories"`
}

func (p RetryPolicy) Validate() error {
	switch p.Class {
	case AgentRetry:
		if p.Deadline != nil || p.MaxAttempts != nil {
			return bad()
		}
	case BoundedRetry:
		if p.Deadline == nil || p.Deadline.Validate() != nil || p.MaxAttempts == nil || p.MaxAttempts.Validate() != nil {
			return bad()
		}
	default:
		return bad()
	}
	seen := map[ErrorCategory]bool{}
	for _, c := range p.Categories {
		if seen[c] || !one(string(c), "rate_limited", "provider_unavailable", "timeout", "network", "provider_error") {
			return bad()
		}
		seen[c] = true
	}
	return nil
}
func (p RetryPolicy) ValidateFor(c Consumer) error {
	if p.Validate() != nil || c.Validate() != nil {
		return bad()
	}
	if c.Kind == AgentConsumer {
		if p.Class != AgentRetry {
			return bad()
		}
	} else if p.Class != BoundedRetry {
		return bad()
	}
	if c.Purpose == ApprovalAuto && (*p.MaxAttempts != 1 || len(p.Categories) != 0) {
		return bad()
	}
	return nil
}
func (p RetryPolicy) Clone() RetryPolicy {
	p.Deadline = copyPtr(p.Deadline)
	p.MaxAttempts = copyPtr(p.MaxAttempts)
	p.Categories = append([]ErrorCategory(nil), p.Categories...)
	return p
}

type ConsumerDependencyDetails struct {
	Binding, Mapping f.Digest
	Locks            []f.LockRequest
	RetryPolicy      *RetryPolicy
}

func (d ConsumerDependencyDetails) clone() ConsumerDependencyDetails {
	d.Locks = append([]f.LockRequest(nil), d.Locks...)
	if d.RetryPolicy != nil {
		p := d.RetryPolicy.Clone()
		d.RetryPolicy = &p
	}
	return d
}

type ConsumerAuthority interface {
	Discover(context.Context, ConsumerRequest) (ConsumerDependencies, error)
	ValidateInTx(context.Context, f.Tx, ConsumerRequest, ConsumerDependencies) error
}
type PlanIssuer struct{ token *byte }

func NewPlanIssuer() PlanIssuer  { return PlanIssuer{new(byte)} }
func (i PlanIssuer) Valid() bool { return i.token != nil }

type consumerDependencyData struct {
	issuer  PlanIssuer
	details ConsumerDependencyDetails
}
type resolutionPlanData struct {
	issuer  PlanIssuer
	details ResolutionPlanDetails
}
type referencePlanData struct {
	issuer  PlanIssuer
	details ReferencePlanDetails
}
type replacementPlanData struct {
	issuer  PlanIssuer
	details ReplacementPlanDetails
}
type ConsumerDependencies struct{ data func() consumerDependencyData }
type ResolutionPlan struct{ data func() resolutionPlanData }
type ReferencePlan struct{ data func() referencePlanData }
type ReplacementPlan struct{ data func() replacementPlanData }

func planLocks(i PlanIssuer, b, m f.Digest, locks []f.LockRequest) ([]f.LockRequest, error) {
	if !i.Valid() || b.Validate() != nil || m.Validate() != nil || len(locks) == 0 {
		return nil, bad()
	}
	r := append([]f.LockRequest(nil), locks...)
	for _, l := range r {
		if l.Key.Validate() != nil || !l.Mode.Valid() {
			return nil, bad()
		}
	}
	slices.SortFunc(r, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	out := r[:0]
	for _, l := range r {
		if len(out) > 0 && f.CompareLockKeys(out[len(out)-1].Key, l.Key) == 0 {
			if l.Mode == f.Exclusive {
				out[len(out)-1].Mode = l.Mode
			}
		} else {
			out = append(out, l)
		}
	}
	return out, nil
}
func covers(all, required []f.LockRequest) bool {
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
func NewConsumerDependencies(i PlanIssuer, d ConsumerDependencyDetails) (ConsumerDependencies, error) {
	locks, e := planLocks(i, d.Binding, d.Mapping, d.Locks)
	if e != nil || d.RetryPolicy != nil && d.RetryPolicy.Validate() != nil {
		return ConsumerDependencies{}, bad()
	}
	d = d.clone()
	d.Locks = locks
	x := consumerDependencyData{i, d}
	return ConsumerDependencies{func() consumerDependencyData { return x }}, nil
}
func NewResolutionPlan(i PlanIssuer, d ResolutionPlanDetails) (ResolutionPlan, error) {
	locks, e := planLocks(i, d.Binding, d.Mapping, d.Locks)
	if e != nil || d.validate() != nil || !covers(locks, d.Consumer.RequiredLocks()) || d.Secret != nil && !covers(locks, d.Secret.RequiredLocks()) {
		return ResolutionPlan{}, bad()
	}
	d = d.clone()
	d.Locks = locks
	x := resolutionPlanData{i, d}
	return ResolutionPlan{func() resolutionPlanData { return x }}, nil
}
func NewReferencePlan(i PlanIssuer, d ReferencePlanDetails) (ReferencePlan, error) {
	locks, e := planLocks(i, d.Binding, d.Mapping, d.Locks)
	if e != nil || d.Owner.Validate() != nil || d.OwnerVersion.Validate() != nil || d.Consumer != nil && (d.Consumer.Validate() != nil || !covers(locks, d.Consumer.RequiredLocks())) {
		return ReferencePlan{}, bad()
	}
	d = d.clone()
	d.Locks = locks
	x := referencePlanData{i, d}
	return ReferencePlan{func() referencePlanData { return x }}, nil
}
func NewReplacementPlan(i PlanIssuer, d ReplacementPlanDetails) (ReplacementPlan, error) {
	locks, e := planLocks(i, d.Binding, d.Mapping, d.Locks)
	if e != nil || d.ModelID.Validate() != nil || d.ModelVersion.Validate() != nil || d.Replacement != nil && (d.Replacement.Validate() != nil || *d.Replacement == d.ModelID) {
		return ReplacementPlan{}, bad()
	}
	seen := map[string]bool{}
	for _, v := range d.Items {
		if v.Change.Validate() != nil || v.Plan.Validate() != nil || !covers(locks, v.Plan.RequiredLocks()) {
			return ReplacementPlan{}, bad()
		}
		b, e := ReferenceBinding(v.Change)
		key := v.Change.Owner.Kind + ":" + v.Change.Owner.ID + ":" + v.Change.Owner.Role
		if e != nil || b != v.Plan.Details().Binding || seen[key] || v.Change.Before == nil || *v.Change.Before != d.ModelID || !sameID(v.Change.After, d.Replacement) {
			return ReplacementPlan{}, bad()
		}
		seen[key] = true
	}
	d = d.clone()
	d.Locks = locks
	x := replacementPlanData{i, d}
	return ReplacementPlan{func() replacementPlanData { return x }}, nil
}
func sameID(a, b *ModelID) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func (p ConsumerDependencies) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p ConsumerDependencies) Details() ConsumerDependencyDetails {
	if p.data == nil {
		return ConsumerDependencyDetails{}
	}
	return p.data().details.clone()
}
func (p ConsumerDependencies) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return append([]f.LockRequest(nil), p.data().details.Locks...)
}
func (p ConsumerDependencies) Matches(i PlanIssuer, b, m f.Digest) bool {
	return i.Valid() && p.data != nil && p.data().issuer == i && p.data().details.Binding == b && p.data().details.Mapping == m
}
func (p ConsumerDependencies) MarshalJSON() ([]byte, error) { return []byte(`"model_plan"`), nil }
func (*ConsumerDependencies) UnmarshalJSON([]byte) error    { return bad() }

func (p ResolutionPlan) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p ResolutionPlan) Details() ResolutionPlanDetails {
	if p.data == nil {
		return ResolutionPlanDetails{}
	}
	return p.data().details.clone()
}
func (p ResolutionPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return append([]f.LockRequest(nil), p.data().details.Locks...)
}
func (p ResolutionPlan) Matches(i PlanIssuer, b, m f.Digest) bool {
	return i.Valid() && p.data != nil && p.data().issuer == i && p.data().details.Binding == b && p.data().details.Mapping == m
}
func (p ResolutionPlan) MarshalJSON() ([]byte, error) { return []byte(`"model_plan"`), nil }
func (*ResolutionPlan) UnmarshalJSON([]byte) error    { return bad() }

func (p ReferencePlan) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p ReferencePlan) Details() ReferencePlanDetails {
	if p.data == nil {
		return ReferencePlanDetails{}
	}
	return p.data().details.clone()
}
func (p ReferencePlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return append([]f.LockRequest(nil), p.data().details.Locks...)
}
func (p ReferencePlan) Matches(i PlanIssuer, b, m f.Digest) bool {
	return i.Valid() && p.data != nil && p.data().issuer == i && p.data().details.Binding == b && p.data().details.Mapping == m
}
func (p ReferencePlan) MarshalJSON() ([]byte, error) { return []byte(`"model_plan"`), nil }
func (*ReferencePlan) UnmarshalJSON([]byte) error    { return bad() }

func (p ReplacementPlan) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p ReplacementPlan) Details() ReplacementPlanDetails {
	if p.data == nil {
		return ReplacementPlanDetails{}
	}
	return p.data().details.clone()
}
func (p ReplacementPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return append([]f.LockRequest(nil), p.data().details.Locks...)
}
func (p ReplacementPlan) Matches(i PlanIssuer, b, m f.Digest) bool {
	return i.Valid() && p.data != nil && p.data().issuer == i && p.data().details.Binding == b && p.data().details.Mapping == m
}
func (p ReplacementPlan) MarshalJSON() ([]byte, error) { return []byte(`"model_plan"`), nil }
func (*ReplacementPlan) UnmarshalJSON([]byte) error    { return bad() }

func (PlanIssuer) MarshalJSON() ([]byte, error) { return []byte(`"model_plan_issuer"`), nil }
func (*PlanIssuer) UnmarshalJSON([]byte) error  { return bad() }

func (InputIdentity) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_input_identity")) }
func (InputIdentity) LogValue() slog.Value       { return slog.StringValue("model_input_identity") }

func (AttemptIdentity) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_attempt_identity")) }
func (AttemptIdentity) LogValue() slog.Value       { return slog.StringValue("model_attempt_identity") }

func (ConsumerRequest) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_consumer_request")) }
func (ConsumerRequest) LogValue() slog.Value       { return slog.StringValue("model_consumer_request") }

func (RetryPolicy) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_retry_policy")) }
func (RetryPolicy) LogValue() slog.Value       { return slog.StringValue("model_retry_policy") }

func (ConsumerDependencyDetails) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_consumer_dependency_details"))
}
func (ConsumerDependencyDetails) LogValue() slog.Value {
	return slog.StringValue("model_consumer_dependency_details")
}

func (PlanIssuer) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_plan_issuer")) }
func (PlanIssuer) LogValue() slog.Value       { return slog.StringValue("model_plan_issuer") }

func (ConsumerDependencies) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_consumer_dependencies"))
}
func (ConsumerDependencies) LogValue() slog.Value {
	return slog.StringValue("model_consumer_dependencies")
}

func (ResolutionPlan) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_resolution_plan")) }
func (ResolutionPlan) LogValue() slog.Value       { return slog.StringValue("model_resolution_plan") }

func (ReferencePlan) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_reference_plan")) }
func (ReferencePlan) LogValue() slog.Value       { return slog.StringValue("model_reference_plan") }

func (ReplacementPlan) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_replacement_plan")) }
func (ReplacementPlan) LogValue() slog.Value       { return slog.StringValue("model_replacement_plan") }
