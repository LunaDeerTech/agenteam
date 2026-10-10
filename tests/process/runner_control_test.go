//go:build integration

package process_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func runnerRootDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp(".", ".runner-root-")
	if err != nil {
		t.Fatal("private root identity directory unavailable")
	}
	directory, err = filepath.Abs(directory)
	if err != nil || os.Chmod(directory, 0700) != nil {
		t.Fatal("private root identity directory invalid")
	}
	t.Cleanup(func() {
		if os.RemoveAll(directory) != nil {
			t.Error("owned root identity directory did not retire")
		}
	})
	return directory
}

// A real inherited pipe supplies one enrollment token. The parent closes both
// ends at their ownership boundaries, so EOF is not simulated by a reader stub.
func runnerRootLaunch(t *testing.T, env []string, token *string) *process {
	t.Helper()
	args := []string{}
	if token != nil {
		args = append(args, "--enroll")
	}
	v := &process{command: exec.Command(binaries["agenteam-runner"], args...), stderr: newOutput(), done: make(chan struct{})}
	v.command.Dir, v.command.Env = t.TempDir(), env
	v.command.Stdout, v.command.Stderr = &v.stdout, v.stderr
	var read, write *os.File
	if token != nil {
		var err error
		read, write, err = os.Pipe()
		if err != nil {
			t.Fatal("owned enrollment pipe unavailable")
		}
		defer read.Close()
		defer write.Close()
		v.command.Stdin = read
	}
	if v.command.Start() != nil {
		t.Fatal("default Runner cmd failed to start")
	}
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
				t.Error("owned Runner cmd was not actually waited after kill")
			}
		}
	})
	if token != nil {
		if read.Close() != nil || write.SetWriteDeadline(time.Now().Add(3*time.Second)) != nil {
			t.Fatal("owned enrollment pipe setup did not complete")
		}
		if n, err := io.WriteString(write, *token+"\n"); err != nil || n != len(*token)+1 {
			t.Fatal("original enrollment stdin write did not complete")
		}
		if write.Close() != nil {
			t.Fatal("original enrollment stdin EOF did not complete")
		}
	}
	return v
}

func runnerRootWait(t *testing.T, v *process, want int) {
	t.Helper()
	select {
	case <-v.done:
	case <-time.After(5 * time.Second):
		t.Fatal("original default Runner cmd Wait did not return")
	}
	code := 0
	if v.err != nil {
		var exited *exec.ExitError
		if !errors.As(v.err, &exited) {
			t.Fatal("original Runner cmd Wait returned a non-exit failure")
		}
		code = exited.ExitCode()
	}
	if code != want || v.stdout.Len() != 0 {
		t.Fatalf("default Runner cmd actual exit=%d want=%d stdout_empty=%t", code, want, v.stdout.Len() == 0)
	}
}

func runnerRootState(t *testing.T, v *process, want string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-v.stderr.events:
			if event["event"] == "runner_connection" && event["state"] == want {
				if event["connected"] != (want == "connected") || event["authenticated"] != (want == "connected") || event["ready"] != false {
					t.Fatal("default Runner connection log violates its public projection")
				}
				return
			}
		case <-v.done:
			t.Fatal("default Runner cmd exited before the expected connection state")
		case <-timer.C:
			t.Fatal("default Runner cmd did not publish the expected connection state")
		}
	}
}

func runnerRootStop(t *testing.T, v *process, signal syscall.Signal) {
	t.Helper()
	if v.command.Process.Signal(signal) != nil {
		t.Fatal("original Runner cmd signal failed")
	}
	runnerRootWait(t, v, 0)
	if !strings.Contains(v.stderr.String(), `"outcome":"drained"`) {
		t.Fatal("default Runner cmd did not report its actual drained shutdown")
	}
}

func runnerRootLock(t *testing.T, path string, locked bool) {
	t.Helper()
	file, err := identity.Open(path)
	if file != nil && file.Close() != nil {
		t.Fatal("root identity observation owner did not close")
	}
	if locked && !errors.Is(err, identity.ErrLocked) || !locked && err != nil {
		t.Fatal("default Runner identity lock disagrees with actual process ownership")
	}
}

