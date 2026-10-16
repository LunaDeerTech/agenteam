//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/accountenv"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	smtpfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/smtp"
	"github.com/jackc/pgx/v5"
)

type recoveryRuntime struct {
	Origin          string    `json:"origin"`
	LocalAddress    string    `json:"local_tls_address"`
	CAFile          string    `json:"certificate_ca_file"`
	SPKI            string    `json:"certificate_spki"`
	Nonce           string    `json:"nonce"`
	Scope           string    `json:"test_scope"`
	Expires         time.Time `json:"expires_at"`
	DatabaseFixture string    `json:"database_fixture"`
	DatabaseName    string    `json:"database_name"`
	SMTPControl     string    `json:"smtp_control"`
	SMTPNonce       string    `json:"smtp_nonce"`
	SMTPEndpoint    string    `json:"smtp_endpoint_id"`
	Credentials     struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	} `json:"credentials"`
	RegularCredentials struct {
		Email string `json:"email"`
	} `json:"regular_credentials"`
}

// TestOwnedRuntimeEmailInitialization deliberately reuses an already running,
// explicitly owned local preview. It neither builds App nor allocates fixtures.
// The CLI receives fresh configuration-only keyrings: issuance uses random
// authorization material and its SHA verifier, not the App's runtime MAC keys.
func recoveryOwnedRuntime(t *testing.T) (context.Context, recoveryRuntime, *pgfixture.Descriptor, *pgx.Conn, *http.Client) {
	t.Helper()
	if os.Getenv("AGENTEAM_ACCOUNT_RECOVERY_OWNED") != "1" {
		t.Skip("explicit owned recovery runtime opt-in required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	runtimePath := os.Getenv("AGENTEAM_ACCOUNT_RECOVERY_RUNTIME")
	private := filepath.Dir(runtimePath)
	st, err := os.Lstat(private)
	actual, pathErr := filepath.EvalSymlinks(private)
	if !filepath.IsAbs(runtimePath) || filepath.Base(runtimePath) != "runtime.json" || err != nil || pathErr != nil || actual != private || !st.IsDir() || st.Mode().Perm() != 0700 {
		t.Fatal("recovery requires an owned private preview directory")
	}
	var runtime recoveryRuntime
	recoveryReadJSON(t, runtimePath, &runtime)
	var sidecar struct {
		Scope   string `json:"test_scope"`
		Runtime string `json:"runtime_file"`
		Nonce   string `json:"nonce"`
	}
	recoveryReadJSON(t, filepath.Join(private, "passkey-owned.json"), &sidecar)
	if runtime.Scope != "account-passkeys-email-change" || sidecar.Scope != runtime.Scope || sidecar.Runtime != runtimePath || sidecar.Nonce != runtime.Nonce || !recoveryNonce(runtime.Nonce) || runtime.Origin != "https://localhost:5270" || runtime.LocalAddress != "127.0.0.1:5270" || !runtime.Expires.After(time.Now().Add(3*time.Minute)) || runtime.Credentials.Email == "" || runtime.Credentials.Password == "" {
		t.Fatal("recovery refused a nonlocal, expired or mismatched preview")
	}
	if runtime.DatabaseFixture == "" || os.Getenv(pgfixture.Env) != runtime.DatabaseFixture {
		t.Fatal("recovery database descriptor must explicitly match the preview")
	}
	fixture, err := pgfixture.Load()
	recoveryCheck(t, "owned PostgreSQL descriptor", err)
	if !strings.HasPrefix(runtime.DatabaseName, "d03_"+fixture.Nonce[:12]+"_") {
		t.Fatal("recovery refused an unowned database")
	}
	_, err = fixture.Config(runtime.DatabaseName, nil)
	recoveryCheck(t, "owned PostgreSQL configuration", err)
	connection, err := fixture.Connect(ctx, runtime.DatabaseName)
	recoveryCheck(t, "owned PostgreSQL connection", err)
	t.Cleanup(func() {
		closeContext, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		recoveryCheck(t, "owned PostgreSQL close", connection.Close(closeContext))
	})
	client := recoveryHTTPClient(t, runtime, private)
	var identity struct {
		Nonce string `json:"nonce"`
	}
	recoveryHTTP(t, ctx, client, runtime, "GET", "/__fixture__/identity", "", nil, http.StatusOK, &identity)
	if identity.Nonce != runtime.Nonce {
		t.Fatal("recovery HTTPS fixture identity mismatch")
	}
	recoveryVerifySMTP(t, ctx, runtime)
	return ctx, runtime, fixture, connection, client
}

func TestOwnedRuntimeEmailInitialization(t *testing.T) {
	ctx, runtime, fixture, connection, client := recoveryOwnedRuntime(t)
	private := filepath.Dir(os.Getenv("AGENTEAM_ACCOUNT_RECOVERY_RUNTIME"))
	recoveryEmailChangePostcondition(t, ctx, connection, runtime.RegularCredentials.Email)
	var user string
	var unverified bool
	recoveryCheck(t, "bootstrap initialization owner", connection.QueryRow(ctx, `SELECT u.id::text,u.email_verified_at IS NULL FROM agenteam_account.bootstrap b JOIN agenteam_account.users u ON u.id=b.user_id WHERE b.singleton AND u.email=$1`, runtime.Credentials.Email).Scan(&user, &unverified))
	if !unverified {
		t.Fatal("recovery initialization requires the untouched bootstrap account")
	}
	current := recoveryLogin(t, ctx, client, runtime)
	otherClient := recoveryHTTPClient(t, runtime, private)
	other := recoveryLogin(t, ctx, otherClient, runtime)
	if current.User.ID != user || other.User.ID != user || current.Session.ID == other.Session.ID {
		t.Fatal("recovery requires two distinct sessions for its bootstrap owner")
	}
	var before struct {
		Verified bool `json:"email_verified"`
		Required bool `json:"email_initialization_required"`
	}
	recoveryHTTP(t, ctx, client, runtime, "GET", "/api/v1/me/passkeys", "", nil, http.StatusOK, &before)
	if before.Verified || !before.Required {
		t.Fatal("bootstrap did not require email initialization")
	}
	// The earlier replay test may have sent a code near a budget boundary. Wait
	// for its actual persisted cooldown without editing the rate-limit facts.
	var waitSeconds float64
	recoveryCheck(t, "initialization delivery cooldown", connection.QueryRow(ctx, `SELECT coalesce((SELECT greatest(0,extract(epoch FROM last_delivery_at+interval '60 seconds'-clock_timestamp())) FROM agenteam_account.email_rate_limits WHERE user_id=$1 AND kind='initialization'),0)::float8`, user).Scan(&waitSeconds))
	if waitSeconds > 60 {
		t.Fatal("initialization cooldown exceeds the expected fixture bound")
	}
	if waitSeconds > 0 {
		timer := time.NewTimer(time.Duration(waitSeconds*float64(time.Second)) + 10*time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			t.Fatal("initialization cooldown deadline")
		case <-timer.C:
		}
	}
	target := "cli-initialized-" + runtime.Nonce[:12] + "@example.test"
	output := filepath.Join(t.TempDir(), "authorization.json")
	binary := os.Getenv("AGENTEAM_ACCOUNT_RECOVERY_BINARY")
	binaryInfo, err := os.Lstat(binary)
	if !filepath.IsAbs(binary) || err != nil || !binaryInfo.Mode().IsRegular() || binaryInfo.Mode().Perm()&0111 == 0 || binaryInfo.Mode().Perm()&0022 != 0 {
		t.Fatal("recovery requires an explicit owned CLI binary")
	}
	process := exec.CommandContext(ctx, binary, "--user-id", user, "--purpose", "initialize_email", "--email", target, "--output", output)
	process.Env = recoveryCLIEnvironment(t, fixture, runtime)
	process.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	defer func() { clear(stdout.Bytes()); clear(stderr.Bytes()) }()
	recoveryCheck(t, "CLI process start", process.Start())
	pid := process.Process.Pid
	err = process.Wait()
	if err != nil || !process.ProcessState.Success() || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("recovery CLI process did not exit cleanly with empty output (pid=%d)", pid)
	}
	var issued struct {
		ID            string    `json:"id"`
		Authorization string    `json:"authorization"`
		UserID        string    `json:"user_id"`
		Purpose       string    `json:"purpose"`
		Email         string    `json:"email"`
		ExpiresAt     time.Time `json:"expires_at"`
	}
	recoveryReadJSON(t, output, &issued)
	if issued.UserID != user || issued.Purpose != "initialize_email" || issued.Email != target || issued.Authorization == "" || !issued.ExpiresAt.After(time.Now().Add(29*time.Minute)) {
		t.Fatal("CLI authorization file did not bind the expected initialization")
	}
	var authorized bool
	recoveryCheck(t, "CLI authorization and atomic audit", connection.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.operator_authorizations o JOIN agenteam_audit.audit_records a ON a.cause_ref=o.id::text WHERE o.id=$1 AND o.user_id=$2 AND o.purpose='initialize_email' AND o.target_email=$3 AND o.consumed_request_id IS NULL AND o.revoked_at IS NULL AND o.expires_at=o.created_at+interval '30 minutes' AND a.service_name='account-maintenance' AND a.action='account.email.authorize' AND a.resource_id=o.user_id AND a.outcome='success')`, issued.ID, user, target).Scan(&authorized))
	if !authorized {
		t.Fatal("CLI authorization or same-transaction audit missing")
	}
	var receipt struct {
		RequestID string `json:"request_id"`
	}
	recoveryHTTP(t, ctx, client, runtime, "POST", "/api/v1/me/email-initialization/request", current.CSRF, map[string]string{"authorization": issued.Authorization, "password": runtime.Credentials.Password}, http.StatusAccepted, &receipt)
	issued.Authorization = ""
	if receipt.RequestID == "" {
		t.Fatal("initialization request receipt missing")
	}
	var job string
	for {
		var joined bool
		recoveryCheck(t, "initialization SMTP completion", connection.QueryRow(ctx, `SELECT coalesce((SELECT j.id::text FROM agenteam_account.mail_jobs j JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id WHERE i.link_id=$1),''),EXISTS(SELECT 1 FROM agenteam_account.mail_jobs j JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id WHERE i.link_id=$1 AND j.phase='sent' AND a.channel='smtp' AND a.terminal AND a.io_joined)`, receipt.RequestID).Scan(&job, &joined))
		if joined {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("initialization SMTP completion deadline")
		case <-time.After(50 * time.Millisecond):
		}
	}
	code := recoverySMTPCode(t, ctx, runtime, target, job)
	// A rejected code commits only its attempt accounting. Replaying the same
	// command must neither spend another attempt nor publish a revocation event.
	type attemptState struct {
		Attempts, Failures, Events int
		Version, Sequence          int64
		Email                      string
		Verified, Current, Other   bool
	}
	attemptSnapshot := func() attemptState {
		t.Helper()
		var state attemptState
		recoveryCheck(t, "initialization attempt snapshot", connection.QueryRow(ctx, `SELECT r.attempts,b.failures,u.version,u.auth_sequence,u.email,u.email_verified_at IS NOT NULL,(SELECT count(*) FROM agenteam_outbox.events e WHERE e.aggregate_id=u.id AND e.event_type='account.sessions-revoked'),EXISTS(SELECT 1 FROM agenteam_account.sessions s WHERE s.id=$2 AND s.user_id=u.id AND s.revoked_at IS NULL),EXISTS(SELECT 1 FROM agenteam_account.sessions s WHERE s.id=$3 AND s.user_id=u.id AND s.revoked_at IS NULL) FROM agenteam_account.email_requests r JOIN agenteam_account.users u ON u.id=r.user_id JOIN agenteam_account.email_rate_limits b ON b.user_id=r.user_id AND b.kind=r.kind WHERE r.id=$1`, receipt.RequestID, current.Session.ID, other.Session.ID).Scan(&state.Attempts, &state.Failures, &state.Version, &state.Sequence, &state.Email, &state.Verified, &state.Events, &state.Current, &state.Other))
		return state
	}
	beforeAttempt := attemptSnapshot()
	if !beforeAttempt.Current || !beforeAttempt.Other || beforeAttempt.Verified || beforeAttempt.Email != runtime.Credentials.Email {
		t.Fatal("initialization attempt control state changed before confirmation")
	}
	wrongCode := "000000"
	if code == wrongCode {
		wrongCode = "111111"
	}
	wrongKey, err := foundation.NewID[struct{}]()
	recoveryCheck(t, "incorrect-code command key", err)
	wrongBody := map[string]string{"request_id": receipt.RequestID, "code": wrongCode}
	wantAttempt := beforeAttempt
	wantAttempt.Attempts++
	wantAttempt.Failures++
	for range 2 {
		var rejection json.RawMessage
		recoveryHTTP(t, ctx, client, runtime, "POST", "/api/v1/me/email-initialization/confirm", current.CSRF, wrongBody, http.StatusBadRequest, &rejection, wrongKey.String())
		validRejection := bytes.Contains(rejection, []byte("INVALID_OR_EXPIRED")) && bytes.Contains(rejection, []byte("/code"))
		clear(rejection)
		if !validRejection || attemptSnapshot() != wantAttempt {
			t.Fatal("incorrect code or its replay changed business state or charged the attempt budget incorrectly")
		}
	}
	var changed struct {
		Email   string             `json:"email"`
		Version foundation.Version `json:"version"`
	}
	recoveryHTTP(t, ctx, client, runtime, "POST", "/api/v1/me/email-initialization/confirm", current.CSRF, map[string]string{"request_id": receipt.RequestID, "code": code}, http.StatusOK, &changed)
	code = ""
	if changed.Email != target || changed.Version < 2 {
		t.Fatal("initialization confirmation returned the wrong mailbox receipt")
	}
	var after recoverySession
	recoveryHTTP(t, ctx, client, runtime, "GET", "/api/v1/session", "", nil, http.StatusOK, &after)
	if after.Session.ID != current.Session.ID || after.User.Email != target {
		t.Fatal("initialization did not retain its current session and update its mailbox")
	}
	recoveryHTTP(t, ctx, otherClient, runtime, "GET", "/api/v1/session", "", nil, http.StatusUnauthorized, nil)
	recoveryHTTP(t, ctx, client, runtime, "GET", "/api/v1/me/passkeys", "", nil, http.StatusOK, &before)
	if !before.Verified || before.Required {
		t.Fatal("initialization did not clear the bootstrap email requirement")
	}
	var committed bool
	recoveryCheck(t, "initialization durable completion", connection.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.email_requests r JOIN agenteam_account.operator_authorizations o ON o.id=r.authorization_id JOIN agenteam_account.users u ON u.id=r.user_id WHERE r.id=$1 AND r.phase='consumed' AND o.id=$2 AND o.consumed_request_id=r.id AND u.email=$3 AND u.email_verified_at IS NOT NULL AND EXISTS(SELECT 1 FROM agenteam_account.sessions s WHERE s.id=$4 AND s.user_id=u.id AND s.revoked_at IS NULL) AND NOT EXISTS(SELECT 1 FROM agenteam_account.sessions s WHERE s.user_id=u.id AND s.id<>$4 AND s.revoked_at IS NULL))`, receipt.RequestID, issued.ID, target, current.Session.ID).Scan(&committed))
	if !committed {
		t.Fatal("initialization durable mailbox, authorization or session facts disagree")
	}
	t.Logf("actual CLI pid=%d exit=0, empty stdout/stderr and private 0600 authorization; incorrect code plus same-key replay charged one attempt without a mailbox/session/event mutation; correct SMTP initialization consumed, current session retained and second session revoked; existing Passkeys/reset credentials not exercised", pid)
}

