//go:build integration

package account_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type personalWebCredential struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	UserID   string `json:"user_id"`
}

type personalWebFixture struct {
	*authenticationWebFixture
	admin, member personalWebCredential
	newPassword   string
}

func newPersonalWebFixture(t *testing.T, ctx context.Context) *personalWebFixture {
	t.Helper()
	f := &personalWebFixture{authenticationWebFixture: newAuthenticationWebFixture(t, ctx)}
	f.admin = personalWebCredential{Email: f.entry.Email, Password: f.entry.Password, UserID: f.entry.ID}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal("private setup entropy unavailable")
	}
	f.newPassword = " Changed 字符 " + base64.RawURLEncoding.EncodeToString(secret) + " "
	clear(secret)
	admin := f.setupClient()
	anonymous := f.setupRequest(ctx, admin, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	f.setupRequest(ctx, admin, "POST", "/api/v1/sessions/login", map[string]string{"email": f.admin.Email, "password": f.admin.Password}, httpString(t, anonymous, "csrf_token"), true, 200)
	session := f.setupRequest(ctx, admin, "GET", "/api/v1/session", nil, "", false, 200)
	csrf := httpString(t, session, "csrf_token")
	f.member = f.inviteMember(ctx, admin, csrf, "personal-settings-long-email-address-for-owned-browser@example.com", "settings-member")
	duplicate := f.inviteMember(ctx, admin, csrf, "settings-existing@example.com", "settings-existing")
	duplicate.Password = ""
	material, err := json.Marshal(struct {
		Admin       personalWebCredential `json:"admin"`
		Member      personalWebCredential `json:"member"`
		NewPassword string                `json:"new_password"`
	}{f.admin, f.member, f.newPassword})
	if err != nil {
		t.Fatal("private settings material encoding failed")
	}
	defer clear(material)
	if err = os.WriteFile(filepath.Join(f.directory, "personal-credentials.json"), material, 0600); err != nil {
		t.Fatal("private settings material transfer failed")
	}
	f.writeImages()
	t.Cleanup(func() {
		for _, value := range []string{f.admin.Password, f.member.Password, f.newPassword} {
			if f.log.contains(value) {
				t.Error("settings material escaped restricted transfer")
			}
		}
		f.admin.Password, f.member.Password, f.newPassword = "", "", ""
	})
	return f
}

func (f *personalWebFixture) setupClient() *http.Client {
	f.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		f.t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	f.t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Jar: jar, Transport: transport, Timeout: 20 * time.Second}
}

func (f *personalWebFixture) setupRequest(ctx context.Context, client *http.Client, method, path string, body any, csrf string, keyed bool, status int) map[string]any {
	f.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal("invalid private setup body")
	}
	defer clear(raw)
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(raw)
	}
	r, err := http.NewRequestWithContext(ctx, method, f.origin+path, reader)
	if err != nil {
		f.t.Fatal("invalid owned setup request")
	}
	r.Header.Set("Origin", f.origin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if keyed {
		r.Header.Set("Idempotency-Key", id[struct{}](f.t).String())
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := client.Do(r)
	if err != nil {
		f.t.Fatal("formal settings setup HTTP failed")
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		f.t.Fatalf("formal settings setup %s %s status=%d want=%d", method, path, response.StatusCode, status)
	}
	var value map[string]any
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value) != nil {
		f.t.Fatal("formal settings setup response invalid")
	}
	return value
}

// Ordinary users are created by the exact random-origin invitation flow. The
// retained record closure reads only this root's restricted recovery log.
func (f *personalWebFixture) inviteMember(ctx context.Context, admin *http.Client, csrf, email, username string) personalWebCredential {
	f.t.Helper()
	created := f.setupRequest(ctx, admin, "POST", "/api/v1/system/invitations", map[string]string{"email": email}, csrf, true, 201)
	entry := f.record("invitation", email, httpString(f.t, created, "id"))
	u, err := url.Parse(entry.URL)
	if err != nil || u.Scheme+"://"+u.Host != f.origin || u.Fragment == "" {
		f.t.Fatal("invitation was not bound to exact owned origin")
	}
	client := f.setupClient()
	anonymous := f.setupRequest(ctx, client, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	anonCSRF := httpString(f.t, anonymous, "csrf_token")
	f.setupRequest(ctx, client, "POST", "/api/v1/invitations/inspect", map[string]string{"token": u.Fragment}, anonCSRF, false, 200)
	password := "Invitation private " + id[struct{}](f.t).String() + "!"
	f.setupRequest(ctx, client, "POST", "/api/v1/invitations/redeem", map[string]string{"token": u.Fragment, "username": username, "display_name": "Owned member", "password": password, "confirmation": password}, anonCSRF, true, 201)
	f.setupRequest(ctx, client, "POST", "/api/v1/sessions/login", map[string]string{"email": email, "password": password}, anonCSRF, true, 200)
	session := f.setupRequest(ctx, client, "GET", "/api/v1/session", nil, "", false, 200)
	user := httpObject(f.t, session, "user")
	if user["role"] != "user" || user["email"] != email || f.log.contains(u.Fragment) || f.log.contains(password) {
		f.t.Fatal("formal invitation role or restricted material invariant failed")
	}
	return personalWebCredential{Email: email, Password: password, UserID: httpString(f.t, user, "id")}
}

func (f *personalWebFixture) writeImages() {
	f.t.Helper()
	raw := avatarPNG(f.t, 29)
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		f.t.Fatal("owned image preparation failed")
	}
	var jpg bytes.Buffer
	if jpeg.Encode(&jpg, decoded, &jpeg.Options{Quality: 88}) != nil {
		f.t.Fatal("owned JPEG preparation failed")
	}
	webp, err := os.ReadFile("../../internal/central/account/testdata/b04-avatar/lossless.webp")
	if err != nil {
		f.t.Fatal("accepted static WebP fixture unavailable")
	}
	// A legal PNG chunk with acTL is a real animated-container rejection input.
	chunk := make([]byte, 20)
	binary.BigEndian.PutUint32(chunk[:4], 8)
	copy(chunk[4:8], "acTL")
	binary.BigEndian.PutUint32(chunk[8:12], 1)
	binary.BigEndian.PutUint32(chunk[16:20], crc32.ChecksumIEEE(chunk[4:16]))
	animated := append(append(append([]byte{}, raw[:33]...), chunk...), raw[33:]...)
	for name, value := range map[string][]byte{"static.png": raw, "static.jpg": jpg.Bytes(), "static.webp": webp, "animated.png": animated} {
		if os.WriteFile(filepath.Join(f.directory, name), value, 0600) != nil {
			f.t.Fatal("owned image transfer failed")
		}
	}
}

