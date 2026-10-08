//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestModelProjectAuditHTTPProjectionAndPaging(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectAuditFixture(t)
	path := projectAuditPath(v.project.ID)
	// Main history uses real producers, including safe credential/model facts.
	first := v.create(t, "audit-first", "AUDIT-MATERIAL-ONE")
	second := v.create(t, "audit-second", "AUDIT-MATERIAL-TWO")
	configurationWriteSix(t, v.configurationWriteFixture, "audit-model-", first, second, func(method, path, key string, body []byte) systemHTTPResponse {
		return v.command(t, v.ownerBrowser, method, path, key, body)
	}, false)
	v.request(t, v.ownerBrowser, "PUT", v.collection()+"/"+first, "audit-rotate", projectUpdateJSON(t, map[string]any{"expected_version": "1", "value": "AUDIT-ROTATED-MATERIAL"})).want(t, 200)
	v.request(t, v.ownerBrowser, "DELETE", v.collection()+"/"+first, "audit-delete", projectUpdateJSON(t, map[string]any{"expected_version": "2"})).want(t, 200)
	for i := 0; i < 201; i++ {
		projectUsageRename(t, v.projectUsageHTTPFixture, fmt.Sprintf("audit-row-%03d", i))
	}
	// Sorting auxiliary only: change timestamps of already formally produced rows,
	// not normal Audit payloads, actors, IDs, or claims of identical producer clocks.
	v.sql(t, `UPDATE agenteam_audit.audit_records SET created_at=$2 WHERE project_id=$1 AND action='project.update'`, v.project.ID.String(), time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC))
	listPath := path + "?limit=200&action=project.update"
	response := v.read(t, v.ownerBrowser, "GET", listPath)
	page := projectAuditDecodePage(t, response, v.project.ID)
	if len(page.Items) != 200 || page.NextCursor == "" {
		t.Fatal("real 201 sentinel missing")
	}
	for i, item := range page.Items {
		if item.Action != "project.update" || (i > 0 && page.Items[i-1].AuditID <= item.AuditID) {
			t.Fatal("DESC tie-breaker/filter violated")
		}
	}
	final := projectAuditDecodePage(t, v.read(t, v.ownerBrowser, "GET", listPath+"&cursor="+url.QueryEscape(page.NextCursor)), v.project.ID)
	if len(final.Items) != 1 || final.NextCursor != "" || final.Items[0].AuditID >= page.Items[199].AuditID {
		t.Fatal("sentinel/sign-last retained item mismatch")
	}
	changedLimit := projectAuditDecodePage(t, v.read(t, v.ownerBrowser, "GET", path+"?limit=1&action=project.update&cursor="+url.QueryEscape(page.NextCursor)), v.project.ID)
	if len(changedLimit.Items) != 1 || changedLimit.Items[0].AuditID != final.Items[0].AuditID {
		t.Fatal("limit incorrectly bound into cursor")
	}
	detailPath := path + "/" + page.Items[0].AuditID
	detail := v.read(t, v.ownerBrowser, "GET", detailPath).want(t, 200)
	var item projectAuditRecord
	if json.Unmarshal(detail.body, &item) != nil || item.AuditID != page.Items[0].AuditID {
		t.Fatal("detail mismatch")
	}
	for _, target := range []string{listPath, detailPath} {
		get := v.read(t, v.ownerBrowser, "GET", target).want(t, 200)
		head := v.read(t, v.ownerBrowser, "HEAD", target).want(t, 200)
		if len(head.body) != 0 || head.headers.Get("Content-Length") != get.headers.Get("Content-Length") {
			t.Fatal("HEAD body/length")
		}
	}
	for _, tokenPath := range []string{path + "?cursor=not-a-token", path + "?action=secret.create&cursor=" + url.QueryEscape(page.NextCursor)} {
		v.read(t, v.ownerBrowser, "GET", tokenPath).problem(t, 400, f.CursorInvalid)
	}
	for _, action := range []string{"secret.create", "secret.update", "secret.delete", string(ac.ProviderCreate), string(ac.ProviderUpdate), string(ac.ProviderDelete), "model.create", "model.update", "model.delete", "project.create.completed"} {
		t.Logf("checking formal Audit action %q", action)
		p := projectAuditDecodePage(t, v.read(t, v.ownerBrowser, "GET", path+"?action="+action), v.project.ID)
		if len(p.Items) == 0 {
			t.Fatal("formal producer history absent", action)
		}
	}
	empty := projectAuditDecodePage(t, v.read(t, v.ownerBrowser, "GET", path+"?action=account.login"), v.project.ID)
	if len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("legal System filter returned Project data")
	}
	for _, private := range []string{"AUDIT-MATERIAL-ONE", "AUDIT-MATERIAL-TWO", "AUDIT-ROTATED-MATERIAL", "audit-row-"} {
		if bytes.Contains(response.body, []byte(private)) || strings.Contains(v.logs.text(), private) {
			t.Fatal("Audit leaked source content")
		}
	}
	otherOwned := v.createProject(t, v.ownerBrowser.actor)
	otherOwner := v.createProject(t, v.otherBrowser.actor)
	v.read(t, v.ownerBrowser, "GET", projectAuditPath(otherOwned.ID)+"/"+page.Items[0].AuditID).problem(t, 404, f.NotFound)
	v.read(t, v.ownerBrowser, "GET", projectAuditPath(otherOwned.ID)+"?action=project.update&cursor="+url.QueryEscape(page.NextCursor)).problem(t, 400, f.CursorInvalid)
	v.read(t, v.ownerBrowser, "GET", projectAuditPath(otherOwner.ID)).problem(t, 404, f.NotFound)
	renewed := v.login(t, v.ownerBrowser.email)
	projectAuditDecodePage(t, v.read(t, renewed, "GET", listPath+"&cursor="+url.QueryEscape(page.NextCursor)), v.project.ID)
	projectAuditSchema(t, "ProjectAuditPage", response.body)
	projectAuditSchema(t, "ProjectAuditRecord", detail.body)
	projectAuditExport(t, "projection-list", "GET", listPath, v.project.ID, response)
	projectAuditExport(t, "projection-detail", "GET", detailPath, v.project.ID, detail)
	projectAuditExport(t, "projection-head", "HEAD", detailPath, v.project.ID, v.read(t, v.ownerBrowser, "HEAD", detailPath).want(t, 200))
	// Lifecycle setup invokes the real operation acceptance. Archived completion
	// is an explicit isolated Authority input, not unbound participant runtime.
	for _, state := range []pc.Lifecycle{pc.Archiving, pc.Archived, pc.Deleting} {
		t.Run(string(state), func(t *testing.T) {
			old := v.project
			v.project = v.createProject(t, v.owner)
			defer func() { v.project = old }()
			v.gate(t, state)
			r := v.read(t, v.ownerBrowser, "GET", projectAuditPath(v.project.ID))
			if state == pc.Deleting {
				r.problem(t, 409, f.ProjectNotActive)
			} else {
				r.want(t, 200)
			}
		})
	}
	t.Run("pending-initialization", func(t *testing.T) {
		v.skills.setMode("pending")
		defer v.skills.setMode("")
		target := newID[id.Project](t)
		out, e := v.projectService.CreateProject(testContext(t), v.owner, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: f.IdempotencyKey(target.String())}, pc.CreateProjectRequest{ProjectID: target, Name: "audit-pending-" + target.String()[24:]})
		if e != nil || out.State != pc.CreationPending {
			t.Fatal("real pending initialization", e)
		}
		v.read(t, v.ownerBrowser, "GET", projectAuditPath(target)).problem(t, 409, f.ProjectNotActive)
	})
}

