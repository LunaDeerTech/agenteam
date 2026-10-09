//go:build integration

package process_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func independentProcessAwait(t *testing.T, ch <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("independent stage did not complete", stage)
	}
}
func independentProcessEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			out = append(out, entry)
		}
	}
	return append(out, key+"="+value)
}
func independentProxyRoot(t *testing.T) (*modelSystemBinary, *commitproxy.Proxy) {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	proxy, e := commitproxy.New(databaseContext(t), net.JoinHostPort("127.0.0.1", db.Fixture.Port))
	if e != nil {
		t.Fatal("owned proxy start failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := proxy.Close(ctx); e != nil {
			t.Error("independent proxy actual join failed")
		}
	})
	u, e := url.Parse(db.Fixture.URL(db.Name))
	if e != nil {
		t.Fatal("owned database URL malformed")
	}
	u.Host = proxy.Address()
	env := databaseEnvironment(t, db, "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080")
	env = independentProcessEnv(env, "AGENTEAM_CENTRAL_DATABASE_URL", u.String())
	env = independentProcessEnv(env, "AGENTEAM_CENTRAL_DATABASE_TLS_MODE", "disable")
	env = slices.DeleteFunc(env, func(entry string) bool { return strings.HasPrefix(entry, "AGENTEAM_CENTRAL_DATABASE_CA_FILE=") })
	env = independentProcessEnv(env, "AGENTEAM_CENTRAL_DATABASE_LOCK_TIMEOUT", "5s")
	v := &modelSystemBinary{db: db, env: env, client: &http.Client{Timeout: 8 * time.Second}, cookies: map[string]*http.Cookie{}}
	for _, entry := range env {
		if strings.HasPrefix(entry, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=") {
			v.logPath = strings.TrimPrefix(entry, "AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=")
		}
	}
	v.start(t)
	t.Cleanup(v.client.CloseIdleConnections)
	t.Cleanup(func() { clear(v.cookies); v.csrf = ""; clear(v.logSecrets) })
	v.login(t)
	return v, proxy
}

func independentAdvisoryWaiter(t *testing.T, conn *pgx.Conn, key int64, blocker int32) int32 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n := uint64(key)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var pids []int32
		e := conn.QueryRow(ctx, `SELECT COALESCE(array_agg(l.pid),ARRAY[]::integer[]) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=current_database() AND a.xact_start IS NOT NULL AND l.locktype='advisory' AND l.classid::bigint=$1 AND l.objid::bigint=$2 AND l.objsubid=1 AND NOT l.granted AND l.mode='ExclusiveLock' AND $3::integer=ANY(pg_blocking_pids(l.pid))`, int64(n>>32), int64(n&0xffffffff), blocker).Scan(&pids)
		if e != nil {
			t.Fatal("independent lock observation failed")
		}
		if len(pids) == 1 && pids[0] > 0 && pids[0] != blocker {
			return pids[0]
		}
		if len(pids) > 1 {
			t.Fatal("independent writer/confirmation is ambiguous")
		}
		select {
		case <-ctx.Done():
			t.Fatal("exact independent PID/key/blocker not observed")
		case <-tick.C:
		}
	}
}

