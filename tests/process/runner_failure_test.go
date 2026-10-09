//go:build integration && linux

package process_test

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This transport may hold a new device request or change only its upstream
// address after an actual Central restart. No successful response is supplied.
type runnerFailureTransport struct {
	server    *httptest.Server
	upstream  atomic.Pointer[url.URL]
	held      atomic.Bool
	requests  atomic.Int32
	wss       atomic.Int32
	enroll    atomic.Int32
	reached   chan struct{}
	release   chan struct{}
	reachOnce sync.Once
	freeOnce  sync.Once
}

func (v *runnerFailureTransport) target(t *testing.T, address string) {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		t.Fatal("original Central transport address invalid")
	}
	v.upstream.Store(u)
}
func (v *runnerFailureTransport) unblock() { v.freeOnce.Do(func() { close(v.release) }) }

func (v *runnerFailureTransport) controlRetired(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for v.wss.Load() != 0 {
		select {
		case <-ctx.Done():
			t.Fatal("original default control transport has not joined")
		case <-tick.C:
		}
	}
}

func newRunnerFailureTransport(t *testing.T, address string) *runnerFailureTransport {
	t.Helper()
	v := &runnerFailureTransport{reached: make(chan struct{}), release: make(chan struct{})}
	v.target(t, address)
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, ResponseHeaderTimeout: 5 * time.Second, DisableKeepAlives: true}
	proxy := &httputil.ReverseProxy{
		Rewrite:   func(r *httputil.ProxyRequest) { r.SetURL(v.upstream.Load()) },
		Transport: transport, ErrorLog: log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "unavailable", http.StatusBadGateway)
		},
	}
	v.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.requests.Add(1)
		defer v.requests.Add(-1)
		if r.URL.Path == "/api/v1/runner/enroll" {
			v.enroll.Add(1)
		}
		if v.held.Load() {
			v.reachOnce.Do(func() { close(v.reached) })
			select {
			case <-r.Context().Done():
				return
			case <-v.release:
			}
			if r.Context().Err() != nil {
				return
			}
		}
		if r.URL.Path == "/api/v1/runner/control" {
			v.wss.Add(1)
			defer v.wss.Add(-1)
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		v.unblock()
		transport.CloseIdleConnections()
		v.server.Close()
		if v.requests.Load() != 0 || v.wss.Load() != 0 {
			t.Error("original failure transport handlers have not returned")
		}
	})
	return v
}

func newRunnerFailureCentral(t *testing.T) *modelSystemBinary {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	env := databaseEnvironment(t, db, "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080", "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s")
	v := &modelSystemBinary{db: db, env: env, client: &http.Client{Timeout: 5 * time.Second}, cookies: map[string]*http.Cookie{}}
	for _, e := range env {
		if strings.HasPrefix(e, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=") {
			v.logPath = strings.TrimPrefix(e, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=")
		}
	}
	v.start(t)
	t.Cleanup(v.client.CloseIdleConnections)
	t.Cleanup(func() { clear(v.cookies); v.csrf = ""; clear(v.logSecrets) })
	v.login(t)
	return v
}

type runnerFailureDevice struct {
	process       *process
	path, target  string
	configuration identity.Configuration
	public        [32]byte
}

func startRunnerFailureDevice(t *testing.T, central *modelSystemBinary, proxy *runnerFailureTransport) runnerFailureDevice {
	t.Helper()
	directory := runnerRootDirectory(t)
	path, ca := filepath.Join(directory, "identity.json"), filepath.Join(directory, "root.pem")
	if os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.server.Certificate().Raw}), 0600) != nil {
		t.Fatal("owned failure transport trust unavailable")
	}
	target := modelSystemKey(t)
	runnerID, err := f.ParseID[rc.Runner](target)
	if err != nil {
		t.Fatal("owned failure Runner identity invalid")
	}
	configuration := identity.Configuration{CentralURL: proxy.server.URL, RunnerID: p.ID(target), RootPath: "/srv/runner"}
	_, token := runnerRootCredential(t, central.request(t, "POST", "/api/v1/system/runners", modelSystemKey(t), rc.CreateRequest{RunnerID: runnerID, Name: "root failure runner", Description: "bounded lifecycle", Tags: []string{}, RootPath: configuration.RootPath}, nil).want(t, 200), true)
	env := []string{"AGENTEAM_RUNNER_IDENTITY_FILE=" + path, "AGENTEAM_RUNNER_CA_FILE=" + ca, "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT=3s", "AGENTEAM_RUNNER_CENTRAL_URL=" + configuration.CentralURL, "AGENTEAM_RUNNER_ID=" + target, "AGENTEAM_RUNNER_ROOT_PATH=" + configuration.RootPath}
	child := runnerRootLaunch(t, env, &token)
	runnerRootState(t, child, "connected")
	public, seed := runnerRootIdentity(t, path, configuration)
	central.logSecrets = append(central.logSecrets, token, seed, path)
	online := runnerRootSnapshot(t, central, target, rc.Online)
	if online.Version != 2 || online.CredentialGeneration != 1 || online.PublicKeyFingerprint == nil || string(*online.PublicKeyFingerprint) != p.PublicKeyFingerprint(public) || online.LastHello == nil || len(online.LastHello.Capabilities) != 0 || len(online.LastHello.FeatureFlags) != 0 || proxy.wss.Load() != 1 {
		t.Fatal("default failure roots did not establish original identity and empty registry")
	}
	runnerRootLock(t, path, true)
	return runnerFailureDevice{child, path, target, configuration, public}
}

