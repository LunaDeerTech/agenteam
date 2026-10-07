//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestModelProjectCredentialHTTPDefaultRoot(t *testing.T) {
	projectCredentialTop(t)
	v := newProjectCredentialFixture(t)
	root := startProjectUsageRoot(t, v.projectUsageHTTPFixture)
	before := v.facts(t)
	key := "root-credential-create"
	body := projectUpdateJSON(t, map[string]any{"value": "root-only-private-material"})
	created := projectUpdateRootRequest(t, root, v.ownerBrowser, "POST", v.collection(), key, body).want(t, 200)
	target := created.object(t)["credential_id"].(string)
	path := v.collection() + "/" + target
	metadata := projectUpdateRootRequest(t, root, v.ownerBrowser, "GET", path, "", nil).want(t, 200)
	head := projectUpdateRootRequest(t, root, v.ownerBrowser, "HEAD", path, "", nil).want(t, 200)
	if len(head.body) != 0 || head.headers.Get("Content-Length") != metadata.headers.Get("Content-Length") {
		t.Fatal("root HEAD lost safe encoding")
	}
	observed := projectUpdateRootRequest(t, root, v.ownerBrowser, "POST", v.lookupPath(), key, projectCredentialLookupBody(t, sc.Create, "", 0)).want(t, 200)
	for _, tc := range []struct {
		name, method, path, schema string
		response                   systemHTTPResponse
	}{{"root-create", "POST", v.collection(), "Mutation", created}, {"root-metadata", "GET", path, "Metadata", metadata}, {"root-lookup", "POST", v.lookupPath(), "Observation", observed}} {
		projectCredentialSchema(t, tc.schema, tc.response.body)
		projectCredentialExport(t, tc.name, tc.method, tc.path, tc.response)
	}
	after := v.facts(t)
	if after.Canonical != before.Canonical+1 || after.Receipts != before.Receipts+1 || after.Audits != before.Audits+1 || after.Events != before.Events {
		t.Fatal("default root real Secret/Audit facts missing")
	}
	if !bytes.Equal(projectUpdateRootRequest(t, root, v.ownerBrowser, "POST", v.collection(), key, body).want(t, 200).body, created.body) {
		t.Fatal("root changed original key/body")
	}
	v.unchanged(t, after)
	provider := v.projectProvider(t, projectCredentialRefPtr(credentialRefProject(t, v, target)))
	m := v.projectModel(t, provider)
	base := projectOwnerReadPath(v.project.ID) + "/"
	for _, suffix := range []string{"model-providers", "model-providers/" + provider.ID.String(), "models", "models/" + m.ID.String(), "available-chat-models"} {
		response := root.request(t, v.ownerBrowser, "GET", base+suffix, nil).want(t, 200)
		head := root.request(t, v.ownerBrowser, "HEAD", base+suffix, nil).want(t, 200)
		if len(head.body) != 0 || head.headers.Get("Content-Length") != response.headers.Get("Content-Length") {
			t.Fatal("accepted Model read route changed")
		}
	}
	rotated := projectUpdateRootRequest(t, root, v.ownerBrowser, "PUT", path, "root-rotate", projectUpdateJSON(t, map[string]any{"expected_version": "1", "value": "root-rotation-private"})).want(t, 200)
	if rotated.object(t)["version"] != "2" {
		t.Fatal("root rotate version")
	}
	// An unreferenced second target proves Delete; the first remains a live
	// credential reference consumed only by the accepted Model configuration.
	second := projectUpdateRootRequest(t, root, v.ownerBrowser, "POST", v.collection(), "root-second", body).want(t, 200).object(t)["credential_id"].(string)
	projectUpdateRootRequest(t, root, v.ownerBrowser, "DELETE", v.collection()+"/"+second, "root-delete", projectUpdateJSON(t, map[string]any{"expected_version": "1"})).want(t, 200)
	projectUpdateRootRequest(t, root, v.adminBrowser, "GET", path, "", nil).problem(t, 404, f.NotFound)
	for _, p := range []string{projectOwnerReadPath(v.project.ID), "/api/v1/projects", projectUsageResolve(v.ownerName, v.project.Name), projectUsagePath(v.project.ID), projectUsagePath(v.project.ID) + "/summary?group_by=day", "/api/v1/session"} {
		root.request(t, v.ownerBrowser, "GET", p, nil).want(t, 200)
	}
	description := "root-credential-owner-compat"
	projectUpdateRootRequest(t, root, v.ownerBrowser, "PATCH", projectOwnerReadPath(v.project.ID), "root-owner-patch", projectUpdateBody(t, 1, nil, &description)).want(t, 200)
	summary := root.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200).object(t)
	if len(summary) != 3 || summary["model"] != nil || summary["version"] != "1" {
		t.Fatal("system unified Summary changed")
	}
	system := projectUpdateRootRequest(t, root, v.adminBrowser, "POST", "/api/v1/system/model-credentials", "root-system-credential", body).want(t, 200)
	if system.object(t)["purpose"] != "model" {
		t.Fatal("System credential compatibility")
	}
	ready := root.request(t, v.ownerBrowser, "GET", "/readyz", nil)
	var problem httpapi.Problem
	if ready.status != 503 || json.Unmarshal(ready.body, &problem) != nil || (problem.Code != f.DependencyUnbound && problem.Code != f.DependencyUnavailable) {
		t.Fatal("unbound readiness changed")
	}
	root.stop(t)
	if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
		t.Fatal("root failed actual drain")
	}
	for _, private := range []string{"root-only-private-material", "root-rotation-private", key, v.ownerBrowser.cookie, v.ownerBrowser.csrf} {
		if strings.Contains(root.logs.String(), private) {
			t.Fatal("root log contains material/header")
		}
	}
	projectCredentialRootShutdown(t, v)
}
func projectCredentialRefPtr(v sc.CredentialRef) *sc.CredentialRef { return &v }

