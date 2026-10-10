// Package process_test verifies the real cmd binaries in isolated processes.
package process_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	runneridentity "github.com/LunaDeerTech/agenteam/internal/runner/identity"
	runnerprotocol "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

var binaries map[string]string
var goTool, repository string

const processPublicHost = "localhost:8080"

func TestMain(m *testing.M) {
	goTool = os.Getenv("AGENTEAM_GO")
	if goTool == "" {
		goTool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	var err error
	repository, err = filepath.Abs("../..")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate repository")
		os.Exit(1)
	}
	version := exec.Command(goTool, "env", "GOVERSION")
	version.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	v, err := version.Output()
	if err != nil || strings.TrimSpace(string(v)) != "go1.27.1" {
		fmt.Fprintln(os.Stderr, "Go go1.27.1 is required for process tests")
		os.Exit(1)
	}
	directory, err := os.MkdirTemp("", "agenteam-process-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create process test directory")
		os.Exit(1)
	}
	binaries = make(map[string]string)
	for _, name := range []string{"agenteam", "agenteam-runner"} {
		path := filepath.Join(directory, name)
		command := exec.Command(goTool, "build", "-o", path, "./cmd/"+name)
		command.Dir = repository
		command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
		if output, err := command.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "test binary build failed: %v\n%s", err, output)
			_ = os.RemoveAll(directory)
			os.Exit(1)
		}
		binaries[name] = path
	}
	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

type eventOutput struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	pending []byte
	events  chan map[string]any
}

func newOutput() *eventOutput { return &eventOutput{events: make(chan map[string]any, 256)} }
func (w *eventOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.buffer.Write(b)
	w.pending = append(w.pending, b...)
	for {
		at := bytes.IndexByte(w.pending, '\n')
		if at < 0 {
			break
		}
		var event map[string]any
		if json.Unmarshal(w.pending[:at], &event) == nil {
			select {
			case w.events <- event:
			default:
			}
		}
		w.pending = w.pending[at+1:]
	}
	return len(b), nil
}
func (w *eventOutput) String() string { w.mu.Lock(); defer w.mu.Unlock(); return w.buffer.String() }

type process struct {
	command *exec.Cmd
	stdout  bytes.Buffer
	stderr  *eventOutput
	done    chan struct{}
	err     error
}

func launch(t *testing.T, name string, args, env []string) *process {
	t.Helper()
	p := &process{command: exec.Command(binaries[name], args...), stderr: newOutput(), done: make(chan struct{})}
	p.command.Dir = t.TempDir()
	p.command.Env = env
	p.command.Stdout = &p.stdout
	p.command.Stderr = p.stderr
	if err := p.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.command.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
			_ = p.command.Process.Kill()
			select {
			case <-p.done:
			case <-time.After(5 * time.Second):
				t.Error("owned child failed to exit after kill")
			}
		}
	})
	return p
}
func (p *process) wait(t *testing.T, want int) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("process did not exit: %s", p.stderr.String())
	}
	code := 0
	if p.err != nil {
		exit, ok := p.err.(*exec.ExitError)
		if !ok {
			t.Fatal(p.err)
		}
		code = exit.ExitCode()
	}
	if code != want {
		t.Fatalf("exit=%d want=%d stdout=%s stderr=%s", code, want, p.stdout.String(), p.stderr.String())
	}
}
func (p *process) event(t *testing.T, key, value string) map[string]any {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case record := <-p.stderr.events:
			if record[key] == value {
				return record
			}
		case <-p.done:
			t.Fatalf("process exited before %s=%s: %s", key, value, p.stderr.String())
			return nil
		case <-timer.C:
			t.Fatalf("missing %s=%s: %s", key, value, p.stderr.String())
			return nil
		}
	}
}

