//go:build integration

package accountmail_test

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

// This live-only authority never supplies a death proof. Crash tests install a
// separate adapter backed by an actually held/released owned ProcessGuard.
type liveProcess struct{ id c.ProcessID }

func (p liveProcess) CurrentProcess() c.ProcessID { return p.id }
func (p liveProcess) ConfirmStopped(context.Context, c.ProcessID) error {
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
}

type fixture struct {
	service   *account.Service
	store     *postgres.Store
	authority *account.Authority
	audit     *audit.Service
	secrets   *secret.Service
	sink      *recoverylog.Sink
	log       string
	process   c.ProcessAuthority
	admin     identity.Actor
}

func newAccount(t *testing.T) *fixture {
	t.Helper()
	_, store, authority := database(t)
	_, cursor, master := keys(t)
	aud, e := audit.New(store, cursor, audit.Authorizations{Sessions: authority, System: authority, Accounts: authority})
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.New(store, master, aud, secret.Authorizations{Sessions: authority, System: authority, Usage: authority, AccountWrites: authority})
	if e != nil {
		t.Fatal(e)
	}
	if e = secrets.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "recovery.jsonl")
	sink, e := recoverylog.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	catalog := event.NewCatalog()
	revoked, e := c.DefineSessionsRevoked(catalog)
	if e != nil {
		t.Fatal(e)
	}
	delivery, e := c.DefineDeliveryRequested(catalog)
	if e != nil {
		t.Fatal(e)
	}
	events, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.AccountProducer: authority}, Sessions: authority, System: authority, Audit: aud, Cursors: cursor})
	if e != nil {
		t.Fatal(e)
	}
	process := liveProcess{id[c.Process](t)}
	s, e := account.New(account.Dependencies{Authority: authority, Audit: aud, Secrets: secrets, Events: events, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: process, RecoveryLog: sink})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := s.Force(ctx); e != nil && !s.Joined() {
			t.Error(e)
		}
	})
	f := &fixture{s, store, authority, aud, secrets, sink, path, process, identity.Actor{}}
	boot, e := s.Bootstrap(ctxFor(t))
	if e != nil || !boot.Created || boot.LogState != "written" {
		t.Fatal("real bootstrap", e)
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var row struct {
		Password string `json:"initial_password"`
	}
	if e = json.Unmarshal(raw, &row); e != nil {
		t.Fatal(e)
	}
	clear(raw)
	password, e := sc.NewSecretMaterial([]byte(row.Password))
	row.Password = ""
	if e != nil {
		t.Fatal(e)
	}
	defer password.Destroy()
	anonymous, e := s.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, e := s.VerifyAnonymousContext(ctxFor(t), anonymous.Cookie, anonymous.CSRF)
	if e != nil {
		t.Fatal(e)
	}
	req, e := c.NewLoginRequest(c.LoginFields{Browser: browser, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "admin@mail.com", Password: password, ClientIP: netip.MustParseAddr("192.0.2.7")})
	if e != nil {
		t.Fatal(e)
	}
	response, e := s.Login(ctxFor(t), req)
	if e != nil {
		t.Fatal("real login", e)
	}
	var cookie sc.SecretMaterial
	if e = response.UseCookie(func(b []byte) error { cookie, e = sc.NewSecretMaterial(b); return e }); e != nil {
		t.Fatal(e)
	}
	defer cookie.Destroy()
	f.admin, e = s.Authenticate(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return f
}

type fixtureResolver struct{ ip netip.Addr }

func (r fixtureResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if host != smtpfixture.Host {
		return nil, foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return []netip.Addr{r.ip}, ctx.Err()
}

type mailComposition struct {
	worker   *accountmail.Worker
	port     c.DeliveryPort
	registry *accountmail.WorkRegistry
	policy   *outbound.PolicyService
	client   *outbound.Client
	f        *fixture
}

func composeMail(t *testing.T, f *fixture, network *smtpfixture.Fixture, ports ...uint16) *mailComposition {
	t.Helper()
	policy, e := outbound.NewPolicyService(f.store, f.audit, outbound.Authorizations{Sessions: f.authority, System: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	if e = policy.Reload(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	selected, e := outbound.SelectedPorts(ports...)
	if e != nil {
		t.Fatal(e)
	}
	rule, e := outbound.NewRule(network.PrivateIP+"/32", selected, false)
	if e != nil {
		t.Fatal(e)
	}
	rules, e := outbound.NewRules(rule)
	if e != nil {
		t.Fatal(e)
	}
	key, e := foundation.NewCommandIdentity("outbound-policy", []string{f.admin.Details().UserID}, "update", foundation.IdempotencyKey(id[struct{}](t).String()))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = policy.UpdatePolicy(ctxFor(t), f.admin, outbound.CommandMeta{Identity: key, ExpectedVersion: *policy.Status().Version}, rules); e != nil {
		t.Fatal(e)
	}
	trust, e := outbound.LoadTrustStore(network.CAFile)
	if e != nil {
		t.Fatal(e)
	}
	client, e := outbound.NewClient(policy, trust, fixtureResolver{netip.MustParseAddr(network.PrivateIP)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := client.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	registry, e := accountmail.NewWorkRegistry(f.process)
	if e != nil {
		t.Fatal(e)
	}
	port, e := account.NewDeliveryPort(f.service, registry)
	if e != nil {
		t.Fatal(e)
	}
	worker, e := accountmail.New(accountmail.Dependencies{Port: port, Registry: registry, Outbound: client, Trust: trust, RecoveryLog: f.sink, PublicOrigin: "https://accounts.example.test"})
	if e != nil {
		t.Fatal(e)
	}
	return &mailComposition{worker, port, registry, policy, client, f}
}
func startSMTP(t *testing.T) *smtpfixture.Fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	f, e := smtpfixture.Start(ctx)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if e := f.Close(ctx); e != nil {
			t.Error(e)
		}
	})
	return f
}
func configure(t *testing.T, f *fixture, endpoint smtpfixture.Endpoint, mode string, auth bool) {
	t.Helper()
	settings, e := f.service.GetSMTPSettings(ctxFor(t), f.admin)
	if e != nil {
		t.Fatal(e)
	}
	fields := c.SMTPUpdateFields{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: settings.Version, Configured: true, Host: smtpfixture.Host, Port: endpoint.Port, TLSMode: mode, SenderEmail: "sender@example.test", SenderName: "Owned sender", RetryCount: 3, RetryIntervalSeconds: 10}
	if auth {
		password, e := sc.NewSecretMaterial([]byte("owned fixture password"))
		if e != nil {
			t.Fatal(e)
		}
		defer password.Destroy()
		fields.Username = "fixture"
		fields.Password = &password
	}
	req, e := c.NewSMTPUpdate(fields)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.UpdateSMTPSettings(ctxFor(t), req); e != nil {
		t.Fatal("save", e)
	}
}
func testJob(t *testing.T, f *fixture) c.JobID {
	t.Helper()
	job, e := f.service.TestSMTP(ctxFor(t), c.SMTPTest{Actor: f.admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Recipient: "recipient@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return job.JobID
}
