//go:build integration

package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const appMasterTwo = `{"format":1,"current_version":"2","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="},{"version":"2","key_b64":"QEFCQ0RFRkdISUpLTE1OT1BRUlNUVVZXWFlaW1xdXl8="}]}`

// A test-only seed identity. The command binary and the actual app fixture
// still use unbound business authorities; this only creates genuine envelopes
// through the formal service/Audit ports before exercising process recovery.
type seedSecretIdentity struct{ actor identity.Actor }

func (a seedSecretIdentity) RequireCurrentSession(_ context.Context, _ foundation.Tx, actor identity.Actor) error {
	if actor.Details().Kind != identity.Human || actor.Details().UserID != a.actor.Details().UserID || actor.Details().SessionID != a.actor.Details().SessionID {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return nil
}
func (a seedSecretIdentity) AuthorizeSystem(ctx context.Context, tx foundation.Tx, actor identity.Actor, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if err := a.RequireCurrentSession(ctx, tx, actor); err != nil {
		return identity.AccessGrant{}, err
	}
	at, _ := foundation.NewInstant(time.Now())
	return identity.NewAccessGrant(actor, identity.SystemScope(), intent, at, 1)
}
func seedAppSecret(t *testing.T, db *pgfixture.Database) {
	t.Helper()
	ctx := databaseTestContext(t)
	cfg := db.Config(t, nil)
	m, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(ctx); !result.Migrated {
		t.Fatal(result.Fault)
	}
	store, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.ForceClose(closeCtx); err != nil {
			t.Error(err)
		}
	}()
	cursorKeys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := secret.LoadKeyring(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, cursorKeys)
	if err != nil {
		t.Fatal(err)
	}
	user, _ := foundation.NewID[identity.User]()
	session, _ := foundation.NewID[identity.Session]()
	actor, _ := identity.NewHuman(user, session)
	auth := seedSecretIdentity{actor}
	auditing, err := audit.New(store, cursorKeys, audit.Authorizations{Sessions: auth, System: auth})
	if err != nil {
		t.Fatal(err)
	}
	service, err := secret.New(store, keys, auditing, secret.Authorizations{Sessions: auth, System: auth})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	material, _ := sc.NewSecretMaterial([]byte("owned process worker payload"))
	defer material.Destroy()
	command, _ := foundation.NewCommandIdentity("secret", []string{user.String()}, "create", "process-worker-fixture")
	if _, err = service.ExecuteWrite(ctx, sc.WriteRequest{Actor: actor, Scope: identity.SystemScope(), Identity: command, Kind: sc.Create, Purpose: sc.System, Value: material}); err != nil {
		t.Fatal(err)
	}
}
func TestRealSecretWorkerHTTPDrainTimeoutAndSecondSignal(t *testing.T) {
	for _, mode := range []string{"secret_worker_graceful", "secret_worker_timeout", "secret_worker_second"} {
		t.Run(mode, func(t *testing.T) {
			db := pgfixture.NewDatabase(t)
			seedAppSecret(t, db)
			admin := db.Connect(t)
			if _, err := admin.Exec(databaseTestContext(t), `CREATE TABLE app_process_fact(value integer PRIMARY KEY)`); err != nil {
				t.Fatal(err)
			}
			locker := db.Connect(t)
			if _, err := locker.Exec(databaseTestContext(t), `BEGIN; SELECT payload_id FROM agenteam_secret.secret_payloads FOR UPDATE`); err != nil {
				t.Fatal(err)
			}
			timeout := "5s"
			if mode == "secret_worker_timeout" {
				timeout = "100ms"
			}
			p := launchDatabaseApp(t, db, mode, timeout, "AGENTEAM_CENTRAL_SECRET_KEYRING="+appMasterTwo, "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s")
			address := p.stderr.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })["listen_address"].(string)
			waitAppDatabase(t, db, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock' AND query LIKE 'UPDATE agenteam_secret.secret_payloads SET wrap_nonce=%')`)
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			response := asyncGet(client, "http://"+address+"/transaction")
			p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "transaction_entered" })
			started := time.Now()
			if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			p.stderr.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
			if strings.Contains(p.stdout.String(), "database_admission_stopped") {
				t.Fatal("DB stopped before HTTP and current rewrap batch drained")
			}
			if mode == "secret_worker_graceful" {
				_, _ = io.WriteString(p.stdin, "release\n")
				got := receiveResponse(t, response)
				if got.err != nil || got.status != 200 {
					t.Fatal("HTTP could not commit during Secret drain", got.err)
				}
				if strings.Contains(p.stdout.String(), "database_admission_stopped") {
					t.Fatal("DB stopped while worker SQL was still blocked")
				}
				if _, err := locker.Exec(databaseTestContext(t), `ROLLBACK`); err != nil {
					t.Fatal(err)
				}
				p.wait(t, 0)
				var processed int
				var state string
				if err := admin.QueryRow(databaseTestContext(t), `SELECT processed,state FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processed, &state); err != nil || processed != 2 || state != "running" {
					t.Fatalf("current batch/checkpoint %d %s %v", processed, state, err)
				}
			} else {
				if mode == "secret_worker_second" {
					if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
						t.Fatal(err)
					}
				}
				p.wait(t, 1)
				if time.Since(started) > 1600*time.Millisecond {
					t.Fatal("HTTP/SQL worker force budgets were added")
				}
				if got := receiveResponse(t, response); got.err == nil && got.status == 200 {
					t.Fatal("forced HTTP falsely committed")
				}
				var processed int
				if err := admin.QueryRow(databaseTestContext(t), `SELECT processed FROM agenteam_secret.secret_rotation_runs WHERE target_version=2`).Scan(&processed); err != nil || processed != 0 {
					t.Fatal("uncommitted worker checkpoint escaped", err)
				}
			}
			// Keep the row lock held in forced cases. All owned PostgreSQL backends must
			// still exit through real cancellation, not wait for fixture teardown.
			waitAppDatabase(t, db, `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`)
		})
	}
}
func TestRealSecretStartupSecondSignalSingleForceBudget(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	m, err := postgres.NewMigrator(db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(databaseTestContext(t)); !result.Migrated {
		t.Fatal(result.Fault)
	}
	guard := db.Connect(t)
	key, _ := foundation.SystemConfigLock("secret-write-key")
	if _, err = guard.Exec(databaseTestContext(t), `SELECT pg_advisory_lock($1)`, key.AdvisoryKey()); err != nil {
		t.Fatal(err)
	}
	p := launchDatabaseApp(t, db, "secret_startup_second_signal", "5s", "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s")
	waitAppDatabase(t, db, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam' AND wait_event_type='Lock' AND wait_event='advisory')`)
	if err = p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "secret_initialization_returned" })
	start := time.Now()
	if err = p.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 1)
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatal("Secret startup force reset budget")
	}
	waitAppDatabase(t, db, `SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`)
	if strings.Contains(p.stderr.String(), `"event":"listening"`) {
		t.Fatal("cancelled Secret startup listened")
	}
}
