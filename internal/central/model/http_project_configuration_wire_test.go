package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestProjectModelConfigurationHTTPPureWire(t *testing.T) {
	t.Run("strict_envelopes", func(t *testing.T) {
		for _, tc := range configurationTestCases() {
			for _, bad := range []string{"", `null`, `[]`, tc.body + `{}`, strings.Replace(tc.body, `{`, `{"scope":"system",`, 1), strings.Replace(tc.body, `{`, `{"scope":null,`, 1), strings.Replace(tc.body, `{`, `{"scope":1,"scope":2,`, 1)} {
				t.Run(tc.kind, func(t *testing.T) {
					h, _, s := configurationTestHandler(t)
					w := summaryWriter()
					h.ServeHTTP(w, configurationTestRequest(tc.method, tc.path, bad))
					if w.Code != 400 || s.writes.Load()+s.lookups.Load() != 0 {
						t.Fatal("bad envelope reached service", w.Code)
					}
					w.cleared(t)
				})
			}
		}
	})
	t.Run("typed_projection_zero_service", func(t *testing.T) {
		for _, tc := range []struct{ body, path, method string }{
			{`{}`, configurationTestProviders, "POST"}, {`{"input":null}`, configurationTestProviders, "POST"},
			{strings.Replace(configurationTestCases()[0].body, `"credential_ref":null,`, ``, 1), configurationTestProviders, "POST"},
			{strings.Replace(configurationTestCases()[0].body, `"options":{}`, `"options":null`, 1), configurationTestProviders, "POST"},
			{strings.Replace(configurationTestCases()[0].body, `"enabled":true,`, ``, 1), configurationTestProviders, "POST"},
			{strings.Replace(configurationTestCases()[0].body, `openai-chat-completions`, `openai-embeddings`, 1), configurationTestProviders, "POST"},
			{strings.Replace(configurationTestCases()[3].body, `"reasoning_efforts":[]`, `"reasoning_efforts":null`, 1), configurationTestModels, "POST"},
			{strings.Replace(configurationTestCases()[3].body, `"max_output":null`, `"max_output":12`, 1), configurationTestModels, "POST"},
			{strings.Replace(configurationTestCases()[3].body, `"header_overwrite":{}`, `"header_overwrite":null`, 1), configurationTestModels, "POST"},
			{strings.Replace(configurationTestCases()[3].body, `"type":"chat"`, `"type":"embedding"`, 1), configurationTestModels, "POST"},
			{`{"expected_version":"1"}`, configurationTestModel, "DELETE"},
			{`{"expected_version":"1","replacement":"` + projectHTTPTestModel + `"}`, configurationTestModel, "DELETE"},
			{`{"command":"provider.create","input":null}`, configurationTestLookup, "POST"},
			{`{"command":"selection.set"}`, configurationTestLookup, "POST"},
			{`{"Command":"model.delete"}`, configurationTestLookup, "POST"},
			{`{"command":"model.delete","command":"model.create"}`, configurationTestLookup, "POST"},
		} {
			h, _, s := configurationTestHandler(t)
			w := summaryWriter()
			h.ServeHTTP(w, configurationTestRequest(tc.method, tc.path, tc.body))
			if w.Code != 400 || s.writes.Load()+s.lookups.Load() != 0 {
				t.Fatal("typed invalid candidate dispatched", w.Code)
			}
		}
	})
	for _, raw := range []string{`null`, `1`, `"0"`, `"01"`, `"+1"`, `"1e1"`, `"9223372036854775808"`, `" 1"`} {
		t.Run("noncanonical_version_"+raw, func(t *testing.T) {
			h, _, s := configurationTestHandler(t)
			w := summaryWriter()
			h.ServeHTTP(w, configurationTestRequest("DELETE", configurationTestProvider, `{"expected_version":`+raw+`}`))
			if w.Code != 400 || s.writes.Load() != 0 {
				t.Fatal("noncanonical version reached service")
			}
		})
	}
	t.Run("maximum_version_reaches_original_rule", func(t *testing.T) {
		h, b, s := configurationTestHandler(t)
		original := fault(f.InvalidState)
		var got error
		b.problem = func(w http.ResponseWriter, r *http.Request, e error) { got = e; w.WriteHeader(409) }
		s.execute = func(_ context.Context, r configurationRequest) (mc.CommandReceipt, error) {
			if r.expected != math.MaxInt64 {
				t.Fatal("max version changed")
			}
			return mc.CommandReceipt{}, original
		}
		h.ServeHTTP(summaryWriter(), configurationTestRequest("DELETE", configurationTestProvider, `{"expected_version":"9223372036854775807"}`))
		if got != original || s.writes.Load() != 1 || s.lookups.Load() != 0 {
			t.Fatal("maximum int64 was silently rejected/retried")
		}
	})
	t.Run("policy_remains_service_owned", func(t *testing.T) {
		for _, tc := range []configurationTestCase{
			{"provider.create", "POST", configurationTestProviders, strings.Replace(configurationTestCases()[0].body, `"options":{}`, `"options":{"private_option":true}`, 1)},
			{"model.create", "POST", configurationTestModels, strings.Replace(configurationTestCases()[3].body, `"parameters":{}`, `"parameters":{"temperature":1}`, 1)},
		} {
			h, b, s := configurationTestHandler(t)
			original := fault(f.CapabilityUnsupported)
			var got error
			b.problem = func(w http.ResponseWriter, r *http.Request, e error) { got = e; w.WriteHeader(400) }
			s.execute = func(context.Context, configurationRequest) (mc.CommandReceipt, error) {
				return mc.CommandReceipt{}, original
			}
			h.ServeHTTP(summaryWriter(), configurationTestRequest(tc.method, tc.path, tc.body))
			if got != original || s.writes.Load() != 1 || s.lookups.Load() != 0 {
				t.Fatal("original service policy replaced")
			}
		}
	})
	t.Run("headers_queries_and_shapes", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			status int
			change func(*http.Request)
		}{
			{"no_key", 400, func(r *http.Request) { r.Header.Del("Idempotency-Key") }},
			{"multiple_keys", 400, func(r *http.Request) { r.Header.Add("Idempotency-Key", "another") }},
			{"media", 415, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
			{"encoding", 415, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
			{"query", 400, func(r *http.Request) { r.URL.RawQuery = "private=1" }},
			{"empty_query", 400, func(r *http.Request) { r.URL.ForceQuery = true }},
			{"bad_project", 400, func(r *http.Request) { r.URL.Path = "/api/v1/projects/bad/model-commands/lookup" }},
			{"extra_segment", 404, func(r *http.Request) { r.URL.Path = configurationTestLookup + "/extra" }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				h, _, s := configurationTestHandler(t)
				w := summaryWriter()
				r := configurationTestRequest("POST", configurationTestLookup, `{"command":"model.delete"}`)
				tc.change(r)
				h.ServeHTTP(w, r)
				if w.Code != tc.status || s.writes.Load()+s.lookups.Load() != 0 {
					t.Fatal("malformed request dispatched", w.Code)
				}
			})
		}
	})
	t.Run("exact_all_seven_body_caps", func(t *testing.T) {
		for _, tc := range configurationTestCases() {
			for _, extra := range []int{0, 1} {
				t.Run(fmt.Sprintf("%s_%d", tc.kind, extra), func(t *testing.T) {
					h, _, s := configurationTestHandler(t)
					body := tc.body + strings.Repeat(" ", configurationBodyBytes-len(tc.body)+extra)
					w := summaryWriter()
					h.ServeHTTP(w, configurationTestRequest(tc.method, tc.path, body))
					if extra == 0 {
						if w.Code != 200 || s.writes.Load()+s.lookups.Load() != 1 {
							t.Fatal("exact cap rejected", w.Code)
						}
					} else if w.Code != 413 || s.writes.Load()+s.lookups.Load() != 0 {
						t.Fatal("oversize reached service", w.Code)
					}
				})
			}
		}
	})
	t.Run("largest_safe_lookup_and_redaction", func(t *testing.T) {
		receipt := mc.CommandReceipt{Kind: "model.delete", ResourceID: projectHTTPTestModel, Version: math.MaxInt64, AffectedReferences: math.MaxInt64}
		raw, e := configurationEncodeLookup(context.Background(), receipt.Kind, CommandLookup{Found: true, Receipt: &receipt})
		if e != nil || len(raw) > configurationOutputBytes || !bytes.Contains(raw, []byte(`"version":"9223372036854775807"`)) {
			t.Fatal("maximum safe representation")
		}
		absent, e := configurationEncodeLookup(context.Background(), receipt.Kind, CommandLookup{})
		if e != nil || string(absent) != `{"found":false,"receipt":null}` {
			t.Fatal("absent shape")
		}
		t.Logf("maximum_safe_lookup_bytes=%d cap=%d", len(raw), configurationOutputBytes)
		var input configurationProviderInput
		if json.Unmarshal([]byte(configurationTestProviderJSON), &input) != nil {
			t.Fatal("decode")
		}
		var logs bytes.Buffer
		slog.New(slog.NewJSONHandler(&logs, nil)).Info("input", "value", input)
		if strings.Contains(fmt.Sprintf("%+v %#v", input, input)+logs.String(), "provider-private") {
			t.Fatal("unsafe diagnostic formatting")
		}
	})
	t.Run("actual_encoder_schema", configurationTestSchema)
}
func configurationTestSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_PROJECT_MODEL_CONFIGURATION_SCHEMA_PYTHON")
	if !filepath.IsAbs(python) {
		t.Fatal("fixed absolute schema interpreter required")
	}
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	samples := map[string]json.RawMessage{}
	for _, tc := range configurationTestCases() {
		h, _, _ := configurationTestHandler(t)
		w := summaryWriter()
		h.ServeHTTP(w, configurationTestRequest(tc.method, tc.path, tc.body))
		if w.Code != 200 {
			t.Fatal("encoder rejected schema sample", w.Code)
		}
		samples[tc.kind] = bytes.Clone(w.Body.Bytes())
	}
	receipt := mc.CommandReceipt{Kind: "model.delete", ResourceID: projectHTTPTestModel, Version: math.MaxInt64, AffectedReferences: math.MaxInt64}
	samples["found"], e = configurationEncodeLookup(context.Background(), receipt.Kind, CommandLookup{Found: true, Receipt: &receipt})
	if e != nil {
		t.Fatal(e)
	}
	data, e := json.Marshal(samples)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", configurationSchemaScript, root)
	cmd.Stdin = bytes.NewReader(data)
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal("standard schema validation", e, string(raw))
	}
	t.Log(string(raw))
}

