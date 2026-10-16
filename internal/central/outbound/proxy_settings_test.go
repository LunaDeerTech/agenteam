package outbound

import (
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestProxySettingsGatewayAndPasswordValidation(t *testing.T) {
	for _, host := range []string{"proxy", "PROXY.Example.", "10.2.3.4", "fd00::123", "localhost", "child.localhost", "127.0.0.1", "::1", "169.254.169.254", "fe80::1", "metadata.google.internal"} {
		input := ProxyUpdate{Mode: "direct", Gateway: &ProxyGateway{Protocol: "https", Host: host, Port: 8443}, CredentialAction: "keep"}
		if _, e := normalizeProxyUpdate(input); e != nil {
			t.Fatal("valid explicit gateway rejected")
		}
	}
	for _, host := range []string{"https://proxy", "user@proxy", "proxy/path", "0177.0.0.1", "xn--.example", "proxy%zone", "代理"} {
		if _, e := normalizeProxyUpdate(ProxyUpdate{Mode: "proxy", Gateway: &ProxyGateway{Protocol: "http", Host: host, Port: 8080}, CredentialAction: "keep"}); e == nil {
			t.Fatal("ambiguous gateway accepted")
		}
	}
	secret, e := sc.NewSecretMaterial([]byte("proxy-secret"))
	if e != nil {
		t.Fatal(e)
	}
	defer secret.Destroy()
	for _, input := range []ProxyUpdate{
		{Mode: "proxy", CredentialAction: "remove"},
		{Mode: "direct", CredentialAction: "keep"},
		{Mode: "direct", CredentialAction: "remove", Password: &secret},
		{Mode: "proxy", Gateway: &ProxyGateway{Protocol: "http", Host: "proxy", Port: 8080, Username: "with:colon"}, CredentialAction: "keep", Password: &secret},
		{Mode: "proxy", Gateway: &ProxyGateway{Protocol: "http", Host: "proxy", Port: 8080, Username: "keep-user"}, CredentialAction: "remove"},
	} {
		if _, e := normalizeProxyUpdate(input); e == nil {
			t.Fatal("invalid credential intent accepted")
		}
	}
}
func TestProxySettingsUnavailableNeedsBothCurrentSnapshots(t *testing.T) {
	rules, e := NewRules()
	if e != nil {
		t.Fatal(e)
	}
	state := &policyState{mirror: policy(1, Denylist, rules), available: true, proxy: proxySnapshot{version: 1, mode: "direct"}, proxyAvailable: true, proxyDependencies: &ProxyDependencies{}}
	s := &PolicyService{data: func() *policyState { return state }}
	s.unavailable()
	if e = s.publish(policy(2, Denylist, rules)); e != nil {
		t.Fatal(e)
	}
	if s.Status().Available {
		t.Fatal("rules publish reopened unavailable proxy")
	}
	if _, e = s.snapshot(); e == nil {
		t.Fatal("rule snapshot admitted while proxy unavailable")
	}
	if e = s.publishProxy(proxyRecord{settings: ProxySettings{Version: foundation.Version(2), Mode: "direct"}}); e != nil {
		t.Fatal(e)
	}
	if !s.Status().Available {
		t.Fatal("both explicit current snapshots did not restore availability")
	}
	s.unavailable()
	if e = s.publishProxy(proxyRecord{settings: ProxySettings{Version: foundation.Version(3), Mode: "direct"}}); e != nil {
		t.Fatal(e)
	}
	if s.Status().Available {
		t.Fatal("proxy publish reopened unavailable rules")
	}
}
