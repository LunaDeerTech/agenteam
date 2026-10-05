//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type systemHTTPWrites struct {
	sc.HumanWriteCommands
	afterMetadata   func(sc.Metadata)
	beforeLookup    func()
	writes, lookups atomic.Int64
}

func (w *systemHTTPWrites) Metadata(ctx context.Context, a id.Actor, r sc.CredentialRef) (sc.Metadata, error) {
	out, err := w.HumanWriteCommands.Metadata(ctx, a, r)
	if err == nil && w.afterMetadata != nil {
		w.afterMetadata(out)
	}
	return out, err
}
func (w *systemHTTPWrites) LookupWriteCommand(ctx context.Context, r sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
	w.lookups.Add(1)
	if w.beforeLookup != nil {
		w.beforeLookup()
	}
	return w.HumanWriteCommands.LookupWriteCommand(ctx, r)
}
func (w *systemHTTPWrites) ExecuteWrite(ctx context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
	w.writes.Add(1)
	return w.HumanWriteCommands.ExecuteWrite(ctx, r)
}

func systemLookup(t *testing.T, b systemHTTPBrowser, key string, kind sc.MutationKind, credential string, expected f.Version) sc.WriteCommandLookupRequest {
	t.Helper()
	command, err := f.NewCommandIdentity("secret", []string{b.actor.Details().UserID}, string(kind), f.IdempotencyKey(key))
	if err != nil {
		t.Fatal(err)
	}
	r := sc.WriteCommandLookupRequest{Actor: b.actor, Scope: id.SystemScope(), Identity: command, Kind: kind, Purpose: sc.Model, ExpectedVersion: expected}
	if credential != "" {
		parsed, err := f.ParseID[sc.Credential](credential)
		if err != nil {
			t.Fatal(err)
		}
		r.Ref, _ = sc.NewCredentialRef(parsed, r.Scope)
	}
	return r
}
func (v *systemHTTPFixture) secretWrite(t *testing.T, b systemHTTPBrowser, key string, kind sc.MutationKind, credential string, expected f.Version, purpose sc.Purpose, value string) sc.MutationResult {
	t.Helper()
	lookup := systemLookup(t, b, key, kind, credential, expected)
	var material sc.SecretMaterial
	var err error
	if kind != sc.Delete {
		material, err = sc.NewSecretMaterial([]byte(value))
		if err != nil {
			t.Fatal(err)
		}
		defer material.Destroy()
	}
	result, err := v.secrets.ExecuteWrite(testContext(t), sc.WriteRequest{Actor: lookup.Actor, Scope: lookup.Scope, Identity: lookup.Identity, Kind: kind, Ref: lookup.Ref, ExpectedVersion: expected, Purpose: purpose, Value: material})
	if err != nil {
		t.Fatal("real Human write", err)
	}
	return result
}
func (v *systemHTTPFixture) nonce(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := v.raw.QueryRow(testContext(t), `SELECT nonce_high_water FROM agenteam_secret.secret_master_registry WHERE version=1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestModelSystemHTTPCredentialWritesAndBinding(t *testing.T) {
	v := newSystemHTTPFixture(t)
	key := newID[struct{}](t).String()
	credential := v.credential(t, key, "credential-value-sentinel")
	lookup := systemLookup(t, v.adminBrowser, key, sc.Create, "", 0)
	observed, err := v.secrets.LookupWriteCommand(testContext(t), lookup)
	if err != nil || !observed.Observed || observed.Result.Metadata.CredentialRef.Details().ID.String() != credential {
		t.Fatal("real receipt", err)
	}
	var plain bool
	if err = v.raw.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads WHERE position(convert_to('credential-value-sentinel','UTF8') in ciphertext)>0)`).Scan(&plain); err != nil || plain {
		t.Fatal("value not protected by AEAD", err)
	}
	body := httpProviderBody(mc.OpenAIChat)
	body["input"].(map[string]any)["credential_ref"] = credential
	provider := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", newID[struct{}](t).String(), body).want(t, 200).object(t)["resource_id"].(string)
	v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/model-credentials/"+credential, newID[struct{}](t).String(), map[string]any{"expected_version": "1"}).problem(t, 409, f.ResourceBusy)
	v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/model-providers/"+provider, newID[struct{}](t).String(), map[string]any{"expected_version": "1"}).want(t, 200)
	metadata, err := v.secrets.Metadata(testContext(t), v.admin, observed.Result.Metadata.CredentialRef)
	if err != nil || metadata.Version != 1 {
		t.Fatal("provider deletion deleted credential", err)
	}
	other := v.secretWrite(t, v.adminBrowser, newID[struct{}](t).String(), sc.Create, "", 0, sc.SMTP, "smtp-only-sentinel").Metadata.CredentialRef.Details().ID.String()
	for _, method := range []string{"PUT", "DELETE"} {
		t.Run("non-model-"+method, func(t *testing.T) {
			before := v.facts(t)
			input := map[string]any{"expected_version": "1"}
			if method == "PUT" {
				input["value"] = "new-sentinel"
			}
			v.request(t, v.adminBrowser, method, "/api/v1/system/model-credentials/"+other, newID[struct{}](t).String(), input).problem(t, 404, f.NotFound)
			httpSameFacts(t, before, v.facts(t))
		})
	}
	body["input"].(map[string]any)["credential_ref"] = other
	before := v.facts(t)
	v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", newID[struct{}](t).String(), body).problem(t, 403, f.Forbidden)
	httpSameFacts(t, before, v.facts(t))
	for _, mode := range []string{"purpose", "version"} {
		t.Run("metadata-race-"+mode, func(t *testing.T) {
			ref := v.credential(t, newID[struct{}](t).String(), "before-race")
			var fired atomic.Bool
			var afterOther map[string][32]byte
			decorator := &systemHTTPWrites{HumanWriteCommands: v.secrets, afterMetadata: func(m sc.Metadata) {
				if !fired.CompareAndSwap(false, true) {
					return
				}
				if m.Version != 1 || m.Purpose != sc.Model {
					t.Fatal("barrier not after valid Model metadata")
				}
				purpose := sc.Model
				if mode == "purpose" {
					purpose = sc.SMTP
				}
				v.secretWrite(t, v.adminBrowser, newID[struct{}](t).String(), sc.Update, ref, 1, purpose, "concurrent-actual-value")
				afterOther = v.facts(t)
			}}
			v.install(t, decorator)
			v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-credentials/"+ref, newID[struct{}](t).String(), map[string]any{"expected_version": "1", "value": "late-http-value"}).problem(t, 409, f.VersionConflict)
			v.install(t, v.secrets)
			if !fired.Load() || decorator.writes.Load() != 1 {
				t.Fatal("metadata race/exact one write not reached")
			}
			httpSameFacts(t, afterOther, v.facts(t))
			t.Log("real Metadata version=1; concurrent Human update committed version=2; original HTTP expected=1 rejected with no additional business facts")
		})
	}
	if strings.Contains(v.log.text(), "credential-value-sentinel") || strings.Contains(v.log.text(), "smtp-only-sentinel") {
		t.Fatal("material leaked")
	}
}

