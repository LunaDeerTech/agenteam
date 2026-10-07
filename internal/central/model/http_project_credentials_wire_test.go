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

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestProjectCredentialHTTPPureStrictWire(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		change                   func(*http.Request)
	}{
		{"create", "POST", credentialTestCollection, `{"value":"private"}`, 200, nil},
		{"rotate", "PUT", credentialTestDetail, `{"expected_version":"1","value":"private"}`, 200, nil},
		{"delete", "DELETE", credentialTestDetail, `{"expected_version":"1"}`, 200, nil},
		{"lookup_create", "POST", credentialTestLookup, `{"kind":"create"}`, 200, nil},
		{"lookup_update", "POST", credentialTestLookup, `{"kind":"update","credential_id":"` + credentialTestID + `","expected_version":"1"}`, 200, nil},
		{"lookup_delete", "POST", credentialTestLookup, `{"kind":"delete","credential_id":"` + credentialTestID + `","expected_version":"1"}`, 200, nil},
		{"missing", "POST", credentialTestCollection, `{}`, 400, nil}, {"null", "POST", credentialTestCollection, `{"value":null}`, 400, nil}, {"empty", "POST", credentialTestCollection, `{"value":""}`, 400, nil},
		{"number", "POST", credentialTestCollection, `{"value":12}`, 400, nil}, {"unknown", "POST", credentialTestCollection, `{"value":"private","scope":"system"}`, 400, nil},
		{"create_id_null", "POST", credentialTestCollection, `{"value":"private","credential_id":null}`, 400, nil}, {"create_version", "POST", credentialTestCollection, `{"value":"private","expected_version":"1"}`, 400, nil},
		{"duplicate", "POST", credentialTestCollection, `{"value":"private","value":"other"}`, 400, nil}, {"case", "POST", credentialTestCollection, `{"Value":"private"}`, 400, nil},
		{"trailing", "POST", credentialTestCollection, `{"value":"private"}{}`, 400, nil}, {"utf8", "POST", credentialTestCollection, "{\"value\":\"" + string([]byte{255}) + "\"}", 400, nil},
		{"media", "POST", credentialTestCollection, `{"value":"private"}`, 415, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{"encoding", "POST", credentialTestCollection, `{"value":"private"}`, 415, func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") }},
		{"no_key", "POST", credentialTestCollection, `{"value":"private"}`, 400, func(r *http.Request) { r.Header.Del("Idempotency-Key") }},
		{"two_keys", "POST", credentialTestCollection, `{"value":"private"}`, 400, func(r *http.Request) { r.Header.Add("Idempotency-Key", "other") }},
		{"query", "POST", credentialTestCollection + "?x=y", `{"value":"private"}`, 400, nil}, {"empty_query", "POST", credentialTestCollection + "?", `{"value":"private"}`, 400, nil},
		{"bad_project", "POST", "/api/v1/projects/bad/model-credentials", `{"value":"private"}`, 400, nil}, {"bad_ref", "DELETE", credentialTestCollection + "/bad", `{"expected_version":"1"}`, 400, nil},
		{"no_list", "GET", credentialTestCollection, "", 405, nil}, {"no_patch", "PATCH", credentialTestDetail, "", 405, nil}, {"no_lookup_get", "GET", credentialTestLookup, "", 405, nil},
		{"null_ref", "POST", credentialTestLookup, `{"kind":"create","credential_id":null}`, 400, nil}, {"null_expected", "POST", credentialTestLookup, `{"kind":"create","expected_version":null}`, 400, nil},
		{"lookup_value", "POST", credentialTestLookup, `{"kind":"create","value":"private"}`, 400, nil}, {"wrong_kind", "POST", credentialTestLookup, `{"kind":"rotate"}`, 400, nil},
		{"lookup_missing_ref", "POST", credentialTestLookup, `{"kind":"update","expected_version":"1"}`, 400, nil},
		{"body_get", "GET", credentialTestDetail, "x", 400, nil}, {"unknown_length", "GET", credentialTestDetail, "", 400, func(r *http.Request) { r.ContentLength = -1 }},
		{"chunked", "GET", credentialTestDetail, "", 400, func(r *http.Request) { r.TransferEncoding = []string{"chunked"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, s := credentialTestHandler(t)
			r := credentialTestRequest(tc.method, tc.path, tc.body)
			if tc.change != nil {
				tc.change(r)
			}
			w := summaryWriter()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatal("status", w.Code, tc.status)
			}
			if tc.status != 200 && s.writes.Load()+s.reads.Load()+s.lookups.Load() != 0 {
				t.Fatal("rejected input reached service")
			}
			if strings.Contains(w.Body.String(), "private") {
				t.Fatal("material in response")
			}
			w.cleared(t)
		})
	}
	for _, raw := range []string{"", `null`, `1`, `"0"`, `"01"`, `"+1"`, `"1e1"`, `"9223372036854775807"`, `"9223372036854775808"`, `" 1"`} {
		for _, lookup := range []bool{false, true} {
			t.Run(fmt.Sprintf("version_%d_%t", len(raw), lookup), func(t *testing.T) {
				h, _, s := credentialTestHandler(t)
				body := `{"expected_version":` + raw + `}`
				path, method := credentialTestDetail, "DELETE"
				if lookup {
					body = `{"kind":"update","credential_id":"` + credentialTestID + `","expected_version":` + raw + `}`
					path, method = credentialTestLookup, "POST"
				}
				w := summaryWriter()
				h.ServeHTTP(w, credentialTestRequest(method, path, body))
				if w.Code != 400 || s.writes.Load()+s.lookups.Load() != 0 {
					t.Fatal("noncanonical version accepted")
				}
			})
		}
	}
}
func TestProjectCredentialHTTPPureMaximumMaterialAndBodyCaps(t *testing.T) {
	// Keep only length/boolean assertions in failures; never dump request bytes.
	maximum := `{"value":"` + strings.Repeat(`\u0000`, sc.MaxValueBytes-1) + `\u0001"}`
	if len(maximum) >= credentialMaterialBodyBytes {
		t.Fatal("maximum legal shell exceeds cap")
	}
	for _, method := range []string{"POST", "PUT"} {
		h, _, s := credentialTestHandler(t)
		body, path := maximum, credentialTestCollection
		if method == "PUT" {
			body = `{"expected_version":"1",` + strings.TrimPrefix(maximum, "{")
			path = credentialTestDetail
		}
		s.execute = func(_ context.Context, r sc.WriteRequest) (sc.MutationResult, error) {
			if e := r.Value.Use(func(v []byte) error {
				if len(v) != sc.MaxValueBytes || v[len(v)-1] != 1 || v[len(v)-2] != 0 {
					t.Fatal("maximum bytes truncated")
				}
				return nil
			}); e != nil {
				t.Fatal("maximum material unavailable")
			}
			return sc.MutationResult{Metadata: sc.Metadata{CredentialRef: credentialTestRef(), Purpose: sc.Model, Version: r.ExpectedVersion + 1}}, nil
		}
		w := summaryWriter()
		h.ServeHTTP(w, credentialTestRequest(method, path, body))
		if w.Code != 200 || s.writes.Load() != 1 {
			t.Fatal("legal maximum rejected")
		}
		t.Logf("maximum_%s raw_bytes=%d decoded_bytes=%d terminal_byte_verified=true", method, len(body), sc.MaxValueBytes)
	}
	for _, tc := range []struct {
		method, path, body string
		cap                int
	}{{"POST", credentialTestCollection, `{"value":"private"}`, credentialMaterialBodyBytes}, {"PUT", credentialTestDetail, `{"expected_version":"1","value":"private"}`, credentialMaterialBodyBytes}, {"DELETE", credentialTestDetail, `{"expected_version":"1"}`, credentialSmallBodyBytes}, {"POST", credentialTestLookup, `{"kind":"create"}`, credentialSmallBodyBytes}} {
		for _, extra := range []int{0, 1} {
			t.Run(tc.method+tc.path+fmt.Sprint(extra), func(t *testing.T) {
				h, _, s := credentialTestHandler(t)
				body := tc.body + strings.Repeat(" ", tc.cap-len(tc.body)+extra)
				w := summaryWriter()
				h.ServeHTTP(w, credentialTestRequest(tc.method, tc.path, body))
				want := 200
				if extra != 0 {
					want = 413
				}
				if w.Code != want {
					t.Fatal("body cap", len(body), w.Code)
				}
				if extra != 0 && s.writes.Load()+s.reads.Load()+s.lookups.Load() != 0 {
					t.Fatal("oversized input reached service")
				}
			})
		}
	}
	for _, value := range []string{strings.Repeat("x", sc.MaxValueBytes+1), strings.Repeat("界", sc.MaxValueBytes/3+1)} {
		h, _, s := credentialTestHandler(t)
		raw, _ := json.Marshal(struct {
			Value string `json:"value"`
		}{value})
		w := summaryWriter()
		h.ServeHTTP(w, credentialTestRequest("POST", credentialTestCollection, string(raw)))
		if w.Code != 400 || s.writes.Load() != 0 {
			t.Fatal("decoded byte bound ignored")
		}
	}
}
func TestProjectCredentialHTTPPureSafeProjection(t *testing.T) {
	h, b, _ := credentialTestHandler(t)
	_ = h
	ref := credentialTestRef()
	scope := ref.Details().Scope
	key, _ := credentialIdentity(b.actor, scope, "key", sc.Update)
	r := sc.WriteCommandLookupRequest{Actor: b.actor, Scope: scope, Identity: key, Kind: sc.Update, Ref: ref, ExpectedVersion: math.MaxInt64 - 1, Purpose: sc.Model}
	value := sc.MutationResult{Metadata: sc.Metadata{CredentialRef: ref, Purpose: sc.Model, Version: math.MaxInt64}}
	for _, mutate := range []func(*sc.MutationResult){func(v *sc.MutationResult) { v.Metadata.Version = 1 }, func(v *sc.MutationResult) { v.Metadata.Purpose = sc.SMTP }, func(v *sc.MutationResult) { v.Deleted = true }, func(v *sc.MutationResult) { v.Metadata.CredentialRef = sc.CredentialRef{} }, func(v *sc.MutationResult) {
		v.Metadata.CredentialRef, _ = sc.NewCredentialRef(ref.Details().ID, id.SystemScope())
	}} {
		bad := value
		mutate(&bad)
		if data, e := credentialEncodeMutation(context.Background(), r, bad); e == nil || data != nil {
			t.Fatal("malformed mutation published")
		}
	}
	for _, o := range []sc.WriteCommandObservation{{Observed: true}, {Result: &value}} {
		if data, e := credentialEncodeObservation(context.Background(), r, o); e == nil || data != nil {
			t.Fatal("bad observation union published")
		}
	}
	data, e := credentialEncodeObservation(context.Background(), r, sc.WriteCommandObservation{Observed: true, Result: &value})
	if e != nil || len(data) > credentialOutputBytes || !bytes.Contains(data, []byte(`"version":"9223372036854775807"`)) {
		t.Fatal("largest safe result failed")
	}
	t.Logf("maximum_safe_observation_bytes=%d cap=%d", len(data), credentialOutputBytes)
	absent, e := credentialEncodeObservation(context.Background(), r, sc.WriteCommandObservation{})
	if e != nil || string(absent) != `{"observed":false,"result":null}` {
		t.Fatal("absent observation shape")
	}
	for _, p := range []sc.Purpose{sc.SMTP, sc.Purpose("invalid")} {
		v := value.Metadata
		v.Purpose = p
		if data, e := credentialEncodeMetadata(context.Background(), ref, v); e == nil || data != nil {
			t.Fatal("foreign or damaged purpose published")
		}
	}
	var input httpCredentialValue
	canary := "unique_private_material_canary"
	if json.Unmarshal([]byte(`"`+canary+`"`), &input) != nil {
		t.Fatal("value decode")
	}
	encoded, _ := json.Marshal(struct {
		Value httpCredentialValue `json:"value"`
	}{input})
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("input", "value", input)
	for _, s := range []string{fmt.Sprintf("%+v %#v", input, input), string(encoded), log.String()} {
		if strings.Contains(s, canary) {
			t.Fatal("input formatting leaked material")
		}
	}
}
func TestProjectCredentialHTTPPureSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_PROJECT_CREDENTIAL_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("fixed standard schema interpreter required")
	}
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	_, b, _ := credentialTestHandler(t)
	ref := credentialTestRef()
	key, _ := credentialIdentity(b.actor, ref.Details().Scope, "key", sc.Create)
	request := sc.WriteCommandLookupRequest{Actor: b.actor, Scope: ref.Details().Scope, Identity: key, Kind: sc.Create, Purpose: sc.Model}
	result := sc.MutationResult{Metadata: sc.Metadata{CredentialRef: ref, Purpose: sc.Model, Version: 1}}
	metadata, _ := credentialEncodeMetadata(context.Background(), ref, result.Metadata)
	mutation, _ := credentialEncodeMutation(context.Background(), request, result)
	observation, _ := credentialEncodeObservation(context.Background(), request, sc.WriteCommandObservation{Observed: true, Result: &result})
	none, _ := credentialEncodeObservation(context.Background(), request, sc.WriteCommandObservation{})
	input, _ := json.Marshal(map[string]json.RawMessage{"Metadata": metadata, "Mutation": mutation, "Observation": observation, "Absent": none})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", credentialSchemaScript, root)
	cmd.Stdin = bytes.NewReader(input)
	output, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal("standard schema validation failed", e, string(output))
	}
}

const credentialSchemaScript = `
import json,pathlib,sys
from jsonschema import Draft202012Validator,FormatChecker
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
base=pathlib.Path(sys.argv[1])/'api/openapi'
path=base/'project-model-credentials.json'
s=json.loads(path.read_text());common=json.loads((base/'common.json').read_text())
r=Registry().with_resources([(path.as_uri(),Resource.from_contents(s,default_specification=DRAFT202012)),((base/'common.json').as_uri(),Resource.from_contents(common,default_specification=DRAFT202012))])
assert s['openapi']=='3.1.0'
assert sum(len(v) for v in s['paths'].values())==6
for schema in s['components']['schemas'].values(): Draft202012Validator.check_schema(schema)
data=json.load(sys.stdin)
for name,value in data.items():
 target='Observation' if name=='Absent' else name
 validator=Draft202012Validator({'$ref':path.as_uri()+'#/components/schemas/'+target},registry=r,format_checker=FormatChecker())
 assert not list(validator.iter_errors(value))
 bad=dict(value);bad['value']='never_evidence_material';assert list(validator.iter_errors(bad))
print('safe_projection_schema_pass')
`
