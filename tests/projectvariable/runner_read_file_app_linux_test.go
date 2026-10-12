//go:build integration && linux

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	runnerservice "github.com/LunaDeerTech/agenteam/internal/central/runner/service"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// The method's compile stage supplies this separately built original command,
// its source closure and artifact identity. Native acceptance never builds an
// unrecorded second program and missing input is a failure, not a skipped test.
func runnerReadFileAppBinary(t *testing.T) string {
	t.Helper()
	path := os.Getenv("AGENTEAM_RUNNER_BINARY")
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		t.Fatal("AGENTEAM_RUNNER_BINARY requires the absolute prebuilt original Runner command")
	}
	info, err := os.Lstat(path)
	real, realErr := filepath.EvalSymlinks(path)
	if err != nil || realErr != nil || real != path || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		t.Fatal("prebuilt Runner command must be a regular executable without symlink components")
	}
	return path
}

type runnerReadFileAppOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	overflow bool
}

func (v *runnerReadFileAppOutput) Write(raw []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data.Len()+len(raw) > 1<<20 {
		v.overflow = true
	} else {
		_, _ = v.data.Write(raw)
	}
	return len(raw), nil
}

func (v *runnerReadFileAppOutput) snapshot() ([]byte, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return bytes.Clone(v.data.Bytes()), v.overflow
}

func (v *runnerReadFileAppOutput) event(name, field, value string) bool {
	raw, overflow := v.snapshot()
	defer clear(raw)
	if overflow {
		return false
	}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		var event map[string]any
		if json.Unmarshal(line, &event) == nil && event["event"] == name && event[field] == value {
			if name == "runner_connection" && (event["connected"] != true || event["authenticated"] != true || event["ready"] != false) {
				return false
			}
			return true
		}
	}
	return false
}

type runnerReadFileAppProcess struct {
	command        *exec.Cmd
	stdout, stderr runnerReadFileAppOutput
	done           chan struct{}
	err            error // Read only after the original command.Wait returns.
}

// The original command reads a native stdin pipe. No token is placed in argv,
// environment, logs or a synthetic enrollment/identity implementation.
func runnerReadFileAppLaunch(t *testing.T, binary string, env, args []string, token *string) *runnerReadFileAppProcess {
	t.Helper()
	v := &runnerReadFileAppProcess{command: exec.Command(binary, args...), done: make(chan struct{})}
	v.command.Dir, v.command.Env = t.TempDir(), slices.Clone(env)
	v.command.Stdout, v.command.Stderr = &v.stdout, &v.stderr
	var read, write *os.File
	if token != nil {
		var err error
		read, write, err = os.Pipe()
		readFileAdapterOK(t, err, "original enrollment pipe")
		defer read.Close()
		defer write.Close()
		v.command.Stdin = read
	}
	readFileAdapterOK(t, v.command.Start(), "original Runner command Start")
	t.Logf("runner_cli_start pid=%d enrollment=%t", v.command.Process.Pid, token != nil)
	go func() { v.err = v.command.Wait(); close(v.done) }()
	t.Cleanup(func() {
		select {
		case <-v.done:
			return
		default:
		}
		t.Error("Runner command did not return before failure cleanup; killing only its owned PID")
		_ = v.command.Process.Kill()
		select {
		case <-v.done:
		case <-time.After(5 * time.Second):
			t.Error("owned Runner original Wait missing after kill; retain fixture resources until actual return or global test deadline")
			<-v.done
		}
	})
	if token != nil {
		readFileAdapterOK(t, read.Close(), "parent enrollment read end Close")
		readFileAdapterOK(t, write.SetWriteDeadline(time.Now().Add(3*time.Second)), "enrollment pipe bound")
		n, err := io.WriteString(write, *token+"\n")
		if err != nil || n != len(*token)+1 {
			t.Fatal("original enrollment stdin write incomplete")
		}
		readFileAdapterOK(t, write.Close(), "original enrollment EOF")
	}
	return v
}

