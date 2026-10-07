//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/runtimeinfo"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type runtimeRootObserver struct {
	mu                           sync.Mutex
	source                       *runtimeInformationSource
	factories, clockCalls        int
	lastClock                    time.Time
	databaseChecks, objectChecks atomic.Int64
	reads, committed             atomic.Int64
}

func (o *runtimeRootObserver) now() time.Time {
	now := time.Now()
	o.mu.Lock()
	o.clockCalls++
	o.lastClock = now
	o.mu.Unlock()
	return now
}

type runtimeRootDatabase struct {
	*modelRootStore
	observer *runtimeRootObserver
}

func (s *runtimeRootDatabase) Check(ctx context.Context) (postgres.DatabaseHealth, error) {
	s.observer.databaseChecks.Add(1)
	return s.modelRootStore.Check(ctx)
}

func (s *runtimeRootDatabase) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	read := cause.Details().Owner == "runtimeinfo.system-read"
	if read {
		s.observer.reads.Add(1)
	}
	result := s.modelRootStore.WithinTx(ctx, cause, fn)
	if read && result.State() == foundation.Committed {
		s.observer.committed.Add(1)
	}
	return result
}

// Embedding the actual concrete owner preserves all lifecycle methods,
// including its private forceTransports capability; only Check is observed.
type runtimeRootObjects struct {
	*objectAssembly
	observer *runtimeRootObserver
}

func (s *runtimeRootObjects) Check(ctx context.Context) error {
	s.observer.objectChecks.Add(1)
	return s.objectAssembly.Check(ctx)
}

