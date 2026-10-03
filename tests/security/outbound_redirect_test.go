//go:build integration

package security_test

import (
	"net/http"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

func TestOutboundNetworkRedirectEachHopAndMethodBoundary(t *testing.T) {
	f := newOutboundNetwork(t)
	f.allow(t, true, 8443, 8080)
	for _, mode := range []string{"downgrade", "post", "private_port", "loopback", "six_hops"} {
		t.Run(mode, func(t *testing.T) {
			dest := f.scenario(t, netfixture.ScenarioConfig{Body: "destination"})
			location := f.url(dest)
			method := "GET"
			var body *strings.Reader
			reason := ac.RedirectDenied
			switch mode {
			case "downgrade":
				location = "http://fixture.test:8080/case/" + dest
			case "post":
				method = "POST"
				body = strings.NewReader("body")
			case "private_port":
				location = "https://fixture.test:8444/case/" + dest
				reason = ac.PortDenied
			case "loopback":
				location = "https://127.0.0.1:8443/case/" + dest
				reason = ac.AddressForbidden
			case "six_hops":
				for range 5 {
					id := f.scenario(t, netfixture.ScenarioConfig{Mode: "redirect", Redirect: location})
					location = f.url(id)
				}
			}
			start := f.scenario(t, netfixture.ScenarioConfig{Mode: "redirect", Redirect: location})
			req, _ := http.NewRequest(method, f.url(start), nil)
			if body != nil {
				req, _ = http.NewRequest(method, f.url(start), body)
			}
			_, e := f.client.Do(auditContext(t), req, f.profile(t, outbound.ProfileOptions{AllowHTTP: true}))
			// Rejected fresh-hop DNS/port attempts have no sent bytes even though
			// a preceding, separately counted redirect response was received.
			sent := mode != "private_port" && mode != "loopback"
			outboundReason(t, e, reason, sent)
			if len(f.state(t, dest).Requests) != 0 {
				t.Fatal("redirect reached forbidden target")
			}
		})
	}
}
