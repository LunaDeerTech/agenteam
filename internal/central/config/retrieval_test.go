package config

import (
	"strings"
	"testing"
)

func TestRetrievalRuntimeExplicitProfile(t *testing.T) {
	values := runtimeConfigValues(strings.Replace(runtimeConfigJSON, "direct-text-v1", "tool-loop-v1", 1))
	for _, raw := range []string{`{"backend":"native_pg_fts","dimensions":4096}`, `{"backend":"pgroonga","dimensions":1536}`, `{"backend":"pg_search","dimensions":4096}`} {
		values[Prefix+retrievalRuntimeSetting] = raw
		cfg, err := loadValues(values)
		if err != nil || cfg.Validate() != nil {
			t.Fatalf("explicit profile rejected: %v", err)
		}
		options, present := cfg.RetrievalRuntime()
		if !present || options.Validate() != nil || options.Dimensions() == 0 {
			t.Fatal("profile missing")
		}
	}
	delete(values, Prefix+retrievalRuntimeSetting)
	cfg, err := loadValues(values)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := cfg.RetrievalRuntime(); present {
		t.Fatal("absence manufactured retrieval")
	}
}
func TestRetrievalRuntimeRejectsImplicitAndAmbiguousProfiles(t *testing.T) {
	for _, raw := range []string{"", `null`, `{}`, `{"backend":"unknown","dimensions":4096}`, `{"backend":"native_pg_fts","dimensions":0}`, `{"backend":"native_pg_fts","dimensions":4097}`, `{"backend":"native_pg_fts","dimensions":1.5}`, `{"backend":"native_pg_fts","dimensions":4096,"dimensions":1024}`, `{"Backend":"native_pg_fts","dimensions":4096}`, `{"backend":"native_pg_fts","dimensions":4096,"password":"sentinel"}`} {
		values := runtimeConfigValues(strings.Replace(runtimeConfigJSON, "direct-text-v1", "tool-loop-v1", 1))
		values[Prefix+retrievalRuntimeSetting] = raw
		if _, err := loadValues(values); err == nil || strings.Contains(err.Error(), "sentinel") {
			t.Fatal("invalid or sensitive profile accepted")
		}
	}
	values := runtimeConfigValues(runtimeConfigJSON)
	values[Prefix+retrievalRuntimeSetting] = `{"backend":"native_pg_fts","dimensions":4096}`
	if _, err := loadValues(values); err == nil {
		t.Fatal("retrieval enabled without tool runtime")
	}
}