func runnerRootIdentity(t *testing.T, path string, config identity.Configuration) ([32]byte, string) {
	t.Helper()
	stored, err := identity.ReadOnly(path)
	if err != nil || stored.State() != identity.Active || stored.Configuration() != config {
		t.Fatal("default Runner did not persist its exact active identity")
	}
	public, err := stored.PublicKey()
	if err != nil {
		t.Fatal("persisted public identity unavailable")
	}
	raw, err := os.ReadFile(path)
	defer clear(raw)
	var private struct {
		Seed string `json:"private_seed"`
	}
	if err != nil || json.Unmarshal(raw, &private) != nil || private.Seed == "" {
		t.Fatal("owned identity leak sentinel unavailable")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("default Runner private identity mode changed")
	}
	return public, private.Seed
}

func runnerRootSnapshot(t *testing.T, v *modelSystemBinary, target string, want rc.Status) rc.Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		response := v.request(t, "GET", "/api/v1/system/runners/"+target, "", nil, func(r *http.Request) { *r = *r.WithContext(ctx) }).want(t, 200)
		var snapshot rc.Snapshot
		err := json.Unmarshal(response.body, &snapshot)
		clear(response.body)
		if err != nil {
			t.Fatal("default Central Reader returned an invalid public snapshot")
		}
		if snapshot.Status == want {
			return snapshot
		}
		select {
		case <-ctx.Done():
			t.Fatal("default Central Reader did not publish the expected Runner state")
		case <-ticker.C:
		}
	}
}

func runnerRootCredential(t *testing.T, response modelSystemResponse, wantToken bool) (rc.Receipt, string) {
	t.Helper()
	defer clear(response.body)
	var out struct {
		Receipt   rc.Receipt `json:"receipt"`
		Available bool       `json:"token_available"`
		Token     string     `json:"enrollment_token"`
	}
	if json.Unmarshal(response.body, &out) != nil || out.Receipt.Validate() != nil || out.Available != wantToken || (out.Token != "") != wantToken {
		t.Fatal("default Central credential receipt or one-time token invalid")
	}
	if wantToken {
		if _, err := p.ParseEnrollmentToken(out.Token); err != nil {
			t.Fatal("default Central enrollment token invalid")
		}
	}
	return out.Receipt, out.Token
}

