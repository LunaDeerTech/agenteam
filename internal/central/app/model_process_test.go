//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/lifecycle"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Every operation delegates to the one actual Store. Observations and the
// bounded pre-Exec barrier neither replace authority nor manufacture outcomes.
type modelRootStore struct {
	*postgres.Store
	mu            sync.Mutex
	owned         *resources
	trace         []string
	modelContexts []context.Context
	secretContext context.Context
	modelStarted  int
	modelResults  []foundation.CommitState
	modelFaults   []foundation.Code
	beforeExec    func(context.Context, string)
	request       context.Context
	activeAtStop  []int
	forceDeadline []time.Time
	forceCanceled []bool
}

func (s *modelRootStore) record(v string) { s.mu.Lock(); s.trace = append(s.trace, v); s.mu.Unlock() }
func (s *modelRootStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	d := cause.Details()
	if d.Owner == "model.initialize" {
		s.mu.Lock()
		s.modelContexts = append(s.modelContexts, ctx)
		s.trace = append(s.trace, "model.initialize")
		s.mu.Unlock()
	}
	modelCommand := d.Kind == foundation.CommandsCause && d.Primary.Namespace() == "model.system"
	if modelCommand {
		s.mu.Lock()
		s.modelStarted++
		s.mu.Unlock()
	}
	r := s.Store.WithinTx(ctx, cause, fn)
	if modelCommand {
		s.mu.Lock()
		s.modelResults = append(s.modelResults, r.State())
		var code foundation.Code
		if fault := r.Fault(); fault != nil {
			code = fault.Code
		}
		s.modelFaults = append(s.modelFaults, code)
		s.mu.Unlock()
	}
	return r
}
func (s *modelRootStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	e, err := s.Store.InTx(tx)
	if err != nil {
		return nil, err
	}
	return modelRootSQL{SQLExecutor: e, store: s}, nil
}

type modelRootSQL struct {
	postgres.SQLExecutor
	store *modelRootStore
}

func (e modelRootSQL) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	e.store.mu.Lock()
	hook := e.store.beforeExec
	e.store.mu.Unlock()
	if hook != nil {
		hook(ctx, query)
	}
	return e.SQLExecutor.Exec(ctx, query, args...)
}
func (s *modelRootStore) StopAdmission() {
	active := 0
	if s.owned != nil {
		s.owned.mu.Lock()
		active = s.owned.httpActive
		s.owned.mu.Unlock()
	}
	s.mu.Lock()
	s.activeAtStop = append(s.activeAtStop, active)
	s.trace = append(s.trace, "db.stop")
	s.mu.Unlock()
	s.Store.StopAdmission()
}
func (s *modelRootStore) Drain(ctx context.Context) error {
	s.record("db.drain")
	return s.Store.Drain(ctx)
}
func (s *modelRootStore) ForceClose(ctx context.Context) error {
	s.mu.Lock()
	d, _ := ctx.Deadline()
	s.forceDeadline = append(s.forceDeadline, d)
	s.forceCanceled = append(s.forceCanceled, s.request == nil || s.request.Err() != nil)
	s.trace = append(s.trace, "db.force")
	s.mu.Unlock()
	return s.Store.ForceClose(ctx)
}

type modelRootApp struct {
	db      *pgfixture.Database
	cfg     config.Config
	store   *modelRootStore
	core    *account.Service
	owned   *resources
	logs    *eventLog
	signals chan os.Signal
	done    chan struct{}
	err     error
	cancel  context.CancelFunc
}

