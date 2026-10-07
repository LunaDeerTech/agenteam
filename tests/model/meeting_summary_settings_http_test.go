//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelMeetingSummarySettingsHTTP(t *testing.T) {
	meetingSummarySettingsBudget(t)
	v := newMeetingSummaryFixture(t, false)
	request := func(b systemHTTPBrowser, method, key string, body any) systemHTTPResponse {
		return meetingSummarySettingsRequest(t, v.systemHTTPFixture, b, method, meetingSummarySettingsPath, key, body)
	}
	request(v.adminBrowser, "GET", "", nil).problem(t, 503, f.DependencyUnbound)
	if e := v.service.InitializeMeetingSummarySelection(testContext(t)); e != nil {
		t.Fatal(e)
	}
	empty := request(v.adminBrowser, "GET", "", nil).want(t, 200)
	fields := empty.object(t)
	if len(fields) != 3 || fields["model"] != nil || fields["version"] != "1" {
		t.Fatal("null singleton projection")
	}
	meetingSummarySettingsSafeBody(t, "null", meetingSummarySettingsPath, empty)
	head := request(v.adminBrowser, "HEAD", "", nil).want(t, 200)
	if len(head.body) != 0 || head.headers.Get("Content-Length") != empty.headers.Get("Content-Length") {
		t.Fatal("HEAD differs")
	}
	plain, next := v.chat(t, false), v.chat(t, false)
	original := v.choice(t, plain)
	body := meetingSummarySettingsBody(original)
	receipt := request(v.adminBrowser, "PUT", string(original.Key), body)
	meetingSummarySettingsReceipt(t, receipt, original)
	meetingSummarySettingsSafeBody(t, "receipt", meetingSummarySettingsPath, receipt, original)
	configured := request(v.adminBrowser, "GET", "", nil).want(t, 200)
	if out := configured.object(t); out["model"] != plain.String() || out["version"] != "2" {
		t.Fatal("plain text chat not selected")
	}
	meetingSummarySettingsSafeBody(t, "configured", meetingSummarySettingsPath, configured)
	stable := v.summaryFacts(t)
	httpReceiptSame(t, receipt, request(v.adminBrowser, "PUT", string(original.Key), body).want(t, 200))
	httpSameFacts(t, stable, v.summaryFacts(t))
	same := v.choice(t, plain)
	meetingSummarySettingsReceipt(t, request(v.adminBrowser, "PUT", string(same.Key), meetingSummarySettingsBody(same)), same)
	legacy := v.request(t, v.adminBrowser, "GET", "/api/v1/system/model-selection", "", nil).want(t, 200).object(t)
	if legacy["configured"] != nil || legacy["id"] == original.SelectionID || legacy["version"] != "1" {
		t.Fatal("Summary changed old selector")
	}
	// The old seven-kind namespace must reject a different selector/body with the
	// same key rather than turn a generic lookup receipt into write permission.
	oldBody := map[string]any{"id": legacy["id"], "expected_version": "1", "embedding": v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel), "memory": v.chat(t, true).String(), "reranker": nil, "image": nil}
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-selection", string(original.Key), oldBody).problem(t, 409, f.IdempotencyKeyReused)
	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
		code   f.Code
	}{
		{"clear", map[string]any{"id": original.SelectionID, "expected_version": "3", "model": nil}, 400, f.InvalidArgument},
		{"wrong_selector", map[string]any{"id": legacy["id"], "expected_version": "1", "model": plain.String()}, 404, f.NotFound},
		{"stale", body, 409, f.VersionConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := v.summaryFacts(t)
			request(v.adminBrowser, "PUT", newID[struct{}](t).String(), tc.body).problem(t, tc.status, tc.code)
			httpSameFacts(t, before, v.summaryFacts(t))
		})
	}
	a, b := v.choice(t, plain), v.choice(t, next)
	responses := make(chan systemHTTPResponse, 2)
	var wg sync.WaitGroup
	for _, candidate := range []mc.UpdateMeetingSummarySelectionRequest{a, b} {
		wg.Add(1)
		go func(c mc.UpdateMeetingSummarySelectionRequest) {
			defer wg.Done()
			raw := summaryJSON(t, meetingSummarySettingsBody(c))
			r, abort, _ := meetingSummarySettingsRaw(testContext(t), v.http, v.adminBrowser, "PUT", meetingSummarySettingsPath, string(c.Key), raw, nil, false)
			if abort {
				t.Error("concurrent real command aborted")
			}
			responses <- r
		}(candidate)
	}
	wg.Wait()
	close(responses)
	successes := 0
	for r := range responses {
		if r.status == 200 {
			successes++
		} else {
			var p httpapi.Problem
			if json.Unmarshal(r.body, &p) != nil || r.status != 409 || (p.Code != f.VersionConflict && p.Code != f.ResourceBusy) {
				t.Fatal("uncontrolled concurrent outcome")
			}
		}
	}
	if successes != 1 {
		t.Fatal("expected version admitted two writes")
	}
	for _, method := range []string{"POST", "DELETE", "PATCH"} {
		r := request(v.adminBrowser, method, "", nil)
		r.problem(t, 405, f.MethodNotAllowed)
		if r.headers.Get("Allow") != "GET, HEAD, PUT" {
			t.Fatal("wrong Allow")
		}
	}
	for _, path := range []string{meetingSummarySettingsPath + "?", meetingSummarySettingsPath + "?private=canary"} {
		meetingSummarySettingsRequest(t, v.systemHTTPFixture, v.adminBrowser, "GET", path, "", nil).problem(t, 400, f.InvalidArgument)
	}
	// The existing Account boundary rejects noncanonical paths before routing.
	meetingSummarySettingsRequest(t, v.systemHTTPFixture, v.adminBrowser, "GET", meetingSummarySettingsPath+"/", "", nil).problem(t, 400, f.InvalidArgument)
	meetingSummarySettingsRequest(t, v.systemHTTPFixture, v.adminBrowser, "GET", meetingSummarySettingsPath+"/unknown", "", nil).problem(t, 404, f.NotFound)
	request(systemHTTPBrowser{}, "GET", "", nil).problem(t, 401, f.Unauthenticated)
	for _, tc := range []struct {
		name, method string
		change       func(*http.Request)
		status       int
		code         f.Code
	}{
		{"csrf", "PUT", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403, f.CSRFFailed},
		{"origin", "GET", func(r *http.Request) { r.Header.Set("Origin", "https://other.example") }, 403, f.OriginDenied},
		{"raw_path", "GET", func(r *http.Request) { r.URL.RawPath = r.URL.Path }, 400, f.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload []byte
			if tc.method == "PUT" {
				payload = summaryJSON(t, body)
			}
			r, aborted, _ := meetingSummarySettingsRaw(testContext(t), v.http, v.adminBrowser, tc.method, meetingSummarySettingsPath, newID[struct{}](t).String(), payload, tc.change, false)
			if aborted {
				t.Fatal("safe boundary failure aborted")
			}
			r.problem(t, tc.status, tc.code)
		})
	}
	// Auxiliary identities reuse the existing SQL seed + formal Login helper;
	// they are not claimed as an invitation/redeem acceptance result.
	regular := v.addBrowser(t, "user")
	request(regular, "GET", "", nil).problem(t, 403, f.Forbidden)
	t.Run("current_session_rechecked_in_transaction", func(t *testing.T) {
		for _, method := range []string{"GET", "PUT"} {
			browser := v.addBrowser(t, "admin")
			choice := v.choice(t, plain)
			var fired atomic.Bool
			v.tracked.setHooks(func(_ context.Context, cause f.TransactionCause) error {
				if (cause.Details().Owner == "model.meeting-summary-query" || cause.Details().Primary.Namespace() == "model.system") && fired.CompareAndSwap(false, true) {
					return v.account.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())})
				}
				return nil
			}, nil)
			var data any
			if method == "PUT" {
				data = meetingSummarySettingsBody(choice)
			}
			r := request(browser, method, string(choice.Key), data)
			v.tracked.setHooks(nil, nil)
			if !fired.Load() {
				t.Fatal("post-HTTP-auth real transaction barrier missed")
			}
			r.problem(t, 401, f.SessionRevoked)
		}
	})
	// Canonical/reference/receipt/Audit/Event all agree, not merely an HTTP 200.
	var consistent bool
	e := v.raw.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_model.meeting_summary_selection s JOIN agenteam_model.references r ON r.owner_id=s.id AND r.owner_kind='platform_selector' AND r.role='meeting_summary' AND r.model_id=s.model_id AND r.owner_version=s.version WHERE EXISTS(SELECT 1 FROM agenteam_model.commands c WHERE c.resource_id=s.id AND c.phase='committed' AND c.safe_receipt IS NOT NULL AND EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=c.event_id AND e.producer='model')) AND EXISTS(SELECT 1 FROM agenteam_audit.audit_records a WHERE a.resource_id=s.id AND a.producer='model' AND a.action='model.selection.update'))`).Scan(&consistent)
	if e != nil || !consistent {
		t.Fatal("atomic effects incomplete", e)
	}
}

func TestModelMeetingSummarySettingsRecovery(t *testing.T) {
	meetingSummarySettingsBudget(t)
	t.Run("accepted_short_response_and_historical_replay", func(t *testing.T) {
		v := newMeetingSummaryFixture(t, true)
		target, next := v.chat(t, false), v.chat(t, false)
		original := v.choice(t, target)
		body := meetingSummarySettingsBody(original)
		short, aborted, complete := meetingSummarySettingsRaw(testContext(t), v.http, v.adminBrowser, "PUT", meetingSummarySettingsPath, string(original.Key), summaryJSON(t, body), nil, true)
		if !aborted || len(short.body) != 1 || !bytes.HasPrefix(complete, short.body) {
			t.Fatal("real accepted response not truncated")
		}
		accepted := systemHTTPResponse{short.status, short.headers, complete}
		meetingSummarySettingsReceipt(t, accepted, original)
		stable := v.summaryFacts(t)
		lookup := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-commands/lookup", string(original.Key), map[string]any{"command": "model.selection.update"}).want(t, 200)
		if lookup.object(t)["found"] != true {
			t.Fatal("original lookup did not observe receipt")
		}
		httpSameFacts(t, stable, v.summaryFacts(t))
		meetingSummarySettingsSafeBody(t, "lookup", "/api/v1/system/model-commands/lookup", lookup, original)
		changed := meetingSummarySettingsBody(original)
		changed["model"] = next.String()
		meetingSummarySettingsRequest(t, v.systemHTTPFixture, v.adminBrowser, "PUT", meetingSummarySettingsPath, string(original.Key), changed).problem(t, 409, f.IdempotencyKeyReused)
		// A later disabled model is still observed, and the original receipt survives
		// both disable and formal atomic replacement/deletion at a higher version.
		view, e := v.service.GetModel(testContext(t), v.admin, target)
		if e != nil {
			t.Fatal(e)
		}
		view.Input.Enabled = false
		if _, e = v.service.UpdateModel(testContext(t), mc.UpdateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: target, ExpectedVersion: view.Version, Input: view.Input}); e != nil {
			t.Fatal(e)
		}
		current := meetingSummarySettingsRequest(t, v.systemHTTPFixture, v.adminBrowser, "GET", meetingSummarySettingsPath, "", nil).want(t, 200).object(t)
		if current["model"] != target.String() {
			t.Fatal("disabled reference erased")
		}
		impactPath := "/api/v1/system/models/" + target.String() + "/deletion-impact"
		impact := v.request(t, v.adminBrowser, "GET", impactPath, "", nil).want(t, 200)
		meetingSummarySettingsSafeBody(t, "deletion-impact", impactPath, impact)
		v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/models/"+target.String(), newID[struct{}](t).String(), map[string]any{"expected_version": view.Version + 1, "replacement": next.String()}).want(t, 200)
		stable = v.summaryFacts(t)
		replay := meetingSummarySettingsRequest(t, v.systemHTTPFixture, v.adminBrowser, "PUT", meetingSummarySettingsPath, string(original.Key), body).want(t, 200)
		httpReceiptSame(t, accepted, replay)
		httpSameFacts(t, stable, v.summaryFacts(t))
		other := v.addBrowser(t, "admin")
		otherLookup := v.request(t, other, "POST", "/api/v1/system/model-commands/lookup", string(original.Key), map[string]any{"command": "model.selection.update"}).want(t, 200).object(t)
		if otherLookup["found"] != false || otherLookup["receipt"] != nil {
			t.Fatal("cross-admin receipt disclosed")
		}
		oldSession := v.adminBrowser
		if e = v.account.Logout(testContext(t), account.LogoutRequest{Actor: oldSession.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); e != nil {
			t.Fatal(e)
		}
		meetingSummarySettingsRequest(t, v.systemHTTPFixture, oldSession, "PUT", meetingSummarySettingsPath, string(original.Key), body).problem(t, 401, f.SessionRevoked)
		renewed := v.login(t, oldSession.email)
		httpReceiptSame(t, accepted, meetingSummarySettingsRequest(t, v.systemHTTPFixture, renewed, "PUT", meetingSummarySettingsPath, string(original.Key), body).want(t, 200))
		t.Log("real accepted command with controlled truncated writer; lookup read-only; exact receipt retained across disable/delete/replacement and formal new Session")
	})
	t.Run("original_commit_ack_loss_via_http", func(t *testing.T) {
		base := newMeetingSummaryFixture(t, true)
		target := base.chat(t, false)
		original := base.choice(t, target)
		identity, e := f.NewCommandIdentity("model.system", []string{base.admin.Details().UserID}, "model.selection.update", original.Key)
		if e != nil {
			t.Fatal(e)
		}
		trace := newProjectModelPGTrace(t, net.JoinHostPort("127.0.0.1", base.db.Fixture.Port))
		u, e := url.Parse(base.db.Fixture.URL(base.db.Name))
		if e != nil {
			t.Fatal(e)
		}
		u.Host = trace.listener.Addr().String()
		raw := openStore(t, base.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
		writer := &meetingSummaryCommitStore{Store: raw, target: identity.Canonical(), arm: func() { trace.dropCommitACK.Store(true) }, reached: make(chan struct{}), release: make(chan struct{})}
		candidate := assemble(t, base.db, raw, writer)
		handler, e := model.NewSystemHTTPHandler(candidate.service, base.account, base.secrets, model.SystemHTTPOptions{PublicOrigin: systemHTTPOrigin})
		if e != nil {
			t.Fatal(e)
		}
		var once sync.Once
		release := func() { once.Do(func() { close(writer.release) }) }
		defer release()
		ctx, cancel := context.WithTimeout(testContext(t), 8*time.Second)
		defer cancel()
		type result struct {
			response systemHTTPResponse
			aborted  bool
		}
		done := make(chan result, 1)
		var joined sync.Once
		var got result
		joinCommand := func() { joined.Do(func() { got = <-done }) }
		t.Cleanup(func() { release(); cancel(); joinCommand() })
		payload := summaryJSON(t, meetingSummarySettingsBody(original))
		go func() {
			r, abort, _ := meetingSummarySettingsRaw(ctx, httpapi.Handler(nil, handler), base.adminBrowser, "PUT", meetingSummarySettingsPath, string(original.Key), payload, nil, false)
			done <- result{r, abort}
		}()
		waitSignal(t, trace.ackDropped)
		waitSignal(t, writer.reached)
		release()
		joinCommand()
		writer.mu.Lock()
		state := writer.original.State()
		writer.mu.Unlock()
		if state != f.Unknown || !writer.fired.Load() || got.aborted {
			t.Fatal("original real Unknown not observed/recovered")
		}
		meetingSummarySettingsReceipt(t, got.response, original)
		stable := base.summaryFacts(t)
		lookup := base.request(t, base.adminBrowser, "POST", "/api/v1/system/model-commands/lookup", string(original.Key), map[string]any{"command": "model.selection.update"}).want(t, 200)
		if lookup.object(t)["found"] != true {
			t.Fatal("recovered receipt missing")
		}
		httpSameFacts(t, stable, base.summaryFacts(t))
		replay := meetingSummarySettingsRequest(t, base.systemHTTPFixture, base.adminBrowser, "PUT", meetingSummarySettingsPath, string(original.Key), meetingSummarySettingsBody(original)).want(t, 200)
		httpReceiptSame(t, got.response, replay)
		httpSameFacts(t, stable, base.summaryFacts(t))
		t.Log("original PG COMMIT/idle ACK dropped; real Unknown confirmed by existing service; HTTP returned original receipt, actual handler joined")
	})
	t.Run("pending_original_writer_then_lookup_and_replay", func(t *testing.T) {
		base := newMeetingSummaryFixture(t, true)
		target := base.chat(t, false)
		original := base.choice(t, target)
		identity, e := f.NewCommandIdentity("model.system", []string{base.admin.Details().UserID}, "model.selection.update", original.Key)
		if e != nil {
			t.Fatal(e)
		}
		held := newModelCommitProxy(t, net.JoinHostPort("127.0.0.1", base.db.Fixture.Port), false)
		u, e := url.Parse(base.db.Fixture.URL(base.db.Name))
		if e != nil {
			t.Fatal(e)
		}
		u.Host = held.listener.Addr().String()
		raw := openStore(t, base.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
		writer := &meetingSummaryCommitStore{Store: raw, target: identity.Canonical(), arm: func() { held.armed.Store(true) }, reached: make(chan struct{}), release: make(chan struct{})}
		candidate := assemble(t, base.db, raw, writer)
		handler, e := model.NewSystemHTTPHandler(candidate.service, base.account, base.secrets, model.SystemHTTPOptions{PublicOrigin: systemHTTPOrigin})
		if e != nil {
			t.Fatal(e)
		}
		var writerOnce, confirmOnce sync.Once
		releaseWriter := func() { writerOnce.Do(func() { close(held.release) }) }
		releaseConfirmation := func() { confirmOnce.Do(func() { close(writer.release) }) }
		defer releaseWriter()
		defer releaseConfirmation()
		ctx, cancel := context.WithTimeout(testContext(t), 8*time.Second)
		defer cancel()
		type result struct {
			response systemHTTPResponse
			aborted  bool
		}
		done := make(chan result, 1)
		var got result
		var joined sync.Once
		join := func() { joined.Do(func() { got = <-done }) }
		t.Cleanup(func() { releaseConfirmation(); releaseWriter(); cancel(); join() })
		payload := summaryJSON(t, meetingSummarySettingsBody(original))
		go func() {
			r, abort, _ := meetingSummarySettingsRaw(ctx, httpapi.Handler(nil, handler), base.adminBrowser, "PUT", meetingSummarySettingsPath, string(original.Key), payload, nil, false)
			done <- result{r, abort}
		}()
		waitSignal(t, held.reached)
		waitSignal(t, writer.reached)
		releaseConfirmation()
		join()
		writer.mu.Lock()
		state := writer.original.State()
		writer.mu.Unlock()
		var problem httpapi.Problem
		if got.aborted || got.response.status != 503 || json.Unmarshal(got.response.body, &problem) != nil || problem.CommitState != f.Unknown || state != f.Unknown {
			t.Fatal("pending original Unknown classification lost")
		}
		var wire map[string]any
		if json.Unmarshal(got.response.body, &wire) != nil || wire["retry_hint"] != "lookup" {
			t.Fatal("Unknown original lookup hint lost")
		}
		meetingSummarySettingsSafeBody(t, "unknown", meetingSummarySettingsPath, got.response, original)
		releaseWriter()
		waitSignal(t, held.completed)
		key, e := f.CommandLock(identity)
		if e != nil {
			t.Fatal(e)
		}
		terminal := base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
			return base.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}})
		})
		if terminal.State() != f.Committed {
			t.Fatal("original writer did not actually release command lock", terminal.Fault())
		}
		stable := base.summaryFacts(t)
		lookupResponse := base.request(t, base.adminBrowser, "POST", "/api/v1/system/model-commands/lookup", string(original.Key), map[string]any{"command": "model.selection.update"}).want(t, 200)
		lookup := lookupResponse.object(t)
		if lookup["found"] != false || lookup["receipt"] != nil || base.summary(t).Model != nil {
			t.Fatal("rolled-back writer became receipt")
		}
		httpSameFacts(t, stable, base.summaryFacts(t))
		meetingSummarySettingsSafeBody(t, "lookup-false", "/api/v1/system/model-commands/lookup", lookupResponse, original)
		replay := meetingSummarySettingsRequest(t, base.systemHTTPFixture, base.adminBrowser, "PUT", meetingSummarySettingsPath, string(original.Key), meetingSummarySettingsBody(original))
		meetingSummarySettingsReceipt(t, replay, original)
		meetingSummarySettingsSafeBody(t, "replay", meetingSummarySettingsPath, replay, original)
		t.Log("original actual final COMMIT remained pending through HTTP503/Unknown; original writer then actually ended, read-only lookup returned false, exact original PUT applied once")
	})

}
