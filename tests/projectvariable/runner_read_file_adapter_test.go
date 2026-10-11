//go:build integration && linux

package projectvariable_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/control"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	"github.com/LunaDeerTech/agenteam/internal/runner/readfile"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// D15 section 7 permits a strict operation-authority substitute while its real
// ToolCall producer is unbound. This fixture supplies that one missing port;
// it does not create or claim a real running Execution/Tool authorization row.
// Account/Owner, current initialized Agent, Mount mapping, Runner enrollment,
// TLS/WSS, dispatch and the filesystem path below are real implementations.
type readFileAdapterAuthority struct {
	store  *hookStore
	intent mc.ReadFileIntent
}

type readFileAdapterWitnessKey struct{}
type readFileAdapterWitness struct {
	owner *readFileAdapterAuthority
	tx    f.Tx
	live  atomic.Bool
}

func (o *readFileAdapterAuthority) matches(in mc.ReadFileIntent) bool {
	want := o.intent
	return in.Validate() == nil && in.Actor.Equal(want.Actor) && in.ProjectID == want.ProjectID && in.AgentID == want.AgentID && in.ExecutionID == want.ExecutionID && in.OperationID == want.OperationID && in.MountID == want.MountID && in.Arguments == want.Arguments
}

func (o *readFileAdapterAuthority) ReadFileLocks(in mc.ReadFileIntent) ([]f.LockRequest, error) {
	if !o.matches(in) {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	trigger, err := f.ProjectScheduleLock(in.ProjectID.String())
	if err != nil {
		return nil, err
	}
	return []f.LockRequest{{Key: trigger, Mode: f.Exclusive}}, nil
}

func readFileAdapterConfigurationRequest(in mc.ReadFileIntent) ac.ExecutionConfigurationRequest {
	return ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
}

func (o *readFileAdapterAuthority) held(ctx context.Context, tx f.Tx) error {
	if _, err := o.store.InTx(tx); err != nil {
		return err // Exact original live Store ownership, not Tx.Valid alone.
	}
	extra, err := o.ReadFileLocks(o.intent)
	if err != nil {
		return err
	}
	operation, err := f.AggregateLock(f.OperationAggregate, o.intent.OperationID.String())
	if err != nil {
		return err
	}
	locks := append(readFileAdapterConfigurationRequest(o.intent).RequiredLocks(), extra...)
	locks = append(locks, f.LockRequest{Key: operation, Mode: f.Exclusive})
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return err
	}
	return o.store.RequireHeldLocks(ctx, tx, locks)
}

func (o *readFileAdapterAuthority) WithReadFileInTx(ctx context.Context, tx f.Tx, in mc.ReadFileIntent, fn func(context.Context) error) error {
	if ctx == nil || fn == nil || !o.matches(in) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if err := o.held(ctx, tx); err != nil {
		return err
	}
	witness := &readFileAdapterWitness{owner: o, tx: tx}
	witness.live.Store(true)
	defer witness.live.Store(false)
	return fn(context.WithValue(ctx, readFileAdapterWitnessKey{}, witness))
}