func TestModelSystemHTTPPassiveCredentialLookup(t *testing.T) {
	v := newSystemHTTPFixture(t)
	createKey, updateKey, deleteKey := newID[struct{}](t).String(), newID[struct{}](t).String(), newID[struct{}](t).String()
	credential := v.credential(t, createKey, "initial")
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-credentials/"+credential, updateKey, map[string]any{"expected_version": "1", "value": "updated"}).want(t, 200)
	v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/model-credentials/"+credential, deleteKey, map[string]any{"expected_version": "2"}).want(t, 200)
	other := v.addBrowser(t, "admin")
	sameUser := v.login(t, v.adminBrowser.email)
	before, n := v.facts(t), v.nonce(t)
	decorator := &systemHTTPWrites{HumanWriteCommands: v.secrets}
	v.install(t, decorator)
	for _, tc := range []struct {
		kind, key, expected, version string
		deleted                      bool
	}{{"create", createKey, "", "1", false}, {"update", updateKey, "1", "2", false}, {"delete", deleteKey, "2", "3", true}} {
		input := map[string]any{"kind": tc.kind}
		if tc.expected != "" {
			input["credential_id"], input["expected_version"] = credential, tc.expected
		}
		out := v.request(t, sameUser, "POST", "/api/v1/system/model-credential-commands/lookup", tc.key, input).want(t, 200).object(t)
		if out["observed"] != true {
			t.Fatal("history absent")
		}
		result := out["result"].(map[string]any)
		if result["version"] != tc.version || result["deleted"] != tc.deleted || result["credential_id"] != credential || result["purpose"] != "model" || len(result) != 4 {
			t.Fatal("historical safe result")
		}
		absent := v.request(t, other, "POST", "/api/v1/system/model-credential-commands/lookup", tc.key, input).want(t, 200).object(t)
		if absent["observed"] != false || absent["result"] != nil {
			t.Fatal("other user receipt")
		}
	}
	for _, tc := range []struct {
		key    string
		input  map[string]any
		status int
		code   f.Code
	}{{"never-seen", map[string]any{"kind": "create"}, 200, ""}, {updateKey, map[string]any{"kind": "update", "credential_id": newID[sc.Credential](t).String(), "expected_version": "1"}, 409, f.IdempotencyKeyReused}, {updateKey, map[string]any{"kind": "update", "credential_id": credential, "expected_version": "2"}, 409, f.IdempotencyKeyReused}, {createKey, map[string]any{"kind": "update", "credential_id": credential, "expected_version": "1"}, 200, ""}, {createKey, map[string]any{"kind": "create", "purpose": "smtp"}, 400, f.InvalidArgument}} {
		response := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-credential-commands/lookup", tc.key, tc.input)
		if tc.status == 200 {
			out := response.want(t, 200).object(t)
			if out["observed"] != false || out["result"] != nil {
				t.Fatal("absence meaning")
			}
		} else {
			response.problem(t, tc.status, tc.code)
		}
	}
	if decorator.writes.Load() != 0 {
		t.Fatal("lookup executed a write")
	}
	httpSameFacts(t, before, v.facts(t))
	if v.nonce(t) != n {
		t.Fatal("passive query reserved nonce")
	}
	seen := 0
	for _, trace := range v.tracked.observations() {
		if trace.owner == "secret-write-lookup" {
			seen++
			if trace.payloadReads != 0 || trace.held == 0 || len(trace.locks) != 2 || trace.locks[0].Mode != f.Shared || trace.locks[1].Mode != f.Shared {
				t.Fatal("passive query decrypted or missing actual locks")
			}
		}
	}
	if seen == 0 {
		t.Fatal("no actual passive Tx")
	}
	t.Logf("passive read transactions=%d, ExecuteWrite=0, payload/digest reads=0; complete Secret/Audit/Outbox facts and nonce high water unchanged", seen)
}