// This read-only continuation verifies the commit already acknowledged by the
// first run's HTTP 200. It performs no CLI issue, login, SMTP send or confirm.
// The first run failed while decoding its string-valued Version into an int64;
// its failed test exit remains separate from these durable postconditions.
func TestOwnedRuntimeEmailInitializationCommitted(t *testing.T) {
	ctx, runtime, _, connection, _ := recoveryOwnedRuntime(t)
	target := "cli-initialized-" + runtime.Nonce[:12] + "@example.test"
	var confirms, requests, authorizations, events, revokedOthers int
	var receiptMatches, requestConsumed, authorizationConsumed, currentRetained, noOtherActive, eventBound bool
	recoveryCheck(t, "initialization HTTP-200 commit postcondition", connection.QueryRow(ctx, `
WITH latest AS (
 SELECT c.* FROM agenteam_account.commands c
 JOIN agenteam_account.bootstrap b ON b.user_id=c.user_id
 JOIN agenteam_account.security_receipts r ON r.command_id=c.id
 WHERE b.singleton AND c.command_name='email-initialize-confirm' AND c.phase='committed' AND r.receipt ? 'email'
 ORDER BY c.created_at DESC LIMIT 1
)
SELECT count(*),count(DISTINCT r.id),count(DISTINCT o.id),count(DISTINCT e.id),
 coalesce(bool_and(jsonb_typeof(saved.receipt->'version')='string' AND saved.receipt->>'version'=u.version::text AND saved.receipt->>'email'=$1 AND saved.receipt ? 'created_at' AND u.email=$1 AND u.email_verified_at IS NOT NULL),false),
 coalesce(bool_and(r.kind='initialization' AND r.phase='consumed' AND r.target_email=$1 AND r.original_email=$2 AND r.session_id=c.session_id AND r.user_id=c.user_id),false),
 coalesce(bool_and(o.purpose='initialize_email' AND o.user_id=c.user_id AND o.target_email=$1 AND o.consumed_request_id=r.id),false),
 coalesce(bool_and(EXISTS(SELECT 1 FROM agenteam_account.sessions s WHERE s.id=c.session_id AND s.user_id=c.user_id AND s.revoked_at IS NULL)),false),
 coalesce(bool_and(NOT EXISTS(SELECT 1 FROM agenteam_account.sessions s WHERE s.user_id=c.user_id AND s.id<>c.session_id AND s.revoked_at IS NULL)),false),
 coalesce(bool_and(e.event_type='account.sessions-revoked' AND e.aggregate_id=c.user_id AND e.header=c.event_header AND e.payload=c.event_payload AND convert_from(e.payload,'UTF8')::jsonb->>'scope'='others' AND convert_from(e.payload,'UTF8')::jsonb->>'reason'='email_changed' AND convert_from(e.payload,'UTF8')::jsonb->>'user_id'=c.user_id::text AND convert_from(e.payload,'UTF8')::jsonb->>'session_id'=c.session_id::text),false),
 coalesce(max((SELECT count(*) FROM agenteam_account.sessions s WHERE s.user_id=c.user_id AND s.id<>c.session_id AND s.revoked_at IS NOT NULL AND s.revoked_reason='administrative')),0)
FROM latest c JOIN agenteam_account.users u ON u.id=c.user_id
LEFT JOIN agenteam_account.security_receipts saved ON saved.command_id=c.id
LEFT JOIN agenteam_account.email_requests r ON r.user_id=c.user_id AND r.session_id=c.session_id AND r.kind='initialization' AND r.phase='consumed' AND r.target_email=$1
LEFT JOIN agenteam_account.operator_authorizations o ON o.id=r.authorization_id
LEFT JOIN agenteam_outbox.events e ON e.id=c.event_id`, target, runtime.Credentials.Email).Scan(&confirms, &requests, &authorizations, &events, &receiptMatches, &requestConsumed, &authorizationConsumed, &currentRetained, &noOtherActive, &eventBound, &revokedOthers))
	if confirms != 1 || requests != 1 || authorizations != 1 || events != 1 || revokedOthers < 1 || !receiptMatches || !requestConsumed || !authorizationConsumed || !currentRetained || !noOtherActive || !eventBound {
		t.Fatalf("initialization committed postcondition: confirms=%d requests=%d authorizations=%d events=%d revoked_others=%d receipt=%t request=%t authorization=%t current_retained=%t no_other_active=%t event=%t", confirms, requests, authorizations, events, revokedOthers, receiptMatches, requestConsumed, authorizationConsumed, currentRetained, noOtherActive, eventBound)
	}
	t.Log("original HTTP-200 initialization is committed: string-version receipt, verified mailbox, consumed request/authorization and one retained-session event agree; current session remains active and all other sessions are revoked; original in-memory cookies are gone and owned fixture teardown will remove the database")
}

