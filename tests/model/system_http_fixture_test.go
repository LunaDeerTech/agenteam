//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	accountc "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const systemHTTPOrigin = "https://system-http.example.test"

// These decorators never provide authorization. All callbacks delegate to the
// same real Store/Tx first; result projection is an explicit application test,
// not a network/ROLLBACK proxy or proof of an original writer still running.
type systemHTTPTrace struct {
	owner, namespace, command        string
	locks                            []f.LockRequest
	held, receiptReads, payloadReads int
	finalFact                        bool
	commandRawHash, receiptDigest    string
	actual, projected                f.CommitState
}
type systemHTTPStore struct {
	*postgres.Store
	mu       sync.Mutex
	active   map[f.Tx]*systemHTTPTrace
	traces   []systemHTTPTrace
	before   func(context.Context, f.TransactionCause) error
	decorate func(context.Context, f.TransactionCause, systemHTTPTrace, f.CommitResult) f.CommitResult
}

func (s *systemHTTPStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	before, decorate := s.before, s.decorate
	s.mu.Unlock()
	if before != nil {
		if err := before(ctx, cause); err != nil {
			var fault *f.Fault
			if !errors.As(err, &fault) {
				fault = f.NewFault(f.DependencyUnavailable, f.NotStarted)
			}
			return f.NotCommittedResult(fault)
		}
	}
	d := cause.Details()
	trace := &systemHTTPTrace{owner: d.Owner, namespace: d.Primary.Namespace(), command: d.Primary.Command()}
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		s.mu.Lock()
		s.active[tx] = trace
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.active, tx); s.mu.Unlock() }()
		if err := fn(ctx, tx); err != nil {
			return err
		}
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		if trace.namespace == "model.system" {
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.commands WHERE command_identity=$1 AND phase='committed' AND safe_receipt IS NOT NULL)`, d.Primary.Canonical()).Scan(&trace.finalFact)
		} else if trace.namespace == "secret" {
			raw := []byte(d.Primary.Canonical())
			digest, digestErr := cursor.Digest(raw)
			if digestErr != nil {
				return digestErr
			}
			trace.commandRawHash = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
			trace.receiptDigest = string(digest)
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_command_receipts WHERE scope='system' AND scope_key='system' AND command_digest=$1)`, string(digest)).Scan(&trace.finalFact)
		}
		return err
	})
	trace.actual = result.State()
	if decorate != nil {
		result = decorate(ctx, cause, *trace, result)
	}
	trace.projected = result.State()
	s.mu.Lock()
	s.traces = append(s.traces, *trace)
	s.mu.Unlock()
	return result
}
func (s *systemHTTPStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	if trace := s.active[tx]; trace != nil {
		trace.locks = append([]f.LockRequest(nil), locks...)
	}
	s.mu.Unlock()
	return s.Store.AcquireAll(ctx, tx, locks)
}
func (s *systemHTTPStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	err := s.Store.RequireHeldLocks(ctx, tx, locks)
	s.mu.Lock()
	if trace := s.active[tx]; trace != nil && err == nil {
		trace.held++
	}
	s.mu.Unlock()
	return err
}
func (s *systemHTTPStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, err := s.Store.InTx(tx)
	if err != nil {
		return nil, err
	}
	return systemHTTPExecutor{x, s, tx}, nil
}

type systemHTTPExecutor struct {
	postgres.SQLExecutor
	s  *systemHTTPStore
	tx f.Tx
}

func (x systemHTTPExecutor) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	x.s.mu.Lock()
	if trace := x.s.active[x.tx]; trace != nil {
		if strings.Contains(sql, "agenteam_secret.secret_command_receipts") {
			trace.receiptReads++
		}
		if strings.Contains(sql, "agenteam_secret.secret_payloads") || strings.Contains(sql, "digest_payload_id") {
			trace.payloadReads++
		}
	}
	x.s.mu.Unlock()
	return x.SQLExecutor.QueryRow(ctx, sql, args...)
}
func (s *systemHTTPStore) setHooks(before func(context.Context, f.TransactionCause) error, decorate func(context.Context, f.TransactionCause, systemHTTPTrace, f.CommitResult) f.CommitResult) {
	s.mu.Lock()
	s.before, s.decorate = before, decorate
	s.mu.Unlock()
}
func (s *systemHTTPStore) observations() []systemHTTPTrace {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]systemHTTPTrace(nil), s.traces...)
}