func TestModelSystemHTTPPassiveLookupLocksAndFailures(t *testing.T) {
	v := newSystemHTTPFixture(t)
	key := newID[struct{}](t).String()
	credential := v.credential(t, key, "history")
	request := systemLookup(t, v.adminBrowser, key, sc.Create, "", 0)
	t.Run("uninitialized-read", func(t *testing.T) {
		_, _, keys := testKeys(t)
		service, err := secret.New(v.tracked, keys, v.aud, secret.Authorizations{Sessions: v.accounts, System: v.accounts})
		if err != nil {
			t.Fatal(err)
		}
		before, n := v.facts(t), v.nonce(t)
		out, err := service.LookupWriteCommand(testContext(t), request)
		if err != nil || !out.Observed || out.Result.Metadata.CredentialRef.Details().ID.String() != credential {
			t.Fatal("read required writable state", err)
		}
		if service.Status().Available {
			t.Fatal("test initialized writer")
		}
		httpSameFacts(t, before, v.facts(t))
		if v.nonce(t) != n {
			t.Fatal("nonce on read")
		}
	})
	for _, kind := range []string{"command", "user"} {
		t.Run("real-lock-"+kind, func(t *testing.T) {
			lock, _ := f.CommandLock(request.Identity)
			if kind == "user" {
				lock, _ = f.UserLock(request.Actor.Details().UserID)
			}
			held, release, done := make(chan struct{}), make(chan struct{}), make(chan f.CommitResult, 1)
			go func() {
				done <- v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
					if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}); err != nil {
						return err
					}
					close(held)
					<-release
					return nil
				})
			}()
			waitSignal(t, held)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			out, err := v.secrets.LookupWriteCommand(ctx, request)
			cancel()
			close(release)
			if result := <-done; result.State() != f.Committed {
				t.Fatal(result.Fault())
			}
			if err == nil || out.Observed || out.Result != nil {
				t.Fatal("held lock disguised as absence")
			}
		})
	}
	for _, mode := range []string{"not-committed", "unknown", "cancel-committed-present", "cancel-committed-absent"} {
		t.Run(mode, func(t *testing.T) {
			before, n := v.facts(t), v.nonce(t)
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			var reached atomic.Bool
			if mode == "not-committed" {
				v.tracked.setHooks(func(_ context.Context, cause f.TransactionCause) error {
					if cause.Details().Owner == "secret-write-lookup" {
						reached.Store(true)
						return f.NewFault(f.DependencyUnavailable, f.NotStarted)
					}
					return nil
				}, nil)
			} else {
				v.tracked.setHooks(nil, func(_ context.Context, cause f.TransactionCause, trace systemHTTPTrace, r f.CommitResult) f.CommitResult {
					if trace.owner == "secret-write-lookup" {
						if r.State() != f.Committed {
							t.Fatal("read did not really commit")
						}
						reached.Store(true)
						t.Logf("actual passive read CommitResult=%s; target=%s receiptReads=%d heldChecks=%d", r.State(), mode, trace.receiptReads, trace.held)
						if mode == "unknown" {
							return f.UnknownResult(newID[f.TransactionAttempt](t), cause)
						}
						cancel()
					}
					return r
				})
			}
			input := request
			if strings.HasSuffix(mode, "absent") {
				input = systemLookup(t, v.adminBrowser, "absent-command", sc.Create, "", 0)
			}
			out, err := v.secrets.LookupWriteCommand(ctx, input)
			v.tracked.setHooks(nil, nil)
			if !reached.Load() || err == nil || out.Observed || out.Result != nil {
				t.Fatal("failed observation escaped", err)
			}
			var fault *f.Fault
			if !errors.As(err, &fault) {
				t.Fatal("missing foundation fault")
			}
			if strings.HasPrefix(mode, "cancel-") && fault.CommitState != f.Committed {
				t.Fatal("read commit state lost")
			}
			if mode == "unknown" && fault.CommitState != f.Unknown {
				t.Fatal("Unknown reclassified")
			}
			httpSameFacts(t, before, v.facts(t))
			if v.nonce(t) != n {
				t.Fatal("failed lookup writes")
			}
		})
	}
	t.Run("current-after-entry", func(t *testing.T) {
		browser := v.addBrowser(t, "admin")
		ownedKey := newID[struct{}](t).String()
		owned := v.request(t, browser, "POST", "/api/v1/system/model-credentials", ownedKey, map[string]any{"value": "owned"}).want(t, 200)
		_ = owned
		var fired atomic.Bool
		port := &systemHTTPWrites{HumanWriteCommands: v.secrets, beforeLookup: func() {
			if fired.CompareAndSwap(false, true) {
				if err := v.account.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())}); err != nil {
					t.Fatal(err)
				}
			}
		}}
		v.install(t, port)
		v.request(t, browser, "POST", "/api/v1/system/model-credential-commands/lookup", ownedKey, map[string]any{"kind": "create"}).problem(t, 401, f.SessionRevoked)
		v.install(t, v.secrets)
		if !fired.Load() || port.writes.Load() != 0 {
			t.Fatal("current receipt authorization not reached")
		}
	})
}