// The preceding browser journey has already logged out. Check the session
// named by its immutable confirmation event, not that session's current state.
func recoveryEmailChangePostcondition(t *testing.T, ctx context.Context, connection *pgx.Conn, oldEmail string) {
	t.Helper()
	if oldEmail == "" {
		t.Fatal("ordinary email-change source mailbox is absent from the owned manifest")
	}
	var confirms, notices, intents, jobs int
	var receiptMatches, eventBound, retainedSession, oldNotice, smtpComplete bool
	recoveryCheck(t, "ordinary email-change durable postcondition", connection.QueryRow(ctx, `
WITH latest AS (
 SELECT c.* FROM agenteam_account.commands c JOIN agenteam_account.users u ON u.id=c.user_id
 WHERE u.username='outbound-reader' AND c.command_name='email-change-confirm' AND c.phase='committed'
 ORDER BY c.created_at DESC LIMIT 1
)
SELECT count(*),
 coalesce(bool_and(r.receipt ? 'version' AND r.receipt ? 'email' AND r.receipt ? 'created_at' AND r.receipt->>'email'=u.email),false),
 coalesce(bool_and(e.event_type='account.sessions-revoked' AND e.aggregate_id=c.user_id AND e.header=c.event_header AND e.payload=c.event_payload),false),
 coalesce(bool_and(convert_from(e.payload,'UTF8')::jsonb->>'scope'='others' AND convert_from(e.payload,'UTF8')::jsonb->>'reason'='email_changed' AND convert_from(e.payload,'UTF8')::jsonb->>'user_id'=c.user_id::text AND convert_from(e.payload,'UTF8')::jsonb->>'session_id'=c.session_id::text),false),
 count(DISTINCT n.id),count(DISTINCT i.id),count(DISTINCT j.id),
 coalesce(bool_and(n.kind='old_email_notice' AND n.original_email=$1 AND n.target_email=$1 AND n.user_id=c.user_id AND n.session_id=c.session_id AND n.user_version::text=r.receipt->>'version'),false),
 coalesce(bool_and(i.kind='email_notice' AND i.job_id=j.id AND j.id=c.attempt_id AND j.phase='sent' AND a.channel='smtp' AND a.result='sent' AND a.terminal AND a.io_joined),false)
FROM latest c JOIN agenteam_account.users u ON u.id=c.user_id
LEFT JOIN agenteam_account.security_receipts r ON r.command_id=c.id
LEFT JOIN agenteam_outbox.events e ON e.id=c.event_id
LEFT JOIN agenteam_account.email_requests n ON n.id=c.resource_id
LEFT JOIN agenteam_account.delivery_intents i ON i.id=c.id AND i.link_id=n.id
LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id
LEFT JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id`, oldEmail).Scan(&confirms, &receiptMatches, &eventBound, &retainedSession, &notices, &intents, &jobs, &oldNotice, &smtpComplete))
	if confirms != 1 || notices != 1 || intents != 1 || jobs != 1 || !receiptMatches || !eventBound || !retainedSession || !oldNotice || !smtpComplete {
		t.Fatalf("ordinary email-change postcondition: confirms=%d notices=%d intents=%d jobs=%d receipt=%t event=%t retained_session=%t old_notice=%t smtp_complete=%t", confirms, notices, intents, jobs, receiptMatches, eventBound, retainedSession, oldNotice, smtpComplete)
	}
	t.Log("ordinary email change: committed receipt and retained-session event agree; one original-mailbox notice/intent/job finished SMTP with terminal I/O joined")
}

