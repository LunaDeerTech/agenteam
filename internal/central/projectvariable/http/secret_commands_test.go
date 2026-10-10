package projectvariablehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func secretCreateBody(value string) string {
	raw, err := json.Marshal(map[string]any{"request": map[string]any{"variable_id": testID[id.ProjectVariable](3), "name": "NAME", "description": "public description", "value": value}})
	if err != nil {
		panic(err)
	}
	return string(raw)
}
func secretLookupBody(command c.SecretCommandName) string {
	body := `{"command":"` + string(command) + `","target_id":"` + testID[id.ProjectVariable](3).String() + `"`
	if command != c.SecretCreateCommand {
		body += `,"expected_version":"1"`
	}
	return body + `}`
}

func TestSecretHTTPCommandsAndIdentityOnlyLookup(t *testing.T) {
	for _, tc := range []struct {
		name               c.SecretCommandName
		method, path, body string
		version            f.Version
	}{
		{c.SecretCreateCommand, "POST", "/secret-variables", secretCreateBody("private-value-canary"), 1},
		{c.SecretUpdateCommand, "PATCH", "/secret-variables/" + testID[id.ProjectVariable](3).String(), `{"expected_version":"1","request":{"name":"NAME","value":"private-value-canary"}}`, 2},
		{c.SecretDeleteCommand, "DELETE", "/secret-variables/" + testID[id.ProjectVariable](3).String(), `{"expected_version":"1"}`, 2},
	} {
		t.Run(string(tc.name), func(t *testing.T) {
			h, _, p := secretTestHandler(t)
			p.result = secretTestReceipt(t, tc.name, true, tc.version)
			check := func(value []byte) error {
				if string(value) != "private-value-canary" {
					t.Fatal("material changed")
				}
				return nil
			}
			p.useCreate = func(v c.SecretVariableCreate) {
				if err := v.UseValue(check); err != nil {
					t.Fatal(err)
				}
			}
			p.useUpdate = func(v c.SecretVariableUpdate) {
				if !v.Fields().ValuePresent {
					t.Fatal("lost presence")
				}
				if err := v.UseValue(check); err != nil {
					t.Fatal(err)
				}
			}
			w := newTestWriter()
			if serveTest(h, commandRequest(tc.method, tc.path, tc.body), w) || w.Code != 200 || p.calls != 1 || p.target != testID[id.ProjectVariable](3) || p.meta.RequestID.Validate() != nil || p.meta.IdempotencyKey != "original-key" {
				t.Fatal("command dispatch", w.Code)
			}
			if strings.Contains(w.Body.String(), "private-value-canary") || strings.Contains(w.Body.String(), `"value"`) {
				t.Fatal("material in receipt")
			}
			if p.create != nil && p.create.UseValue(check) == nil || p.update != nil && p.update.UseValue(check) == nil {
				t.Fatal("request material survived handler")
			}
			p.lookup, _ = c.NewSecretVariableCommandLookup(c.SecretLookupCommitted, &p.result)
			w = newTestWriter()
			if serveTest(h, commandRequest("POST", "/secret-variables/commands/lookup", secretLookupBody(tc.name)), w) || w.Code != 200 || p.calls != 2 {
				t.Fatal("identity-only history", w.Code)
			}
			q := p.lookupIn.Fields()
			if q.Command != tc.name || q.TargetID != testID[id.ProjectVariable](3) || q.IdempotencyKey != "original-key" || (q.ExpectedVersion == nil) != (tc.name == c.SecretCreateCommand) {
				t.Fatal("lookup identity/presence")
			}
		})
	}
	h, _, p := secretTestHandler(t)
	p.result = secretTestReceipt(t, c.SecretUpdateCommand, false, 1)
	w := newTestWriter()
	if serveTest(h, commandRequest("PATCH", "/secret-variables/"+testID[id.ProjectVariable](3).String(), `{"expected_version":"1","request":{"name":"NAME"}}`), w) || w.Code != 200 || p.update.Fields().ValuePresent {
		t.Fatal("metadata-only no-op")
	}
	p.lookup, _ = c.NewSecretVariableCommandLookup(c.SecretLookupNotObserved, nil)
	w = newTestWriter()
	if serveTest(h, commandRequest("POST", "/secret-variables/commands/lookup", secretLookupBody(c.SecretCreateCommand)), w) || w.Body.String() != `{"status":"not_observed","receipt":null}` {
		t.Fatal("not_observed shape")
	}
}

