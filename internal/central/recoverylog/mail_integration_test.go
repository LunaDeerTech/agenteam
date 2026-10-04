//go:build integration

package recoverylog_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func mailCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func mailID[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	id, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func mailAwait(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(4 * time.Second):
		t.Fatal("owned boundary not reached")
	}
}

// This wrapper always performs real file Write/Sync/Close. Its barriers control
// when those syscalls can complete; no success result or Done is synthesized.
type heldMailFile struct {
	file                                         *os.File
	stage                                        string
	armed, fired                                 atomic.Bool
	entered, release, closeEntered, closeRelease chan struct{}
	blockClose                                   bool
	writes                                       atomic.Int64
	releaseOnce, closeOnce                       sync.Once
}

func (w *heldMailFile) pause(stage string) {
	if w.stage == stage && w.armed.Load() && w.fired.CompareAndSwap(false, true) {
		close(w.entered)
		<-w.release
	}
}
func (w *heldMailFile) Write(b []byte) (int, error) {
	w.pause("write")
	w.writes.Add(1)
	return w.file.Write(b)
}
func (w *heldMailFile) Sync() error { w.pause("sync"); return w.file.Sync() }
func (w *heldMailFile) Close() error {
	if w.blockClose {
		close(w.closeEntered)
		<-w.closeRelease
	}
	return w.file.Close()
}
func (w *heldMailFile) unblock()      { w.releaseOnce.Do(func() { close(w.release) }) }
func (w *heldMailFile) unblockClose() { w.closeOnce.Do(func() { close(w.closeRelease) }) }

type logProcess struct {
	id    c.ProcessID
	guard *object.ProcessGuard
}

func (p logProcess) CurrentProcess() c.ProcessID { return p.id }
func (p logProcess) ConfirmStopped(ctx context.Context, id c.ProcessID) error {
	v, _ := foundation.ParseID[oc.Process](id.String())
	return p.guard.ConfirmStopped(ctx, v)
}

type observeMailPort struct {
	c.DeliveryPort
	prepared chan c.JobID
	begins   atomic.Int64
}

func (p *observeMailPort) PrepareDelivery(ctx context.Context, a c.DeliveryAttempt) (c.DeliveryMaterials, error) {
	m, e := p.DeliveryPort.PrepareDelivery(ctx, a)
	if e == nil {
		p.prepared <- a.Details().JobID
	}
	return m, e
}
func (p *observeMailPort) BeginDelivery(ctx context.Context, a c.DeliveryAttempt, phase c.DeliveryPhase) (c.SendPermit, error) {
	p.begins.Add(1)
	return p.DeliveryPort.BeginDelivery(ctx, a, phase)
}

type logMailFixture struct {
	s         *account.Service
	store     *postgres.Store
	actor     identity.Actor
	worker    *accountmail.Worker
	runtime   *accountmail.Runtime
	registry  *accountmail.WorkRegistry
	port      *observeMailPort
	sink      *recoverylog.Sink
	file      *heldMailFile
	guard     *object.ProcessGuard
	claimPath string
}