func (v *runnerReadFileAppProcess) wait(t *testing.T, code int, check bool) {
	t.Helper()
	select {
	case <-v.done:
	case <-time.After(5 * time.Second):
		t.Fatal("original Runner command Wait did not return within its bound")
	}
	state, ok := v.command.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !state.Exited() || state.ExitStatus() != code || (v.err == nil) != (code == 0) {
		t.Fatal("original Runner command Wait and expected exit disagree")
	}
	t.Logf("runner_cli_original_wait pid=%d exit=%d", v.command.Process.Pid, state.ExitStatus())
	out, outOverflow := v.stdout.snapshot()
	errOutput, errOverflow := v.stderr.snapshot()
	defer clear(out)
	defer clear(errOutput)
	if outOverflow || errOverflow || !check && len(out) != 0 {
		t.Fatal("Runner output exceeded its private bound or emitted unexpected stdout")
	}
	if check {
		var result struct {
			Scope         string `json:"scope"`
			Valid         bool   `json:"valid"`
			Ready         bool   `json:"ready"`
			Connected     bool   `json:"connected"`
			Authenticated bool   `json:"authenticated"`
		}
		if json.Unmarshal(out, &result) != nil || result.Scope != "d15" || !result.Valid || result.Ready || result.Connected || result.Authenticated {
			t.Fatal("offline command reported an invalid or runtime-ready configuration result")
		}
	}
}

func (v *runnerReadFileAppProcess) connected(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if v.stderr.event("runner_connection", "state", "connected") {
			return
		}
		select {
		case <-v.done:
			t.Fatal("original Runner exited before its authenticated Hello acknowledgment")
		case <-ctx.Done():
			t.Fatal("original Runner did not connect within the existing native bound")
		case <-tick.C:
		}
	}
}

func (v *runnerReadFileAppProcess) noLeaks(t *testing.T, secrets ...string) {
	t.Helper()
	select {
	case <-v.done:
	default:
		t.Fatal("private command output inspected before original Wait")
	}
	for _, output := range []*runnerReadFileAppOutput{&v.stdout, &v.stderr} {
		raw, _ := output.snapshot()
		for _, secret := range secrets {
			if secret != "" && bytes.Contains(raw, []byte(secret)) {
				clear(raw)
				t.Fatal("Runner command disclosed private identity or enrollment material")
			}
		}
		clear(raw)
	}
}

type runnerReadFileAppFixture struct {
	setup         *runnerMountPGFixture
	runners       *runnerservice.Service
	device        *runnerhttp.DeviceHandler
	server        *readFileAdapterServer
	binary        string
	configuration identity.Configuration
	path, ca      string
	token, seed   string
	intent        mc.ReadFileIntent
	content       []byte
	counts        [3]atomic.Int64 // Original enroll/challenge/control invocations.
	active        atomic.Int64    // Actual handler returns, including WSS hijacks.
	controls      atomic.Int64
	connection    string
	requests      map[p.ID]bool
}

