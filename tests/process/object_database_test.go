//go:build integration

package process_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type processStorageProxy struct {
	server          *httptest.Server
	mode            atomic.Int32
	reached, joined chan struct{}
}

func newProcessStorageProxy(t *testing.T) *processStorageProxy {
	t.Helper()
	d, err := objectfixture.Load()
	if err != nil {
		t.Fatal("owned object descriptor unavailable")
	}
	_, transport, err := d.Client()
	if err != nil {
		t.Fatal("owned TLS client unavailable")
	}
	upstream, _ := url.Parse(d.Endpoint())
	p := &processStorageProxy{reached: make(chan struct{}, 1), joined: make(chan struct{}, 1)}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe := strings.Contains(r.URL.Path, "/control/probe/")
		if p.mode.Load() == 2 && strings.Contains(r.URL.Path, "/control/") && r.Method == http.MethodGet {
			http.Error(w, "owned storage unavailable", 503)
			return
		}
		request := r.Clone(r.Context())
		request.RequestURI = ""
		request.URL.Scheme, request.URL.Host = upstream.Scheme, upstream.Host
		response, err := transport.RoundTrip(request)
		if err != nil {
			http.Error(w, "owned upstream unavailable", 503)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			w.Header()[key] = append([]string(nil), values...)
		}
		w.WriteHeader(response.StatusCode)
		if p.mode.Load() == 1 && probe && r.Method == http.MethodGet && response.StatusCode == 200 {
			w.(http.Flusher).Flush()
			select {
			case p.reached <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			select {
			case p.joined <- struct{}{}:
			default:
			}
			return
		}
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(func() { p.server.CloseClientConnections(); p.server.Close(); transport.CloseIdleConnections() })
	return p
}

func (p *processStorageProxy) environment(t *testing.T, db *pgfixture.Database) []string {
	return databaseEnvironment(t, db, "AGENTEAM_CENTRAL_OBJECT_ENDPOINT="+p.server.URL, "AGENTEAM_CENTRAL_OBJECT_TLS_MODE=disable", "AGENTEAM_CENTRAL_OBJECT_CA_FILE=")
}

func TestCentralObjectStartupSignalClosesActualProbeBeforeListen(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newProcessStorageProxy(t)
	proxy.mode.Store(1)
	p := launch(t, "agenteam", nil, proxy.environment(t, db))
	select {
	case <-proxy.reached:
	case <-time.After(8 * time.Second):
		t.Fatal("actual storage probe never started")
	}
	if strings.Contains(p.stderr.String(), `"event":"listening"`) {
		t.Fatal("listener preceded actual probe")
	}
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	select {
	case <-proxy.joined:
	case <-time.After(time.Second):
		t.Fatal("startup storage socket not closed")
	}
	if strings.Contains(p.stderr.String(), `"event":"listening"`) {
		t.Fatal("cancelled object startup listened")
	}
	noCentralBackends(t, db)
	var unfinished, stopped int
	if err := db.Connect(t).QueryRow(databaseContext(t), `SELECT (SELECT count(*) FROM agenteam_object.startup_probes WHERE phase<>'complete'),(SELECT count(*) FROM agenteam_object.process_claims WHERE state='stopped')`).Scan(&unfinished, &stopped); err != nil || unfinished != 1 || stopped != 1 {
		t.Fatal("cancelled actual probe lost checkpoint/join", unfinished, stopped, err)
	}
	assertDatabaseLogsSafe(t, p, db)
}

func TestCentralObjectStartupSecondSignalKeepsUnconfirmedProcessClaim(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newProcessStorageProxy(t)
	proxy.mode.Store(1)
	environment := append(proxy.environment(t, db), "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT=10s", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s")
	p := launch(t, "agenteam", nil, environment)
	select {
	case <-proxy.reached:
	case <-time.After(8 * time.Second):
		t.Fatal("actual storage probe never started")
	}
	guard := db.Connect(t)
	var processID string
	if err := guard.QueryRow(databaseContext(t), `SELECT process_id FROM agenteam_object.startup_probes`).Scan(&processID); err != nil {
		t.Fatal(err)
	}
	key, _ := foundation.SystemConfigLock("object-process-" + processID)
	if _, err := guard.Exec(databaseContext(t), `SELECT pg_advisory_lock($1)`, key.AdvisoryKey()); err != nil {
		t.Fatal(err)
	}
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// Late runtime acquisition stays owned: after the actual probe joins,
	// its normal drain must reach the exact process-claim completion lock.
	waitDatabaseFact(t, db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid WHERE a.datname=current_database() AND a.application_name='agenteam' AND l.locktype='advisory' AND NOT l.granted AND $1=ANY(pg_blocking_pids(a.pid)))`, int32(guard.PgConn().PID()))
	start := time.Now()
	if err := p.command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 1)
	if !strings.Contains(p.stderr.String(), `"code":"FORCED_SHUTDOWN"`) {
		t.Fatal("second signal omitted forced shutdown result")
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatal("late object resource renewed force budget")
	}
	select {
	case <-proxy.joined:
	default:
		t.Fatal("forced startup failed to join actual probe")
	}
	noCentralBackends(t, db)
	var state string
	if err := guard.QueryRow(databaseContext(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, processID).Scan(&state); err != nil || state != "claimed" {
		t.Fatal("unconfirmed process finalization invented stopped state", state, err)
	}
	if strings.Contains(p.stderr.String(), `"event":"listening"`) {
		t.Fatal("forced object startup listened")
	}
	assertDatabaseLogsSafe(t, p, db)
}

func waitObjectCapability(t *testing.T, address, want string) {
	t.Helper()
	timeout := time.NewTimer(14 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(30 * time.Millisecond)
	defer tick.Stop()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for {
		response, err := client.Get("http://" + address + "/diagnostics")
		if err != nil {
			t.Fatal("owned diagnostic unavailable")
		}
		var diagnostic struct {
			Ready        bool
			Capabilities []struct{ Name, Status string }
		}
		err = json.NewDecoder(response.Body).Decode(&diagnostic)
		_ = response.Body.Close()
		if err != nil || diagnostic.Ready {
			t.Fatal("invalid object diagnostic")
		}
		matched := false
		for _, c := range diagnostic.Capabilities {
			if c.Name == "object_storage" && c.Status == want {
				matched = true
			}
			if (c.Name == "object_authorization" || c.Name == "runner_transfer_authorization") && c.Status != "unbound" {
				t.Fatal("invented product authority")
			}
			if c.Name == "postgresql" && c.Status != "available" {
				t.Fatal("object failure incorrectly marked DB unavailable")
			}
		}
		if matched {
			return
		}
		select {
		case <-tick.C:
		case <-timeout.C:
			t.Fatal("object capability did not track actual storage")
		}
	}
}

func TestCentralObjectHealthReflectsActualStorageFailureAndRecovery(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	proxy := newProcessStorageProxy(t)
	p := launch(t, "agenteam", nil, proxy.environment(t, db))
	address := p.event(t, "event", "listening")["listen_address"].(string)
	waitObjectCapability(t, address, "available")
	proxy.mode.Store(2)
	waitObjectCapability(t, address, "unavailable")
	proxy.mode.Store(0)
	waitObjectCapability(t, address, "available")
	if err := p.command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	noCentralBackends(t, db)
	assertDatabaseLogsSafe(t, p, db)
}