func TestIndependentProjectVariablesProcessConfirmationExit(t *testing.T) {
	v, proxy := independentProxyRoot(t)
	project := workRootProject(t, v) // disclosed fixture Skills, real Project command
	path := "/api/v1/projects/" + project.ID.String() + "/variables"
	key, target := modelSystemKey(t), modelSystemKey(t)
	privateValue := "independent-process-original-value-canary"
	input := map[string]any{"variable_id": target, "name": "INDEPENDENT_PROCESS", "description": "", "value": privateValue}
	v.logSecrets = append(v.logSecrets, key, privateValue)
	identity, e := vc.VariableCommandIdentity(project.ID, vc.CreateCommand, f.IdempotencyKey(key))
	if e != nil {
		t.Fatal(e)
	}
	commandLock, e := f.CommandLock(identity)
	if e != nil {
		t.Fatal(e)
	}
	// The trigger key is an independent advisory lock, never a sleep or a
	// replacement CommitResult. It exposes this one exact writer's real PID.
	barrierKey, e := f.RecordLock(f.ReferenceRecordLock, "independent-variable-trigger:"+target)
	if e != nil {
		t.Fatal(e)
	}
	admin := v.db.Connect(t)
	holder := v.db.Connect(t)
	var holderPID int32
	if e = holder.QueryRow(databaseContext(t), `SELECT pg_backend_pid()`).Scan(&holderPID); e != nil {
		t.Fatal("holder PID unavailable")
	}
	if _, e = holder.Exec(databaseContext(t), `SELECT pg_advisory_lock($1)`, barrierKey.AdvisoryKey()); e != nil {
		t.Fatal("owned trigger lock unavailable")
	}
	defer func() {
		_, _ = holder.Exec(databaseContext(t), `SELECT pg_advisory_unlock($1)`, barrierKey.AdvisoryKey())
	}()
	// All interpolation is task-generated UUID/integer. No application body,
	// token, cookie or credential is placed in this trigger or an error log.
	sql := fmt.Sprintf(`CREATE SCHEMA independent_variable_boundary; CREATE FUNCTION independent_variable_boundary.hold_complete() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.idempotency_key='%s' AND NEW.state='completed' THEN PERFORM pg_advisory_xact_lock(%d::bigint); END IF; RETURN NEW; END$$; CREATE TRIGGER independent_hold_complete AFTER INSERT OR UPDATE ON agenteam_projectvariable.commands FOR EACH ROW EXECUTE FUNCTION independent_variable_boundary.hold_complete()`, key, barrierKey.AdvisoryKey())
	if _, e = admin.Exec(databaseContext(t), sql); e != nil {
		t.Fatal("owned command boundary installation failed")
	}
	body, e := json.Marshal(map[string]any{"request": input})
	if e != nil {
		t.Fatal(e)
	}
	r, e := http.NewRequest("POST", v.address+path, bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	r.Host = "localhost:8080"
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", v.csrf)
	r.Header.Set("Idempotency-Key", key)
	for _, cookie := range v.cookies {
		r.AddCookie(cookie)
	}
	type reply struct {
		status int
		code   f.Code
		state  f.CommitState
		err    bool
	}
	result := make(chan reply, 1)
	httpJoined := make(chan struct{})
	go func() {
		defer close(httpJoined)
		response, e := v.client.Do(r)
		if e != nil {
			result <- reply{err: true}
			return
		}
		raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
		closeErr := response.Body.Close()
		var p httpapi.Problem
		_ = json.Unmarshal(raw, &p)
		clear(raw)
		result <- reply{response.StatusCode, p.Code, p.CommitState, e != nil || closeErr != nil}
	}()
	t.Cleanup(func() {
		v.client.CloseIdleConnections()
		independentProcessAwait(t, httpJoined, "original HTTP callback")
	})
	writerPID := independentAdvisoryWaiter(t, admin, barrierKey.AdvisoryKey(), holderPID)
	if e = proxy.Arm(writerPID); e != nil {
		t.Fatal("proxy target not exclusive")
	}
	if _, e = holder.Exec(databaseContext(t), `SELECT pg_advisory_unlock($1)`, barrierKey.AdvisoryKey()); e != nil {
		t.Fatal("writer barrier did not release")
	}
	independentProcessAwait(t, proxy.Reached(), "complete original COMMIT frame")
	if proxy.WriterPID() != writerPID {
		t.Fatal("COMMIT bound another writer")
	}
	confirmationPID := independentAdvisoryWaiter(t, admin, commandLock.AdvisoryKey(), writerPID)
	if confirmationPID == writerPID {
		t.Fatal("confirmation reused original transaction")
	}
	var before int
	if e = admin.QueryRow(databaseContext(t), `SELECT count(*) FROM agenteam_projectvariable.variables WHERE id=$1`, target).Scan(&before); e != nil || before != 0 {
		t.Fatal("uncommitted result escaped", e)
	}
	if e = v.p.command.Process.Signal(syscall.SIGTERM); e != nil {
		t.Fatal("root signal failed")
	}
	v.p.wait(t, 0) // actual executable Wait; no inferred process absence
	independentProcessAwait(t, httpJoined, "cancelled confirmation HTTP return")
	got := <-result
	if !got.err && (got.status != 503 || got.code != f.CommitUnknown || got.state != f.Unknown) {
		t.Fatal("root shutdown converted uncertain write to known result", got.status, got.code)
	}
	for _, value := range v.logSecrets {
		if strings.Contains(v.p.stderr.String()+v.p.stdout.String(), value) {
			t.Fatal("root log exposed private material")
		}
	}
	// The independent proxy still owns the original upstream writer. Its later
	// commit is deliberately separate from root's already-returned local calls.
	proxy.Release()
	independentProcessAwait(t, proxy.Committed(), "backend COMMIT+ReadyForQuery")
	independentProcessAwait(t, proxy.HeldJoined(), "original proxy connection actual return")
	noCentralBackends(t, v.db)
	assertDatabaseLogsSafe(t, v.p, v.db)
	var commands, history, audits, events, version int64
	if e = admin.QueryRow(databaseContext(t), `SELECT (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1 AND idempotency_key=$2 AND state='completed'),(SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='projectvariable'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='projectvariable'),(SELECT version FROM agenteam_projectvariable.variables WHERE id=$3)`, project.ID.String(), key, target).Scan(&commands, &history, &audits, &events, &version); e != nil || commands != 1 || history != 1 || audits != 1 || events != 1 || version != 1 {
		t.Fatal("late commit durable facts differ", e)
	}
	v.client.CloseIdleConnections()
	clear(v.cookies)
	v.csrf = ""
	v.start(t)
	v.login(t)
	lookup := map[string]any{"command": vc.CreateCommand, "request": input}
	response := v.request(t, "POST", path+"/commands/lookup", key, lookup, nil).want(t, 200)
	var receipt vc.VariableCommandLookup
	if json.Unmarshal(response.body, &receipt) != nil || receipt.Status() != vc.LookupCommitted || receipt.Receipt() == nil || receipt.Receipt().Fields().Variable.Fields().Value != privateValue {
		t.Fatal("restarted root lost original intent")
	}
	saved, e := json.Marshal(receipt.Receipt())
	if e != nil {
		t.Fatal(e)
	}
	replayed := v.request(t, "POST", path, key, map[string]any{"request": input}, nil).want(t, 200)
	if !bytes.Equal(saved, replayed.body) {
		t.Fatal("restarted explicit replay changed receipt")
	}
	var afterCommands, afterHistory, afterEvents int64
	if e = admin.QueryRow(databaseContext(t), `SELECT (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1),(SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='projectvariable')`, project.ID.String()).Scan(&afterCommands, &afterHistory, &afterEvents); e != nil || afterCommands != 1 || afterHistory != 1 || afterEvents != 1 {
		t.Fatal("restart/lookup replayed mutation", e)
	}
	v.stop(t, syscall.SIGTERM)
}
