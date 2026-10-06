//go:build integration

package model_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// This observer delegates every SQL operation and transaction. Hooks run only
// after the real row was scanned while its original transaction is still live.
type managementObservedStore struct {
	*systemHTTPStore
	mu        sync.Mutex
	reads     map[string]int
	afterRead func(context.Context, string) error
}
type managementObservedExecutor struct {
	postgres.SQLExecutor
	store *managementObservedStore
}
type managementObservedRow struct {
	postgres.Row
	ctx   context.Context
	store *managementObservedStore
	kind  string
}

func (s *managementObservedStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, err := s.systemHTTPStore.InTx(tx)
	if err != nil {
		return nil, err
	}
	return managementObservedExecutor{x, s}, nil
}
func (x managementObservedExecutor) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	kind := "other"
	switch {
	case strings.Contains(q, "FROM agenteam_secret.secrets WHERE id="):
		kind = "metadata"
	case strings.Contains(q, "SELECT m.version"):
		kind = "model"
	case strings.Contains(q, "WITH bounded AS MATERIALIZED"):
		kind = "counts"
	case strings.Contains(q, "LIMIT 5"):
		kind = "platform"
	}
	return managementObservedRow{x.SQLExecutor.QueryRow(ctx, q, args...), ctx, x.store, kind}
}
func (r managementObservedRow) Scan(dst ...any) error {
	err := r.Row.Scan(dst...)
	if err == nil {
		r.store.mu.Lock()
		r.store.reads[r.kind]++
		hook := r.store.afterRead
		r.store.mu.Unlock()
		if hook != nil {
			return hook(r.ctx, r.kind)
		}
	}
	return err
}
func (s *managementObservedStore) count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads[kind]
}

