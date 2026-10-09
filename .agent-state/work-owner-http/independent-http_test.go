//go:build integration

package work_test

// Independent HTTP recovery and current-authorization probes. Account cookies
// come from the real service fixture. These tests do not claim Account login
// HTTP, natural deadline coverage, or the default application root.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type independentHTTPResult struct {
	status int
	header http.Header
	body   []byte
	err    error
}

// A real upstream response is consumed through EOF before the downstream
// connection is closed without a response. No domain result/CommitResult is
// replaced. The discarded receipt never supplies this probe's expectations.
type independentHTTPCut struct {
	complete      bool
	status, bytes int
	err           error
}

type independentHTTPWire struct {
	origin, cut *httptest.Server
	client      *http.Client
	cuts        <-chan independentHTTPCut
	forwarded   *atomic.Int32
}

func independentHTTPServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	var active sync.WaitGroup
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		defer active.Done()
		h.ServeHTTP(w, r)
	}))
	s.Config.ReadHeaderTimeout = 5 * time.Second
	s.Config.ReadTimeout = 35 * time.Second
	s.Config.WriteTimeout = 35 * time.Second
	s.Config.IdleTimeout = time.Second
	s.Start()
	t.Cleanup(func() {
		// Server.Close joins its real connections. The additional wait owns a
		// proxy handler even after it has explicitly hijacked/closed its socket.
		s.Close()
		active.Wait()
	})
	return s
}

func independentHTTPRead(resp *http.Response) ([]byte, error) {
	const cap = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, cap+1))
	var extra [1]byte
	n, eof := resp.Body.Read(extra[:])
	closed := resp.Body.Close()
	if err != nil || len(raw) > cap || n != 0 || eof != io.EOF || closed != nil || resp.ContentLength != int64(len(raw)) {
		return nil, errors.New("independent response lacked bounded complete EOF")
	}
	return raw, nil
}

func independentHTTPNetwork(t *testing.T, handler http.Handler) independentHTTPWire {
	t.Helper()
	origin := independentHTTPServer(t, handler)
	upstream, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal("owned origin URL")
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 40 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	facts := make(chan independentHTTPCut, 8)
	forwarded := new(atomic.Int32)
	cut := independentHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var fact independentHTTPCut
		defer func() { facts <- fact }()
		request := r.Clone(r.Context())
		copyURL := *request.URL
		copyURL.Scheme, copyURL.Host = upstream.Scheme, upstream.Host
		request.URL, request.RequestURI, request.GetBody = &copyURL, "", nil
		forwarded.Add(1)
		response, err := transport.RoundTrip(request)
		if err != nil {
			fact.err = errors.New("owned upstream transport failed")
			return
		}
		body, err := independentHTTPRead(response)
		fact.status, fact.bytes, fact.err = response.StatusCode, len(body), err
		fact.complete = err == nil && response.StatusCode == http.StatusOK && json.Valid(body)
		clear(body)
		if !fact.complete {
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			fact.err = errors.New("owned downstream hijack failed")
			return
		}
		if err = conn.Close(); err != nil {
			fact.err = errors.New("owned downstream close failed")
		}
	}))
	return independentHTTPWire{origin, cut, client, facts, forwarded}
}

func (n independentHTTPWire) call(ctx context.Context, b workOwnerHTTPBrowser, cut bool, method, path, body string, key f.IdempotencyKey) independentHTTPResult {
	r := workOwnerHTTPRequest(ctx, b, method, path, body, key)
	destination := n.origin.URL
	if cut {
		destination = n.cut.URL
	}
	u, err := url.Parse(destination)
	if err != nil {
		return independentHTTPResult{err: err}
	}
	r.URL.Scheme, r.URL.Host, r.RequestURI, r.GetBody = u.Scheme, u.Host, "", nil
	// Host/Origin/cookie/CSRF remain the real fixture's canonical authority.
	response, err := n.client.Do(r)
	if err != nil {
		return independentHTTPResult{err: err}
	}
	raw, err := independentHTTPRead(response)
	return independentHTTPResult{response.StatusCode, response.Header.Clone(), raw, err}
}

func independentHTTPJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("independent request encoding")
	}
	return string(raw)
}

func independentHTTPObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil || out == nil {
		t.Fatal("invalid independent JSON object")
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		t.Fatal("trailing independent JSON")
	}
	return out
}

