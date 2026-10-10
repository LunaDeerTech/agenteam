// Package launchtemporary holds Execution's private launch rejection binding.
// Go's internal boundary prevents other domains from issuing this marker.
package launchtemporary

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type binding struct {
	digest  f.Digest
	request f.ID[f.Request]
	key     f.IdempotencyKey
}

type rejection struct {
	binding func() binding
	cause   func() error
}

func (*rejection) Error() string { return "execution_launch_lock_timeout" }
func (*rejection) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_launch_lock_timeout")
}
func (*rejection) LogValue() slog.Value {
	return slog.StringValue("execution_launch_lock_timeout")
}
func (*rejection) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_launch_lock_timeout"`), nil
}
func (r *rejection) Unwrap() error {
	if r == nil || r.cause == nil {
		return nil
	}
	return r.cause()
}

// MintLockTimeout is called only after the final Launch transaction has returned
// NotCommitted with its original PostgreSQL lock-timeout poison. It does not
// decide that physical outcome itself. The original cause remains explicitly
// inspectable, while ordinary error, JSON and log formatting stays safe.
func MintLockTimeout(digest f.Digest, request f.ID[f.Request], key f.IdempotencyKey, cause error) error {
	if digest.Validate() != nil || request.Validate() != nil || key.Validate() != nil || cause == nil {
		return nil
	}
	// Closures also prevent recursive formatting of an enclosing private field
	// from traversing request material or the diagnostic cause.
	return &rejection{binding: func() binding { return binding{digest, request, key} }, cause: func() error { return cause }}
}

func Match(err error, digest f.Digest, request f.ID[f.Request], key f.IdempotencyKey) bool {
	var marker *rejection
	if digest.Validate() != nil || request.Validate() != nil || key.Validate() != nil ||
		!errors.As(err, &marker) || marker == nil || marker.binding == nil {
		return false
	}
	original := marker.binding()
	return original.digest == digest && original.request == request && original.key == key
}
