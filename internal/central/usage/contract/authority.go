package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type InvocationFacts interface {
	Discover(context.Context, InvocationRequest) (InvocationDependencies, error)
	ValidateInTx(context.Context, f.Tx, InvocationRequest, InvocationDependencies) (InvocationFact, error)
}

type PlanIssuer struct{ token *byte }

func NewPlanIssuer() PlanIssuer  { return PlanIssuer{new(byte)} }
func (i PlanIssuer) Valid() bool { return i.token != nil }

type InvocationDependencyDetails struct {
	Binding, Mapping f.Digest
	Locks            []f.LockRequest
}

func (d InvocationDependencyDetails) clone() InvocationDependencyDetails {
	d.Locks = append([]f.LockRequest(nil), d.Locks...)
	return d
}

type InvocationPlanDetails struct {
	Binding, Mapping f.Digest
	Locks            []f.LockRequest
	Facts            InvocationDependencies
	Cause            f.TransactionCause
}

func (d InvocationPlanDetails) clone() InvocationPlanDetails {
	d.Locks = append([]f.LockRequest(nil), d.Locks...)
	return d
}

type dependencyData struct {
	issuer  PlanIssuer
	details InvocationDependencyDetails
}
type planData struct {
	issuer  PlanIssuer
	details InvocationPlanDetails
}
type InvocationDependencies struct{ data func() dependencyData }
type InvocationPlan struct{ data func() planData }

func canonicalLocks(i PlanIssuer, b, m f.Digest, in []f.LockRequest) ([]f.LockRequest, error) {
	if !i.Valid() || b.Validate() != nil || m.Validate() != nil || len(in) == 0 {
		return nil, bad()
	}
	out := append([]f.LockRequest(nil), in...)
	for _, l := range out {
		if l.Key.Validate() != nil || !l.Mode.Valid() {
			return nil, bad()
		}
	}
	slices.SortFunc(out, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	result := out[:0]
	for _, l := range out {
		if len(result) > 0 && f.CompareLockKeys(result[len(result)-1].Key, l.Key) == 0 {
			if l.Mode == f.Exclusive {
				result[len(result)-1].Mode = f.Exclusive
			}
		} else {
			result = append(result, l)
		}
	}
	return result, nil
}
func covers(all, required []f.LockRequest) bool {
	for _, r := range required {
		found := false
		for _, l := range all {
			if f.CompareLockKeys(r.Key, l.Key) == 0 && (l.Mode == f.Exclusive || l.Mode == r.Mode) {
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
func NewInvocationDependencies(i PlanIssuer, d InvocationDependencyDetails) (InvocationDependencies, error) {
	locks, e := canonicalLocks(i, d.Binding, d.Mapping, d.Locks)
	if e != nil {
		return InvocationDependencies{}, e
	}
	d = d.clone()
	d.Locks = locks
	x := dependencyData{i, d}
	return InvocationDependencies{func() dependencyData { return x }}, nil
}
func NewInvocationPlan(i PlanIssuer, d InvocationPlanDetails) (InvocationPlan, error) {
	locks, e := canonicalLocks(i, d.Binding, d.Mapping, d.Locks)
	if e != nil || d.Facts.Validate() != nil || d.Facts.Details().Binding != d.Binding || d.Cause.Validate() != nil || !covers(locks, d.Facts.RequiredLocks()) {
		return InvocationPlan{}, bad()
	}
	d = d.clone()
	d.Locks = locks
	x := planData{i, d}
	return InvocationPlan{func() planData { return x }}, nil
}
func (d InvocationDependencies) Validate() error {
	if d.data == nil {
		return bad()
	}
	return nil
}
func (d InvocationDependencies) Details() InvocationDependencyDetails {
	if d.data == nil {
		return InvocationDependencyDetails{}
	}
	return d.data().details.clone()
}
func (d InvocationDependencies) RequiredLocks() []f.LockRequest { return d.Details().Locks }
func (d InvocationDependencies) Matches(i PlanIssuer, b, m f.Digest) bool {
	return i.Valid() && d.data != nil && d.data().issuer == i && d.data().details.Binding == b && d.data().details.Mapping == m
}
func (d InvocationPlan) Validate() error {
	if d.data == nil {
		return bad()
	}
	return nil
}
func (d InvocationPlan) Details() InvocationPlanDetails {
	if d.data == nil {
		return InvocationPlanDetails{}
	}
	return d.data().details.clone()
}
func (d InvocationPlan) RequiredLocks() []f.LockRequest { return d.Details().Locks }
func (d InvocationPlan) Cause() f.TransactionCause      { return d.Details().Cause }
func (d InvocationPlan) Matches(i PlanIssuer, b, m f.Digest) bool {
	return i.Valid() && d.data != nil && d.data().issuer == i && d.data().details.Binding == b && d.data().details.Mapping == m
}
func digestBytes(raw []byte) f.Digest {
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

func (PlanIssuer) MarshalJSON() ([]byte, error) { return []byte(`"usage_plan_issuer"`), nil }
func (*PlanIssuer) UnmarshalJSON([]byte) error  { return bad() }
func (PlanIssuer) Format(w fmt.State, _ rune)   { _, _ = w.Write([]byte("usage_plan_issuer")) }
func (PlanIssuer) LogValue() slog.Value         { return slog.StringValue("usage_plan_issuer") }
func (InvocationDependencies) MarshalJSON() ([]byte, error) {
	return []byte(`"usage_dependencies"`), nil
}
func (*InvocationDependencies) UnmarshalJSON([]byte) error { return bad() }
func (InvocationDependencies) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_dependencies"))
}
func (InvocationDependencies) LogValue() slog.Value { return slog.StringValue("usage_dependencies") }
func (InvocationPlan) MarshalJSON() ([]byte, error) { return []byte(`"usage_plan"`), nil }
func (*InvocationPlan) UnmarshalJSON([]byte) error  { return bad() }
func (InvocationPlan) Format(w fmt.State, _ rune)   { _, _ = w.Write([]byte("usage_plan")) }
func (InvocationPlan) LogValue() slog.Value         { return slog.StringValue("usage_plan") }
func (InvocationDependencyDetails) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_dependency_details"))
}
func (InvocationDependencyDetails) LogValue() slog.Value {
	return slog.StringValue("usage_dependency_details")
}
func (InvocationPlanDetails) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("usage_plan_details"))
}
func (InvocationPlanDetails) LogValue() slog.Value { return slog.StringValue("usage_plan_details") }
