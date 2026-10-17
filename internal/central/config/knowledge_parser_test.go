package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func knowledgeParserValues() map[string]string {
	values := runtimeConfigValues(strings.Replace(runtimeConfigJSON, "direct-text-v1", "tool-loop-v1", 1))
	values[Prefix+retrievalRuntimeSetting] = `{"backend":"native_pg_fts"}`
	values[Prefix+knowledgePDFToTextPath] = "/absent/pdftotext"
	values[Prefix+knowledgePDFToTextSHA256] = strings.Repeat("a", 64)
	values[Prefix+knowledgePrlimitPath] = "/absent/prlimit"
	values[Prefix+knowledgePrlimitSHA256] = strings.Repeat("b", 64)
	return values
}
func TestKnowledgeParserConfigurationDoesNotProbe(t *testing.T) {
	values := knowledgeParserValues()
	// A path that cannot be opened as a regular executable is still valid
	// configuration: probing and execution belong only to actual startup.
	path := filepath.Join(t.TempDir(), "directory-not-executable")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	values[Prefix+knowledgePDFToTextPath] = path
	cfg, err := loadValues(values)
	if err != nil || cfg.Validate() != nil {
		t.Fatalf("shape-only configuration: %v", err)
	}
	options, enabled := cfg.KnowledgeParser()
	if !enabled || options.Poppler().PDFToTextPath != path {
		t.Fatal("explicit parser binding missing")
	}
	for _, key := range []string{knowledgePDFToTextPath, knowledgePDFToTextSHA256, knowledgePrlimitPath, knowledgePrlimitSHA256} {
		delete(values, Prefix+key)
	}
	cfg, err = loadValues(values)
	if err != nil {
		t.Fatal(err)
	}
	if _, enabled = cfg.KnowledgeParser(); enabled {
		t.Fatal("implicit PDF parser enabled")
	}
}
func TestKnowledgeParserConfigurationRejectsPartialOrAmbiguousBindings(t *testing.T) {
	for _, key := range []string{knowledgePDFToTextPath, knowledgePDFToTextSHA256, knowledgePrlimitPath, knowledgePrlimitSHA256} {
		for _, missing := range []bool{true, false} {
			values := knowledgeParserValues()
			if missing {
				delete(values, Prefix+key)
			} else {
				values[Prefix+key] = ""
			}
			if _, err := loadValues(values); err == nil {
				t.Fatal("partial parser accepted")
			}
		}
	}
	for _, value := range []string{"relative/bin", "/absent/../bin", "/bad\x00name"} {
		values := knowledgeParserValues()
		values[Prefix+knowledgePDFToTextPath] = value
		if _, err := loadValues(values); err == nil {
			t.Fatal("invalid absolute argv accepted")
		}
	}
	for _, value := range []string{strings.Repeat("A", 64), strings.Repeat("z", 64), strings.Repeat("a", 63)} {
		values := knowledgeParserValues()
		values[Prefix+knowledgePDFToTextSHA256] = value
		if _, err := loadValues(values); err == nil {
			t.Fatal("invalid executable digest accepted")
		}
	}
	values := knowledgeParserValues()
	delete(values, Prefix+retrievalRuntimeSetting)
	if _, err := loadValues(values); err == nil {
		t.Fatal("PDF parser without retrieval accepted")
	}
}