func newRunnerReadFileAppFixture(t *testing.T, binary string) *runnerReadFileAppFixture {
	t.Helper()
	v := &runnerReadFileAppFixture{setup: newRunnerMountPGFixture(t), binary: binary, requests: make(map[p.ID]bool)}
	base := v.setup.agent.p2.base
	directory := readFileAdapterPrivateDirectory(t)
	root := filepath.Join(directory, "root")
	readFileAdapterOK(t, os.Mkdir(root, 0700), "owned deployment root")
	v.path, v.ca = filepath.Join(directory, "identity.json"), filepath.Join(directory, "ca.pem")
	authority, err := runnerservice.NewAuthority(base.tracked, base.accounts)
	readFileAdapterOK(t, err, "real Runner authority")
	auditor, err := audit.New(base.tracked, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Runners: authority})
	readFileAdapterOK(t, err, "real Runner Audit")
	v.runners, err = runnerservice.New(authority, auditor)
	readFileAdapterOK(t, err, "real Runner service")
	t.Cleanup(func() {
		v.runners.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if v.runners.Drain(ctx) != nil {
			t.Error("Runner service fallback cleanup did not actually join")
		}
	})
	create, err := rc.NewCreate(rc.CreateRequest{RunnerID: id[rc.Runner](t), Name: "read-file-app-runner", Description: "Original command entry", Tags: []string{}, RootPath: root})
	readFileAdapterOK(t, err, "formal Runner creation intent")
	created, err := v.runners.Execute(ctxFor(t), base.adminBrowser.actor, "read-file-app-create", create)
	readFileAdapterOK(t, err, "formal system Runner creation")
	if created.Receipt.Validate() != nil || created.Material == nil || !created.Material.Token.Valid() {
		t.Fatal("formal Runner creation omitted its original enrollment material")
	}
	v.token, err = created.Material.Token.Wire()
	created.Material.Token = p.EnrollmentToken{}
	readFileAdapterOK(t, err, "original enrollment token wire")
	declarations := []ac.RunnerMountDeclaration{{RunnerID: created.Receipt.Runner.ID, Workspace: "source"}}
	request, meta, _ := v.setup.createRequest(t, declarations)
	mutation := v.setup.mutation(t, v.setup.request(t, base.ownerBrowser, http.MethodPost, v.setup.collection(), meta.IdempotencyKey, jsonBytes(t, map[string]any{"request": request})), 1, true, declarations)
	binding := (*mutation.Fields().RunnerMounts)[0]
	project, agentID := v.setup.agent.p2.project.ID, mutation.Fields().Agent.Fields().Core.ID
	execution := id[i.Execution](t)
	actor, err := i.NewAgentRun(project, agentID, execution)
	readFileAdapterOK(t, err, "D15 fixture operation actor")
	v.intent = mc.ReadFileIntent{Actor: actor, ProjectID: project, AgentID: agentID, ExecutionID: execution, OperationID: id[tc.Operation](t), MountID: binding.MountID, Arguments: p.ReadFileArguments{Path: "notes.txt", Offset: 7, Length: 64, Encoding: "utf8"}}
	// These are private deployment data, not a second Runner Manager/Handler.
	workspace := filepath.Join(root, project.String(), agentID.String(), binding.Workspace)
	readFileAdapterOK(t, os.MkdirAll(workspace, 0700), "owned deployment data directories")
	v.content = []byte("prefix:真实 read-file bytes\n")
	readFileAdapterOK(t, os.WriteFile(filepath.Join(workspace, "notes.txt"), v.content, 0600), "owned range-read bytes")
	v.device, err = runnerhttp.NewDeviceHandler(v.runners)
	readFileAdapterOK(t, err, "original device HTTP handler")
	v.server = newReadFileAdapterServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.active.Add(1)
		defer v.active.Add(-1)
		switch r.URL.Path {
		case "/api/v1/runner/enroll":
			v.counts[0].Add(1)
		case "/api/v1/runner/challenge":
			v.counts[1].Add(1)
		case "/api/v1/runner/control":
			v.counts[2].Add(1)
			v.controls.Add(1)
			defer v.controls.Add(-1)
		}
		v.device.ServeHTTP(w, r)
	}), v.runners)
	v.configuration = identity.Configuration{CentralURL: v.server.origin, RunnerID: p.ID(created.Receipt.Runner.ID.String()), RootPath: root}
	// The old server deliberately exposes its trust pool, not a certificate
	// getter. Export only its original CA from a fully verified native handshake.
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp4", v.server.listener.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: v.server.roots})
	readFileAdapterOK(t, err, "original fixture CA verified handshake")
	chains := conn.ConnectionState().VerifiedChains
	closeErr := conn.Close()
	readFileAdapterOK(t, closeErr, "CA observation original TLS Close")
	if len(chains) != 1 || len(chains[0]) != 1 || !chains[0][0].IsCA || chains[0][0].CheckSignatureFrom(chains[0][0]) != nil {
		t.Fatal("fixture trust export did not observe its original verified self-signed CA")
	}
	readFileAdapterOK(t, os.WriteFile(v.ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: chains[0][0].Raw}), 0600), "private original CA export")
	v.idle(t)
	return v
}

func (v *runnerReadFileAppFixture) env(operation *string, explicit bool) []string {
	out := []string{"AGENTEAM_RUNNER_IDENTITY_FILE=" + v.path, "AGENTEAM_RUNNER_CA_FILE=" + v.ca, "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT=3s"}
	if operation != nil {
		out = append(out, "AGENTEAM_RUNNER_OPERATIONS="+*operation)
	}
	if explicit {
		out = append(out, "AGENTEAM_RUNNER_CENTRAL_URL="+v.configuration.CentralURL, "AGENTEAM_RUNNER_ID="+string(v.configuration.RunnerID), "AGENTEAM_RUNNER_ROOT_PATH="+v.configuration.RootPath)
	}
	return out
}