func recoveryCheck(t *testing.T, stage string, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(stage + " failed")
	}
}

func recoveryReadJSON(t *testing.T, path string, out any) {
	t.Helper()
	info, err := os.Lstat(path)
	actual, pathErr := filepath.EvalSymlinks(path)
	if err != nil || pathErr != nil || actual != path || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 64<<10 {
		t.Fatal("recovery private JSON file invalid")
	}
	raw, err := os.ReadFile(path)
	defer clear(raw)
	recoveryCheck(t, "private JSON read", err)
	recoveryCheck(t, "private JSON decode", json.Unmarshal(raw, out))
}

func recoveryNonce(raw string) bool {
	value, err := hex.DecodeString(raw)
	return err == nil && len(value) == 16 && raw == strings.ToLower(raw)
}

func recoveryCLIEnvironment(t *testing.T, fixture *pgfixture.Descriptor, runtime recoveryRuntime) []string {
	t.Helper()
	values := objectfixture.ConfigOnlyValues()
	for key, value := range accountenv.New(t).Values() {
		values[key] = value
	}
	for _, item := range []struct{ name, current, field string }{
		{"SECRET_KEYRING", "current_version", "version"},
		{"CURSOR_KEYRING", "current_kid", "kid"},
		{"KNOWLEDGE_CONFIRMATION_KEYRING", "current_kid", "kid"},
		{"OBJECT_DOWNLOAD_KEYRING", "current_kid", "kid"},
	} {
		var key [32]byte
		_, err := rand.Read(key[:])
		recoveryCheck(t, "CLI test key entropy", err)
		ring, err := json.Marshal(map[string]any{"format": 1, item.current: "1", "keys": []map[string]string{{item.field: "1", "key_b64": base64.StdEncoding.EncodeToString(key[:])}}})
		clear(key[:])
		recoveryCheck(t, "CLI test keyring encoding", err)
		values["AGENTEAM_CENTRAL_"+item.name] = string(ring)
		clear(ring)
	}
	values["AGENTEAM_CENTRAL_DATABASE_URL"] = fixture.URL(runtime.DatabaseName)
	values["AGENTEAM_CENTRAL_DATABASE_TLS_MODE"] = "verify-full"
	values["AGENTEAM_CENTRAL_DATABASE_CA_FILE"] = fixture.CAFile
	values["AGENTEAM_CENTRAL_PUBLIC_ORIGIN"] = runtime.Origin
	var env []string
	for name, value := range values {
		env = append(env, name+"="+value)
	}
	return env
}

