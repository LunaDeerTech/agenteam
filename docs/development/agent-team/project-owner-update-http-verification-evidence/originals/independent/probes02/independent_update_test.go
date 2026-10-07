//go:build integration

package model_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Captures genuine values while delegating both calls to the actual journal.
// The accepted author fixture supplies formal identity and persisted Skills;
// this private service changes no stored fact, validator result or lock plan.
type independentUpdateAppender struct {
	*outbox.Service
	event    ec.Event
	plan     oc.AppendPlan
	appended bool
}

func (a *independentUpdateAppender) PrepareAppend(ctx context.Context, actor id.Actor, event ec.Event) (oc.AppendPlan, error) {
	plan, err := a.Service.PrepareAppend(ctx, actor, event)
	if err == nil {
		a.event, a.plan = event, plan
	}
	return plan, err
}
func (a *independentUpdateAppender) AppendEventInTx(ctx context.Context, tx f.Tx, actor id.Actor, event ec.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	receipt, err := a.Service.AppendEventInTx(ctx, tx, actor, event, plan)
	if err == nil {
		a.appended = true
	}
	return receipt, err
}
func independentUpdateCapturedService(t *testing.T, v *projectUpdateFixture) *independentUpdateAppender {
	t.Helper()
	ak, ck, _ := testKeys(t)
	accounts, err := account.NewAuthority(v.tracked, ak)
	if err != nil {
		t.Fatal(err)
	}
	authority := v.gates.Authority
	auditor, err := audit.New(v.tracked, ck, audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: authority})
	if err != nil {
		t.Fatal(err)
	}
	catalog := ec.NewCatalog()
	events, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := liveProcess{newID[oc.Process](t)}
	journal, err := outbox.New(v.tracked, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{pc.ProjectProducer: authority}, Projects: authority, Processes: process, Sessions: accounts, System: accounts, Audit: auditor, Cursors: ck})
	if err != nil {
		t.Fatal(err)
	}
	capture := &independentUpdateAppender{Service: journal}
	service, err := project.New(v.tracked, project.Dependencies{Authority: authority, Activity: accounts, Audit: auditor, Events: capture, ProjectEvents: events, Processes: process, Cursors: ck}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.Stop()
		if err := service.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	})
	v.install(t, service)
	return capture
}
func independentUpdateCode(t *testing.T, err error, want f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != want {
		t.Fatalf("actual fault=%v want=%v", err, want)
	}
}

func TestModelProjectOwnerUpdateIndependentAuthorityAndTerminal(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectUpdateFixture(t)
	capture := independentUpdateCapturedService(t, v)
	path := projectOwnerReadPath(v.project.ID)
	description := "independent original"
	originalBody := projectUpdateBody(t, 1, nil, &description)
	first := v.write(t, v.ownerBrowser, "PATCH", path, "ind-original", originalBody).want(t, 200)
	if first.object(t)["version"] != "2" || !capture.appended {
		t.Fatal("genuine final event not reached")
	}
	originalEvent, originalPlan := capture.event, capture.plan
	stable := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	user, _ := f.UserLock(v.ownerBrowser.actor.Details().UserID)
	projectLock, _ := f.ProjectLock(v.project.ID.String())
	registry, _ := f.SystemConfigLock("outbox-registration")
	for _, tc := range []struct {
		name    string
		missing *f.LockKey
	}{{"complete-lock-positive", nil}, {"missing-user", &user}, {"missing-project", &projectLock}, {"missing-outbox-registration", &registry}} {
		t.Run(tc.name, func(t *testing.T) {
			locks := originalPlan.Locks()
			selected := make([]f.LockRequest, 0, len(locks))
			omitted := 0
			for _, lock := range locks {
				if tc.missing != nil && f.CompareLockKeys(lock.Key, *tc.missing) == 0 {
					omitted++
					continue
				}
				selected = append(selected, lock)
			}
			if tc.missing != nil && omitted != 1 {
				t.Fatal("fixture did not select exactly one genuine lock", omitted)
			}
			reached := false
			var appendErr error
			result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				if err := v.raw.AcquireAll(ctx, tx, selected); err != nil {
					return err
				}
				reached = true
				_, appendErr = capture.Service.AppendEventInTx(ctx, tx, v.ownerBrowser.actor, originalEvent, originalPlan)
				return appendErr
			})
			if !reached {
				t.Fatal("did not reach actual Outbox validator")
			}
			if tc.missing == nil {
				if result.State() != f.Committed || appendErr != nil {
					t.Fatal("full genuine plan control rejected", result.Fault(), appendErr)
				}
			} else {
				if result.State() != f.NotCommitted {
					t.Fatal("missing lock committed")
				}
				independentUpdateCode(t, appendErr, f.DependencyUnavailable)
			}
			if projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != stable {
				t.Fatal("lock check changed business facts")
			}
		})
	}
	description = "independent later"
	v.write(t, v.ownerBrowser, "PATCH", path, "ind-later", projectUpdateBody(t, 2, nil, &description)).want(t, 200)
	newerEvent := capture.event
	stable = projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	var staleErr error
	result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.raw.AcquireAll(ctx, tx, originalPlan.Locks()); err != nil {
			return err
		}
		_, staleErr = capture.Service.AppendEventInTx(ctx, tx, v.ownerBrowser.actor, newerEvent, originalPlan)
		return staleErr
	})
	if result.State() != f.NotCommitted {
		t.Fatal("old genuine plan accepted a different real event")
	}
	independentUpdateCode(t, staleErr, f.InvalidArgument)
	if projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != stable {
		t.Fatal("old plan changed facts")
	}
	t.Run("pending-final-rollback-and-original-key", func(t *testing.T) { independentUpdateRollback(t, v) })
	if err := v.core.Logout(testContext(t), account.LogoutRequest{Actor: v.ownerBrowser.actor, Key: f.IdempotencyKey("ind-logout-committed")}); err != nil {
		t.Fatal(err)
	}
	// Logout has actually committed before either access, eliminating lock-race ambiguity.
	v.write(t, v.ownerBrowser, "PATCH", path, "ind-original", originalBody).problem(t, 401, f.SessionRevoked)
	v.write(t, v.ownerBrowser, "POST", projectUpdateLookupPath(v.project.ID), "ind-original", projectUpdateLookupBody).problem(t, 401, f.SessionRevoked)
	var revokedErr error
	result = v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.raw.AcquireAll(ctx, tx, originalPlan.Locks()); err != nil {
			return err
		}
		_, revokedErr = capture.Service.AppendEventInTx(ctx, tx, v.ownerBrowser.actor, originalEvent, originalPlan)
		return revokedErr
	})
	if result.State() != f.NotCommitted {
		t.Fatal("revoked original actor reused prepared event")
	}
	independentUpdateCode(t, revokedErr, f.SessionRevoked)
	t.Log("genuine Outbox plan: full-lock control, three missing locks, different-event rejection, and committed-Logout current authority; private formal fixture, no root injection")
}

