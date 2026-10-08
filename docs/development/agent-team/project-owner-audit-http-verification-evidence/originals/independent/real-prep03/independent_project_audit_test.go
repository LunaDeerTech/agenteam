//go:build integration

package model_test

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
)

func TestIndependentProjectAuditOwnerAndPage(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectAuditFixture(t)
	for i := 0; i < 3; i++ {
		projectUsageRename(t, v.projectUsageHTTPFixture, fmt.Sprintf("independent-audit-row-%d", i))
	}
	filter := ac.Filter{Action: ac.ProjectUpdate}
	page, e := v.aud.ListProject(testContext(t), v.owner, v.project.ID, filter, f.PageRequest{Limit: 2})
	if e != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatal("formal Project producer pagination", e)
	}
	last, e := v.aud.ListProject(testContext(t), v.owner, v.project.ID, filter, f.PageRequest{Limit: 1, Cursor: page.NextCursor})
	if e != nil || len(last.Items) != 1 || last.NextCursor != "" || last.Items[0].AuditID == page.Items[1].AuditID {
		t.Fatal("real signed cursor continuation with changed limit", e)
	}
	target := last.Items[0].AuditID
	t.Run("both-shared-locks-coexist", func(t *testing.T) {
		ctx, cancel := context.WithCancel(testContext(t))
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		user, _ := f.UserLock(v.owner.Details().UserID)
		project, _ := f.ProjectLock(v.project.ID.String())
		locks := []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}}
		var terminal f.CommitResult
		t.Cleanup(func() { unblock(); cancel(); <-done })
		go func() {
			defer close(done)
			cause, _ := f.NewRecoveryCause("audit-independent-holder", newID[struct{}](t).String(), "")
			terminal = v.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				if e := v.raw.AcquireAll(ctx, tx, locks); e != nil {
					return e
				}
				close(entered)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		waitSignal(t, entered)
		var read atomic.Int32
		v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
			if !projectAuditReadCause(c) {
				return nil
			}
			read.Add(1)
			return v.raw.RequireHeldLocks(ctx, tx, locks)
		}, nil)
		defer v.tracked.hooks(nil, nil)
		actual, e := v.aud.GetProject(testContext(t), v.owner, v.project.ID, target)
		if e != nil || actual.AuditID != target || read.Load() != 1 {
			t.Fatal("real read SH compatible with held SH", e)
		}
		select {
		case <-done:
			t.Fatal("holder ended before read observation")
		default:
		}
		unblock()
		<-done
		if terminal.State() != f.Committed {
			t.Fatal("holder actual terminal", terminal.Fault())
		}
	})
	t.Run("bad-hidden-sentinel-current-authority-first", func(t *testing.T) {
		var original string
		if e := v.raw.QueryRow(testContext(t), `SELECT coalesce(request_id::text,'') FROM agenteam_audit.audit_records WHERE id=$1`, target.String()).Scan(&original); e != nil {
			t.Fatal(e)
		}
		// This negative case keeps the formally produced metadata and third-row order.
		// PostgreSQL accepts UUIDv4 here; the scanner requires UUIDv7 associations.
		v.sql(t, `UPDATE agenteam_audit.audit_records SET request_id='00000000-0000-4000-8000-000000000000'::uuid WHERE id=$1`, target.String())
		defer v.sql(t, `UPDATE agenteam_audit.audit_records SET request_id=NULLIF($2,'')::uuid WHERE id=$1`, target.String(), original)
		bad, e := v.aud.ListProject(testContext(t), v.owner, v.project.ID, filter, f.PageRequest{Limit: 2})
		requireCode(t, e, f.DependencyUnavailable)
		if len(bad.Items) != 0 || bad.NextCursor != "" {
			t.Fatal("bad third sentinel leaked first two")
		}
		for _, actor := range []id.Actor{v.otherBrowser.actor, v.adminBrowser.actor} {
			got, e := v.aud.GetProject(testContext(t), actor, v.project.ID, target)
			requireCode(t, e, f.NotFound)
			if !reflect.DeepEqual(got, ac.SafeRecord{}) {
				t.Fatal("nonowner candidate")
			}
		}
		browser := v.login(t, v.ownerBrowser.email)
		if e := v.core.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); e != nil {
			t.Fatal(e)
		}
		got, e := v.aud.GetProject(testContext(t), browser.actor, v.project.ID, target)
		requireCode(t, e, f.SessionRevoked)
		if !reflect.DeepEqual(got, ac.SafeRecord{}) {
			t.Fatal("revoked direct-facade candidate")
		}
		v.read(t, v.ownerBrowser, "GET", projectAuditPath(v.project.ID)+"?limit=2&action=project.update").problem(t, 503, f.DependencyUnavailable)
	})
}

