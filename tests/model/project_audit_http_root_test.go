//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func TestModelProjectAuditHTTPDefaultRoot(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectAuditFixture(t)
	// Fail the real required CheckStorage before listening, then restore exactly.
	v.sql(t, `ALTER TABLE agenteam_audit.audit_records RENAME TO unavailable_project_audit`)
	restored := false
	restore := func() { v.sql(t, `ALTER TABLE agenteam_audit.unavailable_project_audit RENAME TO audit_records`) }
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
	first := call("POST", v.collection(), "audit-root-first", projectUpdateJSON(t, map[string]any{"value": "AUDIT-ROOT-MATERIAL-ONE"})).want(t, 200).object(t)["credential_id"].(string)
	second := call("POST", v.collection(), "audit-root-second", projectUpdateJSON(t, map[string]any{"value": "AUDIT-ROOT-MATERIAL-TWO"})).want(t, 200).object(t)["credential_id"].(string)
	configurationWriteSix(t, v.configurationWriteFixture, "audit-root-model-", first, second, call, false)
	call("PUT", v.collection()+"/"+first, "audit-root-rotate", projectUpdateJSON(t, map[string]any{"expected_version": "1", "value": "AUDIT-ROOT-ROTATED"})).want(t, 200)
	call("DELETE", v.collection()+"/"+first, "audit-root-delete", projectUpdateJSON(t, map[string]any{"expected_version": "2"})).want(t, 200)
	description := "AUDIT-ROOT-DESCRIPTION-CANARY"
	call("PATCH", projectOwnerReadPath(v.project.ID), "audit-root-patch", projectUpdateBody(t, 1, nil, &description)).want(t, 200)
	path := projectAuditPath(v.project.ID)
	list := root.request(t, v.ownerBrowser, "GET", path, nil)
	page := projectAuditDecodePage(t, list, v.project.ID)
	found := map[string]bool{}
	for _, item := range page.Items {
		found[item.Action] = true
	}
	for _, action := range []string{"secret.create", "secret.update", "secret.delete", "project.update", string(ac.ProviderCreate), string(ac.ProviderUpdate), string(ac.ProviderDelete), "model.create", "model.update", "model.delete"} {
		if !found[action] {
			t.Fatal("default real producer absent", action)
		}
	}
	detailPath := path + "/" + page.Items[0].AuditID
	detail := root.request(t, v.ownerBrowser, "GET", detailPath, nil).want(t, 200)
	head := root.request(t, v.ownerBrowser, "HEAD", detailPath, nil).want(t, 200)
	if len(head.body) != 0 || head.headers.Get("Content-Length") != detail.headers.Get("Content-Length") {
		t.Fatal("real root HEAD mismatch")
	}
	root.request(t, v.adminBrowser, "GET", path, nil).problem(t, 404, f.NotFound)
	for _, target := range []string{projectOwnerReadPath(v.project.ID), "/api/v1/projects", projectUsagePath(v.project.ID), projectUsagePath(v.project.ID) + "/summary?group_by=day", projectUsageResolve(v.ownerName, v.project.Name), v.base() + "models", v.base() + "model-providers", v.base() + "available-chat-models", v.collection() + "/" + second, "/api/v1/session"} {
		root.request(t, v.ownerBrowser, "GET", target, nil).want(t, 200)
	}
	root.request(t, v.adminBrowser, "GET", "/api/v1/system/audit", nil).want(t, 200)
	root.request(t, v.adminBrowser, "GET", "/api/v1/system/model-providers", nil).want(t, 200)
	root.request(t, v.adminBrowser, "GET", meetingSummarySettingsPath, nil).want(t, 200)
	ready := root.request(t, v.ownerBrowser, "GET", "/readyz", nil)
	var problem httpapi.Problem
	if ready.status != 503 || json.Unmarshal(ready.body, &problem) != nil || problem.CommitState != f.NotStarted || (problem.Code != f.DependencyUnbound && problem.Code != f.DependencyUnavailable) {
		t.Fatal("original readiness Problem changed")
	}
	diagnostics := root.request(t, v.ownerBrowser, "GET", "/diagnostics", nil)
	if diagnostics.status != 200 || diagnostics.object(t)["ready"] != false {
		t.Fatal("unbound production capabilities changed")
	}
	projectAuditSchema(t, "ProjectAuditPage", list.body)
	projectAuditSchema(t, "ProjectAuditRecord", detail.body)
	projectAuditExport(t, "root-list", "GET", path, v.project.ID, list)
	projectAuditExport(t, "root-detail", "GET", detailPath, v.project.ID, detail)
	projectAuditExport(t, "root-head", "HEAD", detailPath, v.project.ID, head)
	requestID := list.headers.Get("X-Request-ID")
	if requestID == "" || strings.Count(root.logs.String(), `"request_id":"`+requestID+`"`) != 1 {
		t.Fatal("Audit root middleware logging count")
	}
	root.stop(t)
	if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
		t.Fatal("root did not actually drain")
	}
	for _, private := range []string{"AUDIT-ROOT-MATERIAL-ONE", "AUDIT-ROOT-MATERIAL-TWO", "AUDIT-ROOT-ROTATED", description, v.ownerBrowser.cookie, v.ownerBrowser.csrf} {
		if strings.Contains(root.logs.String(), private) || strings.Contains(string(list.body), private) {
			t.Fatal("root/body leaked protected producer value")
		}
	}
	again := startProjectUsageRoot(t, v.projectUsageHTTPFixture)
	projectAuditDecodePage(t, again.request(t, v.ownerBrowser, "GET", path, nil), v.project.ID)
	again.stop(t)
	t.Log("default app.Run with real shared Store/authorities; no new initializer or worker; production Resolution/Invocations and Object runtime join remain unbound/stopped")
}

