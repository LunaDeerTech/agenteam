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
	if len(paths) != 2 {
		t.Fatal("unexpected public route")
	}
	for _, value := range paths {
		p := value.(map[string]any)
		if len(p) != 3 || p["get"] == nil || p["head"] == nil {
			t.Fatal("write method exposed")
		}
		head := p["head"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)
		if head["content"] != nil {
			t.Fatal("HEAD schema exposes body")
		}
	}
	compiled := map[string]*js.Schema{}
	for _, name := range []string{"ArtifactPage", "ArtifactRead"} {
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
}
