//go:build integration

package work_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	workhttp "github.com/LunaDeerTech/agenteam/internal/central/work/http"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

const workOwnerHTTPOrigin = "https://work-owner.example.test"

type workOwnerHTTPProcess struct{ id ac.ProcessID }

func (p workOwnerHTTPProcess) CurrentProcess() ac.ProcessID { return p.id }
func (workOwnerHTTPProcess) ConfirmStopped(context.Context, ac.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

type workOwnerHTTPBrowser struct {
	actor               identity.Actor
	cookie, csrf, email string
}

func (workOwnerHTTPBrowser) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "work_http_browser")
}

type workOwnerHTTPLog struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (l *workOwnerHTTPLog) Write(raw []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.Write(raw)
}
func (l *workOwnerHTTPLog) text() string { l.mu.Lock(); defer l.mu.Unlock(); return l.data.String() }

type workOwnerHTTPFixture struct {
	// Embedded Work helpers may inspect canonical facts and issue real Work
	// commands. Identity positive cases MUST use login/invite below, never the
	// older library fixture's SQL human/renew helpers.
	*blockerFixture
	core                                     *account.Service
	worker                                   *accountmail.Worker
	tracked                                  *hookStore
	password                                 sc.SecretMaterial
	logPath                                  string
	adminBrowser, ownerBrowser, otherBrowser workOwnerHTTPBrowser
	project                                  pc.ProjectRef
	blockerReader                            *work.BlockerReader
	boundary                                 *account.HTTPBoundary
	handler                                  http.Handler
	logs                                     *workOwnerHTTPLog
}

func newWorkOwnerHTTPFixture(t *testing.T) *workOwnerHTTPFixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	return assembleWorkOwnerHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
}

