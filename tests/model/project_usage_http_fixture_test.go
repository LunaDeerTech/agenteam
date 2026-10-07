//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	usagehttp "github.com/LunaDeerTech/agenteam/internal/central/usage/http"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// This Store changes only the returned terminal observation in explicitly armed
// tests. Every query, lock and transaction still belongs to the real Store.
type projectUsageHTTPStore struct {
	*postgres.Store
	mu       sync.Mutex
	after    func(context.Context, f.Tx, f.TransactionCause) error
	terminal func(f.TransactionCause, f.CommitResult) f.CommitResult
}

func (s *projectUsageHTTPStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	after, terminal := s.after, s.terminal
	s.mu.Unlock()
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if after != nil {
			return after(ctx, tx, cause)
		}
		return nil
	})
	if terminal != nil {
		return terminal(cause, result)
	}
	return result
}
func (s *projectUsageHTTPStore) hooks(after func(context.Context, f.Tx, f.TransactionCause) error, terminal func(f.TransactionCause, f.CommitResult) f.CommitResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.after, s.terminal = after, terminal
}
func projectUsageReadCause(cause f.TransactionCause) bool {
	return cause.Kind() == f.RecoveryCause && strings.HasPrefix(cause.Details().Owner, "model.usage.")
}

type projectUsageHTTPFixture struct {
	*usageFixture
	core                                     *account.Service
	worker                                   *accountmail.Worker
	password                                 sc.SecretMaterial
	logPath                                  string
	adminBrowser, ownerBrowser, otherBrowser systemHTTPBrowser
	ownerName                                string
	tracked                                  *projectUsageHTTPStore
	handler                                  http.Handler
	logs                                     *systemHTTPLog
}