func TestIndependentProjectAuditTerminalAndRoot(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectAuditFixture(t)
	page, e := v.aud.ListProject(testContext(t), v.owner, v.project.ID, ac.Filter{}, f.PageRequest{Limit: 1})
	if e != nil || len(page.Items) != 1 {
		t.Fatal("formal Audit history", e)
	}
	target := page.Items[0].AuditID
	t.Run("physical-detail-original-Unknown-accessor", func(t *testing.T) {
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
		projects, e := project.NewAuthority(tracked, project.AuthorityDependencies{Sessions: sessions, Routes: sessions})
		if e != nil {
			t.Fatal(e)
		}
		reader, e := audit.New(tracked, ck, audit.Authorizations{Sessions: sessions, Projects: projects})
		if e != nil {
			t.Fatal(e)
		}
		var calls atomic.Int32
		var pid int32
		var original f.CommitResult
		var cause f.TransactionCause
		tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
			if !projectAuditReadCause(c) || calls.Add(1) != 1 {
				return f.NewFault(f.DependencyUnavailable, f.NotStarted)
			}
			user, _ := f.UserLock(v.owner.Details().UserID)
			project, _ := f.ProjectLock(v.project.ID.String())
			if e := raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}}); e != nil {
				return e
			}
			x, e := raw.InTx(tx)
			if e != nil {
				return e
			}
			var count int64
			if e := x.QueryRow(ctx, `SELECT pg_backend_pid(),count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND id=$2::uuid AND scope='project'`, v.project.ID.String(), target.String()).Scan(&pid, &count); e != nil {
				return e
			}
			if pid <= 0 || count != 1 {
				return f.NewFault(f.DependencyUnavailable, f.NotStarted)
			}
			cause = c
			proxy.dropCommitACK.Store(true)
			return nil
		}, func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
			if projectAuditReadCause(c) {
				original = r
			}
			return r
		})
		defer tracked.hooks(nil, nil)
		got, e := reader.GetProject(testContext(t), v.owner, v.project.ID, target)
		requireCode(t, e, f.CommitUnknown)
		waitSignal(t, proxy.ackDropped)
		attempt, ok := audit.ProjectReadUnknownAttempt(fmt.Errorf("independent wrap: %w", e))
		if !ok || calls.Load() != 1 || original.State() != f.Unknown || original.AttemptID().Validate() != nil || attempt.State() != original.State() || attempt.AttemptID() != original.AttemptID() || !reflect.DeepEqual(attempt.Cause().Details(), original.Cause().Details()) || !reflect.DeepEqual(original.Cause().Details(), cause.Details()) || !reflect.DeepEqual(got, ac.SafeRecord{}) {
			t.Fatal("physical original Unknown/accessor/zero candidate")
		}
		connection := ""
		for _, event := range proxy.snapshot() {
			n, kind, ok := strings.Cut(event, ":")
			if ok && kind == fmt.Sprintf("backend-%d", pid) {
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
		for _, event := range proxy.snapshot() {
			n, kind, _ := strings.Cut(event, ":")
			if kind == "commit-idle-ack-dropped" {
				drops++
			}
			if n == connection && stage < len(sequence) && kind == sequence[stage] {
				stage++
			}
		}
		if stage != len(sequence) || drops != 1 {
			t.Fatal("not exact same-backend terminal-first")
		}
		t.Logf("detail backend=%d original_attempt=%s original_cause_owner=%s server_COMMIT=true server_idle=true ACK_dropped=true no_confirmation=true", pid, attempt.AttemptID().String(), attempt.Cause().Details().Owner)
	})
	t.Run("default-root-original-list-detail-HEAD", func(t *testing.T) {
		root := startProjectUsageRoot(t, v.projectUsageHTTPFixture)
		path := projectAuditPath(v.project.ID)
		list := root.request(t, v.ownerBrowser, "GET", path, nil).want(t, 200)
		actual := projectAuditDecodePage(t, list, v.project.ID)
		if len(actual.Items) == 0 {
			t.Fatal("root omitted original producers")
		}
		detailPath := path + "/" + actual.Items[0].AuditID
		detail := root.request(t, v.ownerBrowser, "GET", detailPath, nil).want(t, 200)
		head := root.request(t, v.ownerBrowser, "HEAD", detailPath, nil).want(t, 200)
		if len(head.body) != 0 || head.headers.Get("Content-Length") != detail.headers.Get("Content-Length") {
			t.Fatal("real HEAD/GET encoding length")
		}
		root.request(t, v.adminBrowser, "GET", detailPath, nil).problem(t, 404, f.NotFound)
		projectAuditExport(t, "independent-root-list", "GET", path, v.project.ID, list)
		projectAuditExport(t, "independent-root-detail", "GET", detailPath, v.project.ID, detail)
		projectAuditExport(t, "independent-root-head", "HEAD", detailPath, v.project.ID, head)
		root.stop(t)
		if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
			t.Fatal("real root did not drain")
		}
	})
}
