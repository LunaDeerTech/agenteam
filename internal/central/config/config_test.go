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
func TestDefaultsAndImmutableConfig(t *testing.T) {
	values := map[string]string{"AGENTEAM_RUNNER_FUTURE_SECRET": "ignored-SENTINEL", "UNRELATED_SECRET": "ignored-SENTINEL"}
	c, err := loadValues(values)
	if err != nil || c.Validate() != nil || c.HTTPAddr() != "127.0.0.1:8080" || c.PublicOrigin() != "http://localhost:8080" || c.LogLevel() != slog.LevelInfo || c.ShutdownTimeout() != 10*time.Second {
		t.Fatalf("defaults: %v", err)
	}
	values[Prefix+"HTTP_ADDR"] = "secret-SENTINEL"
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if c.HTTPAddr() != "127.0.0.1:8080" || c.Validate() != nil {
				t.Error("configuration changed after load")
			}
		})
	}
	wg.Wait()
	if (Config{}).Validate() == nil {
		t.Fatal("zero config accepted")
	}
}

func TestConfigurationBoundariesAndSafeErrors(t *testing.T) {
	for key, values := range map[string][]string{
		"LOG_LEVEL":        {"", "INFO", "verbose", "credential-SENTINEL"},
		"SHUTDOWN_TIMEOUT": {"", "0", "0s", "99ms", "5m1ns", "-1s", "credential-SENTINEL"},
		"HTTP_ADDR":        {"", "localhost", "127.0.0.1:-1", "127.0.0.1:65536", "127.0.0.1:abc", "localhost:0", ":0", "0.0.0.0:0", "[::]:0", "127.0.0.1 :8080", "[::1%zone]:0", "credential-SENTINEL"},
		"PUBLIC_ORIGIN":    {"", "/relative", "ftp://localhost", "http://user:credential-SENTINEL@localhost", "http://localhost/path", "http://localhost?secret=credential-SENTINEL", "http://localhost?", "http://localhost#", "http://localhost:0", "http://localhost:", "http://localhost:65536", "http://localhost/%2f", "http://localhost http://other"},
	} {
		for _, value := range values {
			t.Run(key+"/"+value, func(t *testing.T) {
				_, err := loadValues(map[string]string{Prefix + key: value})
				if err == nil {
					t.Fatal("invalid configuration accepted")
				}
				if strings.Contains(fmt.Sprintf("%+v", err), "SENTINEL") {
					t.Fatal("configuration value leaked")
				}
				issue, ok := err.(*Error)
				if !ok || issue.Field() != Prefix+key || issue.Reason() != "invalid" {
					t.Fatal("unsafe error classification")
				}
			})
		}
	}
	for _, key := range []string{Prefix + "DATABASE_URL", Prefix + "credential-SENTINEL"} {
		_, err := loadValues(map[string]string{key: "credential-SENTINEL"})
		if err == nil || strings.Contains(err.Error(), "SENTINEL") {
			t.Fatal("unknown setting accepted/leaked")
		}
	}
	if _, err := Load(nil, nil); err == nil {
		t.Fatal("nil lookup accepted")
	}
	for key, values := range map[string][]string{
		"LOG_LEVEL":        {"debug", "info", "warn", "error"},
		"SHUTDOWN_TIMEOUT": {"100ms", "5m"},
		"HTTP_ADDR":        {"127.0.0.1:0", "[::1]:0", "127.0.0.2:0", ":8080", "0.0.0.0:8080", "[::]:8080", "localhost:65535"},
	} {
		for _, value := range values {
			if _, err := loadValues(map[string]string{Prefix + key: value}); err != nil {
				t.Errorf("valid %s=%s: %v", key, value, err)
			}
		}
	}
	for raw, want := range map[string]string{"HTTP://EXAMPLE.COM:80/": "http://example.com", "https://Example.com:443": "https://example.com", "http://[::1]:80/": "http://[::1]", "https://localhost:08443/": "https://localhost:8443"} {
		c, err := loadValues(map[string]string{Prefix + "PUBLIC_ORIGIN": raw})
		if err != nil || c.PublicOrigin() != want {
			t.Errorf("normalization %s: %s %v", raw, c.PublicOrigin(), err)
		}
	}
}

func TestLoadNeverReadsIgnoredValues(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if !strings.HasPrefix(key, Prefix) {
			t.Fatalf("read unrelated field %s", key)
		}
		return "", false
	}, []string{"AGENTEAM_RUNNER_SECRET=credential-SENTINEL", "CREDENTIAL=credential-SENTINEL"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOriginCanonicalIPv6MatchesBrowserSerialization(t *testing.T) {
	for raw, want := range map[string]string{
		"HTTP://[0:0:0:0:0:0:0:1]:80/":                           "http://[::1]",
		"https://[2001:0DB8:0000:0000:0000:0000:0000:0001]:443/": "https://[2001:db8::1]",
		"http://[::ffff:192.0.2.1]":                              "http://[::ffff:c000:201]",
		"http://[0:0:0:0:0:ffff:c000:0201]":                      "http://[::ffff:c000:201]",
		"http://[2001:0:0:1:0:0:1:1]":                            "http://[2001::1:0:0:1:1]",
		"http://[1:2:3:4:5:6:0:0]":                               "http://[1:2:3:4:5:6::]",
		"http://[0:0:0:0:0:0:0:0]":                               "http://[::]",
	} {
		cfg, err := loadValues(map[string]string{Prefix + "PUBLIC_ORIGIN": raw})
		if err != nil || cfg.PublicOrigin() != want || cfg.Validate() != nil {
			t.Errorf("IPv6 origin %s: got %s want %s error %v", raw, cfg.PublicOrigin(), want, err)
		}
	}
}
