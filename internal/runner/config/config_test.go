package config

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func loadValues(values map[string]string) (Config, error) {
	var env []string
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	return Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, env)
}
func TestRunnerConfiguration(t *testing.T) {
	c, err := loadValues(map[string]string{"AGENTEAM_CENTRAL_SECRET": "credential-SENTINEL", "UNRELATED": "credential-SENTINEL"})
	if err != nil || c.Validate() != nil || c.LogLevel() != slog.LevelInfo || c.ShutdownTimeout() != 10*time.Second {
		t.Fatalf("defaults: %v", err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if c.Validate() != nil {
				t.Error("concurrent config changed")
			}
		})
	}
	wg.Wait()
	for field, values := range map[string][]string{"LOG_LEVEL": {"", "INFO", "trace", "credential-SENTINEL"}, "SHUTDOWN_TIMEOUT": {"", "0", "99ms", "5m1ns", "-1s", "credential-SENTINEL"}} {
		for _, value := range values {
			_, err := loadValues(map[string]string{Prefix + field: value})
			if err == nil || strings.Contains(fmt.Sprintf("%+v", err), "SENTINEL") {
				t.Errorf("bad value accepted/leaked for %s", field)
			}
		}
	}
	for _, key := range []string{Prefix + "CENTRAL_URL", Prefix + "credential-SENTINEL"} {
		_, err := loadValues(map[string]string{key: "credential-SENTINEL"})
		if err == nil || strings.Contains(err.Error(), "SENTINEL") {
			t.Fatal("unknown field accepted/leaked")
		}
	}
	for field, values := range map[string][]string{"LOG_LEVEL": {"debug", "info", "warn", "error"}, "SHUTDOWN_TIMEOUT": {"100ms", "5m"}} {
		for _, value := range values {
			if _, err := loadValues(map[string]string{Prefix + field: value}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := Load(nil, nil); err == nil {
		t.Fatal("nil lookup accepted")
	}
	if (Config{}).Validate() == nil {
		t.Fatal("zero config accepted")
	}
}
