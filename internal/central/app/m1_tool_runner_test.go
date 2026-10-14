//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Output remains private and bounded. Diagnostics expose only fixed stages,
// original PID/exit and selected safe events, never credentials or paths.
type m1RunnerOutput struct {
	mu       sync.Mutex
	bytes    bytes.Buffer
	overflow bool
}

func (o *m1RunnerOutput) Write(raw []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.bytes.Len()+len(raw) > 1<<20 {
		o.overflow = true
	} else {
		_, _ = o.bytes.Write(raw)
	}
	return len(raw), nil
}

func (o *m1RunnerOutput) snapshot() ([]byte, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return bytes.Clone(o.bytes.Bytes()), o.overflow
}

func (o *m1RunnerOutput) event(name, field, value string) bool {
	raw, overflow := o.snapshot()
	defer clear(raw)
	if overflow {
		return false
	}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		var event map[string]any
		if json.Unmarshal(line, &event) == nil && event["event"] == name && event[field] == value {
			return name != "runner_connection" || event["connected"] == true && event["authenticated"] == true && event["ready"] == false
		}
	}
	return false
}

type m1RunnerProxy struct {
	server    *http.Server
	listener  net.Listener
	transport *http.Transport
	done      chan error
	active    atomic.Int64
	counts    [3]atomic.Int64
	once      sync.Once
	key       ed25519.PrivateKey
}

func (v *m1RunnerProxy) stop(t *testing.T) {
	t.Helper()
	v.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := v.server.Shutdown(ctx); err != nil {
			t.Error("M1 Runner TLS proxy shutdown exceeded its bound")
		}
		_ = v.server.Close()
		_ = v.listener.Close()
		select {
		case err := <-v.done:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("M1 Runner TLS original Serve failed")
			}
		case <-ctx.Done():
			t.Error("M1 Runner TLS original Serve return missing; retaining resources")
			<-v.done
		}
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for v.active.Load() != 0 {
			select {
			case <-tick.C:
			case <-ctx.Done():
				t.Error("M1 Runner TLS upgraded handler has not returned; retaining resources")
				for v.active.Load() != 0 {
					<-tick.C
				}
			}
		}
		v.transport.CloseIdleConnections()
		clear(v.key)
	})
}

// TLS terminates in this owned proxy, whose upstream is the running App's
// original router and DeviceHandler. No second device service is constructed.
func (w *m1Workflow) runnerTLS(t *testing.T, private string) (*m1RunnerProxy, string, string) {
	t.Helper()
	backend, err := url.Parse(w.app.address)
	workAppCheck(t, "m1-runner-upstream", err)
	publicOrigin, err := url.Parse(w.origin)
	workAppCheck(t, "m1-runner-public-origin", err)
	public, key, err := ed25519.GenerateKey(rand.Reader)
	workAppCheck(t, "m1-runner-tls-key", err)
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "owned M1 Runner fixture"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, public, key)
	workAppCheck(t, "m1-runner-tls-certificate", err)
	ca := filepath.Join(private, "runner-ca.pem")
	workAppCheck(t, "m1-runner-private-ca", os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600))
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	workAppCheck(t, "m1-runner-tls-listener", err)
	v := &m1RunnerProxy{listener: listener, transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, done: make(chan error, 1), key: key}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = v.transport
	director := proxy.Director
	proxy.Director = func(r *http.Request) { director(r); r.Host = publicOrigin.Host }
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ErrorHandler = func(out http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(out, "owned Runner proxy failed", http.StatusBadGateway)
	}
	v.server = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		index := -1
		switch r.URL.Path {
		case "/api/v1/runner/enroll":
			index = 0
		case "/api/v1/runner/challenge":
			index = 1
		case "/api/v1/runner/control":
			index = 2
		}
		if index < 0 {
			http.NotFound(out, r)
			return
		}
		v.counts[index].Add(1)
		v.active.Add(1)
		defer v.active.Add(-1)
		proxy.ServeHTTP(out, r)
	})}
	secure := tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	go func() { v.done <- v.server.Serve(secure) }()
	t.Cleanup(func() { v.stop(t) })
	return v, "https://" + listener.Addr().String(), ca
}

