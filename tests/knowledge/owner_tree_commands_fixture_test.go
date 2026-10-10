//go:build integration

package knowledge_test

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
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	commandhttp "github.com/LunaDeerTech/agenteam/internal/central/knowledge/commandhttp"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const treeCommandHTTPOrigin = "https://knowledge-tree.example.test"

type treeCommandHTTPProcess struct{ id ac.ProcessID }

func (p treeCommandHTTPProcess) CurrentProcess() ac.ProcessID { return p.id }
func (treeCommandHTTPProcess) ConfirmStopped(context.Context, ac.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

type treeCommandHTTPBrowser struct {
	actor               identity.Actor
	cookie, csrf, email string
}

func (treeCommandHTTPBrowser) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tree_command_browser")
}

type treeCommandHTTPLog struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (l *treeCommandHTTPLog) Write(raw []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.Write(raw)
}
func (l *treeCommandHTTPLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.String()
}

// Account test construction is adapted from the fixed read-adapter fixture
// 1ee8b7e3; no read HTTP product is imported or required.
// The B02 publication fixture supplies the real same-Store Knowledge/Object/
// Audit/Outbox services. This fixture never calls its SQL human/session helpers:
// all positive identities use actual Bootstrap/Invitation/Redeem/Login below.
// Project initialization alone is the explicitly seeded upstream B02 fixture;
// this matrix does not claim Project.Create or Skills initialization execution.
type treeCommandHTTPFixture struct {
	*ownerTreeFixture
	core                                     *account.Service
	worker                                   *accountmail.Worker
	password                                 sc.SecretMaterial
	logPath                                  string
	adminBrowser, ownerBrowser, otherBrowser treeCommandHTTPBrowser
	project                                  identity.ProjectID
	boundary                                 *account.HTTPBoundary
	handler                                  http.Handler
	logs                                     *treeCommandHTTPLog
}

func newTreeCommandHTTPFixture(t *testing.T) *treeCommandHTTPFixture {
	t.Helper()
	return assembleTreeCommandHTTPFixture(t, newPublicationFixture(t))
}

