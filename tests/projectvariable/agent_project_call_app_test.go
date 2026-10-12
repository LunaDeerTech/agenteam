//go:build integration && linux

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/app"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

type agentProjectAppCall struct {
	ID         string     `json:"id"`
	Project    string     `json:"project_id"`
	Process    string     `json:"process_id"`
	Instance   string     `json:"instance_id"`
	Lane       string     `json:"lane"`
	Class      string     `json:"class"`
	Phase      string     `json:"phase"`
	Registered time.Time  `json:"registered_at"`
	Joined     *time.Time `json:"joined_at"`
}

type agentProjectApp struct {
	setup   *agentCreateFixture
	http    *agentAppHTTP
	ctx     context.Context
	signals chan os.Signal
	done    chan struct{}
	stop    sync.Once
	process string
	claim   *os.File
	logs    *agentAppLog
	runErr  error
}

func (v *agentProjectApp) requestStop() { v.stop.Do(func() { v.signals <- syscall.SIGTERM }) }

func (v *agentProjectApp) calls(t *testing.T) []agentProjectAppCall {
	t.Helper()
	var raw []byte
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_agent.project_calls c WHERE project_id=$1`, v.setup.p2.project.ID.String()).Scan(&raw))
	var rows []agentProjectAppCall
	agentAppRequire(t, json.Unmarshal(raw, &rows))
	return rows
}

func (v *agentProjectApp) callReturned(t *testing.T, before []agentProjectAppCall, lane, class string) agentProjectAppCall {
	t.Helper()
	after := v.calls(t)
	prior := make(map[string]agentProjectAppCall, len(before))
	for _, row := range before {
		prior[row.ID] = row
	}
	var added []agentProjectAppCall
	for _, row := range after {
		if previous, found := prior[row.ID]; found {
			if !reflect.DeepEqual(previous, row) {
				t.Fatal("another public Agent call rewrote original ownership history")
			}
		} else {
			added = append(added, row)
		}
	}
	if len(after) != len(before)+1 || len(added) != 1 {
		t.Fatal("one original HTTP call did not retain exactly one distinct CallID")
	}
	row := added[0]
	if row.ID == "" || row.Project != v.setup.p2.project.ID.String() || row.Process != v.process || row.Instance == "" || row.Lane != lane || row.Class != class || row.Phase != "joined" || row.Registered.IsZero() || row.Joined == nil || row.Joined.Before(row.Registered) {
		t.Fatal("original HTTP return omitted its complete joined call identity")
	}
	for _, other := range before {
		if other.Process != row.Process || other.Instance != row.Instance {
			t.Fatal("default App split Service and Directory across ownership pairs")
		}
	}
	return row
}

// Unlike the old completed-exercise wrapper, this task-local owner exposes its
// original signal channel so a real pending Directory checkpoint crosses App
// Drain. Configuration, public Run, TLS forwarding and setup services are real.
func withAgentProjectApp(t *testing.T, a *agentCreateFixture, oldProcess string, exercise func(*agentProjectApp)) {
	t.Helper()
	directory, err := os.MkdirTemp("", "agent-call-app-")
	agentAppRequire(t, err)
	h := &agentAppHTTP{done: make(chan agentAppProxyResult, 1), transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, createPath: variableHTTPPath(a.p2.project.ID, "/agents")}
	h.server = httptest.NewUnstartedServer(nil)
	h.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	cfg := agentConfigurationAppConfig(t, a, directory, "https://"+h.server.Listener.Addr().String())
	if _, enabled := cfg.ExecutionRuntime(); enabled {
		t.Fatal("Agent ownership App fixture enabled Execution Runtime")
	}
	logs := &agentAppLog{listening: make(chan string, 1)}
	logger, err := logging.New(logging.Central, slog.LevelInfo, logs)
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	v := &agentProjectApp{setup: a, http: h, ctx: ctx, signals: make(chan os.Signal, 1), done: make(chan struct{}), logs: logs}
	go func() { v.runErr = app.Run(ctx, cfg, logger, v.signals); close(v.done) }()
	defer func() {
		v.requestStop()
		select {
		case <-v.done:
		case <-time.After(15 * time.Second):
			t.Error("original App did not return within cleanup budget; retaining its dependencies")
			cancel()
			<-v.done // Never drop the original fixture DB under an unreturned App.
		}
		h.server.Close()
		h.transport.CloseIdleConnections()
		cancel()
		if v.claim != nil {
			agentProjectAppJoinClaim(t, v.claim)
		} else {
			// Startup may fail after acquiring a guard but before the listener.
			// Inspect only this fresh App's own fixed claim directory before
			// allowing the inherited destructive fixture cleanup to continue.
			claims := cfg.Objects().SpoolDirectory() + ".processes"
			entries, err := os.ReadDir(claims)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Error("cannot establish original startup guard exit; retaining fixture", err)
				select {}
			}
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".claim") {
					claim, err := os.OpenFile(filepath.Join(claims, entry.Name()), os.O_RDWR, 0)
					if err != nil {
						t.Error("cannot inspect original startup claim; retaining fixture", err)
						select {}
					}
					agentProjectAppJoinClaim(t, claim)
				}
			}
		}
		agentAppRequire(t, os.RemoveAll(directory))
	}()
	var address string
	select {
	case address = <-logs.listening:
	case <-v.done:
		t.Fatal("public App failed before publishing its real listener", v.runErr)
	case <-ctx.Done():
		t.Fatal("public App listener did not arrive")
	}
	agentAppRequire(t, a.p2.base.raw.QueryRow(ctx, `SELECT process_id::text FROM agenteam_object.process_claims WHERE process_id<>$1 AND state='claimed'`, oldProcess).Scan(&v.process))
	v.claim, err = os.OpenFile(filepath.Join(cfg.Objects().SpoolDirectory()+".processes", v.process+".claim"), os.O_RDWR, 0)
	agentAppRequire(t, err)
	v.requireGuardHeld(t)
	h.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.serve(address, w, r) })
	h.server.StartTLS()
	if h.server.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned TLS verification is disabled")
	}
	exercise(v)
	v.requestStop()
	select {
	case <-v.done:
	case <-ctx.Done():
		t.Fatal("original App did not join graceful shutdown")
	}
	agentAppRequire(t, v.runErr)
	if !logs.drained() {
		t.Fatal("original App did not report graceful completion")
	}
	for _, row := range v.calls(t) {
		if row.Process != v.process || row.Phase != "joined" || row.Joined == nil {
			t.Fatal("App returned without settling its original registered calls")
		}
	}
	var active, newStopped, executions, operations, invocations int64
	agentAppRequire(t, a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_object.process_claims WHERE state<>'stopped'),
 (SELECT count(*) FROM agenteam_object.process_claims WHERE process_id=$1 AND state='stopped' AND stopped_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_execution.executions),
 (SELECT count(*) FROM agenteam_tool.operations),
 (SELECT count(*) FROM agenteam_model.invocations)`, v.process).Scan(&active, &newStopped, &executions, &operations, &invocations))
	if active != 0 || newStopped != 1 || executions != 0 || operations != 0 || invocations != 0 {
		t.Fatal("public App omitted actual guard retirement or dispatched unrelated work")
	}
	agentAppRequire(t, syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	agentAppRequire(t, syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
}

// A failed public Run may leave a real producer holding its guard. This test
// cannot settle that private owner itself. Report failure and retain its DB and
// spool until actual release, or until the enclosing test-process timeout exits.
// The blocking flock never releases or closes the App's original guard handle.
func agentProjectAppJoinClaim(t *testing.T, claim *os.File) {
	t.Helper()
	err := syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		t.Error("App returned before its original guard release; retaining fixture", err)
		for err != nil {
			if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
				select {} // Unknown guard state cannot authorize fixture destruction.
			}
			err = syscall.Flock(int(claim.Fd()), syscall.LOCK_EX)
		}
	}
	if err := syscall.Flock(int(claim.Fd()), syscall.LOCK_UN); err != nil {
		t.Error("test claim observation unlock", err)
	}
	if err := claim.Close(); err != nil {
		t.Error("test claim observation close", err)
	}
}