func independentUpdateRollback(t *testing.T, v *projectUpdateFixture) {
	proxy := newProjectUpdateHeldProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), false)
	var once sync.Once
	release := func() { once.Do(func() { close(proxy.release) }) }
	defer release()
	u, err := url.Parse(v.db.Fixture.URL(v.db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = proxy.listener.Addr().String()
	raw := openStore(t, v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	tracked := &projectUpdateConfirmStore{projectUsageHTTPStore: &projectUsageHTTPStore{Store: raw}, confirm: make(chan context.Context, 1)}
	service, _, _ := projectUpdateService(t, v.projectUsageHTTPFixture, tracked)
	old := v.handler
	v.install(t, service)
	defer func() { v.handler = old }()
	current, err := v.projectService.GetProject(testContext(t), v.owner, v.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	key := "ind-pending-rollback"
	identity, _ := pc.CommandIdentity(v.project.ID, pc.UpdateCommand, f.IdempotencyKey(key))
	tracked.target = identity.Canonical()
	desc := "independent rolled back writer"
	body := projectUpdateBody(t, current.Version, nil, &desc)
	before := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	var armed atomic.Bool
	tracked.hooks(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != tracked.target || armed.Load() {
			return nil
		}
		x, err := raw.InTx(tx)
		if err != nil {
			return err
		}
		var state string
		var pid int32
		if err = x.QueryRow(ctx, `SELECT state,pg_backend_pid() FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND key=$2`, v.project.ID.String(), key).Scan(&state, &pid); err != nil {
			return err
		}
		if state == "completed" {
			armed.Store(true)
			proxy.targetPID.Store(pid)
		}
		return nil
	}, nil)
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	recorder := &projectUsageRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan bool, 1)
	joined := false
	defer func() {
		cancel()
		release()
		if !joined {
			<-done
		}
	}()
	go func() {
		done <- projectUpdateAbort(func() {
			v.handler.ServeHTTP(recorder, projectUpdateRequest(ctx, v.ownerBrowser, "PATCH", projectOwnerReadPath(v.project.ID), key, body))
		})
	}()
	waitSignal(t, proxy.reached)
	cancel()
	var confirmation context.Context
	select {
	case confirmation = <-tracked.confirm:
	case <-time.After(time.Second):
		t.Fatal("original confirmation absent")
	}
	deadline, ok := confirmation.Deadline()
	if !ok || confirmation.Err() != nil || time.Until(deadline) > 3*time.Second {
		t.Fatal("not live original confirmation")
	}
	command, _ := f.CommandLock(identity)
	managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, command)
	drain, cancelDrain := context.WithTimeout(context.Background(), 20*time.Millisecond)
	service.Stop()
	err = service.Drain(drain)
	cancelDrain()
	if err == nil {
		t.Fatal("held original call reported joined")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("original caller escaped before release")
	default:
	}
	release() // false mode closes the server socket; this signal alone is NOT rollback proof.
	select {
	case aborted := <-done:
		joined = true
		if !aborted || recorder.Body.Len() != 0 {
			t.Fatal("late publication")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("original call did not actually retire")
	}
	if err = service.Drain(testContext(t)); err != nil {
		t.Fatal(err)
	}
	v.handler = old
	observed := v.write(t, v.ownerBrowser, "POST", projectUpdateLookupPath(v.project.ID), key, projectUpdateLookupBody).want(t, 200)
	if observed.object(t)["state"] != "in_progress" || projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != before {
		t.Fatal("serialized lookup did not prove final rollback with durable plan")
	}
	// The actual serializing lookup and unchanged facts establish rollback, not proxy.completed.
	v.write(t, v.ownerBrowser, "PATCH", projectOwnerReadPath(v.project.ID), key, body).want(t, 200)
	after := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
	v.write(t, v.ownerBrowser, "PATCH", projectOwnerReadPath(v.project.ID), key, body).want(t, 200)
	if projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != after {
		t.Fatal("same original key/body/version replay duplicated facts")
	}
}

func TestModelProjectOwnerUpdateIndependentRootAndHistory(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectUpdateFixture(t)
	root := startProjectUsageRoot(t, v.projectUsageHTTPFixture)
	path := projectOwnerReadPath(v.project.ID)
	lookup := projectUpdateLookupPath(v.project.ID)
	description := strings.Repeat("界", 2730) + "ab"
	if len(description) != 8192 {
		t.Fatal("private description bytes")
	}
	body := projectUpdateBody(t, 1, nil, &description)
	first := ownerUpdateIndependentRequest(t, testContext(t), root, v.ownerBrowser, "PATCH", path, "ind-root-original", body).want(t, 200)
	name := "independent-renamed"
	ownerUpdateIndependentRequest(t, testContext(t), root, v.ownerBrowser, "PATCH", path, "ind-root-next", projectUpdateBody(t, 2, &name, nil)).want(t, 200)
	current := root.request(t, v.ownerBrowser, "GET", path, nil).want(t, 200)
	replay := ownerUpdateIndependentRequest(t, testContext(t), root, v.ownerBrowser, "PATCH", path, "ind-root-original", body).want(t, 200)
	history := ownerUpdateIndependentRequest(t, testContext(t), root, v.ownerBrowser, "POST", lookup, "ind-root-original", projectUpdateLookupBody).want(t, 200)
	if current.object(t)["version"] != "3" || !bytes.Equal(first.body, replay.body) || first.object(t)["description"] != description {
		t.Fatal("current projection/history/UTF8 description lost")
	}
	historical, ok := history.object(t)["result"].(map[string]any)
	if !ok {
		t.Fatal("lookup result missing")
	}
	historicalProject, ok := historical["project"].(map[string]any)
	if !ok || historical["command"] != "update" || historicalProject["version"] != "2" {
		t.Fatal("lookup widened or advanced history")
	}
	var createKey string
	if err := v.raw.QueryRow(testContext(t), `SELECT key FROM agenteam_project.creations WHERE project_id=$1`, v.project.ID.String()).Scan(&createKey); err != nil {
		t.Fatal(err)
	}
	otherVerb := ownerUpdateIndependentRequest(t, testContext(t), root, v.ownerBrowser, "POST", lookup, createKey, projectUpdateLookupBody).want(t, 200)
	if otherVerb.object(t)["state"] != "not_observed" {
		t.Fatal("update lookup exposed Create namespace")
	}
	ownerUpdateIndependentRequest(t, testContext(t), root, v.ownerBrowser, "POST", lookup, createKey, []byte(`{"command":"create"}`)).problem(t, 400, f.InvalidArgument)
	root.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, name), nil).want(t, 200)
	root.request(t, v.ownerBrowser, "GET", projectUsageResolve(v.ownerName, v.project.Name), nil).problem(t, 404, f.NotFound)
	dir := os.Getenv("AGENTEAM_PROJECT_UPDATE_BODY_DIR")
	if dir == "" {
		t.Fatal("fixed safe body destination required")
	}
	ownerUpdateIndependentExport(t, dir, "patch", "PATCH", path, v.project.ID.String(), os.Getenv("AGENTEAM_PROJECT_UPDATE_RUN"), os.Getenv("AGENTEAM_PROJECT_UPDATE_CANDIDATE"), "../../api/openapi/project-owner.json", first)
	ownerUpdateIndependentExport(t, dir, "lookup", "POST", lookup, v.project.ID.String(), os.Getenv("AGENTEAM_PROJECT_UPDATE_RUN"), os.Getenv("AGENTEAM_PROJECT_UPDATE_CANDIDATE"), "../../api/openapi/project-owner.json", history)
	root.stop(t)
	if !strings.Contains(root.logs.String(), `"outcome":"drained"`) {
		t.Fatal("public root did not actually drain")
	}
	t.Log("default app.Run without injected ports; current v3 versus original receipt v2; real original response bytes; update-only namespace; root actual drain; in-flight/forced root evidence separately belongs frozen author tests")
}