func (o *readFileAdapterAuthority) RequireExecutionConfigurationInTx(ctx context.Context, tx f.Tx, in ac.ExecutionConfigurationRequest) error {
	if ctx == nil {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	witness, ok := ctx.Value(readFileAdapterWitnessKey{}).(*readFileAdapterWitness)
	want := readFileAdapterConfigurationRequest(o.intent)
	if !ok || witness.owner != o || witness.tx != tx || !witness.live.Load() || in.Validate() != nil || in.Stage != ac.ExecutionConfigurationCurrent || !in.Actor.Equal(want.Actor) || in.ProjectID != want.ProjectID || in.AgentID != want.AgentID || in.ExecutionID != want.ExecutionID {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return o.held(ctx, tx)
}

// The small server tracks original physical Close returns, including hijacked
// WSS connections. Neither HTTP Shutdown nor StateClosed is used as a substitute.
type readFileAdapterServer struct {
	server       *http.Server
	listener     net.Listener
	served       chan error
	handlers     sync.WaitGroup
	mu           sync.Mutex
	conns        map[*readFileAdapterConn]int
	closeFailure bool
	changed      chan struct{}
	origin       string
	roots        *x509.CertPool
}

type readFileAdapterListener struct {
	net.Listener
	owner *readFileAdapterServer
}

func (l readFileAdapterListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	owned := &readFileAdapterConn{Conn: conn, owner: l.owner}
	l.owner.mu.Lock()
	l.owner.conns[owned] = 0
	l.owner.mu.Unlock()
	return owned, nil
}

type readFileAdapterConn struct {
	net.Conn
	owner          *readFileAdapterServer
	closeSucceeded bool // Protected by owner.mu, including concurrent Close calls.
}

func (c *readFileAdapterConn) Close() error {
	c.owner.mu.Lock()
	c.owner.conns[c]++
	c.owner.mu.Unlock()
	err := c.Conn.Close()
	c.owner.mu.Lock()
	if err == nil {
		c.closeSucceeded = true
	} else if !errors.Is(err, net.ErrClosed) {
		c.owner.closeFailure = true
	}
	c.owner.conns[c]--
	if c.owner.conns[c] == 0 {
		// A duplicate ErrClosed is acceptable only after the original physical
		// close actually succeeded; concurrent calls must all return first.
		if !c.closeSucceeded {
			c.owner.closeFailure = true
		}
		delete(c.owner.conns, c)
	}
	close(c.owner.changed)
	c.owner.changed = make(chan struct{})
	c.owner.mu.Unlock()
	return err
}

func newReadFileAdapterServer(t *testing.T, handler http.Handler, service *runnerservice.Service) *readFileAdapterServer {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	readFileAdapterOK(t, err, "owned TLS key")
	t.Cleanup(func() { clear(private) })
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned read-file adapter fixture"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	readFileAdapterOK(t, err, "owned TLS certificate")
	cert, err := x509.ParseCertificate(der)
	readFileAdapterOK(t, err, "owned TLS certificate parse")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	readFileAdapterOK(t, err, "owned TLS listener")
	v := &readFileAdapterServer{listener: listener, served: make(chan error, 1), conns: make(map[*readFileAdapterConn]int), changed: make(chan struct{}), origin: "https://" + listener.Addr().String(), roots: x509.NewCertPool()}
	v.roots.AddCert(cert)
	v.server = &http.Server{ErrorLog: log.New(io.Discard, "", 0), Handler: httpapi.WithRequestID(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.handlers.Add(1)
		defer v.handlers.Done()
		handler.ServeHTTP(w, r)
	}))}
	secure := tls.NewListener(readFileAdapterListener{Listener: listener, owner: v}, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}})
	go func() { v.served <- v.server.Serve(secure) }()
	t.Cleanup(func() {
		// Client cleanup was registered later and joins first. This original
		// service owns all server WSS callbacks before HTTP/listener retirement.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		service.Stop()
		if err := service.Drain(ctx); err != nil {
			t.Error("read-file Runner service did not actually join")
		}
		if err := v.server.Shutdown(ctx); err != nil {
			t.Error("read-file HTTP shutdown did not join")
		}
		_ = v.server.Close()
		_ = v.listener.Close()
		select {
		case err := <-v.served:
			if !errors.Is(err, http.ErrServerClosed) {
				t.Error("read-file original Serve failed")
			}
		case <-ctx.Done():
			t.Error("read-file original Serve return missing")
		}
		joined := make(chan struct{})
		go func() { v.handlers.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-ctx.Done():
			t.Error("read-file original HTTP/WSS handlers did not join")
		}
		for {
			v.mu.Lock()
			empty, changed, closeFailure := len(v.conns) == 0, v.changed, v.closeFailure
			v.mu.Unlock()
			if empty {
				if closeFailure {
					t.Error("read-file physical connection Close reported an unexpected error")
				}
				if ctx.Err() != nil {
					t.Error("physical Close observation exceeded the original cleanup context")
				}
				return
			}
			select {
			case <-changed:
			case <-ctx.Done():
				t.Error("read-file physical connection Close return missing")
				return
			}
		}
	})
	return v
}

