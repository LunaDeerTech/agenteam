//go:build linux

package control

import (
	"context"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func ownedReadFileIdentity(t *testing.T) (string, identity.Configuration) {
	t.Helper()
	directory, err := os.MkdirTemp(".", ".owned-read-file-")
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
	root := filepath.Join(directory, "workspaces")
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	configuration := identity.Configuration{CentralURL: "https://runner.example", RunnerID: sessionID(t), RootPath: root}
	stored, err := identity.NewPending(configuration)
	if err != nil {
		t.Fatal(err)
	}
	stored, err = stored.AsActive()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "identity.json")
	owner, err := identity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.Save(stored); err != nil {
		_ = owner.Close()
		t.Fatal(err)
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	return path, configuration
}

func requireOwnedReadFileIdentityLocked(t *testing.T, path string) {
	t.Helper()
	other, err := identity.Open(path)
	if other != nil {
		_ = other.Close()
	}
	if err != identity.ErrLocked {
		t.Fatal("original identity lock was released before the owner returned", err)
	}
}

func requireOwnedReadFileIdentityReleased(t *testing.T, path string) {
	t.Helper()
	other, err := identity.Open(path)
	if err != nil {
		t.Fatal("original return did not release the identity lock", err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}

func ownedReadFileRequest(t *testing.T, root string) p.Request {
	t.Helper()
	r := request(t)
	r.OperationName, r.OperationRevision = p.ReadFileOperation, p.ReadFileRevision
	directory := filepath.Join(root, string(r.ProjectID), string(r.AgentID), "source")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "file"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	r.Payload, err = p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: "source", Arguments: p.ReadFileArguments{Path: "file", Offset: 1, Length: 3, Encoding: "utf8"}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ownedReadFileDevice(t *testing.T, c *Client, expected identity.Configuration, socket wireSocket) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	challenge := freshChallenge(t)
	device := &clientDevice{
		challengeFn: func(context.Context, p.ID) (p.ChallengeResponse, error) { return challenge, nil },
		connectFn: func(_ context.Context, id identity.Identity, _ p.Nonce) (*wire, error) {
			if id.Configuration() != expected {
				t.Error("connection changed the locked identity configuration")
			}
			return wireWithSocket(socket), nil
		},
	}
	c.device = func(configuration identity.Configuration, _ *x509.CertPool) (deviceBackend, error) {
		calls.Add(1)
		if configuration != expected {
			t.Error("device construction changed the locked identity configuration")
		}
		return device, nil
	}
	return calls
}

type ownedReadFileRun struct {
	client *Client
	done   chan error
	joined bool
}

func startOwnedReadFileRun(t *testing.T, c *Client, release func()) *ownedReadFileRun {
	t.Helper()
	r := &ownedReadFileRun{client: c, done: make(chan error, 1)}
	t.Cleanup(func() {
		c.Stop()
		if release != nil {
			release()
		}
		if !r.joined {
			_ = r.join(t)
		}
	})
	go func() { r.done <- c.Run(context.Background()) }()
	return r
}

func (r *ownedReadFileRun) join(t *testing.T) error {
	t.Helper()
	err := awaitClient(t, r.done)
	r.joined = true
	return err
}

func (r *ownedReadFileRun) requireHeld(t *testing.T, path string) {
	t.Helper()
	force, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.client.Force(force); err != context.Canceled {
		t.Fatal("expired Force fabricated original owner return", err)
	}
	select {
	case <-r.client.done:
		t.Fatal("Client.done preceded its original cleanup return")
	default:
	}
	requireOwnedReadFileIdentityLocked(t, path)
}

func receiveOwnedReadFile[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("original owned read-file boundary did not return")
		var zero T
		return zero
	}
}

func TestOwnedReadFileStoredIdentityAndDefault(t *testing.T) {
	for _, mode := range []string{"", p.ReadFileOperation} {
		t.Run(map[string]string{"": "default", p.ReadFileOperation: "stored-read-file"}[mode], func(t *testing.T) {
			path, configuration := ownedReadFileIdentity(t)
			options := Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: mode}
			c, err := New(options)
			if err != nil {
				t.Fatal(err)
			}
			// Mutation of the caller's value must not change the fixed selection.
			options.Operations = "changed-after-construction"
			var factories atomic.Int32
			c.ownReadFile = func(got identity.Configuration) (operationExecutor, ownedReadFileManager, error) {
				factories.Add(1)
				if got != configuration {
					t.Error("runtime root did not come from the original locked identity")
				}
				other, lockErr := identity.Open(path)
				if other != nil {
					_ = other.Close()
				}
				if lockErr != identity.ErrLocked {
					t.Error("workspace constructed without the original identity lock")
				}
				return newOwnedReadFile(got)
			}
			socket := clientSocket()
			ownedReadFileDevice(t, c, configuration, socket)
			run := startOwnedReadFileRun(t, c, nil)
			hello, ok := nextMessage(t, socket).Payload().(p.Hello)
			if !ok || hello.RunnerID != configuration.RunnerID || len(hello.FeatureFlags) != 0 {
				t.Fatal("owned client did not use the original Hello contract")
			}
			want := int32(0)
			if mode != "" {
				want = 1
			}
			if factories.Load() != want || len(hello.Capabilities) != int(want) || want == 1 && hello.Capabilities[0] != p.ReadFileOperation {
				t.Fatal("operation selection opened or announced the wrong owner")
			}
			socket.in <- helloAck(t)
			request := ownedReadFileRequest(t, configuration.RootPath)
			socket.in <- correlated(t, request, request.RequestID, request.OperationID)
			response, ok := nextMessage(t, socket).Payload().(p.Response)
			if !ok || response.RequestID != request.RequestID || response.OperationID != request.OperationID {
				t.Fatal("owned read lost original response correlation")
			}
			if mode == "" {
				if response.Outcome != p.Failure || response.Code == nil || *response.Code != p.UnsupportedOperation {
					t.Fatal("default New enabled filesystem execution")
				}
			} else {
				result, err := p.DecodeReadFileResult(response.Payload)
				if response.Outcome != p.Success || err != nil || result.Data != "ell" || result.BytesRead != 3 || result.EOF {
					t.Fatal("owned real Handler did not read the exact stored-root range", err)
				}
			}
			c.Stop()
			if err := run.join(t); err != context.Canceled {
				t.Fatal(err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}

func TestOwnedReadFileRejectsBeforeDevice(t *testing.T) {
	for _, mode := range []string{"bad-root", "configuration-conflict", "external-executor"} {
		t.Run(mode, func(t *testing.T) {
			path, configuration := ownedReadFileIdentity(t)
			options := Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: p.ReadFileOperation}
			if mode == "bad-root" {
				if err := os.Remove(configuration.RootPath); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "configuration-conflict" {
				wrong := configuration
				wrong.RootPath = filepath.Join(configuration.RootPath, "different")
				options.Configuration = &wrong
			}
			var c *Client
			var err error
			if mode == "external-executor" {
				handler, _ := readFileFixture(t)
				c, err = NewReadFile(options, handler)
			} else {
				c, err = New(options)
			}
			if err != nil {
				t.Fatal("offline constructor unexpectedly acquired an owner", err)
			}
			var opens, devices atomic.Int32
			c.open = func(path string) (ownedIdentity, error) { opens.Add(1); return identity.Open(path) }
			c.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) {
				devices.Add(1)
				return nil, ErrTransport
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := c.Run(ctx); err == nil || devices.Load() != 0 || mode == "external-executor" && opens.Load() != 0 {
				t.Fatal("invalid configured owner reached device construction or changed external ownership", err)
			}
			if err := c.Drain(context.Background()); err != nil {
				t.Fatal(err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
	if _, err := New(Options{IdentityFile: "/owned/identity.json", Operations: "read-file,run-command-unknown", RunnerVersion: "fixture-1"}); err == nil {
		t.Fatal("direct constructor bypassed the closed operation selection")
	}
}

// The wrapper holds the original composition call before/after the REAL
// Manager.Drain. It does not model a Manager or claim a kernel close is held.
type ownedReadFileDrainBarrier struct {
	manager                    ownedReadFileManager
	stopped                    chan struct{}
	entered                    chan context.Context
	physical                   chan error
	proceed, returned          chan struct{}
	stopOnce, goOnce, tailOnce sync.Once
	fault                      error
}

func newOwnedReadFileDrainBarrier() *ownedReadFileDrainBarrier {
	return &ownedReadFileDrainBarrier{stopped: make(chan struct{}), entered: make(chan context.Context, 1), physical: make(chan error, 1), proceed: make(chan struct{}), returned: make(chan struct{})}
}
func (b *ownedReadFileDrainBarrier) Stop() {
	b.manager.Stop()
	b.stopOnce.Do(func() { close(b.stopped) })
}
func (b *ownedReadFileDrainBarrier) Drain(ctx context.Context) error {
	b.entered <- ctx
	<-b.proceed
	err := b.manager.Drain(ctx)
	b.physical <- err
	<-b.returned
	if err != nil {
		return err
	}
	return b.fault // An explicit composition-return error, not a native failure.
}
func (b *ownedReadFileDrainBarrier) releasePhysical() { b.goOnce.Do(func() { close(b.proceed) }) }
func (b *ownedReadFileDrainBarrier) releaseReturn()   { b.tailOnce.Do(func() { close(b.returned) }) }
func (b *ownedReadFileDrainBarrier) release()         { b.releasePhysical(); b.releaseReturn() }

type ownedReadFileHeldReader struct {
	*sessionSocket
	started, entered, release  chan struct{}
	startOnce, enterOnce, once sync.Once
}

func (r *ownedReadFileHeldReader) read() ([]byte, error) {
	r.startOnce.Do(func() { close(r.started) })
	raw, err := r.sessionSocket.read()
	if err != nil {
		r.enterOnce.Do(func() { close(r.entered) })
		<-r.release
	}
	return raw, err
}
func (r *ownedReadFileHeldReader) unblock() { r.once.Do(func() { close(r.release) }) }

func TestOwnedReadFileOriginalReturnKeepsIdentity(t *testing.T) {
	for _, closeError := range []bool{false, true} {
		t.Run(map[bool]string{false: "joined", true: "cleanup-error"}[closeError], func(t *testing.T) {
			path, configuration := ownedReadFileIdentity(t)
			c, err := New(Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: p.ReadFileOperation})
			if err != nil {
				t.Fatal(err)
			}
			barrier := newOwnedReadFileDrainBarrier()
			if closeError {
				barrier.fault = workspace.ErrUnavailable
			}
			c.ownReadFile = func(cfg identity.Configuration) (operationExecutor, ownedReadFileManager, error) {
				executor, manager, err := newOwnedReadFile(cfg)
				if err != nil {
					return executor, manager, err
				}
				barrier.manager = manager
				return executor, barrier, nil
			}
			socket := &ownedReadFileHeldReader{sessionSocket: clientSocket(), started: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{})}
			ownedReadFileDevice(t, c, configuration, socket)
			run := startOwnedReadFileRun(t, c, func() { socket.unblock(); barrier.release() })
			if nextMessage(t, socket.sessionSocket).Type() != p.HelloType {
				t.Fatal("owned session did not enter the original wire")
			}
			// Hello is produced by the independent writer. Cancellation must
			// follow the actual reader entry for its return barrier to apply.
			await(t, socket.started)
			c.Stop()
			await(t, socket.entered)
			run.requireHeld(t, path)
			select {
			case <-barrier.stopped:
				t.Fatal("Manager stopped before the original session reader returned")
			default:
			}
			socket.unblock()
			await(t, barrier.stopped)
			cleanup := receiveOwnedReadFile(t, barrier.entered)
			if cleanup == nil || cleanup.Err() != nil || cleanup.Done() != nil {
				t.Fatal("cleanup borrowed a cancelled or budget-limited context")
			}
			run.requireHeld(t, path)
			barrier.releasePhysical()
			if err := receiveOwnedReadFile(t, barrier.physical); err != nil {
				t.Fatal("real Manager did not physically drain", err)
			}
			run.requireHeld(t, path)
			barrier.releaseReturn()
			err = run.join(t)
			if closeError && err != workspace.ErrUnavailable || !closeError && err != context.Canceled {
				t.Fatal("cleanup return was swallowed or changed into cancellation", err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}

func TestOwnedReadFilePartialOwnerAndCancelledHandoff(t *testing.T) {
	for _, mode := range []string{"partial-error", "cancelled-handoff"} {
		t.Run(mode, func(t *testing.T) {
			path, _ := ownedReadFileIdentity(t)
			c, err := New(Options{IdentityFile: path, RunnerVersion: "fixture-1", Operations: p.ReadFileOperation})
			if err != nil {
				t.Fatal(err)
			}
			barrier := newOwnedReadFileDrainBarrier()
			factoryEntered, factoryRelease := make(chan struct{}), make(chan struct{})
			var factoryOnce sync.Once
			releaseFactory := func() { factoryOnce.Do(func() { close(factoryRelease) }) }
			failure := errors.New("controlled owned construction failure")
			c.ownReadFile = func(cfg identity.Configuration) (operationExecutor, ownedReadFileManager, error) {
				executor, manager, err := newOwnedReadFile(cfg)
				if err != nil {
					return executor, manager, err
				}
				barrier.manager = manager
				close(factoryEntered)
				<-factoryRelease
				if mode == "partial-error" {
					return nil, barrier, failure
				}
				return executor, barrier, nil
			}
			var devices atomic.Int32
			c.device = func(identity.Configuration, *x509.CertPool) (deviceBackend, error) {
				devices.Add(1)
				return nil, ErrTransport
			}
			run := startOwnedReadFileRun(t, c, func() { releaseFactory(); barrier.release() })
			await(t, factoryEntered)
			requireOwnedReadFileIdentityLocked(t, path)
			if mode == "cancelled-handoff" {
				run.requireHeld(t, path)
			}
			releaseFactory()
			await(t, barrier.stopped)
			cleanup := receiveOwnedReadFile(t, barrier.entered)
			if cleanup.Err() != nil || cleanup.Done() != nil || devices.Load() != 0 {
				t.Fatal("partial/cancelled construction escaped cleanup or reached the device")
			}
			run.requireHeld(t, path)
			barrier.releasePhysical()
			if err := receiveOwnedReadFile(t, barrier.physical); err != nil {
				t.Fatal("partial owner's real Manager did not drain", err)
			}
			run.requireHeld(t, path)
			barrier.releaseReturn()
			err = run.join(t)
			if mode == "partial-error" && err != failure || mode == "cancelled-handoff" && err != context.Canceled {
				t.Fatal("original construction outcome was lost", err)
			}
			requireOwnedReadFileIdentityReleased(t, path)
		})
	}
}
