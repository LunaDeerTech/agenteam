package previewruntime

import (
	"testing"
	"time"
)

func TestPublicPreviewLifetimeRequiresExplicitOptIn(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, port, lifetime string
		public                        bool
	}{
		{name: "default"},
		{name: "local", enabled: "1", port: "5250", lifetime: "45m"},
		{name: "duration_without_preview", port: "5254", lifetime: "24h"},
		{name: "duration_on_local_port", enabled: "1", port: "5250", lifetime: "24h"},
		{name: "unbounded_duration", enabled: "1", port: "5254", lifetime: "48h"},
		{name: "public_default", enabled: "1", port: "5254", lifetime: "45m"},
		{name: "public_explicit", enabled: "1", port: "5254", lifetime: "24h", public: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENTEAM_ACCOUNT_PREVIEW", tc.enabled)
			t.Setenv("AGENTEAM_ACCOUNT_PREVIEW_PORT", tc.port)
			t.Setenv("AGENTEAM_ACCOUNT_PREVIEW_LIFETIME", tc.lifetime)
			wantLifetime := 45 * time.Minute
			if tc.public {
				wantLifetime = 24 * time.Hour
			}
			if got := Lifetime(); got != wantLifetime {
				t.Fatalf("lifetime=%s want=%s", got, wantLifetime)
			}
			for _, original := range []time.Duration{time.Hour, 2 * time.Hour} {
				wantValidity := original
				if tc.public {
					wantValidity = 25 * time.Hour
				}
				got := CertificateValidity(original)
				if got != wantValidity || got <= Lifetime() {
					t.Fatalf("certificate validity=%s want=%s beyond lifetime=%s", got, wantValidity, Lifetime())
				}
			}
		})
	}
}
