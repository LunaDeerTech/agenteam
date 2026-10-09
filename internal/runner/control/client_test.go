package control

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type clientIdentity struct {
	mu      sync.Mutex
	value   identity.Identity
	loadErr error
	saved   []identity.Identity
	save    func(identity.Identity) error
	closed  atomic.Bool
}

func TestClientFailedActivationKeepsPendingAndJoins(t *testing.T) {
	id := deviceIdentity(t)
	file := &clientIdentity{value: id, save: func(identity.Identity) error { return identity.ErrUnavailable }}
	socket := clientSocket()
	challenge := freshChallenge(t)
	device := &clientDevice{
		challengeFn: func(context.Context, p.ID) (p.ChallengeResponse, error) { return challenge, nil },
		connectFn:   func(context.Context, identity.Identity, p.Nonce) (*wire, error) { return wireWithSocket(socket), nil },
	}
	c := newClientForTest(t, Options{}, file, device)
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background()) }()
	if nextMessage(t, socket).Type() != p.HelloType {
		t.Fatal("missing hello")
	}
	socket.in <- helloAck(t)
	if err := awaitClient(t, done); err != identity.ErrUnavailable {
		t.Fatal("local durable failure became remote success", err)
	}
	if len(file.snapshot()) != 0 || !file.closed.Load() || !device.closed.Load() {
		t.Fatal("failed activation published or retained an owner")
	}
	select {
	case <-socket.closed:
	default:
		t.Fatal("failed activation left its authenticated wire open")
	}
}

func TestClientConstructionIsOfflineAndCopiesOptions(t *testing.T) {
	id := deviceIdentity(t)
	config := id.Configuration()
	token, _ := p.NewEnrollmentToken()
	tokenWire, _ := token.Wire()
	options := Options{IdentityFile: "/PRIVATE_CLIENT_CANARY/identity.json", Configuration: &config, EnrollmentToken: token, RunnerVersion: "fixture-1", Roots: x509.NewCertPool()}
	c, err := New(options)
	if err != nil {
		t.Fatal("construction performed filesystem access", err)
	}
	config.RootPath = "/changed"
	if c.options.Configuration.RootPath != id.Configuration().RootPath || c.options.Roots == options.Roots {
		t.Fatal("construction retained mutable caller options")
	}
	for _, value := range []any{options, c} {
		var logs bytes.Buffer
		slog.New(slog.NewJSONHandler(&logs, nil)).Info("client", "value", value)
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		text := fmt.Sprintf("%+v %#v", value, value) + string(raw) + logs.String()
		if strings.Contains(text, "PRIVATE_CLIENT_CANARY") || strings.Contains(text, tokenWire) {
			t.Fatal("default client output leaked identity path/token")
		}
	}
	c.Stop()
	if c.Force(context.Background()) != nil || c.Run(context.Background()) != ErrClosed {
		t.Fatal("offline stop admitted a later run")
	}
}

func (f *clientIdentity) Load() (identity.Identity, error) { return f.value, f.loadErr }
func (f *clientIdentity) Save(id identity.Identity) error {
	if f.save != nil {
		if err := f.save(id); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, id)
	f.value = id
	return nil
}
func (f *clientIdentity) Close() error { f.closed.Store(true); return nil }
func (f *clientIdentity) snapshot() []identity.Identity {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]identity.Identity(nil), f.saved...)
}

type clientDevice struct {
	challengeFn func(context.Context, p.ID) (p.ChallengeResponse, error)
	enrollFn    func(context.Context, identity.Identity, p.EnrollmentToken, string, string) (p.EnrollmentResponse, error)
	connectFn   func(context.Context, identity.Identity, p.Nonce) (*wire, error)
	closed      atomic.Bool
}

func (d *clientDevice) challenge(ctx context.Context, id p.ID) (p.ChallengeResponse, error) {
	return d.challengeFn(ctx, id)
}
func (d *clientDevice) enroll(ctx context.Context, id identity.Identity, token p.EnrollmentToken, platform, arch string) (p.EnrollmentResponse, error) {
	return d.enrollFn(ctx, id, token, platform, arch)
}
func (d *clientDevice) connect(ctx context.Context, id identity.Identity, nonce p.Nonce) (*wire, error) {
	return d.connectFn(ctx, id, nonce)
}
func (d *clientDevice) close() { d.closed.Store(true) }
func newClientForTest(t *testing.T, options Options, file ownedIdentity, device deviceBackend) *Client {
	t.Helper()
	if options.IdentityFile == "" {
		options.IdentityFile = "/owned/private/identity.json"
	}
	options.RunnerVersion = "fixture-1"
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	c.open = func(string) (ownedIdentity, error) { return file, nil }
	c.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) { return device, nil }
	t.Cleanup(c.Stop)
	return c
}
func freshChallenge(t *testing.T) p.ChallengeResponse {
	t.Helper()
	nonce, _ := p.NewNonce()
	at, _ := p.NewInstant(time.Now().Add(30 * time.Second))
	r, err := p.NewChallengeResponse(nonce, at)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func clientSocket() *sessionSocket {
	return &sessionSocket{in: make(chan p.Message, 16), out: make(chan p.Message, 64), closed: make(chan struct{})}
}
func awaitClient(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("original client did not return")
		return nil
	}
}