func TestSystemRuntimeInformationRootSnapshotBinding(t *testing.T) {
	started := time.Now()
	observer := &runtimeRootObserver{}
	a := newModelRootApp(t, "3s", func(a *modelRootApp, d *dependencies) {
		// Only this local collaborator's sampling window changes. The original
		// monitor still runs; production 10s/2s/20s defaults are not changed.
		d.health.interval = time.Hour
		d.health.now = observer.now
		open := d.open
		d.open = func(ctx context.Context, cfg postgres.Config) (database, error) {
			db, err := open(ctx, cfg)
			if err != nil {
				return db, err
			}
			if db != a.store {
				return nil, errors.New("fixture original Store changed")
			}
			return &runtimeRootDatabase{a.store, observer}, nil
		}
		bind := d.bind
		d.bind = func(ctx context.Context, cfg config.Config, db database, owned *resources, deps *dependencies) error {
			if err := bind(ctx, cfg, db, owned, deps); err != nil {
				return err
			}
			objects := deps.objects
			deps.objects = func(ctx context.Context, cfg config.Config, db database, auditing *audit.Service) (objectStorage, error) {
				original, err := objects(ctx, cfg, db, auditing)
				if err != nil {
					return original, err
				}
				assembly, ok := original.(*objectAssembly)
				if !ok {
					return original, errors.New("fixture original Object owner changed")
				}
				return &runtimeRootObjects{assembly, observer}, nil
			}
			factory := deps.runtimeInformation
			if factory == nil {
				return errors.New("formal runtime factory missing")
			}
			deps.runtimeInformation = func(source runtimeinfo.Source) (http.Handler, error) {
				actual, ok := source.(*runtimeInformationSource)
				if !ok {
					return nil, errors.New("root did not construct its actual Source")
				}
				observer.mu.Lock()
				observer.factories++
				observer.source = actual
				observer.mu.Unlock()
				return factory(source) // exactly the original Source, not a replacement
			}
			return nil
		}
	})
	address := a.address(t)
	observer.mu.Lock()
	source, factories := observer.source, observer.factories
	observer.mu.Unlock()
	if factories != 1 || source == nil || source.monitor == nil || source.secrets != a.owned.secret() || source.outbound != a.owned.outbound() {
		t.Fatal("root did not bind once to its original monitor/services")
	}
	if observer.databaseChecks.Load() < 1 || observer.objectChecks.Load() != 1 {
		t.Fatal("original required startup checks not observed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	login, err := fixtureAccountLoginResponse(ctx, a.cfg, a.core)
	if err != nil {
		t.Fatal("formal root login failed", err)
	}
	var material sc.SecretMaterial
	if err = login.UseCookie(func(raw []byte) error { var err error; material, err = sc.NewSecretMaterial(raw); return err }); err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	if err = login.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var cookie string
	if err = material.Use(func(raw []byte) error { cookie = string(raw); return nil }); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	type result struct {
		value     map[string]any
		requestID string
	}
	call := func(path string, status, limit int) result {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Host = "localhost:8080"
		request.Header.Set("Origin", "http://localhost:8080")
		request.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookie})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("root GET failed", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(limit+1)))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != status || len(body) > limit {
			t.Fatal("root GET incomplete or wrong status", response.StatusCode)
		}
		ids := response.Header.Values("X-Request-ID")
		if len(ids) != 1 {
			t.Fatal("root GET did not keep one RequestID")
		}
		parsed, err := foundation.ParseID[foundation.Request](ids[0])
		if err != nil || parsed.String() != ids[0] {
			t.Fatal("root RequestID was not canonical")
		}
		if strings.HasPrefix(path, "/api/") && (response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" || response.Header.Get("Referrer-Policy") != "no-referrer") {
			t.Fatal("root API security headers changed")
		}
		if path == runtimeInformationPath && (response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Content-Length") != strconv.Itoa(len(body))) {
			t.Fatal("runtime complete representation headers missing")
		}
		var value map[string]any
		if json.Unmarshal(body, &value) != nil {
			t.Fatal("root GET body was not JSON")
		}
		return result{value, ids[0]}
	}
	object := func(value map[string]any, field string, count int) map[string]any {
		t.Helper()
		obj, ok := value[field].(map[string]any)
		if !ok || len(obj) != count {
			t.Fatal("runtime nested DTO is incomplete", field)
		}
		return obj
	}
	var postgresVersion, vectorVersion string
	if err = a.store.QueryRow(ctx, `SELECT current_setting('server_version'),(SELECT extversion FROM pg_extension WHERE extname='vector')`).Scan(&postgresVersion, &vectorVersion); err != nil {
		t.Fatal("readonly actual PG version observation failed", err)
	}
	// Capture the actual successful startup cache. No test writes flags/times.
	monitor := source.monitor
	monitor.mu.RLock()
	health, received, objectReceived := monitor.last, monitor.received, monitor.objectReceived
	outboxReceived, accountReceived := monitor.outboxReceived, monitor.accountReceived
	dbAvailable, objectAvailable := monitor.available, monitor.objectAvailable
	outboxAvailable, accountAvailable, accountBound := monitor.outboxAvailable, monitor.accountAvailable, monitor.accountBound
	stale := monitor.timing.stale
	monitor.mu.RUnlock()
	if health.ServerVersion != postgresVersion || health.ExtensionVersion != vectorVersion || !healthy(health) || !accountBound || received.IsZero() || objectReceived.IsZero() || outboxReceived.IsZero() || accountReceived.IsZero() || stale != 20*time.Second {
		t.Fatal("root startup cache differs from real successful checks")
	}
	instantString := func(value time.Time) string {
		t.Helper()
		instant, err := foundation.NewInstant(value)
		if err != nil || instant.Validate() != nil || value.IsZero() {
			t.Fatal("invalid captured root time")
		}
		return instant.String()
	}
	status := func(available bool, sampled, observed time.Time) (string, any) {
		if !available {
			return "unavailable", "check_unavailable"
		}
		if observed.Sub(sampled) > stale {
			return "stale", "sample_stale"
		}
		return "available", nil
	}
	for range 2 {
		dbBefore, objectBefore := observer.databaseChecks.Load(), observer.objectChecks.Load()
		readsBefore, committedBefore := observer.reads.Load(), observer.committed.Load()
		observer.mu.Lock()
		clockBefore := observer.clockCalls
		observer.mu.Unlock()
		got := call(runtimeInformationPath, 200, 16<<10)
		observer.mu.Lock()
		clockAfter, observed, factoryCount := observer.clockCalls, observer.lastClock, observer.factories
		sameSource := observer.source == source
		observer.mu.Unlock()
		if clockAfter != clockBefore+1 || factoryCount != 1 || !sameSource || observer.reads.Load() != readsBefore+1 || observer.committed.Load() != committedBefore+1 {
			t.Fatal("GET did not use one observation and one real committed read with fixed factory")
		}
		if observer.databaseChecks.Load() != dbBefore || observer.objectChecks.Load() != objectBefore {
			t.Fatal("GET performed an extra health Check")
		}
		value := got.value
		if len(value) != 5 || value["observed_at"] != instantString(observed) {
			t.Fatal("runtime top-level shape or single observation time changed")
		}
		central := object(value, "central", 2)
		if central["version"] != nil || central["safe_reason"] != "build_version_not_recorded" {
			t.Fatal("unknown Central version was invented")
		}
		database := object(value, "database", 3)
		last := object(database, "last_success", 4)
		wantDB, dbReason := status(dbAvailable, received, observed)
		if database["status"] != wantDB || database["safe_reason"] != dbReason || last["checked_at"] != health.CheckedAt.String() || last["received_at"] != instantString(received) || last["postgresql_version"] != postgresVersion || last["pgvector_version"] != vectorVersion {
			t.Fatal("database projection did not preserve its actual historic sample")
		}
		objects := object(value, "object_storage", 6)
		wantObject, objectReason := status(objectAvailable, objectReceived, observed)
		if objects["backend"] != "minio" || objects["assessment"] != "object_storage_aggregate" || objects["status"] != wantObject || objects["safe_reason"] != objectReason || objects["last_success_received_at"] != instantString(objectReceived) || objects["details"] != "not_reported" {
			t.Fatal("Object aggregate observation was replaced or embellished")
		}
		wantOutbox, _ := status(outboxAvailable, outboxReceived, observed)
		wantAccount, _ := status(accountAvailable, accountReceived, observed)
		reason := string(foundation.DependencyUnbound)
		if wantDB != "available" || wantObject != "available" || wantOutbox != "available" || wantAccount != "available" || !source.secrets.Status().Available || !source.outbound.Status().Available {
			reason = string(foundation.DependencyUnavailable)
		}
		ready := object(value, "readiness", 2)
		if ready["ready"] != false || ready["safe_reason"] != reason {
			t.Fatal("runtime observation changed the six-item ready=false boundary")
		}
		monitor.mu.RLock()
		unchanged := monitor.last == health && monitor.received == received && monitor.objectReceived == objectReceived && monitor.outboxReceived == outboxReceived && monitor.accountReceived == accountReceived && monitor.available == dbAvailable && monitor.objectAvailable == objectAvailable && monitor.outboxAvailable == outboxAvailable && monitor.accountAvailable == accountAvailable
		monitor.mu.RUnlock()
		if !unchanged {
			t.Fatal("GET refreshed or mutated cached successful samples")
		}
	}
	for _, path := range []string{"/api/v1/me", "/api/v1/session", "/api/v1/system/model-providers", "/api/v1/system/outbound-policy", "/api/v1/system/audit"} {
		call(path, 200, 1<<20)
	}
	if live := call("/livez", 200, 16<<10); live.value["status"] != "alive" {
		t.Fatal("original livez changed")
	}
	diagnostic := call("/diagnostics", 200, 16<<10).value
	if diagnostic["ready"] != false || diagnostic["capabilities"] == nil || diagnostic["security_stage"] == nil {
		t.Fatal("original diagnostics replaced by management DTO")
	}
	ready := call("/readyz", 503, 16<<10).value
	if ready["code"] != string(foundation.DependencyUnbound) && ready["code"] != string(foundation.DependencyUnavailable) {
		t.Fatal("original readyz 503 reason changed")
	}
	if observer.factories != 1 {
		t.Fatal("old routes rebuilt runtime factory")
	}
	if strings.Contains(a.logs.String(), cookie) || bytes.Contains([]byte(a.logs.String()), []byte(`"last_success"`)) {
		t.Fatal("root ordinary logs exposed browser material or runtime DTO")
	}
	client.CloseIdleConnections()
	a.signals <- syscall.SIGTERM
	await(t, a.done)
	if a.err != nil || !a.owned.accounts().Joined() {
		t.Fatal("root normal shutdown did not actually join", a.err)
	}
	a.store.mu.Lock()
	normal := len(a.store.activeAtStop) != 0 && len(a.store.forceDeadline) == 0
	for _, active := range a.store.activeAtStop {
		normal = normal && active == 0
	}
	a.store.mu.Unlock()
	if !normal {
		t.Fatal("normal root shutdown required force or overtook HTTP owner")
	}
	if time.Since(started) > 2*time.Minute {
		t.Fatal("root scenario exceeded two-minute budget")
	}
	t.Logf("actual root factory=1 runtime_reads=%d committed=%d; per-GET DB/Object Check delta=0; original cache and PG versions agree; no MinIO fault injection; ready remains false; shutdown joined", observer.reads.Load(), observer.committed.Load())
}
