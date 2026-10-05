//go:build integration

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
)

// A fixture uses the same bootstrap file and real login/read protocols as a
// client. It cannot mint a Session or bypass the current Account authorities.
func fixtureAccountLoginResponse(ctx context.Context, cfg config.Config, core *account.Service) (account.LoginResponse, error) {
	failed := errors.New("fixture account authentication unavailable")
	if core == nil {
		return account.LoginResponse{}, failed
	}
	data, err := os.ReadFile(cfg.AccountRecoveryLog())
	if err != nil {
		return account.LoginResponse{}, failed
	}
	defer clear(data)
	var email, password string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var record struct {
			Purpose  string `json:"purpose"`
			Email    string `json:"email"`
			Password string `json:"initial_password"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			return account.LoginResponse{}, failed
		}
		if record.Purpose == "bootstrap" {
			email, password = record.Email, record.Password
		}
	}
	if scanner.Err() != nil || email == "" || password == "" {
		return account.LoginResponse{}, failed
	}
	material, err := sc.NewSecretMaterial([]byte(password))
	password = ""
	if err != nil {
		return account.LoginResponse{}, failed
	}
	defer material.Destroy()
	anonymous, err := core.NewAnonymousContext(ctx)
	if err != nil {
		return account.LoginResponse{}, failed
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, err := core.VerifyAnonymousContext(ctx, anonymous.Cookie, anonymous.CSRF)
	if err != nil {
		return account.LoginResponse{}, failed
	}
	id, err := foundation.NewID[c.Command]()
	if err != nil {
		return account.LoginResponse{}, failed
	}
	key, err := foundation.ParseIdempotencyKey(id.String())
	if err != nil {
		return account.LoginResponse{}, failed
	}
	request, err := c.NewLoginRequest(c.LoginFields{Browser: browser, Key: key, Email: email, Password: material, ClientIP: netip.MustParseAddr("127.0.0.1")})
	if err != nil {
		return account.LoginResponse{}, failed
	}
	response, err := core.Login(ctx, request)
	if err != nil {
		return account.LoginResponse{}, failed
	}
	return response, nil
}

func fixtureAccountActor(ctx context.Context, cfg config.Config, core *account.Service) (identity.Actor, error) {
	failed := errors.New("fixture account authentication unavailable")
	response, err := fixtureAccountLoginResponse(ctx, cfg, core)
	if err != nil {
		return identity.Actor{}, failed
	}
	var actor identity.Actor
	use := response.UseCookie(func(value []byte) error {
		cookie, err := sc.NewSecretMaterial(value)
		if err != nil {
			return err
		}
		defer cookie.Destroy()
		actor, err = core.Authenticate(ctx, cookie)
		return err
	})
	closed := response.Close(context.Background())
	if use != nil || closed != nil || actor.Validate() != nil {
		return identity.Actor{}, failed
	}
	return actor, nil
}

func TestB04AppSMTPFinalReplyDrainsBeforeCoreAndGuard(t *testing.T) {
	setup, setupCancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer setupCancel()
	network, err := smtpfixture.Start(setup)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := network.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	endpoint, err := network.Create(databaseTestContext(t), smtpfixture.Scenario{Mode: "starttls", PauseStage: "accepted"})
	if err != nil {
		t.Fatal(err)
	}
	db := pgfixture.NewDatabase(t)
	env := append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=https://accounts.example.test", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
	cfg := guardConfiguration(t, env)
	var core *account.Service
	var transport *outboundRuntime
	var owned *resources
	mailDrainEntered, coreDrainEntered := make(chan struct{}), make(chan struct{})
	deps := dependencies{
		observeAccount: func(v *account.Service) { core = v },
		outboundConstruct: func(ctx context.Context, cfg config.Config, v *outboundRuntime) error {
			transport = v
			if err := v.client.ForceClose(ctx); err != nil {
				return err
			}
			var err error
			v.client, err = outbound.NewClient(v.policy, cfg.OutboundTrust(), appFixtureResolver{netip.MustParseAddr(network.PrivateIP)})
			return err
		},
		bind: func(ctx context.Context, cfg config.Config, store database, owner *resources, d *dependencies) error {
			owned = owner
			if err := bindAccounts(ctx, cfg, store, owner, d); err != nil {
				return err
			}
			assembly := owner.accounts().(*accountAssembly)
			assembly.mu.Lock()
			assembly.runtime = &b04ObservedAccountDrain{accountRuntime: assembly.runtime, entered: coreDrainEntered}
			assembly.mail = &b04ObservedAccountDrain{accountRuntime: assembly.mail, entered: mailDrainEntered}
			assembly.mu.Unlock()
			return nil
		},
	}
	output := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 2)
	done := make(chan struct{})
	var result error
	go func() { result = run(ctx, cfg, logger, signals, deps); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(7 * time.Second):
			t.Error("real root did not join after test cleanup")
		}
	})
	output.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })
	actor, err := fixtureAccountActor(databaseTestContext(t), cfg, core)
	if err != nil {
		t.Fatal(err)
	}
	ports, err := outbound.SelectedPorts(uint16(endpoint.Port))
	if err != nil {
		t.Fatal(err)
	}
	rule, err := outbound.NewRule(network.PrivateIP+"/32", ports, false)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := outbound.NewRules(rule)
	if err != nil {
		t.Fatal(err)
	}
	key, err := foundation.NewCommandIdentity("outbound-policy", []string{actor.Details().UserID}, "update", foundation.IdempotencyKey(guardID[struct{}](t).String()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = transport.policy.UpdatePolicy(databaseTestContext(t), actor, outbound.CommandMeta{Identity: key, ExpectedVersion: *transport.policy.Status().Version}, rules); err != nil {
		t.Fatal(err)
	}
	settings, err := core.GetSMTPSettings(databaseTestContext(t), actor)
	if err != nil {
		t.Fatal(err)
	}
	password, err := sc.NewSecretMaterial([]byte("root-owned-SMTP-password-sentinel"))
	if err != nil {
		t.Fatal(err)
	}
	defer password.Destroy()
	update, err := c.NewSMTPUpdate(c.SMTPUpdateFields{Actor: actor, Key: foundation.IdempotencyKey(guardID[struct{}](t).String()), ExpectedVersion: settings.Version, Configured: true, Host: smtpfixture.Host, Port: endpoint.Port, TLSMode: "starttls", Username: "fixture", Password: &password, CredentialAction: "keep", SenderEmail: "sender@example.test", SenderName: "Root fixture", RetryCount: 0, RetryIntervalSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.UpdateSMTPSettings(databaseTestContext(t), update); err != nil {
		t.Fatal(err)
	}
	job, err := core.TestSMTP(databaseTestContext(t), c.SMTPTest{Actor: actor, Key: foundation.IdempotencyKey(guardID[struct{}](t).String()), Recipient: "recipient@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	wait := databaseTestContext(t)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := network.State(wait, endpoint.ID)
		if err != nil {
			t.Fatal(err)
		}
		if state.Phase == "accepted" && state.Messages == 1 {
			break
		}
		select {
		case <-tick.C:
		case <-wait.Done():
			t.Fatal("SMTP did not reach its paused final reply")
		}
	}
	objects := owned.objects().(*objectAssembly)
	claimPath := filepath.Join(cfg.Objects().SpoolDirectory()+".processes", objects.process.String()+".claim")
	claim, err := os.OpenFile(claimPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	signals <- syscall.SIGTERM
	output.wait(t, func(e map[string]any) bool { return e["phase"] == string(logging.Stopping) })
	select {
	case <-mailDrainEntered:
	case <-time.After(time.Second):
		t.Fatal("root did not initiate real Mail Drain")
	}
	select {
	case <-coreDrainEntered:
		t.Error("root began Core Drain before the paused SMTP final reply")
	case <-time.After(30 * time.Millisecond):
	}
	if err := syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
		t.Fatal("shared guard was retired before SMTP's final reply and completion")
	} else if !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatal(err)
	}
	if owned.accounts().Joined() {
		t.Fatal("account/mail group reported joined before protocol completion")
	}
	select {
	case <-done:
		t.Fatal("root returned while the SMTP reply was paused")
	default:
	}
	if err := network.Release(databaseTestContext(t), endpoint.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if result != nil {
			t.Fatal("root failed graceful SMTP shutdown", result)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("SMTP completion exceeded the original root shutdown budget")
	}
	conn := db.Connect(t)
	var phase, guardState string
	var terminal, joined bool
	var audits, leases, active int
	err = conn.QueryRow(databaseTestContext(t), `SELECT j.phase,a.terminal,a.io_joined,(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery'),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=a.id),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_id=a.id AND NOT released) FROM agenteam_account.mail_jobs j JOIN agenteam_account.mail_attempts a ON a.job_id=j.id WHERE j.id=$1`, job.JobID.String()).Scan(&phase, &terminal, &joined, &audits, &leases, &active)
	if err != nil || phase != "sent" || !terminal || !joined || audits != 1 || leases != 1 || active != 0 {
		t.Fatal("graceful root exit lost the real SMTP completion/lease/Audit transaction", err, phase, terminal, joined, audits, leases, active)
	}
	if err := conn.QueryRow(databaseTestContext(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, objects.process.String()).Scan(&guardState); err != nil || guardState != "stopped" {
		t.Fatal("joined root did not retire its exact persistent guard", err)
	}
	if err := syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal("joined root kept the actual guard file locked", err)
	}
	_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
	if bytes.Contains([]byte(output.String()), []byte("root-owned-SMTP-password-sentinel")) {
		t.Fatal("SMTP credentials escaped into root diagnostics")
	}
}

// Transparent observation of the real lifecycle call; all work, errors and
// Joined evidence are provided by the actual Account/Mail Runtime.
type b04ObservedAccountDrain struct {
	accountRuntime
	entered chan struct{}
	once    sync.Once
}

func (r *b04ObservedAccountDrain) Drain(ctx context.Context) error {
	r.once.Do(func() { close(r.entered) })
	return r.accountRuntime.Drain(ctx)
}

// These two observers delegate every operation to the real owners. Their only
// additional effect is recording when the production Force calls occur.
type b04ResponseDatabase struct{ *postgres.Store }

func (d b04ResponseDatabase) ForceClose(ctx context.Context) error {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "response_db_force_started", "expired": ctx.Err() != nil})
	err := d.Store.ForceClose(ctx)
	fmt.Fprintln(os.Stdout, `{"event":"response_db_force_returned"}`)
	return err
}

