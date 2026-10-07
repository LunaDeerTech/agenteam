//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

type projectUsageFlight struct {
	done    chan struct{}
	writer  *projectUsageRecorder
	aborted bool
}

func (v *projectUsageHTTPFixture) flight(ctx context.Context, browser systemHTTPBrowser, path string) *projectUsageFlight {
	flight := &projectUsageFlight{done: make(chan struct{}), writer: &projectUsageRecorder{ResponseRecorder: httptest.NewRecorder()}}
	go func() {
		defer close(flight.done)
		defer func() {
			if p := recover(); p != nil {
				if p != http.ErrAbortHandler {
					panic(p)
				}
				flight.aborted = true
			}
		}()
		v.handler.ServeHTTP(flight.writer, projectUsageRequest(ctx, browser, "GET", path))
	}()
	return flight
}
func (r *projectUsageFlight) result() systemHTTPResponse {
	return systemHTTPResponse{r.writer.Code, r.writer.Header().Clone(), bytes.Clone(r.writer.Body.Bytes())}
}

func TestModelProjectUsageHTTPAuthorityAndTerminal(t *testing.T) {
	started := time.Now()
	defer projectUsageComplete(t, started)
	v := newProjectUsageHTTPFixture(t)
	invocation := v.success(t, json.RawMessage(`{"prompt_tokens":7,"completion_tokens":0,"total_tokens":7}`), nil)
	path := projectUsagePath(v.project.ID)
	resources := []string{path, path + "/summary?group_by=day", projectUsageResolve(v.ownerName, v.project.Name)}
	for _, browser := range []systemHTTPBrowser{v.otherBrowser, v.adminBrowser} {
		for _, resource := range resources {
			v.request(t, browser, "GET", resource).problem(t, 404, f.NotFound)
		}
	}
	for _, resource := range resources {
		v.request(t, systemHTTPBrowser{}, "GET", resource).problem(t, 401, f.Unauthenticated)
	}
	// Observed Store decorators apply only after the real transaction has ended;
	// they test publication behavior and do not simulate TCP commit uncertainty.
	for _, resource := range resources {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run("Unknown/"+method+resource, func(t *testing.T) {
				marker := newID[f.TransactionAttempt](t)
				var actual atomic.Bool
				v.tracked.hooks(nil, func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
					if projectUsageReadCause(cause) || cause.Kind() == f.RecoveryCause && cause.Details().Owner == "project.resolve" {
						actual.Store(result.State() == f.Committed)
						return f.UnknownResult(marker, cause)
					}
					return result
				})
				defer v.tracked.hooks(nil, nil)
				response := v.request(t, v.ownerBrowser, method, resource)
				response.want(t, 503)
				if !actual.Load() || bytes.Contains(response.body, []byte(invocation.event.Identity.Attempt.InvocationID.String())) || bytes.Contains(response.body, []byte(v.project.Name)) {
					t.Fatal("Unknown retained a candidate or skipped actual transaction")
				}
				if method == "HEAD" {
					if len(response.body) != 0 {
						t.Fatal("Unknown HEAD wrote body")
					}
				} else {
					var problem httpapi.Problem
					if json.Unmarshal(response.body, &problem) != nil || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.RetryHint != "lookup" {
						t.Fatal("Unknown identity lost")
					}
				}
			})
		}
	}
	t.Run("UserSH-before-formal-Logout", func(t *testing.T) {
		browser := v.login(t, v.ownerBrowser.email)
		user, _ := f.UserLock(browser.actor.Details().UserID)
		project, _ := f.ProjectLock(v.project.ID.String())
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		ctx, cancel := context.WithCancel(testContext(t))
		defer cancel()
		var hit atomic.Bool
		v.tracked.hooks(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if projectUsageReadCause(cause) && hit.CompareAndSwap(false, true) {
				if err := v.raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}}); err != nil {
					return err
				}
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}, nil)
		flight := v.flight(ctx, browser, path)
		defer func() { unblock(); cancel(); <-flight.done; v.tracked.hooks(nil, nil) }()
		waitSignal(t, entered)
		logoutDone := make(chan error, 1)
		logoutCtx, logoutCancel := context.WithCancel(testContext(t))
		defer logoutCancel()
		go func() {
			logoutDone <- v.core.Logout(logoutCtx, account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())})
		}()
		var logoutErr error
		joined := false
		defer func() {
			unblock()
			if !joined {
				logoutCancel()
				logoutErr = <-logoutDone
			}
		}()
		managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, user)
		if flight.writer.writeCount() != 0 {
			t.Fatal("candidate published before actual read transaction end")
		}
		unblock()
		<-flight.done
		logoutErr = <-logoutDone
		joined = true
		if flight.aborted || logoutErr != nil {
			t.Fatal("read/Logout actual terminal", logoutErr)
		}
		flight.result().want(t, 200)
		v.tracked.hooks(nil, nil)
		v.request(t, browser, "GET", path).problem(t, 401, f.SessionRevoked)
		t.Log("real User SH retained current read until commit; formal Logout User EX waited then revoked later request")
	})
	t.Run("ProjectSH-blocks-ProjectEX", func(t *testing.T) {
		project, _ := f.ProjectLock(v.project.ID.String())
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		var hit atomic.Bool
		v.tracked.hooks(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if projectUsageReadCause(cause) && hit.CompareAndSwap(false, true) {
				if err := v.raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: project, Mode: f.Shared}}); err != nil {
					return err
				}
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}, nil)
		ctx, cancel := context.WithCancel(testContext(t))
		defer cancel()
		flight := v.flight(ctx, v.ownerBrowser, path+"/summary?group_by=day")
		defer func() { unblock(); cancel(); <-flight.done; v.tracked.hooks(nil, nil) }()
		waitSignal(t, entered)
		writeDone := make(chan f.CommitResult, 1)
		writeCtx, writeCancel := context.WithCancel(testContext(t))
		defer writeCancel()
		cause := recoveryCause(t)
		go func() {
			writeDone <- v.raw.WithinTx(writeCtx, cause, func(ctx context.Context, tx f.Tx) error {
				return v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: project, Mode: f.Exclusive}})
			})
		}()
		joined := false
		defer func() {
			unblock()
			if !joined {
				writeCancel()
				<-writeDone
			}
		}()
		managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, project)
		if flight.writer.writeCount() != 0 {
			t.Fatal("aggregate published with transaction still live")
		}
		unblock()
		<-flight.done
		result := <-writeDone
		joined = true
		if flight.aborted || result.State() != f.Committed {
			t.Fatal("Project lock owners did not commit", result.Fault())
		}
		flight.result().want(t, 200)
	})
	t.Run("actual-parent-cancel-tail", func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		var hit atomic.Bool
		v.tracked.hooks(func(ctx context.Context, _ f.Tx, cause f.TransactionCause) error {
			if projectUsageReadCause(cause) && hit.CompareAndSwap(false, true) {
				close(entered)
				<-release
				return ctx.Err()
			}
			return nil
		}, nil)
		ctx, cancel := context.WithTimeout(testContext(t), 120*time.Millisecond)
		defer cancel()
		flight := v.flight(ctx, v.ownerBrowser, path)
		defer func() { unblock(); cancel(); <-flight.done; v.tracked.hooks(nil, nil) }()
		waitSignal(t, entered)
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal("parent expiry changed")
		}
		select {
		case <-flight.done:
			t.Fatal("returned before actual transaction callback tail")
		default:
		}
		if flight.writer.writeCount() != 0 {
			t.Fatal("cancelled candidate published")
		}
		unblock()
		<-flight.done
		if !flight.aborted || len(flight.writer.Body.Bytes()) != 0 {
			t.Fatal("cancelled handler did not abort without candidate")
		}
	})
	t.Run("earlier-PG-lock-timeout", func(t *testing.T) {
		project, _ := f.ProjectLock(v.project.ID.String())
		release := managementHold(t, &systemHTTPFixture{fixture: v.fixture}, project, f.Exclusive)
		defer release()
		observed := make(chan f.CommitResult, 1)
		v.tracked.hooks(nil, func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if projectUsageReadCause(cause) {
				select {
				case observed <- result:
				default:
				}
			}
			return result
		})
		defer v.tracked.hooks(nil, nil)
		began := time.Now()
		response := v.request(t, v.ownerBrowser, "GET", path)
		elapsed := time.Since(began)
		response.problem(t, 500, f.InternalError)
		var problem httpapi.Problem
		if json.Unmarshal(response.body, &problem) != nil || problem.CommitState != f.NotCommitted {
			t.Fatal("PG lock timeout lost its original commit state")
		}
		select {
		case result := <-observed:
			if result.State() != f.NotCommitted {
				t.Fatal("PG lock timeout did not complete its actual transaction")
			}
			requireProjectLockTimeout(t, result.Fault())
		default:
			t.Fatal("PG lock timeout skipped terminal observation")
		}
		if elapsed < 600*time.Millisecond || elapsed >= 1900*time.Millisecond {
			t.Fatal("PG one-second timeout not distinct from HTTP two-second expiry")
		}
		for _, forbidden := range []string{invocation.event.Identity.Attempt.InvocationID.String(), `"items"`, "DATABASE_LOCK_FAILED", "55P03", "pg_advisory"} {
			if bytes.Contains(response.body, []byte(forbidden)) {
				t.Fatal("PG failure published a candidate or private database cause")
			}
		}
		release()
		t.Log("earlier PG lock_timeout completed safely; this is not natural HTTP budget evidence")
	})
	t.Log("formal Owner isolation, real User/Project shared locks, original Unknown and actual cancelled transaction tails verified with controlled writer; no TCP commit-visibility claim")
}