func TestClientLostEnrollmentUsesOriginalPendingKey(t *testing.T) {
	for _, enroll := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing-pending", true: "lost-enrollment"}[enroll], func(t *testing.T) {
			original := deviceIdentity(t)
			configuration := original.Configuration()
			token, _ := p.NewEnrollmentToken()
			file := &clientIdentity{value: original}
			options := Options{Configuration: &configuration}
			if enroll {
				file.value = identity.Identity{}
				file.loadErr = os.ErrNotExist
				options.EnrollmentToken = token
			}
			socket := clientSocket()
			challenge := freshChallenge(t)
			activated := make(chan struct{})
			var enrollCalls, connectCalls int
			var key [32]byte
			file.save = func(id identity.Identity) error {
				if id.State() == identity.Active {
					close(activated)
				}
				return nil
			}
			device := &clientDevice{}
			device.enrollFn = func(_ context.Context, id identity.Identity, got p.EnrollmentToken, _, _ string) (p.EnrollmentResponse, error) {
				enrollCalls++
				saved := file.snapshot()
				if len(saved) != 1 || saved[0] != id || id.State() != identity.Pending || got != token {
					t.Error("request attempted before original pending key persisted")
				}
				key, _ = id.PublicKey()
				return p.EnrollmentResponse{}, ErrTransport
			}
			device.challengeFn = func(_ context.Context, id p.ID) (p.ChallengeResponse, error) {
				if id != configuration.RunnerID {
					t.Error("challenge switched runner")
				}
				return challenge, nil
			}
			device.connectFn = func(_ context.Context, id identity.Identity, nonce p.Nonce) (*wire, error) {
				connectCalls++
				got, _ := id.PublicKey()
				if enroll && got != key || !enroll && id != original || nonce != challenge.Nonce() {
					t.Error("lost response recovery replaced original key/nonce")
				}
				return wireWithSocket(socket), nil
			}
			c := newClientForTest(t, options, file, device)
			done := make(chan error, 1)
			go func() { done <- c.Run(context.Background()) }()
			sent := nextMessage(t, socket)
			if sent.Type() != p.HelloType {
				t.Fatal("connection skipped hello")
			}
			h := sent.Payload().(p.Hello)
			if h.RunnerID != configuration.RunnerID || len(h.Capabilities) != 0 || len(h.FeatureFlags) != 0 {
				t.Fatal("unbound production operation advertised")
			}
			socket.in <- helloAck(t)
			await(t, activated)
			c.Stop()
			if err := awaitClient(t, done); !errors.Is(err, context.Canceled) {
				t.Fatal("stop result", err)
			}
			if c.Drain(context.Background()) != nil || !file.closed.Load() || !device.closed.Load() || connectCalls != 1 || enrollCalls != map[bool]int{false: 0, true: 1}[enroll] {
				t.Fatal("recovery replayed enrollment or did not join owners")
			}
			saved := file.snapshot()
			last := saved[len(saved)-1]
			if last.State() != identity.Active {
				t.Fatal("authenticated original key was not persisted active")
			}
		})
	}
}
func TestClientRejectedPendingDoesNotReplayEnrollment(t *testing.T) {
	id := deviceIdentity(t)
	file := &clientIdentity{value: id}
	calls := 0
	device := &clientDevice{challengeFn: func(context.Context, p.ID) (p.ChallengeResponse, error) {
		calls++
		return p.ChallengeResponse{}, ErrAuthentication
	}, enrollFn: func(context.Context, identity.Identity, p.EnrollmentToken, string, string) (p.EnrollmentResponse, error) {
		t.Error("implicit enrollment attempted")
		return p.EnrollmentResponse{}, ErrProtocol
	}}
	c := newClientForTest(t, Options{}, file, device)
	if err := c.Run(context.Background()); err != ErrAuthentication || calls != 1 || len(file.snapshot()) != 0 || !file.closed.Load() || !device.closed.Load() {
		t.Fatal("unknown credential was retried or activated", err, calls)
	}
}
func TestClientActiveBackoffAndCancellation(t *testing.T) {
	pending := deviceIdentity(t)
	active, _ := pending.AsActive()
	file := &clientIdentity{value: active}
	var bounds []time.Duration
	device := &clientDevice{challengeFn: func(context.Context, p.ID) (p.ChallengeResponse, error) {
		return p.ChallengeResponse{}, ErrAuthentication
	}}
	c := newClientForTest(t, Options{}, file, device)
	c.jitter = func(bound time.Duration) (time.Duration, error) { bounds = append(bounds, bound); return bound, nil }
	c.wait = func(ctx context.Context, delay time.Duration) error {
		if len(bounds) == 7 {
			c.Stop()
			return ctx.Err()
		}
		return nil
	}
	if err := c.Run(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	if !reflect.DeepEqual(bounds, want) {
		t.Fatal("reconnect did not preserve full jitter ceilings", bounds)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := waitReconnect(ctx, 30*time.Second); err != context.Canceled || time.Since(started) > time.Second {
		t.Fatal("stop did not interrupt backoff")
	}
	for _, bound := range want {
		for range 20 {
			delay, err := reconnectJitter(bound)
			if err != nil || delay < 0 || delay > bound {
				t.Fatal("jitter exceeded cap", err)
			}
		}
	}
}
func TestClientLocalFailuresDoNotStartNetwork(t *testing.T) {
	id := deviceIdentity(t)
	configuration := id.Configuration()
	wrong := configuration
	wrong.RootPath = "/other"
	for _, kind := range []string{"mismatch", "missing", "pending-save", "stopped"} {
		t.Run(kind, func(t *testing.T) {
			file := &clientIdentity{value: id}
			options := Options{Configuration: &configuration}
			network := 0
			opens := 0
			switch kind {
			case "mismatch":
				options.Configuration = &wrong
			case "missing":
				file.loadErr = os.ErrNotExist
			case "pending-save":
				token, _ := p.NewEnrollmentToken()
				options.EnrollmentToken = token
				file.save = func(identity.Identity) error { return identity.ErrUnavailable }
			}
			c := newClientForTest(t, options, file, nil)
			c.open = func(string) (ownedIdentity, error) { opens++; return file, nil }
			c.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) {
				network++
				return nil, ErrTransport
			}
			if kind == "stopped" {
				c.Stop()
			}
			if err := c.Run(context.Background()); err == nil || network != 0 || kind == "stopped" && opens != 0 {
				t.Fatal("local rejection opened native work", err, network, opens)
			}
			if err := c.Drain(context.Background()); err != nil {
				t.Fatal("failed start retained owner", err)
			}
		})
	}
}
func TestClientForceWaitsForActualReturnAndIdentityLock(t *testing.T) {
	// Use the actual Unix owner here, not a boolean lock fake. Only the remote
	// transport is controlled; no native socket is opened by this unit test.
	directory, err := os.MkdirTemp(".", ".client-identity-")
	if err != nil {
		t.Fatal(err)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(directory, "identity.json")
	id := deviceIdentity(t)
	id, _ = id.AsActive()
	owner, err := identity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.Save(id); err != nil {
		t.Fatal(err)
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	requestContext := make(chan context.Context, 1)
	device := &clientDevice{challengeFn: func(ctx context.Context, _ p.ID) (p.ChallengeResponse, error) {
		requestContext <- ctx
		close(entered)
		<-release
		return p.ChallengeResponse{}, ErrTransport
	}}
	c, err := New(Options{IdentityFile: path, RunnerVersion: "fixture-1"})
	if err != nil {
		t.Fatal(err)
	}
	c.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) { return device, nil }
	t.Cleanup(c.Stop)
	done := make(chan error, 1)
	go func() { done <- c.Run(context.Background()) }()
	await(t, entered)
	original := <-requestContext
	c.Stop()
	await(t, original.Done())
	force, finish := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer finish()
	if err := c.Force(force); err != context.DeadlineExceeded {
		t.Fatal("force fabricated actual return", err)
	}
	if device.closed.Load() {
		t.Fatal("transport closed before admitted callback returned")
	}
	if other, err := identity.Open(path); err != identity.ErrLocked {
		if other != nil {
			other.Close()
		}
		t.Fatal("identity lock released before actual network join", err)
	}
	select {
	case <-done:
		t.Fatal("original client returned while transport callback still held")
	default:
	}
	close(release)
	if err := awaitClient(t, done); err != context.Canceled {
		t.Fatal(err)
	}
	if err = c.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	next, err := identity.Open(path)
	if err != nil {
		t.Fatal("actual join failed to release original lock", err)
	}
	if err = next.Close(); err != nil {
		t.Fatal(err)
	}
	if !device.closed.Load() {
		t.Fatal("device transport did not close after original return")
	}
}
