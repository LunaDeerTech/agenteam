//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	auditc "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	variablehttp "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/http"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const variableHTTPOrigin = "https://variable-owner.example.test"

type variableHTTPProcess struct{ id ac.ProcessID }

func (p variableHTTPProcess) CurrentProcess() ac.ProcessID { return p.id }
func (variableHTTPProcess) ConfirmStopped(context.Context, ac.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

type variableHTTPBrowser struct {
	actor               identity.Actor
	cookie, csrf, email string
}

func (variableHTTPBrowser) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "work_http_browser")
}

type variableHTTPLog struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (l *variableHTTPLog) Write(raw []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.Write(raw)
}
func (l *variableHTTPLog) text() string { l.mu.Lock(); defer l.mu.Unlock(); return l.data.String() }

type variableHTTPFixture struct {
	db                                       *pgfixture.Database
	raw                                      *postgres.Store
	accounts                                 *account.Authority
	projectAuthority                         *project.Authority
	projects                                 *project.Service
	authority                                *pv.Authority
	service                                  *pv.Service
	skills                                   *skillFixture
	events                                   *outbox.Service
	variableEvents                           vc.VariableEvents
	audit                                    *audit.Service
	keys                                     cursor.Keyring
	core                                     *account.Service
	worker                                   *accountmail.Worker
	tracked                                  *hookStore
	password                                 sc.SecretMaterial
	logPath                                  string
	adminBrowser, ownerBrowser, otherBrowser variableHTTPBrowser
	project                                  pc.ProjectRef
	boundary                                 *account.HTTPBoundary
	handler                                  http.Handler
	logs                                     *variableHTTPLog
}

func newVariableHTTPFixture(t *testing.T) *variableHTTPFixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	return assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
}