func (v *runnerReadFileAppFixture) identity(t *testing.T, locked bool) [32]byte {
	t.Helper()
	file, err := identity.Open(v.path)
	if file != nil {
		readFileAdapterOK(t, file.Close(), "identity observation owner Close")
	}
	if locked && !errors.Is(err, identity.ErrLocked) || !locked && err != nil {
		t.Fatal("identity lock differs from original command ownership")
	}
	stored, err := identity.ReadOnly(v.path)
	readFileAdapterOK(t, err, "original persisted identity")
	if stored.State() != identity.Active || stored.Configuration() != v.configuration {
		t.Fatal("command did not retain its original active configuration")
	}
	info, err := os.Stat(v.path)
	readFileAdapterOK(t, err, "original identity metadata")
	if info.Mode().Perm() != 0600 {
		t.Fatal("original identity permissions differ")
	}
	raw, err := os.ReadFile(v.path)
	readFileAdapterOK(t, err, "private identity comparison")
	defer clear(raw)
	var private struct {
		Seed string `json:"private_seed"`
	}
	if json.Unmarshal(raw, &private) != nil || private.Seed == "" || v.seed != "" && v.seed != private.Seed {
		t.Fatal("normal command restart changed private identity material")
	}
	v.seed = private.Seed
	return sha256.Sum256(raw)
}

func (v *runnerReadFileAppFixture) idle(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		v.server.mu.Lock()
		empty, failed := len(v.server.conns) == 0, v.server.closeFailure
		v.server.mu.Unlock()
		if empty && v.active.Load() == 0 {
			if failed {
				t.Fatal("original physical connection Close failed")
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("original device handlers or physical connections did not return")
		case <-tick.C:
		}
	}
}

func (v *runnerReadFileAppFixture) facts(t *testing.T) [7]int64 {
	t.Helper()
	var out [7]int64
	err := v.setup.agent.p2.base.raw.QueryRow(ctxFor(t), `SELECT connection_generation,
 (SELECT count(*) FROM agenteam_runner.connections WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1 AND consumed_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1),
 (SELECT count(*) FROM agenteam_runner.challenges WHERE runner_id=$1)
 FROM agenteam_runner.runners WHERE id=$1`, string(v.configuration.RunnerID)).Scan(&out[0], &out[1], &out[2], &out[3], &out[4], &out[5], &out[6])
	readFileAdapterOK(t, err, "independent original Runner facts")
	return out
}

func (v *runnerReadFileAppFixture) countsNow() [3]int64 {
	return [3]int64{v.counts[0].Load(), v.counts[1].Load(), v.counts[2].Load()}
}

func (v *runnerReadFileAppFixture) online(t *testing.T, process *runnerReadFileAppProcess, generation int64, enabled bool) {
	t.Helper()
	process.connected(t)
	v.identity(t, true)
	target, err := f.ParseID[rc.Runner](string(v.configuration.RunnerID))
	readFileAdapterOK(t, err, "original Runner reference")
	snapshot, err := v.runners.Get(ctxFor(t), v.setup.agent.p2.base.adminBrowser.actor, target)
	readFileAdapterOK(t, err, "real Runner current Hello")
	want := []string{}
	if enabled {
		want = []string{p.ReadFileOperation}
	}
	stored, err := identity.ReadOnly(v.path)
	readFileAdapterOK(t, err, "original public identity")
	public, err := stored.PublicKey()
	readFileAdapterOK(t, err, "original public key")
	if snapshot.Status != rc.Online || snapshot.Version != 2 || snapshot.CredentialGeneration != 1 || snapshot.PublicKeyFingerprint == nil || string(*snapshot.PublicKeyFingerprint) != p.PublicKeyFingerprint(public) || snapshot.LastHello == nil || !slices.Equal(snapshot.LastHello.Capabilities, want) || len(snapshot.LastHello.FeatureFlags) != 0 || v.controls.Load() != 1 {
		t.Fatal("original command identity, capability or active WSS owner differs")
	}
	var current string
	var actual int64
	err = v.setup.agent.p2.base.raw.QueryRow(ctxFor(t), `SELECT id::text,generation FROM agenteam_runner.connections WHERE runner_id=$1 AND hello_at IS NOT NULL AND lease_expires_at>clock_timestamp()`, string(v.configuration.RunnerID)).Scan(&current, &actual)
	readFileAdapterOK(t, err, "actual current WSS session identity")
	if actual != generation || current == "" || current == v.connection {
		t.Fatal("restart reused a prior WSS session or changed its expected generation")
	}
	v.connection = current
	facts := v.facts(t)
	if facts[0] != generation || facts[1] != 1 || facts[2] != 1 || facts[3] != 1 || facts[4] != 2 || facts[5] != 2 || v.countsNow() != [3]int64{1, generation, generation} {
		t.Fatal("original enrollment/restart duplicated identity facts or device requests")
	}
}