// Unlike the older library constructors, this glue creates every identity with
// Bootstrap/Invitation/Redeem/Login, including the controlled wire policy actor.
func newProjectUsageHTTPFixture(t *testing.T) *projectUsageHTTPFixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	tracked := &projectUsageHTTPStore{Store: raw}
	base := assembleProjectConfiguration(t, db, raw, tracked)
	_, ck, sk := testKeys(t)
	catalog := ec.NewCatalog()
	revoked, err := ac.DefineSessionsRevoked(catalog)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := ac.DefineDeliveryRequested(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := systemHTTPAccountProcess{newID[ac.Process](t)}
	processID, _ := f.ParseID[oc.Process](process.id.String())
	events, err := outbox.New(tracked, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{ac.AccountProducer: base.accounts}, Sessions: base.accounts, System: base.accounts, Audit: base.aud, Cursors: ck, Processes: liveProcess{processID}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "account-recovery.jsonl")
	sink, err := recoverylog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	challenges, err := account.NewChallenges(base.accounts, process.id)
	if err != nil {
		t.Fatal(err)
	}
	core, err := account.New(account.Dependencies{Authority: base.accounts, Audit: base.aud, Secrets: base.secrets, Events: events, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: process, RecoveryLog: sink, Challenges: challenges})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		core.StopAdmission()
		drainErr := core.Drain(ctx)
		cancel()
		if drainErr != nil || !core.Joined() {
			t.Error("formal Account Drain did not join", drainErr)
			// Force owns a fresh cleanup lifecycle. A context return can precede
			// forceDone, which Joined does not cover. Wait for that same Force
			// coordinator before waiting for the remaining local ownership.
			forceCtx, forceCancel := context.WithTimeout(context.Background(), 3*time.Second)
			forceErr := core.Force(forceCtx)
			forceCancel()
			if forceErr != nil || !core.Joined() {
				t.Error("formal Account Force incomplete; retaining actual join ownership", forceErr)
			}
			if err := core.Force(context.Background()); err != nil {
				t.Error("formal Account Force completed with error", err)
			}
			for !core.Joined() {
				time.Sleep(10 * time.Millisecond)
			}
		}
	})
	status, err := core.Bootstrap(testContext(t))
	if err != nil || !status.Created || status.LogState != "written" {
		t.Fatal("formal Bootstrap failed", err)
	}
	password := projectUsageRecoveryMaterial(t, logPath, "bootstrap", "")
	v := &projectUsageHTTPFixture{core: core, password: password, logPath: logPath, tracked: tracked, logs: &systemHTTPLog{}}
	t.Cleanup(password.Destroy)
	v.adminBrowser = v.login(t, "admin@mail.com")
	base.admin = v.adminBrowser.actor
	policy, err := outbound.NewPolicyService(tracked, base.aud, outbound.Authorizations{Sessions: base.accounts, System: base.accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = policy.Reload(testContext(t)); err != nil {
		t.Fatal(err)
	}
	trust, err := outbound.LoadTrustStore("")
	if err != nil {
		t.Fatal(err)
	}
	client, err := outbound.NewClient(policy, trust, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		client.StopAdmission()
		if e := client.Drain(ctx); e != nil {
			t.Error("mail transport cleanup", e)
		}
	})
	registry, err := accountmail.NewWorkRegistry(process)
	if err != nil {
		t.Fatal(err)
	}
	port, err := account.NewDeliveryPort(core, registry)
	if err != nil {
		t.Fatal(err)
	}
	v.worker, err = accountmail.New(accountmail.Dependencies{Port: port, Registry: registry, Outbound: client, Trust: trust, RecoveryLog: sink, PublicOrigin: systemHTTPOrigin})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		registry.StopAdmission()
		if e := registry.Drain(ctx); e != nil || !registry.Joined() {
			t.Error("mail material owner did not join", e)
		}
	})
	v.ownerName = "owner-" + newID[struct{}](t).String()[24:]
	v.ownerBrowser = v.invite(t, v.ownerName)
	v.otherBrowser = v.invite(t, "other-"+newID[struct{}](t).String()[24:])
	base.regular, base.owner = v.ownerBrowser.actor, v.ownerBrowser.actor
	base.project = base.createProject(t, base.owner)
	base.scope, _ = id.InProject(base.project.ID)
	_, err = raw.Exec(testContext(t), `CREATE SCHEMA model_resolution_fixture; CREATE TABLE model_resolution_fixture.consumers(unit text PRIMARY KEY,request_data jsonb NOT NULL,version bigint NOT NULL,enabled boolean NOT NULL); CREATE TABLE model_resolution_fixture.inputs(unit text PRIMARY KEY,snapshot_id uuid NOT NULL);
CREATE SCHEMA usage_runtime_fixture; CREATE TABLE usage_runtime_fixture.calls(id uuid PRIMARY KEY,data jsonb NOT NULL,version bigint NOT NULL);
CREATE TABLE usage_runtime_fixture.attempts(id uuid PRIMARY KEY,call_id uuid NOT NULL,identity_data jsonb NOT NULL); CREATE UNIQUE INDEX usage_runtime_attempt_ordinal ON usage_runtime_fixture.attempts(call_id,(identity_data->>'attempt_index'));
CREATE TABLE usage_runtime_fixture.events(invocation_id uuid NOT NULL,sequence bigint NOT NULL,data jsonb NOT NULL,version bigint NOT NULL,PRIMARY KEY(invocation_id,sequence));`)
	if err != nil {
		t.Fatal(err)
	}
	resolved := bindCurrentResolution(t, base)
	observed := &usageObservedStore{Store: tracked}
	runtimeOwner := &usageRuntime{store: observed, base: resolved, issuer: uc.NewPlanIssuer(), secretIssuer: sc.NewPlanIssuer()}
	authority, err := usage.NewAuthority(observed, usage.Authorizations{Sessions: base.accounts, Projects: base.projects, Invocations: runtimeOwner})
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := usage.New(observed, authority, usage.Dependencies{Cursors: ck})
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.Initialize(testContext(t)); err != nil {
		t.Fatal(err)
	}
	projectSecrets, err := project.NewSecretAuthority(base.projects)
	if err != nil {
		t.Fatal(err)
	}
	reads, err := secret.New(observed, sk, base.aud, secret.Authorizations{Sessions: base.accounts, System: base.accounts, Projects: projectSecrets, Usage: runtimeOwner, AccountWrites: base.accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = reads.Initialize(testContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reads.StopMaintenance)
	v.usageFixture = &usageFixture{currentResolutionFixture: resolved, runtimeOwner: runtimeOwner, ledger: ledger, reads: reads, storeView: observed}
	v.wire = projectUsageWireFixture(t, policy, v.adminBrowser.actor)
	v.wire.allow(t, true)
	readAuthority, err := usage.NewAuthority(tracked, usage.Authorizations{Sessions: base.accounts, Projects: base.projects, Invocations: nil})
	if err != nil {
		t.Fatal(err)
	}
	readService, err := usage.New(tracked, readAuthority, usage.Dependencies{Cursors: ck})
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := account.NewHTTPBoundary(core, systemHTTPOrigin)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := usagehttp.NewHTTPHandler(base.projects, readService, boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), handler)
	return v
}