type recoverySession struct {
	CSRF string `json:"csrf_token"`
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user"`
	Session struct {
		ID string `json:"id"`
	} `json:"session"`
}

func recoveryHTTPClient(t *testing.T, runtime recoveryRuntime, private string) *http.Client {
	t.Helper()
	if runtime.CAFile != filepath.Join(private, "certificate-ca.pem") {
		t.Fatal("recovery refused a foreign TLS CA path")
	}
	ca, err := os.ReadFile(runtime.CAFile)
	recoveryCheck(t, "preview TLS CA", err)
	trust := x509.NewCertPool()
	if !trust.AppendCertsFromPEM(ca) {
		t.Fatal("preview TLS CA invalid")
	}
	pin, err := base64.StdEncoding.DecodeString(runtime.SPKI)
	if err != nil || len(pin) != sha256.Size {
		t.Fatal("preview TLS pin invalid")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: trust, MinVersion: tls.VersionTLS12, VerifyConnection: func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("preview TLS leaf missing")
		}
		sum := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
		if !bytes.Equal(sum[:], pin) {
			return errors.New("preview TLS leaf pin mismatch")
		}
		return nil
	}}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != "localhost:5270" {
			return nil, errors.New("recovery refused a foreign HTTPS upstream")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", runtime.LocalAddress)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	jar, err := cookiejar.New(nil)
	recoveryCheck(t, "recovery cookie jar", err)
	return &http.Client{Transport: transport, Jar: jar, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func recoveryHTTP(t *testing.T, ctx context.Context, client *http.Client, runtime recoveryRuntime, method, path, csrf string, body any, want int, out any, sameKey ...string) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		recoveryCheck(t, "recovery HTTP request encoding", err)
		defer clear(raw)
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, runtime.Origin+path, reader)
	recoveryCheck(t, "recovery HTTP request", err)
	request.Header.Set("Origin", runtime.Origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	if method == "POST" {
		if len(sameKey) == 1 && sameKey[0] != "" {
			request.Header.Set("Idempotency-Key", sameKey[0])
		} else {
			key, err := foundation.NewID[struct{}]()
			recoveryCheck(t, "recovery HTTP command key", err)
			request.Header.Set("Idempotency-Key", key.String())
		}
	}
	response, err := client.Do(request)
	recoveryCheck(t, "recovery TLS HTTP round trip", err)
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	defer clear(raw)
	recoveryCheck(t, "recovery HTTP response read", err)
	if response.StatusCode != want {
		t.Fatalf("recovery %s status=%d want=%d", path, response.StatusCode, want)
	}
	if out != nil {
		recoveryCheck(t, "recovery HTTP response decode", json.Unmarshal(raw, out))
	}
}

func recoveryLogin(t *testing.T, ctx context.Context, client *http.Client, runtime recoveryRuntime) recoverySession {
	t.Helper()
	var current recoverySession
	recoveryHTTP(t, ctx, client, runtime, "GET", "/api/v1/auth/bootstrap", "", nil, http.StatusOK, &current)
	recoveryHTTP(t, ctx, client, runtime, "POST", "/api/v1/sessions/login", current.CSRF, runtime.Credentials, http.StatusOK, nil)
	recoveryHTTP(t, ctx, client, runtime, "GET", "/api/v1/session", "", nil, http.StatusOK, &current)
	if current.CSRF == "" || current.Session.ID == "" {
		t.Fatal("recovery current session identity missing")
	}
	return current
}

func recoveryVerifySMTP(t *testing.T, ctx context.Context, runtime recoveryRuntime) {
	t.Helper()
	control, err := url.Parse(runtime.SMTPControl)
	if err != nil || control.Scheme != "http" || control.Port() != "9000" || control.Path != "" || control.RawQuery != "" || control.Fragment != "" || control.User != nil || !recoveryNonce(runtime.SMTPNonce) || !recoveryNonce(runtime.SMTPEndpoint) {
		t.Fatal("recovery SMTP descriptor invalid")
	}
	ip, err := netip.ParseAddr(control.Hostname())
	if err != nil || !ip.IsPrivate() {
		t.Fatal("recovery SMTP control must be a private fixture address")
	}
	raw, err := pgfixture.Docker(ctx, "inspect", "--format", "{{json .}}", "agenteam-d07-smtp-"+runtime.SMTPNonce)
	recoveryCheck(t, "owned SMTP container inspection", err)
	var container struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
		State           struct{ Running bool }
		NetworkSettings struct {
			Ports    map[string]any
			Networks map[string]struct{ NetworkID, IPAddress string }
		}
	}
	recoveryCheck(t, "owned SMTP container metadata", json.Unmarshal(raw, &container))
	if len(container.ID) != 64 || container.Name != "/agenteam-d07-smtp-"+runtime.SMTPNonce || container.Config.Image != pgfixture.Image || container.Config.Labels[smtpfixture.Label] != runtime.SMTPNonce || !container.State.Running || len(container.NetworkSettings.Networks) != 1 {
		t.Fatal("recovery SMTP container ownership mismatch")
	}
	for _, ports := range container.NetworkSettings.Ports {
		if ports != nil {
			t.Fatal("recovery SMTP fixture unexpectedly publishes a port")
		}
	}
	for _, network := range container.NetworkSettings.Networks {
		if len(network.NetworkID) != 64 || network.IPAddress != ip.String() {
			t.Fatal("recovery SMTP socket ownership mismatch")
		}
		raw, err = pgfixture.Docker(ctx, "network", "inspect", "--format", "{{json .}}", network.NetworkID)
		recoveryCheck(t, "owned SMTP network inspection", err)
		var inspected struct {
			ID       string
			Internal bool
			Labels   map[string]string
		}
		recoveryCheck(t, "owned SMTP network metadata", json.Unmarshal(raw, &inspected))
		if inspected.ID != network.NetworkID || !inspected.Internal || inspected.Labels[smtpfixture.Label] != runtime.SMTPNonce {
			t.Fatal("recovery SMTP network ownership mismatch")
		}
	}
}