func newLogMail(t *testing.T, stage string, blockClose bool) *logMailFixture {
	t.Helper()
	ctx := mailCtx(t)
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	m, e := postgres.NewMigrator(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if r := m.Migrate(ctx); !r.Migrated {
		t.Fatal(r.Fault)
	}
	store, e := postgres.Open(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		x, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := store.ForceClose(x); e != nil {
			t.Error(e)
		}
	})
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	ck, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	sk, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), ck)
	if e != nil {
		t.Fatal(e)
	}
	dk, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), ck, sk)
	if e != nil {
		t.Fatal(e)
	}
	ak, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), ck, sk, dk)
	if e != nil {
		t.Fatal(e)
	}
	auth, e := account.NewAuthority(store, ak)
	if e != nil {
		t.Fatal(e)
	}
	if e = auth.Initialize(ctx); e != nil {
		t.Fatal(e)
	}
	aud, e := audit.New(store, ck, audit.Authorizations{Sessions: auth, System: auth, Accounts: auth})
	if e != nil {
		t.Fatal(e)
	}
	sec, e := secret.New(store, sk, aud, secret.Authorizations{Sessions: auth, System: auth, Usage: auth, AccountWrites: auth})
	if e != nil {
		t.Fatal(e)
	}
	if e = sec.Initialize(ctx); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	file, e := os.OpenFile(filepath.Join(dir, "recovery.jsonl"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	w := &heldMailFile{file: file, stage: stage, blockClose: blockClose, entered: make(chan struct{}), release: make(chan struct{}), closeEntered: make(chan struct{}), closeRelease: make(chan struct{})}
	sink := recoverylog.IntegrationSink(w)
	pid := mailID[oc.Process](t)
	spool, e := object.OpenSpool(filepath.Join(dir, "spool"), pid)
	if e != nil {
		t.Fatal(e)
	}
	guard, e := object.OpenProcessGuard(spool, pid)
	if e != nil {
		t.Fatal(e)
	}
	processID, _ := foundation.ParseID[c.Process](pid.String())
	process := logProcess{processID, guard}
	catalog := event.NewCatalog()
	revoked, e := c.DefineSessionsRevoked(catalog)
	if e != nil {
		t.Fatal(e)
	}
	delivery, e := c.DefineDeliveryRequested(catalog)
	if e != nil {
		t.Fatal(e)
	}
	events, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]ob.ProducerAuthority{c.AccountProducer: auth}, Sessions: auth, System: auth, Audit: aud, Cursors: ck})
	if e != nil {
		t.Fatal(e)
	}
	s, e := account.New(account.Dependencies{Authority: auth, Audit: aud, Secrets: sec, Events: events, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: process, RecoveryLog: sink})
	if e != nil {
		t.Fatal(e)
	}
	f := &logMailFixture{s: s, store: store, sink: sink, file: w, guard: guard, claimPath: filepath.Join(dir, "spool.processes", pid.String()+".claim")}
	t.Cleanup(func() {
		w.unblock()
		w.unblockClose()
		x, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if f.runtime != nil {
			_ = f.runtime.Force(x)
		}
		_ = s.Force(x)
		if !sink.Joined() {
			t.Error("owned sink still active")
		}
		_ = guard.Close()
		_ = spool.Close()
	})
	boot, e := s.Bootstrap(ctx)
	if e != nil || boot.LogState != "written" {
		t.Fatal("bootstrap compatibility", e)
	}
	raw, e := os.ReadFile(file.Name())
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
	anon, e := s.NewAnonymousContext(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer anon.Cookie.Destroy()
	defer anon.CSRF.Destroy()
	req, e := c.NewLoginRequest(c.LoginFields{Browser: anon.Identity, Key: foundation.IdempotencyKey(mailID[struct{}](t).String()), Email: "admin@mail.com", Password: password, ClientIP: netip.MustParseAddr("192.0.2.3")})
	if e != nil {
		t.Fatal(e)
	}
	response, e := s.Login(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	var cookie sc.SecretMaterial
	if e = response.UseCookie(func(b []byte) error { var e error; cookie, e = sc.NewSecretMaterial(b); return e }); e != nil {
		t.Fatal(e)
	}
	defer cookie.Destroy()
	f.actor, e = s.Authenticate(ctx, cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = response.Close(ctx); e != nil {
		t.Fatal(e)
	}
	policy, e := outbound.NewPolicyService(store, aud, outbound.Authorizations{Sessions: auth, System: auth})
	if e != nil {
		t.Fatal(e)
	}
	if e = policy.Reload(ctx); e != nil {
		t.Fatal(e)
	}
	trust, e := outbound.LoadTrustStore("")
	if e != nil {
		t.Fatal(e)
	}
	client, e := outbound.NewClient(policy, trust, nil)
	if e != nil {
		t.Fatal(e)
	}
	registry, e := accountmail.NewWorkRegistry(process)
	if e != nil {
		t.Fatal(e)
	}
	port, e := account.NewDeliveryPort(s, registry)
	if e != nil {
		t.Fatal(e)
	}
	f.registry = registry
	f.port = &observeMailPort{DeliveryPort: port, prepared: make(chan c.JobID, 2)}
	f.worker, e = accountmail.New(accountmail.Dependencies{Port: f.port, Registry: registry, Outbound: client, Trust: trust, RecoveryLog: sink, PublicOrigin: "https://account.example.test"})
	if e != nil {
		t.Fatal(e)
	}
	f.runtime, e = accountmail.NewRuntime(s, f.worker)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *logMailFixture) invite(t *testing.T) c.InvitationReceipt {
	t.Helper()
	r, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: f.actor, Key: foundation.IdempotencyKey(mailID[struct{}](t).String()), Email: mailID[struct{}](t).String() + "@example.test"})
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.s.CreateInvitation(mailCtx(t), r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ReconcileDeliveryIntents(mailCtx(t)); e != nil {
		t.Fatal(e)
	}
	return v
}
func (f *logMailFixture) revoke(t *testing.T, v c.InvitationReceipt) {
	t.Helper()
	if e := f.s.RevokeInvitation(mailCtx(t), c.InvitationRevoke{Actor: f.actor, Key: foundation.IdempotencyKey(mailID[struct{}](t).String()), ID: v.ID, ExpectedVersion: v.Version}); e != nil {
		t.Fatal(e)
	}
}
func (f *logMailFixture) leases(t *testing.T, want int) {
	t.Helper()
	var n int
	if e := f.store.QueryRow(mailCtx(t), `SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='account_delivery_attempt' AND NOT released`).Scan(&n); e != nil || n != want {
		t.Fatal("live mail leases", n, want, e)
	}
}
func (f *logMailFixture) prepared(t *testing.T, job c.JobID) {
	t.Helper()
	select {
	case got := <-f.port.prepared:
		if got != job {
			t.Fatal("wrong prepared job")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("preparation did not finish")
	}
}
func TestAccountMailLogQueueHeadRechecksRealAuthority(t *testing.T) {
	f := newLogMail(t, "sync", false)
	a, b := f.invite(t), f.invite(t)
	before := f.file.writes.Load()
	f.file.armed.Store(true)
	adone := make(chan error, 1)
	go func() { adone <- f.worker.RunJob(mailCtx(t), a.JobID) }()
	f.prepared(t, a.JobID)
	mailAwait(t, f.file.entered)
	bdone := make(chan error, 1)
	go func() { bdone <- f.worker.RunJob(mailCtx(t), b.JobID) }()
	f.prepared(t, b.JobID)
	f.leases(t, 2)
	f.revoke(t, b)
	if f.port.begins.Load() != 1 {
		t.Fatal("queued B was preauthorized")
	}
	f.leases(t, 2)
	f.file.unblock()
	if e := <-adone; e != nil {
		t.Fatal(e)
	}
	if e := <-bdone; e == nil {
		t.Fatal("revoked queued B wrote")
	}
	if f.file.writes.Load() != before+1 || f.port.begins.Load() != 2 {
		t.Fatal("queue head write/admission count")
	}
	f.leases(t, 0)
	if e := f.runtime.Drain(mailCtx(t)); e != nil || !f.runtime.Joined() {
		t.Fatal("real sink drain", e)
	}
}
func TestAccountMailLogGrantedWriteOutlivesWaitCancellation(t *testing.T) {
	f := newLogMail(t, "write", false)
	a := f.invite(t)
	before := f.file.writes.Load()
	f.file.armed.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.worker.RunJob(ctx, a.JobID) }()
	f.prepared(t, a.JobID)
	mailAwait(t, f.file.entered)
	f.revoke(t, a)
	cancel()
	f.leases(t, 1)
	x, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if e := f.registry.Drain(x); e == nil || f.registry.Joined() {
		t.Fatal("Wait cancellation was treated as actual join")
	}
	if f.file.writes.Load() != before {
		t.Fatal("barrier did not precede actual Write")
	}
	f.file.unblock()
	if e := <-done; e != nil {
		t.Fatal("already granted full Write+Sync", e)
	}
	f.leases(t, 0)
	var phase string
	if e := f.store.QueryRow(mailCtx(t), `SELECT phase FROM agenteam_account.mail_jobs WHERE id=$1`, a.JobID.String()).Scan(&phase); e != nil || phase != "sent" {
		t.Fatal("granted result", phase, e)
	}
	if e := f.runtime.Drain(mailCtx(t)); e != nil || !f.runtime.Joined() {
		t.Fatal(e)
	}
}
func TestAccountMailLogForceRetainsGuardUntilWriteSyncCloseJoin(t *testing.T) {
	for _, stage := range []string{"write", "sync"} {
		t.Run(stage, func(t *testing.T) {
			f := newLogMail(t, stage, true)
			a := f.invite(t)
			f.file.armed.Store(true)
			done := make(chan error, 1)
			go func() { done <- f.worker.RunJob(mailCtx(t), a.JobID) }()
			f.prepared(t, a.JobID)
			mailAwait(t, f.file.entered)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			if e := f.runtime.Force(ctx); e == nil || f.runtime.Joined() {
				t.Fatal("force invented join")
			}
			mailAwait(t, f.file.closeEntered)
			f.leases(t, 1)
			claim, e := os.OpenFile(f.claimPath, os.O_RDWR, 0)
			if e != nil {
				t.Fatal(e)
			}
			defer claim.Close()
			if e = syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != syscall.EWOULDBLOCK {
				t.Fatal("actual guard released with local IO outstanding", e)
			}
			f.file.unblock()
			f.file.unblockClose()
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("actual worker did not join")
			}
			wait, cancelWait := context.WithTimeout(context.Background(), time.Second)
			defer cancelWait()
			if e = f.sink.Drain(wait); e != nil || !f.runtime.Joined() {
				t.Fatal("full sink owner did not join", e)
			}
			if _, e = f.port.RecoverDeliveries(mailCtx(t)); e != nil {
				t.Fatal("expired-force checkpoint recovery", e)
			}
			f.leases(t, 0)
			if e = f.guard.Close(); e != nil {
				t.Fatal(e)
			}
			if e = syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
				t.Fatal("joined guard could not release", e)
			}
			_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
		})
	}
}
