package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// StoppedCallObservationRequest identifies an original logical call. These
// associations are not a death proof or permission to recover or resend it.
type StoppedCallObservationRequest struct {
	Actor      id.Actor
	Consumer   Consumer
	CallID     CallID
	Input      InputIdentity
	SnapshotID SnapshotID
}

func (r StoppedCallObservationRequest) Validate() error {
	if !actorMatches(r.Actor, r.Consumer) || r.Consumer.Validate() != nil || r.CallID.Validate() != nil || r.Input.ValidateFor(r.Consumer) != nil || r.SnapshotID.Validate() != nil {
		return bad()
	}
	return nil
}
func (r StoppedCallObservationRequest) Clone() StoppedCallObservationRequest {
	r.Consumer, r.Input = r.Consumer.Clone(), r.Input.Clone()
	return r
}
func StoppedCallObservationBinding(r StoppedCallObservationRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	return binding(struct {
		Actor      id.ActorDetails
		Consumer   Consumer
		CallID     CallID
		Input      InputIdentity
		SnapshotID SnapshotID
	}{r.Actor.Details(), r.Consumer, r.CallID, r.Input, r.SnapshotID})
}

// The domain must prove the original durable owner and current ProcessGuard.
// Cancellation may permit this cleanup observation, never a new Resolve/Begin.
// RetryPolicy must be nil. No DTO or snapshot foreign key grants this access.
type StoppedCallAuthority interface {
	DiscoverStoppedCall(context.Context, StoppedCallObservationRequest) (ConsumerDependencies, error)
	ValidateStoppedCallInTx(context.Context, f.Tx, StoppedCallObservationRequest, ConsumerDependencies) error
}

// Found=false proves only absence of a Model call row under its original lock;
// it does not prove that a consumer or pre-admission borrower stopped. A found
// result is returned only after the exact old process is confirmed stopped,
// Model is retired, and its Usage borrowers are closed. A known success still
// carries no provider output and must not be turned into a fabricated result.
type StoppedCallObservation struct {
	Found   bool
	Attempt *AttemptIdentity
	Status  string
	Retired bool
}

func (o StoppedCallObservation) Validate() error {
	if !o.Found {
		if o.Attempt != nil || o.Status != "" || o.Retired {
			return bad()
		}
		return nil
	}
	if !o.Retired || o.Attempt == nil || o.Attempt.Validate() != nil || !one(o.Status, "succeeded", "failed", "cancelled", "unknown") {
		return bad()
	}
	return nil
}

type StoppedCallObserver interface {
	LookupStoppedCall(context.Context, StoppedCallObservationRequest) (StoppedCallObservation, error)
}

func (StoppedCallObservationRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_stopped_call_request"))
}
func (StoppedCallObservationRequest) LogValue() slog.Value {
	return slog.StringValue("model_stopped_call_request")
}
func (StoppedCallObservationRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"model_stopped_call_request"`), nil
}
func (StoppedCallObservation) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_stopped_call_observation"))
}
func (StoppedCallObservation) LogValue() slog.Value {
	return slog.StringValue("model_stopped_call_observation")
}