func independentHTTPSuccess(t *testing.T, response independentHTTPResult) map[string]any {
	t.Helper()
	if response.err != nil || response.status != http.StatusOK || response.header.Get("Content-Type") != "application/json" || response.header.Get("Cache-Control") != "no-store" || response.header.Get("X-Request-ID") == "" {
		t.Fatal("independent complete HTTP success missing", response.status)
	}
	return independentHTTPObject(t, response.body)
}

func independentHTTPProblem(t *testing.T, response independentHTTPResult, status int, code f.Code) {
	t.Helper()
	if response.err != nil || response.status != status {
		t.Fatal("independent HTTP denial status", response.status)
	}
	v := independentHTTPObject(t, response.body)
	if v["code"] != string(code) || v["request_id"] != response.header.Get("X-Request-ID") {
		t.Fatal("independent HTTP denial identity")
	}
}

// A direct durable read is independent of HTTP projection and of the author's
// assertion helpers. Request fields and expected versions are checked below;
// exact receipt equality additionally checks historical IDs and timestamps.
func independentHTTPReceipt(t *testing.T, v *workOwnerHTTPFixture, table, command string, key f.IdempotencyKey) map[string]any {
	t.Helper()
	if table != "structure_commands" && table != "task_commands" && table != "task_blocker_commands" {
		t.Fatal("independent fixed table")
	}
	var commandID, writer, state, eventID, receipt string
	err := v.raw.QueryRow(ctxFor(t), `SELECT id::text,actor_user_id::text,state,event_id::text,receipt::text FROM agenteam_work.`+table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, v.project.ID.String(), command, string(key)).Scan(&commandID, &writer, &state, &eventID, &receipt)
	if err != nil || state != "completed" || writer != v.ownerBrowser.actor.Details().UserID {
		t.Fatal("lost HTTP command was not durably completed")
	}
	var events int
	err = v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1 AND project_id=$2 AND producer='work'`, eventID, v.project.ID.String()).Scan(&events)
	if err != nil || events != 1 {
		t.Fatal("lost HTTP command lacked its real Outbox fact")
	}
	if table != "structure_commands" {
		column := "operation_id"
		if table == "task_blocker_commands" {
			column = "blocker_operation_id"
		}
		var history int
		err = v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_events WHERE `+column+`=$1 AND project_id=$2`, commandID, v.project.ID.String()).Scan(&history)
		if err != nil || history != 1 {
			t.Fatal("lost HTTP command lacked its real history fact")
		}
	}
	return independentHTTPObject(t, []byte(receipt))
}