// Only an ordinary held lock controls timing. This calls the public default
// app.Run with no handler/Store injection, preserving the accepted config/root.
func projectCredentialRootShutdown(t *testing.T, v *projectCredentialFixture) {
	for _, shutdown := range []string{"3s", "100ms"} {
		t.Run("actual_writer_shutdown_"+shutdown, func(t *testing.T) {
			root := startProjectUpdateRoot(t, v.projectUsageHTTPFixture, v.db.Fixture.URL(v.db.Name), shutdown)
			key := "root-writer-" + shutdown
			lookup := v.lookup(t, v.ownerBrowser, key, sc.Create, "", 0)
			command, _ := f.CommandLock(lookup.Identity)
			barrier, _ := f.SystemConfigLock("secret-write-key")
			release := managementHold(t, &systemHTTPFixture{fixture: v.fixture}, barrier, f.Exclusive)
			defer release()
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			r, e := http.NewRequestWithContext(ctx, "POST", "http://"+root.address+v.collection(), bytes.NewReader(projectUpdateJSON(t, map[string]any{"value": "inflight-private"})))
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
			type terminal struct {
				status int
				err    error
			}
			done := make(chan terminal, 1)
			joined := false
			defer func() {
				release()
				cancel()
				if !joined {
					<-done
				}
			}()
			go func() {
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
			pid := projectCredentialRootWriterPID(t, v, barrier, command)
			started := time.Now()
			root.cancel()
			if shutdown == "3s" {
				select {
				case <-root.done:
					t.Fatal("root returned before active writer retired")
				case <-time.After(40 * time.Millisecond):
				}
				release()
				out := <-done
				joined = true
				if out.err != nil || out.status != 200 {
					t.Fatal("normal writer did not complete")
				}
				root.stop(t)
				if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
					t.Fatal("normal root did not drain")
				}
				observed, e := v.writer.LookupWriteCommand(testContext(t), lookup)
				if e != nil || !observed.Observed {
					t.Fatal("normal root writer receipt missing")
				}
			} else {
				select {
				case <-root.done:
				case <-time.After(3 * time.Second):
					t.Fatal("forced root did not return")
				}
				if time.Since(started) < 80*time.Millisecond || !strings.Contains(root.logs.String(), `"outcome":"forced"`) {
					t.Fatal("shutdown budget outcome missing")
				}
				release()
				out := <-done
				joined = true
				if out.err == nil && out.status == 200 {
					t.Fatal("force published success while blocked")
				}
				root.once.Do(func() { root.client.CloseIdleConnections() })
				// Command SH observation waits for the actual backend transaction end.
				observed, e := v.writer.LookupWriteCommand(testContext(t), lookup)
				if e != nil || observed.Observed {
					t.Fatal("cancelled writer committed or remains unconfirmed")
				}
			}
			t.Log("default root original Secret writer uniquely held command EX at key-gate barrier; backend", pid, "shutdown", shutdown, "client actually joined; forced app return is not a claim of every inner join")
		})
	}
}
func projectCredentialRootWriterPID(t *testing.T, v *projectCredentialFixture, barrier, command f.LockKey) int32 {
	t.Helper()
	b, c := uint64(barrier.AdvisoryKey()), uint64(command.AdvisoryKey())
	ctx, cancel := context.WithTimeout(testContext(t), 2*time.Second)
	defer cancel()
	timer := time.NewTicker(5 * time.Millisecond)
	defer timer.Stop()
	for {
		var count int
		var pid int32
		e := v.raw.QueryRow(ctx, `SELECT count(*),COALESCE(min(l.pid),0) FROM pg_locks l WHERE l.locktype='advisory' AND l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND l.classid::bigint=$1 AND l.objid::bigint=$2 AND l.objsubid=1 AND NOT l.granted AND EXISTS(SELECT 1 FROM pg_locks c WHERE c.pid=l.pid AND c.locktype='advisory' AND c.classid::bigint=$3 AND c.objid::bigint=$4 AND c.objsubid=1 AND c.granted AND c.mode='ExclusiveLock')`, int64(uint32(b>>32)), int64(uint32(b)), int64(uint32(c>>32)), int64(uint32(c))).Scan(&count, &pid)
		if e != nil {
			t.Fatal("target writer observation failed")
		}
		if count > 1 {
			t.Fatal("ambiguous Secret writer")
		}
		if count == 1 && pid > 0 {
			return pid
		}
		select {
		case <-ctx.Done():
			t.Fatal("Secret writer did not reach exact barrier")
		case <-timer.C:
		}
	}
}
