//go:build integration

package runnercontrol_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func serviceID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	v, e := f.NewID[K]()
	if e != nil {
		t.Fatal("fixture identity unavailable")
	}
	return v
}
func serviceKey(t *testing.T) f.IdempotencyKey {
	return f.IdempotencyKey(serviceID[struct{}](t).String())
}
func requireServiceOK(t *testing.T, e error, stage string) {
	t.Helper()
	if e == nil {
		return
	}
	code, state := "unknown", "unknown"
	var fault *f.Fault
	if errors.As(e, &fault) && fault != nil {
		code, state = string(fault.Code.Safe()), string(fault.CommitState.Safe())
	}
	t.Fatalf("%s: code=%s commit_state=%s SQLSTATE=%s", stage, code, state, sqlCode(e))
}
func requireServiceCode(t *testing.T, e error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(e, &fault) || fault == nil || fault.Code != code {
		t.Fatalf("Runner rejection: expected=%s error_present=%t", code.Safe(), e != nil)
	}
}

// Account's required process port is test-owned and never claims another
// process dead. It does not provide Runner liveness or substitute for a lease.
type accountFixtureProcess struct{ id ac.ProcessID }

func (v accountFixtureProcess) CurrentProcess() ac.ProcessID { return v.id }
func (accountFixtureProcess) ConfirmStopped(context.Context, ac.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

type runnerServiceFixture struct {
	db        *pgfixture.Database
	store     *postgres.Store
	accounts  *account.Service
	keys      account.Keyring
	authority *service.Authority
	auditor   *audit.Service
	runner    *service.Service
	password  sc.SecretMaterial
	actor     i.Actor
}

func openRunnerStore(t *testing.T, db *pgfixture.Database) *postgres.Store {
	t.Helper()
	store, e := postgres.Open(migrationContext(t), db.Config(t, nil))
	requireServiceOK(t, e, "open verified Store")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := store.ForceClose(ctx); e != nil {
			t.Error("owned Store did not actually join")
		}
	})
	return store
}

func newRunnerServiceFixture(t *testing.T) *runnerServiceFixture {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	runnerMigrate(t, db, 26)
	store := openRunnerStore(t, db)
	encoded := func(n byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{n}, 32)) }
	ck, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, encoded(1)))
	requireServiceOK(t, e, "cursor keyring")
	sk, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, encoded(2)), ck)
	requireServiceOK(t, e, "Secret keyring")
	dk, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, encoded(3)), ck, sk)
	requireServiceOK(t, e, "download keyring")
	ak, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, encoded(4)), ck, sk, dk)
	requireServiceOK(t, e, "Account keyring")
	aa, e := account.NewAuthority(store, ak)
	requireServiceOK(t, e, "Account authority")
	requireServiceOK(t, aa.Initialize(migrationContext(t)), "Account initialization")
	ra, e := service.NewAuthority(store, aa)
	requireServiceOK(t, e, "Runner authority")
	aud, e := audit.New(store, ck, audit.Authorizations{Sessions: aa, System: aa, Accounts: aa, Runners: ra})
	requireServiceOK(t, e, "single typed Audit")
	secrets, e := secret.New(store, sk, aud, secret.Authorizations{Sessions: aa, System: aa, Usage: aa, AccountWrites: aa})
	requireServiceOK(t, e, "Secret service")
	requireServiceOK(t, secrets.Initialize(migrationContext(t)), "Secret initialization")
	t.Cleanup(secrets.StopMaintenance)
	dir := t.TempDir()
	requireServiceOK(t, os.Chmod(dir, 0700), "private recovery directory")
	logPath := filepath.Join(dir, "bootstrap.jsonl")
	sink, e := recoverylog.Open(logPath)
	requireServiceOK(t, e, "private recovery sink")
	catalog := event.NewCatalog()
	revoked, e := ac.DefineSessionsRevoked(catalog)
	requireServiceOK(t, e, "Account event registration")
	events, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{ac.AccountProducer: aa}, Sessions: aa, System: aa, Audit: aud, Cursors: ck})
	requireServiceOK(t, e, "Account Outbox")
	accounts, e := account.New(account.Dependencies{Authority: aa, Audit: aud, Secrets: secrets, Events: events, SessionsRevoked: revoked, Processes: accountFixtureProcess{serviceID[ac.Process](t)}, RecoveryLog: sink})
	requireServiceOK(t, e, "Account service")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := accounts.Force(ctx); e != nil || !accounts.Joined() {
			t.Error("Account callback/recovery sink did not actually join")
		}
	})
	fixture := &runnerServiceFixture{db: db, store: store, accounts: accounts, keys: ak, authority: ra, auditor: aud}
	fixture.runner = fixture.newRunner(t)
	bootstrap, e := accounts.Bootstrap(migrationContext(t))
	requireServiceOK(t, e, "real bootstrap")
	if !bootstrap.Created || bootstrap.LogState != "written" {
		t.Fatal("fresh bootstrap did not publish private recovery record")
	}
	raw, e := os.ReadFile(logPath)
	requireServiceOK(t, e, "read owned bootstrap output")
	var record struct {
		Password string `json:"initial_password"`
	}
	if json.Unmarshal(raw, &record) != nil || len(record.Password) != 24 {
		clear(raw)
		t.Fatal("private bootstrap shape invalid")
	}
	clear(raw)
	fixture.password, e = sc.NewSecretMaterial([]byte(record.Password))
	record.Password = ""
	requireServiceOK(t, e, "bootstrap material")
	t.Cleanup(fixture.password.Destroy)
	fixture.actor = fixture.login(t)
	return fixture
}

