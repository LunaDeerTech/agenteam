package projectvariablehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func TestSecretHTTPStringWireAndByteLimits(t *testing.T) {
	for _, value := range []string{" leading and trailing \n", "é中😀/\\\"\b\f\t", strings.Repeat("\x01", c.MaxSecretValueBytes), strings.Repeat("€", 21845) + "x"} {
		h, _, p := secretTestHandler(t)
		p.result = secretTestReceipt(t, c.SecretCreateCommand, true, 1)
		p.useCreate = func(q c.SecretVariableCreate) {
			if err := q.UseValue(func(raw []byte) error {
				if !bytes.Equal(raw, []byte(value)) {
					t.Fatal("wire normalized material")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		w := newTestWriter()
		body := secretCreateBody(value)
		if len(body) > bodyLimit || serveTest(h, commandRequest("POST", "/secret-variables", body), w) || w.Code != 200 {
			t.Fatal("valid byte boundary rejected")
		}
	}
	for _, value := range []string{strings.Repeat("x", 65537), strings.Repeat("€", 21846)} {
		h, _, p := secretTestHandler(t)
		w := newTestWriter()
		if serveTest(h, commandRequest("POST", "/secret-variables", secretCreateBody(value)), w) || w.Code != 400 || p.calls != 0 {
			t.Fatal("byte cap replaced by scalar count")
		}
	}
	h, _, p := secretTestHandler(t)
	p.result = secretTestReceipt(t, c.SecretCreateCommand, true, 1)
	p.useCreate = func(q c.SecretVariableCreate) {
		if err := q.UseValue(func(raw []byte) error {
			if string(raw) != "😀" {
				t.Fatal("surrogate pair changed")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	body := strings.Replace(secretCreateBody("MARKER"), `"MARKER"`, `"\ud83d\ude00"`, 1)
	w := newTestWriter()
	if serveTest(h, commandRequest("POST", "/secret-variables", body), w) || w.Code != 200 {
		t.Fatal("valid scalar pair rejected")
	}
}

func TestSecretHTTPFormalSchema(t *testing.T) {
	var vectors []schemaVector
	add := func(name, schema, raw string, valid bool) {
		vectors = append(vectors, schemaVector{name, schema, json.RawMessage(raw), valid})
	}
	for _, tc := range []struct {
		name                       c.SecretCommandName
		method, path, body, schema string
		version                    f.Version
	}{
		{c.SecretCreateCommand, "POST", "/secret-variables", secretCreateBody(strings.Repeat("\x01", c.MaxSecretValueBytes)), "SecretVariableCreateBody", 1},
		{c.SecretUpdateCommand, "PATCH", "/secret-variables/" + testID[id.ProjectVariable](3).String(), `{"expected_version":"1","request":{"value":"opaque-input"}}`, "SecretVariableUpdateBody", 2},
		{c.SecretDeleteCommand, "DELETE", "/secret-variables/" + testID[id.ProjectVariable](3).String(), `{"expected_version":"1"}`, "SecretVariableDeleteBody", 2},
	} {
		h, _, p := secretTestHandler(t)
		p.result = secretTestReceipt(t, tc.name, true, tc.version)
		w := newTestWriter()
		if serveTest(h, commandRequest(tc.method, tc.path, tc.body), w) || w.Code != 200 {
			t.Fatal("wire fixture rejected")
		}
		add(string(tc.name), "SecretVariableMutation", w.Body.String(), true)
		add("request", tc.schema, tc.body, true)
		p.lookup, _ = c.NewSecretVariableCommandLookup(c.SecretLookupCommitted, &p.result)
		w = newTestWriter()
		query := secretLookupBody(tc.name)
		if serveTest(h, commandRequest("POST", "/secret-variables/commands/lookup", query), w) || w.Code != 200 {
			t.Fatal("lookup wire fixture")
		}
		add("lookup", "SecretVariableCommandLookup", w.Body.String(), true)
		add("query", "SecretVariableLookupBody", query, true)
	}
	for _, tc := range []struct{ path, schema string }{{"/secret-variables", "SecretVariableSummaryPage"}, {"/secret-variables/" + testID[id.ProjectVariable](3).String(), "SecretVariable"}} {
		h, _, _ := secretTestHandler(t)
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath(tc.path), nil), w) || w.Code != 200 {
			t.Fatal("read fixture")
		}
		add("read", tc.schema, w.Body.String(), true)
	}
	maximum := f.Page[c.SecretVariable]{Items: []c.SecretVariable{}, NextCursor: strings.Repeat("x", maxCursorBytes)}
	for n := range c.MaxPageLimit {
		fields := secretTestValue(t, 1).Fields()
		fields.ID = testID[id.ProjectVariable](100 + n)
		fields.Description = strings.Repeat("\t", c.MaxDescriptionBytes)
		fields.Name = "N" + strings.Repeat("x", 127)
		item, err := c.NewSecretVariable(fields)
		if err != nil {
			t.Fatal(err)
		}
		maximum.Items = append(maximum.Items, item)
	}
	raw, err := encodeValue(context.Background(), maximum, listLimit)
	if err != nil {
		t.Fatal(err)
	}
	add("maximum-page", "SecretVariableSummaryPage", string(raw), true)
	noop := secretTestReceipt(t, c.SecretUpdateCommand, false, 1)
	raw, _ = json.Marshal(noop)
	add("noop", "SecretVariableMutation", string(raw), true)
	add("not-observed", "SecretVariableCommandLookup", `{"status":"not_observed","receipt":null}`, true)
	for _, bad := range []struct{ schema, raw string }{
		{"SecretVariableUpdate", `{}`}, {"SecretVariableUpdate", `{"value":null}`}, {"SecretVariableCommandLookup", `{"status":"in_progress","receipt":null}`}, {"SecretVariableLookupBody", `{"command":"project.secret_variable.create"}`}, {"SecretVariableValue", `""`}, {"SecretVariableValue", `"\u0000"`},
	} {
		add("negative", bad.schema, bad.raw, false)
	}
	unsafe := secretTestValue(t, 1)
	raw, _ = json.Marshal(unsafe)
	add("unsafe-value", "SecretVariable", strings.TrimSuffix(string(raw), "}")+`,"value":"unsafe"}`, false)
	python := os.Getenv("AGENTEAM_PROJECT_VARIABLE_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed local schema interpreter required")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "api", "openapi"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(vectors)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", strings.ReplaceAll(schemaProgram, "project-variables.json", "secret-variables.json"), root)
	cmd.Stdin = bytes.NewReader(raw)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("schema: %v %s", err, output)
	}
	t.Log(strings.TrimSpace(string(output)))
}