// Recovery bytes are read only from this fixture's private sink and never
// included in failure text, ordinary logs or retained evidence artifacts.
func projectUsageRecoveryMaterial(t *testing.T, path, purpose, resource string) sc.SecretMaterial {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("owned recovery read")
	}
	defer clear(data)
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var row struct {
			Purpose  string `json:"purpose"`
			ID       string `json:"id"`
			Password string `json:"initial_password"`
			URL      string `json:"url"`
		}
		if json.Unmarshal(line, &row) != nil || row.Purpose != purpose || resource != "" && row.ID != resource {
			continue
		}
		value := row.Password
		if purpose == "invitation" {
			u, e := url.Parse(row.URL)
			if e != nil {
				t.Fatal("private invite URL malformed")
			}
			prefix, token, ok := strings.Cut(u.Fragment, ".")
			if !ok || prefix != resource {
				t.Fatal("private invite binding")
			}
			value = token
		}
		material, e := sc.NewSecretMaterial([]byte(value))
		row.Password, row.URL, value = "", "", ""
		if e != nil {
			t.Fatal("private recovery material malformed")
		}
		return material
	}
	t.Fatal("formal recovery record missing")
	return sc.SecretMaterial{}
}
func (v *projectUsageHTTPFixture) login(t *testing.T, email string) systemHTTPBrowser {
	t.Helper()
	helper := &systemHTTPFixture{account: v.core, password: v.password}
	return helper.login(t, email)
}
func (v *projectUsageHTTPFixture) invite(t *testing.T, name string) systemHTTPBrowser {
	t.Helper()
	email := name + "@example.test"
	request, err := ac.NewInvitationCreate(ac.InvitationCreateFields{Actor: v.adminBrowser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String()), Email: email})
	if err != nil {
		t.Fatal(err)
	}
	invite, err := v.core.CreateInvitation(testContext(t), request)
	if err != nil {
		t.Fatal("formal invitation create", err)
	}
	if _, err = v.core.ReconcileDeliveryIntents(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if err = v.worker.RunJob(testContext(t), invite.JobID); err != nil {
		t.Fatal("formal recovery delivery", err)
	}
	material := projectUsageRecoveryMaterial(t, v.logPath, "invitation", invite.ID.String())
	defer material.Destroy()
	token, err := ac.NewInvitationToken(invite.ID, material)
	if err != nil {
		t.Fatal(err)
	}
	anonymous, err := v.core.NewAnonymousContext(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	redeem, err := ac.NewInvitationRedeem(ac.RedeemFields{Browser: anonymous.Identity, Key: f.IdempotencyKey(newID[struct{}](t).String()), Token: token, Username: name, DisplayName: "Usage owner", Password: v.password, Confirmation: v.password})
	if err != nil {
		t.Fatal(err)
	}
	result, err := v.core.RedeemInvitation(testContext(t), redeem)
	if err != nil || !result.Completed {
		t.Fatal("formal invitation redeem", err)
	}
	return v.login(t, email)
}
func projectUsageWireFixture(t *testing.T, policy *outbound.PolicyService, actor id.Actor) *wireFixture {
	t.Helper()
	descriptor, err := netfixture.Load()
	if err != nil {
		t.Fatal("owned wire fixture unavailable")
	}
	trust, err := outbound.LoadTrustStore(descriptor.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	v := &wireFixture{net: descriptor, policy: policy, actor: actor, transport: wire.Transport{Policy: policy, Trust: trust, Resolver: wireResolver{netip.MustParseAddr(descriptor.PrivateIP)}}}
	v.budget = wire.NewBudget()
	v.adapter = v.newAdapter(t, v.budget)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		joined := true
		for _, b := range v.budgets {
			if e := b.Force(ctx); e != nil || !b.Joined() {
				joined = false
				t.Error("wire did not actually join", e)
			}
		}
		if joined {
			for _, m := range v.materials {
				m.Destroy()
			}
		}
		for _, key := range v.cases {
			v.settled(t, key)
		}
	})
	return v
}

