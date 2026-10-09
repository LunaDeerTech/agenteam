//go:build integration

package account_test

// This overlay adds independent scenarios to the accepted real fixture. It
// does not replace the author's six top-level tests or their result checks.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

var independentModelChecks = map[string][]string{
	"a": {"credential_rotation_unknown_original", "provider_update_unknown_original", "lookups_observe_only", "exact_original_and_unique_history", "safe_schema_client"},
	"b": {"archived_credential_rotation_lookup_only", "archived_provider_update_original", "current_revoked_session_denied", "exact_original_and_unique_history", "safe_schema_client"},
}

func independentModelResult(raw []byte, selected, inputHash string) (projectModelsWebResult, error) {
	var result projectModelsWebResult
	bad := errors.New("independent closed result rejected")
	if _, err := projectModelsWebObject(raw, "protocol", "input_hash", "completed", "mode", "checks", "counts", "schema_bodies", "client_bodies", "layouts"); err != nil || json.Unmarshal(raw, &result) != nil {
		return result, bad
	}
	checks := independentModelChecks[selected]
	if len(checks) == 0 || result.Protocol != projectModelsWebProtocol || result.InputHash != inputHash || !result.Completed || result.Mode != "independent-"+selected || result.Layouts != 0 || len(result.Checks) != len(checks) {
		return result, bad
	}
	for _, key := range checks {
		if !result.Checks[key] {
			return result, bad
		}
	}
	if result.SchemaBodies < 1 || result.SchemaBodies > 512 || result.ClientBodies != result.SchemaBodies {
		return result, bad
	}
	counts, err := projectModelsWebObject(result.Counts, "server", "browser")
	if err != nil {
		return result, bad
	}
	browser, err := projectModelsWebObject(counts["browser"], "attempts", "complete_eof", "incomplete", "typed_client_ok", "schema_ok")
	if err != nil {
		return result, bad
	}
	numbers := map[string]int{}
	for key, value := range browser {
		var n int
		if string(value) == "null" || json.Unmarshal(value, &n) != nil || n < 0 || n > 512 {
			return result, bad
		}
		numbers[key] = n
	}
	if numbers["attempts"] != numbers["complete_eof"]+numbers["incomplete"] || numbers["complete_eof"] != result.SchemaBodies || numbers["schema_ok"] != result.SchemaBodies || numbers["typed_client_ok"] != result.ClientBodies {
		return result, bad
	}
	if _, err := projectModelsWebObject(counts["server"], "operations", "session", "server", "controls", "browser_eof", "schema_bodies", "client_bodies"); err != nil {
		return result, bad
	}
	// verifyModelBrowserEvidence additionally binds this complete server object
	// to the fixture's actual last counts, and every native EOF to safe bytes.
	return result, nil
}

