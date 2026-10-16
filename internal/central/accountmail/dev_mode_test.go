package accountmail

import (
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
)

type devModeNoCallPort struct{ c.DeliveryPort }

func TestDevModeMailOrigin(t *testing.T) {
	trust, err := outbound.LoadTrustStore("")
	if err != nil {
		t.Fatal(err)
	}
	for _, dev := range []bool{false, true} {
		for _, origin := range []string{"http://public.example.test:8080", "http://localhost:8080", "http://127.0.0.1:8080", "https://public.example.test"} {
			deps := Dependencies{DevMode: dev, Port: devModeNoCallPort{}, Registry: &WorkRegistry{}, Trust: trust, RecoveryLog: &recoverylog.Sink{}, PublicOrigin: origin}
			worker, err := New(deps)
			if !dev && origin == "http://public.example.test:8080" {
				if err == nil || worker != nil {
					t.Fatal("production mail worker accepted remote HTTP")
				}
			} else if err != nil || worker == nil || worker.data().PublicOrigin != origin {
				t.Fatal("configured mail origin changed", err)
			}
		}
	}
	for _, origin := range []string{"ftp://public.example.test", "http://user@public.example.test", "http://public.example.test/path", "http://public.example.test?token=secret", "http://public.example.test#token"} {
		if worker, err := New(Dependencies{DevMode: true, Port: devModeNoCallPort{}, Registry: &WorkRegistry{}, Trust: trust, RecoveryLog: &recoverylog.Sink{}, PublicOrigin: origin}); err == nil || worker != nil {
			t.Fatal("invalid development mail origin accepted")
		}
	}
}
