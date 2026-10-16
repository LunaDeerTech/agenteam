package outbound

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func modeRules(t *testing.T, targets ...string) Rules {
	t.Helper()
	rules := make([]Rule, len(targets))
	for i, target := range targets {
		var err error
		rules[i], err = NewRule(target, AllPorts())
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := NewRules(rules...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRuleModesTargetNormalization(t *testing.T) {
	for input, want := range map[string]string{
		"8.8.8.8": "8.8.8.8/32", "FD00::1": "fd00::1/128",
		"0.0.0.0/0": "0.0.0.0/0", "::/0": "::/0",
		"EXAMPLE.Com.": "example.com", "*.EXAMPLE.Com.": "*.example.com",
		"localhost": "localhost", "xn--bcher-kva.example": "xn--bcher-kva.example",
	} {
		rule, err := NewRule(input, AllPorts())
		if err != nil || rule.data().target != want {
			t.Errorf("input %q normalization: %v", input, err)
		}
	}
	for _, input := range []string{"", " ", "example.com ", "https://example.com", "user@example.com", "example.com:443", "example.com/path", "example.com?query", "example.com#fragment", "example.com..", "*.127.0.0.1", "*example.com", "a.*.example.com", "*.*.example.com", "bücher.example", "127.1", "2130706433", "0177.0.0.1", "::ffff:127.0.0.1", "fe80::1%eth0", "10.1.2.3/8", "FD00::/8", "::ffff:10.0.0.0/104"} {
		if _, err := NewRule(input, AllPorts()); err == nil {
			t.Errorf("invalid target accepted: %q", input)
		}
	}
}

func TestRuleModesCompleteSetAndDomainBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  RuleMode
		rules []string
		host  string
		ips   []string
		want  ac.Reason
	}{
		{"empty deny public", Denylist, nil, "public.example", []string{"8.8.8.8"}, ""},
		{"empty deny private", Denylist, nil, "private.example", []string{"10.2.3.4", "fd00::1"}, ""},
		{"empty deny risk is editable", Denylist, nil, "metadata.google.internal", []string{"127.0.0.1", "169.254.169.254", "::1"}, ""},
		{"empty allow rejects", Allowlist, nil, "public.example", []string{"8.8.8.8"}, ac.RuleNotAllowed},
		{"domain allows complete set", Allowlist, []string{"example.com"}, "EXAMPLE.COM.", []string{"8.8.8.8", "10.2.3.4", "::1"}, ""},
		{"wildcard one level", Allowlist, []string{"*.example.com"}, "one.example.com", []string{"127.0.0.1"}, ""},
		{"wildcard multiple levels", Allowlist, []string{"*.example.com"}, "one.two.example.com", []string{"8.8.8.8"}, ""},
		{"wildcard excludes apex", Allowlist, []string{"*.example.com"}, "example.com", []string{"8.8.8.8"}, ac.RuleNotAllowed},
		{"wildcard label boundary", Allowlist, []string{"*.example.com"}, "notexample.com", []string{"8.8.8.8"}, ac.RuleNotAllowed},
		{"exact domain excludes subdomain", Allowlist, []string{"example.com"}, "one.example.com", []string{"8.8.8.8"}, ac.RuleNotAllowed},
		{"domain deny overrides IP", Denylist, []string{"example.com"}, "example.com", []string{"8.8.8.8"}, ac.RuleNotAllowed},
		{"no CNAME identity substitution", Allowlist, []string{"canonical.example"}, "original.example", []string{"8.8.8.8"}, ac.RuleNotAllowed},
		{"all IPs allowed by separate rules", Allowlist, []string{"8.8.8.8", "10.0.0.0/8"}, "unmatched.example", []string{"8.8.8.8", "10.2.3.4"}, ""},
		{"one IP insufficient allow", Allowlist, []string{"8.8.8.8"}, "unmatched.example", []string{"8.8.8.8", "10.2.3.4"}, ac.RuleNotAllowed},
		{"one IP sufficient deny", Denylist, []string{"10.0.0.0/8"}, "unmatched.example", []string{"8.8.8.8", "10.2.3.4"}, ac.RuleNotAllowed},
		{"literal IP uses prefix", Allowlist, []string{"8.8.8.0/24"}, "8.8.8.8", []string{"8.8.8.8"}, ""},
		{"invalid mode remains closed", RuleMode(""), nil, "example.com", []string{"8.8.8.8"}, ac.PolicyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ips := make([]netip.Addr, len(tc.ips))
			for i, v := range tc.ips {
				ips[i] = netip.MustParseAddr(v)
			}
			if got := modeRules(t, tc.rules...).check(tc.mode, tc.host, ips, 443); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	rules := modeRules(t, "example.com")
	for _, addresses := range [][]netip.Addr{nil, {netip.Addr{}}, {netip.MustParseAddr("fe80::1%eth0")}} {
		if got := rules.check(Allowlist, "example.com", addresses, 443); got != ac.InvalidTarget {
			t.Fatalf("invalid IP set allowed: %s", got)
		}
	}
	ports, _ := SelectedPorts(443)
	rule, _ := NewRule("example.com", ports)
	portRules, _ := NewRules(rule)
	if portRules.check(Allowlist, "example.com", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, 80) != ac.RuleNotAllowed {
		t.Fatal("domain matched wrong port")
	}
	if portRules.check(Denylist, "example.com", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, 80) != "" {
		t.Fatal("deny matched wrong port")
	}
}

func TestRuleModesDefaultRowsCanBeRemoved(t *testing.T) {
	defaults := DefaultRules()
	if defaults.Count() != 32 {
		t.Fatal("unexpected default rule count")
	}
	for _, tc := range []struct{ host, ip string }{
		{"localhost", "127.0.0.1"}, {"local.example", "::1"},
		{"metadata.google.internal", "8.8.8.8"}, {"metadata.goog", "8.8.8.8"},
		{"metadata.example", "169.254.169.254"}, {"metadata.example", "fd00:ec2::254"},
	} {
		ips := []netip.Addr{netip.MustParseAddr(tc.ip)}
		if defaults.check(Denylist, tc.host, ips, 443) != ac.RuleNotAllowed {
			t.Fatal("default risk rule not enforced")
		}
		if modeRules(t).check(Denylist, tc.host, ips, 443) != "" {
			t.Fatal("deleted rules still implicitly denied")
		}
	}
	for _, ip := range []string{"8.8.8.8", "10.2.3.4", "172.16.2.3", "192.168.2.3", "fd00::1"} {
		if defaults.check(Denylist, ip, []netip.Addr{netip.MustParseAddr(ip)}, 443) != "" {
			t.Fatal("default blocked ordinary target", ip)
		}
	}
}

func TestRuleModesHTTPCurrentModeAndLoopbackAfterRemoval(t *testing.T) {
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); _, _ = io.WriteString(w, "ok") }))
	defer target.Close()
	port := target.Listener.Addr().(*net.TCPAddr).Port
	trust, err := LoadTrustStore("")
	if err != nil {
		t.Fatal(err)
	}
	client, state := proxyTestClient(t, trust, &proxyTestResolver{ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}, nil)
	profile := proxyTestProfile(t, false, Limits{})
	url := "http://api.model.example:" + strconv.Itoa(port) + "/"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	send := func(c context.Context) error {
		request, _ := http.NewRequestWithContext(c, "GET", url, nil)
		response, err := client.Do(c, request, profile)
		if err != nil {
			return err
		}
		_, err = io.ReadAll(response.Body())
		response.Close()
		return err
	}
	publish := func(version foundation.Version, mode RuleMode, rules Rules) {
		t.Helper()
		release, err := state.gate.acquire(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if err := client.state().policy.publish(policy(version, mode, rules)); err != nil {
			t.Fatal(err)
		}
	}
	if err = send(ctx); err == nil || SafeNetworkError(err).Decision().Reason != ac.RuleNotAllowed || requests.Load() != 0 {
		t.Fatal("default loopback rule was bypassed", err)
	}
	publish(2, Denylist, modeRules(t))
	if err = send(ctx); err != nil || requests.Load() != 1 {
		t.Fatal("empty denylist did not permit owned HTTP", err)
	}
	rules := modeRules(t, "*.model.example")
	publish(3, Allowlist, rules)
	var gotConn atomic.Bool
	traced := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { gotConn.Store(true); publish(4, Denylist, rules) }})
	if err = send(traced); err == nil || SafeNetworkError(err).Decision().Reason != ac.RuleNotAllowed || !gotConn.Load() || requests.Load() != 1 {
		t.Fatal("mode change after connection leaked a request", err)
	}
	publish(5, Allowlist, rules)
	if err = send(ctx); err != nil || requests.Load() != 2 {
		t.Fatal("domain allowlist retained implicit loopback deny", err)
	}
}