type b04ResponseAccount struct{ accountStorage }

func (a b04ResponseAccount) Force(ctx context.Context) error {
	fmt.Fprintln(os.Stdout, `{"event":"response_account_force_started"}`)
	err := a.accountStorage.Force(ctx)
	fmt.Fprintln(os.Stdout, `{"event":"response_account_force_returned"}`)
	return err
}

func TestB04AppResponseUseChild(t *testing.T) {
	if os.Getenv("AGENTEAM_B04_RESPONSE_CHILD") != "1" {
		return
	}
	descriptor, err := pgfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = descriptor.Config(os.Getenv("AGENTEAM_B04_RESPONSE_DATABASE"), nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(os.LookupEnv, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	logger, err := logging.New(logging.Central, slog.LevelInfo, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	var owner atomic.Pointer[resources]
	var core atomic.Pointer[account.Service]
	deps := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			store, err := postgres.Open(ctx, cfg)
			if err != nil {
				return nil, err
			}
			return b04ResponseDatabase{store}, nil
		},
		observeAccount: func(service *account.Service) { core.Store(service) },
		bind: func(ctx context.Context, cfg config.Config, db database, owned *resources, d *dependencies) error {
			owner.Store(owned)
			if err := bindAccounts(ctx, cfg, db, owned, d); err != nil {
				return err
			}
			owned.mu.Lock()
			owned.accountService = b04ResponseAccount{owned.accountService}
			owned.mu.Unlock()
			return nil
		},
	}
	done := make(chan struct{})
	var runErr error
	go func() { runErr = run(context.Background(), cfg, logger, signals, deps); close(done) }()
	input := bufio.NewScanner(os.Stdin)
	if !input.Scan() || input.Text() != "hold" {
		t.Fatal("owned parent did not admit a material use")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	response, err := fixtureAccountLoginResponse(ctx, cfg, core.Load())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	entered := make(chan struct{})
	used := make(chan error, 1)
	go func() {
		used <- response.UseCookie(func(raw []byte) error {
			if len(raw) == 0 {
				return errors.New("real response contained no material")
			}
			close(entered)
			<-release // The real material consumer has not returned yet.
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-used:
		t.Fatal("real material use did not enter", err)
	case <-ctx.Done():
		t.Fatal("real material use did not enter before its deadline")
	}
	objects := owner.Load().objects().(*objectAssembly)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "response_use_entered", "attempt_id": response.AttemptID().String(), "process_id": objects.process.String()})
	<-done
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "response_root_returned", "failed": runErr != nil, "joined": owner.Load().producersJoined(), "response_joined": response.Joined()})
	exposed := false
	if err := response.UseCookie(func([]byte) error { exposed = true; return nil }); err == nil || exposed {
		t.Fatal("forced root admitted a new material use")
	}
	if !input.Scan() || input.Text() != "release" {
		t.Fatal("owned parent did not release its exact material consumer")
	}
	close(release)
	select {
	case err := <-used:
		if err != nil {
			t.Fatal("admitted material consumer failed", err)
		}
	case <-ctx.Done():
		t.Fatal("released material consumer failed to join")
	}
	closed, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	closeErr := response.Close(closed)
	closeCancel()
	if !response.Joined() {
		t.Fatal("material Close returned without actual read/release work joining")
	}
	// Root has already forced the real database closed. A release checkpoint
	// may therefore remain for recovery; actual consumer join is separate.
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "response_use_joined", "cleanup_failed": closeErr != nil})
	if !input.Scan() || input.Text() != "exit" {
		t.Fatal("owned parent did not finish the exact child")
	}
}

