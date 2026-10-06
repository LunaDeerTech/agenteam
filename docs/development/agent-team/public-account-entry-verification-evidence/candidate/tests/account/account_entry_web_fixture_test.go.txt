//go:build integration

package account_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type entryWebCredential struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	UserID   string `json:"user_id"`
}
type entryWebFixture struct {
	*authenticationWebFixture
	admin, member                              entryWebCredential
	invitationID, invitationEmail, newPassword string
	secrets                                    []string
}

func newEntryWebFixture(t *testing.T, ctx context.Context) *entryWebFixture {
	t.Helper()
	f := &entryWebFixture{authenticationWebFixture: newAuthenticationWebFixture(t, ctx)}
	f.admin = entryWebCredential{Email: f.entry.Email, Password: f.entry.Password, UserID: f.entry.ID}
	f.newPassword = " 新密码 owned reset " + id[struct{}](t).String() + " "
	f.secrets = append(f.secrets, f.admin.Password, f.newPassword)
	admin := f.entryClient()
	anonymous := f.entryHTTP(ctx, admin, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	f.entryHTTP(ctx, admin, "POST", "/api/v1/sessions/login", map[string]string{"email": f.admin.Email, "password": f.admin.Password}, httpString(t, anonymous, "csrf_token"), true, 200)
	session := f.entryHTTP(ctx, admin, "GET", "/api/v1/session", nil, "", false, 200)
	csrf := httpString(t, session, "csrf_token")
	create := func(email string) (string, string, string) {
		created := f.entryHTTP(ctx, admin, "POST", "/api/v1/system/invitations", map[string]string{"email": email}, csrf, true, 201)
		invitationID := httpString(t, created, "id")
		record := f.record("invitation", email, invitationID)
		f.link(record.URL, "/invite")
		return invitationID, record.URL, httpString(t, created, "version")
	}
	memberEmail := "public-entry-member@example.com"
	_, memberLink, _ := create(memberEmail)
	memberPassword := " Member private " + id[struct{}](t).String() + " "
	f.secrets = append(f.secrets, memberPassword)
	memberClient := f.entryClient()
	anonymous = f.entryHTTP(ctx, memberClient, "GET", "/api/v1/auth/bootstrap", nil, "", false, 200)
	anonymousCSRF := httpString(t, anonymous, "csrf_token")
	link, _ := url.Parse(memberLink)
	f.entryHTTP(ctx, memberClient, "POST", "/api/v1/invitations/inspect", map[string]string{"token": link.Fragment}, anonymousCSRF, false, 200)
	f.entryHTTP(ctx, memberClient, "POST", "/api/v1/invitations/redeem", map[string]string{"token": link.Fragment, "username": "entry-existing", "password": memberPassword, "confirmation": memberPassword}, anonymousCSRF, true, 201)
	f.entryHTTP(ctx, memberClient, "POST", "/api/v1/sessions/login", map[string]string{"email": memberEmail, "password": memberPassword}, anonymousCSRF, true, 200)
	memberSession := f.entryHTTP(ctx, memberClient, "GET", "/api/v1/session", nil, "", false, 200)
	f.member = entryWebCredential{Email: memberEmail, Password: memberPassword, UserID: httpString(t, httpObject(t, memberSession, "user"), "id")}
	f.invitationEmail = "public-entry-long-email-address-for-responsive-layout@example.com"
	var invitationLink string
	f.invitationID, invitationLink, _ = create(f.invitationEmail)
	revokedID, revokedLink, revokedVersion := create("public-entry-revoked@example.com")
	f.entryHTTP(ctx, admin, "POST", "/api/v1/system/invitations/"+revokedID+"/revoke", map[string]string{"version": revokedVersion}, csrf, true, 204)
	f.writePrivate("entry-material.json", map[string]any{"admin": f.admin, "member": f.member, "invitation_email": f.invitationEmail, "invitation_link": invitationLink, "consumed_link": memberLink, "revoked_link": revokedLink, "new_password": f.newPassword})
	t.Cleanup(func() {
		for _, secret := range f.secrets {
			if f.log.contains(secret) {
				t.Error("public entry material escaped private transfer")
			}
		}
		f.secrets = nil
		f.admin.Password = ""
		f.member.Password = ""
		f.newPassword = ""
	})
	return f
}

func (f *entryWebFixture) link(value, path string) {
	f.t.Helper()
	u, e := url.Parse(value)
	if e != nil || u.Scheme+"://"+u.Host != f.origin || u.Path != path || len(u.Fragment) != 80 || u.RawQuery != "" {
		f.t.Fatal("entry link not bound to exact owned origin and purpose")
	}
	f.secrets = append(f.secrets, value, u.Fragment)
}
func (f *entryWebFixture) writePrivate(name string, value any) {
	f.t.Helper()
	raw, e := json.Marshal(value)
	if e != nil {
		f.t.Fatal("private entry encoding failed")
	}
	defer clear(raw)
	temporary := filepath.Join(f.directory, name+".tmp")
	if os.WriteFile(temporary, raw, 0600) != nil || os.Rename(temporary, filepath.Join(f.directory, name)) != nil {
		f.t.Fatal("private entry transfer failed")
	}
}
func (f *entryWebFixture) entryClient() *http.Client {
	f.t.Helper()
	jar, e := cookiejar.New(nil)
	if e != nil {
		f.t.Fatal("private Cookie jar unavailable")
	}
	transport := &http.Transport{Proxy: nil}
	f.t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Jar: jar, Transport: transport, Timeout: 20 * time.Second}
}
func (f *entryWebFixture) entryHTTP(ctx context.Context, client *http.Client, method, path string, body any, csrf string, keyed bool, status int) map[string]any {
	f.t.Helper()
	raw, e := json.Marshal(body)
	if e != nil {
		f.t.Fatal("private entry request encoding failed")
	}
	defer clear(raw)
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(raw)
	}
	r, e := http.NewRequestWithContext(ctx, method, f.origin+path, reader)
	if e != nil {
		f.t.Fatal("private entry request invalid")
	}
	r.Header.Set("Origin", f.origin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if keyed {
		r.Header.Set("Idempotency-Key", id[struct{}](f.t).String())
	}
	response, e := client.Do(r)
	if e != nil {
		f.t.Fatal("formal entry prerequisite HTTP failed")
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		f.t.Fatalf("formal entry prerequisite %s %s status=%d want=%d", method, path, response.StatusCode, status)
	}
	if status == 204 {
		if body, e := io.ReadAll(io.LimitReader(response.Body, 1)); e != nil || len(body) != 0 {
			f.t.Fatal("invalid entry 204")
		}
		return nil
	}
	var value map[string]any
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value) != nil {
		f.t.Fatal("formal entry prerequisite response invalid")
	}
	return value
}