type personalWebResult struct {
	UserID        string   `json:"user_id"`
	MemberID      string   `json:"member_id"`
	SessionID     string   `json:"session_id"`
	OldSessionIDs []string `json:"old_session_ids"`
	Version       string   `json:"version"`
	Writes        int      `json:"writes"`
	Uploads       int      `json:"uploads"`
	Rejected      int      `json:"rejected"`
	Layouts       int      `json:"layouts"`
	Completed     bool     `json:"completed"`
}

type personalWebObservation struct {
	Version  string `json:"version"`
	Theme    string `json:"theme"`
	Writes   int    `json:"writes"`
	Audits   int    `json:"audits"`
	AvatarID string `json:"avatar_id"`
}

func (f *personalWebFixture) browserSettings(ctx context.Context, name string) personalWebResult {
	f.t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	if err != nil {
		f.t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "personal-settings.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TMPDIR=") && !strings.HasPrefix(value, "AGENTEAM_AUTH_WEB_") && !strings.HasPrefix(value, "AGENTEAM_PERSONAL_WEB_") && !strings.HasPrefix(value, "PLAYWRIGHT_NO_COPY_PROMPT=") && !strings.HasPrefix(value, "DEBUG=") && !strings.HasPrefix(value, "PWDEBUG=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_PERSONAL_WEB_CASE="+name, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "PLAYWRIGHT_NO_COPY_PROMPT=1")
	if images := os.Getenv("AGENTEAM_AUTH_WEB_IMAGES"); images != "" {
		if !filepath.IsAbs(images) {
			f.t.Fatal("owned image evidence path must be absolute")
		}
		cmd.Env = append(cmd.Env, "AGENTEAM_AUTH_WEB_IMAGES="+images)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		f.t.Fatal("locked settings browser runner could not start")
	}
	defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Env = nil }()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	conn := f.db.Connect(f.t)
	sequence := 0
	var runErr error
wait:
	for {
		select {
		case runErr = <-done:
			break wait
		case <-ticker.C:
			raw, e := os.ReadFile(filepath.Join(f.directory, "observe.json"))
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			var request struct {
				Sequence int    `json:"sequence"`
				UserID   string `json:"user_id"`
			}
			if e != nil || json.Unmarshal(raw, &request) != nil {
				f.t.Error("private read-only observation request invalid")
				_ = cmd.Cancel()
				continue
			}
			clear(raw)
			if request.Sequence == sequence {
				continue
			}
			if request.Sequence != sequence+1 || request.Sequence > 32 || request.UserID != f.admin.UserID && request.UserID != f.member.UserID {
				f.t.Error("observation did not identify an exact owned user and bounded sequence")
				_ = cmd.Cancel()
				continue
			}
			var value personalWebObservation
			e = conn.QueryRow(ctx, `SELECT version::text,theme,coalesce(avatar_object_id::text,''),(SELECT count(*) FROM agenteam_account.commands WHERE user_id=u.id AND command_name IN ('profile-update','avatar-update','password-change') AND phase='committed'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='account' AND resource_id=u.id AND action IN ('account.profile.update','account.avatar.update','account.password.change') AND outcome='success') FROM agenteam_account.users u WHERE id=$1`, request.UserID).Scan(&value.Version, &value.Theme, &value.AvatarID, &value.Writes, &value.Audits)
			if e != nil {
				f.t.Error("exact owned observation query failed", e)
				_ = cmd.Cancel()
				continue
			}
			encoded, e := json.Marshal(value)
			ack := filepath.Join(f.directory, "observation-"+strconv.Itoa(request.Sequence)+".json")
			if e != nil || os.WriteFile(ack+".tmp", encoded, 0600) != nil || os.Rename(ack+".tmp", ack) != nil {
				f.t.Error("private read-only observation acknowledgement failed")
				_ = cmd.Cancel()
				continue
			}
			sequence = request.Sequence
			f.t.Logf("owned observation %d user=%s version=%s theme=%s committed=%d audit=%d avatar=%t", sequence, request.UserID, value.Version, value.Theme, value.Writes, value.Audits, value.AvatarID != "")
		}
	}
	safe := output.String()
	for _, value := range []string{f.admin.Password, f.member.Password, f.newPassword} {
		safe = strings.ReplaceAll(safe, value, "[redacted]")
	}
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual settings production browser failed: %v", runErr)
	}
	raw, err := os.ReadFile(filepath.Join(f.directory, "personal-result.json"))
	if err != nil {
		f.t.Fatal("safe settings result missing")
	}
	defer clear(raw)
	var result personalWebResult
	if json.Unmarshal(raw, &result) != nil || !result.Completed || result.UserID != f.admin.UserID {
		f.t.Fatal("safe settings result invalid")
	}
	return result
}
