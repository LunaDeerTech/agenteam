//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// The first successful HTTP response is intentionally discarded by the caller.
// The repeated request uses the same authenticated browser, body and command key.
func previewEmailInitializationReplay(t *testing.T, parent context.Context, cfg config.Config, core *account.Service, db *pgfixture.Database, origin, localAddress string, ca *x509.Certificate, credential previewCredential) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	connection := db.Connect(t)
	var user string
	previewCheck(t, "email replay bootstrap owner", connection.QueryRow(ctx, `SELECT user_id::text FROM agenteam_account.bootstrap WHERE singleton`).Scan(&user))
	store, err := postgres.Open(ctx, cfg.Database())
	previewCheck(t, "email replay operator database", err)
	defer func() {
		store.StopAdmission()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := store.Drain(cleanup); err != nil {
			force, finish := context.WithTimeout(context.Background(), 5*time.Second)
			defer finish()
			previewCheck(t, "email replay operator database join", store.ForceClose(force))
		}
	}()
	authority, err := account.NewAuthority(store, cfg.AccountKeyring())
	previewCheck(t, "email replay operator authority", err)
	auditor, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: authority})
	previewCheck(t, "email replay operator audit", err)
	issued, err := authority.IssueEmailAuthorization(ctx, auditor, user, "initialize_email", "initialized-owner@example.test")
	previewCheck(t, "email replay operator authorization", err)
	defer issued.Authorization.Destroy()

	trust := x509.NewCertPool()
	trust.AddCert(ca)
	originURL, err := url.Parse(origin)
	previewCheck(t, "email replay origin", err)
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: trust, MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != originURL.Host {
			return nil, errors.New("email replay refused a different upstream")
		}
		return (&net.Dialer{}).DialContext(ctx, network, localAddress)
	}}
	defer transport.CloseIdleConnections()
	jar, err := cookiejar.New(nil)
	previewCheck(t, "email replay browser jar", err)
	client := &http.Client{Transport: transport, Jar: jar, Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path, key, csrf string, body any, discard ...bool) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			previewCheck(t, "email replay request encoding", err)
			defer clear(raw)
			reader = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, origin+path, reader)
		previewCheck(t, "email replay request construction", err)
		req.Header.Set("Origin", origin)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(req)
		previewCheck(t, "email replay TLS HTTP", err)
		defer response.Body.Close()
		if len(discard) > 0 && discard[0] {
			return response.StatusCode, nil
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		previewCheck(t, "email replay response read", err)
		return response.StatusCode, raw
	}
	expect := func(method, path, key, csrf string, body any, want int) []byte {
		t.Helper()
		got, raw := request(method, path, key, csrf, body)
		if got != want {
			clear(raw)
			t.Fatalf("email replay %s status=%d want=%d", path, got, want)
		}
		return raw
	}
	bootstrap := expect(http.MethodGet, "/api/v1/auth/bootstrap", "", "", nil, http.StatusOK)
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	previewCheck(t, "email replay anonymous CSRF", json.Unmarshal(bootstrap, &session))
	clear(bootstrap)
	if session.CSRF == "" {
		t.Fatal("email replay anonymous CSRF missing")
	}
	raw := expect(http.MethodPost, "/api/v1/sessions/login", guardID[struct{}](t).String(), session.CSRF, credential, http.StatusOK)
	clear(raw)
	raw = expect(http.MethodGet, "/api/v1/session", "", "", nil, http.StatusOK)
	previewCheck(t, "email replay current session", json.Unmarshal(raw, &session))
	clear(raw)
	if session.CSRF == "" {
		t.Fatal("email replay current CSRF missing")
	}
	err = issued.Authorization.Use(func(token []byte) error {
		body := map[string]string{"authorization": string(token), "password": credential.Password}
		key := guardID[struct{}](t).String()
		// Close the first HTTP response without reading its receipt body.
		status, _ := request(http.MethodPost, "/api/v1/me/email-initialization/request", key, session.CSRF, body, true)
		if status != http.StatusAccepted {
			t.Fatalf("email replay first request status=%d want=202", status)
		}
		var first []byte
		previewCheck(t, "email replay authoritative first receipt", connection.QueryRow(ctx, `SELECT r.receipt FROM agenteam_account.security_receipts r JOIN agenteam_account.commands c ON c.id=r.command_id WHERE c.user_id=$1 AND c.command_key=$2 AND c.command_name='email-initialize-request'`, user, key).Scan(&first))
		defer clear(first)
		var receipt account.EmailProofReceipt
		previewCheck(t, "email replay original receipt", json.Unmarshal(first, &receipt))
		if receipt.RequestID == "" {
			t.Fatal("email replay original request missing")
		}
		sameReceipt := func(raw []byte) bool {
			var got account.EmailProofReceipt
			return json.Unmarshal(raw, &got) == nil && reflect.DeepEqual(receipt, got)
		}
		// Only the repeated response can supply the browser's recovery receipt.
		repeated := expect(http.MethodPost, "/api/v1/me/email-initialization/request", key, session.CSRF, body, http.StatusAccepted)
		defer clear(repeated)
		if !sameReceipt(repeated) {
			t.Fatal("email replay changed the original 202 receipt")
		}
		mismatch := map[string]string{"authorization": string(token), "password": credential.Password + "different"}
		rejected := expect(http.MethodPost, "/api/v1/me/email-initialization/request", key, session.CSRF, mismatch, http.StatusConflict)
		if !bytes.Contains(rejected, []byte("IDEMPOTENCY_KEY_REUSED")) {
			clear(rejected)
			t.Fatal("email replay different body was not an idempotency conflict")
		}
		clear(rejected)
		status, rejected = request(http.MethodPost, "/api/v1/me/email-initialization/request", guardID[struct{}](t).String(), session.CSRF, body)
		clear(rejected)
		if status != http.StatusBadRequest {
			t.Fatalf("email replay consumed authorization with new key status=%d want=400", status)
		}
		var consumed string
		previewCheck(t, "email replay consumed authorization", connection.QueryRow(ctx, `SELECT consumed_request_id::text FROM agenteam_account.operator_authorizations WHERE id=$1`, issued.ID).Scan(&consumed))
		if consumed != receipt.RequestID {
			t.Fatal("email replay authorization consumed a different request")
		}
		_, err := core.ReconcileDeliveryIntents(ctx)
		previewCheck(t, "email replay canonical enqueue", err)
		var requests, intents, jobs int
		previewCheck(t, "email replay durable request count", connection.QueryRow(ctx, `SELECT count(*) FROM agenteam_account.email_requests WHERE authorization_id=$1`, issued.ID).Scan(&requests))
		previewCheck(t, "email replay durable intent/job count", connection.QueryRow(ctx, `SELECT count(DISTINCT i.id),count(DISTINCT j.id) FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id WHERE i.link_id=$1`, receipt.RequestID).Scan(&intents, &jobs))
		if requests != 1 || intents != 1 || jobs != 1 {
			t.Fatalf("email replay duplicated durable work requests=%d intents=%d jobs=%d", requests, intents, jobs)
		}
		// Join this real SMTP job before the caller seeds its ordinary invitation,
		// so this code cannot replace that invitation in the sink's last-message slot.
		for {
			var phase, channel string
			var terminal, joined bool
			previewCheck(t, "email replay SMTP completion", connection.QueryRow(ctx, `SELECT j.phase,coalesce(a.channel,''),coalesce(a.terminal,false),coalesce(a.io_joined,false) FROM agenteam_account.mail_jobs j JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id LEFT JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id WHERE i.link_id=$1`, receipt.RequestID).Scan(&phase, &channel, &terminal, &joined))
			if terminal && joined {
				if phase != "sent" || channel != "smtp" {
					t.Fatal("email replay code did not finish through its owned SMTP sink")
				}
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("email replay SMTP completion deadline")
			case <-time.After(50 * time.Millisecond):
			}
		}
		// Expiring the ten-minute attempt window must not erase the independent
		// sixty-second send cooldown from a delivery near that window's end.
		_, err = connection.Exec(ctx, `UPDATE agenteam_account.email_rate_limits SET window_started_at=clock_timestamp()-interval '11 minutes',last_delivery_at=clock_timestamp() WHERE user_id=$1 AND kind='initialization'`, user)
		previewCheck(t, "email replay controlled budget window boundary", err)
		cooldown := expect(http.MethodPost, "/api/v1/me/email-initialization/resend", guardID[struct{}](t).String(), session.CSRF, map[string]string{"request_id": receipt.RequestID}, http.StatusTooManyRequests)
		if !bytes.Contains(cooldown, []byte("RATE_LIMITED")) {
			clear(cooldown)
			t.Fatal("email replay budget reset bypassed the send cooldown")
		}
		clear(cooldown)
		previewCheck(t, "email replay cooldown preserves intent/job", connection.QueryRow(ctx, `SELECT count(DISTINCT i.id),count(DISTINCT j.id) FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id WHERE i.link_id=$1`, receipt.RequestID).Scan(&intents, &jobs))
		if intents != 1 || jobs != 1 {
			t.Fatal("email replay cooldown created an extra intent or job")
		}
		_, err = connection.Exec(ctx, `UPDATE agenteam_account.operator_authorizations SET revoked_at=clock_timestamp() WHERE id=$1`, issued.ID)
		previewCheck(t, "email replay controlled authorization revocation", err)
		historical := expect(http.MethodPost, "/api/v1/me/email-initialization/request", key, session.CSRF, body, http.StatusAccepted)
		defer clear(historical)
		if !sameReceipt(historical) {
			t.Fatal("email replay lost historical receipt after authorization revocation")
		}
		return nil
	})
	previewCheck(t, "email replay authorization material", err)
	raw = expect(http.MethodPost, "/api/v1/sessions/logout", guardID[struct{}](t).String(), session.CSRF, struct{}{}, http.StatusNoContent)
	clear(raw)
	t.Log("email initialization HTTP 202 replay: same receipt, one request/intent/job; changed body conflicts, consumed authorization rejects a new key, revoked authorization still recovers its receipt")
}
