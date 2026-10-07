//go:build integration

package model_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type configurationWriteHistory struct {
	kind, method, path, key string
	body                    []byte
	response                systemHTTPResponse
	receipt                 mc.CommandReceipt
}
type configurationWriteDo func(string, string, string, []byte) systemHTTPResponse

func configurationWriteSix(t *testing.T, v *configurationWriteFixture, prefix, first, second string, call configurationWriteDo, checks bool) []configurationWriteHistory {
	t.Helper()
	history := []configurationWriteHistory{}
	run := func(kind, method, path string, body any) configurationWriteHistory {
		r := configurationWriteHistory{kind: kind, method: method, path: path, key: prefix + kind, body: projectUpdateJSON(t, body)}
		r.response = call(method, path, r.key, r.body)
		r.receipt = configurationWriteReceipt(t, r.response, kind)
		history = append(history, r)
		return r
	}
	provider := configurationProviderBody(first)
	p := run("provider.create", "POST", v.base()+"model-providers", map[string]any{"input": provider})
	ppath := v.base() + "model-providers/" + p.receipt.ResourceID
	if checks {
		before := v.snapshot(t)
		call("PUT", ppath, prefix+"noop-provider", projectUpdateJSON(t, map[string]any{"expected_version": "1", "input": provider})).problem(t, 400, f.InvalidArgument)
		v.sameSnapshot(t, before)
		changed := configurationProviderBody(first)
		changed["name"] = "different private semantic"
		call("POST", p.path, p.key, projectUpdateJSON(t, map[string]any{"input": changed})).problem(t, 409, f.IdempotencyKeyReused)
		v.sameSnapshot(t, before)
	}
	provider["credential_ref"] = second
	provider["name"] = "HTTP Project provider updated"
	run("provider.update", "PUT", ppath, map[string]any{"expected_version": "1", "input": provider})
	if checks {
		var oldRefs, newRefs int
		e := v.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_secret.secret_references WHERE credential_id=$1 AND consumer='model' AND owner_id=$3),(SELECT count(*) FROM agenteam_secret.secret_references WHERE credential_id=$2 AND consumer='model' AND owner_id=$3)`, first, second, p.receipt.ResourceID).Scan(&oldRefs, &newRefs)
		if e != nil || oldRefs != 0 || newRefs != 1 {
			t.Fatal("exact reference replacement", e)
		}
	}
	modelBody := configurationModelBody()
	m := run("model.create", "POST", v.base()+"models", map[string]any{"provider_id": p.receipt.ResourceID, "input": modelBody})
	mpath := v.base() + "models/" + m.receipt.ResourceID
	if checks {
		before := v.snapshot(t)
		call("DELETE", ppath, prefix+"provider-nonempty", projectUpdateJSON(t, map[string]any{"expected_version": "2"})).problem(t, 409, f.InvalidState)
		v.sameSnapshot(t, before)
		call("PUT", mpath, prefix+"noop-model", projectUpdateJSON(t, map[string]any{"expected_version": "1", "input": modelBody})).problem(t, 400, f.InvalidArgument)
		v.sameSnapshot(t, before)
		for _, suffix := range []string{"model-providers", "model-providers/" + p.receipt.ResourceID, "models", "models/" + m.receipt.ResourceID, "available-chat-models"} {
			get := call("GET", v.base()+suffix, "", nil).want(t, 200)
			head := call("HEAD", v.base()+suffix, "", nil).want(t, 200)
			if len(head.body) != 0 || head.headers.Get("Content-Length") != get.headers.Get("Content-Length") {
				t.Fatal("read/HEAD compatibility")
			}
		}
		// Explicit auxiliary overflow state; the public request itself is legal.
		v.sql(t, `UPDATE agenteam_model.models SET version=9223372036854775807 WHERE id=$1`, m.receipt.ResourceID)
		restore := func() { v.sql(t, `UPDATE agenteam_model.models SET version=1 WHERE id=$1`, m.receipt.ResourceID) }
		restored := false
		t.Cleanup(func() {
			if !restored {
				restore()
			}
		})
		maxBefore := v.snapshot(t)
		call("DELETE", mpath, prefix+"max-version", projectUpdateJSON(t, map[string]any{"expected_version": "9223372036854775807", "replacement": nil})).problem(t, 409, f.InvalidState)
		v.sameSnapshot(t, maxBefore)
		restore()
		restored = true
	}
	modelBody["name"] = "HTTP Project chat updated"
	run("model.update", "PUT", mpath, map[string]any{"expected_version": "1", "input": modelBody})
	run("model.delete", "DELETE", mpath, map[string]any{"expected_version": "2", "replacement": nil})
	run("provider.delete", "DELETE", ppath, map[string]any{"expected_version": "2"})
	if checks {
		var retained bool
		e := v.raw.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE credential_id=$1 AND consumer='model' AND owner_id=$2)`, second, p.receipt.ResourceID).Scan(&retained)
		if e != nil || !retained {
			t.Fatal("provider delete must release exact ref but retain credential", e)
		}
	}
	return history
}
func configurationReplayHistory(t *testing.T, v *configurationWriteFixture, history []configurationWriteHistory, call configurationWriteDo) {
	t.Helper()
	before := v.snapshot(t)
	for _, h := range history {
		got := call(h.method, h.path, h.key, h.body).want(t, 200)
		if !bytes.Equal(got.body, h.response.body) {
			t.Fatal("original historical response changed")
		}
		lookup := call("POST", v.base()+"model-commands/lookup", h.key, projectUpdateJSON(t, map[string]any{"command": h.kind}))
		configurationLookupReceipt(t, lookup, &h.receipt)
		configurationWriteSchema(t, "ConfigurationLookup", lookup.body)
	}
	v.sameSnapshot(t, before)
}
func TestModelProjectConfigurationWriteHTTPCRUDAndHistory(t *testing.T) {
	configurationWriteTop(t)
	v := newConfigurationWriteFixture(t)
	first := v.create(t, "configuration-credential-first", "first-private-material")
	second := v.create(t, "configuration-credential-second", "second-private-material")
	call := func(method, path, key string, body []byte) systemHTTPResponse {
		return v.command(t, v.ownerBrowser, method, path, key, body)
	}
	history := configurationWriteSix(t, v, "crud-", first, second, call, true)
	for _, h := range history {
		configurationWriteSchema(t, "ConfigurationReceipt", h.response.body)
		configurationWriteExport(t, h.kind, h.method, h.path, h.response)
	}
	call("GET", history[1].path, "", nil).problem(t, 404, f.NotFound)
	call("GET", history[3].path, "", nil).problem(t, 404, f.NotFound)
	configurationReplayHistory(t, v, history, call)
	renewed := v.login(t, v.ownerBrowser.email)
	configurationReplayHistory(t, v, history, func(method, path, key string, body []byte) systemHTTPResponse {
		return v.command(t, renewed, method, path, key, body)
	})
	absent := call("POST", v.base()+"model-commands/lookup", "not-observed", projectUpdateJSON(t, map[string]any{"command": "provider.create"}))
	configurationLookupReceipt(t, absent, nil)
	configurationWriteSchema(t, "ConfigurationLookup", absent.body)
	configurationWriteExport(t, "lookup-absent", "POST", v.base()+"model-commands/lookup", absent)
	observed := call("POST", v.base()+"model-commands/lookup", history[0].key, projectUpdateJSON(t, map[string]any{"command": "provider.create"}))
	configurationWriteExport(t, "lookup-found", "POST", v.base()+"model-commands/lookup", observed)
	before := v.snapshot(t)
	// Bad typed projection is rejected even when this original key has history.
	invalid := configurationProviderBody(first)
	invalid["options"] = nil
	call("POST", v.base()+"model-providers", history[0].key, projectUpdateJSON(t, map[string]any{"input": invalid})).problem(t, 400, f.InvalidArgument)
	policy := configurationProviderBody(first)
	policy["options"] = map[string]any{"unsupported": true}
	call("POST", v.base()+"model-providers", "unsupported-policy", projectUpdateJSON(t, map[string]any{"input": policy})).problem(t, 422, f.CapabilityUnsupported)
	v.sameSnapshot(t, before)
	var commands, audits, events int
	e := v.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM agenteam_model.commands WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='model' AND project_id=$1),(SELECT count(*) FROM agenteam_outbox.events WHERE producer='model' AND project_id=$1)`, v.project.ID.String()).Scan(&commands, &audits, &events)
	if e != nil || commands != 6 || audits != 6 || events != 6 {
		t.Fatal("six exact atomic command facts", commands, audits, events, e)
	}
	for _, private := range []string{"first-private-material", "second-private-material", history[0].key, v.ownerBrowser.cookie, v.ownerBrowser.csrf} {
		if strings.Contains(v.logs.text(), private) {
			t.Fatal("private data entered logs")
		}
	}
	var wire map[string]any
	if json.Unmarshal(observed.body, &wire) != nil || wire["found"] != true {
		t.Fatal("original found/receipt union")
	}
	t.Log("six HTTP commands and safe original receipts; same User/new Session history; typed malformed input cannot consult receipt; actual reference swap/release; maximum version/no-change original failures")
}