func TestModelProjectAuditHTTPAuthorityAndTerminal(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectAuditFixture(t)
	path := projectAuditPath(v.project.ID)
	for _, browser := range []systemHTTPBrowser{v.otherBrowser, v.adminBrowser} {
		v.read(t, browser, "GET", path).problem(t, 404, f.NotFound)
	}
	agent, _ := id.NewAgentRun(v.project.ID, newID[id.Agent](t), newID[id.Execution](t))
	registration, _ := id.RegisterService(id.ProjectInitialization)
	service, _ := registration.Actor(newID[struct{}](t).String(), v.scope)
	for _, actor := range []id.Actor{agent, service} {
		page, e := v.aud.ListProject(testContext(t), actor, v.project.ID, ac.Filter{}, f.PageRequest{})
		requireCode(t, e, f.Forbidden)
		if len(page.Items) != 0 || page.NextCursor != "" {
			t.Fatal("nonhuman candidate")
		}
	}
	t.Run("committed-Logout-before-read", func(t *testing.T) {
		b := v.login(t, v.ownerBrowser.email)
		if e := v.core.Logout(testContext(t), account.LogoutRequest{Actor: b.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); e != nil {
			t.Fatal(e)
		}
		var read atomic.Int32
		v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
			if projectAuditReadCause(c) {
				read.Add(1)
			}
			return nil
		}, nil)
		defer v.tracked.hooks(nil, nil)
		v.read(t, b, "GET", path).problem(t, 401, f.SessionRevoked)
		if read.Load() != 0 {
			t.Fatal("revoked session reached Audit query")
		}
	})
	t.Run("read-shared-lock-then-Logout", func(t *testing.T) {
		b := v.login(t, v.ownerBrowser.email)
		entered, gate := make(chan struct{}), make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		var armed atomic.Bool
		v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
			if projectAuditReadCause(c) && armed.CompareAndSwap(false, true) {
				u, _ := f.UserLock(b.actor.Details().UserID)
				p, _ := f.ProjectLock(v.project.ID.String())
				if e := v.raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: u, Mode: f.Shared}, {Key: p, Mode: f.Shared}}); e != nil {
					return e
				}
				close(entered)
				select {
				case <-gate:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}, nil)
		ctx, cancel := context.WithCancel(testContext(t))
		flight := v.flight(ctx, b, path)
		t.Cleanup(func() { release(); cancel(); <-flight.done; v.tracked.hooks(nil, nil) })
		waitSignal(t, entered)
		logoutCtx, logoutCancel := context.WithCancel(testContext(t))
		logoutDone := make(chan struct{})
		var logoutErr error
		t.Cleanup(func() { release(); logoutCancel(); <-logoutDone })
		go func() {
			defer close(logoutDone)
			logoutErr = v.core.Logout(logoutCtx, account.LogoutRequest{Actor: b.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())})
		}()
		user, _ := f.UserLock(b.actor.Details().UserID)
		managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, user)
		if flight.writer.writeCount() != 0 {
			t.Fatal("read published before transaction terminal")
		}
		release()
		<-flight.done
		<-logoutDone
		if logoutErr != nil || flight.aborted {
			t.Fatal("actual read/Logout terminal", logoutErr)
		}
		flight.result().want(t, 200)
		v.tracked.hooks(nil, nil)
		v.read(t, b, "GET", path).problem(t, 401, f.SessionRevoked)
	})
	t.Run("ProjectEX-and-real-cancel", func(t *testing.T) {
		key, _ := f.ProjectLock(v.project.ID.String())
		release, join := projectAuditHold(t, v, key)
		ctx, cancel := context.WithCancel(testContext(t))
		flight := v.flight(ctx, v.ownerBrowser, path)
		t.Cleanup(func() { cancel(); release(); <-flight.done })
		managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, key)
		cancel()
		<-flight.done
		if !flight.aborted || flight.writer.writeCount() != 0 {
			t.Fatal("cancelled lock wait published candidate")
		}
		release()
		join()
	})
	t.Run("physical-terminal-first-ACKloss", func(t *testing.T) { projectAuditRealUnknown(t, v, path) })
}

