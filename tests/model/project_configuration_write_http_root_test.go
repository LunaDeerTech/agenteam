//go:build integration

package model_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelProjectConfigurationWriteHTTPDefaultRoot(t *testing.T) {
	configurationWriteTop(t)
	v := newConfigurationWriteFixture(t)
	// Public app.Run(nil hooks) must fail before listening if its original
	// required initialization table is absent; no fake Store/handler is injected.
	v.sql(t, `ALTER TABLE agenteam_model.meeting_summary_selection RENAME TO unavailable_configuration_write_summary`)
	restored := false
	restore := func() {
		v.sql(t, `ALTER TABLE agenteam_model.unavailable_configuration_write_summary RENAME TO meeting_summary_selection`)
	}
	t.Cleanup(func() {
		if !restored {
			restore()
		}
	})
	startMeetingSummarySettingsRoot(t, &systemHTTPFixture{fixture: v.fixture}, true)
	restore()
	restored = true
	root := startProjectUsageRoot(t, v.projectUsageHTTPFixture)
	call := func(method, path, key string, body []byte) systemHTTPResponse {
		return projectUpdateRootRequest(t, root, v.ownerBrowser, method, path, key, body)
	}
	first := call("POST", v.collection(), "root-configuration-secret-first", projectUpdateJSON(t, map[string]any{"value": "root-first-private"})).want(t, 200).object(t)["credential_id"].(string)
	second := call("POST", v.collection(), "root-configuration-secret-second", projectUpdateJSON(t, map[string]any{"value": "root-second-private"})).want(t, 200).object(t)["credential_id"].(string)
	history := configurationWriteSix(t, v, "root-configuration-", first, second, call, true)
	configurationReplayHistory(t, v, history, call)
	for _, h := range history {
		configurationWriteSchema(t, "ConfigurationReceipt", h.response.body)
		configurationWriteExport(t, "root-"+h.kind, h.method, h.path, h.response)
	}
	for _, suffix := range []string{"model-providers", "models", "available-chat-models"} {
		call("GET", v.base()+suffix, "", nil).want(t, 200)
	}
	for _, path := range []string{v.collection() + "/" + first, v.collection() + "/" + second, projectOwnerReadPath(v.project.ID), "/api/v1/projects", projectUsageResolve(v.ownerName, v.project.Name), projectUsagePath(v.project.ID), projectUsagePath(v.project.ID) + "/summary?group_by=day", "/api/v1/session"} {
		root.request(t, v.ownerBrowser, "GET", path, nil).want(t, 200)
	}
	root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers", nil).want(t, 200)
	summary := root.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200)
	if summary.object(t)["model"] != nil || summary.object(t)["version"] != "1" {
		t.Fatal("Summary root initialization drift")
	}
	desc := "configuration-root-owner-compat"
	patch := projectUpdateRootRequest(t, root, v.ownerBrowser, "PATCH", projectOwnerReadPath(v.project.ID), "configuration-root-owner-patch", projectUpdateBody(t, 1, nil, &desc)).want(t, 200)
	if patch.object(t)["version"] != "2" {
		t.Fatal("Owner Update root binding")
	}
	ready := root.request(t, v.ownerBrowser, "GET", "/readyz", nil)
	var problem httpapi.Problem
	if ready.status != 503 || json.Unmarshal(ready.body, &problem) != nil || (problem.Code != f.DependencyUnbound && problem.Code != f.DependencyUnavailable) || problem.CommitState != f.NotStarted {
		t.Fatal("unbound readiness changed")
	}
	if root.connections.Load() != 1 {
		t.Fatal("complete bodies failed keepalive ownership")
	}
	root.stop(t)
	if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
		t.Fatal("actual default root drain")
	}
	again := startProjectUsageRoot(t, v.projectUsageHTTPFixture)
	replay := func(method, path, key string, body []byte) systemHTTPResponse {
		return projectUpdateRootRequest(t, again, v.ownerBrowser, method, path, key, body)
	}
	configurationReplayHistory(t, v, history, replay)
	if !bytes.Equal(again.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200).body, summary.body) {
		t.Fatal("restart initialized over singleton")
	}
	again.request(t, v.ownerBrowser, "GET", v.collection()+"/"+second, nil).want(t, 200)
	again.stop(t)
	for _, logs := range []string{root.logs.String(), again.logs.String()} {
		for _, private := range []string{"root-first-private", "root-second-private", history[0].key, v.ownerBrowser.cookie, v.ownerBrowser.csrf} {
			if strings.Contains(logs, private) {
				t.Fatal("root log leaked private configuration")
			}
		}
	}
	t.Log("default app.Run/no replacement Store or handler: init failure no listener; formal credentials and six writes share existing Authority; five reads/Owner Update/Usage/System/Summary retained; stable restart receipts; unresolved production Resolution/Invocations still unbound")
}

