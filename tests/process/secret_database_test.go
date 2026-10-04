//go:build integration

package process_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const processMasterOne = `{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`
const processMasterTwo = `{"format":1,"current_version":"2","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="},{"version":"2","key_b64":"QEFCQ0RFRkdISUpLTE1OT1BRUlNUVVZXWFlaW1xdXl8="}]}`
const processMasterOnlyTwo = `{"format":1,"current_version":"2","keys":[{"version":"2","key_b64":"QEFCQ0RFRkdISUpLTE1OT1BRUlNUVVZXWFlaW1xdXl8="}]}`

func secretEnvironment(t *testing.T, db *pgfixture.Database, raw string, extras ...string) []string {
	env := databaseEnvironment(t, db, extras...)
	for i, v := range env {
		if strings.HasPrefix(v, "AGENTEAM_CENTRAL_SECRET_KEYRING=") {
			env[i] = "AGENTEAM_CENTRAL_SECRET_KEYRING=" + raw
		}
	}
	return env
}
func waitSecretCompleted(t *testing.T, db *pgfixture.Database, target int64) {
	t.Helper()
	waitDatabaseFact(t, db.Connect(t), `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_rotation_runs WHERE target_version=$1 AND state='completed')`, target)
}
func stopSecretProcess(t *testing.T, p *process, db *pgfixture.Database) {
	t.Helper()
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	noCentralBackends(t, db)
	assertDatabaseLogsSafe(t, p, db)
}
func TestCentralSecretActualStartupRotationAndIndependentKeyRemoval(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	for _, version := range []struct {
		raw    string
		target int64
	}{{processMasterOne, 1}, {processMasterTwo, 2}, {processMasterOnlyTwo, 2}} {
		p := launch(t, "agenteam", nil, secretEnvironment(t, db, version.raw))
		address := p.event(t, "event", "listening")["listen_address"].(string)
		checkDiagnosticBinary(t, address)
		waitSecretCompleted(t, db, version.target)
		response := waitSecretDiagnostic(t, address, version.target)
		var status struct {
			Ready  bool `json:"ready"`
			Secret struct {
				Available    bool   `json:"available"`
				WriteVersion string `json:"write_version"`
				Rotation     string `json:"rotation"`
				Remaining    string `json:"remaining"`
			} `json:"secret"`
		}
		if json.Unmarshal(response, &status) != nil || status.Ready || !status.Secret.Available || status.Secret.Rotation != "completed" || status.Secret.Remaining != "0" {
			t.Fatal("real Secret diagnostic state incorrect")
		}
		for _, private := range []string{"key_b64", "fingerprint", "canary", "wrapped_dek", "ciphertext", "credential_ref"} {
			if strings.Contains(string(response), private) {
				t.Fatal("Secret diagnostics expose protected data")
			}
		}
		stopSecretProcess(t, p, db)
	}
	var retired bool
	if err := db.Connect(t).QueryRow(databaseContext(t), `SELECT retireable FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&retired); err != nil || !retired {
		t.Fatal("real entry omitted retirement fence", err)
	}
}
func TestCentralSecretCanaryAndMissingOldKeyFailBeforeHTTP(t *testing.T) {
	for _, mode := range []string{"wrong_material", "missing_old_key", "bad_canary"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			p := launch(t, "agenteam", nil, secretEnvironment(t, db, processMasterOne))
			p.event(t, "event", "listening")
			waitSecretCompleted(t, db, 1)
			stopSecretProcess(t, p, db)
			raw := processMasterOne
			switch mode {
			case "wrong_material":
				raw = strings.ReplaceAll(raw, "ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8=", "QEFCQ0RFRkdISUpLTE1OT1BRUlNUVVZXWFlaW1xdXl8=")
			case "missing_old_key":
				raw = processMasterOnlyTwo
			case "bad_canary":
				if _, err := db.Connect(t).Exec(databaseContext(t), `UPDATE agenteam_secret.secret_master_registry SET canary_ciphertext=set_byte(canary_ciphertext,0,get_byte(canary_ciphertext,0)#1) WHERE version=1`); err != nil {
					t.Fatal(err)
				}
			}
			p = launch(t, "agenteam", nil, secretEnvironment(t, db, raw))
			p.wait(t, 1)
			if strings.Contains(p.stderr.String(), `"event":"listening"`) || !strings.Contains(p.stderr.String(), `"phase":"failed"`) {
				t.Fatal("failed canary/required key reached HTTP")
			}
			if strings.Contains(p.stderr.String(), "key_b64") || strings.Contains(p.stderr.String(), "canary.plaintext") {
				t.Fatal("startup exposed encryption data")
			}
			noCentralBackends(t, db)
			assertDatabaseLogsSafe(t, p, db)
		})
	}
}
func TestCentralSecretStartupSignalCancelsOwnedRegistryLock(t *testing.T) {
	for _, signal := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(signal.String(), func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			m, err := postgres.NewMigrator(db.Config(t, nil))
			if err != nil {
				t.Fatal(err)
			}
			if result := m.Migrate(databaseContext(t)); !result.Migrated {
				t.Fatal(result.Fault)
			}
			guard := db.Connect(t)
			key, _ := foundation.SystemConfigLock("secret-write-key")
			if _, err = guard.Exec(databaseContext(t), `SELECT pg_advisory_lock($1)`, key.AdvisoryKey()); err != nil {
				t.Fatal(err)
			}
			p := launch(t, "agenteam", nil, secretEnvironment(t, db, processMasterOne, "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
			p.event(t, "phase", "secret_initializing")
			waitDatabaseFact(t, db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock' AND wait_event='advisory')`)
			started := time.Now()
			if err = p.command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			p.wait(t, 0)
			if time.Since(started) > 1500*time.Millisecond {
				t.Fatal("startup Secret shutdown reset force budget")
			}
			// Keep the guard held while checking: socket closure alone cannot masquerade
			// as successful cancellation of the actual owned waiting PostgreSQL backend.
			noCentralBackends(t, db)
			if strings.Contains(p.stderr.String(), `"event":"listening"`) {
				t.Fatal("cancelled Secret init listened")
			}
		})
	}
}

func secretDiagnostic(t *testing.T, address string) []byte {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + address + "/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != 200 {
		t.Fatal("diagnostic read failed", err)
	}
	return data
}

func waitSecretDiagnostic(t *testing.T, address string, version int64) []byte {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		data := secretDiagnostic(t, address)
		var out struct {
			Secret struct {
				Available bool   `json:"available"`
				Version   string `json:"write_version"`
				Rotation  string `json:"rotation"`
			} `json:"secret"`
		}
		if json.Unmarshal(data, &out) != nil {
			t.Fatal("invalid Secret diagnostics")
		}
		if out.Secret.Available && out.Secret.Version == strconv.FormatInt(version, 10) && out.Secret.Rotation == "completed" {
			return data
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatal("Secret diagnostics did not observe completed worker")
		}
	}
}
