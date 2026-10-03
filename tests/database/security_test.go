//go:build integration

package database_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func TestTLSVerifyFullRejectsCAAndHostnameWithoutFallback(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrate(t, cfg)
	store := openStore(t, cfg)
	var ssl bool
	if err := store.QueryRow(testContext(t), "SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&ssl); err != nil || !ssl {
		t.Fatal("verify-full did not use TLS")
	}
	for _, mode := range []string{"wrong_ca", "wrong_hostname", "wrong_password"} {
		t.Run(mode, func(t *testing.T) {
			changes := map[string]string{}
			u, _ := url.Parse(db.Fixture.URL(db.Name))
			switch mode {
			case "wrong_ca":
				changes["CA_FILE"] = db.Fixture.WrongCAFile
			case "wrong_hostname":
				u.Host = net.JoinHostPort("localhost", db.Fixture.Port)
				changes["URL"] = u.String()
			case "wrong_password":
				u.User = url.UserPassword(db.Fixture.User, "private-password-sentinel")
				changes["URL"] = u.String()
			}
			rejected, err := postgres.Open(testContext(t), db.Config(t, changes))
			if err == nil {
				_ = rejected.ForceClose(testContext(t))
				t.Fatal("bad TLS/auth accepted")
			}
			if mode == "wrong_ca" {
				var issue x509.UnknownAuthorityError
				if !errors.As(err, &issue) {
					t.Fatalf("failure did not prove CA rejection: %T", err)
				}
			}
			if mode == "wrong_hostname" {
				var issue x509.HostnameError
				if !errors.As(err, &issue) {
					t.Fatalf("failure did not prove hostname rejection: %T", err)
				}
			}
			assertSafe(t, err, "private-password-sentinel", db.Fixture.Password, db.Fixture.CAFile)
		})
	}
}
func assertSafe(t *testing.T, value any, sentinels ...string) {
	t.Helper()
	var outputs []string
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		outputs = append(outputs, fmt.Sprintf(format, value))
	}
	b, _ := json.Marshal(value)
	outputs = append(outputs, string(b))
	var log bytes.Buffer
	slog.New(slog.NewTextHandler(&log, nil)).Info("fixture", "error", value)
	outputs = append(outputs, log.String())
	for _, output := range outputs {
		for _, sentinel := range sentinels {
			if sentinel != "" && strings.Contains(output, sentinel) {
				t.Fatal("safe projection exposed private input")
			}
		}
	}
}
func TestPGEnvironmentAndFilesCannotOverrideConfig(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	path := filepath.Join(t.TempDir(), "credentials-sentinel")
	if err := os.WriteFile(path, []byte("private-file-sentinel"), 0000); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"PGHOST", "PGPASSWORD", "PGPASSFILE", "PGSERVICE", "PGSERVICEFILE", "PGSSLCERT", "PGSSLKEY", "PGSSLROOTCERT", "PGOPTIONS"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, path)
			start := time.Now()
			store, err := postgres.Open(testContext(t), cfg)
			if err == nil {
				_ = store.ForceClose(testContext(t))
				t.Fatal("implicit source accepted")
			}
			if postgres.CodeOf(err) != postgres.EnvironmentRejected || time.Since(start) > time.Second {
				t.Fatal("implicit source was consulted")
			}
			assertSafe(t, err, path, "private-file-sentinel")
		})
	}
}
func TestActualUnsupportedPGMajorAndExtensionMetadata(t *testing.T) {
	f, err := pgfixture.LoadUnsupported()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := f.Connect(testContext(t), "fixture_control")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var version int
	if err := conn.QueryRow(testContext(t), "SELECT current_setting('server_version_num')::int").Scan(&version); err != nil || version/10000 != 16 {
		t.Fatal("unsupported PG fixture is not PG16")
	}
	t.Logf("actual rejected PostgreSQL server_version_num=%d image=%s", version, f.Image)
	cfg, err := f.Config("fixture_control", nil)
	if err != nil {
		t.Fatal(err)
	}
	if store, err := postgres.Open(testContext(t), cfg); err == nil {
		_ = store.ForceClose(testContext(t))
		t.Fatal("PG16 accepted")
	} else if postgres.CodeOf(err) != postgres.VersionUnsupported {
		t.Fatal("PG16 failed outside version gate")
	}
	m, _ := postgres.NewMigrator(cfg)
	if state := m.Migrate(testContext(t)); state.Migrated || state.Fault.Code() != postgres.VersionUnsupported {
		t.Fatal("migrator accepted PG16")
	}
	db := pgfixture.NewDatabase(t)
	migrate(t, db.Config(t, nil))
	admin := db.Connect(t)
	// This tests rejection of installed catalog version mismatch, not execution
	// compatibility with an old vector binary. The only supported version is .1.
	if _, err := admin.Exec(testContext(t), "UPDATE pg_extension SET extversion='0.8.0' WHERE extname='vector'"); err != nil {
		t.Fatal("isolated extension metadata mutation failed")
	}
	if store, err := postgres.Open(testContext(t), db.Config(t, nil)); err == nil {
		_ = store.ForceClose(testContext(t))
		t.Fatal("wrong extension catalog version accepted")
	} else if postgres.CodeOf(err) != postgres.ExtensionUnsupported {
		t.Fatal("extension version gate not used")
	}
}
func TestMigrationPermissionFailureAndPGDetailAreSafe(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	admin := db.Connect(t)
	suffix, err := pgfixture.RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	role := "d03_limited_" + suffix
	password := "fixture_" + suffix
	if _, err := admin.Exec(testContext(t), "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD '"+password+"'; GRANT CREATE ON DATABASE "+pgx.Identifier{db.Name}.Sanitize()+" TO "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal("fixture role setup failed")
	}
	u, _ := url.Parse(db.Fixture.URL(db.Name))
	u.User = url.UserPassword(role, password)
	cfg := db.Config(t, map[string]string{"URL": u.String()})
	m, _ := postgres.NewMigrator(cfg)
	state := m.Migrate(testContext(t))
	if state.Migrated || state.Fault == nil {
		t.Fatal("extension privilege bypassed")
	}
	assertSafe(t, state.Fault, password, u.String())
	var absent bool
	if err := admin.QueryRow(testContext(t), "SELECT to_regclass('agenteam_meta.health_probe') IS NULL AND NOT EXISTS(SELECT 1 FROM pg_extension WHERE extname='vector') AND NOT EXISTS(SELECT 1 FROM agenteam_meta.goose_db_version WHERE version_id=1)").Scan(&absent); err != nil || !absent {
		t.Fatal("failed migration leaked DDL/version")
	}
	migrate(t, db.Config(t, nil))
	store := openStore(t, db.Config(t, nil))
	if _, err := store.Exec(testContext(t), "CREATE TABLE detail_fact(value text PRIMARY KEY); INSERT INTO detail_fact VALUES('private-detail-sentinel')"); err != nil {
		t.Fatal("fixture detail setup failed")
	}
	_, err = store.Exec(testContext(t), "INSERT INTO detail_fact VALUES($1)", "private-detail-sentinel")
	if err == nil {
		t.Fatal("unique violation missing")
	}
	assertSafe(t, err, "private-detail-sentinel", "detail_fact")
	var safe *postgres.Error
	if !errors.As(err, &safe) || safe.SQLState() != "23505" {
		t.Fatal("safe SQLSTATE missing")
	}
}