func newModelRootApp(t *testing.T, timeout string, setup func(*modelRootApp, *dependencies)) *modelRootApp {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	env := append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT="+timeout)
	a := &modelRootApp{db: db, cfg: guardConfiguration(t, env), store: &modelRootStore{}, logs: newEventLog(), signals: make(chan os.Signal, 4), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	deps := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			store, err := postgres.Open(ctx, cfg)
			if err != nil {
				return nil, err
			}
			a.store.Store = store
			return a.store, nil
		},
		bind: func(ctx context.Context, cfg config.Config, db database, owned *resources, d *dependencies) error {
			a.owned, a.store.owned = owned, owned
			return bindAccounts(ctx, cfg, db, owned, d)
		},
		observeAccount: func(core *account.Service) { a.core = core },
		listen: func(ctx context.Context, network, address string) (net.Listener, error) {
			a.store.record("listen")
			return (&net.ListenConfig{}).Listen(ctx, network, address)
		},
	}
	if setup != nil {
		setup(a, &deps)
	}
	logger, err := logging.New(logging.Central, slog.LevelInfo, a.logs)
	if err != nil {
		t.Fatal(err)
	}
	go func() { a.err = run(ctx, a.cfg, logger, a.signals, deps); close(a.done) }()
	t.Cleanup(func() { cancel(); await(t, a.done) })
	return a
}
func (a *modelRootApp) address(t *testing.T) string {
	t.Helper()
	return "http://" + a.logs.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })["listen_address"].(string)
}