type m1RunnerProcess struct {
	cmd            *exec.Cmd
	stdout, stderr m1RunnerOutput
	done           chan struct{}
	err            error // Observed only after the original Wait closes done.
	proxy          *m1RunnerProxy
	identityPath   string
	configuration  identity.Configuration
	secrets        []string
}

func m1RunnerBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("AGENTEAM_RUNNER_BINARY")
	if binary == "" || !filepath.IsAbs(binary) || filepath.Clean(binary) != binary {
		t.Fatal("M1 requires AGENTEAM_RUNNER_BINARY as the absolute prebuilt original Runner")
	}
	info, err := os.Lstat(binary)
	real, realErr := filepath.EvalSymlinks(binary)
	if err != nil || realErr != nil || real != binary || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		t.Fatal("M1 Runner binary must be an existing executable without symlinks")
	}
	return binary
}

func (v *m1RunnerProcess) cleanup(t *testing.T) {
	t.Helper()
	select {
	case <-v.done:
		return
	default:
	}
	_ = v.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-v.done:
	case <-time.After(5 * time.Second):
		t.Error("M1 Runner did not join before cleanup; killing only its original PID")
		_ = v.cmd.Process.Kill()
		select {
		case <-v.done:
		case <-time.After(5 * time.Second):
			t.Error("M1 Runner original Wait missing after kill; retaining resources")
			<-v.done
		}
	}
	t.Logf("m1_runner_cleanup_original_wait pid=%d", v.cmd.Process.Pid)
}

func (w *m1Workflow) startRunner(t *testing.T) {
	t.Helper()
	if w.runner != nil || w.runnerToken == "" {
		t.Fatal("M1 Runner may only be enrolled once from original material")
	}
	binary := m1RunnerBinary(t)
	// The real identity owner rejects /tmp's world-writable ancestors. Only
	// this generated private directory is removed, after process and TLS join.
	private, err := os.MkdirTemp(".", ".m1-runner-")
	workAppCheck(t, "m1-runner-private-directory", err)
	private, err = filepath.Abs(private)
	workAppCheck(t, "m1-runner-private-absolute", err)
	workAppCheck(t, "m1-runner-private-mode", os.Chmod(private, 0700))
	t.Cleanup(func() {
		if err := os.RemoveAll(private); err != nil {
			t.Error("M1 Runner private directory cleanup failed")
		}
	})
	proxy, origin, ca := w.runnerTLS(t, private)
	v := &m1RunnerProcess{cmd: exec.Command(binary, "--enroll"), done: make(chan struct{}), proxy: proxy, identityPath: filepath.Join(private, "identity.json"), configuration: identity.Configuration{CentralURL: origin, RunnerID: p.ID(w.runnerID.String()), RootPath: w.root}}
	v.cmd.Dir = private
	v.cmd.Env = []string{"AGENTEAM_RUNNER_IDENTITY_FILE=" + v.identityPath, "AGENTEAM_RUNNER_CA_FILE=" + ca, "AGENTEAM_RUNNER_CENTRAL_URL=" + origin, "AGENTEAM_RUNNER_ID=" + w.runnerID.String(), "AGENTEAM_RUNNER_ROOT_PATH=" + w.root, "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT=3s", "AGENTEAM_RUNNER_OPERATIONS=read-file,write-file,run-command"}
	v.cmd.Stdout, v.cmd.Stderr = &v.stdout, &v.stderr
	read, write, err := os.Pipe()
	workAppCheck(t, "m1-runner-enrollment-pipe", err)
	defer read.Close()
	defer write.Close()
	v.cmd.Stdin = read
	workAppCheck(t, "m1-runner-original-start", v.cmd.Start())
	w.runner = v
	go func() { v.err = v.cmd.Wait(); close(v.done) }()
	t.Cleanup(func() { v.cleanup(t) })
	t.Logf("m1_runner_original_start pid=%d", v.cmd.Process.Pid)
	workAppCheck(t, "m1-runner-parent-read-close", read.Close())
	workAppCheck(t, "m1-runner-enrollment-write-bound", write.SetWriteDeadline(time.Now().Add(3*time.Second)))
	n, err := io.WriteString(write, w.runnerToken+"\n")
	if err != nil || n != len(w.runnerToken)+1 {
		t.Fatal("M1 original enrollment stdin write incomplete")
	}
	workAppCheck(t, "m1-runner-enrollment-eof", write.Close())
	v.secrets = []string{w.runnerToken, w.root, v.identityPath}
	ctx, cancel := context.WithTimeout(w.app.ctx, 12*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-v.done:
			t.Fatal("M1 original Runner exited before authenticated Hello")
		default:
		}
		if v.stderr.event("runner_connection", "state", "connected") {
			snapshot, err := w.app.assembly.runners.service.Get(ctx, w.app.actor, w.runnerID)
			workAppCheck(t, "m1-runner-real-online", err)
			if snapshot.Status == rc.Online && snapshot.LastHello != nil {
				if !slices.Equal(snapshot.LastHello.Capabilities, []string{p.ReadFileOperation, p.WriteFileOperation, p.RunCommandOperation}) || len(snapshot.LastHello.FeatureFlags) != 0 || snapshot.CredentialGeneration != 1 || snapshot.PublicKeyFingerprint == nil {
					t.Fatal("M1 original Runner Hello or credential facts differ")
				}
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("M1 original Runner did not become online within its bound")
		case <-tick.C:
		}
	}
	file, err := identity.Open(v.identityPath)
	if file != nil {
		_ = file.Close()
	}
	if !errors.Is(err, identity.ErrLocked) {
		t.Fatal("M1 live Runner does not own its original identity lock")
	}
	stored, err := identity.ReadOnly(v.identityPath)
	workAppCheck(t, "m1-runner-persisted-identity", err)
	if stored.State() != identity.Active || stored.Configuration() != v.configuration {
		t.Fatal("M1 original enrollment persisted different identity facts")
	}
	raw, err := os.ReadFile(v.identityPath)
	workAppCheck(t, "m1-runner-private-identity-inspection", err)
	defer clear(raw)
	var protected struct {
		Seed string `json:"private_seed"`
	}
	if json.Unmarshal(raw, &protected) != nil || protected.Seed == "" {
		t.Fatal("M1 original identity omitted private key material")
	}
	v.secrets = append(v.secrets, protected.Seed)
}

