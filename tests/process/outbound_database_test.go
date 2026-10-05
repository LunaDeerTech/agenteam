//go:build integration

package process_test

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func TestCentralOutboundActualStartupDiagnosticAndSignals(t *testing.T) {
	for _, signal := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(signal.String(), func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			p := launch(t, "agenteam", nil, databaseEnvironment(t, db))
			address := p.event(t, "event", "listening")["listen_address"].(string)
			data := secretDiagnostic(t, address)
			var d struct {
				Ready         bool
				SecurityStage string `json:"security_stage"`
				Outbound      struct {
					Available bool
					Version   string
				}
				Capabilities []struct{ Name, Status string }
			}
			if err := json.Unmarshal(data, &d); err != nil || d.Ready || !d.Outbound.Available || d.Outbound.Version != "1" || d.SecurityStage != "initialized" {
				t.Fatal("policy diagnostic not actual v1", err)
			}
			found := map[string]string{}
			for _, cap := range d.Capabilities {
				found[cap.Name] = cap.Status
			}
			if found["outbound"] != "available" || found["outbound_authorization"] != "system_bound" || found["identity"] != "available" {
				t.Fatal("outbound or the actual account authority is unavailable")
			}
			for _, name := range []string{"project_authorization", "runner_transfer_authorization", "runner_protocol"} {
				if found[name] != "unbound" {
					t.Fatal("outbound fabricated a future domain binding")
				}
			}
			for _, private := range []string{"cidr", "allow_http", "key_b64", "ciphertext"} {
				if strings.Contains(string(data), private) {
					t.Fatal("private security facts in diagnostics")
				}
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

func TestCentralOutboundInvalidStoragePreventsListening(t *testing.T) {
	for _, mode := range []string{"missing", "invalid_rules"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			m, err := postgres.NewMigrator(db.Config(t, nil))
			if err != nil {
				t.Fatal(err)
			}
			if result := m.Migrate(databaseContext(t)); !result.Migrated {
				t.Fatal(result.Fault)
			}
			query := `DROP TABLE agenteam_outbound.outbound_policy`
			if mode == "invalid_rules" {
				query = `UPDATE agenteam_outbound.outbound_policy SET rules='[{"cidr":"127.0.0.0/8","ports":"all","allow_http":true}]'::jsonb`
			}
			if _, err := db.Connect(t).Exec(databaseContext(t), query); err != nil {
				t.Fatal(err)
			}
			p := launch(t, "agenteam", nil, databaseEnvironment(t, db))
			p.wait(t, 1)
			logs := p.stderr.String()
			if strings.Contains(logs, `"event":"listening"`) || !strings.Contains(logs, `"phase":"outbound_initializing"`) || !strings.Contains(logs, `"event":"security","phase":"failed"`) {
				t.Fatal("invalid policy reached HTTP or omitted stage")
			}
			noCentralBackends(t, db)
			assertDatabaseLogsSafe(t, p, db)
		})
	}
}

func TestCentralOutboundStartupCancellationStopsOwnedBackend(t *testing.T) {
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
			key, _ := foundation.SystemConfigLock("outbound-policy")
			if _, err := guard.Exec(databaseContext(t), `SELECT pg_advisory_lock($1)`, key.AdvisoryKey()); err != nil {
				t.Fatal(err)
			}
			p := launch(t, "agenteam", nil, databaseEnvironment(t, db, "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
			p.event(t, "phase", "outbound_initializing")
			waitDatabaseFact(t, db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock' AND wait_event='advisory')`)
			start := time.Now()
			if err := p.command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			p.wait(t, 0)
			if time.Since(start) > 1500*time.Millisecond {
				t.Fatal("policy cancellation exceeded shared bound")
			}
			noCentralBackends(t, db)
			if strings.Contains(p.stderr.String(), `"event":"listening"`) {
				t.Fatal("cancelled policy load bound HTTP")
			}
		})
	}
}