func readFileAdapterOK(t *testing.T, err error, step string) {
	t.Helper()
	if err != nil {
		t.Fatal(step + " failed") // Native errors must not disclose identity paths.
	}
}

func readFileAdapterPrivateDirectory(t *testing.T) string {
	t.Helper()
	// Production identity rejects world-writable /tmp ancestors. This exact
	// checkout-owned directory contains only this test's generated materials.
	directory, err := os.MkdirTemp(".", ".runner-read-file-")
	if err != nil {
		// Preserve the original failure while exposing only a closed errno class
		// and contemporaneous parent facts; never format the native error/path.
		classify := func(value error) string {
			if value == nil {
				return "none"
			}
			for _, known := range []struct {
				code syscall.Errno
				name string
			}{{syscall.EACCES, "access"}, {syscall.EPERM, "permission"}, {syscall.ENOSPC, "space"}, {syscall.EDQUOT, "quota"}, {syscall.ENOENT, "missing"}, {syscall.ENOTDIR, "not_directory"}, {syscall.EROFS, "read_only"}, {syscall.ELOOP, "symlink_loop"}, {syscall.ENAMETOOLONG, "name_length"}, {syscall.ESTALE, "stale"}, {syscall.EIO, "io"}, {syscall.EEXIST, "exists"}, {syscall.EMFILE, "process_files"}, {syscall.ENFILE, "system_files"}} {
				if errors.Is(value, known.code) {
					return known.name
				}
			}
			return "other"
		}
		parent, statErr := os.Stat(".")
		_, cwdErr := os.Getwd()
		var mode uint32
		var device uint64
		var directory, ownerKnown, owned, linked bool
		if statErr == nil {
			mode, directory = uint32(parent.Mode().Perm()), parent.IsDir()
			if native, ok := parent.Sys().(*syscall.Stat_t); ok {
				ownerKnown, owned, linked = true, native.Uid == uint32(os.Geteuid()), native.Nlink > 0
				device = uint64(native.Dev)
			}
		}
		t.Logf("owned_private_directory errno=%s parent_stat=%s parent_known=%t parent_directory=%t parent_mode=%04o parent_owner_known=%t parent_owned=%t parent_linked=%t cwd=%s", classify(err), classify(statErr), statErr == nil, directory, mode, ownerKnown, owned, linked, classify(cwdErr))
		var fs syscall.Statfs_t
		fsErr := syscall.Statfs(".", &fs)
		if fsErr != nil {
			fs = syscall.Statfs_t{}
		}
		t.Logf("owned_private_directory_fs statfs=%s statfs_known=%t type=%#x bsize=%d blocks=%d bfree=%d bavail=%d files=%d ffree=%d parent_device_known=%t parent_device=%d uid=%d euid=%d gid=%d egid=%d", classify(fsErr), fsErr == nil, fs.Type, fs.Bsize, fs.Blocks, fs.Bfree, fs.Bavail, fs.Files, fs.Ffree, ownerKnown, device, os.Getuid(), os.Geteuid(), os.Getgid(), os.Getegid())
	}
	readFileAdapterOK(t, err, "owned private directory")
	directory, err = filepath.Abs(directory)
	readFileAdapterOK(t, err, "owned absolute directory")
	readFileAdapterOK(t, os.Chmod(directory, 0700), "private directory mode")
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error("owned read-file directory removal failed")
		}
		if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
			t.Error("owned read-file directory remains")
		}
	})
	return directory
}