func (v *agentProjectApp) requireGuardHeld(t *testing.T) {
	t.Helper()
	err := syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN)
		t.Fatal("original App released its process guard before its borrower joined")
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatal("original App guard observation failed", err)
	}
}

func agentProjectAppHandoff(t *testing.T, exercise func(*agentProjectApp)) {
	t.Helper()
	entered, completed := false, false
	t.Cleanup(func() {
		if !entered || !completed {
			t.Error("real setup-to-App handoff did not complete")
		}
	})
	a := newAgentCreateFixture(t)
	process, err := a.guard.CurrentProcess()
	agentAppRequire(t, err)
	registration := agentAppRegistration(t, a)
	if a.p2.base.afterOwners != nil {
		t.Fatal("original setup already has an App handoff")
	}
	a.p2.base.afterOwners = func() {
		entered = true
		if !a.p2.base.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
			t.Fatal("setup owners have not actually returned")
		}
		if _, err := a.guard.CurrentProcess(); err == nil {
			t.Fatal("setup still owns its guard")
		}
		var retired bool
		agentAppRequire(t, a.p2.base.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&retired))
		if !retired {
			t.Fatal("setup process has not actually retired")
		}
		withAgentProjectApp(t, a, process.String(), func(v *agentProjectApp) {
			if agentAppRegistration(t, a) != registration || len(v.calls(t)) != 0 {
				t.Fatal("App startup rewrote builtin history or fabricated Agent calls")
			}
			exercise(v)
		})
		completed = true
	}
}

