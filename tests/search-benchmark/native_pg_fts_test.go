package searchbenchmark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// These synthetic controls validate adapter admission/publication, not search
// quality. They do not open the benchmark corpus, query labels or qrels.
func syntheticExport(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	for _, file := range []struct {
		name, id, text string
		count          int
	}{
		{"corpus.jsonl", "source_id", "index_text", 64},
		{"queries.jsonl", "query_id", "text", 72},
	} {
		var raw bytes.Buffer
		for i := 1; i <= file.count; i++ {
			row := map[string]string{"dataset_revision": datasetRevision, file.id: fmt.Sprintf("x%02d", i), file.text: "中文 exact_Identifier /path 🙂"}
			if err := json.NewEncoder(&raw).Encode(row); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(directory, file.name), raw.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "complete.json"), []byte("{\"format_version\":1,\"kind\":\"export\",\"files\":[\"corpus.jsonl\",\"queries.jsonl\"]}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestNativePGFTSInputContract(t *testing.T) {
	directory := syntheticExport(t)
	input, err := loadNativeInput(directory)
	if err != nil || len(input.Sources) != 64 || len(input.Queries) != 72 || len(input.SHA256) != 3 || input.Sources[0].Text != "中文 exact_Identifier /path 🙂" {
		t.Fatalf("exact answer-free export not preserved: %v", err)
	}
	for _, tc := range []struct {
		name, file string
		change     func([]byte) []byte
	}{
		{"labels-not-admitted", "corpus.jsonl", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`{"dataset_revision"`), []byte(`{"grades":{},"dataset_revision"`), 1)
		}},
		{"duplicate-member", "queries.jsonl", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`{"dataset_revision"`), []byte(`{"text":"overwritten","dataset_revision"`), 1)
		}},
		{"wrong-revision", "queries.jsonl", func(b []byte) []byte { return bytes.Replace(b, []byte(datasetRevision), []byte("lexical-v1"), 1) }},
		{"duplicate-id", "corpus.jsonl", func(b []byte) []byte { return bytes.Replace(b, []byte(`"x02"`), []byte(`"x01"`), 1) }},
		{"missing-query", "queries.jsonl", func(b []byte) []byte { return b[bytes.IndexByte(b, '\n')+1:] }},
		{"query-null", "queries.jsonl", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"text":"中文 exact_Identifier /path 🙂"`), []byte(`"text":null`), 1)
		}},
		{"invalid-utf8", "queries.jsonl", func(b []byte) []byte { return bytes.Replace(b, []byte("中文"), []byte{0xff}, 1) }},
		{"unpaired-surrogate", "queries.jsonl", func(b []byte) []byte { return bytes.Replace(b, []byte("🙂"), []byte(`\ud800`), 1) }},
		{"extra-json", "complete.json", func(b []byte) []byte { return append(b, []byte("{}\n")...) }},
		{"incomplete-marker", "complete.json", func([]byte) []byte { return []byte("{}\n") }},
		{"row-size", "queries.jsonl", func(b []byte) []byte {
			return bytes.Replace(b, []byte("中文"), bytes.Repeat([]byte{'x'}, maxLineBytes), 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := syntheticExport(t)
			path := filepath.Join(directory, tc.file)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, tc.change(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadNativeInput(directory); err == nil {
				t.Fatal("invalid export admitted")
			}
		})
	}
	t.Run("paired-surrogate-preserved", func(t *testing.T) {
		fields, err := nativeFields([]byte(`{"value":"\ud83d\ude42"}`), "value")
		var text string
		if err != nil || json.Unmarshal(fields["value"], &text) != nil || text != "🙂" {
			t.Fatal("valid Unicode pair rejected or rewritten")
		}
	})
}

func TestNativePGFTSPublication(t *testing.T) {
	base := t.TempDir()
	output := filepath.Join(base, "candidate")
	if err := publishNative(output, map[string]any{"run.json": map[string]any{"actual": "controlled-output-only"}}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(output, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := publishNative(output, map[string]any{"run.json": "replacement"}); err == nil {
		t.Fatal("existing output overwritten")
	}
	after, err := os.ReadFile(filepath.Join(output, "run.json"))
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("failed publication changed existing output")
	}
	if _, err := os.Stat(filepath.Join(output, "complete.json")); err != nil {
		t.Fatal("successful controlled publication lacks completion")
	}
	failed := filepath.Join(base, "invalid")
	if err := publishNative(failed, map[string]any{"run.json": math.NaN()}); err == nil {
		t.Fatal("nonfinite output accepted")
	}
	if _, err := os.Stat(failed); !os.IsNotExist(err) {
		t.Fatal("failed publication left a candidate directory")
	}
}
