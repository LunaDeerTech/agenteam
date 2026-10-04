// Package contract defines Outbox's trusted composition and authorization
// ports. Neutral event schemas live in event/contract below this layer.
package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type Stage string

const (
	CurrentAccess Stage = "current_access"
	NewFact       Stage = "new_fact"
)

func (s Stage) Valid() bool { return s == CurrentAccess || s == NewFact }

type issuerIdentity struct{ nonzero byte }
type PlanIssuer struct{ identity *issuerIdentity }

func NewPlanIssuer() PlanIssuer  { return PlanIssuer{&issuerIdentity{1}} }
func (i PlanIssuer) Valid() bool { return i.identity != nil }

type dependencyData struct {
	issuer  PlanIssuer
	binding foundation.Digest
	locks   []foundation.LockRequest
	opaque  []byte
}
type Dependencies struct{ data func() dependencyData }

func NewDependencies(issuer PlanIssuer, binding foundation.Digest, locks []foundation.LockRequest, opaque []byte) (Dependencies, error) {
	if !issuer.Valid() || binding.Validate() != nil || len(opaque) > 16384 {
		return Dependencies{}, invalid()
	}
	locks, err := NormalizeLocks(locks)
	if err != nil {
		return Dependencies{}, err
	}
	d := dependencyData{issuer, binding, locks, append([]byte(nil), opaque...)}
	return Dependencies{func() dependencyData { return d }}, nil
}
func (d Dependencies) Validate() error {
	if d.data == nil {
		return invalid()
	}
	return nil
}
func (d Dependencies) Matches(i PlanIssuer, binding foundation.Digest) bool {
	return d.data != nil && d.data().issuer == i && d.data().binding == binding
}
func (d Dependencies) Binding() foundation.Digest {
	if d.data == nil {
		return ""
	}
	return d.data().binding
}
func (d Dependencies) Locks() []foundation.LockRequest {
	if d.data == nil {
		return nil
	}
	return append([]foundation.LockRequest(nil), d.data().locks...)
}
func (d Dependencies) Opaque() []byte {
	if d.data == nil {
		return nil
	}
	return append([]byte(nil), d.data().opaque...)
}
func (d Dependencies) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbox_dependencies") }
func (d Dependencies) MarshalJSON() ([]byte, error) { return []byte(`"outbox_dependencies"`), nil }
func (*Dependencies) UnmarshalJSON([]byte) error    { return invalid() }
func (d Dependencies) LogValue() slog.Value         { return slog.StringValue("outbox_dependencies") }