// Author root integration. No direct SQL write, service/Client replacement or
// fake token publication supplies any positive Runner/Account result. Requires
// the existing seven-resource root chain, not the PG-only fixture window.
func TestRunnerControlDefaultProcesses(t *testing.T) {
	v := newModelSystemBinary(t)
	proxy := newRunnerFailureTransport(t, v.address)
	directory := runnerRootDirectory(t)
	path, ca := filepath.Join(directory, "identity.json"), filepath.Join(directory, "root.pem")
	if os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.server.Certificate().Raw}), 0600) != nil {
		t.Fatal("owned root trust file unavailable")
	}
	target := modelSystemKey(t)
	runnerID, err := f.ParseID[rc.Runner](target)
	if err != nil {
		t.Fatal("root Runner identity invalid")
	}
	configuration := identity.Configuration{CentralURL: proxy.server.URL, RunnerID: p.ID(target), RootPath: "/srv/runner"}
	request := rc.CreateRequest{RunnerID: runnerID, Name: "default process runner", Description: "root lifecycle", Tags: []string{}, RootPath: configuration.RootPath}
	_, token := runnerRootCredential(t, v.request(t, "POST", "/api/v1/system/runners", modelSystemKey(t), request, nil).want(t, 200), true)
	v.logSecrets = append(v.logSecrets, token)
	base := []string{"AGENTEAM_RUNNER_IDENTITY_FILE=" + path, "AGENTEAM_RUNNER_CA_FILE=" + ca, "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT=3s"}
	enroll := append(append([]string{}, base...), "AGENTEAM_RUNNER_CENTRAL_URL="+configuration.CentralURL, "AGENTEAM_RUNNER_ID="+target, "AGENTEAM_RUNNER_ROOT_PATH="+configuration.RootPath)
	first := runnerRootLaunch(t, enroll, &token)
	runnerRootState(t, first, "connected")
	public, seed := runnerRootIdentity(t, path, configuration)
	v.logSecrets = append(v.logSecrets, seed, path)
	online := runnerRootSnapshot(t, v, target, rc.Online)
	if online.Version != 2 || online.CredentialGeneration != 1 || online.PublicKeyFingerprint == nil || string(*online.PublicKeyFingerprint) != p.PublicKeyFingerprint(public) || online.LastHello == nil || len(online.LastHello.Capabilities) != 0 || len(online.LastHello.FeatureFlags) != 0 || proxy.wss.Load() != 1 {
		t.Fatal("default roots did not bind the original key, empty operation registry and live control transport")
	}
	runnerRootLock(t, path, true)
	competitor := runnerRootLaunch(t, base, nil)
	runnerRootWait(t, competitor, 1)
	runnerRootLock(t, path, true)
	runnerRootSnapshot(t, v, target, rc.Online)
	runnerRootStop(t, first, syscall.SIGTERM)
	proxy.controlRetired(t)
	runnerRootLock(t, path, false)
	runnerRootSnapshot(t, v, target, rc.Offline)

	// No token/config is supplied to the normal process restart.
	second := runnerRootLaunch(t, base, nil)
	runnerRootState(t, second, "connected")
	reloaded, _ := runnerRootIdentity(t, path, configuration)
	if reloaded != public {
		t.Fatal("default restart changed the persisted device key")
	}
	online = runnerRootSnapshot(t, v, target, rc.Online)
	pathAPI := "/api/v1/system/runners/" + target
	revoked, _ := runnerRootCredential(t, v.request(t, "POST", pathAPI+"/revoke", modelSystemKey(t), rc.CredentialRequest{ExpectedVersion: online.Version}, nil).want(t, 200), false)
	runnerRootState(t, second, "disconnected")
	proxy.controlRetired(t)
	if revoked.Runner.Version != 3 || revoked.Runner.CredentialGeneration != 2 || runnerRootSnapshot(t, v, target, rc.Offline).PublicKeyFingerprint != nil {
		t.Fatal("default revoke retained a current credential")
	}
	runnerRootStop(t, second, syscall.SIGINT)
	runnerRootLock(t, path, false)
	issued, nextToken := runnerRootCredential(t, v.request(t, "POST", pathAPI+"/enrollment", modelSystemKey(t), rc.CredentialRequest{ExpectedVersion: revoked.Runner.Version}, nil).want(t, 200), true)
	v.logSecrets = append(v.logSecrets, nextToken)
	third := runnerRootLaunch(t, base, &nextToken)
	runnerRootState(t, third, "connected")
	nextPublic, nextSeed := runnerRootIdentity(t, path, configuration)
	v.logSecrets = append(v.logSecrets, nextSeed)
	online = runnerRootSnapshot(t, v, target, rc.Online)
	if issued.Runner.Version != 4 || issued.Runner.CredentialGeneration != 3 || online.Version != 5 || online.CredentialGeneration != 3 || nextPublic == public || online.PublicKeyFingerprint == nil || string(*online.PublicKeyFingerprint) != p.PublicKeyFingerprint(nextPublic) {
		t.Fatal("explicit re-enrollment did not replace the retired credential at the new generation")
	}
	conn := v.db.Connect(t)
	var facts bool
	if err := conn.QueryRow(databaseContext(t), `SELECT
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1)=3 AND
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1 AND consumed_at IS NOT NULL)=2 AND
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1)=5 AND
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1)=5 AND
 (SELECT connection_generation FROM agenteam_runner.runners WHERE id=$1)=3`, target).Scan(&facts); err != nil || !facts {
		t.Fatal("default roots duplicated or lost durable credential/connection facts")
	}
	// Stop Central while its original control connection is still alive. Its
	// actual cmd Wait and absent database backends must precede fixture retirement.
	v.stop(t, syscall.SIGTERM)
	runnerRootState(t, third, "disconnected")
	proxy.controlRetired(t)
	runnerRootStop(t, third, syscall.SIGTERM)
	runnerRootLock(t, path, false)
	if err := conn.QueryRow(databaseContext(t), `SELECT NOT EXISTS(SELECT 1 FROM agenteam_runner.connections WHERE runner_id=$1)`, target).Scan(&facts); err != nil || !facts {
		t.Fatal("default Central stopped before retiring its current connection")
	}
	for _, process := range []*process{first, competitor, second, third} {
		for _, secret := range v.logSecrets {
			if strings.Contains(process.stderr.String()+process.stdout.String(), secret) {
				t.Fatal("default Runner output leaked a credential or private identity path")
			}
		}
	}
}