// The explicit Store argument also permits the already accepted complete-frame
// COMMIT proxy, armed only after setup. It never fabricates CommitResult or
// authorization. The caller owns the fresh migrated DB and its real Store.
func assembleVariableHTTPFixture(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store *hookStore) *variableHTTPFixture {
	t.Helper()
	ak, ck := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	va, err := pv.NewAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	pa, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, AuditFacts: map[auditc.Producer]auditc.ProjectFactAuthority{auditc.ProjectVariableProducer: va}})
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
	vt, err := vc.RegisterVariableEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := variableHTTPProcess{id[ac.Process](t)}
	outProcess, err := f.ParseID[oc.Process](process.id.String())
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{ac.AccountProducer: accounts, pc.ProjectProducer: pa, vc.VariableProducer: va}, Sessions: accounts, System: accounts, Projects: pa, Audit: aud, Cursors: ck, Processes: fixtureProcess{outProcess}})
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
	password := variableHTTPRecoveryMaterial(t, logPath, "bootstrap", "")
	t.Cleanup(password.Destroy)
	v := &variableHTTPFixture{db: db, raw: raw, accounts: accounts, projectAuthority: pa, authority: va, events: box, variableEvents: vt, audit: aud, keys: ck, core: core, tracked: store, password: password, logPath: logPath, logs: &variableHTTPLog{}}
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
	v.worker, err = accountmail.New(accountmail.Dependencies{Port: port, Registry: registry, Outbound: client, Trust: trust, RecoveryLog: sink, PublicOrigin: variableHTTPOrigin})
	if err != nil {
		t.Fatal(err)
	}
	v.ownerBrowser = v.invite(t, "owner-"+id[struct{}](t).String()[24:])
	v.otherBrowser = v.invite(t, "other-"+id[struct{}](t).String()[24:])
	if _, err = raw.Exec(ctxFor(t), `CREATE SCHEMA variable_fixture; CREATE TABLE variable_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	skills := &skillFixture{store: store, authority: pa, issuer: pc.NewInitializationPlanIssuer()}
	projects, err := project.New(store, project.Dependencies{Authority: pa, Activity: accounts, Audit: aud, Events: box, ProjectEvents: pt, Initializer: skills, Processes: fixtureProcess{outProcess}, Cursors: ck, LifecycleRegistry: variableHTTPLifecycleRegistry(t)}, project.DefaultConfig())
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
	v.projects, v.skills = projects, skills
	v.service = v.newService(t, box, accounts)
	v.project, _, _ = v.createProject(t, v.ownerBrowser.actor, "variable-http")
	v.boundary, err = account.NewHTTPBoundary(core, variableHTTPOrigin)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := variablehttp.NewHTTPHandler(v.service, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), handler)
	t.Log("real Bootstrap/Invitation/Redeem/Login; Project service with persistent test Skills receipt; no Project creation HTTP or production Skills claim")
	return v
}

// Real BeginArchive can accept its complete manifest. The missing participant
// runtimes must never be silently simulated as stopped/cleaned/archived.
func variableHTTPLifecycleRegistry(t *testing.T) *project.LifecycleRegistry {
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
		port := &unavailableParticipant{name: entry.Name}
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

func variableHTTPRecoveryMaterial(t *testing.T, path, purpose, resource string) sc.SecretMaterial {
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
func (v *variableHTTPFixture) login(t *testing.T, email string) variableHTTPBrowser {
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
	out := variableHTTPBrowser{actor: actor, email: email}
	if err = cookie.Use(func(raw []byte) error { out.cookie = string(raw); return nil }); err != nil {
		t.Fatal("owned browser cookie copy")
	}
	if err = view.CSRF.Use(func(raw []byte) error { out.csrf = string(raw); return nil }); err != nil {
		t.Fatal("owned browser CSRF copy")
	}
	return out
}
func (v *variableHTTPFixture) invite(t *testing.T, name string) variableHTTPBrowser {
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
	material := variableHTTPRecoveryMaterial(t, v.logPath, "invitation", invite.ID.String())
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
	redeem, err := ac.NewInvitationRedeem(ac.RedeemFields{Browser: browser, Key: f.IdempotencyKey(id[struct{}](t).String()), Token: token, Username: name, DisplayName: "Variable owner", Password: v.password, Confirmation: v.password})
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
type variableHTTPRecorder struct{ *httptest.ResponseRecorder }

func (*variableHTTPRecorder) SetReadDeadline(time.Time) error  { return nil }
func (*variableHTTPRecorder) SetWriteDeadline(time.Time) error { return nil }
func (w *variableHTTPRecorder) FlushError() error              { w.Flush(); return nil }

type variableHTTPResponse struct {
	status  int
	header  http.Header
	body    []byte
	aborted bool
}

func variableHTTPRequest(ctx context.Context, browser variableHTTPBrowser, method, path, body string, key f.IdempotencyKey) *http.Request {
	r := httptest.NewRequest(method, variableHTTPOrigin+path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Origin", variableHTTPOrigin)
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
func (v *variableHTTPFixture) request(t *testing.T, browser variableHTTPBrowser, method, path, body string, key f.IdempotencyKey) variableHTTPResponse {
	t.Helper()
	return v.serve(variableHTTPRequest(ctxFor(t), browser, method, path, body, key))
}
func (v *variableHTTPFixture) serve(r *http.Request) (result variableHTTPResponse) {
	w := &variableHTTPRecorder{httptest.NewRecorder()}
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
func variableHTTPPath(project pc.ProjectID, suffix string) string {
	return "/api/v1/projects/" + project.String() + suffix
}

// All positive identity facts come from the real Account service above.
// These callback hooks observe the real Store and never replace CommitResult.
type fixtureStore interface {
	project.Store
	audit.Store
	pv.Store
}
type hookStore struct {
	fixtureStore
	mu          sync.Mutex
	after       func(context.Context, f.Tx, f.TransactionCause) error
	afterResult func(f.TransactionCause, f.CommitResult)
	beforeLocks func(context.Context, f.Tx, []f.LockRequest) error
}

func (s *hookStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	after := s.after
	afterResult := s.afterResult
	s.mu.Unlock()
	result := s.fixtureStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if after != nil {
			return after(ctx, tx, cause)
		}
		return nil
	})
	if afterResult != nil {
		afterResult(cause, result)
	}
	return result
}
func (s *hookStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	before := s.beforeLocks
	s.mu.Unlock()
	if before != nil {
		if err := before(ctx, tx, locks); err != nil {
			return err
		}
	}
	return s.fixtureStore.AcquireAll(ctx, tx, locks)
}
func (s *hookStore) setAfter(fn func(context.Context, f.Tx, f.TransactionCause) error) {
	s.mu.Lock()
	s.after = fn
	s.mu.Unlock()
}

func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func id[K any](t *testing.T) f.ID[K] {
	t.Helper()
	v, e := f.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func meta(t *testing.T, key string, version *f.Version) f.CommandMeta {
	return f.CommandMeta{RequestID: id[f.Request](t), IdempotencyKey: f.IdempotencyKey(key), ExpectedVersion: version}
}
func requireCode(t *testing.T, e error, want f.Code) {
	t.Helper()
	var f *f.Fault
	if !errors.As(e, &f) || f.Code != want {
		t.Fatalf("error=%v want=%s", e, want)
	}
}
func cause(t *testing.T) f.TransactionCause {
	v, e := f.NewRecoveryCause("projectvariable.fixture", id[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return v
}

type fixtureProcess struct{ id oc.ProcessID }

func (p fixtureProcess) CurrentProcess() oc.ProcessID { return p.id }
func (p fixtureProcess) ConfirmStopped(context.Context, oc.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

func keys(t *testing.T) (account.Keyring, cursor.Keyring) {
	t.Helper()
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	cursor, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	secret, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), cursor)
	if e != nil {
		t.Fatal(e)
	}
	download, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), cursor, secret)
	if e != nil {
		t.Fatal(e)
	}
	account, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), cursor, secret, download)
	if e != nil {
		t.Fatal(e)
	}
	return account, cursor
}
func openStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	s, e := postgres.Open(ctxFor(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e = s.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	return s
}

type skillFixture struct {
	store     project.Store
	authority *project.Authority
	issuer    pc.InitializationPlanIssuer
	mu        sync.Mutex
	mode      string
}

func (s *skillFixture) setMode(mode string) { s.mu.Lock(); s.mode = mode; s.mu.Unlock() }
func (s *skillFixture) InspectProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	if actor.Details().ServiceName != identity.ProjectInitialization || actor.Details().CauseRef != r.CreationID.String() || actor.Details().ProjectID != r.ProjectID.String() {
		return pc.InitializationResult{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	var project, key, skill string
	var revision int64
	e := s.store.QueryRow(ctx, `SELECT project_id::text,init_key,skill_id::text,revision FROM variable_fixture.skills WHERE creation_id=$1`, r.CreationID.String()).Scan(&project, &key, &skill, &revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return pc.InitializationResult{State: pc.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: pc.ReasonWorkPending}, nil
	}
	if e != nil {
		return pc.InitializationResult{}, e
	}
	if project != r.ProjectID.String() || key != string(r.InitializationKey) {
		return pc.InitializationResult{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	skillID, e := f.ParseID[pc.Skill](skill)
	if e != nil {
		return pc.InitializationResult{}, e
	}
	rev := f.Revision(revision)
	return pc.InitializationResult{State: pc.InitializationCompleted, CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: &skillID, Revision: &rev}, nil
}
func (s *skillFixture) InitializeProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	s.mu.Lock()
	mode := s.mode
	s.mu.Unlock()
	if mode == "pending" {
		return pc.InitializationResult{State: pc.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: pc.ReasonWorkPending}, nil
	}
	projectKey, _ := f.ProjectLock(r.ProjectID.String())
	command, _ := f.NewRecoveryCause("project.fixture-skill", r.CreationID.String(), "")
	result := s.store.WithinTx(ctx, command, func(ctx context.Context, tx f.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: projectKey, Mode: f.Exclusive}}); e != nil {
			return e
		}
		if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
			return e
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return e
		}
		skill, e := f.NewID[pc.Skill]()
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO variable_fixture.skills(creation_id,project_id,init_key,skill_id,revision,protected,published) VALUES($1,$2,$3,$4,1,true,true) ON CONFLICT(creation_id) DO NOTHING`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), skill.String())
		return e
	})
	if result.State() != f.Committed {
		return pc.InitializationResult{}, f.NewFault(f.DependencyUnavailable, result.State())
	}
	return s.InspectProjectSkills(ctx, actor, r)
}
func (s *skillFixture) DiscoverConfirmation(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationConfirmationPlan, error) {
	result, e := s.InspectProjectSkills(ctx, actor, r)
	if e != nil {
		return pc.InitializationConfirmationPlan{}, e
	}
	if result.State != pc.InitializationCompleted {
		return pc.InitializationConfirmationPlan{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	key, _ := f.ProjectLock(r.ProjectID.String())
	locks := []f.LockRequest{{Key: key, Mode: f.Exclusive}}
	return s.issuer.Plan(actor, r, pc.InitializationReceipt{CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}, locks)
}
func (s *skillFixture) ConfirmInitializedInTx(ctx context.Context, tx f.Tx, actor identity.Actor, r pc.InitializationRequest, plan pc.InitializationConfirmationPlan) (pc.InitializationReceipt, error) {
	if !s.issuer.Matches(plan, actor, r) {
		return pc.InitializationReceipt{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	locks := plan.RequiredLocks()
	if e := s.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return pc.InitializationReceipt{}, e
	}
	if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
		return pc.InitializationReceipt{}, e
	}
	x, e := s.store.InTx(tx)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	receipt := plan.ProposedReceipt()
	var valid bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM variable_fixture.skills WHERE creation_id=$1 AND project_id=$2 AND init_key=$3 AND skill_id=$4 AND revision=$5 AND protected AND published)`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), receipt.AddSkillsID.String(), int64(receipt.Revision)).Scan(&valid)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	if !valid {
		return pc.InitializationReceipt{}, f.NewFault(f.InvalidState, f.NotStarted)
	}
	return receipt, nil
}

func (v *variableHTTPFixture) createProject(t *testing.T, actor identity.Actor, name string) (pc.ProjectRef, f.CommandMeta, pc.CreateProjectRequest) {
	t.Helper()
	request := pc.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: name, Description: "private body"}
	m := meta(t, id[struct{}](t).String(), nil)
	result, e := v.projects.CreateProject(ctxFor(t), actor, m, request)
	if e != nil || result.State != pc.CreationReady {
		t.Fatal("create ready", result.State, e)
	}
	return *result.Project, m, request
}

func migrationFiles(t *testing.T, through string) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name[:5] > through {
			continue
		}
		raw, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	return files
}
func migrate(t *testing.T, db *pgfixture.Database, sources ...postgres.Source) {
	t.Helper()
	m, err := postgres.NewMigrator(db.Config(t, nil), sources...)
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(ctxFor(t)); !result.Migrated {
		// This isolated fixture has only fixed migration SQL at this stage.
		// Keep connection data, parameters, Detail and the raw cause private.
		var pg *pgconn.PgError
		if errors.As(result.Fault, &pg) {
			t.Logf("project migration pgerror sqlstate=%s message=%.512q constraint=%.128q position=%d", pg.Code, pg.Message, pg.ConstraintName, pg.Position)
		}
		t.Fatal("project migration", result.Fault)
	}
}
func migrationPrefix(t *testing.T, through string) postgres.Source {
	t.Helper()
	source, err := postgres.NewSource(migrationFiles(t, through), nil)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

type unavailableParticipant struct {
	name  pc.ParticipantName
	calls atomic.Int32
}

func (p *unavailableParticipant) Name() pc.ParticipantName { return p.name }
func (p *unavailableParticipant) RequestStop(context.Context, identity.Actor, pc.LifecycleCause, pc.ScopeRef) (pc.StopReport, error) {
	p.calls.Add(1)
	return pc.StopReport{}, errors.New("archive acceptance must not invoke RequestStop")
}
func (p *unavailableParticipant) InspectStop(context.Context, identity.Actor, pc.LifecycleCause, pc.ScopeRef) (pc.StopReport, error) {
	p.calls.Add(1)
	return pc.StopReport{}, errors.New("archive acceptance must not invoke InspectStop")
}
func (p *unavailableParticipant) Cleanup(context.Context, identity.Actor, pc.LifecycleCause, pc.ScopeRef, *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	p.calls.Add(1)
	return pc.CleanupReport{}, errors.New("archive acceptance must not invoke Cleanup")
}

func newDatabase(t *testing.T) *pgfixture.Database {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	migrate(t, db)
	return db
}
func (v *variableHTTPFixture) newService(t *testing.T, appender oc.Appender, activity pv.ActivityAuthority) *pv.Service {
	t.Helper()
	s, err := pv.New(v.tracked, pv.Dependencies{Authority: v.authority, Projects: v.projectAuthority, Events: appender, VariableEvents: v.variableEvents, Audit: v.audit, Activity: activity, Cursors: v.keys})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error("variable calls did not join", err)
		}
	})
	return s
}
func (v *variableHTTPFixture) bindService(t *testing.T, s *pv.Service) {
	t.Helper()
	h, err := variablehttp.NewHTTPHandler(s, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
}
func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	b, e := json.Marshal(value)
	if e != nil {
		t.Fatal("safe fixture encoding failure")
	}
	return b
}
func createInput(t *testing.T, name, value string) vc.VariableCreate {
	t.Helper()
	r, e := vc.NewVariableCreate(vc.VariableCreateFields{ID: id[identity.ProjectVariable](t), Name: name, Description: "private description", Value: value})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func updateInput(t *testing.T, fields vc.VariableUpdateFields) vc.VariableUpdate {
	t.Helper()
	r, e := vc.NewVariableUpdate(fields)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (v *variableHTTPFixture) createVariable(t *testing.T, name, value string) vc.Variable {
	t.Helper()
	out, e := v.service.CreateVariable(ctxFor(t), v.ownerBrowser.actor, meta(t, id[struct{}](t).String(), nil), v.project.ID, createInput(t, name, value))
	if e != nil {
		t.Fatal("create variable", e)
	}
	return out.Fields().Variable
}
