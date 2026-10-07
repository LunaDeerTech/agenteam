package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func projectHTTPValues(t *testing.T) (id.ProjectID, mc.ProviderView, mc.ModelView, AvailableChatModel) {
	t.Helper()
	p, _ := f.ParseID[id.Project](projectHTTPTestProject)
	scope, _ := id.InProject(p)
	at, _ := f.NewInstant(time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC))
	provider, _ := f.ParseID[mc.Provider](projectHTTPTestProvider)
	model, _ := f.ParseID[mc.Model](projectHTTPTestModel)
	pv := mc.ProviderView{ID: provider, Scope: scope, Input: mc.ProviderInput{Name: "private_provider_name", Protocol: mc.OpenAIChat, BaseURL: "https://private-endpoint.example/v1", Options: json.RawMessage(`{"private_option":"<>&"}`)}, Version: 9007199254740993, CreatedAt: at, UpdatedAt: at}
	count := mc.TokenCount(math.MaxInt64)
	mv := mc.ModelView{ID: model, ProviderID: provider, Scope: scope, Input: mc.ModelInput{Name: "model", ProviderModelID: "private_model_name", Type: mc.ChatModel, Parameters: json.RawMessage(`{"private_parameter":1}`), RequestOverwrite: json.RawMessage(`{"private_request":"value"}`), HeaderOverwrite: map[string]string{"X-Private-Canary": "private_header_value"}, Capabilities: mc.Capabilities{Streaming: true, ContextLength: &count}}, Version: math.MaxInt64, CreatedAt: at, UpdatedAt: at}
	av := AvailableChatModel{ID: model, ProviderID: provider, Scope: scope, Name: mv.Input.Name, ProviderName: pv.Input.Name, Version: mv.Version, Capabilities: mv.Input.Capabilities}
	if pv.Validate() != nil || mv.Validate() != nil {
		t.Fatal("invalid synthetic baseline")
	}
	return p, pv, mv, av
}
func TestProjectModelHTTPPureQuery(t *testing.T) {
	for _, q := range []string{"?", "?limit=0", "?limit=101", "?limit=01", "?limit=+1", "?limit=%2b1", "?limit=-1", "?limit=1.0", "?limit=１", "?limit=1&%6cimit=2", "?cursor=", "?cursor=x&cursor=y", "?limit", "?=1", "?limit=1&&cursor=x", "?limit=1;cursor=x", "?limit=1&", "?cursor=%00", "?cursor=%ff", "?cursor=%zz", "?provider_id=x", "?scope=system", "?enabled=true", "?cursor=" + strings.Repeat("x", 8193), "?cursor=" + strings.Repeat("x", 32769)} {
		t.Run(fmt.Sprint(len(q), "/", q[:min(len(q), 40)]), func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://project.example/", nil)
			r.URL.RawQuery = strings.TrimPrefix(q, "?")
			r.URL.ForceQuery = q == "?"
			if _, e := projectHTTPQuery(r, true); e == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
	for _, q := range []string{"", "?limit=1", "?limit=100", "?cursor=a%252Fb&limit=2"} {
		r := httptest.NewRequest("GET", "http://project.example/"+q, nil)
		got, e := projectHTTPQuery(r, true)
		if e != nil {
			t.Fatal(e)
		}
		if q == "" && got.Limit != 50 || strings.Contains(q, "cursor") && got.Cursor != "a%2Fb" {
			t.Fatal("default or double decode")
		}
	}
}
func TestProjectModelHTTPPureWireProjection(t *testing.T) {
	p, pv, mv, av := projectHTTPValues(t)
	ctx := context.Background()
	cases := []struct {
		name string
		body []byte
		err  error
	}{}
	b, e := projectHTTPEncodeProvider(ctx, p, pv.ID, pv)
	cases = append(cases, struct {
		name string
		body []byte
		err  error
	}{"Provider", b, e})
	b, e = projectHTTPEncodeModel(ctx, p, mv.ID, mv)
	cases = append(cases, struct {
		name string
		body []byte
		err  error
	}{"Model", b, e})
	b, e = projectHTTPEncodeAvailable(ctx, p, ProjectQuery{Limit: 50}, AvailableChatModelPage{Items: []AvailableChatModel{av}})
	cases = append(cases, struct {
		name string
		body []byte
		err  error
	}{"AvailableChatModelPage", b, e})
	for _, tc := range cases {
		if tc.err != nil || !json.Valid(tc.body) {
			t.Fatal(tc.name, tc.err)
		}
		if bytes.Contains(tc.body, []byte(`"version":922`)) {
			t.Fatal("numeric version")
		}
	}
	var item map[string]json.RawMessage
	var page struct{ Items []json.RawMessage }
	_ = json.Unmarshal(cases[2].body, &page)
	_ = json.Unmarshal(page.Items[0], &item)
	if len(item) != 7 {
		t.Fatal("catalog fields", len(item))
	}
	for _, secret := range []string{"private-endpoint", "private_option", "private_parameter", "private_request", "private_model_name", "X-Private-Canary", "private_header_value", "credential_ref", "created_at"} {
		if bytes.Contains(cases[2].body, []byte(secret)) {
			t.Fatal("catalog leak", secret)
		}
	}
	for _, body := range [][]byte{cases[1].body, cases[2].body} {
		for _, s := range []string{`"reasoning_efforts":[]`, `"structured_output_modes":[]`, `"max_output":null`, `"context_length":"9223372036854775807"`} {
			if !bytes.Contains(body, []byte(s)) {
				t.Fatal("missing exact nullable/array", s)
			}
		}
	}
	b, e = projectHTTPEncodeProviders(ctx, p, ProjectQuery{Limit: 50}, ProviderPage{})
	if e != nil || string(b) != `{"items":[],"next_cursor":null}` {
		t.Fatal("empty page", e, string(b))
	}
	caps := projectHTTPCaps(mv.Input.Capabilities)
	*caps.ContextLength = 1
	if *mv.Input.Capabilities.ContextLength != math.MaxInt64 {
		t.Fatal("caps pointer aliased")
	}
	projected, e := projectHTTPModelValue(mv, p)
	if e != nil {
		t.Fatal(e)
	}
	projected.Input.HeaderOverwrite["X-Private-Canary"] = "changed"
	projected.Input.Parameters[0] = '['
	if mv.Input.HeaderOverwrite["X-Private-Canary"] != "private_header_value" || mv.Input.Parameters[0] != '{' {
		t.Fatal("DTO cloned data alias")
	}
}
func TestProjectModelHTTPPureRejectWholeInvalidResult(t *testing.T) {
	p, pv, mv, av := projectHTTPValues(t)
	ctx := context.Background()
	other, _ := id.InProject(mustID[id.Project](t))
	wrongRef, _ := sc.NewCredentialRef(mustID[sc.Credential](t), id.SystemScope())
	for _, mutate := range []func(*mc.ProviderView){func(v *mc.ProviderView) { v.Scope = other }, func(v *mc.ProviderView) { v.ID = mc.ProviderID{} }, func(v *mc.ProviderView) { v.Input.CredentialRef = &wrongRef }, func(v *mc.ProviderView) { v.Input.Protocol = mc.OpenAIEmbeddings }, func(v *mc.ProviderView) { v.UpdatedAt, _ = f.NewInstant(v.CreatedAt.Time().Add(-time.Second)) }, func(v *mc.ProviderView) { v.Input.Options = []byte(`{"x":1,"x":2}`) }} {
		bad := pv.Clone()
		mutate(&bad)
		b, e := projectHTTPEncodeProvider(ctx, p, pv.ID, bad)
		if e == nil || b != nil {
			t.Fatal("invalid provider published")
		}
	}
	for _, mutate := range []func(*mc.ModelView){func(v *mc.ModelView) { v.Scope = other }, func(v *mc.ModelView) { v.Input.Type = mc.EmbeddingModel }, func(v *mc.ModelView) { v.Input.Capabilities.ParallelToolCalls = true }, func(v *mc.ModelView) { v.Input.Capabilities.ReasoningEfforts = []string{"high"} }, func(v *mc.ModelView) { v.Input.Capabilities.InputModalities = []string{"text", "text"} }, func(v *mc.ModelView) { v.Input.Capabilities.OutputModalities = []string{"future"} }, func(v *mc.ModelView) { v.Input.Capabilities.MaxOutput = new(mc.TokenCount) }, func(v *mc.ModelView) { v.ProviderID = mc.ProviderID{} }} {
		bad := mv.Clone()
		mutate(&bad)
		b, e := projectHTTPEncodeModel(ctx, p, mv.ID, bad)
		if e == nil || b != nil {
			t.Fatal("invalid model published")
		}
	}
	for _, mutate := range []func(*AvailableChatModel){func(v *AvailableChatModel) { v.Scope = other }, func(v *AvailableChatModel) { v.ProviderName = "" }, func(v *AvailableChatModel) { v.Name = string([]byte{255}) }, func(v *AvailableChatModel) {
		v.Capabilities.ReasoningEfforts = []string{"bad value"}
		v.Capabilities.Reasoning = true
	}, func(v *AvailableChatModel) { v.Version = 0 }} {
		bad := av
		mutate(&bad)
		b, e := projectHTTPEncodeAvailable(ctx, p, ProjectQuery{Limit: 50}, AvailableChatModelPage{Items: []AvailableChatModel{bad}})
		if e == nil || b != nil {
			t.Fatal("bad catalog row published")
		}
	}
	for _, page := range []ProviderPage{{Items: []mc.ProviderView{pv, pv}}, {NextCursor: "opaque"}, {Items: []mc.ProviderView{pv}, NextCursor: "opaque"}, {Items: make([]mc.ProviderView, 101)}} {
		b, e := projectHTTPEncodeProviders(ctx, p, ProjectQuery{Limit: 50}, page)
		if e == nil || b != nil {
			t.Fatal("invalid page published")
		}
	}
	// A bad final row rejects all 100; no success projection of the prefix.
	rows := make([]mc.ModelView, 100)
	for i := range rows {
		rows[i] = mv
		rows[i].ID = mustID[mc.Model](t)
		rows[i].CreatedAt, _ = f.NewInstant(mv.CreatedAt.Time().Add(-time.Duration(i) * time.Second))
	}
	rows[99].Input.Type = mc.ImageModel
	b, e := projectHTTPEncodeModels(ctx, p, ProjectQuery{Limit: 100}, ModelPage{Items: rows})
	if e == nil || b != nil {
		t.Fatal("partial 100-row success")
	}
}
func TestProjectModelHTTPPureRepresentationBudget(t *testing.T) {
	t.Run("checked_edges", func(t *testing.T) {
		b := projectHTTPBound{ctx: context.Background()}
		if !b.add(projectHTTPMaxRepresentation) || b.used != projectHTTPMaxRepresentation || b.add(1) || b.err == nil {
			t.Fatal("exact bound")
		}
		for _, n := range []int{-1, math.MaxInt} {
			b = projectHTTPBound{ctx: context.Background(), used: 1}
			if b.add(n) || b.err == nil {
				t.Fatal("integer overflow")
			}
		}
		b = projectHTTPBound{ctx: context.Background()}
		if b.room(math.MaxInt, 6) {
			t.Fatal("unsafe multiply")
		}
	})
	t.Run("string_escape", func(t *testing.T) {
		for _, v := range []string{"", `quotes"\\`, "\x00\n\t<>&\u2028\u2029", strings.Repeat("世", 128)} {
			b := projectHTTPBound{ctx: context.Background()}
			raw, e := json.Marshal(v)
			if e != nil || !b.text(v, 8192) || b.used != len(raw) {
				t.Fatal("JSON escape accounting", b.used, len(raw), e)
			}
		}
		b := projectHTTPBound{ctx: context.Background(), used: projectHTTPMaxRepresentation - 8}
		if !b.text("<", 1) || b.used != projectHTTPMaxRepresentation {
			t.Fatal("exact escaped boundary")
		}
		b = projectHTTPBound{ctx: context.Background(), used: projectHTTPMaxRepresentation - 7}
		if b.text("<", 1) {
			t.Fatal("escaped bound +1")
		}
	})
	t.Run("raw_HTML_upper", func(t *testing.T) {
		for _, s := range []string{`{"x":"<>&"}`, "{\"x\":\"\u2028\u2029\"}", `{ "x": "\\u003c" }`} {
			raw := json.RawMessage(s)
			wire, e := json.Marshal(raw)
			b := projectHTTPBound{ctx: context.Background()}
			if e != nil || !b.raw(raw) || b.used < len(wire) {
				t.Fatal("RawMessage underestimated")
			}
		}
	})
	t.Run("legal_unbounded_efforts", func(t *testing.T) {
		p, _, mv, _ := projectHTTPValues(t)
		const n = 1 << 20
		efforts := make([]string, n)
		for i := range efforts {
			efforts[i] = fmt.Sprintf("r%08x", i)
		}
		mv.Input.Capabilities.Reasoning = true
		mv.Input.Capabilities.ReasoningEfforts = efforts
		if e := mv.Input.Capabilities.Validate(); e != nil {
			t.Fatal("counterexample not domain-legal", e)
		}
		if 1+12*n != 12582913 {
			t.Fatal("counterexample size")
		}
		body, e := projectHTTPEncodeModel(context.Background(), p, mv.ID, mv)
		if e == nil || body != nil {
			t.Fatal("legal oversize not zero projection")
		}
		requireCode(t, e, f.DependencyUnavailable)
		// Admission itself rejects a huge container by len before inspecting invalid
		// tokens; this does not claim bounds on this fixture/domain allocation or RSS.
		huge := make([]string, projectHTTPMaxRepresentation/3+1)
		b := projectHTTPBound{ctx: context.Background()}
		if b.strings(huge, 32) || b.used != 0 {
			t.Fatal("container rejection traversed/admitted before lower bound")
		}
	})
	t.Run("cancelled_before_clone", func(t *testing.T) {
		p, _, mv, _ := projectHTTPValues(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if b, e := projectHTTPEncodeModel(ctx, p, mv.ID, mv); b != nil || e != context.Canceled {
			t.Fatal("late projection", e)
		}
	})
}

func TestProjectModelHTTPPureSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_PROJECT_MODEL_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed installed schema interpreter required")
	}
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	p, pv, mv, av := projectHTTPValues(t)
	ctx := context.Background()
	inputs := map[string]json.RawMessage{}
	inputs["Provider"], e = projectHTTPEncodeProvider(ctx, p, pv.ID, pv)
	if e != nil {
		t.Fatal(e)
	}
	inputs["Model"], e = projectHTTPEncodeModel(ctx, p, mv.ID, mv)
	if e != nil {
		t.Fatal(e)
	}
	inputs["AvailableChatModelPage"], e = projectHTTPEncodeAvailable(ctx, p, ProjectQuery{Limit: 50}, AvailableChatModelPage{Items: []AvailableChatModel{av}})
	if e != nil {
		t.Fatal(e)
	}
	inputs["ProviderPage"], _ = projectHTTPEncodeProviders(ctx, p, ProjectQuery{Limit: 50}, ProviderPage{})
	inputs["ModelPage"], _ = projectHTTPEncodeModels(ctx, p, ProjectQuery{Limit: 1}, ModelPage{Items: []mc.ModelView{mv}, NextCursor: "opaque"})
	raw, _ := json.Marshal(inputs)
	run, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(run, python, "-c", projectHTTPTestSchemaScript, root)
	cmd.Stdin = bytes.NewReader(raw)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("schema %v %s", e, out)
	}
	t.Log(strings.TrimSpace(string(out)))
}