func (v *runnerReadFileAppFixture) stop(t *testing.T, process *runnerReadFileAppProcess) {
	t.Helper()
	readFileAdapterOK(t, process.command.Process.Signal(syscall.SIGTERM), "original Runner SIGTERM")
	process.wait(t, 0, false)
	if !process.stderr.event("shutdown_complete", "outcome", "drained") {
		t.Fatal("original command did not report its actual normal shutdown")
	}
	v.idle(t)
	if v.facts(t)[1] != 0 {
		t.Fatal("original WSS connection remains after process Wait and handler return")
	}
	target, err := f.ParseID[rc.Runner](string(v.configuration.RunnerID))
	readFileAdapterOK(t, err, "retired Runner reference")
	snapshot, err := v.runners.Get(ctxFor(t), v.setup.agent.p2.base.adminBrowser.actor, target)
	readFileAdapterOK(t, err, "original retired Runner projection")
	if snapshot.Status != rc.Offline {
		t.Fatal("original Runner remains online after its actual WSS handler return")
	}
	v.identity(t, false)
	process.noLeaks(t, v.token, v.seed, v.path, v.configuration.RootPath)
}

func (v *runnerReadFileAppFixture) dispatch(t *testing.T, enabled bool, length int) {
	t.Helper()
	in := v.intent
	in.Arguments.Length = length
	// This is exactly the existing D15 strict Operation-authority substitute.
	// The current Agent, Mount ticket, real device session and Runner are real;
	// this test does not claim an Execution/ToolCall producer or Agent loop.
	owner := &readFileAdapterAuthority{store: v.setup.agent.p2.base.tracked, intent: in}
	current, err := agent.NewExecutionConfiguration(v.setup.agent.providers.Agents, owner)
	readFileAdapterOK(t, err, "real current Agent configuration")
	resolver, err := mount.NewReadFileResolver(v.setup.agent.p2.base.tracked, current, owner)
	readFileAdapterOK(t, err, "real current Mount resolver")
	dispatcher, err := runnerhttp.NewReadFileDispatcher(v.device, resolver)
	readFileAdapterOK(t, err, "real ticket-only dispatcher")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticket, err := resolver.Resolve(ctx, in)
	readFileAdapterOK(t, err, "current authorized read-file ticket")
	call, err := dispatcher.Prepare(ticket)
	readFileAdapterOK(t, err, "original read-file request identity")
	wantID := call.RequestID()
	if !wantID.Valid() || v.requests[wantID] {
		t.Fatal("dispatcher did not allocate a distinct original RequestID")
	}
	v.requests[wantID] = true
	result, err := call.Dispatch(ctx)
	if !enabled {
		requireCode(t, err, f.CapabilityUnsupported)
		if result.Attempted || result.Terminal != nil {
			t.Fatal("empty production capabilities admitted a read-file request")
		}
		return
	}
	readFileAdapterOK(t, err, "original CLI real WSS range read")
	if !result.Attempted || result.Terminal == nil || result.Terminal.Outcome != p.Success || result.Terminal.RequestID != wantID || result.Terminal.OperationID != p.ID(in.OperationID.String()) {
		t.Fatal("real CLI response does not correlate to its original prepared call")
	}
	value, err := p.DecodeReadFileResult(result.Terminal.Payload)
	end := min(7+length, len(v.content))
	if err != nil || value.Path != "notes.txt" || value.Offset != 7 || value.BytesRead != end-7 || value.EOF != (end == len(v.content)) || value.Encoding != "utf8" || value.Data != string(v.content[7:end]) {
		t.Fatal("real CLI read-file bytes/range differ from private deployment data")
	}
}

