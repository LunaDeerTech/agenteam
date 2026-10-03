package outbound

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestClassificationVectors(t *testing.T) {
	groups := map[AddressClass][]string{
		Public:      {"1.1.1.1", "8.8.8.8", "9.255.255.255", "11.0.0.0", "100.63.255.255", "100.128.0.0", "172.15.255.255", "172.32.0.0", "192.169.0.0", "198.17.255.255", "198.20.0.0", "223.255.255.255", "2000::1", "2001:4860:4860::8888", "2606:4700:4700::1111", "3ffe:ffff::1"},
		Private:     {"10.0.0.0", "10.255.255.255", "172.16.0.0", "172.31.255.255", "192.168.0.0", "192.168.255.255", "fc00::", "fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		Metadata:    {"169.254.169.254", "169.254.170.2", "100.100.100.200", "168.63.129.16", "fd00:ec2::254"},
		Loopback:    {"127.0.0.1", "127.255.255.255", "::1"},
		LinkLocal:   {"169.254.0.0", "169.254.255.255", "fe80::1", "febf:ffff::1"},
		Unspecified: {"0.0.0.0", "::"},
		Multicast:   {"224.0.0.0", "239.255.255.255", "ff00::", "ffff:ffff::1"},
		Reserved:    {"0.0.0.1", "0.255.255.255", "100.64.0.0", "100.127.255.255", "192.0.0.0", "192.0.0.9", "192.0.0.255", "192.0.2.0", "192.0.2.255", "192.31.196.0", "192.31.196.255", "192.52.193.0", "192.52.193.255", "192.88.99.0", "192.88.99.255", "192.175.48.0", "192.175.48.255", "198.18.0.0", "198.19.255.255", "198.51.100.0", "198.51.100.255", "203.0.113.0", "203.0.113.255", "240.0.0.0", "255.255.255.255", "::abcd:1234", "64:ff9b::1", "64:ff9b:1::1", "100::1", "2001::1", "2001:1ff:ffff::1", "2001:db8::1", "2002::1", "2620:4f:8000::1", "3fff::1", "3fff:fff::1", "4000::1", "5f00::1", "fec0::1", "fe80::1%eth0"},
	}
	for want, ips := range groups {
		for _, s := range ips {
			t.Run(s, func(t *testing.T) {
				ip := netip.MustParseAddr(s)
				if got := Classify(ip); got != want {
					t.Fatalf("got %s want %s", got, want)
				}
				if ip.Is4() {
					mapped := netip.AddrFrom16(ip.As16())
					if got := Classify(mapped); got != want {
						t.Fatalf("mapped got %s want %s", got, want)
					}
				}
			})
		}
	}
	if Classify(netip.Addr{}) != Reserved {
		t.Fatal("invalid address allowed")
	}
}
func TestTargetCanonicalOrigins(t *testing.T) {
	cases := map[string]string{
		"https://EXAMPLE.Com./path?q=secret":     "https://example.com:443",
		"https://example.com:00443":              "https://example.com:443",
		"http://example.com":                     "http://example.com:80",
		"https://[2001:4860:0000:0::8888]:8443/": "https://[2001:4860::8888]:8443",
		"https://[::ffff:192.168.1.1]":           "https://[::ffff:c0a8:101]:443",
		"https://xn--bcher-kva.example":          "https://xn--bcher-kva.example:443",
	}
	for raw, want := range cases {
		target, e := ParseTarget(raw)
		if e != nil {
			t.Fatalf("valid target failed: %s", raw)
		}
		if target.Origin().String() != want {
			t.Fatalf("got %s want %s", target.Origin().String(), want)
		}
		u := target.URL()
		u.Host = "attacker"
		if target.Origin().String() != want || target.URL().Host == "attacker" {
			t.Fatal("mutable target")
		}
	}
	a, _ := ParseOrigin("https://Example.com/")
	b, _ := ParseOrigin("https://example.com:443")
	c, _ := ParseOrigin("http://example.com:443")
	if !a.Equal(b) || a.Equal(c) {
		t.Fatal("origin comparison")
	}
	bad := []string{"", "//example.com", "ftp://example.com", "https:example.com", "https://user:pass@example.com", "https://example.com/#", "https://example.com/#fragment", "https://example.com:", "https://example.com:0", "https://example.com:65536", "https://example.com:+443", "https://example.com:abc", "https://[fe80::1%25eth0]", "https://[127.0.0.1]", "https://2001:4860::1", "https://exa_mple.com", "https://-example.com", "https://example..com", "https://bücher.example", "https://127.1", "https://2130706433", "https://0177.0.0.1", "https://0x7f.0.0.1", "https://0x7f000001", "https://127.0.0.1.", "https://example.123", "https://example.com..", "https://example.com\\@attacker", "https://example.com\n"}
	for _, raw := range bad {
		if _, e := ParseTarget(raw); e == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"https://example.com/a", "https://example.com/?secret=x", "https://example.com/?"} {
		if _, e := ParseOrigin(raw); e == nil {
			t.Errorf("origin accepted %s", raw)
		}
	}
}
func TestRulesCanonicalAndStrictDecode(t *testing.T) {
	raw := []byte(`[{"ports":[8443,443],"cidr":"fd00::/8"},{"allow_http":true,"ports":"all","cidr":"10.2.0.0/16"}]`)
	r, e := DecodeRules(raw)
	if e != nil {
		t.Fatal(e)
	}
	want := `[{"cidr":"10.2.0.0/16","ports":"all","allow_http":true},{"cidr":"fd00::/8","ports":[443,8443],"allow_http":false}]`
	if string(r.JSON()) != want {
		t.Fatalf("canonical=%s", r.JSON())
	}
	again, e := DecodeRules(r.JSON())
	if e != nil || !bytes.Equal(r.JSON(), again.JSON()) {
		t.Fatal("not canonical roundtrip")
	}
	for _, bad := range []string{`null`, `{}`, `[] true`, `[{"cidr":"10.0.0.0/8"}]`, `[{"cidr":"10.0.0.0/8","ports":null}]`, `[{"cidr":"10.0.0.0/8","ports":[]}]`, `[{"cidr":"10.0.0.0/8","ports":[0]}]`, `[{"cidr":"10.0.0.0/8","ports":[65536]}]`, `[{"cidr":"10.0.0.0/8","ports":[443,443]}]`, `[{"cidr":"10.0.0.0/8","ports":[1e2]}]`, `[{"cidr":"10.0.0.0/8","ports":"all","ports":"all"}]`, `[{"cidr":"10.0.0.0/8","ports":"all","extra":true}]`, `[{"cidr":"10.0.0.0/8","ports":"all","allow_http":null}]`, `[{"cidr":"10.0.0.0/8","ports":"all","allow_http":"false"}]`} {
		if _, e := DecodeRules([]byte(bad)); e == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	for _, cidr := range []string{"10.0.0.1/8", "10.0.0.0/7", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "0.0.0.0/0", "8.8.8.8/32", "::/0", "2001:4860::/32", "fe80::/10", "::ffff:10.0.0.0/104", "FD00::/8"} {
		if _, e := NewRule(cidr, AllPorts(), true); e == nil {
			t.Errorf("rule accepted %s", cidr)
		}
	}
	if _, e := NewRule("10.0.0.0/8", Ports{}, false); e == nil {
		t.Fatal("implicit all ports")
	}
	var many []Rule
	for range 257 {
		rule, _ := NewRule("10.0.0.0/8", AllPorts(), false)
		many = append(many, rule)
	}
	if _, e := NewRules(many...); e == nil {
		t.Fatal("too many rules")
	}
	ports := make([]uint16, 257)
	for i := range ports {
		ports[i] = uint16(i + 1)
	}
	if _, e := SelectedPorts(ports...); e == nil {
		t.Fatal("too many ports")
	}
}

func TestMappedOriginIdentityAndOversizedNumericHost(t *testing.T) {
	a, _ := ParseOrigin("https://[::ffff:192.168.1.1]")
	b, _ := ParseOrigin("https://[0:0:0:0:0:ffff:c0a8:101]:443/")
	v4, _ := ParseOrigin("https://192.168.1.1")
	if !a.Equal(b) || a.Equal(v4) {
		t.Fatal("mapped IPv6 origin identity lost")
	}
	target, _ := ParseTarget("https://[::ffff:192.168.1.1]/request")
	if target.URL().Host != "[::ffff:c0a8:101]:443" || Classify(netip.MustParseAddr(target.Origin().Host())) != Private {
		t.Fatal("origin serialization/classification mismatch")
	}
	for _, raw := range []string{"https://0x", "https://0X", "https://example.0x", "https://0x10000000000000000", "https://example.0x10000000000000000", "https://0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"} {
		if _, e := ParseTarget(raw); e == nil {
			t.Errorf("numeric host accepted: %s", raw)
		}
	}
	for _, raw := range []string{"https://0xg", "https://example.0xg", "https://0x-not-a-number.example"} {
		if _, e := ParseTarget(raw); e != nil {
			t.Errorf("unambiguous DNS name refused: %s", raw)
		}
	}
}
func TestRulesRequireOneCompleteMatchAndFixedDenials(t *testing.T) {
	ports, _ := SelectedPorts(443)
	a, _ := NewRule("10.0.0.0/8", ports, false)
	other, _ := SelectedPorts(8080)
	b, _ := NewRule("10.0.0.0/8", other, true)
	ula, _ := NewRule("fc00::/7", AllPorts(), true)
	r, _ := NewRules(a, b, ula)
	tests := []struct {
		ip   string
		port uint16
		http bool
		want ac.Reason
	}{{"10.2.3.4", 443, false, ""}, {"10.2.3.4", 443, true, ac.HTTPDenied}, {"10.2.3.4", 8080, true, ""}, {"10.2.3.4", 80, true, ac.PortDenied}, {"192.168.1.1", 443, false, ac.PrivateNotAllowed}, {"fd00:ec2::254", 443, false, ac.AddressForbidden}, {"fd00:123::1", 443, false, ""}, {"127.0.0.1", 443, false, ac.AddressForbidden}, {"8.8.8.8", 80, true, ""}}
	for _, tt := range tests {
		if got := r.check(netip.MustParseAddr(tt.ip), tt.port, tt.http); got != tt.want {
			t.Errorf("%s:%d got %s want %s", tt.ip, tt.port, got, tt.want)
		}
	}
}
func TestBoundarySafeImplicitProjections(t *testing.T) {
	target, _ := ParseTarget("https://example.com/target-canary?token=target-canary")
	rule, _ := NewRule("10.123.0.0/16", AllPorts(), true)
	rules, _ := NewRules(rule)
	rawCause := errors.New("raw-cause-canary")
	err := failure(foundation.CommitUnknown, ac.PolicyUnavailable, rawCause)
	var f *foundation.Fault
	if !errors.As(err, &f) || f.CommitState != foundation.Unknown || !errors.Is(err, rawCause) {
		t.Fatal("lost safe fault state/cause")
	}
	values := []any{target, rule, rules, err, struct {
		target Target
		rules  Rules
		err    error
	}{target, rules, err}}
	for _, v := range values {
		var buf bytes.Buffer
		slog.New(slog.NewTextHandler(&buf, nil)).Info("probe", "v", v)
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		all := buf.String() + string(b)
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			all += fmt.Sprintf(verb, v)
		}
		for _, secret := range []string{"target-canary", "raw-cause-canary", "10.123.0.0"} {
			if strings.Contains(all, secret) {
				t.Fatalf("implicit leak: %s", secret)
			}
		}
	}
}
