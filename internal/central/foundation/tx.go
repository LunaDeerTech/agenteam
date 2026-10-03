package foundation

import (
	"fmt"
	"io"
	"log/slog"
)

// Tx is a comparable, opaque callback-local handle. Its identity is neither an
// authorization grant nor a connection; the adapter must check owner and life.
type Tx struct{ identity *txIdentity }
type txIdentity struct{ nonzero byte }

func NewTx() Tx                           { return Tx{identity: &txIdentity{1}} }
func (t Tx) Valid() bool                  { return t.identity != nil }
func (t Tx) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "opaque_tx") }
func (t Tx) MarshalJSON() ([]byte, error) { return []byte(`"opaque_tx"`), nil }
func (t Tx) LogValue() slog.Value         { return slog.StringValue("opaque_tx") }

// TransactionAttempt identifies one physical attempt, not a durable outcome.
type TransactionAttempt struct{}

// CommitResult deliberately does not implement error. Unknown requires lookup
// by the original cause; no generic transaction-result store is implied.
type CommitResult struct {
	state   CommitState
	fault   *Fault
	attempt ID[TransactionAttempt]
	cause   TransactionCause
}

func CommittedResult() CommitResult { return CommitResult{state: Committed} }
func NotCommittedResult(fault *Fault) CommitResult {
	if fault == nil {
		fault = NewFault(InternalError, NotCommitted)
	}
	copy := *fault
	copy.FieldErrors = append([]FieldError(nil), fault.FieldErrors...)
	copy.CommitState = NotCommitted
	return CommitResult{state: NotCommitted, fault: &copy}
}
func UnknownResult(attempt ID[TransactionAttempt], cause TransactionCause) CommitResult {
	return CommitResult{state: Unknown, attempt: attempt, cause: cause}
}
func (r CommitResult) State() CommitState { return r.state.Safe() }
func (r CommitResult) Fault() *Fault {
	if r.fault == nil {
		return nil
	}
	copy := *r.fault
	copy.FieldErrors = append([]FieldError(nil), r.fault.FieldErrors...)
	return &copy
}
func (r CommitResult) AttemptID() ID[TransactionAttempt] { return r.attempt }
func (r CommitResult) Cause() TransactionCause           { return r.cause }
func (r CommitResult) Format(w fmt.State, _ rune)        { _, _ = io.WriteString(w, string(r.State())) }
func (r CommitResult) MarshalJSON() ([]byte, error) {
	return []byte(`{"state":"` + string(r.State()) + `"}`), nil
}
func (r CommitResult) LogValue() slog.Value { return slog.StringValue(string(r.State())) }
