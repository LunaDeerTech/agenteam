package recoverylog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type WriteTicket struct{ data func() *work }

func (t WriteTicket) Wait(ctx context.Context) (Result, error) {
	if t.data == nil {
		return Result{NotWritten}, bad()
	}
	w := t.data()
	select {
	case <-w.done:
		return w.out.result, w.out.err
	default:
	}
	select {
	case <-w.done:
		return w.out.result, w.out.err
	case <-ctx.Done():
		return Result{Unknown}, failure(foundation.DependencyUnavailable, ctx.Err())
	}
}
func (t WriteTicket) Done() <-chan struct{} {
	if t.data == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return t.data().done
}

// Cancel closes only admission. Already granted work remains in flight, and
// Done still waits for the actual Write/Sync and material destruction.
func (t WriteTicket) Cancel() {
	if t.data != nil {
		t.data().cancelBeforeGrant()
	}
}
func (t WriteTicket) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "recovery_log_ticket") }
func (t WriteTicket) MarshalJSON() ([]byte, error) { return []byte(`"recovery_log_ticket"`), nil }
func (*WriteTicket) UnmarshalJSON([]byte) error    { return bad() }
func (t WriteTicket) LogValue() slog.Value         { return slog.StringValue("recovery_log_ticket") }
func validAdmission(a FirstWriteAdmission) bool {
	if a == nil {
		return false
	}
	v := reflect.ValueOf(a)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice, reflect.Interface, reflect.Chan:
		return !v.IsNil()
	}
	return true
}
func (s *Sink) SubmitInvitation(ctx context.Context, r InvitationRecord, a FirstWriteAdmission) (WriteTicket, error) {
	if r.data == nil || !validAdmission(a) {
		return WriteTicket{}, bad()
	}
	return s.enqueue(ctx, r.data(), a)
}
func (s *Sink) SubmitReset(ctx context.Context, r ResetRecord, a FirstWriteAdmission) (WriteTicket, error) {
	if r.data == nil || !validAdmission(a) {
		return WriteTicket{}, bad()
	}
	return s.enqueue(ctx, r.data(), a)
}