func TestSecretHTTPStrictCommands(t *testing.T) {
	valid := secretCreateBody("private-value-canary")
	for _, body := range []string{
		`null`, `[]`, `{}`, valid + ` {}`, strings.Replace(valid, `"request":`, `"Request":`, 1), strings.Replace(valid, `"request":`, `"request":{},"request":`, 1),
		strings.Replace(valid, `"value":"private-value-canary"`, `"value":null`, 1), strings.Replace(valid, `"value":"private-value-canary"`, `"value":"\ud800"`, 1),
		strings.Replace(valid, `"value":"private-value-canary"`, `"value":"\udfff"`, 1), strings.Replace(valid, `"value":"private-value-canary"`, `"value":"\ud800\u0041"`, 1),
		strings.Replace(valid, `"value":"private-value-canary"`, `"value":"\u0000"`, 1), strings.Replace(valid, `"value":"private-value-canary"`, `"value":""`, 1),
		strings.Replace(valid, `"value":"private-value-canary"`, `"value":{},"private-value-canary":"x"`, 1), strings.Replace(valid, `"name":"NAME"`, `"name":"NAME","\u006eame":"OTHER"`, 1),
		strings.Replace(valid, `"value":"private-value-canary"`, `"value":"`+string([]byte{0xff})+`"`, 1),
		strings.Replace(valid, `"request":`, `"private-value-canary":`, 1), strings.Replace(valid, `"name":"NAME"`, `"name":"AGENTEAM_SECRET"`, 1),
	} {
		h, _, p := secretTestHandler(t)
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", "/secret-variables", body), w) || w.Code != 400 || p.calls != 0 || strings.Contains(w.Body.String(), "private-value-canary") {
			t.Fatal("invalid request escaped")
		}
	}
	for _, body := range []string{`{"command":"project.secret_variable.create"}`, `{"command":"project.secret_variable.create","target_id":"` + testID[id.ProjectVariable](3).String() + `","expected_version":"1"}`, `{"command":"project.secret_variable.update","target_id":"` + testID[id.ProjectVariable](3).String() + `"}`, secretLookupBody(c.SecretCreateCommand)[:len(secretLookupBody(c.SecretCreateCommand))-1] + `,"request":{}}`, secretLookupBody(c.SecretCreateCommand)[:len(secretLookupBody(c.SecretCreateCommand))-1] + `,"semantic_digest":"private-value-canary"}`} {
		h, _, p := secretTestHandler(t)
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", "/secret-variables/commands/lookup", body), w) || w.Code != 400 || p.calls != 0 {
			t.Fatal("non-identity Lookup accepted")
		}
	}
	for _, mode := range []string{"wrong-media", "encoding", "duplicate-key-header", "query", "oversize"} {
		h, _, p := secretTestHandler(t)
		w := newTestWriter()
		r := commandRequest("POST", "/secret-variables", valid)
		status := 400
		switch mode {
		case "wrong-media":
			r.Header.Set("Content-Type", "text/plain")
			status = 415
		case "encoding":
			r.Header["Content-Encoding"] = []string{""}
			status = 415
		case "duplicate-key-header":
			r.Header["idempotency-key"] = []string{"second"}
		case "query":
			r.URL.RawQuery = "value=private-value-canary"
		case "oversize":
			r.Body = io.NopCloser(strings.NewReader(strings.Repeat(" ", bodyLimit+1)))
			status = 413
		}
		if serveTest(h, r, w) || w.Code != status || p.calls != 0 {
			t.Fatal("transport shape accepted", mode, w.Code)
		}
	}
}