func TestModelSystemHTTPUnknownAndHistoricalResults(t *testing.T) {
	v := newSystemHTTPFixture(t)
	for _, confirmed := range []bool{true, false} {
		name := "model-confirmed"
		if !confirmed {
			name = "model-still-unknown"
		}
		t.Run(name, func(t *testing.T) {
			key := newID[struct{}](t).String()
			var projected atomic.Bool
			var confirmations atomic.Int64
			v.tracked.setHooks(func(_ context.Context, cause f.TransactionCause) error {
				c := cause.Details().Primary
				if !confirmed && projected.Load() && c.Namespace() == "model.system" && string(c.Key()) == key {
					confirmations.Add(1)
					return f.NewFault(f.DependencyUnavailable, f.NotStarted)
				}
				return nil
			}, func(_ context.Context, cause f.TransactionCause, trace systemHTTPTrace, r f.CommitResult) f.CommitResult {
				c := cause.Details().Primary
				if c.Namespace() == "model.system" && string(c.Key()) == key && trace.finalFact && r.State() == f.Committed && projected.CompareAndSwap(false, true) {
					t.Logf("Model final canonical/receipt Tx actual=%s -> formal Store Unknown", r.State())
					return f.UnknownResult(newID[f.TransactionAttempt](t), cause)
				}
				if c.Namespace() == "model.system" && string(c.Key()) == key && projected.Load() {
					confirmations.Add(1)
				}
				return r
			})
			response := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", key, httpProviderBody(mc.OpenAIChat))
			v.tracked.setHooks(nil, nil)
			if !projected.Load() || confirmations.Load() == 0 {
				t.Fatal("semantic commit/recovery not reached")
			}
			if confirmed {
				response.want(t, 200)
				t.Log("original replayScope confirmation succeeded; service/HTTP 200 original receipt")
			} else {
				response.problem(t, 503, f.CommitUnknown)
				var p httpapi.Problem
				_ = json.Unmarshal(response.body, &p)
				if p.RetryHint != "lookup" {
					t.Fatal("wrong retry hint")
				}
				t.Log("original replayScope confirmation rejected by bounded formal Store result; service/HTTP remained Unknown/503")
			}
			lookup := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-commands/lookup", key, map[string]any{"command": "provider.create"}).want(t, 200).object(t)
			if lookup["found"] != true {
				t.Fatal("committed original missing")
			}
			if confirmed && !reflect.DeepEqual(response.object(t), lookup["receipt"]) {
				t.Fatal("Model receipt mismatch")
			}
		})
	}
	createKey, updateKey, deleteKey := newID[struct{}](t).String(), newID[struct{}](t).String(), newID[struct{}](t).String()
	var projected atomic.Bool
	port := &systemHTTPWrites{HumanWriteCommands: v.secrets}
	v.install(t, port)
	v.tracked.setHooks(nil, func(_ context.Context, cause f.TransactionCause, trace systemHTTPTrace, r f.CommitResult) f.CommitResult {
		if trace.namespace == "secret" && trace.command == "create" && string(cause.Details().Primary.Key()) == createKey && trace.finalFact && r.State() == f.Committed && projected.CompareAndSwap(false, true) {
			if trace.commandRawHash == trace.receiptDigest || trace.held == 0 {
				t.Fatal("Secret final receipt digest/held-lock evidence missing")
			}
			t.Logf("Secret final mutation/receipt Tx actual=%s -> formal Store Unknown; raw identity hash=%s canonical receipt digest=%s heldChecks=%d", r.State(), trace.commandRawHash, trace.receiptDigest, trace.held)
			return f.UnknownResult(newID[f.TransactionAttempt](t), cause)
		}
		return r
	})
	response := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-credentials", createKey, map[string]any{"value": "original-value"})
	v.tracked.setHooks(nil, nil)
	if !projected.Load() {
		t.Fatal("Secret actual mutation not targeted")
	}
	if port.writes.Load() != 1 {
		t.Fatal("Secret Unknown was automatically replayed")
	}
	response.problem(t, 503, f.CommitUnknown)
	t.Log("Secret service/HTTP directly returned Unknown/503 with exactly one ExecuteWrite")
	lookup := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-credential-commands/lookup", createKey, map[string]any{"kind": "create"}).want(t, 200).object(t)
	credential := lookup["result"].(map[string]any)["credential_id"].(string)
	update := v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-credentials/"+credential, updateKey, map[string]any{"expected_version": "1", "value": "updated-value"}).want(t, 200)
	deleted := v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/model-credentials/"+credential, deleteKey, map[string]any{"expected_version": "2"}).want(t, 200)
	before := v.facts(t)
	createReplay := v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-credentials", createKey, map[string]any{"value": "original-value"}).want(t, 200).object(t)
	if createReplay["credential_id"] != credential || createReplay["version"] != "1" || createReplay["deleted"] != false {
		t.Fatal("create history changed")
	}
	httpReceiptSame(t, update, v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-credentials/"+credential, updateKey, map[string]any{"expected_version": "1", "value": "updated-value"}).want(t, 200))
	httpReceiptSame(t, deleted, v.request(t, v.adminBrowser, "DELETE", "/api/v1/system/model-credentials/"+credential, deleteKey, map[string]any{"expected_version": "2"}).want(t, 200))
	v.request(t, v.adminBrowser, "PUT", "/api/v1/system/model-credentials/"+credential, updateKey, map[string]any{"expected_version": "1", "value": "different-value"}).problem(t, 409, f.IdempotencyKeyReused)
	httpSameFacts(t, before, v.facts(t))
	t.Log("deleted credential create/update/delete historical receipt replay preserved; changed material same identity rejected by original ExecuteWrite digest")
	if err := v.account.Logout(testContext(t), account.LogoutRequest{Actor: v.admin, Key: f.IdempotencyKey(newID[struct{}](t).String())}); err != nil {
		t.Fatal(err)
	}
	v.request(t, v.adminBrowser, http.MethodPost, "/api/v1/system/model-credential-commands/lookup", createKey, map[string]any{"kind": "create"}).problem(t, 401, f.SessionRevoked)
}
