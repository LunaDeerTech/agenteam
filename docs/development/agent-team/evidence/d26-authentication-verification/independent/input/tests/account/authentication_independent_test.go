//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAccountAuthenticationWebIndependent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newAuthenticationWebFixture(t, ctx)
	f.setChallengeThreshold(ctx)
	root, err := filepath.Abs("../account-captcha-web")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "independent.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TMPDIR=") && !strings.HasPrefix(value, "AGENTEAM_AUTH_WEB_") && !strings.HasPrefix(value, "PLAYWRIGHT_NO_COPY_PROMPT=") && !strings.HasPrefix(value, "DEBUG=") && !strings.HasPrefix(value, "PWDEBUG=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err = cmd.Run() // Wait returns the actual child terminal state.
	cmd.Env = nil
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	t.Log(strings.ReplaceAll(output.String(), f.entry.Password, "[redacted]"))
	if err != nil {
		t.Fatalf("independent real browser failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(f.directory, "independent-result.json"))
	if err != nil {
		t.Fatal("independent safe result missing")
	}
	defer clear(raw)
	var r struct {
		UserID              string `json:"user_id"`
		SessionID           string `json:"session_id"`
		FailedChallengeID   string `json:"failed_challenge_id"`
		ConsumedChallengeID string `json:"consumed_challenge_id"`
		FailedProof         bool   `json:"failed_proof"`
		FailedFocus         bool   `json:"failed_focus"`
		NativeBlur          bool   `json:"native_blur"`
		ExactIntent         bool   `json:"exact_intent"`
		ExactPass           bool   `json:"exact_pass"`
		IdentityConfirmed   bool   `json:"identity_confirmed"`
		AnonymousRejected   bool   `json:"anonymous_rejected"`
		SessionCSRFMatched  bool   `json:"session_csrf_matched"`
		LoggedOut           bool   `json:"logged_out"`
	}
	if json.Unmarshal(raw, &r) != nil || r.UserID != f.entry.ID || r.SessionID == "" || r.FailedChallengeID == "" || r.ConsumedChallengeID == "" || r.FailedChallengeID == r.ConsumedChallengeID || !r.FailedProof || !r.FailedFocus || !r.NativeBlur || !r.ExactIntent || !r.ExactPass || !r.IdentityConfirmed || !r.AnonymousRejected || !r.SessionCSRFMatched || !r.LoggedOut {
		t.Fatal("independent real chain did not establish every required safe fact")
	}
	conn := f.db.Connect(t)
	for _, v := range []struct{ id, phase string }{{r.FailedChallengeID, "failed"}, {r.ConsumedChallengeID, "consumed"}} {
		var phase string
		if e := conn.QueryRow(ctx, `SELECT phase FROM agenteam_account.challenges WHERE id=$1`, v.id).Scan(&phase); e != nil || phase != v.phase {
			t.Fatal("exact challenge durable terminal fact missing", e)
		}
	}
	var revoked bool
	if e := conn.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND revoked_reason='logout' FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, r.SessionID, r.UserID).Scan(&revoked); e != nil || !revoked {
		t.Fatal("exact independently confirmed Session was not revoked by logout", e)
	}
	var audits int
	if e := conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action IN ('account.login','account.logout') AND metadata->>'session_id'=$1 AND outcome='success'`, r.SessionID).Scan(&audits); e != nil || audits != 2 {
		t.Fatal("exact independent Session login/logout audit pair missing", e)
	}
	t.Logf("independent real chain: wrong proof failed; native blur and failure focus; fresh proof consumed; exact intent/pass; Session identity and CSRF bound; logout=true audit=%d", audits)
}
