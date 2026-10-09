//go:build integration && linux

package runnercontrol_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Only this top builds the unmodified command. Unlike a package TestMain, this
// does not add build work to any previously frozen Runner selector. All build
// time remains inside the existing Go/driver deadline.
func runnerCrashBinary(t *testing.T) string {
	t.Helper()
	tool := os.Getenv("AGENTEAM_GO")
	if tool == "" {
		tool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	root, err := os.Getwd()
	requireServiceOK(t, err, "crash command repository")
	// go test starts in the package, while the PG-only driver starts its
	// compiled test in the checkout root. Admit the actual module in either.
	for {
		module, readErr := os.ReadFile(filepath.Join(root, "go.mod"))
		if readErr == nil && strings.HasPrefix(string(module), "module github.com/LunaDeerTech/agenteam\n") {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("crash command repository module was not found")
		}
		root = parent
	}
	env := append(os.Environ(), "GOTOOLCHAIN=local", "GOENV=off", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTELEMETRY=off")
	version := exec.Command(tool, "env", "GOVERSION")
	version.Dir, version.Env = root, env
	actual, err := version.Output()
	if err != nil || strings.TrimSpace(string(actual)) != "go1.27.1" {
		t.Fatal("crash command requires actual Go 1.27.1")
	}
	binary := filepath.Join(t.TempDir(), "agenteam-runner")
	build := exec.Command(tool, "build", "-race", "-mod=readonly", "-p=1", "-o", binary, "./cmd/agenteam-runner")
	build.Dir, build.Env = root, env
	if output, err := build.CombinedOutput(); err != nil {
		clear(output)
		t.Fatal("original Runner command build did not actually succeed")
	}
	return binary
}

// Output remains private and bounded; none of it is interpolated into failures.
type runnerCrashOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	overflow bool
}

func (v *runnerCrashOutput) Write(raw []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data.Len()+len(raw) > 1<<20 {
		v.overflow = true
	} else {
		_, _ = v.data.Write(raw)
	}
	return len(raw), nil
}

type runnerCrashProcess struct {
	command        *exec.Cmd
	stdout, stderr runnerCrashOutput
	done           chan struct{}
	err            error // Only read after the original Wait has closed done.
}

func launchRunnerCrash(t *testing.T, binary string, env []string, token *string) *runnerCrashProcess {
	t.Helper()
	args := []string{}
	if token != nil {
		args = append(args, "--enroll")
	}
	v := &runnerCrashProcess{command: exec.Command(binary, args...), done: make(chan struct{})}
	v.command.Dir, v.command.Env = t.TempDir(), env
	v.command.Stdout, v.command.Stderr = &v.stdout, &v.stderr
	var read, write *os.File
	if token != nil {
		var err error
		read, write, err = os.Pipe()
		requireServiceOK(t, err, "crash command original stdin pipe")
		defer read.Close()
		defer write.Close()
		v.command.Stdin = read
	}
	requireServiceOK(t, v.command.Start(), "original crash command Start")
	go func() { v.err = v.command.Wait(); close(v.done) }()
	t.Cleanup(func() {
		select {
		case <-v.done:
			return
		default:
			_ = v.command.Process.Kill()
			select {
			case <-v.done:
			case <-time.After(5 * time.Second):
				t.Error("owned crash command lacks actual Wait after cleanup kill")
			}
		}
	})
	if token != nil {
		requireServiceOK(t, read.Close(), "parent enrollment read end Close")
		requireServiceOK(t, write.SetWriteDeadline(time.Now().Add(3*time.Second)), "parent enrollment write bound")
		n, err := io.WriteString(write, *token+"\n")
		if err != nil || n != len(*token)+1 {
			t.Fatal("original crash enrollment pipe write failed")
		}
		requireServiceOK(t, write.Close(), "parent enrollment write EOF")
	}
	return v
}

func (v *runnerCrashProcess) wait(t *testing.T, killed bool, code int) {
	t.Helper()
	select {
	case <-v.done:
	case <-time.After(5 * time.Second):
		t.Fatal("original crash command Wait has not returned")
	}
	state, ok := v.command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || killed && (!state.Signaled() || state.Signal() != syscall.SIGKILL) || !killed && (!state.Exited() || state.ExitStatus() != code) {
		t.Fatal("original Runner process exit does not match the requested crash/restart outcome")
	}
	if (killed || code != 0) && v.err == nil || !killed && code == 0 && v.err != nil {
		t.Fatal("original Wait error disagrees with its actual exit status")
	}
	if v.stdout.data.Len() != 0 || v.stdout.overflow || v.stderr.overflow {
		t.Fatal("crash command emitted stdout or exceeded bounded private output")
	}
}

func (v *runnerCrashProcess) noLeaks(t *testing.T, secrets ...string) {
	t.Helper()
	select {
	case <-v.done:
	default:
		t.Fatal("private output inspected before actual process Wait")
	}
	for _, secret := range secrets {
		if secret != "" && (bytes.Contains(v.stdout.data.Bytes(), []byte(secret)) || bytes.Contains(v.stderr.data.Bytes(), []byte(secret))) {
			t.Fatal("original crash command disclosed private enrollment/identity material")
		}
	}
}

type runnerCrashCheckpoint struct {
	ctx       context.Context
	valid     bool
	public    [32]byte
	fileHash  [32]byte
	committed bool
}

type runnerCrashTransport struct {
	server     *httptest.Server
	checkpoint chan runnerCrashCheckpoint
	abort      chan struct{}
	handlers   atomic.Int64
	mu         sync.Mutex
	enrolls    int
	forwarded  int
	challenges int
	controls   int
	failed     bool
}

func runnerCrashIdentity(path string) (identity.Identity, [32]byte, [32]byte, bool) {
	stored, err := identity.ReadOnly(path)
	public, keyErr := stored.PublicKey()
	raw, readErr := os.ReadFile(path)
	defer clear(raw)
	return stored, public, sha256.Sum256(raw), err == nil && keyErr == nil && readErr == nil
}

var errRunnerCrashHeld = errors.New("owned crash response held")

// This transport has two finite holds, neither of which returns a fabricated
// positive result: before invoking enrollment, or after the real backend's
// complete known response but before any byte reaches the original child.
func newRunnerCrashTransport(t *testing.T, backend *runnerNativeServer, path string, committed bool) *runnerCrashTransport {
	t.Helper()
	v := &runnerCrashTransport{checkpoint: make(chan runnerCrashCheckpoint, 1), abort: make(chan struct{})}
	upstream, err := url.Parse(backend.origin)
	requireServiceOK(t, err, "crash original backend URL")
	transport := backend.transport.Clone()
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport, proxy.ErrorLog = transport, log.New(io.Discard, "", 0)
	hold := func(ctx context.Context, known bool) {
		stored, public, digest, valid := runnerCrashIdentity(path)
		v.checkpoint <- runnerCrashCheckpoint{ctx, valid && stored.State() == identity.Pending, public, digest, known}
		select {
		case <-ctx.Done():
		case <-v.abort:
		}
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.URL.Path != "/api/v1/runner/enroll" {
			return nil
		}
		stored, public, _, valid := runnerCrashIdentity(path)
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, p.MaxDeviceBodyBytes+1))
		closeErr := response.Body.Close()
		response.Body = http.NoBody
		defer clear(raw)
		known, decodeErr := p.DecodeEnrollmentResponse(raw)
		complete := committed && valid && stored.State() == identity.Pending && response.StatusCode == http.StatusOK && readErr == nil && closeErr == nil && len(raw) <= p.MaxDeviceBodyBytes && int64(len(raw)) == response.ContentLength && decodeErr == nil && known.RunnerID() == stored.Configuration().RunnerID && known.Version() == "2" && known.CredentialGeneration() == "1" && known.PublicKeyFingerprint() == p.PublicKeyFingerprint(public)
		hold(response.Request.Context(), complete)
		return errRunnerCrashHeld
	}
	proxy.ErrorHandler = func(_ http.ResponseWriter, _ *http.Request, err error) {
		if !errors.Is(err, errRunnerCrashHeld) {
			v.mu.Lock()
			v.failed = true
			v.mu.Unlock()
		}
	}
	v.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		v.handlers.Add(1)
		defer v.handlers.Add(-1)
		v.mu.Lock()
		first := false
		switch request.URL.Path {
		case "/api/v1/runner/enroll":
			v.enrolls++
			first = v.enrolls == 1 && request.Method == http.MethodPost && request.URL.RawQuery == ""
			v.failed = v.failed || !first
			if first && committed {
				v.forwarded++
			}
		case "/api/v1/runner/challenge":
			v.challenges++
		case "/api/v1/runner/control":
			v.controls++
		default:
			v.failed = true
		}
		v.mu.Unlock()
		if request.URL.Path == "/api/v1/runner/enroll" {
			if !first {
				http.Error(w, "unavailable", http.StatusBadGateway)
				return
			}
			if !committed {
				hold(request.Context(), false)
				return
			}
		}
		proxy.ServeHTTP(w, request)
	}))
	owner := &runnerNativeServer{conns: make(map[*runnerNativeConn]struct{})}
	tracker := owner.nativeConnections()
	v.server.Listener = runnerNativeListener{v.server.Listener, owner}
	v.server.Config.ConnState, v.server.Config.ErrorLog = tracker.observeState, log.New(io.Discard, "", 0)
	v.server.StartTLS()
	t.Cleanup(func() {
		close(v.abort)
		transport.CloseIdleConnections()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.server.Config.Shutdown(ctx); err != nil {
			t.Error("original crash transport HTTP shutdown did not join")
		}
		v.server.Close() // Includes the original httptest Serve owner's actual join.
		if err := tracker.wait(ctx); err != nil || v.handlers.Load() != 0 {
			t.Error("original crash transport physical connections/handlers did not join")
		}
	})
	return v
}