// The explicit Store argument also permits the already accepted complete-frame
// COMMIT proxy, armed only after setup. It never fabricates CommitResult or
// authorization. The caller owns the fresh migrated DB and its real Store.
func assembleWorkOwnerHTTPFixture(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store *hookStore) *workOwnerHTTPFixture {
	t.Helper()
	ak, ck := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	pa, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		t.Fatal(err)
	}
	wa, err := work.NewAuthority(store, pa)
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if err != nil {
		t.Fatal(err)
	}
	// Exactly the same fixed test key material as keys(t); no Model usage
	// router or Object runtime is needed by Account's real secret consumer.
	sk, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), ck)
	if err != nil {
		t.Fatal(err)
	}
	projectSecrets, err := project.NewSecretAuthority(pa)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.New(store, sk, aud, secret.Authorizations{Sessions: accounts, System: accounts, Projects: projectSecrets, Usage: accounts, AccountWrites: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = secrets.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secrets.StopMaintenance)
	catalog := event.NewCatalog()
	revoked, err := ac.DefineSessionsRevoked(catalog)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := ac.DefineDeliveryRequested(catalog)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := wc.RegisterWorkEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	tt, err := wc.RegisterTaskEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	bt, err := wc.RegisterTaskBlockerEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := workOwnerHTTPProcess{id[ac.Process](t)}
	outProcess, err := f.ParseID[oc.Process](process.id.String())
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{ac.AccountProducer: accounts, pc.ProjectProducer: pa, wc.WorkProducer: wa}, Sessions: accounts, System: accounts, Projects: pa, Audit: aud, Cursors: ck, Processes: fixtureProcess{outProcess}})
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
	// Also owns partial construction before Account can assume the sink.
	t.Cleanup(func() {
		sink.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := sink.Drain(ctx); e != nil || !sink.Joined() {
			t.Error("owned recovery sink did not join", e)
		}
	})
	challenges, err := account.NewChallenges(accounts, process.id)
	if err != nil {
		t.Fatal(err)
	}
	core, err := account.New(account.Dependencies{Authority: accounts, Audit: aud, Secrets: secrets, Events: box, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: process, RecoveryLog: sink, Challenges: challenges})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		core.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := core.Drain(ctx)
		cancel()
		if err != nil || !core.Joined() {
			t.Error("formal Account Drain did not join", err)
			forced, stop := context.WithTimeout(context.Background(), 3*time.Second)
			forceErr := core.Force(forced)
			stop()
			if forceErr != nil || !core.Joined() {
				t.Error("formal Account Force did not join", forceErr)
			}
		}
	})
	status, err := core.Bootstrap(ctxFor(t))
	if err != nil || !status.Created || status.LogState != "written" {
		t.Fatal("formal Bootstrap", err)
	}
	password := workOwnerHTTPRecoveryMaterial(t, logPath, "bootstrap", "")
	t.Cleanup(password.Destroy)
	v := &workOwnerHTTPFixture{core: core, tracked: store, password: password, logPath: logPath, logs: &workOwnerHTTPLog{}}
	v.adminBrowser = v.login(t, "admin@mail.com")
	policy, err := outbound.NewPolicyService(store, aud, outbound.Authorizations{Sessions: accounts, System: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = policy.Reload(ctxFor(t)); err != nil {
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
		client.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := client.Drain(ctx); e != nil {
			t.Error("real outbound client did not join", e)
		}
	})
	registry, err := accountmail.NewWorkRegistry(process)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		registry.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := registry.Drain(ctx); e != nil || !registry.Joined() {
			t.Error("real mail material owner did not join", e)
		}
	})
	port, err := account.NewDeliveryPort(core, registry)
	if err != nil {
		t.Fatal(err)
	}
	v.worker, err = accountmail.New(accountmail.Dependencies{Port: port, Registry: registry, Outbound: client, Trust: trust, RecoveryLog: sink, PublicOrigin: workOwnerHTTPOrigin})
	if err != nil {
		t.Fatal(err)
	}
	v.ownerBrowser = v.invite(t, "owner-"+id[struct{}](t).String()[24:])
	v.otherBrowser = v.invite(t, "other-"+id[struct{}](t).String()[24:])
	if _, err = raw.Exec(ctxFor(t), `CREATE SCHEMA work_fixture; CREATE TABLE work_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	skills := &skillFixture{store: store, authority: pa, issuer: pc.NewInitializationPlanIssuer()}
	projects, err := project.New(store, project.Dependencies{Authority: pa, Activity: accounts, Audit: aud, Events: box, ProjectEvents: pt, Initializer: skills, Processes: fixtureProcess{outProcess}, Cursors: ck, LifecycleRegistry: workOwnerHTTPLifecycleRegistry(t)}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		projects.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := projects.Drain(ctx); e != nil {
			t.Error("real Project Drain", e)
		}
	})
	base := &fixture{db: db, raw: raw, store: store, accounts: accounts, projectAuthority: pa, projects: projects, authority: wa, skills: skills, events: box, workEvents: wt, keys: ck}
	base.service = base.newService(t, box, accounts)
	base.reader, err = work.NewReader(store, wa, ck)
	if err != nil {
		t.Fatal(err)
	}
	tasks := &taskFixture{fixture: base, taskEvents: tt}
	tasks.tasks = tasks.newTaskService(t, box, accounts)
	tasks.taskReader, err = work.NewTaskReader(store, wa, base.reader, ck)
	if err != nil {
		t.Fatal(err)
	}
	v.blockerFixture = &blockerFixture{taskFixture: tasks, blockerEvents: bt, blockerOutbox: box}
	v.blockers = v.newBlockerService(t, box, accounts)
	v.blockerReader, err = work.NewBlockerReader(store, wa, ck)
	if err != nil {
		t.Fatal(err)
	}
	v.project, _, _ = base.create(t, v.ownerBrowser.actor, "work-http")
	v.boundary, err = account.NewHTTPBoundary(core, workOwnerHTTPOrigin)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := workhttp.NewHTTPHandler(workhttp.Bindings{Structure: base.service, StructureReader: base.reader, Tasks: tasks.tasks, TaskReader: tasks.taskReader, Blockers: v.blockers, BlockerReader: v.blockerReader}, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), handler)
	t.Log("real Bootstrap/Invitation/Redeem/Login; Project service with persistent test Skills receipt; no Project creation HTTP or production Skills claim")
	return v
}

// Real BeginArchive can accept its complete manifest. The missing participant
// runtimes must never be silently simulated as stopped/cleaned/archived.
func workOwnerHTTPLifecycleRegistry(t *testing.T) *project.LifecycleRegistry {
	t.Helper()
	entries := []pc.ParticipantRegistration{
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []pc.ReferenceKind{"object"}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []pc.ReferenceKind{"secret"}},
		{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []pc.ParticipantName{pc.ArtifactObjectParticipant, pc.SecretParticipant}},
		{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	}
	manifest, err := pc.NewRequiredManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range entries {
		port := &blockerInteropParticipant{name: entry.Name}
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: port})
		t.Cleanup(func() {
			if port.calls.Load() != 0 {
				t.Error("archive acceptance invoked unavailable participant", port.name)
			}
		})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func workOwnerHTTPRecoveryMaterial(t *testing.T, path, purpose, resource string) sc.SecretMaterial {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("owned recovery read failed")
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
				t.Fatal("private invitation URL malformed")
			}
			prefix, token, ok := strings.Cut(u.Fragment, ".")
			if !ok || prefix != resource {
				t.Fatal("private invitation binding mismatch")
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
	t.Fatal("formal private recovery record missing")
	return sc.SecretMaterial{}
}
func (v *workOwnerHTTPFixture) login(t *testing.T, email string) workOwnerHTTPBrowser {
	t.Helper()
	anonymous, err := v.core.NewAnonymousContext(ctxFor(t))
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, err := v.core.VerifyAnonymousContext(ctxFor(t), anonymous.Cookie, anonymous.CSRF)
	if err != nil {
		t.Fatal(err)
	}
	request, err := ac.NewLoginRequest(ac.LoginFields{Browser: browser, Key: f.IdempotencyKey(id[struct{}](t).String()), Email: email, Password: v.password, ClientIP: netip.MustParseAddr("192.0.2.18")})
	if err != nil {
		t.Fatal(err)
	}
	response, err := v.core.Login(ctxFor(t), request)
	if err != nil {
		t.Fatal("formal Login", err)
	}
	closed := false
	defer func() {
		if !closed {
			if e := response.Close(ctxFor(t)); e != nil {
				t.Error("Login response cleanup", e)
			}
		}
	}()
	var cookie sc.SecretMaterial
	if err = response.UseCookie(func(raw []byte) error { var e error; cookie, e = sc.NewSecretMaterial(raw); return e }); err != nil {
		t.Fatal("formal Login cookie")
	}
	defer cookie.Destroy()
	if err = response.Close(ctxFor(t)); err != nil {
		t.Fatal("formal Login response did not close", err)
	}
	closed = true
	actor, err := v.core.Authenticate(ctxFor(t), cookie)
	if err != nil {
		t.Fatal("formal Authenticate", err)
	}
	view, err := v.core.GetSession(ctxFor(t), cookie)
	if err != nil {
		t.Fatal("formal Session", err)
	}
	defer view.CSRF.Destroy()
	out := workOwnerHTTPBrowser{actor: actor, email: email}
	if err = cookie.Use(func(raw []byte) error { out.cookie = string(raw); return nil }); err != nil {
		t.Fatal("owned browser cookie copy")
	}
	if err = view.CSRF.Use(func(raw []byte) error { out.csrf = string(raw); return nil }); err != nil {
		t.Fatal("owned browser CSRF copy")
	}
	return out
}
func (v *workOwnerHTTPFixture) invite(t *testing.T, name string) workOwnerHTTPBrowser {
	t.Helper()
	email := name + "@example.test"
	request, err := ac.NewInvitationCreate(ac.InvitationCreateFields{Actor: v.adminBrowser.actor, Key: f.IdempotencyKey(id[struct{}](t).String()), Email: email})
	if err != nil {
		t.Fatal(err)
	}
	invite, err := v.core.CreateInvitation(ctxFor(t), request)
	if err != nil {
		t.Fatal("formal invitation creation", err)
	}
	if _, err = v.core.ReconcileDeliveryIntents(ctxFor(t)); err != nil {
		t.Fatal("formal delivery reconciliation", err)
	}
	if err = v.worker.RunJob(ctxFor(t), invite.JobID); err != nil {
		t.Fatal("formal recovery-log delivery", err)
	}
	material := workOwnerHTTPRecoveryMaterial(t, v.logPath, "invitation", invite.ID.String())
	defer material.Destroy()
	token, err := ac.NewInvitationToken(invite.ID, material)
	if err != nil {
		t.Fatal("formal invitation token", err)
	}
	anonymous, err := v.core.NewAnonymousContext(ctxFor(t))
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, err := v.core.VerifyAnonymousContext(ctxFor(t), anonymous.Cookie, anonymous.CSRF)
	if err != nil {
		t.Fatal(err)
	}
	redeem, err := ac.NewInvitationRedeem(ac.RedeemFields{Browser: browser, Key: f.IdempotencyKey(id[struct{}](t).String()), Token: token, Username: name, DisplayName: "Work owner", Password: v.password, Confirmation: v.password})
	if err != nil {
		t.Fatal(err)
	}
	result, err := v.core.RedeemInvitation(ctxFor(t), redeem)
	if err != nil || !result.Completed {
		t.Fatal("formal invitation redeem", err)
	}
	return v.login(t, email)
}

// This recorder supplies capability methods only for the PG business matrix.
// It does not establish native deadlines, TCP EOF, connection reuse or app Join.
type workOwnerHTTPRecorder struct{ *httptest.ResponseRecorder }

func (*workOwnerHTTPRecorder) SetReadDeadline(time.Time) error  { return nil }
func (*workOwnerHTTPRecorder) SetWriteDeadline(time.Time) error { return nil }
func (w *workOwnerHTTPRecorder) FlushError() error              { w.Flush(); return nil }

type workOwnerHTTPResponse struct {
	status  int
	header  http.Header
	body    []byte
	aborted bool
}

func workOwnerHTTPRequest(ctx context.Context, browser workOwnerHTTPBrowser, method, path, body string, key f.IdempotencyKey) *http.Request {
	r := httptest.NewRequest(method, workOwnerHTTPOrigin+path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Origin", workOwnerHTTPOrigin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if browser.cookie != "" {
		r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: browser.cookie})
	}
	if browser.csrf != "" {
		r.Header.Set("X-CSRF-Token", browser.csrf)
	}
	if method != "GET" && method != "HEAD" {
		r.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", string(key))
	}
	return r
}
func (v *workOwnerHTTPFixture) request(t *testing.T, browser workOwnerHTTPBrowser, method, path, body string, key f.IdempotencyKey) workOwnerHTTPResponse {
	t.Helper()
	return v.serve(workOwnerHTTPRequest(ctxFor(t), browser, method, path, body, key))
}
func (v *workOwnerHTTPFixture) serve(r *http.Request) (result workOwnerHTTPResponse) {
	w := &workOwnerHTTPRecorder{httptest.NewRecorder()}
	defer func() {
		if p := recover(); p != nil {
			if p != http.ErrAbortHandler {
				panic(p)
			}
			result.aborted = true
		}
		result.status = w.Code
		result.header = w.Header().Clone()
		result.body = bytes.Clone(w.Body.Bytes())
	}()
	v.handler.ServeHTTP(w, r)
	return result
}
func workOwnerHTTPPath(project pc.ProjectID, suffix string) string {
	return "/api/v1/projects/" + project.String() + suffix
}
