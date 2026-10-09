//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Independent assertions use the real fixture's Account/Project setup and the
// physical COMMIT proxy only as infrastructure. Each oracle below reads the
// returned typed receipt and canonical database facts itself.
func independentHTTPMutation(t *testing.T, r variableHTTPResponse) vc.VariableMutation {
	t.Helper()
	var m vc.VariableMutation
	if r.aborted || r.status != http.StatusOK || json.Unmarshal(r.body, &m) != nil || m.Validate() != nil {
		t.Fatalf("independent mutation boundary status=%d aborted=%t", r.status, r.aborted)
	}
	return m
}
func independentHTTPProblem(t *testing.T, r variableHTTPResponse, code f.Code) httpapi.Problem {
	t.Helper()
	var p httpapi.Problem
	if r.aborted || json.Unmarshal(r.body, &p) != nil || p.Code != code || p.Status != r.status {
		t.Fatal("independent rejection boundary", r.status, code)
	}
	return p
}
func independentHTTPReceipt(t *testing.T, r variableHTTPResponse, want vc.VariableMutation) {
	t.Helper()
	var got vc.VariableCommandLookup
	if r.aborted || r.status != 200 || json.Unmarshal(r.body, &got) != nil || got.Validate() != nil || got.Status() != vc.LookupCommitted || got.Receipt() == nil {
		t.Fatal("independent original intent not committed")
	}
	a, e := json.Marshal(want)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(got.Receipt())
	if e != nil || !bytes.Equal(a, b) {
		t.Fatal("independent historical receipt changed")
	}
}
func independentFacts(t *testing.T, v *variableHTTPFixture) (commands, history, audits, events, generation int64) {
	t.Helper()
	e := v.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1 AND state='completed'),(SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='projectvariable'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='projectvariable'),COALESCE((SELECT query_generation FROM agenteam_projectvariable.project_generations WHERE project_id=$1),1)`, v.project.ID.String()).Scan(&commands, &history, &audits, &events, &generation)
	if e != nil {
		t.Fatal("independent durable observation failed", e)
	}
	return
}

type independentUnreadBody struct{ reads atomic.Int32 }

func (b *independentUnreadBody) Read([]byte) (int, error) {
	b.reads.Add(1)
	return 0, io.ErrUnexpectedEOF
}
func (*independentUnreadBody) Close() error { return nil }

func TestIndependentProjectVariableHTTPIntentAndCommit(t *testing.T) {
	t.Run("current-account-before-decode-and-original-history", func(t *testing.T) {
		v := newVariableHTTPFixture(t)
		path := variableHTTPPath(v.project.ID, "/variables")
		for _, which := range []string{"anonymous", "missing_csrf", "revoked"} {
			b := v.ownerBrowser
			code := f.CSRFFailed
			switch which {
			case "anonymous":
				b = variableHTTPBrowser{}
				code = f.Unauthenticated
			case "missing_csrf":
				b.csrf = ""
			case "revoked":
				b = v.login(t, v.ownerBrowser.email)
				v.revoke(t, b)
				code = f.SessionRevoked
			}
			r := variableHTTPRequest(ctxFor(t), b, "POST", path, "{}", "independent-priority")
			body := &independentUnreadBody{}
			r.Body = body
			independentHTTPProblem(t, v.serve(r), code)
			if body.reads.Load() != 0 {
				t.Fatal("Account denial read private input")
			}
		}
		target := id[identity.ProjectVariable](t)
		name, value := "Independent_Ordinary", "independent-original-value-canary"
		createKey := f.IdempotencyKey(id[struct{}](t).String())
		create := map[string]any{"variable_id": target, "name": name, "description": "description-original", "value": value}
		original := independentHTTPMutation(t, v.request(t, v.ownerBrowser, "POST", path, string(jsonBytes(t, map[string]any{"request": create})), createKey))
		if d := original.Fields().Variable.Fields(); d.ID != target || d.Version != 1 || d.Value != value {
			t.Fatal("independent create projection")
		}
		patch := map[string]any{"value": "independent-later-value", "description": ""}
		updateKey := f.IdempotencyKey(id[struct{}](t).String())
		updated := independentHTTPMutation(t, v.request(t, v.ownerBrowser, "PATCH", path+"/"+target.String(), string(jsonBytes(t, map[string]any{"expected_version": "1", "request": patch})), updateKey))
		if d := updated.Fields().Variable.Fields(); d.Version != 2 || d.Description != "" {
			t.Fatal("independent update presence")
		}
		deleteKey := f.IdempotencyKey(id[struct{}](t).String())
		deleted := independentHTTPMutation(t, v.request(t, v.ownerBrowser, "DELETE", path+"/"+target.String(), `{"expected_version":"2"}`, deleteKey))
		if deleted.Fields().Deleted == nil || deleted.Fields().Deleted.Version != 3 {
			t.Fatal("independent tombstone")
		}
		otherID := id[identity.ProjectVariable](t)
		reused := map[string]any{"variable_id": otherID, "name": name, "description": "", "value": "replacement-current"}
		independentHTTPMutation(t, v.request(t, v.ownerBrowser, "POST", path, string(jsonBytes(t, map[string]any{"request": reused})), f.IdempotencyKey(id[struct{}](t).String())))
		fresh := v.login(t, v.ownerBrowser.email)
		for _, saved := range []struct {
			key     f.IdempotencyKey
			body    map[string]any
			receipt vc.VariableMutation
		}{
			{createKey, map[string]any{"command": vc.CreateCommand, "request": create}, original},
			{updateKey, map[string]any{"command": vc.UpdateCommand, "target_id": target, "expected_version": "1", "request": patch}, updated},
			{deleteKey, map[string]any{"command": vc.DeleteCommand, "target_id": target, "expected_version": "2"}, deleted},
		} {
			independentHTTPReceipt(t, v.request(t, fresh, "POST", path+"/commands/lookup", string(jsonBytes(t, saved.body)), saved.key), saved.receipt)
		}
		// An explicitly changed version is a different intent; the current row
		// and same-name replacement cannot repair it into the old command.
		wrong := map[string]any{"command": vc.UpdateCommand, "target_id": target, "expected_version": "3", "request": patch}
		independentHTTPProblem(t, v.request(t, fresh, "POST", path+"/commands/lookup", string(jsonBytes(t, wrong)), updateKey), f.IdempotencyKeyReused)
		lookupBody := string(jsonBytes(t, map[string]any{"command": vc.CreateCommand, "request": create}))
		for _, b := range []variableHTTPBrowser{v.otherBrowser, v.adminBrowser} {
			r := v.request(t, b, "POST", path+"/commands/lookup", lookupBody, createKey)
			independentHTTPProblem(t, r, f.NotFound)
			if bytes.Contains(r.body, []byte(value)) {
				t.Fatal("historical value escaped current ownership")
			}
		}
		v.revoke(t, v.ownerBrowser)
		independentHTTPProblem(t, v.request(t, v.ownerBrowser, "POST", path+"/commands/lookup", lookupBody, createKey), f.SessionRevoked)
		independentHTTPReceipt(t, v.request(t, fresh, "POST", path+"/commands/lookup", lookupBody, createKey), original)
		var version int64
		var tombstone bool
		var erased, description string
		if e := v.raw.QueryRow(ctxFor(t), `SELECT version,deleted_at IS NOT NULL,value,description FROM agenteam_projectvariable.variables WHERE project_id=$1 AND id=$2`, v.project.ID.String(), target.String()).Scan(&version, &tombstone, &erased, &description); e != nil || version != 3 || !tombstone || erased != "" || description != "" {
			t.Fatal("independent canonical tombstone mismatch", e)
		}
		c, h, a, ev, g := independentFacts(t, v)
		if c != 4 || h != 4 || a != 4 || ev != 4 || g != 5 {
			t.Fatal("independent facts were duplicated or missing", c, h, a, ev, g)
		}
	})
	t.Run("actual-commit-confirmation-rechecks-revoked-session", func(t *testing.T) {
		v := newVariableHTTPFixture(t)
		i := v.intent(t, vc.UpdateCommand)
		service, store, proxy := proxyVariableService(t, v, true)
		v.bindService(t, service)
		defer v.bindService(t, v.service)
		_, original := armVariableProxy(t, store, proxy, i, "completed")
		confirmEntered := make(chan struct{})
		confirmRelease := make(chan struct{})
		var confirmOnce, releaseOnce sync.Once
		releaseConfirm := func() { releaseOnce.Do(func() { close(confirmRelease) }) }
		defer releaseConfirm()
		store.mu.Lock()
		before := store.beforeLocks
		store.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
			if e := before(ctx, tx, locks); e != nil {
				return e
			}
			select {
			case <-proxy.reached:
				confirmOnce.Do(func() {
					close(confirmEntered)
					select {
					case <-confirmRelease:
					case <-ctx.Done():
					}
				})
			default:
			}
			return nil
		}
		store.mu.Unlock()
		var proxyOnce sync.Once
		releaseProxy := func() { proxyOnce.Do(func() { close(proxy.release) }) }
		defer releaseProxy()
		result := httpAsync(t, v, variableHTTPRequest(ctxFor(t), v.ownerBrowser, i.method, i.path, i.body, i.meta.IdempotencyKey))
		select {
		case <-proxy.reached:
		case <-time.After(5 * time.Second):
			t.Fatal("independent complete COMMIT not intercepted")
		}
		select {
		case <-confirmEntered:
		case <-time.After(3 * time.Second):
			t.Fatal("independent confirmation did not start")
		}
		releaseProxy()
		await(t, proxy.completed)
		// Original writer is now durably committed, while the actual fresh
		// confirmation Tx has not acquired its Account/Project locks yet.
		v.revoke(t, v.ownerBrowser)
		releaseConfirm()
		problem := independentHTTPProblem(t, httpReply(t, result), f.CommitUnknown)
		if problem.CommitState != f.Unknown || problem.RetryHint != "lookup" {
			t.Fatal("confirmation refusal rewrote original Unknown")
		}
		select {
		case r := <-original:
			if r.AttemptID().Validate() != nil || r.Cause().Details().Primary.Canonical() != i.identity.Canonical() {
				t.Fatal("actual commit cause missing")
			}
		default:
			t.Fatal("actual physical Unknown absent")
		}
		v.bindService(t, v.service)
		fresh := v.login(t, v.ownerBrowser.email)
		var recovered vc.VariableCommandLookup
		r := v.lookupHTTP(t, fresh, i)
		if r.status != 200 || r.aborted || json.Unmarshal(r.body, &recovered) != nil || recovered.Status() != vc.LookupCommitted || recovered.Receipt() == nil {
			t.Fatal("actual committed receipt not recoverable")
		}
		want := *recovered.Receipt()
		if want.Fields().Variable.Fields().Version != 2 {
			t.Fatal("wrong committed version")
		}
		replayed := independentHTTPMutation(t, v.send(t, fresh, i))
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(replayed)
		if !bytes.Equal(a, b) {
			t.Fatal("explicit replay replaced original receipt")
		}
		c, h, au, ev, g := independentFacts(t, v)
		if c != 2 || h != 2 || au != 2 || ev != 2 || g != 3 {
			t.Fatal("unknown confirmation or replay generated second fact", c, h, au, ev, g)
		}
	})
}
