//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/app"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func projectUpdateRootRequest(t *testing.T, root *projectUsageRoot, browser systemHTTPBrowser, method, path, key string, body []byte) systemHTTPResponse {
	t.Helper()
	r, e := http.NewRequestWithContext(testContext(t), method, "http://"+root.address+path, bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	r.Host = "system-http.example.test"
	r.Header.Set("Origin", systemHTTPOrigin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("X-CSRF-Token", browser.csrf)
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: browser.cookie})
	response, e := root.client.Do(r)
	if e != nil {
		t.Fatal("default root request terminal", e)
	}
	raw, re := io.ReadAll(io.LimitReader(response.Body, 65537))
	ce := response.Body.Close()
	if re != nil || ce != nil || len(raw) > 65536 {
		t.Fatal("default root EOF/Close", re, ce)
	}
	return systemHTTPResponse{response.StatusCode, response.Header.Clone(), raw}
}
func TestModelProjectOwnerUpdateHTTPRootBinding(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectUpdateFixture(t)
	root := startProjectUsageRoot(t, v.projectUsageHTTPFixture)
	path := projectOwnerReadPath(v.project.ID)
	lookup := projectUpdateLookupPath(v.project.ID)
	before := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	name := "Root-Updated"
	body := projectUpdateBody(t, 1, &name, nil)
	first := projectUpdateRootRequest(t, root, v.ownerBrowser, "PATCH", path, "root-original", body).want(t, 200)
	if first.object(t)["version"] != "2" {
		t.Fatal("root not real Service")
	}
	after := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	if after.Completed != before.Completed+1 || after.Events != before.Events+1 || after.Audits != before.Audits+1 {
		t.Fatal("root missing real transaction providers")
	}
	desc := "later"
	projectUpdateRootRequest(t, root, v.ownerBrowser, "PATCH", path, "root-later", projectUpdateBody(t, 2, nil, &desc)).want(t, 200)
	history := projectUpdateRootRequest(t, root, v.ownerBrowser, "POST", lookup, "root-original", projectUpdateLookupBody).want(t, 200)
	historical := history.object(t)["result"].(map[string]any)["project"].(map[string]any)
	if historical["version"] != "2" || historical["description"] == desc {
		t.Fatal("root returned current instead of history")
	}
	if !bytes.Equal(projectUpdateRootRequest(t, root, v.ownerBrowser, "PATCH", path, "root-original", body).want(t, 200).body, first.body) {
		t.Fatal("original intent lost")
	}
	for _, target := range []string{path, "/api/v1/projects", projectUsageResolve(v.ownerName, name), projectUsagePath(v.project.ID), projectUsagePath(v.project.ID) + "/summary?group_by=day", "/api/v1/session"} {
		root.request(t, v.ownerBrowser, "GET", target, nil).want(t, 200)
	}
	root.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, v.project.Name), nil).problem(t, 404, f.NotFound)
	root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers", nil).want(t, 200)
	selection := root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-selection/meeting-summary", nil).want(t, 200).object(t)
	if len(selection) != 3 || selection["model"] != nil || selection["version"] != "1" {
		t.Fatal("Summary initialization changed")
	}
	for _, command := range []string{"create", "archive", "restore", "delete", "retry-lifecycle"} {
		projectUpdateRootRequest(t, root, v.ownerBrowser, "POST", lookup, "root-original", projectUpdateJSON(t, map[string]string{"command": command})).problem(t, 400, f.InvalidArgument)
	}
	root.request(t, v.ownerBrowser, "POST", "/api/v1/projects", nil).problem(t, 405, f.MethodNotAllowed)
	projectUpdateRootRequest(t, root, v.adminBrowser, "PATCH", path, "root-original", body).problem(t, 404, f.NotFound)
	ready := root.request(t, v.ownerBrowser, "GET", "/readyz", nil)
	var problem httpapi.Problem
	if ready.status != 503 || json.Unmarshal(ready.body, &problem) != nil || (problem.Code != f.DependencyUnbound && problem.Code != f.DependencyUnavailable) || problem.CommitState != f.NotStarted {
		t.Fatal("default readiness changed")
	}
	projectUpdateExport(t, "root-patch", "PATCH", path, first)
	projectUpdateExport(t, "root-lookup", "POST", lookup, history)
	root.stop(t)
	if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
		t.Fatal("default root did not actually drain")
	}
	for _, private := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf, "root-original", name} {
		if strings.Contains(root.logs.String(), private) {
			t.Fatal("root leaked command")
		}
	}
	projectUpdateRootUnknownShutdown(t, v)
	t.Log("public default root real Audit/Outbox/Project Service; original key preserved, current GET differs from historical receipt; actual shutdown")
}

