//go:build integration

package process_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func waitOutboxCapability(t *testing.T, address, want string) {
	t.Helper()
	deadline := time.NewTimer(14 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(30 * time.Millisecond)
	defer tick.Stop()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for {
		res, err := client.Get("http://" + address + "/diagnostics")
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Ready        bool                            `json:"ready"`
			Capabilities []struct{ Name, Status string } `json:"capabilities"`
		}
		err = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		if err != nil || body.Ready {
			t.Fatal("invalid readiness projection")
		}
		matched := false
		found := map[string]string{}
		for _, c := range body.Capabilities {
			found[c.Name] = c.Status
			if c.Name == "outbox" && c.Status == want {
				matched = true
			}
		}
		for _, name := range []string{"project_authorization", "runner_transfer_authorization", "runner_protocol"} {
			if found[name] != "unbound" {
				t.Fatal("outbox fabricated a future domain binding")
			}
		}
		if matched && found["outbox_authorization"] == "system_bound" && found["outbox_handlers"] == "available" && found["identity"] == "available" {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("outbox component state not observed")
		case <-tick.C:
		}
	}
}
func TestCentralOutboxEmptyMechanismHealthRecoveryAndSignal(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	p := launch(t, "agenteam", nil, databaseEnvironment(t, db))
	address := p.event(t, "event", "listening")["listen_address"].(string)
	waitOutboxCapability(t, address, "available")
	checkDiagnosticBinary(t, address)
	conn := db.Connect(t)
	var handlers, subscriptions, exactHandler, exactSubscription, attempts int
	if err := conn.QueryRow(databaseContext(t), `SELECT
 (SELECT count(*) FROM agenteam_outbox.handlers),
 (SELECT count(*) FROM agenteam_outbox.subscriptions),
 (SELECT count(*) FROM agenteam_outbox.handlers WHERE id='account.mail-enqueue' AND format=1 AND effect='canonical_converge' AND ordering_policy='canonical_reconcile'),
 (SELECT count(*) FROM agenteam_outbox.subscriptions WHERE handler_id='account.mail-enqueue' AND event_type='account.delivery-requested' AND format=1 AND accepted_versions=ARRAY[1]::bigint[]),
 (SELECT count(*) FROM agenteam_outbox.attempts)`).Scan(&handlers, &subscriptions, &exactHandler, &exactSubscription, &attempts); err != nil || handlers != 1 || subscriptions != 1 || exactHandler != 1 || exactSubscription != 1 || attempts != 0 {
		t.Fatal("production catalog differs from the sole account mail binding or fabricated a business attempt")
	}
	if _, err := conn.Exec(databaseContext(t), `ALTER TABLE agenteam_outbox.control RENAME TO unavailable_control`); err != nil {
		t.Fatal(err)
	}
	waitOutboxCapability(t, address, "unavailable")
	if _, err := conn.Exec(databaseContext(t), `ALTER TABLE agenteam_outbox.unavailable_control RENAME TO control`); err != nil {
		t.Fatal(err)
	}
	waitOutboxCapability(t, address, "available")
	if err := p.command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	noCentralBackends(t, db)
	assertDatabaseLogsSafe(t, p, db)
	if !strings.Contains(p.stderr.String(), `"phase":"outbox_available"`) || !strings.Contains(p.stderr.String(), `"phase":"outbox_unavailable"`) {
		t.Fatal("actual component health transitions omitted")
	}
}
func TestCentralOutboxInvalidStorageAndCatalogPreventListening(t *testing.T) {
	for _, mode := range []string{"storage", "catalog"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			cfg := db.Config(t, nil)
			m, err := postgres.NewMigrator(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if result := m.Migrate(databaseContext(t)); !result.Migrated {
				t.Fatal(result.Fault)
			}
			conn := db.Connect(t)
			if mode == "storage" {
				_, err = conn.Exec(databaseContext(t), `DROP TABLE agenteam_outbox.control`)
			} else {
				_, err = conn.Exec(databaseContext(t), `INSERT INTO agenteam_outbox.handlers(id,effect,ordering_policy,declaration_digest) VALUES('unbound.fixture','canonical_converge','version_guarded','sha256:'||repeat('0',64))`)
			}
			if err != nil {
				t.Fatal(err)
			}
			p := launch(t, "agenteam", nil, databaseEnvironment(t, db))
			p.wait(t, 1)
			logs := p.stderr.String()
			if strings.Contains(logs, `"event":"listening"`) || !strings.Contains(logs, `"phase":"outbox_unavailable"`) {
				t.Fatal("outbox initialization failure admitted HTTP")
			}
			noCentralBackends(t, db)
			assertDatabaseLogsSafe(t, p, db)
			var stopped int
			if err = conn.QueryRow(databaseContext(t), `SELECT count(*) FROM agenteam_object.process_claims WHERE state='stopped'`).Scan(&stopped); err != nil || stopped != 1 {
				t.Fatal("failed Outbox initialization leaked shared guard")
			}
		})
	}
}
func TestCentralOutboxStartupCancelJoinsOwnedDatabaseWait(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	m, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(databaseContext(t)); !result.Migrated {
		t.Fatal(result.Fault)
	}
	blocker := db.Connect(t)
	tx, err := blocker.Begin(databaseContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(databaseContext(t))
	if _, err = tx.Exec(databaseContext(t), `LOCK TABLE agenteam_outbox.control IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	p := launch(t, "agenteam", nil, databaseEnvironment(t, db, "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s"))
	waitDatabaseFact(t, db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE a.datname=current_database() AND a.application_name='agenteam' AND $1=ANY(pg_blocking_pids(a.pid)) AND position('agenteam_outbox.control' in a.query)>0)`, int32(blocker.PgConn().PID()))
	if strings.Contains(p.stderr.String(), `"event":"listening"`) {
		t.Fatal("listener preceded Outbox storage check")
	}
	if err = p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	noCentralBackends(t, db)
	assertDatabaseLogsSafe(t, p, db)
	var stopped int
	if err = blocker.QueryRow(databaseContext(t), `SELECT count(*) FROM agenteam_object.process_claims WHERE state='stopped'`).Scan(&stopped); err != nil || stopped != 1 {
		t.Fatal("cancelled initialization lost joined guard ownership")
	}
}