type entryWebResult struct {
	Completed         bool     `json:"completed"`
	UserID            string   `json:"user_id"`
	SessionID         string   `json:"session_id"`
	OldSessionIDs     []string `json:"old_session_ids"`
	IdentityPreserved bool     `json:"identity_preserved"`
	GenericResponse   bool     `json:"generic_response"`
	Layouts           int      `json:"layouts"`
}

var entryCommandKey = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// Only explicit safe scalars leave this fixture. The key, addresses, IDs,
// Secret references, hashes and event/recovery payloads are never logged.
func (f *entryWebFixture) resetFacts(ctx context.Context, db *pgx.Conn, key, stage string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var count int
	var commandID string
	var member bool
	var raw []byte
	e := db.QueryRow(ctx, `WITH owned AS (
 SELECT c.id,c.phase,c.user_id,c.resource_id,c.password_version,c.attempt_id,r.phase AS request_phase,r.pass
 FROM agenteam_account.commands c JOIN agenteam_account.reset_requests r ON r.command_id=c.id
 WHERE c.namespace='account.reset' AND c.command_name='reset-request' AND c.command_key=$3
 AND r.canonical_email=$2 AND r.browser_id=c.browser_id
), intents AS (
 SELECT i.* FROM agenteam_account.delivery_intents i JOIN owned o ON i.id=o.id
 WHERE i.kind='password_reset' AND i.link_id=o.resource_id
), jobs AS (
 SELECT j.* FROM agenteam_account.mail_jobs j JOIN intents i ON j.id=i.job_id AND j.intent_id=i.id
)
SELECT (SELECT count(*)::int FROM owned),coalesce((SELECT min(id::text) FROM owned),''),
 EXISTS(SELECT 1 FROM agenteam_account.users WHERE id=$1::uuid AND email=$2 AND password_version>0),
 jsonb_build_object(
 'member_exists',EXISTS(SELECT 1 FROM agenteam_account.users WHERE id=$1::uuid AND email=$2),
 'member_password_version_valid',EXISTS(SELECT 1 FROM agenteam_account.users WHERE id=$1::uuid AND email=$2 AND password_version>0),
 'requests',(SELECT count(*) FROM owned),
 'accepted',(SELECT count(*) FROM owned WHERE request_phase='accepted'),
 'processed',(SELECT count(*) FROM owned WHERE request_phase='processed'),
 'recovery_pass_max',(SELECT coalesce(max(pass),0) FROM owned),
 'committed_commands',(SELECT count(*) FROM owned WHERE phase='committed'),
 'command_user_bound',(SELECT count(*) FROM owned WHERE user_id=$1::uuid),
 'reset_rows',(SELECT count(*) FROM agenteam_account.password_resets WHERE user_id=$1::uuid),
 'recent_existing_resets',(SELECT count(*) FROM agenteam_account.password_resets WHERE user_id=$1::uuid AND last_delivery_at>clock_timestamp()-interval '60 seconds'),
 'current_password_and_resource_bound',(SELECT count(*) FROM owned o JOIN agenteam_account.password_resets p ON p.id=o.resource_id AND p.user_id=o.user_id JOIN agenteam_account.users u ON u.id=p.user_id WHERE u.id=$1::uuid AND p.password_version=u.password_version AND o.password_version=u.password_version AND p.expires_at>clock_timestamp()),
 'auth_attempts',(SELECT count(*) FROM agenteam_account.auth_attempts a JOIN owned o ON a.id=o.attempt_id AND a.command_id=o.id WHERE a.kind='reset_request' AND a.phase='completed' AND a.outcome='success'),
 'audit_attempts',(SELECT count(*) FROM agenteam_audit.audit_records a JOIN owned o ON a.metadata->>'attempt_id'=o.attempt_id::text WHERE a.producer='account' AND a.action='account.password.reset.request' AND a.outcome='success'),
 'delivery_intents',(SELECT count(*) FROM intents),
 'outbox_events',(SELECT count(*) FROM agenteam_outbox.events e JOIN owned o ON e.id::text=o.id::text WHERE e.producer='account'),
 'outbox_phases',coalesce((SELECT jsonb_object_agg(phase,n) FROM (SELECT d.phase,count(*) n FROM agenteam_outbox.deliveries d JOIN owned o ON d.event_id::text=o.id::text WHERE d.handler_id='account.mail-enqueue' GROUP BY d.phase) phases),'{}'::jsonb),
 'mail_phases',coalesce((SELECT jsonb_object_agg(phase,n) FROM (SELECT phase,count(*) n FROM jobs GROUP BY phase) phases),'{}'::jsonb),
 'log_attempts_joined',(SELECT count(*) FROM agenteam_account.mail_attempts a JOIN jobs j ON j.id=a.job_id WHERE a.channel='log' AND a.result='sent' AND a.terminal AND a.io_joined)
 )`, f.member.UserID, f.member.Email, key).Scan(&count, &commandID, &member, &raw)
	defer clear(raw)
	if e != nil {
		var sql interface{ SQLState() string }
		code := "none"
		if errors.As(e, &sql) {
			code = sql.SQLState()
		}
		f.t.Errorf("entry reset safe facts stage=%s query_failed SQLSTATE=%s", stage, code)
		return "", false
	}
	f.t.Logf("entry reset safe facts stage=%s %s", stage, raw)
	if count != 1 || !member {
		f.t.Error("entry reset request did not bind one exact eligible member command")
		return "", false
	}
	return commandID, true
}