func assembleTreeCommandHTTPFixture(t *testing.T, x *ownerTreeFixture) *treeCommandHTTPFixture {
	t.Helper()
	store := x.raw
	ck := x.deps.Cursors
	accounts := x.deps.Activity.(*account.Authority)
	pa := x.deps.Projects.(*project.Authority)
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if err != nil {
		t.Fatal(err)
	}
	// Exactly the same fixed test key material as the B02 fixture; no Model usage
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
	if err = secrets.Initialize(knowledgeContext(t)); err != nil {
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
	process := treeCommandHTTPProcess{treeID[ac.Process](t)}
	outProcess, err := f.ParseID[oc.Process](process.id.String())
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{ac.AccountProducer: accounts}, Sessions: accounts, System: accounts, Projects: pa, Audit: aud, Cursors: ck, Processes: treeProcess{outProcess}})
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
	status, err := core.Bootstrap(knowledgeContext(t))
	if err != nil || !status.Created || status.LogState != "written" {
		t.Fatal("formal Bootstrap", err)
	}
	password := treeCommandHTTPRecoveryMaterial(t, logPath, "bootstrap", "")
	t.Cleanup(password.Destroy)
	v := &treeCommandHTTPFixture{ownerTreeFixture: x, core: core, password: password, logPath: logPath, logs: &treeCommandHTTPLog{}}
	v.adminBrowser = v.login(t, "admin@mail.com")
	policy, err := outbound.NewPolicyService(store, aud, outbound.Authorizations{Sessions: accounts, System: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = policy.Reload(knowledgeContext(t)); err != nil {
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
	v.worker, err = accountmail.New(accountmail.Dependencies{Port: port, Registry: registry, Outbound: client, Trust: trust, RecoveryLog: sink, PublicOrigin: treeCommandHTTPOrigin})
	if err != nil {
		t.Fatal(err)
	}
	v.ownerBrowser = v.invite(t, "owner-"+treeID[struct{}](t).String()[24:])
	v.otherBrowser = v.invite(t, "other-"+treeID[struct{}](t).String()[24:])
	v.project = x.project(t, v.ownerBrowser.actor, true)
	v.boundary, err = account.NewHTTPBoundary(core, treeCommandHTTPOrigin)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := commandhttp.NewHTTPHandler(x.service, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), handler)
	t.Log("real Account login and B02 publication; Project initialization is explicit upstream SQL fixture, not Project/Skills creation acceptance")
	return v
}

func treeCommandHTTPRecoveryMaterial(t *testing.T, path, purpose, resource string) sc.SecretMaterial {
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
func (v *treeCommandHTTPFixture) login(t *testing.T, email string) treeCommandHTTPBrowser {
	t.Helper()
	anonymous, err := v.core.NewAnonymousContext(knowledgeContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, err := v.core.VerifyAnonymousContext(knowledgeContext(t), anonymous.Cookie, anonymous.CSRF)
	if err != nil {
		t.Fatal(err)
	}
	request, err := ac.NewLoginRequest(ac.LoginFields{Browser: browser, Key: f.IdempotencyKey(treeID[struct{}](t).String()), Email: email, Password: v.password, ClientIP: netip.MustParseAddr("192.0.2.18")})
	if err != nil {
		t.Fatal(err)
	}
	response, err := v.core.Login(knowledgeContext(t), request)
	if err != nil {
		t.Fatal("formal Login", err)
	}
	closed := false
	defer func() {
		if !closed {
			if e := response.Close(knowledgeContext(t)); e != nil {
				t.Error("Login response cleanup", e)
			}
		}
	}()
	var cookie sc.SecretMaterial
	if err = response.UseCookie(func(raw []byte) error { var e error; cookie, e = sc.NewSecretMaterial(raw); return e }); err != nil {
		t.Fatal("formal Login cookie")
	}
	defer cookie.Destroy()
	if err = response.Close(knowledgeContext(t)); err != nil {
		t.Fatal("formal Login response did not close", err)
	}
	closed = true
	actor, err := v.core.Authenticate(knowledgeContext(t), cookie)
	if err != nil {
		t.Fatal("formal Authenticate", err)
	}
	view, err := v.core.GetSession(knowledgeContext(t), cookie)
	if err != nil {
		t.Fatal("formal Session", err)
	}
	defer view.CSRF.Destroy()
	out := treeCommandHTTPBrowser{actor: actor, email: email}
	if err = cookie.Use(func(raw []byte) error { out.cookie = string(raw); return nil }); err != nil {
		t.Fatal("owned browser cookie copy")
	}
	if err = view.CSRF.Use(func(raw []byte) error { out.csrf = string(raw); return nil }); err != nil {
		t.Fatal("owned browser CSRF copy")
	}
	return out
}
func (v *treeCommandHTTPFixture) invite(t *testing.T, name string) treeCommandHTTPBrowser {
	t.Helper()
	email := name + "@example.test"
	request, err := ac.NewInvitationCreate(ac.InvitationCreateFields{Actor: v.adminBrowser.actor, Key: f.IdempotencyKey(treeID[struct{}](t).String()), Email: email})
	if err != nil {
		t.Fatal(err)
	}
	invite, err := v.core.CreateInvitation(knowledgeContext(t), request)
	if err != nil {
		t.Fatal("formal invitation creation", err)
	}
	if _, err = v.core.ReconcileDeliveryIntents(knowledgeContext(t)); err != nil {
		t.Fatal("formal delivery reconciliation", err)
	}
	if err = v.worker.RunJob(knowledgeContext(t), invite.JobID); err != nil {
		t.Fatal("formal recovery-log delivery", err)
	}
	material := treeCommandHTTPRecoveryMaterial(t, v.logPath, "invitation", invite.ID.String())
	defer material.Destroy()
	token, err := ac.NewInvitationToken(invite.ID, material)
	if err != nil {
		t.Fatal("formal invitation token", err)
	}
	anonymous, err := v.core.NewAnonymousContext(knowledgeContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, err := v.core.VerifyAnonymousContext(knowledgeContext(t), anonymous.Cookie, anonymous.CSRF)
	if err != nil {
		t.Fatal(err)
	}
	redeem, err := ac.NewInvitationRedeem(ac.RedeemFields{Browser: browser, Key: f.IdempotencyKey(treeID[struct{}](t).String()), Token: token, Username: name, DisplayName: "Knowledge owner", Password: v.password, Confirmation: v.password})
	if err != nil {
		t.Fatal(err)
	}
	result, err := v.core.RedeemInvitation(knowledgeContext(t), redeem)
	if err != nil || !result.Completed {
		t.Fatal("formal invitation redeem", err)
	}
	return v.login(t, email)
}

// This recorder supplies capability methods only for the PG business matrix.
// It does not establish native deadlines, TCP EOF, connection reuse or app Join.
type treeCommandHTTPRecorder struct{ *httptest.ResponseRecorder }

func (*treeCommandHTTPRecorder) SetReadDeadline(time.Time) error  { return nil }
func (*treeCommandHTTPRecorder) SetWriteDeadline(time.Time) error { return nil }
func (w *treeCommandHTTPRecorder) FlushError() error              { w.Flush(); return nil }

type treeCommandHTTPResponse struct {
	status  int
	header  http.Header
	body    []byte
	aborted bool
}

func treeCommandHTTPRequest(ctx context.Context, browser treeCommandHTTPBrowser, method, path, body string, key f.IdempotencyKey) *http.Request {
	r := httptest.NewRequest(method, treeCommandHTTPOrigin+path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Origin", treeCommandHTTPOrigin)
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
func (v *treeCommandHTTPFixture) request(t *testing.T, browser treeCommandHTTPBrowser, method, path, body string, key f.IdempotencyKey) treeCommandHTTPResponse {
	t.Helper()
	return v.serve(treeCommandHTTPRequest(knowledgeContext(t), browser, method, path, body, key))
}
func (v *treeCommandHTTPFixture) serve(r *http.Request) (result treeCommandHTTPResponse) {
	w := &treeCommandHTTPRecorder{httptest.NewRecorder()}
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
func treeCommandHTTPPath(project identity.ProjectID, suffix string) string {
	return "/api/v1/projects/" + project.String() + "/knowledge/documents" + suffix
}

func (v *treeCommandHTTPFixture) bind(t *testing.T, service *knowledge.Service) {
	t.Helper()
	handler, err := commandhttp.NewHTTPHandler(service, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), handler)
}