func independentModelBrowser(f *projectModelsWebFixture, ctx context.Context, selected string) projectModelsWebResult {
	f.t.Helper()
	repository := os.Getenv("MODELS_INDEPENDENT_ROOT")
	if !filepath.IsAbs(repository) {
		f.t.Fatal("independent repository unavailable")
	}
	config := filepath.Join(repository, ".agent-state/model-ui-independent/independent.config.mjs")
	cli := filepath.Join(repository, "tests/account-captcha-web/node_modules/@playwright/test/cli.js")
	for _, path := range []string{config, cli} {
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			f.t.Fatal("independent browser input unavailable")
		}
	}
	browserCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(browserCtx, "node", cli, "test", "--config", config)
	cmd.Dir = repository
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && !strings.HasPrefix(key, "AGENTEAM_") && !strings.HasPrefix(key, "MODELS_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "MODELS_INDEPENDENT_ROOT="+repository, "MODELS_INDEPENDENT_CASE="+selected,
		"AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory,
		"AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_PROJECT_MODELS_WEB_CASE="+f.mode,
		"AGENTEAM_PROJECT_MODELS_WEB_DIST="+f.webRoot, "AGENTEAM_PROJECT_MODELS_WEB_EVIDENCE="+f.evidence,
		"AGENTEAM_PROJECT_MODELS_WEB_INPUT_HASH="+f.inputHash, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var privateOutput bytes.Buffer
	cmd.Stdout, cmd.Stderr = &privateOutput, &privateOutput
	f.mu.Lock()
	f.browserActive = true
	f.mu.Unlock()
	if err := cmd.Start(); err != nil {
		f.t.Fatal("independent browser start failed")
	}
	done := make(chan error, 1)
	joined := false
	defer func() {
		f.releaseAll()
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		clear(privateOutput.Bytes())
		cmd.Env = nil
		f.t.Log("Independent Node child actually waited; outer supervisor owns adopted children")
	}()
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	sequence := 0
	var previous []byte
	defer func() { clear(previous) }()
	for {
		select {
		case runErr := <-done:
			joined = true
			if runErr != nil {
				f.safeEvidence("independent-browser-exit.json", map[string]any{"exit_code": cmd.ProcessState.ExitCode(), "context_done": browserCtx.Err() != nil})
				f.t.Fatal("independent browser failed; private diagnostics suppressed")
			}
			raw, err := projectModelsWebReadPrivate(filepath.Join(f.directory, "independent-result.json"), 65536)
			if err != nil {
				f.t.Fatal("independent browser result unavailable")
			}
			defer clear(raw)
			result, err := independentModelResult(raw, selected, f.inputHash)
			if err != nil || f.verifyModelBrowserEvidence(result) != nil {
				f.t.Fatal("independent browser final facts rejected")
			}
			f.safeEvidence("independent-browser-result.json", result)
			return result
		case <-ticker.C:
			raw, err := projectModelsWebReadPrivate(filepath.Join(f.directory, "project-models-ipc.json"), 8192)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				f.t.Fatal("independent IPC file rejected")
			}
			if bytes.Equal(previous, raw) {
				clear(raw)
				continue
			}
			request, code := decodeProjectModelsWebIPC(raw, f.inputHash, sequence+1)
			if code != "" {
				clear(raw)
				f.t.Fatal("independent IPC envelope rejected")
			}
			clear(previous)
			previous = raw
			sequence = request.Sequence
			ack := f.modelIPC(ctx, request)
			encoded, err := json.Marshal(ack)
			if err != nil || len(encoded) > 65536 {
				f.t.Fatal("independent IPC acknowledgement rejected")
			}
			f.private("project-models-ack-"+fmtIndependentSequence(sequence)+".json", ack)
			if ack["ok"] != true {
				cancel()
			}
		}
	}
}

func fmtIndependentSequence(n int) string {
	// The accepted protocol requires canonical base-ten 1..128 filenames.
	return strconv.Itoa(n)
}

func runIndependentModels(t *testing.T, selected, fixtureMode string) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("independent top exceeded original cleanup budget")
		}
	})
	f := newProjectModelsWebFixture(t, ctx, fixtureMode)
	result := independentModelBrowser(f, ctx, selected)
	f.stopProxy()
	f.mu.Lock()
	joined := f.modelServer.Started == f.modelServer.Finished && f.modelControls.Held == f.modelControls.HeldJoined && !f.modelFailure
	f.mu.Unlock()
	if !joined {
		t.Fatal("independent proxy did not actually join")
	}
	f.safeEvidence("independent-go-facts.json", map[string]any{"protocol": projectModelsWebProtocol, "input_hash": f.inputHash, "case": selected, "proxy_actual_join": joined, "browser_result": result, "auxiliary_archive_fixture_only": true})
}

func TestIndependentProjectModelsWebUnknownOriginalCombination(t *testing.T) {
	runIndependentModels(t, "a", "recovery")
}
func TestIndependentProjectModelsWebCurrentAuthorityArchivedRecovery(t *testing.T) {
	runIndependentModels(t, "b", "authority")
}
