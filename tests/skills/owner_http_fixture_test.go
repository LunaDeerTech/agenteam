//go:build integration

package skill_test

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
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	skillhttp "github.com/LunaDeerTech/agenteam/internal/central/skill/http"
)

const skillOwnerHTTPOrigin = "https://skills-owner.example.test"

type skillOwnerHTTPProcess struct{ id ac.ProcessID }

func (p skillOwnerHTTPProcess) CurrentProcess() ac.ProcessID { return p.id }
func (skillOwnerHTTPProcess) ConfirmStopped(context.Context, ac.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

type skillOwnerHTTPBrowser struct {
	actor               identity.Actor
	cookie, csrf, email string
}

func (skillOwnerHTTPBrowser) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skills_http_browser")
}

type skillOwnerHTTPLog struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (l *skillOwnerHTTPLog) Write(raw []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.Write(raw)
}
func (l *skillOwnerHTTPLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.String()
}

// Real Account Bootstrap/Invitation/Redeem/Login and P2 writes share the exact
// PostgreSQL Store. Project/Creation/ready/lifecycle are explicit upstream
// seeds. P2's existing controlled Object port prepares the metadata fixture;
// this HTTP adapter must never call it. It does not certify a new D05 physical
// publication, Project.Create, BeginDelete or production root.
type skillOwnerHTTPFixture struct {
	*skillPG
	seed                                     *skillCase
	service                                  *skill.Service
	core                                     *account.Service
	worker                                   *accountmail.Worker
	password                                 sc.SecretMaterial
	logPath                                  string
	adminBrowser, ownerBrowser, otherBrowser skillOwnerHTTPBrowser
	project                                  identity.ProjectID
	boundary                                 *account.HTTPBoundary
	handler                                  http.Handler
	logs                                     *skillOwnerHTTPLog
}

func newSkillOwnerHTTPFixture(t *testing.T) *skillOwnerHTTPFixture {
	t.Helper()
	return assembleSkillOwnerHTTPFixture(t, newSkillPG(t))
}

func assembleSkillOwnerHTTPFixture(t *testing.T, p *skillPG) *skillOwnerHTTPFixture {
	t.Helper()
	store := p.store
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	ck, err := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if err != nil {
		t.Fatal(err)
	}
	secretKeys, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), ck)
	if err != nil {
		t.Fatal(err)
	}
	downloads, err := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), ck, secretKeys)
	if err != nil {
		t.Fatal(err)
	}
	accountKeys, err := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), ck, secretKeys, downloads)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := account.NewAuthority(store, accountKeys)
	if err != nil {
		t.Fatal(err)
	}
	pa := p.projects
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if err != nil {
		t.Fatal(err)
	}
	// Exactly the same fixed test key material as the P2 fixture; no Model usage
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
	if err = secrets.Initialize(testContext(t)); err != nil {
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
	process := skillOwnerHTTPProcess{testID[ac.Process](t)}
	outProcess, err := f.ParseID[oc.Process](process.id.String())
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{ac.AccountProducer: accounts}, Sessions: accounts, System: accounts, Projects: pa, Audit: aud, Cursors: ck, Processes: skillOwnerHTTPOutProcess{outProcess}})
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
	status, err := core.Bootstrap(testContext(t))
	if err != nil || !status.Created || status.LogState != "written" {
		t.Fatal("formal Bootstrap", err)
	}
	password := skillOwnerHTTPRecoveryMaterial(t, logPath, "bootstrap", "")
	t.Cleanup(password.Destroy)
	v := &skillOwnerHTTPFixture{skillPG: p, core: core, password: password, logPath: logPath, logs: &skillOwnerHTTPLog{}}
	v.adminBrowser = v.login(t, "admin@mail.com")
	policy, err := outbound.NewPolicyService(store, aud, outbound.Authorizations{Sessions: accounts, System: accounts})
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
	v.worker, err = accountmail.New(accountmail.Dependencies{Port: port, Registry: registry, Outbound: client, Trust: trust, RecoveryLog: sink, PublicOrigin: skillOwnerHTTPOrigin})
	if err != nil {
		t.Fatal(err)
	}
	v.ownerBrowser = v.invite(t, "owner-"+testID[struct{}](t).String()[24:])
	v.otherBrowser = v.invite(t, "other-"+testID[struct{}](t).String()[24:])
	v.seed = v.newCase(t, true, true)
	v.project = v.seed.request.ProjectID
	v.service = v.seed.service
	v.boundary, err = account.NewHTTPBoundary(core, skillOwnerHTTPOrigin)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := skillhttp.NewHTTPHandler(v.service, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), handler)
	t.Log("real Account login and P2 publication with controlled Object; Project initialization is explicit upstream SQL fixture, not Project.Create or D05 physical acceptance")
	return v
}

