package recoverylog

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type RecordIdentity struct{ Purpose, ResourceID, AttemptID string }

// Authorize runs synchronously on the sole writer, after encoding. The trusted
// adapter must consume this work's GrantOnce synchronously, never delegate it.
type FirstWriteAdmission interface {
	Authorize(context.Context, RecordIdentity, GrantOnce) error
}
type GrantOnce struct{ grant func() error }

func (g GrantOnce) Grant() error {
	if g.grant == nil {
		return bad()
	}
	return g.grant()
}
func (g GrantOnce) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "recovery_log_grant") }
func (g GrantOnce) MarshalJSON() ([]byte, error) { return []byte(`"recovery_log_grant"`), nil }
func (*GrantOnce) UnmarshalJSON([]byte) error    { return bad() }
func (g GrantOnce) LogValue() slog.Value         { return slog.StringValue("recovery_log_grant") }

func (w *work) cancelBeforeGrant() {
	w.mu.Lock()
	if !w.granted {
		w.cancelled = true
		w.cancel()
	}
	w.mu.Unlock()
}
func (w *work) admit() error {
	if w.admission == nil {
		return nil
	} // Preserved bootstrap/legacy synchronous API.
	w.mu.Lock()
	if w.cancelled || w.ctx.Err() != nil {
		w.mu.Unlock()
		return failure(foundation.ShuttingDown, w.ctx.Err())
	}
	w.authorizing = true
	w.mu.Unlock()
	g := GrantOnce{func() error {
		w.mu.Lock()
		defer w.mu.Unlock()
		if !w.authorizing || w.granted || w.cancelled || w.ctx.Err() != nil {
			return failure(foundation.InvalidState, w.ctx.Err())
		}
		w.granted = true
		return nil
	}}
	identity := RecordIdentity{w.record.purpose, w.record.id, w.record.attempt}
	e := w.admission.Authorize(w.ctx, identity, g)
	w.mu.Lock()
	w.authorizing = false
	granted := w.granted
	w.mu.Unlock()
	if granted {
		return nil
	} // Admission cannot be revoked after its linearization.
	if e != nil {
		return failure(foundation.DependencyUnavailable, e)
	}
	return failure(foundation.Forbidden, nil)
}