func TestB04AppResponseUseKeepsGuardUntilExactChildExit(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	env := guardEnvironment(t, db)
	cfg := guardConfiguration(t, env)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestB04AppResponseUseChild$", "-test.timeout=40s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = append(env, "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env), "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_B04_RESPONSE_CHILD=1", "AGENTEAM_B04_RESPONSE_DATABASE="+db.Name)
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = p.stdin.Close()
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			await(t, p.done)
		}
	})
	p.stderr.wait(t, func(e map[string]any) bool { return e["event"] == "listening" })
	if _, err := io.WriteString(p.stdin, "hold\n"); err != nil {
		t.Fatal(err)
	}
	held := p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "response_use_entered" })
	processID, ok := held["process_id"].(string)
	if !ok || processID == "" {
		t.Fatal("owned child did not identify its actual process")
	}
	attemptID, ok := held["attempt_id"].(string)
	if !ok || attemptID == "" {
		t.Fatal("owned child did not identify its actual material read")
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.stderr.wait(t, func(e map[string]any) bool { return e["phase"] == string(logging.Stopping) })
	started := time.Now()
	if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	forced := p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "response_db_force_started" })
	returned := p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "response_root_returned" })
	if time.Since(started) > 1500*time.Millisecond || forced["expired"] != true || returned["failed"] != true || returned["joined"] != false || returned["response_joined"] != false {
		t.Fatal("actual material use was treated as joined or received a renewed force budget")
	}
	text := p.stdout.String()
	accountAt := strings.Index(text, `"event":"response_account_force_returned"`)
	dbAt := strings.Index(text, `"event":"response_db_force_started"`)
	if accountAt < 0 || dbAt <= accountAt {
		t.Fatal("real database force did not happen last after Account/Mail")
	}
	conn := db.Connect(t)
	var state string
	if err := conn.QueryRow(databaseTestContext(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, processID).Scan(&state); err != nil || state != "claimed" {
		t.Fatal("unjoined material use retired its persistent guard", err)
	}
	var activeLeases, connections int
	if err := conn.QueryRow(databaseTestContext(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_response' AND owner_id=$1 AND NOT released`, attemptID).Scan(&activeLeases); err != nil || activeLeases != 1 {
		t.Fatal("running material use lost its exact real Secret lease", err)
	}
	if err := conn.QueryRow(databaseTestContext(t), `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam'`).Scan(&connections); err != nil || connections != 0 {
		t.Fatal("expired final force left real root database sockets", err)
	}
	claim, err := os.OpenFile(filepath.Join(cfg.Objects().SpoolDirectory()+".processes", processID+".claim"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	assertHeld := func() {
		t.Helper()
		select {
		case <-p.done:
			t.Fatal("owned child exited before its guard was checked")
		default:
		}
		if err := syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
			t.Fatal("unretired process lost its actual guard flock")
		} else if !errors.Is(err, syscall.EWOULDBLOCK) {
			t.Fatal(err)
		}
	}
	assertHeld()
	if _, err := io.WriteString(p.stdin, "release\n"); err != nil {
		t.Fatal(err)
	}
	p.stdout.wait(t, func(e map[string]any) bool { return e["event"] == "response_use_joined" })
	assertHeld()
	if _, err := io.WriteString(p.stdin, "exit\n"); err != nil {
		t.Fatal(err)
	}
	p.wait(t, 0)
	if err := syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal("exact exited child retained its guard flock", err)
	}
	_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
	if strings.Contains(p.stdout.String()+p.stderr.String(), cfg.AccountRecoveryLog()) {
		t.Fatal("root leaked its private bootstrap/recovery path")
	}
}

// The D06 guard fixture retains its custom producer and handler. Its Account,
// Secret, Avatar, Object and background owners use the real D07 construction
// and startup; only its eventual Outbox runtime is the fixture's typed runtime.
func guardAccountAssembly(t *testing.T, cfg config.Config, store *postgres.Store) (*resources, *objectAssembly) {
	t.Helper()
	ctx := databaseTestContext(t)
	owned := &resources{db: store}
	deps := dependencies{}
	if err := bindAccounts(ctx, cfg, store, owned, &deps); err != nil {
		t.Fatal("guard account construction failed", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		cleanupResources(ctx, owned, nil)
	})
	auditor, err := deps.security(ctx, cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	owned.setAudit(auditor)
	secrets, err := deps.secret(ctx, cfg, store, auditor)
	if err != nil {
		t.Fatal(err)
	}
	if !owned.startMaintenance(ctx, secrets) {
		t.Fatal("secret maintenance owner rejected")
	}
	if _, err = deps.outbound(ctx, cfg, store, auditor); err != nil {
		t.Fatal(err)
	}
	objects, err := deps.objects(ctx, cfg, store, auditor)
	if err != nil {
		t.Fatal(err)
	}
	if err = objects.StartMaintenance(context.WithoutCancel(ctx)); err != nil {
		t.Fatal(err)
	}
	if err = owned.accounts().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = owned.accounts().Check(ctx); err != nil {
		t.Fatal(err)
	}
	// No worker was started on the standard runtime; the test installs its
	// original callback runtime and preserves all its actual-join assertions.
	standard := owned.outbox()
	standard.StopClaims()
	if err = standard.Drain(ctx); err != nil || !standard.Joined() {
		t.Fatal("unstarted typed runtime failed to join")
	}
	return owned, objects.(*objectAssembly)
}
