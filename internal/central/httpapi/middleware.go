package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type requestIDKey struct{}

// RequestID returns the server-owned transport identity, or an invalid zero ID
// outside a traced request. It is unrelated to the business Idempotency-Key.
func RequestID(ctx context.Context) foundation.ID[foundation.Request] {
	id, _ := ctx.Value(requestIDKey{}).(foundation.ID[foundation.Request])
	return id
}

// WithRequestID is the outer middleware. Incoming trace headers are ignored.
// logger may be nil to disable logging; otherwise only safe explicit fields are emitted.
func WithRequestID(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id, err := foundation.NewID[foundation.Request]()
		if err != nil {
			if logger != nil {
				logger.ErrorContext(r.Context(), "http_identity_failed", "event", "http_identity_failed", "code", foundation.InternalError)
			}
			writeEntropyFailure(w)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		w, state := trackResponse(w)
		w.Header().Set("X-Request-ID", id.String())
		defer func() {
			if logger == nil {
				return
			}
			route := r.Pattern
			if route == "" {
				route = "unknown_route"
			}
			fields := []any{
				"event", "http_request", "request_id", id.String(), "method", safeMethod(r.Method),
				"route", route, "status", state.finalStatus(), "duration", time.Since(started), "bytes", state.bytes,
			}
			if state.code != "" {
				fields = append(fields, "code", state.code.Safe())
			}
			logger.InfoContext(r.Context(), "http_request", fields...)
		}()
		next.ServeHTTP(w, r)
	})
}

// Recover must sit inside WithRequestID and outside routing. It never formats a
// panic value or a raw stack; committed responses are aborted without a suffix.
func Recover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w, state := trackResponse(w)
		defer func() {
			value := recover()
			if value == nil {
				return
			}
			if value == http.ErrAbortHandler {
				state.abort()
				panic(http.ErrAbortHandler)
			}
			state.code = foundation.InternalError
			if logger != nil {
				logger.ErrorContext(r.Context(), "http_panic", "event", "http_panic", "request_id", RequestID(r.Context()).String(), "code", foundation.InternalError)
			}
			if state.committed {
				state.abort()
				panic(http.ErrAbortHandler)
			}
			WriteProblem(w, r, foundation.NewFault(foundation.InternalError, foundation.Unknown))
		}()
		next.ServeHTTP(w, r)
	})
}

// Handler installs the required middleware order for a Central HTTP router.
func Handler(logger *slog.Logger, next http.Handler) http.Handler {
	return WithRequestID(logger, Recover(logger, next))
}

func safeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return method
	default:
		return "unknown_method"
	}
}