func (v *agentProjectApp) create(t *testing.T) (ac.AgentMutation, []byte, f.IdempotencyKey) {
	t.Helper()
	request, _, _ := v.setup.request(t)
	wire := map[string]json.RawMessage{}
	agentAppRequire(t, json.Unmarshal(jsonBytes(t, request), &wire))
	delete(wire, "add_skills_enabled")
	delete(wire, "install_skill_enabled")
	raw := jsonBytes(t, wire)
	key := f.IdempotencyKey("owned-app-agent-create")
	before := v.calls(t)
	reply := v.http.request(t, v.setup.p2.base.ownerBrowser, http.MethodPost, v.http.createPath, jsonBytes(t, map[string]any{"request": json.RawMessage(raw)}), key, false)
	receipt, err := ac.DecodeAgentMutation(reply.body)
	agentAppRequire(t, err)
	if reply.status != http.StatusOK || receipt.Validate() != nil || receipt.Fields().Agent.Fields().Core.ID != request.Fields().AgentID {
		t.Fatal("actual App create failed to return its original Agent")
	}
	v.callReturned(t, before, "service", "mutation")
	return receipt, raw, key
}

func TestAgentProjectCallApp(t *testing.T) {
	if !t.Run("default-http-shared-call-owner", func(t *testing.T) {
		agentProjectAppHandoff(t, func(v *agentProjectApp) {
			receipt, raw, key := v.create(t)
			target := receipt.Fields().Agent.Fields().Core.ID
			path := v.http.createPath + "/" + target.String()
			owner := v.setup.p2.base.ownerBrowser
			before := v.calls(t)
			get := v.http.request(t, owner, http.MethodGet, path+"/configuration", nil, "", false)
			if get.status != 200 || !bytes.Equal(get.body, jsonBytes(t, receipt.Fields().Agent)) {
				t.Fatal("actual App configuration Get changed its complete original value")
			}
			v.callReturned(t, before, "service", "read")
			before = v.calls(t)
			lookup := v.http.request(t, owner, http.MethodPost, variableHTTPPath(v.setup.p2.project.ID, "/agent-commands/lookup"), jsonBytes(t, map[string]any{"command": ac.CreateAgentCommand, "request": json.RawMessage(raw)}), key, false)
			found, err := ac.DecodeAgentCommandLookup(lookup.body)
			agentAppRequire(t, err)
			if lookup.status != 200 || found.Status() != ac.AgentLookupCommitted || found.Receipt() == nil || !bytes.Equal(jsonBytes(t, *found.Receipt()), jsonBytes(t, receipt)) {
				t.Fatal("actual App Lookup changed the original command receipt")
			}
			v.callReturned(t, before, "service", "read")
			before = v.calls(t)
			mounts := v.http.request(t, owner, http.MethodGet, path+"/runner-mounts", nil, "", false)
			var bound ac.AgentRunnerMounts
			agentAppRequire(t, json.Unmarshal(mounts.body, &bound))
			if mounts.status != 200 || bound.Validate() != nil || bound.ProjectID != v.setup.p2.project.ID || bound.AgentID != target {
				t.Fatal("actual Runner Mount handler lost its fixed Service")
			}
			v.callReturned(t, before, "service", "read")
			before = v.calls(t)
			directory := v.http.request(t, owner, http.MethodGet, path, nil, "", false)
			var entry ac.DirectoryEntry
			var safe map[string]json.RawMessage
			agentAppRequire(t, json.Unmarshal(directory.body, &entry))
			agentAppRequire(t, json.Unmarshal(directory.body, &safe))
			if directory.status != 200 || entry.Validate() != nil || entry.ID != target || len(safe) != 9 {
				t.Fatal("actual Directory Get lost its safe original projection")
			}
			v.callReturned(t, before, "directory", "read")
			before = v.calls(t)
			page := v.http.request(t, owner, http.MethodGet, v.http.createPath, nil, "", false)
			var entries f.Page[ac.DirectoryEntry]
			agentAppRequire(t, json.Unmarshal(page.body, &entries))
			if page.status != 200 || len(entries.Items) != 1 || entries.Items[0].ID != target {
				t.Fatal("actual Directory List route changed")
			}
			v.callReturned(t, before, "directory", "read")
			before = v.calls(t)
			for _, denied := range []string{path + "/configuration", path} {
				agentAppProblem(t, v.http.request(t, v.setup.p2.base.otherBrowser, http.MethodGet, denied, nil, "", false), http.StatusNotFound, f.NotFound)
			}
			if len(before) != 6 || !reflect.DeepEqual(v.calls(t), before) {
				t.Fatal("non-Owner requests registered calls or changed accepted ownership")
			}
			t.Log("agent_project_call_app public_run=true same_pair_service_directory=true service_mutation=1 service_reads=3 directory_reads=2 distinct_joined_calls=6 denied_registration=0")
		})
	}) {
		return
	}
	t.Run("directory-return-checkpoint-drains-before-guard", func(t *testing.T) {
		agentProjectAppHandoff(t, func(v *agentProjectApp) { testAgentProjectAppDirectoryTail(t, v) })
	})
}

