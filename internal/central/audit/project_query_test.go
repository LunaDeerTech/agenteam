package audit

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const projectReadID = "01900000-0000-7000-8000-000000000003"

type projectReadAuthority struct {
	c.ProjectAuthority
	call func(context.Context, foundation.Tx, identity.Actor, identity.ProjectID, identity.AccessIntent) (identity.AccessGrant, error)
}

func (a projectReadAuthority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, project identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	return a.call(ctx, tx, actor, project, intent)
}

type projectReadStore struct {
	*auditReadStore
	project identity.ProjectID
	locks   int
}

func (s *projectReadStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.started.Add(1)
	s.active.Add(1)
	defer s.active.Add(-1)
	if cause.Validate() != nil || cause.Details().Owner != "audit.project-read" {
		s.t.Error("wrong private read cause")
	}
	s.tx = foundation.NewTx()
	s.steps = append(s.steps, "begin")
	err := fn(ctx, s.tx)
	s.steps = append(s.steps, "callback-ended")
	if s.tail != nil {
		s.tail(ctx)
	}
	s.steps = append(s.steps, "transaction-ended")
	if s.result != nil {
		return *s.result
	}
	if err != nil {
		var f *foundation.Fault
		if !errors.As(err, &f) {
			f = foundation.NewFault(foundation.InternalError, foundation.NotCommitted).WithCause(err)
		}
		return foundation.NotCommittedResult(f)
	}
	return foundation.CommittedResult()
}
func (s *projectReadStore) Acquire(_ context.Context, tx foundation.Tx, key foundation.LockKey, mode foundation.LockMode) error {
	user, _ := foundation.UserLock("01900000-0000-7000-8000-000000000001")
	project, _ := foundation.ProjectLock(s.project.String())
	want := user
	name := "user-shared"
	if s.locks == 1 {
		want = project
		name = "project-shared"
	}
	if tx != s.tx || s.locks > 1 || foundation.CompareLockKeys(key, want) != 0 || mode != foundation.Shared {
		s.t.Error("wrong ordered same-Tx SH locks")
	}
	s.locks++
	s.steps = append(s.steps, name)
	s.held = s.locks == 2 && s.acquireErr == nil
	return s.acquireErr
}
func projectReadRow() auditTestRow {
	r := systemTestRow()
	r.values[2] = "project"
	r.values[3] = projectReadID
	return r
}
func projectReadFixture(t *testing.T) (*Service, *projectReadStore, identity.Actor, identity.ProjectID, c.ID) {
	t.Helper()
	old, store, actor, id := systemTestService(t)
	p, _ := foundation.ParseID[identity.Project](projectReadID)
	s := &projectReadStore{auditReadStore: store, project: p}
	s.row = projectReadRow()
	old.store = s
	old.auth.System = nil
	old.auth.Sessions = sessionPort(func(_ context.Context, tx foundation.Tx, got identity.Actor) error {
		if tx != s.tx || !s.held || got.Details() != actor.Details() {
			t.Error("session bypassed current same-Tx principal/locks")
		}
		s.steps = append(s.steps, "session")
		return nil
	})
	old.auth.Projects = projectReadAuthority{call: func(_ context.Context, tx foundation.Tx, a identity.Actor, pid identity.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
		if tx != s.tx || !s.held || pid != p || intent != identity.Read {
			t.Error("owner query bypassed same-Tx read locks")
		}
		s.steps = append(s.steps, "owner")
		scope, _ := identity.InProject(p)
		now, _ := foundation.NewInstant(time.Now())
		return identity.NewAccessGrant(a, scope, intent, now, 1)
	}}
	return old, s, actor, p, id
}
func TestProjectAuditReadTransactionAndAuthority(t *testing.T) {
	t.Run("same-transaction", func(t *testing.T) {
		svc, s, a, p, id := projectReadFixture(t)
		got, err := svc.GetProject(context.Background(), a, p, id)
		if err != nil || got.AuditID != id {
			t.Fatal(err)
		}
		want := []string{"begin", "user-shared", "project-shared", "session", "owner", "executor", "query-row", "callback-ended", "transaction-ended"}
		if !reflect.DeepEqual(s.steps, want) || s.started.Load() != 1 {
			t.Fatal(s.steps)
		}
		if !strings.Contains(s.query, "WHERE scope=$1 AND scope_key=$2 AND id=$3") || !reflect.DeepEqual(s.args, []any{"project", p.String(), id.String()}) {
			t.Fatal("detail scope binding")
		}
	})
	for _, name := range []string{"revoked", "not-owner", "forged-grant", "user-lock", "wrong-scope", "wrong-id", "bad-metadata", "bad-time", "missing-project-port", "non-human", "nil-context"} {
		t.Run(name, func(t *testing.T) {
			svc, s, a, p, id := projectReadFixture(t)
			ctx := context.Background()
			code, state := foundation.DependencyUnavailable, foundation.NotCommitted
			switch name {
			case "revoked":
				code = foundation.Unauthenticated
				svc.auth.Sessions = sessionPort(func(context.Context, foundation.Tx, identity.Actor) error {
					return foundation.NewFault(code, foundation.NotCommitted)
				})
			case "not-owner":
				code = foundation.NotFound
				svc.auth.Projects = projectReadAuthority{call: func(context.Context, foundation.Tx, identity.Actor, identity.ProjectID, identity.AccessIntent) (identity.AccessGrant, error) {
					return identity.AccessGrant{}, foundation.NewFault(code, foundation.NotCommitted)
				}}
			case "forged-grant":
				svc.auth.Projects = projectReadAuthority{call: func(_ context.Context, _ foundation.Tx, a identity.Actor, _ identity.ProjectID, i identity.AccessIntent) (identity.AccessGrant, error) {
					now, _ := foundation.NewInstant(time.Now())
					return identity.NewAccessGrant(a, identity.SystemScope(), i, now, 1)
				}}
			case "user-lock":
				s.acquireErr = errors.New("lock-rejected")
			case "wrong-scope":
				s.row = systemTestRow()
			case "wrong-id":
				r := projectReadRow()
				r.values[0] = "01900000-0000-7000-8000-000000000007"
				s.row = r
			case "bad-metadata":
				r := projectReadRow()
				r.values[16] = `{"version":"1","raw":"secret-canary"}`
				s.row = r
			case "bad-time":
				r := projectReadRow()
				r.values[1] = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
				s.row = r
			case "missing-project-port":
				svc.auth.Projects = nil
				code, state = foundation.DependencyUnbound, foundation.NotStarted
			case "non-human":
				reg, _ := identity.RegisterService(identity.SecretService)
				a, _ = reg.Actor("sha256:"+strings.Repeat("a", 64), identity.SystemScope())
				code, state = foundation.Forbidden, foundation.NotStarted
			case "nil-context":
				ctx = nil
				code, state = foundation.InvalidArgument, foundation.NotStarted
			}
			got, err := svc.GetProject(ctx, a, p, id)
			if !reflect.DeepEqual(got, c.SafeRecord{}) {
				t.Fatal("candidate after failure")
			}
			wantSystemFault(t, err, code, state)
			if name == "revoked" || name == "not-owner" || name == "user-lock" {
				for _, step := range s.steps {
					if step == "query-row" {
						t.Fatal("unauthorized observation")
					}
				}
			}
		})
	}
	t.Run("sql-bound-filter", func(t *testing.T) {
		svc, s, a, p, _ := projectReadFixture(t)
		_, err := svc.ListProject(context.Background(), a, p, c.Filter{Action: c.SecretCreate}, foundation.PageRequest{Limit: 200})
		if err == nil || !strings.Contains(s.query, "LIMIT $5") || !reflect.DeepEqual(s.args, []any{"project", p.String(), p.String(), string(c.SecretCreate), 201}) {
			t.Fatal("SQL lost scope/filter/exact sentinel limit", s.query, s.args, err)
		}
	})
}
func TestProjectAuditReadTerminalAndPage(t *testing.T) {
	t.Run("original-unknown", func(t *testing.T) {
		svc, s, a, p, id := projectReadFixture(t)
		attempt, _ := foundation.NewID[foundation.TransactionAttempt]()
		cause, _ := foundation.NewRecoveryCause("read-probe", p.String(), "")
		r := foundation.UnknownResult(attempt, cause)
		s.result = &r
		got, err := svc.GetProject(context.Background(), a, p, id)
		wantSystemFault(t, err, foundation.CommitUnknown, foundation.Unknown)
		actual, ok := ProjectReadUnknownAttempt(fmt.Errorf("wrapped: %w", err))
		if !ok || actual.State() != r.State() || actual.AttemptID() != attempt || !reflect.DeepEqual(actual.Cause().Details(), cause.Details()) || !reflect.DeepEqual(got, c.SafeRecord{}) || s.started.Load() != 1 {
			t.Fatal("original unknown/read ownership lost")
		}
	})
	t.Run("cancel-after-candidate", func(t *testing.T) {
		svc, s, a, p, id := projectReadFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.tail = func(context.Context) { cancel() }
		got, err := svc.GetProject(ctx, a, p, id)
		wantSystemFault(t, err, foundation.DependencyUnavailable, foundation.NotStarted)
		if !reflect.DeepEqual(got, c.SafeRecord{}) {
			t.Fatal("cancelled publication")
		}
	})
	t.Run("actual-tail", func(t *testing.T) {
		svc, s, a, p, id := projectReadFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		var err error
		s.scan = func(ctx context.Context) scanner {
			return auditScanFunc(func(...any) error { close(entered); <-ctx.Done(); <-release; return ctx.Err() })
		}
		go func() { defer close(done); _, err = svc.GetProject(ctx, a, p, id) }()
		t.Cleanup(func() { cancel(); once.Do(func() { close(release) }); <-done })
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("scan missing")
		}
		cancel()
		select {
		case <-done:
			t.Fatal("escaped actual scan")
		default:
		}
		once.Do(func() { close(release) })
		<-done
		wantSystemFault(t, err, foundation.DependencyUnavailable, foundation.NotCommitted)
		if s.active.Load() != 0 {
			t.Fatal("unjoined transaction")
		}
	})
	for _, name := range []string{"success-200", "sentinel-scope", "sentinel-filter", "sentinel-metadata", "sentinel-order", "sentinel-duplicate", "rows-error", "close-error", "too-many", "sign-error"} {
		t.Run(name, func(t *testing.T) {
			svc, _, _, p, _ := projectReadFixture(t)
			scope, _ := identity.InProject(p)
			filter := c.Filter{Action: c.SecretCreate}
			digest, _ := filterDigest(filter)
			binding := cursor.Binding{Scope: scope, QueryDigest: digest, Order: cursor.AuditOrder}
			rows := &auditTestRows{}
			for i := 0; i < 201; i++ {
				r := projectReadRow()
				r.values[0] = fmt.Sprintf("01900000-0000-7000-8000-%012x", 1000-i)
				rows.rows = append(rows.rows, r)
			}
			last := rows.rows[200]
			switch name {
			case "sentinel-scope":
				last.values[2] = "system"
				last.values[3] = ""
			case "sentinel-filter":
				last.values[12] = "secret.update"
			case "sentinel-metadata":
				last.values[16] = `{"version":"0"}`
			case "sentinel-order":
				last.values[0] = rows.rows[0].values[0]
			case "sentinel-duplicate":
				last.values[0] = rows.rows[199].values[0]
			case "rows-error":
				rows.err = errors.New("scan-tail")
			case "close-error":
				rows.closeErr = errors.New("close-tail")
			case "too-many":
				rows.rows = append(rows.rows, projectReadRow())
			case "sign-error":
				svc.keys = cursor.Keyring{}
			}
			check, err := svc.projectPageCheck(scope, filter, foundation.PageRequest{Limit: 200})
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.recordPage(rows, binding, 200, check)
			if rows.closes != 1 {
				t.Fatal("Rows.Close ownership")
			}
			if name != "success-200" {
				if err == nil || len(got.Items) != 0 || got.NextCursor != "" {
					t.Fatal("bad full row/sentinel leaked page")
				}
				return
			}
			if err != nil || len(got.Items) != 200 || got.NextCursor == "" {
				t.Fatal("max legal page rejected", err)
			}
			pos, err := svc.keys.Verify(got.NextCursor, binding)
			if err != nil || pos.Scalars[1].Value() != got.Items[199].AuditID.String() {
				t.Fatal("cursor points at sentinel")
			}
			other, _ := foundation.ParseID[identity.Project]("01900000-0000-7000-8000-000000000004")
			foreign, _ := identity.InProject(other)
			if _, err := svc.projectPageCheck(foreign, filter, foundation.PageRequest{Limit: 1, Cursor: got.NextCursor}); err == nil {
				t.Fatal("cross project cursor")
			}
			if _, err := svc.projectPageCheck(scope, c.Filter{}, foundation.PageRequest{Limit: 1, Cursor: got.NextCursor}); err == nil {
				t.Fatal("cross filter cursor")
			}
			if _, err := svc.projectPageCheck(scope, filter, foundation.PageRequest{Limit: 1, Cursor: got.NextCursor}); err != nil {
				t.Fatal("limit incorrectly bound", err)
			}
		})
	}
}