func (w *m1Workflow) stopRunner(t *testing.T) {
	t.Helper()
	v := w.runner
	if v == nil {
		t.Fatal("M1 original Runner owner absent")
	}
	workAppCheck(t, "m1-runner-sigterm", v.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-v.done:
	case <-time.After(5 * time.Second):
		t.Fatal("M1 original Runner Wait exceeded its shutdown bound")
	}
	status, ok := v.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Exited() || status.ExitStatus() != 0 || v.err != nil || !v.stderr.event("shutdown_complete", "outcome", "drained") {
		t.Fatal("M1 original Runner did not exit through normal drained shutdown")
	}
	t.Logf("m1_runner_original_wait pid=%d exit=%d", v.cmd.Process.Pid, status.ExitStatus())
	ctx, cancel := context.WithTimeout(w.app.ctx, 3*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		snapshot, err := w.app.assembly.runners.service.Get(ctx, w.app.actor, w.runnerID)
		workAppCheck(t, "m1-runner-real-offline", err)
		if snapshot.Status == rc.Offline && v.proxy.active.Load() == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("M1 original Runner WSS handler or online lease remains after Wait")
		case <-tick.C:
		}
	}
	for index := range v.proxy.counts {
		if v.proxy.counts[index].Load() != 1 {
			t.Fatal("M1 original Runner unexpectedly repeated enrollment or connection")
		}
	}
	file, err := identity.Open(v.identityPath)
	workAppCheck(t, "m1-runner-retired-identity-lock", err)
	workAppCheck(t, "m1-runner-retired-identity-close", file.Close())
	for _, output := range []*m1RunnerOutput{&v.stdout, &v.stderr} {
		raw, overflow := output.snapshot()
		if overflow {
			clear(raw)
			t.Fatal("M1 Runner private output exceeded its bound")
		}
		for _, secret := range v.secrets {
			if secret != "" && bytes.Contains(raw, []byte(secret)) {
				clear(raw)
				t.Fatal("M1 Runner output disclosed private enrollment or identity material")
			}
		}
		if output == &v.stdout && len(raw) != 0 {
			clear(raw)
			t.Fatal("M1 Runner emitted unexpected stdout")
		}
		clear(raw)
	}
	v.proxy.stop(t)
}