type projectUsageRecorder struct {
	*httptest.ResponseRecorder
	mu     sync.Mutex
	writes int
}

func (w *projectUsageRecorder) SetReadDeadline(time.Time) error  { return nil }
func (w *projectUsageRecorder) SetWriteDeadline(time.Time) error { return nil }
func (w *projectUsageRecorder) FlushError() error                { w.Flush(); return nil }
func (w *projectUsageRecorder) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	return w.ResponseRecorder.Write(b)
}
func (w *projectUsageRecorder) writeCount() int { w.mu.Lock(); defer w.mu.Unlock(); return w.writes }
func projectUsageRequest(ctx context.Context, browser systemHTTPBrowser, method, path string) *http.Request {
	r := httptest.NewRequest(method, systemHTTPOrigin+path, nil).WithContext(ctx)
	r.Header.Set("Origin", systemHTTPOrigin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if browser.cookie != "" {
		r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: browser.cookie})
	}
	return r
}
func (v *projectUsageHTTPFixture) request(t *testing.T, browser systemHTTPBrowser, method, path string) systemHTTPResponse {
	t.Helper()
	w := &projectUsageRecorder{ResponseRecorder: httptest.NewRecorder()}
	v.handler.ServeHTTP(w, projectUsageRequest(testContext(t), browser, method, path))
	return systemHTTPResponse{w.Code, w.Header().Clone(), bytes.Clone(w.Body.Bytes())}
}
func projectUsagePath(project id.ProjectID) string {
	return "/api/v1/projects/" + project.String() + "/model-usage"
}
func projectUsageResolve(username, name string) string {
	return "/api/v1/projects/resolve?" + url.Values{"username": {username}, "project_name": {name}}.Encode()
}
func projectUsageRename(t *testing.T, v *projectUsageHTTPFixture, name string) pc.ProjectRef {
	t.Helper()
	version := v.project.Version
	ref, err := v.projectService.UpdateProject(testContext(t), v.owner, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: f.IdempotencyKey(newID[struct{}](t).String()), ExpectedVersion: &version}, v.project.ID, pc.UpdateProjectRequest{Name: &name})
	if err != nil {
		t.Fatal("formal project rename", err)
	}
	v.project = ref
	return ref
}
func projectUsageComplete(t *testing.T, started time.Time) {
	t.Helper()
	if time.Since(started) > 2*time.Minute {
		t.Error("Usage HTTP top-level exceeded two-minute budget")
	}
}

var _ io.Writer = (*projectUsageRecorder)(nil)
