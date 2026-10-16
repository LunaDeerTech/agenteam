//go:build integration

package account_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Only the post-command read is rejected. All command transactions and their
// current authorization, audit and outbox writes use the real PostgreSQL store.
type smtpProjectionStore struct {
	*postgres.Store
	fail atomic.Bool
}

func (s *smtpProjectionStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	owner := cause.Details().Owner
	if s.fail.Load() && (owner == "account.smtp-settings-read" || owner == "account.mail-query") {
		return foundation.NotCommittedResult(foundation.NewFault(foundation.DependencyUnavailable, foundation.NotCommitted))
	}
	return s.Store.WithinTx(ctx, cause, fn)
}

// The SMTP routes never invoke ProfilePort; embedding it makes unexpected use
// fail instead of supplying a fake profile, authentication or success result.
type unusedSMTPProfiles struct{ c.ProfilePort }

func TestAccountSMTPCommittedProjectionHTTPAndReplay(t *testing.T) {
	db, raw, _ := database(t)
	store := &smtpProjectionStore{Store: raw}
	keyring, pagination, _ := keys(t)
	authority, err := account.NewAuthority(store, keyring)
	if err != nil {
		t.Fatal(err)
	}
	f := assembleB02(t, db, raw, store, authority, nil)
	password := f.bootstrap(t)
	login, _ := loginRequest(t, f.fixture, password, "admin@mail.com")
	response, err := f.service.Login(ctxFor(t), login)
	if err != nil {
		t.Fatal(err)
	}
	cookie := useCookie(t, response)
	if err = response.Close(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	session, err := f.service.GetSession(ctxFor(t), cookie)
	if err != nil {
		t.Fatal(err)
	}
	defer session.CSRF.Destroy()
	actor, err := f.service.Authenticate(ctxFor(t), cookie)
	if err != nil {
		t.Fatal(err)
	}
	var cookieValue, csrf string
	if err = cookie.Use(func(v []byte) error { cookieValue = string(v); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = session.CSRF.Use(func(v []byte) error { csrf = string(v); return nil }); err != nil {
		t.Fatal(err)
	}
	system, err := account.NewSystemHTTPFacade(f.service, pagination)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := account.NewHTTPHandler(f.service, unusedSMTPProfiles{}, account.HTTPOptions{PublicOrigin: httpOrigin, System: system})
	if err != nil {
		t.Fatal(err)
	}
	handler = httpapi.Handler(nil, handler)
	request := func(method, path, key string, body map[string]any) httpResult {
		t.Helper()
		var encoded []byte
		if body != nil {
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, httpOrigin+path, bytes.NewReader(encoded))
		r.Header.Set("Origin", httpOrigin)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookieValue})
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return httpResult{status: w.Code, headers: w.Header(), data: w.Body.Bytes()}
	}
	settings := request("GET", "/api/v1/system/smtp", "", nil).want(t, 200).object(t)
	configuration := func(version any) map[string]any {
		return map[string]any{"version": version, "host": "mail.example.test", "port": 2525, "encryption": "none", "sender_email": "sender@example.test", "auto_retry_count": "0", "retry_interval_seconds": "10"}
	}
	// The same input first commits, then historical replay also loses its read.
	// Recovery must return one receipt, without another config version or job.
	recover := func(method, path string, body map[string]any, status int) map[string]any {
		t.Helper()
		key := id[struct{}](t).String()
		store.fail.Store(true)
		for range 2 {
			got := request(method, path, key, body).want(t, http.StatusServiceUnavailable)
			problem := got.object(t)
			if problem["code"] != "DEPENDENCY_UNAVAILABLE" || problem["commit_state"] != "committed" || problem["retry_hint"] != "replay" || len(got.headers.Values("Set-Cookie")) != 0 {
				t.Fatal("post-commit projection failure lost outcome or changed cookie")
			}
		}
		store.fail.Store(false)
		got := request(method, path, key, body).want(t, status).object(t)
		again := request(method, path, key, body).want(t, status).object(t)
		if got["applied_version"] != again["applied_version"] || got["job_id"] != again["job_id"] {
			t.Fatal("same-key replay changed receipt")
		}
		return got
	}
	updated := recover("PUT", "/api/v1/system/smtp", configuration(settings["version"]), 200)
	if updated["applied_version"] != "2" || updated["settings"].(map[string]any)["version"] != "2" {
		t.Fatal("SMTP PUT replay incremented version")
	}
	unconfigured := recover("POST", "/api/v1/system/smtp/unconfigure", map[string]any{"version": "2", "auto_retry_count": "0", "retry_interval_seconds": "10"}, 200)
	if unconfigured["applied_version"] != "3" || unconfigured["settings"].(map[string]any)["configured"] != false {
		t.Fatal("SMTP unconfigure replay changed receipt")
	}
	request("PUT", "/api/v1/system/smtp", id[struct{}](t).String(), configuration("3")).want(t, 200)
	accepted := recover("POST", "/api/v1/system/smtp/test", map[string]any{"recipient": "owned@example.test"}, 202)
	job, err := foundation.ParseID[c.MailJob](accepted["job_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ReconcileDeliveryIntents(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	port, runtime := newManualMail(t, f)
	attempt, err := port.ClaimDelivery(ctxFor(t), job)
	if err != nil {
		t.Fatal(err)
	}
	runtime.accept(attempt)
	done := runtime.finish(t, attempt, c.DeliveryMaterials{})
	if err = port.FinishDelivery(ctxFor(t), attempt, done); err != nil {
		t.Fatal(err)
	}
	terminal, err := f.service.GetMailJob(ctxFor(t), actor, job)
	if err != nil {
		t.Fatal(err)
	}
	retried := recover("POST", "/api/v1/system/mail-jobs/"+job.String()+"/retry", map[string]any{"version": strconv.FormatUint(uint64(terminal.Version), 10)}, 202)
	if retried["job_id"] == accepted["job_id"] {
		t.Fatal("manual retry did not create a new job")
	}
	var intents, tests, retries int
	if err = raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.delivery_intents),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.test.request'),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='smtp.delivery.retry')`).Scan(&intents, &tests, &retries); err != nil || intents != 2 || tests != 1 || retries != 1 {
		t.Fatal("replay repeated committed effects", intents, tests, retries, err)
	}
	store.fail.Store(true)
	read := request("GET", "/api/v1/system/smtp", "", nil).want(t, 503).object(t)
	if read["commit_state"] != "not_committed" {
		t.Fatal("ordinary read falsely classified as a committed command")
	}
}
