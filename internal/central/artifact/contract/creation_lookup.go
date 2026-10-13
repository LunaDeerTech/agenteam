package contract

// CreationState describes the original inline creation command. Pending and
// cancellation states grant no right to read an Object or its payload.
type CreationState string

const (
	CreationNotObserved     CreationState = "not_observed"
	CreationPending         CreationState = "pending"
	CreationCommitted       CreationState = "committed"
	CreationCancelRequested CreationState = "cancel_requested"
	CreationCancelled       CreationState = "cancelled"
)

// CreationLookupResult is a current authorized observation, not a reservation
// of the idempotency key. Only a committed command carries Artifact metadata.
type CreationLookupResult struct {
	State    CreationState
	Artifact *Metadata
}

func (r CreationLookupResult) Validate() error {
	switch r.State {
	case CreationCommitted:
		if r.Artifact == nil || r.Artifact.Validate() != nil {
			return invalid()
		}
	case CreationNotObserved, CreationPending, CreationCancelRequested, CreationCancelled:
		if r.Artifact != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