func TestModelProjectConfigurationWriteHTTPShutdown(t *testing.T) {
	configurationWriteTop(t)
	v := newConfigurationWriteFixture(t)
	for _, shutdown := range []string{"3s", "100ms"} {
		t.Run("writer_"+shutdown, func(t *testing.T) {
			root := startProjectUpdateRoot(t, v.projectUsageHTTPFixture, v.db.Fixture.URL(v.db.Name), shutdown)
			key := "configuration-root-writer-" + shutdown
			identity := configurationCommandIdentity(t, v.ownerBrowser, v.project.ID, "provider.create", key)
			command, _ := f.CommandLock(identity)
			barrier, _ := f.SystemConfigLock("model-references")
			release := managementHold(t, &systemHTTPFixture{fixture: v.fixture}, barrier, f.Exclusive)
			ctx, cancel := context.WithCancel(testContext(t))
			body := projectUpdateJSON(t, map[string]any{"input": configurationProviderBody(nil)})
			r := projectUpdateRequest(ctx, v.ownerBrowser, "POST", v.base()+"model-providers", key, body)
			r.URL.Scheme = "http"
			r.URL.Host = root.address
			r.RequestURI = ""
			type terminal struct {
				status int
				err    error
			}
			done := make(chan terminal, 1)
			joined := make(chan struct{})
			t.Cleanup(func() { release(); cancel(); <-joined })
			go func() {
				defer close(joined)
				response, e := root.client.Do(r)
				out := terminal{err: e}
				if response != nil {
					out.status = response.StatusCode
					_, re := io.Copy(io.Discard, response.Body)
					ce := response.Body.Close()
					if out.err == nil {
						if re != nil {
							out.err = re
						} else {
							out.err = ce
						}
					}
				}
				done <- out
			}()
			pid := projectCredentialRootWriterPID(t, v.projectCredentialFixture, barrier, command)
			start := time.Now()
			root.cancel()
			if shutdown == "3s" {
				select {
				case <-root.done:
					t.Fatal("root returned before writer tail")
				case <-time.After(40 * time.Millisecond):
				}
				release()
				out := <-done
				if out.err != nil || out.status != 200 {
					t.Fatal("normal writer failed")
				}
				root.stop(t)
				if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
					t.Fatal("normal root did not drain")
				}
			} else {
				select {
				case <-root.done:
				case <-time.After(3 * time.Second):
					t.Fatal("forced root did not return")
				}
				if time.Since(start) < 80*time.Millisecond || !strings.Contains(root.logs.String(), `"outcome":"forced"`) {
					t.Fatal("force budget/outcome")
				}
				release()
				out := <-done
				if out.err == nil && out.status == 200 {
					t.Fatal("forced blocked writer published success")
				}
				root.once.Do(func() { root.client.CloseIdleConnections() })
			}
			observed, e := v.service.LookupCommand(testContext(t), model.LookupCommandRequest{Meta: mc.CommandMeta{Actor: v.ownerBrowser.actor, Scope: v.scope, Key: f.IdempotencyKey(key)}, Command: "provider.create"})
			if e != nil || observed.Found != (shutdown == "3s") {
				t.Fatal("actual Command EX terminal observation", e)
			}
			t.Logf("default_model_writer_targeted=true backend_pid=%d client_joined=true command_EX_terminal=true shutdown=%s; forced app return alone does not prove every inner join", pid, shutdown)
		})
	}
	for _, shutdown := range []string{"3s", "100ms"} {
		t.Run("slow_body_"+shutdown, func(t *testing.T) {
			root := startProjectUpdateRoot(t, v.projectUsageHTTPFixture, v.db.Fixture.URL(v.db.Name), shutdown)
			conn, e := net.DialTimeout("tcp", root.address, time.Second)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = conn.Close() })
			if e = conn.SetDeadline(time.Now().Add(5 * time.Second)); e != nil {
				t.Fatal(e)
			}
			prefix := "POST " + v.base() + "model-providers HTTP/1.1\r\nHost: system-http.example.test\r\nOrigin: " + systemHTTPOrigin + "\r\nSec-Fetch-Site: same-origin\r\nContent-Type: application/json\r\nX-CSRF-Token: " + v.ownerBrowser.csrf + "\r\nIdempotency-Key: configuration-slow-body\r\nCookie: __Host-agenteam_session=" + v.ownerBrowser.cookie + "\r\nContent-Length: 2\r\nExpect: 100-continue\r\nConnection: close\r\n\r\n"
			if _, e = io.WriteString(conn, prefix); e != nil {
				t.Fatal(e)
			}
			// The real server emits 100 Continue only when the authenticated
			// handler begins reading this request body; no fake root hook.
			reader := bufio.NewReader(conn)
			interim, err := http.ReadResponse(reader, &http.Request{Method: "POST"})
			if err != nil || interim.StatusCode != 100 {
				t.Fatal("slow body did not enter authenticated read")
			}
			if _, err = io.WriteString(conn, "{"); err != nil {
				t.Fatal(err)
			}
			root.cancel()
			if shutdown == "3s" {
				if _, e = io.WriteString(conn, "}"); e != nil {
					t.Fatal(e)
				}
				raw, e := io.ReadAll(reader)
				if e != nil || !bytes.Contains(raw, []byte("400 Bad Request")) {
					t.Fatal("normal slow body did not finish strict input")
				}
				root.stop(t)
			} else {
				select {
				case <-root.done:
				case <-time.After(3 * time.Second):
					t.Fatal("forced slow body still owned")
				}
				raw, e := io.ReadAll(reader)
				if e != nil || bytes.Contains(raw, []byte("200 OK")) {
					t.Fatal("forced slow body published success")
				}
				root.once.Do(func() { root.client.CloseIdleConnections() })
			}
		})
	}
}