type agentProjectAppWaiter struct {
	PID     int32     `json:"pid"`
	Started time.Time `json:"started"`
}

// Both observations use the exact Project advisory tuple, task-owned blocker,
// database, production application name and original lock statement. A reused
// connection is distinguished by the actual transaction start, not by PID alone.
func (v *agentProjectApp) waiter(ctx context.Context, key f.LockKey, holder int32, previous *agentProjectAppWaiter) (agentProjectAppWaiter, error) {
	bits := uint64(key.AdvisoryKey())
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var raw []byte
		err := v.setup.p2.base.raw.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('pid',a.pid,'started',a.xact_start)),'[]'::jsonb)
 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid
 JOIN pg_stat_activity a ON a.pid=w.pid
 WHERE a.datname=current_database() AND a.application_name='agenteam' AND a.state='active' AND a.query='SELECT pg_advisory_xact_lock($1)'
 AND w.locktype='advisory' AND w.classid::bigint=$1 AND w.objid::bigint=$2 AND w.objsubid=1 AND NOT w.granted AND w.mode='ExclusiveLock'
 AND h.pid=$3 AND h.granted AND h.mode='ShareLock' AND $3::int=ANY(pg_blocking_pids(w.pid))`, int64(bits>>32), int64(bits&0xffffffff), holder).Scan(&raw)
		if err != nil {
			return agentProjectAppWaiter{}, err
		}
		var rows []agentProjectAppWaiter
		if err := json.Unmarshal(raw, &rows); err != nil || len(rows) > 1 {
			return agentProjectAppWaiter{}, errors.New("original App checkpoint waiter was not unique")
		}
		if len(rows) == 1 && rows[0].PID > 0 && !rows[0].Started.IsZero() && (previous == nil || rows[0] != *previous) {
			return rows[0], nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return agentProjectAppWaiter{}, ctx.Err()
		case <-v.done:
			return agentProjectAppWaiter{}, errors.New("App returned before its original Directory checkpoint")
		}
	}
}

func (v *agentProjectApp) cancelCheckpoint(t *testing.T, key f.LockKey, holder int32, waiter agentProjectAppWaiter) {
	t.Helper()
	bits := uint64(key.AdvisoryKey())
	var cancelled bool
	// CASE preserves the exact-identity guard before this single cancellation.
	// No backend termination, connection replacement or synthetic CommitResult.
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT CASE WHEN EXISTS(
 SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid
 JOIN pg_stat_activity a ON a.pid=w.pid
 WHERE a.datname=current_database() AND a.application_name='agenteam' AND a.state='active' AND a.query='SELECT pg_advisory_xact_lock($1)'
 AND a.pid=$4 AND a.xact_start=$5 AND w.locktype='advisory' AND w.classid::bigint=$1 AND w.objid::bigint=$2 AND w.objsubid=1 AND NOT w.granted AND w.mode='ExclusiveLock'
 AND h.pid=$3 AND h.granted AND h.mode='ShareLock' AND $3::int=ANY(pg_blocking_pids(w.pid)))
 THEN pg_cancel_backend($4) ELSE false END`, int64(bits>>32), int64(bits&0xffffffff), holder, waiter.PID, waiter.Started).Scan(&cancelled))
	if !cancelled {
		t.Fatal("exact original checkpoint statement was not cancelled; no retry")
	}
}