// Author integration: real owned PG/Account/Owner configuration plus native
// TLS/WSS/Runner/file I/O. The original P2 Object ProcessGuard remains held by
// newRunnerMountPGFixture until this entire test's later cleanup actually joins.
func TestRunnerReadFileAdapter(t *testing.T) {
	v := newRunnerMountPGFixture(t)
	base := v.agent.p2.base
	directory := readFileAdapterPrivateDirectory(t)
	rootPath := filepath.Join(directory, "root")
	readFileAdapterOK(t, os.Mkdir(rootPath, 0700), "owned device root")
	authority, err := runnerservice.NewAuthority(base.tracked, base.accounts)
	readFileAdapterOK(t, err, "real Runner authority")
	auditor, err := audit.New(base.tracked, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Runners: authority})
	readFileAdapterOK(t, err, "real Runner Audit")
	runners, err := runnerservice.New(authority, auditor)
	readFileAdapterOK(t, err, "real Runner service")
	t.Cleanup(func() {
		runners.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runners.Drain(ctx); err != nil {
			t.Error("Runner service fallback cleanup did not join")
		}
	})
	create, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "read-file-runner", Description: "Owned read-file adapter fixture", Tags: []string{}, RootPath: rootPath})
	readFileAdapterOK(t, err, "formal Runner creation intent")
	created, err := runners.Execute(ctxFor(t), base.adminBrowser.actor, "read-file-runner-create", create)
	material := created.Material
	defer func() {
		if material != nil {
			material.Token = p.EnrollmentToken{}
		}
	}()
	readFileAdapterOK(t, err, "formal Runner creation")
	if created.Receipt.Validate() != nil || created.Receipt.Runner.Status != rc.Offline || created.Material == nil || !created.Material.Token.Valid() || created.Material.ExpiresAt.Validate() != nil {
		t.Fatal("first known Runner creation omitted valid one-time material")
	}
	declarations := []ac.RunnerMountDeclaration{{RunnerID: created.Receipt.Runner.ID, Workspace: "source"}}
	createAgent, commandMeta, _ := v.createRequest(t, declarations)
	mutation := v.mutation(t, v.request(t, base.ownerBrowser, http.MethodPost, v.collection(), commandMeta.IdempotencyKey, jsonBytes(t, map[string]any{"request": createAgent})), 1, true, declarations)
	binding := (*mutation.Fields().RunnerMounts)[0]
	projectID, agentID := v.agent.p2.project.ID, mutation.Fields().Agent.Fields().Core.ID
	device, err := runnerhttp.NewDeviceHandler(runners)
	readFileAdapterOK(t, err, "real device HTTP handler")
	server := newReadFileAdapterServer(t, device, runners)
	root, err := os.Open(rootPath)
	readFileAdapterOK(t, err, "trusted root descriptor")
	manager, managerErr := workspace.New(root)
	closeErr := root.Close()
	readFileAdapterOK(t, managerErr, "real Workspace manager")
	readFileAdapterOK(t, closeErr, "borrowed root descriptor close")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Drain(ctx); err != nil {
			t.Error("real Workspace manager did not actually join")
		}
	})
	handle, err := manager.Ensure(ctxFor(t), workspace.Identity{ProjectID: p.ID(projectID.String()), AgentID: p.ID(agentID.String()), Name: binding.Workspace})
	readFileAdapterOK(t, err, "real Workspace Ensure")
	readFileAdapterOK(t, handle.Drain(context.Background()), "initial Workspace Handle close")
	content := []byte("prefix:真实 read-file bytes\n")
	filePath := filepath.Join(rootPath, projectID.String(), agentID.String(), binding.Workspace, "notes.txt")
	readFileAdapterOK(t, os.WriteFile(filePath, content, 0600), "owned read-file content")
	handler, err := readfile.New(manager)
	readFileAdapterOK(t, err, "real read-file handler")
	configuration := identity.Configuration{CentralURL: server.origin, RunnerID: p.ID(created.Receipt.Runner.ID.String()), RootPath: rootPath}
	states := make(chan control.ConnectionState, 16)
	client, err := control.NewReadFile(control.Options{IdentityFile: filepath.Join(directory, "identity.json"), Configuration: &configuration, EnrollmentToken: created.Material.Token, RunnerVersion: "read-file-fixture", Roots: server.roots, Observe: func(ctx context.Context, state control.ConnectionState) {
		select {
		case states <- state:
		case <-ctx.Done():
		}
	}}, handler)
	readFileAdapterOK(t, err, "real explicitly bound Runner client")
	created.Material = nil // Drop the fixture's copy after explicit client handoff.
	runDone := make(chan struct{})
	var runErr error
	go func() { runErr = client.Run(context.Background()); close(runDone) }()
	t.Cleanup(func() {
		client.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Drain(ctx); err != nil {
			t.Error("real read-file client Drain did not join")
		}
		select {
		case <-runDone:
			if !errors.Is(runErr, context.Canceled) {
				t.Error("original read-file Client.Run cancellation outcome differs")
			}
		case <-ctx.Done():
			t.Error("original read-file Client.Run return missing")
		}
	})
	connected, cancelConnect := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelConnect()
