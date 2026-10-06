//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type systemDirectoryWebUser struct {
	ID                        string `json:"id"`
	Email                     string `json:"email"`
	Username                  string `json:"username"`
	DisplayName               string `json:"display_name"`
	Role                      string `json:"role"`
	Theme                     string `json:"theme"`
	Version                   string `json:"version"`
	InitialPasswordSuggestion bool   `json:"initial_password_suggestion"`
	CreatedAt                 string `json:"created_at"`
}

type systemDirectoryWebFixture struct {
	*authenticationWebFixture
	admin, member  personalWebCredential
	expected       []systemDirectoryWebUser
	revokedSession string
	demotedSession string
}

func newSystemDirectoryWebFixture(t *testing.T, ctx context.Context) *systemDirectoryWebFixture {
	t.Helper()
	base := newAuthenticationWebFixture(t, ctx)
	f := &systemDirectoryWebFixture{authenticationWebFixture: base}
	f.admin = personalWebCredential{Email: base.entry.Email, Password: base.entry.Password, UserID: base.entry.ID}
	// Reuse the accepted private HTTP preparation helpers. These two identities
	// come from real bootstrap and invitation redemption, not direct inserts.
	setup := &personalWebFixture{authenticationWebFixture: base}
	admin := setup.setupClient()
	bootstrap := setup.setupRequest(ctx, admin, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	setup.setupRequest(ctx, admin, "POST", "/api/v1/sessions/login", map[string]string{"email": f.admin.Email, "password": f.admin.Password}, httpString(t, bootstrap, "csrf_token"), true, 200)
	session := setup.setupRequest(ctx, admin, "GET", "/api/v1/session", nil, "", false, 200)
	f.member = setup.inviteMember(ctx, admin, httpString(t, session, "csrf_token"), "directory-member@example.com", "directory-member")
	conn := base.db.Connect(t)
	// These 28 rows are explicitly read-only pagination/layout fixtures. They
	// are not evidence of invitation registration. Paired microsecond timestamps
	// exercise the complete (created_at,id) ordering within one millisecond.
	for i := range 28 {
		at := time.Date(2026, 10, 5, 1, 2, 3, 456000000, time.UTC).Add(time.Duration(i/2) * time.Microsecond)
		display := strings.Repeat("系统目录长名称 Mixed ", 4)
		if i%3 == 0 {
			display = ""
		}
		_, err := conn.Exec(ctx, `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme,created_at,updated_at)
SELECT $1,$2,$3,$4,'user',password_phc,1,$5,1,false,'system',$6,$6 FROM agenteam_account.users WHERE id=$7`,
			id[struct{}](t).String(), fmt.Sprintf("directory-%02d-%s@example.com", i, strings.Repeat("long", 20)), fmt.Sprintf("directory-fixture-%02d", i), display, int64(i+1), at, f.member.UserID)
		if err != nil {
			t.Fatal("owned pagination-only sample preparation failed", err)
		}
	}
	f.expected = f.databaseUsers(ctx)
	if len(f.expected) != 30 {
		t.Fatal("owned directory requires two formally created identities and 28 explicit samples")
	}
	f.writePrivate("directory-material.json", map[string]any{"admin": f.admin, "member": f.member, "expected": f.expected})
	t.Cleanup(func() {
		for _, secret := range []string{f.admin.Password, f.member.Password} {
			if base.log.contains(secret) {
				t.Error("directory credentials escaped restricted private transfer")
			}
		}
		f.admin.Password, f.member.Password = "", ""
	})
	return f
}

func (f *systemDirectoryWebFixture) databaseUsers(ctx context.Context) []systemDirectoryWebUser {
	f.t.Helper()
	rows, err := f.db.Connect(f.t).Query(ctx, `SELECT id::text,email,username,display_name,role,theme,version::text,initial_password_suggestion,created_at FROM agenteam_account.users ORDER BY created_at DESC,id DESC`)
	if err != nil {
		f.t.Fatal("owned canonical directory query failed", err)
	}
	defer rows.Close()
	var result []systemDirectoryWebUser
	for rows.Next() {
		var user systemDirectoryWebUser
		var at time.Time
		if err := rows.Scan(&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.Role, &user.Theme, &user.Version, &user.InitialPasswordSuggestion, &at); err != nil {
			f.t.Fatal("owned canonical directory scan failed", err)
		}
		canonical, err := foundation.NewInstant(at)
		if err != nil {
			f.t.Fatal("owned canonical registration time failed", err)
		}
		user.CreatedAt = canonical.String()
		result = append(result, user)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal("owned canonical directory did not finish", err)
	}
	return result
}

func (f *systemDirectoryWebFixture) writePrivate(name string, value any) {
	f.t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal("private directory encoding failed")
	}
	defer clear(raw)
	path := filepath.Join(f.directory, name)
	if os.WriteFile(path+".tmp", raw, 0600) != nil || os.Rename(path+".tmp", path) != nil {
		f.t.Fatal("private directory transfer failed")
	}
}