func runnerFailureSafeOutput(t *testing.T, central *modelSystemBinary, processes ...*process) {
	t.Helper()
	for _, child := range processes {
		select {
		case <-child.done:
		default:
			t.Fatal("failure output inspected before actual process Wait")
		}
		for _, secret := range central.logSecrets {
			if strings.Contains(child.stderr.String()+child.stdout.String(), secret) {
				t.Fatal("failure process output leaked private identity or enrollment material")
			}
		}
	}
}

func runnerFailureKilled(t *testing.T, child *process) {
	t.Helper()
	if child.command.Process.Kill() != nil {
		t.Fatal("owned Central SIGKILL failed")
	}
	select {
	case <-child.done:
	case <-time.After(5 * time.Second):
		t.Fatal("original killed Central Wait did not return")
	}
	var exit *exec.ExitError
	if !errors.As(child.err, &exit) {
		t.Fatal("killed Central did not return an actual exit status")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("Central actual Wait did not prove the owned unexpected exit")
	}
}

// These cases require the unchanged seven-resource root chain. They run the
// real default commands; the proxy only holds transport and SQL only adds a
// cancellation-sensitive lock barrier to the original connection DELETE.
func TestRunnerControlDefaultFailures(t *testing.T) {
	t.Run("central_crash_original_lease_and_same_key_reconnect", func(t *testing.T) {
		central := newRunnerFailureCentral(t)
		proxy := newRunnerFailureTransport(t, central.address)
		device := startRunnerFailureDevice(t, central, proxy)
		conn := central.db.Connect(t)
		var owner, connection string
		var lease time.Time
		var originalNonceHash []byte
		if err := conn.QueryRow(databaseContext(t), `SELECT owner_id,id,lease_expires_at FROM agenteam_runner.connections WHERE runner_id=$1`, device.target).Scan(&owner, &connection, &lease); err != nil {
			t.Fatal("original live Central connection facts unavailable")
		}
		if err := conn.QueryRow(databaseContext(t), `SELECT nonce_hash FROM agenteam_runner.challenges WHERE runner_id=$1 AND consumed_at IS NOT NULL`, device.target).Scan(&originalNonceHash); err != nil || len(originalNonceHash) != 32 {
			t.Fatal("original consumed challenge fact unavailable")
		}
		proxy.held.Store(true)
		original := central.p
		runnerFailureKilled(t, original)
		noCentralBackends(t, central.db)
		runnerRootState(t, device.process, "disconnected")
		proxy.controlRetired(t)
		select {
		case <-proxy.reached:
		case <-time.After(5 * time.Second):
			t.Fatal("original Runner did not attempt a fresh connection after Central death")
		}
		central.start(t)
		proxy.target(t, central.address)
		// The new root owns no old socket. Its real HTTP Reader must still
		// report the committed old-owner lease while DB time says it is live.
		var live bool
		if err := conn.QueryRow(databaseContext(t), `SELECT owner_id=$2 AND id=$3 AND lease_expires_at=$4 AND lease_expires_at>clock_timestamp() FROM agenteam_runner.connections WHERE runner_id=$1`, device.target, owner, connection, lease).Scan(&live); err != nil || !live || proxy.wss.Load() != 0 {
			t.Fatal("crash/restart changed or outlived the original bounded lease before observation")
		}
		runnerRootSnapshot(t, central, device.target, rc.Online)
		waitDatabaseFact(t, conn, `SELECT clock_timestamp()>=$1::timestamptz`, lease)
		runnerRootSnapshot(t, central, device.target, rc.Offline)
		if err := conn.QueryRow(databaseContext(t), `SELECT owner_id=$2 AND id=$3 AND lease_expires_at=$4 AND NOT(lease_expires_at>clock_timestamp()) FROM agenteam_runner.connections WHERE runner_id=$1`, device.target, owner, connection, lease).Scan(&live); err != nil || !live {
			t.Fatal("lease observation rewrote the original connection instead of awaiting expiry")
		}
		proxy.unblock()
		runnerRootState(t, device.process, "connected")
		public, _ := runnerRootIdentity(t, device.path, device.configuration)
		online := runnerRootSnapshot(t, central, device.target, rc.Online)
		if public != device.public || online.PublicKeyFingerprint == nil || string(*online.PublicKeyFingerprint) != p.PublicKeyFingerprint(device.public) || proxy.enroll.Load() != 1 || proxy.wss.Load() != 1 {
			t.Fatal("new Central connection changed key or replayed enrollment")
		}
		if err := conn.QueryRow(databaseContext(t), `SELECT
 (SELECT owner_id<>$2 AND id<>$3 AND generation=2 AND credential_generation=1 AND hello_at IS NOT NULL FROM agenteam_runner.connections WHERE runner_id=$1) AND
 (SELECT connection_generation=2 AND version=2 AND credential_generation=1 FROM agenteam_runner.runners WHERE id=$1) AND
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1)=1 AND
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1 AND consumed_at IS NOT NULL)=1 AND
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1)=2 AND
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1)=2 AND
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1 AND consumed_at IS NOT NULL)=1 AND
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1 AND consumed_at IS NOT NULL AND nonce_hash<>$4)=1`, device.target, owner, connection, originalNonceHash).Scan(&live); err != nil || !live {
			t.Fatal("same-key reconnect lost the fresh owner/nonce or duplicated durable identity facts")
		}
		runnerRootStop(t, device.process, syscall.SIGTERM)
		proxy.controlRetired(t)
		runnerRootLock(t, device.path, false)
		runnerRootSnapshot(t, central, device.target, rc.Offline)
		central.stop(t, syscall.SIGTERM)
		runnerFailureSafeOutput(t, central, original, central.p, device.process)
	})
	for _, mode := range []string{"second_signal", "original_deadline"} {
		t.Run("central_held_control_retirement_"+mode, func(t *testing.T) {
			central := newRunnerFailureCentral(t)
			proxy := newRunnerFailureTransport(t, central.address)
			device := startRunnerFailureDevice(t, central, proxy)
			guard := central.db.Connect(t)
			const keyA, keyB = 151041, 7
			if _, err := guard.Exec(databaseContext(t), `SELECT pg_advisory_lock($1::integer,$2::integer)`, keyA, keyB); err != nil {
				t.Fatal("owned retirement barrier could not be acquired")
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if _, err := guard.Exec(ctx, `SELECT pg_advisory_unlock($1::integer,$2::integer)`, keyA, keyB); err != nil {
					t.Error("owned retirement barrier did not actually unlock")
				}
			})
			// The only direct SQL mutation is the observer barrier, never a
			// replacement credential, canonical receipt, lease or connection.
			statement := fmt.Sprintf(`CREATE FUNCTION agenteam_runner.test_c_retire_gate() RETURNS trigger LANGUAGE plpgsql AS $body$
BEGIN IF OLD.runner_id='%s'::uuid THEN PERFORM pg_advisory_xact_lock(%d,%d); END IF; RETURN OLD; END $body$;
CREATE TRIGGER test_c_retire_gate BEFORE DELETE ON agenteam_runner.connections FOR EACH ROW EXECUTE FUNCTION agenteam_runner.test_c_retire_gate()`, device.target, keyA, keyB)
			if _, err := guard.Exec(databaseContext(t), statement); err != nil {
				t.Fatal("owned original DELETE barrier could not be installed")
			}
			started := time.Now()
			if central.p.command.Process.Signal(syscall.SIGTERM) != nil {
				t.Fatal("owned Central initial stop failed")
			}
			witness := central.db.Connect(t)
			waitDatabaseFact(t, witness, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid WHERE a.datname=current_database() AND a.application_name='agenteam' AND l.locktype='advisory' AND NOT l.granted AND l.classid=$1::oid AND l.objid=$2::oid AND l.objsubid=2 AND $3=ANY(pg_blocking_pids(a.pid)) AND a.query LIKE 'DELETE FROM agenteam_runner.connections%')`, keyA, keyB, int32(guard.PgConn().PID()))
			select {
			case <-central.p.done:
				t.Fatal("Central exited while its original retirement callback was held")
			default:
			}
			forceAt := time.Now()
			if mode == "second_signal" && central.p.command.Process.Signal(syscall.SIGINT) != nil {
				t.Fatal("owned Central second signal failed")
			}
			runnerRootWait(t, central.p, 1)
			want := "SHUTDOWN_TIMEOUT"
			if mode == "second_signal" {
				want = "FORCED_SHUTDOWN"
				if time.Since(forceAt) > 2*time.Second {
					t.Fatal("second signal renewed the original force tail")
				}
			} else if elapsed := time.Since(started); elapsed < 2800*time.Millisecond || elapsed > 5*time.Second {
				t.Fatal("held default Central did not use its original shutdown deadline")
			}
			text := central.p.stderr.String()
			if !strings.Contains(text, `"code":"`+want+`"`) || !strings.Contains(text, `"outcome":"forced"`) || strings.Contains(text, `"outcome":"drained"`) {
				t.Fatal("held original callback was reported as clean shutdown")
			}
			noCentralBackends(t, central.db)
			var retained bool
			if err := witness.QueryRow(databaseContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_runner.connections WHERE runner_id=$1)`, device.target).Scan(&retained); err != nil || !retained {
				t.Fatal("cancelled original retirement was falsely committed")
			}
			runnerRootState(t, device.process, "disconnected")
			runnerRootStop(t, device.process, syscall.SIGTERM)
			proxy.controlRetired(t)
			runnerRootLock(t, device.path, false)
			runnerFailureSafeOutput(t, central, central.p, device.process)
			assertDatabaseLogsSafe(t, central.p, central.db)
		})
	}
}