type secretObservedBody struct {
	raw       []byte
	data      []byte
	once      bool
	panicRead bool
}

func (b *secretObservedBody) Read(dst []byte) (int, error) {
	if b.once {
		return 0, io.EOF
	}
	b.once = true
	b.raw = dst
	n := copy(dst, b.data)
	if b.panicRead {
		panic("private-value-canary")
	}
	return n, io.EOF
}
func (*secretObservedBody) Close() error { return nil }
func zeroBytes(raw []byte) bool          { return bytes.Equal(raw, make([]byte, len(raw))) }

func TestSecretHTTPOwnedMaterialAllTails(t *testing.T) {
	for _, mode := range []string{"success", "port-error", "port-panic", "expired", "decode-error", "read-panic"} {
		t.Run(mode, func(t *testing.T) {
			h, _, p := secretTestHandler(t)
			p.result = secretTestReceipt(t, c.SecretCreateCommand, true, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := &secretObservedBody{data: []byte(secretCreateBody("private-value-canary"))}
			if mode == "decode-error" {
				body.data = []byte(strings.Replace(string(body.data), `"name":"NAME"`, `"name":"AGENTEAM_SECRET"`, 1))
			}
			body.panicRead = mode == "read-panic"
			var borrowed []byte
			p.useCreate = func(q c.SecretVariableCreate) {
				if !zeroBytes(body.raw) {
					t.Fatal("raw body retained into domain call")
				}
				if err := q.UseValue(func(value []byte) error { borrowed = value; return nil }); err != nil {
					t.Fatal(err)
				}
				if !zeroBytes(borrowed) {
					t.Fatal("borrowed value retained")
				}
			}
			switch mode {
			case "port-error":
				p.err = f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(errors.New("private-value-canary"))
			case "port-panic":
				p.before = func(context.Context) { panic("private-value-canary") }
			case "expired":
				p.before = func(context.Context) { cancel() }
			}
			r := commandRequest("POST", "/secret-variables", "").WithContext(ctx)
			r.Body = body
			w := newTestWriter()
			aborted := serveTest(h, r, w)
			if aborted != (mode == "port-panic" || mode == "expired" || mode == "read-panic") || !zeroBytes(body.raw) || strings.Contains(w.Body.String(), "private-value-canary") {
				t.Fatal("raw ownership/output tail", mode)
			}
			if p.create != nil && p.create.UseValue(func([]byte) error { return nil }) == nil {
				t.Fatal("typed material retained")
			}
		})
	}
}

func TestSecretHTTPReceiptBindingAfterMutation(t *testing.T) {
	for _, mode := range []string{"value-noop", "wrong-version", "wrong-metadata", "wrong-target", "wrong-command"} {
		h, _, p := secretTestHandler(t)
		p.result = secretTestReceipt(t, c.SecretUpdateCommand, true, 2)
		switch mode {
		case "value-noop":
			p.result = secretTestReceipt(t, c.SecretUpdateCommand, false, 1)
		case "wrong-version":
			p.result = secretTestReceipt(t, c.SecretUpdateCommand, true, 3)
		case "wrong-metadata", "wrong-target":
			fields := p.result.Fields()
			v := fields.Variable.Fields()
			if mode == "wrong-metadata" {
				v.Name = "OTHER"
			} else {
				v.ID = testID[id.ProjectVariable](99)
			}
			fields.Variable, _ = c.NewSecretVariable(v)
			p.result, _ = c.NewSecretVariableMutation(fields)
		case "wrong-command":
			p.result = secretTestReceipt(t, c.SecretCreateCommand, true, 1)
		}
		w := newTestWriter()
		if !serveTest(h, commandRequest("PATCH", "/secret-variables/"+testID[id.ProjectVariable](3).String(), `{"expected_version":"1","request":{"name":"NAME","value":"private-value-canary"}}`), w) || p.calls != 1 || w.Body.Len() != 0 {
			t.Fatal("bad post-write receipt became a response", mode)
		}
	}
}
