//go:build integration

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type observedDatabase struct{ *postgres.Store }

func (d observedDatabase) StopAdmission() {
	d.Store.StopAdmission()
	fmt.Fprintln(os.Stdout, `{"event":"database_admission_stopped"}`)
}
func (d observedDatabase) ForceClose(ctx context.Context) error {
	fmt.Fprintln(os.Stdout, `{"event":"database_force_started"}`)
	return d.Store.ForceClose(ctx)
}

// Only this test executable has fixture routes and synchronization inputs. It
// delegates every database operation to the production Store and Migrator.
func TestDatabaseAppProcessFixture(t *testing.T) {
	mode := os.Getenv("AGENTEAM_DATABASE_APP_FIXTURE")
	if mode == "" {
		return
	}
	fixture, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	databaseName := os.Getenv("AGENTEAM_DATABASE_APP_DATABASE")
	if _, err := fixture.Config(databaseName, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(os.LookupEnv, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	logger, err := logging.New(logging.Central, slog.LevelInfo, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err == nil && strings.TrimSpace(line) == "release" {
			close(release)
		}
	}()
	var store *postgres.Store
	deps := dependencies{open: func(ctx context.Context, cfg postgres.Config) (database, error) {
		var err error
		store, err = postgres.Open(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return observedDatabase{store}, nil
	}}
	cause, err := foundation.NewRecoveryCause("database_process_test", "01900000-0000-7000-8000-000000000001", "")
	if err != nil {
		t.Fatal(err)
	}
	perform := func(ctx context.Context) foundation.CommitResult {
		return store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			e, err := store.InTx(tx)
			if err != nil {
				return err
			}
			key, _ := foundation.ProjectLock("01900000-0000-7000-8000-000000000002")
			if err := store.Acquire(ctx, tx, key, foundation.Exclusive); err != nil {
				return err
			}
			if _, err := e.Exec(ctx, "INSERT INTO app_process_fact VALUES(1)"); err != nil {
				return err
			}
			var pid int32
			if err := e.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
				return err
			}
			_ = json.NewEncoder(os.Stdout).Encode(struct {
				Event string `json:"event"`
				PID   int32  `json:"pid"`
			}{"transaction_entered", pid})
			if mode == "blocked_io" {
				_, err := e.Exec(ctx, "SELECT pg_sleep(60)")
				return err
			}
			<-release
			_, err = e.Exec(ctx, "INSERT INTO app_process_fact VALUES(2)")
			return err
		})
	}
	deps.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "background" {
			go perform(context.WithoutCancel(r.Context()))
			_ = httpapi.WriteJSON(w, r, 202, struct {
				Accepted bool `json:"accepted"`
			}{true})
			return
		}
		result := perform(r.Context())
		status := 503
		if result.State() == foundation.Committed {
			status = 200
		}
		_ = httpapi.WriteJSON(w, r, status, struct {
			State foundation.CommitState `json:"state"`
		}{result.State()})
	})
	if mode == "startup_second_signal" {
		deps.migrate = func(ctx context.Context, cfg postgres.Config) postgres.MigrationState {
			m, err := postgres.NewMigrator(cfg)
			if err != nil {
				t.Fatal(err)
			}
			result := m.Migrate(ctx)
			fmt.Fprintln(os.Stdout, `{"event":"migration_cancelled"}`)
			<-release // Tests the app's force bound after real migration cancellation.
			return result
		}
	}
	if mode == "secret_startup_second_signal" {
		deps.secret = func(ctx context.Context, cfg config.Config, db database, auditing *audit.Service) (maintenance, error) {
			service, err := initializeSecret(ctx, cfg, db, auditing)
			fmt.Fprintln(os.Stdout, `{"event":"secret_initialization_returned"}`)
			<-release // Only the test executable delays the real failed initializer.
			return service, err
		}
	}
	configureOutboundFixture(t, mode, cfg, &deps, release)
	err = run(context.Background(), cfg, logger, signals, deps)
	signal.Stop(signals)
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func databaseTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func launchDatabaseApp(t *testing.T, db *pgfixture.Database, mode, timeout string, extras ...string) *fixtureProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestDatabaseAppProcessFixture$", "-test.timeout=30s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = []string{`AGENTEAM_CENTRAL_SECRET_KEYRING={"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, `AGENTEAM_CENTRAL_CURSOR_KEYRING={"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, "PATH=" + os.Getenv("PATH"), pgfixture.Env + "=" + os.Getenv(pgfixture.Env), "AGENTEAM_DATABASE_APP_FIXTURE=" + mode, "AGENTEAM_DATABASE_APP_DATABASE=" + db.Name, "AGENTEAM_CENTRAL_HTTP_ADDR=127.0.0.1:0", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=" + timeout, "AGENTEAM_CENTRAL_DATABASE_URL=" + db.Fixture.URL(db.Name), "AGENTEAM_CENTRAL_DATABASE_CA_FILE=" + db.Fixture.CAFile, "AGENTEAM_CENTRAL_DATABASE_STARTUP_TIMEOUT=20s"}
	objects, err := objectfixture.Environment(context.Background(), db.Name)
	if err != nil {
		t.Fatal("owned MinIO configuration failed")
	}
	p.cmd.Env = append(p.cmd.Env, objects...)
	for _, extra := range extras {
		key, _, _ := strings.Cut(extra, "=")
		replaced := false
		for i, entry := range p.cmd.Env {
			if strings.HasPrefix(entry, key+"=") {
				p.cmd.Env[i] = extra
				replaced = true
			}
		}
		if !replaced {
			p.cmd.Env = append(p.cmd.Env, extra)
		}
	}
	p.cmd.Stdout = p.stdout
	p.cmd.Stderr = p.stderr
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = p.stdin.Close()
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			await(t, p.done)
		}
	})
	return p
}
func waitAppDatabase(t *testing.T, db *pgfixture.Database, query string, args ...any) {
	t.Helper()
	conn := db.Connect(t)
	ctx := databaseTestContext(t)
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		var ready bool
		if err := conn.QueryRow(ctx, query, args...).Scan(&ready); err != nil {
			t.Fatal("fixture observation failed")
		}
		if ready {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("database observation deadline exceeded")
		case <-timer.C:
		}
	}
}
func TestRealDatabaseHTTPTransactionDrainAndForce(t *testing.T) {
	for _, mode := range []string{"http_graceful", "background", "blocked_io", "second_signal"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			admin := db.Connect(t)
			if _, err := admin.Exec(databaseTestContext(t), "CREATE TABLE app_process_fact(value integer PRIMARY KEY)"); err != nil {
				t.Fatal("fixture fact table failed")
			}
			timeout := "5s"
			if mode == "blocked_io" {
				timeout = "100ms"
			}
			p := launchDatabaseApp(t, db, mode, timeout)
			address := p.stderr.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })["listen_address"].(string)
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			response := asyncGet(client, "http://"+address+"/transaction")
			entered := p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "transaction_entered" })
			pid := int32(entered["pid"].(float64))
			if mode == "background" {
				if r := receiveResponse(t, response); r.err != nil || r.status != 202 {
					t.Fatal("background fixture handoff failed")
				}
			}
			if mode == "blocked_io" {
				waitAppDatabase(t, db, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event='PgSleep')", pid)
			}
			if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			p.stderr.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
			if mode == "http_graceful" || mode == "background" {
				if mode == "http_graceful" && strings.Contains(p.stdout.String(), "database_admission_stopped") {
					t.Fatal("database stopped before active HTTP drained")
				}
				if mode == "background" {
					p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "database_admission_stopped" })
				}
				_, _ = io.WriteString(p.stdin, "release\n")
				if mode == "http_graceful" {
					r := receiveResponse(t, response)
					if r.err != nil || r.status != 200 || !strings.Contains(string(r.body), `"state":"committed"`) {
						t.Fatal("active HTTP transaction could not commit during drain")
					}
				}
				p.wait(t, 0)
				var count int
				if err := admin.QueryRow(databaseTestContext(t), "SELECT count(*) FROM app_process_fact").Scan(&count); err != nil || count != 2 {
					t.Fatal("drained transaction facts missing")
				}
			} else {
				started := time.Now()
				if mode == "second_signal" {
					if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
						t.Fatal(err)
					}
				}
				p.stderr.wait(t, func(e map[string]any) bool { return e["outcome"] == "forced" })
				bound := 1500 * time.Millisecond
				if mode == "blocked_io" {
					bound += 100 * time.Millisecond
				}
				if time.Since(started) > bound {
					t.Fatal("HTTP/database force budgets were reset or added")
				}
				p.wait(t, 1)
				if r := receiveResponse(t, response); r.err == nil && strings.Contains(string(r.body), `"state":"committed"`) {
					t.Fatal("forced transaction claimed committed")
				}
				var count int
				if err := admin.QueryRow(databaseTestContext(t), "SELECT count(*) FROM app_process_fact").Scan(&count); err != nil || count != 0 {
					t.Fatal("forced fixture transaction committed")
				}
			}
			waitAppDatabase(t, db, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')")
			if strings.Contains(p.stderr.String(), db.Fixture.Password) || strings.Contains(p.stderr.String(), db.Fixture.CAFile) {
				t.Fatal("fixture credential leaked")
			}
		})
	}
}
func TestRealDatabaseStartupSecondSignalBound(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	admin := db.Connect(t)
	if _, err := admin.Exec(databaseTestContext(t), "SELECT pg_advisory_lock(1095193677,1)"); err != nil {
		t.Fatal("fixture migration guard failed")
	}
	p := launchDatabaseApp(t, db, "startup_second_signal", "5s")
	p.stderr.wait(t, func(e map[string]any) bool { return e["database_phase"] == "migrating" })
	waitAppDatabase(t, db, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock')")
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "migration_cancelled" })
	started := time.Now()
	if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	p.stderr.wait(t, func(e map[string]any) bool { return e["outcome"] == "forced" })
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("startup second signal did not share force bound")
	}
	p.wait(t, 1)
	if strings.Contains(p.stderr.String(), `"event":"listening"`) || strings.Contains(p.stderr.String(), `"database_phase":"migrated"`) {
		t.Fatal("cancelled startup claimed success")
	}
	waitAppDatabase(t, db, "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')")
}