const projectHTTPTestSchemaScript = `import copy,json,sys
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base='https://project-model.test/'
r=Registry()
for n in ['common.json','project-models.json']:
 d=json.loads((Path(sys.argv[1])/'api/openapi'/n).read_text());r=r.with_resource(base+n,Resource.from_contents(d,default_specification=DRAFT202012))
values=json.load(sys.stdin)
def valid(name,v):
 s={'$ref':base+'project-models.json#/components/schemas/'+name}
 return not list(Draft202012Validator(s,registry=r,format_checker=FormatChecker()).iter_errors(v))
for n,v in values.items(): assert valid(n,v),n
negative=[]
for n in ['Provider','Model']:
 for key,val in [('extra',True),('version',1),('version','9223372036854775808'),('id','invalid'),('scope',{'kind':'system'})]:
  v=copy.deepcopy(values[n]);v[key]=val;negative.append((n,v))
for key in ['base_url','protocol','options','credential_ref','provider_model_id','parameters','request_overwrite','header_overwrite','created_at']:
 v=copy.deepcopy(values['AvailableChatModelPage']);v['items'][0][key]='private';negative.append(('AvailableChatModelPage',v))
for field in ['input_modalities','output_modalities','reasoning_efforts','structured_output_modes','context_length','max_output']:
 v=copy.deepcopy(values['Model']);del v['input']['capabilities'][field];negative.append(('Model',v))
for field,val in [('input_modalities',None),('input_modalities',['text','text']),('input_modalities',['future']),('structured_output_modes',['future']),('reasoning_efforts',['x']),('parallel_tool_calls',True),('context_length',1),('max_output','0')]:
 v=copy.deepcopy(values['Model']);v['input']['capabilities'][field]=val;negative.append(('Model',v))
for n,v in negative: assert not valid(n,v),('accepted negative',n)
system=copy.deepcopy(values['AvailableChatModelPage']);system['items'][0]['scope']={'kind':'system'};assert valid('AvailableChatModelPage',system)
print('SCHEMA_PASS positive='+str(len(values)+1)+' negative='+str(len(negative)))
`