func skillOwnerHTTPRecoveryMaterial(t *testing.T, path, purpose, resource string) sc.SecretMaterial {
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
func (v *skillOwnerHTTPFixture) login(t *testing.T, email string) skillOwnerHTTPBrowser {
	t.Helper()
	anonymous, err := v.core.NewAnonymousContext(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, err := v.core.VerifyAnonymousContext(testContext(t), anonymous.Cookie, anonymous.CSRF)
	if err != nil {
		t.Fatal(err)
	}
	request, err := ac.NewLoginRequest(ac.LoginFields{Browser: browser, Key: f.IdempotencyKey(testID[struct{}](t).String()), Email: email, Password: v.password, ClientIP: netip.MustParseAddr("192.0.2.18")})
	if err != nil {
		t.Fatal(err)
	}
	response, err := v.core.Login(testContext(t), request)
	if err != nil {
		t.Fatal("formal Login", err)
	}
	closed := false
	defer func() {
		if !closed {
			if e := response.Close(testContext(t)); e != nil {
				t.Error("Login response cleanup", e)
			}
		}
	}()
	var cookie sc.SecretMaterial
	if err = response.UseCookie(func(raw []byte) error { var e error; cookie, e = sc.NewSecretMaterial(raw); return e }); err != nil {
		t.Fatal("formal Login cookie")
	}
	defer cookie.Destroy()
	if err = response.Close(testContext(t)); err != nil {
		t.Fatal("formal Login response did not close", err)
	}
	closed = true
	actor, err := v.core.Authenticate(testContext(t), cookie)
	if err != nil {
		t.Fatal("formal Authenticate", err)
	}
	view, err := v.core.GetSession(testContext(t), cookie)
	if err != nil {
		t.Fatal("formal Session", err)
	}
	defer view.CSRF.Destroy()
	out := skillOwnerHTTPBrowser{actor: actor, email: email}
	if err = cookie.Use(func(raw []byte) error { out.cookie = string(raw); return nil }); err != nil {
		t.Fatal("owned browser cookie copy")
	}
	if err = view.CSRF.Use(func(raw []byte) error { out.csrf = string(raw); return nil }); err != nil {
		t.Fatal("owned browser CSRF copy")
	}
	return out
}
func (v *skillOwnerHTTPFixture) invite(t *testing.T, name string) skillOwnerHTTPBrowser {
	t.Helper()
	email := name + "@example.test"
	request, err := ac.NewInvitationCreate(ac.InvitationCreateFields{Actor: v.adminBrowser.actor, Key: f.IdempotencyKey(testID[struct{}](t).String()), Email: email})
	if err != nil {
		t.Fatal(err)
	}
	invite, err := v.core.CreateInvitation(testContext(t), request)
	if err != nil {
		t.Fatal("formal invitation creation", err)
	}
	if _, err = v.core.ReconcileDeliveryIntents(testContext(t)); err != nil {
		t.Fatal("formal delivery reconciliation", err)
	}
	if err = v.worker.RunJob(testContext(t), invite.JobID); err != nil {
		t.Fatal("formal recovery-log delivery", err)
	}
	material := skillOwnerHTTPRecoveryMaterial(t, v.logPath, "invitation", invite.ID.String())
	defer material.Destroy()
	token, err := ac.NewInvitationToken(invite.ID, material)
	if err != nil {
		t.Fatal("formal invitation token", err)
	}
	anonymous, err := v.core.NewAnonymousContext(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, err := v.core.VerifyAnonymousContext(testContext(t), anonymous.Cookie, anonymous.CSRF)
	if err != nil {
		t.Fatal(err)
	}
	redeem, err := ac.NewInvitationRedeem(ac.RedeemFields{Browser: browser, Key: f.IdempotencyKey(testID[struct{}](t).String()), Token: token, Username: name, DisplayName: "Skill owner", Password: v.password, Confirmation: v.password})
	if err != nil {
		t.Fatal(err)
	}
	result, err := v.core.RedeemInvitation(testContext(t), redeem)
	if err != nil || !result.Completed {
		t.Fatal("formal invitation redeem", err)
	}
	return v.login(t, email)
}

// This recorder supplies capability methods only for the PG business matrix.
// It does not establish native deadlines, TCP EOF, connection reuse or app Join.
type skillOwnerHTTPRecorder struct{ *httptest.ResponseRecorder }

func (*skillOwnerHTTPRecorder) SetReadDeadline(time.Time) error  { return nil }
func (*skillOwnerHTTPRecorder) SetWriteDeadline(time.Time) error { return nil }
func (w *skillOwnerHTTPRecorder) FlushError() error              { w.Flush(); return nil }

type skillOwnerHTTPResponse struct {
	status  int
	header  http.Header
	body    []byte
	aborted bool
}

func skillOwnerHTTPRequest(ctx context.Context, browser skillOwnerHTTPBrowser, method, path, body string, key f.IdempotencyKey) *http.Request {
	r := httptest.NewRequest(method, skillOwnerHTTPOrigin+path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Origin", skillOwnerHTTPOrigin)
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
func (v *skillOwnerHTTPFixture) request(t *testing.T, browser skillOwnerHTTPBrowser, method, path, body string, key f.IdempotencyKey) skillOwnerHTTPResponse {
	t.Helper()
	return v.serve(skillOwnerHTTPRequest(testContext(t), browser, method, path, body, key))
}
func (v *skillOwnerHTTPFixture) serve(r *http.Request) (result skillOwnerHTTPResponse) {
	w := &skillOwnerHTTPRecorder{httptest.NewRecorder()}
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
func skillOwnerHTTPPath(project identity.ProjectID, suffix string) string {
	return "/api/v1/projects/" + project.String() + "/skills" + suffix
}

// The test-owned lifetime identity never reports another process dead.
type skillOwnerHTTPOutProcess struct{ id oc.ProcessID }

func (p skillOwnerHTTPOutProcess) CurrentProcess() oc.ProcessID { return p.id }
func (skillOwnerHTTPOutProcess) ConfirmStopped(context.Context, oc.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

func (v *skillOwnerHTTPFixture) newCase(t *testing.T, publish, ready bool) *skillCase {
	t.Helper()
	c := v.skillPG.newCase(t)
	owner, err := f.ParseID[identity.User](v.ownerBrowser.actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	// Before any accepted initialization/read, replace only this explicit seed's
	// owner with the user created by the actual Account invitation/redeem path.
	readerMutation(t, v.skillPG, c, `UPDATE agenteam_project.projects SET owner_user_id=$2 WHERE id=$1`, c.request.ProjectID.String(), owner.String())
	readerMutation(t, v.skillPG, c, `UPDATE agenteam_project.creations SET owner_user_id=$2 WHERE id=$1`, c.request.CreationID.String(), owner.String())
	c.owner = owner
	skillID := testID[pc.Skill](t)
	if publish {
		result, e := c.service.InitializeProjectSkills(testContext(t), c.actor, c.request)
		if e != nil || result.State != pc.InitializationCompleted || !result.Matches(c.request) || result.AddSkillsID == nil {
			t.Fatal("real P2 initialization metadata", e)
		}
		skillID = *result.AddSkillsID
	}
	if ready {
		seedReaderReadyProject(t, v.skillPG, c, skillID)
	}
	return c
}