func (v *runnerCrashTransport) idle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for v.handlers.Load() != 0 {
		select {
		case <-ctx.Done():
			t.Fatal("original crash transport handler has not returned")
		case <-ticker.C:
		}
	}
}

func (v *runnerCrashTransport) counts(t *testing.T, forwarded, challenges, controls int) {
	t.Helper()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.failed || v.enrolls != 1 || v.forwarded != forwarded || v.challenges != challenges || v.controls != controls {
		t.Fatalf("crash transport request facts failed=%t enrolls=%d forwarded=%d challenges=%d controls=%d", v.failed, v.enrolls, v.forwarded, v.challenges, v.controls)
	}
}

func runnerCrashUnlocked(t *testing.T, path string) {
	t.Helper()
	file, err := identity.Open(path)
	requireServiceOK(t, err, "identity lock reacquisition after actual process exit")
	requireServiceOK(t, file.Close(), "post-crash identity lock observer Close")
}

func runnerCrashFacts(t *testing.T, v *runnerServiceFixture, target rc.RunnerID, committed, connected bool) {
	t.Helper()
	var commands, tokens, consumed, events, audits, challenges, used, connections int
	var version, generation, connectionGeneration int64
	var enrolled bool
	err := v.store.QueryRow(migrationContext(t), `SELECT version,credential_generation,connection_generation,device_public_key IS NOT NULL,
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid AND consumed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1::uuid AND consumed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1::uuid)
 FROM agenteam_runner.runners WHERE id=$1::uuid`, target.String()).Scan(&version, &generation, &connectionGeneration, &enrolled, &commands, &tokens, &consumed, &events, &audits, &challenges, &used, &connections)
	requireServiceOK(t, err, "crash original durable facts")
	wantEnroll, wantConnection := 0, 0
	if committed {
		wantEnroll = 1
	}
	if connected {
		wantConnection = 1
	}
	if version != int64(1+wantEnroll) || generation != 1 || connectionGeneration != int64(wantConnection) || enrolled != committed || commands != 1 || tokens != 1 || consumed != wantEnroll || events != 1+wantEnroll || audits != 1+wantEnroll || challenges != wantConnection || used != wantConnection || connections != wantConnection {
		t.Fatal("crash/restart duplicated, lost or invented durable identity/connection facts")
	}
}

