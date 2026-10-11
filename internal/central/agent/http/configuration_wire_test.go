package agenthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestAgentConfigurationHTTPStrictCreateIntent(t *testing.T) {
	h, _, port := configurationFixture(t)
	original := configurationCreateJSON()
	for _, extra := range []string{
		`"expected_version":"1",`, `"target_id":"` + directoryID[i.Agent](3).String() + `",`,
		`"semantic_digest":"sha256:` + strings.Repeat("a", 64) + `",`, `"Request":{},`, `"request":{},`,
	} {
		r := httptest.NewRequest("POST", directoryPath(), strings.NewReader(`{`+extra+`"request":`+original+`}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "original-agent-create")
		w, abort := directoryServe(h, r)
		if abort || w.Code != 400 || port.creates != 0 {
			t.Fatal("extra/duplicate envelope accepted")
		}
	}
	for _, request := range []string{
		`null`,
		strings.Replace(original, `"name":"Agent-One"`, `"name":"Agent-One","name":"other"`, 1),
		strings.Replace(original, `"allowed_tool_ids":[]`, `"allowed_tool_ids":null`, 1),
		strings.Replace(original, `"inject_agents_md":true,`, "", 1),
		strings.TrimSuffix(original, "}") + `,"add_skills_enabled":null}`,
	} {
		w, abort := directoryServe(h, configurationHTTPRequest(false, request))
		if abort || w.Code != 400 || port.creates != 0 {
			t.Fatal("invalid nested request accepted")
		}
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Add("Idempotency-Key", "second") },
		func(r *http.Request) { r.Header["idempotency-key"] = []string{"second"} },
		func(r *http.Request) { r.URL.RawQuery = "mode=create" },
		func(r *http.Request) { r.URL.ForceQuery = true },
	} {
		r := configurationHTTPRequest(false, original)
		mutate(r)
		w, abort := directoryServe(h, r)
		if abort || w.Code != 400 || port.creates != 0 {
			t.Fatal("noncanonical transport accepted")
		}
	}
	r := configurationHTTPRequest(false, original)
	r.Header["Content-Encoding"] = []string{""}
	w, abort := directoryServe(h, r)
	if abort || w.Code != 415 || port.creates != 0 {
		t.Fatal("encoding accepted")
	}
	falseRequest := strings.TrimSuffix(original, "}") + `,"add_skills_enabled":false,"install_skill_enabled":false,"description":"","instructions":"` + strings.Repeat("x", c.MaxAgentInstructionsBytes) + `"}`
	w, abort = directoryServe(h, configurationHTTPRequest(false, falseRequest))
	if abort || w.Code != 200 || port.creates != 1 || *port.request.Fields().AddSkillsEnabled || *port.request.Fields().InstallSkillEnabled || len(port.request.Fields().Instructions) != c.MaxAgentInstructionsBytes {
		t.Fatal("explicit false or legal full request lost")
	}
	if len(port.config.Fields().AllowedToolIDs) != 0 {
		t.Fatal("false gained default tool")
	}
	r = configurationHTTPRequest(false, original)
	r.Body = configurationTestBody(strings.Repeat(" ", configurationBodyLimit) + `{}`)
	r.ContentLength = int64(configurationBodyLimit + 2)
	w, abort = directoryServe(h, r)
	if abort || w.Code != 413 || port.creates != 1 {
		t.Fatal("outer request cap")
	}
	for _, lookup := range []bool{false, true} {
		r := configurationHTTPRequest(lookup, original)
		r.Method = "PATCH"
		w, abort := directoryServe(h, r)
		if abort || w.Code != 405 || w.Header().Get("Allow") != "POST" {
			t.Fatal("method widened")
		}
	}
}

func configurationTestBody(value string) *directoryBody {
	return &directoryBody{Reader: strings.NewReader(value)}
}

func TestAgentConfigurationHTTPStrictUpdateIntent(t *testing.T) {
	h, _, port := configurationFixture(t)
	for _, expected := range []string{`null`, `0`, `1`, `"0"`, `"01"`, `"-1"`, `"9223372036854775808"`} {
		for _, lookup := range []bool{false, true} {
			w, abort := directoryServe(h, configurationUpdateHTTPRequest(lookup, expected, configurationUpdateJSON))
			if abort || w.Code != 400 || port.updates != 0 || port.lookups != 0 {
				t.Fatal("invalid original expected version accepted")
			}
		}
	}
	for _, patch := range []string{
		`{}`, `null`, `[]`, `{"description":null}`, `{"inject_agents_md":null}`, `{"allowed_tool_ids":null}`,
		`{"model_ref":null}`, `{"approval_policy":null}`, `{"description":"a","description":"b"}`,
		`{"Description":"a"}`, `{"description":"\ud800"}`,
		`{"agent_id":"` + directoryID[i.Agent](3).String() + `"}`, `{"version":"2"}`,
		`{"add_skills_enabled":false}`, `{"install_skill_enabled":false}`,
	} {
		w, abort := directoryServe(h, configurationUpdateHTTPRequest(false, `"1"`, patch))
		if abort || w.Code != 400 || port.updates != 0 {
			t.Fatal("invalid patch presence or closed field accepted")
		}
	}
	for _, body := range []string{
		`{"request":` + configurationUpdateJSON + `}`,
		`{"expected_version":"1","expected_version":"2","request":` + configurationUpdateJSON + `}`,
		`{"target_id":null,"expected_version":"1","request":` + configurationUpdateJSON + `}`,
		`{"command":"agent.update","expected_version":"1","request":` + configurationUpdateJSON + `}`,
		`{"semantic_digest":"sha256:` + strings.Repeat("a", 64) + `","expected_version":"1","request":` + configurationUpdateJSON + `}`,
	} {
		r := configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON)
		r.Body, r.ContentLength = configurationTestBody(body), int64(len(body))
		w, abort := directoryServe(h, r)
		if abort || w.Code != 400 || port.updates != 0 {
			t.Fatal("invalid PATCH envelope accepted")
		}
	}
	for _, body := range []string{
		`{"command":null,"request":` + configurationCreateJSON() + `}`,
		`{"request":` + configurationCreateJSON() + `}`,
		`{"command":"agent.update","expected_version":"1","request":` + configurationUpdateJSON + `}`,
		`{"command":"agent.update","target_id":null,"expected_version":"1","request":` + configurationUpdateJSON + `}`,
		`{"command":"agent.update","target_id":"` + directoryID[i.Agent](3).String() + `","request":` + configurationUpdateJSON + `}`,
		`{"command":"agent.create","target_id":"` + directoryID[i.Agent](3).String() + `","request":` + configurationCreateJSON() + `}`,
		`{"command":"agent.create","expected_version":null,"request":` + configurationCreateJSON() + `}`,
	} {
		r := configurationUpdateHTTPRequest(true, `"1"`, configurationUpdateJSON)
		r.Body, r.ContentLength = configurationTestBody(body), int64(len(body))
		w, abort := directoryServe(h, r)
		if abort || w.Code != 400 || port.lookups != 0 || port.creates != 0 {
			t.Fatal("lookup union mixed original command shapes")
		}
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Add("Idempotency-Key", "second") },
		func(r *http.Request) { r.Header["idempotency-key"] = []string{"second"} },
		func(r *http.Request) { r.URL.RawQuery = "mode=update" },
		func(r *http.Request) { r.URL.ForceQuery = true },
	} {
		r := configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON)
		mutate(r)
		w, abort := directoryServe(h, r)
		if abort || w.Code != 400 || port.updates != 0 {
			t.Fatal("noncanonical update transport accepted")
		}
	}
	r := configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON)
	r.Header["Content-Encoding"] = []string{""}
	w, abort := directoryServe(h, r)
	if abort || w.Code != 415 || port.updates != 0 {
		t.Fatal("encoded PATCH accepted")
	}
	mutation := configurationUpdateMutation(t, 2, true)
	fields := mutation.Fields().Agent.Fields()
	fields.Core.Instructions = strings.Repeat("<", c.MaxAgentInstructionsBytes)
	config, err := c.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	mutation = configurationMutation(t, config)
	port.mutation = &mutation
	patch := strings.TrimSuffix(configurationUpdateJSON, "}") + `,"instructions":"` + fields.Core.Instructions + `"}`
	w, abort = directoryServe(h, configurationUpdateHTTPRequest(false, `"1"`, patch))
	if abort || w.Code != 200 || port.updates != 1 {
		t.Fatal("legal complete original update was truncated")
	}
	present := port.update.Fields()
	if !present.DisplayName.Present || present.DisplayName.Value != nil || present.InjectAgentsMD == nil || *present.InjectAgentsMD || present.AllowedToolIDs == nil || *present.AllowedToolIDs == nil || len(*present.AllowedToolIDs) != 0 || present.AllowedMountIDs != nil || present.ApprovalModelRef.Present || present.Instructions == nil || *present.Instructions != fields.Core.Instructions {
		t.Fatal("null/false/empty/omitted/full text update semantics changed")
	}
	port.status = c.AgentLookupCommitted
	w, abort = directoryServe(h, configurationUpdateHTTPRequest(true, `"1"`, patch))
	if abort || w.Code != 200 || port.lookups != 1 || port.updates != 1 {
		t.Fatal("full original patch cannot be looked up")
	}
	r = configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON)
	r.Body = configurationTestBody(strings.Repeat(" ", configurationBodyLimit) + `{}`)
	r.ContentLength = int64(configurationBodyLimit + 2)
	w, abort = directoryServe(h, r)
	if abort || w.Code != 413 || port.updates != 1 {
		t.Fatal("PATCH body cap changed")
	}
}

func TestAgentConfigurationHTTPCompoundProjection(t *testing.T) {
	request, err := c.DecodeAgentCreate([]byte(configurationCreateJSON()))
	if err != nil {
		t.Fatal(err)
	}
	config := configurationValue(t, request)
	if !configurationReceiptMatches(configurationMutation(t, config), directoryID[i.Project](4), request) {
		t.Fatal("valid creation projection")
	}
	for _, change := range []func(*c.AgentConfigFields){
		func(v *c.AgentConfigFields) { v.Core.Version = 2 },
		func(v *c.AgentConfigFields) { v.Core.ProjectID = directoryID[i.Project](5) },
		func(v *c.AgentConfigFields) { v.Core.Instructions = "different instructions" },
		func(v *c.AgentConfigFields) { v.AllowedToolIDs = []i.ToolID{} },
		func(v *c.AgentConfigFields) {
			v.AllowedToolIDs = []i.ToolID{directoryID[i.Tool](30), directoryID[i.Tool](31)}
		},
	} {
		v := config.Fields()
		change(&v)
		bad, err := c.NewAgentConfig(v)
		if err != nil {
			t.Fatal("malformed controlled projection", err)
		}
		if configurationReceiptMatches(configurationMutation(t, bad), directoryID[i.Project](4), request) {
			t.Fatal("unrelated creation receipt accepted")
		}
	}
	h, _, port := configurationFixture(t)
	v := port.config.Fields()
	v.Core.ProjectID = directoryID[i.Project](5)
	port.config, err = c.NewAgentConfig(v)
	if err != nil {
		t.Fatal(err)
	}
	w, abort := directoryServe(h, configurationHTTPRequest(true, configurationCreateJSON()))
	if abort || w.Code != 503 || strings.Contains(w.Body.String(), "Agent-One") {
		t.Fatal("Lookup published unrelated historical config")
	}
	path := directoryPath() + "/" + directoryID[i.Agent](3).String() + "/configuration"
	w, abort = directoryServe(h, httptest.NewRequest("GET", path, nil))
	if abort || w.Code != 503 {
		t.Fatal("GET published unrelated config")
	}
}

func TestAgentConfigurationHTTPStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_AGENT_DIRECTORY_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed local standard schema interpreter required")
	}
	type vector struct {
		Name, Schema string
		Body         json.RawMessage
		Valid        bool
	}
	vectors := []vector{}
	add := func(name, schema string, body []byte, valid bool) {
		vectors = append(vectors, vector{name, schema, body, valid})
	}
	original := []byte(configurationCreateJSON())
	request, err := c.DecodeAgentCreate(original)
	if err != nil {
		t.Fatal(err)
	}
	add("omitted defaults", "AgentCreateBody", []byte(`{"request":`+string(original)+`}`), true)
	fields := request.Fields()
	fields.Instructions = strings.Repeat("<", c.MaxAgentInstructionsBytes)
	expanded, err := c.NewAgentCreate(fields)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(expanded)
	if err != nil {
		t.Fatal(err)
	}
	add("full original lookup", "AgentCreateLookupBody", []byte(`{"command":"agent.create","request":`+string(raw)+`}`), true)
	config := configurationValue(t, expanded)
	for _, entry := range []struct {
		schema string
		value  any
	}{
		{"AgentConfig", config},
		{"AgentCreateMutation", configurationMutation(t, config)},
	} {
		raw, err := encodeDirectory(context.Background(), entry.value, configurationBodyLimit)
		if err != nil {
			t.Fatal(err)
		}
		add("typed maximum", entry.schema, raw, true)
	}
	for _, status := range []c.AgentLookupStatus{c.AgentLookupNotObserved, c.AgentLookupInProgress, c.AgentLookupCommitted} {
		var receipt *c.AgentMutation
		if status == c.AgentLookupCommitted {
			v := configurationMutation(t, config)
			receipt = &v
		}
		v, err := c.NewAgentCommandLookup(status, receipt)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		add("lookup union", "AgentCreateLookup", raw, true)
	}
	add("unrelated command", "AgentCreateLookupBody", []byte(`{"command":"agent.update","request":`+string(original)+`}`), false)
	add("null default", "AgentCreateBody", []byte(`{"request":`+strings.TrimSuffix(string(original), "}")+`,"install_skill_enabled":null}}`), false)
	add("extra expected", "AgentCreateBody", []byte(`{"expected_version":"1","request":`+string(original)+`}`), false)
	add("missing committed receipt", "AgentCreateLookup", []byte(`{"status":"committed","receipt":null}`), false)
	add("numeric config version", "AgentConfig", []byte(strings.Replace(mustConfigurationJSON(t, config), `"version":"1"`, `"version":1`, 1)), false)
	add("full patch presence", "AgentUpdateBody", []byte(`{"expected_version":"1","request":`+configurationUpdateJSON+`}`), true)
	add("maximum original update", "AgentUpdateBody", []byte(`{"expected_version":"1","request":{"instructions":"`+fields.Instructions+`"}}`), true)
	add("update lookup", "AgentCommandLookupBody", []byte(`{"command":"agent.update","target_id":"`+directoryID[i.Agent](3).String()+`","expected_version":"1","request":`+configurationUpdateJSON+`}`), true)
	add("create lookup compatibility", "AgentCommandLookupBody", []byte(`{"command":"agent.create","request":`+string(original)+`}`), true)
	for _, patch := range []string{`{}`, `{"description":null}`, `{"inject_agents_md":null}`, `{"allowed_tool_ids":null}`, `{"install_skill_enabled":false}`, `{"agent_id":"` + directoryID[i.Agent](3).String() + `"}`} {
		add("invalid update presence", "AgentUpdateBody", []byte(`{"expected_version":"1","request":`+patch+`}`), false)
	}
	add("numeric update version", "AgentUpdateBody", []byte(`{"expected_version":1,"request":`+configurationUpdateJSON+`}`), false)
	add("missing update target", "AgentCommandLookupBody", []byte(`{"command":"agent.update","expected_version":"1","request":`+configurationUpdateJSON+`}`), false)
	add("create with expected", "AgentCommandLookupBody", []byte(`{"command":"agent.create","expected_version":"1","request":`+string(original)+`}`), false)
	for _, changed := range []bool{false, true} {
		mutation := configurationUpdateMutation(t, 2, changed)
		add("update changed or no-op", "AgentUpdateMutation", []byte(mustConfigurationJSON(t, mutation)), true)
		lookup, e := c.NewAgentCommandLookup(c.AgentLookupCommitted, &mutation)
		if e != nil {
			t.Fatal(e)
		}
		add("original update receipt", "AgentUpdateLookup", []byte(mustConfigurationJSON(t, lookup)), true)
	}
	add("no-op with event", "AgentUpdateMutation", []byte(strings.Replace(mustConfigurationJSON(t, configurationUpdateMutation(t, 2, true)), `"changed":true`, `"changed":false`, 1)), false)
	add("changed without event", "AgentUpdateMutation", []byte(strings.Replace(mustConfigurationJSON(t, configurationUpdateMutation(t, 2, false)), `"changed":false`, `"changed":true`, 1)), false)
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
	cmd := exec.CommandContext(ctx, python, "-c", configurationSchemaProgram, root)
	cmd.Stdin = bytes.NewReader(raw)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("standard schema: %v %s", err, output)
	}
}
func mustConfigurationJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const configurationSchemaProgram = `
import sys,json,pathlib
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
root=pathlib.Path(sys.argv[1]);doc=json.loads((root/'agent-configuration.json').read_bytes());common=json.loads((root/'common.json').read_bytes())
base=(root/'agent-configuration.json').as_uri();registry=Registry().with_resource(base,Resource.from_contents(doc,default_specification=DRAFT202012)).with_resource((root/'common.json').as_uri(),Resource.from_contents(common,default_specification=DRAFT202012))
for schema in doc['components']['schemas'].values():Draft202012Validator.check_schema(schema)
for v in json.load(sys.stdin):
 valid=Draft202012Validator({'$ref':base+'#/components/schemas/'+v['Schema']},registry=registry,format_checker=FormatChecker()).is_valid(v['Body'])
 if valid!=v['Valid']:raise SystemExit('configuration schema vector disagrees: '+v['Name'])
`
