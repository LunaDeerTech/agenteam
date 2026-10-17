package outbound

import (
	"context"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

type modelSendGuardKey struct{}

// ModelSendGuard is a trusted in-process business authority, never an HTTP
// input. It must synchronously invoke write at most once while its current
// source authorization locks are held. It cannot open any other network I/O.
type ModelSendGuard func(context.Context, func() (int, error)) (int, error)

// WithModelSendGuard binds one model operation to its source authorization.
// Each actual HTTP attempt (including a zero-byte retry or redirect) rechecks
// it. TCP/proxy CONNECT/TLS handshakes do not invoke this application-data gate.
func WithModelSendGuard(ctx context.Context, guard ModelSendGuard) context.Context {
	return context.WithValue(ctx, modelSendGuardKey{}, guard)
}

func (w *firstWriter) guardedFirstWrite(b []byte) (int, error) {
	guard, _ := w.a.ctx.Value(modelSendGuardKey{}).(ModelSendGuard)
	if guard == nil {
		return w.writeFirst(w.a.ctx, b)
	}
	if w.a.profile.Consumer != ac.Model {
		return 0, w.a.fail(ac.ConsumerDenied, nil)
	}
	ctx, cancel := context.WithTimeout(w.a.ctx, 2*time.Second)
	defer cancel()
	called := false
	return guard(ctx, func() (int, error) {
		if called {
			return 0, w.a.fail(ac.InternalError, nil)
		}
		called = true
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return w.writeFirst(ctx, b)
	})
}
