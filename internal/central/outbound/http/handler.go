// Package outboundhttp exposes the System policy boundary without moving
// browser authorization into the outbound engine.
package outboundhttp

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

const policyPath = "/api/v1/system/outbound-policy"

type SystemHTTPOptions struct{ PublicOrigin string }

type policyService interface {
	GetPolicy(context.Context, identity.Actor) (outbound.Policy, error)
	UpdatePolicy(context.Context, identity.Actor, outbound.CommandMeta, outbound.Rules) (outbound.UpdateResult, error)
}

type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireSystem(*http.Request, identity.AccessIntent) (identity.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}

type systemHTTP struct {
	policy   policyService
	boundary accountBoundary
}

// NewSystemHTTPHandler performs no I/O. The root supplies the already-created
// policy singleton; this adapter neither initializes nor reloads its mirror.
func NewSystemHTTPHandler(policy *outbound.PolicyService, accounts *account.Service, options SystemHTTPOptions) (http.Handler, error) {
	if policy == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	boundary, err := account.NewHTTPBoundary(accounts, options.PublicOrigin)
	if err != nil {
		return nil, err
	}
	return &systemHTTP{policy: policy, boundary: boundary}, nil
}

func (h *systemHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The shared projector intentionally ignores write errors. This local wrapper
	// aborts a failed/short write instead of trying to append another Problem.
	w = abortWriter{w}
	if err := h.boundary.CheckRequest(w, r); err != nil {
		h.boundary.WriteProblem(w, r, err)
		return
	}
	if r.URL.Path != policyPath {
		h.boundary.WriteProblem(w, r, fault(foundation.NotFound))
		return
	}
	intent, budget := identity.Read, 3*time.Second
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		intent, budget = identity.Mutate, 30*time.Second
	default:
		w.Header().Set("Allow", "GET, PUT")
		h.boundary.WriteProblem(w, r, fault(foundation.MethodNotAllowed))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()
	r = r.WithContext(ctx)
	finish, err := connectionBudget(w, ctx)
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
		return
	}
	// Finish stops/joins the native deadline callback before the deferred cancel.
	// Cancelling a successful request first could expire its buffered response.
	defer func() { finish(r.Body) }()
	actor, err := h.boundary.RequireSystem(r, intent)
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		h.boundary.WriteProblem(w, r, fault(foundation.InvalidArgument))
		return
	}
	var body []byte
	if r.Method == http.MethodGet {
		err = noBody(r)
		if err == nil {
			err = beforeCall(ctx)
		}
		if err == nil {
			var policy outbound.Policy
			policy, err = h.policy.GetPolicy(ctx, actor)
			if err == nil {
				body, err = encodePolicy(policy)
			}
		}
	} else {
		var meta outbound.CommandMeta
		var rules outbound.Rules
		meta, rules, err = decodeUpdate(w, r, actor)
		if err == nil {
			err = beforeCall(ctx)
		}
		if err == nil {
			var result outbound.UpdateResult
			result, err = h.policy.UpdatePolicy(ctx, actor, meta, rules)
			if err == nil {
				body, err = encodeUpdate(result)
			}
		}
	}
	if err != nil {
		// Keep the service's real commit classification, even after cancellation.
		// A failed network write aborts through abortWriter; no replacement intent
		// or synthetic "not started" follows a possibly accepted command.
		if expired(ctx) {
			panic(http.ErrAbortHandler)
		}
		h.boundary.WriteProblem(w, r, err)
		return
	}
	if expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func beforeCall(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fault(foundation.DependencyUnavailable).WithCause(err)
	}
	if expired(ctx) {
		return fault(foundation.DependencyUnavailable).WithCause(context.DeadlineExceeded)
	}
	return nil
}

func expired(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return ctx.Err() != nil || ok && !time.Now().Before(deadline)
}

func noBody(r *http.Request) error {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return fault(foundation.InvalidArgument)
	}
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	// Check the actual body as well as framing. The native read deadline owns
	// this read, including an adapter that claimed a zero Content-Length.
	b, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(b) != 0 {
		return fault(foundation.InvalidArgument)
	}
	return nil
}

type abortWriter struct{ http.ResponseWriter }

func (w abortWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w abortWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if err != nil || n != len(b) {
		panic(http.ErrAbortHandler)
	}
	return n, nil
}

// The callback only changes native deadlines; it never publishes a response or
// launches a service call. Its actual completion precedes deadline reset.
func connectionBudget(w http.ResponseWriter, ctx context.Context) (func(io.ReadCloser), error) {
	controller := http.NewResponseController(w)
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, fault(foundation.DependencyUnavailable)
	}
	if err := controller.SetReadDeadline(deadline); err != nil {
		return nil, fault(foundation.DependencyUnavailable)
	}
	if err := controller.SetWriteDeadline(deadline); err != nil {
		if controller.SetReadDeadline(time.Time{}) != nil {
			panic(http.ErrAbortHandler)
		}
		return nil, fault(foundation.DependencyUnavailable)
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		_ = controller.SetReadDeadline(time.Now())
		_ = controller.SetWriteDeadline(time.Now())
	})
	return func(body io.ReadCloser) {
		if body != nil {
			_ = body.Close()
		}
		if !stop() {
			<-done
		}
		readErr := controller.SetReadDeadline(time.Time{})
		writeErr := controller.SetWriteDeadline(time.Time{})
		if readErr != nil || writeErr != nil {
			panic(http.ErrAbortHandler)
		}
	}, nil
}

func fault(code foundation.Code) *foundation.Fault {
	return foundation.NewFault(code, foundation.NotStarted)
}