func (v *runnerServiceFixture) newRunner(t *testing.T) *service.Service {
	t.Helper()
	s, e := service.New(v.authority, v.auditor)
	requireServiceOK(t, e, "Runner service")
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := s.Drain(ctx); e != nil {
			t.Error("Runner callback did not actually join")
		}
	})
	return s
}

func (v *runnerServiceFixture) login(t *testing.T) i.Actor {
	t.Helper()
	ctx := migrationContext(t)
	anonymous, e := v.accounts.NewAnonymousContext(ctx)
	requireServiceOK(t, e, "anonymous login context")
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, e := v.accounts.VerifyAnonymousContext(ctx, anonymous.Cookie, anonymous.CSRF)
	requireServiceOK(t, e, "verify login CSRF")
	request, e := ac.NewLoginRequest(ac.LoginFields{Browser: browser, Key: serviceKey(t), Email: "admin@mail.com", Password: v.password, ClientIP: netip.MustParseAddr("192.0.2.7")})
	requireServiceOK(t, e, "login request")
	response, e := v.accounts.Login(ctx, request)
	requireServiceOK(t, e, "real login")
	defer func() { requireServiceOK(t, response.Close(ctx), "close actual login material lease") }()
	var cookie sc.SecretMaterial
	e = response.UseCookie(func(raw []byte) error { var e error; cookie, e = sc.NewSecretMaterial(raw); return e })
	requireServiceOK(t, e, "actual login cookie")
	defer cookie.Destroy()
	actor, e := v.accounts.Authenticate(ctx, cookie)
	requireServiceOK(t, e, "current login identity")
	if actor.Details().UserID != response.User().ID.String() || actor.Details().SessionID != response.Session().ID.String() {
		t.Fatal("cookie does not bind exact returned identity")
	}
	return actor
}

func (v *runnerServiceFixture) create(t *testing.T, name string) (rc.Intent, f.IdempotencyKey, rc.Mutation) {
	t.Helper()
	intent, e := rc.NewCreate(rc.CreateRequest{RunnerID: serviceID[rc.Runner](t), Name: name, Description: "original", Tags: []string{}, RootPath: "/srv/runner"})
	requireServiceOK(t, e, "create intent")
	key := serviceKey(t)
	mutation, e := v.runner.Execute(migrationContext(t), v.actor, key, intent)
	requireServiceOK(t, e, "real Runner create")
	if mutation.Material == nil || !mutation.Material.Token.Valid() || !mutation.Receipt.Changed || mutation.Receipt.Runner.Version != 1 || mutation.Receipt.Runner.CredentialGeneration != 1 {
		t.Fatal("create did not return first known token and v1 receipt")
	}
	return intent, key, mutation
}

func (v *runnerServiceFixture) enroll(t *testing.T, material rc.Mutation) (ed25519.PrivateKey, p.EnrollmentRequest) {
	t.Helper()
	public, key, e := ed25519.GenerateKey(rand.Reader)
	requireServiceOK(t, e, "device key generation")
	t.Cleanup(func() { clear(key) })
	request, e := p.NewEnrollmentRequest(p.ID(material.Receipt.Runner.ID.String()), material.Material.Token, [32]byte(public), material.Receipt.Runner.RootPath, "linux", "amd64")
	requireServiceOK(t, e, "enrollment request")
	response, e := v.runner.Enroll(migrationContext(t), request)
	requireServiceOK(t, e, "real enrollment")
	if response.RunnerID() != request.RunnerID() || response.Version() != "2" || response.CredentialGeneration() != "1" || response.PublicKeyFingerprint() != p.PublicKeyFingerprint([32]byte(public)) {
		t.Fatal("known enrollment response does not bind original device")
	}
	return key, request
}

func (v *runnerServiceFixture) authentication(t *testing.T, target rc.RunnerID, key ed25519.PrivateKey) p.Authentication {
	t.Helper()
	request, e := p.NewChallengeRequest(p.ID(target.String()))
	requireServiceOK(t, e, "challenge request")
	challenge, e := v.runner.Challenge(migrationContext(t), request)
	requireServiceOK(t, e, "real challenge")
	stamp := p.NewUnixSeconds(time.Now().Unix())
	raw, e := p.SigningBytes(request.RunnerID(), challenge.Nonce(), stamp)
	requireServiceOK(t, e, "original signing bytes")
	auth, e := p.NewAuthentication(request.RunnerID(), challenge.Nonce(), stamp, ed25519.Sign(key, raw))
	requireServiceOK(t, e, "signed authentication")
	return auth
}

func (v *runnerServiceFixture) snapshot(t *testing.T, target rc.RunnerID, status rc.Status) rc.Snapshot {
	t.Helper()
	value, e := v.runner.Get(migrationContext(t), v.actor, target)
	requireServiceOK(t, e, "current Reader")
	if value.Status != status {
		t.Fatalf("Reader status want=%s got=%s", status, value.Status)
	}
	return value
}