func independentHTTPState(t *testing.T, v *workOwnerHTTPFixture) string {
	t.Helper()
	var result string
	err := v.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
	'milestones',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_work.milestones x WHERE project_id=$1),
	'sprints',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_work.sprints x WHERE project_id=$1),
	'tasks',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_work.tasks x WHERE project_id=$1),
	'blockers',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_work.task_blockers x WHERE project_id=$1),
	'structure_commands',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_work.structure_commands x WHERE project_id=$1),
	'task_commands',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_work.task_commands x WHERE project_id=$1),
	'blocker_commands',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_work.task_blocker_commands x WHERE project_id=$1),
	'history',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_work.task_events x WHERE project_id=$1),
	'outbox',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_outbox.events x WHERE project_id=$1)
	)::text`, v.project.ID.String()).Scan(&result)
	if err != nil {
		t.Fatal("independent durable Work state read")
	}
	return result
}

func TestIndependentWorkOwnerHTTPRecoveryAndAuthority(t *testing.T) {
	v := newWorkOwnerHTTPFixture(t)
	network := independentHTTPNetwork(t, v.handler)
	owner := v.ownerBrowser
	milestone := v.milestone(t, owner.actor, v.project.ID, "independent seed milestone")
	sprint := v.sprint(t, owner.actor, v.project.ID, milestone.ID, "independent seed sprint")
	task := v.task(t, owner.actor, v.project.ID, sprint.ID, "independent seed task")
	base := workOwnerHTTPPath(v.project.ID, "")
	const canary = "INDEPENDENT_HTTP_ORIGINAL_CANARY"
	const changed = "INDEPENDENT_HTTP_CURRENT_CANARY"

	t.Run("three-original-intents-survive-complete-response-loss", func(t *testing.T) {
		blocker := id[wc.TaskBlockerIdentity](t)
		for _, tc := range []struct {
			name, table, command, path, lookup, target, field, version string
			request                                                    map[string]any
		}{
			{"structure", "structure_commands", "work.milestone.update", "/milestones/" + milestone.ID.String(), "/structure-commands/lookup", milestone.ID.String(), "milestone", "1", map[string]any{"title": canary}},
			{"task", "task_commands", "work.task.update", "/tasks/" + task.ID.String(), "/task-commands/lookup", task.ID.String(), "task", "1", map[string]any{"title": canary}},
			{"blocker", "task_blocker_commands", "work.task.blocker.add", "/tasks/" + task.ID.String() + "/blockers", "/tasks/" + task.ID.String() + "/blocker-commands/lookup", task.ID.String(), "blocker", "3", map[string]any{"blocker_id": blocker.String(), "type": "waiting_for_human", "description": canary, "metadata": map[string]any{}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				key := f.IdempotencyKey("independent-http-" + tc.name)
				original := independentHTTPJSON(t, map[string]any{"expected_version": tc.version, "request": tc.request})
				lookup := map[string]any{"command": tc.command, "expected_version": tc.version, "request": tc.request}
				if tc.name != "blocker" {
					lookup["target_id"] = tc.target
				}
				lookupBody := independentHTTPJSON(t, lookup)
				method := http.MethodPatch
				if tc.name == "blocker" {
					method = http.MethodPost
				}
				before := network.forwarded.Load()
				lost := network.call(ctxFor(t), owner, true, method, base+tc.path, original, key)
				if !errors.Is(lost.err, io.EOF) || lost.status != 0 || len(lost.body) != 0 {
					t.Fatal("first response was not actually cut before downstream headers")
				}
				select {
				case fact := <-network.cuts:
					if fact.err != nil || !fact.complete || fact.status != 200 || fact.bytes == 0 || network.forwarded.Load() != before+1 {
						t.Fatal("cut did not follow exactly one real full upstream response")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("owned cut handler did not return its observation")
				}
				receipt := independentHTTPReceipt(t, v, tc.table, tc.command, key)
				object, ok := receipt[tc.field].(map[string]any)
				if !ok || object["project_id"] != v.project.ID.String() {
					t.Fatal("durable receipt project")
				}
				if tc.name == "blocker" {
					if object["id"] != blocker.String() || object["task_id"] != tc.target || object["description"] != canary || object["resolved_at"] != nil {
						t.Fatal("durable original blocker intent")
					}
					taskImage, ok := receipt["task"].(map[string]any)
					if !ok || taskImage["id"] != tc.target || taskImage["version"] != "4" || taskImage["title"] != changed {
						t.Fatal("durable blocker Task postimage")
					}
				} else if object["id"] != tc.target || object["title"] != canary || object["version"] != "2" {
					t.Fatal("durable original target/version/intent")
				}
				if tc.name == "blocker" {
					body := independentHTTPJSON(t, map[string]any{"expected_version": "4", "request": map[string]any{"blocker_id": blocker.String(), "resolution_comment": changed}})
					independentHTTPSuccess(t, network.call(ctxFor(t), owner, false, "POST", base+tc.path+"/resolve", body, key+"-advance"))
					var resolved bool
					if v.raw.QueryRow(ctxFor(t), `SELECT resolved_at IS NOT NULL FROM agenteam_work.task_blockers WHERE id=$1`, blocker.String()).Scan(&resolved) != nil || !resolved {
						t.Fatal("current blocker did not really change")
					}
				} else {
					body := independentHTTPJSON(t, map[string]any{"expected_version": "2", "request": map[string]any{"title": changed}})
					advanced := independentHTTPSuccess(t, network.call(ctxFor(t), owner, false, "PATCH", base+tc.path, body, key+"-advance"))
					current := advanced[tc.field].(map[string]any)
					if current["title"] != changed || current["version"] != "3" {
						t.Fatal("current resource did not advance independently")
					}
					table := "milestones"
					if tc.name == "task" {
						table = "tasks"
					}
					var title string
					var version int64
					if v.raw.QueryRow(ctxFor(t), `SELECT title,version FROM agenteam_work.`+table+` WHERE project_id=$1 AND id=$2`, v.project.ID.String(), tc.target).Scan(&title, &version) != nil || title != changed || version != 3 {
						t.Fatal("current content change was not persisted")
					}
				}
				fresh := v.login(t, owner.email)
				if fresh.actor.Details().UserID != owner.actor.Details().UserID || fresh.actor.Details().SessionID == owner.actor.Details().SessionID {
					t.Fatal("formal post-loss new Session identity")
				}
				state := independentHTTPState(t, v)
				found := independentHTTPSuccess(t, network.call(ctxFor(t), fresh, false, "POST", base+tc.lookup, lookupBody, key))
				statusField, receiptField := "status", "receipt"
				if tc.name == "structure" {
					statusField, receiptField = "state", "result"
				}
				if found[statusField] != "committed" || !reflect.DeepEqual(found[receiptField], receipt) {
					t.Fatal("Lookup reconstructed current state instead of exact original receipt")
				}
				replay := independentHTTPSuccess(t, network.call(ctxFor(t), fresh, false, method, base+tc.path, original, key))
				if !reflect.DeepEqual(replay, receipt) {
					t.Fatal("same-intent replay replaced historical result")
				}
				changedIntent := make(map[string]any, len(tc.request))
				for k, val := range tc.request {
					changedIntent[k] = val
				}
				if tc.name == "blocker" {
					changedIntent["description"] = changed
				} else {
					changedIntent["title"] = changed
				}
				lookup["request"] = changedIntent
				independentHTTPProblem(t, network.call(ctxFor(t), fresh, false, "POST", base+tc.lookup, independentHTTPJSON(t, lookup), key), 409, f.IdempotencyKeyReused)
				independentHTTPProblem(t, network.call(ctxFor(t), v.adminBrowser, false, "POST", base+tc.lookup, lookupBody, key), 404, f.NotFound)
				if after := independentHTTPState(t, v); after != state {
					t.Fatal("Lookup/replay/denial wrote a second Work fact")
				}
			})
		}
		if strings.Contains(v.logs.text(), canary) || strings.Contains(v.logs.text(), changed) {
			t.Fatal("original intent escaped through HTTP logs")
		}
	})

	t.Run("formal-logout-after-http-authentication-before-reader-locks", func(t *testing.T) {
		current := v.login(t, owner.email)
		lock, err := f.AggregateLock(f.TaskAggregate, task.ID.String())
		if err != nil {
			t.Fatal("target lock identity")
		}
		entered, release := make(chan int32, 1), make(chan struct{})
		var once sync.Once
		unhold := func() { once.Do(func() { close(release) }) }
		var selected atomic.Bool
		v.tracked.mu.Lock()
		v.tracked.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
			for _, request := range locks {
				if request.Mode != f.Shared || f.CompareLockKeys(request.Key, lock) != 0 || !selected.CompareAndSwap(false, true) {
					continue
				}
				x, e := v.raw.InTx(tx)
				if e != nil {
					return e
				}
				var pid int32
				if e = x.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); e != nil {
					return e
				}
				entered <- pid
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
		v.tracked.mu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		result, done := make(chan independentHTTPResult, 1), make(chan struct{})
		go func() {
			defer close(done)
			result <- network.call(ctx, current, false, "GET", base+"/tasks/"+task.ID.String(), "", "")
		}()
		t.Cleanup(func() {
			unhold()
			cancel()
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Error("independent real HTTP caller did not join")
			}
			v.tracked.mu.Lock()
			v.tracked.beforeLocks = nil
			v.tracked.mu.Unlock()
		})
		var pid int32
		select {
		case pid = <-entered:
		case <-done:
			t.Fatal("HTTP call returned before current-session barrier")
		case <-time.After(time.Second):
			t.Fatal("HTTP did not reach reader Tx after authentication")
		}
		var inTx bool
		if v.raw.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database() AND xact_start IS NOT NULL)`, pid).Scan(&inTx) != nil || !inTx {
			t.Fatal("HTTP barrier lacked real transaction")
		}
		before := independentHTTPState(t, v)
		if e := v.core.Logout(ctxFor(t), account.LogoutRequest{Actor: current.actor, Key: "independent-http-current-logout"}); e != nil {
			t.Fatal("formal concurrent Logout failed")
		}
		unhold()
		select {
		case response := <-result:
			independentHTTPProblem(t, response, 401, f.SessionRevoked)
		case <-time.After(3 * time.Second):
			t.Fatal("revoked real HTTP reader did not return")
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("HTTP reader caller missing actual join")
		}
		if after := independentHTTPState(t, v); after != before {
			t.Fatal("revoked HTTP read changed Work facts")
		}
		var held int
		if v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_locks WHERE pid=$1 AND locktype='advisory'`, pid).Scan(&held) != nil || held != 0 {
			t.Fatal("revoked HTTP reader retained transaction locks")
		}
	})
}