// Real production command/config/App/Client acceptance. Only the Operation
// authorization port retains the explicitly documented D15 fixture substitute.
// All process Waits precede the original server, P2 guard and database cleanup.
func TestRunnerReadFileApp(t *testing.T) {
	v := newRunnerReadFileAppFixture(t, runnerReadFileAppBinary(t))
	readFile := "read-file"
	if !t.Run("explicit-cli-range-and-identity-restart", func(t *testing.T) {
		first := runnerReadFileAppLaunch(t, v.binary, v.env(&readFile, true), []string{"--enroll"}, &v.token)
		v.online(t, first, 1, true)
		original := v.identity(t, true)
		v.dispatch(t, true, 64)
		v.stop(t, first)
		// Neither a new token nor Central/Runner/root configuration is supplied.
		second := runnerReadFileAppLaunch(t, v.binary, v.env(&readFile, false), nil, nil)
		v.online(t, second, 2, true)
		if v.identity(t, true) != original {
			t.Fatal("identity-only restart changed the original persisted bytes")
		}
		v.dispatch(t, true, 9)
		v.stop(t, second)
		t.Log("original_cli_enrollment=1 identity_only_restart=1 real_range_reads=2 original_waits=2 identity_released=true")
	}) {
		return
	}
	// Keep the stored root absent for both default compatibility and offline
	// validation. Only this fixture's deployment directory is renamed.
	root := v.configuration.RootPath
	readFileAdapterOK(t, os.Rename(root, root+".held"), "temporarily absent owned deployment root")
	t.Cleanup(func() {
		if os.Rename(root+".held", root) != nil {
			t.Error("owned deployment root restoration failed")
		}
	})
	if !t.Run("default-empty-capability-unsupported", func(t *testing.T) {
		empty := ""
		for index, operations := range []*string{nil, &empty} {
			process := runnerReadFileAppLaunch(t, v.binary, v.env(operations, false), nil, nil)
			v.online(t, process, int64(index+3), false)
			v.dispatch(t, false, 64)
			v.stop(t, process)
		}
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("empty operations opened or created an absent deployment root")
		}
		t.Log("default_unset_and_empty=2 original_capability_unsupported=2 root_remained_absent=true original_waits=2")
	}) {
		return
	}
	t.Run("invalid-root-and-explicit-config-before-connect", func(t *testing.T) {
		original, facts, counts := v.identity(t, false), v.facts(t), v.countsNow()
		check := runnerReadFileAppLaunch(t, v.binary, v.env(&readFile, false), []string{"--check-config"}, nil)
		check.wait(t, 0, true)
		check.noLeaks(t, v.token, v.seed, v.path, root)
		badRoot := runnerReadFileAppLaunch(t, v.binary, v.env(&readFile, false), nil, nil)
		badRoot.wait(t, 1, false)
		badRoot.noLeaks(t, v.token, v.seed, v.path, root)
		mismatch := v.env(&readFile, true)
		mismatch[len(mismatch)-1] = "AGENTEAM_RUNNER_ROOT_PATH=" + root + ".different"
		conflict := runnerReadFileAppLaunch(t, v.binary, mismatch, nil, nil)
		conflict.wait(t, 2, false)
		conflict.noLeaks(t, v.token, v.seed, v.path, root)
		for _, value := range []string{"write-file", "read-file,read-file"} {
			invalid := runnerReadFileAppLaunch(t, v.binary, v.env(&value, false), nil, nil)
			invalid.wait(t, 2, false)
			invalid.noLeaks(t, v.token, v.seed, v.path, root)
		}
		v.idle(t)
		// Protocol invocation and generation facts prove rejection before device
		// authentication/announcement. They are not a claim about zero TCP SYNs.
		if v.identity(t, false) != original || v.facts(t) != facts || v.countsNow() != counts {
			t.Fatal("offline check or rejected configuration reached the original device protocol or changed identity")
		}
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("offline or rejected command created the missing deployment root")
		}
		t.Log("offline_check=1 runtime_root_rejection=1 explicit_mismatch=1 closed_set_rejections=2 device_protocol_delta=0 identity_unchanged=true original_waits=5")
	})
}