func projectAuditRealUnknown(t *testing.T, v *projectAuditFixture, path string) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			proxy := newProjectConfigurationHTTPPGTrace(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port))
			u, e := url.Parse(v.db.Fixture.URL(v.db.Name))
			if e != nil {
				t.Fatal(e)
			}
			u.Host = proxy.listener.Addr().String()
			raw := openStore(t, v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			tracked := &projectUsageHTTPStore{Store: raw}
			ak, ck, _ := testKeys(t)
			sessions, e := account.NewAuthority(tracked, ak)
			if e != nil {
				t.Fatal(e)
			}
			authority, e := project.NewAuthority(tracked, project.AuthorityDependencies{Sessions: sessions, Routes: sessions})
			if e != nil {
				t.Fatal(e)
			}
			reader, e := audit.New(tracked, ck, audit.Authorizations{Sessions: sessions, Projects: authority})
			if e != nil {
				t.Fatal(e)
			}
			old := v.handler
			v.installAudit(t, reader)
			defer func() { v.handler = old }()
			var hit atomic.Bool
			var backend atomic.Int32
			var originalCause f.TransactionCause
			observed := make(chan f.CommitResult, 1)
			tracked.hooks(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
				if !projectAuditReadCause(cause) || !hit.CompareAndSwap(false, true) {
					return f.NewFault(f.DependencyUnavailable, f.NotStarted)
				}
				user, _ := f.UserLock(v.ownerBrowser.actor.Details().UserID)
				project, _ := f.ProjectLock(v.project.ID.String())
				if e := raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}}); e != nil {
					return e
				}
				x, e := raw.InTx(tx)
				if e != nil {
					return e
				}
				var pid int32
				var count int64
				e = x.QueryRow(ctx, `SELECT pg_backend_pid(),count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND scope='project' AND scope_key=$2::text`, v.project.ID.String(), v.project.ID.String()).Scan(&pid, &count)
				if e != nil {
					return e
				}
				if pid <= 0 || count == 0 {
					return f.NewFault(f.DependencyUnavailable, f.NotStarted)
				}
				backend.Store(pid)
				originalCause = cause
				proxy.dropCommitACK.Store(true)
				return nil
			}, func(c f.TransactionCause, result f.CommitResult) f.CommitResult {
				if projectAuditReadCause(c) {
					observed <- result
				}
				return result
			})
			response := v.read(t, v.ownerBrowser, method, path).want(t, 503)
			var original f.CommitResult
			select {
			case original = <-observed:
			case <-time.After(time.Second):
				t.Fatal("original read terminal absent")
			}
			waitSignal(t, proxy.ackDropped)
			if !hit.Load() || original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Details().Owner != originalCause.Details().Owner || original.Cause().Details().RecoveryRunID != originalCause.Details().RecoveryRunID || original.Cause().Details().CheckpointRef != originalCause.Details().CheckpointRef {
				t.Fatal("original read Cause/Attempt lost")
			}
			if method == "HEAD" {
				if len(response.body) != 0 {
					t.Fatal("Unknown HEAD body")
				}
			} else {
				response.problem(t, 503, f.CommitUnknown)
				var problem httpapi.Problem
				if json.Unmarshal(response.body, &problem) != nil || problem.CommitState != f.Unknown || problem.RetryHint != "lookup" {
					t.Fatal("Unknown classification")
				}
			}
			for _, private := range []string{`"items"`, `"audit_id"`, original.AttemptID().String(), v.project.Name} {
				if bytes.Contains(response.body, []byte(private)) {
					t.Fatal("Unknown candidate or internal attempt leaked")
				}
			}
			events := proxy.snapshot()
			connection := ""
			for _, event := range events {
				n, kind, ok := strings.Cut(event, ":")
				if ok && kind == fmt.Sprintf("backend-%d", backend.Load()) {
					if connection != "" && connection != n {
						t.Fatal("ambiguous backend")
					}
					connection = n
				}
			}
			if connection == "" {
				t.Fatal("target backend absent")
			}
			sequence := []string{"send-COMMIT", "tag-COMMIT", "ready-I-before-drop", "commit-idle-ack-dropped"}
			stage, drops := 0, 0
			for _, event := range events {
				n, kind, ok := strings.Cut(event, ":")
				if !ok {
					continue
				}
				if kind == "commit-idle-ack-dropped" {
					drops++
				}
				if n == connection && stage < len(sequence) && kind == sequence[stage] {
					stage++
				}
			}
			if stage != len(sequence) || drops != 1 {
				t.Fatal("not one same-backend terminal-first ACK loss")
			}
			t.Logf("physical Audit read backend=%d cause_owner=%s attempt=%s server_COMMIT=true server_idle=true ACK_dropped=true", backend.Load(), original.Cause().Details().Owner, original.AttemptID().String())
			tracked.hooks(nil, nil)
			v.read(t, v.ownerBrowser, "GET", path).want(t, 200) // Independent new read; never original-attempt confirmation.
		})
	}
}