func startProjectUpdateRoot(t *testing.T, v *projectUsageHTTPFixture, databaseURL, shutdown string) *projectUsageRoot {
	t.Helper()
	objects, err := objectfixture.Environment(testContext(t), v.db.Name)
	if err != nil {
		t.Fatal("owned root object environment unavailable")
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, entry := range objects {
		k, value, _ := strings.Cut(entry, "=")
		values[k] = value
	}
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	for k, value := range map[string]string{
		"DATABASE_URL": databaseURL, "DATABASE_TLS_MODE": "disable", "DATABASE_STARTUP_TIMEOUT": "15s", "HTTP_ADDR": "127.0.0.1:0", "PUBLIC_ORIGIN": systemHTTPOrigin, "SHUTDOWN_TIMEOUT": shutdown,
		"ACCOUNT_RECOVERY_LOG":           filepath.Join(dir, "root-recovery.jsonl"),
		"CURSOR_KEYRING":                 fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)),
		"SECRET_KEYRING":                 fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)),
		"OBJECT_DOWNLOAD_KEYRING":        fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)),
		"ACCOUNT_KEYRING":                fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)),
		"KNOWLEDGE_CONFIRMATION_KEYRING": fmt.Sprintf(`{"format":1,"current_kid":"k","keys":[{"kid":"k","key_b64":%q}]}`, b64(5)),
	} {
		values[config.Prefix+k] = value
	}
	var environment []string
	for k, value := range values {
		environment = append(environment, k+"="+value)
	}
	cfg, err := config.Load(func(k string) (string, bool) { value, ok := values[k]; return value, ok }, environment)
	if err != nil {
		t.Fatal("owned default root configuration", err)
	}
	r := &projectUsageRoot{done: make(chan struct{}), logs: &projectUsageRootLog{listening: make(chan string, 1)}}
	logger, err := logging.New(logging.Central, slog.LevelInfo, r.logs)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() { r.err = app.Run(ctx, cfg, logger, nil); close(r.done) }()
	t.Cleanup(func() { r.stop(t) })
	select {
	case r.address = <-r.logs.listening:
	case <-r.done:
		t.Fatal("default root initialization failed", r.err)
	case <-time.After(40 * time.Second):
		t.Fatal("default root startup did not complete")
	}
	transport := &http.Transport{Proxy: nil, MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		conn, e := (&net.Dialer{}).DialContext(ctx, network, r.address)
		if e == nil {
			r.connections.Add(1)
		}
		return conn, e
	}}
	r.client = &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return r
}
func projectUpdateRootUnknownShutdown(t *testing.T, v *projectUpdateFixture) {
	for _, shutdown := range []string{"3s", "100ms"} {
		t.Run("inflight-Unknown-shutdown-"+shutdown, func(t *testing.T) {
			proxy := newProjectUpdateHeldProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), true)
			var released sync.Once
			release := func() { released.Do(func() { close(proxy.release) }) }
			defer release()
			u, e := url.Parse(v.db.Fixture.URL(v.db.Name))
			if e != nil {
				t.Fatal(e)
			}
			u.Host = proxy.listener.Addr().String()
			root := startProjectUpdateRoot(t, v.projectUsageHTTPFixture, u.String(), shutdown)
			current, e := v.projectService.GetProject(testContext(t), v.owner, v.project.ID)
			if e != nil {
				t.Fatal(e)
			}
			key := "root-held-" + shutdown
			identity, _ := pc.CommandIdentity(v.project.ID, pc.UpdateCommand, f.IdempotencyKey(key))
			command, _ := f.CommandLock(identity)
			// This exact original Outbox registration SH belongs to the final lock union;
			// the planning transaction does not acquire it. Do not infer phase from time.
			registry, _ := f.SystemConfigLock("outbox-registration")
			unblock := managementHold(t, &systemHTTPFixture{fixture: v.fixture}, registry, f.Exclusive)
			defer unblock()
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			desc := "shutdown-" + shutdown
			r, e := http.NewRequestWithContext(ctx, "PATCH", "http://"+root.address+projectOwnerReadPath(v.project.ID), bytes.NewReader(projectUpdateBody(t, current.Version, nil, &desc)))
			if e != nil {
				t.Fatal(e)
			}
			r.Host = "system-http.example.test"
			r.Header.Set("Origin", systemHTTPOrigin)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", v.ownerBrowser.csrf)
			r.Header.Set("Idempotency-Key", key)
			r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: v.ownerBrowser.cookie})
			clientDone := make(chan error, 1)
			clientJoined := false
			defer func() {
				cancel()
				release()
				if !clientJoined {
					<-clientDone
				}
			}()
			go func() {
				response, err := root.client.Do(r)
				if response != nil {
					_, readErr := io.Copy(io.Discard, response.Body)
					closeErr := response.Body.Close()
					if err == nil {
						err = errors.Join(readErr, closeErr)
					}
				}
				clientDone <- err
			}()
			writer := projectUpdateWriterPID(t, v, registry, command, key)
			proxy.targetPID.Store(writer)
			unblock()
			waitSignal(t, proxy.reached)
			if proxy.backendPID.Load() != writer {
				t.Fatal("wrong backend COMMIT captured")
			}
			managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, command)
			cancel()
			clientErr := <-clientDone
			clientJoined = true
			if clientErr == nil {
				t.Fatal("cancelled request published complete response")
			}
			started := time.Now()
			root.cancel()
			if shutdown == "3s" {
				select {
				case <-root.done:
					t.Fatal("root retired while original confirmation still held")
				case <-time.After(40 * time.Millisecond):
				}
				release()
				waitSignal(t, proxy.completed)
				root.stop(t)
				if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
					t.Fatal("normal in-flight root failed actual drain")
				}
			} else {
				select {
				case <-root.done:
				case <-time.After(2 * time.Second):
					t.Fatal("root shutdown budget not bounded")
				}
				elapsed := time.Since(started)
				if elapsed < 80*time.Millisecond || !strings.Contains(root.logs.String(), `"outcome":"forced"`) {
					t.Fatal("exhausted root did not record forced terminal", elapsed)
				}
				// Returning from app.Run is not proof of every inner join. The separately
				// owned proxy still retains the original PostgreSQL writer until released.
				select {
				case <-proxy.completed:
					t.Fatal("root forged proxy writer terminal")
				default:
				}
				release()
				waitSignal(t, proxy.completed)
				root.once.Do(func() { root.client.CloseIdleConnections() })
			}
			var state string
			if err := v.raw.QueryRow(testContext(t), `SELECT state FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND key=$2`, v.project.ID.String(), key).Scan(&state); err != nil || state != "completed" {
				t.Fatal("original physical writer terminal", err, state)
			}
			t.Log("target Project/update planned fact + unique registry waiter holding exact command lock bound original backend", writer, "shutdown", shutdown, "proxy original COMMIT terminal separately joined; forced root return does not prove inner joins")
		})
	}
}
func projectUpdateWriterPID(t *testing.T, v *projectUpdateFixture, barrier, command f.LockKey, key string) int32 {
	t.Helper()
	deadline := time.Now().Add(700 * time.Millisecond)
	b, c := uint64(barrier.AdvisoryKey()), uint64(command.AdvisoryKey())
	for time.Now().Before(deadline) {
		var pid int32
		var count int
		e := v.raw.QueryRow(testContext(t), `SELECT count(*),COALESCE(min(l.pid),0) FROM pg_locks l WHERE l.locktype='advisory' AND l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND l.classid::bigint=$1 AND l.objid::bigint=$2 AND l.objsubid=1 AND NOT l.granted AND EXISTS(SELECT 1 FROM pg_locks c WHERE c.pid=l.pid AND c.locktype='advisory' AND c.classid::bigint=$3 AND c.objid::bigint=$4 AND c.objsubid=1 AND c.granted) AND EXISTS(SELECT 1 FROM agenteam_project.commands WHERE project_id=$5 AND command_name='update' AND key=$6 AND state='planned' AND safe_result IS NULL AND plan IS NOT NULL)`, int64(uint32(b>>32)), int64(uint32(b)), int64(uint32(c>>32)), int64(uint32(c)), v.project.ID.String(), key).Scan(&count, &pid)
		if e != nil {
			t.Fatal(e)
		}
		if count > 1 {
			t.Fatal("ambiguous target backend")
		}
		if count == 1 && pid > 0 {
			return pid
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("exact final writer/plan barrier not reached")
	return 0
}