func (f *entryWebFixture) resetRecord(ctx context.Context, resetID string) (httpRecoveryRecord, bool, error) {
	if e := ctx.Err(); e != nil {
		return httpRecoveryRecord{}, false, e
	}
	file, e := os.OpenFile(f.recoveryLogPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return httpRecoveryRecord{}, false, errors.New("owned recovery record open failed")
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		return httpRecoveryRecord{}, false, errors.New("owned recovery record stat failed")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return httpRecoveryRecord{}, false, errors.New("owned recovery record ownership invalid")
	}
	raw, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	defer clear(raw)
	if e != nil || len(raw) > 1<<20 {
		return httpRecoveryRecord{}, false, errors.New("owned recovery record read failed")
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		var record httpRecoveryRecord
		if json.Unmarshal(scanner.Bytes(), &record) == nil && record.Purpose == "password_reset" && record.Email == f.member.Email && record.ID == resetID {
			if e = ctx.Err(); e != nil {
				return httpRecoveryRecord{}, false, e
			}
			return record, true, nil
		}
	}
	if scanner.Err() != nil {
		return httpRecoveryRecord{}, false, errors.New("owned recovery record scan failed")
	}
	return httpRecoveryRecord{}, false, ctx.Err()
}

func (f *entryWebFixture) browserEntry(ctx context.Context, name string) entryWebResult {
	f.t.Helper()
	root, e := filepath.Abs("../account-captcha-web")
	if e != nil {
		f.t.Fatal(e)
	}
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "account-entry.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TMPDIR=") && !strings.HasPrefix(value, "AGENTEAM_AUTH_WEB_") && !strings.HasPrefix(value, "AGENTEAM_ENTRY_WEB_") && !strings.HasPrefix(value, "PLAYWRIGHT_NO_COPY_PROMPT=") && !strings.HasPrefix(value, "DEBUG=") && !strings.HasPrefix(value, "PWDEBUG=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+f.directory, "AGENTEAM_AUTH_WEB_ORIGIN="+f.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+f.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_ENTRY_WEB_CASE="+name, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if e = cmd.Start(); e != nil {
		f.t.Fatal("entry browser runner could not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if !joined {
			// Fatal exits from the private IPC helpers must also join the real
			// browser before the fixture's directories and servers are cleaned.
			<-done
		}
		cmd.Env = nil
	}()
	resetDB := f.db.Connect(f.t)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	sequence := 0
	var handshake context.Context
	var stopHandshake context.CancelFunc
	var deadlineMS int64
	var commandKey, commandID string
	var formerBoundReported, resetVisible bool
	var runErr error
loop:
	for {
		select {
		case runErr = <-done:
			joined = true
			break loop
		case <-ticker.C:
			raw, e := os.ReadFile(filepath.Join(f.directory, "entry-ipc.json"))
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				f.t.Error("entry IPC read failed")
				_ = cmd.Cancel()
				continue
			}
			var request struct {
				Sequence   int    `json:"sequence"`
				Email      string `json:"email"`
				Kind       string `json:"kind"`
				CommandKey string `json:"command_key"`
				DeadlineMS int64  `json:"deadline_ms"`
			}
			e = json.Unmarshal(raw, &request)
			clear(raw)
			if e != nil || request.Sequence < 1 || request.Sequence > 2 || request.Email != f.member.Email || request.Kind != "reset-link" || !entryCommandKey.MatchString(request.CommandKey) {
				f.t.Error("entry IPC ownership rejected")
				_ = cmd.Cancel()
				continue
			}
			if request.Sequence <= sequence {
				continue
			}
			if request.Sequence != sequence+1 {
				f.t.Error("entry IPC sequence rejected")
				_ = cmd.Cancel()
				continue
			}
			if handshake == nil {
				deadline := time.UnixMilli(request.DeadlineMS)
				now := time.Now()
				if !deadline.After(now) || deadline.After(now.Add(20*time.Second)) {
					f.t.Error("entry IPC deadline rejected")
					_ = cmd.Cancel()
					continue
				}
				deadlineMS, commandKey = request.DeadlineMS, request.CommandKey
				f.secrets = append(f.secrets, commandKey)
				handshake, stopHandshake = context.WithDeadline(ctx, deadline)
				defer stopHandshake()
				var valid bool
				commandID, valid = f.resetFacts(handshake, resetDB, commandKey, "request")
				if !valid {
					_ = cmd.Cancel()
					runErr = <-done
					joined = true
					break loop
				}
			} else if deadlineMS != request.DeadlineMS || commandKey != request.CommandKey {
				f.t.Error("entry IPC original deadline or command changed")
				_ = cmd.Cancel()
				continue
			}
			if handshake.Err() != nil {
				// The preceding safe snapshots remain the last observed facts.
				// Do not start any new query after the original deadline.
				f.t.Error("entry recovery handshake exceeded its original 20s deadline")
				_ = cmd.Cancel()
				runErr = <-done
				joined = true
				break loop
			}
			var resetID string
			if e = resetDB.QueryRow(handshake, `SELECT p.id::text FROM agenteam_account.password_resets p JOIN agenteam_account.users u ON u.id=p.user_id JOIN agenteam_account.commands c ON c.resource_id=p.id AND c.user_id=p.user_id JOIN agenteam_account.reset_requests r ON r.command_id=c.id AND r.browser_id=c.browser_id WHERE c.id=$2::uuid AND c.phase='committed' AND r.phase='processed' AND p.user_id=$1::uuid AND p.password_version=u.password_version AND c.password_version=u.password_version AND p.expires_at>clock_timestamp()`, f.member.UserID, commandID).Scan(&resetID); e != nil {
				// Formal 202 commits a reset request; Account recovery's 10s
				// tick creates the reset and Outbox event. Mail then delivers it.
				if errors.Is(e, pgx.ErrNoRows) {
					if !formerBoundReported && time.Now().After(time.UnixMilli(deadlineMS).Add(-16*time.Second)) {
						_, valid := f.resetFacts(handshake, resetDB, commandKey, "former-4s-bound")
						formerBoundReported = true
						if !valid {
							_ = cmd.Cancel()
							runErr = <-done
							joined = true
							break loop
						}
					}
					continue
				}
				if handshake.Err() == nil {
					_, _ = f.resetFacts(handshake, resetDB, commandKey, "reset-query-failed")
				}
				f.t.Error("entry reset poststate query failed")
				_ = cmd.Cancel()
				runErr = <-done
				joined = true
				break loop
			}
			if !resetVisible {
				_, valid := f.resetFacts(handshake, resetDB, commandKey, "reset-visible")
				if !valid {
					_ = cmd.Cancel()
					runErr = <-done
					joined = true
					break loop
				}
				resetVisible = true
			}
			record, found, e := f.resetRecord(handshake, resetID)
			if e != nil {
				if handshake.Err() == nil {
					_, _ = f.resetFacts(handshake, resetDB, commandKey, "record-read-failed")
				}
				f.t.Error("exact owned recovery record read failed")
				_ = cmd.Cancel()
				runErr = <-done
				joined = true
				break loop
			}
			if !found {
				continue
			}
			_, valid := f.resetFacts(handshake, resetDB, commandKey, "link-visible")
			if !valid || handshake.Err() != nil {
				f.t.Error("entry recovery acknowledgement missed original deadline")
				_ = cmd.Cancel()
				runErr = <-done
				joined = true
				break loop
			}
			f.link(record.URL, "/reset-password")
			sequence = request.Sequence
			f.writePrivate("entry-link-"+string(rune('0'+sequence))+".json", map[string]string{"url": record.URL})
			stopHandshake()
			handshake, stopHandshake = nil, nil
			commandKey, commandID = "", ""
			formerBoundReported, resetVisible = false, false
		}
	}
	safe := output.String()
	for _, secret := range f.secrets {
		safe = strings.ReplaceAll(safe, secret, "[redacted]")
	}
	f.t.Log(safe)
	if runErr != nil {
		f.t.Fatalf("actual public entry browser failed: %v", runErr)
	}
	raw, e := os.ReadFile(filepath.Join(f.directory, "entry-result.json"))
	if e != nil {
		f.t.Fatal("safe entry browser result missing")
	}
	defer clear(raw)
	var result entryWebResult
	if json.Unmarshal(raw, &result) != nil || !result.Completed {
		f.t.Fatal("safe entry browser result invalid")
	}
	return result
}