connect:
	for {
		select {
		case state := <-states:
			if state == control.Connected {
				break connect
			}
		case <-runDone:
			t.Fatal("real Runner returned before authenticated hello")
		case <-connected.Done():
			t.Fatal("real Runner did not complete authenticated hello")
		}
	}
	executionID := id[i.Execution](t)
	actor, err := i.NewAgentRun(projectID, agentID, executionID)
	readFileAdapterOK(t, err, "fixture AgentRun identity")
	intent := mc.ReadFileIntent{Actor: actor, ProjectID: projectID, AgentID: agentID, ExecutionID: executionID, OperationID: id[tc.Operation](t), MountID: binding.MountID, Arguments: p.ReadFileArguments{Path: "notes.txt", Offset: 7, Length: 64, Encoding: "utf8"}}
	owner := &readFileAdapterAuthority{store: base.tracked, intent: intent}
	current, err := agent.NewExecutionConfiguration(v.agent.providers.Agents, owner)
	readFileAdapterOK(t, err, "real current Agent configuration adapter")
	resolver, err := mount.NewReadFileResolver(base.tracked, current, owner)
	readFileAdapterOK(t, err, "real current Mount resolver")
	dispatcher, err := runnerhttp.NewReadFileDispatcher(device, resolver)
	readFileAdapterOK(t, err, "real ticket-only dispatcher")
	var previousRequest p.ID
	check := func(result runnerhttp.ReadFileDispatchResult, err error) {
		t.Helper()
		readFileAdapterOK(t, err, "real WSS read-file dispatch")
		if !result.Attempted || result.Terminal == nil || result.Terminal.Outcome != p.Success || result.Terminal.OperationID != p.ID(intent.OperationID.String()) || !result.Terminal.RequestID.Valid() || result.Terminal.RequestID == previousRequest {
			t.Fatal("read-file dispatch did not produce its real correlated terminal")
		}
		previousRequest = result.Terminal.RequestID
		value, err := p.DecodeReadFileResult(result.Terminal.Payload)
		if err != nil || value.Path != intent.Arguments.Path || value.Offset != 7 || value.BytesRead != len(content)-7 || !value.EOF || value.Encoding != "utf8" || value.Data != string(content[7:]) {
			t.Fatal("real Runner result differs from the owned file bytes/range")
		}
	}
	first, err := resolver.Resolve(ctxFor(t), intent)
	readFileAdapterOK(t, err, "initial verified ticket")
	check(dispatcher.Dispatch(ctxFor(t), first))
	stale, err := resolver.Resolve(ctxFor(t), intent)
	readFileAdapterOK(t, err, "pre-clear unclaimed ticket")
	v.update(t, agentID, "read-file-formal-clear", 1, []ac.RunnerMountDeclaration{}, 2, true)
	rejected, err := dispatcher.Dispatch(ctxFor(t), stale)
	requireCode(t, err, f.Forbidden)
	if rejected.Attempted || rejected.Terminal != nil {
		t.Fatal("clear allowed the old unclaimed ticket to reach the wire")
	}
	if _, err := resolver.Resolve(ctxFor(t), intent); err == nil {
		t.Fatal("current cleared Mount yielded a new ticket")
	}
	readded, _ := v.update(t, agentID, "read-file-formal-readd", 2, declarations, 3, true)
	bindings := readded.Fields().RunnerMounts
	if bindings == nil || len(*bindings) != 1 || (*bindings)[0] != binding {
		t.Fatal("formal re-add changed the original stable Mount/Workspace mapping")
	}
	restored, err := resolver.Resolve(ctxFor(t), intent)
	readFileAdapterOK(t, err, "current re-added Mount ticket")
	check(dispatcher.Dispatch(ctxFor(t), restored))
}
