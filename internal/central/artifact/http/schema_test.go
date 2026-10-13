package artifacthttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/dlclark/regexp2"
	js "github.com/santhosh-tekuri/jsonschema/v6"
)

type noSchemaNetwork struct{}

func (noSchemaNetwork) Load(string) (any, error) {
	return nil, errors.New("only checked local OpenAPI resources are allowed")
}

type schemaRegexp struct{ expression *regexp2.Regexp }

func (r schemaRegexp) String() string { return r.expression.String() }
func (r schemaRegexp) MatchString(value string) bool {
	ok, err := r.expression.MatchString(value)
	return err == nil && ok
}

func TestArtifactHTTPActualOpenAPI(t *testing.T) {
	compiler := js.NewCompiler()
	compiler.DefaultDraft(js.Draft2020)
	compiler.UseLoader(noSchemaNetwork{})
	compiler.UseRegexpEngine(func(pattern string) (js.Regexp, error) {
		r, err := regexp2.Compile(pattern, regexp2.ECMAScript)
		if err != nil {
			return nil, err
		}
		r.MatchTimeout = 100 * time.Millisecond
		return schemaRegexp{r}, nil
	})
	t.Cleanup(regexp2.StopTimeoutClock)
	const base = "https://schema.invalid/"
	var document map[string]any
	for _, name := range []string{"artifact-owner.json", "common.json"} {
		raw, err := os.ReadFile(filepath.Join("../../../..", "api/openapi", name))
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err = json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if err = compiler.AddResource(base+name, value); err != nil {
			t.Fatal(err)
		}
		if name == "artifact-owner.json" {
			document = value.(map[string]any)
		}
	}
	paths := document["paths"].(map[string]any)
	if len(paths) != 5 {
		t.Fatal("unexpected public route")
	}
	for _, value := range paths {
		p := value.(map[string]any)
		if post, ok := p["post"].(map[string]any); ok {
			if len(p) != 2 || post["requestBody"].(map[string]any)["x-max-encoded-json-bytes"] != float64(maxCommandRequestBytes) {
				t.Fatal("command route or recovery input bound changed")
			}
			continue
		}
		if len(p) != 3 || p["get"] == nil || p["head"] == nil {
			t.Fatal("write method exposed")
		}
		head := p["head"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)
		if head["content"] != nil {
			t.Fatal("HEAD schema exposes body")
		}
	}
	compiled := map[string]*js.Schema{}
	for _, name := range []string{"ArtifactPage", "ArtifactRead", "ArtifactContentIntent", "ArtifactContentCreated", "ArtifactCreationLookup", "ArtifactCreationCancelled"} {
		s, err := compiler.Compile(base + "artifact-owner.json#/components/schemas/" + name)
		if err != nil {
			t.Fatal(err)
		}
		compiled[name] = s
	}
	validate := func(name string, raw []byte, want bool) {
		t.Helper()
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		if got := compiled[name].Validate(value) == nil; got != want {
			t.Fatalf("%s validation=%v wanted=%v", name, got, want)
		}
	}
	list, err := encodeList(context.Background(), testReference().ProjectID, ac.ListFilter{}, f.PageRequest{Limit: 1}, []ac.Metadata{testMetadata()}, "cursor")
	if err != nil {
		t.Fatal(err)
	}
	validate("ArtifactPage", list, true)
	h, _, _ := testHandler()
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", testPath(true), nil), w) || w.Code != 200 {
		t.Fatal("actual handler control failed")
	}
	raw := append([]byte(nil), w.Body.Bytes()...)
	validate("ArtifactRead", raw, true)
	for _, mode := range []string{"stored-object-id", "extra", "missing-reference", "number-offset", "negative-offset", "leading-zero-offset", "newline-offset", "double-creator", "operation-without-execution", "file-with-text", "image-invalid-utf8", "unknown-projection"} {
		t.Run(mode, func(t *testing.T) {
			var value map[string]any
			_ = json.Unmarshal(raw, &value)
			metadata := value["artifact"].(map[string]any)
			switch mode {
			case "stored-object-id":
				metadata["object"].(map[string]any)["id"] = testMetadata().Details().Object.ID.String()
			case "extra":
				value["url"] = "private URL canary"
			case "missing-reference":
				delete(metadata["artifact_ref"].(map[string]any), "file_id")
			case "number-offset":
				value["offset"] = 0
			case "negative-offset":
				value["offset"] = "-1"
			case "leading-zero-offset":
				value["offset"] = "00"
			case "newline-offset":
				value["offset"] = "0\n"
			case "double-creator":
				metadata["created_by_agent_id"] = metadata["created_by_user_id"]
			case "operation-without-execution":
				metadata["source_operation_id"] = metadata["created_by_user_id"]
			case "file-with-text":
				value["projection"] = "file_ref"
				value["next_offset"] = "0"
			case "image-invalid-utf8":
				value["projection"] = "image_ref"
				value["next_offset"] = "0"
				value["invalid_utf8"] = true
				delete(value, "text")
			case "unknown-projection":
				value["projection"] = "download_url"
			}
			mutated, _ := json.Marshal(value)
			validate("ArtifactRead", mutated, false)
		})
	}
	for _, projection := range []ac.Projection{ac.FileProjection, ac.ImageProjection} {
		d := ac.ReadDetails{Artifact: testMetadata(), Projection: projection}
		if projection == ac.FileProjection {
			d.InvalidUTF8 = true
		}
		raw, err := encodeRead(context.Background(), testReference(), ac.PreviewRequest{Limit: 8192}, readWith(t, d))
		if err != nil {
			t.Fatal(err)
		}
		validate("ArtifactRead", raw, true)
	}
	t.Run("content-command-responses", func(t *testing.T) {
		for _, action := range []string{"create-content", "lookup", "cancel"} {
			for _, state := range []ac.CreationState{ac.CreationNotObserved, ac.CreationPending, ac.CreationCommitted, ac.CreationCancelRequested, ac.CreationCancelled} {
				if action == "cancel" && state == ac.CreationCommitted {
					continue
				}
				h, _, p := contentHandler(t)
				p.lookup.State = state
				if state == ac.CreationCommitted {
					p.lookup.Artifact = &p.create
				}
				if state == ac.CreationPending {
					p.cleanup = "pending"
				}
				w := commandResponse(t, h, contentRequest(action, contentJSON))
				if w.Code != 200 {
					t.Fatal("actual command handler failed", action, state, w.Code)
				}
				name := map[string]string{"create-content": "ArtifactContentCreated", "lookup": "ArtifactCreationLookup", "cancel": "ArtifactCreationCancelled"}[action]
				validate(name, w.Body.Bytes(), true)
			}
		}
		for _, tc := range []struct{ schema, body string }{
			{"ArtifactCreationLookup", `{"state":"committed","artifact":null}`},
			{"ArtifactCreationLookup", `{"state":"unknown","artifact":null}`},
			{"ArtifactCreationLookup", `{"state":"pending"}`},
			{"ArtifactCreationLookup", `{"state":"cancel_requested","artifact":null,"cleanup":"completed"}`},
			{"ArtifactCreationCancelled", `{"state":"not_observed","cleanup":"completed"}`},
			{"ArtifactCreationCancelled", `{"state":"cancel_requested","cleanup":"completed"}`},
			{"ArtifactCreationCancelled", `{"state":"cancelled","cleanup":"pending"}`},
			{"ArtifactCreationCancelled", `{"state":"cancelled","cleanup":null}`},
			{"ArtifactCreationCancelled", `{"state":"unknown","cleanup":null}`},
		} {
			validate(tc.schema, []byte(tc.body), false)
		}
		h, _, _ := contentHandler(t)
		w := commandResponse(t, h, contentRequest("create-content", contentJSON))
		for _, mode := range []string{"source", "storage", "kind", "creator"} {
			var value map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &value)
			metadata := value["artifact"].(map[string]any)
			switch mode {
			case "source":
				metadata["source_execution_id"] = metadata["created_by_user_id"]
			case "storage":
				metadata["object"].(map[string]any)["id"] = testMetadata().Details().Object.ID.String()
			case "kind":
				metadata["kind"] = "user_upload"
			case "creator":
				delete(metadata, "created_by_user_id")
			}
			raw, _ := json.Marshal(value)
			validate("ArtifactContentCreated", raw, false)
		}
	})
	t.Run("content-command-input", func(t *testing.T) {
		validate("ArtifactContentIntent", []byte(contentJSON), true)
		validate("ArtifactContentIntent", []byte(`{"name":"empty.txt","media_type":"text/plain","content":""}`), true)
		validate("ArtifactContentIntent", []byte(`{"name":"nul.txt","media_type":"text/plain","content":"\u0000"}`), true)
		for _, body := range []string{
			`{"name":"n","media_type":"text/plain"}`,
			`{"name":"n","media_type":"text/plain","content":null}`,
			`{"name":"n","media_type":"text/plain","content":"x","description":null}`,
			`{"name":"n","media_type":"text/plain","content":"x","source":"private"}`,
			`{"name":"n","media_type":"text/plain","content":"x","kind":"generated"}`,
			`{"name":"n","media_type":"text/plain","content":"x","description":"\u0000"}`,
		} {
			validate("ArtifactContentIntent", []byte(body), false)
		}
	})
}
