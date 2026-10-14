package config

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

func TestOutboundProxyOrigins(t *testing.T) {
	t.Run("default_direct", func(t *testing.T) {
		cfg, err := loadValues(nil)
		if err != nil || len(cfg.OutboundProxyOrigins()) != 0 {
			t.Fatal("unset proxy origins changed default routing")
		}
	})
	t.Run("exact_immutable", func(t *testing.T) {
		cfg, err := loadValues(map[string]string{Prefix + outboundProxyOriginsSetting: `["https://model.example","https://second.example:8443"]`})
		if err != nil || cfg.Validate() != nil {
			t.Fatal("valid exact origins rejected")
		}
		origins := cfg.OutboundProxyOrigins()
		if len(origins) != 2 || origins[0].Port() != 443 || origins[1].Port() != 8443 {
			t.Fatal("canonical origins changed")
		}
		origins[0] = outbound.Origin{}
		if !cfg.OutboundProxyOrigins()[0].Valid() || cfg.Validate() != nil {
			t.Fatal("caller mutated configuration")
		}
		assertConfigProjectionSafe(t, cfg)
	})
	for name, raw := range map[string]string{
		"empty": "", "null": "null", "empty_array": "[]", "object": `{}`, "non_string": `[1]`,
		"http": `["http://model.example"]`, "path": `["https://model.example/v1"]`,
		"query": `["https://model.example/?secret=SENTINEL"]`, "userinfo": `["https://secret-SENTINEL@model.example"]`,
		"fragment": `["https://model.example/#SENTINEL"]`, "numeric": `["https://8.8.8.8"]`,
		"ipv6": `["https://[2606:4700:4700::1111]"]`, "duplicate": `["https://model.example","https://model.example:443/"]`,
		"trailing": `["https://model.example"] false`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadValues(map[string]string{Prefix + outboundProxyOriginsSetting: raw})
			var issue *Error
			if !errors.As(err, &issue) || issue.Field() != Prefix+outboundProxyOriginsSetting {
				t.Fatal("invalid proxy origin was not rejected at its configuration field")
			}
			assertConfigProjectionSafe(t, err)
		})
	}
	t.Run("bounded", func(t *testing.T) {
		values := make([]string, 33)
		for n := range values {
			values[n] = "https://model.example"
		}
		raw, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadValues(map[string]string{Prefix + outboundProxyOriginsSetting: string(raw)}); err == nil {
			t.Fatal("unbounded list accepted")
		}
	})
}