func assertLogRecords(t *testing.T, output, service string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	runID := ""
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record["service"] != service || record["event"] == nil || record["level"] == nil || record["ready"] != false {
			t.Fatalf("incomplete or false-ready log: %v", record)
		}
		stamp, ok := record["time"].(string)
		if !ok || !strings.HasSuffix(stamp, "Z") {
			t.Fatal("UTC timestamp missing")
		}
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
			t.Fatal(err)
		}
		id, ok := record["run_id"].(string)
		if !ok || len(id) != 32 {
			t.Fatal("process correlation ID missing")
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatal(err)
		}
		if runID != "" && runID != id {
			t.Fatal("run ID changed within one process")
		}
		runID = id
		if service == "runner" && (record["connected"] != false || record["authenticated"] != false) {
			t.Fatal("Runner claimed a connection or authentication")
		}
	}
}

func TestRunnerRealSIGTERMAndSIGINT(t *testing.T) {
	for _, signal := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(signal.String(), func(t *testing.T) {
			requestEntered, handlerReturned := make(chan struct{}), make(chan struct{})
			var once sync.Once
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/api/v1/runner/challenge" {
					http.Error(w, "rejected", http.StatusBadRequest)
					return
				}
				raw, err := io.ReadAll(io.LimitReader(r.Body, runnerprotocol.MaxDeviceBodyBytes+1))
				r.Body.Close()
				if _, decodeErr := runnerprotocol.DecodeChallengeRequest(raw); err != nil || decodeErr != nil {
					http.Error(w, "rejected", http.StatusBadRequest)
					return
				}
				once.Do(func() { close(requestEntered) })
				<-r.Context().Done()
				close(handlerReturned)
			}))
			t.Cleanup(server.Close)
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			p := launch(t, "agenteam-runner", nil, runnerProcessEnvironment(t, server.URL, ca))
			p.event(t, "state", "connecting")
			select {
			case <-requestEntered:
			case <-time.After(5 * time.Second):
				t.Fatal("Runner never entered its actual TLS challenge")
			}
			if err := p.command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			p.wait(t, 0)
			select {
			case <-handlerReturned:
			case <-time.After(5 * time.Second):
				t.Fatal("Runner native request did not actually retire")
			}
			if !strings.Contains(p.stderr.String(), `"outcome":"drained"`) || p.stdout.Len() != 0 {
				t.Fatal("clean Runner stop did not finish")
			}
			assertLogRecords(t, p.stderr.String(), "runner")
		})
	}
}

// The private file is a process stimulus, not proof of a Central enrollment.
// Normal authentication and same-key recovery are verified by runnercontrol's
// real Central integration matrix, while this fixture owns only process exit.
func runnerProcessEnvironment(t *testing.T, origin string, ca []byte) []string {
	t.Helper()
	directory, err := os.MkdirTemp(".", ".runner-process-")
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
	id, err := runnerprotocol.NewID()
	if err != nil {
		t.Fatal(err)
	}
	v, err := runneridentity.NewPending(runneridentity.Configuration{CentralURL: origin, RunnerID: id, RootPath: "/runner/work"})
	if err != nil {
		t.Fatal(err)
	}
	v, err = v.AsActive()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := runneridentity.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.Save(v); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	env := []string{"AGENTEAM_RUNNER_IDENTITY_FILE=" + path}
	if ca != nil {
		file := filepath.Join(directory, "root.pem")
		if err = os.WriteFile(file, ca, 0600); err != nil {
			t.Fatal(err)
		}
		env = append(env, "AGENTEAM_RUNNER_CA_FILE="+file)
	}
	return env
}

