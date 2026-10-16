package config

import "testing"

func TestOutboundProxyEnvironmentRemoved(t *testing.T) {
	if _, err := loadValues(map[string]string{Prefix + "OUTBOUND_PROXY_ORIGINS": `["https://model.example"]`}); err == nil {
		t.Fatal("retired deployment proxy option accepted")
	}
	cfg, err := loadValues(nil)
	if err != nil || cfg.Validate() != nil {
		t.Fatal("proxy configuration leaked into deployment startup")
	}
}