// These are actual Linux process deaths with original cmd Wait and file-lock
// reacquisition, not the earlier in-process Client's response-loss recovery.
// Neither stimulus simulates DB COMMIT Unknown or a failed filesystem fsync.
func TestRunnerControlProcessCrashRecovery(t *testing.T) {
	binary := runnerCrashBinary(t)
	for _, committed := range []bool{false, true} {
		name := "pending persisted before backend admission"
		if committed {
			name = "backend committed before active file publication"
		}
		t.Run(name, func(t *testing.T) {
			v := newRunnerServiceFixture(t)
			backend := newRunnerNativeServer(t, v.runner)
			_, _, created := v.create(t, "original process crash recovery")
			target := created.Receipt.Runner.ID
			path := nativeIdentityPath(t)
			proxy := newRunnerCrashTransport(t, backend, path, committed)
			configuration := identity.Configuration{CentralURL: proxy.server.URL, RunnerID: p.ID(target.String()), RootPath: created.Receipt.Runner.RootPath}
			ca := filepath.Join(filepath.Dir(path), "owned-ca.pem")
			requireServiceOK(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.server.Certificate().Raw}), 0600), "crash command private trust file")
			base := []string{"AGENTEAM_RUNNER_IDENTITY_FILE=" + path, "AGENTEAM_RUNNER_CA_FILE=" + ca, "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT=3s"}
			enroll := append(append([]string{}, base...), "AGENTEAM_RUNNER_CENTRAL_URL="+configuration.CentralURL, "AGENTEAM_RUNNER_ID="+target.String(), "AGENTEAM_RUNNER_ROOT_PATH="+configuration.RootPath)
			token, err := created.Material.Token.Wire()
			requireServiceOK(t, err, "original crash enrollment token")
			first := launchRunnerCrash(t, binary, enroll, &token)
			var point runnerCrashCheckpoint
			select {
			case point = <-proxy.checkpoint:
			case <-first.done:
				t.Fatal("original Runner exited before its crash checkpoint")
			case <-time.After(5 * time.Second):
				t.Fatal("original Runner did not reach its bounded crash checkpoint")
			}
			if !point.valid || point.committed != committed || point.ctx.Err() != nil {
				t.Fatal("crash checkpoint lacks original pending file or complete backend fact")
			}
			requireNativeIdentityLocked(t, path)
			requireServiceOK(t, first.command.Process.Signal(syscall.SIGKILL), "owned original Runner SIGKILL")
			first.wait(t, true, -1)
			proxy.idle(t)
			runnerCrashUnlocked(t, path)
			stored, public, digest, valid := runnerCrashIdentity(path)
			if !valid || stored.Configuration() != configuration || stored.State() != identity.Pending || public != point.public || digest != point.fileHash {
				t.Fatal("actual process crash changed the original persisted pending identity")
			}
			raw, err := os.ReadFile(path)
			requireServiceOK(t, err, "original private crash leak sentinel")
			var private struct {
				Seed string `json:"private_seed"`
			}
			err = json.Unmarshal(raw, &private)
			clear(raw)
			if err != nil || private.Seed == "" {
				t.Fatal("original private crash leak sentinel invalid")
			}
			forwarded := 0
			if committed {
				forwarded = 1
			}
			proxy.counts(t, forwarded, 0, 0)
			runnerCrashFacts(t, v, target, committed, false)
			first.noLeaks(t, token, private.Seed, path)

			// A fresh OS process receives only the identity path and CA. No
			// token, device configuration, previous memory or client is reused.
			restarted := launchRunnerCrash(t, binary, base, nil)
			if !committed {
				restarted.wait(t, false, 1)
				proxy.idle(t)
				proxy.counts(t, 0, 1, 0)
				runnerCrashFacts(t, v, target, false, false)
				_, nextPublic, nextDigest, valid := runnerCrashIdentity(path)
				if !valid || nextPublic != public || nextDigest != digest {
					t.Fatal("unregistered no-token restart replaced or activated its pending key")
				}
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					actual, err := v.runner.Get(ctx, v.actor, target)
					requireServiceOK(t, err, "current crash recovery Reader")
					restored, recoveredPublic, _, valid := runnerCrashIdentity(path)
					if actual.Status == rc.Online && valid && restored.State() == identity.Active {
						if recoveredPublic != public || restored.Configuration() != configuration || actual.PublicKeyFingerprint == nil || string(*actual.PublicKeyFingerprint) != p.PublicKeyFingerprint(public) || actual.LastHello == nil || len(actual.LastHello.Capabilities) != 0 || len(actual.LastHello.FeatureFlags) != 0 {
							t.Fatal("original committed key did not recover through real challenge and hello")
						}
						break
					}
					select {
					case <-restarted.done:
						t.Fatal("actual restarted command exited before committed-key recovery")
					case <-ctx.Done():
						t.Fatal("actual restarted command did not recover within the original bound")
					case <-ticker.C:
					}
				}
				proxy.counts(t, 1, 1, 1)
				runnerCrashFacts(t, v, target, true, true)
				requireNativeIdentityLocked(t, path)
				requireServiceOK(t, restarted.command.Process.Signal(syscall.SIGTERM), "original restarted Runner shutdown")
				restarted.wait(t, false, 0)
				if !bytes.Contains(restarted.stderr.data.Bytes(), []byte(`"outcome":"drained"`)) {
					t.Fatal("actual recovered Runner did not report drained shutdown")
				}
				proxy.idle(t)
				backend.waitControls(t, 0)
				v.snapshot(t, target, rc.Offline)
				var remaining int
				err = v.store.QueryRow(migrationContext(t), `SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1::uuid`, target.String()).Scan(&remaining)
				requireServiceOK(t, err, "recovered original connection retirement")
				if remaining != 0 {
					t.Fatal("recovered process exited with its original DB connection still live")
				}
				proxy.counts(t, 1, 1, 1)
			}
			runnerCrashUnlocked(t, path)
			restarted.noLeaks(t, token, private.Seed, path)
		})
	}
}