func checkDiagnosticBinary(t *testing.T, address string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	wrongHost, _ := http.NewRequest("GET", "http://"+address+"/api/v1/session?secret=query-SENTINEL", nil)
	wrongHost.Host = "wrong-public-origin.invalid"
	wrongHost.Header.Set("Authorization", "header-SENTINEL")
	rejected, err := client.Do(wrongHost)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, rejected.Body)
	_ = rejected.Body.Close()
	if readErr != nil || rejected.StatusCode != http.StatusForbidden || rejected.Header.Get("X-Request-ID") == "" {
		t.Fatal("wrong public Host crossed the account boundary")
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/livez", 200}, {"HEAD", "/livez", 200}, {"GET", "/readyz", 503}, {"HEAD", "/readyz", 503}, {"GET", "/diagnostics", 200}, {"HEAD", "/diagnostics", 200}, {"POST", "/diagnostics", 405}, {"GET", "/api/v1/session", 401}, {"GET", "/page", 404}, {"GET", "/assets/missing.js", 404},
	} {
		request, _ := http.NewRequest(tc.method, "http://"+address+tc.path+"?secret=query-SENTINEL", nil)
		request.Host = processPublicHost
		request.Header.Set("Authorization", "header-SENTINEL")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != tc.status || response.Header.Get("X-Request-ID") == "" {
			t.Fatalf("%s %s status %d error %v", tc.method, tc.path, response.StatusCode, err)
		}
		if tc.method == "HEAD" && len(body) != 0 {
			t.Fatal("HEAD emitted body")
		}
		if tc.method == "GET" && tc.path == "/readyz" && !bytes.Contains(body, []byte(`"code":"DEPENDENCY_UNBOUND"`)) {
			t.Fatal("false readiness")
		}
		if tc.path == "/diagnostics" && tc.method == "GET" && !bytes.Contains(body, []byte(`"ready":false`)) {
			t.Fatal("diagnostics claimed readiness")
		}
	}
}

func assertNoSockets(t *testing.T, pid int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("socket descriptor inspection requires Linux")
	}
	directory := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, "socket:") {
			t.Fatalf("unconnected Runner owns a socket: %s", target)
		}
	}
}

