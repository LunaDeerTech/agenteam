package adapter

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func structuredInput(t *testing.T) (Request, CallOptions) {
	t.Helper()
	r, o := unitInput(t)
	r.Snapshot.Identity.AdapterRevision = OpenAIChatStructuredRevision
	r.Snapshot.Capabilities.StructuredOutputModes = []string{"json_schema", "text"}
	r.ResponseFormat = mc.ResponseFormat{Kind: "json_schema", Name: "memory_result", Schema: schemaJSON(t, schemaObject(map[string]any{"v": map[string]any{"type": "integer"}}))}
	return r, o
}
func TestStructuredWireRequestAndProfile(t *testing.T) {
	r, o := structuredInput(t)
	req, _, schema, err := prepareWithSchema(context.Background(), r, o)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(req.Body)
	req.Body.Close()
	var body struct {
		Format struct {
			Type   string `json:"type"`
			Schema struct {
				Name   string          `json:"name"`
				Raw    json.RawMessage `json:"schema"`
				Strict bool            `json:"strict"`
			} `json:"json_schema"`
		} `json:"response_format"`
		Stream bool `json:"stream"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Format.Type != "json_schema" || !body.Format.Schema.Strict || body.Format.Schema.Name != "memory_result" || body.Stream || !bytes.Equal(body.Format.Schema.Raw, r.ResponseFormat.Schema) {
		t.Fatal("native strict format changed")
	}
	for i := range r.ResponseFormat.Schema {
		r.ResponseFormat.Schema[i] = 'x'
	}
	if err := schema.complete(context.Background(), `{"v":9007199254740993}`, "stop"); err != nil {
		t.Fatal("compiled schema alias", err)
	}
	requireModel(t, schema.complete(context.Background(), `{"v":"x"}`, "stop"), "provider_error", "wire_protocol_invalid")
	for _, mode := range []ResponseMode{JSONResponse, SSEResponse} {
		r, o := structuredInput(t)
		r.Mode = mode
		r.ResponseFormat = mc.ResponseFormat{Kind: "text"}
		req, _, program, err := prepareWithSchema(context.Background(), r, o)
		if err != nil || program != nil {
			t.Fatal("new revision text path", err)
		}
		raw, _ := io.ReadAll(req.Body)
		req.Body.Close()
		if bytes.Contains(raw, []byte("response_format")) {
			t.Fatal("text acquired structured wrapper")
		}
	}
	for _, tc := range []struct {
		name    string
		edit    func(*Request)
		invalid bool
	}{
		{"old_revision", func(r *Request) { r.Snapshot.Identity.AdapterRevision = OpenAIChatTextRevision }, false},
		{"unknown_revision", func(r *Request) { r.Snapshot.Identity.AdapterRevision = "future" }, false},
		{"missing_capability", func(r *Request) { r.Snapshot.Capabilities.StructuredOutputModes = []string{"text"} }, false},
		{"invalid_native_name", func(r *Request) { r.ResponseFormat.Name = "c0.valid:native.invalid" }, true},
		{"unsupported_keyword", func(r *Request) {
			r.ResponseFormat.Schema = json.RawMessage(`{"type":"object","$ref":"https://example.invalid/schema"}`)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, o := structuredInput(t)
			tc.edit(&r)
			budget := NewBudget()
			budget.StopAdmission()
			// No valid transport exists. A schema error must precede even the
			// stopped admission check, with no accepted work or async client.
			a := &OpenAIChat{budget: budget}
			x, err := a.Start(context.Background(), r, o)
			if x != nil || !budget.Joined() {
				t.Fatal("rejected input accepted work")
			}
			if tc.invalid {
				requireFault(t, err, f.InvalidArgument)
			} else {
				requireModel(t, err, "unsupported_feature", "wire_unsupported_feature")
			}
		})
	}
	r, o = structuredInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	x, err := (&OpenAIChat{budget: NewBudget()}).Start(ctx, r, o)
	if x != nil {
		t.Fatal("cancelled input accepted")
	}
	requireModel(t, err, "cancelled", "wire_cancelled")
	for _, value := range []any{r, Result{Text: "synthetic-structured-private-canary"}, Event{Text: "synthetic-structured-private-canary"}, schema} {
		if value == schema {
			continue
		} // The compiled program is private, never a public payload.
		encoded, _ := json.Marshal(value)
		if strings.Contains(fmt.Sprintf("%+v %#v %s", value, value, encoded), "synthetic-structured-private-canary") {
			t.Fatal("unsafe public format")
		}
	}
}

func TestStructuredPinnedSources(t *testing.T) {
	raw, err := os.ReadFile("testdata/openai-chat-structured-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Revision, Protocol, Profile, Commit string
		Sources                             []struct {
			Path, URL, SHA256 string
			GitBlob           string `json:"git_blob"`
			Source            string `json:"source_base64"`
			Bytes             int
		}
		Reused struct{ SHA256 string } `json:"reused_text_manifest"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Revision != OpenAIChatStructuredRevision || m.Protocol != string(mc.OpenAIChat) || m.Profile != string(mc.OpenAIChatV1) || m.Commit != "becc1d20eed83c1b8d85e15dc131a372d9dc7813" || len(m.Sources) != 2 {
		t.Fatal("structured source identity")
	}
	expected := map[string]string{"src/openai/types/shared_params/response_format_json_schema.py": "4a81522587ed5fa7571f376854c87870f8b0a5e7f6e473eb9fc2e297bfbd856a", "src/openai/lib/_pydantic.py": "09fd2f1b0b9674d12e21483c58d4448d644602930e6e097c0e374b5387f98c07"}
	for _, source := range m.Sources {
		body, err := base64.StdEncoding.DecodeString(source.Source)
		sum := sha256.Sum256(body)
		git := sha1.New()
		git.Write([]byte("blob " + strconv.Itoa(len(body)) + "\x00"))
		git.Write(body)
		if err != nil || len(body) != source.Bytes || expected[source.Path] != source.SHA256 || hex.EncodeToString(sum[:]) != source.SHA256 || hex.EncodeToString(git.Sum(nil)) != source.GitBlob || source.URL != "https://github.com/openai/openai-python/blob/"+m.Commit+"/"+source.Path {
			t.Fatal("SDK source bytes do not match pinned identity")
		}
		delete(expected, source.Path)
	}
	if len(expected) != 0 {
		t.Fatal("duplicate/missing source")
	}
	old, err := os.ReadFile("testdata/openai-chat-text-v1.json")
	sum := sha256.Sum256(old)
	if err != nil || hex.EncodeToString(sum[:]) != m.Reused.SHA256 {
		t.Fatal("reused text provenance changed")
	}
}