// This asynchronous GET preserves the original verified TLS bridge and waits
// for its real upstream body/handler. It returns errors to the test goroutine.
func agentProjectAppGet(ctx context.Context, h *agentAppHTTP, owner variableHTTPBrowser, path string) (variableHTTPResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		return variableHTTPResponse{}, err
	}
	request.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: owner.cookie, Secure: true, HttpOnly: true, Path: "/"})
	request.Header.Set("Origin", h.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	client := *h.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return variableHTTPResponse{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 || response.ContentLength != int64(len(raw)) || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
		return variableHTTPResponse{}, errors.New("original Directory HTTP body did not actually finish")
	}
	select {
	case observed := <-h.done:
		if observed.err != nil || observed.cut {
			return variableHTTPResponse{}, errors.New("original Directory bridge did not return normally")
		}
	case <-ctx.Done():
		return variableHTTPResponse{}, ctx.Err()
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Request-ID") == "" {
		return variableHTTPResponse{}, errors.New("original Directory HTTP metadata missing")
	}
	return variableHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: raw}, nil
}

func testAgentProjectAppDirectoryTail(t *testing.T, v *agentProjectApp) {
	t.Helper()
	receipt, _, _ := v.create(t)
	target := receipt.Fields().Agent.Fields().Core.ID
	prior := v.calls(t)
	conn, err := v.setup.p2.base.db.Fixture.Connect(v.ctx, v.setup.p2.base.db.Name)
	agentAppRequire(t, err)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		agentAppRequire(t, conn.Close(ctx))
	}()
	tx, err := conn.Begin(v.ctx)
	agentAppRequire(t, err)
	var releaseOnce sync.Once
	var releaseErr error
	release := func() {
		releaseOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			releaseErr = tx.Rollback(ctx)
		})
	}
	defer func() { release(); agentAppRequire(t, releaseErr) }()
	key, err := f.ProjectLock(v.setup.p2.project.ID.String())
	agentAppRequire(t, err)
	_, err = tx.Exec(v.ctx, `SELECT pg_advisory_xact_lock_shared($1)`, key.AdvisoryKey())
	agentAppRequire(t, err)
	holder := int32(conn.PgConn().PID())
	requestCtx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
	done := make(chan struct{})
	var response variableHTTPResponse
	var responseErr error
	go func() {
		defer close(done)
		response, responseErr = agentProjectAppGet(requestCtx, v.http, v.setup.p2.base.ownerBrowser, v.http.createPath+"/"+target.String())
	}()
	defer func() {
		release()
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("original Directory HTTP call did not return; retaining dependencies")
			<-done
		}
	}()
	ready, finishReady := context.WithTimeout(v.ctx, time.Second)
	first, err := v.waiter(ready, key, holder, nil)
	finishReady()
	agentAppRequire(t, err)
	running := v.calls(t)
	if len(running) != 2 {
		t.Fatal("real Directory read did not register exactly its original call")
	}
	var original agentProjectAppCall
	for _, row := range running {
		if row.ID != prior[0].ID {
			original = row
		}
	}
	if original.Phase != "running" || original.Joined != nil || original.Lane != "directory" || original.Class != "read" || original.Process != v.process || original.Instance != prior[0].Instance {
		t.Fatal("blocked checkpoint lacked its real shared-pair Directory owner")
	}
	select {
	case <-done:
		t.Fatal("Directory HTTP returned before original checkpoint statement")
	default:
	}
	v.requireGuardHeld(t)
	// The unchanged HTTP read budget is 2s, shorter than finish's 3s. Cancel
	// only this proven owned lock statement, once, so its real rollback returns
	// inside HTTP's original budget. This is not COMMIT Unknown or a retry.
	v.cancelCheckpoint(t, key, holder, first)
	select {
	case <-done:
	case <-requestCtx.Done():
		t.Fatal("original Directory HTTP did not actually return")
	}
	agentAppRequire(t, responseErr)
	var entry ac.DirectoryEntry
	agentAppRequire(t, json.Unmarshal(response.body, &entry))
	if response.status != 200 || entry.Validate() != nil || entry.ID != target || !reflect.DeepEqual(v.calls(t), running) {
		t.Fatal("checkpoint failure rewrote known HTTP result or lost pending ownership")
	}
	var firstActive bool
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database() AND xact_start=$2)`, first.PID, first.Started).Scan(&firstActive))
	if firstActive {
		t.Fatal("HTTP returned before the original checkpoint transaction ended")
	}
	v.requestStop()
	drainReady, finishDrainReady := context.WithTimeout(v.ctx, 3*time.Second)
	second, err := v.waiter(drainReady, key, holder, &first)
	finishDrainReady()
	agentAppRequire(t, err)
	select {
	case <-v.done:
		t.Fatal("App returned while the original Directory checkpoint still borrowed its providers")
	default:
	}
	v.requireGuardHeld(t)
	if !reflect.DeepEqual(v.calls(t), running) {
		t.Fatal("App Drain replaced the original retained Directory CallID")
	}
	var live bool
	agentAppRequire(t, v.setup.p2.base.raw.QueryRow(v.ctx, `SELECT state='claimed' AND stopped_at IS NULL FROM agenteam_object.process_claims WHERE process_id=$1`, v.process).Scan(&live))
	if !live {
		t.Fatal("App retired its guard fact before the original checkpoint returned")
	}
	release()
	agentAppRequire(t, releaseErr)
	select {
	case <-v.done:
	case <-v.ctx.Done():
		t.Fatal("App did not return after original holder Rollback")
	}
	agentAppRequire(t, v.runErr)
	settled := v.callReturned(t, prior, "directory", "read")
	if settled.ID != original.ID || settled.Registered != original.Registered || settled.Process != original.Process || settled.Instance != original.Instance {
		t.Fatal("App Drain recreated rather than settled its exact original call")
	}
	t.Logf("agent_project_call_app original_http_returned=true directory_tail_retained=true original_call_settled=true guard_held_until_checkpoint=true holder_backend=%d original_checkpoint_backend=%d drain_checkpoint_backend=%d", holder, first.PID, second.PID)
}