func TestCLIScopeAndSafeFailures(t *testing.T) {
	const secret = "credential-SENTINEL"
	for _, name := range []string{"agenteam", "agenteam-runner"} {
		t.Run(name, func(t *testing.T) {
			prefix, service := "AGENTEAM_CENTRAL_", "central"
			if name == "agenteam-runner" {
				prefix, service = "AGENTEAM_RUNNER_", "runner"
			}
			for _, arg := range []string{"--help", "--version"} {
				p := launch(t, name, []string{arg}, []string{prefix + "LOG_LEVEL=" + secret})
				p.wait(t, 0)
				if strings.Contains(p.stdout.String()+p.stderr.String(), secret) || p.stderr.String() != "" {
					t.Fatal("informational CLI loaded configuration")
				}
			}
			var checkedEnvironment []string
			scope := "d15"
			if service == "central" {
				scope = "d05"
				checkedEnvironment = []string{`AGENTEAM_CENTRAL_SECRET_KEYRING={"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, `AGENTEAM_CENTRAL_CURSOR_KEYRING={"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, "AGENTEAM_CENTRAL_DATABASE_URL=postgresql://config:config-only@127.0.0.1:1/config_only", "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable"}
			}
			if service == "central" {
				checkedEnvironment = append(checkedEnvironment, objectfixture.ConfigOnlyEnvironment()...)
				checkedEnvironment = append(checkedEnvironment, accountEnvironment(t, "config")...)
			} else {
				checkedEnvironment = runnerProcessEnvironment(t, "https://runner.invalid", nil)
			}
			p := launch(t, name, []string{"--check-config"}, checkedEnvironment)
			p.wait(t, 0)
			var result map[string]any
			if json.Unmarshal(p.stdout.Bytes(), &result) != nil || result["scope"] != scope || result["valid"] != true || result["ready"] != false {
				t.Fatal("config check pretended to validate product dependencies")
			}
			if service == "runner" && (result["connected"] != false || result["authenticated"] != false) {
				t.Fatal("Runner config check claimed connection")
			}
			for _, args := range [][]string{{"--" + secret}, {secret}, {"--help", secret}, {"--version", "--check-config"}} {
				p := launch(t, name, args, nil)
				p.wait(t, 2)
				if strings.Contains(p.stdout.String()+p.stderr.String(), secret) {
					t.Fatal("CLI argument leaked")
				}
				assertLogRecords(t, p.stderr.String(), service)
			}
			for _, env := range [][]string{{prefix + "LOG_LEVEL=" + secret}, {prefix + "SHUTDOWN_TIMEOUT="}, {prefix + "FUTURE_SECRET=" + secret}, {prefix + secret + "=" + secret}} {
				p := launch(t, name, []string{"--check-config"}, env)
				p.wait(t, 2)
				logs := p.stderr.String()
				if strings.Contains(logs+p.stdout.String(), secret) || strings.Contains(logs, `"event":"listening"`) || strings.Contains(logs, `"phase":"unconnected"`) {
					t.Fatal("invalid configuration leaked or started resources")
				}
				assertLogRecords(t, logs, service)
			}
		})
	}
}

func TestCentralMissingDatabaseAndInvalidAddressExit(t *testing.T) {
	p := launch(t, "agenteam", nil, nil)
	p.wait(t, 2)
	if !strings.Contains(p.stderr.String(), `"field":"AGENTEAM_CENTRAL_DATABASE_URL"`) || strings.Contains(p.stderr.String(), `"event":"listening"`) {
		t.Fatal("required database configuration not enforced")
	}
	p = launch(t, "agenteam", nil, []string{"AGENTEAM_CENTRAL_HTTP_ADDR=credential-SENTINEL"})
	p.wait(t, 2)
	if strings.Contains(p.stderr.String(), "SENTINEL") {
		t.Fatal("invalid address leaked")
	}
}

func TestCentralCheckAndCompiledRepairNeverConnect(t *testing.T) {
	source, err := postgres.EmbeddedSource()
	if err != nil {
		t.Fatal(err)
	}
	for _, repair := range []bool{false, true} {
		name := "check"
		if repair {
			name = "unsupported_compiled_repair"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan bool, 1)
			go func() {
				conn, err := listener.Accept()
				if err == nil {
					_ = conn.Close()
				}
				accepted <- err == nil
			}()
			args := []string{"--check-config"}
			want := 0
			if repair {
				args = []string{"--expected-checksum", string(source.Manifest()[0].Checksum), "--repair-migration", "1"}
				want = 1
			}
			p := launch(t, "agenteam", args, append(append(objectfixture.ConfigOnlyEnvironment(), accountEnvironment(t, "check-repair")...), []string{`AGENTEAM_CENTRAL_SECRET_KEYRING={"format":1,"current_version":"1","keys":[{"version":"1","key_b64":"ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="}]}`, `AGENTEAM_CENTRAL_CURSOR_KEYRING={"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`, "AGENTEAM_CENTRAL_DATABASE_URL=postgresql://pure:password-SENTINEL@" + listener.Addr().String() + "/pure", "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable"}...))
			p.wait(t, want)
			_ = listener.Close()
			if <-accepted {
				t.Fatal("pure check or unsupported repair opened a database connection")
			}
			if repair && (p.stdout.Len() != 0 || !strings.Contains(p.stderr.String(), "MIGRATION_REPAIR_UNSUPPORTED")) {
				t.Fatal("compiled transactional migration repair claimed success")
			}
			if strings.Contains(p.stdout.String()+p.stderr.String(), "SENTINEL") {
				t.Fatal("database URL credential leaked")
			}
		})
	}
}

func TestRunnerAndNeutralDependencyBoundaries(t *testing.T) {
	for _, pattern := range []string{"./cmd/agenteam-runner", "./internal/platform/..."} {
		command := exec.Command(goTool, "list", "-deps", pattern)
		command.Dir = repository
		command.Env = append(os.Environ(), "GOTOOLCHAIN=local")
		output, err := command.Output()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(output, []byte("github.com/LunaDeerTech/agenteam/internal/central")) {
			t.Fatal("Runner/neutral code imports Central")
		}
		if pattern == "./internal/platform/..." && bytes.Contains(output, []byte("github.com/LunaDeerTech/agenteam/internal/runner/")) {
			t.Fatal("neutral code imports Runner business code")
		}
	}
}
