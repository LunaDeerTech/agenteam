//go:build integration

package process_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func databaseEnvironment(db *pgfixture.Database, extras ...string) []string {
	return append([]string{"AGENTEAM_CENTRAL_HTTP_ADDR=127.0.0.1:0", "AGENTEAM_CENTRAL_DATABASE_URL=" + db.Fixture.URL(db.Name), "AGENTEAM_CENTRAL_DATABASE_CA_FILE=" + db.Fixture.CAFile, "AGENTEAM_CENTRAL_DATABASE_STARTUP_TIMEOUT=15s", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=3s"}, extras...)
}
func databaseContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func waitDatabaseFact(t *testing.T, conn *pgx.Conn, query string, args ...any) {
	t.Helper()
	ctx := databaseContext(t)
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		var matched bool
		if err := conn.QueryRow(ctx, query, args...).Scan(&matched); err != nil {
			t.Fatal("owned database fact query failed")
		}
		if matched {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("database fact deadline exceeded")
		case <-timer.C:
		}
	}
}
func noCentralBackends(t *testing.T, db *pgfixture.Database) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		conn, err := db.Fixture.Connect(ctx, db.Name)
		if err != nil {
			return
		}
		defer conn.Close(ctx)
		rows, err := conn.Query(ctx, "SELECT pid,state,coalesce(wait_event_type,''),coalesce(wait_event,''),query=$1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam'", "SELECT pg_advisory_lock($1::integer,$2::integer)")
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var pid int32
			var state, waitType, wait string
			var guard bool
			if rows.Scan(&pid, &state, &waitType, &wait, &guard) == nil {
				t.Logf("owned backend remains: pid=%d state=%s wait=%s/%s migration_guard=%t", pid, state, waitType, wait, guard)
			}
		}
	})
	waitDatabaseFact(t, db.Connect(t), "SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')")
}
func assertDatabaseLogsSafe(t *testing.T, p *process, db *pgfixture.Database) {
	t.Helper()
	logs := p.stderr.String() + p.stdout.String()
	for _, private := range []string{db.Fixture.Password, db.Fixture.URL(db.Name), db.Fixture.CAFile, "private-query-sentinel"} {
		if strings.Contains(logs, private) {
			t.Fatal("database input leaked through process output")
		}
	}
	assertLogRecords(t, p.stderr.String(), "central")
}
func TestCentralRealDatabaseSIGTERMAndSIGINT(t *testing.T) {
	for _, signal := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(signal.String(), func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			p := launch(t, "agenteam", nil, databaseEnvironment(db))
			listening := p.event(t, "event", "listening")
			address := listening["listen_address"].(string)
			checkDiagnosticBinary(t, address)
			waitDiagnosticState(t, address, "available", time.Second)
			var count int
			if err := db.Connect(t).QueryRow(databaseContext(t), "SELECT count(*) FROM agenteam_meta.health_probe").Scan(&count); err != nil || count != 0 {
				t.Fatal("diagnostic probe persisted")
			}
			if err := p.command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			p.wait(t, 0)
			noCentralBackends(t, db)
			assertDatabaseLogsSafe(t, p, db)
		})
	}
}
func TestCentralDatabaseInitializationFailures(t *testing.T) {
	for _, mode := range []string{"history", "permissions", "guard_timeout", "listen_conflict", "connection"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			admin := db.Connect(t)
			env := databaseEnvironment(db)
			var listener net.Listener
			switch mode {
			case "history":
				cfg := db.Config(t, nil)
				m, _ := postgres.NewMigrator(cfg)
				if !m.Migrate(databaseContext(t)).Migrated {
					t.Fatal("fixture migration failed")
				}
				if _, err := admin.Exec(databaseContext(t), "UPDATE agenteam_meta.migration_journal SET checksum='sha256:' || repeat('0',64) WHERE version=1"); err != nil {
					t.Fatal("fixture history mutation failed")
				}
			case "permissions":
				role := "d03_limited_" + db.Name
				password := "private-query-sentinel"
				if _, err := admin.Exec(databaseContext(t), "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD '"+password+"'; GRANT CREATE ON DATABASE "+pgx.Identifier{db.Name}.Sanitize()+" TO "+pgx.Identifier{role}.Sanitize()); err != nil {
					t.Fatal("fixture role failed")
				}
				// All coordinates still point to the verified task-owned database.
				env = append(env, "AGENTEAM_CENTRAL_DATABASE_URL=postgresql://"+role+":"+password+"@127.0.0.1:"+db.Fixture.Port+"/"+db.Name)
			case "guard_timeout":
				if _, err := admin.Exec(databaseContext(t), "SELECT pg_advisory_lock(1095193677,1)"); err != nil {
					t.Fatal("fixture guard failed")
				}
				env = append(env, "AGENTEAM_CENTRAL_DATABASE_STARTUP_TIMEOUT=1s")
			case "listen_conflict", "connection":
				var err error
				listener, err = net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				if mode == "listen_conflict" {
					env = append(env, "AGENTEAM_CENTRAL_HTTP_ADDR="+listener.Addr().String())
				} else {
					done := make(chan struct{})
					go func() {
						defer close(done)
						for {
							conn, err := listener.Accept()
							if err != nil {
								return
							}
							_ = conn.Close()
						}
					}()
					t.Cleanup(func() { _ = listener.Close(); <-done })
					env = append(env, "AGENTEAM_CENTRAL_DATABASE_URL=postgresql://connection:private-query-sentinel@"+listener.Addr().String()+"/unreachable")
				}
			}
			p := launch(t, "agenteam", nil, env)
			p.wait(t, 1)
			if strings.Contains(p.stderr.String(), `"event":"listening"`) || strings.Contains(p.stderr.String(), `"phase":"diagnostic_serving"`) {
				t.Fatal("failed database startup bound HTTP")
			}
			if mode == "listen_conflict" && !strings.Contains(p.stderr.String(), "LISTEN_FAILED") {
				t.Fatal("bind failure lost")
			}
			if mode == "permissions" {
				var absent bool
				if err := admin.QueryRow(databaseContext(t), "SELECT to_regclass('agenteam_meta.health_probe') IS NULL AND NOT EXISTS(SELECT 1 FROM pg_extension WHERE extname='vector')").Scan(&absent); err != nil || !absent {
					t.Fatal("failed migration leaked DDL")
				}
			}
			noCentralBackends(t, db)
			assertDatabaseLogsSafe(t, p, db)
		})
	}
}
func TestCentralStartupSignalsCancelRealMigrationWait(t *testing.T) {
	for _, signal := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(signal.String(), func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			admin := db.Connect(t)
			if _, err := admin.Exec(databaseContext(t), "SELECT pg_advisory_lock(1095193677,1)"); err != nil {
				t.Fatal("fixture guard failed")
			}
			p := launch(t, "agenteam", nil, databaseEnvironment(db))
			p.event(t, "database_phase", "migrating")
			waitDatabaseFact(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock')")
			if err := p.command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			p.wait(t, 0)
			if strings.Contains(p.stderr.String(), `"database_phase":"migrated"`) || strings.Contains(p.stderr.String(), `"event":"listening"`) {
				t.Fatal("cancelled migration claimed completed startup")
			}
			noCentralBackends(t, db)
			assertDatabaseLogsSafe(t, p, db)
		})
	}
}
func waitDiagnosticState(t *testing.T, address, want string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	timer := time.NewTicker(20 * time.Millisecond)
	defer timer.Stop()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for {
		request, _ := http.NewRequestWithContext(ctx, "GET", "http://"+address+"/diagnostics", nil)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("diagnostic request failed")
		}
		var d struct {
			Ready        bool                            `json:"ready"`
			Capabilities []struct{ Name, Status string } `json:"capabilities"`
			Database     *postgres.DatabaseHealth        `json:"database"`
		}
		err = json.NewDecoder(response.Body).Decode(&d)
		_ = response.Body.Close()
		if err != nil || d.Ready {
			t.Fatal("invalid/false-ready diagnostic response")
		}
		matched := 0
		for _, capability := range d.Capabilities {
			switch capability.Name {
			case "postgresql", "pgvector", "migrations", "read_write":
				if capability.Status == want {
					matched++
				}
			default:
				if capability.Status != "unbound" {
					t.Fatal("future dependency claimed available")
				}
			}
		}
		if matched == 4 {
			if want == "available" && (d.Database == nil || !d.Database.ReadWrite || d.Database.ExtensionVersion != "0.8.1") {
				t.Fatal("database evidence absent")
			}
			if want == "unavailable" && d.Database != nil {
				t.Fatal("stale successful health returned")
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("diagnostic state did not converge")
		case <-timer.C:
		}
	}
}
func TestCentralRealHealthTimeoutAndRecovery(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	p := launch(t, "agenteam", nil, databaseEnvironment(db))
	address := p.event(t, "event", "listening")["listen_address"].(string)
	admin := db.Connect(t)
	tx, err := admin.Begin(databaseContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(databaseContext(t), "LOCK TABLE agenteam_meta.health_probe IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal("fixture health barrier failed")
	}
	waitDiagnosticState(t, address, "unavailable", 15*time.Second)
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for path, code := range map[string]string{"/livez": "alive", "/readyz": "DEPENDENCY_UNAVAILABLE"} {
		response, err := client.Get("http://" + address + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if !strings.Contains(string(body), code) {
			t.Fatal("database failure affected liveness or was hidden")
		}
	}
	if err := tx.Rollback(databaseContext(t)); err != nil {
		t.Fatal("health barrier release failed")
	}
	waitDiagnosticState(t, address, "available", 12*time.Second)
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	noCentralBackends(t, db)
	assertDatabaseLogsSafe(t, p, db)
}
func TestCentralStopWhileHealthQueryIsBlocked(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	p := launch(t, "agenteam", nil, databaseEnvironment(db))
	p.event(t, "event", "listening")
	admin := db.Connect(t)
	tx, err := admin.Begin(databaseContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(databaseContext(t), "LOCK TABLE agenteam_meta.health_probe IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal("fixture health barrier failed")
	}
	waitDatabaseFact(t, admin, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock')")
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	noCentralBackends(t, db)
}