func managementReaders(t *testing.T, v *systemHTTPFixture) (*managementObservedStore, *model.Service, *secret.Service) {
	t.Helper()
	store := &managementObservedStore{systemHTTPStore: v.tracked, reads: map[string]int{}}
	_, _, keys := testKeys(t)
	secrets, err := secret.New(store, keys, v.aud, secret.Authorizations{Sessions: v.accounts, System: v.accounts})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := model.NewAuthority(store, model.Authorizations{Sessions: v.accounts, System: v.accounts})
	if err != nil {
		t.Fatal(err)
	}
	core, err := model.New(store, authority, v.deps)
	if err != nil {
		t.Fatal(err)
	}
	return store, core, secrets
}
func managementInstall(t *testing.T, v *systemHTTPFixture, core *model.Service, secrets sc.HumanWriteCommands) {
	t.Helper()
	handler, err := model.NewSystemHTTPHandler(core, v.account, secrets, model.SystemHTTPOptions{PublicOrigin: systemHTTPOrigin})
	if err != nil {
		t.Fatal(err)
	}
	v.http = httpapi.Handler(slog.New(slog.NewJSONHandler(v.log, nil)), handler)
}
func managementRef(t *testing.T, key string) sc.CredentialRef {
	t.Helper()
	parsed, err := f.ParseID[sc.Credential](key)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := sc.NewCredentialRef(parsed, id.SystemScope())
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
func managementModel(t *testing.T, key string) mc.ModelID {
	t.Helper()
	parsed, err := f.ParseID[mc.Model](key)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func managementWaitBlocked(t *testing.T, v *systemHTTPFixture, key f.LockKey) {
	t.Helper()
	ctx, cancel := context.WithTimeout(testContext(t), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	n := uint64(key.AdvisoryKey())
	for {
		var waiting bool
		err := v.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid::bigint=$1 AND objid::bigint=$2 AND objsubid=1 AND NOT granted)`, int64(uint32(n>>32)), int64(uint32(n))).Scan(&waiting)
		if err != nil {
			t.Fatal("observe ordinary lock waiter", err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("ordinary writer never waited on read lock")
		case <-ticker.C:
		}
	}
}

func managementHold(t *testing.T, v *systemHTTPFixture, key f.LockKey, mode f.LockMode) func() {
	t.Helper()
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan f.CommitResult, 1)
	ctx, cancel := context.WithCancel(testContext(t))
	go func() {
		done <- v.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
			if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: mode}}); err != nil {
				return err
			}
			close(held)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	waitSignal(t, held)
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(release)
			result := <-done
			cancel()
			if result.State() != f.Committed {
				t.Error("ordinary lock holder did not commit", result.Fault())
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestSystemModelManagementReadAuthorityAndAtomicity(t *testing.T) {
	v := newSystemHTTPFixture(t)
	credential := v.credential(t, newID[struct{}](t).String(), "management-current")
	modelKey := v.model(t, v.provider(t, mc.OpenAIChat), mc.ChatModel)
	for _, kind := range []string{"metadata", "model"} {
		t.Run(kind+"-revoke-after-http-authority", func(t *testing.T) {
			browser := v.addBrowser(t, "admin")
			store, core, secrets := managementReaders(t, v)
			managementInstall(t, v, core, secrets)
			defer v.install(t, v.secrets)
			owner := "secret-metadata"
			path := "/api/v1/system/model-credentials/" + credential
			if kind == "model" {
				owner = "model.management-impact"
				path = "/api/v1/system/models/" + modelKey + "/deletion-impact"
			}
			var entered atomic.Bool
			v.tracked.setHooks(func(_ context.Context, cause f.TransactionCause) error {
				if cause.Details().Owner == owner && entered.CompareAndSwap(false, true) {
					return v.account.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())})
				}
				return nil
			}, nil)
			response := v.request(t, browser, "GET", path, "", nil)
			v.tracked.setHooks(nil, nil)
			if !entered.Load() {
				t.Fatal("read barrier missed")
			}
			response.problem(t, 401, f.SessionRevoked)
			if store.count(kind) != 0 {
				t.Fatal("protected metadata SQL ran after actual Session revocation")
			}
			if err := v.accounts.RequireCurrentSession(testContext(t), f.Tx{}, browser.actor); err == nil {
				t.Fatal("revocation was not durable")
			}
		})
		for _, mutation := range []string{"revoke", "role"} {
			t.Run(kind+"-user-lock-"+mutation, func(t *testing.T) {
				browser := v.addBrowser(t, "admin")
				store, core, secrets := managementReaders(t, v)
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				store.afterRead = func(ctx context.Context, stage string) error {
					if stage == kind {
						close(entered)
						select {
						case <-release:
						case <-ctx.Done():
						}
					}
					return nil
				}
				readDone := make(chan error, 1)
				go func() {
					if kind == "metadata" {
						_, err := secrets.Metadata(testContext(t), browser.actor, managementRef(t, credential))
						readDone <- err
					} else {
						_, err := core.GetModelDeletionImpact(testContext(t), browser.actor, managementModel(t, modelKey))
						readDone <- err
					}
				}()
				waitSignal(t, entered)
				user, _ := f.UserLock(browser.actor.Details().UserID)
				writeDone := make(chan error, 1)
				go func() {
					if mutation == "revoke" {
						writeDone <- v.account.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())})
						return
					}
					result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
						if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Exclusive}}); err != nil {
							return err
						}
						x, err := v.raw.InTx(tx)
						if err != nil {
							return err
						}
						_, err = x.Exec(ctx, `UPDATE agenteam_account.users SET role='user',version=version+1 WHERE id=$1`, browser.actor.Details().UserID)
						return err
					})
					if result.State() != f.Committed {
						writeDone <- result.Fault()
					} else {
						writeDone <- nil
					}
				}()
				managementWaitBlocked(t, v, user)
				unblock()
				if err := <-readDone; err != nil {
					t.Fatal("locked read failed", err)
				}
				if err := <-writeDone; err != nil {
					t.Fatal("actual identity mutation failed", err)
				}
				if mutation == "revoke" {
					if err := v.accounts.RequireCurrentSession(testContext(t), f.Tx{}, browser.actor); err == nil {
						t.Fatal("Session still current")
					}
				} else {
					if _, err := v.accounts.AuthorizeSystem(testContext(t), f.Tx{}, browser.actor, id.Read); err == nil {
						t.Fatal("old admin grant survived")
					}
				}
			})
		}
	}
	t.Run("complete-ordinary-lock-unions", func(t *testing.T) {
		user, _ := f.UserLock(v.admin.Details().UserID)
		cred, _ := f.AggregateLock(f.CredentialRefAggregate, credential)
		modelLock, _ := f.AggregateLock(f.ModelConfigAggregate, modelKey)
		selection, _ := f.SystemConfigLock("model-platform-selection")
		references, _ := f.SystemConfigLock("model-references")
		for _, tc := range []struct {
			name     string
			key      f.LockKey
			mode     f.LockMode
			metadata bool
		}{{"credential", cred, f.Exclusive, true}, {"credential-user", user, f.Exclusive, true}, {"model", modelLock, f.Exclusive, false}, {"model-user", user, f.Exclusive, false}, {"selector", selection, f.Exclusive, false}, {"references-writer-shared", references, f.Shared, false}} {
			t.Run(tc.name, func(t *testing.T) {
				release := managementHold(t, v, tc.key, tc.mode)
				ctx, cancel := context.WithTimeout(testContext(t), 150*time.Millisecond)
				defer cancel()
				if tc.metadata {
					out, err := v.secrets.Metadata(ctx, v.admin, managementRef(t, credential))
					if err == nil || !reflect.DeepEqual(out, sc.Metadata{}) {
						t.Fatal("credential read crossed writer lock")
					}
				} else {
					out, err := v.service.GetModelDeletionImpact(ctx, v.admin, managementModel(t, modelKey))
					if err == nil || !reflect.DeepEqual(out, model.ModelDeletionImpact{}) {
						t.Fatal("impact crossed writer lock")
					}
				}
				release()
			})
		}
	})
	t.Run("preview-does-not-authorize-later-write", func(t *testing.T) {
		preview := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-credentials/"+credential, "", nil).want(t, 200).object(t)
		v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-credentials/"+credential, newID[struct{}](t).String(), map[string]any{"expected_version": preview["version"], "value": "first-current-change"}).want(t, 200)
		v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-credentials/"+credential, newID[struct{}](t).String(), map[string]any{"expected_version": preview["version"], "value": "stale-change"}).problem(t, 409, f.VersionConflict)
		embed := v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel)
		before := managementImpact(t, v, embed)
		if before.ReferenceCount != 0 {
			t.Fatal("preview precondition")
		}
		selection := managementSelection(t, v)
		replacement := selection["embedding"]
		selection["embedding"] = embed
		v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-selection", newID[struct{}](t).String(), selection).want(t, 200)
		current := managementImpact(t, v, embed)
		if current.Version != before.Version || current.ReferenceCount != 1 {
			t.Fatal("reference change must be visible despite unchanged Model.version")
		}
		v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+embed, newID[struct{}](t).String(), map[string]any{"expected_version": before.Version, "replacement": nil}).problem(t, 409, f.InvalidState)
		deleted := v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+embed, newID[struct{}](t).String(), map[string]any{"expected_version": before.Version, "replacement": replacement}).want(t, 200).object(t)
		if deleted["affected_references"] != "1" {
			t.Fatal("delete ignored new reference")
		}
	})
}

func TestSystemModelManagementReadBudgetAndUncertainResult(t *testing.T) {
	v := newSystemHTTPFixture(t)
	credential := v.credential(t, newID[struct{}](t).String(), "management-unknown")
	modelKey := v.model(t, v.provider(t, mc.OpenAIChat), mc.ChatModel)
	for _, kind := range []string{"metadata", "model"} {
		for _, mode := range []string{"unknown", "not-committed", "cancel-committed"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				store, core, secrets := managementReaders(t, v)
				managementInstall(t, v, core, secrets)
				defer v.install(t, v.secrets)
				ctx, cancel := context.WithCancel(testContext(t))
				defer cancel()
				owner := "secret-metadata"
				path := "/api/v1/system/model-credentials/" + credential
				if kind == "model" {
					owner = "model.management-impact"
					path = "/api/v1/system/models/" + modelKey + "/deletion-impact"
				}
				var reached atomic.Int64
				before, n := v.facts(t), v.nonce(t)
				if mode == "not-committed" {
					store.afterRead = func(_ context.Context, stage string) error {
						if kind == "metadata" && stage == "metadata" || kind == "model" && stage == "counts" {
							return f.NewFault(f.DependencyUnavailable, f.NotStarted)
						}
						return nil
					}
				}
				v.tracked.setHooks(nil, func(_ context.Context, cause f.TransactionCause, trace systemHTTPTrace, result f.CommitResult) f.CommitResult {
					if trace.owner != owner {
						return result
					}
					reached.Add(1)
					wantState := f.Committed
					if mode == "not-committed" {
						wantState = f.NotCommitted
					}
					if result.State() != wantState || store.count(kind) != 1 || trace.held == 0 {
						t.Fatal("decorator did not observe actual authorized SQL/commit")
					}
					t.Logf("management read owner=%s actual=%s projected=%s authorizedSQL=%d", owner, result.State(), mode, store.count(kind))
					switch mode {
					case "unknown":
						return f.UnknownResult(newID[f.TransactionAttempt](t), cause)
					case "not-committed":
						return result
					default:
						cancel()
						return result
					}
				})
				r := v.rawRequest(ctx, v.adminBrowser, "GET", path, "", nil, nil)
				v.tracked.setHooks(nil, nil)
				if reached.Load() != 1 {
					t.Fatal("read automatically retried or never reached actual commit")
				}
				code := f.DependencyUnavailable
				if mode == "unknown" {
					code = f.CommitUnknown
				}
				r.problem(t, 503, code)
				for _, field := range []string{"credential_id", "version", "model_id", "reference_count", "reference_groups"} {
					if _, ok := r.object(t)[field]; ok {
						t.Fatal("uncertain/cancelled read exposed candidate")
					}
				}
				if mode == "unknown" && r.object(t)["commit_state"] != "unknown" {
					t.Fatal("Unknown state changed")
				}
				httpSameFacts(t, before, v.facts(t))
				if n != v.nonce(t) {
					t.Fatal("read consumed nonce")
				}
			})
		}
	}
	if v.db.Config(t, nil).LockTimeout() != time.Second {
		t.Fatal("fixture no longer supplies its original one-second lock timeout")
	}
	type readResult struct {
		err  error
		zero bool
	}
	read := func(ctx context.Context, kind string, core *model.Service, secrets *secret.Service) readResult {
		if kind == "metadata" {
			out, err := secrets.Metadata(ctx, v.admin, managementRef(t, credential))
			return readResult{err, reflect.DeepEqual(out, sc.Metadata{})}
		}
		out, err := core.GetModelDeletionImpact(ctx, v.admin, managementModel(t, modelKey))
		return readResult{err, reflect.DeepEqual(out, model.ModelDeletionImpact{})}
	}
	for _, kind := range []string{"metadata", "model"} {
		t.Run(kind+"-earlier-database-lock-timeout", func(t *testing.T) {
			key, _ := f.AggregateLock(f.CredentialRefAggregate, credential)
			if kind == "model" {
				key, _ = f.AggregateLock(f.ModelConfigAggregate, modelKey)
			}
			release := managementHold(t, v, key, f.Exclusive)
			caller := testContext(t)
			started := time.Now()
			done := make(chan readResult, 1)
			go func() { done <- read(caller, kind, v.service, v.secrets) }()
			managementWaitBlocked(t, v, key)
			out := <-done
			elapsed := time.Since(started)
			release()
			if !out.zero || caller.Err() != nil || elapsed > 5*time.Second {
				t.Fatal("earlier lock timeout published data or extended read", elapsed, out.err)
			}
			managementLockFault(t, out.err)
			t.Logf("ordinary database lock wait observed; fixture_timeout=1s elapsed=%s caller_live=true state=not_committed postgres=DATABASE_LOCK_FAILED SQLSTATE=55P03", elapsed)
		})
		t.Run(kind+"-actual-three-second-read-context", func(t *testing.T) {
			store, core, secrets := managementReaders(t, v)
			caller := testContext(t)
			owner, stage := "secret-metadata", "metadata"
			if kind == "model" {
				owner, stage = "model.management-impact", "counts"
			}
			reached := false
			var remaining time.Duration
			store.afterRead = func(ctx context.Context, observed string) error {
				if observed != stage {
					return nil
				}
				deadline, ok := ctx.Deadline()
				remaining = time.Until(deadline)
				if !ok || remaining <= 2*time.Second || remaining > 3*time.Second || caller.Err() != nil || store.count(kind) != 1 {
					t.Fatal("read context budget or actual SQL precondition")
				}
				reached = true
				<-ctx.Done()
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) || caller.Err() != nil {
					t.Fatal("read did not expire independently of caller")
				}
				return ctx.Err()
			}
			actual := f.CommitState("")
			v.tracked.setHooks(nil, func(_ context.Context, _ f.TransactionCause, trace systemHTTPTrace, result f.CommitResult) f.CommitResult {
				if trace.owner == owner {
					actual = result.State()
					if trace.held == 0 || store.count(kind) != 1 {
						t.Fatal("deadline observation without authorized SQL")
					}
				}
				return result
			})
			started := time.Now()
			out := read(caller, kind, core, secrets)
			elapsed := time.Since(started)
			v.tracked.setHooks(nil, nil)
			if !reached || out.err == nil || !out.zero || actual != f.NotCommitted || caller.Err() != nil || elapsed < 2*time.Second || elapsed > 5*time.Second {
				t.Fatal("three-second read context did not terminate with zero DTO", elapsed, actual, out.err)
			}
			t.Logf("actual authorized SQL read; initial_remaining=%s elapsed=%s read_deadline_exceeded=true caller_live=true actual=%s zero_DTO=true", remaining, elapsed, actual)
		})
	}
	t.Run("http-earlier-lock-timeout-and-caller", func(t *testing.T) {
		user, _ := f.UserLock(v.admin.Details().UserID)
		release := managementHold(t, v, user, f.Exclusive)
		var authErr error
		v.tracked.setHooks(nil, func(_ context.Context, _ f.TransactionCause, trace systemHTTPTrace, result f.CommitResult) f.CommitResult {
			if trace.owner == "account.session" {
				authErr = result.Fault()
			}
			return result
		})
		done := make(chan systemHTTPResponse, 1)
		go func() {
			done <- v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-credentials/"+credential, "", nil)
		}()
		managementWaitBlocked(t, v, user)
		r := <-done
		v.tracked.setHooks(nil, nil)
		r.problem(t, 500, f.InternalError)
		managementLockFault(t, authErr)
		ctx, cancel := context.WithTimeout(testContext(t), 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		r = v.rawRequest(ctx, v.adminBrowser, "GET", "/api/v1/system/models/"+modelKey+"/deletion-impact", "", nil, nil)
		if r.status == 200 || time.Since(started) > time.Second || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatal("HTTP extended earlier caller")
		}
		release()
	})
	for _, kind := range []string{"metadata", "model"} {
		t.Run(kind+"-http-budget-includes-authentication", func(t *testing.T) {
			store, core, secrets := managementReaders(t, v)
			managementInstall(t, v, core, secrets)
			defer v.install(t, v.secrets)
			caller := testContext(t)
			var reached int
			var remaining time.Duration
			v.tracked.setHooks(func(ctx context.Context, cause f.TransactionCause) error {
				if cause.Details().Owner != "account.session" {
					return nil
				}
				reached++
				deadline, ok := ctx.Deadline()
				remaining = time.Until(deadline)
				if !ok || remaining <= 2*time.Second || remaining > 3*time.Second || caller.Err() != nil {
					t.Fatal("authentication did not inherit new-read budget")
				}
				<-ctx.Done()
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) || caller.Err() != nil {
					t.Fatal("authentication did not expire independently of caller")
				}
				return ctx.Err()
			}, nil)
			path := "/api/v1/system/model-credentials/" + credential
			if kind == "model" {
				path = "/api/v1/system/models/" + modelKey + "/deletion-impact"
			}
			started := time.Now()
			r := v.rawRequest(caller, v.adminBrowser, "GET", path, "", nil, nil)
			elapsed := time.Since(started)
			v.tracked.setHooks(nil, nil)
			if reached != 1 || store.count(kind) != 0 || caller.Err() != nil || elapsed < 2*time.Second || elapsed > 5*time.Second {
				t.Fatal("authentication budget or zero protected SQL boundary", elapsed)
			}
			r.problem(t, 503, f.DependencyUnavailable)
			t.Logf("HTTP account.session budget remaining=%s elapsed=%s caller_live=true protected_SQL=0", remaining, elapsed)
		})
	}
}

func managementLockFault(t *testing.T, err error) {
	t.Helper()
	var fault *f.Fault
	var pg *postgres.Error
	var sql *pgconn.PgError
	if !errors.As(err, &fault) || fault.Code != f.InternalError || fault.CommitState != f.NotCommitted || !errors.As(err, &pg) || pg.Code() != postgres.LockFailed || pg.SQLState() != "55P03" || !errors.As(err, &sql) || sql.Code != "55P03" {
		t.Fatal("original database lock fault/SQLSTATE chain changed", err)
	}
}