func TestModelRootInitializationAndFailureOwnership(t *testing.T) {
	for _, mode := range []string{"success", "model-storage", "secret-storage", "secret-cancel", "model-lock-cancel"} {
		t.Run(mode, func(t *testing.T) {
			var holder *postgres.Store
			held, release, holderDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseHolder := func() { releaseOnce.Do(func() { close(release) }) }
			defer releaseHolder()
			a := newModelRootApp(t, "2s", func(a *modelRootApp, d *dependencies) {
				d.secretInitialize = func(ctx context.Context, service *secret.Service) error {
					a.store.mu.Lock()
					a.store.secretContext = ctx
					a.store.mu.Unlock()
					if mode == "secret-storage" {
						if _, err := a.store.Exec(ctx, `ALTER TABLE agenteam_secret.secret_master_registry RENAME TO unavailable_registry`); err != nil {
							return err
						}
					}
					if err := service.Initialize(ctx); err != nil {
						return err
					}
					a.store.record("secret.success")
					if mode == "secret-cancel" {
						a.cancel()
						<-ctx.Done()
						return nil
					}
					if mode == "model-storage" {
						_, err := a.store.Exec(ctx, `ALTER TABLE agenteam_model.providers RENAME TO unavailable_providers`)
						return err
					}
					if mode == "model-lock-cancel" {
						var err error
						holder, err = postgres.Open(ctx, a.cfg.Database())
						if err != nil {
							return err
						}
						cause, _ := foundation.NewRecoveryCause("model-root-lock", guardID[struct{}](t).String(), "")
						go func() {
							defer close(holderDone)
							r := holder.WithinTx(context.Background(), cause, func(ctx context.Context, tx foundation.Tx) error {
								key, _ := foundation.SystemConfigLock("model-platform-selection")
								if err := holder.Acquire(ctx, tx, key, foundation.Exclusive); err != nil {
									return err
								}
								close(held)
								<-release
								return nil
							})
							if r.State() != foundation.Committed {
								t.Error("normal lock holder failed")
							}
						}()
						<-held
					}
					return nil
				}
			})
			if mode == "success" {
				a.address(t)
				a.signals <- syscall.SIGTERM
			} else if mode == "model-lock-cancel" {
				await(t, held)
				waitModelRootFact(t, a.db, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND cardinality(pg_blocking_pids(pid))>0)`)
				a.signals <- syscall.SIGTERM
			}
			await(t, a.done)
			if holder != nil {
				releaseHolder()
				await(t, holderDone)
				closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				if err := holder.ForceClose(closeCtx); err != nil {
					t.Error(err)
				}
				cancel()
			}
			a.store.mu.Lock()
			count := len(a.store.modelContexts)
			trace := append([]string(nil), a.store.trace...)
			if count > 0 {
				if count != 1 || a.store.modelContexts[0] != a.store.secretContext {
					t.Error("Model did not inherit exact original Secret ctx once")
				}
				deadline, ok := a.store.modelContexts[0].Deadline()
				if !ok || deadline.IsZero() {
					t.Error("security deadline missing")
				}
			}
			a.store.mu.Unlock()
			wantModel := mode != "secret-storage" && mode != "secret-cancel"
			if (count == 1) != wantModel {
				t.Fatal("wrong initialization stage", count, trace)
			}
			if mode == "success" {
				if a.err != nil || strings.Index(strings.Join(trace, ","), "secret.success") > strings.Index(strings.Join(trace, ","), "model.initialize") {
					t.Fatal("startup order", a.err, trace)
				}
				var version int64
				var configured bool
				if err := a.db.Connect(t).QueryRow(databaseTestContext(t), `SELECT version,configured FROM agenteam_model.platform_selection WHERE singleton`).Scan(&version, &configured); err != nil || version != 1 || configured {
					t.Fatal("technical singleton", err)
				}
			} else {
				if strings.Contains(a.logs.String(), `"event":"listening"`) {
					t.Fatal("failed/cancelled initialize listened")
				}
				if (mode == "model-storage" || mode == "secret-storage") && lifecycle.CodeOf(a.err) != lifecycle.InitializationFailed {
					t.Fatal("storage error swallowed", a.err)
				}
				if a.owned == nil || !a.owned.accounts().Joined() {
					t.Fatal("partial Account/log owner not joined")
				}
			}
			waitModelRootFact(t, a.db, `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`)
			t.Logf("mode=%s Model initialize calls=%d trace=%v", mode, count, trace)
		})
	}
}

func waitModelRootFact(t *testing.T, db *pgfixture.Database, query string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := db.Connect(t)
	for {
		var ok bool
		if err := conn.QueryRow(ctx, query).Scan(&ok); err != nil {
			t.Fatal("root fact query failed", err)
		}
		if ok {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("root fact did not converge")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func modelRootRequest(t *testing.T, a *modelRootApp, address string) <-chan responseResult {
	t.Helper()
	response, err := fixtureAccountLoginResponse(databaseTestContext(t), a.cfg, a.core)
	if err != nil {
		t.Fatal("actual Account login failed", err)
	}
	var cookie string
	if err = response.UseCookie(func(b []byte) error { cookie = string(b); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = response.Close(databaseTestContext(t)); err != nil {
		t.Fatal(err)
	}
	material, err := sc.NewSecretMaterial([]byte(cookie))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	session, err := a.core.GetSession(databaseTestContext(t), material)
	if err != nil {
		t.Fatal(err)
	}
	defer session.CSRF.Destroy()
	var csrf string
	if err = session.CSRF.Use(func(b []byte) error { csrf = string(b); return nil }); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"input":{"name":"root-drain","protocol":"openai-chat-completions","base_url":"https://never-contacted.example/v1","enabled":true,"credential_ref":null,"options":{}}}`)
	r, err := http.NewRequest(http.MethodPost, address+"/api/v1/system/model-providers", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Host = "localhost:8080"
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", guardID[struct{}](t).String())
	r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookie})
	client := &http.Client{Timeout: 8 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	done := make(chan responseResult, 1)
	go func() {
		result, err := client.Do(r)
		if err != nil {
			done <- responseResult{err: err}
			return
		}
		raw, err := io.ReadAll(result.Body)
		_ = result.Body.Close()
		done <- responseResult{status: result.StatusCode, body: raw, err: err}
	}()
	return done
}
func modelRootFlight(t *testing.T, timeout string) (*modelRootApp, context.Context, func(), <-chan responseResult, string) {
	t.Helper()
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	var once sync.Once
	a := newModelRootApp(t, timeout, func(a *modelRootApp, _ *dependencies) {
		a.store.beforeExec = func(ctx context.Context, query string) {
			if !strings.HasPrefix(query, "INSERT INTO agenteam_model.providers(") {
				return
			}
			a.store.mu.Lock()
			a.store.request = ctx
			a.store.trace = append(a.store.trace, "provider.exec.entered")
			a.store.mu.Unlock()
			entered <- ctx
			select {
			case <-release:
			case <-ctx.Done():
			}
			a.store.record("provider.exec.released")
		}
	})
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)
	address := a.address(t)
	result := modelRootRequest(t, a, address)
	var ctx context.Context
	select {
	case ctx = <-entered:
	case r := <-result:
		t.Fatalf("real Model request did not enter provider write: status=%d error=%v", r.status, r.err)
	case <-time.After(5 * time.Second):
		t.Fatal("provider write barrier not reached")
	}
	a.owned.mu.Lock()
	active := a.owned.httpActive
	a.owned.mu.Unlock()
	if active != 1 {
		t.Fatal("new actual route missing HTTP ownership", active)
	}
	return a, ctx, finish, result, address
}

func TestModelRootHTTPActiveDrainAndDatabaseLast(t *testing.T) {
	a, ctx, release, result, address := modelRootFlight(t, "3s")
	a.signals <- syscall.SIGTERM
	a.logs.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
	if ctx.Err() != nil {
		t.Fatal("first signal cancelled admitted request")
	}
	a.store.mu.Lock()
	stopped := len(a.store.activeAtStop)
	a.store.mu.Unlock()
	if stopped != 0 {
		t.Fatal("database stopped before actual HTTP completion")
	}
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	r, _ := http.NewRequest("GET", address+"/api/v1/system/model-selection", nil)
	r.Host = "localhost:8080"
	if response, err := client.Do(r); err == nil {
		raw, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 503 || !bytes.Contains(raw, []byte("SHUTTING_DOWN")) {
			t.Fatal("late request admitted")
		}
	}
	release()
	response := receiveResponse(t, result)
	if response.err != nil || response.status != 200 {
		t.Fatal("admitted Model write did not finish", response.status, response.err)
	}
	var receipt struct {
		ID string `json:"resource_id"`
	}
	if json.Unmarshal(response.body, &receipt) != nil || receipt.ID == "" {
		t.Fatal("missing Model receipt")
	}
	await(t, a.done)
	if a.err != nil {
		t.Fatal(a.err)
	}
	var count int
	if err := a.db.Connect(t).QueryRow(databaseTestContext(t), `SELECT count(*) FROM agenteam_model.providers WHERE id=$1`, receipt.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("receipt/canonical mismatch", err)
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	if len(a.store.activeAtStop) == 0 || len(a.store.forceDeadline) != 0 {
		t.Fatal("normal drain did not close DB normally", a.store.trace)
	}
	for _, n := range a.store.activeAtStop {
		if n != 0 {
			t.Fatal("DB stop overtook httpActive")
		}
	}
	if len(a.store.modelResults) == 0 || a.store.modelResults[len(a.store.modelResults)-1] != foundation.Committed {
		t.Fatal("actual command commit missing")
	}
	t.Logf("normal drain trace=%v actual model Tx states=%v faults=%v unreturned=%d", a.store.trace, a.store.modelResults, a.store.modelFaults, a.store.modelStarted-len(a.store.modelResults))
}

func TestModelRootHTTPForceUsesOriginalBudget(t *testing.T) {
	for _, mode := range []string{"deadline", "second-signal"} {
		t.Run(mode, func(t *testing.T) {
			timeout := "120ms"
			want := lifecycle.ShutdownTimeout
			if mode == "second-signal" {
				timeout = "3s"
				want = lifecycle.ForcedShutdown
			}
			a, ctx, release, result, _ := modelRootFlight(t, timeout)
			defer release()
			start := time.Now()
			a.signals <- syscall.SIGTERM
			a.logs.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
			if mode == "second-signal" {
				a.signals <- syscall.SIGINT
			}
			await(t, a.done)
			if time.Since(start) > 3*time.Second || lifecycle.CodeOf(a.err) != want || ctx.Err() == nil {
				t.Fatal("force budget/outcome/context", a.err)
			}
			response := receiveResponse(t, result)
			if response.err == nil && response.status == 200 {
				t.Fatal("blocked forced write claimed success")
			}
			a.store.mu.Lock()
			defer a.store.mu.Unlock()
			if len(a.store.forceDeadline) == 0 {
				t.Fatal("DB ForceClose was not initiated")
			}
			for i, d := range a.store.forceDeadline {
				if d.IsZero() || d.After(start.Add(2*time.Second)) || !a.store.forceCanceled[i] {
					t.Fatal("force renewed budget or ran DB before serving cancellation")
				}
			}
			if strings.Contains(a.logs.String(), `"outcome":"drained"`) {
				t.Fatal("forced path claimed drained")
			}
			t.Logf("force mode=%s trace=%v actual model Tx states=%v faults=%v unreturned=%d; forced exit is not an all-writer join or rollback claim", mode, a.store.trace, a.store.modelResults, a.store.modelFaults, a.store.modelStarted-len(a.store.modelResults))
		})
	}
}