type systemHTTPAudit struct {
	ac.Appender
	fail atomic.Bool
	seen atomic.Int64
}

func (a *systemHTTPAudit) AppendInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	r, err := a.Appender.AppendInTx(ctx, tx, e, k)
	if err == nil && a.fail.Load() {
		a.seen.Add(1)
		return ac.AppendReceipt{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return r, err
}

type systemHTTPEvents struct {
	oc.Appender
	fail atomic.Bool
	seen atomic.Int64
}

func (a *systemHTTPEvents) AppendEventInTx(ctx context.Context, tx f.Tx, actor id.Actor, e ec.Event, p oc.AppendPlan) (oc.AppendReceipt, error) {
	r, err := a.Appender.AppendEventInTx(ctx, tx, actor, e, p)
	if err == nil && a.fail.Load() {
		a.seen.Add(1)
		return oc.AppendReceipt{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return r, err
}

type systemHTTPAccountProcess struct{ id accountc.ProcessID }

func (p systemHTTPAccountProcess) CurrentProcess() accountc.ProcessID { return p.id }
func (p systemHTTPAccountProcess) ConfirmStopped(context.Context, accountc.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}

type systemHTTPLog struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (l *systemHTTPLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.Write(b)
}
func (l *systemHTTPLog) text() string { l.mu.Lock(); defer l.mu.Unlock(); return l.data.String() }

type systemHTTPFixture struct {
	*fixture
	account      *account.Service
	tracked      *systemHTTPStore
	http         http.Handler
	auditFail    *systemHTTPAudit
	eventFail    *systemHTTPEvents
	password     sc.SecretMaterial
	adminBrowser systemHTTPBrowser
	log          *systemHTTPLog
}
type systemHTTPBrowser struct {
	actor        id.Actor
	cookie, csrf string
	email        string
}

func newSystemHTTPFixture(t *testing.T) *systemHTTPFixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	store := &systemHTTPStore{Store: raw, active: map[f.Tx]*systemHTTPTrace{}}
	ak, ck, sk := testKeys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(testContext(t)); err != nil {
		t.Fatal(err)
	}
	ma, err := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts})
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Models: ma})
	if err != nil {
		t.Fatal(err)
	}
	auditFail := &systemHTTPAudit{Appender: aud}
	router, err := model.NewSecretUsageRouter(ma, accounts)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.New(store, sk, auditFail, secret.Authorizations{Sessions: accounts, System: accounts, Usage: router, AccountWrites: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = secrets.Initialize(testContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secrets.StopMaintenance)
	catalog := ec.NewCatalog()
	types, err := model.DefineEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := accountc.DefineSessionsRevoked(catalog)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := accountc.DefineDeliveryRequested(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := systemHTTPAccountProcess{newID[accountc.Process](t)}
	outProcess, _ := f.ParseID[oc.Process](process.id.String())
	events, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{model.ModelProducer: ma, accountc.AccountProducer: accounts}, Sessions: accounts, System: accounts, Audit: auditFail, Cursors: ck, Processes: liveProcess{outProcess}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := outbox.NewRuntime(events, nil, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.Initialize(testContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runtime.StopClaims()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := runtime.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	eventFail := &systemHTTPEvents{Appender: events}
	deps := model.Dependencies{Secret: secrets, Audit: auditFail, Events: eventFail, ConfigurationEvents: types, Cursors: ck}
	service, err := model.New(store, ma, deps)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Initialize(testContext(t)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "recovery.jsonl")
	sink, err := recoverylog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	challenges, err := account.NewChallenges(accounts, process.id)
	if err != nil {
		t.Fatal(err)
	}
	core, err := account.New(account.Dependencies{Authority: accounts, Audit: auditFail, Secrets: secrets, Events: events, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: process, RecoveryLog: sink, Challenges: challenges})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := core.Force(ctx); err != nil && !core.Joined() {
			t.Error(err)
		}
	})
	status, err := core.Bootstrap(testContext(t))
	if err != nil || !status.Created || status.LogState != "written" {
		t.Fatal("real Account bootstrap", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Password string `json:"initial_password"`
	}
	if json.Unmarshal(data, &record) != nil || record.Password == "" {
		t.Fatal("private bootstrap record")
	}
	clear(data)
	password, err := sc.NewSecretMaterial([]byte(record.Password))
	record.Password = ""
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(password.Destroy)
	v := &systemHTTPFixture{fixture: &fixture{db: db, raw: raw, store: store, accounts: accounts, authority: ma, service: service, secrets: secrets, aud: aud, events: events, runtime: runtime, deps: deps}, account: core, tracked: store, auditFail: auditFail, eventFail: eventFail, password: password, log: &systemHTTPLog{}}
	v.adminBrowser = v.login(t, "admin@mail.com")
	v.admin = v.adminBrowser.actor
	v.install(t, secrets)
	return v
}
func (v *systemHTTPFixture) install(t *testing.T, writes sc.HumanWriteCommands) {
	t.Helper()
	handler, err := model.NewSystemHTTPHandler(v.service, v.account, writes, model.SystemHTTPOptions{PublicOrigin: systemHTTPOrigin})
	if err != nil {
		t.Fatal(err)
	}
	v.http = httpapi.Handler(slog.New(slog.NewJSONHandler(v.log, nil)), handler)
}
func (v *systemHTTPFixture) login(t *testing.T, email string) systemHTTPBrowser {
	t.Helper()
	anonymous, err := v.account.NewAnonymousContext(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	defer anonymous.Cookie.Destroy()
	defer anonymous.CSRF.Destroy()
	browser, err := v.account.VerifyAnonymousContext(testContext(t), anonymous.Cookie, anonymous.CSRF)
	if err != nil {
		t.Fatal(err)
	}
	request, err := accountc.NewLoginRequest(accountc.LoginFields{Browser: browser, Key: f.IdempotencyKey(newID[struct{}](t).String()), Email: email, Password: v.password, ClientIP: netip.MustParseAddr("192.0.2.18")})
	if err != nil {
		t.Fatal(err)
	}
	response, err := v.account.Login(testContext(t), request)
	if err != nil {
		t.Fatal("real login", err)
	}
	var cookie sc.SecretMaterial
	if err = response.UseCookie(func(b []byte) error { var e error; cookie, e = sc.NewSecretMaterial(b); return e }); err != nil {
		t.Fatal(err)
	}
	defer cookie.Destroy()
	if err = response.Close(testContext(t)); err != nil {
		t.Fatal("response actual close", err)
	}
	actor, err := v.account.Authenticate(testContext(t), cookie)
	if err != nil {
		t.Fatal(err)
	}
	view, err := v.account.GetSession(testContext(t), cookie)
	if err != nil {
		t.Fatal(err)
	}
	defer view.CSRF.Destroy()
	out := systemHTTPBrowser{actor: actor, email: email}
	if err = cookie.Use(func(b []byte) error { out.cookie = string(b); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = view.CSRF.Use(func(b []byte) error { out.csrf = string(b); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}
func (v *systemHTTPFixture) addBrowser(t *testing.T, role string) systemHTTPBrowser {
	t.Helper()
	user := newID[id.User](t)
	name := "h" + user.String()[24:]
	email := name + "@example.test"
	_, err := v.raw.Exec(testContext(t), `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) SELECT $1,$2,$3,'HTTP fixture',$4,password_phc,1,1,1,false,'system' FROM agenteam_account.users WHERE id=$5`, user.String(), email, name, role, v.admin.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	return v.login(t, email)
}

type systemHTTPResponse struct {
	status  int
	headers http.Header
	body    []byte
}

func (r systemHTTPResponse) object(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatal("non-JSON response", r.status)
	}
	return out
}
func (r systemHTTPResponse) want(t *testing.T, status int) systemHTTPResponse {
	t.Helper()
	if r.status != status {
		var p httpapi.Problem
		_ = json.Unmarshal(r.body, &p)
		t.Fatalf("HTTP=%d want=%d code=%s commit=%s", r.status, status, p.Code, p.CommitState)
	}
	if r.headers.Get("Cache-Control") != "no-store" || r.headers.Get("X-Content-Type-Options") != "nosniff" || r.headers.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing security headers")
	}
	return r
}
func (r systemHTTPResponse) problem(t *testing.T, status int, code f.Code) {
	t.Helper()
	r.want(t, status)
	var p httpapi.Problem
	if json.Unmarshal(r.body, &p) != nil || p.Code != code || p.Instance != "/api/v1" {
		t.Fatalf("wrong safe problem code=%s instance=%s", p.Code, p.Instance)
	}
}
func (v *systemHTTPFixture) request(t *testing.T, b systemHTTPBrowser, method, path, key string, body any) systemHTTPResponse {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	return v.rawRequest(testContext(t), b, method, path, key, raw, nil)
}
func (v *systemHTTPFixture) rawRequest(ctx context.Context, b systemHTTPBrowser, method, path, key string, body []byte, change func(*http.Request)) systemHTTPResponse {
	r := httptest.NewRequest(method, systemHTTPOrigin+path, bytes.NewReader(body)).WithContext(ctx)
	r.Header.Set("Origin", systemHTTPOrigin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	if b.cookie != "" {
		r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: b.cookie})
	}
	if b.csrf != "" {
		r.Header.Set("X-CSRF-Token", b.csrf)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if change != nil {
		change(r)
	}
	w := httptest.NewRecorder()
	v.http.ServeHTTP(w, r)
	return systemHTTPResponse{w.Code, w.Header().Clone(), bytes.Clone(w.Body.Bytes())}
}
func httpProviderBody(protocol mc.Protocol) map[string]any {
	return map[string]any{"input": map[string]any{"name": "HTTP provider", "protocol": protocol, "base_url": "https://never-contacted.example/v1", "enabled": true, "credential_ref": nil, "options": map[string]any{}}}
}
func httpModelBody(provider string, kind mc.ModelType) map[string]any {
	outputs := []string{}
	if kind != mc.RerankerModel {
		outputs = append(outputs, map[mc.ModelType]string{mc.ChatModel: "text", mc.EmbeddingModel: "vector", mc.ImageModel: "image"}[kind])
	}
	return map[string]any{"provider_id": provider, "input": map[string]any{"name": "HTTP model", "provider_model_id": "fixture-model", "type": kind, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": kind == mc.ChatModel, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": outputs, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}}}
}
func (v *systemHTTPFixture) provider(t *testing.T, protocol mc.Protocol) string {
	t.Helper()
	return v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", newID[struct{}](t).String(), httpProviderBody(protocol)).want(t, 200).object(t)["resource_id"].(string)
}
func (v *systemHTTPFixture) model(t *testing.T, provider string, kind mc.ModelType) string {
	t.Helper()
	return v.request(t, v.adminBrowser, "POST", "/api/v1/system/models", newID[struct{}](t).String(), httpModelBody(provider, kind)).want(t, 200).object(t)["resource_id"].(string)
}
func (v *systemHTTPFixture) credential(t *testing.T, key, value string) string {
	t.Helper()
	return v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-credentials", key, map[string]any{"value": value}).want(t, 200).object(t)["credential_id"].(string)
}

func (v *systemHTTPFixture) facts(t *testing.T) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	for _, table := range []string{"agenteam_model.providers", "agenteam_model.models", "agenteam_model.platform_selection", "agenteam_model.commands", "agenteam_model.references", "agenteam_secret.secrets", "agenteam_secret.secret_payloads", "agenteam_secret.secret_references", "agenteam_secret.secret_leases", "agenteam_secret.secret_command_receipts", "agenteam_audit.audit_records", "agenteam_outbox.events"} {
		var raw []byte
		err := v.raw.QueryRow(testContext(t), `SELECT coalesce(jsonb_agg(j ORDER BY j::text),'[]'::jsonb) FROM (SELECT to_jsonb(t) j FROM `+table+` t) rows`).Scan(&raw)
		if err != nil {
			t.Fatal(err)
		}
		out[table] = sha256.Sum256(raw)
		clear(raw)
	}
	return out
}
func httpSameFacts(t *testing.T, before, after map[string][32]byte) {
	t.Helper()
	for table, want := range before {
		if after[table] != want {
			t.Fatalf("business facts changed in %s", table)
		}
	}
}
func (v *systemHTTPFixture) role(t *testing.T, b systemHTTPBrowser, role string) {
	t.Helper()
	key, _ := f.UserLock(b.actor.Details().UserID)
	result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := v.raw.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_account.users SET role=$1,version=version+1 WHERE id=$2`, role, b.actor.Details().UserID)
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("owned role change", result.Fault())
	}
}
func httpReceiptSame(t *testing.T, a, b systemHTTPResponse) {
	t.Helper()
	if !reflect.DeepEqual(a.object(t), b.object(t)) {
		t.Fatal("historical receipt changed")
	}
}
