package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// StoppedResolutionRequest carries the exact original request, including a
// nil Selection.Version when originally omitted. Neither a newer selector nor
// the returned snapshot's version may replace the persisted request's source.
// PreparedProcessID must come from the domain's original durable call owner.
// No DTO field is a process-death proof or a new Resolve/Begin permission.
type StoppedResolutionRequest struct {
	Resolve           ResolveRequest
	CallID            CallID
	Input             InputIdentity
	PreparedProcessID oc.ProcessID
}

func (r StoppedResolutionRequest) Validate() error {
	if r.Resolve.Validate() != nil || r.CallID.Validate() != nil || r.Input.ValidateFor(r.Resolve.Consumer) != nil || r.PreparedProcessID.Validate() != nil || r.Resolve.LeaseOwner.Details().Kind != sc.ModelCallOwner || r.Resolve.LeaseOwner.Details().ID != r.CallID.String() || !one(string(r.Resolve.Consumer.Kind), string(KnowledgeConsumer), string(MemoryConsumer), string(MeetingConsumer)) {
		return bad()
	}
	return nil
}
func (r StoppedResolutionRequest) Clone() StoppedResolutionRequest {
	r.Resolve, r.Input = r.Resolve.Clone(), r.Input.Clone()
	return r
}
func StoppedResolutionBinding(r StoppedResolutionRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	resolve, err := ResolveBinding(r.Resolve)
	if err != nil {
		return "", err
	}
	return binding(struct {
		Resolve           f.Digest
		CallID            CallID
		Input             InputIdentity
		PreparedProcessID oc.ProcessID
	}{resolve, r.CallID, r.Input, r.PreparedProcessID})
}

// The domain proves its private original call, request/source, input and
// prepared process under the current guard. Cleanup after cancellation may be
// authorized, but this authority never grants Resolve, Begin or a retry.
// Plans must bind StoppedResolutionBinding and have no RetryPolicy.
type StoppedResolutionAuthority interface {
	DiscoverStoppedResolution(context.Context, StoppedResolutionRequest) (ConsumerDependencies, error)
	ValidateStoppedResolutionInTx(context.Context, f.Tx, StoppedResolutionRequest, ConsumerDependencies) error
}

// A successful observation always has Settled=true, after actual old-process
// confirmation and revalidation under the original resolution and call locks.
// Model=nil proves that the original resolution never committed/acquired its
// lease; it does not authorize another attempt. A nonnil Model is the original
// immutable resolution, whose lease still needs formal retirement/observation.
// CallFound requires the separate StoppedCall observer before domain closure;
// it does not claim that the invocation or its Usage is already retired.
type StoppedResolutionObservation struct {
	Settled   bool
	Model     *ResolvedModel
	CallFound bool
}

func (o StoppedResolutionObservation) Validate() error {
	if !o.Settled || o.Model == nil && o.CallFound || o.Model != nil && (o.Model.Validate() != nil || o.Model.LeaseOwner.Details().Kind != sc.ModelCallOwner) {
		return bad()
	}
	return nil
}
func (o StoppedResolutionObservation) Clone() StoppedResolutionObservation {
	if o.Model != nil {
		x := o.Model.Clone()
		o.Model = &x
	}
	return o
}

type StoppedResolutionObserver interface {
	LookupStoppedResolution(context.Context, StoppedResolutionRequest) (StoppedResolutionObservation, error)
}

func (StoppedResolutionRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_stopped_resolution_request"))
}
func (StoppedResolutionRequest) LogValue() slog.Value {
	return slog.StringValue("model_stopped_resolution_request")
}
func (StoppedResolutionRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"model_stopped_resolution_request"`), nil
}
func (StoppedResolutionObservation) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_stopped_resolution_observation"))
}
func (StoppedResolutionObservation) LogValue() slog.Value {
	return slog.StringValue("model_stopped_resolution_observation")
}
func (StoppedResolutionObservation) MarshalJSON() ([]byte, error) {
	return []byte(`"model_stopped_resolution_observation"`), nil
}
