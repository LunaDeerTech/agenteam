package config

import (
	"errors"
	"testing"
)

func TestDevModeConfiguration(t *testing.T) {
	for _, raw := range []string{"", "TRUE", "False", "1", "0", " true", "false ", "secret-SENTINEL"} {
		_, err := loadValues(map[string]string{Prefix + "DEV_MODE": raw})
		var issue *Error
		if !errors.As(err, &issue) || issue.Field() != Prefix+"DEV_MODE" {
			t.Fatalf("invalid dev mode accepted: %q", raw)
		}
		assertConfigProjectionSafe(t, err)
	}
	for _, mode := range []string{"unset", "false", "true"} {
		t.Run(mode, func(t *testing.T) {
			values := map[string]string{}
			if mode != "unset" {
				values[Prefix+"DEV_MODE"] = mode
			}
			cfg, err := loadValues(values)
			if err != nil || cfg.Validate() != nil || cfg.DevMode() != (mode == "true") {
				t.Fatal("dev mode configuration", err)
			}
			for _, origin := range []string{"http://192.0.2.40:8080", "http://public.example.test", "http://[2001:db8::1]:8080"} {
				values[Prefix+"PUBLIC_ORIGIN"] = origin
				cfg, err := loadValues(values)
				if mode != "true" {
					if err == nil {
						t.Fatal("remote HTTP accepted without dev mode")
					}
					continue
				}
				if err != nil || cfg.Validate() != nil || cfg.PublicOrigin() != origin {
					t.Fatal("dev HTTP origin rejected", err)
				}
				cfg.devMode = false
				if cfg.Validate() == nil {
					t.Fatal("validation lost production HTTP restriction")
				}
			}
		})
	}
}

func TestDevModeOriginStillRequiresSafeURL(t *testing.T) {
	for _, raw := range []string{"", "/relative", "ftp://example.test", "http://*.example.test", "http://user:secret-SENTINEL@example.test", "http://example.test/path", "http://example.test?", "http://example.test#", "http://example.test:0", "http://example.test:65536", "http://example.test:", "http://example.test/%2f", "http://example.test\\other"} {
		_, err := loadValues(map[string]string{Prefix + "DEV_MODE": "true", Prefix + "PUBLIC_ORIGIN": raw})
		var issue *Error
		if !errors.As(err, &issue) || issue.Field() != Prefix+"PUBLIC_ORIGIN" {
			t.Fatalf("invalid dev origin accepted: %q", raw)
		}
		assertConfigProjectionSafe(t, err)
	}
	for raw, want := range map[string]string{
		"HTTP://PUBLIC.EXAMPLE.TEST:80/": "http://public.example.test",
		"http://LOCALHOST:8080/":         "http://localhost:8080",
		"http://127.0.0.1:80":            "http://127.0.0.1",
		"https://EXAMPLE.TEST:443/":      "https://example.test",
	} {
		cfg, err := loadValues(map[string]string{Prefix + "DEV_MODE": "true", Prefix + "PUBLIC_ORIGIN": raw})
		if err != nil || cfg.Validate() != nil || cfg.PublicOrigin() != want {
			t.Fatal("origin normalization changed", err)
		}
	}
}