func recoverySMTPCode(t *testing.T, ctx context.Context, runtime recoveryRuntime, target, job string) string {
	t.Helper()
	recoveryVerifySMTP(t, ctx, runtime)
	request, err := http.NewRequestWithContext(ctx, "GET", runtime.SMTPControl+"/message/"+runtime.SMTPEndpoint, nil)
	recoveryCheck(t, "owned SMTP message request", err)
	request.Header.Set("X-Fixture-Nonce", runtime.SMTPNonce)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	recoveryCheck(t, "owned SMTP message read", err)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "message/rfc822" || response.ContentLength < 1 || response.ContentLength > 65536 {
		t.Fatal("owned SMTP message unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	defer clear(raw)
	recoveryCheck(t, "owned SMTP message body", err)
	if int64(len(raw)) != response.ContentLength {
		t.Fatal("owned SMTP message length mismatch")
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	recoveryCheck(t, "owned SMTP message parse", err)
	recipients, err := message.Header.AddressList("To")
	if err != nil || len(recipients) != 1 || recipients[0].Address != target || message.Header.Get("Message-ID") != "<"+job+"@localhost>" {
		t.Fatal("owned SMTP message is not this initialization delivery")
	}
	body, err := io.ReadAll(message.Body)
	defer clear(body)
	recoveryCheck(t, "owned SMTP code body", err)
	match := regexp.MustCompile(`Agenteam Email verification verification code: ([0-9]{6})\r\n`).FindSubmatch(body)
	if len(match) != 2 {
		t.Fatal("owned SMTP initialization code missing")
	}
	return string(match[1])
}