const configurationSchemaScript = `
import copy,json,pathlib,sys
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base=pathlib.Path(sys.argv[1])/'api/openapi';path=base/'project-models.json'
s=json.loads(path.read_text());common=json.loads((base/'common.json').read_text())
r=Registry().with_resources([(path.as_uri(),Resource.from_contents(s,default_specification=DRAFT202012)),((base/'common.json').as_uri(),Resource.from_contents(common,default_specification=DRAFT202012))])
assert s['openapi']=='3.1.0'
assert sum(len(v) for v in s['paths'].values())==17
for schema in s['components']['schemas'].values(): Draft202012Validator.check_schema(schema)
data=json.load(sys.stdin);negative=0
for name,value in data.items():
 target='ConfigurationLookup' if name in ('lookup','found') else 'ConfigurationReceipt'
 v=Draft202012Validator({'$ref':path.as_uri()+'#/components/schemas/'+target},registry=r,format_checker=FormatChecker())
 assert not list(v.iter_errors(value)),name
 bad=copy.deepcopy(value);bad['input']={};assert list(v.iter_errors(bad));negative+=1
 if target=='ConfigurationReceipt':
  for k,x in [('version',1),('version','0'),('resource_id','bad'),('affected_references','-1'),('kind','selection.set')]:
   bad=copy.deepcopy(value);bad[k]=x;assert list(v.iter_errors(bad));negative+=1
 else:
  bad=copy.deepcopy(value);bad['found']=not bad['found'];assert list(v.iter_errors(bad));negative+=1
print(json.dumps({'actual_encoder_samples':len(data),'negative':negative,'status':'PASS'}))
`