type systemDirectoryWebResult struct {
	Completed  bool `json:"completed"`
	Canonical  bool `json:"canonical"`
	Pages      int  `json:"pages"`
	Rows       int  `json:"rows"`
	Denied     int  `json:"denied"`
	Revoked    bool `json:"revoked"`
	Demoted    bool `json:"demoted"`
	Switched   bool `json:"switched"`
	Navigation bool `json:"navigation"`
	Layouts    int  `json:"layouts"`
}

func (f *systemDirectoryWebFixture) browserDirectory(ctx context.Context, name string) systemDirectoryWebResult {
	f.t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	if err != nil {
		f.t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "system-user-directory.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TMPDIR=") && !strings.HasPrefix(value, "AGENTEAM_AUTH_WEB_") && !strings.HasPrefix(value, "AGENTEAM_DIRECTORY_WEB_") && !strings.HasPrefix(value, "PLAYWRIGHT_NO_COPY_PROMPT=") && !strings.HasPrefix(value, "DEBUG=") && !strings.HasPrefix(value, "PWDEBUG=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_DIRECTORY_WEB_CASE="+name, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	if images := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES"); images != "" {
		if !filepath.IsAbs(images) {
			f.t.Fatal("owned directory image path must be absolute")
		}
		cmd.Env = append(cmd.Env, "AGENTEAM_AUTH_WEB_IMAGES="+images)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		f.t.Fatal("locked directory browser runner could not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	sequence := 0
	var runErr error
wait:
	for {
		select {
		case runErr = <-done:
			joined = true
			break wait
		case <-ticker.C:
			raw, err := os.ReadFile(filepath.Join(f.directory, "directory-ipc.json"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			var request struct {
				Sequence  int    `json:"sequence"`
				Action    string `json:"action"`
				UserID    string `json:"user_id"`
				SessionID string `json:"session_id"`
			}
			decodeErr := json.Unmarshal(raw, &request)
			clear(raw)
			if err != nil || decodeErr != nil || name != "authority" || request.UserID != f.admin.UserID || request.Sequence < 1 || request.Sequence > 2 {
				f.t.Error("private directory IPC ownership rejected")
				_ = cmd.Cancel()
				continue
			}
			if request.Sequence <= sequence {
				continue
			}
			_, idErr := foundation.ParseID[struct{}](request.SessionID)
			if request.Sequence != sequence+1 || idErr != nil ||
				(request.Sequence == 1 && request.Action != "revoke-session") || (request.Sequence == 2 && request.Action != "demote") {
				f.t.Error("private directory IPC sequence rejected")
				_ = cmd.Cancel()
				continue
			}
			// Mutations affect only the exact owned identity/session asserted above.
			// Product GET/Session requests must still observe the real backend denial.
			conn := f.db.Connect(f.t)
			if request.Action == "revoke-session" {
				tag, err := conn.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, request.SessionID, f.admin.UserID)
				if err != nil || tag.RowsAffected() != 1 {
					f.t.Error("exact owned Session revocation fixture failed", err)
					_ = cmd.Cancel()
					continue
				}
				f.revokedSession = request.SessionID
			} else {
				var owned bool
				if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL)`, request.SessionID, f.admin.UserID).Scan(&owned); err != nil || !owned {
					f.t.Error("exact current owned Session missing before role fixture", err)
					_ = cmd.Cancel()
					continue
				}
				tag, err := conn.Exec(ctx, `UPDATE agenteam_account.users SET role='user',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND role='admin'`, f.admin.UserID)
				if err != nil || tag.RowsAffected() != 1 {
					f.t.Error("exact owned role fixture failed", err)
					_ = cmd.Cancel()
					continue
				}
				f.demotedSession = request.SessionID
			}
			sequence = request.Sequence
			f.writePrivate("directory-ack-"+strconv.Itoa(sequence)+".json", map[string]any{"ok": true, "sequence": sequence})
		}
	}
	safe := output.String()
	for _, password := range []string{f.admin.Password, f.member.Password} {
		safe = strings.ReplaceAll(safe, password, "[redacted]")
	}
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual directory production browser failed: %v", runErr)
	}
	raw, err := os.ReadFile(filepath.Join(f.directory, "directory-result.json"))
	if err != nil {
		f.t.Fatal("safe directory result missing")
	}
	defer clear(raw)
	var result systemDirectoryWebResult
	if json.Unmarshal(raw, &result) != nil || !result.Completed {
		f.t.Fatal("safe directory result invalid")
	}
	return result
}
