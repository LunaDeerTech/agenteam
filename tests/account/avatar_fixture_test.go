//go:build integration

package account_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

type avatarFixture struct {
	*b02Fixture
	profiles  *account.ProfileService
	objects   *object.Service
	avatar    *account.AvatarAuthority
	guard     *object.ProcessGuard
	actor     identity.Actor
	s3        *minio.Client
	bucket    string
	spoolPath string
	deaths    *avatarDeaths
}

func newAvatarFixture(t *testing.T) *avatarFixture {
	t.Helper()
	return assembleAvatarFixture(t, newB02Account(t))
}
func assembleAvatarFixture(t *testing.T, f *b02Fixture) *avatarFixture {
	t.Helper()
	remote, e := objectfixture.Load()
	if e != nil {
		t.Fatal(e)
	}
	s3, transport, e := remote.Client()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(transport.CloseIdleConnections)
	suffix, e := pgfixture.RandomHex(8)
	if e != nil {
		t.Fatal(e)
	}
	bucket := "d07-avatar-" + remote.Nonce[:12] + "-" + suffix
	if e = s3.MakeBucket(ctxFor(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); e != nil {
		t.Fatal("owned bucket creation", e)
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	cfg, e := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if e != nil {
		t.Fatal(e)
	}
	backend, e := object.NewBackend(cfg)
	if e != nil {
		t.Fatal(e)
	}
	process, _ := foundation.ParseID[oc.Process](f.process.id.String())
	spoolPath := filepath.Join(t.TempDir(), "spool")
	spool, e := object.OpenSpool(spoolPath, process)
	if e != nil {
		t.Fatal(e)
	}
	guard, e := object.OpenProcessGuard(spool, process)
	if e != nil {
		t.Fatal(e)
	}
	a, e := account.NewAvatarAuthority(f.service)
	if e != nil {
		t.Fatal(e)
	}
	_, pagination, _ := keys(t)
	aud, e := audit.New(f.store, pagination, audit.Authorizations{Sessions: f.authority, System: f.authority, Accounts: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	objects, e := object.New(f.store, backend, spool, aud, object.Authorizations{Planner: a, Resources: a, Read: a, Gate: a, Cleanup: a, Leases: a, Processes: guard})
	if e != nil {
		t.Fatal(e)
	}
	if e = objects.Initialize(ctxFor(t)); e != nil {
		t.Fatal("object init", e)
	}
	profiles, e := account.NewProfileService(f.service, objects)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		objects.StopAdmission()
		if e := objects.Drain(ctx); e != nil {
			_ = objects.Force(ctx)
			t.Error(e)
		}
		if e := guard.Close(); e != nil {
			t.Error(e)
		}
	})
	password := f.bootstrap(t)
	request, _ := loginRequest(t, f.fixture, password, "admin@mail.com")
	response, e := f.service.Login(ctxFor(t), request)
	if e != nil {
		t.Fatal("login", e)
	}
	cookie := useCookie(t, response)
	actor, e := f.service.Authenticate(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return &avatarFixture{b02Fixture: f, profiles: profiles, objects: objects, avatar: a, guard: guard, actor: actor, s3: s3, bucket: bucket, spoolPath: spoolPath}
}

// This adapter consumes Object's real host/boot/flock proof. It cannot declare
// a live child stopped and does not infer death from a test timer or DB loss.
type avatarGuardProcess struct {
	guard *object.ProcessGuard
	id    c.ProcessID
}

type avatarDeathEntry struct {
	done  <-chan struct{}
	spool string
	proof *object.ProcessGuard
}
type avatarDeaths struct {
	mu      sync.Mutex
	entries map[string]*avatarDeathEntry
}

func (d *avatarDeaths) confirm(ctx context.Context, process oc.ProcessID) error {
	d.mu.Lock()
	entry := d.entries[process.String()]
	var proof *object.ProcessGuard
	if entry != nil {
		proof = entry.proof
	}
	d.mu.Unlock()
	if entry == nil {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	select {
	case <-entry.done:
	default:
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	if proof == nil {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	// Kill+Wait is necessary but not the complete proof: the real guard also
	// checks the old deployment, host/boot and exact original spool claim.
	return proof.ConfirmStopped(ctx, process)
}

type avatarAccountDeaths struct {
	*avatarDeaths
	current c.ProcessID
}

func (p avatarAccountDeaths) CurrentProcess() c.ProcessID { return p.current }
func (p avatarAccountDeaths) ConfirmStopped(ctx context.Context, id c.ProcessID) error {
	v, e := foundation.ParseID[oc.Process](id.String())
	if e != nil {
		return e
	}
	return p.confirm(ctx, v)
}

type avatarObjectDeaths struct{ *avatarDeaths }

func (p avatarObjectDeaths) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	return p.confirm(ctx, id)
}

func (p avatarGuardProcess) CurrentProcess() c.ProcessID { return p.id }
func (p avatarGuardProcess) ConfirmStopped(ctx context.Context, id c.ProcessID) error {
	process, e := foundation.ParseID[oc.Process](id.String())
	if e != nil {
		return e
	}
	return p.guard.ConfirmStopped(ctx, process)
}

// Reopens only resources passed by the parent owned fixture; no configuration
// fallback or existing infrastructure is consulted. The User/Session was made
// by real Bootstrap/Login, and all domain authorizations remain production ones.
func reopenAvatarFixture(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store accountTestStore, authority *account.Authority, bucket, spoolPath string, process c.ProcessID, actor identity.Actor, deaths ...*avatarDeaths) *avatarFixture {
	t.Helper()
	remote, e := objectfixture.Load()
	if e != nil {
		t.Fatal(e)
	}
	s3, transport, e := remote.Client()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(transport.CloseIdleConnections)
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	cfg, e := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if e != nil {
		t.Fatal(e)
	}
	backend, e := object.NewBackend(cfg)
	if e != nil {
		t.Fatal(e)
	}
	pid, e := foundation.ParseID[oc.Process](process.String())
	if e != nil {
		t.Fatal(e)
	}
	spool, e := object.OpenSpool(spoolPath, pid)
	if e != nil {
		t.Fatal(e)
	}
	guard, e := object.OpenProcessGuard(spool, pid)
	if e != nil {
		t.Fatal(e)
	}
	var accountProcesses c.ProcessAuthority = avatarGuardProcess{guard, process}
	var objectProcesses oc.ProcessAuthority = guard
	var registry *avatarDeaths
	if len(deaths) > 0 {
		registry = deaths[0]
		accountProcesses = avatarAccountDeaths{registry, process}
		objectProcesses = avatarObjectDeaths{registry}
	}
	base := assembleAccount(t, raw, store, authority, accountProcesses)
	base.db = db
	owner, e := account.NewAvatarAuthority(base.service)
	if e != nil {
		t.Fatal(e)
	}
	_, cursor, _ := keys(t)
	aud, e := audit.New(store, cursor, audit.Authorizations{Sessions: authority, System: authority, Accounts: authority})
	if e != nil {
		t.Fatal(e)
	}
	objects, e := object.New(store, backend, spool, aud, object.Authorizations{Planner: owner, Resources: owner, Read: owner, Gate: owner, Cleanup: owner, Leases: owner, Processes: objectProcesses})
	if e != nil {
		t.Fatal(e)
	}
	var runtime *object.Runtime
	if registry == nil {
		endpoint, err := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, cfg)
		if err != nil {
			t.Fatal(err)
		}
		transfers, err := object.NewTransferService(objects, nil, endpoint)
		if err != nil {
			t.Fatal(err)
		}
		runtime, e = object.NewRuntime(objects, guard, transfers)
		if e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		objects.StopAdmission()
		if runtime != nil {
			if e := runtime.Drain(ctx); e != nil {
				_ = runtime.Force(ctx)
				t.Error(e)
			}
			return
		}
		if e := objects.Drain(ctx); e != nil {
			_ = objects.Force(ctx)
			t.Error(e)
		}
		if e := guard.Close(); e != nil {
			t.Error(e)
		}
	})
	if runtime == nil {
		e = objects.Initialize(ctxFor(t))
	} else {
		// This is the formal claim binding path; Service.Initialize alone
		// initializes storage but cannot prove a ProcessGuard's DB identity.
		e = runtime.Initialize(ctxFor(t))
	}
	if e != nil {
		t.Fatal("reopen object runtime/storage", e, safeFailure(e))
	}
	profiles, e := account.NewProfileService(base.service, objects)
	if e != nil {
		t.Fatal(e)
	}
	return &avatarFixture{b02Fixture: &b02Fixture{fixture: base, process: liveProcess{process}}, profiles: profiles, objects: objects, avatar: owner, guard: guard, actor: actor, s3: s3, bucket: bucket, spoolPath: spoolPath, deaths: registry}
}
func avatarPNG(t *testing.T, seed byte) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, 47, 31))
	for y := 0; y < 31; y++ {
		for x := 0; x < 47; x++ {
			m.SetNRGBA(x, y, color.NRGBA{R: byte(x) + seed, G: byte(y * 3), B: seed, A: 255})
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, m); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}

// Fixed high-entropy input remains a legal bounded image. After the production
// JPEG transform its payload exceeds Object's 64 KiB integrity tail, so a newly
// opened reader still owns real I/O and an active lease before Close.
func avatarNoisePNG(t *testing.T) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	state := uint32(0x89abcdef)
	for y := range 512 {
		for x := range 512 {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			m.SetNRGBA(x, y, color.NRGBA{R: byte(state), G: byte(state >> 8), B: byte(state >> 16), A: 255})
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, m); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func (f *avatarFixture) putAvatar(t *testing.T, key string, version foundation.Version, seed byte) c.ProfileView {
	t.Helper()
	raw := avatarPNG(t, seed)
	view, e := f.profiles.PutAvatar(ctxFor(t), c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: f.actor, Key: foundation.IdempotencyKey(key), ExpectedVersion: version}, MediaType: "image/png", ByteSize: int64(len(raw)), Body: io.NopCloser(bytes.NewReader(raw))})
	if e != nil {
		t.Fatalf("avatar put %v [%s]", e, safeFailure(e))
	}
	return view
}
func (f *avatarFixture) avatarID(t *testing.T) oc.ObjectID {
	t.Helper()
	var raw string
	if e := f.store.QueryRow(ctxFor(t), `SELECT avatar_object_id::text FROM agenteam_account.users WHERE id=$1`, f.actor.Details().UserID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	v, e := foundation.ParseID[oc.StoredObject](raw)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func requireAvatarFault(t *testing.T, e error, code foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(e, &f) || f.Code != code {
		t.Fatalf("want %s, got %v [%s]", code, e, safeFailure(e))
	}
}