// Identify the actual read waiter by target Project SH plus already-held User
// SH in the same backend. No command receipt exists for this read capability.
func projectAuditRootReadPID(t *testing.T, v *projectAuditFixture, user, project f.LockKey) int32 {
	t.Helper()
	ctx, cancel := context.WithTimeout(testContext(t), 2*time.Second)
	defer cancel()
	u, p := uint64(user.AdvisoryKey()), uint64(project.AdvisoryKey())
	for {
		var count int
		var pid int32
		e := v.raw.QueryRow(ctx, `SELECT count(*)::int,COALESCE(min(p.pid),0)::int FROM pg_locks p WHERE p.locktype='advisory' AND p.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND p.classid::bigint=$1 AND p.objid::bigint=$2 AND p.objsubid=1 AND p.mode='ShareLock' AND NOT p.granted AND EXISTS(SELECT 1 FROM pg_locks u WHERE u.pid=p.pid AND u.locktype='advisory' AND u.database=p.database AND u.classid::bigint=$3 AND u.objid::bigint=$4 AND u.objsubid=1 AND u.mode='ShareLock' AND u.granted)`, int64(uint32(p>>32)), int64(uint32(p)), int64(uint32(u>>32)), int64(uint32(u))).Scan(&count, &pid)
		if e != nil {
			t.Fatal("read backend checkpoint", e)
		}
		if count > 1 {
			t.Fatal("ambiguous Audit root read backend")
		}
		if count == 1 && pid > 0 {
			return pid
		}
		select {
		case <-ctx.Done():
			t.Fatal("Audit root read waiter absent")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func projectAuditRootBackendRetired(t *testing.T, v *projectAuditFixture, pid int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(testContext(t), 3*time.Second)
	defer cancel()
	for {
		var busy bool
		e := v.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND locktype='advisory')`, pid).Scan(&busy)
		if e != nil {
			t.Fatal(e)
		}
		if !busy {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned read backend lock tail remained")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func TestModelProjectAuditHTTPShutdown(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectAuditFixture(t)
	for _, budget := range []string{"3s", "100ms"} {
		t.Run(budget, func(t *testing.T) {
			root := startProjectUpdateRoot(t, v.projectUsageHTTPFixture, v.db.Fixture.URL(v.db.Name), budget)
			user, _ := f.UserLock(v.ownerBrowser.actor.Details().UserID)
			project, _ := f.ProjectLock(v.project.ID.String())
			release, holderWait := projectAuditHold(t, v, project)
			ctx, cancel := context.WithCancel(testContext(t))
			request := projectUsageRequest(ctx, v.ownerBrowser, "GET", projectAuditPath(v.project.ID))
			request.URL.Scheme = "http"
			request.URL.Host = root.address
			request.RequestURI = ""
			done := make(chan struct{})
			var status int
			var requestErr error
			t.Cleanup(func() { release(); cancel(); <-done })
			go func() {
				defer close(done)
				response, e := root.client.Do(request)
				requestErr = e
				if response != nil {
					status = response.StatusCode
					_, readErr := io.Copy(io.Discard, response.Body)
					closeErr := response.Body.Close()
					if requestErr == nil {
						if readErr != nil {
							requestErr = readErr
						} else {
							requestErr = closeErr
						}
					}
				}
			}()
			pid := projectAuditRootReadPID(t, v, user, project)
			started := time.Now()
			root.cancel()
			if budget == "3s" {
				select {
				case <-root.done:
					t.Fatal("normal root returned before live read tail")
				case <-time.After(40 * time.Millisecond):
				}
				release()
				<-done
				if requestErr != nil || status != 200 {
					t.Fatal("normal read drain failed", requestErr)
				}
				root.stop(t)
				if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
					t.Fatal("normal root not drained")
				}
			} else {
				select {
				case <-root.done:
				case <-time.After(3 * time.Second):
					t.Fatal("short shutdown did not return")
				}
				if time.Since(started) < 80*time.Millisecond || !strings.Contains(root.logs.String(), `"outcome":"forced"`) {
					t.Fatal("short shutdown classification")
				}
				release()
				<-done
				if requestErr == nil && status == 200 {
					t.Fatal("forced read published success")
				}
				root.once.Do(func() { root.client.CloseIdleConnections() })
			}
			holderWait()
			projectAuditRootBackendRetired(t, v, pid)
			t.Logf("target_read_backend=%d UserSH=true ProjectSH_wait=true root_returned=true client_actual_join=true fixture_holder_actual_join=true backend_locks_retired=true shutdown=%s; forced root return is not proof of every inner join", pid, budget)
		})
	}
}