func NormalizeLocks(input []foundation.LockRequest) ([]foundation.LockRequest, error) {
	if len(input) > 512 {
		return nil, invalid()
	}
	m := map[string]foundation.LockRequest{}
	for _, l := range input {
		if l.Key.Validate() != nil || !l.Mode.Valid() {
			return nil, invalid()
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

type ProducerAuthority interface {
	DiscoverAppend(context.Context, identity.Actor, event.Summary) (Dependencies, error)
	ValidateAppendInTx(context.Context, foundation.Tx, identity.Actor, event.Summary, Dependencies, Stage) error
}
type ProjectAction string

const (
	AppendProject    ProjectAction = "append"
	DeliverProject   ProjectAction = "deliver"
	InspectProject   ProjectAction = "inspect"
	RequeueProject   ProjectAction = "requeue"
	LifecycleProject ProjectAction = "lifecycle"
)

type ProjectRequestDetails struct {
	Kind      ProjectAction
	ProjectID identity.ProjectID
	Actor     identity.Actor
	Stage     Stage
	Event     event.Summary
	Delivery  DeliveryIdentity
	Lifecycle LifecycleCause
}
type ProjectRequest struct{ data func() ProjectRequestDetails }

func NewProjectRequest(d ProjectRequestDetails) (ProjectRequest, error) {
	if d.ProjectID.Validate() != nil || d.Actor.Validate() != nil {
		return ProjectRequest{}, invalid()
	}
	switch d.Kind {
	case AppendProject:
		if !d.Stage.Valid() || d.Event.Header.Validate() != nil || d.Event.Producer.Validate() != nil || d.Event.PayloadDigest.Validate() != nil || d.Event.Header.Scope.Kind != event.ProjectScope || d.Event.Header.Scope.ProjectID.String() != d.ProjectID.String() || d.Delivery != (DeliveryIdentity{}) || d.Lifecycle.Validate() == nil {
			return ProjectRequest{}, invalid()
		}
	case DeliverProject:
		if !d.Delivery.Valid() || d.Event.Header.Validate() != nil || d.Event.PayloadDigest.Validate() != nil || d.Event.Producer.Validate() != nil || d.Event.Header.EventID != d.Delivery.EventID || d.Event.Header.Scope != d.Delivery.Scope || d.Actor.Details().Kind != identity.Service || d.Actor.Details().ServiceName != identity.OutboxDelivery || d.Delivery.Scope.Kind != event.ProjectScope || d.Delivery.Scope.ProjectID.String() != d.ProjectID.String() || d.Stage != "" || d.Lifecycle.Validate() == nil {
			return ProjectRequest{}, invalid()
		}
	case InspectProject:
		if d.Actor.Details().Kind != identity.Human || d.Event != (event.Summary{}) || d.Delivery != (DeliveryIdentity{}) || d.Stage != "" || d.Lifecycle.Validate() == nil {
			return ProjectRequest{}, invalid()
		}
	case RequeueProject:
		if d.Actor.Details().Kind != identity.Human || d.Event != (event.Summary{}) || !d.Stage.Valid() || !d.Delivery.Valid() || d.Delivery.Scope.Kind != event.ProjectScope || d.Delivery.Scope.ProjectID.String() != d.ProjectID.String() || d.Lifecycle.Validate() == nil {
			return ProjectRequest{}, invalid()
		}
	case LifecycleProject:
		if d.Event != (event.Summary{}) || d.Delivery != (DeliveryIdentity{}) || d.Lifecycle.Validate() != nil || d.Lifecycle.Details().ProjectID != d.ProjectID || d.Stage != "" || d.Actor.Details().Kind != identity.Service || d.Actor.Details().ServiceName != identity.ProjectLifecycle {
			return ProjectRequest{}, invalid()
		}
	default:
		return ProjectRequest{}, invalid()
	}
	if d.Actor.Details().Kind == identity.Service && d.Actor.Details().ProjectID != d.ProjectID.String() {
		return ProjectRequest{}, invalid()
	}
	if d.Kind == DeliverProject {
		ref, err := d.Delivery.CauseRef()
		if err != nil || d.Actor.Details().CauseRef != ref {
			return ProjectRequest{}, invalid()
		}
	}
	d.Event = copySummary(d.Event)
	return ProjectRequest{func() ProjectRequestDetails { return d }}, nil
}
func (r ProjectRequest) Validate() error {
	if r.data == nil {
		return invalid()
	}
	return nil
}
func (r ProjectRequest) Details() ProjectRequestDetails {
	if r.data == nil {
		return ProjectRequestDetails{}
	}
	d := r.data()
	d.Event = copySummary(d.Event)
	return d
}
func (r ProjectRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "outbox_project_request")
}
func (r ProjectRequest) MarshalJSON() ([]byte, error) { return []byte(`"outbox_project_request"`), nil }
func (*ProjectRequest) UnmarshalJSON([]byte) error    { return invalid() }
func (r ProjectRequest) LogValue() slog.Value         { return slog.StringValue("outbox_project_request") }

type ProjectAuthority interface {
	Discover(context.Context, ProjectRequest) (Dependencies, error)
	ValidateInTx(context.Context, foundation.Tx, ProjectRequest, Dependencies) error
}
type ProcessAuthority interface {
	CurrentProcess() ProcessID
	ConfirmStopped(context.Context, ProcessID) error
}

func copySummary(s event.Summary) event.Summary {
	if s.Header.AggregateVersion != nil {
		v := *s.Header.AggregateVersion
		s.Header.AggregateVersion = &v
	}
	if s.Header.AggregateSequence != nil {
		v := *s.Header.AggregateSequence
		s.Header.AggregateSequence = &v
	}
	return s
}
func StableActor(a identity.Actor) (string, error) {
	if a.Validate() != nil {
		return "", invalid()
	}
	d := a.Details()
	d.SessionID = ""
	b, err := json.Marshal(d)
	if err != nil {
		return "", invalid()
	}
	return string(b), nil
}
func DigestBytes(b []byte) foundation.Digest {
	d := sha256.Sum256(b)
	return foundation.Digest("sha256:" + hex.EncodeToString(d[:]))
}
func EventDigest(e event.Event) (foundation.Digest, error) {
	if e.Validate() != nil {
		return "", invalid()
	}
	b, err := json.Marshal(struct {
		Producer event.StableName
		Header   event.Header
		Payload  json.RawMessage
	}{e.Summary().Producer, e.Header(), e.PayloadBytes()})
	if err != nil {
		return "", invalid()
	}
	return DigestBytes(b), nil
}
func SemanticDigest(a identity.Actor, e event.Event) (foundation.Digest, error) {
	actor, err := StableActor(a)
	if err != nil {
		return "", err
	}
	ev, err := EventDigest(e)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(struct{ Format, Actor, Event string }{"outbox-v1", actor, ev.String()})
	if err != nil {
		return "", invalid()
	}
	return DigestBytes(b), nil
}
func ScopeIdentity(s event.Scope) (identity.Scope, error) {
	if s.Validate() != nil {
		return identity.Scope{}, invalid()
	}
	if s.Kind == event.SystemScope {
		return identity.SystemScope(), nil
	}
	p, err := foundation.ParseID[identity.Project](s.ProjectID.String())
	if err != nil {
		return identity.Scope{}, invalid()
	}
	return identity.InProject(p)
}
func invalid() error { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
