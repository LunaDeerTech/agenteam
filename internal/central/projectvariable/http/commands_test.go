package projectvariablehttp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type commandSample struct {
	name               c.CommandName
	method, path, body string
	request            any
	expected           *f.Version
	receipt            c.VariableMutation
}

func samples(t *testing.T) []commandSample {
	t.Helper()
	v := testVariable()
	eid := testID[event.EventIdentity](10)
	aid := testID[ac.Record](11)
	version := f.Version(1)
	create, _ := c.NewVariableCreate(c.VariableCreateFields{ID: v.Fields().ID, Name: v.Fields().Name, Description: v.Fields().Description, Value: v.Fields().Value})
	cr, e := c.NewVariableMutation(c.VariableMutationFields{Command: c.CreateCommand, Changed: true, Variable: v, EventID: &eid, AuditID: &aid})
	if e != nil {
		t.Fatal(e)
	}
	value := "new-value"
	update, _ := c.NewVariableUpdate(c.VariableUpdateFields{Value: &value})
	fields := v.Fields()
	fields.Value = value
	fields.Version = 2
	updated, _ := c.NewVariable(fields)
	ur, e := c.NewVariableMutation(c.VariableMutationFields{Command: c.UpdateCommand, Changed: true, Variable: updated, EventID: &eid, AuditID: &aid})
	if e != nil {
		t.Fatal(e)
	}
	dr, e := c.NewVariableMutation(c.VariableMutationFields{Command: c.DeleteCommand, Changed: true, Deleted: &c.VariableDeleted{ID: fields.ID, ProjectID: fields.ProjectID, Type: fields.Type, Version: 2, DeletedAt: fields.UpdatedAt}, EventID: &eid, AuditID: &aid})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(create)
	return []commandSample{{c.CreateCommand, "POST", "/variables", `{"request":` + string(raw) + `}`, create, nil, cr}, {c.UpdateCommand, "PATCH", "/variables/" + fields.ID.String(), `{"expected_version":"1","request":{"value":"new-value"}}`, update, &version, ur}, {c.DeleteCommand, "DELETE", "/variables/" + fields.ID.String(), `{"expected_version":"1"}`, nil, &version, dr}}
}
func lookupBody(t *testing.T, s commandSample) string {
	t.Helper()
	var body map[string]any
	if json.Unmarshal([]byte(s.body), &body) != nil {
		t.Fatal("sample")
	}
	body["command"] = s.name
	if s.name != c.CreateCommand {
		body["target_id"] = testVariable().Fields().ID.String()
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}
func TestVariableHTTPOriginalIntentAcrossThreeCommandsAndLookup(t *testing.T) {
	for _, sample := range samples(t) {
		for _, lookup := range []bool{false, true} {
			h, _, p := testHandler()
			p.result = sample.receipt
			p.lookup, _ = c.NewVariableCommandLookup(c.LookupCommitted, &sample.receipt)
			method, path, body := sample.method, sample.path, sample.body
			if lookup {
				method, path, body = "POST", "/variables/commands/lookup", lookupBody(t, sample)
			}
			w := newTestWriter()
			if serveTest(h, commandRequest(method, path, body), w) || w.Code != 200 || p.calls != 1 {
				t.Fatal(sample.name, lookup, w.Code, w.Body.String())
			}
			var out c.VariableMutation
			if lookup {
				var restored c.VariableCommandLookup
				if json.Unmarshal(w.Body.Bytes(), &restored) != nil || restored.Receipt() == nil {
					t.Fatal("lookup receipt")
				}
				out = *restored.Receipt()
			} else {
				if json.Unmarshal(w.Body.Bytes(), &out) != nil {
					t.Fatal("receipt")
				}
			}
			if out.Fields().Command != sample.name {
				t.Fatal("wrong receipt")
			}
			if lookup {
				meta := f.CommandMeta{RequestID: testID[f.Request](12), IdempotencyKey: "original-key", ExpectedVersion: sample.expected}
				want, e := c.VariableCommandDigest(testActor(), meta, testVariable().Fields().ProjectID, testVariable().Fields().ID, sample.name, sample.request)
				if e != nil || p.gotLookup.Fields().SemanticDigest != want {
					t.Fatal("digest lost original fields/version")
				}
			} else {
				if p.gotMeta.IdempotencyKey != "original-key" || (p.gotMeta.ExpectedVersion == nil) != (sample.expected == nil) {
					t.Fatal("command meta")
				}
			}
		}
	}
}
func TestVariableHTTPStrictJSONPresenceMediaAndKey(t *testing.T) {
	sample := samples(t)[1]
	for _, body := range []string{`null`, `[]`, `{}`, `{"expected_version":1,"request":{"value":"x"}}`, `{"expected_version":"01","request":{"value":"x"}}`, `{"expected_version":"1","request":{}}`, `{"expected_version":"1","request":{"value":null}}`, `{"expected_version":"1","request":{"value":"x","value":"y"}}`, `{"expected_version":"1","request":{"\u0076alue":"x","value":"y"}}`, `{"expected_version":"1","request":{"Value":"x"}}`, `{"expected_version":"1","request":{"value":"\ud800"}}`, `{"expected_version":"1","request":{"value":"\u0000"}}`, `{"expected_version":"1","request":{"name":"agenteam_PRIVATE"}}`, sample.body + ` {}`, strings.Replace(sample.body, `"request"`, `"Request"`, 1), strings.Replace(sample.body, `"request"`, `"extra":true,"request"`, 1)} {
		h, _, p := testHandler()
		w := newTestWriter()
		if serveTest(h, commandRequest(sample.method, sample.path, body), w) || w.Code != 400 || p.calls != 0 {
			t.Fatal("invalid body admitted", body[:min(100, len(body))], w.Code)
		}
	}
	for _, edit := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Idempotency-Key") }, func(r *http.Request) { r.Header.Add("Idempotency-Key", "second") }, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, func(r *http.Request) { r.Header.Set("Content-Type", "application/json;charset=latin1") }, func(r *http.Request) { r.Header["Content-Encoding"] = []string{""} }, func(r *http.Request) { r.URL.ForceQuery = true }} {
		h, _, p := testHandler()
		r := commandRequest(sample.method, sample.path, sample.body)
		edit(r)
		w := newTestWriter()
		if serveTest(h, r, w) || w.Code < 400 || p.calls != 0 {
			t.Fatal("invalid headers/query admitted")
		}
	}
	for _, body := range []string{`{"expected_version":"1","request":null}`, `{"expected_version":"1","request":{}}`, `{"expected_version":null}`} {
		h, _, p := testHandler()
		w := newTestWriter()
		if serveTest(h, commandRequest("DELETE", sample.path, body), w) || w.Code != 400 || p.calls != 0 {
			t.Fatal("delete shape")
		}
	}
}
func TestVariableHTTPWrongReceiptAbortsAndUnknownIsPreserved(t *testing.T) {
	sample := samples(t)[0]
	h, _, p := testHandler()
	p.result = samples(t)[1].receipt
	w := newTestWriter()
	if !serveTest(h, commandRequest(sample.method, sample.path, sample.body), w) || w.Body.Len() != 0 {
		t.Fatal("published replacement failure after known library success")
	}
	h, _, p = testHandler()
	fault := f.NewFault(f.CommitUnknown, f.Unknown)
	fault.RetryHint = "lookup"
	p.err = fault
	w = newTestWriter()
	if serveTest(h, commandRequest(sample.method, sample.path, sample.body), w) || w.Code != 503 || !strings.Contains(w.Body.String(), `"commit_state":"unknown"`) || !strings.Contains(w.Body.String(), `"retry_hint":"lookup"`) {
		t.Fatal("lost actual unknown", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "canary") || strings.Contains(w.Body.String(), "original-key") {
		t.Fatal("unsafe Problem")
	}
}
